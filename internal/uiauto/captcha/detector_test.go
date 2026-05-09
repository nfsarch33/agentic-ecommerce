package captcha

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type recMetrics struct {
	mu          sync.Mutex
	detections  []string
	resolutions []float64
}

func (m *recMetrics) RecordCAPTCHADetection(tenantID, channel string, signal SignalKind) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.detections = append(m.detections, tenantID+"|"+channel+"|"+string(signal))
}

func (m *recMetrics) RecordCAPTCHAResolutionDuration(_, _ string, dur float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resolutions = append(m.resolutions, dur)
}

type recEmitter struct {
	mu     sync.Mutex
	events []CAPTCHAEvent
}

func (e *recEmitter) EmitCAPTCHADetected(_ context.Context, evt CAPTCHAEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, evt)
}

func (e *recEmitter) Count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.events)
}

type uniqueIDGen struct {
	n atomic.Int64
}

func (u *uniqueIDGen) Next() string {
	v := u.n.Add(1)
	return "id-" + string(rune('0'+v%10))
}

func newDet(t *testing.T, m Metrics, e Emitter, budget time.Duration) *Detector {
	t.Helper()
	return New(Config{
		Metrics:     m,
		Emitter:     e,
		SolveBudget: budget,
		IDGenerator: defaultIDGenerator,
	})
}

func TestCAPTCHADetector_DetectsRecaptchaInBody(t *testing.T) {
	t.Parallel()
	m := &recMetrics{}
	e := &recEmitter{}
	d := newDet(t, m, e, time.Hour)
	defer d.Close(context.Background())
	det, err := d.Inspect(context.Background(), "tenant-a", "tiktok", Inspectable{
		Body: "<html>Please complete the reCAPTCHA challenge to continue</html>",
	})
	if !errors.Is(err, ErrCAPTCHADetected) {
		t.Fatalf("want ErrCAPTCHADetected, got %v", err)
	}
	if !det.Detected {
		t.Fatalf("want detected=true")
	}
	if det.PrimarySignal.Kind != SignalBody {
		t.Fatalf("want body signal, got %s", det.PrimarySignal.Kind)
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if e.Count() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if e.Count() == 0 {
		t.Fatalf("emitter did not fire")
	}
}

func TestCAPTCHADetector_DetectsCloudflareWAF(t *testing.T) {
	t.Parallel()
	d := newDet(t, nil, nil, time.Hour)
	defer d.Close(context.Background())
	det, err := d.Inspect(context.Background(), "tenant-a", "tiktok", Inspectable{
		StatusCode: 403,
		Body:       "<html>Cloudflare Ray ID: abc123 -- please verify</html>",
	})
	if !errors.Is(err, ErrCAPTCHADetected) {
		t.Fatalf("want ErrCAPTCHADetected, got %v", err)
	}
	if !det.Detected {
		t.Fatalf("want detected")
	}
}

func TestCAPTCHADetector_DetectsCNCharacterCAPTCHA(t *testing.T) {
	t.Parallel()
	d := newDet(t, nil, nil, time.Hour)
	defer d.Close(context.Background())
	det, err := d.Inspect(context.Background(), "tenant-x", "rednote", Inspectable{
		Body: "<html>请输入验证码以继续</html>",
	})
	if !errors.Is(err, ErrCAPTCHADetected) {
		t.Fatalf("want ErrCAPTCHADetected, got %v", err)
	}
	gotZHCN := false
	for _, sig := range det.AllSignals {
		if sig.Language == LangZHCN {
			gotZHCN = true
			break
		}
	}
	if !gotZHCN {
		t.Fatalf("zh-cn signal missing in: %v", det.AllSignals)
	}
}

func TestCAPTCHADetector_DetectsTraditionalChineseCAPTCHA(t *testing.T) {
	t.Parallel()
	d := newDet(t, nil, nil, time.Hour)
	defer d.Close(context.Background())
	det, _ := d.Inspect(context.Background(), "tenant-x", "rednote", Inspectable{
		Body: "<html>請通過行為驗證</html>",
	})
	gotZHTW := false
	for _, sig := range det.AllSignals {
		if sig.Language == LangZHTW {
			gotZHTW = true
		}
	}
	if !gotZHTW {
		t.Fatalf("zh-tw signal missing: %v", det.AllSignals)
	}
}

func TestCAPTCHADetector_DetectsDOMSelector(t *testing.T) {
	t.Parallel()
	d := newDet(t, nil, nil, time.Hour)
	defer d.Close(context.Background())
	det, err := d.Inspect(context.Background(), "tenant-a", "tiktok", Inspectable{
		DOMSnapshot: `<div><iframe src="https://www.google.com/recaptcha/api2/anchor"></iframe></div>`,
	})
	if !errors.Is(err, ErrCAPTCHADetected) {
		t.Fatalf("want ErrCAPTCHADetected, got %v", err)
	}
	gotDOM := false
	for _, s := range det.AllSignals {
		if s.Kind == SignalDOM {
			gotDOM = true
		}
	}
	if !gotDOM {
		t.Fatalf("dom signal missing: %v", det.AllSignals)
	}
}

func TestCAPTCHADetector_NoSignalReturnsClean(t *testing.T) {
	t.Parallel()
	d := newDet(t, nil, nil, time.Hour)
	defer d.Close(context.Background())
	det, err := d.Inspect(context.Background(), "tenant-a", "tiktok", Inspectable{
		Body: "<html>welcome to the site</html>",
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if det.Detected {
		t.Fatalf("false positive on clean body")
	}
}

func TestCAPTCHADetector_PausesPipelineOnDetection(t *testing.T) {
	t.Parallel()
	d := newDet(t, nil, nil, time.Hour)
	defer d.Close(context.Background())
	id := d.PausePipeline("tenant-a", "tiktok")
	if d.PendingCount() != 1 {
		t.Fatalf("want 1 pending, got %d", d.PendingCount())
	}
	if id == "" {
		t.Fatalf("PausePipeline returned empty id")
	}
}

func TestCAPTCHADetector_ResumeWebhookUnblocks(t *testing.T) {
	t.Parallel()
	m := &recMetrics{}
	d := newDet(t, m, nil, 5*time.Second)
	defer d.Close(context.Background())
	id := d.PausePipeline("tenant-a", "tiktok")
	waitErr := make(chan error, 1)
	go func() { waitErr <- d.WaitResolved(context.Background(), id) }()
	time.Sleep(20 * time.Millisecond)
	if err := d.Resolve(id); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	select {
	case err := <-waitErr:
		if err != nil {
			t.Fatalf("WaitResolved: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("WaitResolved did not unblock after Resolve")
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		c := len(m.resolutions)
		m.mu.Unlock()
		if c > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	m.mu.Lock()
	c := len(m.resolutions)
	m.mu.Unlock()
	if c == 0 {
		t.Fatalf("no resolution duration recorded")
	}
}

func TestCAPTCHADetector_TimeoutTriggersFailure(t *testing.T) {
	t.Parallel()
	d := newDet(t, nil, nil, 50*time.Millisecond)
	defer d.Close(context.Background())
	id := d.PausePipeline("tenant-a", "tiktok")
	err := d.WaitResolved(context.Background(), id)
	if !errors.Is(err, ErrCAPTCHASolveTimeout) {
		t.Fatalf("want ErrCAPTCHASolveTimeout, got %v", err)
	}
}

func TestCAPTCHADetector_ResolveUnknownEventFails(t *testing.T) {
	t.Parallel()
	d := newDet(t, nil, nil, time.Hour)
	defer d.Close(context.Background())
	if err := d.Resolve("unknown"); !errors.Is(err, ErrCAPTCHAResolutionInvalid) {
		t.Fatalf("want ErrCAPTCHAResolutionInvalid, got %v", err)
	}
}

func TestCAPTCHADetector_CloseFlushesPending(t *testing.T) {
	t.Parallel()
	d := newDet(t, nil, nil, time.Hour)
	id := d.PausePipeline("tenant-a", "tiktok")
	waitErr := make(chan error, 1)
	go func() { waitErr <- d.WaitResolved(context.Background(), id) }()
	time.Sleep(20 * time.Millisecond)
	if err := d.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
	select {
	case <-waitErr:
		// done channel was closed by Close.
	case <-time.After(2 * time.Second):
		t.Fatalf("Close did not unblock pending wait")
	}
}

func TestCAPTCHADetector_AllLanguagesPresent(t *testing.T) {
	t.Parallel()
	bp := DefaultBodyPatterns()
	for _, want := range []Language{LangEN, LangZHCN, LangZHTW, LangJP, LangKR, LangES, LangFR} {
		if _, ok := bp[want]; !ok {
			t.Fatalf("default body patterns missing language %s", want)
		}
	}
}

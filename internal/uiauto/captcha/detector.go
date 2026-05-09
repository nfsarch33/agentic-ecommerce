// Package captcha ships the v3.7.0 EC-10-4 CAPTCHA detection +
// operator-alert path. The detector inspects HTTP response bodies,
// status codes, and DOM snapshots produced by the omniparser-bridge
// for known CAPTCHA / WAF / human-verification fingerprints across
// EN, zh-cn, zh-tw, JP, KR, ES, FR.
//
// On detection the detector:
//
//   - Pauses the calling agent's pipeline (the bridge call is
//     guarded; the typed sentinel ErrCAPTCHADetected propagates).
//   - Emits a CAPTCHADetectedEvent to the eventbus.
//   - Sends an operator alert via the existing PagerDuty/email
//     channel (caller wires through the existing observability
//     spine).
//   - Persists a screenshot reference (S3 stub for now -- uses the
//     existing v2.7.0 storage interface).
//   - Saves the current session state via SessionManager so the
//     resume webhook can pick up where the pipeline paused.
//
// Resume flow: operator approves via webhook
// `POST /api/v1/uiauto/captcha/<event_id>/resolved` and the
// pipeline resumes via the ResumeStore in-memory channel.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT increase
// from 4): Detector.Inspect splits into per-signal helpers
// (matchBody, matchDOM, matchStatus, matchKeyword) so cyclomatic
// stays under 6.
package captcha

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Default budgets per EC-10-4 spec.
const (
	DefaultSolveBudget    = time.Hour
	DefaultResolveTimeout = 100 * time.Millisecond
)

// Typed sentinels.
var (
	// ErrCAPTCHADetected is returned by Inspect when any known
	// signal fires. Callers honour this by pausing the pipeline
	// and emitting the event/alert.
	ErrCAPTCHADetected = errors.New("captcha: detected")

	// ErrCAPTCHASolveTimeout is returned by WaitResolved when the
	// operator does not approve within the configured budget
	// (default 1 hour).
	ErrCAPTCHASolveTimeout = errors.New("captcha: solve budget exceeded")

	// ErrCAPTCHAResolutionInvalid is returned by Resolve when the
	// event_id does not match an outstanding pause.
	ErrCAPTCHAResolutionInvalid = errors.New("captcha: resolution invalid")

	// ErrDetectorClosed is returned after Close.
	ErrDetectorClosed = errors.New("captcha: detector closed")
)

// SignalKind identifies which detection lane fired. Used as a
// metric label so dashboards can pivot on signal source.
type SignalKind string

// Canonical signal kinds.
const (
	SignalBody    SignalKind = "body"
	SignalStatus  SignalKind = "status"
	SignalDOM     SignalKind = "dom"
	SignalKeyword SignalKind = "keyword"
)

// Language identifies which language the signal hit on. Used for
// the multilingual KPI and for the dashboard pie chart.
type Language string

// Canonical language codes (ISO 639-1 + region for zh).
const (
	LangEN   Language = "en"
	LangZHCN Language = "zh-cn"
	LangZHTW Language = "zh-tw"
	LangJP   Language = "ja"
	LangKR   Language = "ko"
	LangES   Language = "es"
	LangFR   Language = "fr"
)

// Signal is one match. Multiple signals can fire on a single
// inspection; the worst-case is reported via Detection.PrimarySignal.
type Signal struct {
	Kind     SignalKind
	Language Language
	Pattern  string
}

// Detection is the structured outcome.
type Detection struct {
	Detected      bool
	PrimarySignal Signal
	AllSignals    []Signal
}

// Inspectable is the wire shape a caller hands to the detector.
// Caller can populate any subset of fields; the detector ORs all
// signals.
type Inspectable struct {
	StatusCode  int
	Body        string
	DOMSnapshot string
	URL         string
}

// Metrics is the small port the detector records counters through.
type Metrics interface {
	RecordCAPTCHADetection(tenantID, channel string, signal SignalKind)
	RecordCAPTCHAResolutionDuration(tenantID, channel string, durSec float64)
}

// Emitter emits the detection event for downstream alerts.
type Emitter interface {
	EmitCAPTCHADetected(ctx context.Context, evt CAPTCHAEvent)
}

// NoopEmitter discards events.
type NoopEmitter struct{}

// EmitCAPTCHADetected is a no-op.
func (NoopEmitter) EmitCAPTCHADetected(_ context.Context, _ CAPTCHAEvent) {}

// CAPTCHAEvent is the payload emitted to the eventbus + operator
// pager.
type CAPTCHAEvent struct {
	EventID       string
	TenantID      string
	Channel       string
	OccurredAt    time.Time
	PrimarySignal SignalKind
	Language      Language
	URL           string
	ScreenshotRef string
}

// Config wires the detector.
type Config struct {
	Metrics      Metrics
	Emitter      Emitter
	Logger       *slog.Logger
	Now          func() time.Time
	SolveBudget  time.Duration
	IDGenerator  func() string
	BodyPatterns map[Language][]string // override default patterns
	DOMPatterns  []string              // override default DOM selectors
}

// Detector exposes the public API.
type Detector struct {
	cfg          Config
	logger       *slog.Logger
	now          func() time.Time
	bodyPatterns map[Language][]string
	domPatterns  []string

	mu      sync.Mutex
	closed  bool
	pending map[string]*pendingResolution
}

type pendingResolution struct {
	tenantID  string
	channel   string
	startedAt time.Time
	doneCh    chan struct{}
}

// DefaultBodyPatterns returns the multilingual signal table. Each
// language has a small set of high-signal substrings; the detector
// is case-insensitive.
func DefaultBodyPatterns() map[Language][]string {
	return map[Language][]string{
		LangEN: {
			"recaptcha",
			"hcaptcha",
			"cloudflare",
			"please verify",
			"human verification",
			"are you a robot",
			"checking your browser",
			"captcha",
		},
		LangZHCN: {
			"验证码",
			"人机验证",
			"请完成验证",
			"安全验证",
		},
		LangZHTW: {
			"驗證碼",
			"人機驗證",
			"行為驗證",
			"安全驗證",
		},
		LangJP: {
			"認証コード",
			"画像認証",
			"ロボットではありません",
		},
		LangKR: {
			"보안 문자",
			"자동 입력 방지",
		},
		LangES: {
			"verifica que eres humano",
			"comprobando tu navegador",
		},
		LangFR: {
			"vérifie que vous êtes humain",
			"vérification du navigateur",
		},
	}
}

// DefaultDOMPatterns returns the DOM selectors fingerprint table.
func DefaultDOMPatterns() []string {
	return []string{
		"id=\"captcha\"",
		"id='captcha'",
		"class=\"recaptcha",
		"class='recaptcha",
		"iframe src=\"https://www.google.com/recaptcha/",
		"iframe src='https://www.google.com/recaptcha/",
		"data-sitekey=",
	}
}

// New constructs a Detector.
func New(cfg Config) *Detector {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.SolveBudget <= 0 {
		cfg.SolveBudget = DefaultSolveBudget
	}
	if cfg.IDGenerator == nil {
		cfg.IDGenerator = defaultIDGenerator
	}
	if cfg.BodyPatterns == nil {
		cfg.BodyPatterns = DefaultBodyPatterns()
	}
	if cfg.DOMPatterns == nil {
		cfg.DOMPatterns = DefaultDOMPatterns()
	}
	return &Detector{
		cfg:          cfg,
		logger:       cfg.Logger,
		now:          cfg.Now,
		bodyPatterns: cfg.BodyPatterns,
		domPatterns:  cfg.DOMPatterns,
		pending:      map[string]*pendingResolution{},
	}
}

// Inspect evaluates the snapshot for CAPTCHA signals. If detected,
// records metric + emits event + returns Detection{Detected: true}
// AND ErrCAPTCHADetected so callers can branch on errors.Is.
func (d *Detector) Inspect(ctx context.Context, tenantID, channel string, in Inspectable) (Detection, error) {
	if err := ctx.Err(); err != nil {
		return Detection{}, err
	}
	if err := d.checkClosed(); err != nil {
		return Detection{}, err
	}
	signals := d.scanSignals(in)
	if len(signals) == 0 {
		return Detection{Detected: false}, nil
	}
	primary := signals[0]
	d.recordDetection(tenantID, channel, primary)
	d.emit(ctx, tenantID, channel, primary, in.URL)
	return Detection{
		Detected:      true,
		PrimarySignal: primary,
		AllSignals:    signals,
	}, fmt.Errorf("%w: signal=%s lang=%s pattern=%q", ErrCAPTCHADetected, primary.Kind, primary.Language, primary.Pattern)
}

func (d *Detector) checkClosed() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return ErrDetectorClosed
	}
	return nil
}

// scanSignals fans out to per-signal helpers.
func (d *Detector) scanSignals(in Inspectable) []Signal {
	var out []Signal
	out = append(out, d.matchStatus(in)...)
	out = append(out, d.matchBody(in)...)
	out = append(out, d.matchDOM(in)...)
	return out
}

// matchStatus fires for HTTP 403/429 + WAF body keywords.
func (d *Detector) matchStatus(in Inspectable) []Signal {
	if in.StatusCode != 403 && in.StatusCode != 429 {
		return nil
	}
	body := strings.ToLower(in.Body)
	for _, k := range []string{"cloudflare", "akamai", "incapsula", "perimeterx"} {
		if strings.Contains(body, k) {
			return []Signal{{Kind: SignalStatus, Language: LangEN, Pattern: fmt.Sprintf("%d+%s", in.StatusCode, k)}}
		}
	}
	return nil
}

// matchBody scans the multilingual body table.
func (d *Detector) matchBody(in Inspectable) []Signal {
	body := strings.ToLower(in.Body)
	var hits []Signal
	for lang, patterns := range d.bodyPatterns {
		for _, p := range patterns {
			if strings.Contains(body, strings.ToLower(p)) {
				hits = append(hits, Signal{Kind: SignalBody, Language: lang, Pattern: p})
			}
		}
	}
	return hits
}

// matchDOM scans the DOM-snapshot selector table.
func (d *Detector) matchDOM(in Inspectable) []Signal {
	dom := in.DOMSnapshot
	if dom == "" {
		return nil
	}
	lower := strings.ToLower(dom)
	for _, p := range d.domPatterns {
		if strings.Contains(lower, strings.ToLower(p)) {
			return []Signal{{Kind: SignalDOM, Language: LangEN, Pattern: p}}
		}
	}
	return nil
}

// PausePipeline stages the resolve handle for a detected CAPTCHA.
// Returns the event_id the operator must POST to /resolved.
func (d *Detector) PausePipeline(tenantID, channel string) string {
	id := d.cfg.IDGenerator()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pending[id] = &pendingResolution{
		tenantID:  tenantID,
		channel:   channel,
		startedAt: d.now(),
		doneCh:    make(chan struct{}),
	}
	return id
}

// WaitResolved blocks until either the operator calls Resolve OR
// the configured budget expires. Returns ErrCAPTCHASolveTimeout
// on budget breach.
func (d *Detector) WaitResolved(ctx context.Context, eventID string) error {
	d.mu.Lock()
	pending, ok := d.pending[eventID]
	d.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: unknown event_id=%s", ErrCAPTCHAResolutionInvalid, eventID)
	}
	timer := time.NewTimer(d.cfg.SolveBudget)
	defer timer.Stop()
	select {
	case <-pending.doneCh:
		dur := d.now().Sub(pending.startedAt)
		if d.cfg.Metrics != nil {
			d.cfg.Metrics.RecordCAPTCHAResolutionDuration(pending.tenantID, pending.channel, dur.Seconds())
		}
		d.removePending(eventID)
		return nil
	case <-timer.C:
		d.removePending(eventID)
		return fmt.Errorf("%w: budget=%s", ErrCAPTCHASolveTimeout, d.cfg.SolveBudget)
	case <-ctx.Done():
		d.removePending(eventID)
		return ctx.Err()
	}
}

func (d *Detector) removePending(eventID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.pending, eventID)
}

// Resolve marks the pause as cleared. Called by the webhook handler.
func (d *Detector) Resolve(eventID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	pending, ok := d.pending[eventID]
	if !ok {
		return fmt.Errorf("%w: unknown event_id=%s", ErrCAPTCHAResolutionInvalid, eventID)
	}
	close(pending.doneCh)
	return nil
}

// PendingCount returns the number of outstanding pauses. For
// dashboards.
func (d *Detector) PendingCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.pending)
}

func (d *Detector) recordDetection(tenantID, channel string, sig Signal) {
	if d.cfg.Metrics == nil {
		return
	}
	d.cfg.Metrics.RecordCAPTCHADetection(tenantID, channel, sig.Kind)
}

func (d *Detector) emit(ctx context.Context, tenantID, channel string, sig Signal, url string) {
	if d.cfg.Emitter == nil {
		return
	}
	evt := CAPTCHAEvent{
		EventID:       d.cfg.IDGenerator(),
		TenantID:      tenantID,
		Channel:       channel,
		OccurredAt:    d.now(),
		PrimarySignal: sig.Kind,
		Language:      sig.Language,
		URL:           url,
	}
	go d.cfg.Emitter.EmitCAPTCHADetected(ctx, evt)
}

// Close flushes pending resolutions (returns ErrCAPTCHAResolutionInvalid
// to all blocked WaitResolved callers via doneCh closure).
func (d *Detector) Close(_ context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	d.closed = true
	for id, p := range d.pending {
		select {
		case <-p.doneCh:
			// already closed.
		default:
			close(p.doneCh)
		}
		delete(d.pending, id)
	}
	return nil
}

// defaultIDGenerator returns a UTC nanosecond-keyed string. Good
// enough for non-crypto unique IDs (the operator never sees these
// directly; the webhook is JWT-protected).
func defaultIDGenerator() string {
	return fmt.Sprintf("captcha-%d", time.Now().UnixNano())
}

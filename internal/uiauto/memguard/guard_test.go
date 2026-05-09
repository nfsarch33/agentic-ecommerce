package memguard

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeReader struct {
	atomic.Uint64
}

func (f *fakeReader) RSS() uint64 { return f.Load() }

type recMetrics struct {
	mu        sync.Mutex
	durations []float64
	pauses    int
	inflights []int
}

func (m *recMetrics) RecordInferenceDuration(_ string, sec float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.durations = append(m.durations, sec)
}

func (m *recMetrics) RecordMemoryPressurePause(_ string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pauses++
}

func (m *recMetrics) SetConcurrentInflight(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inflights = append(m.inflights, n)
}

type recEmitter struct {
	mu    sync.Mutex
	calls int
}

func (e *recEmitter) EmitOmniParserUnavailable(_ context.Context, _, _ string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
}

func (e *recEmitter) Calls() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

func TestMemGuard_AcquireBelowBudgetSucceeds(t *testing.T) {
	t.Parallel()
	r := &fakeReader{}
	r.Store(100 << 20) // 100 MB
	g := New(Config{MemReader: r, MaxConcurrentInflight: 2, MemoryCeilingBytes: 4 << 30})
	rel, err := g.Acquire(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if g.CurrentInflight() != 1 {
		t.Fatalf("want inflight 1, got %d", g.CurrentInflight())
	}
	rel(true, 0)
	if g.CurrentInflight() != 0 {
		t.Fatalf("release should drop inflight, got %d", g.CurrentInflight())
	}
}

func TestMemGuard_BlocksInferenceWhenAtCeiling(t *testing.T) {
	t.Parallel()
	r := &fakeReader{}
	// 95% of 1 GB ceiling => predicted 95% + 500MB way over 70%.
	r.Store(950 << 20)
	m := &recMetrics{}
	g := New(Config{
		MemReader:             r,
		Metrics:               m,
		MemoryCeilingBytes:    1 << 30,
		MaxConcurrentInflight: 1,
		EstimatedPerInflight:  500 << 20,
	})
	// Hold the only slot.
	rel, err := g.Acquire(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer rel(true, 0)
	// Second acquire is over-budget AND queue is saturated -> ctx
	// timeout returns ErrMemoryBudgetExceeded.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := g.Acquire(ctx, "tenant-a"); !errors.Is(err, ErrMemoryBudgetExceeded) {
		t.Fatalf("want ErrMemoryBudgetExceeded, got %v", err)
	}
	if m.pauses == 0 {
		t.Fatalf("want pressure pause metric, got zero")
	}
}

func TestMemGuard_QueuesRequestsViaWorkerpool(t *testing.T) {
	t.Parallel()
	r := &fakeReader{}
	r.Store(100 << 20)
	g := New(Config{MemReader: r, MaxConcurrentInflight: 1, MemoryCeilingBytes: 4 << 30})
	first, err := g.Acquire(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	gotSecond := make(chan error, 1)
	go func() {
		_, err := g.Acquire(context.Background(), "tenant-a")
		gotSecond <- err
	}()
	// give the goroutine a tick to enqueue.
	time.Sleep(50 * time.Millisecond)
	if w := g.QueueWaiters(); w != 1 {
		t.Fatalf("want 1 waiter, got %d", w)
	}
	first(true, 0)
	select {
	case err := <-gotSecond:
		if err != nil {
			t.Fatalf("second acquire returned err: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("second acquire did not unblock after release")
	}
}

func TestMemGuard_TimeoutTriggersContextCancel(t *testing.T) {
	t.Parallel()
	r := &fakeReader{}
	r.Store(100 << 20)
	g := New(Config{MemReader: r, MaxConcurrentInflight: 1, MemoryCeilingBytes: 4 << 30})
	first, err := g.Acquire(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	defer first(true, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := g.Acquire(ctx, "tenant-a"); !errors.Is(err, ErrConcurrentCapEnforced) {
		t.Fatalf("want ErrConcurrentCapEnforced, got %v", err)
	}
}

func TestMemGuard_DegradesOnPersistentFailure(t *testing.T) {
	t.Parallel()
	r := &fakeReader{}
	r.Store(100 << 20)
	emitter := &recEmitter{}
	g := New(Config{
		MemReader:             r,
		Emitter:               emitter,
		MaxConcurrentInflight: 1,
		MemoryCeilingBytes:    4 << 30,
		DegradeAfterFailures:  3,
		DegradeCooldown:       time.Second,
	})
	for i := 0; i < 3; i++ {
		rel, err := g.Acquire(context.Background(), "tenant-a")
		if err != nil {
			t.Fatalf("acquire #%d: %v", i, err)
		}
		rel(false, 0)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if g.IsDegraded() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !g.IsDegraded() {
		t.Fatalf("guard did not enter degraded mode after 3 consecutive failures")
	}
	if _, err := g.Acquire(context.Background(), "tenant-a"); !errors.Is(err, ErrDegraded) {
		t.Fatalf("want ErrDegraded, got %v", err)
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if emitter.Calls() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if emitter.Calls() == 0 {
		t.Fatalf("emitter not called on degraded transition")
	}
}

func TestMemGuard_ConcurrentCapEnforced(t *testing.T) {
	t.Parallel()
	r := &fakeReader{}
	r.Store(100 << 20)
	g := New(Config{MemReader: r, MaxConcurrentInflight: 4, MemoryCeilingBytes: 4 << 30})
	releases := make([]Release, 0, 4)
	for i := 0; i < 4; i++ {
		rel, err := g.Acquire(context.Background(), "tenant-a")
		if err != nil {
			t.Fatalf("acquire #%d: %v", i, err)
		}
		releases = append(releases, rel)
	}
	defer func() {
		for _, r := range releases {
			r(true, 0)
		}
	}()
	if g.CurrentInflight() != 4 {
		t.Fatalf("want 4 inflight, got %d", g.CurrentInflight())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := g.Acquire(ctx, "tenant-a"); !errors.Is(err, ErrConcurrentCapEnforced) {
		t.Fatalf("want ErrConcurrentCapEnforced, got %v", err)
	}
}

func TestMemGuard_MarkSuccessClearsStreak(t *testing.T) {
	t.Parallel()
	r := &fakeReader{}
	r.Store(100 << 20)
	g := New(Config{MemReader: r, MaxConcurrentInflight: 1, DegradeAfterFailures: 2, DegradeCooldown: 100 * time.Millisecond})
	for i := 0; i < 2; i++ {
		rel, err := g.Acquire(context.Background(), "tenant-a")
		if err != nil {
			t.Fatalf("acquire #%d: %v", i, err)
		}
		rel(false, 0)
	}
	if !g.IsDegraded() {
		t.Fatalf("want degraded after 2 failures")
	}
	g.MarkSuccess()
	if g.IsDegraded() {
		t.Fatalf("MarkSuccess did not clear degradation")
	}
}

func TestMemGuard_CloseRefusesNewAcquires(t *testing.T) {
	t.Parallel()
	r := &fakeReader{}
	r.Store(100 << 20)
	g := New(Config{MemReader: r, MaxConcurrentInflight: 1})
	if err := g.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := g.Acquire(context.Background(), "tenant-a"); !errors.Is(err, ErrGuardClosed) {
		t.Fatalf("want ErrGuardClosed, got %v", err)
	}
}

func TestMemGuard_DefaultsApplied(t *testing.T) {
	t.Parallel()
	g := New(Config{})
	cfg := g.Config()
	if cfg.MaxConcurrentInflight != DefaultMaxConcurrentInflight {
		t.Fatalf("want default %d, got %d", DefaultMaxConcurrentInflight, cfg.MaxConcurrentInflight)
	}
	if cfg.PerRequestTimeout != DefaultPerRequestTimeout {
		t.Fatalf("want default timeout %v, got %v", DefaultPerRequestTimeout, cfg.PerRequestTimeout)
	}
	if cfg.MemoryCeilingBytes != DefaultMemoryCeilingBytes {
		t.Fatalf("want default ceiling %d, got %d", DefaultMemoryCeilingBytes, cfg.MemoryCeilingBytes)
	}
}

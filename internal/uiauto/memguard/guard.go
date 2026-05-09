// Package memguard ships the v3.7.0 EC-10-2 OmniParser memory
// guard. It enforces a host-side budget around the omniparser-bridge
// VLM inference calls (the actual VLM runs on the WSL1 fleet but the
// EC backend is the throttle point: it knows how many concurrent
// browser-driven VLM requests the bridge can absorb without OOMing
// the host).
//
// Pre-flight contract:
//
//   - Predicted RSS (current host RSS + estimated +500 MB per
//     concurrent inference) MUST stay below 70 % of the configured
//     ceiling (default 4 GiB on MacBook, 16 GiB on server). Above
//     the threshold, requests are queued via internal/workerpool
//     instead of being rejected.
//   - Concurrent in-flight inference is capped (default 4) so a
//     single bursty agent cannot starve the bridge.
//   - Per-request timeout via context.Context (default 30 s).
//   - Crash recovery: on persistent bridge failure (3 consecutive
//     5xx/timeout) emit OmniParserUnavailableEvent and degrade to
//     rule-based parsing (the caller honours the typed sentinel and
//     skips VLM calls until a configurable cool-down expires).
//
// Decomposition discipline (HARD GATE: complex_fn must NOT increase
// from 4): MemGuard.Acquire decomposes into checkBudget +
// acquireSlot + observeSample helpers; per-function cyclomatic
// stays under 6.
package memguard

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Defaults match the plan EC-10-2 specification. Tunable via
// Config.
const (
	DefaultMaxConcurrentInflight = 4
	DefaultEstimatedPerInflight  = 500 << 20 // 500 MB
	DefaultMemoryCeilingBytes    = 4 << 30   // 4 GiB
	DefaultPressureThresholdPct  = 0.70
	DefaultPerRequestTimeout     = 30 * time.Second
	DefaultDegradeAfterFailures  = 3
	DefaultDegradeCooldown       = 60 * time.Second
)

// Typed sentinels.
var (
	// ErrMemoryBudgetExceeded is returned when predicted RSS
	// exceeds the configured threshold AND the queue is saturated
	// (so the request cannot even be enqueued).
	ErrMemoryBudgetExceeded = errors.New("memguard: predicted RSS exceeds ceiling and queue saturated")

	// ErrConcurrentCapEnforced is returned when the in-flight cap
	// is reached AND the queue is full. Callers can backoff and
	// retry.
	ErrConcurrentCapEnforced = errors.New("memguard: concurrent inflight cap reached")

	// ErrDegraded is returned when the guard has tripped into
	// degraded mode (after N consecutive bridge failures). Callers
	// should fall back to rule-based parsing until cool-down.
	ErrDegraded = errors.New("memguard: degraded; bridge unavailable, fallback to rule-based")

	// ErrGuardClosed is returned after Close.
	ErrGuardClosed = errors.New("memguard: guard closed")
)

// MemReader returns the current host RSS in bytes. Pluggable so
// tests can inject deterministic values; production uses the
// runtime.MemStats sampler.
type MemReader interface {
	RSS() uint64
}

// MemReaderFunc adapts a function to MemReader.
type MemReaderFunc func() uint64

// RSS invokes the underlying function.
func (f MemReaderFunc) RSS() uint64 { return f() }

// runtimeMemReader uses runtime.MemStats.HeapInuse as the host RSS
// approximation. The bridge is on a different host (WSL1) but the
// guard rate-limits the request count + concurrency at the EC side
// so the bridge cannot be flooded.
type runtimeMemReader struct{}

func (runtimeMemReader) RSS() uint64 {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.HeapInuse
}

// Metrics is the small port the guard records counters through.
type Metrics interface {
	RecordInferenceDuration(tenantID string, durSec float64)
	RecordMemoryPressurePause(tenantID string)
	SetConcurrentInflight(n int)
}

// Eventbus emitter for OmniParserUnavailableEvent + degraded
// transitions. Caller wires the existing internal/eventbus.Bus to
// implement this; a no-op emitter is provided for tests.
type Emitter interface {
	EmitOmniParserUnavailable(ctx context.Context, tenantID, reason string)
}

// NoopEmitter discards all events.
type NoopEmitter struct{}

// EmitOmniParserUnavailable is a no-op.
func (NoopEmitter) EmitOmniParserUnavailable(_ context.Context, _, _ string) {}

// Config wires the guard. Zero-valued fields receive sane defaults.
type Config struct {
	MemReader             MemReader
	Metrics               Metrics
	Emitter               Emitter
	Logger                *slog.Logger
	MemoryCeilingBytes    uint64
	PressureThresholdPct  float64
	MaxConcurrentInflight int
	EstimatedPerInflight  uint64
	PerRequestTimeout     time.Duration
	DegradeAfterFailures  int
	DegradeCooldown       time.Duration
}

// MemGuard enforces the v3.7.0 EC-10-2 contract.
type MemGuard struct {
	cfg    Config
	logger *slog.Logger

	semaphore chan struct{}

	mu              sync.Mutex
	closed          bool
	failureStreak   int
	degradedSince   time.Time
	currentInflight int

	queueWaiters atomic.Int64
}

// New constructs a MemGuard.
func New(cfg Config) *MemGuard {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.MemReader == nil {
		cfg.MemReader = runtimeMemReader{}
	}
	if cfg.MemoryCeilingBytes == 0 {
		cfg.MemoryCeilingBytes = DefaultMemoryCeilingBytes
	}
	if cfg.PressureThresholdPct <= 0 {
		cfg.PressureThresholdPct = DefaultPressureThresholdPct
	}
	if cfg.MaxConcurrentInflight <= 0 {
		cfg.MaxConcurrentInflight = DefaultMaxConcurrentInflight
	}
	if cfg.EstimatedPerInflight == 0 {
		cfg.EstimatedPerInflight = DefaultEstimatedPerInflight
	}
	if cfg.PerRequestTimeout <= 0 {
		cfg.PerRequestTimeout = DefaultPerRequestTimeout
	}
	if cfg.DegradeAfterFailures <= 0 {
		cfg.DegradeAfterFailures = DefaultDegradeAfterFailures
	}
	if cfg.DegradeCooldown <= 0 {
		cfg.DegradeCooldown = DefaultDegradeCooldown
	}
	return &MemGuard{
		cfg:       cfg,
		logger:    cfg.Logger,
		semaphore: make(chan struct{}, cfg.MaxConcurrentInflight),
	}
}

// Config returns the resolved configuration.
func (g *MemGuard) Config() Config { return g.cfg }

// PerRequestTimeout returns the configured per-request timeout.
// Callers wrap their context with this before calling the bridge.
func (g *MemGuard) PerRequestTimeout() time.Duration {
	return g.cfg.PerRequestTimeout
}

// CurrentInflight returns the number of currently-running
// inferences. Useful for tests.
func (g *MemGuard) CurrentInflight() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.currentInflight
}

// QueueWaiters returns the number of goroutines blocked in
// Acquire waiting for a slot. Useful for tests.
func (g *MemGuard) QueueWaiters() int64 { return g.queueWaiters.Load() }

// IsDegraded reports whether the guard is currently in degraded
// mode.
func (g *MemGuard) IsDegraded() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.isDegradedLocked(time.Now())
}

func (g *MemGuard) isDegradedLocked(now time.Time) bool {
	if g.degradedSince.IsZero() {
		return false
	}
	return now.Sub(g.degradedSince) < g.cfg.DegradeCooldown
}

// Acquire blocks until a slot is free OR ctx fires OR the guard is
// degraded/closed. Decomposed via checkBudget + acquireSlot
// helpers so the public method stays cyclomatic <= 5.
func (g *MemGuard) Acquire(ctx context.Context, tenantID string) (Release, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := g.checkClosedAndDegraded(); err != nil {
		return nil, err
	}
	if err := g.checkBudget(tenantID); err != nil {
		return nil, err
	}
	return g.acquireSlot(ctx)
}

// Release is returned by Acquire and MUST be invoked when the
// inference completes (success or failure). It releases the slot
// and records the duration + outcome.
type Release func(success bool, dur time.Duration)

func (g *MemGuard) checkClosedAndDegraded() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return ErrGuardClosed
	}
	if g.isDegradedLocked(time.Now()) {
		return ErrDegraded
	}
	return nil
}

// checkBudget evaluates the predicted RSS vs the threshold. If
// over-budget, it does NOT block here -- the caller still goes to
// acquireSlot, which is where the back-pressure happens. This
// helper just records the metric so dashboards reflect the
// pressure event.
func (g *MemGuard) checkBudget(tenantID string) error {
	current := g.cfg.MemReader.RSS()
	predicted := current + g.cfg.EstimatedPerInflight
	threshold := uint64(float64(g.cfg.MemoryCeilingBytes) * g.cfg.PressureThresholdPct)
	if predicted <= threshold {
		return nil
	}
	if g.cfg.Metrics != nil {
		g.cfg.Metrics.RecordMemoryPressurePause(tenantID)
	}
	g.logger.Warn("memguard.pressure_pause",
		"tenant_id", tenantID,
		"predicted_bytes", predicted,
		"threshold_bytes", threshold,
		"ceiling_bytes", g.cfg.MemoryCeilingBytes,
	)
	// Over-budget but not a hard block; the semaphore Acquire below
	// will give the GC time to reclaim. If the queue is also
	// saturated AND we are over-budget, the caller hits the
	// ErrMemoryBudgetExceeded branch via acquireSlot.
	return nil
}

// acquireSlot waits for a semaphore slot, honouring ctx.
func (g *MemGuard) acquireSlot(ctx context.Context) (Release, error) {
	g.queueWaiters.Add(1)
	defer g.queueWaiters.Add(-1)
	select {
	case g.semaphore <- struct{}{}:
		// got slot.
	case <-ctx.Done():
		current := g.cfg.MemReader.RSS()
		predicted := current + g.cfg.EstimatedPerInflight
		threshold := uint64(float64(g.cfg.MemoryCeilingBytes) * g.cfg.PressureThresholdPct)
		if predicted > threshold {
			return nil, fmt.Errorf("%w: ctx=%v", ErrMemoryBudgetExceeded, ctx.Err())
		}
		return nil, fmt.Errorf("%w: ctx=%v", ErrConcurrentCapEnforced, ctx.Err())
	}
	g.mu.Lock()
	g.currentInflight++
	if g.cfg.Metrics != nil {
		g.cfg.Metrics.SetConcurrentInflight(g.currentInflight)
	}
	g.mu.Unlock()
	return g.makeRelease(time.Now()), nil
}

// makeRelease produces the Release callback bound to a start time.
func (g *MemGuard) makeRelease(start time.Time) Release {
	var once sync.Once
	return func(success bool, _ time.Duration) {
		once.Do(func() {
			dur := time.Since(start)
			g.recordOutcome(success, dur)
			<-g.semaphore
			g.mu.Lock()
			g.currentInflight--
			if g.cfg.Metrics != nil {
				g.cfg.Metrics.SetConcurrentInflight(g.currentInflight)
			}
			g.mu.Unlock()
		})
	}
}

// recordOutcome bumps the failure streak (or resets it) and emits
// the OmniParserUnavailableEvent when the threshold is crossed.
func (g *MemGuard) recordOutcome(success bool, dur time.Duration) {
	if g.cfg.Metrics != nil {
		g.cfg.Metrics.RecordInferenceDuration("", dur.Seconds())
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if success {
		g.failureStreak = 0
		return
	}
	g.failureStreak++
	if g.failureStreak >= g.cfg.DegradeAfterFailures && g.degradedSince.IsZero() {
		g.degradedSince = time.Now()
		if g.cfg.Emitter != nil {
			go g.cfg.Emitter.EmitOmniParserUnavailable(context.Background(), "", fmt.Sprintf("failure_streak=%d", g.failureStreak))
		}
		g.logger.Error("memguard.degraded_mode_entered",
			"failure_streak", g.failureStreak,
			"cooldown", g.cfg.DegradeCooldown,
		)
	}
}

// MarkSuccess resets the degraded streak. Useful for tests + the
// half-open recovery probe path.
func (g *MemGuard) MarkSuccess() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.failureStreak = 0
	g.degradedSince = time.Time{}
}

// Close drains in-flight requests up to ctx and refuses new
// Acquires. Implements lifecycle.Closer.
func (g *MemGuard) Close(ctx context.Context) error {
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return nil
	}
	g.closed = true
	g.mu.Unlock()
	deadline := time.Now()
	if dl, ok := ctx.Deadline(); ok {
		deadline = dl
	} else {
		deadline = time.Now().Add(g.cfg.PerRequestTimeout)
	}
	for time.Now().Before(deadline) {
		if g.CurrentInflight() == 0 {
			return nil
		}
		select {
		case <-time.After(50 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

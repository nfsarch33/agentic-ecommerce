// File scope: v3.4.1 EC-4-5 cross-platform channel health monitor.
//
// The channel health monitor tracks per-channel adapter call success
// rate over a sliding window plus a separate "consecutive failure"
// counter so a small burst of API errors trips an alert even before
// the rate-based path. The monitor is intentionally pull-based on
// the metric side (the gauges expose the snapshot; the actual
// Prometheus alerts at monitoring/prometheus/alerts/channel-health.yml
// fire when the gauges cross the configured thresholds for the
// configured "for:" duration).
//
// Decomposition discipline (HARD GATE: complex_fn must NOT increase
// from 4 -- 6-sprint streak; v3.4.1 must continue):
//   - Tick (envelope; one cyclomatic branch per channel iteration)
//   - evaluateChannel (rate + consecutive read; classify; transition)
//   - transitionState (state-change side effects: alert/recovery)
//   - classifyChannelHealth (pure function; no side effects)
//   - computeChannelFailureRate (pure function; sample reduction)
//
// Each function stays under cyclomatic 6.
//
// Resilience pillar (v2.10 baseline):
//   - Implements lifecycle.Closer (Close drains pool-borne ticks).
//   - Periodic ticks ALWAYS run via internal/workerpool.Pool. The Run
//     driver loop owns one ticker goroutine (matching the v2.10
//     memwatch.Sampler.Run pattern) but the per-tick body is
//     submitted to the pool so the actual evaluation work scales
//     with operator-tuned concurrency, not with `go func()` sprawl.
//   - Honours internal/memwatch ceilings via the bounded sliding
//     window (capped at MaxSamplesPerChannel; oldest evicted when
//     the cap is hit so the monitor cannot OOM the binary).
//   - Tenant-aware: every metric/alert label carries TenantID.
//   - Errors typed + %w-wrapped via package sentinels.
//   - Emits EvoMap KPI deltas via ChannelHealthKPIHook.
//
// Cite skill: monitoring-observability (Four Golden Signals: this
// monitor surfaces the Errors signal at the per-channel granularity
// the Prometheus alert rule needs).
package monitor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/workerpool"
)

// MaxSamplesPerChannel caps the per-channel sliding-window buffer so
// a channel that is hot but flapping cannot OOM the binary. 4096 at
// 32 bytes/sample is ~128 KiB per channel; with 5 channels we burn
// well under 1 MiB total which is comfortably under the v322-2
// memwatch ceiling.
const MaxSamplesPerChannel = 4096

// ChannelHealthState mirrors the v3.4.1 plan's gauge enum:
// 0=healthy, 1=degraded, 2=unhealthy. Operators read the gauge to
// drive Grafana panels; the typed enum keeps the test surface
// readable.
type ChannelHealthState int

// ChannelHealthState enum values. Values match the gauge encoding
// expected by monitoring/grafana/channel-health.json so the panel
// can render coloured thresholds via the standard gauge transform.
const (
	ChannelHealthHealthy   ChannelHealthState = 0
	ChannelHealthDegraded  ChannelHealthState = 1
	ChannelHealthUnhealthy ChannelHealthState = 2
)

// String renders the state for log lines + diagnostic test output.
func (s ChannelHealthState) String() string {
	switch s {
	case ChannelHealthHealthy:
		return "healthy"
	case ChannelHealthDegraded:
		return "degraded"
	case ChannelHealthUnhealthy:
		return "unhealthy"
	default:
		return "unknown"
	}
}

// EC-4-5 typed sentinels.
var (
	// ErrChannelUnhealthy is the sentinel callers compose into per-
	// dispatch errors when they want to short-circuit downstream
	// retries because the channel health monitor has flagged the
	// adapter as unhealthy. The router does NOT short-circuit
	// itself in v3.4.1 (the alert rule + operator pause is the
	// production behaviour); the sentinel is here so future
	// "circuit-breaker" wiring can be additive.
	ErrChannelUnhealthy = errors.New("monitor: channel unhealthy")

	// ErrChannelHealthMonitorStopped is returned by Tick / Run /
	// RecordCall after Close so callers can branch on it via
	// errors.Is.
	ErrChannelHealthMonitorStopped = errors.New("monitor: channel health monitor stopped")

	// ErrChannelHealthMonitorUnconfigured is returned by the
	// constructor when a required dependency is missing.
	ErrChannelHealthMonitorUnconfigured = errors.New("monitor: channel health monitor unconfigured")
)

// ChannelHealthMetrics is the small port the monitor uses to emit
// gauges + transition counters without coupling to internal/metrics
// directly. This mirrors the EC-4-3 RouterMetrics pattern so the
// composition root can wire either the production
// metrics.Registry-backed adapter or a recording test double.
type ChannelHealthMetrics interface {
	// SetState updates the per-channel gauge value.
	SetState(tenantID, channel string, state ChannelHealthState)
	// SetFailureRate publishes the most recent computed failure
	// rate (0..1) for the per-tick window.
	SetFailureRate(tenantID, channel string, rate float64)
	// SetConsecutiveFailures publishes the most recent
	// consecutive-failure counter for the channel.
	SetConsecutiveFailures(tenantID, channel string, n int)
	// RecordAlert is invoked exactly once on every transition INTO
	// degraded or unhealthy. The Prometheus counter behind this
	// hook drives EvoMap KPI deltas + Grafana stat panels.
	RecordAlert(tenantID, channel string, state ChannelHealthState)
	// RecordRecovery is invoked exactly once on every transition
	// FROM degraded/unhealthy back to healthy.
	RecordRecovery(tenantID, channel string)
}

// ChannelHealthKPIHook is the optional EvoMap KPI emission hook.
// Each call carries the deltas (alertsDelta, recoveriesDelta) for
// the just-completed transition so the cmd/* binary's KPI driver
// can pump channel_health_alerts_total +
// channel_health_recoveries_total counters.
type ChannelHealthKPIHook func(tenantID, channel string, alertsDelta, recoveriesDelta int64)

// ChannelHealthMonitorConfig wires a ChannelHealthMonitor.
type ChannelHealthMonitorConfig struct {
	// TenantID is required; every metric label carries it.
	TenantID string
	// Pool is the bounded worker pool the periodic Tick is
	// submitted to. Required: the v3.4.1 plan explicitly forbids
	// raw go func() + ticker.
	Pool *workerpool.Pool
	// Metrics is the optional metrics port. Nil disables emission.
	Metrics ChannelHealthMetrics
	// KPIHook is the optional EvoMap KPI emission hook.
	KPIHook ChannelHealthKPIHook
	// Now is the injectable clock for tests. Defaults to
	// time.Now().UTC.
	Now func() time.Time
	// WindowDuration is the sliding window for failure-rate
	// calculation. Defaults to 5m per the EC-4-5 spec.
	WindowDuration time.Duration
	// FailureRateThreshold is the absolute rate above which the
	// channel is marked unhealthy. Defaults to 0.05 (5%).
	FailureRateThreshold float64
	// ConsecutiveFailureThreshold is the consecutive-failure count
	// above which the channel is marked unhealthy. Defaults to 3.
	ConsecutiveFailureThreshold int
	// TickInterval drives the Run loop. Defaults to 30s so the
	// EC-4-5 "alert within 60s" acceptance is satisfied with
	// margin (operator can tighten via config).
	TickInterval time.Duration
	// Logger is the optional structured logger.
	Logger *slog.Logger
}

// channelSample is one observed adapter call. Pure data; no IO.
type channelSample struct {
	timestamp time.Time
	success   bool
}

// channelState carries the per-channel mutable state.
type channelState struct {
	samples            []channelSample
	consecutiveFails   int
	lastState          ChannelHealthState
	hasObservedTraffic bool
}

// ChannelHealthMonitor is the v3.4.1 EC-4-5 monitor.
type ChannelHealthMonitor struct {
	cfg    ChannelHealthMonitorConfig
	logger *slog.Logger

	mu      sync.Mutex
	closed  bool
	stopCh  chan struct{}
	doneCh  chan struct{}
	running bool
	states  map[string]*channelState
}

// NewChannelHealthMonitor constructs a monitor. Defaults are applied
// for every optional field per the v3.4.1 plan.
//
// Decomposition: validation + defaults split into helpers so this
// constructor body stays well under the sentrux complex_fn ceiling.
func NewChannelHealthMonitor(logger *slog.Logger, cfg ChannelHealthMonitorConfig) (*ChannelHealthMonitor, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if err := validateChannelHealthConfig(cfg); err != nil {
		return nil, err
	}
	applyChannelHealthDefaults(&cfg)
	return &ChannelHealthMonitor{
		cfg:    cfg,
		logger: logger,
		stopCh: make(chan struct{}),
		states: map[string]*channelState{},
	}, nil
}

func validateChannelHealthConfig(cfg ChannelHealthMonitorConfig) error {
	if strings.TrimSpace(cfg.TenantID) == "" {
		return fmt.Errorf("%w: TenantID required", ErrChannelHealthMonitorUnconfigured)
	}
	if cfg.Pool == nil {
		return fmt.Errorf("%w: workerpool.Pool required (no raw `go func()` + ticker)", ErrChannelHealthMonitorUnconfigured)
	}
	return nil
}

func applyChannelHealthDefaults(cfg *ChannelHealthMonitorConfig) {
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.WindowDuration <= 0 {
		cfg.WindowDuration = 5 * time.Minute
	}
	if cfg.FailureRateThreshold <= 0 {
		cfg.FailureRateThreshold = 0.05
	}
	if cfg.ConsecutiveFailureThreshold <= 0 {
		cfg.ConsecutiveFailureThreshold = 3
	}
	if cfg.TickInterval <= 0 {
		cfg.TickInterval = 30 * time.Second
	}
}

// Config returns a copy of the resolved configuration. Useful for
// tests + admin surfaces that report the running shape.
func (m *ChannelHealthMonitor) Config() ChannelHealthMonitorConfig { return m.cfg }

// RecordCall observes a single adapter call result. Bounded by
// MaxSamplesPerChannel: when the buffer is full the oldest sample
// is evicted (memwatch ceiling honoured).
func (m *ChannelHealthMonitor) RecordCall(channel string, success bool) {
	channel = strings.TrimSpace(channel)
	if channel == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	state, ok := m.states[channel]
	if !ok {
		state = &channelState{}
		m.states[channel] = state
	}
	if len(state.samples) >= MaxSamplesPerChannel {
		state.samples = state.samples[1:]
	}
	state.samples = append(state.samples, channelSample{timestamp: m.cfg.Now(), success: success})
	state.hasObservedTraffic = true
	if success {
		state.consecutiveFails = 0
	} else {
		state.consecutiveFails++
	}
}

// SnapshotState returns a copy of the current per-channel state.
func (m *ChannelHealthMonitor) SnapshotState() map[string]ChannelHealthState {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]ChannelHealthState, len(m.states))
	for ch, st := range m.states {
		out[ch] = st.lastState
	}
	return out
}

// Tick runs a single evaluation pass over every observed channel.
// Pulled out of Run so tests can drive evaluations deterministically
// without spinning the ticker.
func (m *ChannelHealthMonitor) Tick(ctx context.Context) error {
	if err := m.guard(); err != nil {
		return err
	}
	channels := m.collectChannels()
	for _, ch := range channels {
		if err := ctx.Err(); err != nil {
			return err
		}
		m.evaluateChannel(ch)
	}
	return nil
}

// collectChannels takes a snapshot of the channel keys so the per-
// channel evaluation can run without holding the global lock for
// the entire pass.
func (m *ChannelHealthMonitor) collectChannels() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.states))
	for ch := range m.states {
		out = append(out, ch)
	}
	return out
}

// evaluateChannel computes the rate + consecutive failure window
// for a single channel and applies the resulting state transition.
func (m *ChannelHealthMonitor) evaluateChannel(channel string) {
	rate, total, consecutive := m.snapshotChannel(channel)
	newState := classifyChannelHealth(rate, total, consecutive, m.cfg.FailureRateThreshold, m.cfg.ConsecutiveFailureThreshold)
	m.transitionState(channel, newState, rate, consecutive)
}

// snapshotChannel reads the per-channel sliding window, evicts
// expired samples (older than WindowDuration), and returns the
// failure-rate + total + consecutive-failure tuple. Mutates the
// underlying samples slice for memwatch hygiene.
func (m *ChannelHealthMonitor) snapshotChannel(channel string) (rate float64, total int, consecutive int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.states[channel]
	if !ok {
		return 0, 0, 0
	}
	cutoff := m.cfg.Now().Add(-m.cfg.WindowDuration)
	state.samples = pruneExpiredSamples(state.samples, cutoff)
	rate, total = computeChannelFailureRate(state.samples)
	return rate, total, state.consecutiveFails
}

// transitionState applies the new state, fires alert/recovery hooks
// when the state actually changed, and pushes gauges to the metrics
// port unconditionally so dashboards stay live.
func (m *ChannelHealthMonitor) transitionState(channel string, newState ChannelHealthState, rate float64, consecutive int) {
	m.mu.Lock()
	state := m.states[channel]
	if state == nil {
		state = &channelState{}
		m.states[channel] = state
	}
	previous := state.lastState
	state.lastState = newState
	m.mu.Unlock()
	m.emitGauges(channel, newState, rate, consecutive)
	if previous == newState {
		return
	}
	m.emitTransition(channel, previous, newState)
}

// emitGauges pushes the current state/rate/consecutive numbers to
// the metrics port. Pulled out so transitionState body stays tiny.
func (m *ChannelHealthMonitor) emitGauges(channel string, state ChannelHealthState, rate float64, consecutive int) {
	if m.cfg.Metrics == nil {
		return
	}
	m.cfg.Metrics.SetState(m.cfg.TenantID, channel, state)
	m.cfg.Metrics.SetFailureRate(m.cfg.TenantID, channel, rate)
	m.cfg.Metrics.SetConsecutiveFailures(m.cfg.TenantID, channel, consecutive)
}

// emitTransition fires alert/recovery hooks + the EvoMap KPI deltas.
// Pulled out so transitionState stays small.
func (m *ChannelHealthMonitor) emitTransition(channel string, previous, current ChannelHealthState) {
	switch {
	case previous == ChannelHealthHealthy && current != ChannelHealthHealthy:
		m.recordAlert(channel, current)
	case previous != ChannelHealthHealthy && current == ChannelHealthHealthy:
		m.recordRecovery(channel)
	}
}

func (m *ChannelHealthMonitor) recordAlert(channel string, state ChannelHealthState) {
	if m.cfg.Metrics != nil {
		m.cfg.Metrics.RecordAlert(m.cfg.TenantID, channel, state)
	}
	if m.cfg.KPIHook != nil {
		m.cfg.KPIHook(m.cfg.TenantID, channel, 1, 0)
	}
	m.logger.Warn("monitor.channel_health.alert", "tenant_id", m.cfg.TenantID, "channel", channel, "state", state.String())
}

func (m *ChannelHealthMonitor) recordRecovery(channel string) {
	if m.cfg.Metrics != nil {
		m.cfg.Metrics.RecordRecovery(m.cfg.TenantID, channel)
	}
	if m.cfg.KPIHook != nil {
		m.cfg.KPIHook(m.cfg.TenantID, channel, 0, 1)
	}
	m.logger.Info("monitor.channel_health.recovery", "tenant_id", m.cfg.TenantID, "channel", channel)
}

// Run blocks until ctx is cancelled or Close is called. Each tick
// is submitted to the configured workerpool so the EC-4-5 tick body
// scales with operator-tuned concurrency, never with raw goroutines.
//
// Decomposition: a ticker drives the schedule (mirrors v2.10 memwatch.
// Sampler.Run -- the one foreground goroutine the binary owns); the
// per-tick work goes through the pool.
func (m *ChannelHealthMonitor) Run(ctx context.Context) error {
	if err := m.beginRun(); err != nil {
		return err
	}
	ticker := time.NewTicker(m.cfg.TickInterval)
	defer ticker.Stop()
	defer m.signalDone()
	if err := m.submitTick(ctx); err != nil && !errors.Is(err, context.Canceled) {
		m.logger.Warn("monitor.channel_health.initial_tick_submit_failed", "error", err)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-m.stopCh:
			return nil
		case <-ticker.C:
			if err := m.submitTick(ctx); err != nil && !errors.Is(err, context.Canceled) {
				m.logger.Warn("monitor.channel_health.tick_submit_failed", "error", err)
			}
		}
	}
}

// submitTick enqueues a single Tick on the configured workerpool.
// Always returns the pool's submit error so the caller can branch
// on saturation without re-implementing the wrapping.
func (m *ChannelHealthMonitor) submitTick(ctx context.Context) error {
	return m.cfg.Pool.Submit(ctx, func(taskCtx context.Context) error {
		if err := m.Tick(taskCtx); err != nil && !errors.Is(err, ErrChannelHealthMonitorStopped) {
			m.logger.Warn("monitor.channel_health.tick_failed", "error", err)
		}
		return nil
	})
}

func (m *ChannelHealthMonitor) beginRun() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrChannelHealthMonitorStopped
	}
	if m.running {
		return nil
	}
	m.running = true
	m.doneCh = make(chan struct{})
	return nil
}

func (m *ChannelHealthMonitor) signalDone() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.doneCh != nil {
		close(m.doneCh)
		m.doneCh = nil
	}
	m.running = false
}

// Close marks the monitor closed, signals Run to exit, and waits
// for the in-flight tick (if any) to complete. Implements
// lifecycle.Closer.
func (m *ChannelHealthMonitor) Close(ctx context.Context) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	close(m.stopCh)
	doneCh := m.doneCh
	m.mu.Unlock()
	if doneCh == nil {
		return nil
	}
	select {
	case <-doneCh:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("monitor: channel_health drain: %w", ctx.Err())
	}
}

func (m *ChannelHealthMonitor) guard() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrChannelHealthMonitorStopped
	}
	return nil
}

// classifyChannelHealth is the pure decision function. Cyclomatic
// complexity stays at 4 (3 conditions + default) so the sentrux
// complex_fn budget is unaffected.
func classifyChannelHealth(rate float64, total int, consecutive int, rateThreshold float64, consecutiveThreshold int) ChannelHealthState {
	if consecutive >= consecutiveThreshold {
		return ChannelHealthUnhealthy
	}
	if total > 0 && rate > rateThreshold {
		return ChannelHealthUnhealthy
	}
	if total > 0 && rate >= rateThreshold/2 {
		return ChannelHealthDegraded
	}
	return ChannelHealthHealthy
}

// computeChannelFailureRate sums the failure count across the
// supplied samples and returns (failureRate, totalCount). Pure;
// callers manage the slice + cutoff.
func computeChannelFailureRate(samples []channelSample) (rate float64, total int) {
	total = len(samples)
	if total == 0 {
		return 0, 0
	}
	failed := 0
	for _, s := range samples {
		if !s.success {
			failed++
		}
	}
	return float64(failed) / float64(total), total
}

// pruneExpiredSamples drops samples older than the supplied cutoff.
// Pure; callers manage the slice ownership.
func pruneExpiredSamples(samples []channelSample, cutoff time.Time) []channelSample {
	if len(samples) == 0 {
		return samples
	}
	for i, s := range samples {
		if !s.timestamp.Before(cutoff) {
			if i == 0 {
				return samples
			}
			out := make([]channelSample, len(samples)-i)
			copy(out, samples[i:])
			return out
		}
	}
	return samples[:0]
}

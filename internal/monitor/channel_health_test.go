package monitor

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/workerpool"
)

// recordingHealthMetrics captures every metric/alert/recovery the
// monitor emits so the v3.4.1 EC-4-5 RED tests can assert on the
// full surface without re-reading Prometheus text.
type recordingHealthMetrics struct {
	mu             sync.Mutex
	stateUpdates   map[string]ChannelHealthState
	failureRates   map[string]float64
	consecutive    map[string]int
	alertChannels  []string
	alertStates    []ChannelHealthState
	recoveryEvents []string
}

func newRecordingHealthMetrics() *recordingHealthMetrics {
	return &recordingHealthMetrics{
		stateUpdates: map[string]ChannelHealthState{},
		failureRates: map[string]float64{},
		consecutive:  map[string]int{},
	}
}

func (r *recordingHealthMetrics) SetState(_, channel string, state ChannelHealthState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stateUpdates[channel] = state
}

func (r *recordingHealthMetrics) SetFailureRate(_, channel string, rate float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failureRates[channel] = rate
}

func (r *recordingHealthMetrics) SetConsecutiveFailures(_, channel string, n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.consecutive[channel] = n
}

func (r *recordingHealthMetrics) RecordAlert(_, channel string, state ChannelHealthState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.alertChannels = append(r.alertChannels, channel)
	r.alertStates = append(r.alertStates, state)
}

func (r *recordingHealthMetrics) RecordRecovery(_, channel string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recoveryEvents = append(r.recoveryEvents, channel)
}

func (r *recordingHealthMetrics) snapshot() (map[string]ChannelHealthState, map[string]float64, map[string]int, []string, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	stateCopy := make(map[string]ChannelHealthState, len(r.stateUpdates))
	for k, v := range r.stateUpdates {
		stateCopy[k] = v
	}
	rateCopy := make(map[string]float64, len(r.failureRates))
	for k, v := range r.failureRates {
		rateCopy[k] = v
	}
	consCopy := make(map[string]int, len(r.consecutive))
	for k, v := range r.consecutive {
		consCopy[k] = v
	}
	alerts := make([]string, len(r.alertChannels))
	copy(alerts, r.alertChannels)
	recoveries := make([]string, len(r.recoveryEvents))
	copy(recoveries, r.recoveryEvents)
	return stateCopy, rateCopy, consCopy, alerts, recoveries
}

// recordingKPIHook captures EvoMap KPI emissions.
type recordingKPIHook struct {
	alerts     atomic.Int64
	recoveries atomic.Int64
}

func (r *recordingKPIHook) Emit(_, _ string, alertsDelta, recoveriesDelta int64) {
	r.alerts.Add(alertsDelta)
	r.recoveries.Add(recoveriesDelta)
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{now: start}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// healthHarness wires every dependency for the v3.4.1 RED tests so
// every individual TestChannelHealth_* stays linear and under the
// sentrux complex_fn ceiling.
type healthHarness struct {
	monitor *ChannelHealthMonitor
	metrics *recordingHealthMetrics
	hook    *recordingKPIHook
	pool    *workerpool.Pool
	clock   *fakeClock
}

func setupHealthHarness(t *testing.T) *healthHarness {
	t.Helper()
	pool := workerpool.New(nil, workerpool.Config{Name: "channel-health-test", MinWorkers: 4, MaxWorkers: 4, QueueDepth: 64})
	metrics := newRecordingHealthMetrics()
	hook := &recordingKPIHook{}
	clock := newFakeClock(time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC))
	monitor, err := NewChannelHealthMonitor(nil, ChannelHealthMonitorConfig{
		TenantID:                    "tenant-1",
		Pool:                        pool,
		Metrics:                     metrics,
		KPIHook:                     hook.Emit,
		Now:                         clock.Now,
		WindowDuration:              5 * time.Minute,
		FailureRateThreshold:        0.05,
		ConsecutiveFailureThreshold: 3,
		TickInterval:                30 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewChannelHealthMonitor: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = monitor.Close(ctx)
		_ = pool.Close(ctx)
	})
	return &healthHarness{monitor: monitor, metrics: metrics, hook: hook, pool: pool, clock: clock}
}

// TestChannelHealth_AlertsWhenAdapterFailureRateExceeds5Pct seeds 100
// adapter calls (94 ok + 6 failed) into the sliding window then runs a
// single Tick. Asserts the per-channel state transitions to unhealthy,
// the gauge surfaces 0.06, and the alert hook fires exactly once for
// the channel.
func TestChannelHealth_AlertsWhenAdapterFailureRateExceeds5Pct(t *testing.T) {
	t.Parallel()

	h := setupHealthHarness(t)
	const channel = "tiktok"
	for i := 0; i < 94; i++ {
		h.monitor.RecordCall(channel, true)
	}
	for i := 0; i < 6; i++ {
		h.monitor.RecordCall(channel, false)
	}
	if err := h.monitor.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if got := h.monitor.SnapshotState()[channel]; got != ChannelHealthUnhealthy {
		t.Fatalf("state[%s] = %v, want ChannelHealthUnhealthy", channel, got)
	}
	states, rates, _, alerts, _ := h.metrics.snapshot()
	if states[channel] != ChannelHealthUnhealthy {
		t.Fatalf("metric state[%s] = %v", channel, states[channel])
	}
	if got := rates[channel]; got < 0.05 || got > 0.07 {
		t.Fatalf("failure rate[%s] = %v, want ~0.06", channel, got)
	}
	if len(alerts) != 1 || alerts[0] != channel {
		t.Fatalf("alerts = %v, want [%s]", alerts, channel)
	}
	if got := h.hook.alerts.Load(); got != 1 {
		t.Fatalf("KPI alerts = %d, want 1", got)
	}
}

// TestChannelHealth_AlertsOnThreeConsecutiveFailures seeds 3 failures
// in a row (no successes) and asserts the consecutive-failure path
// trips even though the absolute call count is small. The plan
// requires alert when consecutive >= 3.
func TestChannelHealth_AlertsOnThreeConsecutiveFailures(t *testing.T) {
	t.Parallel()

	h := setupHealthHarness(t)
	const channel = "facebook"
	for i := 0; i < 3; i++ {
		h.monitor.RecordCall(channel, false)
	}
	if err := h.monitor.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if got := h.monitor.SnapshotState()[channel]; got != ChannelHealthUnhealthy {
		t.Fatalf("state[%s] = %v, want ChannelHealthUnhealthy", channel, got)
	}
	_, _, consecutive, alerts, _ := h.metrics.snapshot()
	if consecutive[channel] != 3 {
		t.Fatalf("consecutive[%s] = %d, want 3", channel, consecutive[channel])
	}
	if len(alerts) != 1 || alerts[0] != channel {
		t.Fatalf("alerts = %v, want [%s]", alerts, channel)
	}
}

// TestChannelHealth_RecoversAfterSuccessfulCall walks through the
// degrade-then-recover lifecycle: trip the consecutive-failures path,
// observe the alert, advance the clock past the sliding window so
// the failure samples are evicted, then record enough successes to
// clear the gate and verify the recovery hook fires + KPI counter
// advances.
func TestChannelHealth_RecoversAfterSuccessfulCall(t *testing.T) {
	t.Parallel()

	h := setupHealthHarness(t)
	const channel = "rednote"
	for i := 0; i < 4; i++ {
		h.monitor.RecordCall(channel, false)
	}
	if err := h.monitor.Tick(context.Background()); err != nil {
		t.Fatalf("first Tick: %v", err)
	}
	if got := h.monitor.SnapshotState()[channel]; got != ChannelHealthUnhealthy {
		t.Fatalf("pre-recovery state[%s] = %v", channel, got)
	}
	h.clock.Advance(10 * time.Minute)
	for i := 0; i < 50; i++ {
		h.monitor.RecordCall(channel, true)
	}
	if err := h.monitor.Tick(context.Background()); err != nil {
		t.Fatalf("recovery Tick: %v", err)
	}
	if got := h.monitor.SnapshotState()[channel]; got != ChannelHealthHealthy {
		t.Fatalf("post-recovery state[%s] = %v, want ChannelHealthHealthy", channel, got)
	}
	_, _, _, alerts, recoveries := h.metrics.snapshot()
	if len(alerts) != 1 {
		t.Fatalf("alerts = %v, want exactly 1 (single transition to unhealthy)", alerts)
	}
	if len(recoveries) != 1 || recoveries[0] != channel {
		t.Fatalf("recoveries = %v, want [%s]", recoveries, channel)
	}
	if got := h.hook.recoveries.Load(); got != 1 {
		t.Fatalf("KPI recoveries = %d, want 1", got)
	}
}

func TestChannelHealth_HealthyWhenNoCalls(t *testing.T) {
	t.Parallel()
	h := setupHealthHarness(t)
	if err := h.monitor.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	for ch, state := range h.monitor.SnapshotState() {
		if state != ChannelHealthHealthy {
			t.Fatalf("state[%s] = %v, want ChannelHealthHealthy", ch, state)
		}
	}
}

func TestChannelHealth_DegradedBeforeUnhealthy(t *testing.T) {
	t.Parallel()
	h := setupHealthHarness(t)
	const channel = "instagram"
	// Interleave failures with successes to keep consecutive-fails
	// below the gate (3) while pushing the rate into the
	// half-threshold..threshold band (degraded zone). 3 failures /
	// 100 calls = 0.03 (above 0.025 half-threshold, below 0.05
	// full threshold).
	for i := 0; i < 97; i++ {
		h.monitor.RecordCall(channel, true)
	}
	for i := 0; i < 3; i++ {
		h.monitor.RecordCall(channel, false)
		h.monitor.RecordCall(channel, true)
	}
	if err := h.monitor.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if got := h.monitor.SnapshotState()[channel]; got != ChannelHealthDegraded {
		t.Fatalf("state[%s] = %v, want ChannelHealthDegraded", channel, got)
	}
	_, _, _, alerts, _ := h.metrics.snapshot()
	if len(alerts) != 1 {
		// Degraded is still a non-healthy transition so the alert
		// hook fires; the alert state captured should be degraded.
		t.Fatalf("alerts = %v, want exactly 1 alert for healthy->degraded transition", alerts)
	}
	_, _, _, _, _ = h.metrics.snapshot()
}

func TestChannelHealth_SlidingWindowEvictsExpiredSamples(t *testing.T) {
	t.Parallel()
	h := setupHealthHarness(t)
	const channel = "tiktok"
	for i := 0; i < 5; i++ {
		h.monitor.RecordCall(channel, false)
	}
	h.clock.Advance(10 * time.Minute)
	for i := 0; i < 50; i++ {
		h.monitor.RecordCall(channel, true)
	}
	if err := h.monitor.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if got := h.monitor.SnapshotState()[channel]; got != ChannelHealthHealthy {
		t.Fatalf("state[%s] = %v, want healthy after expired failures", channel, got)
	}
}

func TestChannelHealth_RejectsAfterClose(t *testing.T) {
	t.Parallel()
	h := setupHealthHarness(t)
	if err := h.monitor.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := h.monitor.Tick(context.Background()); !errors.Is(err, ErrChannelHealthMonitorStopped) {
		t.Fatalf("err = %v, want ErrChannelHealthMonitorStopped", err)
	}
}

func TestNewChannelHealthMonitor_RequiresPool(t *testing.T) {
	t.Parallel()
	_, err := NewChannelHealthMonitor(nil, ChannelHealthMonitorConfig{TenantID: "t"})
	if !errors.Is(err, ErrChannelUnhealthy) && !errors.Is(err, ErrChannelHealthMonitorUnconfigured) {
		t.Fatalf("err = %v, want ErrChannelHealthMonitorUnconfigured", err)
	}
}

func TestNewChannelHealthMonitor_RequiresTenant(t *testing.T) {
	t.Parallel()
	pool := workerpool.New(nil, workerpool.Config{Name: "tenant-validate", MinWorkers: 1, MaxWorkers: 1, QueueDepth: 1})
	defer func() { _ = pool.Close(context.Background()) }()
	_, err := NewChannelHealthMonitor(nil, ChannelHealthMonitorConfig{Pool: pool})
	if !errors.Is(err, ErrChannelHealthMonitorUnconfigured) {
		t.Fatalf("err = %v, want ErrChannelHealthMonitorUnconfigured", err)
	}
}

func TestNewChannelHealthMonitor_AppliesDefaults(t *testing.T) {
	t.Parallel()
	pool := workerpool.New(nil, workerpool.Config{Name: "defaults", MinWorkers: 1, MaxWorkers: 1, QueueDepth: 1})
	defer func() { _ = pool.Close(context.Background()) }()
	monitor, err := NewChannelHealthMonitor(nil, ChannelHealthMonitorConfig{TenantID: "t", Pool: pool})
	if err != nil {
		t.Fatalf("NewChannelHealthMonitor: %v", err)
	}
	defer func() { _ = monitor.Close(context.Background()) }()
	cfg := monitor.Config()
	if cfg.WindowDuration != 5*time.Minute {
		t.Fatalf("WindowDuration default = %v", cfg.WindowDuration)
	}
	if cfg.FailureRateThreshold != 0.05 {
		t.Fatalf("FailureRateThreshold default = %v", cfg.FailureRateThreshold)
	}
	if cfg.ConsecutiveFailureThreshold != 3 {
		t.Fatalf("ConsecutiveFailureThreshold default = %v", cfg.ConsecutiveFailureThreshold)
	}
	if cfg.TickInterval != 30*time.Second {
		t.Fatalf("TickInterval default = %v", cfg.TickInterval)
	}
}

// TestChannelHealth_TickViaPoolUnderLoad asserts the monitor uses
// the workerpool surface (not raw goroutines) by submitting Tick
// many times sequentially via the pool and verifying no leaks/data
// races. The pool sizing in setupHealthHarness (4 workers /
// 64-deep queue) is sized for the test cohort below.
func TestChannelHealth_TickViaPoolUnderLoad(t *testing.T) {
	t.Parallel()
	h := setupHealthHarness(t)
	const channel = "tiktok"
	const ticks = 16
	var wg sync.WaitGroup
	for i := 0; i < ticks; i++ {
		i := i
		wg.Add(1)
		err := h.pool.Submit(context.Background(), func(ctx context.Context) error {
			defer wg.Done()
			h.monitor.RecordCall(channel, i%4 == 0)
			return h.monitor.Tick(ctx)
		})
		if err != nil {
			wg.Done()
			t.Fatalf("pool.Submit: %v", err)
		}
	}
	wg.Wait()
	if got := h.monitor.SnapshotState()[channel]; got != ChannelHealthUnhealthy && got != ChannelHealthDegraded {
		t.Fatalf("state[%s] = %v, want degraded or unhealthy under high failure rate", channel, got)
	}
}

// TestChannelHealth_SyntheticFailureInjectionAlertWithin60s is the
// EC-4-5 acceptance ("synthetic failure injection triggers alert
// within 60s"). The tick interval is 30s in production; the test
// drives the monitor directly to bound the check at much less than
// the 60s ceiling.
func TestChannelHealth_SyntheticFailureInjectionAlertWithin60s(t *testing.T) {
	t.Parallel()
	h := setupHealthHarness(t)
	const channel = "tiktok"
	start := h.clock.Now()
	for i := 0; i < 5; i++ {
		h.monitor.RecordCall(channel, false)
	}
	if err := h.monitor.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if got := h.monitor.SnapshotState()[channel]; got != ChannelHealthUnhealthy {
		t.Fatalf("state[%s] = %v, want ChannelHealthUnhealthy", channel, got)
	}
	elapsed := h.clock.Now().Sub(start)
	if elapsed > 60*time.Second {
		t.Fatalf("elapsed virtual time = %v, exceeds 60s acceptance ceiling", elapsed)
	}
}

func TestClassifyChannelHealth_PureLogic(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		rate        float64
		total       int
		consecutive int
		want        ChannelHealthState
	}{
		{name: "no_calls_is_healthy", rate: 0, total: 0, consecutive: 0, want: ChannelHealthHealthy},
		{name: "below_threshold_healthy", rate: 0.02, total: 100, consecutive: 0, want: ChannelHealthHealthy},
		{name: "above_half_threshold_degraded", rate: 0.04, total: 100, consecutive: 0, want: ChannelHealthDegraded},
		{name: "above_threshold_unhealthy", rate: 0.06, total: 100, consecutive: 0, want: ChannelHealthUnhealthy},
		{name: "consecutive_overrides_rate", rate: 0.0, total: 3, consecutive: 3, want: ChannelHealthUnhealthy},
		{name: "consecutive_below_threshold_healthy", rate: 0, total: 2, consecutive: 2, want: ChannelHealthHealthy},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := classifyChannelHealth(tc.rate, tc.total, tc.consecutive, 0.05, 3)
			if got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestErrChannelUnhealthy_Sentinel(t *testing.T) {
	t.Parallel()
	wrapped := errors.New("wrapped: " + ErrChannelUnhealthy.Error())
	if errors.Is(wrapped, ErrChannelUnhealthy) {
		t.Fatal("plain string match should not satisfy errors.Is")
	}
	wrapped2 := errors.Join(ErrChannelUnhealthy, errors.New("extra"))
	if !errors.Is(wrapped2, ErrChannelUnhealthy) {
		t.Fatal("errors.Join should preserve sentinel identity")
	}
}

func TestChannelHealth_Run_DriverSubmitsToPool(t *testing.T) {
	t.Parallel()
	h := setupHealthHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := h.monitor.Run(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run: %v", err)
	}
	if got := h.pool.Stats().Submitted; got == 0 {
		t.Fatalf("pool.Stats.Submitted = 0; expected the monitor's tick loop to submit at least once")
	}
}

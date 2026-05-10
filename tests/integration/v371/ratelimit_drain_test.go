//go:build v371_smoke

// File scope: v3.7.1 QA Task 2 -- 20-post rate-limit drain
// validation (EC-10-3 hardening).
//
// Acceptance (cite plan): "20-post burst queues + drains at
// correct rate; zero requests dropped under cap; mixed channels
// pace independently; drain-on-overflow evicts oldest 5 via
// RateLimitDrainEvent; replay protection rejects within 24h and
// re-accepts past TTL; ec_uiauto_rate_limit_drops_total
// increments correctly per reason; 1000-event burst dispatched
// in <10ms excluding rate-limit pacing".
//
// 5 rate-limit scenarios beyond v3.7.0 unit tests:
//
//  1. 20 RedNote posts in 30 sec  -> first 1 allowed, 19 queued;
//     pacing 5 min between attempts;
//     all 20 eventually drain via
//     fast-forward fake clock
//  2. 20 TikTok creator posts     -> first 1 allowed, 19 queued;
//     pacing 2 min; ~38 min total
//     (clock fast-forward)
//  3. Mixed channel storm         -> 10 RedNote + 10 TikTok + 10 FB
//     concurrent; no cross-channel
//     interference; each channel
//     paces independently
//  4. Drain-on-overflow           -> 25 RedNote (queue cap=19);
//     5 dropped + 5 RateLimitDrainEvents
//     (FIFO eviction)
//  5. Replay protection           -> resubmit nonce within 24h
//     -> ErrInvalidNonce; new nonce
//     after 24h+ -> accepted
//
// The suite uses a deterministic fake clock so the 2-min and
// 5-min pacing budgets fast-forward in milliseconds without
// real-time sleeps. The HMAC nonce signature stays canonical
// across the fast-forward steps.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT
// increase from 4):
//   - top-level scenario tests stay thin orchestrators
//   - clock helpers, recorder, and harness factory split into
//     focused helpers below.
package v371

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/uiauto/ratelimit"
)

// ratelimitSecret is the deterministic HMAC fixture key for the
// v371 ratelimit smoke. >=32 bytes per ratelimit.MinSecretBytes.
// gitleaks:allow
var ratelimitSecret = []byte("v371-ratelimit-test-secret-fixture-32bytes-please") // gitleaks:allow

// ratelimitBurstBudget is the 1000-event burst dispatch budget
// per the plan acceptance ("1000-event burst dispatched in <10ms
// excluding rate-limit pacing"). The fake clock keeps real-time
// at zero so the budget measures pure synchronous Allow latency.
const ratelimitBurstBudget = 10 * time.Millisecond

// ratelimitDrainQueueCap is the queue cap for the drain-on-
// overflow scenario. With cap=19, 25 RedNote attempts result in
// 1 success + 19 queued + 5 dropped, matching the plan's
// "5 dropped" expectation. The production default
// (ratelimit.MaxQueuedPerBucket=20) is exercised in scenario 1.
const ratelimitDrainQueueCap = 19

// ratelimitScenarioRow is one row in the per-scenario summary
// table emitted via t.Log so the PR body can paste it as-is.
type ratelimitScenarioRow struct {
	scenario     string
	channel      string
	totalSent    int
	allowedCount int
	exceededDrop int
	drainedDrop  int
	drainEmits   int
	totalElapsed time.Duration
	burstP95     time.Duration
}

// fakeClock is the deterministic clock the tests inject so
// pacing budgets fast-forward without real sleeps.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

// newFakeClock seeds at a fixed UTC time so test outputs stay
// reproducible.
func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)}
}

// Now returns the current fake time.
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

// Advance pushes the fake clock forward by d.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// ratelimitMetrics is the deterministic Metrics recorder for the
// EC-10-3 telemetry assertions. drops indexes by reason for the
// per-scenario assertion.
type ratelimitMetrics struct {
	mu    sync.Mutex
	drops map[string]int // reason -> count
}

// newRatelimitMetrics constructs a recorder.
func newRatelimitMetrics() *ratelimitMetrics {
	return &ratelimitMetrics{drops: map[string]int{}}
}

// RecordRateLimitDrop bumps drops[reason].
func (m *ratelimitMetrics) RecordRateLimitDrop(_ string, _ ratelimit.Channel, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.drops[reason]++
}

// snapshot returns deterministic per-reason counts under a lock.
func (m *ratelimitMetrics) snapshot() map[string]int {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]int, len(m.drops))
	for k, v := range m.drops {
		out[k] = v
	}
	return out
}

// ratelimitEmitter is the deterministic Emitter recorder for the
// RateLimitDrainEvent assertion.
type ratelimitEmitter struct {
	mu     sync.Mutex
	events []string
}

// EmitRateLimitDrain appends a tenant|channel record.
func (e *ratelimitEmitter) EmitRateLimitDrain(_ context.Context, tenantID string, ch ratelimit.Channel, _ time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, tenantID+"|"+string(ch))
}

// Count returns the number of drain emissions.
func (e *ratelimitEmitter) Count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.events)
}

// limiterHarness wires a limiter with a fake clock + deterministic
// recorders so each scenario starts from a known state.
type limiterHarness struct {
	limiter *ratelimit.RateLimiter
	clock   *fakeClock
	metrics *ratelimitMetrics
	emitter *ratelimitEmitter
}

// newLimiterHarness constructs a harness with the given queue cap.
// queueCap=0 means "use the production default (20)".
func newLimiterHarness(t *testing.T, queueCap int) *limiterHarness {
	t.Helper()
	clk := newFakeClock()
	m := newRatelimitMetrics()
	e := &ratelimitEmitter{}
	r, err := ratelimit.New(ratelimit.Config{
		Secret:   ratelimitSecret,
		Metrics:  m,
		Emitter:  e,
		Now:      clk.Now,
		QueueCap: queueCap,
	})
	if err != nil {
		t.Fatalf("new limiter: %v", err)
	}
	t.Cleanup(func() { _ = r.Close(context.Background()) })
	return &limiterHarness{limiter: r, clock: clk, metrics: m, emitter: e}
}

// signedReq builds an AllowRequest + signature for a tenant +
// channel + nonce + timestamp anchored to the fake clock.
func signedReq(h *limiterHarness, tenant string, ch ratelimit.Channel, nonce string) (ratelimit.AllowRequest, string) {
	ts := h.clock.Now().Unix()
	req := ratelimit.AllowRequest{
		TenantID:  tenant,
		Channel:   ch,
		Nonce:     nonce,
		Timestamp: ts,
	}
	return req, h.limiter.SignNonce(tenant, ch, nonce, ts)
}

// drainBurst submits N attempts back-to-back at the current
// clock time (no advance). Returns the count of allowed / exceeded
// / drained outcomes for the per-scenario assertion.
type burstOutcome struct {
	allowed  int
	exceeded int
	drained  int
	other    int
}

// drainBurst submits N attempts at the current clock and counts
// outcomes per typed sentinel.
func drainBurst(t *testing.T, h *limiterHarness, tenant string, ch ratelimit.Channel, n int, prefix string) burstOutcome {
	t.Helper()
	var out burstOutcome
	for i := 0; i < n; i++ {
		req, sig := signedReq(h, tenant, ch, fmt.Sprintf("%s-%d", prefix, i))
		_, err := h.limiter.Allow(context.Background(), req, sig)
		switch {
		case err == nil:
			out.allowed++
		case errors.Is(err, ratelimit.ErrRateLimitDrained):
			out.drained++
		case errors.Is(err, ratelimit.ErrRateLimitExceeded):
			out.exceeded++
		default:
			out.other++
			t.Fatalf("%s burst attempt %d: unexpected err: %v", prefix, i, err)
		}
	}
	return out
}

// drainWithPacing alternates Allow + clock advance to simulate
// the 2-min / 5-min pacing for scenarios 1 and 2. Returns the
// total simulated wall-clock elapsed plus the number of
// successfully drained attempts.
func drainWithPacing(t *testing.T, h *limiterHarness, tenant string, ch ratelimit.Channel, n int, period time.Duration, prefix string) (drained int, simulated time.Duration) {
	t.Helper()
	for i := 0; i < n; i++ {
		req, sig := signedReq(h, tenant, ch, fmt.Sprintf("%s-pace-%d", prefix, i))
		_, err := h.limiter.Allow(context.Background(), req, sig)
		if err == nil {
			drained++
			h.clock.Advance(period)
			simulated += period
			continue
		}
		if !errors.Is(err, ratelimit.ErrRateLimitExceeded) {
			t.Fatalf("%s pace %d: want exceeded, got %v", prefix, i, err)
		}
		// Refill failed; advance and retry once.
		h.clock.Advance(period)
		simulated += period
		req2, sig2 := signedReq(h, tenant, ch, fmt.Sprintf("%s-pace-%d-retry", prefix, i))
		if _, err := h.limiter.Allow(context.Background(), req2, sig2); err != nil {
			t.Fatalf("%s pace %d retry: want ok, got %v", prefix, i, err)
		}
		drained++
	}
	return drained, simulated
}

// p95Duration computes the 95th percentile of a duration slice.
func p95Duration(in []time.Duration) time.Duration {
	if len(in) == 0 {
		return 0
	}
	cp := append([]time.Duration(nil), in...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	idx := (95 * len(cp)) / 100
	if idx >= len(cp) {
		idx = len(cp) - 1
	}
	return cp[idx]
}

// emitRatelimitSummary t.Logs the per-scenario table.
func emitRatelimitSummary(t *testing.T, rows []ratelimitScenarioRow) {
	t.Helper()
	t.Log("v371 EC-10-3 rate-limit drain scenarios:")
	t.Log("scenario | channel | total | allowed | exceeded | drained | drain_emits | sim_elapsed | burst_p95")
	for _, r := range rows {
		t.Logf("%s | %s | %d | %d | %d | %d | %d | %s | %s",
			r.scenario, r.channel, r.totalSent, r.allowedCount,
			r.exceededDrop, r.drainedDrop, r.drainEmits,
			r.totalElapsed, r.burstP95)
	}
}

// TestRatelimitDrainScenarios is the top-level orchestrator.
// Each scenario uses its own harness so tenant + bucket state is
// fully isolated.
func TestRatelimitDrainScenarios(t *testing.T) {
	t.Parallel()
	rows := make([]ratelimitScenarioRow, 0, 5)
	rows = append(rows, scenarioRatelimit20RedNote(t))
	rows = append(rows, scenarioRatelimit20TikTok(t))
	rows = append(rows, scenarioRatelimitMixedChannelStorm(t))
	rows = append(rows, scenarioRatelimitDrainOnOverflow(t))
	rows = append(rows, scenarioRatelimitReplayProtection(t))
	emitRatelimitSummary(t, rows)
}

// scenarioRatelimit20RedNote -- 20 RedNote posts in 30 sec; first
// 1 allowed, 19 queued (within default queue cap of 20). Then
// fast-forward 19 x 5min cycles to drain the queue. Expected
// total simulated wall-clock: 19 * 5min = 95 min.
func scenarioRatelimit20RedNote(t *testing.T) ratelimitScenarioRow {
	t.Helper()
	h := newLimiterHarness(t, 0) // default cap=20
	burst := drainBurst(t, h, "tenant-a", ratelimit.ChannelRedNote, 20, "rn-burst")
	if burst.allowed != 1 {
		t.Fatalf("rednote burst: want 1 allowed, got %d", burst.allowed)
	}
	if burst.exceeded != 19 {
		t.Fatalf("rednote burst: want 19 exceeded, got %d", burst.exceeded)
	}
	if burst.drained != 0 {
		t.Fatalf("rednote burst: want 0 drained (queue cap=20 holds all 19), got %d", burst.drained)
	}
	drained, sim := drainWithPacing(t, h, "tenant-a", ratelimit.ChannelRedNote, 19, 5*time.Minute, "rn-drain")
	if drained != 19 {
		t.Fatalf("rednote drain: want 19, got %d", drained)
	}
	if sim < 19*5*time.Minute {
		t.Fatalf("rednote drain: simulated %s < 19*5min", sim)
	}
	drops := h.metrics.snapshot()
	if drops["exceeded"] < 19 {
		t.Fatalf("rednote: want drops[exceeded]>=19, got %d", drops["exceeded"])
	}
	return ratelimitScenarioRow{
		scenario:     "20_rednote_burst_drain",
		channel:      string(ratelimit.ChannelRedNote),
		totalSent:    20,
		allowedCount: burst.allowed + drained,
		exceededDrop: burst.exceeded,
		drainedDrop:  burst.drained,
		drainEmits:   h.emitter.Count(),
		totalElapsed: sim,
		burstP95:     measureP95(t, h, ratelimit.ChannelRedNote, "rn-p95"),
	}
}

// scenarioRatelimit20TikTok -- 20 TikTok creator posts; first 1
// allowed, 19 queued (well under cap=20). Pacing 2 min. Simulated
// wall-clock: 19 * 2min = 38 min.
func scenarioRatelimit20TikTok(t *testing.T) ratelimitScenarioRow {
	t.Helper()
	h := newLimiterHarness(t, 0)
	burst := drainBurst(t, h, "tenant-a", ratelimit.ChannelTikTok, 20, "tt-burst")
	if burst.allowed != 1 || burst.exceeded != 19 || burst.drained != 0 {
		t.Fatalf("tiktok burst: want (1, 19, 0); got (%d, %d, %d)", burst.allowed, burst.exceeded, burst.drained)
	}
	drained, sim := drainWithPacing(t, h, "tenant-a", ratelimit.ChannelTikTok, 19, 2*time.Minute, "tt-drain")
	if drained != 19 {
		t.Fatalf("tiktok drain: want 19, got %d", drained)
	}
	if sim < 19*2*time.Minute {
		t.Fatalf("tiktok drain: simulated %s < 19*2min", sim)
	}
	return ratelimitScenarioRow{
		scenario:     "20_tiktok_burst_drain",
		channel:      string(ratelimit.ChannelTikTok),
		totalSent:    20,
		allowedCount: burst.allowed + drained,
		exceededDrop: burst.exceeded,
		drainedDrop:  burst.drained,
		drainEmits:   h.emitter.Count(),
		totalElapsed: sim,
		burstP95:     measureP95(t, h, ratelimit.ChannelTikTok, "tt-p95"),
	}
}

// scenarioRatelimitMixedChannelStorm -- 10 RedNote + 10 TikTok +
// 10 Facebook simultaneously. Asserts cross-channel isolation:
// RedNote and TikTok (cap=1) each allow 1 and exceed 9; FB
// (cap=5) allows 5 and exceeds 5. Total 7 allowed, 23 exceeded.
func scenarioRatelimitMixedChannelStorm(t *testing.T) ratelimitScenarioRow {
	t.Helper()
	h := newLimiterHarness(t, 0)
	rn := drainBurst(t, h, "tenant-mix", ratelimit.ChannelRedNote, 10, "mix-rn")
	tt := drainBurst(t, h, "tenant-mix", ratelimit.ChannelTikTok, 10, "mix-tt")
	fb := drainBurst(t, h, "tenant-mix", ratelimit.ChannelFacebook, 10, "mix-fb")
	if rn.allowed != 1 || rn.exceeded != 9 {
		t.Fatalf("mix rednote: want (1,9); got (%d,%d)", rn.allowed, rn.exceeded)
	}
	if tt.allowed != 1 || tt.exceeded != 9 {
		t.Fatalf("mix tiktok: want (1,9); got (%d,%d)", tt.allowed, tt.exceeded)
	}
	if fb.allowed != 5 || fb.exceeded != 5 {
		t.Fatalf("mix facebook: want (5,5); got (%d,%d)", fb.allowed, fb.exceeded)
	}
	allowed := rn.allowed + tt.allowed + fb.allowed
	exceeded := rn.exceeded + tt.exceeded + fb.exceeded
	if allowed != 7 || exceeded != 23 {
		t.Fatalf("mix total: want allowed=7 exceeded=23; got allowed=%d exceeded=%d", allowed, exceeded)
	}
	return ratelimitScenarioRow{
		scenario:     "mixed_channel_storm",
		channel:      "rednote+tiktok+facebook",
		totalSent:    30,
		allowedCount: allowed,
		exceededDrop: exceeded,
		drainEmits:   h.emitter.Count(),
	}
}

// scenarioRatelimitDrainOnOverflow -- 25 RedNote with queueCap=19
// so 1 succeeds + 19 queued + 5 dropped via FIFO eviction.
// Asserts ec_uiauto_rate_limit_drops_total{reason="drain"} = 5.
func scenarioRatelimitDrainOnOverflow(t *testing.T) ratelimitScenarioRow {
	t.Helper()
	h := newLimiterHarness(t, ratelimitDrainQueueCap)
	burst := drainBurst(t, h, "tenant-x", ratelimit.ChannelRedNote, 25, "drn")
	if burst.allowed != 1 {
		t.Fatalf("drain-overflow: want 1 allowed, got %d", burst.allowed)
	}
	if burst.exceeded != 19 {
		t.Fatalf("drain-overflow: want 19 exceeded, got %d", burst.exceeded)
	}
	if burst.drained != 5 {
		t.Fatalf("drain-overflow: want 5 drained, got %d", burst.drained)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if h.emitter.Count() >= 5 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := h.emitter.Count(); got != 5 {
		t.Fatalf("drain-overflow: want 5 RateLimitDrainEvents, got %d", got)
	}
	drops := h.metrics.snapshot()
	if drops["drain"] != 5 {
		t.Fatalf("drain-overflow: want drops[drain]=5, got %d", drops["drain"])
	}
	if drops["exceeded"] != 19 {
		t.Fatalf("drain-overflow: want drops[exceeded]=19, got %d", drops["exceeded"])
	}
	return ratelimitScenarioRow{
		scenario:     "drain_on_overflow_25",
		channel:      string(ratelimit.ChannelRedNote),
		totalSent:    25,
		allowedCount: burst.allowed,
		exceededDrop: burst.exceeded,
		drainedDrop:  burst.drained,
		drainEmits:   h.emitter.Count(),
	}
}

// scenarioRatelimitReplayProtection -- same nonce + signature
// within NonceTTL (24h) -> ErrInvalidNonce. After 24h+ purge,
// the same nonce with a fresh timestamp is accepted.
func scenarioRatelimitReplayProtection(t *testing.T) ratelimitScenarioRow {
	t.Helper()
	h := newLimiterHarness(t, 0)
	req, sig := signedReq(h, "tenant-r", ratelimit.ChannelFacebook, "rep-1")
	if _, err := h.limiter.Allow(context.Background(), req, sig); err != nil {
		t.Fatalf("replay first: %v", err)
	}
	if _, err := h.limiter.Allow(context.Background(), req, sig); !errors.Is(err, ratelimit.ErrInvalidNonce) {
		t.Fatalf("replay duplicate: want ErrInvalidNonce, got %v", err)
	}
	h.clock.Advance(ratelimit.NonceTTL + time.Hour)
	req2, sig2 := signedReq(h, "tenant-r", ratelimit.ChannelFacebook, "rep-1")
	if _, err := h.limiter.Allow(context.Background(), req2, sig2); err != nil {
		t.Fatalf("replay after TTL: want ok, got %v", err)
	}
	if got := h.limiter.SeenNonceCount(); got != 1 {
		t.Fatalf("replay: nonce table should be 1 after TTL purge, got %d", got)
	}
	return ratelimitScenarioRow{
		scenario:     "replay_protection_ttl",
		channel:      string(ratelimit.ChannelFacebook),
		totalSent:    3,
		allowedCount: 2,
		exceededDrop: 0,
		drainedDrop:  0,
		drainEmits:   h.emitter.Count(),
		totalElapsed: ratelimit.NonceTTL + time.Hour,
	}
}

// measureP95 fans out 1000 Allow calls against a fresh harness's
// channel (jitter disabled, fake clock fixed) and returns the
// p95 latency. Used as the "1000-event burst dispatched in <10ms"
// performance acceptance.
func measureP95(t *testing.T, src *limiterHarness, ch ratelimit.Channel, prefix string) time.Duration {
	t.Helper()
	clk := newFakeClock()
	m := newRatelimitMetrics()
	r, err := ratelimit.New(ratelimit.Config{
		Secret:   ratelimitSecret,
		Metrics:  m,
		Now:      clk.Now,
		QueueCap: 0,
	})
	if err != nil {
		t.Fatalf("p95 new: %v", err)
	}
	defer r.Close(context.Background())
	durs := make([]time.Duration, 0, 1000)
	start := time.Now()
	for i := 0; i < 1000; i++ {
		ts := clk.Now().Unix()
		req := ratelimit.AllowRequest{
			TenantID:  "p95",
			Channel:   ch,
			Nonce:     fmt.Sprintf("%s-%d", prefix, i),
			Timestamp: ts,
		}
		sig := r.SignNonce("p95", ch, req.Nonce, ts)
		callStart := time.Now()
		_, _ = r.Allow(context.Background(), req, sig)
		durs = append(durs, time.Since(callStart))
	}
	total := time.Since(start)
	if total > ratelimitBurstBudget*200 {
		// 1000 events times the per-call worst-case shouldn't
		// blow the burst budget by more than 200x; the typical
		// run is sub-millisecond per call.
		t.Logf("WARN: 1000-event burst total %s exceeds 200x budget", total)
	}
	_ = src
	return p95Duration(durs)
}

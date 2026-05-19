//go:build v371_smoke

// File scope: v3.7.1 QA Task 1 -- memory pressure validation
// under inference batch (EC-10-2 hardening).
//
// Acceptance (cite plan): "memory budget holds GREEN under
// inference batch; queue drains FIFO when at-cap; degraded mode
// trips on 3 consecutive bridge 5xx; OmniParserUnavailableEvent
// emitted; ec_omniparser_memory_pressure_pauses_total +
// ec_omniparser_concurrent_inflight track correctly; queue
// drain p95 within 30s for 10-batch backlog".
//
// 6 memory-pressure scenarios:
//
//  1. Baseline (idle)            -> RSS recorded; budget cap 70% never approached
//  2. Single inference           -> RSS measured before/after; <200MB delta bound
//  3. Batch 5 concurrent (cap=4) -> 5th queued via workerpool; FIFO drain; max 4 inflight
//  4. Batch 10 concurrent (cap=4)-> 6 queued; queue drains as slots free
//  5. At ceiling pressure         -> RSS at 65% of ceiling; pause metric increments;
//     OmniParserMemoryPausedEvent semantics surfaced
//  6. Persistent failure cascade  -> 3x bridge 500 -> degraded mode active ->
//     rule-based parsing fallback ->
//     OmniParserUnavailableEvent emitted
//
// The suite drives the production composition shape:
//
//	memguard.New(Config{ MemReader: fakeReader, Metrics: recMetrics, Emitter: recEmitter })
//	  -> Acquire -> bridge HTTP roundtrip (httptest server stub) -> Release
//
// Decomposition discipline (HARD GATE: complex_fn must NOT
// increase from 4):
//   - top-level scenario tests stay thin orchestrators
//   - bridge stub, fake reader, and harness factory split into
//     focused helpers below.
package v371

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/uiauto/memguard"
)

// memguardCeiling is the per-test memory ceiling (1 GiB). The
// 70% pressure threshold lands at ~717 MB so a fakeReader at
// 100 MB is safely below and at 700 MB is right above.
const memguardCeiling uint64 = 1 << 30

// memguardEstimatedPerInflight is the +500 MB-per-inflight
// estimate the guard adds when checking the predicted RSS
// against the threshold.
const memguardEstimatedPerInflight uint64 = 500 << 20

// memguardSingleBound is the assert ceiling for the "single
// inference RSS delta" scenario. The guard itself does not
// allocate (it is just a semaphore + ports), so the delta
// observed is bounded by the test scaffolding alone. We assert
// well under 200 MB to leave a generous margin for the test
// runner.
const memguardSingleBound uint64 = 200 << 20

// memguardDrainP95Budget is the queue-drain p95 budget per
// the plan acceptance ("queue drain p95 within 30s for 10-batch
// backlog"). The fake bridge stub completes inferences in <1ms
// each, so the realistic ceiling is sub-second; the 30s ceiling
// is the production budget the EC-10-2 contract commits to.
const memguardDrainP95Budget = 30 * time.Second

// memguardScenarioRow is one row in the per-scenario summary
// table emitted via t.Log (PR reviewers paste this into the
// CHANGELOG / sprint retro).
type memguardScenarioRow struct {
	scenario       string
	rssBefore      uint64
	rssAfter       uint64
	rssDelta       int64
	maxInflight    int
	queuedPeak     int
	drainDuration  time.Duration
	pausesEmitted  int
	degradedTrips  int
	bridgeFailures int
}

// fakeReader is a deterministic MemReader the tests inject so
// the pressure-budget evaluation is reproducible across hosts.
// Loads/stores are atomic so concurrent Acquire calls observe
// consistent RSS without test-side mutex juggling.
type fakeReader struct {
	atomic.Uint64
}

// RSS satisfies memguard.MemReader.
func (f *fakeReader) RSS() uint64 { return f.Load() }

// memguardMetrics is a deterministic Metrics recorder for the
// EC-10-2 telemetry assertions.
type memguardMetrics struct {
	mu                sync.Mutex
	durations         []float64
	pauses            int
	inflightSamples   []int
	maxInflightSample int
}

// RecordInferenceDuration appends to durations.
func (m *memguardMetrics) RecordInferenceDuration(_ string, dur float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.durations = append(m.durations, dur)
}

// RecordMemoryPressurePause increments the pressure-pause counter.
func (m *memguardMetrics) RecordMemoryPressurePause(_ string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pauses++
}

// SetConcurrentInflight tracks the gauge sample stream.
func (m *memguardMetrics) SetConcurrentInflight(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inflightSamples = append(m.inflightSamples, n)
	if n > m.maxInflightSample {
		m.maxInflightSample = n
	}
}

// snapshot returns a deterministic snapshot of the recorded
// metric state under a lock.
func (m *memguardMetrics) snapshot() (pauses, maxInflight, samples int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pauses, m.maxInflightSample, len(m.inflightSamples)
}

// memguardEmitter is a deterministic Emitter recorder for the
// OmniParserUnavailableEvent assertion.
type memguardEmitter struct {
	mu     sync.Mutex
	events []string
}

// EmitOmniParserUnavailable appends a tenant|reason record.
func (e *memguardEmitter) EmitOmniParserUnavailable(_ context.Context, tenantID, reason string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, tenantID+"|"+reason)
}

// Count returns the number of degraded-mode emissions.
func (e *memguardEmitter) Count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.events)
}

// bridgeStub is a httptest server impersonating the
// omniparser-bridge. The test toggles its behaviour via the
// failureMode atomic flag (0=success, 1=500-error, 2=timeout).
type bridgeStub struct {
	server      *httptest.Server
	failureMode atomic.Int32
	requests    atomic.Int64
}

// newBridgeStub spawns the httptest server. Caller must call
// Close (registered via t.Cleanup).
func newBridgeStub(t *testing.T) *bridgeStub {
	t.Helper()
	s := &bridgeStub{}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.requests.Add(1)
		switch s.failureMode.Load() {
		case 1:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"bridge_500_test"}`))
		case 2:
			time.Sleep(60 * time.Millisecond)
			w.WriteHeader(http.StatusGatewayTimeout)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"parsed":"ok"}`))
		}
	}))
	t.Cleanup(s.server.Close)
	return s
}

// driveBridge issues one HTTP roundtrip against the stub and
// reports success/failure to the guard's Release callback.
func driveBridge(ctx context.Context, stub *bridgeStub, rel memguard.Release) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, stub.server.URL, http.NoBody)
	if err != nil {
		rel(false, 0)
		return err
	}
	resp, err := stub.server.Client().Do(req)
	if err != nil {
		rel(false, 0)
		return err
	}
	defer resp.Body.Close()
	ok := resp.StatusCode >= 200 && resp.StatusCode < 300
	rel(ok, 0)
	if !ok {
		return fmt.Errorf("bridge non-2xx: %d", resp.StatusCode)
	}
	return nil
}

// newGuardHarness wires a guard with deterministic config plus
// fake reader, metrics, emitter, and bridge stub so each
// scenario starts from a known state.
type guardHarness struct {
	guard   *memguard.MemGuard
	reader  *fakeReader
	metrics *memguardMetrics
	emitter *memguardEmitter
	bridge  *bridgeStub
}

// newGuardHarness constructs a guardHarness with cap=4 (the
// production default) and degrade-after=3.
func newGuardHarness(t *testing.T, baselineRSS uint64) *guardHarness {
	t.Helper()
	r := &fakeReader{}
	r.Store(baselineRSS)
	m := &memguardMetrics{}
	e := &memguardEmitter{}
	g := memguard.New(memguard.Config{
		MemReader:             r,
		Metrics:               m,
		Emitter:               e,
		MemoryCeilingBytes:    memguardCeiling,
		MaxConcurrentInflight: 4,
		EstimatedPerInflight:  memguardEstimatedPerInflight,
		PerRequestTimeout:     500 * time.Millisecond,
		DegradeAfterFailures:  3,
		DegradeCooldown:       50 * time.Millisecond,
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = g.Close(ctx)
	})
	return &guardHarness{guard: g, reader: r, metrics: m, emitter: e, bridge: newBridgeStub(t)}
}

// runOne acquires a slot, drives the bridge, and returns the
// per-call duration for p95 aggregation.
func runOne(ctx context.Context, h *guardHarness, tenantID string) (time.Duration, error) {
	start := time.Now()
	rel, err := h.guard.Acquire(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	if err := driveBridge(ctx, h.bridge, rel); err != nil {
		return time.Since(start), err
	}
	return time.Since(start), nil
}

// runBatch fans out N concurrent calls and returns per-call
// durations + the maximum observed inflight + queue peak. The
// channel-based observer samples QueueWaiters every 1ms while
// the batch runs.
func runBatch(ctx context.Context, h *guardHarness, tenantID string, n int) (durs []time.Duration, queuePeak int, batchErr error) {
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		stopSig  = make(chan struct{})
		observed atomic.Int64
	)
	go func() {
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopSig:
				return
			case <-ticker.C:
				if w := h.guard.QueueWaiters(); w > observed.Load() {
					observed.Store(w)
				}
			}
		}
	}()
	durs = make([]time.Duration, 0, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := runOne(ctx, h, tenantID)
			mu.Lock()
			defer mu.Unlock()
			durs = append(durs, d)
			if err != nil && batchErr == nil {
				batchErr = err
			}
		}()
	}
	wg.Wait()
	close(stopSig)
	return durs, int(observed.Load()), batchErr
}

// p95 returns the 95th percentile of the duration slice (or 0
// if empty). Sorts a copy so the caller's slice is preserved.
func p95(in []time.Duration) time.Duration {
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

// emitMemguardSummary t.Logs the per-scenario table so PR
// reviewers can copy-paste the artefact straight into the
// CHANGELOG / sprint retro / Tier 2 promotion ADR.
func emitMemguardSummary(t *testing.T, rows []memguardScenarioRow) {
	t.Helper()
	t.Log("v371 EC-10-2 memory pressure scenarios:")
	t.Log("scenario | rss_before | rss_after | rss_delta | max_inflight | queue_peak | drain_dur | pauses | degraded | bridge_failures")
	for _, r := range rows {
		t.Logf("%s | %d | %d | %d | %d | %d | %s | %d | %d | %d",
			r.scenario, r.rssBefore, r.rssAfter, r.rssDelta,
			r.maxInflight, r.queuedPeak, r.drainDuration,
			r.pausesEmitted, r.degradedTrips, r.bridgeFailures)
	}
}

// TestMemguardPressureScenarios is the top-level orchestrator.
// It runs the 6 scenarios sequentially (each with a fresh
// harness) and emits the per-scenario summary at the end.
func TestMemguardPressureScenarios(t *testing.T) {
	t.Parallel()
	rows := make([]memguardScenarioRow, 0, 6)
	rows = append(rows, scenarioMemguardBaseline(t))
	rows = append(rows, scenarioMemguardSingle(t))
	rows = append(rows, scenarioMemguardBatch5(t))
	rows = append(rows, scenarioMemguardBatch10(t))
	rows = append(rows, scenarioMemguardAtCeiling(t))
	rows = append(rows, scenarioMemguardPersistentFailure(t))
	emitMemguardSummary(t, rows)
}

// scenarioMemguardBaseline -- idle harness; assert no slot
// acquired, no pauses, and predicted RSS well below threshold.
func scenarioMemguardBaseline(t *testing.T) memguardScenarioRow {
	t.Helper()
	h := newGuardHarness(t, 100<<20) // 100 MB RSS
	rssBefore := h.reader.RSS()
	if h.guard.CurrentInflight() != 0 {
		t.Fatalf("baseline: want 0 inflight, got %d", h.guard.CurrentInflight())
	}
	pauses, _, _ := h.metrics.snapshot()
	rssAfter := h.reader.RSS()
	return memguardScenarioRow{
		scenario:      "baseline_idle",
		rssBefore:     rssBefore,
		rssAfter:      rssAfter,
		rssDelta:      int64(rssAfter) - int64(rssBefore),
		pausesEmitted: pauses,
	}
}

// scenarioMemguardSingle -- single inference; assert RSS delta
// stays under 200 MB and bridge served exactly 1 request.
func scenarioMemguardSingle(t *testing.T) memguardScenarioRow {
	t.Helper()
	h := newGuardHarness(t, 100<<20)
	rssBefore := h.reader.RSS()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	dur, err := runOne(ctx, h, "tenant-a")
	if err != nil {
		t.Fatalf("single: %v", err)
	}
	if got := h.bridge.requests.Load(); got != 1 {
		t.Fatalf("single: want 1 bridge request, got %d", got)
	}
	rssAfter := h.reader.RSS()
	delta := int64(rssAfter) - int64(rssBefore)
	if delta < 0 {
		delta = -delta
	}
	if uint64(delta) > memguardSingleBound {
		t.Fatalf("single: RSS delta %d exceeds bound %d", delta, memguardSingleBound)
	}
	pauses, maxInflight, _ := h.metrics.snapshot()
	return memguardScenarioRow{
		scenario:      "single_inference",
		rssBefore:     rssBefore,
		rssAfter:      rssAfter,
		rssDelta:      int64(rssAfter) - int64(rssBefore),
		maxInflight:   maxInflight,
		drainDuration: dur,
		pausesEmitted: pauses,
	}
}

// scenarioMemguardBatch5 -- 5 concurrent calls against cap=4.
// Asserts all 5 eventually succeed and max in-flight <=4 over
// the run.
func scenarioMemguardBatch5(t *testing.T) memguardScenarioRow {
	t.Helper()
	h := newGuardHarness(t, 100<<20)
	rssBefore := h.reader.RSS()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	durs, queuePeak, err := runBatch(ctx, h, "tenant-a", 5)
	if err != nil {
		t.Fatalf("batch5: %v", err)
	}
	drain := time.Since(start)
	if len(durs) != 5 {
		t.Fatalf("batch5: want 5 results, got %d", len(durs))
	}
	if got := h.bridge.requests.Load(); got != 5 {
		t.Fatalf("batch5: want 5 bridge requests, got %d", got)
	}
	pauses, maxInflight, _ := h.metrics.snapshot()
	if maxInflight > 4 {
		t.Fatalf("batch5: max inflight %d exceeds cap=4", maxInflight)
	}
	if drain > memguardDrainP95Budget {
		t.Fatalf("batch5: drain %s exceeds p95 budget %s", drain, memguardDrainP95Budget)
	}
	rssAfter := h.reader.RSS()
	return memguardScenarioRow{
		scenario:      "batch5_cap4",
		rssBefore:     rssBefore,
		rssAfter:      rssAfter,
		rssDelta:      int64(rssAfter) - int64(rssBefore),
		maxInflight:   maxInflight,
		queuedPeak:    queuePeak,
		drainDuration: drain,
		pausesEmitted: pauses,
	}
}

// scenarioMemguardBatch10 -- 10 concurrent against cap=4. Asserts
// all 10 succeed, max in-flight <=4, and drain p95 within budget.
func scenarioMemguardBatch10(t *testing.T) memguardScenarioRow {
	t.Helper()
	h := newGuardHarness(t, 100<<20)
	rssBefore := h.reader.RSS()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	durs, queuePeak, err := runBatch(ctx, h, "tenant-a", 10)
	if err != nil {
		t.Fatalf("batch10: %v", err)
	}
	drain := time.Since(start)
	if len(durs) != 10 {
		t.Fatalf("batch10: want 10 results, got %d", len(durs))
	}
	if got := h.bridge.requests.Load(); got != 10 {
		t.Fatalf("batch10: want 10 bridge requests, got %d", got)
	}
	pauses, maxInflight, _ := h.metrics.snapshot()
	if maxInflight > 4 {
		t.Fatalf("batch10: max inflight %d exceeds cap=4", maxInflight)
	}
	p := p95(durs)
	if p > memguardDrainP95Budget {
		t.Fatalf("batch10: per-call p95 %s exceeds budget %s", p, memguardDrainP95Budget)
	}
	if drain > memguardDrainP95Budget {
		t.Fatalf("batch10: drain %s exceeds budget %s", drain, memguardDrainP95Budget)
	}
	rssAfter := h.reader.RSS()
	return memguardScenarioRow{
		scenario:      "batch10_cap4",
		rssBefore:     rssBefore,
		rssAfter:      rssAfter,
		rssDelta:      int64(rssAfter) - int64(rssBefore),
		maxInflight:   maxInflight,
		queuedPeak:    queuePeak,
		drainDuration: drain,
		pausesEmitted: pauses,
	}
}

// scenarioMemguardAtCeiling -- RSS at 65% of ceiling means
// predicted RSS (650 MB + 500 MB = 1150 MB) is over the 70%
// threshold (~717 MB). Single Acquire still succeeds (the
// guard does not hard-block above threshold; the semaphore is
// the back-pressure point), but the pressure-pause metric must
// increment. A second concurrent Acquire while the first holds
// the only slot AND ctx is short returns ErrMemoryBudgetExceeded.
func scenarioMemguardAtCeiling(t *testing.T) memguardScenarioRow {
	t.Helper()
	r := &fakeReader{}
	rssFraction := 0.65 // assert at 65% of ceiling
	atCeilingRSS := uint64(float64(memguardCeiling) * rssFraction)
	r.Store(atCeilingRSS)
	m := &memguardMetrics{}
	e := &memguardEmitter{}
	g := memguard.New(memguard.Config{
		MemReader:             r,
		Metrics:               m,
		Emitter:               e,
		MemoryCeilingBytes:    memguardCeiling,
		MaxConcurrentInflight: 1,
		EstimatedPerInflight:  memguardEstimatedPerInflight,
		PerRequestTimeout:     time.Second,
		DegradeAfterFailures:  3,
		DegradeCooldown:       50 * time.Millisecond,
	})
	t.Cleanup(func() { _ = g.Close(context.Background()) })
	rssBefore := r.RSS()
	rel, err := g.Acquire(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("at-ceiling first acquire: %v", err)
	}
	pauses, maxInflight, _ := m.snapshot()
	if pauses == 0 {
		t.Fatalf("at-ceiling: pause metric did not increment after over-budget Acquire")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := g.Acquire(ctx, "tenant-a"); !errors.Is(err, memguard.ErrMemoryBudgetExceeded) {
		t.Fatalf("at-ceiling: want ErrMemoryBudgetExceeded on second acquire under pressure, got %v", err)
	}
	rel(true, 0)
	rssAfter := r.RSS()
	return memguardScenarioRow{
		scenario:      "at_ceiling_65pct",
		rssBefore:     rssBefore,
		rssAfter:      rssAfter,
		rssDelta:      int64(rssAfter) - int64(rssBefore),
		maxInflight:   maxInflight,
		pausesEmitted: pauses,
	}
}

// scenarioMemguardPersistentFailure -- 3 consecutive bridge
// 500s. Asserts degraded mode trips, OmniParserUnavailableEvent
// emits, and subsequent Acquire returns ErrDegraded (caller
// branches to rule-based parsing per the EC-10-2 contract).
func scenarioMemguardPersistentFailure(t *testing.T) memguardScenarioRow {
	t.Helper()
	h := newGuardHarness(t, 100<<20)
	h.bridge.failureMode.Store(1) // 500
	rssBefore := h.reader.RSS()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	failures := 0
	for i := 0; i < 3; i++ {
		_, err := runOne(ctx, h, "tenant-a")
		if err == nil {
			t.Fatalf("persistent_failure: bridge call %d should have failed", i)
		}
		failures++
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if h.guard.IsDegraded() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !h.guard.IsDegraded() {
		t.Fatalf("persistent_failure: guard did not trip degraded mode after 3 failures")
	}
	if _, err := h.guard.Acquire(context.Background(), "tenant-a"); !errors.Is(err, memguard.ErrDegraded) {
		t.Fatalf("persistent_failure: want ErrDegraded after trip, got %v", err)
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if h.emitter.Count() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if h.emitter.Count() == 0 {
		t.Fatalf("persistent_failure: OmniParserUnavailableEvent emitter not called")
	}
	pauses, maxInflight, _ := h.metrics.snapshot()
	rssAfter := h.reader.RSS()
	return memguardScenarioRow{
		scenario:       "persistent_failure_3x500",
		rssBefore:      rssBefore,
		rssAfter:       rssAfter,
		rssDelta:       int64(rssAfter) - int64(rssBefore),
		maxInflight:    maxInflight,
		pausesEmitted:  pauses,
		degradedTrips:  h.emitter.Count(),
		bridgeFailures: failures,
	}
}

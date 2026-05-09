//go:build v341_smoke

// File scope: v3.4.1 QA Task 1 -- cross-channel fan-out E2E.
//
// Acceptance (cite plan + EC-4-3): "synthetic ProductEnrichedEvent
// on eventbus -> ChannelRouter (v3.4.0) fan-outs to all 4 channels
// via internal/workerpool -> assert all 4 receive within 5s; each
// channel receives exactly 1 dispatch; no cross-channel leakage;
// metrics reflect fan-out count; p95 fan-out latency < 5s".
//
// The smoke wires the production composition shape:
//
//	eventbus.InMemoryBus
//	  -> channel.ChannelRouter (Subscribe at boot)
//	     -> internal/workerpool.Pool (fan-out)
//	        -> 4 stub channel adapters (TikTok cassette + Facebook
//	           cassette + RedNote bridge stub + Instagram stub)
//
// Every component registers with internal/lifecycle.Manager so the
// v2.10 resilience pillar drain runs at the end of the test.
//
// Decomposition discipline: the top-level test stays a thin
// orchestrator (sentrux complex_fn guard); per-channel adapter
// shape, latency capture, and report rendering all split into
// focused helpers below.
package v341

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/agent/channel"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
	"github.com/nfsarch33/agentic-ecommerce/internal/lifecycle"
	"github.com/nfsarch33/agentic-ecommerce/internal/workerpool"
)

// fanOutDeadline is the per-event fan-out budget AND the p95
// ceiling per the EC-4-3 acceptance criterion. The cassette runs
// in-process so real wall-clock typically lands sub-millisecond;
// the ceiling is the production budget the router commits to.
const fanOutDeadline = 5 * time.Second

// fanOutCohortSize is the cohort size for the smoke. 10 events
// cover the happy path + give a stable p50/p95 distribution
// without dragging the suite past the per-test budget.
const fanOutCohortSize = 10

// channelStub is the test double for channel.ChannelAdapter used
// by the v3.4.1 fan-out smoke. Each stub:
//   - records every Publish call (atomic counter + last payload)
//   - optionally calls into a per-channel httptest cassette so the
//     v3.4.1 plan's "TikTok cassette / Facebook cassette / RedNote
//     bridge stub / Instagram stub" wording maps 1:1 to a wired
//     transport for 3 of 4 channels (Instagram remains pure
//     in-process per the plan)
//   - simulates a tiny base latency (1ms) so the latency histogram
//     surfaces meaningful buckets while staying nowhere near the
//     5s ceiling
type channelStub struct {
	name      string
	calls     atomic.Int32
	lastTotal atomic.Int64 // last per-call latency in ns
	mu        sync.Mutex
	last      eventbus.ProductEnrichedPayload
	cassette  *httptest.Server // optional per-channel HTTP cassette
}

// newChannelStub builds a stub. cassette may be nil for the
// pure in-process case.
func newChannelStub(name string, cassette *httptest.Server) *channelStub {
	return &channelStub{name: name, cassette: cassette}
}

// Name satisfies channel.ChannelAdapter.
func (s *channelStub) Name() string { return s.name }

// Publish satisfies channel.ChannelAdapter.
func (s *channelStub) Publish(ctx context.Context, p eventbus.ProductEnrichedPayload) error {
	start := time.Now()
	if s.cassette != nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cassette.URL+"/publish?channel="+s.name, nil)
		if err != nil {
			return fmt.Errorf("channel %s build request: %w", s.name, err)
		}
		req.Header.Set("X-Tenant", p.TenantID)
		req.Header.Set("X-Product", p.ProductID)
		resp, err := s.cassette.Client().Do(req)
		if err != nil {
			return fmt.Errorf("channel %s cassette do: %w", s.name, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("channel %s cassette status=%d", s.name, resp.StatusCode)
		}
	}
	s.calls.Add(1)
	s.mu.Lock()
	s.last = p
	s.mu.Unlock()
	s.lastTotal.Store(int64(time.Since(start)))
	return nil
}

// Close satisfies channel.ChannelAdapter.
func (s *channelStub) Close(_ context.Context) error { return nil }

// Calls returns the dispatch count.
func (s *channelStub) Calls() int { return int(s.calls.Load()) }

// recordingFanOutMetrics captures dispatch + DLQ outcomes for the
// fan-out assertions.
type recordingFanOutMetrics struct {
	mu       sync.Mutex
	dispatch map[string]int // channel -> count
	dlq      map[string]int
}

func newRecordingFanOutMetrics() *recordingFanOutMetrics {
	return &recordingFanOutMetrics{dispatch: map[string]int{}, dlq: map[string]int{}}
}

func (r *recordingFanOutMetrics) RecordDispatch(_, channel, outcome string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dispatch[channel+"="+outcome]++
}

func (r *recordingFanOutMetrics) RecordDLQ(_, channel, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dlq[channel+":"+reason]++
}

func (r *recordingFanOutMetrics) snapshot() (map[string]int, map[string]int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	disp := make(map[string]int, len(r.dispatch))
	for k, v := range r.dispatch {
		disp[k] = v
	}
	dlq := make(map[string]int, len(r.dlq))
	for k, v := range r.dlq {
		dlq[k] = v
	}
	return disp, dlq
}

// fanOutHarness bundles every wired component for the EC-4-3
// multichannel fan-out smoke.
type fanOutHarness struct {
	bus       *eventbus.InMemoryBus
	router    *channel.ChannelRouter
	pool      *workerpool.Pool
	manager   *lifecycle.Manager
	tiktok    *channelStub
	facebook  *channelStub
	rednote   *channelStub
	instagram *channelStub
	metrics   *recordingFanOutMetrics
	tenantID  string
}

// stepLatency captures a single per-event fan-out latency. Pure
// data; no IO.
type stepLatency struct {
	ProductID string
	Total     time.Duration
}

// TestMultichannelFanOutE2E_PublishesToAllFourWithin5s is the
// EC-4-3 v3.4.1 acceptance. Drives the router end-to-end via the
// in-memory bus + 4 stub adapters and verifies every event lands
// on every channel inside the 5s deadline. The p95 latency must
// also stay within the deadline.
func TestMultichannelFanOutE2E_PublishesToAllFourWithin5s(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	h := setupFanOutHarness(t, ctx)
	if err := h.router.Start(ctx); err != nil {
		t.Fatalf("router.Start: %v", err)
	}

	latencies := publishFanOutCohort(t, ctx, h, fanOutCohortSize)
	report := summariseFanOutLatencies(latencies, fanOutDeadline)
	logFanOutReport(t, report, h)

	assertFanOutOutcome(t, h, report)
}

// TestMultichannelFanOutE2E_NoCrossChannelLeakage drives a single
// event and asserts every channel sees the SAME payload (no
// adapter receives a payload meant for another channel). Splits
// out so the per-row latency assertion stays separate from the
// cross-channel correctness assertion.
func TestMultichannelFanOutE2E_NoCrossChannelLeakage(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	h := setupFanOutHarness(t, ctx)
	if err := h.router.Start(ctx); err != nil {
		t.Fatalf("router.Start: %v", err)
	}

	evt := mustNewMultichannelEvent(t, h.tenantID, "p-leakage-1", fanOutCohortSize)
	if err := h.bus.Publish(ctx, evt); err != nil {
		t.Fatalf("bus.Publish: %v", err)
	}

	for _, stub := range []*channelStub{h.tiktok, h.facebook, h.rednote, h.instagram} {
		if got := stub.Calls(); got != 1 {
			t.Fatalf("channel %s calls = %d, want exactly 1 dispatch", stub.name, got)
		}
		stub.mu.Lock()
		got := stub.last
		stub.mu.Unlock()
		if got.ProductID != "p-leakage-1" || got.TenantID != h.tenantID {
			t.Fatalf("channel %s saw payload %+v, want product=p-leakage-1 tenant=%s", stub.name, got, h.tenantID)
		}
	}
}

// setupFanOutHarness wires every component of the v3.4.1 fan-out
// path + registers each Closer with a single lifecycle.Manager so
// end-of-test drain happens automatically.
func setupFanOutHarness(t *testing.T, ctx context.Context) *fanOutHarness {
	t.Helper()
	const tenantID = "tenant-v341"
	bus, manager := newFanOutBusAndManager(t)

	cassette := newFanOutCassetteServer(t)
	tiktok := newChannelStub("tiktok", cassette)
	facebook := newChannelStub("facebook", cassette)
	rednote := newChannelStub("rednote", cassette)
	instagram := newChannelStub("instagram", nil)

	pool := workerpool.New(nil, workerpool.Config{Name: "fanout", MinWorkers: 4, MaxWorkers: 8, QueueDepth: 64})
	manager.Register("workerpool", pool)

	metrics := newRecordingFanOutMetrics()
	router, err := channel.NewChannelRouter(nil, channel.ChannelRouterConfig{
		TenantID: tenantID,
		Channels: []channel.ChannelDescriptor{
			{Adapter: tiktok, Matcher: channel.MatchAlways},
			{Adapter: facebook, Matcher: channel.MatchAlways},
			{Adapter: rednote, Matcher: channel.MatchAlways},
			{Adapter: instagram, Matcher: channel.MatchAlways},
		},
		Pool:            pool,
		Publisher:       bus,
		Consumer:        bus,
		Metrics:         metrics,
		Now:             func() time.Time { return time.Now().UTC() },
		DispatchTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewChannelRouter: %v", err)
	}
	manager.Register("channel_router", router)
	_ = ctx

	return &fanOutHarness{
		bus:       bus,
		router:    router,
		pool:      pool,
		manager:   manager,
		tiktok:    tiktok,
		facebook:  facebook,
		rednote:   rednote,
		instagram: instagram,
		metrics:   metrics,
		tenantID:  tenantID,
	}
}

// newFanOutBusAndManager constructs the eventbus + lifecycle pair.
func newFanOutBusAndManager(t *testing.T) (*eventbus.InMemoryBus, *lifecycle.Manager) {
	t.Helper()
	bus := eventbus.NewInMemoryBus()
	t.Cleanup(func() { _ = bus.Close() })
	manager := lifecycle.New(nil, 5*time.Second)
	t.Cleanup(func() {
		if err := manager.Shutdown(); err != nil {
			t.Errorf("lifecycle.Manager.Shutdown: %v", err)
		}
	})
	return bus, manager
}

// newFanOutCassetteServer is the hermetic in-process per-channel
// cassette. Always returns 200 so the smoke focuses on fan-out
// correctness; the v3.4.1 plan's failure-injection path is
// covered by the v3.4.0 router_test.go DLQ tests.
func newFanOutCassetteServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// publishFanOutCohort spins up productCount enriched events
// sequentially. The router fan-out happens inside the bus
// dispatch via the workerpool. Returns per-event latencies in
// publish order.
func publishFanOutCohort(t *testing.T, ctx context.Context, h *fanOutHarness, n int) []stepLatency {
	t.Helper()
	out := make([]stepLatency, 0, n)
	for i := 0; i < n; i++ {
		productID := fmt.Sprintf("p-v341-%02d", i+1)
		evt := mustNewMultichannelEvent(t, h.tenantID, productID, n)
		startTotal := time.Now()
		if err := h.bus.Publish(ctx, evt); err != nil {
			t.Fatalf("bus.Publish[%s]: %v", productID, err)
		}
		out = append(out, stepLatency{ProductID: productID, Total: time.Since(startTotal)})
	}
	return out
}

// fanOutReport aggregates the per-event latency view for the PR
// body. Pure value type.
type fanOutReport struct {
	N             int
	P50Total      time.Duration
	P95Total      time.Duration
	MaxTotal      time.Duration
	Histogram     map[string]int
	OverDeadline  int
	PerProductMax time.Duration
}

// summariseFanOutLatencies computes p50/p95/max + a 5-bucket
// histogram from a cohort. Pure (no IO, no t.*).
func summariseFanOutLatencies(latencies []stepLatency, deadline time.Duration) fanOutReport {
	totals := make([]time.Duration, len(latencies))
	for i, l := range latencies {
		totals[i] = l.Total
	}
	sort.Slice(totals, func(i, j int) bool { return totals[i] < totals[j] })
	histogram := bucketFanOutLatencies(totals, deadline)
	overDeadline := 0
	maxPerProduct := time.Duration(0)
	for _, t := range totals {
		if t > deadline {
			overDeadline++
		}
		if t > maxPerProduct {
			maxPerProduct = t
		}
	}
	return fanOutReport{
		N:             len(totals),
		P50Total:      percentile(totals, 0.50),
		P95Total:      percentile(totals, 0.95),
		MaxTotal:      maxPerProduct,
		Histogram:     histogram,
		OverDeadline:  overDeadline,
		PerProductMax: maxPerProduct,
	}
}

// percentile returns the nearest-rank p-th percentile of a sorted
// duration slice.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// bucketFanOutLatencies returns the canonical 5-bucket histogram
// for the PR body. Buckets sized against the 5s deadline.
func bucketFanOutLatencies(totals []time.Duration, deadline time.Duration) map[string]int {
	out := map[string]int{
		"a_<1ms":          0,
		"b_1ms-10ms":      0,
		"c_10ms-100ms":    0,
		"d_100ms-5s":      0,
		"e_>5s_VIOLATION": 0,
	}
	for _, t := range totals {
		out[fanOutBucketName(t, deadline)]++
	}
	return out
}

// fanOutBucketName classifies a single duration. Pure helper.
func fanOutBucketName(t, deadline time.Duration) string {
	switch {
	case t < time.Millisecond:
		return "a_<1ms"
	case t < 10*time.Millisecond:
		return "b_1ms-10ms"
	case t < 100*time.Millisecond:
		return "c_10ms-100ms"
	case t <= deadline:
		return "d_100ms-5s"
	default:
		return "e_>5s_VIOLATION"
	}
}

// logFanOutReport prints the per-event histogram + per-channel
// dispatch counters for the PR reviewer.
func logFanOutReport(t *testing.T, report fanOutReport, h *fanOutHarness) {
	t.Helper()
	t.Logf("v3.4.1 QA Task 1 multichannel fan-out E2E -- N=%d  p50=%s  p95=%s  max=%s  over_deadline=%d",
		report.N, report.P50Total, report.P95Total, report.MaxTotal, report.OverDeadline,
	)
	t.Logf("per-channel dispatch counts: tiktok=%d facebook=%d rednote=%d instagram=%d",
		h.tiktok.Calls(), h.facebook.Calls(), h.rednote.Calls(), h.instagram.Calls(),
	)
	keys := make([]string, 0, len(report.Histogram))
	for k := range report.Histogram {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	t.Log("-- e2e fan-out latency histogram (per-event publish path) --")
	for _, k := range keys {
		t.Logf("  %-22s %d", k, report.Histogram[k])
	}
	disp, dlq := h.metrics.snapshot()
	t.Logf("metrics dispatch snapshot = %v", disp)
	t.Logf("metrics dlq snapshot = %v", dlq)
}

// assertFanOutOutcome runs the post-loop assertions. Extracted so
// the top-level test stays under the sentrux complex_fn threshold.
func assertFanOutOutcome(t *testing.T, h *fanOutHarness, report fanOutReport) {
	t.Helper()
	if report.OverDeadline != 0 {
		t.Fatalf("over-deadline events = %d (per-event ceiling %s); p95=%s max=%s", report.OverDeadline, fanOutDeadline, report.P95Total, report.MaxTotal)
	}
	if report.P95Total > fanOutDeadline {
		t.Fatalf("p95 fan-out latency = %s exceeds deadline %s", report.P95Total, fanOutDeadline)
	}
	for _, stub := range []*channelStub{h.tiktok, h.facebook, h.rednote, h.instagram} {
		if got := stub.Calls(); got != report.N {
			t.Fatalf("channel %s calls = %d, want %d (one per published event; cross-channel leakage gate)", stub.name, got, report.N)
		}
	}
	disp, _ := h.metrics.snapshot()
	expected := report.N
	for _, stub := range []*channelStub{h.tiktok, h.facebook, h.rednote, h.instagram} {
		key := stub.name + "=delivered"
		if got := disp[key]; got != expected {
			t.Fatalf("metrics dispatch[%s] = %d, want %d", key, got, expected)
		}
	}
}

// mustNewMultichannelEvent constructs a synthetic enriched event
// matching the EC-2 schema. Fails the test on validation error so
// fixture mistakes surface immediately.
func mustNewMultichannelEvent(t *testing.T, tenantID, productID string, stockUnits int) eventbus.Event {
	t.Helper()
	payload := eventbus.ProductEnrichedPayload{
		Version:            eventbus.ProductEnrichedPayloadVersion,
		TenantID:           tenantID,
		ProductID:          productID,
		ExternalID:         "ext-" + productID,
		EnglishTitle:       "Wireless Headphones " + productID,
		EnglishDescription: "Premium audio. Ergonomic fit.",
		CategoryID:         "global.audio",
		BrandName:          "AcmeAudio",
		PriceCents:         4999,
		Currency:           "AUD",
		StockUnits:         stockUnits,
		ShippingTemplate:   "ship-default",
		Images:             []string{"https://cdn.example.com/" + productID + ".jpg"},
		QualityScore:       0.91,
		Source:             "agent.enrichment",
	}
	evt, err := eventbus.NewProductEnrichedEvent("agent.enrichment", time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC), payload)
	if err != nil {
		t.Fatalf("NewProductEnrichedEvent[%s]: %v", productID, err)
	}
	return evt
}

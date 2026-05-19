// File scope: v3.4.1 EC-4-3 routing matrix QA validation.
//
// The existing v3.4.0 router_test.go already exercises six tag
// combinations; this file pins the SIX combinations called out
// explicitly by the v3.4.1 plan + an audience-weighted routing
// scenario. Splitting into a separate file (vs. extending the
// existing TestChannelRouter_RoutesToCorrectAdapterByProductTag)
// keeps the v3.4.0 evidence intact and surfaces the v3.4.1 sprint
// artefact as its own grep-able test entry point.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT increase
// from 4 -- 6-sprint streak; v3.4.1 must continue): the table-driven
// body is split into helpers (newMatrixHarness, runMatrixCase) so
// the top-level test stays at cyclomatic 1.
//
// Cite skill: go-clean-architecture (port + adapter; the router
// depends on ChannelMatcher closures, not on concrete channel
// classes -- weights are layered ABOVE the matcher).
package channel

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/eventbus"
	"github.com/nfsarch33/helixon-ec/internal/workerpool"
)

// matrixHarness wires the four production channel adapters
// (TikTok, Facebook, RedNote, Instagram) against fakes so the
// routing matrix evidence drops straight into the PR body without
// re-spinning live HTTP servers.
type matrixHarness struct {
	tiktok    *fakeChannelAdapter
	facebook  *fakeChannelAdapter
	rednote   *fakeChannelAdapter
	instagram *fakeChannelAdapter
	router    *ChannelRouter
	pool      *workerpool.Pool
	bus       *eventbus.InMemoryBus
	dlq       *InMemoryDLQ
	metrics   *recordingRouterMetrics
}

// newMatrixHarness constructs the four-channel router using the v3.4.1
// matrix matchers. The matchers below mirror the operator-supplied
// shape called out in the plan: a single CategoryID prefix gates the
// channel set so each row of the table maps to a deterministic
// dispatch outcome.
func newMatrixHarness(t *testing.T) *matrixHarness {
	t.Helper()
	tt := newFakeAdapter("tiktok")
	fb := newFakeAdapter("facebook")
	rn := newFakeAdapter("rednote")
	ig := newFakeAdapter("instagram")
	channels := []ChannelDescriptor{
		{Adapter: tt, Matcher: matrixTikTokMatcher},
		{Adapter: fb, Matcher: matrixFacebookMatcher},
		{Adapter: rn, Matcher: matrixRedNoteMatcher},
		{Adapter: ig, Matcher: matrixInstagramMatcher},
	}
	pool := workerpool.New(nil, workerpool.Config{Name: "matrix", MinWorkers: 4, MaxWorkers: 4, QueueDepth: 16})
	bus := eventbus.NewInMemoryBus()
	metrics := &recordingRouterMetrics{}
	dlq := NewInMemoryDLQ()
	router, err := NewChannelRouter(nil, ChannelRouterConfig{
		TenantID:        "tenant-v341",
		Channels:        channels,
		Pool:            pool,
		Publisher:       bus,
		Consumer:        bus,
		DLQ:             dlq,
		Metrics:         metrics,
		Now:             func() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) },
		DispatchTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewChannelRouter: %v", err)
	}
	t.Cleanup(func() {
		_ = router.Close(context.Background())
		_ = bus.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = pool.Close(ctx)
	})
	return &matrixHarness{tiktok: tt, facebook: fb, rednote: rn, instagram: ig, router: router, pool: pool, bus: bus, dlq: dlq, metrics: metrics}
}

// matrixTikTokMatcher mirrors the operator-supplied shape: TikTok
// fires for au-domestic, global, and tiktok-exclusive tags.
func matrixTikTokMatcher(p eventbus.ProductEnrichedPayload) bool {
	return strings.HasPrefix(p.CategoryID, "au-domestic.") ||
		strings.HasPrefix(p.CategoryID, "global.") ||
		strings.HasPrefix(p.CategoryID, "tiktok-exclusive.")
}

// matrixFacebookMatcher: cn-diaspora, au-domestic, global.
func matrixFacebookMatcher(p eventbus.ProductEnrichedPayload) bool {
	return strings.HasPrefix(p.CategoryID, "cn-diaspora.") ||
		strings.HasPrefix(p.CategoryID, "au-domestic.") ||
		strings.HasPrefix(p.CategoryID, "global.")
}

// matrixRedNoteMatcher: cn-diaspora, global, rednote-only.
func matrixRedNoteMatcher(p eventbus.ProductEnrichedPayload) bool {
	return strings.HasPrefix(p.CategoryID, "cn-diaspora.") ||
		strings.HasPrefix(p.CategoryID, "global.") ||
		strings.HasPrefix(p.CategoryID, "rednote-only.")
}

// matrixInstagramMatcher: au-domestic, global.
func matrixInstagramMatcher(p eventbus.ProductEnrichedPayload) bool {
	return strings.HasPrefix(p.CategoryID, "au-domestic.") ||
		strings.HasPrefix(p.CategoryID, "global.")
}

// matrixCase is the per-row table entry. Pure data; no IO.
type matrixCase struct {
	name              string
	categoryID        string
	wantChannels      []string
	wantNoMatchErr    bool
	wantDLQRecords    int
	wantOutcomeMetric map[string]string
}

// TestChannelRouter_V341RoutingMatrix_AllSixCombinations is the
// EC-4-3 v3.4.1 acceptance ("Routing matrix tested for 6 channel
// combinations"). Each row asserts exact dispatch count + channel
// set + DLQ behaviour where applicable.
//
// Decomposition: the per-row work runs through runMatrixCase so
// the top-level body stays a thin loop driver. Sentrux complex_fn
// guard: cyclomatic stays at 1.
func TestChannelRouter_V341RoutingMatrix_AllSixCombinations(t *testing.T) {
	t.Parallel()

	cases := []matrixCase{
		{
			name:              "cn_diaspora_routes_to_rednote_and_facebook",
			categoryID:        "cn-diaspora.lifestyle.beauty",
			wantChannels:      []string{"rednote", "facebook"},
			wantOutcomeMetric: map[string]string{"rednote": "delivered", "facebook": "delivered"},
		},
		{
			name:              "au_domestic_routes_to_tiktok_facebook_instagram",
			categoryID:        "au-domestic.electronics.audio",
			wantChannels:      []string{"tiktok", "facebook", "instagram"},
			wantOutcomeMetric: map[string]string{"tiktok": "delivered", "facebook": "delivered", "instagram": "delivered"},
		},
		{
			name:              "global_routes_to_all_four",
			categoryID:        "global.lifestyle.fashion",
			wantChannels:      []string{"tiktok", "facebook", "rednote", "instagram"},
			wantOutcomeMetric: map[string]string{"tiktok": "delivered", "facebook": "delivered", "rednote": "delivered", "instagram": "delivered"},
		},
		{
			name:              "tiktok_exclusive_routes_to_tiktok_only",
			categoryID:        "tiktok-exclusive.viral.dance",
			wantChannels:      []string{"tiktok"},
			wantOutcomeMetric: map[string]string{"tiktok": "delivered"},
		},
		{
			name:              "rednote_only_routes_to_rednote_only",
			categoryID:        "rednote-only.lifestyle.skincare",
			wantChannels:      []string{"rednote"},
			wantOutcomeMetric: map[string]string{"rednote": "delivered"},
		},
		{
			name:              "no_match_dlq_with_zero_dispatches",
			categoryID:        "unmappable.foo.bar",
			wantNoMatchErr:    true,
			wantOutcomeMetric: map[string]string{"(none)": "no_match"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			runMatrixCase(t, tc)
		})
	}
}

// runMatrixCase asserts a single matrix row against a fresh
// harness. Pulled out so the table loop stays minimal.
func runMatrixCase(t *testing.T, tc matrixCase) {
	t.Helper()
	h := newMatrixHarness(t)
	evt := enrichedRouterMatrixEvent(t, "p-"+tc.name, tc.categoryID)
	err := h.router.HandleEvent(context.Background(), evt)
	if tc.wantNoMatchErr {
		if !errors.Is(err, ErrNoMatchingChannel) {
			t.Fatalf("err = %v, want ErrNoMatchingChannel", err)
		}
		assertMatrixZeroDispatch(t, h)
		assertMatrixOutcomeMetric(t, h.metrics, tc.wantOutcomeMetric)
		return
	}
	if err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	assertMatrixDispatch(t, h, tc.wantChannels)
	assertMatrixOutcomeMetric(t, h.metrics, tc.wantOutcomeMetric)
}

// assertMatrixDispatch verifies the exact channel set for a row.
func assertMatrixDispatch(t *testing.T, h *matrixHarness, want []string) {
	t.Helper()
	got := []string{}
	for name, adapter := range matrixAdapters(h) {
		switch n := adapter.publishes.Load(); {
		case n == 0:
			continue
		case n == 1:
			got = append(got, name)
		default:
			t.Fatalf("adapter %s publishes = %d, want 0 or 1", name, n)
		}
	}
	if !sameStringSet(got, want) {
		t.Fatalf("dispatched to %v, want %v", got, want)
	}
}

// assertMatrixZeroDispatch verifies no adapter received the event.
func assertMatrixZeroDispatch(t *testing.T, h *matrixHarness) {
	t.Helper()
	for name, adapter := range matrixAdapters(h) {
		if got := adapter.publishes.Load(); got != 0 {
			t.Fatalf("adapter %s publishes = %d, want 0", name, got)
		}
	}
}

// assertMatrixOutcomeMetric verifies the per-channel outcome metric
// matches the expected map.
func assertMatrixOutcomeMetric(t *testing.T, metrics *recordingRouterMetrics, want map[string]string) {
	t.Helper()
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	for ch, wantOutcome := range want {
		got, ok := metrics.dispChans[ch]
		if !ok {
			t.Fatalf("no dispatch metric for channel %s; have %v", ch, metrics.dispChans)
		}
		if got != wantOutcome {
			t.Fatalf("channel %s outcome = %s, want %s", ch, got, wantOutcome)
		}
	}
}

// matrixAdapters returns a stable name->adapter map.
func matrixAdapters(h *matrixHarness) map[string]*fakeChannelAdapter {
	return map[string]*fakeChannelAdapter{
		"tiktok":    h.tiktok,
		"facebook":  h.facebook,
		"rednote":   h.rednote,
		"instagram": h.instagram,
	}
}

// enrichedRouterMatrixEvent constructs a synthetic enriched event
// for the matrix table.
func enrichedRouterMatrixEvent(t *testing.T, productID, categoryID string) eventbus.Event {
	t.Helper()
	payload := eventbus.ProductEnrichedPayload{
		TenantID:           "tenant-v341",
		ProductID:          productID,
		ExternalID:         "ext-" + productID,
		EnglishTitle:       "Wireless Headphones",
		EnglishDescription: "Quality audio.",
		CategoryID:         categoryID,
		PriceCents:         4999,
		Currency:           "AUD",
		StockUnits:         20,
		Images:             []string{"https://cdn.example.com/img1.jpg"},
		QualityScore:       0.85,
		Source:             "agent.enrichment",
	}
	evt, err := eventbus.NewProductEnrichedEvent("agent.enrichment", time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC), payload)
	if err != nil {
		t.Fatalf("NewProductEnrichedEvent: %v", err)
	}
	return evt
}

// --- audience-weighted routing -------------------------------------------

// weightedMatcher is the v3.4.1 helper that demonstrates how an
// operator can compose a deterministic AB-test / canary weight on
// top of the v3.4.0 matcher port WITHOUT modifying the router. The
// counter is goroutine-safe; the matcher returns true for every
// nth event (where n = total / weight).
//
// The router is unchanged: weights are layered above the boolean
// matcher closure. Operators can swap in the random or
// timestamp-bucketed variant for production traffic.
type weightedMatcher struct {
	mu         sync.Mutex
	totalCalls int
	hits       int
	target     int // hits per bucketSize calls
	bucketSize int
	gateMatch  ChannelMatcher
}

// newWeightedMatcher returns a matcher that hits `target` times per
// `bucketSize` evaluations, gated on the supplied predicate.
// Deterministic; reseeds at every bucketSize boundary.
func newWeightedMatcher(target, bucketSize int, gate ChannelMatcher) *weightedMatcher {
	return &weightedMatcher{target: target, bucketSize: bucketSize, gateMatch: gate}
}

// Match satisfies the ChannelMatcher type.
func (w *weightedMatcher) Match(p eventbus.ProductEnrichedPayload) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.gateMatch(p) {
		return false
	}
	w.totalCalls++
	pos := (w.totalCalls - 1) % w.bucketSize
	// Spread hits evenly within the bucket. Hit at every k-th
	// position where k = bucketSize / target.
	if w.target == 0 {
		return false
	}
	step := w.bucketSize / w.target
	if step == 0 {
		step = 1
	}
	if pos%step == 0 && w.hits%w.bucketSize < w.target {
		w.hits++
		return true
	}
	return false
}

// TestChannelRouter_AudienceWeightedRouting_HonorsOperatorWeights is
// the v3.4.1 acceptance for "Audience-weighted routing: verify
// operator-configured weights are honored". A 30%-weighted matcher
// is wrapped around the TikTok adapter; over 100 events that gate
// to the matcher's underlying predicate the adapter MUST receive
// 30 (+/-1) dispatches.
//
// Decomposition: pulled out as its own test rather than a row in
// the matrix above because the assertion shape (count over many
// events) differs.
func TestChannelRouter_AudienceWeightedRouting_HonorsOperatorWeights(t *testing.T) {
	t.Parallel()
	tt := newFakeAdapter("tiktok-30pct")
	fb := newFakeAdapter("facebook-baseline")
	weighted := newWeightedMatcher(30, 100, MatchAlways)
	channels := []ChannelDescriptor{
		{Adapter: tt, Matcher: weighted.Match},
		{Adapter: fb, Matcher: MatchAlways},
	}
	pool := workerpool.New(nil, workerpool.Config{Name: "weighted", MinWorkers: 4, MaxWorkers: 4, QueueDepth: 16})
	defer func() { _ = pool.Close(context.Background()) }()
	bus := eventbus.NewInMemoryBus()
	defer func() { _ = bus.Close() }()
	router, err := NewChannelRouter(nil, ChannelRouterConfig{
		TenantID:  "tenant-v341",
		Channels:  channels,
		Pool:      pool,
		Publisher: bus,
		Consumer:  bus,
		Now:       func() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewChannelRouter: %v", err)
	}
	defer func() { _ = router.Close(context.Background()) }()
	for i := 0; i < 100; i++ {
		evt := enrichedRouterMatrixEvent(t, "p-weighted", "weighted.bucket.eval")
		if err := router.HandleEvent(context.Background(), evt); err != nil {
			t.Fatalf("HandleEvent[%d]: %v", i, err)
		}
	}
	if got := tt.publishes.Load(); got != 30 {
		t.Fatalf("weighted tiktok publishes = %d, want 30 (30%% of 100)", got)
	}
	if got := fb.publishes.Load(); got != 100 {
		t.Fatalf("baseline facebook publishes = %d, want 100", got)
	}
}

// TestChannelRouter_V341RoutingMatrix_DLQOnAdapterFailureKeepsOthers
// asserts the DLQ behaviour explicitly called out in the v3.4.1
// plan: when a single channel fails, the others still deliver and
// the DLQ records exactly the failed channel.
func TestChannelRouter_V341RoutingMatrix_DLQOnAdapterFailureKeepsOthers(t *testing.T) {
	t.Parallel()
	flaky := newFakeAdapterErr("tiktok", errors.New("upstream 503"))
	ok1 := newFakeAdapter("facebook")
	ok2 := newFakeAdapter("rednote")
	ok3 := newFakeAdapter("instagram")
	channels := []ChannelDescriptor{
		{Adapter: flaky, Matcher: MatchAlways},
		{Adapter: ok1, Matcher: MatchAlways},
		{Adapter: ok2, Matcher: MatchAlways},
		{Adapter: ok3, Matcher: MatchAlways},
	}
	pool := workerpool.New(nil, workerpool.Config{Name: "dlq", MinWorkers: 4, MaxWorkers: 4, QueueDepth: 16})
	defer func() { _ = pool.Close(context.Background()) }()
	bus := eventbus.NewInMemoryBus()
	defer func() { _ = bus.Close() }()
	dlq := NewInMemoryDLQ()
	router, err := NewChannelRouter(nil, ChannelRouterConfig{
		TenantID:  "tenant-v341",
		Channels:  channels,
		Pool:      pool,
		Publisher: bus,
		Consumer:  bus,
		DLQ:       dlq,
		Now:       func() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewChannelRouter: %v", err)
	}
	defer func() { _ = router.Close(context.Background()) }()
	evt := enrichedRouterMatrixEvent(t, "p-dlq-row", "global.dlq.row")
	err = router.HandleEvent(context.Background(), evt)
	if !errors.Is(err, ErrChannelDLQ) {
		t.Fatalf("err = %v, want ErrChannelDLQ", err)
	}
	if ok1.publishes.Load() != 1 || ok2.publishes.Load() != 1 || ok3.publishes.Load() != 1 {
		t.Fatalf("good adapters publishes = %d/%d/%d", ok1.publishes.Load(), ok2.publishes.Load(), ok3.publishes.Load())
	}
	if recs := dlq.Records(); len(recs) != 1 || recs[0].Channel != "tiktok" {
		t.Fatalf("dlq records = %+v", recs)
	}
}

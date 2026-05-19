package channel

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/eventbus"
	"github.com/nfsarch33/helixon-ec/internal/workerpool"
)

// fakeChannelAdapter is the test double for ChannelAdapter. The
// Counter helper makes it easy to assert per-adapter call totals
// across the table.
type fakeChannelAdapter struct {
	name      string
	publishes atomic.Int32
	closes    atomic.Int32
	mu        sync.Mutex
	last      eventbus.ProductEnrichedPayload
	err       error
}

func newFakeAdapter(name string) *fakeChannelAdapter {
	return &fakeChannelAdapter{name: name}
}

func newFakeAdapterErr(name string, err error) *fakeChannelAdapter {
	return &fakeChannelAdapter{name: name, err: err}
}

func (f *fakeChannelAdapter) Name() string { return f.name }

func (f *fakeChannelAdapter) Publish(_ context.Context, p eventbus.ProductEnrichedPayload) error {
	f.publishes.Add(1)
	f.mu.Lock()
	f.last = p
	f.mu.Unlock()
	return f.err
}

func (f *fakeChannelAdapter) Close(_ context.Context) error {
	f.closes.Add(1)
	return nil
}

// recordingRouterMetrics captures dispatch + DLQ outcomes for
// assertions.
type recordingRouterMetrics struct {
	mu        sync.Mutex
	dispatch  []string
	dlq       []string
	dispChans map[string]string
}

func (r *recordingRouterMetrics) RecordDispatch(_, channel, outcome string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dispChans == nil {
		r.dispChans = map[string]string{}
	}
	r.dispatch = append(r.dispatch, channel+"="+outcome)
	r.dispChans[channel] = outcome
}

func (r *recordingRouterMetrics) RecordDLQ(_, channel, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dlq = append(r.dlq, channel+":"+reason)
}

// setupRouterHarness wires a router + workerpool + metrics + DLQ
// over the supplied channel descriptors. Returns the router and
// the recording aids the tests assert on.
type routerHarness struct {
	router  *ChannelRouter
	metrics *recordingRouterMetrics
	dlq     *InMemoryDLQ
	pool    *workerpool.Pool
	bus     *eventbus.InMemoryBus
}

func setupRouterHarness(t *testing.T, channels []ChannelDescriptor) *routerHarness {
	t.Helper()
	pool := workerpool.New(nil, workerpool.Config{Name: "router-test", MinWorkers: 4, MaxWorkers: 4, QueueDepth: 16})
	bus := eventbus.NewInMemoryBus()
	metrics := &recordingRouterMetrics{}
	dlq := NewInMemoryDLQ()
	router, err := NewChannelRouter(nil, ChannelRouterConfig{
		TenantID:        "tenant-1",
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
		_ = pool.Close(ctx)
		cancel()
	})
	return &routerHarness{router: router, metrics: metrics, dlq: dlq, pool: pool, bus: bus}
}

// matchByCategoryPrefix is a test-only matcher factory used by the
// table-driven RED test. Real production matchers use closures over
// product tags supplied by the operator at the composition root.
func matchByCategoryPrefix(prefix string) ChannelMatcher {
	return func(p eventbus.ProductEnrichedPayload) bool {
		return strings.HasPrefix(p.CategoryID, prefix)
	}
}

func enrichedRouterEvent(t *testing.T, productID, categoryID string) eventbus.Event {
	t.Helper()
	payload := eventbus.ProductEnrichedPayload{
		TenantID:           "tenant-1",
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

// TestChannelRouter_RoutesToCorrectAdapterByProductTag is the
// EC-4-3 RED acceptance test. Table-driven over 6 channel
// combinations defined by the v3.4.0 plan: cn-diaspora,
// au-domestic, global, fashion, electronics, no-match.
func TestChannelRouter_RoutesToCorrectAdapterByProductTag(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name             string
		categoryID       string
		expectChannels   []string // adapters that should receive the publish
		expectNoMatchErr bool
	}{
		{
			name:           "cn_diaspora_routes_to_rednote_only",
			categoryID:     "cn-diaspora.lifestyle.beauty",
			expectChannels: []string{"rednote"},
		},
		{
			name:           "au_domestic_routes_to_tiktok_and_facebook",
			categoryID:     "au-domestic.electronics.audio",
			expectChannels: []string{"tiktok", "facebook"},
		},
		{
			name:           "global_routes_to_all_four",
			categoryID:     "global.lifestyle.fashion",
			expectChannels: []string{"tiktok", "facebook", "rednote", "instagram"},
		},
		{
			name:           "fashion_routes_to_instagram_and_tiktok",
			categoryID:     "lifestyle.fashion.shoes",
			expectChannels: []string{"instagram", "tiktok"},
		},
		{
			name:           "electronics_routes_to_facebook_and_tiktok",
			categoryID:     "electronics.audio.headphones",
			expectChannels: []string{"facebook", "tiktok"},
		},
		{
			name:             "no_matching_tag_dispatches_no_one",
			categoryID:       "unmappable.foo.bar",
			expectNoMatchErr: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Sub-tests run sequentially so per-adapter counters
			// are clean for each row.
			tt := newFakeAdapter("tiktok")
			fb := newFakeAdapter("facebook")
			rn := newFakeAdapter("rednote")
			ig := newFakeAdapter("instagram")
			channels := []ChannelDescriptor{
				{Adapter: tt, Matcher: func(p eventbus.ProductEnrichedPayload) bool {
					return strings.Contains(p.CategoryID, "au-domestic") ||
						strings.HasPrefix(p.CategoryID, "global.") ||
						strings.HasPrefix(p.CategoryID, "lifestyle.fashion") ||
						strings.HasPrefix(p.CategoryID, "electronics.")
				}},
				{Adapter: fb, Matcher: func(p eventbus.ProductEnrichedPayload) bool {
					return strings.Contains(p.CategoryID, "au-domestic") ||
						strings.HasPrefix(p.CategoryID, "global.") ||
						strings.HasPrefix(p.CategoryID, "electronics.")
				}},
				{Adapter: rn, Matcher: func(p eventbus.ProductEnrichedPayload) bool {
					return strings.HasPrefix(p.CategoryID, "cn-diaspora.") ||
						strings.HasPrefix(p.CategoryID, "global.")
				}},
				{Adapter: ig, Matcher: func(p eventbus.ProductEnrichedPayload) bool {
					return strings.HasPrefix(p.CategoryID, "global.") ||
						strings.HasPrefix(p.CategoryID, "lifestyle.fashion")
				}},
			}
			h := setupRouterHarness(t, channels)
			err := h.router.HandleEvent(context.Background(), enrichedRouterEvent(t, "p-"+tc.name, tc.categoryID))
			if tc.expectNoMatchErr {
				if !errors.Is(err, ErrNoMatchingChannel) {
					t.Fatalf("err = %v, want ErrNoMatchingChannel", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("HandleEvent: %v", err)
			}
			gotChannels := []string{}
			for name, adapter := range map[string]*fakeChannelAdapter{"tiktok": tt, "facebook": fb, "rednote": rn, "instagram": ig} {
				if adapter.publishes.Load() > 0 {
					gotChannels = append(gotChannels, name)
				}
			}
			if !sameStringSet(gotChannels, tc.expectChannels) {
				t.Fatalf("dispatched to %v, want %v", gotChannels, tc.expectChannels)
			}
		})
	}
}

func TestChannelRouter_AdapterFailureGoesToDLQ(t *testing.T) {
	t.Parallel()
	failing := newFakeAdapterErr("flaky", errors.New("upstream 500"))
	ok := newFakeAdapter("good")
	h := setupRouterHarness(t, []ChannelDescriptor{
		{Adapter: failing, Matcher: MatchAlways},
		{Adapter: ok, Matcher: MatchAlways},
	})
	err := h.router.HandleEvent(context.Background(), enrichedRouterEvent(t, "p-failover", "any.cat"))
	if !errors.Is(err, ErrChannelDLQ) {
		t.Fatalf("err = %v, want ErrChannelDLQ", err)
	}
	if ok.publishes.Load() != 1 {
		t.Fatalf("good adapter publishes = %d", ok.publishes.Load())
	}
	if failing.publishes.Load() != 1 {
		t.Fatalf("flaky adapter publishes = %d", failing.publishes.Load())
	}
	recs := h.dlq.Records()
	if len(recs) != 1 || recs[0].Channel != "flaky" || recs[0].ProductID != "p-failover" {
		t.Fatalf("dlq records = %+v", recs)
	}
	if !containsString(h.metrics.dlq, "flaky:publish_failed") {
		t.Fatalf("metrics dlq = %v, expected flaky:publish_failed", h.metrics.dlq)
	}
}

func TestChannelRouter_TenantMismatchReturnsSentinel(t *testing.T) {
	t.Parallel()
	h := setupRouterHarness(t, []ChannelDescriptor{{Adapter: newFakeAdapter("x"), Matcher: MatchAlways}})
	payload := eventbus.ProductEnrichedPayload{
		TenantID:     "tenant-other",
		ProductID:    "p",
		EnglishTitle: "X",
		PriceCents:   100,
	}
	evt, err := eventbus.NewProductEnrichedEvent("src", time.Now(), payload)
	if err != nil {
		t.Fatalf("NewProductEnrichedEvent: %v", err)
	}
	err = h.router.HandleEvent(context.Background(), evt)
	if !errors.Is(err, ErrChannelTenantMismatch) {
		t.Fatalf("err = %v, want ErrChannelTenantMismatch", err)
	}
}

func TestChannelRouter_RejectsAfterClose(t *testing.T) {
	t.Parallel()
	h := setupRouterHarness(t, []ChannelDescriptor{{Adapter: newFakeAdapter("x"), Matcher: MatchAlways}})
	_ = h.router.Close(context.Background())
	err := h.router.HandleEvent(context.Background(), enrichedRouterEvent(t, "p", "any"))
	if !errors.Is(err, ErrRouterClosed) {
		t.Fatalf("err = %v, want ErrRouterClosed", err)
	}
}

func TestChannelRouter_StartSubscribes(t *testing.T) {
	t.Parallel()
	adapter := newFakeAdapter("tiktok")
	h := setupRouterHarness(t, []ChannelDescriptor{{Adapter: adapter, Matcher: MatchAlways}})
	if err := h.router.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	evt := enrichedRouterEvent(t, "p-publish", "any.cat")
	if err := h.bus.Publish(context.Background(), evt); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if adapter.publishes.Load() != 1 {
		t.Fatalf("publishes = %d", adapter.publishes.Load())
	}
}

func TestChannelRouter_StartRequiresConsumer(t *testing.T) {
	t.Parallel()
	pool := workerpool.New(nil, workerpool.Config{Name: "router-noconsumer", MaxWorkers: 1, QueueDepth: 1})
	defer func() { _ = pool.Close(context.Background()) }()
	bus := eventbus.NewInMemoryBus()
	defer func() { _ = bus.Close() }()
	router, err := NewChannelRouter(nil, ChannelRouterConfig{
		TenantID:  "tenant-1",
		Channels:  []ChannelDescriptor{{Adapter: newFakeAdapter("x"), Matcher: MatchAlways}},
		Pool:      pool,
		Publisher: bus,
	})
	if err != nil {
		t.Fatalf("NewChannelRouter: %v", err)
	}
	t.Cleanup(func() { _ = router.Close(context.Background()) })
	if err := router.Start(context.Background()); !errors.Is(err, ErrRouterUnconfigured) {
		t.Fatalf("err = %v", err)
	}
}

func TestNewChannelRouter_ConfigValidation(t *testing.T) {
	t.Parallel()
	pool := workerpool.New(nil, workerpool.Config{Name: "validation", MaxWorkers: 1, QueueDepth: 1})
	defer func() { _ = pool.Close(context.Background()) }()
	bus := eventbus.NewInMemoryBus()
	defer func() { _ = bus.Close() }()
	cases := map[string]ChannelRouterConfig{
		"missing tenant":    {Pool: pool, Publisher: bus, Channels: []ChannelDescriptor{{Adapter: newFakeAdapter("x"), Matcher: MatchAlways}}},
		"missing pool":      {TenantID: "t", Publisher: bus, Channels: []ChannelDescriptor{{Adapter: newFakeAdapter("x"), Matcher: MatchAlways}}},
		"missing publisher": {TenantID: "t", Pool: pool, Channels: []ChannelDescriptor{{Adapter: newFakeAdapter("x"), Matcher: MatchAlways}}},
		"missing channels":  {TenantID: "t", Pool: pool, Publisher: bus},
	}
	for name, cfg := range cases {
		name, cfg := name, cfg
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := NewChannelRouter(nil, cfg)
			if !errors.Is(err, ErrRouterUnconfigured) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestChannelRouter_NoMatchEmitsMetric(t *testing.T) {
	t.Parallel()
	h := setupRouterHarness(t, []ChannelDescriptor{{Adapter: newFakeAdapter("never"), Matcher: MatchNever}})
	err := h.router.HandleEvent(context.Background(), enrichedRouterEvent(t, "p-nomatch", "any.cat"))
	if !errors.Is(err, ErrNoMatchingChannel) {
		t.Fatalf("err = %v, want ErrNoMatchingChannel", err)
	}
	if !containsString(h.metrics.dispatch, "(none)=no_match") {
		t.Fatalf("metrics = %v", h.metrics.dispatch)
	}
}

func TestChannelRouter_DecodeRouterEnriched_RejectsWrongType(t *testing.T) {
	t.Parallel()
	_, err := decodeRouterEnriched(eventbus.Event{Type: eventbus.OrderPlaced})
	if !errors.Is(err, ErrChannelEnvelopeInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestInMemoryDLQ_EnqueueWraparound(t *testing.T) {
	t.Parallel()
	q := NewInMemoryDLQWithCapacity(3)
	for i := 0; i < 5; i++ {
		_ = q.Enqueue(context.Background(), DLQRecord{ProductID: string(rune('a' + i))})
	}
	if got := len(q.Records()); got != 3 {
		t.Fatalf("records len = %d", got)
	}
	if got := q.Dropped(); got != 2 {
		t.Fatalf("dropped = %d", got)
	}
}

func TestInMemoryDLQ_DefaultCapacity(t *testing.T) {
	t.Parallel()
	q := NewInMemoryDLQWithCapacity(0)
	if q.capacity != 1024 {
		t.Fatalf("capacity = %d", q.capacity)
	}
}

func TestMatchAlwaysAndMatchNever(t *testing.T) {
	t.Parallel()
	p := eventbus.ProductEnrichedPayload{}
	if !MatchAlways(p) {
		t.Fatal("MatchAlways false")
	}
	if MatchNever(p) {
		t.Fatal("MatchNever true")
	}
}

func TestChannelRouter_NilAdapterOrMatcherSkipped(t *testing.T) {
	t.Parallel()
	good := newFakeAdapter("good")
	channels := []ChannelDescriptor{
		{Adapter: nil, Matcher: MatchAlways},
		{Adapter: good, Matcher: nil},
		{Adapter: good, Matcher: MatchAlways},
	}
	h := setupRouterHarness(t, channels)
	if err := h.router.HandleEvent(context.Background(), enrichedRouterEvent(t, "p-nils", "x.y")); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if good.publishes.Load() != 1 {
		t.Fatalf("good publishes = %d", good.publishes.Load())
	}
}

// Use the tagger-style matcher to prove the public ChannelMatcher
// type plays well with operator-supplied tag closures.
func TestChannelRouter_MatchByCategoryPrefixHelper(t *testing.T) {
	t.Parallel()
	m := matchByCategoryPrefix("global.")
	if !m(eventbus.ProductEnrichedPayload{CategoryID: "global.lifestyle"}) {
		t.Fatal("expected match")
	}
	if m(eventbus.ProductEnrichedPayload{CategoryID: "regional"}) {
		t.Fatal("unexpected match")
	}
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[string]int, len(a))
	for _, s := range a {
		m[s]++
	}
	for _, s := range b {
		m[s]--
	}
	for _, v := range m {
		if v != 0 {
			return false
		}
	}
	return true
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

package sourcing

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/china"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
	"github.com/nfsarch33/agentic-ecommerce/internal/metrics"
	"github.com/nfsarch33/agentic-ecommerce/internal/workerpool"
)

// TestSourcingAgent_SelectsHighMarginProduct is the EC-1-3 RED test
// driving the China sourcing agent against a synthetic catalogue.
//
// The synthetic catalogue contains 5 products with varying margin,
// supplier-quality, and category. The high-margin product must rank
// first and be emitted as a SelectedProducts entry. A vape product
// must be filtered by the compliance gate (TikTok prohibited).
func TestSourcingAgent_SelectsHighMarginProduct(t *testing.T) {
	t.Parallel()

	pool := workerpool.New(nil, workerpool.Config{
		Name:       "sourcing-test",
		MinWorkers: 2,
		MaxWorkers: 4,
		QueueDepth: 8,
	})
	defer func() { _ = pool.Close(context.Background()) }()

	bus := eventbus.NewInMemoryBus()
	defer func() { _ = bus.Close() }()

	syntheticCatalogue := []china.Product{
		{ // ranked-first: short lead time, gold supplier, low cost
			ExternalID: "earbuds-A", Source: china.Source1688, Title: "Premium Earbuds A",
			Category: "electronics", PriceCNYCents: 1500, MOQ: 20, LeadTimeDays: 7,
			SupplierID: "sup-A", SupplierName: "Supplier A", SupplierRating: 4.7,
			SupplierVerified: true, ReviewCount: 200, MonthlySalesUnit: 500,
			URL: "https://detail.1688.com/offer/A.html",
		},
		{ // mid-tier
			ExternalID: "speaker-B", Source: china.Source1688, Title: "Bluetooth Speaker B",
			Category: "electronics", PriceCNYCents: 3000, MOQ: 30, LeadTimeDays: 14,
			SupplierID: "sup-B", SupplierName: "Supplier B", SupplierRating: 4.0,
			SupplierVerified: false, ReviewCount: 80,
			URL: "https://detail.1688.com/offer/B.html",
		},
		{ // weak supplier, should drop below floor
			ExternalID: "phone-C", Source: china.Source1688, Title: "Phone Case C",
			Category: "accessories", PriceCNYCents: 500, MOQ: 1000, LeadTimeDays: 90,
			SupplierID: "sup-C", SupplierName: "Supplier C", SupplierRating: 2.5,
			SupplierVerified: false, ReviewCount: 5,
			URL: "https://detail.1688.com/offer/C.html",
		},
		{ // compliance reject: vape on TikTok
			ExternalID: "vape-D", Source: china.Source1688, Title: "Vape Mod D",
			Category: "vape", PriceCNYCents: 2000, MOQ: 10, LeadTimeDays: 5,
			SupplierID: "sup-D", SupplierName: "Supplier D", SupplierRating: 4.5,
			SupplierVerified: true, ReviewCount: 100,
			URL: "https://detail.1688.com/offer/D.html",
		},
		{ // ok-tier
			ExternalID: "kitchen-E", Source: china.SourceTaobao, Title: "Kitchen Blender",
			Category: "kitchen", PriceCNYCents: 4500, MOQ: 25, LeadTimeDays: 12,
			SupplierID: "sup-E", SupplierName: "Supplier E", SupplierRating: 4.3,
			SupplierVerified: true, ReviewCount: 150,
			URL: "https://detail.taobao.com/E.htm",
		},
	}

	fakeClient1688 := &fakeClient{src: china.Source1688, products: filterBySource(syntheticCatalogue, china.Source1688)}
	fakeClientTaobao := &fakeClient{src: china.SourceTaobao, products: filterBySource(syntheticCatalogue, china.SourceTaobao)}

	registry := metrics.NewRegistry("agent-test")
	agent, err := NewChinaSourcingAgent(nil, ChinaSourcingConfig{
		Clients:             []china.Client{fakeClient1688, fakeClientTaobao},
		Pool:                pool,
		Publisher:           bus,
		MetricsRegistry:     registry,
		OmniParserBridgeURL: "http://test-bridge.local:8080",
		TenantID:            "cylrl",
		MaxResults:          5,
		Now:                 staticNow(time.Date(2026, 5, 9, 21, 0, 0, 0, time.UTC)),
	})
	if err != nil {
		t.Fatalf("NewChinaSourcingAgent: %v", err)
	}
	defer func() { _ = agent.Close(context.Background()) }()

	res, err := agent.Run(context.Background(), SourcingRequest{Keyword: "earbuds", MaxResults: 3})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.SelectedProducts) == 0 {
		t.Fatalf("no products selected; rejected=%v", res.RejectedReasons)
	}
	top := res.SelectedProducts[0]
	if top.ExternalID != "earbuds-A" {
		t.Fatalf("top product = %q, want earbuds-A; selected=%v", top.ExternalID, externalIDs(res.SelectedProducts))
	}
	if got := res.CompositeScores["earbuds-A"]; got <= 0.5 {
		t.Fatalf("earbuds-A composite = %v, want > 0.5", got)
	}
	// Compliance gate must have rejected the vape.
	if rejected := res.RejectedReasons["vape"]; rejected == 0 {
		t.Fatalf("expected vape to be rejected; rejected=%v", res.RejectedReasons)
	}
	// Bus must have received the proposal event.
	delivered := bus.Delivered()
	if len(delivered) == 0 {
		t.Fatal("eventbus received zero events; agent should emit ProductSourcingProposed")
	}
	evt := delivered[len(delivered)-1]
	if evt.Type != eventbus.ProductSourcingProposed {
		t.Fatalf("event.Type = %q, want %q", evt.Type, eventbus.ProductSourcingProposed)
	}
	if evt.TenantID != "cylrl" {
		t.Fatalf("event.TenantID = %q, want cylrl", evt.TenantID)
	}
}

func TestNewChinaSourcingAgent_RejectsMissingDependencies(t *testing.T) {
	t.Parallel()

	pool := workerpool.New(nil, workerpool.Config{Name: "x", MinWorkers: 1, MaxWorkers: 1, QueueDepth: 1})
	defer func() { _ = pool.Close(context.Background()) }()
	bus := eventbus.NewInMemoryBus()
	defer func() { _ = bus.Close() }()
	stub := &fakeClient{src: china.Source1688}

	cases := []struct {
		name string
		mut  func(c *ChinaSourcingConfig)
	}{
		{name: "no clients", mut: func(c *ChinaSourcingConfig) { c.Clients = nil }},
		{name: "no pool", mut: func(c *ChinaSourcingConfig) { c.Pool = nil }},
		{name: "no publisher", mut: func(c *ChinaSourcingConfig) { c.Publisher = nil }},
		{name: "no tenant", mut: func(c *ChinaSourcingConfig) { c.TenantID = " " }},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := ChinaSourcingConfig{
				Clients:             []china.Client{stub},
				Pool:                pool,
				Publisher:           bus,
				OmniParserBridgeURL: "http://x",
				TenantID:            "cylrl",
			}
			tc.mut(&cfg)
			_, err := NewChinaSourcingAgent(nil, cfg)
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, ErrSourcingUnconfigured) {
				t.Fatalf("error not wrapping ErrSourcingUnconfigured: %v", err)
			}
		})
	}
}

func TestNewChinaSourcingAgent_RejectsUnsetOmniParserBridge(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv per the testing contract.
	t.Setenv(OmniParserBridgeEnvVar, "")

	pool := workerpool.New(nil, workerpool.Config{Name: "x", MinWorkers: 1, MaxWorkers: 1, QueueDepth: 1})
	defer func() { _ = pool.Close(context.Background()) }()
	bus := eventbus.NewInMemoryBus()
	defer func() { _ = bus.Close() }()
	stub := &fakeClient{src: china.Source1688}

	_, err := NewChinaSourcingAgent(nil, ChinaSourcingConfig{
		Clients:   []china.Client{stub},
		Pool:      pool,
		Publisher: bus,
		TenantID:  "cylrl",
		// OmniParserBridgeURL deliberately empty; env var also empty.
	})
	if err == nil {
		t.Fatal("expected error when OMNIPARSER_BRIDGE_URL unset")
	}
	if !errors.Is(err, ErrOmniParserUnconfigured) {
		t.Fatalf("error not wrapping ErrOmniParserUnconfigured: %v", err)
	}
}

func TestNewChinaSourcingAgent_AcceptsBridgeFromEnv(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv per the testing contract.
	t.Setenv(OmniParserBridgeEnvVar, "http://omniparser-bridge.local:9000")

	pool := workerpool.New(nil, workerpool.Config{Name: "x", MinWorkers: 1, MaxWorkers: 1, QueueDepth: 1})
	defer func() { _ = pool.Close(context.Background()) }()
	bus := eventbus.NewInMemoryBus()
	defer func() { _ = bus.Close() }()
	stub := &fakeClient{src: china.Source1688}

	agent, err := NewChinaSourcingAgent(nil, ChinaSourcingConfig{
		Clients:   []china.Client{stub},
		Pool:      pool,
		Publisher: bus,
		TenantID:  "cylrl",
	})
	if err != nil {
		t.Fatalf("expected agent to construct from env var: %v", err)
	}
	if got := agent.OmniParserBridgeURL(); got != "http://omniparser-bridge.local:9000" {
		t.Fatalf("OmniParserBridgeURL = %q", got)
	}
}

func TestSourcingAgent_RunRejectsEmptyKeyword(t *testing.T) {
	t.Parallel()

	agent := mustAgent(t)
	defer func() { _ = agent.Close(context.Background()) }()

	_, err := agent.Run(context.Background(), SourcingRequest{Keyword: " "})
	if !errors.Is(err, ErrSourcingEmptyKeyword) {
		t.Fatalf("error = %v, want ErrSourcingEmptyKeyword", err)
	}
}

func TestSourcingAgent_RunAfterCloseFails(t *testing.T) {
	t.Parallel()

	agent := mustAgent(t)
	if err := agent.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
	_, err := agent.Run(context.Background(), SourcingRequest{Keyword: "x"})
	if !errors.Is(err, ErrSourcingClosed) {
		t.Fatalf("error = %v, want ErrSourcingClosed", err)
	}
}

func TestSourcingAgent_RunReturnsEmptyResultsErrorWhenAllFiltered(t *testing.T) {
	t.Parallel()

	pool := workerpool.New(nil, workerpool.Config{Name: "x", MinWorkers: 1, MaxWorkers: 2, QueueDepth: 4})
	defer func() { _ = pool.Close(context.Background()) }()
	bus := eventbus.NewInMemoryBus()
	defer func() { _ = bus.Close() }()
	// Catalogue with only restricted products.
	stub := &fakeClient{src: china.Source1688, products: []china.Product{
		{ExternalID: "v1", Source: china.Source1688, Category: "vape", PriceCNYCents: 100, MOQ: 1, SupplierID: "sup-x", SupplierRating: 4.5},
		{ExternalID: "g1", Source: china.Source1688, Category: "gambling", PriceCNYCents: 100, MOQ: 1, SupplierID: "sup-y", SupplierRating: 4.5},
	}}
	agent, err := NewChinaSourcingAgent(nil, ChinaSourcingConfig{
		Clients:             []china.Client{stub},
		Pool:                pool,
		Publisher:           bus,
		OmniParserBridgeURL: "http://x",
		TenantID:            "cylrl",
	})
	if err != nil {
		t.Fatalf("NewChinaSourcingAgent: %v", err)
	}
	defer func() { _ = agent.Close(context.Background()) }()

	_, err = agent.Run(context.Background(), SourcingRequest{Keyword: "x"})
	if !errors.Is(err, ErrSourcingEmptyResults) {
		t.Fatalf("error = %v, want ErrSourcingEmptyResults", err)
	}
}

func TestSourcingAgent_TrendStoreInfluencesRanking(t *testing.T) {
	t.Parallel()

	pool := workerpool.New(nil, workerpool.Config{Name: "x", MinWorkers: 1, MaxWorkers: 2, QueueDepth: 4})
	defer func() { _ = pool.Close(context.Background()) }()
	bus := eventbus.NewInMemoryBus()
	defer func() { _ = bus.Close() }()
	products := []china.Product{
		// Equal supplier scores; trend store will boost product B.
		{ExternalID: "A", Source: china.Source1688, Category: "electronics", Title: "Aaa",
			PriceCNYCents: 1000, MOQ: 10, LeadTimeDays: 7, SupplierID: "sa", SupplierRating: 4.5, SupplierVerified: true, ReviewCount: 100},
		{ExternalID: "B", Source: china.Source1688, Category: "electronics", Title: "Bbb",
			PriceCNYCents: 1000, MOQ: 10, LeadTimeDays: 7, SupplierID: "sb", SupplierRating: 4.5, SupplierVerified: true, ReviewCount: 100},
	}
	trend := &fakeTrendSignaler{
		responses: map[string]float64{"Aaa": 0.1, "Bbb": 0.95},
	}
	agent, err := NewChinaSourcingAgent(nil, ChinaSourcingConfig{
		Clients:             []china.Client{&fakeClient{src: china.Source1688, products: products}},
		Pool:                pool,
		Publisher:           bus,
		TrendStore:          trend,
		OmniParserBridgeURL: "http://x",
		TenantID:            "cylrl",
		MaxResults:          5,
	})
	if err != nil {
		t.Fatalf("NewChinaSourcingAgent: %v", err)
	}
	defer func() { _ = agent.Close(context.Background()) }()

	res, err := agent.Run(context.Background(), SourcingRequest{Keyword: "x", MaxResults: 2})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.SelectedProducts) < 2 {
		t.Fatalf("selected = %d", len(res.SelectedProducts))
	}
	if res.SelectedProducts[0].ExternalID != "B" {
		t.Fatalf("trend signal failed to bias ranking; got top=%q want B", res.SelectedProducts[0].ExternalID)
	}
}

func TestNormaliseCategoryKeyEmptyDefaultsToUnknown(t *testing.T) {
	t.Parallel()

	if got := normaliseCategoryKey(""); got != "unknown" {
		t.Fatalf("got %q, want unknown", got)
	}
	if got := normaliseCategoryKey(" Electronics "); got != "electronics" {
		t.Fatalf("got %q, want electronics", got)
	}
}

func TestComputeMarginScoreHandlesEdgeCases(t *testing.T) {
	t.Parallel()

	if got := computeMarginScore(100, 0); got != 0 {
		t.Fatalf("zero sell price: got %v, want 0", got)
	}
	if got := computeMarginScore(50, 100); got != 0.5 {
		t.Fatalf("normal: got %v, want 0.5", got)
	}
	if got := computeMarginScore(150, 100); got != 0 {
		t.Fatalf("negative margin clamped: got %v, want 0", got)
	}
}

func TestExpectedSellPriceCNYIsTwoAndHalfX(t *testing.T) {
	t.Parallel()

	if got := expectedSellPriceCNY(100); got != 250 {
		t.Fatalf("got %d, want 250", got)
	}
}

func TestRankCompositeWeightsSumToOne(t *testing.T) {
	t.Parallel()

	if got := rankComposite(1, 1, 1); got != 1.0 {
		t.Fatalf("max composite = %v, want 1.0", got)
	}
	if got := rankComposite(0, 0, 0); got != 0 {
		t.Fatalf("min composite = %v, want 0", got)
	}
}

// --- helpers ----------------------------------------------------------------

func mustAgent(t *testing.T) *ChinaSourcingAgent {
	t.Helper()
	pool := workerpool.New(nil, workerpool.Config{Name: "x", MinWorkers: 1, MaxWorkers: 1, QueueDepth: 1})
	t.Cleanup(func() { _ = pool.Close(context.Background()) })
	bus := eventbus.NewInMemoryBus()
	t.Cleanup(func() { _ = bus.Close() })
	stub := &fakeClient{src: china.Source1688}
	agent, err := NewChinaSourcingAgent(nil, ChinaSourcingConfig{
		Clients:             []china.Client{stub},
		Pool:                pool,
		Publisher:           bus,
		OmniParserBridgeURL: "http://test-bridge",
		TenantID:            "cylrl",
	})
	if err != nil {
		t.Fatalf("NewChinaSourcingAgent: %v", err)
	}
	return agent
}

func staticNow(t time.Time) func() time.Time { return func() time.Time { return t } }

func filterBySource(in []china.Product, src china.Source) []china.Product {
	out := make([]china.Product, 0, len(in))
	for _, p := range in {
		if p.Source == src {
			out = append(out, p)
		}
	}
	return out
}

func externalIDs(in []china.Product) []string {
	out := make([]string, 0, len(in))
	for _, p := range in {
		out = append(out, p.ExternalID)
	}
	return out
}

// fakeClient is the in-memory Client implementation used by tests.
type fakeClient struct {
	src      china.Source
	products []china.Product
	calls    sync.Mutex
	hits     int
}

func (f *fakeClient) Source() china.Source { return f.src }

func (f *fakeClient) Search(_ context.Context, _ china.SearchRequest) ([]china.Product, error) {
	f.calls.Lock()
	defer f.calls.Unlock()
	f.hits++
	out := make([]china.Product, len(f.products))
	copy(out, f.products)
	return out, nil
}

func (f *fakeClient) ProductDetail(_ context.Context, req china.ProductDetailRequest) (china.Product, error) {
	for _, p := range f.products {
		if p.ExternalID == req.ExternalID {
			return p, nil
		}
	}
	return china.Product{}, errors.New("not found")
}

func (f *fakeClient) Close(_ context.Context) error { return nil }

// fakeTrendSignaler mimics a pgvector-backed RAG trend score lookup.
type fakeTrendSignaler struct {
	responses map[string]float64
}

func (f *fakeTrendSignaler) TrendScore(_ context.Context, _, _, productTitle string) (float64, error) {
	if v, ok := f.responses[productTitle]; ok {
		return v, nil
	}
	return 0, nil
}

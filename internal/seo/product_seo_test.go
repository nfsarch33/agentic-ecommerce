package seo

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestSEO_InjectsLongTailKeywordsFromTrendData is the v3.2.0 EC-2-3
// RED test. It exercises the ProductSEO injector against a fake
// trend store and a fake WooCommerce importer:
//
//  1. The injector pulls trending keywords for the tenant + topic
//     from the TrendKeywordSource port (satisfied by EC-2-4
//     rag.TrendIngestor + a small adapter at the cmd/* layer).
//  2. The keywords land in the suggestion's title + meta + tags.
//  3. The importer is called once with the enriched suggestion.
//  4. A second Inject call with identical inputs is idempotent --
//     the importer reports zero duplicate creates.
func TestSEO_InjectsLongTailKeywordsFromTrendData(t *testing.T) {
	t.Parallel()

	trends := &fakeTrendKeywordSource{
		responses: map[string][]string{
			"earbuds:cylrl": {"wireless earbuds 2026", "noise cancelling earbuds", "long battery earbuds"},
		},
	}
	importer := &fakeImporter{}
	injector, err := NewProductSEO(nil, ProductSEOConfig{
		Trends:   trends,
		Importer: importer,
		TenantID: "cylrl",
		Now:      func() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewProductSEO: %v", err)
	}
	t.Cleanup(func() { _ = injector.Close(context.Background()) })

	req := SEOInjectRequest{
		Product: SEOProduct{
			ID:          "earbuds-001",
			Title:       "Premium Wireless Earbuds",
			Description: "Crisp sound and 36-hour battery life.",
			Topic:       "earbuds",
			Categories:  []string{"electronics"},
		},
	}
	res, err := injector.Inject(context.Background(), req)
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	low := strings.ToLower(res.Suggestion.Title + " " + res.Suggestion.MetaDescription)
	if !strings.Contains(low, "wireless earbuds") {
		t.Fatalf("title/meta missing trending keyword: %q + %q", res.Suggestion.Title, res.Suggestion.MetaDescription)
	}
	if !res.UsedTrendData {
		t.Fatalf("UsedTrendData = false; expected true when trend store returns hits")
	}
	if importer.Calls != 1 {
		t.Fatalf("importer called %d times, want 1", importer.Calls)
	}

	// Idempotent re-run: same inputs -> same key -> importer treats as
	// upsert (zero new SKUs).
	importer.NewSKUs = 0
	if _, err := injector.Inject(context.Background(), req); err != nil {
		t.Fatalf("Inject (idempotent): %v", err)
	}
	if importer.NewSKUs != 0 {
		t.Fatalf("idempotent re-run created %d new SKUs, want 0", importer.NewSKUs)
	}
	if importer.Calls != 2 {
		t.Fatalf("importer Calls = %d, want 2 (call cardinal stays even on idempotent re-import)", importer.Calls)
	}
}

func TestSEO_FallsBackWhenTrendStoreReturnsNoHits(t *testing.T) {
	t.Parallel()

	trends := &fakeTrendKeywordSource{} // empty -> ErrSEONoTrendData path
	importer := &fakeImporter{}
	injector, err := NewProductSEO(nil, ProductSEOConfig{
		Trends:   trends,
		Importer: importer,
		TenantID: "cylrl",
	})
	if err != nil {
		t.Fatalf("NewProductSEO: %v", err)
	}
	t.Cleanup(func() { _ = injector.Close(context.Background()) })

	res, err := injector.Inject(context.Background(), SEOInjectRequest{
		Product: SEOProduct{ID: "x", Title: "Plain Title", Description: "Plain description.", Topic: "earbuds"},
	})
	if err != nil {
		t.Fatalf("Inject (no trends): %v", err)
	}
	if res.UsedTrendData {
		t.Fatalf("UsedTrendData = true; expected false when trend store empty")
	}
	if importer.Calls != 1 {
		t.Fatalf("importer called %d times, want 1 (fallback path still imports)", importer.Calls)
	}
}

func TestSEO_TrendStoreErrorWrapsErrSEONoTrendData(t *testing.T) {
	t.Parallel()

	trends := &fakeTrendKeywordSource{err: errors.New("rag down")}
	importer := &fakeImporter{}
	injector, err := NewProductSEO(nil, ProductSEOConfig{
		Trends:       trends,
		Importer:     importer,
		TenantID:     "cylrl",
		StrictTrends: true, // strict mode: surface the error
	})
	if err != nil {
		t.Fatalf("NewProductSEO: %v", err)
	}
	t.Cleanup(func() { _ = injector.Close(context.Background()) })

	_, err = injector.Inject(context.Background(), SEOInjectRequest{
		Product: SEOProduct{ID: "x", Title: "x", Description: "x", Topic: "earbuds"},
	})
	if !errors.Is(err, ErrSEONoTrendData) {
		t.Fatalf("error = %v, want ErrSEONoTrendData", err)
	}
	if importer.Calls != 0 {
		t.Fatalf("importer called %d times under strict trend failure; want 0", importer.Calls)
	}
}

func TestNewProductSEO_RejectsMissingDependencies(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		mut  func(c *ProductSEOConfig)
	}{
		{name: "no trends", mut: func(c *ProductSEOConfig) { c.Trends = nil }},
		{name: "no importer", mut: func(c *ProductSEOConfig) { c.Importer = nil }},
		{name: "no tenant", mut: func(c *ProductSEOConfig) { c.TenantID = " " }},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := ProductSEOConfig{
				Trends:   &fakeTrendKeywordSource{},
				Importer: &fakeImporter{},
				TenantID: "cylrl",
			}
			tc.mut(&cfg)
			_, err := NewProductSEO(nil, cfg)
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, ErrSEOUnconfigured) {
				t.Fatalf("error not wrapping ErrSEOUnconfigured: %v", err)
			}
		})
	}
}

func TestSEO_InjectAfterCloseReturnsClosedError(t *testing.T) {
	t.Parallel()

	injector, err := NewProductSEO(nil, ProductSEOConfig{
		Trends:   &fakeTrendKeywordSource{},
		Importer: &fakeImporter{},
		TenantID: "cylrl",
	})
	if err != nil {
		t.Fatalf("NewProductSEO: %v", err)
	}
	if err := injector.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err = injector.Inject(context.Background(), SEOInjectRequest{Product: SEOProduct{ID: "x", Title: "x"}})
	if !errors.Is(err, ErrSEOClosed) {
		t.Fatalf("error = %v, want ErrSEOClosed", err)
	}
}

// fakeTrendKeywordSource models the EC-2-4-fed trend store from the
// SEO injector's perspective. The composition root in
// cmd/agent-worker wires a real adapter that calls the
// rag.TrendIngestor.
type fakeTrendKeywordSource struct {
	responses map[string][]string
	err       error
	mu        sync.Mutex
	calls     int
}

func (f *fakeTrendKeywordSource) TrendingKeywords(_ context.Context, tenantID, topic string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	key := topic + ":" + tenantID
	out := f.responses[key]
	dup := make([]string, len(out))
	copy(dup, out)
	return dup, nil
}

// fakeImporter models the WooCommerce idempotent importer.
type fakeImporter struct {
	mu      sync.Mutex
	Calls   int
	NewSKUs int
	seen    map[string]struct{}
}

func (f *fakeImporter) Upsert(_ context.Context, req CatalogueUpsertRequest) (CatalogueUpsertResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.seen == nil {
		f.seen = make(map[string]struct{})
	}
	f.Calls++
	created := false
	if _, ok := f.seen[req.SKU]; !ok {
		f.seen[req.SKU] = struct{}{}
		f.NewSKUs++
		created = true
	}
	return CatalogueUpsertResult{SKU: req.SKU, Created: created}, nil
}

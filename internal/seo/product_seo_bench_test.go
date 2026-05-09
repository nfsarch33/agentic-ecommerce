package seo

import (
	"context"
	"testing"
)

// BenchmarkProductSEO_Inject measures the steady-state cost of a
// single SEO inject + WC upsert run with a fake trend store and
// importer. Used by the v3.2.0 regression bench.
func BenchmarkProductSEO_Inject(b *testing.B) {
	trends := &fakeTrendKeywordSource{
		responses: map[string][]string{
			"earbuds:cylrl": {"wireless earbuds 2026", "noise cancelling earbuds"},
		},
	}
	importer := &fakeImporter{}
	injector, err := NewProductSEO(nil, ProductSEOConfig{
		Trends:   trends,
		Importer: importer,
		TenantID: "cylrl",
	})
	if err != nil {
		b.Fatalf("NewProductSEO: %v", err)
	}
	b.Cleanup(func() { _ = injector.Close(context.Background()) })

	req := SEOInjectRequest{
		Product: SEOProduct{
			ID:          "earbuds-001",
			Title:       "Premium Wireless Earbuds",
			Description: "Crisp sound and 36-hour battery life.",
			Topic:       "earbuds",
			Categories:  []string{"electronics"},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := injector.Inject(context.Background(), req); err != nil {
			b.Fatalf("Inject: %v", err)
		}
	}
}

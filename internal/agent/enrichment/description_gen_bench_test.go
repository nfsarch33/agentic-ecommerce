package enrichment

import (
	"context"
	"testing"
)

// BenchmarkDescriptionGen_Generate measures the steady-state cost
// of a single LLM-backed description generation run with a fake
// generator. Used by the v3.2.0 regression bench.
func BenchmarkDescriptionGen_Generate(b *testing.B) {
	gen := &fakeAITextGenerator{
		response: `{"english_title":"Premium Wireless Earbuds","english_description":"Crisp sound, all-day comfort, and 36-hour battery life. Pair instantly with any device. Perfect for commuting, workouts, and remote calls."}`,
	}
	d, err := NewDescriptionGenerator(nil, DescriptionGeneratorConfig{
		Generator:  gen,
		TenantID:   "cylrl",
		MinQuality: 0.75,
	})
	if err != nil {
		b.Fatalf("NewDescriptionGenerator: %v", err)
	}
	b.Cleanup(func() { _ = d.Close(context.Background()) })

	req := DescriptionRequest{
		Product: EnrichmentProduct{
			ID:                 "earbuds-001",
			ChineseTitle:       "无线蓝牙耳机",
			ChineseDescription: "高品质蓝牙耳机, 续航36小时",
			Category:           "electronics",
		},
		Platform: PlatformWooCommerce,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := d.Generate(context.Background(), req); err != nil {
			b.Fatalf("Generate: %v", err)
		}
	}
}

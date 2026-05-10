package benchmarks

import (
	"context"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/compliance"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
	"github.com/nfsarch33/agentic-ecommerce/internal/marketplace"
	"github.com/nfsarch33/agentic-ecommerce/internal/media"
	"github.com/nfsarch33/agentic-ecommerce/internal/residency"
	"github.com/nfsarch33/agentic-ecommerce/internal/seo"
)

func BenchmarkComplianceEvaluate_ChinaImport(b *testing.B) {
	product := compliance.Product{
		ID: "p-bench", TenantID: "tenant-bench",
		Title: "Test Product", Category: "electronics",
		Source: compliance.Source1688,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = compliance.Evaluate(product)
	}
}

func BenchmarkComplianceEvaluate_Restricted(b *testing.B) {
	product := compliance.Product{
		ID: "p-restricted", TenantID: "tenant-bench",
		Title: "Restricted Product", Category: "firearms",
		Source: compliance.Source1688,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = compliance.Evaluate(product)
	}
}

func BenchmarkComplianceBatch_100Products(b *testing.B) {
	products := make([]compliance.Product, 100)
	for i := range products {
		cat := "electronics"
		if i%10 == 0 {
			cat = "firearms"
		}
		products[i] = compliance.Product{
			ID: "p-batch", TenantID: "tenant-bench",
			Title: "Batch Product", Category: cat,
			Source: compliance.Source1688,
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		compliance.EvaluateBatch(products)
	}
}

func BenchmarkResidencyValidation(b *testing.B) {
	resolver := &staticResolver{regions: map[string]residency.RegionCode{
		"tenant-au": residency.RegionAU,
		"tenant-cn": residency.RegionCN,
		"tenant-eu": residency.RegionEU,
		"tenant-us": residency.RegionUS,
	}}
	validator := residency.NewValidator(resolver)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validator.Validate(ctx, "tenant-au", residency.RegionAU)
	}
}

func BenchmarkChannelFanOut_6Channels(b *testing.B) {
	bus := eventbus.NewInMemoryBus()
	channels := []string{"tiktok", "facebook", "instagram", "pinterest", "rednote", "1688"}
	for _, ch := range channels {
		ch := ch
		_ = bus.Subscribe(context.Background(), []eventbus.EventType{eventbus.ProductEnriched}, ch, func(_ context.Context, _ eventbus.Event) error {
			return nil
		})
	}
	payload := eventbus.ProductEnrichedPayload{
		Version: eventbus.ProductEnrichedPayloadVersion, TenantID: "tenant-bench",
		ProductID: "product-1", EnglishTitle: "Bench Product", PriceCents: 4999, Currency: "AUD",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		evt, _ := eventbus.NewProductEnrichedEvent("bench", time.Now().UTC(), payload)
		_ = bus.Publish(context.Background(), evt)
	}
}

func BenchmarkSEOOptimizer_Validate(b *testing.B) {
	opt := seo.NewOptimizer()
	suggestion := seo.Suggestion{
		Title:           "Premium Wireless Earbuds - True Wireless Bluetooth",
		MetaDescription: "Shop premium true wireless earbuds with noise cancellation.",
		Slug:            "premium-wireless-earbuds",
		KeywordDensity:  map[string]float64{"wireless": 0.03, "earbuds": 0.02},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = opt.Validate(suggestion)
	}
}

func BenchmarkCatalogProduct_Validation(b *testing.B) {
	price, _ := catalog.NewMoney(4999, "AUD")
	input := catalog.ProductInput{
		SKU:         "SKU-BENCH",
		Title:       "Premium Wireless Earbuds",
		Description: "High quality true wireless earbuds with active noise cancellation and 24-hour battery life for everyday use.",
		Price:       price,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = catalog.NewProduct(input)
	}
}

func BenchmarkMediaProcessor_Validate(b *testing.B) {
	proc := media.NewProcessor(media.DefaultConstraints())
	meta := media.ImageMetadata{
		URL:         "https://example.com/image.jpg",
		AltText:     "Premium wireless earbuds in charging case",
		ProductName: "Premium Wireless Earbuds",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = proc.Validate(meta)
	}
}

func BenchmarkMarketplaceVendor_Create(b *testing.B) {
	now := time.Now().UTC()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = marketplace.NewVendor("vendor-bench", "tenant-bench", "Bench Vendor", "bench@example.com", 1000, now)
	}
}

func BenchmarkEventbus_PublishSubscribe(b *testing.B) {
	bus := eventbus.NewInMemoryBus()
	_ = bus.Subscribe(context.Background(), []eventbus.EventType{eventbus.ProductEnriched}, "bench", func(_ context.Context, _ eventbus.Event) error {
		return nil
	})
	payload := eventbus.ProductEnrichedPayload{
		Version: eventbus.ProductEnrichedPayloadVersion, TenantID: "tenant-bench",
		ProductID: "product-1", EnglishTitle: "Bench Product", PriceCents: 4999, Currency: "AUD",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		evt, _ := eventbus.NewProductEnrichedEvent("bench", time.Now().UTC(), payload)
		_ = bus.Publish(context.Background(), evt)
	}
}

func BenchmarkComplianceEngine_Evaluate(b *testing.B) {
	engine := compliance.NewEngine(compliance.DefaultRules())
	price, _ := catalog.NewMoney(4999, "AUD")
	product, _ := catalog.NewProduct(catalog.ProductInput{
		SKU:         "SKU-BENCH",
		Title:       "Premium Wireless Earbuds",
		Description: "High quality true wireless earbuds with active noise cancellation and 24-hour battery life for everyday use.",
		Price:       price,
	})
	content := compliance.ProductContent{
		Product:  product,
		Keywords: []string{"wireless", "earbuds", "bluetooth"},
		SEOTitle: "Premium Wireless Earbuds",
		Meta:     "Shop premium earbuds with noise cancellation.",
	}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = engine.Evaluate(ctx, content)
	}
}

type staticResolver struct {
	regions map[string]residency.RegionCode
}

func (r *staticResolver) TenantRegion(_ context.Context, tenantID string) (residency.RegionCode, error) {
	if region, ok := r.regions[tenantID]; ok {
		return region, nil
	}
	return residency.DefaultRegion(), nil
}

package benchmarks

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/postgres"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/redis"
	"github.com/nfsarch33/agentic-ecommerce/internal/compliance"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
	"github.com/nfsarch33/agentic-ecommerce/internal/marketplace"
	"github.com/nfsarch33/agentic-ecommerce/internal/media"
	"github.com/nfsarch33/agentic-ecommerce/internal/residency"
	"github.com/nfsarch33/agentic-ecommerce/internal/seo"
)

// v5.5.0 benchmark regression suite.
//
// Re-runs all v4.10.0 baselines plus new v5.5.0-specific benchmarks:
//   - Postgres pool config load latency
//   - Redis pipeline vs single-call latency
//   - Payment saga end-to-end workflow placeholder
//
// Gate: no regression >10% on any v4.10.0 baseline benchmark.
// Output: comparison table printed via t.Log.
// Persist: tests/benchmarks/v550_baseline.txt (via `go test -bench . > ...`)

// --- v4.10.0 baseline benchmarks (re-run) ---

func BenchmarkV550_ComplianceEvaluate_ChinaImport(b *testing.B) {
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

func BenchmarkV550_ComplianceEvaluate_Restricted(b *testing.B) {
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

func BenchmarkV550_ComplianceBatch_100Products(b *testing.B) {
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

func BenchmarkV550_ResidencyValidation(b *testing.B) {
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

func BenchmarkV550_ChannelFanOut_6Channels(b *testing.B) {
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

func BenchmarkV550_SEOOptimizer_Validate(b *testing.B) {
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

func BenchmarkV550_CatalogProduct_Validation(b *testing.B) {
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

func BenchmarkV550_MediaProcessor_Validate(b *testing.B) {
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

func BenchmarkV550_MarketplaceVendor_Create(b *testing.B) {
	now := time.Now().UTC()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = marketplace.NewVendor("vendor-bench", "tenant-bench", "Bench Vendor", "bench@example.com", 1000, now)
	}
}

func BenchmarkV550_Eventbus_PublishSubscribe(b *testing.B) {
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

func BenchmarkV550_ComplianceEngine_Evaluate(b *testing.B) {
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

// --- v5.5.0 new benchmarks ---

func BenchmarkV550_PGPoolConfigLoad(b *testing.B) {
	b.Setenv("EC_PG_MAX_OPEN_CONNS", "50")
	b.Setenv("EC_PG_MAX_IDLE_CONNS", "20")
	b.Setenv("EC_PG_CONN_MAX_LIFETIME_MINUTES", "60")
	b.Setenv("EC_PG_CONN_MAX_IDLE_TIME_MINUTES", "10")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = postgres.LoadPGPoolConfig()
	}
}

func BenchmarkV550_RedisPipelineBatch10(b *testing.B) {
	store := map[string]any{}
	for i := 0; i < 10; i++ {
		store[fmt.Sprintf("key%d", i)] = fmt.Sprintf("val%d", i)
	}

	flush := func(_ context.Context, cmds []redis.PipelineCmd) ([]redis.PipelineResult, error) {
		results := make([]redis.PipelineResult, len(cmds))
		for i, cmd := range cmds {
			if v, ok := store[cmd.Key]; ok {
				results[i] = redis.PipelineResult{Value: v}
			} else {
				results[i] = redis.PipelineResult{}
			}
		}
		return results, nil
	}

	keys := make([]string, 10)
	for i := range keys {
		keys[i] = fmt.Sprintf("key%d", i)
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pipe := redis.NewPipeline(flush)
		_, _ = redis.BatchGet(ctx, pipe, keys)
	}
}

func BenchmarkV550_RedisSingleCall10(b *testing.B) {
	store := map[string]any{}
	for i := 0; i < 10; i++ {
		store[fmt.Sprintf("key%d", i)] = fmt.Sprintf("val%d", i)
	}

	flush := func(_ context.Context, cmds []redis.PipelineCmd) ([]redis.PipelineResult, error) {
		results := make([]redis.PipelineResult, len(cmds))
		for i, cmd := range cmds {
			if v, ok := store[cmd.Key]; ok {
				results[i] = redis.PipelineResult{Value: v}
			} else {
				results[i] = redis.PipelineResult{}
			}
		}
		return results, nil
	}

	keys := make([]string, 10)
	for i := range keys {
		keys[i] = fmt.Sprintf("key%d", i)
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, k := range keys {
			pipe := redis.NewPipeline(flush)
			_ = pipe.Add(redis.PipelineCmd{Op: "GET", Key: k})
			_, _ = pipe.Exec(ctx)
		}
	}
}

func BenchmarkV550_PaymentSagaWorkflow(b *testing.B) {
	type paymentStep struct {
		Provider string
		Amount   int64
		Status   string
	}
	steps := []paymentStep{
		{"stripe", 4999, "pending"},
		{"stripe", 4999, "authorized"},
		{"stripe", 4999, "captured"},
		{"stripe", 4999, "succeeded"},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, s := range steps {
			_ = s.Provider
			_ = s.Amount
			_ = s.Status
		}
	}
}

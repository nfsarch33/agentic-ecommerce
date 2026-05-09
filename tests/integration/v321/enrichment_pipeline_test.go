//go:build v321_smoke

// File scope: v3.2.1 QA Task 1 -- 50-product live smoke through
// the full v3.2.0 enrichment pipeline.
//
// The smoke wires the production composition shape:
//
//	workerpool.Pool (resilience pillar fan-out)
//	  -> rag.Service (HashEmbedder + InMemoryVectorStore)
//	     -> rag.TrendIngestor seeds the store with platform-trend
//	        keywords for the five fixture topics
//	enrichment.DescriptionGenerator (deterministic LLM stub)
//	media.ProductImagePipeline (StubBackgroundRemover)
//	seo.ProductSEO (ragTrendsAdapter + fixtureCatalogueImporter)
//
// Every component is registered with internal/lifecycle.Manager so
// the v2.10 resilience pillar drain runs at the end of the test.
//
// Acceptance:
//
//   - 50 products through TrendIngestor seed -> DescriptionGen ->
//     ProductImage (stub) -> SEO inject -> WC sync stub.
//   - Quality scorer >= 0.75 hit rate at 100% across the 50.
//   - Per-product evidence + 5-bucket histogram emitted via t.Log.
package v321

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/agent/enrichment"
	"github.com/nfsarch33/agentic-ecommerce/internal/lifecycle"
	"github.com/nfsarch33/agentic-ecommerce/internal/media"
	"github.com/nfsarch33/agentic-ecommerce/internal/rag"
	"github.com/nfsarch33/agentic-ecommerce/internal/seo"
	"github.com/nfsarch33/agentic-ecommerce/internal/workerpool"
)

// productEvidence captures every field a reviewer needs to decide
// whether a single product cleared the v3.2.0 enrichment gate.
type productEvidence struct {
	ProductID           string
	Category            string
	Topic               string
	DescriptionScore    float64
	DescriptionSource   enrichment.ResultSource
	DescriptionLength   int
	ImageBytes          int
	ImageHasTransparent bool
	SEOScore            int
	SEOPass             bool
	UsedTrendData       bool
	TrendKeywordCount   int
}

// smokeHarness bundles every wired component for the 50-product
// smoke. Returned by setupSmokeHarness so the top-level test stays a
// thin orchestrator (cyclomatic complexity guard for sentrux: keeps
// TestEnrichmentPipeline_50ProductSmoke under the complex_fn
// threshold).
type smokeHarness struct {
	descriptionGen *enrichment.DescriptionGenerator
	imagePipeline  *media.ProductImagePipeline
	seoInjector    *seo.ProductSEO
	trendAdapter   *ragTrendsAdapter
	llm            *fixtureLLM
	wcImporter     *fixtureCatalogueImporter
	products       []fixtureProduct
}

// TestEnrichmentPipeline_50ProductSmoke is the v3.2.1 QA-1
// acceptance test (per the parent plan: "End-to-end live smoke:
// 50 sample products through full enrichment pipeline; quality
// scorer >=0.75 hit rate").
func TestEnrichmentPipeline_50ProductSmoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const targetProductCount = 50
	const minQuality = 0.75

	h := setupSmokeHarness(t, ctx, targetProductCount, minQuality)

	evidences := make([]productEvidence, 0, len(h.products))
	for _, p := range h.products {
		ev := runOneProduct(ctx, t, h.descriptionGen, h.imagePipeline, h.seoInjector, h.trendAdapter, p)
		evidences = append(evidences, ev)
	}

	hits, histogram, sources := summariseEvidence(evidences, minQuality)
	hitRate := float64(hits) / float64(len(evidences))
	logSmokeReport(t, evidences, histogram, sources, hits, hitRate, h.wcImporter, h.llm)

	assertSmokeOutcome(t, evidences, hits, hitRate, h.wcImporter, h.llm)
}

// setupSmokeHarness wires every component of the EC-2 enrichment
// pipeline + registers each Closer with a single lifecycle.Manager
// so end-of-test drain happens automatically. The trend store is
// pre-seeded via TrendIngestor.Run so the SEO stage always finds
// keywords for every fixture topic.
func setupSmokeHarness(t *testing.T, ctx context.Context, productCount int, minQuality float64) *smokeHarness {
	t.Helper()
	const tenantID = "cylrl"
	now := func() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) }

	pool := workerpool.New(nil, workerpool.Config{
		Name:       "v321-smoke",
		MinWorkers: 2,
		MaxWorkers: 4,
		QueueDepth: 16,
	})
	embedder := rag.NewHashEmbedder(rag.DefaultEmbeddingDimensions)
	vectorStore := rag.NewInMemoryVectorStore(rag.DefaultEmbeddingDimensions)
	ragService := rag.NewService(embedder, vectorStore, rag.ChunkOptions{MaxWords: 32})

	trendIngestor := mustNewTrendIngestor(t, ragService, pool, tenantID, now)
	seedTrendStore(t, ctx, trendIngestor)

	llm := &fixtureLLM{}
	descriptionGen := mustNewDescriptionGenerator(t, llm, tenantID, minQuality, now)

	products := buildFixtureProducts(productCount)
	if len(products) != productCount {
		t.Fatalf("buildFixtureProducts returned %d, want %d", len(products), productCount)
	}
	imagePipeline := mustNewImagePipeline(t, products, tenantID, now)

	wcImporter := newFixtureCatalogueImporter()
	trendAdapter := &ragTrendsAdapter{service: ragService}
	seoInjector := mustNewSEOInjector(t, trendAdapter, wcImporter, tenantID, now)

	manager := lifecycle.New(nil, 5*time.Second)
	manager.Register("workerpool", pool)
	manager.Register("trend_ingestor", trendIngestor)
	manager.Register("description_gen", descriptionGen)
	manager.Register("image_pipeline", imagePipeline)
	manager.Register("seo_injector", seoInjector)
	t.Cleanup(func() {
		if err := manager.Shutdown(); err != nil {
			t.Errorf("lifecycle.Manager.Shutdown: %v", err)
		}
	})

	return &smokeHarness{
		descriptionGen: descriptionGen,
		imagePipeline:  imagePipeline,
		seoInjector:    seoInjector,
		trendAdapter:   trendAdapter,
		llm:            llm,
		wcImporter:     wcImporter,
		products:       products,
	}
}

// mustNewTrendIngestor builds the EC-2-4 trend ingestor or fails the
// test. Extracted so setupSmokeHarness stays linear (each helper
// raises a single error path; no inline t.Fatalf forks).
func mustNewTrendIngestor(t *testing.T, service *rag.Service, pool *workerpool.Pool, tenantID string, now func() time.Time) *rag.TrendIngestor {
	t.Helper()
	ingestor, err := rag.NewTrendIngestor(nil, rag.TrendIngestorConfig{
		Sources:  buildFixtureTrendSources(),
		Service:  service,
		Pool:     pool,
		TenantID: tenantID,
		Now:      now,
	})
	if err != nil {
		t.Fatalf("NewTrendIngestor: %v", err)
	}
	return ingestor
}

// seedTrendStore pre-populates the rag store with platform trend
// keywords so the SEO injector finds keywords for every fixture topic.
// Fails the test if the seed run returns zero records.
func seedTrendStore(t *testing.T, ctx context.Context, ingestor *rag.TrendIngestor) {
	t.Helper()
	report, err := ingestor.Run(ctx)
	if err != nil {
		t.Fatalf("TrendIngestor.Run: %v", err)
	}
	if report.RecordsIngested == 0 {
		t.Fatalf("TrendIngestor.Run: zero records ingested; report=%+v", report)
	}
}

func mustNewDescriptionGenerator(t *testing.T, gen *fixtureLLM, tenantID string, minQuality float64, now func() time.Time) *enrichment.DescriptionGenerator {
	t.Helper()
	d, err := enrichment.NewDescriptionGenerator(nil, enrichment.DescriptionGeneratorConfig{
		Generator:  gen,
		TenantID:   tenantID,
		MinQuality: minQuality,
		Now:        now,
	})
	if err != nil {
		t.Fatalf("NewDescriptionGenerator: %v", err)
	}
	return d
}

func mustNewImagePipeline(t *testing.T, products []fixtureProduct, tenantID string, now func() time.Time) *media.ProductImagePipeline {
	t.Helper()
	p, err := media.NewProductImagePipeline(nil, media.ProductImagePipelineConfig{
		Downloader: newFixtureDownloader(products),
		Remover:    media.NewStubBackgroundRemover(),
		Store:      newFixtureMediaStore(),
		TenantID:   tenantID,
		KeyPrefix:  "v321-smoke",
		Now:        now,
	})
	if err != nil {
		t.Fatalf("NewProductImagePipeline: %v", err)
	}
	return p
}

func mustNewSEOInjector(t *testing.T, trends *ragTrendsAdapter, importer *fixtureCatalogueImporter, tenantID string, now func() time.Time) *seo.ProductSEO {
	t.Helper()
	inj, err := seo.NewProductSEO(nil, seo.ProductSEOConfig{
		Trends:   trends,
		Importer: importer,
		TenantID: tenantID,
		Now:      now,
	})
	if err != nil {
		t.Fatalf("NewProductSEO: %v", err)
	}
	return inj
}

// assertSmokeOutcome runs the post-loop assertions. Extracted so the
// top-level test stays under the sentrux complex_fn threshold.
func assertSmokeOutcome(
	t *testing.T,
	evidences []productEvidence,
	hits int,
	hitRate float64,
	wcImporter *fixtureCatalogueImporter,
	llm *fixtureLLM,
) {
	t.Helper()
	n := len(evidences)
	if hitRate < 0.75 {
		t.Fatalf("quality-scorer hit rate = %.4f (%d/%d), want >= 0.75 (per Epic 2 EC-2-1 acceptance)", hitRate, hits, n)
	}
	if got := wcImporter.Calls(); got != n {
		t.Fatalf("WC importer Calls = %d, want %d (one per product)", got, n)
	}
	if got := wcImporter.NewSKUs(); got != n {
		t.Fatalf("WC importer NewSKUs = %d, want %d (every fixture is a fresh SKU)", got, n)
	}
	if got := wcImporter.StoredCount(); got != n {
		t.Fatalf("WC importer StoredCount = %d, want %d (zero overlap allowed across the 50 fixture IDs)", got, n)
	}
	if got := llm.calls.Load(); int(got) != n {
		t.Fatalf("fixture LLM calls = %d, want %d (description gen runs exactly once per product)", got, n)
	}
	for _, ev := range evidences {
		if !ev.ImageHasTransparent {
			t.Errorf("product %s: image pipeline produced no transparent pixels", ev.ProductID)
		}
	}
}

func runOneProduct(
	ctx context.Context,
	t *testing.T,
	gen *enrichment.DescriptionGenerator,
	imagePipe *media.ProductImagePipeline,
	seoInj *seo.ProductSEO,
	trends *ragTrendsAdapter,
	p fixtureProduct,
) productEvidence {
	t.Helper()

	descRes, err := gen.Generate(ctx, enrichment.DescriptionRequest{
		Product:  p.toEnrichmentProduct(),
		Platform: enrichment.PlatformWooCommerce,
		Language: "en-AU",
	})
	if err != nil {
		t.Fatalf("DescriptionGen[%s]: %v", p.ID, err)
	}

	imgRes, err := imagePipe.Process(ctx, media.ProductImageRequest{
		ProductID: p.ID,
		ImageURL:  p.ImageURL,
		Action:    media.ActionBackgroundRemoval,
	})
	if err != nil {
		t.Fatalf("ImagePipeline[%s]: %v", p.ID, err)
	}
	imgHasTransparent := pngHasTransparentPixel(t, imgRes.OutputBytes)

	seoRes, err := seoInj.Inject(ctx, seo.SEOInjectRequest{
		Product: seo.SEOProduct{
			ID:          p.ID,
			Title:       descRes.EnglishTitle,
			Description: descRes.EnglishDescription,
			Topic:       p.Topic,
			Categories:  []string{p.Category},
			PriceCents:  p.PriceCents,
			Stock:       p.Stock,
		},
	})
	if err != nil {
		t.Fatalf("SEOInject[%s]: %v", p.ID, err)
	}

	keywords, err := trends.TrendingKeywords(ctx, "cylrl", p.Topic)
	if err != nil {
		t.Fatalf("trend lookup[%s]: %v", p.ID, err)
	}

	return productEvidence{
		ProductID:           p.ID,
		Category:            p.Category,
		Topic:               p.Topic,
		DescriptionScore:    descRes.QualityScore,
		DescriptionSource:   descRes.Source,
		DescriptionLength:   len(descRes.EnglishDescription),
		ImageBytes:          len(imgRes.OutputBytes),
		ImageHasTransparent: imgHasTransparent,
		SEOScore:            seoRes.Suggestion.Score,
		SEOPass:             seoRes.Suggestion.Pass,
		UsedTrendData:       seoRes.UsedTrendData,
		TrendKeywordCount:   len(keywords),
	}
}

// summariseEvidence returns (passing count, histogram, source counts).
func summariseEvidence(evidences []productEvidence, minQuality float64) (int, map[string]int, map[enrichment.ResultSource]int) {
	histogram := map[string]int{
		"a_critical_<0.50":       0,
		"b_low_0.50-0.65":        0,
		"c_borderline_0.65-0.75": 0,
		"d_pass_0.75-0.85":       0,
		"e_excellent_>=0.85":     0,
	}
	sources := map[enrichment.ResultSource]int{}
	hits := 0
	for _, ev := range evidences {
		if ev.DescriptionScore >= minQuality {
			hits++
		}
		histogram[bucketize(ev.DescriptionScore)]++
		sources[ev.DescriptionSource]++
	}
	return hits, histogram, sources
}

func bucketize(score float64) string {
	switch {
	case score < 0.50:
		return "a_critical_<0.50"
	case score < 0.65:
		return "b_low_0.50-0.65"
	case score < 0.75:
		return "c_borderline_0.65-0.75"
	case score < 0.85:
		return "d_pass_0.75-0.85"
	default:
		return "e_excellent_>=0.85"
	}
}

func logSmokeReport(
	t *testing.T,
	evidences []productEvidence,
	histogram map[string]int,
	sources map[enrichment.ResultSource]int,
	hits int,
	hitRate float64,
	wcImporter *fixtureCatalogueImporter,
	llm *fixtureLLM,
) {
	t.Helper()
	t.Logf("v3.2.1 QA-1 50-product smoke summary -- hit rate %.4f (%d/%d) -- LLM calls %d -- WC calls %d -- WC newSKUs %d",
		hitRate, hits, len(evidences), llm.calls.Load(), wcImporter.Calls(), wcImporter.NewSKUs())

	keys := make([]string, 0, len(histogram))
	for k := range histogram {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	t.Log("-- description quality histogram --")
	for _, k := range keys {
		t.Logf("  %-26s %d", k, histogram[k])
	}

	t.Log("-- description source counts --")
	for src, n := range sources {
		t.Logf("  %-12s %d", string(src), n)
	}

	t.Log("-- per-product evidence (first 5 + last 5) --")
	dump := func(rows []productEvidence) {
		for _, ev := range rows {
			t.Logf("  %-22s | cat=%-11s | topic=%-13s | score=%.4f | src=%-9s | imgB=%-4d | imgT=%v | seo=%-3d/%v | trends=%d",
				ev.ProductID, ev.Category, ev.Topic, ev.DescriptionScore, string(ev.DescriptionSource), ev.ImageBytes, ev.ImageHasTransparent, ev.SEOScore, ev.SEOPass, ev.TrendKeywordCount)
		}
	}
	if len(evidences) <= 10 {
		dump(evidences)
	} else {
		dump(evidences[:5])
		t.Log("  ...")
		dump(evidences[len(evidences)-5:])
	}

	// Aggregate per-category for quick triage.
	type catAgg struct {
		count    int
		sumScore float64
		minScore float64
		maxScore float64
	}
	byCat := map[string]*catAgg{}
	for _, ev := range evidences {
		a, ok := byCat[ev.Category]
		if !ok {
			a = &catAgg{minScore: 1.0}
			byCat[ev.Category] = a
		}
		a.count++
		a.sumScore += ev.DescriptionScore
		if ev.DescriptionScore < a.minScore {
			a.minScore = ev.DescriptionScore
		}
		if ev.DescriptionScore > a.maxScore {
			a.maxScore = ev.DescriptionScore
		}
	}
	catKeys := make([]string, 0, len(byCat))
	for k := range byCat {
		catKeys = append(catKeys, k)
	}
	sort.Strings(catKeys)
	t.Log("-- per-category aggregate --")
	for _, k := range catKeys {
		a := byCat[k]
		avg := a.sumScore / float64(a.count)
		t.Logf("  %-11s n=%-3d avg=%.4f min=%.4f max=%.4f", k, a.count, avg, a.minScore, a.maxScore)
	}

	_ = fmt.Sprintf // ensure fmt is used even when log lines change
}

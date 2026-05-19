package observability

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/metrics"
)

// TestMetrics_EnrichmentCounterRegisteredWithoutCollision is the
// v3.2.0 EC-2-5 RED test. It verifies that the EnrichmentMetrics
// helper:
//
//  1. Registers the EC-2-5 enrichment counters + histograms on the
//     supplied metrics.Registry without colliding with the
//     existing v2.10/v3.1 ec_* metric names. Specifically the
//     external roadmap noted a research_scraper_scrape_total
//     namespace collision; the EC-side of that name does not exist
//     in the current code, so the helper avoids the
//     research_scraper_* prefix entirely and keeps to the ec_*
//     namespace.
//  2. Exposes typed Record methods for each pipeline stage
//     (description gen, image gen, SEO inject, WC sync, trend
//     ingest) so callers cannot accidentally typo a stage label.
//  3. Calling Register twice on the same Registry is a no-op
//     (idempotent wiring), so cmd/* binaries that re-build the
//     registry during tests do not panic.
//  4. The /metrics handler emits the new ec_enrichment_runs_total
//     and ec_enrichment_duration_seconds families with the
//     stage label.
func TestMetrics_EnrichmentCounterRegisteredWithoutCollision(t *testing.T) {
	t.Parallel()

	registry := metrics.NewRegistry("agent-worker-test")
	em, err := NewEnrichmentMetrics(registry)
	if err != nil {
		t.Fatalf("NewEnrichmentMetrics: %v", err)
	}
	if em == nil {
		t.Fatal("NewEnrichmentMetrics returned nil helper")
	}

	// Idempotent: a second registration on the same registry must
	// not panic and must return the same series counts.
	if _, err := NewEnrichmentMetrics(registry); err != nil {
		t.Fatalf("NewEnrichmentMetrics (second call): %v", err)
	}

	em.RecordRun("cylrl", EnrichmentStageDescriptionGen, EnrichmentStatusOK, 0.42, 0.91)
	em.RecordRun("cylrl", EnrichmentStageImageGen, EnrichmentStatusOK, 0.81, 0.95)
	em.RecordRun("cylrl", EnrichmentStageSEOInject, EnrichmentStatusOK, 0.05, 0.78)
	em.RecordRun("cylrl", EnrichmentStageWCSync, EnrichmentStatusOK, 0.31, 1.0)
	em.RecordRun("cylrl", EnrichmentStageDescriptionGen, EnrichmentStatusFailed, 1.10, 0)
	em.RecordImageProcessing("background_removal", "ok", 0.06, 4096, 8192)
	em.RecordImageProcessing("lifestyle_generation", "deferred", 0.001, 0, 0)
	em.RecordTrendIngest("tiktok", 12)
	em.RecordTrendIngest("google_trends", 5)
	em.RecordSEOKeywordInject("cylrl", 3)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	registry.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()

	want := []string{
		"ec_enrichment_runs_total",
		"ec_enrichment_duration_seconds",
		"ec_enrichment_quality_score",
		"ec_image_processing_total",
		"ec_trend_ingest_records_total",
		"ec_seo_keyword_injects_total",
		`stage="description_gen"`,
		`stage="image_gen"`,
		`stage="seo_inject"`,
		`stage="wc_sync"`,
		`tenant_id="cylrl"`,
		`status="ok"`,
		`status="failed"`,
		`platform="tiktok"`,
		`platform="google_trends"`,
		`action="background_removal"`,
		`action="lifestyle_generation"`,
	}
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Fatalf("metrics output missing %q\nbody:\n%s", w, body)
		}
	}

	// Namespace collision guard: research_scraper_scrape_total must
	// not appear in the EC stack. The external roadmap flagged this
	// as the v2.10/v3.1 collision sentinel; the EC namespace owns
	// the ec_* prefix so research_* names are out of scope.
	if strings.Contains(body, "research_scraper_scrape_total") {
		t.Fatal("EC metrics handler must not expose research_scraper_scrape_total (namespace collision)")
	}
}

func TestNewEnrichmentMetrics_RejectsNilRegistry(t *testing.T) {
	t.Parallel()

	_, err := NewEnrichmentMetrics(nil)
	if err == nil {
		t.Fatal("expected error when registry is nil")
	}
	if !errors.Is(err, ErrEnrichmentMetricsUnconfigured) {
		t.Fatalf("error not wrapping ErrEnrichmentMetricsUnconfigured: %v", err)
	}
}

func TestEnrichmentMetrics_StageNoOpOnNilHelper(t *testing.T) {
	t.Parallel()

	// Nil-safe accessor pattern: callers typed as
	// *EnrichmentMetrics may pass nil when metrics are disabled.
	// All Record* methods must no-op without panic.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil EnrichmentMetrics receiver panicked: %v", r)
		}
	}()
	var em *EnrichmentMetrics
	em.RecordRun("cylrl", EnrichmentStageDescriptionGen, EnrichmentStatusOK, 0.1, 0.9)
	em.RecordImageProcessing("background_removal", "ok", 0.1, 1, 1)
	em.RecordTrendIngest("tiktok", 1)
	em.RecordSEOKeywordInject("cylrl", 1)
}

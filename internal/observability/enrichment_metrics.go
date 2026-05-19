// File scope: v3.2.0 EC-2-5 Enrichment Pipeline Observability.
//
// EnrichmentMetrics is a small typed facade around the existing
// internal/metrics.Registry that the four enrichment-pipeline
// stages (description gen, image gen, SEO inject, WC sync) plus
// the EC-2-4 trend ingestor call to record per-stage runs,
// duration histograms, and per-stage quality scores.
//
// Design notes:
//
//   - The actual Counter / Histogram values live on metrics.Registry
//     (they are part of the Prometheus exposition path). This
//     package only owns the typed enum + the nil-safe wrapper so
//     callers cannot typo a stage label.
//   - Nil-safe receivers: every Record* method on
//     *EnrichmentMetrics is a no-op when the receiver is nil. This
//     lets cmd/* binaries pass nil when metrics are disabled in
//     dev / unit tests without extra branching at every call site.
//   - The package owns the per-stage label vocabulary so dashboards
//     can pivot on a stable set: description_gen, image_gen,
//     seo_inject, wc_sync.
//   - Namespace collision sentinel: the external roadmap flagged
//     research_scraper_scrape_total as the v2.10/v3.1 namespace
//     collision. The current EC code base does not register that
//     name (verified at v3.2.0 implementation: rg shows zero
//     hits in the agentic-ecommerce repo) so this helper sticks
//     to the ec_* namespace and the test enforces the absence of
//     research_scraper_* in the /metrics output.
package observability

import (
	"errors"
	"fmt"

	"github.com/nfsarch33/helixon-ec/internal/metrics"
)

// ErrEnrichmentMetricsUnconfigured is returned by
// NewEnrichmentMetrics when the supplied registry is nil.
var ErrEnrichmentMetricsUnconfigured = errors.New("observability: enrichment metrics unconfigured")

// EnrichmentStage is the per-stage label used by the EC-2-5
// dashboard. Kept as a typed string so callers cannot typo a
// stage name.
type EnrichmentStage string

const (
	EnrichmentStageDescriptionGen EnrichmentStage = "description_gen"
	EnrichmentStageImageGen       EnrichmentStage = "image_gen"
	EnrichmentStageSEOInject      EnrichmentStage = "seo_inject"
	EnrichmentStageWCSync         EnrichmentStage = "wc_sync"
	EnrichmentStageTrendIngest    EnrichmentStage = "trend_ingest"
)

// EnrichmentStatus is the per-run status label.
type EnrichmentStatus string

const (
	EnrichmentStatusOK       EnrichmentStatus = "ok"
	EnrichmentStatusFailed   EnrichmentStatus = "failed"
	EnrichmentStatusFallback EnrichmentStatus = "fallback"
	EnrichmentStatusDeferred EnrichmentStatus = "deferred"
)

// EnrichmentMetrics is the typed facade.
type EnrichmentMetrics struct {
	registry *metrics.Registry
}

// NewEnrichmentMetrics binds to the supplied registry.
func NewEnrichmentMetrics(registry *metrics.Registry) (*EnrichmentMetrics, error) {
	if registry == nil {
		return nil, fmt.Errorf("%w: metrics.Registry required", ErrEnrichmentMetricsUnconfigured)
	}
	return &EnrichmentMetrics{registry: registry}, nil
}

// RecordRun records one enrichment-stage run with its duration
// (seconds) and quality score (0..1; pass 0 for stages without a
// quality measurement).
func (em *EnrichmentMetrics) RecordRun(tenantID string, stage EnrichmentStage, status EnrichmentStatus, durationSeconds, qualityScore float64) {
	if em == nil || em.registry == nil {
		return
	}
	em.registry.EnrichmentRuns.Inc(metrics.Labels{
		"tenant_id": tenantID,
		"stage":     string(stage),
		"status":    string(status),
	})
	em.registry.EnrichmentDuration.Observe(durationSeconds, metrics.Labels{"stage": string(stage)})
	if qualityScore > 0 {
		em.registry.EnrichmentQualityScore.Observe(qualityScore, metrics.Labels{"stage": string(stage)})
	}
}

// RecordImageProcessing records one EC-2-2 image pipeline run.
// action is "background_removal" or "lifestyle_generation"; status
// is "ok"/"download_failed"/"remove_failed"/"store_failed"/
// "deferred"/"too_large".
func (em *EnrichmentMetrics) RecordImageProcessing(action, status string, durationSeconds float64, bytesIn, bytesOut int) {
	if em == nil || em.registry == nil {
		return
	}
	em.registry.ImageProcessingTotal.Inc(metrics.Labels{
		"action": action,
		"status": status,
	})
	em.registry.ImageProcessingDuration.Observe(durationSeconds, metrics.Labels{"action": action})
	_ = bytesIn  // reserved for future per-action throughput dashboards
	_ = bytesOut //
}

// RecordTrendIngest records one EC-2-4 platform poll outcome.
func (em *EnrichmentMetrics) RecordTrendIngest(platform string, recordCount int) {
	if em == nil || em.registry == nil || recordCount <= 0 {
		return
	}
	em.registry.TrendIngestRecordsTotal.Add(float64(recordCount), metrics.Labels{"platform": platform})
}

// RecordSEOKeywordInject records one EC-2-3 keyword injection run.
func (em *EnrichmentMetrics) RecordSEOKeywordInject(tenantID string, keywordCount int) {
	if em == nil || em.registry == nil {
		return
	}
	em.registry.SEOKeywordInjectsTotal.Add(float64(keywordCount), metrics.Labels{"tenant_id": tenantID})
}

// Registry returns the underlying registry. Useful for tests +
// admin endpoints that need the /metrics handler.
func (em *EnrichmentMetrics) Registry() *metrics.Registry {
	if em == nil {
		return nil
	}
	return em.registry
}

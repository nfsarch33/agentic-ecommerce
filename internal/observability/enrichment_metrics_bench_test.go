package observability

import (
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/metrics"
)

// BenchmarkEnrichmentMetrics_RecordRun measures the steady-state
// cost of a single enrichment metric record. Used by the v3.2.0
// regression bench so dashboards can prove the metric pipeline is
// not on the critical path.
func BenchmarkEnrichmentMetrics_RecordRun(b *testing.B) {
	em, err := NewEnrichmentMetrics(metrics.NewRegistry("agent-worker-bench"))
	if err != nil {
		b.Fatalf("NewEnrichmentMetrics: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		em.RecordRun("cylrl", EnrichmentStageDescriptionGen, EnrichmentStatusOK, 0.1, 0.9)
	}
}

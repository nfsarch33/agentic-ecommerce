// Benchmarks for the v3.5.0 EC-6-1 supplier cost monitor pure-
// path helpers. The Tick + evaluateEntry pipeline exercises the
// adapter so the bench focuses on the deterministic decision
// helpers (computeDelta + decideOutcome).
package monitor

import "testing"

func BenchmarkComputeDelta(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = computeDelta(1000, 1100)
	}
}

func BenchmarkDecideOutcome(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = decideOutcome(0.07, 0.05)
	}
}

//go:build v321_smoke

// Package v321 contains the v3.2.1 enrichment-pipeline smoke
// integration tests. These tests are gated behind the
// `v321_smoke` build tag because they:
//
//   - drive 50 supplier products end-to-end through the full
//     v3.2.0 enrichment pipeline (TrendIngestor seed ->
//     DescriptionGen -> ProductImage stub -> SEO inject ->
//     CatalogueImporter stub) and therefore take longer than the
//     per-package unit suite;
//   - reuse the v2.10 internal/lifecycle.Manager + internal/
//     workerpool harness so the goleak check at the cmd/* level
//     stays representative of production composition.
//
// Run locally:  go test -tags v321_smoke -race -v ./tests/integration/v321/...
// CI:           agentic-ecommerce v3.2.1 QA gate (Phase B Task 1).
//
// Per-product evidence + a quality-score histogram are emitted to
// stdout via t.Log so PR reviewers can copy/paste the artefact
// straight into the CHANGELOG / sprint retro.
//
// Cite skill: go-clean-architecture (the test wires real ports,
// fakes only the LLM/HTTP/WC adapters at the composition root).
package v321

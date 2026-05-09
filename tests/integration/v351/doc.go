//go:build v351_smoke

// Package v351 contains the v3.5.1 pricing + fulfilment QA
// validation integration tests. The v3.5.1 sprint scope is QA-only;
// the production code shipped in v3.5.0 (PR #88). These tests gate
// behind the `v351_smoke` build tag because they:
//
//   - drive the full v3.5.0 EC-7-2 dropship saga end-to-end against
//     httptest mock supplier servers (1688 primary + AliExpress
//     fallback) so the saga rollback acceptance ("compensation API
//     calls made in correct order; rollback completes within 10s")
//     is reproducible from a single runnable test;
//   - drive the v3.5.0 EC-6-1 supplier cost monitor through the full
//     6-scenario validation matrix (>5% increase / >5% decrease /
//     exactly-threshold / sub-threshold noise / configurable
//     threshold / multi-SKU batch) so the event payload contract +
//     ec_supplier_cost_changes_total cardinality stay verifiable
//     from a single runnable test;
//   - reuse the v2.10 internal/lifecycle.Manager + internal/workerpool
//     drain pattern so the goleak check at the cmd/* level stays
//     representative of production composition;
//   - reuse the v3.4.1 sandbox + httptest scaffold patterns so the
//     supplier mock servers stay hermetic (no live network egress).
//
// Run locally:  go test -tags v351_smoke -race -p 4 -v ./tests/integration/v351/...
// CI:           agentic-ecommerce v3.5.1 QA gate (Tasks 1 + 3).
//
// Per-scenario evidence + per-supplier latency lines are emitted to
// stdout via t.Log so PR reviewers can copy-paste the artefact
// straight into the CHANGELOG / sprint retro.
//
// Cite skill: temporal-developer (saga compensation patterns), and
// go-clean-architecture (the test wires real ports; fakes only the
// HTTP transport at the supplier boundary).
package v351

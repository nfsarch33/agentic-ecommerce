//go:build v381_smoke

// Package v381 contains the v3.8.1 logistics + returns + ROI QA
// validation integration tests. The v3.8.1 sprint scope is QA-only;
// the production code shipped in v3.8.0 (PR #94, EC-7-3..EC-7-5 +
// EC-9-3 + Existing #5). These tests gate behind the `v381_smoke`
// build tag because they:
//
//   - drive the full v3.8.0 EC-7-3 ShippingLabelGenerator through 10
//     acceptance scenarios (6 SLA + 4 carrier-failure) so the Epic 7
//     acceptance "domestic AU label cheapest carrier within 5-day SLA;
//     graceful degradation when a carrier is unavailable" is
//     reproducible from a single runnable test against in-memory
//     stub carrier clients (no live API calls);
//   - drive the full v3.8.0 EC-7-4 StatusPropagator + carrier
//     webhook handlers through 8 scenarios (4-channel happy path,
//     retry-success, retry-exhaust, AusPost+DHL webhook end-to-end,
//     replay attack, concurrent webhooks, out-of-order delivery)
//     so the Epic 7 acceptance "all 3 channels updated within 60s
//     of shipment event; retry verified under injected 429" is
//     reproducible from a single runnable test using a fake clock
//   - httptest server stubs (no real-time sleeps);
//   - drive the full v3.8.0 EC-7-5 ReturnsSagaWorkflow through 7
//     end-to-end scenarios (auto-approve, manual-approve denied/
//     approved, approval timeout, refund failure rollback, partial
//     post-refund rollback, multi-channel cross-channel return) so
//     the Epic 7 acceptance "auto-approve threshold correctly
//     applied; supplier RMA initiated for eligible returns; saga
//     deterministic on Temporal replay" is reproducible from a
//     single runnable test using the
//     temporal.testsuite.WorkflowTestSuite environment;
//   - drive the full v3.8.0 EC-9-3 ROIHandler through 5 scenarios
//     (90-day x 100K-orders p95, dead-stock filter, multi-tenant
//     isolation, by-channel breakdown sums, top-20 ordering) so
//     the Epic 9 acceptance "p95 <300ms over 90-day window with
//     100K orders; dead-stock filter identifies slow-movers
//     correctly" is reproducible from a single runnable test using
//     a deterministic in-memory ROIRepository seed (the optional
//     EXPLAIN ANALYZE artefact lives at tests/integration/v381/
//     explain/roi_heatmap_90day.txt for production-shape evidence);
//   - extend the v3.8.0 goleak coverage so the lifecycle.Manager +
//     workerpool drain at end-of-test releases every goroutine
//     spawned by the in-memory eventbus dispatch loop, the
//     status-propagator retry sleep helpers, the carrier webhook
//     httptest servers, the Temporal test workflow environment, and
//     the ROI in-memory repository seed loader;
//   - reuse the v2.10 internal/lifecycle.Manager + internal/
//     workerpool drain pattern so the goleak check at the cmd/*
//     level stays representative of production composition;
//   - reuse the v3.8.0 EC-7-* + EC-9-3 packages directly
//     (fulfilment.ShippingLabelGenerator, fulfilment.StatusPropagator,
//     workflow.ReturnsSagaWorkflow, handler.ROIHandler) without
//     re-implementing any production logic; tests inject fakeClock
//   - stub carriers + stub channels + in-memory ROI repo as the
//     only test doubles.
//
// Operational notes (per recent sprint lessons):
//   - testcontainers-go on MacBook needs TESTCONTAINERS_RYUK_DISABLED=1
//     when the production-scale ROI scenario (100K orders) opts into
//     postgres via the integration_pg path; the default v381_smoke
//     run uses an in-memory ROIRepository so the suite stays hermetic
//     under the public-repo-gate.
//
// Run locally:  go test -tags v381_smoke -race -p 4 -v ./tests/integration/v381/...
// CI:           agentic-ecommerce v3.8.1 QA gate (Tasks 1 + 2 + 3 + 4).
//
// Per-scenario tables (cost / SLA / propagation latency / saga
// deterministic-replay / ROI p95) are emitted to stdout via t.Log
// so PR reviewers can copy-paste the artefact straight into the
// CHANGELOG / sprint retro / EvoLoop capsule.
//
// Cite skills: temporal-developer (deterministic Temporal test
// composition); go-clean-architecture (the suite wires real ports;
// fakes only the carrier clients, channel updaters, and wall clock);
// go-performance-optimization (queue propagation budget validated
// with fast-forward fake clock rather than real-time sleeps).
package v381

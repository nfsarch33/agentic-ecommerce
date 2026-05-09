//go:build v361_smoke

// Package v361 contains the v3.6.1 customer service + analytics QA
// validation integration tests. The v3.6.1 sprint scope is QA-only;
// the production code shipped in v3.6.0 (PR #90). These tests gate
// behind the `v361_smoke` build tag because they:
//
//   - drive the full v3.6.0 EC-8-1 enquiry classifier through a
//     curated 50-fixture bilingual corpus (20 EN + 20 zh-cn + 5
//     zh-tw + 5 mixed/edge) so the Epic 8 acceptance ">=85%
//     accuracy" is reproducible from a single runnable test;
//   - drive the full v3.6.0 EC-8-3 messaging webhook + EC-8-1/-2
//     classifier + responder pipeline through 8 inbound -> reply
//     E2E scenarios (TikTok + Facebook channels; high/medium/low
//     confidence; negative-urgent escalation; bilingual zh-cn
//     refund; idempotent retry; LLM-unavailable rule fallback) so
//     the Epic 8 acceptance "<=30s end-to-end latency" is
//     reproducible from a single runnable test;
//   - drive the full v3.6.0 EC-9-1 GMV handler through 5 extended
//     load scenarios (100K orders / 1M orders / 100 concurrent
//     tenants / by-channel breakdown / by-product top-20) so the
//     Epic 9 acceptance "p95 <200ms over a 30-day window" is
//     reproducible at scale beyond the v3.6.0 10K baseline;
//   - drive the full v3.6.0 EC-9-2 SSE handler through 6 extended
//     load scenarios (100 concurrent connections / 1000-event
//     burst / slow-consumer drop-oldest / 30s heartbeat / 10x10
//     tenant isolation / mid-stream disconnect cleanup) so the
//     Epic 9 acceptance "<=1s SSE latency + per-client 100-event
//     buffer + 30s heartbeat" is reproducible at scale beyond the
//     v3.6.0 single-connection baseline;
//   - reuse the v2.10 internal/lifecycle.Manager + internal/workerpool
//     drain pattern so the goleak check at the cmd/* level stays
//     representative of production composition;
//   - reuse the v3.5.1 hermetic httptest scaffold + the v3.5.0
//     in-memory event bus so the suite has zero live network
//     dependency.
//
// Run locally:  go test -tags v361_smoke -race -p 4 -v ./tests/integration/v361/...
// CI:           agentic-ecommerce v3.6.1 QA gate (Tasks 1 + 2 + 3 + 4).
//
// Per-fixture confusion matrix + per-scenario latency rows are
// emitted to stdout via t.Log so PR reviewers can copy-paste the
// artefact straight into the CHANGELOG / sprint retro.
//
// Cite skills: temporal-developer (deterministic test composition);
// go-clean-architecture (the suite wires real ports; fakes only
// the LLM, FAQ store, GMV repository, and SSE subscriber); and
// go-performance-optimization (latency budgets validated with
// p50/p95/p99 sampling rather than just averages).
package v361

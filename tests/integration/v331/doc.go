//go:build v331_smoke

// Package v331 contains the v3.3.1 EC-3 TikTok Shop QA validation
// integration tests. The v3.3.1 sprint scope is QA-only; the
// production code shipped in v3.3.0 (PR #84). These tests gate
// behind the `v331_smoke` build tag because they:
//
//   - drive a synthetic ProductEnrichedEvent through the full v3.3.0
//     publish path (eventbus -> TikTokListingAgent -> social.Client
//     httptest sandbox) so the EC-3-2 acceptance criterion ("synthetic
//     enriched product event -> listing created in TikTok Shop sandbox
//     within 15s; rollback confirmed on injected API failure") is
//     reproducible from a single runnable test;
//   - capture per-step latencies + a 5-bucket histogram so the PR
//     reviewer can confirm p95 <= 15s without re-running the suite;
//   - reuse the v2.10 internal/lifecycle.Manager + internal/workerpool
//     drain pattern so the goleak check at the cmd/* level stays
//     representative of production composition;
//   - reuse the v3.3.0 cassette + httptest scaffold patterns from
//     internal/adapter/social/tiktok_shop_client_test.go so the
//     sandbox stays hermetic (no live network egress).
//
// Run locally:  go test -tags v331_smoke -race -p 4 -v ./tests/integration/v331/...
// CI:           agentic-ecommerce v3.3.1 QA gate (Phase B Task 1).
//
// Per-event evidence + per-step latency lines are emitted to stdout
// via t.Log so PR reviewers can copy-paste the artefact straight
// into the CHANGELOG / sprint retro.
//
// Cite skill: go-clean-architecture (the test wires real ports;
// fakes only the HTTP transport at the composition root).
package v331

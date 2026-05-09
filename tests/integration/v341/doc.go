//go:build v341_smoke

// Package v341 contains the v3.4.1 cross-channel multichannel
// validation integration tests. The v3.4.1 sprint scope is QA-only;
// the production code shipped in v3.4.0 (PR #86). These tests gate
// behind the `v341_smoke` build tag because they:
//
//   - drive a synthetic ProductEnrichedEvent through the full v3.4.0
//     fan-out path (eventbus -> ChannelRouter -> workerpool ->
//     4 stub adapters [TikTok cassette + RedNote bridge stub +
//     Facebook cassette + Instagram stub]) so the EC-4-3 multichannel
//     fan-out acceptance ("1 product -> TikTok + RedNote + FB +
//     Instagram simultaneously, p95 fan-out latency < 5s") is
//     reproducible from a single runnable test;
//   - capture per-channel + per-event latencies + a 5-bucket
//     histogram so the PR reviewer can confirm p95 <= 5s without
//     re-running the suite;
//   - reuse the v2.10 internal/lifecycle.Manager + internal/workerpool
//     drain pattern so the goleak check at the cmd/* level stays
//     representative of production composition;
//   - reuse the v3.3.1 sandbox + httptest scaffold patterns so the
//     cassette stays hermetic (no live network egress).
//
// Run locally:  go test -tags v341_smoke -race -p 4 -v ./tests/integration/v341/...
// CI:           agentic-ecommerce v3.4.1 QA gate (Phase B Task 1).
//
// Per-event evidence + per-channel latency lines are emitted to
// stdout via t.Log so PR reviewers can copy-paste the artefact
// straight into the CHANGELOG / sprint retro.
//
// Cite skill: go-clean-architecture (the test wires real ports;
// fakes only the HTTP transport at the composition root + the
// channel.ChannelAdapter stubs per channel).
package v341

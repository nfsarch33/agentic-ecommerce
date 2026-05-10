//go:build v371_smoke

// Package v371 contains the v3.7.1 uiauto hardening QA validation
// integration tests. The v3.7.1 sprint scope is QA-only; the
// production code shipped in v3.7.0 (PR #92, EC-10-1..EC-10-5).
// These tests gate behind the `v371_smoke` build tag because they:
//
//   - drive the full v3.7.0 EC-10-2 OmniParser memory guard through
//     6 memory-pressure scenarios (baseline / single inference /
//     batch-5 concurrent / batch-10 concurrent / at-ceiling
//     pressure / persistent-failure cascade) so the Epic 10
//     acceptance "memory budget holds GREEN under inference batch"
//     is reproducible from a single runnable test against a mock
//     omniparser-bridge httptest server;
//   - drive the full v3.7.0 EC-10-3 stealth-pacing rate limiter
//     through 5 drain scenarios (20 RedNote burst / 20 TikTok
//     burst / mixed-channel storm / drain-on-overflow eviction /
//     replay-protection nonce TTL) using a fake clock so the
//     2-min and 5-min pacing budgets fast-forward without
//     real-time sleeps; the Epic 10 acceptance "20-post burst
//     drains at correct rate; zero requests dropped under cap" is
//     reproducible from a single runnable test;
//   - drive the full v3.7.0 EC-10-4 CAPTCHA detection + operator-
//     alert + resume webhook path through 7 end-to-end scenarios
//     (EN reCAPTCHA / CN 验证码 / Cloudflare WAF / operator
//     resolution / resolution timeout / invalid event_id /
//     multi-tenant isolation) so the Epic 10 acceptance "CAPTCHA
//     paused; operator alert emitted; resume webhook restores
//     pipeline" is reproducible from a single runnable test
//     against a mock omniparser-bridge + httptest CAPTCHA pages;
//   - extend the v3.7.0 goleak coverage so the lifecycle.Manager +
//     workerpool drain at end-of-test releases every goroutine
//     spawned by the in-memory eventbus dispatch loop, the rate-
//     limit jitter goroutines, the CAPTCHA pending-resolution
//     channels, and the mock omniparser-bridge httptest servers;
//   - reuse the v2.10 internal/lifecycle.Manager + internal/
//     workerpool drain pattern so the goleak check at the cmd/*
//     level stays representative of production composition;
//   - reuse the v3.7.0 EC-10-* packages directly (memguard,
//     ratelimit, captcha) without re-implementing any production
//     logic; tests inject fakeClock + fakeReader + httptest
//     servers as the only test doubles.
//
// Run locally:  go test -tags v371_smoke -race -p 4 -v ./tests/integration/v371/...
// CI:           agentic-ecommerce v3.7.1 QA gate (Tasks 1 + 2 + 3).
//
// Per-scenario tables (RSS deltas / drop counts / pause+resume
// latencies) are emitted to stdout via t.Log so PR reviewers can
// copy-paste the artefact straight into the CHANGELOG / sprint
// retro / Tier 2 promotion ADR.
//
// Cite skills: temporal-developer (deterministic test composition);
// go-clean-architecture (the suite wires real ports; fakes only
// the MemReader, the bridge HTTP transport, and the wall clock);
// and go-performance-optimization (queue-drain p95 budget
// validated with fast-forward fake clock rather than real-time
// sleeps).
package v371

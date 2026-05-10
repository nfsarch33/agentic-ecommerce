//go:build v421_smoke

// Package v421 contains the v4.2.1 payment foundation QA validation
// integration tests. The v4.2.1 sprint scope is QA-only; the
// production code shipped in v4.2.0 (PaymentGateway port + Stripe +
// Alipay + WeChat + Payment Saga). These tests gate behind the
// `v421_smoke` build tag because they:
//
//   - drive the full order→payment→fulfilment→delivery cycle through
//     each provider (Stripe, Alipay, WeChat Pay) × 2 scenarios
//     (success + declined) = 6 end-to-end tests;
//   - verify idempotency: same order_id → same payment result;
//   - verify tenant isolation: tenant A's payment config does not
//     leak to tenant B;
//   - reuse the v3.8.0 returns_saga pattern for saga compensation
//     verification.
//
// Run locally:  go test -tags v421_smoke -race -p 4 -v ./tests/integration/v421/...
// CI:           agentic-ecommerce v4.2.1 QA gate.
package v421

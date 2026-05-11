# Payment Sandbox Readiness — v6.3.0 Operational Gate

Status: BLOCKED — sandbox merchant credentials unavailable on developer
laptop. CI continues to validate Alipay and WeChat Pay adapters via the
existing `httptest.Server` fixture path (acts as the v5.1.0 cassette
equivalent: deterministic, hermetic, signature-validated). No regression.

Closes ADR-032 CF #3 in `documented operational gate` mode.

## Decision

We do NOT execute live Alipay or WeChat Pay sandbox transactions in
v6.3.0. The hard requirements for live execution are:

1. Alipay Open Platform sandbox merchant + RSA2 keypair, sourced
   through the approved `runx` environment surface.
2. WeChat Pay v3 sandbox merchant ID + APIv3 key + RSA private key,
   sourced through the approved `runx` environment surface.

Inspection of the operator-managed 1Password vault at the start of Pair
3 MVP found zero merchant sandbox items. We will not put sandbox secrets
on shell argv, in test fixtures, or in environment files committed to
the repo. Stdin injection through the approved `runx` surface remains
the only acceptable path; until the vault is populated, the live path is
gated off.

## Existing CI Coverage (no regression)

The following test files exercise the full Alipay and WeChat Pay adapter
paths against an in-process `httptest.Server` that signs and validates
the same RSA2 / RSA-SHA256 / HMAC-SHA256 envelopes as the live gateway:

- `internal/adapter/payment/alipay_adapter_test.go`
  - charge success / failure
  - refund success / failure
  - webhook signature verify
  - status query
- `internal/adapter/payment/wechat_adapter_test.go`
  - charge with v3 RSA-SHA256 envelope
  - refund with HMAC-SHA256 v2 fallback
  - webhook verify
- `internal/adapter/payment/v530_table_test.go`
  - cross-provider table sweep (Stripe + Alipay + WeChat + PayPal)
- `internal/adapter/payment/v520_coverage_test.go`
  - error path coverage

These tests run on every PR with `runx go test --repo ecommerce -- -race
-p 4 -count=1 ./...` and gate Sentrux Quality. They are the v5.1.0
cassette equivalent in spirit: hermetic, deterministic, signature-pinned.
The wire shapes and signing canonicalisation are pinned by these tests
and fail loudly on adapter drift.

## Live-Sandbox Re-Activation Procedure (when credentials arrive)

1. Populate operator vault items for Alipay and WeChat sandbox
   credentials.
2. Add a new build tag `live_payment_sandbox`-gated test file at
   `internal/adapter/payment/sandbox_live_test.go` that:
   - Skips when `op read` is unavailable or vault items missing.
   - Reads each secret through the approved stdin-injection path and
     `t.Setenv` (no argv).
   - Executes auth + capture + refund flows end-to-end.
3. Run via `runx go test --repo ecommerce -- -tags=live_payment_sandbox
   ./internal/adapter/payment/...` ad-hoc; never in default CI.
4. On success, log a capsule snapshot and update this doc to remove the
   BLOCKED status.

## Carry-Forward

CF #3 from ADR-032 stays OPEN until the live path is exercised at least
once with logged evidence. v6.3.0 closes it in the `documented
operational gate` mode and the existing httptest envelope coverage
remains the regression-prevention surface.

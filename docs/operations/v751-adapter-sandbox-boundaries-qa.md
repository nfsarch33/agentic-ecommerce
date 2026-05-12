# v7.5.1 Adapter Sandbox Boundaries QA

**Recorded**: 2026-05-12T14:25:00+10:00  
**Pair**: v7 Pair 6 QA/Retro  
**Branch**: `qa/v751-adapter-sandbox-boundaries`

## Scope

This QA slice validates the v7.5.0 adapter-hardening MVP by pinning the
mock/live boundary for payment, carrier, and social adapters. This QA slice
makes no live sandbox calls. Live execution remains operator-gated because
merchant, carrier, and social account credentials are not available in the
default developer or CI environment.

## Boundary Documents

- `docs/operations/payment-sandbox-readiness.md`
- `docs/operations/carrier-sandbox-readiness.md`
- `docs/operations/social-sandbox-readiness.md`

## QA Findings

- Payment readiness docs were Alipay/WeChat-focused and now cover Stripe,
  PayPal, Alipay, and WeChat.
- Carrier readiness docs already captured the blocked-live posture; QA now pins
  the `EC_AUSPOST_SANDBOX` and `EC_DHL_SANDBOX` toggles plus the v7.5.0 retry
  evidence boundary.
- Social readiness did not have a single mock/live readiness document. QA added
  one for tiktok, facebook, rednote, woocommerce, instagram, and pinterest.
- Instagram and Pinterest operations docs still described the old stubs. QA
  updated them to reflect the production adapters and the injected
  `httptest.Server` test boundary.

## Validation Plan

The default validation path stays credential-free:

```text
go test ./tests/quality -run TestV751 -count=1
go test ./internal/adapter/payment ./internal/adapter/carrier ./internal/adapter/social ./tests/quality -count=1
go test -race ./internal/adapter/payment ./internal/adapter/carrier ./internal/adapter/social ./tests/quality -count=1
go test -race -p 1 -count=1 ./...
go test -covermode=atomic -coverprofile=coverage.out -p 1 -count=1 ./...
govulncheck ./...
cursor-tools docs-check --repo .
runx shell-leak-scan --root . --include-docs
sentrux gate .
git diff --check
```

No live sandbox calls are part of this QA slice. Future build-tagged live tests
must remain operator-gated.

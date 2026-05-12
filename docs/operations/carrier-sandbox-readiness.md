# Carrier Sandbox Readiness -- v7.5.1 QA refresh

Status: BLOCKED — sandbox carrier credentials unavailable on developer
laptop. CI continues to validate AusPost and DHL adapters via the
existing `httptest.Server` fixture path (acts as the v5.1.0 cassette
equivalent: deterministic, hermetic, HMAC-validated). No regression.

Closes ADR-032 CF #4 in `documented operational gate` mode.

## v7.5.1 QA refresh

Pair 6 QA revalidated the carrier mock/live boundary after the v7.5.0 adapter
hardening slice in `docs/operations/v750-adapter-hardening.md`. The default
test path remains credential-free and hermetic. Live AusPost and DHL sandbox
calls are operator-gated until credentials, runx aliases, and budget approval
exist.

## Mock / Live Boundary Matrix

| Provider | Default runtime boundary | Hermetic CI coverage | Live sandbox gate |
| --- | --- | --- | --- |
| AusPost | `EC_AUSPOST_SANDBOX` defaults to sandbox; tests inject `BaseURL` and `HTTPClient`. | `internal/adapter/carrier/auspost_client_test.go` and `internal/adapter/carrier/v530_table_test.go`. | `live_carrier_sandbox`, operator-gated. |
| DHL | `EC_DHL_SANDBOX` defaults to sandbox; tests inject `BaseURL`, `HTTPClient`, and token source. | `internal/adapter/carrier/dhl_client_test.go` and `internal/adapter/carrier/v530_table_test.go`. | `live_carrier_sandbox`, operator-gated. |

The v7.5.0 retry work routes both adapters through shared retry transport, but
does not change the live-call boundary: default CI remains mocked, local, and
deterministic.

## Decision

We do NOT execute live AusPost or DHL sandbox tracking + label
generation in v6.3.0. The hard requirements for live execution are:

1. AusPost MyPost Business / Shipping API sandbox API key + HMAC secret,
   sourced through the approved `runx` environment surface.
2. DHL XML / MyDHL API sandbox account + bearer token, sourced through
   the approved `runx` environment surface.

Inspection of the operator-managed 1Password vault at the start of
Pair 3 MVP found zero carrier sandbox items. AusPost / DHL sandbox URLs
and credentials will only be added via `runx config` (alias-only argv)
and stdin-injected secrets. Until the vault is populated and the runx
alias is registered, the live path is gated off.

## Existing CI Coverage (no regression)

The following test files exercise the full AusPost and DHL adapter
paths against an in-process `httptest.Server` that signs and validates
the same HMAC-SHA256 envelopes as the live gateway:

- `internal/adapter/carrier/auspost_client_test.go`
  - quote flow (HMAC-signed request)
  - label generation (HMAC-signed request, decoded response)
  - HMAC verifier round-trip
- `internal/adapter/carrier/dhl_client_test.go`
  - quote + label generation flows
  - error branch coverage (5xx / 4xx classification)
- `internal/adapter/carrier/v530_table_test.go`
  - cross-carrier table sweep
- `internal/adapter/carrier/error_branches_test.go`
  - typed sentinel error coverage
- `tests/integration/v461/carrier_smoke_test.go`
  - end-to-end carrier+fulfilment slice
- `tests/integration/v381/shipping_label_acceptance_test.go`
  - label generation acceptance

These tests run on every PR with the backend race gate and Sentrux Quality.

## Live-Sandbox Re-Activation Procedure (when credentials arrive)

1. Register runx aliases (no raw hosts on argv):
   - `runx config set carrier.auspost.sandbox-url` -> alias only.
   - `runx config set carrier.dhl.sandbox-url` -> alias only.
2. Populate operator vault items for the AusPost and DHL sandbox
   credentials.
3. Add a new build tag `live_carrier_sandbox`-gated test file at
   `internal/adapter/carrier/sandbox_live_test.go` that:
   - Skips when `op read` is unavailable or vault items missing.
   - Reads each secret through the approved stdin-injection path and
     `t.Setenv` (no argv).
   - Executes quote + label generation + tracking lookup flows.
4. Run via `runx go test --repo ecommerce -- -tags=live_carrier_sandbox
   ./internal/adapter/carrier/...` ad-hoc; never in default CI.
5. On success, log a capsule snapshot and update this doc.

## Carry-Forward

CF #4 from ADR-032 stays OPEN until the live path is exercised at least
once with logged evidence. v6.3.0 closes it in the `documented
operational gate` mode and the existing httptest envelope coverage
remains the regression-prevention surface.

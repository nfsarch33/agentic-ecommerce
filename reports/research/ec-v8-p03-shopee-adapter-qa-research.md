# EC v8 Pair 3 QA Research -- Shopee Adapter

**Date**: 2026-05-12
**Scope**: Shopee adapter QA, sandbox boundary, fixture hygiene, shared-engine retry/DLQ integration, and no-live-call guard.
**Branch**: `qa/v8-p03-shopee-adapter`

## Evidence Inputs

- Pair 3 MVP adapter: `internal/adapter/shopee`.
- Pair 3 MVP research: `reports/research/ec-v8-p03-shopee-adapter-research.md`.
- Pair 1 shared sync engine: `internal/marketplacesync`.
- Pair 2 Shopify QA pattern: `internal/adapter/shopify/client_qa_test.go`.

## QA Decision

Shopee live access needs partner credentials and seller authorization. Until a
partner-console sandbox is available and captured as durable evidence, the
adapter should be difficult to point at official Shopee hosts by accident.

Add a config-level no-live-call guard:

- local `httptest` and explicit mock/sandbox hosts remain allowed,
- official Shopee domains are rejected by default,
- live/sandbox official hosts require an explicit `AllowLiveBaseURL` flag,
- the shared marketplace engine still owns retry and DLQ behavior.

## QA Test Plan

- RED: official Shopee base URL is rejected by default.
- GREEN: explicit opt-in allows the official base URL for sandbox/live use.
- Verify default HTTP client timeout is bounded.
- Scan committed Shopee fixtures for credential and live-host markers.
- Prove Shopee API errors retry through `marketplacesync.Engine` into DLQ.

## Carry-Forward

- Capture official Shopee product add/update payload docs or partner-console
  screenshots before enabling live sandbox calls in CI.
- Add replay cassettes only after they can be scrubbed and committed without
  partner ids, access tokens, shop ids, signatures, or real shop URLs.

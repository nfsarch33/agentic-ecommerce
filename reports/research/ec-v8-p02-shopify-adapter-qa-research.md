# EC v8 Pair 2 QA Research -- Shopify Adapter

**Date**: 2026-05-12  
**Branch**: `qa/v8-p02-shopify-adapter`  
**Scope**: Shopify adapter QA, sandbox/mock boundary, cassette hygiene, retry/DLQ integration, and resource safety.

## Evidence Reviewed

- Pair 2 MVP research: `reports/research/ec-v8-p02-shopify-adapter-research.md`.
- Pair 2 MVP evidence: `docs/operations/v8-p02-shopify-adapter.md`.
- Shopify adapter package: `internal/adapter/shopify`.
- Shared retry/DLQ engine: `internal/marketplacesync`.
- Existing no-live-call cassette practice: `internal/adapter/china/testdata/cassettes/README.md`.
- Existing request timeout/resource discipline: `internal/httpclient`, `internal/middleware/request_budget.go`, `internal/memwatch/backpressure.go`.

## QA Decisions

- Keep QA local and deterministic. No live Shopify Admin API calls, no OAuth flow, no real shop domain, no token capture.
- Use `httptest` for request/response contract verification and shared engine integration.
- Add fixture hygiene tests that scan committed Shopify testdata for credential markers and live-shop indicators.
- Prove the adapter composes with `marketplacesync.Engine` by driving GraphQL `userErrors` through retry exhaustion into DLQ.
- Add a bounded default HTTP timeout when callers do not supply a custom client. This prevents accidental unbounded network waits and aligns with the v8 OOM/resource discipline.

## Acceptance

- RED timeout/resource-safety test fails before implementation.
- QA tests pass with no live network access.
- Focused race tests and full backend gates remain green.
- QA evidence doc records sandbox/mock boundary, cassette scan, and no-live-call proof.

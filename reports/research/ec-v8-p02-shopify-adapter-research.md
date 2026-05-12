# EC v8 Pair 2 Research -- Shopify Adapter

**Date**: 2026-05-12  
**Branch**: `feat/v8-p02-shopify-adapter`  
**Scope**: Shopify product sync adapter behind the shared `marketplacesync.Connector` port.

## Evidence Reviewed

- Pair 1 shared sync core: `internal/marketplacesync/`.
- Pair 1 MVP evidence: `docs/operations/v8-p01-marketplace-sync-core.md`.
- Pair 1 QA evidence: `docs/operations/v8-p01-marketplace-sync-core-qa.md`.
- Shopify GraphQL Admin API reference, latest stable `2026-04`: <https://shopify.dev/docs/api/admin-graphql/latest>.
- Shopify `productSet` mutation reference: <https://shopify.dev/docs/api/admin-graphql/latest/mutations/productSet>.
- Shopify API versioning and rate-limit references: <https://shopify.dev/docs/api/usage/versioning>, <https://shopify.dev/docs/api/usage/rate-limits>.
- Shopify duplicate webhook guidance: <https://shopify.dev/docs/apps/build/webhooks/ignore-duplicates>.

## Official Shopify Findings

- The GraphQL Admin API latest stable reference is `2026-04`; production callers should specify a stable API version and avoid release-candidate or unstable versions.
- GraphQL Admin requests are sent by HTTP `POST` to `/admin/api/2026-04/graphql.json` and require an `X-Shopify-Access-Token` header.
- `productSet` is the preferred GraphQL mutation for syncing product information from an external source into Shopify. It supports synchronous mode for immediate product response and asynchronous mode for more complex inputs.
- `productSet` supports identifiers including `customId` and `handle`; `customId` is the safest first contract for local-product identity because the EC system already has durable local entity IDs.
- GraphQL Admin API throttling is query-cost based, so this adapter should remain single-event, context-aware, and compose with the existing workerpool/backpressure patterns rather than creating internal goroutines.
- Shopify webhook consumers should expect duplicate deliveries and can use `X-Shopify-Event-Id`; Pair 2 does not add webhook ingestion, but the adapter must not weaken Pair 1 idempotency.

## Decision

Add `internal/adapter/shopify` as a small provider adapter that implements:

```go
marketplacesync.Connector
```

The adapter will:

- support product upsert only in this MVP,
- map `marketplacesync.ProductEvent` payload into `ProductSetInput`,
- use `customId` with a configurable namespace/key and the EC event `EntityID`,
- use a versioned GraphQL endpoint with default API version `2026-04`,
- return GraphQL `userErrors`, top-level GraphQL errors, HTTP failures, and context cancellation as normal connector errors so the shared engine can retry and DLQ consistently,
- avoid live calls and committed credentials; all tests use local cassettes and `httptest`.

## Non-Goals

- No live Shopify sandbox calls.
- No OAuth flow or token storage.
- No Shopify webhook ingestion.
- No batching or async product-set operation polling.
- No Shopee behavior.

## Acceptance

- RED contract tests fail before implementation.
- `internal/adapter/shopify.Client` implements `marketplacesync.Connector`.
- Tests prove endpoint versioning, access-token header, `productSet` request shape, custom ID identifier, response mapping, and user-error handling.
- Focused and full backend gates pass before PR.

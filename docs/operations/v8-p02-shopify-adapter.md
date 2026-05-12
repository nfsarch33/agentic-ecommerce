# EC v8 Pair 2 MVP -- Shopify Adapter

**Date**: 2026-05-12  
**Branch**: `feat/v8-p02-shopify-adapter`  
**Release line**: v8 Pair 2 MVP  

## Scope

This MVP adds a Shopify product sync adapter behind the shared Pair 1
`marketplacesync.Connector` port. It does not add live Shopify credentials,
OAuth, webhook ingestion, batching, Shopee support, or frontend behavior.

Implemented surfaces:

- `internal/adapter/shopify.Client`.
- `shopify.Config` with versioned GraphQL endpoint settings.
- Product upsert support using Shopify GraphQL `productSet`.
- Default Admin API version `2026-04`.
- Local EC product identity mapped through Shopify `customId`.
- Existing Shopify product IDs reused when the event carries a
  `gid://shopify/Product/...` external ID.
- GraphQL top-level errors, `userErrors`, HTTP failures, invalid payloads, and
  unsupported marketplace events returned as connector errors for the shared
  retry/DLQ engine.

## Research Evidence

Research artifact:

- `reports/research/ec-v8-p02-shopify-adapter-research.md`

Decision: keep the adapter synchronous and single-event. The shared marketplace
sync engine owns retry/DLQ semantics, while batch execution and rate-aware
concurrency remain composition-root concerns handled by existing workerpool and
resource-guard patterns.

## TDD Evidence

RED tests were added before implementation for:

- versioned Shopify GraphQL endpoint shape,
- `X-Shopify-Access-Token` header,
- `productSet` mutation request body,
- `customId` identifier from EC `EntityID`,
- existing Shopify GraphQL ID reuse,
- GraphQL `userErrors`,
- unsupported marketplace operations.

Initial RED result:

```text
runx worktree run --repo ecommerce --branch feat/v8-p02-shopify-adapter -- go test ./internal/adapter/shopify -count=1
```

Result:

```text
undefined: graphQLRequest
undefined: NewClient
undefined: Config
FAIL github.com/nfsarch33/agentic-ecommerce/internal/adapter/shopify [build failed]
```

Focused GREEN check:

```text
runx worktree run --repo ecommerce --branch feat/v8-p02-shopify-adapter -- go test ./internal/adapter/shopify ./internal/marketplacesync -count=1
```

Result:

```text
ok github.com/nfsarch33/agentic-ecommerce/internal/adapter/shopify
ok github.com/nfsarch33/agentic-ecommerce/internal/marketplacesync
```

## QA Carry-Forward

Pair 2 QA must add:

- Shopify sandbox/mock boundary documentation,
- cassette secret-scan evidence,
- no-live-call proof,
- integration matrix against the shared sync engine,
- broader retry and rate-limit failure coverage.

## MVP Gate Results

Branch-local gates run on `feat/v8-p02-shopify-adapter`:

| Gate | Result | Notes |
| --- | --- | --- |
| `git diff --check` | PASS | No whitespace errors. |
| `cursor-tools docs-check --repo .` | PASS | README freshness gate passed. |
| `runx shell-leak-scan --root <worktree> --include-docs` | PASS | 158 files scanned, no findings. |
| `go test ./internal/adapter/shopify ./internal/marketplacesync -count=1` | PASS | Focused connector/core tests passed. |
| `go test -race ./internal/adapter/shopify ./internal/marketplacesync -count=1` | PASS | Focused race tests passed. |
| `go test ./... -count=1` | PASS | Full backend package suite passed. |
| `make coverage-check` | PASS | Race coverage suite passed; total coverage 85.0%, gate 83%. |
| `make govulncheck-scan` | PASS | No vulnerabilities found. |
| `make build` | PASS | All backend binaries built on the Pair 2 branch. |
| `make compose-config-prod` | PASS | Compose config validated. |
| `make tf-fmt-check` | PASS | Terraform formatting clean. |
| `make tf-validate` | PASS | Terraform modules and stacks valid. |
| `make tf-plan-contract` | PASS | AWS ECS and GCP Cloud Run plan contracts passed. |
| `sentrux gate` | PASS | Quality 6041 -> 6037, coupling 0.04, cycles 1, god files 0; no degradation detected. |

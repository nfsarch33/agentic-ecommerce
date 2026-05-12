# EC v8 Pair 3 MVP -- Shopee Adapter

**Date**: 2026-05-12
**Branch**: `feat/v8-p03-shopee-adapter`
**Release line**: v8 Pair 3 MVP

## Scope

This MVP adds a guarded Shopee product sync adapter behind the shared Pair 1
`marketplacesync.Connector` port. It does not add live Shopee credentials,
seller authorization, webhook ingestion, batching, frontend behavior, or
Shopee QA cassettes.

Implemented surfaces:

- `internal/adapter/shopee.Client`.
- Pure HMAC-SHA256 v2 request signing helpers.
- Product upsert request mapping from `marketplacesync.ProductEvent`.
- Add-item request path for new products and update-item path when an existing
  numeric Shopee item id is supplied as `ExternalID`.
- Shop-scoped signed query parameters: `partner_id`, `timestamp`,
  `access_token`, `shop_id`, and `sign`.
- Shopee API errors and HTTP failures returned as connector errors for shared
  retry/DLQ handling.

## Research Evidence

Research artifact:

- `reports/research/ec-v8-p03-shopee-adapter-research.md`

Decision: keep the adapter synchronous and single-event. The shared marketplace
sync engine owns idempotency, retry, DLQ, replay, and metrics. Live endpoint and
payload validation remain Pair 3 QA scope because current Shopee product docs
were not available as stable machine-readable public pages in this tool surface.

## TDD Evidence

RED tests were added before implementation for:

- v2 shop-scoped HMAC canonical signing,
- signature verification mismatch handling,
- signed product request query parameters,
- product payload mapping,
- existing Shopee item id reuse,
- Shopee API error propagation,
- unsupported marketplace operation rejection.

Initial RED result:

```text
runx worktree run --repo ecommerce --branch feat/v8-p03-shopee-adapter -- go test ./internal/adapter/shopee -count=1
```

Result:

```text
undefined: NewClient
undefined: Config
undefined: productPayload
undefined: SignRequest
FAIL github.com/nfsarch33/agentic-ecommerce/internal/adapter/shopee [build failed]
```

Focused GREEN check:

```text
runx worktree run --repo ecommerce --branch feat/v8-p03-shopee-adapter -- go test ./internal/adapter/shopee ./internal/marketplacesync -count=1
```

Result:

```text
ok github.com/nfsarch33/agentic-ecommerce/internal/adapter/shopee
ok github.com/nfsarch33/agentic-ecommerce/internal/marketplacesync
```

Focused race check:

```text
runx worktree run --repo ecommerce --branch feat/v8-p03-shopee-adapter -- go test -race ./internal/adapter/shopee ./internal/marketplacesync -count=1
```

Result:

```text
ok github.com/nfsarch33/agentic-ecommerce/internal/adapter/shopee
ok github.com/nfsarch33/agentic-ecommerce/internal/marketplacesync
```

## MVP Gate Results

Branch-local gates run on `feat/v8-p03-shopee-adapter`:

| Gate | Result | Notes |
| --- | --- | --- |
| `git diff --check` | PASS | No whitespace errors. |
| `cursor-tools docs-check --repo .` | PASS | README freshness gate passed. |
| `runx shell-leak-scan --root <worktree> --include-docs` | PASS | 162 files scanned, no findings. |
| `go test ./internal/adapter/shopee ./internal/marketplacesync -count=1` | PASS | Focused connector/core tests passed. |
| `go test -race ./internal/adapter/shopee ./internal/marketplacesync -count=1` | PASS | Focused race tests passed. |
| `go test ./... -count=1` | PASS | Full backend package suite passed. |
| `make coverage-check` | PASS | Race coverage suite passed; total coverage 84.9%, gate 83%. |
| `make govulncheck-scan` | PASS | No vulnerabilities found. |
| `make build` | PASS | All backend binaries built on the Pair 3 branch. |
| `make compose-config-prod` | PASS | Compose config validated. |
| `make tf-fmt-check` | PASS | Terraform formatting clean. |
| `make tf-validate` | PASS | Terraform modules and stacks valid. |
| `make tf-plan-contract` | PASS | AWS ECS and GCP Cloud Run plan contracts passed. |
| `sentrux gate` | PASS | Quality 6041 -> 6039, coupling 0.04, cycles 1, god files 0; no degradation detected. |

## QA Carry-Forward

Pair 3 QA must add:

- sandbox readiness evidence,
- official product payload field capture or partner-console screenshot evidence,
- replay cassette fixture hygiene,
- no-live-call proof,
- shared-engine retry-to-DLQ integration,
- branch-local Sentrux gate evidence.

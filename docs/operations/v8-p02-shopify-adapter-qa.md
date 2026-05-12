# EC v8 Pair 2 QA -- Shopify Adapter

**Date**: 2026-05-12  
**Branch**: `qa/v8-p02-shopify-adapter`  
**Release line**: v8 Pair 2 QA  

## Scope

This QA branch hardens the Pair 2 Shopify adapter without adding live Shopify
credentials or frontend behavior.

QA additions:

- bounded default HTTP timeout for Shopify adapter calls,
- fixture/cassette credential-marker scan,
- no-live-call proof through `httptest` only,
- shared-engine integration proving Shopify GraphQL `userErrors` retry into DLQ,
- QA research artifact for sandbox/mock boundary decisions.

## Research Evidence

Research artifact:

- `reports/research/ec-v8-p02-shopify-adapter-qa-research.md`

## TDD Evidence

RED test:

```text
runx worktree run --repo ecommerce --branch qa/v8-p02-shopify-adapter -- go test ./internal/adapter/shopify -run TestQANewClientUsesBoundedDefaultHTTPTimeout -count=1
```

Result:

```text
default timeout = 0s, want bounded positive timeout
FAIL github.com/nfsarch33/agentic-ecommerce/internal/adapter/shopify
```

GREEN check:

```text
runx worktree run --repo ecommerce --branch qa/v8-p02-shopify-adapter -- go test ./internal/adapter/shopify ./internal/marketplacesync -count=1
```

Result:

```text
ok github.com/nfsarch33/agentic-ecommerce/internal/adapter/shopify
ok github.com/nfsarch33/agentic-ecommerce/internal/marketplacesync
```

## Sandbox And Mock Boundary

- No test uses a live Shopify shop URL.
- No test uses a live Admin API token.
- All request/response contracts use `httptest`.
- Committed fixture data lives under `internal/adapter/shopify/testdata/` and is scanned by `TestQACassettesContainNoCredentialMarkers`.
- Live OAuth, partner app review, webhook duplicate ingestion, and real sandbox calls remain follow-on operator-run work.

## QA Gate Results

Branch-local gates run on `qa/v8-p02-shopify-adapter`:

| Gate | Result | Notes |
| --- | --- | --- |
| `go test ./internal/adapter/shopify ./internal/marketplacesync -count=1` | PASS | Focused QA and core tests passed. |
| `go test -race ./internal/adapter/shopify ./internal/marketplacesync -count=1` | PASS | Focused race tests passed. |
| `go test ./... -count=1` | PASS | Full backend package suite passed after the Sentrux-driven refactor. |
| `make coverage-check` | PASS | Race coverage suite passed; total coverage 84.9%, gate 83%. |
| `make govulncheck-scan` | PASS | No vulnerabilities found. |
| `make build` | PASS | All backend binaries built. |
| `make compose-config-prod` | PASS | Compose config validated. |
| `make tf-fmt-check` | PASS | Terraform formatting clean. |
| `make tf-validate` | PASS | Terraform modules and stacks valid. |
| `make tf-plan-contract` | PASS | AWS ECS and GCP Cloud Run plan contracts passed. |
| `cursor-tools docs-check --repo .` | PASS | README freshness gate passed. |
| `runx shell-leak-scan --root <worktree> --include-docs` | PASS | 160 files scanned, no findings. |
| `sentrux <worktree> gate` | PASS | Branch-local gate: quality 6041 -> 6039, coupling 0.04, cycles 1, god files 0; no degradation detected. |

Note: `runx sentrux gate --repo ecommerce` currently targets the canonical
checkout, not a named runx worktree. For branch-local Sentrux evidence, pass the
absolute worktree path through `runx worktree run`, as recorded above.

## Carry-Forward

- Pair 3 covers Shopee official-doc capture and signing/auth tests.
- Shopify webhook duplicate ingestion remains outside Pair 2 and should reuse
  Pair 1 idempotency plus Shopify `X-Shopify-Event-Id` evidence when added.

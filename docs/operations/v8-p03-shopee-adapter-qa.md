# EC v8 Pair 3 QA -- Shopee Adapter

**Date**: 2026-05-12
**Branch**: `qa/v8-p03-shopee-adapter`
**Release line**: v8 Pair 3 QA

## Scope

This QA branch hardens the Pair 3 Shopee adapter without adding live Shopee
calls, credentials, seller authorization, webhooks, batching, or frontend
behavior.

Planned QA surfaces:

- official Shopee host no-live-call guard,
- bounded default HTTP timeout proof,
- committed fixture credential-marker scan,
- shared-engine retry-to-DLQ integration for Shopee API errors.

## Research Evidence

Research artifact:

- `reports/research/ec-v8-p03-shopee-adapter-qa-research.md`

## TDD Evidence

RED tests were added before implementation for:

- rejecting official Shopee base URLs by default,
- allowing official Shopee base URLs only with an explicit config opt-in,
- bounded default HTTP timeout,
- fixture credential hygiene,
- Shopee API errors flowing through shared retry/DLQ.

Initial RED result:

```text
runx worktree run --repo ecommerce --branch qa/v8-p03-shopee-adapter -- go test ./internal/adapter/shopee -count=1
```

Result:

```text
undefined: ErrLiveCallsDisabled
unknown field AllowLiveBaseURL in struct literal of type Config
FAIL github.com/nfsarch33/agentic-ecommerce/internal/adapter/shopee [build failed]
```

Focused GREEN check:

```text
runx worktree run --repo ecommerce --branch qa/v8-p03-shopee-adapter -- go test ./internal/adapter/shopee ./internal/marketplacesync -count=1
```

Result:

```text
ok github.com/nfsarch33/agentic-ecommerce/internal/adapter/shopee
ok github.com/nfsarch33/agentic-ecommerce/internal/marketplacesync
```

Focused race check:

```text
runx worktree run --repo ecommerce --branch qa/v8-p03-shopee-adapter -- go test -race ./internal/adapter/shopee ./internal/marketplacesync -count=1
```

Result:

```text
ok github.com/nfsarch33/agentic-ecommerce/internal/adapter/shopee
ok github.com/nfsarch33/agentic-ecommerce/internal/marketplacesync
```

## QA Gate Results

Branch-local gates run on `qa/v8-p03-shopee-adapter`:

| Gate | Result | Notes |
| --- | --- | --- |
| `cursor-tools docs-check --repo .` | PASS | README freshness gate passed. |
| `runx shell-leak-scan --root <worktree> --include-docs` | PASS | 164 files scanned, no findings. |
| `go test ./internal/adapter/shopee ./internal/marketplacesync -count=1` | PASS | Focused QA/core tests passed. |
| `go test -race ./internal/adapter/shopee ./internal/marketplacesync -count=1` | PASS | Focused race tests passed. |
| `go test ./... -count=1` | PASS | Full backend package suite passed. |
| `make coverage-check` | PASS | Race coverage suite passed; total coverage 84.9%, gate 83%. |
| `make govulncheck-scan` | PASS | No vulnerabilities found. |
| `make build` | PASS | All backend binaries built on the QA branch. |
| `make compose-config-prod` | PASS | Compose config validated. |
| `make tf-fmt-check` | PASS | Terraform formatting clean. |
| `make tf-validate` | PASS | Terraform modules and stacks valid. |
| `make tf-plan-contract` | PASS | AWS ECS and GCP Cloud Run plan contracts passed. |
| `sentrux gate` | PASS | Quality 6041 -> 6039, coupling 0.04, cycles 1, god files 0; no degradation detected. |
| `memory_pressure` | PASS | 68% free memory after heavy gates. |

## Residual Carry-Forward

- Official Shopee product add/update payload field evidence still needs a
  partner-console capture or stable public docs before live sandbox calls are
  enabled in CI.
- No live Shopee credentials, partner ids, access tokens, shop ids, signatures,
  or real shop URLs were added.

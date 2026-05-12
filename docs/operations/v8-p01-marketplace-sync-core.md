# EC v8 Pair 1 MVP -- Marketplace Sync Core

**Date**: 2026-05-12  
**Branch**: `feat/v8-p01-marketplace-sync-core`  
**Release line**: v8 Pair 1 MVP  

## Scope

This MVP adds the shared marketplace sync core only. Provider-specific
Shopify and Shopee adapters remain follow-on v8 Pair 2 and Pair 3 work.

Implemented surfaces:

- `internal/marketplacesync.Engine` for idempotent product sync.
- `Connector`, `Ledger`, `DLQ`, and `Metrics` ports for provider adapters.
- In-memory ledger and DLQ implementations for tests and local harnesses.
- Replay support that skips already-completed events without duplicate connector calls.
- Reconciliation report generation for local/remote version drift.
- Prometheus registry and observability-spine contracts for:
  - `ec_marketplace_sync_events_total`
  - `ec_marketplace_sync_dlq_total`
  - `ec_marketplace_replay_total`
- EvoMap KPI fields for marketplace sync, DLQ, and replay totals.

## Research Evidence

Research artifact:

- `reports/research/ec-v8-p01-marketplace-sync-core-research.md`

Key decision: keep the core synchronous and context-aware. Async execution,
batching, and concurrency limits should be supplied by composition roots using
the existing bounded workerpool/resource-guard patterns.

## TDD Evidence

RED tests were added before implementation for:

- duplicate event skip and no duplicate connector call,
- retry exhaustion and DLQ write,
- replay dedupe,
- reconciliation mismatch reporting,
- marketplace Prometheus metric registration,
- observability-spine inventory declaration,
- bounded-label registry metric adapter.

Focused GREEN checks:

```text
runx worktree run --repo ecommerce --branch feat/v8-p01-marketplace-sync-core -- go test ./internal/marketplacesync ./internal/metrics ./internal/observability/spine ./internal/evomap -count=1
```

Result:

```text
ok github.com/nfsarch33/agentic-ecommerce/internal/marketplacesync
ok github.com/nfsarch33/agentic-ecommerce/internal/metrics
ok github.com/nfsarch33/agentic-ecommerce/internal/observability/spine
ok github.com/nfsarch33/agentic-ecommerce/internal/evomap
```

Race check:

```text
runx worktree run --repo ecommerce --branch feat/v8-p01-marketplace-sync-core -- go test -race ./internal/marketplacesync ./internal/metrics ./internal/observability/spine ./internal/evomap -count=1
```

Result: passed.

## QA Carry-Forward

The Pair 1 QA branch must add:

- k6 retry/load matrix coverage,
- race and replay fixture audit evidence,
- broader backend gates,
- final `docs/operations/v8-p01-marketplace-sync-core-qa.md` on branch `qa/v8-p01-marketplace-sync-core`.

## MVP Gate Results

Branch-local gates run on `feat/v8-p01-marketplace-sync-core`:

| Gate | Result | Notes |
| --- | --- | --- |
| `git diff --check` | PASS | No whitespace errors. |
| `cursor-tools docs-check --repo .` | PASS | README freshness gate passed. |
| `runx shell-leak-scan --root <worktree> --include-docs` | PASS | No findings after path sanitization. |
| `go test ./... -count=1` | PASS | Full backend package suite passed. |
| `make coverage-check` | PASS | Race coverage suite passed; total coverage 85.0%, gate 83%. |
| `make govulncheck-scan` | PASS | No vulnerabilities found. |
| `make build` | PASS | All backend binaries built. |
| `make compose-config-prod` | PASS | Compose config validated. |
| `make tf-fmt-check` | PASS | Terraform formatting clean. |
| `make tf-validate` | PASS | All Terraform modules and stacks valid. |
| `make tf-plan-contract` | PASS | AWS ECS and GCP Cloud Run plan contracts passed. |
| `sentrux gate .` | PASS | Quality 6041 -> 6038, coupling 0.04, cycles 1, god files 0; no degradation detected. |

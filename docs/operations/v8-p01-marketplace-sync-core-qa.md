# EC v8 Pair 1 QA -- Marketplace Sync Core

**Date**: 2026-05-12  
**Branch**: `qa/v8-p01-marketplace-sync-core`  
**Base MVP PR**: backend PR #155, merge `4ce6b24`  

## Scope

Pair 1 QA hardens the v8 marketplace sync core added by the MVP branch.
The QA branch does not add Shopify or Shopee provider behavior; those remain
Pair 2 and Pair 3 work.

## Findings

The new concurrent duplicate-sync test exposed a real check-then-mark gap:
parallel submissions of the same event key could all call the connector before
the ledger was marked complete.

Fix:

- Added a per-event-key in-flight lock inside `internal/marketplacesync.Engine`.
- Duplicate keys serialize through the sync critical section.
- Unrelated keys can still proceed independently.
- The lock map removes idle keys when no callers are waiting, avoiding unbounded
  in-memory retention for completed keys.

## QA Additions

- `internal/marketplacesync/engine_qa_test.go`
  - concurrent duplicate sync applies exactly once,
  - retry outcome matrix,
  - replay fixture audit.
- `internal/marketplacesync/testdata/v8_p01_replay_fixture.json`
  - two deterministic DLQ replay fixtures,
  - no live marketplace calls or credentials.
- `tests/load/k6/v8_marketplace_sync_retry.js`
  - k6 retry/load matrix profile with provider/entity/retry tags,
  - health and metrics scrape surfaces only.
- `tests/load/k6/v8_marketplace_sync_retry_contract_test.go`
  - verifies required k6 matrix tokens and forbids sensitive auth/env tokens.

## TDD Evidence

RED:

```text
runx worktree run --repo ecommerce --branch qa/v8-p01-marketplace-sync-core -- go test ./internal/marketplacesync ./tests/load/k6 -count=1
```

Result before implementation:

```text
--- FAIL: TestEngineConcurrentDuplicateSyncOnlyAppliesOnce
engine_qa_test.go:58: status counts applied=8 duplicate=0, want 1/7
```

GREEN:

```text
runx worktree run --repo ecommerce --branch qa/v8-p01-marketplace-sync-core -- go test ./internal/marketplacesync ./tests/load/k6 -count=1
```

Result: passed.

Race:

```text
runx worktree run --repo ecommerce --branch qa/v8-p01-marketplace-sync-core -- go test -race ./internal/marketplacesync ./tests/load/k6 -count=1
```

Result: passed.

k6 syntax inspection:

```text
runx worktree run --repo ecommerce --branch qa/v8-p01-marketplace-sync-core -- k6 inspect tests/load/k6/v8_marketplace_sync_retry.js
```

Result: profile loads with `marketplace_sync_retry_matrix` at 20 RPS for 30s
and thresholds for p95 latency and failure rate.

## Remaining Pair Carry-Forwards

- Live k6 execution waits for a local or remote EC API target with stable
  `/healthz` and `/metrics` surfaces.
- Shopify and Shopee adapter-specific replay cassettes are deferred to Pair 2
  and Pair 3 after official/sandbox evidence is recorded.

## Final Gate Results

| Gate | Result | Notes |
| --- | --- | --- |
| `git diff --check` | PASS | No whitespace errors. |
| `cursor-tools docs-check --repo .` | PASS | README freshness gate passed. |
| `runx shell-leak-scan --root <worktree> --include-docs` | PASS | No findings. |
| `go test ./internal/marketplacesync ./tests/load/k6 -count=1` | PASS | QA-specific package and k6 contract tests passed. |
| `go test -race ./internal/marketplacesync ./tests/load/k6 -count=1` | PASS | Concurrency fix verified under race detector. |
| `k6 inspect tests/load/k6/v8_marketplace_sync_retry.js` | PASS | Matrix profile parsed with expected scenario and thresholds. |
| `make coverage-check` | PASS | Full race coverage suite passed; total coverage 85.0%, gate 83%. |
| `make govulncheck-scan` | PASS | No vulnerabilities found. |
| `make build` | PASS | All backend binaries built. |
| `make compose-config-prod` | PASS | Compose config validated. |
| `make tf-fmt-check` | PASS | Terraform formatting clean. |
| `make tf-validate` | PASS | All Terraform modules and stacks valid. |
| `make tf-plan-contract` | PASS | AWS ECS and GCP Cloud Run plan contracts passed. |
| `sentrux gate .` | PASS | Quality 6041 -> 6037, coupling 0.04, cycles 1, god files 0; no degradation detected. |

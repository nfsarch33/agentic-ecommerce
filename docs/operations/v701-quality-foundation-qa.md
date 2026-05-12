# v7.0.1 Quality Foundation QA

**Timestamp**: 2026-05-12T10:05:00+10:00  
**Branch**: `qa/v701-quality-foundation-validation`  
**Base**: `main` after PR #142 (`68fd3ce`)  
**Scope**: v7 Pair 1 QA/Retro validation for the quality-foundation MVP

## Summary

Pair 1 QA verified the v7 quality-foundation refactor after merge to main. The
new quality guard is stable under repeated race runs, the touched workflow and
Postgres test-support packages remain deterministic, and the Sentrux baseline
was refreshed to the post-refactor state.

## Validation

| Gate | Result |
|---|---|
| `go test -race -count=3 ./tests/quality ./internal/workflow ./internal/testsupport/postgres ./cmd/mc-api` | PASS |
| `go test -tags=integration_pg -count=3 ./internal/testsupport/postgres` | PASS |
| `sentrux gate .` | PASS, no degradation |
| `sentrux gate --save .` | PASS, saved `.sentrux/baseline.json` with Quality 6041 |
| `gocyclo -over 14 internal cmd` excluding `_test.go` | Remaining production entries reduced to three |

## Remaining Complexity Entries

The first-cut MVP reduced the production `gocyclo >=15` list from five entries
to three. These remain for Pair 2 or a targeted follow-up:

| Function | File | Complexity |
|---|---|---|
| `(*server).membershipsHandler` | `cmd/mc-api/membership_handlers.go` | 15 |
| `newServer` | `cmd/mc-api/main.go` | 15 |
| `(*server).dispatchAdminBillingSubscriptions` | `cmd/mc-api/billing_handlers.go` | 15 |

## Decision

Pair 1 QA accepts the MVP reduction and refreshed baseline. Do not broaden the
new AST quality guard to these three helpers until a RED cycle is written for
their target shape; otherwise the guard would turn into a broad policy change
without scoped evidence.

## Next

- Pair 2 should decide whether to reduce the remaining API/server helpers or
  focus on coverage and contract-test gaps first.
- Keep full local race runs at `-p 1` on this MacBook unless the resource probe
  indicates higher fan-out is safe.

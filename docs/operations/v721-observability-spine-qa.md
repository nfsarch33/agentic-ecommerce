# v7.2.1 Observability Spine QA

**Recorded**: 2026-05-12T11:20:00+10:00  
**Pair**: v7 Pair 3 QA/Retro  
**Branch**: `qa/v721-observability-spine-validation`

## Scope

Pair 3 QA validates the v7.2.0 observability spine after the MVP merge. The
goal is to prove the typed contracts can be replayed from EvoMap NDJSON,
matched against Prometheus `/metrics` output, and checked for dashboard/alert
field completeness.

## Findings

- Added `DecodeCapsuleSnapshots` to replay EvoMap NDJSON capsules into typed
  dashboard snapshots.
- Added `ValidateInventoryAgainstPrometheus` to compare the spine metric
  inventory against Prometheus text output.
- Added `ValidateDashboardFields` to ensure every dashboard field declared by
  the metric inventory exists in a dashboard snapshot.
- Found and fixed a real MVP gap: the metric inventory declared
  `agentrace_session_duration_seconds`, but `SnapshotFromCapsule` did not emit
  that dashboard field. The QA fix adds the field as an additive snapshot
  output.

## Validation

```text
go test ./internal/observability/spine -count=1
go test -race -count=3 ./internal/observability/spine
go test -race -count=1 ./internal/observability/spine ./internal/evomap ./internal/metrics ./internal/observability/agentrace ./internal/observability/hooks
go test -race -p 1 -count=1 ./...
go test -covermode=atomic -coverprofile=coverage.out -p 1 -count=1 ./...
GOTOOLCHAIN=local go tool cover -func=coverage.out | tail -1
govulncheck ./...
cursor-tools docsync check .
runx shell-leak-scan --root . --include-docs
sentrux gate .
git diff --check
```

Results:

- Full backend coverage held at **85.1%**.
- `govulncheck ./...`: no vulnerabilities.
- Sentrux gate: Quality 6041 -> 6040, Coupling 0.04 -> 0.04, Cycles 1, God
  files 0; no degradation detected.
- Documentation drift, docs-inclusive shell-leak, and whitespace checks passed.

## Pair 4 Carry-Forward

Pair 4 should use this spine as the evidence surface for resource-aware
orchestration: bounded worker-pool rejection counts, breaker-open counts, and
coordination conflict counts should remain dashboard-ready while async paths are
audited for context timeouts, graceful shutdown, and leak resistance.

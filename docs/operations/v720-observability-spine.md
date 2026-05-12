# v7.2.0 Observability Spine MVP

**Recorded**: 2026-05-12T10:45:10+10:00  
**Pair**: v7 Pair 3 MVP  
**Branch**: `feat/v720-observability-spine`

## Scope

This MVP adds a typed observability spine contract for the agentic e-commerce
stack. The spine gives dashboards, Prometheus metric inventory checks, and
EvoMap/EvoLoop ingestion the same stable field names without changing existing
runtime metric emitters.

## Contracts Added

- `internal/observability/spine.MetricInventory` lists dashboard-facing metric
  contracts for HTTP latency, OOM alarms, runtime heap/goroutine gauges,
  Agentrace KPIs, bounded worker pool rejections, circuit breaker open counts,
  and coordination conflicts.
- `internal/observability/spine.SnapshotFromCapsule` converts existing
  `evomap.Capsule` values into a stable dashboard snapshot schema.
- `internal/observability/spine.AgentraceMetricSamples` converts Agentrace KPI
  rollups into deterministic metric samples with bounded labels.
- `internal/evomap.KPIs` gains additive fields for `workerpool_rejected_total`,
  `breaker_open_total`, and `coord_conflicts_total` so older capsule readers
  continue to work.

## Validation

Initial MVP validation:

```text
go test ./internal/observability/spine -count=1
go test -race -count=1 ./internal/observability/spine ./internal/evomap
```

Pair QA should replay representative NDJSON capsules through the snapshot path,
compare the spine inventory against `/metrics` output, and verify alert
field names remain dashboard-ready.

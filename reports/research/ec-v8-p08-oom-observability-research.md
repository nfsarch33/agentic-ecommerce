# EC v8 Pair 8 OOM Observability Research

> Date: 2026-05-12  
> Branch: `feat/v8-p08-oom-observability`  
> Scope: resource guard alerts, Sentrux desktop process visibility, workerpool/memwatch metric coverage, and Agenttrace/EvoMap wiring.

## Sources Reviewed

- Existing runtime resource controls:
  - `internal/memwatch/memwatch.go`
  - `internal/memwatch/backpressure.go`
  - `internal/workerpool/pool.go`
  - `internal/workerpool/adaptive.go`
  - `internal/lifecycle/enhanced_shutdown.go`
- Existing observability surfaces:
  - `internal/metrics/metrics.go`
  - `internal/runtimeobs/runtimeobs.go`
  - `internal/observability/agentrace/adapter.go`
  - `internal/observability/spine/spine.go`
  - `internal/evomap/sink.go`
  - `cmd/evomap-rollup`
- Prior release evidence:
  - `docs/operations/v720-observability-spine.md`
  - `docs/operations/v721-observability-spine-qa.md`
  - `docs/operations/v730-resource-aware-orchestration.md`
  - `docs/operations/v731-resource-aware-orchestration-qa.md`
  - `docs/operations/v8-p04-image-editing-qa.md`
- Go runtime facts already used by the codebase:
  - `runtime.ReadMemStats`
  - `runtime.NumGoroutine`
  - bounded channel worker pools
  - context-bounded shutdown and writer drains

## Current Code Facts

- `memwatch.Sampler` already samples heap, heap allocation, goroutine count,
  GC count, and last GC pause.
- `runtimeobs.RuntimeObservability` already fans memwatch samples into
  Prometheus gauges and EvoMap NDJSON capsules.
- `workerpool.Pool` emits active-worker and rejection metrics through
  `PoolMetrics`.
- `workerpool.AdaptivePool` can resize based on heap pressure, but its resize
  decisions are only logged and exposed through `ResizeEvents()`.
- `metrics.Registry` already exposes OOM alarms, goroutine count, heap bytes,
  v6.2 workerpool/circuit-breaker metrics, v7 observability spine fields, and
  v8 marketplace sync counters.
- `observability/agentrace.Adapter` already emits bounded NDJSON events with a
  non-blocking ring and context-bounded `Emit`.
- EvoMap capsules already carry runtime OOM, heap, goroutine, workerpool,
  Agenttrace, and marketplace fields, but do not yet carry resource-guard alert
  counts or Sentrux desktop process pressure.

## Decisions

1. Keep Pair 8 additive and instrumentation-focused.
   - Do not change product behavior or request routing in this MVP.
   - Prefer small, typed hooks over broad composition-root rewrites.
2. Model resource pressure as bounded signals.
   - Signal labels: `heap`, `goroutine`, `sentrux_desktop`.
   - Severity labels: `warning`, `critical`.
   - No raw process command lines, hostnames, user names, paths, or credentials
     are stored in metrics or Agenttrace labels.
3. Keep Sentrux visibility injectable.
   - The backend should accept an already-counted desktop process number.
   - Actual macOS process enumeration remains a runx/cursor-tools operational
     concern so tests stay hermetic and no private shell argv leaks into app
     code.
4. Wire adaptive workerpool resizes to metrics.
   - Current active/rejected metrics are not enough for OOM tuning.
   - Expose current adaptive worker target and resize direction counts.
5. Emit Agenttrace resource alerts through the existing bounded adapter shape.
   - Alert events use `type=resource_alert`, `tool=runtimeobs`, and bounded
     labels for the signal only.
6. Extend EvoMap and the v7 spine with additive KPI fields.
   - Older capsule readers keep working because new JSON fields are optional.

## RED Targets

1. `TestV8ResourceMetricsRegisteredAndExposed`
   - Fails until the metrics registry has resource-guard alert, Sentrux desktop
     process, workerpool size, and workerpool resize metric handles and writes
     them on `/metrics`.
2. `TestAdaptivePool_EmitsResizeMetrics`
   - Fails until `AdaptivePool` accepts an adaptive metrics hook and emits both
     current target worker count and resize direction counters.
3. `TestResourceGuardObservabilityFansOutToMetricsTraceAndEvomap`
   - Fails until `runtimeobs` can evaluate a memwatch sample plus Sentrux
     process snapshot, emit Prometheus metrics, write an Agenttrace alert, and
     persist EvoMap KPI fields.
4. `TestResourceGuardKPIsAggregateAndReachSpine`
   - Fails until EvoMap aggregation and the observability spine include the new
     resource guard fields.

## QA Carry-Forward

- Pair 8 QA should run a short soak with low test ceilings and capture no-leak
  evidence.
- Pair 8 QA should verify runx/cursor-tools Sentrux desktop cleanup visibility
  from operational probes, not from local raw process scans.
- Pair 8 QA should replay the generated EvoMap NDJSON through
  `cmd/evomap-rollup` and confirm the alert fields survive aggregation.
- Pair 8 QA should record whether the full Sentrux gate targets the worktree or
  canonical checkout; use branch-local evidence if the wrapper is still
  canonical-only.

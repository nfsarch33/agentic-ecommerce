# Observability (v2.10.0)

This guide covers the resilience and observability stack added in
v2.10.0: Prometheus exposition, OpenTelemetry tracing, slog
correlation, Grafana dashboards, OOM detection, and the EvoMap
feed.

## Layers

| Layer                 | Package                       | Surface                          |
|-----------------------|-------------------------------|----------------------------------|
| Service lifecycle     | `internal/lifecycle`          | signal-driven drain, Closers     |
| Bounded concurrency   | `internal/workerpool`         | bounded `Pool`, panic isolation  |
| Memory + goroutine    | `internal/memwatch`           | runtime/MemStats sampler         |
| Per-request memcap    | `internal/middleware`         | `MemCap` middleware              |
| Prometheus metrics    | `internal/metrics`            | `ec_*` registry                  |
| Tracing + slog        | `internal/observability`      | OTel + slog correlation          |
| EvoMap feed           | `internal/evomap`             | NDJSON sink + rollup             |
| Daily rollup          | `cmd/evomap-rollup`           | 8th binary                       |

## ec_* metrics

Every binary exposes the following set on its `/metrics` endpoint
(mc-api on `:8080/metrics`, agent-worker on its sidecar
`ECOMMERCE_AGENT_WORKER_METRICS_ADDR`):

| Metric                              | Type      | Labels                                                  |
|-------------------------------------|-----------|---------------------------------------------------------|
| `ec_http_requests_total`            | counter   | `binary, tenant_id, route, method, status`              |
| `ec_http_duration_seconds`          | histogram | `binary, route, method`                                 |
| `ec_workflow_runs_total`            | counter   | `binary, workflow, status`                              |
| `ec_workflow_duration_seconds`      | histogram | `binary, workflow`                                      |
| `ec_workerpool_queued`              | gauge     | `binary, pool`                                          |
| `ec_workerpool_saturation_total`    | counter   | `binary, pool`                                          |
| `ec_oom_alarms_total`               | counter   | `binary`                                                |
| `ec_goroutine_count`                | gauge     | `binary`                                                |
| `ec_heap_bytes`                     | gauge     | `binary`                                                |

Cardinality cap defaults to 10_000 distinct label combinations per
metric. Exceeding the cap increments
`ec_metrics_series_dropped_total`.

## Heap and goroutine ceilings

`memwatch.Sampler` checks every 5 s:

- `ECOMMERCE_HEAP_CEILING_BYTES` (default 4 GiB) — heap-in-use must
  stay below this. Breach + 30 s dwell triggers
  `HeapAlarmCallback`, which by default increments
  `ec_oom_alarms_total`. Production deploys should wire this to a
  graceful `lifecycle.Manager.Shutdown()` so the binary exits before
  the OS OOM-killer arrives.
- `ECOMMERCE_GOROUTINE_CEILING` (default 50 000) — same model for
  `runtime.NumGoroutine`. Breach + 60 s dwell triggers
  `GoroutineAlarmCallback`.

## Per-request memory cap

`middleware.MemCap` rejects oversize requests:

- `ECOMMERCE_MAX_REQUEST_BYTES` (default 10 MiB) — `Content-Length`
  exceeding the cap returns HTTP 413 with a JSON body. The body
  reader is wrapped in `http.MaxBytesReader` so chunked clients
  cannot bypass.
- Per-tenant override via `MemCapConfig.TenantOverride`.

## OpenTelemetry tracing

`internal/observability` exposes the W3C trace context helpers:

- `TraceIDFromContext(ctx)` — current span's trace ID (32-char hex)
- `SpanIDFromContext(ctx)` — current span's span ID (16-char hex)
- `TraceIDFromRequest(r)` — prefers ctx, falls back to the
  `Traceparent` header
- `ParseTraceparent(header)` — pure helper for parsing
- `LoggerFromContext(ctx, base)` — returns a slog logger annotated
  with `trace_id`, `span_id`, `tenant_id` when present
- `LoggerFromRequest(r, base)` — request-scoped variant
- `RequestLogger(base)` — middleware that injects header-derived
  trace IDs into ctx

## EvoMap feed

`internal/evomap` writes NDJSON capsules to
`tests/metrics/evomap.ndjson` (rotated daily by ISO date). Schema:

```json
{
  "recorded_at": "<ISO 8601>",
  "event_at":    "<ISO 8601>",
  "binary":      "<name>",
  "tenant_id":   "<id|empty>",
  "kpis": {
    "throughput_rps":   <float>,
    "p95_ms":           <float>,
    "error_rate":       <float>,
    "oom_alarms":       <int>,
    "goroutine_count":  <int>,
    "gc_pause_p99_us":  <float>,
    "heap_in_use_bytes": <int>
  }
}
```

`cmd/evomap-rollup` reads the NDJSON, aggregates with
`evomap.Aggregate`, and writes a markdown capsule mirroring the
existing fleet evoloop schema. Cron via `runx`.

## Grafana dashboards

Four dashboards ship under
`monitoring/grafana/dashboards/v210/`:

- `ec-overview.json` — RED method (Rate, Errors, Duration) per binary
- `ec-tenant.json` — per-tenant deep dive with templated `tenant_id`
- `ec-workerpools.json` — queue depth + saturation rate per pool
- `ec-resilience.json` — OOM alarms, goroutine count, heap, workflow
  failure rate

## OOM alarm runbook

1. Pager fires on `increase(ec_oom_alarms_total[5m]) > 0`.
2. Dashboard `ec-resilience` shows which binary breached the heap
   ceiling.
3. `kubectl logs <pod>` filtered for `memwatch.heap_ceiling_critical`
   shows heap_in_use_bytes + dwell_ms at the time of the breach.
4. If breach is sustained, `lifecycle.Manager.Shutdown()` triggers
   a graceful exit before the OS OOM-killer arrives. Pod restarts;
   verify dashboards return to baseline within 5 minutes.
5. Inspect `ec_workerpool_queued` + `ec_workerpool_saturation_total`
   for the same window to identify the saturating workload.
6. If a single tenant is responsible, raise their per-tenant memcap
   override or rate-limit their slot.

## References

- [`internal/lifecycle/manager.go`](../internal/lifecycle/manager.go)
- [`internal/workerpool/pool.go`](../internal/workerpool/pool.go)
- [`internal/memwatch/memwatch.go`](../internal/memwatch/memwatch.go)
- [`internal/metrics/metrics.go`](../internal/metrics/metrics.go)
- [`internal/observability/tracing.go`](../internal/observability/tracing.go)
- [`internal/evomap/sink.go`](../internal/evomap/sink.go)
- [`cmd/evomap-rollup/app.go`](../cmd/evomap-rollup/app.go)

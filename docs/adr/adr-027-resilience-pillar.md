# ADR-027: Resilience and Observability Pillar (v2.10.x)

Date: 2026-05-09
Status: Accepted
Owner: nfsarch33
Implements: v2.10.0 PR #76 (`cdad8ec`), v2.10.1 chaos / bench / bridge work

## Context

The 70 GB MacBook OOM event of 2026-05-09 (mitigated for the personal
fleet via cursor-global-kb PR #138 / #139 / #140) showed that
unattended autonomous agents can saturate host memory in minutes
when their bounded-concurrency invariants drift. The same failure
mode applies to the Agentic Ecommerce backend stack: every binary
fans out goroutines for event consumers, workflow activities, agent
runtime spawns, and marketplace plugin event delivery, but until
v2.10.0 these paths used `go func` directly with no upper bound.

v2.0.1 -> v2.9.0 left the stack feature-complete but resilience-light:

- No graceful drain on SIGTERM. Postgres pools, Redis clients, and
  the Temporal worker exited mid-flight under signal. Some k8s
  deploys reported in-flight workflows lost on rolling restarts.
- No memory ceiling. A pathological tenant could allocate the host
  to OOM-killer with zero advance warning; the pod simply
  disappeared with exit code 137.
- No structured observability. `slog` lines existed but had no
  trace-context correlation; Prometheus scraping was opt-in per
  binary and the four Grafana dashboards drifted out of sync with
  the metric names.
- No EvoMap feed. The self-evolution pipeline was reading capsules
  from agentic-ecommerce-not-applicable-but-aspirational paths.

## Decision Drivers

- Every binary must reap cleanly on SIGTERM / SIGINT with in-flight
  work drained to a configurable deadline.
- No unbounded `go func()`. Bounded worker pools must be
  resource-aware and emit backpressure on saturation.
- OOM detection must fire BEFORE the kernel OOM-killer arrives, with
  a graceful shutdown path that lets the manager close pools cleanly.
- Per-handler request memory caps must shield the process from
  large-blob denial-of-service.
- Every request must be traceable end-to-end: HTTP <-> Temporal <->
  agent runtime, with `trace_id` / `span_id` / `tenant_id`
  correlated into the slog stream.
- A pluggable EvoMap feed must capture per-minute KPIs as both an
  NDJSON local sink and a Prometheus Pushgateway remote_write so
  EvoLoop-DRL can self-tune the same SLOs the operator dashboards
  watch.
- Heavy upstreams (OmniParser, MiniMax) must run on the gpu-host-1 fleet
  and be reached via signed bridges so no MacBook agent prints the
  fleet IP on argv.

## Decision

### D1: Bounded concurrency mandate

Every `go func` in the ecommerce + bridge codebases must funnel
through `internal/workerpool.Pool` (v2.10.0 Story 2). The pool's
`MaxWorkers` defaults to `clamp(NumCPU * 2, MinWorkers, MaxWorkers)`
and halves when `runtime.MemStats.HeapAlloc > MaxHeapBytes`. Queue
saturation returns `ErrPoolSaturated`, which the HTTP middleware
maps to `503 Service Unavailable` with a `Retry-After` header.

Sentrux gate enforces this: any new `go func(` outside
`internal/workerpool` or `*_test.go` files is a regression.

### D2: OOM ceiling enforcement

`internal/memwatch.Sampler` reads `runtime.MemStats` every 5 s
(configurable via `ECOMMERCE_HEAP_SAMPLE_SECONDS`) and triggers
`HeapAlarmCallback` when `HeapInuse` exceeds
`ECOMMERCE_HEAP_CEILING_BYTES` (default 4 GiB) for
`ECOMMERCE_HEAP_CEILING_DWELL` (default 30 s). The callback wires
into `lifecycle.Manager.Shutdown()`, draining every Closer in
reverse registration order before the kernel OOM-killer fires.

The same pattern applies to goroutine count
(`ECOMMERCE_GOROUTINE_CEILING`, default 50_000, dwell 60 s).

The chaos suite `tests/chaos/oom_test.go` proves the chain
end-to-end with a 16 MiB sentinel allocation against a 4 MiB
synthetic ceiling.

### D3: OpenTelemetry adoption

Every binary registers `otelhttp` middleware on its HTTP surface
and a Temporal interceptor that propagates `traceparent` across
activity calls. Exporter is OTLP gRPC to `OTEL_EXPORTER_OTLP_ENDPOINT`
(defaults to local collector; production points to fleet collector).

The slog request-scoped logger injects `trace_id`, `span_id`, and
`tenant_id` so log queries can hop into a trace view in Grafana.

Cardinality is bounded at 10_000 distinct label combinations per
metric; exceeding the bound increments
`ec_metrics_series_dropped_total`.

### D4: EvoMap dual-feed (NDJSON local + Prometheus metrics gateway)

`internal/evomap.Sink` writes one JSON line per minute per binary
to `tests/metrics/evomap.ndjson` (rotated daily by ISO date). The
new `cmd/evomap-rollup` 8th binary aggregates the NDJSON every 24 h
and writes a markdown capsule into
`~/Code/global-kb/global-memories/evoloop-capsules/` so the
EvoLoop-DRL self-tuning pipeline gets the same shape it already
consumes.

In parallel, every binary pushes the same KPI shape every 60 s to
the fleet Prometheus metrics gateway. The gateway URL is resolved
via an existing `runx` tunnel forward (alias-only on argv), never
a fleet IP literal.

### D5: Heavy-upstream offload via signed bridges

OmniParser (visual UI parsing for `cmd/uiauto-compare`) and the
Prometheus remote_write target are too heavy to run on the MacBook
and live on the gpu-host-1 fleet host. They are reached
via signed bridges:

- **`omniparser-bridge`** -- new repo `nfsarch33/omniparser-bridge`
  (v2.10.1 Task 4). HMAC-SHA256 + `crypto/subtle.ConstantTimeCompare`
  on a `<unix-secs>\n<path>\n<body>` preimage. Mirrors the proven
  `minimax-openai-bridge` pattern; 32-byte minimum secret,
  configurable replay window (default 60 s), 8 MiB inbound body cap.
- **Metrics gateway runx tunnel** -- the existing v2.10.0 forward
  resolves a runx alias to `127.0.0.1:<port>` on the MacBook. The bridge is
  pure HTTPS basic-auth; no app code needs to know the fleet
  endpoint.

Both bridges accept only signed POST traffic. Both bridges are
configured exclusively via env vars (no argv leak).

### D6: Chaos validation suite

`tests/chaos/` carries four build-tag-gated files:

- `oom_test.go` -- heap/goroutine ceiling -> graceful shutdown
- `postgres_flap_test.go` -- testcontainer Stop/Start -> pool recovery
- `redis_flap_test.go` -- testcontainer Stop/Start -> TCP recovery
- `temporal_flap_test.go` -- testcontainer Stop/Start -> frontend recovery

The chaos suite runs in CI **weekly** and on demand via the
`[chaos]` PR-label trigger. It does not run on every PR because
each test pulls a Docker image and takes 30-180 s.

## Considered Options

### Option 1: Lift bounded concurrency invariants ad-hoc

- Pros: smaller diff, lower review surface.
- Cons: misses the package-as-source-of-truth property; the next
  unbounded `go func` lands without a guardrail.

### Option 2: Wrap a third-party worker pool (e.g. `gammazero/workerpool`)

- Pros: zero-line internal package.
- Cons: adds a third-party dep with an unknown maintainer cadence;
  loses the v2.10.0 control over `runtime.MemStats`-aware sizing.

### Option 3: Use OpenTelemetry SDK only, skip Prometheus

- Pros: one observability surface.
- Cons: Grafana dashboards in the existing fleet are
  Prometheus-shaped; switching them would burn the v2.7-v2.9
  dashboard work; OTel metrics-to-Prometheus exporter is still
  evolving.

### Option 4: Ship MiniMax + OmniParser into the MacBook

- Pros: simplest topology.
- Cons: MiniMax keys must rotate on quota; OmniParser needs a GPU.
  MacBook cannot do either reliably for a 24/7 agent.

We chose the bridge-per-upstream pattern (Option 4 negated) so
heavy compute stays on gpu-host-1 and the MacBook stays a thin client.

## Consequences

### Positive

- v3.0.0 ships with a credible "we will not OOM the host" property
  and an end-to-end traceable observability surface.
- `omniparser-bridge` is reusable for any future fleet-resident
  upstream (planned v3.1: rerank model on gpu-host-2).
- Chaos suite gives v3.x sprints a "is the lifecycle invariant
  still honoured?" gate they can run before merging.

### Negative / Trade-offs

- One new internal package per Story (lifecycle, workerpool,
  memwatch, observability, evomap, metrics). Each adds ~300-500 LOC.
- `goleak.VerifyTestMain` in 5 critical packages (workflow,
  eventbus, agent, marketplace, billing) makes the test binary
  startup ~50 ms slower per package -- acceptable.
- Bridge introduces an extra hop for OmniParser calls. Latency
  budget allows ~5 ms of bridge overhead; measured at 1.2 ms in
  signing + 0.4 ms in forwarder.

### Migration

- v2.10.0 already shipped the implementation; v2.10.1 (this ADR's
  scope) ships the validation. v3.0.0 does not require any
  follow-up migration -- the resilience pillar is already live.
- Operators should verify their `ECOMMERCE_HEAP_CEILING_BYTES` and
  `ECOMMERCE_GOROUTINE_CEILING` env values match their pod limits
  before the v3.0.0 rollout.

## Evidence

- v2.10.0 implementation PR: `cdad8ec` (PR #76).
- v2.10.1 validation artefacts:
  - `tests/chaos/{oom,postgres_flap,redis_flap,temporal_flap}_test.go`
  - `tests/benchmarks/v2.10-baseline.{json,raw.json}`
  - `tests/benchmarks/v2.10-vs-v2.6-comparison.md`
  - `tests/benchmarks/v2.10-toolchain.json`
  - `nfsarch33/omniparser-bridge` initial commit `64e35b6`.
- Personal-fleet OOM mitigation (parallel work, cursor-global-kb):
  PR #138 / #139 / #140.

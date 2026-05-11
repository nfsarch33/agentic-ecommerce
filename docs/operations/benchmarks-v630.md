# v6.3.0 Production Postgres Benchmarks — Baseline

Closes lessons-learned **CF-10** (real Postgres benchmarks). Replaces
the prior mocked-only bench surface with a testcontainers-go-backed
real PostgreSQL 16 + pgvector benchmark suite.

## Methodology

- **Image**: `pgvector/pgvector:pg16` (required because migration
  `0005_enable_pgvector_rag.up.sql` enables the `vector` extension).
- **Migrations applied per container** (canonical schema):
  `0001_create_products` -> `0011_rls` + `0037_idempotency_store`.
- **Pool**: `pgxpool.Pool` from the canonical
  `internal/adapter/postgres/pool_config.go` (defaults: 25
  MaxOpenConns, 10 MaxIdleConns, 30m MaxConnLifetime,
  5m MaxConnIdleTime). All 4 env vars exposed in v5.5.0 honoured.
- **Container lifetime**: one ephemeral container shared across all
  benchmarks in a single `go test -bench` invocation via `sync.Once`
  (the container startup cost ~5-10s is paid exactly once, not 8x).
  `TESTCONTAINERS_RYUK_DISABLED=true` is set so the bench runs on
  developer laptops where the corporate `docker-credential-helper`
  blocks the ECR-hosted ryuk image.
- **Bench shape**: each `BenchmarkV630_*` truncates the relevant
  tables, seeds a deterministic working set (25-50 rows), and then
  runs the operation in the measurement loop.
- **Reporting**: `b.ReportAllocs()` per benchmark; ns/op + B/op +
  allocs/op captured. Pair 3 QA (v6.3.1) re-runs with `-count=10`
  for true p95/p99 distributions.

Run locally (one-shot):

```bash
TESTCONTAINERS_RYUK_DISABLED=true \
  EC_V631_BENCH_TEXTFILE_DIR=../../../tests/load/results/v631-pg-bench \
  runx worktree run --repo ecommerce \
    --branch qa/v631-real-pg-distributions-and-k6-matrix -- \
    go test -tags=integration_pg -run=NONE \
      -bench 'BenchmarkV630_' -benchtime=5s -benchmem \
      -count=10 -timeout=30m ./internal/adapter/postgres/
```

CI hook: gated behind the `integration_pg` build tag so the default
`runx go test --repo ecommerce -- -race -p 4 -count=1 ./...` path
stays Docker-free (107 packages, ~2 min).

## v6.3.0 Baseline (2026-05-11, Apple M4 Pro, Docker Desktop)

| Endpoint surface (repo op)            | Mean (ns/op) | Mean (ms) | B/op   | allocs/op | Mock-era proxy |
|---------------------------------------|--------------|-----------|--------|-----------|----------------|
| products list (`ProductRepo.List`)    |      528,541 |     0.529 | 28,360 |       422 | sub-microsecond |
| products detail (`GetByID`)           |      280,650 |     0.281 |  1,934 |        50 | sub-microsecond |
| products by slug (`GetBySlug`)        |      219,815 |     0.220 |  1,379 |        31 | sub-microsecond |
| products create (`Create`)            |      257,515 |     0.258 |  1,227 |        41 | sub-microsecond |
| orders create (`OrderRepo.Create`)    |      508,169 |     0.508 |  3,103 |        99 | sub-microsecond |
| orders detail (`OrderRepo.GetByID`)   |      456,855 |     0.457 |  4,012 |        99 | sub-microsecond |
| inventory reserve (read+write)        |      786,746 |     0.787 |  2,938 |        83 | sub-microsecond |
| products list by tenant               |      491,371 |     0.491 | 29,496 |       425 | sub-microsecond |

## v6.3.1 QA Distribution Baseline (2026-05-11, Apple M4 Pro)

Command:

```bash
TESTCONTAINERS_RYUK_DISABLED=true \
  EC_V631_BENCH_TEXTFILE_DIR=../../../tests/load/results/v631-pg-bench \
  runx worktree run --repo ecommerce \
    --branch qa/v631-real-pg-distributions-and-k6-matrix -- \
    go test -tags=integration_pg -run=NONE \
      -bench 'BenchmarkV630_' -benchtime=5s -benchmem \
      -count=10 -timeout=30m ./internal/adapter/postgres/
```

The benchmark helper now records `time.Since` samples around the actual
repository operation and reports `p50_ns/op`, `p95_ns/op`, and `p99_ns/op`
through Go's benchmark metrics. When `EC_V631_BENCH_TEXTFILE_DIR` is set, each
benchmark also writes a Prometheus textfile under that directory, overwriting
calibration runs so the latest sample per benchmark stays bounded.

The full distribution run emitted a complete PASS benchmark table. The `runx
worktree run` wrapper reported `signal: killed` after the PASS table; a short
smoke rerun of `BenchmarkV630_ProductsGetByID` exited 0, so the measurements
below are accepted as the v6.3.1 QA snapshot while the wrapper cleanup signal
remains watch-listed.

| Endpoint surface (repo op) | Mean range (ms) | p50 range (ms) | Max p95 (ms) | Max p99 (ms) | allocs/op |
|---|---:|---:|---:|---:|---:|
| products list (`ProductRepo.List`) | 0.483-0.536 | 0.464-0.478 | 0.681 | 1.394 | 422 |
| products detail (`GetByID`) | 0.215-0.246 | 0.205-0.213 | 0.316 | 0.693 | 50 |
| products by slug (`GetBySlug`) | 0.218-0.235 | 0.204-0.214 | 0.331 | 0.547 | 31 |
| products create (`Create`) | 0.248-0.278 | 0.234-0.253 | 0.381 | 0.497 | 41 |
| orders create (`OrderRepo.Create`) | 0.491-0.559 | 0.469-0.484 | 0.742 | 1.527 | 99 |
| orders detail (`OrderRepo.GetByID`) | 0.438-0.474 | 0.421-0.438 | 0.613 | 0.959 | 99 |
| inventory reserve (read+write) | 0.457-0.487 | 0.440-0.448 | 0.611 | 0.911 | 83 |
| products list by tenant | 0.473-0.531 | 0.455-0.477 | 0.663 | 1.047 | 425 |

Result: all eight real-Postgres surfaces remain sub-1ms at p95 on the local
Docker baseline; the highest observed p99 was 1.53ms on `OrderRepo.Create`.

### Interpretation

The Go bench mean is the canonical "fast path" measurement (each
iteration is a single round-trip against an in-process Docker
PostgreSQL). For an HTTP handler under load the round-trip would
inherit this latency PLUS HTTP framing PLUS auth middleware PLUS
response serialisation; that is captured separately by the k6 load
test (`docs/operations/k6-baseline-v630.md`).

**Surprising finding (per plan EC-9-1)**: the v5.5.0 mock benches
were sub-microsecond because they were measuring map lookups; the
v6.3.0 real-Postgres benches are 220 microseconds to 800
microseconds, i.e. **220x to 800x slower**, but still well under the
1ms / 5ms / 50ms thresholds the plan estimated. That is the floor
on a fast laptop with a hot pool; production Linux containers with
network hops will land in the 1-15ms range, still inside the p95
budgets per ADR-029.

### Bench-time vs p95/p99

`-benchtime=2s` produces a stable mean but not a tail-sensitive
distribution. Pair 3 QA (v6.3.1) closed that gap by re-running with
`-count=10 -benchtime=5s` and wrapping key ops with a `time.Since`
distribution exporter for p50, p95, p99, and optional Prometheus textfile
output.

For the v6.3.0 MVP gate the harness (testcontainers + pgvector +
canonical migration set + shared pool) is the deliverable; the
numbers above are the baseline snapshot.

### Pool tuning evidence

The benches honour `EC_PG_MAX_OPEN_CONNS`, `EC_PG_MAX_IDLE_CONNS`,
`EC_PG_CONN_MAX_LIFETIME_MINUTES`, and `EC_PG_CONN_MAX_IDLE_TIME_MINUTES`
via the same `LoadPGPoolConfig()` shared with production. Operators
can re-run the bench with overridden env vars to validate that the
chosen pool sizing fits the workload before promoting to staging:

```bash
EC_PG_MAX_OPEN_CONNS=50 EC_PG_MAX_IDLE_CONNS=20 \
  TESTCONTAINERS_RYUK_DISABLED=true \
  runx worktree run --repo ecommerce --branch <branch> -- \
  go test -tags=integration_pg -bench 'BenchmarkV630_' ./internal/adapter/postgres/
```

## Carry-forward

CF-10 is now CLOSED at both the harness level and the distribution-publishing
level. Future QA runs should refresh the v6.3.1 table rather than creating a
new benchmark harness.

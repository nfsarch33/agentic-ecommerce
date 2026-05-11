# v6.3.x k6 Load Test Baseline

Closes lessons-learned **CF-9** and ADR-032 **CF #8** at the
"installed + executed + baseline captured" level. Pair 3 QA
(v6.3.1) extends the v6.3.0 healthz smoke with the canonical
`v490_comprehensive.js` HTTP matrix.

## Tooling

```
$ k6 version
k6 v1.7.1 (commit/devel, go1.26.1, darwin/arm64)
```

`brew install k6` on this MacBook is a one-time setup; binary
lives at `/opt/homebrew/bin/k6`.

## Smoke Baseline (`v630_smoke.js` — 100 RPS x 60s)

Hits the unauthenticated `mc-api /healthz` endpoint of a
locally-running `cmd/mc-api` binary (no Docker stack required).
Establishes the floor latency baseline for HTTP framing + atomic
counter increments.

```
$ k6 run tests/load/k6/v630_smoke.js \
    -e BASE_URL=http://127.0.0.1:8080 \
    --summary-export tests/load/results/v630_smoke_summary.json
```

| Metric                | Value      |
|-----------------------|------------|
| Iterations completed  | 6,001      |
| Sustained RPS         | 100        |
| Duration              | 60 s       |
| `healthz_duration` avg| 0.71 ms    |
| `healthz_duration` p50| 0.60 ms    |
| `healthz_duration` p90| 1.00 ms    |
| `healthz_duration` p95| **1.42 ms**|
| `healthz_duration` max| 7.41 ms    |
| Error rate            | 0% (0/6001)|
| Threshold `p(95)<500ms`| PASS      |
| Threshold `errors<1%`  | PASS      |

Pass criteria from plan EC-9-1 / lessons-learned CF-9: **p95 <
500ms under 100 RPS sustained**. Achieved p95 = 1.42 ms (350x
better than budget). Healthz is the right floor metric: all stack
work (DB, RPC, business logic) lives ABOVE this floor, so any
production-path number must be > 1.4 ms; that is why the full
matrix run is the QA deliverable.

## Comprehensive Matrix (`v490_comprehensive.js` — v6.3.1 QA)

Pair 3 QA found two issues in the old v4.9.0 matrix before accepting
numbers:

1. The script still targeted stale or never-mounted routes such as
   `/api/v1/payments/charge`, `/api/v1/payments/webhook`,
   `/api/v1/coaching/tip`, `/api/v1/marketplace/commissions/report`,
   and `/api/v1/analytics/gmv/daily`.
2. The scenario rates summed to 470+ HTTP requests/s, not the planned
   100 RPS QA baseline.

The fixed matrix now targets mounted `mc-api` surfaces and defaults to
100 HTTP requests/s:

| Scenario | Route | Rate |
|---|---|---:|
| payment list | `/api/v1/payments` | 10 RPS |
| webhook registry | `/api/v1/webhooks` | 15 RPS |
| admin summary + orders | `/api/v1/admin/summary`, `/api/v1/admin/orders` | 15 RPS |
| admin channels | `/api/v1/admin/channels` | 5 RPS |
| marketplace plugins | `/api/v1/marketplace/plugins` | 10 RPS |
| tenant dashboard | `/api/v1/tenants/{tenant_id}/dashboard` | 10 RPS |
| GMV API | `/api/v1/analytics/gmv` | 20 RPS |

Important boundary: the current `cmd/mc-api` composition remains the
developer/compose runtime. It depends on Postgres, Redis, and Temporal
for readiness and adjacent services, but many HTTP handlers still use
in-memory fixture repositories until the production composition root is
promoted. The real Postgres fast-path evidence is therefore the
benchmark suite in `docs/operations/benchmarks-v630.md`; this k6
matrix measures mounted HTTP routing, middleware, serialization,
rate-limiting, and fixture-backed handler latency.

QA runbook:

```
TENANT_ID=load-test-tenant \
  k6 run tests/load/k6/v490_comprehensive.js \
    -e EC_K6_SCENARIO_DURATION=5m \
    -e BASE_URL=http://127.0.0.1:8080 \
    --summary-export tests/load/results/v631_v490_comprehensive_summary.json
```

### v6.3.1 QA Result (2026-05-11, Docker Compose, 100 RPS x 5 min)

The final matrix run exited 0. It completed 30,008 requests at
100.02 RPS with 30,008 checks passing, 0 failed checks, and 0 HTTP
errors.

| Metric | Avg | p50 | p90 | p95 | Max | Budget |
|---|---:|---:|---:|---:|---:|---:|
| payment list | 5.06 ms | 4.54 ms | 7.59 ms | **8.66 ms** | 265.95 ms | 200 ms |
| webhook registry | 4.67 ms | 4.19 ms | 6.92 ms | **7.84 ms** | 254.17 ms | 100 ms |
| admin summary + orders | 3.57 ms | 2.69 ms | 6.22 ms | **7.14 ms** | 234.11 ms | 150 ms |
| admin channels | 5.45 ms | 5.31 ms | 7.75 ms | **8.83 ms** | 166.95 ms | 100 ms |
| marketplace plugins | 5.10 ms | 4.61 ms | 7.67 ms | **8.71 ms** | 276.17 ms | 200 ms |
| tenant dashboard | 5.07 ms | 4.57 ms | 7.63 ms | **8.66 ms** | 265.62 ms | 150 ms |
| GMV API | 4.57 ms | 3.94 ms | 7.00 ms | **7.99 ms** | 285.42 ms | 35 ms |
| aggregate HTTP request | 4.48 ms | 3.88 ms | 7.07 ms | **8.06 ms** | 285.42 ms | 500 ms |

Result: all scenario p95 values are comfortably inside the encoded
thresholds. The max values show occasional quarter-second outliers on
the laptop/compose baseline, but the tail budget for Pair 3 QA is p95,
not max.

## Carry-forward

CF-9 + ADR-032 CF #8 are CLOSED for v6.3.1 at the load-test evidence
level: k6 is installed, smoke is captured, the stale matrix has route
contract coverage, and the 100 RPS x 5 min matrix distribution table
is published. The next improvement is a production composition root
that replaces fixture-backed HTTP repositories with Postgres-backed
adapters for the admin, payments, dashboard, and GMV HTTP paths.

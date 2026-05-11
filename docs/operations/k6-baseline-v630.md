# v6.3.0 k6 Load Test Baseline

Closes lessons-learned **CF-9** and ADR-032 **CF #8** at the
"installed + executed + baseline captured" level. Full-stack
matrix execution (`v490_comprehensive.js`) deferred to v6.3.1 QA
when the Docker Compose stack is brought up end-to-end.

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

## Comprehensive Matrix (`v490_comprehensive.js` — deferred to QA)

Existing canonical script at `tests/load/k6/v490_comprehensive.js`:
7 scenarios (payment_charge, webhook_normaliser, admin_mobile,
coaching_tip, commission_report, tenant_dashboard, gmv_api) at
20-100 RPS each for 2 minutes per scenario. Per-endpoint p95
thresholds already encoded in the script (35ms gmv -> 200ms
payment_charge).

Defer to v6.3.1 QA because:

1. The script targets endpoints that require the full
   `mc-api -> postgres -> redis -> temporal` stack. Standing up
   docker-compose end-to-end is the right boundary for a load
   test, not the in-memory adapter set the binary boots with by
   default.
2. The plan explicitly assigns "k6 latency distributions" to the
   Pair 3 QA (v6.3.1) task list.

QA runbook (Pair 3 QA):

```
make compose-up                      # postgres, redis, temporal,
                                     # minio, mc-api, temporal-worker
brew services start k6 || true       # binary, no daemon
TENANT_ID=load-test-tenant \
  k6 run tests/load/k6/v490_comprehensive.js \
    -e BASE_URL=http://127.0.0.1:8080 \
    --summary-export tests/load/results/v490_$(date +%s).json
```

QA capsule MUST capture:

- Per-scenario p50/p95/p99 + error rate.
- Sustained RPS achieved vs target (100 RPS x 5 min for the
  authoritative baseline).
- Threshold pass/fail per scenario; OPEN issue for any breach.
- Annotated Grafana screenshot of the matching `mc_api_*`
  Prometheus metrics from `startObservability()` over the run
  window.

## Carry-forward

CF-9 + ADR-032 CF #8 close at the "k6 installed + smoke baseline +
matrix-run runbook" level in v6.3.0. They stay OPEN at the "matrix
distributions captured" level until v6.3.1 QA publishes the full
table.

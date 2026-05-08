# v2.8.0 k6 load test profiles

These profiles exercise every bounded context introduced through v2.7.0
(catalog, order, membership, digital, marketplace, billing,
registration, marketplace-submission) at multi-tenant scale, gated
by per-surface p95 thresholds and per-tenant breakdown tags.

## Files

- `comprehensive-multi-tenant.js` -- 8 scenarios across all bounded
  contexts. Default 10 simulated tenants for 60 seconds.

## Run

Local (compose stack on port 8080):

```bash
make compose-up
BEARER_TOKEN="$(jq -r .admin_token < .local/dev-tokens.json)" \
  STRIPE_WEBHOOK_SECRET="$(jq -r .stripe_webhook_secret < .local/dev-tokens.json)" \
  MT_DURATION=60s K6_TENANTS=10 \
  k6 run --summary-export=tests/load/k6/v280/results-$(date -u +%F).json \
    tests/load/k6/v280/comprehensive-multi-tenant.js
```

Output JSON feeds into the post-run summary script (used by the
`reports/research/v280-k6-load-test-<date>.md` report).

## Per-surface p95 budgets (ms)

| Surface | p95 budget | Failure-rate gate |
|---|---|---|
| catalog | 150 | <1% |
| order | 300 | <2% |
| membership | 400 | <5% |
| digital | 400 | <5% |
| marketplace | 800 | <70% (idempotent re-install path) |
| billing | 500 | <5% |
| marketplace_submission | 600 | <5% |
| registration | 500 | <5% |

The marketplace failure-rate gate is intentionally loose because
the lifecycle scenario (install/activate/deactivate) repeats against
the same `(tenant, slug)` and the second iteration is expected to
return `409 Conflict` (idempotent state machine -- the gate exists
so we still notice if all calls fail, e.g. due to RLS regression).

## Tenant breakdown

Every request carries `surface` and `tenant` tags. The k6 summary
JSON exposes per-tag p50/p95/p99 so the post-run report can fan out
both per-surface AND per-tenant tables.

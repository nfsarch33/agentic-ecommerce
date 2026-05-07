# Full Stack Docker Compose

This stack is production-like for local and single-host validation. It keeps published ports on loopback by default, uses placeholder-only environment templates, and does not run live WooCommerce or MiniMax calls unless you explicitly provide credentials and enable the relevant profile.

## Services

- `mc-api`: Go API built from this repo and exposed on `127.0.0.1:${MC_API_HOST_PORT:-8080}`.
- `frontend`: public image `ghcr.io/nfsarch33/agentic-ecommerce-web:${WEB_IMAGE_TAG:-main}`.
- `postgres`: PostgreSQL 16 with pgvector.
- `redis`: Redis 7 for cache/session/event-bus plumbing.
- `prometheus`: scrapes `mc-api:/metrics` and loads local alert rules.
- `grafana`: provisions the Prometheus datasource and the Agentic Ecommerce overview dashboard.
- `wc-sync`: one-shot WooCommerce sync worker under the `workers` or `sync` profile. It defaults to dry-run mode.
- `content-worker`: scaffold content worker under the `workers` profile.
- `agent-worker`: v0.6.0 scheduler runtime placeholder under the `workers` profile. It exposes `/healthz` and `/metrics` on container port `8081` and is ready for the backend orchestrator package to replace its TODO scheduler hook.
- `minimax-openai-bridge`: optional placeholder under the `ai-bridge` profile. Real keys must come from local secret management, never from committed files.

## Bring-Up

```bash
cp .env.compose.example .env.compose
docker compose --env-file .env.compose -f docker-compose.yml config
docker compose --env-file .env.compose -f docker-compose.yml up -d --build
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/metrics
```

Grafana is available at `http://127.0.0.1:${GRAFANA_HOST_PORT:-3001}`. Prometheus is available at `http://127.0.0.1:${PROMETHEUS_HOST_PORT:-9090}`.

## Observability Alert Runbook

Validate the checked-in monitoring config before booting the stack:

```bash
make monitoring-validate
make compose-config-prod
make compose-workers-config
```

`make monitoring-validate` runs `promtool check config` and `promtool check rules` when `promtool` is installed, then falls back to the repository Go validation tests for Prometheus YAML and Grafana dashboard JSON coverage.

Inspect alerts locally after `compose-up`:

```bash
open http://127.0.0.1:${PROMETHEUS_HOST_PORT:-9090}/alerts
open http://127.0.0.1:${PROMETHEUS_HOST_PORT:-9090}/rules
open http://127.0.0.1:${GRAFANA_HOST_PORT:-3001}
```

Expected v0.8.0 rules:

- `AgenticEcommerceHighApiLatency`: API p95 latency above 500ms for 5 minutes.
- `AgenticEcommerceHighErrorRate`: API 5xx error rate above 1% for 5 minutes.
- `AgenticEcommerceSyncLagHigh`: WooCommerce sync lag above 5 minutes.
- `AgenticEcommerceAgentFailureRateHigh`: agent-worker failure rate above 5% for 10 minutes.
- `AgenticEcommerceComplianceFailureSpike`: backend plus worker compliance failures above the 15-minute spike threshold.

The `Agentic Ecommerce Overview` dashboard should show API latency/error rate, WooCommerce sync lag/conflicts, agent run success/failure, and compliance pass/fail rates. The worker-backed panels populate when the `workers` profile is running.

Run scaffolded workers explicitly:

```bash
docker compose --env-file .env.compose -f docker-compose.yml --profile workers up --build wc-sync content-worker agent-worker
curl http://127.0.0.1:${AGENT_WORKER_METRICS_HOST_PORT:-8082}/healthz
curl http://127.0.0.1:${AGENT_WORKER_METRICS_HOST_PORT:-8082}/metrics
```

Stop the stack while preserving named volumes:

```bash
docker compose --env-file .env.compose -f docker-compose.yml down
```

## Frontend Image

The compose file references `ghcr.io/nfsarch33/agentic-ecommerce-web`. To test a local frontend build, build and push/tag that image from the frontend repo, then set `WEB_IMAGE_TAG` in `.env.compose`. Do not point compose at a private absolute build path.

## Security Boundaries

Default `BIND_HOST=127.0.0.1` keeps every published port on loopback. Change this only behind a trusted reverse proxy or firewall.

Keep `.env.compose` untracked. The committed `.env.compose.example` contains placeholders only. Do not commit WooCommerce credentials, MiniMax keys, Grafana passwords, browser profiles, private hostnames, or internal IPs.

The backend and frontend should call MiniMax only through a fleet bridge URL. The optional bridge service is a local placeholder for wiring and health validation; leave `ECOMMERCE_AI_BRIDGE_URL`, `FLEET_AI_BRIDGE_URL`, and `MINIMAX_API_KEY_*` blank unless you are intentionally testing against a controlled local bridge.

`wc-sync` defaults to `ECOMMERCE_SYNC_DRY_RUN=true`. Do not run live WooCommerce sync from this stack until credentials, webhook secrets, and target store ownership have been reviewed.

## Media and Compliance Placeholders

Production-like compose exposes media and compliance configuration without
mounting a writable local upload volume:

```bash
ECOMMERCE_MEDIA_STORE=object
ECOMMERCE_MEDIA_BUCKET=
ECOMMERCE_MEDIA_PUBLIC_BASE_URL=
ECOMMERCE_COMPLIANCE_MIN_SEO_SCORE=70
ECOMMERCE_COMPLIANCE_MAX_IMAGE_SIZE_BYTES=5242880
ECOMMERCE_COMPLIANCE_ALLOWED_MIME_TYPES=image/jpeg,image/png,image/webp
```

Use `docker-compose.dev.yml` for filesystem-backed uploads during development.

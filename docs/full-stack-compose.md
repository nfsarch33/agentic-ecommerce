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
curl http://127.0.0.1:8080/readyz
curl http://127.0.0.1:8080/metrics
```

Grafana is available at `http://127.0.0.1:${GRAFANA_HOST_PORT:-3001}`. Prometheus is available at `http://127.0.0.1:${PROMETHEUS_HOST_PORT:-9090}`.

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

## Cloud Readiness

`mc-api` exposes three unauthenticated platform endpoints:

- `/healthz`: liveness only; it does not call Postgres, Redis, WooCommerce, or AI bridges.
- `/readyz`: readiness; configured `ECOMMERCE_DB_URL` and `ECOMMERCE_REDIS_ADDR` dependencies must pass lightweight checks or the endpoint returns `503`.
- `/metrics`: Prometheus text exposition with build metadata, HTTP request counters/duration buckets, and stack-level placeholders for sync, agent, compliance, and media dashboards.

`ECOMMERCE_READINESS_TIMEOUT` defaults to `2s`. Keep it below the load balancer health-check timeout so an unhealthy dependency fails fast. `ECOMMERCE_SHUTDOWN_TIMEOUT` defaults to `10s` and bounds graceful HTTP shutdown after SIGTERM.

`mc-api` writes structured JSON access logs with `request_id`, mirrors `X-Request-ID` to every response, and supports optional OpenTelemetry HTTP spans with `ECOMMERCE_OTEL_ENABLED=true`. Probe endpoints are excluded from spans to reduce noise.

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

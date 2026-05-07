# Full Stack Docker Compose

This stack is production-like for local and single-host validation. It keeps published ports on loopback by default, uses placeholder-only environment templates, and does not run live WooCommerce or MiniMax calls unless you explicitly provide credentials and enable the relevant profile.

## Services

- `mc-api`: Go API built from this repo and exposed on `127.0.0.1:${MC_API_HOST_PORT:-8080}`.
- `frontend`: public image `ghcr.io/nfsarch33/agentic-ecommerce-web:${WEB_IMAGE_TAG:-main}`.
- `postgres`: PostgreSQL 16 with pgvector via `pgvector/pgvector:pg16`.
- `redis`: Redis 7 for cache/session/event-bus plumbing.
- `prometheus`: scrapes `mc-api:/metrics` and loads local alert rules.
- `grafana`: provisions the Prometheus datasource and the Agentic Ecommerce overview dashboard.
- `wc-sync`: one-shot WooCommerce sync worker under the `workers` or `sync` profile. It defaults to dry-run mode.
- `content-worker`: scaffold content worker under the `workers` profile.
- `agent-worker`: scheduler runtime under the `workers` profile. It exposes `/healthz` and `/metrics` on container port `8081`, including v1.6.0 schedule-control metrics.
- `temporal`: v1.2.0 Temporal CLI dev server under the `temporal` profile. It exposes gRPC on loopback port `${TEMPORAL_GRPC_HOST_PORT:-7233}` and the Web UI on `${TEMPORAL_UI_HOST_PORT:-8233}`.
- `temporal-worker`: backend Temporal worker under the `temporal-worker` profile. It polls `ECOMMERCE_TEMPORAL_TASK_QUEUE=ec-workflows` by default and logs v1.6.0 schedule-control config.
- `n8n`: v1.5.0 local automation service under the `n8n` profile. It exposes the editor and webhook runtime on loopback port `${N8N_HOST_PORT:-5678}` and persists state in a named volume.
- `minimax-openai-bridge`: optional placeholder under the `ai-bridge` profile. Real keys must come from local secret management, never from committed files.

## RAG and Embedding Configuration

v1.3.0 adds `migrations/0005_enable_pgvector_rag.*.sql` for the `vector`
extension, RAG document/chunk tables, and a cosine HNSW index for
1536-dimensional embeddings. Compose passes these placeholders to the services
that will own content generation and workflow execution:

```bash
ECOMMERCE_EMBEDDING_BRIDGE_URL=
ECOMMERCE_EMBEDDING_MODEL=minimax-embedding-01
ECOMMERCE_EMBEDDING_DIMENSIONS=1536
ECOMMERCE_RAG_CHUNK_SIZE=1000
```

`ECOMMERCE_EMBEDDING_BRIDGE_URL` must point to the approved fleet bridge when
enabled; app containers must not call MiniMax provider URLs directly. For local
infra validation, `make rag-seed` and `make rag-search-smoke` use checked-in
fixture vectors from `seed/` and make no live embedding calls.

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

## Observability Alert Runbook

Validate the checked-in monitoring config before booting the stack:

```bash
make monitoring-validate
make compose-config-prod
make compose-workers-config
make compose-temporal-config
make compose-agent-schedules-config
make n8n-config
```

`make monitoring-validate` runs `promtool check config` and `promtool check rules` when `promtool` is installed, then falls back to the repository Go validation tests for Prometheus YAML and Grafana dashboard JSON coverage.

Inspect alerts locally after `compose-up`:

```bash
open http://127.0.0.1:${PROMETHEUS_HOST_PORT:-9090}/alerts
open http://127.0.0.1:${PROMETHEUS_HOST_PORT:-9090}/rules
open http://127.0.0.1:${GRAFANA_HOST_PORT:-3001}
```

Expected baseline and v1.3.0 rules:

- `AgenticEcommerceHighApiLatency`: API p95 latency above 500ms for 5 minutes.
- `AgenticEcommerceHighErrorRate`: API 5xx error rate above 1% for 5 minutes.
- `AgenticEcommerceSyncLagHigh`: WooCommerce sync lag above 5 minutes.
- `AgenticEcommerceAgentFailureRateHigh`: agent-worker failure rate above 5% for 10 minutes.
- `AgenticEcommerceScheduledAgentFailuresHigh`: Temporal-backed scheduled agent failures observed in the last 15 minutes.
- `AgenticEcommerceComplianceFailureSpike`: backend plus worker compliance failures above the 15-minute spike threshold.
- `AgenticEcommerceRAGSearchLatencyHigh`: RAG vector search p95 above 1 second.
- `AgenticEcommerceEmbeddingFailuresHigh`: approved embedding bridge failures detected.

The `Agentic Ecommerce Overview` dashboard should show API latency/error rate, WooCommerce sync lag/conflicts, agent run success/failure, compliance pass/fail rates, RAG search p95 latency, and embedding bridge failures. The worker-backed panels populate when the `workers` profile is running.

Temporal local infrastructure is intentionally excluded from default full-stack
boot. Start it when workflow development needs a server:

```bash
make temporal-up
open "http://127.0.0.1:${TEMPORAL_UI_HOST_PORT:-8233}"
```

See `docs/temporal-local.md` for health checks, readiness expectations, and the
worker runtime. See `docs/agent-schedules.md` for v1.6.0 schedule inspection and
smoke targets.

n8n local automation is also excluded from the default full-stack boot. Start it
only when importing or testing automation templates:

```bash
make n8n-up
open "http://127.0.0.1:${N8N_HOST_PORT:-5678}"
```

See `docs/n8n-local.md` for workflow imports, environment placeholders, and
credential handling. The backend outbound webhook bridge is a separate v1.5.0
backend implementation slice.

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

RAG embeddings follow the same bridge-only boundary. App containers must not
call MiniMax directly; configure `ECOMMERCE_EMBEDDING_BRIDGE_URL` only with an
approved fleet bridge endpoint. `ECOMMERCE_EMBEDDING_MODEL` defaults to
`minimax-embedding-01`, `ECOMMERCE_EMBEDDING_DIMENSIONS` defaults to `1536`,
and `ECOMMERCE_RAG_CHUNK_SIZE` defaults to `1000`.

`wc-sync` defaults to `ECOMMERCE_SYNC_DRY_RUN=true`. Do not run live WooCommerce sync from this stack until credentials, webhook secrets, and target store ownership have been reviewed.

n8n workflow JSON in `deploy/n8n/workflows/` must stay inactive and
credential-free. Keep `N8N_SLACK_WEBHOOK_URL`, `N8N_ORDER_EMAIL_ENDPOINT_URL`,
and `N8N_ENCRYPTION_KEY` blank in committed examples and set them only in local
untracked environment files.

## Cloud Readiness

`mc-api` exposes three unauthenticated platform endpoints:

- `/healthz`: liveness only; it does not call Postgres, Redis, WooCommerce, or AI bridges.
- `/readyz`: readiness; configured `ECOMMERCE_DB_URL` and `ECOMMERCE_REDIS_ADDR` dependencies must pass lightweight checks or the endpoint returns `503` with generic `dependency_failed` or `dependency_timeout` detail.
- `/metrics`: Prometheus text exposition with build metadata, HTTP request counters/duration buckets, and stack-level placeholders for sync, agent, compliance, and media dashboards.
- RAG observability stubs expose `agentic_ecommerce_rag_search_duration_seconds`
  and `agentic_ecommerce_embedding_failures_total` so dashboards and alerts can
  be wired before live bridge calls are enabled.

`ECOMMERCE_READINESS_TIMEOUT` defaults to `2s`. Keep it below the load balancer health-check timeout so an unhealthy dependency fails fast. PostgreSQL pool sizing is controlled by `ECOMMERCE_DB_POOL_MAX_CONNS`, `ECOMMERCE_DB_POOL_MIN_CONNS`, `ECOMMERCE_DB_POOL_MAX_CONN_LIFETIME`, `ECOMMERCE_DB_POOL_MAX_CONN_IDLE_TIME`, and `ECOMMERCE_DB_CONNECT_TIMEOUT`; Redis operations use `ECOMMERCE_REDIS_TIMEOUT` when the request has no deadline. `ECOMMERCE_SHUTDOWN_TIMEOUT` defaults to `10s` and bounds graceful HTTP shutdown after SIGTERM.

`mc-api` writes structured JSON access logs with `request_id`, `trace_id`, `tenant_id`, `actor_id`, route, status, duration, client IP, and user-agent fields, mirrors `X-Request-ID` to every response, and supports optional OpenTelemetry HTTP spans with `ECOMMERCE_OTEL_ENABLED=true`. Probe endpoints are excluded from spans to reduce noise.

## Media and Compliance Placeholders

Production-like compose exposes media and compliance configuration with a named
filesystem volume for local release-candidate smokes:

```bash
ECOMMERCE_MEDIA_STORAGE_DRIVER=filesystem
ECOMMERCE_MEDIA_STORE=filesystem
ECOMMERCE_MEDIA_BASE_PATH=/var/lib/agentic-ecommerce/media
ECOMMERCE_MEDIA_ROOT=/var/lib/agentic-ecommerce/media
ECOMMERCE_MEDIA_BUCKET=
ECOMMERCE_MEDIA_PUBLIC_BASE_URL=/media
ECOMMERCE_MEDIA_MAX_SIZE_BYTES=5242880
ECOMMERCE_MEDIA_ALLOWED_MIME_TYPES=image/jpeg,image/png,image/webp
ECOMMERCE_COMPLIANCE_MIN_SEO_SCORE=70
ECOMMERCE_COMPLIANCE_MAX_IMAGE_SIZE_BYTES=5242880
ECOMMERCE_COMPLIANCE_ALLOWED_MIME_TYPES=image/jpeg,image/png,image/webp
```

Use `make compose-media-config` to validate the optional MinIO profile when
testing S3-compatible storage locally. Cloud deployments should set
`ECOMMERCE_MEDIA_STORAGE_DRIVER` to `s3` or `gcs`; see `docs/media-storage.md`.

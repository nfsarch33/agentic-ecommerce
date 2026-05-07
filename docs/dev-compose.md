# Development Compose Stack

The development compose stack is intentionally local-first. Published ports bind to `BIND_HOST=127.0.0.1` by default, data lives in named volumes, and no live WooCommerce credentials are required to boot or validate the stack.

## Core Backend

Start the backend services:

```bash
make dev
curl http://127.0.0.1:8080/healthz
make redis-ping
```

This starts PostgreSQL, Redis, and `mc-api`. Redis is available to containers at `redis:6379` and to host tools at `127.0.0.1:${REDIS_HOST_PORT:-6379}`.

## Local WooCommerce

Start the optional WordPress and MariaDB services:

```bash
make wc-up
```

WordPress is published at `http://127.0.0.1:${WC_HOST_PORT:-8081}`. MariaDB is published on loopback at `${WC_DB_HOST_PORT:-3307}` for local debugging only.

The compose file also defines a `wc-cli` helper service. It does not run automatically, because WordPress installation creates local admin state and WooCommerce REST keys. When you need a disposable local store, run the setup explicitly after `make wc-up`:

```bash
docker compose -f docker-compose.dev.yml --profile woocommerce --profile tools run --rm wc-cli \
  core install \
  --url="http://127.0.0.1:${WC_HOST_PORT:-8081}" \
  --title="Agentic Ecommerce Dev" \
  --admin_user=admin \
  --admin_password=admin \
  --admin_email=dev@example.local \
  --skip-email

docker compose -f docker-compose.dev.yml --profile woocommerce --profile tools run --rm wc-cli \
  plugin install woocommerce --activate
```

Do not commit generated REST API keys. Create WooCommerce API keys through the local admin UI or WP-CLI, place them in `.env`, and keep `.env` untracked:

```bash
ECOMMERCE_WC_BASE_URL=http://wordpress
ECOMMERCE_WC_CONSUMER_KEY=
ECOMMERCE_WC_CONSUMER_SECRET=
```

Stop the local WooCommerce containers while preserving volumes:

```bash
make wc-down
```

## Sync Worker

`wc-sync` is built from the same Dockerfile as `mc-api` with `TARGET=wc-sync`. It is exposed through the compose `sync` profile and defaults to a one-shot run:

```bash
make sync-once
```

If `ECOMMERCE_WC_CONSUMER_KEY` or `ECOMMERCE_WC_CONSUMER_SECRET` is blank, the worker uses the no-op channel and logs a dry-run credential notice. With credentials present, it uses the WooCommerce REST adapter pointed at `ECOMMERCE_WC_BASE_URL`.

## Agent Worker

`agent-worker` is the v0.6.0 scheduler runtime shell. It intentionally does not duplicate the parallel backend orchestrator work yet; the binary reads scheduler and event-bus config, exposes health/metrics, and logs a `agent-worker.scheduler_placeholder` TODO hook for backend QA to wire to `internal/agent` once the orchestrator branch lands.

Run the placeholder once without compose:

```bash
make agent-run-once
```

Run it as a long-lived compose worker:

```bash
docker compose -f docker-compose.dev.yml --profile workers up --build agent-worker
curl http://127.0.0.1:${AGENT_WORKER_METRICS_HOST_PORT:-8082}/metrics
```

Scheduler controls:

```bash
ECOMMERCE_AGENT_WORKER_ENABLED=true
ECOMMERCE_AGENT_WORKER_RUN_ONCE=false
ECOMMERCE_AGENT_WORKER_CONCURRENCY=1
ECOMMERCE_AGENT_WORKER_INTERVAL=5m
```

## Temporal Dev Server

v1.2.0 adds an opt-in Temporal profile for local workflow development:

```bash
make temporal-up
make temporal-status
```

The Temporal gRPC endpoint is published on
`127.0.0.1:${TEMPORAL_GRPC_HOST_PORT:-7233}` and the Web UI is published on
`http://127.0.0.1:${TEMPORAL_UI_HOST_PORT:-8233}`. Both ports stay loopback-only
through `BIND_HOST=127.0.0.1`.

`temporal-worker` is defined under the separate `temporal-worker` profile as an
image-only placeholder until `cmd/temporal-worker` lands. Its task queue contract
is `ECOMMERCE_TEMPORAL_TASK_QUEUE=ec-workflows`. See
`docs/temporal-local.md` for the full runbook and worker handoff.

## n8n Automation Profile

v1.5.0 adds n8n as an opt-in local automation profile:

```bash
make n8n-config
make n8n-up
```

n8n is published on `http://127.0.0.1:${N8N_HOST_PORT:-5678}` through the same
loopback-only `BIND_HOST` boundary as the rest of the stack. Import templates
from `deploy/n8n/workflows/` after completing the local owner setup in the n8n
UI. See `docs/n8n-local.md` for credential placeholders, workflow import steps,
and the security boundary.

## Media Storage and Compliance Placeholders

The dev stack mounts a host media directory into backend services for upload and
processor work:

```bash
ECOMMERCE_MEDIA_HOST_DIR=./.local/media-uploads
ECOMMERCE_MEDIA_STORAGE_DRIVER=filesystem
ECOMMERCE_MEDIA_STORE=filesystem
ECOMMERCE_MEDIA_BASE_PATH=/var/lib/agentic-ecommerce/media
ECOMMERCE_MEDIA_ROOT=/var/lib/agentic-ecommerce/media
ECOMMERCE_MEDIA_PUBLIC_BASE_URL=/media
ECOMMERCE_MEDIA_MAX_SIZE_BYTES=5242880
ECOMMERCE_MEDIA_ALLOWED_MIME_TYPES=image/jpeg,image/png,image/webp
```

Use the Makefile helpers to reset local upload state:

```bash
make media-store-seed
make media-store-clean
```

`make compose-media-config` validates the opt-in MinIO profile for S3-compatible
adapter work. The profile uses placeholder environment variables only; set
`MINIO_ROOT_USER` and `MINIO_ROOT_PASSWORD` locally before starting it. See
`docs/media-storage.md` for details.

The compliance thresholds are config placeholders for the v0.7.0 engine and
should be consumed by the backend compliance rules when that implementation
lands:

```bash
ECOMMERCE_COMPLIANCE_MIN_SEO_SCORE=70
ECOMMERCE_COMPLIANCE_MAX_IMAGE_SIZE_BYTES=5242880
ECOMMERCE_COMPLIANCE_ALLOWED_MIME_TYPES=image/jpeg,image/png,image/webp
```

## Redis Event Bus Contract

v0.3.0 reserves Redis for the sync event bus without adding a full eventing implementation in this infra slice. The backend and worker receive the same environment variables:

```bash
ECOMMERCE_EVENTBUS_DRIVER=redis
ECOMMERCE_EVENTBUS_REDIS_ADDR=redis:6379
ECOMMERCE_EVENTBUS_REDIS_DB=0
ECOMMERCE_EVENTBUS_CHANNEL_SYNC=ec.sync.events
ECOMMERCE_EVENTBUS_CHANNEL_DLQ=ec.sync.deadletter
```

Planned event names for the Go implementation:

- `product.sync.requested`
- `product.sync.completed`
- `product.sync.failed`
- `order.sync.received`
- `inventory.sync.reconciled`
- `sync.conflict.detected`

Messages should include `event_id`, `event_name`, `occurred_at`, `source`, `entity_type`, `entity_id`, and `correlation_id`. Failed messages that cannot be retried safely should be published to `ECOMMERCE_EVENTBUS_CHANNEL_DLQ`.

## Validation

Use the compose config target before opening a PR:

```bash
make compose-config
make compose-wc-config
make compose-workers-config
make compose-temporal-config
make n8n-config
```

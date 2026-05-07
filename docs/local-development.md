# Local Development

This repo owns the Go backend services for the Agentic Ecommerce stack. The public Next.js storefront lives in `agentic-ecommerce-web` and should be run beside this compose stack when exercising cart, checkout, and order flows.

## Backend Stack

1. Copy `.env.example` to `.env` and adjust ports only if they conflict locally.
2. Start backend dependencies and `mc-api`:

   ```bash
   make dev
   ```

3. Follow logs when needed:

   ```bash
   make dev-logs
   ```

4. Check service health:

   ```bash
   curl http://127.0.0.1:8080/healthz
   curl http://127.0.0.1:8080/readyz
   make redis-ping
   ```

The dev compose stack publishes PostgreSQL, Redis, and `mc-api` on loopback by default through `BIND_HOST=127.0.0.1`.

For the optional WooCommerce test instance and `wc-sync` worker, see `docs/dev-compose.md`. The WooCommerce profile keeps WordPress and MariaDB separate from the default backend boot path so API development does not require a local store.

For local Temporal workflow infrastructure, use `make temporal-up` and the
runbook in `docs/temporal-local.md`. Temporal is opt-in and does not gate
`/healthz` or `/readyz` until workflow APIs are implemented.

For local n8n automation templates, use `make n8n-up` and the runbook in
`docs/n8n-local.md`. n8n is opt-in, loopback-only by default, and does not imply
that the backend outbound webhook bridge API has landed.

## Local RAG and Embedding Fixtures

The dev PostgreSQL service already uses `pgvector/pgvector:pg16`. v1.3.0 adds
`migrations/0005_enable_pgvector_rag.*.sql` for the `vector` extension and RAG
document/chunk tables. Apply the migration with the normal migration target:

```bash
make migrate-up
```

RAG smoke targets use deterministic fixture vectors only and do not call the
embedding bridge or MiniMax:

```bash
make rag-seed
make rag-search-smoke
```

Embedding runtime configuration is reserved for the backend RAG package:

```bash
ECOMMERCE_EMBEDDING_BRIDGE_URL=
ECOMMERCE_EMBEDDING_MODEL=minimax-embedding-01
ECOMMERCE_EMBEDDING_DIMENSIONS=1536
ECOMMERCE_RAG_CHUNK_SIZE=1000
```

`ECOMMERCE_EMBEDDING_BRIDGE_URL` must point to the approved fleet bridge when
enabled. Do not point app containers or MacBook-local runs directly at MiniMax
provider endpoints.

## Tenant Isolation Fixtures

v1.9.0 adds synthetic tenant fixtures for backend admin/compliance QA support.
Apply migrations, then seed and assert the fixture boundaries:

```bash
make migrate-up
make tenant-isolation-smoke
```

For fast Go-only coverage without a running database:

```bash
make tenant-isolation-test
```

The fixture tenant IDs are `tenant-alpha-demo` and `tenant-beta-demo`. They are
not provisioned tenants and do not require real credentials. See
`docs/tenant-isolation.md` for the migration matrix, current limitations, and
monitoring label policy.

## Local Media Storage

v1.4.0 uses `.local/media-uploads/` for filesystem-backed media storage in
development. The directory is ignored by git and mounted into backend services
by `docker-compose.dev.yml`.

```bash
make media-store-seed
make media-store-clean
```

The seed target copies only synthetic fixtures from `seed/media/`. Do not place
supplier, customer, or production media in git.

Use `make compose-media-config` to validate the optional MinIO profile before
testing an S3-compatible adapter. See `docs/media-storage.md` for the full
filesystem, MinIO, S3, and GCS mapping.

## Redis Session Infrastructure

Redis 7 is available at `redis:6379` inside compose and `127.0.0.1:${REDIS_HOST_PORT:-6379}` from the host. It is reserved for v0.2.0 cart/session storage and later Redis-backed event bus work.

`/healthz` remains a liveness check that does not depend on downstream services. `/readyz` is the cloud readiness check:

- PostgreSQL is checked with a lightweight pgxpool ping when `ECOMMERCE_DB_URL` is set. The pool is tuned by `ECOMMERCE_DB_POOL_MAX_CONNS` (default `10`), `ECOMMERCE_DB_POOL_MIN_CONNS` (default `1`), `ECOMMERCE_DB_POOL_MAX_CONN_LIFETIME` (default `30m`), `ECOMMERCE_DB_POOL_MAX_CONN_IDLE_TIME` (default `5m`), and `ECOMMERCE_DB_CONNECT_TIMEOUT` (default `5s`).
- Redis is checked with `PING` when `ECOMMERCE_REDIS_ADDR` is set, selecting `ECOMMERCE_REDIS_DB` first when non-zero.
- Unset dependencies are returned as `skipped` and do not gate readiness.

The readiness timeout is controlled by `ECOMMERCE_READINESS_TIMEOUT` and defaults to `2s`. Redis rate-limit operations use `ECOMMERCE_REDIS_TIMEOUT` and default to `500ms` when the request context has no deadline. Graceful HTTP shutdown is bounded by `ECOMMERCE_SHUTDOWN_TIMEOUT`, defaulting to `10s`.

## Local Auth and Rate Limits

Set `ECOMMERCE_JWT_SECRET` to a local-only value of at least 32 bytes and configure `ECOMMERCE_ADMIN_USERNAME` plus `ECOMMERCE_ADMIN_PASSWORD` before exercising protected admin/operator endpoints. The login endpoint is:

```bash
curl -s http://127.0.0.1:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin@example.invalid","password":"replace-with-local-admin-password"}'
```

The returned JWT access token is used as `Authorization: Bearer <token>`. Roles are `admin`, `operator`, and `viewer`; product reads, carts, checkout order creation, `/healthz`, `/readyz`, and `/metrics` remain unauthenticated for storefront and platform probes. Mutations emit structured `audit.event` logs without request bodies or secret values.

Rate limiting defaults to `ECOMMERCE_RATE_LIMIT_CAPACITY=60` requests per `ECOMMERCE_RATE_LIMIT_REFILL=1m`, keyed by bearer token hash when present or client IP otherwise. When `ECOMMERCE_REDIS_ADDR` is configured, the limiter uses Redis for shared token buckets; tests and no-Redis local runs use an in-memory bucket.

## Request IDs and Traces

Every request gets an `X-Request-ID` response header. If the caller supplies `X-Request-ID`, the backend reuses it; otherwise it generates one and includes the value in structured JSON access logs. Access logs also include cloud-friendly correlation fields: `trace_id` from W3C `traceparent` or OpenTelemetry context, `tenant_id` from `X-Tenant-ID`, authenticated `actor_id`, route, HTTP method/status, duration, client IP, and user agent.

Set `ECOMMERCE_OTEL_ENABLED=true` to wrap application routes with OpenTelemetry HTTP instrumentation and W3C trace-context propagation. Health, readiness, and metrics endpoints are excluded from spans to keep probes quiet.

## Frontend Flow

Run the frontend repo in a separate terminal:

```bash
bun install
bun run dev
```

Set the frontend API base URL to the local backend, typically `http://127.0.0.1:8080`. The v0.2.0 QA flow will validate browse products, add to cart, checkout, and order confirmation after the backend and frontend feature branches are merged.

Do not run full end-to-end tests for this infra slice; v0.2.0 QA owns the final Playwright flow.

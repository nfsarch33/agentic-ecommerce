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

## Local Media Directory

v0.7.0 reserves `.local/media-uploads/` for filesystem-backed media storage in
development. The directory is ignored by git and mounted into `mc-api` by
`docker-compose.dev.yml`.

```bash
make media-seed
make media-clean
```

The seed target copies only synthetic fixtures from `seed/media/`. Do not place
supplier, customer, or production media in git.

## Redis Session Infrastructure

Redis 7 is available at `redis:6379` inside compose and `127.0.0.1:${REDIS_HOST_PORT:-6379}` from the host. It is reserved for v0.2.0 cart/session storage and later Redis-backed event bus work.

`/healthz` remains a liveness check that does not depend on downstream services. `/readyz` is the cloud readiness check:

- PostgreSQL is checked with a lightweight pgxpool ping when `ECOMMERCE_DB_URL` is set.
- Redis is checked with `PING` when `ECOMMERCE_REDIS_ADDR` is set, selecting `ECOMMERCE_REDIS_DB` first when non-zero.
- Unset dependencies are returned as `skipped` and do not gate readiness.

The readiness timeout is controlled by `ECOMMERCE_READINESS_TIMEOUT` and defaults to `2s`. Graceful HTTP shutdown is bounded by `ECOMMERCE_SHUTDOWN_TIMEOUT`, defaulting to `10s`.

## Request IDs and Traces

Every request gets an `X-Request-ID` response header. If the caller supplies `X-Request-ID`, the backend reuses it; otherwise it generates one and includes the value in structured JSON access logs.

Set `ECOMMERCE_OTEL_ENABLED=true` to wrap application routes with OpenTelemetry HTTP instrumentation and W3C trace-context propagation. Health, readiness, and metrics endpoints are excluded from spans to keep probes quiet.

## Frontend Flow

Run the frontend repo in a separate terminal:

```bash
bun install
bun run dev
```

Set the frontend API base URL to the local backend, typically `http://127.0.0.1:8080`. The v0.2.0 QA flow will validate browse products, add to cart, checkout, and order confirmation after the backend and frontend feature branches are merged.

Do not run full end-to-end tests for this infra slice; v0.2.0 QA owns the final Playwright flow.

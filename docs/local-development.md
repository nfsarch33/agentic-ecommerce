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
   make redis-ping
   ```

The dev compose stack publishes PostgreSQL, Redis, and `mc-api` on loopback by default through `BIND_HOST=127.0.0.1`.

## Redis Session Infrastructure

Redis 7 is available at `redis:6379` inside compose and `127.0.0.1:${REDIS_HOST_PORT:-6379}` from the host. It is reserved for v0.2.0 cart/session storage and later Redis-backed event bus work.

Current backend code does not consume Redis yet. When cart/session storage is wired, add a `/readyz` endpoint that verifies:

- PostgreSQL can accept a lightweight query through the configured pool.
- Redis responds to `PING` using `ECOMMERCE_REDIS_ADDR`, `ECOMMERCE_REDIS_DB`, and `ECOMMERCE_REDIS_KEY_PREFIX`.
- `/healthz` remains a liveness check that does not depend on downstream services.

Until then, Redis readiness is covered by the compose healthcheck and `make redis-ping`.

## Frontend Flow

Run the frontend repo in a separate terminal:

```bash
bun install
bun run dev
```

Set the frontend API base URL to the local backend, typically `http://127.0.0.1:8080`. The v0.2.0 QA flow will validate browse products, add to cart, checkout, and order confirmation after the backend and frontend feature branches are merged.

Do not run full end-to-end tests for this infra slice; v0.2.0 QA owns the final Playwright flow.

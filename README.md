# Agentic Ecommerce

Standalone Go service stack for WooCommerce-first agentic ecommerce.

## Scope

- Mission Control API spine (`cmd/mc-api`)
- WooCommerce sync worker (`cmd/wc-sync`)
- Content and compliance worker (`cmd/content-worker`)
- Clean Architecture packages under `internal/domain`, `internal/port`, `internal/app`, and `internal/adapter`

This repo starts with WooCommerce cashflow first, then grows into multi-channel catalog, content compliance, Temporal workflows, n8n event automation, and media intelligence.


## Public Safety

This repository is Apache-2.0 and safe for public collaboration only while it contains generic source, tests, documentation, and placeholder configuration. Do not commit live WooCommerce credentials, MiniMax keys, browser profiles, private fleet hostnames, internal IPs, or local `.env` files. See `SECURITY.md`.

The public Next.js frontend lives at `nfsarch33/agentic-ecommerce-web` and consumes the API contract in `api/openapi.yaml`.

## Local Development

Use the dev compose stack for backend work:

```bash
cp .env.example .env
make dev
curl http://127.0.0.1:8080/healthz
make redis-ping
```

The stack runs PostgreSQL, Redis 7, and `mc-api` with published ports bound to `127.0.0.1` by default. Redis is reserved for v0.2.0 cart/session storage and is exposed to `mc-api` as `ECOMMERCE_REDIS_ADDR=redis:6379`; host tools can use `ECOMMERCE_REDIS_ADDR=127.0.0.1:6379`.

For the storefront checkout flow, run `agentic-ecommerce-web` separately with `bun run dev` and point it at `http://127.0.0.1:8080`. See `docs/local-development.md` for the backend compose, frontend dev, and Redis readiness plan.

## Gates

```bash
go test -race ./...
go vet ./...
make build
go test -race -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

Private-repo operations must go through `runx` once the `ecommerce` alias is registered.

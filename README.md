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

## Gates

```bash
go test -race ./...
go vet ./...
make build
go test -race -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

Private-repo operations must go through `runx` once the `ecommerce` alias is registered.

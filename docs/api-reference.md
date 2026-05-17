# v9.0.0 API Reference

`api/openapi.yaml` is the source of truth for the backend API. This guide groups the v9.0.0 release-baseline surface area for release review and frontend coordination; update both when a route, schema, auth boundary, or tenant behavior changes.

## Contract Ownership

- Backend repo: `api/openapi.yaml`, OpenAPI contract tests, handler implementations, auth/RBAC, tenant scoping, workflow orchestration, webhook signing, media storage, and compliance/RAG behavior.
- Frontend repo: generated TypeScript schema, API adapters, BFF session/AI helper routes, and admin UX documentation.
- Release rule: regenerate frontend types with `bun run api:generate` after backend OpenAPI changes and include the resulting diff in the frontend PR when schemas change.

## Platform and Auth

| Surface | Routes | Auth |
| --- | --- | --- |
| Platform health | `GET /healthz`, `GET /readyz`, `GET /metrics` | Public |
| Auth session | `POST /api/v1/auth/login`, `POST /api/v1/auth/refresh`, `GET /api/v1/auth/me`, `POST /api/v1/auth/logout` | Login public; session routes use bearer JWT |
| Storefront commerce | `GET /api/v1/products`, product detail, cart, checkout, order status | Public reads and checkout; protected admin mutations |

Protected routes use short-lived HMAC-signed JWT bearer tokens with `admin`, `operator`, or `viewer` roles. Tenant-aware admin routes accept `X-Tenant-ID` for MVP/local workflows when the JWT does not carry a tenant claim. Public docs and examples must not include live JWTs, WooCommerce credentials, bridge keys, object-store keys, n8n credentials, or private hosts.

## v9.0.0 Capability Groups

| Capability | Primary routes | Notes |
| --- | --- | --- |
| Catalog and orders | `/api/v1/products`, `/api/v1/orders`, checkout/cart routes | Storefront reads remain public; admin writes require JWT/RBAC. |
| RAG and fact-checking | `/api/v1/rag/documents`, `/api/v1/rag/search` | Stores tenant-scoped documents/chunks in PostgreSQL + pgvector and returns evidence for content checks. |
| Media Intelligence | `/api/v1/media/source`, `/api/v1/media/process`, `/api/v1/media/{id}`, `/api/v1/media/{id}/approve`, `/api/v1/media/{id}/reject`, `/api/v1/media/{id}/validate` | Deterministic source/review/process/QA contracts with filesystem or S3/GCS-backed storage plus explicit operator review transitions. |
| Compliance | `/api/v1/products/{id}/compliance-check`, `/api/v1/compliance/rules`, `/api/v1/compliance/custom-rules`, `/api/v1/compliance/reports/*` | Built-in and tenant custom rules, rule versioning, history, summary, and export. |
| Tenant settings | `/api/v1/tenant/settings` | Branding, WooCommerce credential references, AI preferences, and compliance overrides. |
| Agent automation | `/api/v1/agents/*`, `/api/v1/agent-schedules/*` | Sourcing, pricing, and compliance run contracts plus schedule controls. |
| Webhooks | `/api/v1/webhooks`, `/api/v1/webhooks/{id}`, `/api/v1/webhooks/{id}/test`, inbound WooCommerce webhook routes | Outbound registrations are tenant scoped and HMAC signed. See `docs/webhook-contracts.md`. |
| Temporal workflows | `/api/v1/workflows`, `/api/v1/workflows/product-publish`, `/api/v1/workflows/content-generation`, `/api/v1/workflows/media-processing`, `/api/v1/workflows/sourcing`, `/api/v1/workflows/{id}`, `/api/v1/workflows/{id}/signals/review` | Durable workflow starts, authoritative lifecycle list/detail reads, and human-review signal contracts. Review signal responses may include the refreshed workflow snapshot. See `docs/temporal-workflow-specs.md`. |

## Tenant-Aware Contract

The v9.0.0 API is tenant-aware but does not provision tenants. Tenant IDs are opaque strings used to scope repository operations, tenant settings, custom compliance rules, compliance history, webhook registrations, RAG documents, and release fixtures.

Tenant IDs must not be used as raw Prometheus labels. Use structured logs, SQL exports, or bounded labels such as `tenant_scope` for observability. See `docs/tenant-isolation.md`.

## Media Lifecycle Contract

Media assets now move through a review-bearing lifecycle instead of implicit
best-effort processing:

- `POST /api/v1/media/source` creates or reuses a media request and returns the
  current lifecycle state.
- `POST /api/v1/media/{id}/approve` and `POST /api/v1/media/{id}/reject` are
  the only explicit review transitions for pending assets.
- `POST /api/v1/media/process` only accepts assets that are already approved.
- Media responses include status timestamps so operators and downstream UIs can
  audit when a request last changed state.
- Repeated source requests are idempotent for the same request payload; callers
  should treat duplicate sourcing as "return the current asset state", not "make
  another asset".

## Validation

Run focused contract gates after OpenAPI or docs changes:

```bash
make contract-test
go test ./cmd/mc-api -run 'Test.*OpenAPI|Test.*Contract' -count=1
runx shell-leak-scan --repo ecommerce
```

Run the broader release gates from `docs/release-checklist.md` before tagging v9.0.0.

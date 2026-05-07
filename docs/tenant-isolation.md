# Tenant Isolation Infra and QA Support

v1.9.0 adds backend QA support for tenant-aware admin and compliance reporting without implementing tenant provisioning UI or duplicating the feature work. The current contract is deterministic test data, migration documentation, and guardrails for tenant-safe observability.

## Tenant Identifiers

The backend currently represents tenants as opaque string IDs. `internal/tenant` provides context helpers and repository methods accept an explicit tenant ID for tenant-scoped catalog and order writes/reads.

Current test tenant IDs:

- `default`: migration backfill value for existing local/dev data.
- `tenant-alpha-demo`: synthetic v1.9.0 fixture tenant.
- `tenant-beta-demo`: synthetic v1.9.0 fixture tenant.

All fixture names, IDs, emails, media URLs, and checksums are fake. The fixture tenants are for local QA only and do not imply a tenant provisioning table or account lifecycle.

## Migration Coverage

`migrations/0004_add_tenant_id.up.sql` adds `tenant_id TEXT NOT NULL DEFAULT 'default'` plus simple lookup indexes to the existing v1.0-v1.4 domain tables:

- `products`
- `orders`
- `categories`
- `product_images`
- `product_media_assets`

`migrations/0005_enable_pgvector_rag.up.sql` introduces RAG tables with tenant IDs from day one:

- `rag_documents`
- `rag_document_chunks`

The down migrations remove tenant indexes before removing the columns, and remove RAG tables in dependency order.

## Relationship Boundaries

Some child tables intentionally do not carry their own `tenant_id` yet:

- `product_categories` inherits tenant scope through `products` and `categories`.
- `order_items` inherits tenant scope through `orders`; fixture smoke tests also verify that the referenced product belongs to the same tenant.
- `carts` and `cart_items` remain storefront session scoped. Tenant-aware cart partitioning is a future feature, not part of this infra slice.

Current uniqueness constraints are still global on `products.sku`, `products.slug`, `categories.slug`, and `product_media_assets.storage_key`. Test fixtures include the tenant ID in those values to avoid collisions. Tenant-scoped unique indexes can replace this once tenant provisioning and tenant-aware write paths are fully implemented.

## Fixture Workflow

Bring up the dev database, apply migrations, then seed and smoke test the v1.9.0 fixtures:

```bash
make dev
make migrate-up
make tenant-isolation-smoke
```

`make tenant-isolation-smoke` runs `seed/tenant_isolation.sql` and then `seed/tenant_isolation_smoke.sql`. The smoke script raises if:

- either test tenant has the wrong fixture product count;
- Alpha-prefixed products appear under the Beta tenant or vice versa;
- product images or media assets point at a product from a different tenant;
- fixture order items reference products from a different tenant than their order.

For fast non-DB checks, run:

```bash
make tenant-isolation-test
```

This covers the tenant context helpers, tenant-aware repository contracts, and monitoring-cardinality guardrails.

## API and Admin Limitations

The current admin-facing isolation surface is infrastructure only:

- No tenant provisioning UI or tenant selector persistence exists yet.
- No `tenants` table exists yet.
- JWT tenant claims and subdomain extraction are planned v1.9.0 feature work; request logging already records `X-Tenant-ID` for local/cloud correlation.
- Existing public storefront catalog APIs are not fully tenant scoped in this infra slice. Use repository tenant methods and the fixture smoke tests for backend isolation proof until the admin/API feature work lands.
- Compliance report export and custom rule management are future feature work. This slice only prepares tenant fixture data and reporting guardrails.

## Monitoring Dimensions

Do not add raw `tenant_id` as a Prometheus label. Tenant IDs are unbounded customer identifiers and would create high-cardinality series, especially on HTTP, agent, RAG, and compliance metrics.

Use these safer surfaces instead:

- Structured logs and traces may include `tenant_id` for request-level investigation.
- Compliance reports should aggregate tenant-specific data from PostgreSQL at export time.
- Prometheus metrics may use bounded labels only, such as `tenant_scope="default|fixture|prod"` or `tenant_tier="dev|standard|enterprise"` once those enums exist.
- Dashboards should not group by raw tenant IDs. For per-tenant drill-downs, use SQL/report exports or log queries with explicit time windows.

`monitoring/config_validation_test.go` rejects checked-in Prometheus static labels, alert expressions, and Grafana panel queries that introduce raw `tenant_id` metric dimensions.

# Backend Performance Audit

This audit records the v1.8.0 database query baseline for the Go backend.

## Critical Queries

- Product catalog page: `products ORDER BY created_at LIMIT/OFFSET`, target p95 `<100ms`.
- Product detail by slug/ID: `products.slug` / primary key lookup, target comfortably below catalog p95.
- Tenant catalog page: `products WHERE tenant_id`, used by v1.1+ tenant-aware repository methods.
- Order detail: `orders` primary key plus `order_items.order_id`, target p95 `<200ms` for order workflows.
- Cart detail: `carts.session_id` plus `cart_items.session_id`.
- RAG evidence search: `rag_document_chunks.embedding <=> query`, target bounded by pgvector HNSW index for populated corpora.

## How To Run

```bash
make dev
make migrate-up
make seed
make db-perf-audit
```

The target runs `scripts/db_performance_audit.sql` inside the dev PostgreSQL container with `EXPLAIN (ANALYZE, BUFFERS, VERBOSE)`. The script wraps all statements in a transaction and rolls back.

## Expected Local Baseline

On a seeded local database, the audit should show:

- Product slug lookup using `idx_products_slug`.
- Order item lookup using `idx_order_items_order_id`.
- Cart item lookup using `idx_cart_items_session_id`.
- RAG vector search using `idx_rag_document_chunks_embedding_cosine` when enough chunks exist for the planner to prefer HNSW.
- No critical query exceeding low double-digit milliseconds on the seed dataset.

Small seeded tables may still produce sequential scans because PostgreSQL correctly estimates that scanning a handful of rows is cheaper than using an index. Treat that as acceptable only when actual time remains below the endpoint budget and row counts are tiny.

## v1.8.0 Local Evidence

Captured on 2026-05-08 with `docker compose -f docker-compose.dev.yml up -d --wait postgres`, migrations, seed data, and `make db-perf-audit`:

- Product catalog page used `idx_products_created_at`; execution time `0.041ms`.
- Product slug lookup used `idx_products_slug`; execution time `0.022ms`.
- Tenant catalog page used `idx_products_tenant_id` plus bounded sort; execution time `0.110ms`.
- Order detail plan used `idx_orders_created_at`, `orders_pkey`, and `idx_order_items_order_id`; execution time `0.069ms` on an empty seeded order table.
- Cart lookup used `carts_pkey` and planned `idx_cart_items_session_id`; execution time `0.058ms` on an empty seeded cart table.
- RAG vector query planned `idx_rag_document_chunks_tenant_model` on an empty seeded RAG corpus; execution time `0.052ms`. Expect the HNSW cosine index to become preferred once chunk volume is large enough.

\echo 'Agentic Ecommerce DB performance audit'
\echo 'Run after make dev && make migrate-up && make seed'

BEGIN;
SET LOCAL jit = off;
SET LOCAL statement_timeout = '5s';

\echo ''
\echo '1. Product catalog page: expect idx_products_created_at or bounded sort over small page'
EXPLAIN (ANALYZE, BUFFERS, VERBOSE)
SELECT id, sku, title, slug, description, price_amount, price_currency, stock, status, created_at, updated_at
FROM products
ORDER BY created_at ASC
LIMIT 20 OFFSET 0;

\echo ''
\echo '2. Product slug lookup: expect idx_products_slug'
EXPLAIN (ANALYZE, BUFFERS, VERBOSE)
SELECT id, sku, title, slug, description, price_amount, price_currency, stock, status, created_at, updated_at
FROM products
WHERE slug = 'resistance-band-set';

\echo ''
\echo '3. Tenant-scoped catalog page: expect tenant/status filtering plus bounded page'
EXPLAIN (ANALYZE, BUFFERS, VERBOSE)
SELECT id, sku, title, slug, description, price_amount, price_currency, stock, status, created_at, updated_at
FROM products
WHERE tenant_id = 'default'
ORDER BY created_at ASC
LIMIT 20 OFFSET 0;

\echo ''
\echo '4. Order detail lookup: expect orders PK plus idx_order_items_order_id'
EXPLAIN (ANALYZE, BUFFERS, VERBOSE)
WITH candidate AS (
  SELECT id
  FROM orders
  ORDER BY created_at DESC
  LIMIT 1
)
SELECT o.id, o.customer_email, o.status, o.subtotal_amount, o.currency, o.shipping_amount, o.total_amount,
       o.shipping_name, o.shipping_line1, o.shipping_line2, o.shipping_city, o.shipping_region,
       o.shipping_postal_code, o.shipping_country, o.created_at, o.updated_at,
       oi.product_id, oi.sku, oi.title, oi.quantity, oi.unit_price_amount, oi.currency, oi.line_total_amount
FROM candidate c
JOIN orders o ON o.id = c.id
LEFT JOIN order_items oi ON oi.order_id = o.id
ORDER BY oi.id ASC;

\echo ''
\echo '5. Cart lookup: expect carts PK plus idx_cart_items_session_id'
EXPLAIN (ANALYZE, BUFFERS, VERBOSE)
SELECT c.session_id, c.subtotal_amount, c.currency, c.total_amount, c.updated_at,
       ci.product_id, ci.sku, ci.title, ci.quantity, ci.unit_price_amount, ci.currency, ci.line_total_amount
FROM carts c
LEFT JOIN cart_items ci ON ci.session_id = c.session_id
WHERE c.session_id = 'session-contract'
ORDER BY ci.id ASC;

\echo ''
\echo '6. RAG vector search: expect idx_rag_document_chunks_embedding_cosine once chunks exist'
EXPLAIN (ANALYZE, BUFFERS, VERBOSE)
SELECT c.id::text, d.source_uri, c.tenant_id, d.title, d.source_uri, c.content,
       1 - (c.embedding <=> ('[' || repeat('0,', 1535) || '0]')::vector) AS score,
       c.metadata
FROM rag_document_chunks c
JOIN rag_documents d ON d.id = c.document_id
WHERE c.tenant_id = 'default'
ORDER BY c.embedding <=> ('[' || repeat('0,', 1535) || '0]')::vector
LIMIT 5;

ROLLBACK;

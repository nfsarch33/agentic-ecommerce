-- v4.1.1 IC-2 — Add IF NOT EXISTS to 6 index creation statements
-- from migration 0001 that were created without it.
--
-- Forward-only, idempotent. Running this on a fresh database or on
-- an existing one with the indexes already present is safe because
-- CREATE INDEX IF NOT EXISTS is a no-op when the index exists.
--
-- The original 0001_create_products.up.sql used bare CREATE INDEX.
-- Re-running that migration on an existing database would fail on
-- the duplicate index. This backfill makes the schema fully
-- idempotent.

CREATE INDEX IF NOT EXISTS idx_categories_slug ON categories(slug);
CREATE INDEX IF NOT EXISTS idx_products_status ON products(status);
CREATE INDEX IF NOT EXISTS idx_products_slug ON products(slug);
CREATE INDEX IF NOT EXISTS idx_products_created_at ON products(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_product_categories_category ON product_categories(category_id);
CREATE INDEX IF NOT EXISTS idx_product_images_product_id ON product_images(product_id, sort_order);

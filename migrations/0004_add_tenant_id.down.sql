DROP INDEX IF EXISTS idx_product_media_assets_tenant_id;
DROP INDEX IF EXISTS idx_product_images_tenant_id;
DROP INDEX IF EXISTS idx_categories_tenant_id;
DROP INDEX IF EXISTS idx_orders_tenant_id;
DROP INDEX IF EXISTS idx_products_tenant_id;

ALTER TABLE product_media_assets DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE product_images DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE categories DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE orders DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE products DROP COLUMN IF EXISTS tenant_id;

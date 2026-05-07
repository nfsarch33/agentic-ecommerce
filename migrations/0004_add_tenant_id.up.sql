ALTER TABLE products ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'default';
ALTER TABLE orders ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'default';
ALTER TABLE categories ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'default';
ALTER TABLE product_images ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'default';
ALTER TABLE product_media_assets ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'default';

CREATE INDEX idx_products_tenant_id ON products(tenant_id);
CREATE INDEX idx_orders_tenant_id ON orders(tenant_id);
CREATE INDEX idx_categories_tenant_id ON categories(tenant_id);
CREATE INDEX idx_product_images_tenant_id ON product_images(tenant_id);
CREATE INDEX idx_product_media_assets_tenant_id ON product_media_assets(tenant_id);

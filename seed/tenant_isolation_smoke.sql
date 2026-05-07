-- v1.9.0 tenant isolation smoke assertions for deterministic fixtures.
-- This script raises on cross-tenant fixture leakage or incomplete seeds.

DO $$
DECLARE
    alpha_products INTEGER;
    beta_products INTEGER;
    cross_tenant_products INTEGER;
    cross_tenant_images INTEGER;
    cross_tenant_media INTEGER;
    cross_tenant_order_items INTEGER;
BEGIN
    SELECT count(*)
    INTO alpha_products
    FROM products
    WHERE tenant_id = 'tenant-alpha-demo'
      AND id::text LIKE 'b1900000-%';

    SELECT count(*)
    INTO beta_products
    FROM products
    WHERE tenant_id = 'tenant-beta-demo'
      AND id::text LIKE 'b1900000-%';

    IF alpha_products <> 2 THEN
        RAISE EXCEPTION 'tenant-alpha-demo fixture product count = %, want 2', alpha_products;
    END IF;
    IF beta_products <> 2 THEN
        RAISE EXCEPTION 'tenant-beta-demo fixture product count = %, want 2', beta_products;
    END IF;

    SELECT count(*)
    INTO cross_tenant_products
    FROM products
    WHERE id::text LIKE 'b1900000-%'
      AND (
          (tenant_id = 'tenant-alpha-demo' AND sku LIKE 'BETA-%')
          OR (tenant_id = 'tenant-beta-demo' AND sku LIKE 'ALPHA-%')
      );

    IF cross_tenant_products <> 0 THEN
        RAISE EXCEPTION 'cross-tenant product fixture leakage count = %, want 0', cross_tenant_products;
    END IF;

    SELECT count(*)
    INTO cross_tenant_images
    FROM product_images i
    JOIN products p ON p.id = i.product_id
    WHERE i.id::text LIKE 'c1900000-%'
      AND i.tenant_id <> p.tenant_id;

    IF cross_tenant_images <> 0 THEN
        RAISE EXCEPTION 'cross-tenant image fixture leakage count = %, want 0', cross_tenant_images;
    END IF;

    SELECT count(*)
    INTO cross_tenant_media
    FROM product_media_assets m
    JOIN products p ON p.id = m.product_id
    WHERE m.id::text LIKE 'd1900000-%'
      AND m.tenant_id <> p.tenant_id;

    IF cross_tenant_media <> 0 THEN
        RAISE EXCEPTION 'cross-tenant media fixture leakage count = %, want 0', cross_tenant_media;
    END IF;

    SELECT count(*)
    INTO cross_tenant_order_items
    FROM order_items oi
    JOIN orders o ON o.id = oi.order_id
    JOIN products p ON p.id = oi.product_id
    WHERE oi.id::text LIKE 'f1900000-%'
      AND o.tenant_id <> p.tenant_id;

    IF cross_tenant_order_items <> 0 THEN
        RAISE EXCEPTION 'cross-tenant order item fixture leakage count = %, want 0', cross_tenant_order_items;
    END IF;
END $$;

SELECT
    tenant_id,
    count(*) AS fixture_products
FROM products
WHERE id::text LIKE 'b1900000-%'
GROUP BY tenant_id
ORDER BY tenant_id;

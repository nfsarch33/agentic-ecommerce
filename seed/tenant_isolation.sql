-- v1.9.0 deterministic tenant isolation fixtures.
-- Run after make migrate-up. All tenant names, IDs, emails, URLs, and checksums
-- are synthetic and safe for public test data.

INSERT INTO categories (id, name, slug, tenant_id) VALUES
    ('a1900000-0000-0000-0000-000000000001', 'Alpha Strength Fixtures', 'tenant-alpha-demo-strength', 'tenant-alpha-demo'),
    ('a1900000-0000-0000-0000-000000000002', 'Alpha Recovery Fixtures', 'tenant-alpha-demo-recovery', 'tenant-alpha-demo'),
    ('a1900000-0000-0000-0000-000000000003', 'Beta Cardio Fixtures', 'tenant-beta-demo-cardio', 'tenant-beta-demo'),
    ('a1900000-0000-0000-0000-000000000004', 'Beta Wellness Fixtures', 'tenant-beta-demo-wellness', 'tenant-beta-demo')
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    slug = EXCLUDED.slug,
    tenant_id = EXCLUDED.tenant_id,
    updated_at = now();

INSERT INTO products (
    id,
    sku,
    title,
    slug,
    description,
    price_amount,
    price_currency,
    stock,
    status,
    tenant_id
) VALUES
    (
        'b1900000-0000-0000-0000-000000000001',
        'ALPHA-RB-SET',
        'Alpha Demo Resistance Band Set',
        'tenant-alpha-demo-resistance-band-set',
        'Synthetic tenant Alpha fixture for tenant-scoped catalog tests.',
        4995,
        'AUD',
        40,
        'active',
        'tenant-alpha-demo'
    ),
    (
        'b1900000-0000-0000-0000-000000000002',
        'ALPHA-FOAM-45',
        'Alpha Demo Foam Roller',
        'tenant-alpha-demo-foam-roller',
        'Synthetic tenant Alpha recovery fixture for isolation tests.',
        3500,
        'AUD',
        25,
        'active',
        'tenant-alpha-demo'
    ),
    (
        'b1900000-0000-0000-0000-000000000003',
        'BETA-SPEED-ROPE',
        'Beta Demo Speed Rope',
        'tenant-beta-demo-speed-rope',
        'Synthetic tenant Beta cardio fixture for tenant-scoped catalog tests.',
        2495,
        'AUD',
        55,
        'active',
        'tenant-beta-demo'
    ),
    (
        'b1900000-0000-0000-0000-000000000004',
        'BETA-YOGA-MAT',
        'Beta Demo Yoga Mat',
        'tenant-beta-demo-yoga-mat',
        'Synthetic tenant Beta wellness fixture for isolation tests.',
        5495,
        'AUD',
        35,
        'draft',
        'tenant-beta-demo'
    )
ON CONFLICT (id) DO UPDATE SET
    sku = EXCLUDED.sku,
    title = EXCLUDED.title,
    slug = EXCLUDED.slug,
    description = EXCLUDED.description,
    price_amount = EXCLUDED.price_amount,
    price_currency = EXCLUDED.price_currency,
    stock = EXCLUDED.stock,
    status = EXCLUDED.status,
    tenant_id = EXCLUDED.tenant_id,
    updated_at = now();

INSERT INTO product_categories (product_id, category_id) VALUES
    ('b1900000-0000-0000-0000-000000000001', 'a1900000-0000-0000-0000-000000000001'),
    ('b1900000-0000-0000-0000-000000000002', 'a1900000-0000-0000-0000-000000000002'),
    ('b1900000-0000-0000-0000-000000000003', 'a1900000-0000-0000-0000-000000000003'),
    ('b1900000-0000-0000-0000-000000000004', 'a1900000-0000-0000-0000-000000000004')
ON CONFLICT DO NOTHING;

INSERT INTO product_images (id, product_id, url, alt, sort_order, tenant_id) VALUES
    (
        'c1900000-0000-0000-0000-000000000001',
        'b1900000-0000-0000-0000-000000000001',
        '/fixtures/tenant-alpha-demo/resistance-band-set.jpg',
        'Tenant Alpha synthetic resistance band fixture',
        0,
        'tenant-alpha-demo'
    ),
    (
        'c1900000-0000-0000-0000-000000000002',
        'b1900000-0000-0000-0000-000000000003',
        '/fixtures/tenant-beta-demo/speed-rope.jpg',
        'Tenant Beta synthetic speed rope fixture',
        0,
        'tenant-beta-demo'
    )
ON CONFLICT (id) DO UPDATE SET
    product_id = EXCLUDED.product_id,
    url = EXCLUDED.url,
    alt = EXCLUDED.alt,
    sort_order = EXCLUDED.sort_order,
    tenant_id = EXCLUDED.tenant_id;

INSERT INTO product_media_assets (
    id,
    product_id,
    storage_key,
    source_url,
    public_url,
    original_filename,
    mime_type,
    size_bytes,
    checksum_sha256,
    width_px,
    height_px,
    alt_text,
    sort_order,
    tenant_id
) VALUES
    (
        'd1900000-0000-0000-0000-000000000001',
        'b1900000-0000-0000-0000-000000000001',
        'fixtures/tenant-alpha-demo/resistance-band-set.jpg',
        'fixture://tenant-alpha-demo/resistance-band-set',
        '/media/fixtures/tenant-alpha-demo/resistance-band-set.jpg',
        'tenant-alpha-demo-resistance-band-set.jpg',
        'image/jpeg',
        1024,
        '0000000000000000000000000000000000000000000000000000000000001901',
        1200,
        900,
        'Tenant Alpha synthetic resistance band media fixture',
        0,
        'tenant-alpha-demo'
    ),
    (
        'd1900000-0000-0000-0000-000000000002',
        'b1900000-0000-0000-0000-000000000003',
        'fixtures/tenant-beta-demo/speed-rope.jpg',
        'fixture://tenant-beta-demo/speed-rope',
        '/media/fixtures/tenant-beta-demo/speed-rope.jpg',
        'tenant-beta-demo-speed-rope.jpg',
        'image/jpeg',
        1024,
        '0000000000000000000000000000000000000000000000000000000000001902',
        1200,
        900,
        'Tenant Beta synthetic speed rope media fixture',
        0,
        'tenant-beta-demo'
    )
ON CONFLICT (id) DO UPDATE SET
    product_id = EXCLUDED.product_id,
    storage_key = EXCLUDED.storage_key,
    source_url = EXCLUDED.source_url,
    public_url = EXCLUDED.public_url,
    original_filename = EXCLUDED.original_filename,
    mime_type = EXCLUDED.mime_type,
    size_bytes = EXCLUDED.size_bytes,
    checksum_sha256 = EXCLUDED.checksum_sha256,
    width_px = EXCLUDED.width_px,
    height_px = EXCLUDED.height_px,
    alt_text = EXCLUDED.alt_text,
    sort_order = EXCLUDED.sort_order,
    tenant_id = EXCLUDED.tenant_id,
    updated_at = now();

INSERT INTO orders (
    id,
    customer_email,
    status,
    subtotal_amount,
    currency,
    shipping_amount,
    total_amount,
    shipping_name,
    shipping_line1,
    shipping_city,
    shipping_region,
    shipping_postal_code,
    shipping_country,
    tenant_id
) VALUES
    (
        'e1900000-0000-0000-0000-000000000001',
        'shopper.alpha@example.invalid',
        'paid',
        4995,
        'AUD',
        0,
        4995,
        'Alpha Demo Shopper',
        '1 Fixture Lane',
        'Sydney',
        'NSW',
        '2000',
        'AU',
        'tenant-alpha-demo'
    ),
    (
        'e1900000-0000-0000-0000-000000000002',
        'shopper.beta@example.invalid',
        'pending',
        2495,
        'AUD',
        0,
        2495,
        'Beta Demo Shopper',
        '2 Fixture Lane',
        'Melbourne',
        'VIC',
        '3000',
        'AU',
        'tenant-beta-demo'
    )
ON CONFLICT (id) DO UPDATE SET
    customer_email = EXCLUDED.customer_email,
    status = EXCLUDED.status,
    subtotal_amount = EXCLUDED.subtotal_amount,
    currency = EXCLUDED.currency,
    shipping_amount = EXCLUDED.shipping_amount,
    total_amount = EXCLUDED.total_amount,
    shipping_name = EXCLUDED.shipping_name,
    shipping_line1 = EXCLUDED.shipping_line1,
    shipping_city = EXCLUDED.shipping_city,
    shipping_region = EXCLUDED.shipping_region,
    shipping_postal_code = EXCLUDED.shipping_postal_code,
    shipping_country = EXCLUDED.shipping_country,
    tenant_id = EXCLUDED.tenant_id,
    updated_at = now();

INSERT INTO order_items (
    id,
    order_id,
    product_id,
    sku,
    title,
    quantity,
    unit_price_amount,
    currency,
    line_total_amount
) VALUES
    (
        'f1900000-0000-0000-0000-000000000001',
        'e1900000-0000-0000-0000-000000000001',
        'b1900000-0000-0000-0000-000000000001',
        'ALPHA-RB-SET',
        'Alpha Demo Resistance Band Set',
        1,
        4995,
        'AUD',
        4995
    ),
    (
        'f1900000-0000-0000-0000-000000000002',
        'e1900000-0000-0000-0000-000000000002',
        'b1900000-0000-0000-0000-000000000003',
        'BETA-SPEED-ROPE',
        'Beta Demo Speed Rope',
        1,
        2495,
        'AUD',
        2495
    )
ON CONFLICT (id) DO UPDATE SET
    order_id = EXCLUDED.order_id,
    product_id = EXCLUDED.product_id,
    sku = EXCLUDED.sku,
    title = EXCLUDED.title,
    quantity = EXCLUDED.quantity,
    unit_price_amount = EXCLUDED.unit_price_amount,
    currency = EXCLUDED.currency,
    line_total_amount = EXCLUDED.line_total_amount;

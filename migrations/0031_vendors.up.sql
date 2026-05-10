-- v4.8.0: vendors table for marketplace multi-vendor support.
-- Stores vendor profiles with commission rates and lifecycle state.

CREATE TABLE IF NOT EXISTS vendors (
    vendor_id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           TEXT NOT NULL,
    name                TEXT NOT NULL,
    contact_email       TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'deactivated')),
    commission_rate_bps INTEGER NOT NULL DEFAULT 0 CHECK (commission_rate_bps >= 0 AND commission_rate_bps <= 10000),
    joined_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deactivated_at      TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_vendors_tenant_name
    ON vendors (tenant_id, LOWER(name));

CREATE INDEX idx_vendors_tenant_status
    ON vendors (tenant_id, status);

-- Vendor product association: nullable FK on existing products table.
-- Only add if the products table exists (safe for fresh installs).
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'products') THEN
        IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'products' AND column_name = 'vendor_id') THEN
            ALTER TABLE products ADD COLUMN vendor_id UUID REFERENCES vendors(vendor_id);
            CREATE INDEX idx_products_vendor_id ON products (vendor_id);
        END IF;
    END IF;
END $$;

-- RLS: tenant isolation.
ALTER TABLE vendors ENABLE ROW LEVEL SECURITY;

CREATE POLICY vendors_tenant_isolation
    ON vendors
    FOR ALL
    USING (tenant_id = current_setting('app.tenant_id', true));

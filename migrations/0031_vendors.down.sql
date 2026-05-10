-- Reverse vendor product association.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'products' AND column_name = 'vendor_id') THEN
        DROP INDEX IF EXISTS idx_products_vendor_id;
        ALTER TABLE products DROP COLUMN vendor_id;
    END IF;
END $$;

DROP POLICY IF EXISTS vendors_tenant_isolation ON vendors;
DROP TABLE IF EXISTS vendors;

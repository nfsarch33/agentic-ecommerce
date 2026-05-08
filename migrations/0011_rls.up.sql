-- v2.5.0 — Postgres Row-Level Security for tenant-keyed tables.
-- Defence-in-depth so a forgotten WHERE tenant_id=$1 in app code
-- still cannot read another tenant's rows. The middleware layer sets
-- the GUC `app.current_tenant_id` per request; the policy below
-- filters/writes accordingly.
--
-- Tables covered (all tenant_id-keyed):
--   tenants, tenant_settings,
--   memberships, subscriptions, billing_cycles,
--   digital_products, digital_licenses, digital_access_grants, digital_download_tokens,
--   marketplace_installations, marketplace_event_subscriptions,
--   billing_subscriptions, billing_invoices, usage_records.

CREATE OR REPLACE FUNCTION current_tenant_setting() RETURNS TEXT AS $$
DECLARE
    v TEXT;
BEGIN
    BEGIN
        v := current_setting('app.current_tenant_id', TRUE);
    EXCEPTION WHEN OTHERS THEN
        v := NULL;
    END;
    RETURN COALESCE(v, '');
END;
$$ LANGUAGE plpgsql STABLE;

-- enable_tenant_rls applies a uniform policy: when the GUC is empty
-- (admin/super-admin context), all rows are visible; otherwise the
-- visible/writable rows are restricted to the current tenant.
DO $$
DECLARE
    target TEXT;
    targets TEXT[] := ARRAY[
        'tenants',
        'tenant_settings',
        'memberships',
        'subscriptions',
        'billing_cycles',
        'digital_products',
        'digital_licenses',
        'digital_access_grants',
        'digital_download_tokens',
        'marketplace_installations',
        'marketplace_event_subscriptions',
        'billing_subscriptions',
        'billing_invoices',
        'usage_records'
    ];
    column_name TEXT;
BEGIN
    FOREACH target IN ARRAY targets LOOP
        IF NOT EXISTS (
            SELECT 1 FROM information_schema.tables
            WHERE table_name = target AND table_schema = current_schema()
        ) THEN
            CONTINUE;
        END IF;
        -- The tenants table uses `id` (NOT `tenant_id`) as the scoping column.
        IF target = 'tenants' THEN
            column_name := 'id';
        ELSE
            column_name := 'tenant_id';
        END IF;
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', target);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', target);
        EXECUTE format('DROP POLICY IF EXISTS tenant_isolation ON %I', target);
        EXECUTE format($f$
            CREATE POLICY tenant_isolation ON %I
            USING (
                current_tenant_setting() = ''
                OR %I = current_tenant_setting()
            )
            WITH CHECK (
                current_tenant_setting() = ''
                OR %I = current_tenant_setting()
            )
        $f$, target, column_name, column_name);
    END LOOP;
END;
$$;

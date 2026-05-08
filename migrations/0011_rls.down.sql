-- v2.5.0 — Disable Row-Level Security tenant isolation policies.

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
BEGIN
    FOREACH target IN ARRAY targets LOOP
        IF NOT EXISTS (
            SELECT 1 FROM information_schema.tables
            WHERE table_name = target AND table_schema = current_schema()
        ) THEN
            CONTINUE;
        END IF;
        EXECUTE format('DROP POLICY IF EXISTS tenant_isolation ON %I', target);
        EXECUTE format('ALTER TABLE %I NO FORCE ROW LEVEL SECURITY', target);
        EXECUTE format('ALTER TABLE %I DISABLE ROW LEVEL SECURITY', target);
    END LOOP;
END;
$$;

DROP FUNCTION IF EXISTS current_tenant_setting();

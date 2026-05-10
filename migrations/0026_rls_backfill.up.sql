-- v4.1.0 IC-1 — RLS backfill for tables created in migrations 0012-0025.
--
-- Migrations 0001-0011 already have RLS via 0011_rls.up.sql. Tables
-- created after 0011 were NOT covered because the 0011 DO block
-- hard-codes a ARRAY[] of target table names. This migration closes
-- the gap using the same idempotent pattern (IF NOT EXISTS / DROP
-- POLICY IF EXISTS) so it is safe to re-run.
--
-- Materialized views (gmv_daily_rollup, roi_daily_rollup,
-- channel_content_daily_rollup) are excluded because Postgres does
-- not support RLS on materialized views.
--
-- Every table below carries a tenant_id column. The policy is
-- identical to the one in 0011: when the GUC app.current_tenant_id
-- is empty (admin/super-admin context), all rows are visible;
-- otherwise visibility is restricted to the current tenant.

DO $$
DECLARE
    target TEXT;
    targets TEXT[] := ARRAY[
        'marketplace_submissions',
        'supplier_cost_baselines',
        'faq_entries',
        'shipping_labels',
        'returns',
        'competitor_prices',
        'content_calendar_entries',
        'content_performance_history',
        'onboarding_wizards',
        'operator_alerts'
    ];
BEGIN
    FOREACH target IN ARRAY targets LOOP
        IF NOT EXISTS (
            SELECT 1 FROM information_schema.tables
            WHERE table_name = target AND table_schema = current_schema()
        ) THEN
            CONTINUE;
        END IF;

        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', target);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', target);
        EXECUTE format('DROP POLICY IF EXISTS tenant_isolation ON %I', target);
        EXECUTE format($f$
            CREATE POLICY tenant_isolation ON %I
            USING (
                current_tenant_setting() = ''
                OR tenant_id = current_tenant_setting()
            )
            WITH CHECK (
                current_tenant_setting() = ''
                OR tenant_id = current_tenant_setting()
            )
        $f$, target);
    END LOOP;
END;
$$;

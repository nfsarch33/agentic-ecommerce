-- v4.1.0 IC-1 — rollback RLS backfill.

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
        EXECUTE format('DROP POLICY IF EXISTS tenant_isolation ON %I', target);
        EXECUTE format('ALTER TABLE %I DISABLE ROW LEVEL SECURITY', target);
    END LOOP;
END;
$$;

-- v3.9.1 EC-9-4 -- rollback channel content rollup.
DROP INDEX IF EXISTS idx_channel_content_daily_rollup_tenant_channel_day;
DROP INDEX IF EXISTS idx_channel_content_daily_rollup_tenant_day;
DROP INDEX IF EXISTS uq_channel_content_daily_rollup_pk;
DROP MATERIALIZED VIEW IF EXISTS channel_content_daily_rollup;

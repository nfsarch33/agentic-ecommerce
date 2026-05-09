-- v3.6.0 EC-9-1 — Rollback for 0016 gmv_daily_rollup.
DROP INDEX IF EXISTS idx_gmv_daily_rollup_tenant_day;
DROP INDEX IF EXISTS uq_gmv_daily_rollup_tenant_channel_day;
DROP MATERIALIZED VIEW IF EXISTS gmv_daily_rollup;

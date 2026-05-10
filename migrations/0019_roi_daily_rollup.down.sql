-- Reverse v3.8.0 EC-9-3 ROI daily rollup.
DROP INDEX IF EXISTS idx_roi_daily_rollup_tenant_channel_day;
DROP INDEX IF EXISTS idx_roi_daily_rollup_tenant_day;
DROP INDEX IF EXISTS uq_roi_daily_rollup_pk;
DROP MATERIALIZED VIEW IF EXISTS roi_daily_rollup;

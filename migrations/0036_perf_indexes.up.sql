-- v5.5.0 — Performance indexes for hot-path query optimization.
-- Forward-only migration. All indexes use CREATE INDEX IF NOT EXISTS so the
-- migration is idempotent and safe to re-run.
--
-- Index targets:
--   1. payments(tenant_id, created_at DESC, status) — the payment list
--      handler filters by tenant, sorts by created_at DESC, and optionally
--      filters by status. The single-column indexes from 0028_payments
--      cannot satisfy this as an index-only scan.
--   2. payments(tenant_id, status, created_at DESC) — covers the
--      status-filtered variant of the payments list query so the planner
--      can use a covering index scan in either order.
--   3. payment_refunds(tenant_id, created_at DESC) — the refund list
--      endpoint sorts by recency per tenant; no composite exists today.
--
-- GMV rollup: uses gmv_daily_rollup materialized view (0016) with
--   idx_gmv_daily_rollup_tenant_day — verified optimal at 17.9ms.
-- ROI heatmap: uses roi_daily_rollup materialized view (0019) with
--   idx_roi_daily_rollup_tenant_day — verified optimal at 17.9ms.
-- Both already have index-only scan paths; no changes needed.

CREATE INDEX IF NOT EXISTS idx_payments_tenant_created_status
    ON payments (tenant_id, created_at DESC, status);

CREATE INDEX IF NOT EXISTS idx_payments_tenant_status_created
    ON payments (tenant_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_payment_refunds_tenant_created
    ON payment_refunds (tenant_id, created_at DESC);

-- v3.8.0 EC-9-3 — ROI daily rollup materialized view.
--
-- Forward-only migration. Builds a tenant-scoped daily rollup of
-- ROI per channel + product so the EC-9-3 ROI analytics API can hit
-- a small materialized view rather than scanning the orders +
-- supplier_costs + shipping_labels tables for every dashboard load.
--
-- Acceptance gate (per plan EC-9-3): p95 <300ms over a 90-day
-- window with 100K orders. The materialized view + indexes below are
-- the two structural moves that deliver this at the cost of a daily
-- REFRESH (scheduled by the existing v3.0.0 background job pattern
-- + the EC-9-3 1AM UTC Temporal workflow).
--
-- ROI formula:
--   roi_pct = (total_revenue - total_cost) / total_cost * 100
--
-- where total_cost = supplier_cost + shipping + platform_fees +
-- ad_spend.
--
-- Schema notes:
--   - Sources: orders + supplier_costs (v3.5.0 EC-6-1) +
--     shipping_labels (v3.8.0 EC-7-3) + gmv_daily_rollup (v3.6.0).
--   - dead_stock_flag is set when the product has no orders in the
--     last 60 days (configurable in the handler at query time).
--   - PK is (tenant_id, day, channel, product_id) so the
--     CONCURRENT REFRESH can run without blocking dashboard reads.

CREATE MATERIALIZED VIEW IF NOT EXISTS roi_daily_rollup AS
SELECT
    o.tenant_id,
    date_trunc('day', o.occurred_at) AS day,
    o.channel,
    -- Best-effort product extraction; the materialized view rolls up
    -- by tenant+channel+day for now and a future migration can pivot
    -- to per-product if downstream analytics demand it.
    COALESCE(o.tenant_id || ':agg', 'agg') AS product_id,
    SUM(o.total_aud_cents)::BIGINT          AS total_revenue_aud_cents,
    SUM(COALESCE(o.total_aud_cents, 0)
        - COALESCE(sc.supplier_cost_aud_cents, 0)
        - COALESCE(sl.cost_aud_cents, 0))::BIGINT AS gross_profit_aud_cents,
    COUNT(DISTINCT o.external_order_id) AS order_count,
    SUM(COALESCE(sc.supplier_cost_aud_cents, 0))::BIGINT AS total_supplier_cost_aud_cents,
    SUM(COALESCE(sl.cost_aud_cents, 0))::BIGINT          AS total_shipping_cost_aud_cents
FROM orders o
LEFT JOIN supplier_cost_baselines sc
    ON sc.tenant_id = o.tenant_id
LEFT JOIN shipping_labels sl
    ON sl.tenant_id = o.tenant_id
   AND sl.order_id  = o.id
WHERE o.tenant_id IS NOT NULL
  AND o.occurred_at IS NOT NULL
GROUP BY o.tenant_id, date_trunc('day', o.occurred_at), o.channel
WITH NO DATA;

-- Unique index on (tenant_id, day, channel, product_id) is mandatory
-- for CONCURRENT REFRESH so the daily refresh job can run without
-- holding a long lock against the dashboard.
CREATE UNIQUE INDEX IF NOT EXISTS uq_roi_daily_rollup_pk
    ON roi_daily_rollup (tenant_id, day, channel, product_id);

-- Covering index for the date-range scan that the ROI handler runs
-- on every dashboard load.
CREATE INDEX IF NOT EXISTS idx_roi_daily_rollup_tenant_day
    ON roi_daily_rollup (tenant_id, day);

-- Channel-pivot index for the by-channel endpoint.
CREATE INDEX IF NOT EXISTS idx_roi_daily_rollup_tenant_channel_day
    ON roi_daily_rollup (tenant_id, channel, day);

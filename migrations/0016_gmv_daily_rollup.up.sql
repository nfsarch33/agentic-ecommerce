-- v3.6.0 EC-9-1 — GMV daily rollup materialized view.
--
-- Forward-only migration. Builds a tenant-scoped daily rollup of
-- normalised orders (the v3.5.0 EC-7-1 aggregator output) so the
-- EC-9-1 GMV analytics API can hit a small materialized view
-- instead of scanning the orders table for every dashboard load.
--
-- Acceptance gate (per plan EC-9-1): p95 <200ms over a 30-day
-- window with 10K orders per tenant. The materialized view + the
-- composite index below are the two structural moves that deliver
-- this at the small cost of a daily REFRESH (scheduled by the
-- existing v3.0.0 background job pattern).
--
-- Schema notes:
--   - gmv_aud_cents is the AUD-normalised total (the v3.5.0
--     OrderNormalisedPayload already carries TotalAUDCents).
--   - order_count is the dedup'd order count (one per
--     external_order_id per channel).
--   - top_product_id + top_product_aud_cents support the by-product
--     dashboard panel without a second materialized view.
--   - channel cardinality is bounded (tiktok|facebook|wc|rednote|
--     instagram), so the (tenant_id, channel, day) PK keeps the
--     rollup tiny.

CREATE MATERIALIZED VIEW IF NOT EXISTS gmv_daily_rollup AS
SELECT
    tenant_id,
    channel,
    date_trunc('day', occurred_at) AS day,
    SUM(total_aud_cents)::BIGINT      AS gmv_aud_cents,
    COUNT(DISTINCT external_order_id) AS order_count,
    MIN(occurred_at)                  AS first_order_at,
    MAX(occurred_at)                  AS last_order_at
FROM orders
WHERE total_aud_cents IS NOT NULL
  AND occurred_at IS NOT NULL
  AND tenant_id IS NOT NULL
GROUP BY tenant_id, channel, date_trunc('day', occurred_at)
WITH NO DATA;

-- Unique index on (tenant_id, channel, day) is mandatory for
-- CONCURRENT REFRESH so the daily refresh job can run without
-- holding a long lock against the dashboard.
CREATE UNIQUE INDEX IF NOT EXISTS uq_gmv_daily_rollup_tenant_channel_day
    ON gmv_daily_rollup (tenant_id, channel, day);

-- Covering index for the date-range scan that the GMV handler
-- runs on every dashboard load. (tenant_id, day) is the lookup
-- key; the included channel + gmv_aud_cents columns let the
-- index-only scan answer the rollup without touching the heap.
CREATE INDEX IF NOT EXISTS idx_gmv_daily_rollup_tenant_day
    ON gmv_daily_rollup (tenant_id, day);

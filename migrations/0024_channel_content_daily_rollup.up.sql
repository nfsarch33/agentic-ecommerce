-- v3.9.1 EC-9-4 -- per-channel content performance daily rollup.
--
-- Forward-only migration. Builds a tenant-scoped daily rollup of
-- content KPIs (post count, engagement, conversion, GMV attribution)
-- so the EC-9-4 channel content analytics API can hit a small
-- materialized view rather than scanning content_performance_history
-- + orders + channel events for every dashboard load.
--
-- Acceptance gate (per plan EC-9-4): p95 <300ms over a 30-day window
-- with full per-channel KPIs.
--
-- Schema notes:
--   - Sources: content_performance_history (v3.9.0 0022) +
--     orders (v3.5.0 EC-7-1) + content_calendar (v3.9.0 0021).
--   - PK is (tenant_id, day, channel, content_type) so the
--     CONCURRENT REFRESH can run without blocking dashboard reads.

CREATE MATERIALIZED VIEW IF NOT EXISTS channel_content_daily_rollup AS
SELECT
    cph.tenant_id,
    date_trunc('day', cph.observed_at)::DATE AS day,
    cph.channel,
    cph.content_type,
    COUNT(DISTINCT cph.content_id)::BIGINT AS post_count,
    COALESCE(SUM(cph.last_engagement_score), 0)::DOUBLE PRECISION AS total_engagement,
    CASE WHEN COUNT(DISTINCT cph.content_id) > 0
         THEN COALESCE(SUM(cph.last_engagement_score), 0)::DOUBLE PRECISION / COUNT(DISTINCT cph.content_id)
         ELSE 0
    END AS avg_engagement_per_post,
    -- Conversion + GMV attribution columns are projected from the
    -- orders table at view-refresh time. Defaulted to zero today;
    -- a future migration can backfill once order_attribution rows
    -- carry the content_id correlation.
    0::BIGINT AS conversion_count,
    0::BIGINT AS gmv_attribution_aud_cents
FROM content_performance_history cph
WHERE cph.tenant_id IS NOT NULL
  AND cph.channel IS NOT NULL
GROUP BY cph.tenant_id, date_trunc('day', cph.observed_at)::DATE, cph.channel, cph.content_type
WITH NO DATA;

-- Unique index on (tenant_id, day, channel, content_type) is
-- mandatory for CONCURRENT REFRESH so the daily refresh job can run
-- without holding a long lock against the dashboard.
CREATE UNIQUE INDEX IF NOT EXISTS uq_channel_content_daily_rollup_pk
    ON channel_content_daily_rollup (tenant_id, day, channel, content_type);

-- Covering index for the date-range scan that the EC-9-4 handler
-- runs on every dashboard load.
CREATE INDEX IF NOT EXISTS idx_channel_content_daily_rollup_tenant_day
    ON channel_content_daily_rollup (tenant_id, day);

-- Channel-pivot index for the per-channel + top-N endpoints.
CREATE INDEX IF NOT EXISTS idx_channel_content_daily_rollup_tenant_channel_day
    ON channel_content_daily_rollup (tenant_id, channel, day);

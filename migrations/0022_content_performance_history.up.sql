-- v3.9.0 EC-5-5 -- Content performance EMA history.
--
-- Forward-only migration. Stores per-(tenant, channel, content_type,
-- content_id) EMA-smoothed performance score the EC-5-5 feedback
-- loop maintains. ema_score is the latest exponential moving
-- average; sample_count is the number of engagement observations
-- folded into it; alpha is the smoothing coefficient used for the
-- rollup (default 0.2).
--
-- RLS-aware: the existing migrations/0011_rls.up.sql DO block applies
-- the uniform tenant_id=current_tenant_setting() policy when this
-- table is registered there.

CREATE TABLE IF NOT EXISTS content_performance_history (
    tenant_id     TEXT        NOT NULL,
    content_id    TEXT        NOT NULL,
    channel       TEXT        NOT NULL,
    content_type  TEXT        NOT NULL,
    ema_score     DOUBLE PRECISION NOT NULL DEFAULT 0,
    sample_count  INTEGER     NOT NULL DEFAULT 0,
    alpha         DOUBLE PRECISION NOT NULL DEFAULT 0.2,
    last_engagement_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    observed_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, content_id, channel),
    CONSTRAINT content_performance_channel_chk CHECK (
        channel IN ('tiktok', 'rednote', 'facebook', 'instagram')
    ),
    CONSTRAINT content_performance_content_type_chk CHECK (
        content_type IN ('video', 'post', 'reel', 'story', 'live')
    ),
    CONSTRAINT content_performance_alpha_bounds
        CHECK (alpha > 0 AND alpha <= 1)
);

CREATE INDEX IF NOT EXISTS idx_content_performance_tenant_channel
    ON content_performance_history (tenant_id, channel);

CREATE INDEX IF NOT EXISTS idx_content_performance_tenant_observed
    ON content_performance_history (tenant_id, observed_at DESC);

-- v3.9.0 EC-6-4 -- Competitor price intelligence persistence.
--
-- Forward-only migration. Stores per-(tenant, sku, channel,
-- competitor_id) latest observed price + a JSON history array so
-- the EC-6-4 scraper can detect deltas vs. the prior observation
-- and feed CompetitorUndercutEvent into the EC-6-3 dynamic pricing
-- agent.
--
-- The (tenant_id, sku, channel, competitor_id) PK keeps the
-- upsert path simple; the (tenant_id, observed_at) covering index
-- supports the daily monitor sweep ("re-scrape competitors older
-- than 23h"). RLS-aware: the existing migrations/0011_rls.up.sql
-- DO block applies the uniform tenant_id=current_tenant_setting()
-- policy when this table is registered there.

CREATE TABLE IF NOT EXISTS competitor_prices (
    tenant_id            TEXT        NOT NULL,
    sku                  TEXT        NOT NULL,
    channel              TEXT        NOT NULL,
    competitor_id        TEXT        NOT NULL,
    competitor_name      TEXT        NOT NULL DEFAULT '',
    competitor_url       TEXT        NOT NULL DEFAULT '',
    observed_price_aud_cents INTEGER NOT NULL,
    last_delta_pct       DOUBLE PRECISION NOT NULL DEFAULT 0,
    price_history        JSONB       NOT NULL DEFAULT '[]'::JSONB,
    image_fingerprint    TEXT        NOT NULL DEFAULT '',
    observed_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, sku, channel, competitor_id),
    CONSTRAINT competitor_prices_channel_chk CHECK (
        channel IN ('tiktok', 'rednote', 'facebook', 'instagram', 'amazon_au', 'catch', 'mydeal')
    ),
    CONSTRAINT competitor_prices_observed_positive
        CHECK (observed_price_aud_cents >= 0)
);

CREATE INDEX IF NOT EXISTS idx_competitor_prices_tenant_observed
    ON competitor_prices (tenant_id, observed_at DESC);

CREATE INDEX IF NOT EXISTS idx_competitor_prices_tenant_sku
    ON competitor_prices (tenant_id, sku);

CREATE INDEX IF NOT EXISTS idx_competitor_prices_tenant_channel
    ON competitor_prices (tenant_id, channel);

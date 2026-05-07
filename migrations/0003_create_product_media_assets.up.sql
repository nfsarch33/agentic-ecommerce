CREATE TABLE IF NOT EXISTS product_media_assets (
    id                UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    product_id        UUID REFERENCES products(id) ON DELETE SET NULL,
    storage_key       TEXT NOT NULL UNIQUE,
    source_url        TEXT NOT NULL DEFAULT '',
    public_url        TEXT NOT NULL DEFAULT '',
    original_filename TEXT NOT NULL DEFAULT '',
    mime_type         TEXT NOT NULL,
    size_bytes        BIGINT NOT NULL,
    checksum_sha256   TEXT NOT NULL DEFAULT '',
    width_px          INTEGER,
    height_px         INTEGER,
    alt_text          TEXT NOT NULL DEFAULT '',
    sort_order        INTEGER NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT product_media_assets_size_non_negative CHECK (size_bytes >= 0),
    CONSTRAINT product_media_assets_width_positive CHECK (width_px IS NULL OR width_px > 0),
    CONSTRAINT product_media_assets_height_positive CHECK (height_px IS NULL OR height_px > 0)
);

CREATE INDEX idx_product_media_assets_product_id ON product_media_assets(product_id, sort_order);
CREATE INDEX idx_product_media_assets_mime_type ON product_media_assets(mime_type);

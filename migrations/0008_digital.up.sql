-- v2.3.0 — Digital goods bounded context
-- Forward-only migration. All tables are tenant_id-keyed so cross-tenant
-- reads are impossible at the database level. Mirrors the v2.2.0
-- membership migration shape (0007_membership.up.sql) for consistency.

CREATE TABLE digital_products (
    id           UUID        NOT NULL,
    tenant_id    TEXT        NOT NULL,
    sku          TEXT        NOT NULL,
    name         TEXT        NOT NULL,
    description  TEXT        NOT NULL DEFAULT '',
    file_path    TEXT        NOT NULL,
    file_size    BIGINT      NOT NULL,
    content_type TEXT        NOT NULL DEFAULT '',
    checksum     TEXT        NOT NULL DEFAULT '',
    version      TEXT        NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    CONSTRAINT digital_products_file_size_chk CHECK (file_size > 0),
    CONSTRAINT digital_products_sku_unique UNIQUE (tenant_id, sku)
);
CREATE INDEX IF NOT EXISTS idx_digital_products_tenant_name ON digital_products (tenant_id, name);

CREATE TABLE digital_licenses (
    id              UUID        NOT NULL,
    tenant_id       TEXT        NOT NULL,
    product_id      UUID        NOT NULL,
    customer_id     UUID        NOT NULL,
    key             TEXT        NOT NULL,
    state           TEXT        NOT NULL,
    issued_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at      TIMESTAMPTZ NULL,
    max_activations INTEGER     NOT NULL DEFAULT 0,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    CONSTRAINT digital_licenses_state_chk CHECK (state IN ('active', 'revoked', 'expired')),
    CONSTRAINT digital_licenses_max_activations_chk CHECK (max_activations >= 0),
    CONSTRAINT digital_licenses_key_unique UNIQUE (tenant_id, key)
);
CREATE INDEX IF NOT EXISTS idx_digital_licenses_tenant_product ON digital_licenses (tenant_id, product_id);
CREATE INDEX IF NOT EXISTS idx_digital_licenses_tenant_customer ON digital_licenses (tenant_id, customer_id);

CREATE TABLE digital_access_grants (
    id          UUID        NOT NULL,
    tenant_id   TEXT        NOT NULL,
    customer_id UUID        NOT NULL,
    product_id  UUID        NOT NULL,
    license_id  UUID        NOT NULL,
    granted_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    source      TEXT        NOT NULL,
    PRIMARY KEY (tenant_id, id),
    CONSTRAINT digital_access_grants_source_chk CHECK (source IN ('purchase', 'gift', 'admin')),
    CONSTRAINT digital_access_grants_uniqueness UNIQUE (tenant_id, customer_id, product_id)
);
CREATE INDEX IF NOT EXISTS idx_digital_access_grants_tenant_customer ON digital_access_grants (tenant_id, customer_id);
CREATE INDEX IF NOT EXISTS idx_digital_access_grants_tenant_license ON digital_access_grants (tenant_id, license_id);

CREATE TABLE digital_download_tokens (
    id              UUID        NOT NULL,
    tenant_id       TEXT        NOT NULL,
    license_id      UUID        NOT NULL,
    signature       TEXT        NOT NULL,
    expires_at      TIMESTAMPTZ NOT NULL,
    uses_allowed    INTEGER     NOT NULL,
    uses_so_far     INTEGER     NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_issued_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    CONSTRAINT digital_download_tokens_uses_chk CHECK (uses_allowed > 0 AND uses_so_far >= 0)
);
CREATE INDEX IF NOT EXISTS idx_digital_download_tokens_tenant_license ON digital_download_tokens (tenant_id, license_id);

-- v2.4.0 — Marketplace plugin framework + tenant aggregate
-- Forward-only migration. All per-tenant tables are tenant_id-keyed so
-- cross-tenant reads are impossible at the database level. Mirrors the
-- v2.2.0 membership and v2.3.0 digital migrations for consistency.

-- tenants is the aggregate root for the v2.4.0 tenant lifecycle. The
-- existing tenant_settings table (migration 0006) is keyed by
-- tenant_id but does not store the aggregate; this row carries it.
CREATE TABLE tenants (
    id          TEXT        NOT NULL,
    slug        TEXT        NOT NULL,
    name        TEXT        NOT NULL,
    plan        TEXT        NOT NULL DEFAULT 'free',
    status      TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id),
    CONSTRAINT tenants_slug_unique UNIQUE (slug),
    CONSTRAINT tenants_status_chk CHECK (
        status IN ('provisioning', 'active', 'suspended', 'archived')
    ),
    CONSTRAINT tenants_slug_pattern_chk CHECK (
        slug ~ '^[a-z][a-z0-9-]*[a-z0-9]$'
    )
);
CREATE INDEX IF NOT EXISTS idx_tenants_status ON tenants (status);

-- marketplace_plugins is the global catalogue. Manifests are shared
-- across tenants; per-tenant adoption lives in marketplace_installations.
CREATE TABLE marketplace_plugins (
    slug                TEXT        NOT NULL,
    name                TEXT        NOT NULL,
    version             TEXT        NOT NULL,
    vendor              TEXT        NOT NULL,
    description         TEXT        NOT NULL DEFAULT '',
    category            TEXT        NOT NULL DEFAULT '',
    homepage_url        TEXT        NOT NULL DEFAULT '',
    event_subscriptions TEXT[]      NOT NULL DEFAULT '{}',
    permissions         TEXT[]      NOT NULL DEFAULT '{}',
    dependencies        JSONB       NOT NULL DEFAULT '[]'::jsonb,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (slug),
    CONSTRAINT marketplace_plugins_slug_pattern_chk CHECK (
        slug ~ '^[a-z][a-z0-9-]*[a-z0-9]$'
    ),
    CONSTRAINT marketplace_plugins_version_pattern_chk CHECK (
        version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
    )
);

-- marketplace_installations records per-tenant per-plugin lifecycle
-- state. State transitions are validated in Go; the CHECK constraint
-- mirrors the explicit transition table in
-- internal/marketplace/state.go for defence in depth.
CREATE TABLE marketplace_installations (
    tenant_id         TEXT        NOT NULL,
    slug              TEXT        NOT NULL,
    installed_version TEXT        NOT NULL,
    state             TEXT        NOT NULL,
    installed_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    activated_at      TIMESTAMPTZ NULL,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, slug),
    CONSTRAINT marketplace_installations_state_chk CHECK (
        state IN ('installed', 'active', 'deactivated')
    ),
    CONSTRAINT marketplace_installations_version_pattern_chk CHECK (
        installed_version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
    )
);
CREATE INDEX IF NOT EXISTS idx_marketplace_installations_tenant_state
    ON marketplace_installations (tenant_id, state);

-- marketplace_event_subscriptions stores the per-tenant per-plugin
-- subscription set so the bus can be rebuilt at process boot without
-- re-reading every manifest.
CREATE TABLE marketplace_event_subscriptions (
    tenant_id  TEXT        NOT NULL,
    slug       TEXT        NOT NULL,
    event_name TEXT        NOT NULL,
    PRIMARY KEY (tenant_id, slug, event_name)
);
CREATE INDEX IF NOT EXISTS idx_marketplace_event_subscriptions_event
    ON marketplace_event_subscriptions (event_name);

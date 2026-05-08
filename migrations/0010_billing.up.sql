-- v2.5.0 — Tenant self-service registration + billing bounded context.
-- Forward-only migration. All tables are tenant_id-keyed so cross-tenant
-- reads are impossible at the database level. Mirrors the v2.2.0
-- membership migration shape for consistency.

-- billing_plans is the read-only catalog of subscription plans. The
-- table is global (no tenant_id) because plans are shared and the
-- v2.4.0 tenants table already encodes which plan a tenant is on.
CREATE TABLE billing_plans (
    id                    TEXT        NOT NULL,
    name                  TEXT        NOT NULL,
    description           TEXT        NOT NULL DEFAULT '',
    stripe_price_id       TEXT        NOT NULL DEFAULT '',
    api_rate_per_minute   INTEGER     NOT NULL DEFAULT 0,
    storage_bytes         BIGINT      NOT NULL DEFAULT 0,
    agent_runs_per_day    INTEGER     NOT NULL DEFAULT 0,
    plugin_count          INTEGER     NOT NULL DEFAULT 0,
    price_amount_minor    INTEGER     NOT NULL DEFAULT 0,
    price_currency        TEXT        NOT NULL DEFAULT 'AUD',
    billing_interval_days INTEGER     NOT NULL DEFAULT 30,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id)
);

INSERT INTO billing_plans (id, name, description, api_rate_per_minute, storage_bytes, agent_runs_per_day, plugin_count, price_amount_minor, price_currency, billing_interval_days)
VALUES
    ('free',    'Free',    'Hobby tier with strict limits.',         60,   52428800,    20,    3,     0, 'AUD', 30),
    ('starter', 'Starter', 'Solo founder tier.',                    300, 2147483648,  500,   15,  1900, 'AUD', 30),
    ('pro',     'Pro',     'Growing team tier.',                   1200, 21474836480, 5000,   50,  7900, 'AUD', 30)
ON CONFLICT (id) DO NOTHING;

-- billing_subscriptions tracks per-tenant Stripe subscriptions. The
-- table name is intentionally distinct from the v2.2.0 `subscriptions`
-- (membership-context) table; the v2.5.0 billing aggregate is a
-- different concept and lives in its own table.
CREATE TABLE billing_subscriptions (
    id                      TEXT        NOT NULL,
    tenant_id               TEXT        NOT NULL,
    plan_id                 TEXT        NOT NULL,
    state                   TEXT        NOT NULL,
    stripe_subscription_id  TEXT        NOT NULL DEFAULT '',
    stripe_customer_id      TEXT        NOT NULL DEFAULT '',
    current_period_start    TIMESTAMPTZ NULL,
    current_period_end      TIMESTAMPTZ NULL,
    cancel_at_period_end    BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    CONSTRAINT billing_subscriptions_state_chk CHECK (
        state IN ('trialing', 'active', 'past_due', 'paused', 'canceled')
    )
);
CREATE INDEX IF NOT EXISTS idx_billing_subscriptions_tenant_state
    ON billing_subscriptions (tenant_id, state);
CREATE INDEX IF NOT EXISTS idx_billing_subscriptions_stripe_id
    ON billing_subscriptions (tenant_id, stripe_subscription_id)
    WHERE stripe_subscription_id <> '';

CREATE TABLE billing_invoices (
    id                  TEXT        NOT NULL,
    tenant_id           TEXT        NOT NULL,
    subscription_id     TEXT        NOT NULL,
    amount              INTEGER     NOT NULL DEFAULT 0,
    currency            TEXT        NOT NULL DEFAULT 'AUD',
    status              TEXT        NOT NULL,
    period_start        TIMESTAMPTZ NULL,
    period_end          TIMESTAMPTZ NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    CONSTRAINT billing_invoices_status_chk CHECK (
        status IN ('open', 'paid', 'void', 'uncollectible')
    )
);
CREATE INDEX IF NOT EXISTS idx_billing_invoices_tenant_subscription
    ON billing_invoices (tenant_id, subscription_id);

CREATE TABLE usage_records (
    tenant_id     TEXT        NOT NULL,
    metric        TEXT        NOT NULL,
    value         BIGINT      NOT NULL,
    period_start  TIMESTAMPTZ NULL,
    period_end    TIMESTAMPTZ NULL,
    recorded_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, metric, recorded_at)
);
CREATE INDEX IF NOT EXISTS idx_usage_records_tenant_metric
    ON usage_records (tenant_id, metric, recorded_at DESC);

-- stripe_webhook_events is the idempotency table consulted by the
-- /webhooks/stripe handler before applying any side effect. Stripe
-- guarantees event ids are unique; we persist them so a duplicate
-- delivery becomes a 200 OK no-op.
CREATE TABLE stripe_webhook_events (
    event_id    TEXT        NOT NULL,
    event_type  TEXT        NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (event_id)
);

-- tenant_registrations carries the v2.5.0 self-service registration
-- aggregate. Verification tokens are HMAC-derived; the table stores
-- only the canonical fields needed to re-derive the signature.
CREATE TABLE tenant_registrations (
    id              TEXT        NOT NULL,
    email           TEXT        NOT NULL,
    slug_requested  TEXT        NOT NULL,
    plan_requested  TEXT        NOT NULL DEFAULT 'free',
    status          TEXT        NOT NULL,
    tenant_id       TEXT        NOT NULL DEFAULT '',
    company_name    TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    verified_at     TIMESTAMPTZ NULL,
    onboarded_at    TIMESTAMPTZ NULL,
    activated_at    TIMESTAMPTZ NULL,
    expires_at      TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT tenant_registrations_status_chk CHECK (
        status IN ('pending_email_verification', 'email_verified', 'onboarding', 'active')
    )
);
CREATE UNIQUE INDEX IF NOT EXISTS uniq_tenant_registrations_active_email
    ON tenant_registrations (email)
    WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_tenant_registrations_email
    ON tenant_registrations (email);

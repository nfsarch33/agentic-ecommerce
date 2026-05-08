-- v2.2.0 — Membership bounded context
-- Forward-only migration. All tables are tenant_id-keyed so cross-tenant
-- reads are impossible at the database level. Money fields follow the
-- same minor-units + currency convention used by orders/products.

CREATE TABLE membership_plans (
    id              UUID        NOT NULL,
    tenant_id       TEXT        NOT NULL,
    name            TEXT        NOT NULL,
    description     TEXT        NOT NULL DEFAULT '',
    billing_cycle   TEXT        NOT NULL,
    price_amount    INTEGER     NOT NULL,
    currency        TEXT        NOT NULL,
    benefits        TEXT[]      NOT NULL DEFAULT '{}',
    stripe_price_id TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    CONSTRAINT membership_plans_billing_cycle_chk CHECK (
        billing_cycle IN ('monthly', 'quarterly', 'annual')
    ),
    CONSTRAINT membership_plans_price_chk CHECK (price_amount > 0)
);
CREATE INDEX IF NOT EXISTS idx_membership_plans_tenant_name ON membership_plans (tenant_id, name);

CREATE TABLE memberships (
    id         UUID        NOT NULL,
    tenant_id  TEXT        NOT NULL,
    email      TEXT        NOT NULL,
    joined_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, email)
);
CREATE INDEX IF NOT EXISTS idx_memberships_tenant_email ON memberships (tenant_id, email);

CREATE TABLE subscriptions (
    id                     UUID        NOT NULL,
    tenant_id              TEXT        NOT NULL,
    member_id              UUID        NOT NULL,
    plan_id                UUID        NOT NULL,
    state                  TEXT        NOT NULL,
    current_period_start   TIMESTAMPTZ NOT NULL,
    current_period_end     TIMESTAMPTZ NOT NULL,
    trial_ends_at          TIMESTAMPTZ NOT NULL,
    stripe_subscription_id TEXT        NOT NULL DEFAULT '',
    cancelled_at           TIMESTAMPTZ NULL,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    CONSTRAINT subscriptions_state_chk CHECK (
        state IN ('trial', 'active', 'paused', 'cancelled', 'expired')
    )
);
CREATE INDEX IF NOT EXISTS idx_subscriptions_tenant_member ON subscriptions (tenant_id, member_id);
CREATE INDEX IF NOT EXISTS idx_subscriptions_tenant_state ON subscriptions (tenant_id, state);

CREATE TABLE billing_cycles (
    subscription_id UUID        NOT NULL,
    tenant_id       TEXT        NOT NULL,
    cycle_index     INTEGER     NOT NULL,
    period_start    TIMESTAMPTZ NOT NULL,
    period_end      TIMESTAMPTZ NOT NULL,
    amount          INTEGER     NOT NULL,
    currency        TEXT        NOT NULL,
    charged_at      TIMESTAMPTZ NULL,
    PRIMARY KEY (tenant_id, subscription_id, cycle_index),
    CONSTRAINT billing_cycles_amount_chk CHECK (amount > 0)
);
CREATE INDEX IF NOT EXISTS idx_billing_cycles_tenant_subscription ON billing_cycles (tenant_id, subscription_id);

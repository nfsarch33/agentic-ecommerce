-- v4.2.0 payment foundation: multi-provider payments table.
-- Tenant-aware with RLS, idempotency via unique (tenant_id, order_id, provider).

CREATE TYPE payment_provider AS ENUM ('stripe', 'alipay', 'wechat', 'paypal');
CREATE TYPE payment_status AS ENUM ('pending', 'succeeded', 'failed', 'refunded');

CREATE TABLE IF NOT EXISTS payments (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       TEXT NOT NULL,
    order_id        TEXT NOT NULL,
    payment_id      TEXT NOT NULL,
    provider        payment_provider NOT NULL,
    status          payment_status NOT NULL DEFAULT 'pending',
    amount_cents    BIGINT NOT NULL CHECK (amount_cents > 0),
    currency        TEXT NOT NULL DEFAULT 'AUD',
    external_ref    TEXT,
    fail_reason     TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, order_id, provider)
);

CREATE INDEX IF NOT EXISTS idx_payments_tenant_id ON payments(tenant_id);
CREATE INDEX IF NOT EXISTS idx_payments_order_id ON payments(order_id);
CREATE INDEX IF NOT EXISTS idx_payments_status ON payments(status);
CREATE INDEX IF NOT EXISTS idx_payments_provider ON payments(provider);
CREATE INDEX IF NOT EXISTS idx_payments_created_at ON payments(created_at DESC);

-- RLS policy mirroring the v2.6.0 pattern from 0011_rls.up.sql.
ALTER TABLE payments ENABLE ROW LEVEL SECURITY;

CREATE POLICY payments_tenant_isolation ON payments
    USING (tenant_id = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id = current_setting('app.current_tenant', true));

-- Payment refunds table for tracking refund lifecycle.
CREATE TABLE IF NOT EXISTS payment_refunds (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       TEXT NOT NULL,
    payment_id      TEXT NOT NULL REFERENCES payments(payment_id) DEFERRABLE,
    refund_id       TEXT NOT NULL,
    provider        payment_provider NOT NULL,
    amount_cents    BIGINT NOT NULL CHECK (amount_cents > 0),
    currency        TEXT NOT NULL DEFAULT 'AUD',
    status          TEXT NOT NULL DEFAULT 'pending',
    external_ref    TEXT,
    reason          TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, refund_id)
);

CREATE INDEX IF NOT EXISTS idx_payment_refunds_tenant_id ON payment_refunds(tenant_id);
CREATE INDEX IF NOT EXISTS idx_payment_refunds_payment_id ON payment_refunds(payment_id);

ALTER TABLE payment_refunds ENABLE ROW LEVEL SECURITY;

CREATE POLICY payment_refunds_tenant_isolation ON payment_refunds
    USING (tenant_id = current_setting('app.current_tenant', true))
    WITH CHECK (tenant_id = current_setting('app.current_tenant', true));

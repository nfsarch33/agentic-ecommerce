-- v4.8.0: vendor_payouts table for commission payout tracking.
-- Records payout periods and amounts per vendor.

CREATE TABLE IF NOT EXISTS vendor_payouts (
    payout_id    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vendor_id    UUID NOT NULL REFERENCES vendors(vendor_id),
    tenant_id    TEXT NOT NULL,
    amount_cents BIGINT NOT NULL CHECK (amount_cents >= 0),
    status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'paid', 'failed')),
    period_start TIMESTAMPTZ NOT NULL,
    period_end   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_vendor_payouts_vendor_period
    ON vendor_payouts (tenant_id, vendor_id, period_start, period_end);

CREATE INDEX idx_vendor_payouts_status
    ON vendor_payouts (tenant_id, status);

-- RLS: tenant isolation.
ALTER TABLE vendor_payouts ENABLE ROW LEVEL SECURITY;

CREATE POLICY vendor_payouts_tenant_isolation
    ON vendor_payouts
    FOR ALL
    USING (tenant_id = current_setting('app.tenant_id', true));

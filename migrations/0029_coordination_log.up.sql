-- v4.7.0: coordination_log table for MADRL decision audit trail.
-- Stores every coordination decision for offline analysis and
-- reward signal tracking.

CREATE TABLE IF NOT EXISTS coordination_log (
    coordination_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       TEXT NOT NULL,
    sku             TEXT NOT NULL,
    agents          JSONB NOT NULL DEFAULT '[]',
    conflict_type   TEXT NOT NULL,
    resolution      TEXT NOT NULL,
    policy_name     TEXT NOT NULL DEFAULT 'weighted_priority',
    chosen_agent    TEXT NOT NULL,
    reward_value    DOUBLE PRECISION DEFAULT 0.0,
    metadata        JSONB DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_coordination_log_tenant_created
    ON coordination_log (tenant_id, created_at DESC);

CREATE INDEX idx_coordination_log_sku
    ON coordination_log (sku);

-- RLS: tenant isolation per the v4.7.0 per-tenant metrics story.
ALTER TABLE coordination_log ENABLE ROW LEVEL SECURITY;

CREATE POLICY coordination_log_tenant_isolation
    ON coordination_log
    FOR ALL
    USING (tenant_id = current_setting('app.tenant_id', true));

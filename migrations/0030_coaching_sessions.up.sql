-- v4.8.0: coaching_sessions table for operator coaching history.
-- Stores every coaching tip delivered to operators for audit and
-- acceptance tracking.

CREATE TABLE IF NOT EXISTS coaching_sessions (
    session_id  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   TEXT NOT NULL,
    operator_id TEXT NOT NULL,
    context     TEXT NOT NULL CHECK (context IN ('onboarding', 'pricing_strategy', 'channel_optimization', 'inventory_management')),
    tip         TEXT NOT NULL,
    source      TEXT NOT NULL CHECK (source IN ('llm', 'rule')),
    accepted    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_coaching_sessions_tenant_operator
    ON coaching_sessions (tenant_id, operator_id, created_at DESC);

CREATE INDEX idx_coaching_sessions_context
    ON coaching_sessions (tenant_id, context);

-- RLS: tenant isolation.
ALTER TABLE coaching_sessions ENABLE ROW LEVEL SECURITY;

CREATE POLICY coaching_sessions_tenant_isolation
    ON coaching_sessions
    FOR ALL
    USING (tenant_id = current_setting('app.tenant_id', true));

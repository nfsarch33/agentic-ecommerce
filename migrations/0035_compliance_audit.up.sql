-- v4.9.0 Story 2: Compliance audit log.
-- Every deletion and export operation is logged for regulatory audit trail.

CREATE TABLE IF NOT EXISTS compliance_audit_log (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  TEXT NOT NULL,
    subject_id TEXT NOT NULL,
    action     TEXT NOT NULL CHECK (action IN ('right_to_delete', 'data_export')),
    details    TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_compliance_audit_log_tenant_subject
    ON compliance_audit_log (tenant_id, subject_id);

CREATE INDEX idx_compliance_audit_log_action
    ON compliance_audit_log (tenant_id, action, created_at DESC);

-- RLS: tenant isolation.
ALTER TABLE compliance_audit_log ENABLE ROW LEVEL SECURITY;

CREATE POLICY compliance_audit_log_tenant_isolation
    ON compliance_audit_log
    FOR ALL
    USING (tenant_id = current_setting('app.tenant_id', true));

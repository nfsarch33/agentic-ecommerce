-- v4.9.0 Story 2: GDPR/CCPA consent tracking.
-- Records per-subject consent grants and revocations.

CREATE TABLE IF NOT EXISTS consent_records (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subject_id   TEXT NOT NULL,
    tenant_id    TEXT NOT NULL,
    consent_type TEXT NOT NULL,
    granted_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at   TIMESTAMPTZ,
    UNIQUE (tenant_id, subject_id, consent_type)
);

CREATE INDEX idx_consent_records_subject
    ON consent_records (tenant_id, subject_id);

CREATE INDEX idx_consent_records_type
    ON consent_records (tenant_id, consent_type);

-- RLS: tenant isolation.
ALTER TABLE consent_records ENABLE ROW LEVEL SECURITY;

CREATE POLICY consent_records_tenant_isolation
    ON consent_records
    FOR ALL
    USING (tenant_id = current_setting('app.tenant_id', true));

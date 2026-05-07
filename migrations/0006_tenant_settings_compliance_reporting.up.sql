CREATE TABLE tenant_settings (
    tenant_id TEXT PRIMARY KEY,
    branding JSONB NOT NULL DEFAULT '{}'::jsonb,
    woocommerce JSONB NOT NULL DEFAULT '{}'::jsonb,
    ai_preferences JSONB NOT NULL DEFAULT '{}'::jsonb,
    compliance_overrides JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE compliance_custom_rules (
    tenant_id TEXT NOT NULL,
    rule_id TEXT NOT NULL,
    version INTEGER NOT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    severity TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    definition JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, rule_id)
);

CREATE TABLE compliance_audit_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    product_id UUID NOT NULL,
    checked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    pass BOOLEAN NOT NULL,
    score INTEGER NOT NULL,
    severity TEXT NOT NULL,
    failed_rule_ids TEXT[] NOT NULL DEFAULT '{}',
    result JSONB NOT NULL
);

CREATE INDEX idx_compliance_custom_rules_tenant_id ON compliance_custom_rules(tenant_id);
CREATE INDEX idx_compliance_audit_entries_tenant_checked_at ON compliance_audit_entries(tenant_id, checked_at);
CREATE INDEX idx_compliance_audit_entries_tenant_product ON compliance_audit_entries(tenant_id, product_id);

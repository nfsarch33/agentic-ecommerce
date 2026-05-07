DROP INDEX IF EXISTS idx_compliance_audit_entries_tenant_product;
DROP INDEX IF EXISTS idx_compliance_audit_entries_tenant_checked_at;
DROP INDEX IF EXISTS idx_compliance_custom_rules_tenant_id;

DROP TABLE IF EXISTS compliance_audit_entries;
DROP TABLE IF EXISTS compliance_custom_rules;
DROP TABLE IF EXISTS tenant_settings;

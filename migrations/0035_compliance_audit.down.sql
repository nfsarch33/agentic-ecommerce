-- v4.9.0 Story 2: rollback compliance audit log.

DROP POLICY IF EXISTS compliance_audit_log_tenant_isolation ON compliance_audit_log;
DROP TABLE IF EXISTS compliance_audit_log;

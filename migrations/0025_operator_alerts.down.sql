-- v3.9.1 EC-9-5 -- operator alerts rollback.
DROP INDEX IF EXISTS idx_operator_alerts_expires;
DROP INDEX IF EXISTS idx_operator_alerts_tenant_created;
DROP INDEX IF EXISTS idx_operator_alerts_tenant_status;
DROP TABLE IF EXISTS operator_alerts;

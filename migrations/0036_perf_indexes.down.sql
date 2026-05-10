-- Rollback v5.5.0 performance indexes.
DROP INDEX IF EXISTS idx_payments_tenant_created_status;
DROP INDEX IF EXISTS idx_payments_tenant_status_created;
DROP INDEX IF EXISTS idx_payment_refunds_tenant_created;

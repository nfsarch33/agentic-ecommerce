-- v4.2.0 rollback: drop payments tables and enums.
DROP POLICY IF EXISTS payment_refunds_tenant_isolation ON payment_refunds;
DROP TABLE IF EXISTS payment_refunds;
DROP POLICY IF EXISTS payments_tenant_isolation ON payments;
DROP TABLE IF EXISTS payments;
DROP TYPE IF EXISTS payment_status;
DROP TYPE IF EXISTS payment_provider;

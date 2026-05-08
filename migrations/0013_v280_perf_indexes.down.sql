-- v2.8.0 — Rollback for 0013 perf indexes. Forward-only migrations are
-- preferred but the down file exists for migrate-down compatibility.
DROP INDEX IF EXISTS idx_marketplace_installations_tenant_installed;
DROP INDEX IF EXISTS idx_tenant_registrations_status_expires;
DROP INDEX IF EXISTS idx_billing_subscriptions_tenant_state_created;
DROP INDEX IF EXISTS idx_billing_invoices_tenant_created;
DROP INDEX IF EXISTS idx_digital_download_tokens_tenant_expires;
DROP INDEX IF EXISTS idx_subscriptions_tenant_period_end;
DROP INDEX IF EXISTS idx_marketplace_submissions_state_submitted;

-- v2.5.0 — Tenant self-service registration + billing bounded context (down).

DROP INDEX IF EXISTS idx_tenant_registrations_email;
DROP INDEX IF EXISTS uniq_tenant_registrations_active_email;
DROP TABLE IF EXISTS tenant_registrations;

DROP TABLE IF EXISTS stripe_webhook_events;

DROP INDEX IF EXISTS idx_usage_records_tenant_metric;
DROP TABLE IF EXISTS usage_records;

DROP INDEX IF EXISTS idx_billing_invoices_tenant_subscription;
DROP TABLE IF EXISTS billing_invoices;

DROP INDEX IF EXISTS idx_billing_subscriptions_stripe_id;
DROP INDEX IF EXISTS idx_billing_subscriptions_tenant_state;
DROP TABLE IF EXISTS billing_subscriptions;

DROP TABLE IF EXISTS billing_plans;

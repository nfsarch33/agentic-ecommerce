-- v2.8.0 — Performance indexes for the comprehensive QA + security audit.
-- Forward-only migration. All indexes use CREATE INDEX IF NOT EXISTS so the
-- migration is idempotent and safe to re-run. Indexes are added based on
-- the v2.2-v2.7 query review documented in
-- reports/research/v280-explain-analyze-2026-05-09.md (global-kb).
--
-- Each index targets a real query pattern in the v2.2-v2.7 repos and
-- avoids the seq-scan path identified during the post-v2.7.0 EXPLAIN
-- ANALYZE pass. None of these indexes are tenant-isolation-critical
-- (RLS already enforces tenant_id boundaries via 0011_rls.up.sql);
-- these are pure latency wins.

-- v2.7.0 marketplace submissions: list pending submissions ordered by
-- submission time. The existing idx_marketplace_submissions_state +
-- idx_marketplace_submissions_tenant cover the simple cases but a
-- composite (state, submitted_at DESC) eliminates the sort step in the
-- super-admin queue view (`/admin/marketplace/submissions?state=pending_review`).
CREATE INDEX IF NOT EXISTS idx_marketplace_submissions_state_submitted
    ON marketplace_submissions (state, submitted_at DESC);

-- v2.2.0 membership: list subscriptions about to expire by tenant.
-- Existing idx_subscriptions_tenant_member + idx_subscriptions_tenant_state
-- do not cover (tenant_id, current_period_end) which the renewal workflow
-- queries every minute.
CREATE INDEX IF NOT EXISTS idx_subscriptions_tenant_period_end
    ON subscriptions (tenant_id, current_period_end)
    WHERE state IN ('trial', 'active');

-- v2.3.0 digital: cleanup expired download tokens by tenant + expiry.
-- Existing idx_digital_download_tokens_tenant_license is the hot-path
-- read but the cleanup sweep ("delete tokens with expires_at < now()")
-- has no covering index today.
CREATE INDEX IF NOT EXISTS idx_digital_download_tokens_tenant_expires
    ON digital_download_tokens (tenant_id, expires_at);

-- v2.5.0 billing: paginate invoices by recency. The admin invoice list
-- view orders by created_at DESC and paginates with LIMIT/OFFSET.
-- Today the (tenant_id, subscription_id) index does not help the
-- list-all-by-tenant endpoint.
CREATE INDEX IF NOT EXISTS idx_billing_invoices_tenant_created
    ON billing_invoices (tenant_id, created_at DESC);

-- v2.5.0 billing: subscription state filter ordered by created_at DESC.
-- The /admin/billing/subscriptions endpoint filters by state and
-- paginates by recency. The (tenant_id, state) index covers the
-- filter; this composite eliminates the trailing sort.
CREATE INDEX IF NOT EXISTS idx_billing_subscriptions_tenant_state_created
    ON billing_subscriptions (tenant_id, state, created_at DESC);

-- v2.5.0 registration: cleanup expired registration tokens. The
-- existing idx_tenant_registrations_email is fine for the verify
-- endpoint but the periodic sweep needs (status, expires_at).
CREATE INDEX IF NOT EXISTS idx_tenant_registrations_status_expires
    ON tenant_registrations (status, expires_at)
    WHERE status IN ('pending_email_verification', 'email_verified');

-- v2.4.0 marketplace: list installed plugins for a tenant ordered by
-- recency for the per-tenant dashboard ("recently installed").
CREATE INDEX IF NOT EXISTS idx_marketplace_installations_tenant_installed
    ON marketplace_installations (tenant_id, installed_at DESC)
    WHERE state IN ('installed', 'active');

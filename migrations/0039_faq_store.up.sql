-- v6.2.0 CF-13 -- FAQ store production readiness.
--
-- The faq_entries table itself was created in 0015 (v3.6.0 EC-8-2).
-- This migration adds the supporting indexes the v6.2.0 Postgres
-- adapter relies on: a covering index for the (tenant, language,
-- intent) lookup path that the responder hits on every classified
-- enquiry, plus an updated_at index so the v7.x freshness sweep can
-- evict stale entries without a sequential scan.
--
-- Idempotent: every CREATE INDEX uses IF NOT EXISTS so re-running
-- the migration on an already-migrated cluster is a no-op.

CREATE INDEX IF NOT EXISTS idx_faq_entries_tenant_lang_intent_covering
    ON faq_entries (tenant_id, language, intent_category)
    INCLUDE (entry_id, question, answer);

CREATE INDEX IF NOT EXISTS idx_faq_entries_updated_at
    ON faq_entries (updated_at);

COMMENT ON TABLE faq_entries IS
    'v6.2.0 CF-13 production-backed FAQ store. v3.6.0 schema kept; '
    || 'see internal/adapter/postgres/faq_store.go for the adapter.';

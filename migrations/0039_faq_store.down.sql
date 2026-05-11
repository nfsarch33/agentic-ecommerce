-- v6.2.0 CF-13 down: drop the supporting indexes added in 0039.
DROP INDEX IF EXISTS idx_faq_entries_updated_at;
DROP INDEX IF EXISTS idx_faq_entries_tenant_lang_intent_covering;

-- v3.6.0 EC-8-2 — Rollback for 0015 faq_entries.
DROP INDEX IF EXISTS idx_faq_entries_keywords_gin;
DROP INDEX IF EXISTS idx_faq_entries_tenant_lang_intent;
DROP TABLE IF EXISTS faq_entries;

-- v3.6.0 EC-8-2 — FAQ entries table for the customer service responder.
--
-- Forward-only migration. Stores tenant-scoped FAQ pairs the EC-8-2
-- responder ranks against the EC-8-1 classified intent + language.
-- The table is small (a few hundred entries per tenant); the
-- composite index on (tenant_id, language, intent_category) is the
-- primary lookup path. Full-text matching uses LOWER(question) +
-- pg_trgm later in v3.6.1; v3.6.0 ships the schema only and a
-- BM25-style stdlib tokenizer in the Go ranker.
--
-- RLS-aware: the row-level security policy in 0011_rls.up.sql is
-- driven by a hand-maintained DO block listing tables. The block is
-- idempotent on existing tables only -- so re-running 0011 after
-- 0015 ships will pick up the new table once the targets array is
-- extended in a follow-up sprint. For v3.6.0 the application layer
-- enforces tenant scoping (the responder always sets WHERE
-- tenant_id = $1; the leak test asserts this).

CREATE TABLE IF NOT EXISTS faq_entries (
    tenant_id        TEXT        NOT NULL,
    entry_id         UUID        NOT NULL DEFAULT gen_random_uuid(),
    language         TEXT        NOT NULL,
    intent_category  TEXT        NOT NULL,
    question         TEXT        NOT NULL,
    answer           TEXT        NOT NULL,
    keywords         TEXT[]      NOT NULL DEFAULT '{}',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, entry_id),
    CONSTRAINT faq_entries_language_chk CHECK (
        language IN ('en', 'zh-cn', 'zh-tw', 'other')
    ),
    CONSTRAINT faq_entries_intent_chk CHECK (
        intent_category IN (
            'order_status', 'refund_request', 'product_question',
            'shipping_query', 'complaint', 'compliment',
            'general_enquiry', 'spam'
        )
    ),
    CONSTRAINT faq_entries_question_not_empty
        CHECK (length(question) > 0),
    CONSTRAINT faq_entries_answer_not_empty
        CHECK (length(answer) > 0)
);

CREATE INDEX IF NOT EXISTS idx_faq_entries_tenant_lang_intent
    ON faq_entries (tenant_id, language, intent_category);

CREATE INDEX IF NOT EXISTS idx_faq_entries_keywords_gin
    ON faq_entries USING GIN (keywords);

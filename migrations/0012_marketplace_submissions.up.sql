-- v2.7.0 — Marketplace plugin submission queue.
-- Forward-only migration. Vendors POST a manifest to
-- /api/v1/marketplace/plugins/submit which lands in this table in
-- the `pending_review` state. Super-admins approve or reject,
-- transitioning the row to `approved` (publishes to
-- marketplace_plugins) or `rejected` (terminal).
--
-- The state column mirrors the explicit transition table in
-- internal/marketplace/submission_state.go for defence in depth.

CREATE TABLE marketplace_submissions (
    id              TEXT        NOT NULL,
    tenant_id       TEXT        NOT NULL,
    submitter_email TEXT        NOT NULL,
    slug            TEXT        NOT NULL,
    name            TEXT        NOT NULL,
    version         TEXT        NOT NULL,
    vendor          TEXT        NOT NULL,
    description     TEXT        NOT NULL DEFAULT '',
    category        TEXT        NOT NULL DEFAULT '',
    homepage_url    TEXT        NOT NULL DEFAULT '',
    manifest        JSONB       NOT NULL,
    state           TEXT        NOT NULL,
    review_notes    TEXT        NOT NULL DEFAULT '',
    submitted_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    reviewed_at     TIMESTAMPTZ NULL,
    reviewer        TEXT        NOT NULL DEFAULT '',
    PRIMARY KEY (id),
    CONSTRAINT marketplace_submissions_state_chk CHECK (
        state IN ('pending_review', 'approved', 'rejected')
    ),
    CONSTRAINT marketplace_submissions_slug_pattern_chk CHECK (
        slug ~ '^[a-z][a-z0-9-]*[a-z0-9]$'
    ),
    CONSTRAINT marketplace_submissions_version_pattern_chk CHECK (
        version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'
    )
);
CREATE INDEX IF NOT EXISTS idx_marketplace_submissions_state
    ON marketplace_submissions (state);
CREATE INDEX IF NOT EXISTS idx_marketplace_submissions_tenant
    ON marketplace_submissions (tenant_id, submitted_at DESC);
CREATE INDEX IF NOT EXISTS idx_marketplace_submissions_slug
    ON marketplace_submissions (slug);

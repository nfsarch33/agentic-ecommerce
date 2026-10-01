-- v18870-2: the cost ledger. One row per model call (and per recorded
-- action), job- and tenant-attributed, AUD cents via internal/costcalc.
CREATE TABLE IF NOT EXISTS cost_ledger (
    id           BIGSERIAL PRIMARY KEY,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    job_id       TEXT        NOT NULL DEFAULT '',
    tenant_id    TEXT        NOT NULL DEFAULT '',
    action       TEXT        NOT NULL DEFAULT '',
    model        TEXT        NOT NULL DEFAULT '',
    tokens_in    BIGINT      NOT NULL DEFAULT 0,
    tokens_out   BIGINT      NOT NULL DEFAULT 0,
    cost_cents   BIGINT      NOT NULL DEFAULT 0,
    status       TEXT        NOT NULL DEFAULT 'ok',   -- ok | error
    error_text   TEXT        NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS cost_ledger_created_at_idx ON cost_ledger (created_at);
CREATE INDEX IF NOT EXISTS cost_ledger_job_id_idx    ON cost_ledger (job_id);

-- v6.2.0 ADR-032 CF #6 -- JWT secret key rotation catalogue.
--
-- Each row is one signing-key version known to the rotator. The
-- secret bytes themselves are NEVER persisted in this table; only
-- the version metadata + the grace deadline lives here. The actual
-- HMAC bytes resolve at boot time from the secrets manager (1Password
-- operator vault / AWS Secrets Manager) so a Postgres dump cannot
-- leak signing material.
--
-- Lifecycle:
--   pending -> active -> retiring -> retired
--
-- The rotator reconciles its in-memory keyRing against this table
-- on the same cadence as the existing config refresher (60s).
-- `is_active` is unique-true so multiple replicas converge on a
-- single minting key.
CREATE TABLE IF NOT EXISTS jwt_key_versions (
    version           TEXT        PRIMARY KEY,
    state             TEXT        NOT NULL CHECK (state IN ('pending', 'active', 'retiring', 'retired')),
    secret_ref        TEXT        NOT NULL,
    not_after         TIMESTAMPTZ NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    activated_at      TIMESTAMPTZ NULL,
    retired_at        TIMESTAMPTZ NULL
);

-- Only one active key at a time; partial unique index lets the
-- pending/retiring/retired rows coexist with the single active row.
CREATE UNIQUE INDEX IF NOT EXISTS jwt_key_versions_one_active
    ON jwt_key_versions (state)
    WHERE state = 'active';

CREATE INDEX IF NOT EXISTS jwt_key_versions_state_not_after_idx
    ON jwt_key_versions (state, not_after);

-- v6.1.0 CF-15: Postgres-backed webhook IdempotencyStore.
--
-- Replaces the in-memory MemoryIdempotencyStore for production callers.
-- The in-memory implementation remains as a test double.
--
-- Schema design:
--   - PRIMARY KEY (tenant_id, idempotency_key) enforces per-tenant uniqueness
--     atomically; the adapter uses INSERT ... ON CONFLICT DO NOTHING to
--     race-safely claim a key.
--   - reserved_at TIMESTAMPTZ captures the reservation time so operators
--     can purge stale rows via a future TTL job (deferred; tracked in
--     CF-15 follow-up notes for v6.2.x retention sweep).
--   - tenant_id TEXT matches the surrounding webhook code conventions
--     (no separate tenants table FK because webhook ingress predates
--     full tenant onboarding).
-- RLS is intentionally OFF: this table is internal-only book-keeping,
-- read/written by webhook ingress handlers running with the same Postgres
-- role across tenants. Tenant isolation is enforced in application code via
-- the composite key; adding RLS here would add per-row policy cost on
-- every webhook delivery without changing the security boundary.

CREATE TABLE IF NOT EXISTS webhook_idempotency (
    tenant_id        TEXT        NOT NULL,
    idempotency_key  TEXT        NOT NULL,
    reserved_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_webhook_idempotency_reserved_at
    ON webhook_idempotency (reserved_at);

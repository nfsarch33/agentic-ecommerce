-- v3.8.0 EC-7-5 — returns table.
--
-- Forward-only migration. Persists customer-initiated returns
-- (Return Merchandise Authorization / RMA) so the EC-7-5 returns
-- saga workflow can run end-to-end across the auto-approve gate,
-- label generation, refund processing, inventory adjustment, and
-- channel status propagation.
--
-- Idempotency key: (tenant_id, rma_id). The workflow is the source
-- of truth for state transitions; we mirror the canonical state
-- enum here so the dashboard can pivot on a single column.
--
-- Tenant-aware: every RMA carries tenant_id; downstream RLS
-- (migration 0011) enforces the per-tenant scoping at the row level.

CREATE TABLE IF NOT EXISTS returns (
    tenant_id              TEXT        NOT NULL,
    rma_id                 TEXT        NOT NULL,
    order_id               TEXT        NOT NULL,
    reason                 TEXT        NOT NULL,
    refund_amount_aud_cents BIGINT     NOT NULL,
    auto_approved          BOOLEAN     NOT NULL DEFAULT FALSE,
    status                 TEXT        NOT NULL DEFAULT 'requested',
    rolled_back_reason     TEXT        NULL,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, rma_id),
    CONSTRAINT returns_status_chk
        CHECK (status IN ('requested', 'pending_approval', 'approved', 'labelled', 'refunded', 'completed', 'rolled_back'))
);

CREATE INDEX IF NOT EXISTS idx_returns_tenant_order
    ON returns (tenant_id, order_id);

CREATE INDEX IF NOT EXISTS idx_returns_tenant_status
    ON returns (tenant_id, status);

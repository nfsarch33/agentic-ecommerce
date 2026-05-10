-- v3.8.0 EC-7-3 — shipping labels table.
--
-- Forward-only migration. Persists carrier-issued shipping labels so
-- the EC-7-3 generator can return cached labels on subsequent calls
-- with the same tracking_number, and so the EC-7-4 status propagator
-- can correlate carrier webhook events to internal orders.
--
-- Idempotency key: (tenant_id, tracking_number). The carrier API is
-- the source of truth for tracking_number uniqueness; we mirror that
-- here so duplicate Generate calls observe a stable cached label.
--
-- Tenant-aware: every label carries tenant_id; downstream RLS
-- (migration 0011) enforces the per-tenant scoping at the row level.

CREATE TABLE IF NOT EXISTS shipping_labels (
    tenant_id            TEXT        NOT NULL,
    tracking_number      TEXT        NOT NULL,
    order_id             TEXT        NOT NULL,
    carrier              TEXT        NOT NULL,
    label_pdf_path       TEXT        NOT NULL,
    cost_aud_cents       BIGINT      NOT NULL,
    eta_days             INTEGER     NOT NULL,
    sla_days             INTEGER     NOT NULL,
    status               TEXT        NOT NULL DEFAULT 'created',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, tracking_number),
    CONSTRAINT shipping_labels_status_chk
        CHECK (status IN ('created', 'in_transit', 'delivered', 'cancelled', 'exception'))
);

CREATE INDEX IF NOT EXISTS idx_shipping_labels_tenant_order
    ON shipping_labels (tenant_id, order_id);

CREATE INDEX IF NOT EXISTS idx_shipping_labels_tenant_status
    ON shipping_labels (tenant_id, status);

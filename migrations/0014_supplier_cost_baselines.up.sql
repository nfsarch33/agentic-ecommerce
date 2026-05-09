-- v3.5.0 EC-6-1 — Supplier cost monitor baseline table.
--
-- Forward-only migration. Stores the last-observed unit cost per
-- supplier+SKU so the EC-6-1 scheduled monitor can detect deltas
-- > 5% and emit SupplierCostChangedEvent. Tenant-id keyed so RLS
-- (migrations/0011_rls.up.sql) enforces cross-tenant isolation
-- consistent with v3.1.0 sourcing fixtures.
--
-- The (tenant_id, source, supplier_sku) PK keeps the upsert path
-- simple in v3.5.0; the (tenant_id, observed_at) covering index
-- supports the daily monitor sweep ("re-scrape suppliers older
-- than 23h").

CREATE TABLE IF NOT EXISTS supplier_cost_baselines (
    tenant_id            TEXT        NOT NULL,
    source               TEXT        NOT NULL,
    supplier_sku         TEXT        NOT NULL,
    supplier_id          TEXT        NOT NULL DEFAULT '',
    baseline_cny_cents   INTEGER     NOT NULL,
    last_observed_cny    INTEGER     NOT NULL,
    last_delta_pct       DOUBLE PRECISION NOT NULL DEFAULT 0,
    observed_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, source, supplier_sku),
    CONSTRAINT supplier_cost_baselines_source_chk CHECK (
        source IN ('1688', 'taobao', 'aliexpress', 'pinduoduo', 'dhgate')
    ),
    CONSTRAINT supplier_cost_baselines_baseline_positive
        CHECK (baseline_cny_cents >= 0),
    CONSTRAINT supplier_cost_baselines_last_observed_positive
        CHECK (last_observed_cny >= 0)
);

CREATE INDEX IF NOT EXISTS idx_supplier_cost_baselines_tenant_observed
    ON supplier_cost_baselines (tenant_id, observed_at DESC);

CREATE INDEX IF NOT EXISTS idx_supplier_cost_baselines_tenant_source
    ON supplier_cost_baselines (tenant_id, source);

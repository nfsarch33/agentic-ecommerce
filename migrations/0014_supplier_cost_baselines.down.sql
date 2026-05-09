-- v3.5.0 EC-6-1 — Rollback for 0014 supplier_cost_baselines.
-- Forward-only migrations are preferred but the down file exists
-- for migrate-down compatibility (matches the 0001-0013 pattern).
DROP INDEX IF EXISTS idx_supplier_cost_baselines_tenant_source;
DROP INDEX IF EXISTS idx_supplier_cost_baselines_tenant_observed;
DROP TABLE IF EXISTS supplier_cost_baselines;

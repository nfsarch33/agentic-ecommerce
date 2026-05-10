-- v4.9.0 Story 1: rollback per-tenant data residency.

DROP INDEX IF EXISTS idx_tenants_data_region;
ALTER TABLE tenants DROP COLUMN IF EXISTS data_region;

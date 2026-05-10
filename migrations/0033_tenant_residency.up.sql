-- v4.9.0 Story 1: Per-tenant data residency.
-- Adds data_region column to tenants table with AU default.

ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS data_region TEXT NOT NULL DEFAULT 'AU'
        CHECK (data_region IN ('AU', 'CN', 'EU', 'US'));

CREATE INDEX IF NOT EXISTS idx_tenants_data_region
    ON tenants (data_region);

COMMENT ON COLUMN tenants.data_region IS
    'Data residency region code: AU=australia-southeast1, CN=asia-east2, EU=europe-west1, US=us-central1';

-- v3.9.0 EC-6-4 -- competitor_prices table rollback.

DROP INDEX IF EXISTS idx_competitor_prices_tenant_channel;
DROP INDEX IF EXISTS idx_competitor_prices_tenant_sku;
DROP INDEX IF EXISTS idx_competitor_prices_tenant_observed;
DROP TABLE IF EXISTS competitor_prices;

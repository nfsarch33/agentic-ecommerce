-- v3.9.0 EC-5-5 -- content_performance_history table rollback.

DROP INDEX IF EXISTS idx_content_performance_tenant_observed;
DROP INDEX IF EXISTS idx_content_performance_tenant_channel;
DROP TABLE IF EXISTS content_performance_history;

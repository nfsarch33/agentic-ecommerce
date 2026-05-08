-- v2.7.0 down migration for marketplace submissions.
DROP INDEX IF EXISTS idx_marketplace_submissions_slug;
DROP INDEX IF EXISTS idx_marketplace_submissions_tenant;
DROP INDEX IF EXISTS idx_marketplace_submissions_state;
DROP TABLE IF EXISTS marketplace_submissions;

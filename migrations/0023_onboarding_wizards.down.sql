-- v3.9.1 Existing #10 -- onboarding wizard rollback.
DROP INDEX IF EXISTS idx_onboarding_wizards_started_at;
DROP INDEX IF EXISTS idx_onboarding_wizards_tenant_completed;
DROP TABLE IF EXISTS onboarding_wizards;

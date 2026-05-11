-- v6.2.0 ADR-032 CF #6 down: drop the JWT key catalogue.
DROP INDEX IF EXISTS jwt_key_versions_state_not_after_idx;
DROP INDEX IF EXISTS jwt_key_versions_one_active;
DROP TABLE IF EXISTS jwt_key_versions;

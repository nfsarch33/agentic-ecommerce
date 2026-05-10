-- v4.9.0 Story 2: rollback consent records.

DROP POLICY IF EXISTS consent_records_tenant_isolation ON consent_records;
DROP TABLE IF EXISTS consent_records;

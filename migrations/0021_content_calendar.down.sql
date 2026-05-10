-- v3.9.0 EC-5-2 -- content_calendar_entries table rollback.

DROP INDEX IF EXISTS idx_content_calendar_tenant_channel_status;
DROP INDEX IF EXISTS idx_content_calendar_tenant_scheduled;
DROP TABLE IF EXISTS content_calendar_entries;

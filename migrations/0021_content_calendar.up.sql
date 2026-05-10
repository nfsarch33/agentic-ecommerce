-- v3.9.0 EC-5-2 -- Content calendar persistence.
--
-- Forward-only migration. Stores per-(tenant, scheduled_at, channel)
-- entries the EC-5-2 calendar agent + n8n scheduler dispatch.
-- payload_ref is an opaque foreign key resolved by the worker that
-- actually publishes the content (e.g. video script id, image set id,
-- caption draft id) so the calendar table stays small + fast.
--
-- RLS-aware: the existing migrations/0011_rls.up.sql DO block applies
-- the uniform tenant_id=current_tenant_setting() policy when this
-- table is registered there.

CREATE TABLE IF NOT EXISTS content_calendar_entries (
    id              TEXT        NOT NULL,
    tenant_id       TEXT        NOT NULL,
    scheduled_at    TIMESTAMPTZ NOT NULL,
    channel         TEXT        NOT NULL,
    content_type    TEXT        NOT NULL,
    payload_ref     TEXT        NOT NULL,
    status          TEXT        NOT NULL DEFAULT 'scheduled',
    attempt_count   INTEGER     NOT NULL DEFAULT 0,
    last_error      TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    CONSTRAINT content_calendar_status_chk CHECK (
        status IN ('scheduled', 'publishing', 'published', 'failed', 'cancelled')
    ),
    CONSTRAINT content_calendar_channel_chk CHECK (
        channel IN ('tiktok', 'rednote', 'facebook', 'instagram')
    ),
    CONSTRAINT content_calendar_content_type_chk CHECK (
        content_type IN ('video', 'post', 'reel', 'story', 'live')
    )
);

CREATE INDEX IF NOT EXISTS idx_content_calendar_tenant_scheduled
    ON content_calendar_entries (tenant_id, scheduled_at);

CREATE INDEX IF NOT EXISTS idx_content_calendar_tenant_channel_status
    ON content_calendar_entries (tenant_id, channel, status);

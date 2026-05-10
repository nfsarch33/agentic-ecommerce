-- v3.9.1 EC-9-5 -- operator alert centre.
--
-- Forward-only migration. Centralised alert ledger consumed by the
-- operator alert handler. Every operator-actionable event the
-- platform emits (large refund pending, large dropship pending,
-- price-change pending, CAPTCHA detected, OmniParser unavailable,
-- rate-limit drain, channel-status update failure, large margin
-- alert) lands here for triage.
--
-- Lifecycle: pending -> acknowledged -> resolved (or expired after
-- 24h via a sweeper job).
--
-- RLS-aware: the existing migrations/0011_rls.up.sql DO block applies
-- the uniform tenant_id=current_tenant_setting() policy when this
-- table is registered there.

CREATE TABLE IF NOT EXISTS operator_alerts (
    tenant_id     TEXT        NOT NULL,
    alert_id      TEXT        NOT NULL,
    alert_type    TEXT        NOT NULL,
    severity      TEXT        NOT NULL DEFAULT 'warning',
    status        TEXT        NOT NULL DEFAULT 'pending',
    payload_json  JSONB       NOT NULL DEFAULT '{}'::JSONB,
    action_taken  TEXT,
    operator_id   TEXT,
    note          TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    acknowledged_at TIMESTAMPTZ,
    resolved_at   TIMESTAMPTZ,
    expires_at    TIMESTAMPTZ NOT NULL DEFAULT now() + INTERVAL '24 hours',
    PRIMARY KEY (tenant_id, alert_id),
    CONSTRAINT operator_alerts_status_chk CHECK (
        status IN ('pending', 'acknowledged', 'resolved', 'expired')
    ),
    CONSTRAINT operator_alerts_severity_chk CHECK (
        severity IN ('info', 'warning', 'critical')
    ),
    CONSTRAINT operator_alerts_action_chk CHECK (
        action_taken IS NULL OR action_taken IN ('approve', 'deny')
    ),
    CONSTRAINT operator_alerts_type_chk CHECK (
        alert_type IN (
            'large_refund_pending_approval',
            'large_dropship_pending_approval',
            'price_change_pending_approval',
            'captcha_detected',
            'omniparser_unavailable',
            'rate_limit_drain',
            'channel_status_update_failed',
            'large_margin_alert'
        )
    )
);

CREATE INDEX IF NOT EXISTS idx_operator_alerts_tenant_status
    ON operator_alerts (tenant_id, status);

CREATE INDEX IF NOT EXISTS idx_operator_alerts_tenant_created
    ON operator_alerts (tenant_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_operator_alerts_expires
    ON operator_alerts (expires_at)
    WHERE status IN ('pending', 'acknowledged');

-- v3.9.1 Existing #10 -- AI onboarding wizard state.
--
-- Forward-only migration. Stores per-(tenant_id, wizard_id) state
-- for the 4-step onboarding wizard handler. The wizard drives:
--
--   step 1: tenant identity   -> tenant identity captured
--   step 2: channel selection -> channels chosen
--   step 3: compliance gate   -> AU/CN compliance flags applied
--   step 4: initial seeding   -> 1688/Taobao or WC import target
--
-- On step 4 completion the handler launches the existing
-- internal/workflow/tenant_onboarding.go workflow (v3.0.0) so the
-- canonical tenant aggregate provisioning path is reused; the
-- wizard table only holds the wizard-specific projection.
--
-- RLS-aware: the existing migrations/0011_rls.up.sql DO block applies
-- the uniform tenant_id=current_tenant_setting() policy when this
-- table is registered there.

CREATE TABLE IF NOT EXISTS onboarding_wizards (
    tenant_id        TEXT        NOT NULL,
    wizard_id        TEXT        NOT NULL,
    current_step     INTEGER     NOT NULL DEFAULT 1,
    completed_steps  INTEGER[]   NOT NULL DEFAULT ARRAY[]::INTEGER[],
    state_json       JSONB       NOT NULL DEFAULT '{}'::JSONB,
    completed        BOOLEAN     NOT NULL DEFAULT FALSE,
    started_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at     TIMESTAMPTZ,
    last_advanced_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, wizard_id),
    CONSTRAINT onboarding_wizard_step_bounds
        CHECK (current_step >= 1 AND current_step <= 5)
);

CREATE INDEX IF NOT EXISTS idx_onboarding_wizards_tenant_completed
    ON onboarding_wizards (tenant_id, completed);

CREATE INDEX IF NOT EXISTS idx_onboarding_wizards_started_at
    ON onboarding_wizards (started_at DESC);

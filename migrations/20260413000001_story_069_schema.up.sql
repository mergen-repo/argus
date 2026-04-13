-- STORY-069: Onboarding, Reporting & Notification Completeness
-- Creates: onboarding_sessions, scheduled_reports, webhook_configs, webhook_deliveries,
--          notification_preferences, notification_templates, sms_outbound
-- Adds:    users.locale, ip_addresses.grace_expires_at, ip_addresses.released_at
-- Optional: sims.owner_user_id (only if missing)
-- Migration pair: 20260413000001_story_069_schema

-- ============================================================
-- onboarding_sessions (AC-1)
-- ============================================================
CREATE TABLE IF NOT EXISTS onboarding_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    started_by UUID NOT NULL REFERENCES users(id),
    current_step INTEGER NOT NULL DEFAULT 1,
    step_1_data JSONB,
    step_2_data JSONB,
    step_3_data JSONB,
    step_4_data JSONB,
    step_5_data JSONB,
    state VARCHAR(20) NOT NULL DEFAULT 'in_progress',
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_onb_sessions_tenant_state ON onboarding_sessions (tenant_id, state);

-- ============================================================
-- scheduled_reports (AC-2)
-- ============================================================
CREATE TABLE IF NOT EXISTS scheduled_reports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    report_type VARCHAR(50) NOT NULL,
    schedule_cron VARCHAR(100) NOT NULL,
    format VARCHAR(10) NOT NULL CHECK (format IN ('pdf','csv','xlsx')),
    recipients TEXT[] NOT NULL DEFAULT '{}',
    filters JSONB NOT NULL DEFAULT '{}',
    last_run_at TIMESTAMPTZ,
    next_run_at TIMESTAMPTZ,
    last_job_id UUID,
    state VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by UUID REFERENCES users(id),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_sched_reports_tenant_state ON scheduled_reports (tenant_id, state);
CREATE INDEX idx_sched_reports_next_run ON scheduled_reports (next_run_at) WHERE state='active';

-- ============================================================
-- webhook_configs (AC-5)
-- ============================================================
CREATE TABLE IF NOT EXISTS webhook_configs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    url TEXT NOT NULL,
    secret_encrypted BYTEA NOT NULL,
    event_types VARCHAR[] NOT NULL DEFAULT '{}',
    enabled BOOLEAN NOT NULL DEFAULT true,
    last_success_at TIMESTAMPTZ,
    last_failure_at TIMESTAMPTZ,
    failure_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_webhook_configs_tenant ON webhook_configs (tenant_id) WHERE enabled=true;

-- ============================================================
-- webhook_deliveries (AC-5)
-- ============================================================
CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    config_id UUID NOT NULL REFERENCES webhook_configs(id) ON DELETE CASCADE,
    event_type VARCHAR(50) NOT NULL,
    payload_hash VARCHAR(64) NOT NULL,
    payload_preview TEXT NOT NULL,
    signature VARCHAR(128) NOT NULL,
    response_status INTEGER,
    response_body TEXT,
    attempt_count INTEGER NOT NULL DEFAULT 1,
    next_retry_at TIMESTAMPTZ,
    final_state VARCHAR(20) NOT NULL DEFAULT 'retrying',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_webhook_deliveries_config_time ON webhook_deliveries (config_id, created_at DESC);
CREATE INDEX idx_webhook_deliveries_retry ON webhook_deliveries (next_retry_at) WHERE final_state='retrying';
CREATE INDEX idx_webhook_deliveries_tenant_state ON webhook_deliveries (tenant_id, final_state);

-- ============================================================
-- notification_preferences (AC-7)
-- ============================================================
CREATE TABLE IF NOT EXISTS notification_preferences (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    event_type VARCHAR(50) NOT NULL,
    channels VARCHAR[] NOT NULL DEFAULT '{}',
    severity_threshold VARCHAR(10) NOT NULL DEFAULT 'info',
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, event_type)
);
CREATE INDEX idx_notif_prefs_tenant ON notification_preferences (tenant_id);

-- ============================================================
-- notification_templates (AC-8) — global, no tenant_id, no RLS
-- ============================================================
CREATE TABLE IF NOT EXISTS notification_templates (
    event_type VARCHAR(50) NOT NULL,
    locale VARCHAR(5) NOT NULL,
    subject VARCHAR(255) NOT NULL,
    body_text TEXT NOT NULL,
    body_html TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (event_type, locale)
);

-- ============================================================
-- Column additions: users, ip_addresses
-- ============================================================
ALTER TABLE users ADD COLUMN IF NOT EXISTS locale VARCHAR(5) NOT NULL DEFAULT 'en' CHECK (locale IN ('tr','en'));

ALTER TABLE ip_addresses ADD COLUMN IF NOT EXISTS grace_expires_at TIMESTAMPTZ;
ALTER TABLE ip_addresses ADD COLUMN IF NOT EXISTS released_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_ip_addresses_grace_release
    ON ip_addresses (grace_expires_at)
    WHERE released_at IS NULL AND grace_expires_at IS NOT NULL;

-- ============================================================
-- AC-9: sims.owner_user_id — not found in any prior migration; added here
-- ============================================================
ALTER TABLE sims ADD COLUMN IF NOT EXISTS owner_user_id UUID REFERENCES users(id);
CREATE INDEX IF NOT EXISTS idx_sims_owner_user ON sims (owner_user_id) WHERE owner_user_id IS NOT NULL;

-- ============================================================
-- sms_outbound (AC-12)
-- ============================================================
CREATE TABLE IF NOT EXISTS sms_outbound (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    sim_id UUID NOT NULL REFERENCES sims(id),
    msisdn VARCHAR(20) NOT NULL,
    text_hash VARCHAR(64) NOT NULL,
    text_preview VARCHAR(80),
    status VARCHAR(20) NOT NULL DEFAULT 'queued',
    provider_message_id VARCHAR(255),
    error_code VARCHAR(50),
    queued_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sent_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ
);
CREATE INDEX idx_sms_outbound_tenant_sim_time ON sms_outbound (tenant_id, sim_id, queued_at DESC);
CREATE INDEX idx_sms_outbound_provider_id ON sms_outbound (provider_message_id) WHERE provider_message_id IS NOT NULL;
CREATE INDEX idx_sms_outbound_status ON sms_outbound (status);

-- STORY-073 Task 1: Admin compliance tables
-- Creates kill_switches (global feature flags) and maintenance_windows (scheduled degradation periods).

CREATE TABLE IF NOT EXISTS kill_switches (
    key          VARCHAR(64) PRIMARY KEY,
    label        VARCHAR(128) NOT NULL,
    description  TEXT NOT NULL,
    enabled      BOOLEAN NOT NULL DEFAULT false,
    reason       TEXT,
    toggled_by   UUID REFERENCES users(id),
    toggled_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed the 5 canonical kill switches (idempotent)
INSERT INTO kill_switches (key, label, description) VALUES
  ('radius_auth',           'Disable RADIUS Auth',            'Reject all RADIUS Access-Request with Access-Reject'),
  ('session_create',        'Disable New Session Creation',   'Reject new session creation (AAA + portal)'),
  ('bulk_operations',       'Disable Bulk Operations',        'Reject bulk SIM state-change / policy-assign / eSIM-switch'),
  ('read_only_mode',        'Read-Only Mode',                 'Reject all non-GET mutations except auth/logout'),
  ('external_notifications','Disable External Notifications', 'Suppress email/SMS/webhook/telegram dispatch')
ON CONFLICT (key) DO NOTHING;

CREATE TABLE IF NOT EXISTS maintenance_windows (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          UUID REFERENCES tenants(id),
    title              VARCHAR(255) NOT NULL,
    description        TEXT NOT NULL,
    starts_at          TIMESTAMPTZ NOT NULL,
    ends_at            TIMESTAMPTZ NOT NULL,
    affected_services  VARCHAR[] NOT NULL DEFAULT '{}',
    cron_expression    VARCHAR(100),
    notify_plan        JSONB NOT NULL DEFAULT '{}',
    state              VARCHAR(20) NOT NULL DEFAULT 'scheduled' CHECK (state IN ('scheduled','active','completed','cancelled')),
    created_by         UUID REFERENCES users(id),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (ends_at > starts_at)
);

CREATE INDEX IF NOT EXISTS idx_maintenance_windows_active
    ON maintenance_windows (starts_at, ends_at)
    WHERE state IN ('scheduled','active');

CREATE INDEX IF NOT EXISTS idx_maintenance_windows_tenant
    ON maintenance_windows (tenant_id)
    WHERE tenant_id IS NOT NULL;

ALTER TABLE maintenance_windows ENABLE ROW LEVEL SECURITY;
ALTER TABLE maintenance_windows FORCE ROW LEVEL SECURITY;
CREATE POLICY maintenance_windows_tenant_isolation ON maintenance_windows
    USING (
        tenant_id IS NULL
        OR tenant_id = current_setting('app.current_tenant', true)::uuid
    );

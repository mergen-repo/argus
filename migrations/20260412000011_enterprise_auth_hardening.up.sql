-- STORY-068 Task 1: Enterprise auth schema hardening
-- Adds password change tracking, API key IP whitelisting, per-tenant API key cap,
-- password history enforcement table, and 2FA backup codes table with RLS.

-- ============================================================
-- users: force-change flag + change tracking
-- ============================================================
ALTER TABLE users ADD COLUMN IF NOT EXISTS password_change_required BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE users ADD COLUMN IF NOT EXISTS password_changed_at TIMESTAMPTZ;

-- ============================================================
-- api_keys: IP allowlist
-- ============================================================
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS allowed_ips TEXT[] NOT NULL DEFAULT '{}';

CREATE INDEX IF NOT EXISTS idx_api_keys_allowed_ips_gin ON api_keys USING GIN (allowed_ips);

-- ============================================================
-- tenants: max api keys cap
-- ============================================================
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS max_api_keys INTEGER NOT NULL DEFAULT 20;

-- ============================================================
-- password_history: prevents reuse of recent passwords
-- ============================================================
CREATE TABLE IF NOT EXISTS password_history (
    id          BIGSERIAL PRIMARY KEY,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    password_hash TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_password_history_user_time
    ON password_history (user_id, created_at DESC);

-- ============================================================
-- user_backup_codes: 2FA TOTP backup codes
-- ============================================================
CREATE TABLE IF NOT EXISTS user_backup_codes (
    id        BIGSERIAL PRIMARY KEY,
    user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL,
    used_at   TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_user_backup_codes_user_unused
    ON user_backup_codes (user_id) WHERE used_at IS NULL;

-- ============================================================
-- RLS: password_history — scoped via users.tenant_id
-- ============================================================
ALTER TABLE password_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE password_history FORCE ROW LEVEL SECURITY;
CREATE POLICY password_history_tenant_isolation ON password_history
    USING (user_id IN (
        SELECT id FROM users
        WHERE tenant_id = current_setting('app.current_tenant', true)::uuid
    ));

-- ============================================================
-- RLS: user_backup_codes — scoped via users.tenant_id
-- ============================================================
ALTER TABLE user_backup_codes ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_backup_codes FORCE ROW LEVEL SECURITY;
CREATE POLICY user_backup_codes_tenant_isolation ON user_backup_codes
    USING (user_id IN (
        SELECT id FROM users
        WHERE tenant_id = current_setting('app.current_tenant', true)::uuid
    ));

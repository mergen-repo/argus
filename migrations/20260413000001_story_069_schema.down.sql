-- STORY-069: Reverse Onboarding, Reporting & Notification Completeness
-- Drops tables and columns in reverse dependency order.

-- ============================================================
-- sms_outbound (AC-12)
-- ============================================================
DROP INDEX IF EXISTS idx_sms_outbound_status;
DROP INDEX IF EXISTS idx_sms_outbound_provider_id;
DROP INDEX IF EXISTS idx_sms_outbound_tenant_sim_time;
DROP TABLE IF EXISTS sms_outbound;

-- ============================================================
-- AC-9: sims.owner_user_id
-- ============================================================
DROP INDEX IF EXISTS idx_sims_owner_user;
ALTER TABLE sims DROP COLUMN IF EXISTS owner_user_id;

-- ============================================================
-- ip_addresses columns
-- ============================================================
DROP INDEX IF EXISTS idx_ip_addresses_grace_release;
ALTER TABLE ip_addresses DROP COLUMN IF EXISTS released_at;
ALTER TABLE ip_addresses DROP COLUMN IF EXISTS grace_expires_at;

-- ============================================================
-- users.locale
-- ============================================================
ALTER TABLE users DROP COLUMN IF EXISTS locale;

-- ============================================================
-- notification_templates (AC-8)
-- ============================================================
DROP TABLE IF EXISTS notification_templates;

-- ============================================================
-- notification_preferences (AC-7)
-- ============================================================
DROP INDEX IF EXISTS idx_notif_prefs_tenant;
DROP TABLE IF EXISTS notification_preferences;

-- ============================================================
-- webhook_deliveries before webhook_configs (AC-5)
-- ============================================================
DROP INDEX IF EXISTS idx_webhook_deliveries_tenant_state;
DROP INDEX IF EXISTS idx_webhook_deliveries_retry;
DROP INDEX IF EXISTS idx_webhook_deliveries_config_time;
DROP TABLE IF EXISTS webhook_deliveries;

DROP INDEX IF EXISTS idx_webhook_configs_tenant;
DROP TABLE IF EXISTS webhook_configs;

-- ============================================================
-- scheduled_reports (AC-2)
-- ============================================================
DROP INDEX IF EXISTS idx_sched_reports_next_run;
DROP INDEX IF EXISTS idx_sched_reports_tenant_state;
DROP TABLE IF EXISTS scheduled_reports;

-- ============================================================
-- onboarding_sessions (AC-1)
-- ============================================================
DROP INDEX IF EXISTS idx_onb_sessions_tenant_state;
DROP TABLE IF EXISTS onboarding_sessions;

-- STORY-069: Drop RLS policies for onboarding/scheduled_reports/webhooks/preferences/sms

-- sms_outbound
DROP POLICY IF EXISTS sms_outbound_tenant_isolation ON sms_outbound;
ALTER TABLE sms_outbound NO FORCE ROW LEVEL SECURITY;
ALTER TABLE sms_outbound DISABLE ROW LEVEL SECURITY;

-- notification_preferences
DROP POLICY IF EXISTS notification_preferences_tenant_isolation ON notification_preferences;
ALTER TABLE notification_preferences NO FORCE ROW LEVEL SECURITY;
ALTER TABLE notification_preferences DISABLE ROW LEVEL SECURITY;

-- webhook_deliveries
DROP POLICY IF EXISTS webhook_deliveries_tenant_isolation ON webhook_deliveries;
ALTER TABLE webhook_deliveries NO FORCE ROW LEVEL SECURITY;
ALTER TABLE webhook_deliveries DISABLE ROW LEVEL SECURITY;

-- webhook_configs
DROP POLICY IF EXISTS webhook_configs_tenant_isolation ON webhook_configs;
ALTER TABLE webhook_configs NO FORCE ROW LEVEL SECURITY;
ALTER TABLE webhook_configs DISABLE ROW LEVEL SECURITY;

-- scheduled_reports
DROP POLICY IF EXISTS scheduled_reports_tenant_isolation ON scheduled_reports;
ALTER TABLE scheduled_reports NO FORCE ROW LEVEL SECURITY;
ALTER TABLE scheduled_reports DISABLE ROW LEVEL SECURITY;

-- onboarding_sessions
DROP POLICY IF EXISTS onboarding_sessions_tenant_isolation ON onboarding_sessions;
ALTER TABLE onboarding_sessions NO FORCE ROW LEVEL SECURITY;
ALTER TABLE onboarding_sessions DISABLE ROW LEVEL SECURITY;

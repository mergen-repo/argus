-- STORY-069: RLS policies for onboarding/scheduled_reports/webhooks/preferences/sms
-- Note: notification_templates is GLOBAL, no RLS needed.

-- onboarding_sessions
ALTER TABLE onboarding_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE onboarding_sessions FORCE ROW LEVEL SECURITY;
CREATE POLICY onboarding_sessions_tenant_isolation ON onboarding_sessions
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- scheduled_reports
ALTER TABLE scheduled_reports ENABLE ROW LEVEL SECURITY;
ALTER TABLE scheduled_reports FORCE ROW LEVEL SECURITY;
CREATE POLICY scheduled_reports_tenant_isolation ON scheduled_reports
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- webhook_configs
ALTER TABLE webhook_configs ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_configs FORCE ROW LEVEL SECURITY;
CREATE POLICY webhook_configs_tenant_isolation ON webhook_configs
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- webhook_deliveries
ALTER TABLE webhook_deliveries ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_deliveries FORCE ROW LEVEL SECURITY;
CREATE POLICY webhook_deliveries_tenant_isolation ON webhook_deliveries
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- notification_preferences
ALTER TABLE notification_preferences ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_preferences FORCE ROW LEVEL SECURITY;
CREATE POLICY notification_preferences_tenant_isolation ON notification_preferences
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- sms_outbound
ALTER TABLE sms_outbound ENABLE ROW LEVEL SECURITY;
ALTER TABLE sms_outbound FORCE ROW LEVEL SECURITY;
CREATE POLICY sms_outbound_tenant_isolation ON sms_outbound
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

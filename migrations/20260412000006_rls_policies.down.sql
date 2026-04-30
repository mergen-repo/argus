-- STORY-064 Task 7 rollback: remove RLS policies and disable RLS.
-- Applied in reverse order of the up migration.

-- ============================================================
-- Indirect tables
-- ============================================================

DROP POLICY IF EXISTS sim_state_history_tenant_isolation ON sim_state_history;
ALTER TABLE sim_state_history NO FORCE ROW LEVEL SECURITY;
ALTER TABLE sim_state_history DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS user_sessions_tenant_isolation ON user_sessions;
ALTER TABLE user_sessions NO FORCE ROW LEVEL SECURITY;
ALTER TABLE user_sessions DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS esim_profiles_tenant_isolation ON esim_profiles;
ALTER TABLE esim_profiles NO FORCE ROW LEVEL SECURITY;
ALTER TABLE esim_profiles DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS ip_addresses_tenant_isolation ON ip_addresses;
ALTER TABLE ip_addresses NO FORCE ROW LEVEL SECURITY;
ALTER TABLE ip_addresses DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS policy_rollouts_tenant_isolation ON policy_rollouts;
ALTER TABLE policy_rollouts NO FORCE ROW LEVEL SECURITY;
ALTER TABLE policy_rollouts DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS policy_assignments_tenant_isolation ON policy_assignments;
ALTER TABLE policy_assignments NO FORCE ROW LEVEL SECURITY;
ALTER TABLE policy_assignments DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS policy_versions_tenant_isolation ON policy_versions;
ALTER TABLE policy_versions NO FORCE ROW LEVEL SECURITY;
ALTER TABLE policy_versions DISABLE ROW LEVEL SECURITY;

-- ============================================================
-- Direct tenant_id tables
-- ============================================================

DROP POLICY IF EXISTS sla_reports_tenant_isolation ON sla_reports;
ALTER TABLE sla_reports NO FORCE ROW LEVEL SECURITY;
ALTER TABLE sla_reports DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_retention_config_tenant_isolation ON tenant_retention_config;
ALTER TABLE tenant_retention_config NO FORCE ROW LEVEL SECURITY;
ALTER TABLE tenant_retention_config DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS s3_archival_log_tenant_isolation ON s3_archival_log;
ALTER TABLE s3_archival_log NO FORCE ROW LEVEL SECURITY;
ALTER TABLE s3_archival_log DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS ota_commands_tenant_isolation ON ota_commands;
ALTER TABLE ota_commands NO FORCE ROW LEVEL SECURITY;
ALTER TABLE ota_commands DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS policy_violations_tenant_isolation ON policy_violations;
ALTER TABLE policy_violations NO FORCE ROW LEVEL SECURITY;
ALTER TABLE policy_violations DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS anomalies_tenant_isolation ON anomalies;
ALTER TABLE anomalies NO FORCE ROW LEVEL SECURITY;
ALTER TABLE anomalies DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS sim_segments_tenant_isolation ON sim_segments;
ALTER TABLE sim_segments NO FORCE ROW LEVEL SECURITY;
ALTER TABLE sim_segments DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS msisdn_pool_tenant_isolation ON msisdn_pool;
ALTER TABLE msisdn_pool NO FORCE ROW LEVEL SECURITY;
ALTER TABLE msisdn_pool DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS notification_configs_tenant_isolation ON notification_configs;
ALTER TABLE notification_configs NO FORCE ROW LEVEL SECURITY;
ALTER TABLE notification_configs DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS notifications_tenant_isolation ON notifications;
ALTER TABLE notifications NO FORCE ROW LEVEL SECURITY;
ALTER TABLE notifications DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS jobs_tenant_isolation ON jobs;
ALTER TABLE jobs NO FORCE ROW LEVEL SECURITY;
ALTER TABLE jobs DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS audit_logs_tenant_isolation ON audit_logs;
ALTER TABLE audit_logs NO FORCE ROW LEVEL SECURITY;
ALTER TABLE audit_logs DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS cdrs_tenant_isolation ON cdrs;
ALTER TABLE cdrs NO FORCE ROW LEVEL SECURITY;
ALTER TABLE cdrs DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS sessions_tenant_isolation ON sessions;
ALTER TABLE sessions NO FORCE ROW LEVEL SECURITY;
ALTER TABLE sessions DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS operator_grants_tenant_isolation ON operator_grants;
ALTER TABLE operator_grants NO FORCE ROW LEVEL SECURITY;
ALTER TABLE operator_grants DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS policies_tenant_isolation ON policies;
ALTER TABLE policies NO FORCE ROW LEVEL SECURITY;
ALTER TABLE policies DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS ip_pools_tenant_isolation ON ip_pools;
ALTER TABLE ip_pools NO FORCE ROW LEVEL SECURITY;
ALTER TABLE ip_pools DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS apns_tenant_isolation ON apns;
ALTER TABLE apns NO FORCE ROW LEVEL SECURITY;
ALTER TABLE apns DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS sims_tenant_isolation ON sims;
ALTER TABLE sims NO FORCE ROW LEVEL SECURITY;
ALTER TABLE sims DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS api_keys_tenant_isolation ON api_keys;
ALTER TABLE api_keys NO FORCE ROW LEVEL SECURITY;
ALTER TABLE api_keys DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS users_tenant_isolation ON users;
ALTER TABLE users NO FORCE ROW LEVEL SECURITY;
ALTER TABLE users DISABLE ROW LEVEL SECURITY;

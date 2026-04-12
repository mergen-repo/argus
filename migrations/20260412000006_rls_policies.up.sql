-- STORY-064 Task 7: Row-Level Security defense-in-depth (DEV-167)
-- Enables RLS on all multi-tenant tables as a backstop against ad-hoc database access.
-- Application role (argus_app) MUST have BYPASSRLS (configured out-of-band in deploy/).
-- Session variable 'app.current_tenant' is set by gateway middleware per-request.
-- Non-app connections without SET app.current_tenant will see zero rows — defense-in-depth.
--
-- Notes:
--   - FORCE ROW LEVEL SECURITY is applied so policies cover the table owner too
--     (the migration role). BYPASSRLS on argus_app lets the app through at runtime.
--   - sessions/cdrs are TimescaleDB hypertables; RLS applies to the parent hypertable
--     (PG 13+). Compressed chunks may be excluded from RLS filtering for non-app roles;
--     this is acceptable for defense-in-depth, since app access uses BYPASSRLS.
--   - Role grants (ALTER ROLE argus_app BYPASSRLS, reporting-role SELECT grants) are
--     handled out-of-band in deploy/ — this migration does NOT touch roles.
--   - See docs/architecture/db/rls.md for operational guidance.

-- ============================================================
-- Direct tenant_id tables (simple USING clause)
-- ============================================================

-- users
ALTER TABLE users ENABLE ROW LEVEL SECURITY;
ALTER TABLE users FORCE ROW LEVEL SECURITY;
CREATE POLICY users_tenant_isolation ON users
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- api_keys
ALTER TABLE api_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE api_keys FORCE ROW LEVEL SECURITY;
CREATE POLICY api_keys_tenant_isolation ON api_keys
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- sims (LIST-partitioned by operator_id)
ALTER TABLE sims ENABLE ROW LEVEL SECURITY;
ALTER TABLE sims FORCE ROW LEVEL SECURITY;
CREATE POLICY sims_tenant_isolation ON sims
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- apns
ALTER TABLE apns ENABLE ROW LEVEL SECURITY;
ALTER TABLE apns FORCE ROW LEVEL SECURITY;
CREATE POLICY apns_tenant_isolation ON apns
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- ip_pools
ALTER TABLE ip_pools ENABLE ROW LEVEL SECURITY;
ALTER TABLE ip_pools FORCE ROW LEVEL SECURITY;
CREATE POLICY ip_pools_tenant_isolation ON ip_pools
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- policies
ALTER TABLE policies ENABLE ROW LEVEL SECURITY;
ALTER TABLE policies FORCE ROW LEVEL SECURITY;
CREATE POLICY policies_tenant_isolation ON policies
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- operator_grants
ALTER TABLE operator_grants ENABLE ROW LEVEL SECURITY;
ALTER TABLE operator_grants FORCE ROW LEVEL SECURITY;
CREATE POLICY operator_grants_tenant_isolation ON operator_grants
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- sessions (TimescaleDB hypertable)
ALTER TABLE sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE sessions FORCE ROW LEVEL SECURITY;
CREATE POLICY sessions_tenant_isolation ON sessions
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- cdrs (TimescaleDB hypertable)
ALTER TABLE cdrs ENABLE ROW LEVEL SECURITY;
ALTER TABLE cdrs FORCE ROW LEVEL SECURITY;
CREATE POLICY cdrs_tenant_isolation ON cdrs
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- audit_logs (RANGE-partitioned by created_at)
ALTER TABLE audit_logs ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_logs FORCE ROW LEVEL SECURITY;
CREATE POLICY audit_logs_tenant_isolation ON audit_logs
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- jobs
ALTER TABLE jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE jobs FORCE ROW LEVEL SECURITY;
CREATE POLICY jobs_tenant_isolation ON jobs
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- notifications
ALTER TABLE notifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE notifications FORCE ROW LEVEL SECURITY;
CREATE POLICY notifications_tenant_isolation ON notifications
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- notification_configs
ALTER TABLE notification_configs ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_configs FORCE ROW LEVEL SECURITY;
CREATE POLICY notification_configs_tenant_isolation ON notification_configs
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- msisdn_pool
ALTER TABLE msisdn_pool ENABLE ROW LEVEL SECURITY;
ALTER TABLE msisdn_pool FORCE ROW LEVEL SECURITY;
CREATE POLICY msisdn_pool_tenant_isolation ON msisdn_pool
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- sim_segments
ALTER TABLE sim_segments ENABLE ROW LEVEL SECURITY;
ALTER TABLE sim_segments FORCE ROW LEVEL SECURITY;
CREATE POLICY sim_segments_tenant_isolation ON sim_segments
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- anomalies
ALTER TABLE anomalies ENABLE ROW LEVEL SECURITY;
ALTER TABLE anomalies FORCE ROW LEVEL SECURITY;
CREATE POLICY anomalies_tenant_isolation ON anomalies
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- policy_violations
ALTER TABLE policy_violations ENABLE ROW LEVEL SECURITY;
ALTER TABLE policy_violations FORCE ROW LEVEL SECURITY;
CREATE POLICY policy_violations_tenant_isolation ON policy_violations
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- ota_commands
ALTER TABLE ota_commands ENABLE ROW LEVEL SECURITY;
ALTER TABLE ota_commands FORCE ROW LEVEL SECURITY;
CREATE POLICY ota_commands_tenant_isolation ON ota_commands
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- s3_archival_log
ALTER TABLE s3_archival_log ENABLE ROW LEVEL SECURITY;
ALTER TABLE s3_archival_log FORCE ROW LEVEL SECURITY;
CREATE POLICY s3_archival_log_tenant_isolation ON s3_archival_log
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- tenant_retention_config
ALTER TABLE tenant_retention_config ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_retention_config FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_retention_config_tenant_isolation ON tenant_retention_config
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- sla_reports
ALTER TABLE sla_reports ENABLE ROW LEVEL SECURITY;
ALTER TABLE sla_reports FORCE ROW LEVEL SECURITY;
CREATE POLICY sla_reports_tenant_isolation ON sla_reports
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- ============================================================
-- Indirect tables (tenant scope via JOIN / subquery)
-- ============================================================

-- policy_versions: scoped via policies.tenant_id
ALTER TABLE policy_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE policy_versions FORCE ROW LEVEL SECURITY;
CREATE POLICY policy_versions_tenant_isolation ON policy_versions
    USING (policy_id IN (
        SELECT id FROM policies
        WHERE tenant_id = current_setting('app.current_tenant', true)::uuid
    ));

-- policy_assignments: scoped via sims.tenant_id (sim_id is NOT NULL on this table)
ALTER TABLE policy_assignments ENABLE ROW LEVEL SECURITY;
ALTER TABLE policy_assignments FORCE ROW LEVEL SECURITY;
CREATE POLICY policy_assignments_tenant_isolation ON policy_assignments
    USING (sim_id IN (
        SELECT id FROM sims
        WHERE tenant_id = current_setting('app.current_tenant', true)::uuid
    ));

-- policy_rollouts: scoped via policy_versions → policies.tenant_id (two-level join)
ALTER TABLE policy_rollouts ENABLE ROW LEVEL SECURITY;
ALTER TABLE policy_rollouts FORCE ROW LEVEL SECURITY;
CREATE POLICY policy_rollouts_tenant_isolation ON policy_rollouts
    USING (policy_version_id IN (
        SELECT pv.id FROM policy_versions pv
        JOIN policies p ON pv.policy_id = p.id
        WHERE p.tenant_id = current_setting('app.current_tenant', true)::uuid
    ));

-- ip_addresses: scoped via ip_pools.tenant_id
ALTER TABLE ip_addresses ENABLE ROW LEVEL SECURITY;
ALTER TABLE ip_addresses FORCE ROW LEVEL SECURITY;
CREATE POLICY ip_addresses_tenant_isolation ON ip_addresses
    USING (pool_id IN (
        SELECT id FROM ip_pools
        WHERE tenant_id = current_setting('app.current_tenant', true)::uuid
    ));

-- esim_profiles: scoped via sims.tenant_id
ALTER TABLE esim_profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE esim_profiles FORCE ROW LEVEL SECURITY;
CREATE POLICY esim_profiles_tenant_isolation ON esim_profiles
    USING (sim_id IN (
        SELECT id FROM sims
        WHERE tenant_id = current_setting('app.current_tenant', true)::uuid
    ));

-- user_sessions: scoped via users.tenant_id
ALTER TABLE user_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_sessions FORCE ROW LEVEL SECURITY;
CREATE POLICY user_sessions_tenant_isolation ON user_sessions
    USING (user_id IN (
        SELECT id FROM users
        WHERE tenant_id = current_setting('app.current_tenant', true)::uuid
    ));

-- sim_state_history: scoped via sims.tenant_id (RANGE-partitioned by created_at)
ALTER TABLE sim_state_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE sim_state_history FORCE ROW LEVEL SECURITY;
CREATE POLICY sim_state_history_tenant_isolation ON sim_state_history
    USING (sim_id IN (
        SELECT id FROM sims
        WHERE tenant_id = current_setting('app.current_tenant', true)::uuid
    ));

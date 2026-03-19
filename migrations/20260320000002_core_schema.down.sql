-- Reverse core schema: drop all tables in reverse dependency order

-- Drop triggers first
DROP TRIGGER IF EXISTS trg_notification_configs_updated_at ON notification_configs;
DROP TRIGGER IF EXISTS trg_esim_profiles_updated_at ON esim_profiles;
DROP TRIGGER IF EXISTS trg_policies_updated_at ON policies;
DROP TRIGGER IF EXISTS trg_apns_updated_at ON apns;
DROP TRIGGER IF EXISTS trg_operators_updated_at ON operators;
DROP TRIGGER IF EXISTS trg_users_updated_at ON users;
DROP TRIGGER IF EXISTS trg_tenants_updated_at ON tenants;
DROP FUNCTION IF EXISTS update_updated_at_column();

-- Drop tables in reverse dependency order
DROP TABLE IF EXISTS sim_segments CASCADE;
DROP TABLE IF EXISTS msisdn_pool CASCADE;
DROP TABLE IF EXISTS operator_health_logs CASCADE;
DROP TABLE IF EXISTS notification_configs CASCADE;
DROP TABLE IF EXISTS notifications CASCADE;
DROP TABLE IF EXISTS jobs CASCADE;
DROP TABLE IF EXISTS audit_logs CASCADE;
DROP TABLE IF EXISTS cdrs CASCADE;
DROP TABLE IF EXISTS sessions CASCADE;
DROP TABLE IF EXISTS policy_rollouts CASCADE;
DROP TABLE IF EXISTS policy_assignments CASCADE;
DROP TABLE IF EXISTS sim_state_history CASCADE;
DROP TABLE IF EXISTS sims CASCADE;
DROP TABLE IF EXISTS esim_profiles CASCADE;
DROP TABLE IF EXISTS ip_addresses CASCADE;
DROP TABLE IF EXISTS ip_pools CASCADE;
DROP TABLE IF EXISTS apns CASCADE;
DROP TABLE IF EXISTS policy_versions CASCADE;
DROP TABLE IF EXISTS policies CASCADE;
DROP TABLE IF EXISTS operator_grants CASCADE;
DROP TABLE IF EXISTS operators CASCADE;
DROP TABLE IF EXISTS api_keys CASCADE;
DROP TABLE IF EXISTS user_sessions CASCADE;
DROP TABLE IF EXISTS users CASCADE;
DROP TABLE IF EXISTS tenants CASCADE;

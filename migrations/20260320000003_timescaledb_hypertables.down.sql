-- Reverse hypertable setup
-- Note: TimescaleDB hypertables cannot be reverted to regular tables.
-- The down migration removes policies and indexes added by this migration.
-- The tables themselves will be dropped by the core_schema down migration.

-- Remove retention policy
SELECT remove_retention_policy('operator_health_logs', if_exists => true);

-- Remove compression policies
SELECT remove_compression_policy('operator_health_logs', if_exists => true);
SELECT remove_compression_policy('cdrs', if_exists => true);
SELECT remove_compression_policy('sessions', if_exists => true);

-- Drop indexes added in this migration
DROP INDEX IF EXISTS idx_sessions_sim_active;
DROP INDEX IF EXISTS idx_sessions_tenant_active;
DROP INDEX IF EXISTS idx_sessions_tenant_operator;
DROP INDEX IF EXISTS idx_sessions_acct_session;
DROP INDEX IF EXISTS idx_cdrs_session;
DROP INDEX IF EXISTS idx_cdrs_tenant_time;
DROP INDEX IF EXISTS idx_cdrs_tenant_operator_time;
DROP INDEX IF EXISTS idx_cdrs_sim_time;

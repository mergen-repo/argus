-- Migration: 20260412000003_enum_check_constraints (DOWN)
--
-- Drops all 9 CHECK constraints added by the up migration, in reverse order.

ALTER TABLE operators        DROP CONSTRAINT IF EXISTS chk_operators_state;
ALTER TABLE policy_versions  DROP CONSTRAINT IF EXISTS chk_policy_versions_state;
ALTER TABLE policies         DROP CONSTRAINT IF EXISTS chk_policies_state;
ALTER TABLE apns             DROP CONSTRAINT IF EXISTS chk_apns_state;
ALTER TABLE sims             DROP CONSTRAINT IF EXISTS chk_sims_sim_type;
ALTER TABLE sims             DROP CONSTRAINT IF EXISTS chk_sims_state;
ALTER TABLE users            DROP CONSTRAINT IF EXISTS chk_users_state;
ALTER TABLE users            DROP CONSTRAINT IF EXISTS chk_users_role;
ALTER TABLE tenants          DROP CONSTRAINT IF EXISTS chk_tenants_state;

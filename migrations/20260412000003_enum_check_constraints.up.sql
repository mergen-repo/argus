-- Migration: 20260412000003_enum_check_constraints
--
-- Purpose: Add DB-level CHECK constraints on all VARCHAR enum columns that
-- currently lack them. These constraints are defense-in-depth — the application
-- layer already enforces valid values, but the DB constraint prevents any
-- direct-SQL or migration-script from inserting an invalid state.
--
-- Tables covered (9 constraints):
--   tenants.state, users.role, users.state, sims.state, sims.sim_type,
--   apns.state, policies.state, policy_versions.state, operators.state
--
-- Pattern per constraint:
--   1. Fail-fast guard: raise if any existing row would violate the new constraint.
--   2. ALTER TABLE ADD CONSTRAINT.
--
-- Naming: chk_<table>_<column>

-- ============================================================
-- 1. tenants.state
-- ============================================================
DO $$ BEGIN
  IF (SELECT count(*) FROM tenants WHERE state NOT IN ('active','suspended','terminated') OR state IS NULL) > 0 THEN
    RAISE EXCEPTION 'chk_tenants_state violation: bad rows exist';
  END IF;
END $$;

ALTER TABLE tenants ADD CONSTRAINT chk_tenants_state
  CHECK (state IN ('active','suspended','terminated'));

-- ============================================================
-- 2. users.role
-- ============================================================
DO $$ BEGIN
  IF (SELECT count(*) FROM users WHERE role NOT IN ('super_admin','tenant_admin','sim_manager','policy_editor','compliance_officer','auditor','api_user') OR role IS NULL) > 0 THEN
    RAISE EXCEPTION 'chk_users_role violation: bad rows exist';
  END IF;
END $$;

ALTER TABLE users ADD CONSTRAINT chk_users_role
  CHECK (role IN ('super_admin','tenant_admin','sim_manager','policy_editor','compliance_officer','auditor','api_user'));

-- ============================================================
-- 3. users.state
-- ============================================================
DO $$ BEGIN
  IF (SELECT count(*) FROM users WHERE state NOT IN ('active','disabled','invited') OR state IS NULL) > 0 THEN
    RAISE EXCEPTION 'chk_users_state violation: bad rows exist';
  END IF;
END $$;

ALTER TABLE users ADD CONSTRAINT chk_users_state
  CHECK (state IN ('active','disabled','invited'));

-- ============================================================
-- 4. sims.state
-- ============================================================
DO $$ BEGIN
  IF (SELECT count(*) FROM sims WHERE state NOT IN ('ordered','active','suspended','terminated','stolen_lost','available','purged') OR state IS NULL) > 0 THEN
    RAISE EXCEPTION 'chk_sims_state violation: bad rows exist';
  END IF;
END $$;

ALTER TABLE sims ADD CONSTRAINT chk_sims_state
  CHECK (state IN ('ordered','active','suspended','terminated','stolen_lost','available','purged'));

-- ============================================================
-- 5. sims.sim_type
-- ============================================================
DO $$ BEGIN
  IF (SELECT count(*) FROM sims WHERE sim_type NOT IN ('physical','esim') OR sim_type IS NULL) > 0 THEN
    RAISE EXCEPTION 'chk_sims_sim_type violation: bad rows exist';
  END IF;
END $$;

ALTER TABLE sims ADD CONSTRAINT chk_sims_sim_type
  CHECK (sim_type IN ('physical','esim'));

-- ============================================================
-- 6. apns.state
-- ============================================================
DO $$ BEGIN
  IF (SELECT count(*) FROM apns WHERE state NOT IN ('active','archived') OR state IS NULL) > 0 THEN
    RAISE EXCEPTION 'chk_apns_state violation: bad rows exist';
  END IF;
END $$;

ALTER TABLE apns ADD CONSTRAINT chk_apns_state
  CHECK (state IN ('active','archived'));

-- ============================================================
-- 7. policies.state
-- ============================================================
DO $$ BEGIN
  IF (SELECT count(*) FROM policies WHERE state NOT IN ('active','disabled','archived') OR state IS NULL) > 0 THEN
    RAISE EXCEPTION 'chk_policies_state violation: bad rows exist';
  END IF;
END $$;

ALTER TABLE policies ADD CONSTRAINT chk_policies_state
  CHECK (state IN ('active','disabled','archived'));

-- ============================================================
-- 8. policy_versions.state
-- ============================================================
DO $$ BEGIN
  IF (SELECT count(*) FROM policy_versions WHERE state NOT IN ('draft','active','rolling_out','superseded','archived') OR state IS NULL) > 0 THEN
    RAISE EXCEPTION 'chk_policy_versions_state violation: bad rows exist';
  END IF;
END $$;

ALTER TABLE policy_versions ADD CONSTRAINT chk_policy_versions_state
  CHECK (state IN ('draft','active','rolling_out','superseded','archived'));

-- ============================================================
-- 9. operators.state
-- ============================================================
DO $$ BEGIN
  IF (SELECT count(*) FROM operators WHERE state NOT IN ('active','disabled') OR state IS NULL) > 0 THEN
    RAISE EXCEPTION 'chk_operators_state violation: bad rows exist';
  END IF;
END $$;

ALTER TABLE operators ADD CONSTRAINT chk_operators_state
  CHECK (state IN ('active','disabled'));

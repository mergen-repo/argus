-- Migration: 20260427000001_policy_state_machine
--
-- Purpose: FIX-231 — make policy_assignments the canonical source of truth for the
-- SIM → policy_version mapping. The sims.policy_version_id column becomes a
-- trigger-synced denormalised pointer for the RADIUS hot path; assignment writes
-- propagate to sims via trg_sims_policy_version_sync (DEV-346).
--
-- Also enforces two operational invariants at the schema level:
--   1. Single in-flight rollout per policy: partial unique index on
--      policy_rollouts.policy_id WHERE state IN ('pending','in_progress').
--      Requires the new policy_rollouts.policy_id column (DEV-345) since the
--      table only links to policy_version_id today.
--   2. Single active version per policy: partial unique index on
--      policy_versions.policy_id WHERE state = 'active'. A defensive guard
--      (DEV-352) aborts the migration with a clear error if any policy
--      already violates this constraint, so we do not silently corrupt data.
--
-- Note (DEV-347): the CHECK constraint on policy_versions.state is already
-- defined in migrations/20260412000003_enum_check_constraints.up.sql:111;
-- this migration deliberately does NOT redeclare it.
--
-- See docs/stories/fix-ui-review/FIX-231-plan.md §Decisions for the full design.

BEGIN;

-- =====================================================================
-- Step 1 — Add policy_rollouts.policy_id (DEV-345)
-- =====================================================================
-- Backfill from policy_versions before adding NOT NULL + FK so existing
-- rollout rows survive the migration.
ALTER TABLE policy_rollouts ADD COLUMN IF NOT EXISTS policy_id UUID;

UPDATE policy_rollouts pr
   SET policy_id = pv.policy_id
  FROM policy_versions pv
 WHERE pv.id = pr.policy_version_id
   AND pr.policy_id IS NULL;

ALTER TABLE policy_rollouts ALTER COLUMN policy_id SET NOT NULL;

ALTER TABLE policy_rollouts
  ADD CONSTRAINT fk_policy_rollouts_policy
  FOREIGN KEY (policy_id) REFERENCES policies(id);

CREATE INDEX IF NOT EXISTS idx_policy_rollouts_policy
  ON policy_rollouts (policy_id);

-- =====================================================================
-- Step 2 — Trigger function: single writer of sims.policy_version_id (DEV-346)
-- =====================================================================
-- policy_assignments is now canonical. This trigger keeps sims.policy_version_id
-- in lock-step with the assignment row so the RADIUS hot path can keep doing
-- a single-row read on sims without joining to policy_assignments.
--
-- PAT-016: trigger references NEW.policy_version_id and NEW.sim_id (assignment
-- table columns) — never NEW.id, which would be the assignment row PK.
CREATE OR REPLACE FUNCTION sims_policy_version_sync()
RETURNS TRIGGER AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    UPDATE sims
       SET policy_version_id = NULL,
           updated_at        = NOW()
     WHERE id = OLD.sim_id;
    RETURN OLD;
  ELSE
    UPDATE sims
       SET policy_version_id = NEW.policy_version_id,
           updated_at        = NOW()
     WHERE id = NEW.sim_id;
    RETURN NEW;
  END IF;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_sims_policy_version_sync ON policy_assignments;
CREATE TRIGGER trg_sims_policy_version_sync
AFTER INSERT OR UPDATE OF policy_version_id OR DELETE ON policy_assignments
FOR EACH ROW EXECUTE FUNCTION sims_policy_version_sync();

-- =====================================================================
-- Step 3 — Single in-flight rollout per policy (AC-5, DEV-345)
-- =====================================================================
CREATE UNIQUE INDEX IF NOT EXISTS policy_active_rollout
  ON policy_rollouts (policy_id)
  WHERE state IN ('pending', 'in_progress');

-- =====================================================================
-- Step 4 — Defensive guard before single-active-version index (DEV-352)
-- =====================================================================
-- If any policy already has 2+ active versions, the partial unique index
-- below would fail with a generic "could not create unique index" error.
-- Surface a clear, actionable error instead so operators know exactly
-- what to reconcile before re-running the migration.
DO $$
DECLARE
  conflict_count INT;
BEGIN
  SELECT count(*) INTO conflict_count FROM (
    SELECT policy_id
      FROM policy_versions
     WHERE state = 'active'
     GROUP BY policy_id
    HAVING count(*) > 1
  ) x;

  IF conflict_count > 0 THEN
    RAISE EXCEPTION 'FIX-231 schema migration aborted: % policies have multiple active versions. Run reconciliation or fix manually before retrying.', conflict_count;
  END IF;
END $$;

-- =====================================================================
-- Step 5 — Single active version per policy (AC-3 invariant)
-- =====================================================================
CREATE UNIQUE INDEX IF NOT EXISTS policy_active_version
  ON policy_versions (policy_id)
  WHERE state = 'active';

COMMIT;

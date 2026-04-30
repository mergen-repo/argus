-- Reverse of 20260427000001_policy_state_machine
--
-- Drop order is the reverse of creation: indexes first, then trigger and
-- function, then policy_rollouts.policy_id column (with its FK + supporting
-- index). Wrapping in a single transaction keeps a partial down-migration
-- from leaving the schema half-reverted.

BEGIN;

DROP INDEX IF EXISTS policy_active_version;
DROP INDEX IF EXISTS policy_active_rollout;

DROP TRIGGER IF EXISTS trg_sims_policy_version_sync ON policy_assignments;
DROP FUNCTION IF EXISTS sims_policy_version_sync();

ALTER TABLE policy_rollouts DROP CONSTRAINT IF EXISTS fk_policy_rollouts_policy;
DROP INDEX IF EXISTS idx_policy_rollouts_policy;
ALTER TABLE policy_rollouts DROP COLUMN IF EXISTS policy_id;

COMMIT;

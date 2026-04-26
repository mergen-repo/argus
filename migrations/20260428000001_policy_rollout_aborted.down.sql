DROP INDEX IF EXISTS idx_policy_rollouts_aborted_at;
ALTER TABLE policy_rollouts DROP COLUMN IF EXISTS aborted_at;

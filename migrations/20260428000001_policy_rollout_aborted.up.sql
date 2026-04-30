-- Migration: 20260428000001_policy_rollout_aborted
-- Purpose: FIX-232 — adds aborted_at column for the new AbortRollout endpoint.
-- Aborted rollouts retain migrated assignments (no revert). Distinct from rolled_back
-- (which reverts) and completed (which finalises).

ALTER TABLE policy_rollouts ADD COLUMN IF NOT EXISTS aborted_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_policy_rollouts_aborted_at
  ON policy_rollouts (aborted_at) WHERE aborted_at IS NOT NULL;

COMMENT ON COLUMN policy_rollouts.aborted_at IS
  'FIX-232: timestamp set when admin aborts an in-progress rollout via POST /policy-rollouts/{id}/abort. Aborted rollouts retain migrated assignments (no revert).';

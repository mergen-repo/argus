-- Down migration: 20260429000001_policy_assignments_stage_pct
-- Purpose: FIX-233 — remove stage_pct column and its composite index.

BEGIN;

DROP INDEX IF EXISTS idx_policy_assignments_rollout_stage;

ALTER TABLE policy_assignments
    DROP COLUMN IF EXISTS stage_pct;

COMMIT;

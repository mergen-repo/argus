-- Migration: 20260429000001_policy_assignments_stage_pct
-- Purpose: FIX-233 — add stage_pct column to policy_assignments for rollout cohort filter.
-- NULL for legacy rows (pre-FIX-233); new assignments written by rollout executeStage will
-- carry the stage percentage (1, 10, 100, …). Composite index supports the SIM-list cohort
-- filter: WHERE pa.rollout_id = $X AND pa.stage_pct = $Y.
-- See docs/stories/fix-ui-review/FIX-233-plan.md (Task 0, AC-7).

BEGIN;

ALTER TABLE policy_assignments
    ADD COLUMN IF NOT EXISTS stage_pct INT;

COMMENT ON COLUMN policy_assignments.stage_pct IS
    'NULL for legacy rows pre-FIX-233; otherwise the rollout stage percentage at the time of migration (1, 10, 100, etc.). Used by SIM list cohort filter (FIX-233 AC-7).';

CREATE INDEX IF NOT EXISTS idx_policy_assignments_rollout_stage
    ON policy_assignments (rollout_id, stage_pct);

COMMIT;

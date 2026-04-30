-- 20260430000001_coa_status_enum_extension.up.sql
-- FIX-234: Extend coa_status value-set with queued, no_session, skipped
-- Final canonical set: pending | queued | acked | failed | no_session | skipped
--
-- Sessions schema verified via \d sessions:
--   column: session_state VARCHAR(20) NOT NULL DEFAULT 'active'
--   active filter: session_state = 'active'

-- Step 1: Reclassify pre-existing pending rows that have no active session.
-- Rows with coa_status = 'pending' but no matching active session are stale —
-- the SIM has no live session, so CoA cannot succeed. Reclassify to no_session.
UPDATE policy_assignments pa
SET coa_status = 'no_session'
WHERE coa_status = 'pending'
  AND NOT EXISTS (
      SELECT 1 FROM sessions s
      WHERE s.sim_id = pa.sim_id
        AND s.session_state = 'active'
  );

-- Step 2: Add CHECK constraint pinning the canonical set.
-- Existing column is VARCHAR(20); we add a CHECK to enforce the enum semantics.
ALTER TABLE policy_assignments
ADD CONSTRAINT chk_coa_status
CHECK (coa_status IN ('pending', 'queued', 'acked', 'failed', 'no_session', 'skipped'));

-- Step 3: Index to support the failure-alerter sweep query (60s cadence)
-- and the CoA status counts gauge.
CREATE INDEX IF NOT EXISTS idx_policy_assignments_coa_failed_age
ON policy_assignments(coa_status, coa_sent_at)
WHERE coa_status = 'failed';

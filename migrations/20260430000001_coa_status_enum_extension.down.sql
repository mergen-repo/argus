-- 20260430000001_coa_status_enum_extension.down.sql
-- FIX-234: Revert CHECK constraint and partial index for coa_status enum extension

DROP INDEX IF EXISTS idx_policy_assignments_coa_failed_age;
ALTER TABLE policy_assignments DROP CONSTRAINT IF EXISTS chk_coa_status;
-- Reclassification is intentionally NOT reversed: a row classified as no_session
-- is more accurate than the pre-FIX-234 over-counted pending state.
-- If a strict revert is required, manually update no_session rows back to pending.

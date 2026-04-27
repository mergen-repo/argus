-- Down: 20260503000003_esim_profile_state_failed
-- Restore the original CHECK constraint without 'failed'.
-- Note: If any rows have profile_state='failed', this rollback will fail.
-- Ensure no failed-state rows exist before rolling back.

ALTER TABLE esim_profiles DROP CONSTRAINT IF EXISTS chk_esim_profile_state;
ALTER TABLE esim_profiles ADD CONSTRAINT chk_esim_profile_state
  CHECK (profile_state IN ('available','enabled','disabled','deleted'));

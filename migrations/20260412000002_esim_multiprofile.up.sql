-- Add profile_id column
ALTER TABLE esim_profiles ADD COLUMN IF NOT EXISTS profile_id VARCHAR(64);

-- Change default from 'disabled' to 'available'
ALTER TABLE esim_profiles ALTER COLUMN profile_state SET DEFAULT 'available';

-- Remove old single-profile UNIQUE constraint
DROP INDEX IF EXISTS idx_esim_profiles_sim;
ALTER TABLE esim_profiles DROP CONSTRAINT IF EXISTS esim_profiles_sim_id_key;

-- Add new partial unique (only one enabled per SIM)
CREATE UNIQUE INDEX IF NOT EXISTS idx_esim_profiles_sim_enabled
  ON esim_profiles (sim_id) WHERE profile_state = 'enabled';

-- Add multi-profile unique (one row per sim+profile_id combo)
CREATE UNIQUE INDEX IF NOT EXISTS idx_esim_profiles_sim_profile
  ON esim_profiles (sim_id, profile_id) WHERE profile_id IS NOT NULL;

-- Add state filter index for listing
CREATE INDEX IF NOT EXISTS idx_esim_profiles_sim_state
  ON esim_profiles (sim_id, profile_state);

-- Valid states check
ALTER TABLE esim_profiles ADD CONSTRAINT chk_esim_profile_state
  CHECK (profile_state IN ('available', 'enabled', 'disabled', 'deleted'));

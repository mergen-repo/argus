-- Remove CHECK constraint
ALTER TABLE esim_profiles DROP CONSTRAINT IF EXISTS chk_esim_profile_state;

-- For sims with multiple profiles, keep only enabled or most recent one
DELETE FROM esim_profiles WHERE id NOT IN (
  SELECT DISTINCT ON (sim_id) id FROM esim_profiles
  ORDER BY sim_id,
    CASE WHEN profile_state = 'enabled' THEN 0 ELSE 1 END,
    updated_at DESC
);

-- Drop new indexes
DROP INDEX IF EXISTS idx_esim_profiles_sim_state;
DROP INDEX IF EXISTS idx_esim_profiles_sim_profile;
DROP INDEX IF EXISTS idx_esim_profiles_sim_enabled;

-- Restore original UNIQUE constraint on sim_id
CREATE UNIQUE INDEX IF NOT EXISTS idx_esim_profiles_sim ON esim_profiles (sim_id);

-- Revert default
ALTER TABLE esim_profiles ALTER COLUMN profile_state SET DEFAULT 'disabled';

-- Drop profile_id column
ALTER TABLE esim_profiles DROP COLUMN IF EXISTS profile_id;

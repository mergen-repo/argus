DROP INDEX IF EXISTS idx_sims_binding_mode;
ALTER TABLE sims
  DROP COLUMN IF EXISTS binding_grace_expires_at,
  DROP COLUMN IF EXISTS last_imei_seen_at,
  DROP COLUMN IF EXISTS binding_verified_at,
  DROP COLUMN IF EXISTS binding_status,
  DROP COLUMN IF EXISTS binding_mode,
  DROP COLUMN IF EXISTS bound_imei;

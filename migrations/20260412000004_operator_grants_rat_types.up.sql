-- Add grant-level RAT type overrides to operator_grants.
-- The SoR engine checks supported_rat_types on the grant first; if non-empty it
-- overrides the operator-level default, enabling per-tenant RAT restrictions
-- without touching the shared operators table.
ALTER TABLE operator_grants ADD COLUMN IF NOT EXISTS supported_rat_types TEXT[] NOT NULL DEFAULT '{}';

CREATE INDEX IF NOT EXISTS idx_operator_grants_rat_types_gin ON operator_grants USING gin (supported_rat_types);

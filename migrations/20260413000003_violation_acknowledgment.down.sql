DROP INDEX IF EXISTS idx_policy_violations_unack;

ALTER TABLE policy_violations
  DROP COLUMN IF EXISTS acknowledgment_note,
  DROP COLUMN IF EXISTS acknowledged_by,
  DROP COLUMN IF EXISTS acknowledged_at;

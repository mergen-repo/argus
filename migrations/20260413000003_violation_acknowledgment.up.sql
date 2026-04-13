ALTER TABLE policy_violations
  ADD COLUMN IF NOT EXISTS acknowledged_at   TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS acknowledged_by   UUID,
  ADD COLUMN IF NOT EXISTS acknowledgment_note TEXT;

CREATE INDEX IF NOT EXISTS idx_policy_violations_unack
  ON policy_violations(tenant_id, created_at DESC)
  WHERE acknowledged_at IS NULL;

-- Down migration for 20260423000001_alerts_dedup_statemachine.
-- Restores the FIX-209 non-unique partial index and removes the 4 new columns.

DROP INDEX IF EXISTS idx_alerts_cooldown_lookup;
DROP INDEX IF EXISTS idx_alerts_dedup_unique;

CREATE INDEX idx_alerts_dedup
  ON alerts (tenant_id, dedup_key)
  WHERE dedup_key IS NOT NULL
    AND state IN ('open', 'suppressed');

ALTER TABLE alerts
  DROP COLUMN IF EXISTS cooldown_until,
  DROP COLUMN IF EXISTS last_seen_at,
  DROP COLUMN IF EXISTS first_seen_at,
  DROP COLUMN IF EXISTS occurrence_count;

-- AC-1: six new nullable columns on parent partitioned table.
-- Per DEV-410, existing rows keep binding_mode IS NULL (no backfill).
-- Per AC-1, ALL six columns are NULLABLE permanently — single-step additive.
ALTER TABLE sims
  ADD COLUMN bound_imei VARCHAR(15) NULL,
  ADD COLUMN binding_mode VARCHAR(20) NULL
    CHECK (binding_mode IN ('strict','allowlist','first-use','tac-lock','grace-period','soft')),
  ADD COLUMN binding_status VARCHAR(20) NULL
    CHECK (binding_status IN ('verified','pending','mismatch','unbound','disabled')),
  ADD COLUMN binding_verified_at TIMESTAMPTZ NULL,
  ADD COLUMN last_imei_seen_at TIMESTAMPTZ NULL,
  ADD COLUMN binding_grace_expires_at TIMESTAMPTZ NULL;

-- Partial index — created on the parent partitioned table; PG auto-propagates
-- to existing + future partitions.
CREATE INDEX IF NOT EXISTS idx_sims_binding_mode
  ON sims (binding_mode)
  WHERE binding_mode IS NOT NULL;

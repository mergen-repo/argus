-- NOTE: cannot FK directly to sims(id) because sims PK is composite
-- (id, operator_id) due to LIST partitioning. RLS is enforced via the
-- parent SIM lookup at store layer (see AC-6). Cleanup on SIM delete is
-- handled via store-layer logic + tenant scoping check.
CREATE TABLE IF NOT EXISTS sim_imei_allowlist (
  sim_id UUID NOT NULL,
  imei VARCHAR(15) NOT NULL,
  added_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  added_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  PRIMARY KEY (sim_id, imei)
);

ALTER TABLE sim_imei_allowlist ENABLE ROW LEVEL SECURITY;
-- Deny by default. Cross-tenant guards live at the store layer (every
-- Add/Remove/List/IsAllowed must first verify sim_id resolves to current tenant).

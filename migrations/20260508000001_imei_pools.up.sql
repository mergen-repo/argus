-- TBL-56: imei_whitelist
CREATE TABLE IF NOT EXISTS imei_whitelist (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  kind VARCHAR(15) NOT NULL CHECK (kind IN ('full_imei','tac_range')),
  imei_or_tac VARCHAR(15) NOT NULL,
  device_model VARCHAR(255) NULL,
  description TEXT NULL,
  created_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT imei_whitelist_unique_entry UNIQUE (tenant_id, imei_or_tac)
);
CREATE INDEX IF NOT EXISTS idx_imei_whitelist_tenant_kind ON imei_whitelist (tenant_id, kind);
ALTER TABLE imei_whitelist ENABLE ROW LEVEL SECURITY;
CREATE POLICY imei_whitelist_tenant_isolation ON imei_whitelist
  USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- TBL-57: imei_greylist (adds quarantine_reason)
CREATE TABLE IF NOT EXISTS imei_greylist (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  kind VARCHAR(15) NOT NULL CHECK (kind IN ('full_imei','tac_range')),
  imei_or_tac VARCHAR(15) NOT NULL,
  device_model VARCHAR(255) NULL,
  description TEXT NULL,
  quarantine_reason TEXT NOT NULL,
  created_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT imei_greylist_unique_entry UNIQUE (tenant_id, imei_or_tac)
);
CREATE INDEX IF NOT EXISTS idx_imei_greylist_tenant_kind ON imei_greylist (tenant_id, kind);
ALTER TABLE imei_greylist ENABLE ROW LEVEL SECURITY;
CREATE POLICY imei_greylist_tenant_isolation ON imei_greylist
  USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- TBL-58: imei_blacklist (adds block_reason + imported_from)
CREATE TABLE IF NOT EXISTS imei_blacklist (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  kind VARCHAR(15) NOT NULL CHECK (kind IN ('full_imei','tac_range')),
  imei_or_tac VARCHAR(15) NOT NULL,
  device_model VARCHAR(255) NULL,
  description TEXT NULL,
  block_reason TEXT NOT NULL,
  imported_from VARCHAR(20) NOT NULL
    CHECK (imported_from IN ('manual','gsma_ceir','operator_eir')),
  created_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT imei_blacklist_unique_entry UNIQUE (tenant_id, imei_or_tac)
);
CREATE INDEX IF NOT EXISTS idx_imei_blacklist_tenant_kind ON imei_blacklist (tenant_id, kind);
ALTER TABLE imei_blacklist ENABLE ROW LEVEL SECURITY;
CREATE POLICY imei_blacklist_tenant_isolation ON imei_blacklist
  USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

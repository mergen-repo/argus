CREATE TABLE IF NOT EXISTS imei_history (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  sim_id UUID NOT NULL,
  observed_imei VARCHAR(15) NOT NULL,
  observed_software_version VARCHAR(2) NULL,
  observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  capture_protocol VARCHAR(20) NOT NULL
    CHECK (capture_protocol IN ('radius','diameter_s6a','5g_sba')),
  nas_ip_address INET NULL,
  was_mismatch BOOLEAN NOT NULL DEFAULT FALSE,
  alarm_raised BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE INDEX IF NOT EXISTS idx_imei_history_sim_observed ON imei_history (sim_id, observed_at DESC);
CREATE INDEX IF NOT EXISTS idx_imei_history_tenant ON imei_history (tenant_id);

ALTER TABLE imei_history ENABLE ROW LEVEL SECURITY;
-- RLS session variable is `app.current_tenant` (set by gateway middleware per-request,
-- per existing convention in 20260412000006_rls_policies.up.sql + 8 other policies).
CREATE POLICY imei_history_tenant_isolation ON imei_history
  USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

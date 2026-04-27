-- Migration: 20260503000001_esim_ota_commands
--
-- Purpose: OTA command queue for M2M SGP.02 SM-SR push pipeline.
-- Each row represents a single platform-initiated command sent to an eUICC via SM-SR.
-- State machine: queued → sent → acked (terminal) | failed (terminal) | timeout (→ retry or terminal)
-- FIX-235 — AC-1 (TBL-25)

CREATE TABLE esim_ota_commands (
  id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id             UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  eid                   VARCHAR(32) NOT NULL,
  profile_id            UUID REFERENCES esim_profiles(id) ON DELETE SET NULL,
  command_type          VARCHAR(20) NOT NULL,
  target_operator_id    UUID REFERENCES operators(id) ON DELETE SET NULL,
  source_profile_id     UUID REFERENCES esim_profiles(id) ON DELETE SET NULL,
  target_profile_id     UUID REFERENCES esim_profiles(id) ON DELETE SET NULL,
  status                VARCHAR(20) NOT NULL DEFAULT 'queued',
  smsr_command_id       VARCHAR(128),
  retry_count           INT NOT NULL DEFAULT 0,
  last_error            TEXT,
  job_id                UUID REFERENCES jobs(id) ON DELETE SET NULL,
  correlation_id        UUID,
  created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  sent_at               TIMESTAMPTZ,
  acked_at              TIMESTAMPTZ,
  next_retry_at         TIMESTAMPTZ,
  CONSTRAINT chk_esim_ota_command_type   CHECK (command_type IN ('enable','disable','switch','delete')),
  CONSTRAINT chk_esim_ota_command_status CHECK (status       IN ('queued','sent','acked','failed','timeout'))
);

CREATE INDEX idx_esim_ota_status_created ON esim_ota_commands (status, created_at);
CREATE INDEX idx_esim_ota_eid_created    ON esim_ota_commands (eid, created_at DESC);
CREATE INDEX idx_esim_ota_tenant_status  ON esim_ota_commands (tenant_id, status);
CREATE INDEX idx_esim_ota_sent_partial   ON esim_ota_commands (sent_at) WHERE status = 'sent';

ALTER TABLE esim_ota_commands ENABLE ROW LEVEL SECURITY;
ALTER TABLE esim_ota_commands FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_esim_ota ON esim_ota_commands
  USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

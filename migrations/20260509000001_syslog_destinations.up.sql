-- TBL-61: syslog_destinations
CREATE TABLE IF NOT EXISTS syslog_destinations (
  id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id            UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  name                 VARCHAR(255) NOT NULL,
  host                 VARCHAR(255) NOT NULL,
  port                 INT NOT NULL,
  transport            VARCHAR(10) NOT NULL CHECK (transport IN ('udp','tcp','tls')),
  format               VARCHAR(10) NOT NULL CHECK (format IN ('rfc3164','rfc5424')),
  facility             INT NOT NULL CHECK (facility BETWEEN 0 AND 23),
  severity_floor       INT NULL CHECK (severity_floor BETWEEN 0 AND 7),
  filter_categories    TEXT[] NOT NULL DEFAULT '{}',
  filter_min_severity  INT NULL CHECK (filter_min_severity BETWEEN 0 AND 7),
  tls_ca_pem           TEXT NULL,
  tls_client_cert_pem  TEXT NULL,
  tls_client_key_pem   TEXT NULL,
  enabled              BOOLEAN NOT NULL DEFAULT TRUE,
  last_delivery_at     TIMESTAMPTZ NULL,
  last_error           TEXT NULL,
  created_by           UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT syslog_destinations_unique_name UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_syslog_destinations_tenant_enabled
  ON syslog_destinations (tenant_id, enabled);

ALTER TABLE syslog_destinations ENABLE ROW LEVEL SECURITY;
-- RLS session variable is `app.current_tenant` (set by gateway middleware per-request,
-- per existing convention in 20260412000006_rls_policies.up.sql + 8 other policies).
-- Idempotent: drop-then-create keeps boot-time auto-migrate retry-safe (Phase 11 Gate FIX).
DROP POLICY IF EXISTS syslog_destinations_tenant_isolation ON syslog_destinations;
CREATE POLICY syslog_destinations_tenant_isolation ON syslog_destinations
  USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

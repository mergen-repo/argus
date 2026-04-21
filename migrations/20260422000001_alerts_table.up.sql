-- Migration: 20260422000001_alerts_table
--
-- Purpose: Unified alerts table for every argus.events.alert.triggered event
-- (SIM anomalies, operator down/recovery/SLA, NATS consumer lag, storage/DB
-- monitor, roaming renewal, policy violations, anomaly-batch crashes).
-- FIX-209 — see docs/stories/fix-ui-review/FIX-209-plan.md
--
-- Severity values: canonical 5-level enum from FIX-211 (chk_alerts_severity
-- uses the name reserved in FIX-211 plan §Out of Scope).
-- State values: 4-enum (suppressed reserved for FIX-210 dedup/state machine).
-- Source values: 5-enum (sim, operator, infra, policy, system).

CREATE TABLE IF NOT EXISTS alerts (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id       UUID NOT NULL REFERENCES tenants(id),
  type            VARCHAR(64) NOT NULL,
  severity        TEXT NOT NULL,
  source          VARCHAR(16) NOT NULL,
  state           VARCHAR(20) NOT NULL DEFAULT 'open',
  title           TEXT NOT NULL,
  description     TEXT NOT NULL DEFAULT '',
  meta            JSONB NOT NULL DEFAULT '{}',
  sim_id          UUID,
  operator_id     UUID,
  apn_id          UUID,
  dedup_key       VARCHAR(255),
  fired_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  acknowledged_at TIMESTAMPTZ,
  acknowledged_by UUID REFERENCES users(id),
  resolved_at     TIMESTAMPTZ,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT chk_alerts_severity CHECK (severity IN ('critical','high','medium','low','info')),
  CONSTRAINT chk_alerts_state    CHECK (state    IN ('open','acknowledged','resolved','suppressed')),
  CONSTRAINT chk_alerts_source   CHECK (source   IN ('sim','operator','infra','policy','system'))
);

CREATE INDEX idx_alerts_tenant_fired_at
  ON alerts (tenant_id, fired_at DESC);

CREATE INDEX idx_alerts_tenant_open
  ON alerts (tenant_id, fired_at DESC)
  WHERE state = 'open';

CREATE INDEX idx_alerts_tenant_source
  ON alerts (tenant_id, source);

CREATE INDEX idx_alerts_sim       ON alerts (sim_id)      WHERE sim_id IS NOT NULL;
CREATE INDEX idx_alerts_operator  ON alerts (operator_id) WHERE operator_id IS NOT NULL;
CREATE INDEX idx_alerts_apn       ON alerts (apn_id)      WHERE apn_id IS NOT NULL;

CREATE INDEX idx_alerts_dedup
  ON alerts (tenant_id, dedup_key)
  WHERE dedup_key IS NOT NULL AND state IN ('open','suppressed');

ALTER TABLE alerts ENABLE ROW LEVEL SECURITY;
ALTER TABLE alerts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_alerts ON alerts
  USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

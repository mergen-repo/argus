-- Migration: 20260503000002_esim_profile_stock
--
-- Purpose: Per-tenant, per-operator eSIM profile stock ledger.
-- Tracks total provisioned, allocated, and computed available profiles.
-- Allocation is done atomically via UPDATE+RETURNING (no advisory locks).
-- FIX-235 — AC-2 (TBL-26)

CREATE TABLE esim_profile_stock (
  tenant_id      UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  operator_id    UUID NOT NULL REFERENCES operators(id) ON DELETE CASCADE,
  total          BIGINT NOT NULL DEFAULT 0 CHECK (total >= 0),
  allocated      BIGINT NOT NULL DEFAULT 0 CHECK (allocated >= 0),
  available      BIGINT NOT NULL GENERATED ALWAYS AS (total - allocated) STORED,
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (tenant_id, operator_id),
  CONSTRAINT chk_stock_alloc_le_total CHECK (allocated <= total)
);

ALTER TABLE esim_profile_stock ENABLE ROW LEVEL SECURITY;
ALTER TABLE esim_profile_stock FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_stock ON esim_profile_stock
  USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

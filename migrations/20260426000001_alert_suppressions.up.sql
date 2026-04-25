-- Migration: 20260426000001_alert_suppressions
-- Purpose: TBL-55 alert mute rules — backs both ad-hoc Mute (AC-1) and saved
-- reusable rules (AC-5). rule_name NULL = ad-hoc; NOT NULL = saved rule.
-- Applied at trigger time via UpsertWithDedup (DEV-334).
-- FIX-229 — see docs/stories/fix-ui-review/FIX-229-plan.md (DEV-333, DEV-339, DEV-340).

CREATE TABLE IF NOT EXISTS alert_suppressions (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  scope_type   VARCHAR(16) NOT NULL,
  scope_value  TEXT        NOT NULL,
  expires_at   TIMESTAMPTZ NOT NULL,
  reason       TEXT,
  rule_name    VARCHAR(100),
  created_by   UUID REFERENCES users(id),
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT chk_alert_suppressions_scope_type
    CHECK (scope_type IN ('this','type','operator','dedup_key')),
  CONSTRAINT chk_alert_suppressions_scope_value_nonempty
    CHECK (length(scope_value) > 0)
);

-- IMPORTANT: NOW() is not IMMUTABLE → cannot be used in a partial-index predicate.
-- Use a non-partial composite index and apply expires_at > NOW() in queries.
CREATE INDEX idx_alert_suppressions_active
  ON alert_suppressions (tenant_id, scope_type, scope_value, expires_at);

CREATE INDEX idx_alert_suppressions_tenant_created
  ON alert_suppressions (tenant_id, created_at DESC);

CREATE UNIQUE INDEX uq_alert_suppressions_tenant_rule_name
  ON alert_suppressions (tenant_id, rule_name)
  WHERE rule_name IS NOT NULL;

CREATE INDEX idx_alert_suppressions_expires_at
  ON alert_suppressions (expires_at);

ALTER TABLE alert_suppressions ENABLE ROW LEVEL SECURITY;
ALTER TABLE alert_suppressions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_alert_suppressions ON alert_suppressions
  USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

COMMENT ON TABLE alert_suppressions IS
  'TBL-55 FIX-229 (DEV-333): alert mute rules. rule_name NULL = ad-hoc mute (AC-1); NOT NULL = saved rule (AC-5). Applied at trigger time via UpsertWithDedup (DEV-334).';

-- Additional index on alerts table for similar-alerts lookup across ALL states (DEV-339).
-- Existing idx_alerts_dedup is partial on state IN ('open','suppressed') — too narrow.
CREATE INDEX idx_alerts_dedup_all_states
  ON alerts (tenant_id, dedup_key, fired_at DESC)
  WHERE dedup_key IS NOT NULL;

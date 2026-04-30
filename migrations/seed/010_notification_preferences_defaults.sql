-- FIX-309: seed default notification_preferences rows so a freshly-onboarded
-- tenant has sensible defaults out of the box. UAT batch2 F-9: API returned
-- [] for every tenant because no defaults were ever inserted.
--
-- Defaults cover the 11 valid event types from internal/api/notification/handler.go:
--   - operator.down, operator.recovered  → critical, in_app+email
--   - sim.state_changed                  → info,     in_app
--   - job.completed                      → info,     in_app
--   - job.failed                         → high,     in_app+email
--   - alert.new                          → high,     in_app+email
--   - sla.violation                      → critical, in_app+email
--   - policy.rollout_completed           → info,     in_app
--   - quota.warning                      → high,     in_app+email
--   - quota.exceeded                     → critical, in_app+email
--   - anomaly.detected                   → high,     in_app+email
--
-- Idempotent via the (tenant_id, event_type) UNIQUE index. Seeds for ALL
-- existing tenants; new tenants must trigger a similar default-population
-- step at tenant creation (deferred to D-NNN — see step-log).

INSERT INTO notification_preferences (tenant_id, event_type, channels, severity_threshold, enabled)
SELECT t.id, e.event_type, e.channels, e.severity_threshold, true
FROM tenants t
CROSS JOIN (VALUES
  ('operator.down',            ARRAY['in_app','email']::varchar[], 'critical'),
  ('operator.recovered',       ARRAY['in_app','email']::varchar[], 'info'),
  ('sim.state_changed',        ARRAY['in_app']::varchar[],         'info'),
  ('job.completed',            ARRAY['in_app']::varchar[],         'info'),
  ('job.failed',               ARRAY['in_app','email']::varchar[], 'high'),
  ('alert.new',                ARRAY['in_app','email']::varchar[], 'high'),
  ('sla.violation',            ARRAY['in_app','email']::varchar[], 'critical'),
  ('policy.rollout_completed', ARRAY['in_app']::varchar[],         'info'),
  ('quota.warning',            ARRAY['in_app','email']::varchar[], 'high'),
  ('quota.exceeded',           ARRAY['in_app','email']::varchar[], 'critical'),
  ('anomaly.detected',         ARRAY['in_app','email']::varchar[], 'high')
) AS e(event_type, channels, severity_threshold)
ON CONFLICT (tenant_id, event_type) DO NOTHING;

-- SEED-12: Demo Fixture Oracles — Detail-Screen Tab Coverage
--
-- Purpose: Guarantee that named demo fixtures (DEMO-SIM-A/B/C, DEMO-OP-X/Y,
-- DEMO-APN-IOT/M2M, DEMO-SESS-1/2/3) have non-empty data on EVERY tab the
-- frontend Detail screens render. Counts are deterministic and documented in
-- docs/reports/seed-report.md "Detail-Screen Tab Oracles" section, where E1
-- and E5 testers assert against them.
--
-- This seed file is GAP-FILL ONLY: prior seeds (003, 005, 005a, 007, 008, 011)
-- already populate most data. This file:
--   1. Backfills binding_status='verified' on DEMO-SIM-A (was NULL)
--   2. Adds 10+ audit_logs per demo fixture where coverage was sparse
--   3. Populates sor_decision JSONB on DEMO-SESS-1/2/3 (zero before)
--   4. Adds policy_violations rows scoped to demo SIMs (table was 0 rows)
--   5. Adds alerts scoped to demo operators + APNs (audit/alerts tabs)
--   6. Adds operator-scoped + APN-scoped audit_logs
--
-- Idempotency: every INSERT uses ON CONFLICT DO NOTHING with deterministic
-- IDs, OR conditional DO $$ blocks gated by a marker row.
--
-- Audit hash chain: post-seed `argus repair-audit` rebuilds the chain.
--
-- Demo Fixture IDs (canonical — referenced from seed-report.md):
--   DEMO-SIM-A   = 1c869918-9d62-41ba-a23e-a7492ef24e26  (strict-verified)
--   DEMO-SIM-B   = 92cd76d7-eb12-45bd-b373-5fb1fb64ff9f  (strict-mismatch, active)
--   DEMO-SIM-C   = 4af3d846-e31e-4ae0-be4c-81f3ee4b756e  (allowlist-verified)
--   DEMO-OP-X    = 20000000-0000-0000-0000-000000000001  (Turkcell)
--   DEMO-OP-Y    = 20000000-0000-0000-0000-000000000002  (Vodafone TR)
--   DEMO-APN-IOT = 06000000-0000-0000-0000-000000000001  (iot.demo)
--   DEMO-APN-M2M = 06000000-0000-0000-0000-000000000002  (m2m.demo)
--   DEMO-SESS-1  = 431c84f7-2249-4a12-b9d0-d68b3b9f0080  (RADIUS active)
--   DEMO-SESS-2  = a33746d0-d21f-4b67-b724-7e7ef3bf49dd  (Diameter active)
--   DEMO-SESS-3  = 7f975eec-bf81-4096-a429-4a3536c85d3d  (5G SBA, sim_id=DEMO-SIM-B)
--   ADMIN_USER   = 00000000-0000-0000-0000-000000000010
--   TENANT_PRI   = 00000000-0000-0000-0000-000000000001
--   POLICY_QOS   = 05000000-0000-0000-0000-000000000001 (Demo Standard QoS)
--   POLICY_IOT   = 05000000-0000-0000-0000-000000000002 (Demo IoT Savings)
--   POLICY_VER_QOS = 05100000-0000-0000-0000-000000000001
--   POLICY_VER_IOT = 05100000-0000-0000-0000-000000000002

BEGIN;

-- ============================================================
-- 1) Backfill DEMO-SIM-A binding_status (was NULL → 'verified')
-- ============================================================
UPDATE sims
SET binding_status='verified',
    binding_verified_at = COALESCE(binding_verified_at, NOW() - INTERVAL '5 days')
WHERE id='1c869918-9d62-41ba-a23e-a7492ef24e26'
  AND (binding_status IS NULL OR binding_status='');

-- ============================================================
-- 2) Audit logs — gap fill so every demo fixture has >=10 entries
--    Wrapped in marker-gated DO block so re-runs are no-ops.
--    Hash chain placeholder zeros — repair-audit fixes post-seed.
-- ============================================================
DO $$
DECLARE
  zero_hash CONSTANT TEXT := repeat('0', 64);
  pri_tenant CONSTANT UUID := '00000000-0000-0000-0000-000000000001'::uuid;
  admin_user CONSTANT UUID := '00000000-0000-0000-0000-000000000010'::uuid;
  marker_exists BOOLEAN;
BEGIN
  -- Bypass user triggers (chain guard) for bulk insert
  PERFORM set_config('session_replication_role', 'replica', true);

  -- Check marker
  SELECT EXISTS (
    SELECT 1 FROM audit_logs
    WHERE tenant_id = pri_tenant
      AND after_data ? 'seed_generated_012'
  ) INTO marker_exists;

  IF NOT marker_exists THEN
    -- ===== DEMO-SIM-C audits (was 0; needs 12 distinct rows) =====
    INSERT INTO audit_logs (tenant_id, user_id, action, entity_type, entity_id, before_data, after_data, hash, prev_hash, correlation_id, ip_address, created_at) VALUES
    (pri_tenant, admin_user, 'sim.activate',                 'sim', '4af3d846-e31e-4ae0-be4c-81f3ee4b756e', '{"state":"ordered"}'::jsonb,                                 '{"state":"active","seed_generated_012":true}'::jsonb,                  zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '30 days'),
    (pri_tenant, admin_user, 'sim.policy_assign',            'sim', '4af3d846-e31e-4ae0-be4c-81f3ee4b756e', NULL,                                                          '{"policy_id":"05000000-0000-0000-0000-000000000002","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '28 days'),
    (pri_tenant, admin_user, 'binding.allowlist.add',        'sim', '4af3d846-e31e-4ae0-be4c-81f3ee4b756e', NULL,                                                          '{"imei":"353273090012345","seed_generated_012":true}'::jsonb,           zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '20 days'),
    (pri_tenant, admin_user, 'binding.allowlist.add',        'sim', '4af3d846-e31e-4ae0-be4c-81f3ee4b756e', NULL,                                                          '{"imei":"359225100023456","seed_generated_012":true}'::jsonb,           zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '18 days'),
    (pri_tenant, admin_user, 'binding.mode_changed',         'sim', '4af3d846-e31e-4ae0-be4c-81f3ee4b756e', '{"binding_mode":null}'::jsonb,                                '{"binding_mode":"allowlist","seed_generated_012":true}'::jsonb,         zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '15 days'),
    (pri_tenant, admin_user, 'binding.imei_observed',        'sim', '4af3d846-e31e-4ae0-be4c-81f3ee4b756e', NULL,                                                          '{"imei":"353273090012345","protocol":"radius","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '12 days'),
    (pri_tenant, admin_user, 'binding.imei_observed',        'sim', '4af3d846-e31e-4ae0-be4c-81f3ee4b756e', NULL,                                                          '{"imei":"359225100023456","protocol":"diameter_s6a","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '9 days'),
    (pri_tenant, admin_user, 'sim.update',                   'sim', '4af3d846-e31e-4ae0-be4c-81f3ee4b756e', '{"max_concurrent_sessions":1}'::jsonb,                        '{"max_concurrent_sessions":2,"seed_generated_012":true}'::jsonb,        zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '7 days'),
    (pri_tenant, admin_user, 'binding.imei_observed',        'sim', '4af3d846-e31e-4ae0-be4c-81f3ee4b756e', NULL,                                                          '{"imei":"353273090012345","protocol":"5g_sba","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '5 days'),
    (pri_tenant, admin_user, 'sim.policy_assign',            'sim', '4af3d846-e31e-4ae0-be4c-81f3ee4b756e', '{"policy_version_id":null}'::jsonb,                           '{"policy_version_id":"05100000-0000-0000-0000-000000000002","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '3 days'),
    (pri_tenant, admin_user, 'binding.verify',               'sim', '4af3d846-e31e-4ae0-be4c-81f3ee4b756e', NULL,                                                          '{"status":"verified","seed_generated_012":true}'::jsonb,                zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '2 days'),
    (pri_tenant, admin_user, 'sim.note',                     'sim', '4af3d846-e31e-4ae0-be4c-81f3ee4b756e', NULL,                                                          '{"note":"Allowlist demo fixture for tab oracle","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '1 days');

    -- ===== DEMO-OP-X (Turkcell) audit logs (was 0, needs 10) =====
    INSERT INTO audit_logs (tenant_id, user_id, action, entity_type, entity_id, before_data, after_data, hash, prev_hash, correlation_id, ip_address, created_at) VALUES
    (pri_tenant, admin_user, 'operator.update',              'operator', '20000000-0000-0000-0000-000000000001', '{"failover_timeout_ms":5000}'::jsonb,                       '{"failover_timeout_ms":4500,"seed_generated_012":true}'::jsonb,         zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '20 days'),
    (pri_tenant, admin_user, 'operator.health_probe',        'operator', '20000000-0000-0000-0000-000000000001', NULL,                                                       '{"latency_ms":42,"status":"healthy","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '15 days'),
    (pri_tenant, admin_user, 'operator.test_connection',     'operator', '20000000-0000-0000-0000-000000000001', NULL,                                                       '{"protocol":"radius","success":true,"latency_ms":38,"seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '12 days'),
    (pri_tenant, admin_user, 'operator.test_connection',     'operator', '20000000-0000-0000-0000-000000000001', NULL,                                                       '{"protocol":"diameter","success":true,"latency_ms":67,"seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '12 days'),
    (pri_tenant, admin_user, 'operator.update',              'operator', '20000000-0000-0000-0000-000000000001', '{"sla_uptime_target":99.90}'::jsonb,                       '{"sla_uptime_target":99.95,"seed_generated_012":true}'::jsonb,           zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '10 days'),
    (pri_tenant, admin_user, 'operator.circuit_breaker_trip','operator', '20000000-0000-0000-0000-000000000001', '{"circuit_state":"closed"}'::jsonb,                       '{"circuit_state":"open","reason":"5 consecutive failures","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '8 days'),
    (pri_tenant, admin_user, 'operator.circuit_breaker_recover','operator', '20000000-0000-0000-0000-000000000001', '{"circuit_state":"open"}'::jsonb,                       '{"circuit_state":"closed","seed_generated_012":true}'::jsonb,            zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '8 days'),
    (pri_tenant, admin_user, 'operator.update',              'operator', '20000000-0000-0000-0000-000000000001', '{"circuit_breaker_threshold":5}'::jsonb,                  '{"circuit_breaker_threshold":7,"seed_generated_012":true}'::jsonb,       zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '5 days'),
    (pri_tenant, admin_user, 'operator.health_probe',        'operator', '20000000-0000-0000-0000-000000000001', NULL,                                                       '{"latency_ms":51,"status":"healthy","seed_generated_012":true}'::jsonb,  zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '2 days'),
    (pri_tenant, admin_user, 'operator.note',                'operator', '20000000-0000-0000-0000-000000000001', NULL,                                                       '{"note":"Primary operator for IoT fleet — DEMO-OP-X","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '1 days');

    -- ===== DEMO-OP-Y (Vodafone TR) audit logs (was 0, needs 10) =====
    INSERT INTO audit_logs (tenant_id, user_id, action, entity_type, entity_id, before_data, after_data, hash, prev_hash, correlation_id, ip_address, created_at) VALUES
    (pri_tenant, admin_user, 'operator.update',              'operator', '20000000-0000-0000-0000-000000000002', '{"failover_policy":"reject"}'::jsonb,                      '{"failover_policy":"failover","seed_generated_012":true}'::jsonb,        zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '22 days'),
    (pri_tenant, admin_user, 'operator.health_probe',        'operator', '20000000-0000-0000-0000-000000000002', NULL,                                                       '{"latency_ms":61,"status":"healthy","seed_generated_012":true}'::jsonb,  zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '14 days'),
    (pri_tenant, admin_user, 'operator.test_connection',     'operator', '20000000-0000-0000-0000-000000000002', NULL,                                                       '{"protocol":"radius","success":true,"latency_ms":44,"seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '13 days'),
    (pri_tenant, admin_user, 'operator.test_connection',     'operator', '20000000-0000-0000-0000-000000000002', NULL,                                                       '{"protocol":"5g_sba","success":true,"latency_ms":29,"seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '13 days'),
    (pri_tenant, admin_user, 'operator.update',              'operator', '20000000-0000-0000-0000-000000000002', '{"sla_latency_threshold_ms":500}'::jsonb,                  '{"sla_latency_threshold_ms":450,"seed_generated_012":true}'::jsonb,      zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '11 days'),
    (pri_tenant, admin_user, 'operator.health_probe',        'operator', '20000000-0000-0000-0000-000000000002', NULL,                                                       '{"latency_ms":480,"status":"degraded","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '7 days'),
    (pri_tenant, admin_user, 'operator.health_probe',        'operator', '20000000-0000-0000-0000-000000000002', NULL,                                                       '{"latency_ms":58,"status":"healthy","seed_generated_012":true}'::jsonb,  zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '6 days'),
    (pri_tenant, admin_user, 'operator.update',              'operator', '20000000-0000-0000-0000-000000000002', '{"health_check_interval_sec":30}'::jsonb,                 '{"health_check_interval_sec":15,"seed_generated_012":true}'::jsonb,      zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '4 days'),
    (pri_tenant, admin_user, 'operator.test_connection',     'operator', '20000000-0000-0000-0000-000000000002', NULL,                                                       '{"protocol":"radius","success":true,"latency_ms":47,"seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '2 days'),
    (pri_tenant, admin_user, 'operator.note',                'operator', '20000000-0000-0000-0000-000000000002', NULL,                                                       '{"note":"Secondary operator with failover — DEMO-OP-Y","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '1 days');

    -- ===== DEMO-APN-IOT (iot.demo) audit logs (was 0, needs 10) =====
    INSERT INTO audit_logs (tenant_id, user_id, action, entity_type, entity_id, before_data, after_data, hash, prev_hash, correlation_id, ip_address, created_at) VALUES
    (pri_tenant, admin_user, 'apn.create',                   'apn', '06000000-0000-0000-0000-000000000001', NULL,                                                       '{"name":"iot.demo","apn_type":"iot","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '60 days'),
    (pri_tenant, admin_user, 'apn.update',                   'apn', '06000000-0000-0000-0000-000000000001', '{"display_name":null}'::jsonb,                              '{"display_name":"IoT Demo APN — fleet IoT modules","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '55 days'),
    (pri_tenant, admin_user, 'apn.policy_attach',            'apn', '06000000-0000-0000-0000-000000000001', NULL,                                                       '{"policy_id":"05000000-0000-0000-0000-000000000002","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '50 days'),
    (pri_tenant, admin_user, 'apn.update',                   'apn', '06000000-0000-0000-0000-000000000001', '{"supported_rat_types":[]}'::jsonb,                          '{"supported_rat_types":["LTE","NB-IoT","LTE-M"],"seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '40 days'),
    (pri_tenant, admin_user, 'apn.policy_attach',            'apn', '06000000-0000-0000-0000-000000000001', NULL,                                                       '{"policy_id":"05000000-0000-0000-0000-000000000001","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '30 days'),
    (pri_tenant, admin_user, 'apn.ip_pool_attach',           'apn', '06000000-0000-0000-0000-000000000001', NULL,                                                       '{"ip_pool_id":"07000000-0000-0000-0000-000000000001","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '25 days'),
    (pri_tenant, admin_user, 'apn.update',                   'apn', '06000000-0000-0000-0000-000000000001', '{"settings":{}}'::jsonb,                                     '{"settings":{"qos":"best-effort","mtu":1500},"seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '15 days'),
    (pri_tenant, admin_user, 'apn.note',                     'apn', '06000000-0000-0000-0000-000000000001', NULL,                                                       '{"note":"Primary IoT APN — DEMO-APN-IOT fixture","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '10 days'),
    (pri_tenant, admin_user, 'apn.update',                   'apn', '06000000-0000-0000-0000-000000000001', '{"display_name":"IoT Demo APN — fleet IoT modules"}'::jsonb, '{"display_name":"IoT Demo APN — Fleet IoT (NB-IoT/LTE-M)","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '5 days'),
    (pri_tenant, admin_user, 'apn.policy_detach',            'apn', '06000000-0000-0000-0000-000000000001', '{"policy_id":"05000000-0000-0000-0000-000000000001"}'::jsonb, '{"policy_id":"05000000-0000-0000-0000-000000000001","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '2 days');

    -- ===== DEMO-APN-M2M (m2m.demo) audit logs (was 0, needs 10) =====
    INSERT INTO audit_logs (tenant_id, user_id, action, entity_type, entity_id, before_data, after_data, hash, prev_hash, correlation_id, ip_address, created_at) VALUES
    (pri_tenant, admin_user, 'apn.create',                   'apn', '06000000-0000-0000-0000-000000000002', NULL,                                                       '{"name":"m2m.demo","apn_type":"m2m","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '58 days'),
    (pri_tenant, admin_user, 'apn.update',                   'apn', '06000000-0000-0000-0000-000000000002', '{"display_name":null}'::jsonb,                              '{"display_name":"M2M Demo APN — smart meters / streetlights","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '52 days'),
    (pri_tenant, admin_user, 'apn.policy_attach',            'apn', '06000000-0000-0000-0000-000000000002', NULL,                                                       '{"policy_id":"05000000-0000-0000-0000-000000000002","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '48 days'),
    (pri_tenant, admin_user, 'apn.update',                   'apn', '06000000-0000-0000-0000-000000000002', '{"supported_rat_types":[]}'::jsonb,                          '{"supported_rat_types":["LTE","NB-IoT"],"seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '38 days'),
    (pri_tenant, admin_user, 'apn.ip_pool_attach',           'apn', '06000000-0000-0000-0000-000000000002', NULL,                                                       '{"ip_pool_id":"07000000-0000-0000-0000-000000000002","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '28 days'),
    (pri_tenant, admin_user, 'apn.update',                   'apn', '06000000-0000-0000-0000-000000000002', '{"settings":{}}'::jsonb,                                     '{"settings":{"qos":"low-latency","mtu":1492},"seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '20 days'),
    (pri_tenant, admin_user, 'apn.note',                     'apn', '06000000-0000-0000-0000-000000000002', NULL,                                                       '{"note":"Primary M2M APN — DEMO-APN-M2M fixture","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '12 days'),
    (pri_tenant, admin_user, 'apn.update',                   'apn', '06000000-0000-0000-0000-000000000002', '{"display_name":"M2M Demo APN — smart meters / streetlights"}'::jsonb, '{"display_name":"M2M Demo APN — Smart Meters (water/elec)","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '8 days'),
    (pri_tenant, admin_user, 'apn.policy_attach',            'apn', '06000000-0000-0000-0000-000000000002', NULL,                                                       '{"policy_id":"05000000-0000-0000-0000-000000000001","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '6 days'),
    (pri_tenant, admin_user, 'apn.note',                     'apn', '06000000-0000-0000-0000-000000000002', NULL,                                                       '{"note":"Reviewed for go-live readiness","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '1 days');

    -- ===== DEMO-SESS-1 / DEMO-SESS-2 / DEMO-SESS-3 audit logs =====
    -- (12 rows total: ~4 per session; covers Audit tab on Session Detail)
    INSERT INTO audit_logs (tenant_id, user_id, action, entity_type, entity_id, before_data, after_data, hash, prev_hash, correlation_id, ip_address, created_at) VALUES
    (pri_tenant, admin_user, 'session.access_request',       'session', '431c84f7-2249-4a12-b9d0-d68b3b9f0080', NULL, '{"protocol":"radius","decision":"accept","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '2 hours'),
    (pri_tenant, admin_user, 'session.accounting_start',     'session', '431c84f7-2249-4a12-b9d0-d68b3b9f0080', NULL, '{"acct_status_type":"start","seed_generated_012":true}'::jsonb,             zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '2 hours'),
    (pri_tenant, admin_user, 'session.policy_applied',       'session', '431c84f7-2249-4a12-b9d0-d68b3b9f0080', NULL, '{"policy_version_id":"05100000-0000-0000-0000-000000000002","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '2 hours'),
    (pri_tenant, admin_user, 'session.interim_update',       'session', '431c84f7-2249-4a12-b9d0-d68b3b9f0080', NULL, '{"acct_status_type":"interim","bytes_in":50000000,"bytes_out":20000000,"seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '1 hours'),
    (pri_tenant, admin_user, 'session.access_request',       'session', 'a33746d0-d21f-4b67-b724-7e7ef3bf49dd', NULL, '{"protocol":"diameter","decision":"accept","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '3 hours'),
    (pri_tenant, admin_user, 'session.accounting_start',     'session', 'a33746d0-d21f-4b67-b724-7e7ef3bf49dd', NULL, '{"acct_status_type":"start","seed_generated_012":true}'::jsonb,             zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '3 hours'),
    (pri_tenant, admin_user, 'session.policy_applied',       'session', 'a33746d0-d21f-4b67-b724-7e7ef3bf49dd', NULL, '{"policy_version_id":"05100000-0000-0000-0000-000000000001","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '3 hours'),
    (pri_tenant, admin_user, 'session.interim_update',       'session', 'a33746d0-d21f-4b67-b724-7e7ef3bf49dd', NULL, '{"acct_status_type":"interim","bytes_in":3000000,"bytes_out":13000000,"seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '2 hours'),
    (pri_tenant, admin_user, 'session.access_request',       'session', '7f975eec-bf81-4096-a429-4a3536c85d3d', NULL, '{"protocol":"5g_sba","decision":"accept","seed_generated_012":true}'::jsonb,  zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '4 hours'),
    (pri_tenant, admin_user, 'session.accounting_start',     'session', '7f975eec-bf81-4096-a429-4a3536c85d3d', NULL, '{"acct_status_type":"start","seed_generated_012":true}'::jsonb,             zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '4 hours'),
    (pri_tenant, admin_user, 'session.policy_applied',       'session', '7f975eec-bf81-4096-a429-4a3536c85d3d', NULL, '{"policy_version_id":"05100000-0000-0000-0000-000000000001","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '4 hours'),
    (pri_tenant, admin_user, 'session.terminate',            'session', '7f975eec-bf81-4096-a429-4a3536c85d3d', NULL, '{"acct_status_type":"stop","terminate_cause":"User-Request","seed_generated_012":true}'::jsonb, zero_hash, zero_hash, gen_random_uuid(), '10.0.0.10'::inet, NOW() - INTERVAL '3 hours');
  END IF;

  PERFORM set_config('session_replication_role', 'origin', true);
END $$;

-- ============================================================
-- 3) sor_decision JSONB on DEMO sessions
--    UI reads .sor_decision.scoring[] + .sor_decision.chosen_operator_id
-- ============================================================
UPDATE sessions SET sor_decision = jsonb_build_object(
    'engine', 'cost_optimizer_v1',
    'evaluated_at', (NOW() - INTERVAL '2 hours')::text,
    'chosen_operator_id', '20000000-0000-0000-0000-000000000001',
    'inputs', jsonb_build_object('apn','iot.demo','rat','LTE','tenant_priority','standard','sim_iccid','89900100000000002009'),
    'scoring', jsonb_build_array(
      jsonb_build_object('operator_id','20000000-0000-0000-0000-000000000001','score',0.94,'reason','Lowest cost + healthy'),
      jsonb_build_object('operator_id','20000000-0000-0000-0000-000000000002','score',0.78,'reason','Higher latency, fallback'),
      jsonb_build_object('operator_id','20000000-0000-0000-0000-000000000003','score',0.51,'reason','Capacity constrained')
    )
  )
WHERE id='431c84f7-2249-4a12-b9d0-d68b3b9f0080' AND sor_decision IS NULL;

UPDATE sessions SET sor_decision = jsonb_build_object(
    'engine', 'cost_optimizer_v1',
    'evaluated_at', (NOW() - INTERVAL '3 hours')::text,
    'chosen_operator_id', '20000000-0000-0000-0000-000000000001',
    'inputs', jsonb_build_object('apn','m2m.demo','rat','LTE','tenant_priority','high'),
    'scoring', jsonb_build_array(
      jsonb_build_object('operator_id','20000000-0000-0000-0000-000000000001','score',0.91,'reason','Active S6a session, low latency'),
      jsonb_build_object('operator_id','20000000-0000-0000-0000-000000000002','score',0.72,'reason','Failover candidate')
    )
  )
WHERE id='a33746d0-d21f-4b67-b724-7e7ef3bf49dd' AND sor_decision IS NULL;

UPDATE sessions SET sor_decision = jsonb_build_object(
    'engine', 'cost_optimizer_v1',
    'evaluated_at', (NOW() - INTERVAL '4 hours')::text,
    'chosen_operator_id', '20000000-0000-0000-0000-000000000002',
    'inputs', jsonb_build_object('apn','iot.demo','rat','NR','tenant_priority','standard','slice','eMBB'),
    'scoring', jsonb_build_array(
      jsonb_build_object('operator_id','20000000-0000-0000-0000-000000000002','score',0.88,'reason','5G SA available, slice-aware'),
      jsonb_build_object('operator_id','20000000-0000-0000-0000-000000000001','score',0.65,'reason','LTE fallback only'),
      jsonb_build_object('operator_id','20000000-0000-0000-0000-000000000003','score',0.42,'reason','No 5G SBA support')
    )
  )
WHERE id='7f975eec-bf81-4096-a429-4a3536c85d3d' AND sor_decision IS NULL;

-- Set policy_version_id on demo sessions so Policy tab is populated
UPDATE sessions SET policy_version_id='05100000-0000-0000-0000-000000000002' WHERE id='431c84f7-2249-4a12-b9d0-d68b3b9f0080' AND policy_version_id IS NULL;
UPDATE sessions SET policy_version_id='05100000-0000-0000-0000-000000000001' WHERE id='a33746d0-d21f-4b67-b724-7e7ef3bf49dd' AND policy_version_id IS NULL;
UPDATE sessions SET policy_version_id='05100000-0000-0000-0000-000000000001' WHERE id='7f975eec-bf81-4096-a429-4a3536c85d3d' AND policy_version_id IS NULL;

-- ============================================================
-- 4) Policy violations — table was 0 rows; populate for demo SIMs
-- ============================================================
INSERT INTO policy_violations (id, tenant_id, sim_id, policy_id, version_id, rule_index, violation_type, action_taken, details, severity, created_at) VALUES
('20300000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001', '1c869918-9d62-41ba-a23e-a7492ef24e26', '05000000-0000-0000-0000-000000000002', '05100000-0000-0000-0000-000000000002', 0, 'data_quota_exceeded',  'throttle',   '{"threshold_mb":500,"observed_mb":612,"seed_generated_012":true}'::jsonb,         'medium', NOW() - INTERVAL '3 days'),
('20300000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000001', '1c869918-9d62-41ba-a23e-a7492ef24e26', '05000000-0000-0000-0000-000000000002', '05100000-0000-0000-0000-000000000002', 1, 'apn_mismatch',         'block',      '{"expected":"iot.demo","observed":"unknown.apn","seed_generated_012":true}'::jsonb, 'high',   NOW() - INTERVAL '5 days'),
('20300000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-000000000001', '92cd76d7-eb12-45bd-b373-5fb1fb64ff9f', '05000000-0000-0000-0000-000000000001', '05100000-0000-0000-0000-000000000001', 0, 'imei_mismatch',        'alert_only', '{"expected_imei":"354533080400094","observed_imei":"359225100023456","seed_generated_012":true}'::jsonb, 'high',   NOW() - INTERVAL '2 days'),
('20300000-0000-0000-0000-000000000004', '00000000-0000-0000-0000-000000000001', '92cd76d7-eb12-45bd-b373-5fb1fb64ff9f', '05000000-0000-0000-0000-000000000001', '05100000-0000-0000-0000-000000000001', 1, 'roaming_disallowed',   'block',      '{"observed_mcc":"262","observed_mnc":"01","seed_generated_012":true}'::jsonb,     'critical', NOW() - INTERVAL '4 days'),
('20300000-0000-0000-0000-000000000005', '00000000-0000-0000-0000-000000000001', '4af3d846-e31e-4ae0-be4c-81f3ee4b756e', '05000000-0000-0000-0000-000000000002', '05100000-0000-0000-0000-000000000002', 2, 'time_window_violation','alert_only', '{"window":"08:00-18:00","observed":"22:14","seed_generated_012":true}'::jsonb,    'low',    NOW() - INTERVAL '1 days')
ON CONFLICT (id) DO NOTHING;

-- ============================================================
-- 5) Alerts — demo operators + APNs (Audit/Alerts tabs on Detail screens)
-- Existing alerts table has 7 rows; add 8 more pinned to demo entities.
-- ============================================================
INSERT INTO alerts (id, tenant_id, type, severity, source, state, title, description, meta, sim_id, operator_id, apn_id, dedup_key, fired_at, first_seen_at, last_seen_at, occurrence_count) VALUES
('30300000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001', 'operator.health_degraded',       'high',     'operator', 'open',     'DEMO-OP-X latency degraded',                    'Average RADIUS auth latency exceeded 250ms threshold for >5 min', '{"latency_p95_ms":312,"threshold_ms":250,"seed_generated_012":true}'::jsonb,            NULL,                                   '20000000-0000-0000-0000-000000000001', NULL,                                  'op-x-degraded-2026-05-01', NOW() - INTERVAL '3 days', NOW() - INTERVAL '3 days', NOW() - INTERVAL '2 hours', 4),
('30300000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000001', 'operator.circuit_breaker_open',  'critical', 'operator', 'resolved', 'DEMO-OP-X circuit breaker opened',              '5 consecutive failures observed; circuit opened, traffic redirected', '{"failure_count":5,"recovered_after_sec":120,"seed_generated_012":true}'::jsonb,        NULL,                                   '20000000-0000-0000-0000-000000000001', NULL,                                  'op-x-cb-open-2026-04-26', NOW() - INTERVAL '8 days', NOW() - INTERVAL '8 days', NOW() - INTERVAL '8 days', 1),
('30300000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-000000000001', 'operator.sla_breach',            'medium',   'operator', 'open',     'DEMO-OP-Y monthly SLA at risk',                 'Uptime trending below 99.95% target for current month',          '{"uptime_pct":99.91,"target_pct":99.95,"seed_generated_012":true}'::jsonb,              NULL,                                   '20000000-0000-0000-0000-000000000002', NULL,                                  'op-y-sla-202605', NOW() - INTERVAL '1 days', NOW() - INTERVAL '5 days', NOW() - INTERVAL '6 hours', 12),
('30300000-0000-0000-0000-000000000004', '00000000-0000-0000-0000-000000000001', 'operator.config_drift',          'low',      'operator', 'open',     'DEMO-OP-Y adapter config drift detected',       'Adapter config differs from declared baseline (3 fields)',       '{"diff_field_count":3,"seed_generated_012":true}'::jsonb,                              NULL,                                   '20000000-0000-0000-0000-000000000002', NULL,                                  'op-y-drift-2026-05-04', NOW() - INTERVAL '1 days', NOW() - INTERVAL '1 days', NOW() - INTERVAL '1 days', 1),
('30300000-0000-0000-0000-000000000005', '00000000-0000-0000-0000-000000000001', 'apn.high_traffic',               'medium',   'infra',    'open',     'DEMO-APN-IOT traffic spike',                    'Traffic on iot.demo +180% vs 7-day average',                     '{"current_gbps":4.2,"baseline_gbps":1.5,"seed_generated_012":true}'::jsonb,            NULL,                                   NULL,                                  '06000000-0000-0000-0000-000000000001', 'apn-iot-spike-2026-05-04', NOW() - INTERVAL '1 days', NOW() - INTERVAL '1 days', NOW() - INTERVAL '2 hours', 3),
('30300000-0000-0000-0000-000000000006', '00000000-0000-0000-0000-000000000001', 'apn.policy_violation',           'high',     'policy',   'open',     'DEMO-APN-IOT policy violation rate elevated',   '5 violations in last 24h; threshold is 2',                       '{"violations_24h":5,"threshold":2,"seed_generated_012":true}'::jsonb,                  NULL,                                   NULL,                                  '06000000-0000-0000-0000-000000000001', 'apn-iot-violations-2026-05-05', NOW() - INTERVAL '12 hours', NOW() - INTERVAL '12 hours', NOW() - INTERVAL '1 hours', 5),
('30300000-0000-0000-0000-000000000007', '00000000-0000-0000-0000-000000000001', 'apn.ip_pool_exhaustion_warning', 'high',     'infra',    'open',     'DEMO-APN-M2M IP pool 80% full',                 'Demo M2M Pool utilization at 82%, allocate more or expand',      '{"pool_id":"07000000-0000-0000-0000-000000000002","utilization_pct":82,"seed_generated_012":true}'::jsonb, NULL,                                   NULL,                                  '06000000-0000-0000-0000-000000000002', 'apn-m2m-pool-warning-2026-05-04', NOW() - INTERVAL '1 days', NOW() - INTERVAL '2 days', NOW() - INTERVAL '4 hours', 7),
('30300000-0000-0000-0000-000000000008', '00000000-0000-0000-0000-000000000001', 'sim.binding_mismatch',           'high',     'sim',      'open',     'DEMO-SIM-B IMEI mismatch detected',             'Observed IMEI does not match bound IMEI on SIM 92cd76d7',         '{"bound_imei":"354533080400094","observed_imei":"359225100023456","seed_generated_012":true}'::jsonb, '92cd76d7-eb12-45bd-b373-5fb1fb64ff9f', '20000000-0000-0000-0000-000000000001', '06000000-0000-0000-0000-000000000001', 'sim-b-mismatch-2026-05-03', NOW() - INTERVAL '2 days', NOW() - INTERVAL '2 days', NOW() - INTERVAL '6 hours', 9)
ON CONFLICT (id) DO NOTHING;

COMMIT;

-- End of SEED-12

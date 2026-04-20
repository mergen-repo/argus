-- SEED-05: Realistic Multi-Operator Test Data (STORY-080)
--
-- Idempotent: every statement uses ON CONFLICT DO NOTHING / WHERE-guarded
-- UPDATE so running this seed multiple times produces zero row changes.
--
-- Shared secrets below are TEST-ONLY and used both for operator.adapter_config
-- and for simulator (cmd/simulator, STORY-082) client-side signing. Never
-- use these values in production.

BEGIN;

-- ─────────────────────────────────────────────────────────────
-- TENANTS — rename existing demo to XYZ Edaş, add ABC Edaş
-- ─────────────────────────────────────────────────────────────

UPDATE tenants
SET name = 'XYZ Edaş',
    domain = 'xyz.local',
    contact_email = 'admin@xyz.local',
    updated_at = NOW()
WHERE id = '00000000-0000-0000-0000-000000000001'
  AND name NOT IN ('XYZ Edaş');

INSERT INTO tenants (id, name, domain, contact_email, max_sims, max_apns, max_users, state)
VALUES (
    '00000000-0000-0000-0000-000000000002',
    'ABC Edaş',
    'abc.local',
    'admin@abc.local',
    100000, 100, 50,
    'active'
) ON CONFLICT DO NOTHING;

-- ─────────────────────────────────────────────────────────────
-- OPERATORS — Turkcell, Vodafone TR, Türk Telekom
-- STORY-090 Wave 2 D2-B: adapter_type column dropped; adapter_config
-- carries the nested {"radius":{...},"mock":{...}} shape.
-- STORY-090 Gate (F-A6): canonical `radius` sub-key with
-- shared_secret/listen_addr/host/port so the RADIUS adapter factory
-- consumes the config directly. `mock` sibling with enabled=true keeps
-- the simulator's RADIUS-style secret lookup path working.
-- STORY-089 (2026-04-18): added `http` sub-key per operator pointing at
-- `argus-operator-sim:9595/<operator_code>`. Simulator exposes GET /health,
-- GET /subscribers/:imsi, POST /cdr under each operator prefix. Only
-- turkcell/vodafone/turk_telekom get http enabled; the mock operator
-- (seed 002) does not participate in http routing.
-- ─────────────────────────────────────────────────────────────

INSERT INTO operators (id, name, code, mcc, mnc, adapter_config, supported_rat_types, health_status, state)
VALUES
    (
        '20000000-0000-0000-0000-000000000001',
        'Turkcell',
        'turkcell',
        '286', '01',
        '{"radius":{"enabled":true,"shared_secret":"sim-turkcell-secret-32-chars-long","listen_addr":":1812","host":"radius.turkcell.sim.local","port":1812,"timeout_ms":3000},"http":{"enabled":true,"base_url":"http://argus-operator-sim:9595/turkcell","health_path":"/health","timeout_ms":2000},"mock":{"enabled":true,"latency_ms":5,"simulated_imsi_count":1000}}',
        ARRAY['nb_iot', 'lte_m', 'lte', 'nr_5g'],
        'healthy',
        'active'
    ),
    (
        '20000000-0000-0000-0000-000000000002',
        'Vodafone TR',
        'vodafone_tr',
        '286', '02',
        '{"radius":{"enabled":true,"shared_secret":"sim-vodafone-secret-32-chars-long","listen_addr":":1812","host":"radius.vodafone.sim.local","port":1812,"timeout_ms":3000},"http":{"enabled":true,"base_url":"http://argus-operator-sim:9595/vodafone","health_path":"/health","timeout_ms":2000},"mock":{"enabled":true,"latency_ms":5,"simulated_imsi_count":1000}}',
        ARRAY['nb_iot', 'lte_m', 'lte', 'nr_5g'],
        'healthy',
        'active'
    ),
    (
        '20000000-0000-0000-0000-000000000003',
        'Türk Telekom',
        'turk_telekom',
        '286', '03',
        '{"radius":{"enabled":true,"shared_secret":"sim-tt-secret-0000000000-32chars","listen_addr":":1812","host":"radius.turktelekom.sim.local","port":1812,"timeout_ms":3000},"http":{"enabled":true,"base_url":"http://argus-operator-sim:9595/turk_telekom","health_path":"/health","timeout_ms":2000},"mock":{"enabled":true,"latency_ms":5,"simulated_imsi_count":500}}',
        ARRAY['nb_iot', 'lte_m', 'lte'],
        'healthy',
        'active'
    )
ON CONFLICT (code) DO UPDATE SET
    adapter_config = EXCLUDED.adapter_config,
    supported_rat_types = EXCLUDED.supported_rat_types;

-- ─────────────────────────────────────────────────────────────
-- SIM PARTITIONS — MUST exist before any INSERT INTO sims for
-- these operator_ids (sims is LIST-partitioned by operator_id).
-- ─────────────────────────────────────────────────────────────

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_class WHERE relname = 'sims_turkcell') THEN
        EXECUTE format(
            'CREATE TABLE sims_turkcell PARTITION OF sims FOR VALUES IN (%L)',
            '20000000-0000-0000-0000-000000000001'
        );
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_class WHERE relname = 'sims_vodafone') THEN
        EXECUTE format(
            'CREATE TABLE sims_vodafone PARTITION OF sims FOR VALUES IN (%L)',
            '20000000-0000-0000-0000-000000000002'
        );
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_class WHERE relname = 'sims_turk_telekom') THEN
        EXECUTE format(
            'CREATE TABLE sims_turk_telekom PARTITION OF sims FOR VALUES IN (%L)',
            '20000000-0000-0000-0000-000000000003'
        );
    END IF;
END
$$;

-- ─────────────────────────────────────────────────────────────
-- OPERATOR GRANTS — both tenants × all 3 real operators
-- ─────────────────────────────────────────────────────────────

INSERT INTO operator_grants (id, tenant_id, operator_id, enabled) VALUES
    -- XYZ Edaş
    ('00000000-0000-0000-0000-000000000201', '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', true),
    ('00000000-0000-0000-0000-000000000202', '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', true),
    ('00000000-0000-0000-0000-000000000203', '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000003', true),
    -- ABC Edaş
    ('00000000-0000-0000-0000-000000000211', '00000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001', true),
    ('00000000-0000-0000-0000-000000000212', '00000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000002', true),
    ('00000000-0000-0000-0000-000000000213', '00000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000003', true)
ON CONFLICT DO NOTHING;

-- ─────────────────────────────────────────────────────────────
-- APNs — 3 per tenant, each bound to a specific operator
-- (operator_id is NOT NULL on apns). We pick one "primary"
-- operator per APN; SIMs can still be attached regardless
-- because SIM.operator_id is independent of APN.operator_id.
-- ─────────────────────────────────────────────────────────────

INSERT INTO apns (id, tenant_id, operator_id, name, display_name, apn_type, supported_rat_types, state) VALUES
    -- XYZ Edaş APNs
    ('00000000-0000-0000-0000-000000000301', '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', 'iot.xyz.local',     'XYZ IoT',     'iot', ARRAY['nb_iot','lte_m','lte'],        'active'),
    ('00000000-0000-0000-0000-000000000302', '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', 'm2m.xyz.local',     'XYZ M2M',     'm2m', ARRAY['lte','lte_m','nr_5g'],         'active'),
    ('00000000-0000-0000-0000-000000000303', '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000003', 'private.xyz.local', 'XYZ Private', 'private', ARRAY['lte','lte_m'],             'active'),
    -- ABC Edaş APNs
    ('00000000-0000-0000-0000-000000000311', '00000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001', 'iot.abc.local',     'ABC IoT',     'iot', ARRAY['nb_iot','lte_m','lte'],        'active'),
    ('00000000-0000-0000-0000-000000000312', '00000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000002', 'm2m.abc.local',     'ABC M2M',     'm2m', ARRAY['lte','lte_m','nr_5g'],         'active'),
    ('00000000-0000-0000-0000-000000000313', '00000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000003', 'private.abc.local', 'ABC Private', 'private', ARRAY['lte','lte_m'],             'active')
ON CONFLICT DO NOTHING;

-- ─────────────────────────────────────────────────────────────
-- IP POOLS — 2 per tenant (iot + m2m)
-- ─────────────────────────────────────────────────────────────

INSERT INTO ip_pools (id, tenant_id, apn_id, name, cidr_v4, total_addresses, used_addresses, state) VALUES
    -- XYZ
    ('00000000-0000-0000-0000-000000000401', '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000301', 'XYZ-IoT-Pool', '10.100.0.0/22'::cidr, 1024, 0, 'active'),
    ('00000000-0000-0000-0000-000000000402', '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000302', 'XYZ-M2M-Pool', '10.100.4.0/22'::cidr, 1024, 0, 'active'),
    -- ABC
    ('00000000-0000-0000-0000-000000000411', '00000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000311', 'ABC-IoT-Pool', '10.200.0.0/22'::cidr, 1024, 0, 'active'),
    ('00000000-0000-0000-0000-000000000412', '00000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000312', 'ABC-M2M-Pool', '10.200.4.0/22'::cidr, 1024, 0, 'active')
ON CONFLICT DO NOTHING;

-- ─────────────────────────────────────────────────────────────
-- POLICIES + VERSIONS — 2 per tenant
-- ─────────────────────────────────────────────────────────────

INSERT INTO policies (id, tenant_id, name, description, scope, state, created_by) VALUES
    -- XYZ
    ('00000000-0000-0000-0000-000000000501', '00000000-0000-0000-0000-000000000001', 'XYZ Data Cap',        '5GB monthly cap for XYZ SIMs',                 'tenant', 'active', '00000000-0000-0000-0000-000000000010'),
    ('00000000-0000-0000-0000-000000000502', '00000000-0000-0000-0000-000000000001', 'XYZ Business Hours',  'Allow access weekdays 09:00–18:00 local time', 'tenant', 'active', '00000000-0000-0000-0000-000000000010'),
    -- ABC
    ('00000000-0000-0000-0000-000000000511', '00000000-0000-0000-0000-000000000002', 'ABC Data Cap',        '10GB monthly cap for ABC SIMs',                'tenant', 'active', '00000000-0000-0000-0000-000000000010'),
    ('00000000-0000-0000-0000-000000000512', '00000000-0000-0000-0000-000000000002', 'ABC Roaming Block',   'Deny sessions from operators not granted',    'tenant', 'active', '00000000-0000-0000-0000-000000000010')
ON CONFLICT DO NOTHING;

INSERT INTO policy_versions (id, policy_id, version, dsl_content, compiled_rules, state, activated_at, created_by) VALUES
    ('00000000-0000-0000-0000-000000000601', '00000000-0000-0000-0000-000000000501', 1,
        'IF apn IN ("iot.xyz.local","m2m.xyz.local") AND monthly_bytes > 5368709120 THEN reject',
        '{"rules":[{"when":{"apn_in":["iot.xyz.local","m2m.xyz.local"],"monthly_bytes_gt":5368709120},"then":{"action":"reject"}}]}',
        'active', NOW(), '00000000-0000-0000-0000-000000000010'),
    ('00000000-0000-0000-0000-000000000602', '00000000-0000-0000-0000-000000000502', 1,
        'IF time_of_day NOT IN ("09:00-18:00") THEN reject',
        '{"rules":[{"when":{"time_not_in":["09:00-18:00"]},"then":{"action":"reject"}}]}',
        'active', NOW(), '00000000-0000-0000-0000-000000000010'),
    ('00000000-0000-0000-0000-000000000611', '00000000-0000-0000-0000-000000000511', 1,
        'IF apn IN ("iot.abc.local","m2m.abc.local") AND monthly_bytes > 10737418240 THEN reject',
        '{"rules":[{"when":{"apn_in":["iot.abc.local","m2m.abc.local"],"monthly_bytes_gt":10737418240},"then":{"action":"reject"}}]}',
        'active', NOW(), '00000000-0000-0000-0000-000000000010'),
    ('00000000-0000-0000-0000-000000000612', '00000000-0000-0000-0000-000000000512', 1,
        'IF operator NOT IN (granted_operators) THEN reject',
        '{"rules":[{"when":{"operator_not_granted":true},"then":{"action":"reject"}}]}',
        'active', NOW(), '00000000-0000-0000-0000-000000000010')
ON CONFLICT DO NOTHING;

-- ─────────────────────────────────────────────────────────────
-- SIMs — 16 total, 8 per tenant, round-robin across operators
--
-- IMSI format: <mcc><mnc>0<tenant_digit><seq>  (10 digits)
--   Turkcell tenant-1 seq-1 → 2860100001
--   Vodafone tenant-2 seq-4 → 2860200024
-- ICCID: 8990 + 15-digit IMSI right-padded (zeros) to 22 chars
-- MSISDN: +905XX where XX varies per tenant/operator
-- ─────────────────────────────────────────────────────────────

INSERT INTO sims (id, tenant_id, operator_id, apn_id, iccid, imsi, msisdn, sim_type, state, rat_type, activated_at) VALUES
    -- XYZ Edaş (tenant_digit=1) — 8 SIMs: 3 Turkcell + 3 Vodafone + 2 Türk Telekom
    ('00000000-0000-0000-0000-000000000701', '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000301', '8990286010000100001001', '2860100001', '+905310000001', 'physical', 'active', 'lte',    NOW()),
    ('00000000-0000-0000-0000-000000000702', '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000301', '8990286010000100002001', '2860100002', '+905310000002', 'physical', 'active', 'nb_iot', NOW()),
    ('00000000-0000-0000-0000-000000000703', '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000301', '8990286010000100003001', '2860100003', '+905310000003', 'physical', 'active', 'lte_m',  NOW()),
    ('00000000-0000-0000-0000-000000000704', '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000302', '8990286020000100004001', '2860200001', '+905320000001', 'physical', 'active', 'lte',    NOW()),
    ('00000000-0000-0000-0000-000000000705', '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000302', '8990286020000100005001', '2860200002', '+905320000002', 'physical', 'active', 'lte',    NOW()),
    ('00000000-0000-0000-0000-000000000706', '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000302', '8990286020000100006001', '2860200003', '+905320000003', 'physical', 'active', 'nr_5g',  NOW()),
    ('00000000-0000-0000-0000-000000000707', '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-000000000303', '8990286030000100007001', '2860300001', '+905330000001', 'physical', 'active', 'lte',    NOW()),
    ('00000000-0000-0000-0000-000000000708', '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-000000000303', '8990286030000100008001', '2860300002', '+905330000002', 'physical', 'active', 'lte_m',  NOW()),
    -- ABC Edaş (tenant_digit=2) — 8 SIMs: 3 Turkcell + 3 Vodafone + 2 Türk Telekom
    ('00000000-0000-0000-0000-000000000711', '00000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000311', '8990286010000200001001', '2860100021', '+905410000001', 'physical', 'active', 'lte',    NOW()),
    ('00000000-0000-0000-0000-000000000712', '00000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000311', '8990286010000200002001', '2860100022', '+905410000002', 'physical', 'active', 'nb_iot', NOW()),
    ('00000000-0000-0000-0000-000000000713', '00000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000311', '8990286010000200003001', '2860100023', '+905410000003', 'physical', 'active', 'lte_m',  NOW()),
    ('00000000-0000-0000-0000-000000000714', '00000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000312', '8990286020000200004001', '2860200021', '+905420000001', 'physical', 'active', 'lte',    NOW()),
    ('00000000-0000-0000-0000-000000000715', '00000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000312', '8990286020000200005001', '2860200022', '+905420000002', 'physical', 'active', 'lte',    NOW()),
    ('00000000-0000-0000-0000-000000000716', '00000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000312', '8990286020000200006001', '2860200023', '+905420000003', 'physical', 'active', 'nr_5g',  NOW()),
    ('00000000-0000-0000-0000-000000000717', '00000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-000000000313', '8990286030000200007001', '2860300021', '+905430000001', 'physical', 'active', 'lte',    NOW()),
    ('00000000-0000-0000-0000-000000000718', '00000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-000000000313', '8990286030000200008001', '2860300022', '+905430000002', 'physical', 'active', 'lte_m',  NOW())
ON CONFLICT (id, operator_id) DO NOTHING;

-- ─────────────────────────────────────────────────────────────
-- SIMULATOR READ-ONLY DB ROLE (for STORY-082)
-- Grants SELECT on the minimum set needed by simulator discovery.
-- ─────────────────────────────────────────────────────────────

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'argus_sim') THEN
        CREATE ROLE argus_sim LOGIN PASSWORD 'sim_ro_pass';
    END IF;
END$$;

-- BYPASSRLS is required: STORY-064 added ENABLE + FORCE ROW LEVEL SECURITY
-- on sims, apns, and several other tables. A normal read role cannot see
-- rows unless `app.current_tenant` is set per-session. Since the simulator
-- is a cross-tenant read-only dev tool (needs to discover SIMs across all
-- tenants), BYPASSRLS is the pragmatic choice. Still read-only at the grant
-- level; cannot modify anything.
ALTER ROLE argus_sim BYPASSRLS;

GRANT CONNECT ON DATABASE argus TO argus_sim;
GRANT USAGE ON SCHEMA public TO argus_sim;
GRANT SELECT ON tenants, operators, apns, sims, operator_grants, ip_pools TO argus_sim;
-- Explicitly deny write access at role level — schema-level GRANT above
-- never included INSERT/UPDATE/DELETE, but we make this explicit for audit.
REVOKE INSERT, UPDATE, DELETE, TRUNCATE ON tenants, operators, apns, sims, operator_grants, ip_pools FROM argus_sim;

COMMIT;

-- Verification queries (commented — run manually after seed):
-- SELECT COUNT(*) FROM tenants;                                                     -- expect 2
-- SELECT COUNT(*) FROM operators;                                                    -- expect 4 (mock + 3 real)
-- SELECT COUNT(*) FROM sims;                                                         -- expect 16
-- SELECT COUNT(*) FROM apns;                                                         -- expect 6
-- SELECT COUNT(*) FROM ip_pools;                                                     -- expect 4
-- SELECT COUNT(*) FROM policies;                                                     -- expect 4
-- SELECT COUNT(*) FROM policy_versions WHERE state='active';                         -- expect 4
-- SELECT relname FROM pg_class WHERE relname LIKE 'sims_%' ORDER BY relname;        -- expect sims_mock, sims_turk_telekom, sims_turkcell, sims_vodafone
-- SELECT rolname FROM pg_roles WHERE rolname='argus_sim';                            -- expect 1 row

-- SEED-08: Scale SIM fleet to ~200 total (100 per tenant) (STORY-082 follow-up)
--
-- Extends the test infra from 16 SIMs to ~200 so the Live Event Stream
-- carries real per-subscriber signal volume (dozens of auths/minute,
-- hundreds of interim updates/minute).
--
-- Idempotent: SIM inserts use ON CONFLICT (imsi, operator_id) DO NOTHING;
-- IP pool capacity expansion guarded by WHERE NOT EXISTS.
--
-- Distribution per tenant (matches seed 005 pattern):
--   40 Turkcell (APN = iot.<tenant>.local)
--   40 Vodafone (APN = m2m.<tenant>.local)
--   20 Türk Telekom (APN = private.<tenant>.local)
-- Total: 100 per tenant × 2 tenants = 200 SIMs.
--
-- IMSI format: <mcc:3><mnc:2>0<tenant_digit:1><seq:3> (10 digits).
-- ICCID format: 8990<mcc><mnc>0000<tenant_digit>0<seq:3>XXXX (22 digits).
-- MSISDN format: +905<op>0<tenant><seq:4>.

BEGIN;

-- ─────────────────────────────────────────────────────────────
-- Expand ip_addresses to 200 per pool (was 50 in seed 006). Idempotent.
-- ─────────────────────────────────────────────────────────────

INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '00000000-0000-0000-0000-000000000401', ('10.100.0.' || g)::inet, 'dynamic', 'available'
FROM generate_series(1, 200) AS g
WHERE NOT EXISTS (
    SELECT 1 FROM ip_addresses WHERE pool_id = '00000000-0000-0000-0000-000000000401' AND address_v4 = ('10.100.0.' || g)::inet
);

INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '00000000-0000-0000-0000-000000000402', ('10.100.4.' || g)::inet, 'dynamic', 'available'
FROM generate_series(1, 200) AS g
WHERE NOT EXISTS (
    SELECT 1 FROM ip_addresses WHERE pool_id = '00000000-0000-0000-0000-000000000402' AND address_v4 = ('10.100.4.' || g)::inet
);

INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '00000000-0000-0000-0000-000000000403', ('10.100.8.' || g)::inet, 'dynamic', 'available'
FROM generate_series(1, 200) AS g
WHERE NOT EXISTS (
    SELECT 1 FROM ip_addresses WHERE pool_id = '00000000-0000-0000-0000-000000000403' AND address_v4 = ('10.100.8.' || g)::inet
);

INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '00000000-0000-0000-0000-000000000411', ('10.200.0.' || g)::inet, 'dynamic', 'available'
FROM generate_series(1, 200) AS g
WHERE NOT EXISTS (
    SELECT 1 FROM ip_addresses WHERE pool_id = '00000000-0000-0000-0000-000000000411' AND address_v4 = ('10.200.0.' || g)::inet
);

INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '00000000-0000-0000-0000-000000000412', ('10.200.4.' || g)::inet, 'dynamic', 'available'
FROM generate_series(1, 200) AS g
WHERE NOT EXISTS (
    SELECT 1 FROM ip_addresses WHERE pool_id = '00000000-0000-0000-0000-000000000412' AND address_v4 = ('10.200.4.' || g)::inet
);

INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '00000000-0000-0000-0000-000000000413', ('10.200.8.' || g)::inet, 'dynamic', 'available'
FROM generate_series(1, 200) AS g
WHERE NOT EXISTS (
    SELECT 1 FROM ip_addresses WHERE pool_id = '00000000-0000-0000-0000-000000000413' AND address_v4 = ('10.200.8.' || g)::inet
);

-- ─────────────────────────────────────────────────────────────
-- Generate SIMs via generate_series. Start seq at 100 to avoid colliding
-- with seed 005's 001-003/021-023 range (room for future ranges below).
-- ─────────────────────────────────────────────────────────────

-- Helper macros (inline PL/pgSQL): one insert per tenant × operator group.

-- XYZ Edaş × Turkcell — 40 SIMs (seq 100..139)
INSERT INTO sims (tenant_id, operator_id, apn_id, iccid, imsi, msisdn, sim_type, state, rat_type, activated_at)
SELECT
    '00000000-0000-0000-0000-000000000001',
    '00000000-0000-0000-0000-000000000101',
    '00000000-0000-0000-0000-000000000301',
    '8990286010000100' || lpad(g::text, 3, '0') || '001',
    '286010100' || lpad(g::text, 3, '0'),
    '+90531' || lpad((100 + g - 100)::text, 7, '0'),
    'physical', 'active',
    CASE (g % 3) WHEN 0 THEN 'lte' WHEN 1 THEN 'nb_iot' ELSE 'lte_m' END,
    NOW()
FROM generate_series(100, 139) AS g
ON CONFLICT (imsi, operator_id) DO NOTHING;

-- XYZ × Vodafone — 40 SIMs
INSERT INTO sims (tenant_id, operator_id, apn_id, iccid, imsi, msisdn, sim_type, state, rat_type, activated_at)
SELECT
    '00000000-0000-0000-0000-000000000001',
    '00000000-0000-0000-0000-000000000102',
    '00000000-0000-0000-0000-000000000302',
    '8990286020000100' || lpad(g::text, 3, '0') || '001',
    '286020100' || lpad(g::text, 3, '0'),
    '+90532' || lpad((g - 100)::text, 7, '0'),
    'physical', 'active',
    CASE (g % 3) WHEN 0 THEN 'lte' WHEN 1 THEN 'nr_5g' ELSE 'lte_m' END,
    NOW()
FROM generate_series(100, 139) AS g
ON CONFLICT (imsi, operator_id) DO NOTHING;

-- XYZ × Türk Telekom — 20 SIMs
INSERT INTO sims (tenant_id, operator_id, apn_id, iccid, imsi, msisdn, sim_type, state, rat_type, activated_at)
SELECT
    '00000000-0000-0000-0000-000000000001',
    '00000000-0000-0000-0000-000000000103',
    '00000000-0000-0000-0000-000000000303',
    '8990286030000100' || lpad(g::text, 3, '0') || '001',
    '286030100' || lpad(g::text, 3, '0'),
    '+90533' || lpad((g - 100)::text, 7, '0'),
    'physical', 'active',
    CASE (g % 2) WHEN 0 THEN 'lte' ELSE 'lte_m' END,
    NOW()
FROM generate_series(100, 119) AS g
ON CONFLICT (imsi, operator_id) DO NOTHING;

-- ABC Edaş × Turkcell — 40 SIMs
INSERT INTO sims (tenant_id, operator_id, apn_id, iccid, imsi, msisdn, sim_type, state, rat_type, activated_at)
SELECT
    '00000000-0000-0000-0000-000000000002',
    '00000000-0000-0000-0000-000000000101',
    '00000000-0000-0000-0000-000000000311',
    '8990286010000200' || lpad(g::text, 3, '0') || '001',
    '286010200' || lpad(g::text, 3, '0'),
    '+90541' || lpad((g - 100)::text, 7, '0'),
    'physical', 'active',
    CASE (g % 3) WHEN 0 THEN 'lte' WHEN 1 THEN 'nb_iot' ELSE 'lte_m' END,
    NOW()
FROM generate_series(100, 139) AS g
ON CONFLICT (imsi, operator_id) DO NOTHING;

-- ABC × Vodafone — 40 SIMs
INSERT INTO sims (tenant_id, operator_id, apn_id, iccid, imsi, msisdn, sim_type, state, rat_type, activated_at)
SELECT
    '00000000-0000-0000-0000-000000000002',
    '00000000-0000-0000-0000-000000000102',
    '00000000-0000-0000-0000-000000000312',
    '8990286020000200' || lpad(g::text, 3, '0') || '001',
    '286020200' || lpad(g::text, 3, '0'),
    '+90542' || lpad((g - 100)::text, 7, '0'),
    'physical', 'active',
    CASE (g % 3) WHEN 0 THEN 'lte' WHEN 1 THEN 'nr_5g' ELSE 'lte_m' END,
    NOW()
FROM generate_series(100, 139) AS g
ON CONFLICT (imsi, operator_id) DO NOTHING;

-- ABC × Türk Telekom — 20 SIMs
INSERT INTO sims (tenant_id, operator_id, apn_id, iccid, imsi, msisdn, sim_type, state, rat_type, activated_at)
SELECT
    '00000000-0000-0000-0000-000000000002',
    '00000000-0000-0000-0000-000000000103',
    '00000000-0000-0000-0000-000000000313',
    '8990286030000200' || lpad(g::text, 3, '0') || '001',
    '286030200' || lpad(g::text, 3, '0'),
    '+90543' || lpad((g - 100)::text, 7, '0'),
    'physical', 'active',
    CASE (g % 2) WHEN 0 THEN 'lte' ELSE 'lte_m' END,
    NOW()
FROM generate_series(100, 119) AS g
ON CONFLICT (imsi, operator_id) DO NOTHING;

-- ─────────────────────────────────────────────────────────────
-- Reserve IPs for any SIM still without one (re-uses seed 006 logic).
-- Idempotent — SIMs already pointing at an ip_address_id are skipped.
-- ─────────────────────────────────────────────────────────────

WITH
candidates AS (
    SELECT s.id AS sim_id, s.tenant_id, s.operator_id, s.apn_id,
           ROW_NUMBER() OVER (PARTITION BY s.apn_id ORDER BY s.imsi) AS rn
    FROM sims s
    WHERE s.ip_address_id IS NULL
      AND s.apn_id IS NOT NULL
      AND s.state = 'active'
),
pool_ips AS (
    SELECT ipa.id AS ip_id, ipa.pool_id, p.apn_id,
           ROW_NUMBER() OVER (PARTITION BY p.apn_id ORDER BY ipa.address_v4) AS rn
    FROM ip_addresses ipa
    JOIN ip_pools p ON p.id = ipa.pool_id
    WHERE ipa.state = 'available' AND ipa.sim_id IS NULL
),
pairs AS (
    SELECT c.sim_id, c.operator_id, p.ip_id
    FROM candidates c
    JOIN pool_ips p ON p.apn_id = c.apn_id AND p.rn = c.rn
),
reserved AS (
    UPDATE ip_addresses ipa
    SET state = 'reserved', allocation_type = 'static', sim_id = p.sim_id, allocated_at = NOW()
    FROM pairs p
    WHERE ipa.id = p.ip_id
    RETURNING ipa.id AS ip_id, p.sim_id, p.operator_id
)
UPDATE sims s
SET ip_address_id = r.ip_id, updated_at = NOW()
FROM reserved r
WHERE s.id = r.sim_id AND s.operator_id = r.operator_id;

-- Refresh pool usage counters.
UPDATE ip_pools p
SET used_addresses = sub.used_count
FROM (
    SELECT pool_id, COUNT(*) AS used_count
    FROM ip_addresses
    WHERE state IN ('allocated', 'reserved')
    GROUP BY pool_id
) sub
WHERE p.id = sub.pool_id;

-- Also ensure the seeded activation history exists for new SIMs
-- (mirrors seed 007 — "ordered → active manual_seed_test_activation").
INSERT INTO sim_state_history (sim_id, from_state, to_state, reason, triggered_by, user_id, created_at)
SELECT s.id, 'ordered', 'active', 'manual_seed_test_activation', 'user',
       '00000000-0000-0000-0000-000000000010',
       NOW() - INTERVAL '7 days'
FROM sims s
WHERE s.state = 'active'
  AND NOT EXISTS (
      SELECT 1 FROM sim_state_history h
      WHERE h.sim_id = s.id AND h.reason = 'manual_seed_test_activation'
  );

COMMIT;

-- Verification:
-- SELECT tenant_id, operator_id, COUNT(*) FROM sims WHERE state='active' GROUP BY 1,2 ORDER BY 1,2;
-- SELECT COUNT(*) FROM sims WHERE ip_address_id IS NOT NULL;
-- SELECT name, total_addresses, used_addresses FROM ip_pools ORDER BY name;

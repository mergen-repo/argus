-- SEED-06: Reserve a static IP per SIM from its APN's pool (STORY-082 follow-up)
--
-- Scope extended by STORY-092 (Wave 1, D1-A): also materialise ip_addresses
-- rows for seed 003's 13 tenant+demo pools so that any SIM arriving with an
-- APN but no preallocated ip_address_id can pick one dynamically (RADIUS
-- Access-Accept path + Diameter CCR-I). Without these rows, AllocateIP
-- returns ErrPoolExhausted against freshly-seeded dev/test databases even
-- though ip_pools.used_addresses records nonzero usage from the seed data.
--
-- Idempotent:
--   - ip_addresses rows use WHERE NOT EXISTS (pool_id, address_v4)
--     (the v4 unique index is partial-filtered WHERE address_v4 IS NOT NULL
--     and cannot be used in ON CONFLICT)
--   - SIM reservation block is guarded by sims.ip_address_id IS NULL
--   - ip_pools.used_addresses recount is deterministic from ip_addresses
--
-- Why: seed 005 creates 4 ip_pools (total_addresses=1024 each) but zero
-- actual ip_addresses rows; seed 003 creates 13 more pools with nonzero
-- used_addresses counters but also zero ip_addresses rows. RADIUS/Diameter
-- handlers only attach Framed-IP if a row can be located in ip_addresses.
-- This seed populates 50 addresses per pool (plenty for 16 SIMs + future
-- growth) and reserves one per SIM permanently
-- (state='reserved', allocation_type='static').

BEGIN;

-- ─────────────────────────────────────────────────────────────
-- Seed 005 created ip_pools only for iot+m2m APNs. Add pools for
-- `private.xyz.local` and `private.abc.local` so every SIM has a
-- pool to draw from. Idempotent via ON CONFLICT (id) DO NOTHING.
-- ─────────────────────────────────────────────────────────────

-- STORY-092 D1-A: seed 003 declares apn m2m.water but forgot to provision an
-- ip_pools row for it. Active SIMs on that APN would hit
-- ErrPoolExhausted forever. Add a pool here so the fail-fast verification
-- at the bottom of this seed can succeed on a seed-003-only database.
INSERT INTO ip_pools (id, tenant_id, apn_id, name, cidr_v4, total_addresses, used_addresses, state)
SELECT '70000000-0000-0000-0000-000000000015',
       '10000000-0000-0000-0000-000000000002',
       '60000000-0000-0000-0000-000000000015',
       'Water Pool', '10.14.0.0/24'::cidr, 254, 0, 'active'
WHERE EXISTS (SELECT 1 FROM apns WHERE id = '60000000-0000-0000-0000-000000000015')
ON CONFLICT (id) DO NOTHING;

-- Guard with EXISTS subqueries so that databases which only ran seed 003
-- (without seed 005) don't hit an FK violation. Without this guard, the
-- whole transaction aborts before any of the STORY-092 D1-A additions
-- below can execute.
INSERT INTO ip_pools (id, tenant_id, apn_id, name, cidr_v4, total_addresses, used_addresses, state)
SELECT '00000000-0000-0000-0000-000000000403', '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000303', 'XYZ-Private-Pool', '10.100.8.0/22'::cidr, 1024, 0, 'active'
WHERE EXISTS (SELECT 1 FROM apns WHERE id = '00000000-0000-0000-0000-000000000303')
ON CONFLICT (id) DO NOTHING;

INSERT INTO ip_pools (id, tenant_id, apn_id, name, cidr_v4, total_addresses, used_addresses, state)
SELECT '00000000-0000-0000-0000-000000000413', '00000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000313', 'ABC-Private-Pool', '10.200.8.0/22'::cidr, 1024, 0, 'active'
WHERE EXISTS (SELECT 1 FROM apns WHERE id = '00000000-0000-0000-0000-000000000313')
ON CONFLICT (id) DO NOTHING;

-- ─────────────────────────────────────────────────────────────
-- Populate ip_addresses from each pool's CIDR (first 50 usable IPs)
-- ─────────────────────────────────────────────────────────────

-- WHERE NOT EXISTS idempotency (the v4 unique index is partial-filtered
-- WHERE address_v4 IS NOT NULL and cannot be used in ON CONFLICT).

-- XYZ IoT pool: 10.100.0.0/22 → 10.100.0.1..50 (guarded by EXISTS — pool
-- only present on databases that ran seed 005).
INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '00000000-0000-0000-0000-000000000401',
       ('10.100.0.' || g)::inet,
       'dynamic', 'available'
FROM generate_series(1, 50) AS g
WHERE EXISTS (SELECT 1 FROM ip_pools WHERE id = '00000000-0000-0000-0000-000000000401')
  AND NOT EXISTS (
    SELECT 1 FROM ip_addresses
    WHERE pool_id = '00000000-0000-0000-0000-000000000401'
      AND address_v4 = ('10.100.0.' || g)::inet
);

-- XYZ M2M pool: 10.100.4.0/22 → 10.100.4.1..50 (guarded — see above).
INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '00000000-0000-0000-0000-000000000402',
       ('10.100.4.' || g)::inet,
       'dynamic', 'available'
FROM generate_series(1, 50) AS g
WHERE EXISTS (SELECT 1 FROM ip_pools WHERE id = '00000000-0000-0000-0000-000000000402')
  AND NOT EXISTS (
    SELECT 1 FROM ip_addresses
    WHERE pool_id = '00000000-0000-0000-0000-000000000402'
      AND address_v4 = ('10.100.4.' || g)::inet
);

-- ABC IoT pool: 10.200.0.0/22 → 10.200.0.1..50 (guarded — see above).
INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '00000000-0000-0000-0000-000000000411',
       ('10.200.0.' || g)::inet,
       'dynamic', 'available'
FROM generate_series(1, 50) AS g
WHERE EXISTS (SELECT 1 FROM ip_pools WHERE id = '00000000-0000-0000-0000-000000000411')
  AND NOT EXISTS (
    SELECT 1 FROM ip_addresses
    WHERE pool_id = '00000000-0000-0000-0000-000000000411'
      AND address_v4 = ('10.200.0.' || g)::inet
);

-- ABC M2M pool: 10.200.4.0/22 → 10.200.4.1..50 (guarded — see above).
INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '00000000-0000-0000-0000-000000000412',
       ('10.200.4.' || g)::inet,
       'dynamic', 'available'
FROM generate_series(1, 50) AS g
WHERE EXISTS (SELECT 1 FROM ip_pools WHERE id = '00000000-0000-0000-0000-000000000412')
  AND NOT EXISTS (
    SELECT 1 FROM ip_addresses
    WHERE pool_id = '00000000-0000-0000-0000-000000000412'
      AND address_v4 = ('10.200.4.' || g)::inet
);

-- XYZ Private pool: 10.100.8.0/22 → 10.100.8.1..50 (guarded — pool may not exist
-- on seed-003-only databases; see the XYZ/ABC pool INSERT guards above).
INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '00000000-0000-0000-0000-000000000403',
       ('10.100.8.' || g)::inet,
       'dynamic', 'available'
FROM generate_series(1, 50) AS g
WHERE EXISTS (SELECT 1 FROM ip_pools WHERE id = '00000000-0000-0000-0000-000000000403')
  AND NOT EXISTS (
    SELECT 1 FROM ip_addresses
    WHERE pool_id = '00000000-0000-0000-0000-000000000403'
      AND address_v4 = ('10.100.8.' || g)::inet
);

-- ABC Private pool: 10.200.8.0/22 → 10.200.8.1..50 (guarded — see above).
INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '00000000-0000-0000-0000-000000000413',
       ('10.200.8.' || g)::inet,
       'dynamic', 'available'
FROM generate_series(1, 50) AS g
WHERE EXISTS (SELECT 1 FROM ip_pools WHERE id = '00000000-0000-0000-0000-000000000413')
  AND NOT EXISTS (
    SELECT 1 FROM ip_addresses
    WHERE pool_id = '00000000-0000-0000-0000-000000000413'
      AND address_v4 = ('10.200.8.' || g)::inet
);

-- ─────────────────────────────────────────────────────────────
-- STORY-092 D1-A: materialise ip_addresses for seed 003's 13 pools.
-- Each pool below uses the same pattern: generate first 50 usable IPs
-- of its /24 (or smaller). All pool prefixes are ≥ /25 so 50 IPs fit.
-- ─────────────────────────────────────────────────────────────

-- Nar Teknoloji: Fleet IPv4 Pool (10.1.0.0/22)
INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '70000000-0000-0000-0000-000000000001',
       ('10.1.0.' || g)::inet,
       'dynamic', 'available'
FROM generate_series(1, 50) AS g
WHERE NOT EXISTS (
    SELECT 1 FROM ip_addresses
    WHERE pool_id = '70000000-0000-0000-0000-000000000001'
      AND address_v4 = ('10.1.0.' || g)::inet
);

-- Nar Teknoloji: Meter IPv4 Pool (10.2.0.0/24)
INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '70000000-0000-0000-0000-000000000002',
       ('10.2.0.' || g)::inet,
       'dynamic', 'available'
FROM generate_series(1, 50) AS g
WHERE NOT EXISTS (
    SELECT 1 FROM ip_addresses
    WHERE pool_id = '70000000-0000-0000-0000-000000000002'
      AND address_v4 = ('10.2.0.' || g)::inet
);

-- Nar Teknoloji: Sensor IPv4 Pool (10.3.0.0/23)
INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '70000000-0000-0000-0000-000000000003',
       ('10.3.0.' || g)::inet,
       'dynamic', 'available'
FROM generate_series(1, 50) AS g
WHERE NOT EXISTS (
    SELECT 1 FROM ip_addresses
    WHERE pool_id = '70000000-0000-0000-0000-000000000003'
      AND address_v4 = ('10.3.0.' || g)::inet
);

-- Nar Teknoloji: Camera IPv4 Pool (10.4.0.0/25)
INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '70000000-0000-0000-0000-000000000004',
       ('10.4.0.' || g)::inet,
       'dynamic', 'available'
FROM generate_series(1, 50) AS g
WHERE NOT EXISTS (
    SELECT 1 FROM ip_addresses
    WHERE pool_id = '70000000-0000-0000-0000-000000000004'
      AND address_v4 = ('10.4.0.' || g)::inet
);

-- Nar Teknoloji: Industrial Pool (10.5.0.0/24)
INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '70000000-0000-0000-0000-000000000005',
       ('10.5.0.' || g)::inet,
       'dynamic', 'available'
FROM generate_series(1, 50) AS g
WHERE NOT EXISTS (
    SELECT 1 FROM ip_addresses
    WHERE pool_id = '70000000-0000-0000-0000-000000000005'
      AND address_v4 = ('10.5.0.' || g)::inet
);

-- Bosphorus: City IoT Pool (10.10.0.0/22)
INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '70000000-0000-0000-0000-000000000011',
       ('10.10.0.' || g)::inet,
       'dynamic', 'available'
FROM generate_series(1, 50) AS g
WHERE NOT EXISTS (
    SELECT 1 FROM ip_addresses
    WHERE pool_id = '70000000-0000-0000-0000-000000000011'
      AND address_v4 = ('10.10.0.' || g)::inet
);

-- Bosphorus: Agri Pool (10.11.0.0/24)
INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '70000000-0000-0000-0000-000000000012',
       ('10.11.0.' || g)::inet,
       'dynamic', 'available'
FROM generate_series(1, 50) AS g
WHERE NOT EXISTS (
    SELECT 1 FROM ip_addresses
    WHERE pool_id = '70000000-0000-0000-0000-000000000012'
      AND address_v4 = ('10.11.0.' || g)::inet
);

-- Bosphorus: Transport Pool (10.12.0.0/24)
INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '70000000-0000-0000-0000-000000000013',
       ('10.12.0.' || g)::inet,
       'dynamic', 'available'
FROM generate_series(1, 50) AS g
WHERE NOT EXISTS (
    SELECT 1 FROM ip_addresses
    WHERE pool_id = '70000000-0000-0000-0000-000000000013'
      AND address_v4 = ('10.12.0.' || g)::inet
);

-- Bosphorus: Energy Pool (10.13.0.0/25)
INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '70000000-0000-0000-0000-000000000014',
       ('10.13.0.' || g)::inet,
       'dynamic', 'available'
FROM generate_series(1, 50) AS g
WHERE NOT EXISTS (
    SELECT 1 FROM ip_addresses
    WHERE pool_id = '70000000-0000-0000-0000-000000000014'
      AND address_v4 = ('10.13.0.' || g)::inet
);

-- Bosphorus: Water Pool (10.14.0.0/24) — provisioned above for m2m.water APN.
INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '70000000-0000-0000-0000-000000000015',
       ('10.14.0.' || g)::inet,
       'dynamic', 'available'
FROM generate_series(1, 50) AS g
WHERE EXISTS (SELECT 1 FROM ip_pools WHERE id = '70000000-0000-0000-0000-000000000015')
  AND NOT EXISTS (
    SELECT 1 FROM ip_addresses
    WHERE pool_id = '70000000-0000-0000-0000-000000000015'
      AND address_v4 = ('10.14.0.' || g)::inet
);

-- Demo tenant: Demo IoT Pool (10.20.0.0/22)
INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '07000000-0000-0000-0000-000000000001',
       ('10.20.0.' || g)::inet,
       'dynamic', 'available'
FROM generate_series(1, 50) AS g
WHERE NOT EXISTS (
    SELECT 1 FROM ip_addresses
    WHERE pool_id = '07000000-0000-0000-0000-000000000001'
      AND address_v4 = ('10.20.0.' || g)::inet
);

-- Demo tenant: Demo M2M Pool (10.21.0.0/24)
INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '07000000-0000-0000-0000-000000000002',
       ('10.21.0.' || g)::inet,
       'dynamic', 'available'
FROM generate_series(1, 50) AS g
WHERE NOT EXISTS (
    SELECT 1 FROM ip_addresses
    WHERE pool_id = '07000000-0000-0000-0000-000000000002'
      AND address_v4 = ('10.21.0.' || g)::inet
);

-- Demo tenant: Demo Data Pool (10.22.0.0/24)
INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '07000000-0000-0000-0000-000000000003',
       ('10.22.0.' || g)::inet,
       'dynamic', 'available'
FROM generate_series(1, 50) AS g
WHERE NOT EXISTS (
    SELECT 1 FROM ip_addresses
    WHERE pool_id = '07000000-0000-0000-0000-000000000003'
      AND address_v4 = ('10.22.0.' || g)::inet
);

-- Demo tenant: Demo Sensor Pool (10.23.0.0/25)
INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '07000000-0000-0000-0000-000000000004',
       ('10.23.0.' || g)::inet,
       'dynamic', 'available'
FROM generate_series(1, 50) AS g
WHERE NOT EXISTS (
    SELECT 1 FROM ip_addresses
    WHERE pool_id = '07000000-0000-0000-0000-000000000004'
      AND address_v4 = ('10.23.0.' || g)::inet
);

-- ─────────────────────────────────────────────────────────────
-- Reserve one IP per SIM from the pool bound to the SIM's APN.
--
-- For each SIM whose ip_address_id IS NULL:
--   1. pick the lowest-numbered available IP from the pool whose apn_id
--      matches the SIM's apn_id
--   2. mark that ip_addresses row as reserved + static + sim_id=SIM
--   3. update sims.ip_address_id to that row
--
-- Uses a CTE with DISTINCT ON to guarantee one IP per SIM in a single
-- statement. Wrapped in a loop-free set operation for idempotency.
-- ─────────────────────────────────────────────────────────────

WITH
-- unassigned SIMs that have an APN
candidates AS (
    SELECT s.id AS sim_id, s.tenant_id, s.operator_id, s.apn_id,
           ROW_NUMBER() OVER (PARTITION BY s.apn_id ORDER BY s.imsi) AS rn
    FROM sims s
    WHERE s.ip_address_id IS NULL
      AND s.apn_id IS NOT NULL
      AND s.state = 'active'
),
-- available IPs grouped by pool (which is linked to APN via ip_pools.apn_id)
pool_ips AS (
    SELECT ipa.id AS ip_id, ipa.pool_id, p.apn_id,
           ROW_NUMBER() OVER (PARTITION BY p.apn_id ORDER BY ipa.address_v4) AS rn
    FROM ip_addresses ipa
    JOIN ip_pools p ON p.id = ipa.pool_id
    WHERE ipa.state = 'available'
      AND ipa.sim_id IS NULL
),
-- match each SIM to the Nth available IP in its APN's pool
pairs AS (
    SELECT c.sim_id, c.operator_id, p.ip_id
    FROM candidates c
    JOIN pool_ips p ON p.apn_id = c.apn_id AND p.rn = c.rn
),
-- reserve the IPs
reserved AS (
    UPDATE ip_addresses ipa
    SET state = 'reserved',
        allocation_type = 'static',
        sim_id = p.sim_id,
        allocated_at = NOW()
    FROM pairs p
    WHERE ipa.id = p.ip_id
    RETURNING ipa.id AS ip_id, p.sim_id, p.operator_id
)
-- and point each SIM at its reserved IP
UPDATE sims s
SET ip_address_id = r.ip_id,
    updated_at = NOW()
FROM reserved r
WHERE s.id = r.sim_id
  AND s.operator_id = r.operator_id;

-- ─────────────────────────────────────────────────────────────
-- Recalculate ip_pools.used_addresses deterministically. Idempotent:
-- always reflects current ip_addresses reality regardless of run count.
-- ─────────────────────────────────────────────────────────────

UPDATE ip_pools p
SET used_addresses = sub.used_count
FROM (
    SELECT pool_id, COUNT(*) AS used_count
    FROM ip_addresses
    WHERE state IN ('allocated', 'reserved')
    GROUP BY pool_id
) sub
WHERE p.id = sub.pool_id;

-- ─────────────────────────────────────────────────────────────
-- STORY-092 D1-A fail-fast verification: every active SIM with an APN
-- must now have either a preallocated ip_address_id OR a matching pool
-- with available addresses. Previously, silently missing ip_addresses
-- rows caused AllocateIP to hit ErrPoolExhausted on the first RADIUS
-- Access-Request against any seed-003 SIM. Raise loudly here so bad
-- seed state fails the seed run rather than surprising a test engineer.
-- ─────────────────────────────────────────────────────────────

DO $$
DECLARE
    orphan_count INTEGER;
BEGIN
    SELECT COUNT(*) INTO orphan_count
    FROM sims s
    WHERE s.state = 'active'
      AND s.apn_id IS NOT NULL
      AND s.ip_address_id IS NULL
      AND NOT EXISTS (
          SELECT 1 FROM ip_pools p
          JOIN ip_addresses ipa ON ipa.pool_id = p.id
          WHERE p.apn_id = s.apn_id
            AND p.tenant_id = s.tenant_id
            AND ipa.state = 'available'
          LIMIT 1
      );
    IF orphan_count > 0 THEN
        RAISE EXCEPTION 'STORY-092 seed check: % active SIMs have no preallocated IP and no pool with available addresses', orphan_count;
    END IF;
END $$;

COMMIT;

-- Verification (run manually):
-- SELECT COUNT(*) FROM sims WHERE ip_address_id IS NOT NULL;      -- expect 16
-- SELECT COUNT(*) FROM ip_addresses WHERE state='reserved';        -- expect 16
-- SELECT s.imsi, ipa.address_v4 FROM sims s JOIN ip_addresses ipa ON ipa.id=s.ip_address_id ORDER BY s.imsi LIMIT 20;
-- SELECT name, cidr_v4, total_addresses, used_addresses FROM ip_pools ORDER BY name;

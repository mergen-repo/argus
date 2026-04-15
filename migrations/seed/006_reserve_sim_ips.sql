-- SEED-06: Reserve a static IP per SIM from its APN's pool (STORY-082 follow-up)
--
-- Idempotent:
--   - ip_addresses rows use ON CONFLICT (pool_id, address_v4) DO NOTHING
--   - SIM reservation block is guarded by sims.ip_address_id IS NULL
--   - ip_pools.used_addresses recount is deterministic from ip_addresses
--
-- Why: seed 005 creates 4 ip_pools (total_addresses=1024 each) but zero
-- actual ip_addresses rows. RADIUS handler only returns Framed-IP if the
-- SIM has a pre-assigned ip_address_id; otherwise it skips. This seed
-- populates 50 addresses per pool (plenty for 16 SIMs + future growth)
-- and reserves one per SIM permanently (state='reserved', allocation_type='static').

BEGIN;

-- ─────────────────────────────────────────────────────────────
-- Seed 005 created ip_pools only for iot+m2m APNs. Add pools for
-- `private.xyz.local` and `private.abc.local` so every SIM has a
-- pool to draw from. Idempotent via ON CONFLICT (id) DO NOTHING.
-- ─────────────────────────────────────────────────────────────

INSERT INTO ip_pools (id, tenant_id, apn_id, name, cidr_v4, total_addresses, used_addresses, state) VALUES
    ('00000000-0000-0000-0000-000000000403', '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000303', 'XYZ-Private-Pool', '10.100.8.0/22'::cidr, 1024, 0, 'active'),
    ('00000000-0000-0000-0000-000000000413', '00000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000313', 'ABC-Private-Pool', '10.200.8.0/22'::cidr, 1024, 0, 'active')
ON CONFLICT (id) DO NOTHING;

-- ─────────────────────────────────────────────────────────────
-- Populate ip_addresses from each pool's CIDR (first 50 usable IPs)
-- ─────────────────────────────────────────────────────────────

-- WHERE NOT EXISTS idempotency (the v4 unique index is partial-filtered
-- WHERE address_v4 IS NOT NULL and cannot be used in ON CONFLICT).

-- XYZ IoT pool: 10.100.0.0/22 → 10.100.0.1..50
INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '00000000-0000-0000-0000-000000000401',
       ('10.100.0.' || g)::inet,
       'dynamic', 'available'
FROM generate_series(1, 50) AS g
WHERE NOT EXISTS (
    SELECT 1 FROM ip_addresses
    WHERE pool_id = '00000000-0000-0000-0000-000000000401'
      AND address_v4 = ('10.100.0.' || g)::inet
);

-- XYZ M2M pool: 10.100.4.0/22 → 10.100.4.1..50
INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '00000000-0000-0000-0000-000000000402',
       ('10.100.4.' || g)::inet,
       'dynamic', 'available'
FROM generate_series(1, 50) AS g
WHERE NOT EXISTS (
    SELECT 1 FROM ip_addresses
    WHERE pool_id = '00000000-0000-0000-0000-000000000402'
      AND address_v4 = ('10.100.4.' || g)::inet
);

-- ABC IoT pool: 10.200.0.0/22 → 10.200.0.1..50
INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '00000000-0000-0000-0000-000000000411',
       ('10.200.0.' || g)::inet,
       'dynamic', 'available'
FROM generate_series(1, 50) AS g
WHERE NOT EXISTS (
    SELECT 1 FROM ip_addresses
    WHERE pool_id = '00000000-0000-0000-0000-000000000411'
      AND address_v4 = ('10.200.0.' || g)::inet
);

-- ABC M2M pool: 10.200.4.0/22 → 10.200.4.1..50
INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '00000000-0000-0000-0000-000000000412',
       ('10.200.4.' || g)::inet,
       'dynamic', 'available'
FROM generate_series(1, 50) AS g
WHERE NOT EXISTS (
    SELECT 1 FROM ip_addresses
    WHERE pool_id = '00000000-0000-0000-0000-000000000412'
      AND address_v4 = ('10.200.4.' || g)::inet
);

-- XYZ Private pool: 10.100.8.0/22 → 10.100.8.1..50
INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '00000000-0000-0000-0000-000000000403',
       ('10.100.8.' || g)::inet,
       'dynamic', 'available'
FROM generate_series(1, 50) AS g
WHERE NOT EXISTS (
    SELECT 1 FROM ip_addresses
    WHERE pool_id = '00000000-0000-0000-0000-000000000403'
      AND address_v4 = ('10.100.8.' || g)::inet
);

-- ABC Private pool: 10.200.8.0/22 → 10.200.8.1..50
INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT '00000000-0000-0000-0000-000000000413',
       ('10.200.8.' || g)::inet,
       'dynamic', 'available'
FROM generate_series(1, 50) AS g
WHERE NOT EXISTS (
    SELECT 1 FROM ip_addresses
    WHERE pool_id = '00000000-0000-0000-0000-000000000413'
      AND address_v4 = ('10.200.8.' || g)::inet
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

COMMIT;

-- Verification (run manually):
-- SELECT COUNT(*) FROM sims WHERE ip_address_id IS NOT NULL;      -- expect 16
-- SELECT COUNT(*) FROM ip_addresses WHERE state='reserved';        -- expect 16
-- SELECT s.imsi, ipa.address_v4 FROM sims s JOIN ip_addresses ipa ON ipa.id=s.ip_address_id ORDER BY s.imsi LIMIT 20;
-- SELECT name, cidr_v4, total_addresses, used_addresses FROM ip_pools ORDER BY name;

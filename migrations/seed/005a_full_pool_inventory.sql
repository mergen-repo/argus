-- SEED-05a: Complete ip_addresses inventory for every ip_pools row (FIX-105).
--
-- Why: seed 003 (Nar+Bosphorus+Demo) and seed 005 (XYZ+ABC) create ip_pools
-- with CIDRs spanning 1022 hosts (/22), 510 (/23), 254 (/24), 126 (/25), but
-- provisioned zero ip_addresses rows. Seed 006 then materialised only the
-- first 50 IPs per pool — enough for its own 16-SIM reservation but far short
-- of what seed 003's 624 active SIMs need. The result was POOL_EXHAUSTED on
-- any admin-initiated POST /sims/{id}/activate against a freshly-seeded DB
-- (UAT Batch 1 F-15 CRITICAL).
--
-- This seed runs BEFORE 006 (alphabetical sort: `005_*` < `005a_*` < `006_*`)
-- and materialises the full usable-host inventory for every pool. Seed 006
-- then finds available IPs for its SIM reservations and its fail-fast
-- orphan-check passes.
--
-- Approach: data-driven. Rather than one generate_series block per pool
-- (which can't express /22 pools spanning 4 octets with simple string concat),
-- we iterate over every ip_pools row with a CIDR and generate its full
-- usable-host range via CIDR arithmetic:
--   generate_series(1, power(2, 32 - masklen) - 2) → skip network + broadcast
--   host(cidr::inet + g::bigint)::inet            → Nth usable host
--
-- The three pool-creation INSERTs at the top mirror seed 006's late-adds
-- (Water Pool + XYZ/ABC Private) so that 005a's single inventory query
-- covers them in the same pass. EXISTS guards on APN prevent FK violations
-- on databases that ran only seed 003.
--
-- Counter reconciliation policy (AC-4/5/7): Option A (app-level inc/dec +
-- seed-time recount). AllocateIP / ReleaseIP / ReserveStaticIP already maintain
-- ip_pools.used_addresses transactionally. The recount below rewrites the
-- counter from ip_addresses reality, using the AC-7 definition
-- used_addresses = COUNT(state <> 'available') — includes allocated +
-- reserved + reclaiming, because Terminate transitions allocated→reclaiming
-- without decrementing the counter (the decrement happens later in
-- FinalizeReclaim). `RecountUsedAddresses` in internal/store/ippool.go is
-- the drift-recovery knob at runtime. No DB trigger added; current
-- concurrency does not warrant one.
--
-- AllocateIP short-circuits pools in state='exhausted'. Pools marked
-- exhausted by a pre-fix run would continue to reject allocations even
-- after inventory is materialised. The state-reset at the end flips them
-- back to 'active' when they now have available addresses; 'disabled'
-- pools are left untouched.
--
-- Idempotent: INSERT guarded by WHERE NOT EXISTS on (pool_id, address_v4).
-- Re-running is safe and converges.
--
-- Scale: 13 pools (seed 003) + 4 pools (seed 005) + up to 3 pools from the
-- top block = up to 20 pools × mean 400 IPs ≈ 8,000 rows. Seed time << 1s.

BEGIN;

INSERT INTO ip_pools (id, tenant_id, apn_id, name, cidr_v4, total_addresses, used_addresses, state)
SELECT '70000000-0000-0000-0000-000000000015',
       '10000000-0000-0000-0000-000000000002',
       '60000000-0000-0000-0000-000000000015',
       'Water Pool', '10.14.0.0/24'::cidr, 254, 0, 'active'
WHERE EXISTS (SELECT 1 FROM apns WHERE id = '60000000-0000-0000-0000-000000000015')
ON CONFLICT (id) DO NOTHING;

INSERT INTO ip_pools (id, tenant_id, apn_id, name, cidr_v4, total_addresses, used_addresses, state)
SELECT '00000000-0000-0000-0000-000000000403', '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000303', 'XYZ-Private-Pool', '10.100.8.0/22'::cidr, 1022, 0, 'active'
WHERE EXISTS (SELECT 1 FROM apns WHERE id = '00000000-0000-0000-0000-000000000303')
ON CONFLICT (id) DO NOTHING;

INSERT INTO ip_pools (id, tenant_id, apn_id, name, cidr_v4, total_addresses, used_addresses, state)
SELECT '00000000-0000-0000-0000-000000000413', '00000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000313', 'ABC-Private-Pool', '10.200.8.0/22'::cidr, 1022, 0, 'active'
WHERE EXISTS (SELECT 1 FROM apns WHERE id = '00000000-0000-0000-0000-000000000313')
ON CONFLICT (id) DO NOTHING;

INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
SELECT p.id,
       host((p.cidr_v4::inet + g::bigint))::inet,
       'dynamic',
       'available'
FROM ip_pools p
CROSS JOIN LATERAL generate_series(1, power(2, 32 - masklen(p.cidr_v4))::int - 2) AS g
WHERE p.cidr_v4 IS NOT NULL
  AND masklen(p.cidr_v4) <= 30
  AND NOT EXISTS (
      SELECT 1 FROM ip_addresses a
      WHERE a.pool_id = p.id
        AND a.address_v4 = host((p.cidr_v4::inet + g::bigint))::inet
  );

UPDATE ip_pools p
SET used_addresses = COALESCE(sub.used_count, 0)
FROM (
    SELECT pool_id, COUNT(*) AS used_count
    FROM ip_addresses
    WHERE state <> 'available'
    GROUP BY pool_id
) sub
WHERE p.id = sub.pool_id;

UPDATE ip_pools p
SET used_addresses = 0
WHERE NOT EXISTS (
    SELECT 1 FROM ip_addresses a
    WHERE a.pool_id = p.id AND a.state <> 'available'
);

UPDATE ip_pools p
SET state = 'active'
WHERE p.state = 'exhausted'
  AND EXISTS (
      SELECT 1 FROM ip_addresses a
      WHERE a.pool_id = p.id AND a.state = 'available'
      LIMIT 1
  );

DO $$
DECLARE
    uncovered_count INTEGER;
    pool_rec RECORD;
BEGIN
    SELECT COUNT(*) INTO uncovered_count
    FROM ip_pools p
    WHERE p.cidr_v4 IS NOT NULL
      AND masklen(p.cidr_v4) <= 30
      AND NOT EXISTS (
          SELECT 1 FROM ip_addresses a WHERE a.pool_id = p.id
      );
    IF uncovered_count > 0 THEN
        RAISE EXCEPTION 'FIX-105 seed check: % ip_pools have no ip_addresses inventory', uncovered_count;
    END IF;

    FOR pool_rec IN
        SELECT p.id, p.name, p.cidr_v4,
               (power(2, 32 - masklen(p.cidr_v4))::bigint - 2) AS expected,
               (SELECT COUNT(*) FROM ip_addresses a WHERE a.pool_id = p.id) AS actual
        FROM ip_pools p
        WHERE p.cidr_v4 IS NOT NULL AND masklen(p.cidr_v4) <= 30
    LOOP
        IF pool_rec.actual < pool_rec.expected THEN
            RAISE EXCEPTION 'FIX-105 seed check: pool % (%) has % ip_addresses, expected >= %',
                pool_rec.name, pool_rec.cidr_v4, pool_rec.actual, pool_rec.expected;
        END IF;
    END LOOP;
END $$;

COMMIT;

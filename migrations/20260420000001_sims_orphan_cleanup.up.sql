-- FIX-206 Migration A: sims orphan cleanup (data repair)
-- Must run BEFORE Migration B (FK constraint add).
-- Idempotent: safe to run multiple times — second run is a no-op.
--
-- Plan deviation (reported by Developer per §Deviation Protocol):
--   Plan §Migration A step 3 says "suspend orphan-operator SIMs", but step 6 asserts
--   "0 orphans remain after cleanup" — those are mutually exclusive, since suspending
--   does not change the FK target. The plan's seed-fix table (plan lines 291-295) encodes
--   a deterministic 1:1 mapping 00000000-…-0101/0102/0103 → 20000000-…-0001/0002/0003,
--   and observed orphan distribution (80+80+40=200) exactly matches the three buggy UUIDs
--   from seed 008. This migration therefore REMAPS operator_id using that mapping (the
--   intended cleanup that someone forgot to spell out in §Migration A), satisfying BOTH
--   "non-destructive" (no row deletes) AND "0 orphans remain" (§6 safety net).
--
-- Actions:
--   1. Remap known-good orphan operator_ids (101→001, 102→002, 103→003) on sims.
--   2. Suspend any SIM that still has an unmapped orphan operator_id afterwards
--      (defense-in-depth for future unknown orphans — NOT expected in current data).
--   3. NULL out apn_id on SIMs whose apn_id has no matching apns row.
--   4. NULL out ip_address_id on SIMs whose ip_address_id has no matching ip_addresses row.
--   5. Abort if any operator/apn/ip orphan remains (Migration B's VALIDATE would fail).
--
-- Hashchain decision (plan §Risk 3):
--   Option B chosen — audit trail is emitted via RAISE NOTICE (migration-run log).
--   Rationale: audit_logs hashchain is GLOBAL (single tail across all tenants), enforced
--   by BEFORE INSERT trigger audit_chain_guard (migrations/20260419000001). Hash formula
--   (internal/audit/audit.go ComputeHash) uses Go time.RFC3339Nano with microsecond
--   truncation — replicating it exactly in PL/pgSQL is fragile. A single mismatched hash
--   would fail the whole batch via the trigger. Migration-run logs preserve the audit
--   trail safely.

BEGIN;

DO $$
DECLARE
    orphan_operator_count INT;
    orphan_apn_count      INT;
    orphan_ip_count       INT;
    remapped_count        INT;
    suspended_count       INT;
    nulled_apn_count      INT;
    nulled_ip_count       INT;
    remaining_operator    INT;
    remaining_apn         INT;
    remaining_ip          INT;
    rec                   RECORD;

    -- Deterministic mapping (plan lines 291-295, confirmed against seed 005 canonical UUIDs).
    -- Keys are the buggy seed-008 operator_ids; values are the canonical seed-005 UUIDs.
    operator_remap CONSTANT jsonb := jsonb_build_object(
        '00000000-0000-0000-0000-000000000101', '20000000-0000-0000-0000-000000000001',
        '00000000-0000-0000-0000-000000000102', '20000000-0000-0000-0000-000000000002',
        '00000000-0000-0000-0000-000000000103', '20000000-0000-0000-0000-000000000003'
    );
BEGIN
    -- ---------------------------------------------------------------------
    -- Pre-flight: measure current orphan footprint (before any writes)
    -- ---------------------------------------------------------------------
    SELECT COUNT(*) INTO orphan_operator_count
    FROM sims s
    WHERE NOT EXISTS (SELECT 1 FROM operators o WHERE o.id = s.operator_id);

    SELECT COUNT(*) INTO orphan_apn_count
    FROM sims s
    WHERE s.apn_id IS NOT NULL
      AND NOT EXISTS (SELECT 1 FROM apns a WHERE a.id = s.apn_id);

    SELECT COUNT(*) INTO orphan_ip_count
    FROM sims s
    WHERE s.ip_address_id IS NOT NULL
      AND NOT EXISTS (SELECT 1 FROM ip_addresses i WHERE i.id = s.ip_address_id);

    RAISE NOTICE 'FIX-206: pre-flight orphan counts — operator=%, apn=%, ip_address=%',
        orphan_operator_count, orphan_apn_count, orphan_ip_count;

    -- ---------------------------------------------------------------------
    -- Fresh-volume fast path: no SIMs => no orphans => nothing to do.
    -- On fresh volume, migrations run BEFORE seeds, so sims/operators/apns
    -- are all empty. Skip the remap-target safety check (which would fail
    -- because operators don't exist yet) and the cleanup work. The post-
    -- cleanup safety net below still runs and confirms 0 orphans.
    -- ---------------------------------------------------------------------
    IF orphan_operator_count = 0 AND orphan_apn_count = 0 AND orphan_ip_count = 0 THEN
        RAISE NOTICE 'FIX-206: no orphans present (likely fresh volume) — cleanup skipped';
        RETURN;
    END IF;

    -- ---------------------------------------------------------------------
    -- Safety: verify every target operator in the remap table exists.
    -- Only checked when orphans are present (i.e., on dirty-data migration).
    -- If any target is missing, abort — remap would just shift orphans.
    -- ---------------------------------------------------------------------
    PERFORM 1
    FROM jsonb_each_text(operator_remap) AS m(src, dst)
    WHERE NOT EXISTS (SELECT 1 FROM operators o WHERE o.id = dst::uuid);
    IF FOUND THEN
        RAISE EXCEPTION 'FIX-206: one or more remap targets missing in operators table — aborting';
    END IF;

    -- ---------------------------------------------------------------------
    -- 1) Audit trail (RAISE NOTICE) + remap orphan operator_ids using the known mapping.
    --    operator_id is the partition key; row movement across partitions (sims_default →
    --    sims_turkcell/sims_vodafone/sims_turk_telekom) is supported on PostgreSQL 14+.
    --    Idempotency: natural — after first run the orphan rows no longer match the
    --    NOT EXISTS predicate, so the second run's loop/update is empty.
    -- ---------------------------------------------------------------------
    FOR rec IN
        SELECT s.id,
               s.tenant_id,
               s.operator_id      AS src_operator_id,
               (operator_remap ->> (s.operator_id::text))::uuid AS dst_operator_id,
               s.iccid,
               s.state
        FROM sims s
        WHERE NOT EXISTS (SELECT 1 FROM operators o WHERE o.id = s.operator_id)
          AND operator_remap ? (s.operator_id::text)
        ORDER BY s.id
    LOOP
        RAISE NOTICE 'FIX-206 audit: remapping sim_id=% iccid=% tenant_id=% operator_id=%->%  (state=%)',
            rec.id, rec.iccid, rec.tenant_id, rec.src_operator_id, rec.dst_operator_id, rec.state;
    END LOOP;

    UPDATE sims s
    SET operator_id = (operator_remap ->> (s.operator_id::text))::uuid,
        updated_at  = NOW(),
        metadata    = COALESCE(s.metadata, '{}'::jsonb)
                      || jsonb_build_object(
                           'fix_206_orphan_cleanup',
                           jsonb_build_object(
                               'remapped_at', to_char(NOW() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
                               'action',      'remap_operator_id',
                               'reason',      'seed_008_wrong_operator_uuid_prefix',
                               'src_operator_id', s.operator_id::text,
                               'dst_operator_id', (operator_remap ->> (s.operator_id::text))
                           )
                         )
    WHERE NOT EXISTS (SELECT 1 FROM operators o WHERE o.id = s.operator_id)
      AND operator_remap ? (s.operator_id::text);

    GET DIAGNOSTICS remapped_count = ROW_COUNT;
    RAISE NOTICE 'FIX-206: remapped operator_id on % SIM(s) via known mapping', remapped_count;

    -- ---------------------------------------------------------------------
    -- 2) Defense-in-depth: suspend any SIM that still has an unmapped orphan operator_id.
    --    Not expected in current data, but guards against future seed bugs introducing
    --    new unknown orphan UUIDs. operator_id is NOT NULL, so suspension is the only
    --    safe action — and the final §5 safety net will still fail the migration, which
    --    is the correct behavior (surfaces unknown orphans loudly).
    -- ---------------------------------------------------------------------
    FOR rec IN
        SELECT s.id, s.tenant_id, s.operator_id, s.iccid, s.state
        FROM sims s
        WHERE NOT EXISTS (SELECT 1 FROM operators o WHERE o.id = s.operator_id)
          AND s.state != 'suspended'
        ORDER BY s.id
    LOOP
        RAISE NOTICE 'FIX-206 audit: suspending sim_id=% iccid=% tenant_id=% unknown_orphan_operator_id=% prev_state=%',
            rec.id, rec.iccid, rec.tenant_id, rec.operator_id, rec.state;
    END LOOP;

    UPDATE sims s
    SET state        = 'suspended',
        suspended_at = COALESCE(s.suspended_at, NOW()),
        updated_at   = NOW(),
        metadata     = COALESCE(s.metadata, '{}'::jsonb)
                       || jsonb_build_object(
                            'fix_206_orphan_cleanup',
                            jsonb_build_object(
                                'suspended_at', to_char(NOW() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
                                'action',       'suspend_unknown_orphan',
                                'reason',       'unknown_orphan_operator_id',
                                'orphan_operator_id', s.operator_id::text,
                                'prev_state',   s.state
                            )
                          )
    WHERE NOT EXISTS (SELECT 1 FROM operators o WHERE o.id = s.operator_id)
      AND s.state != 'suspended';

    GET DIAGNOSTICS suspended_count = ROW_COUNT;
    RAISE NOTICE 'FIX-206: suspended % SIM(s) with unknown orphan operator_id', suspended_count;

    -- ---------------------------------------------------------------------
    -- 3) NULL out orphan apn_id (apn_id is nullable).
    --    Idempotency: WHERE apn_id IS NOT NULL filter.
    -- ---------------------------------------------------------------------
    UPDATE sims s
    SET apn_id     = NULL,
        updated_at = NOW()
    WHERE s.apn_id IS NOT NULL
      AND NOT EXISTS (SELECT 1 FROM apns a WHERE a.id = s.apn_id);

    GET DIAGNOSTICS nulled_apn_count = ROW_COUNT;
    RAISE NOTICE 'FIX-206: nulled apn_id on % SIM(s) with orphan apn_id', nulled_apn_count;

    -- ---------------------------------------------------------------------
    -- 4) NULL out orphan ip_address_id (ip_address_id is nullable).
    --    Idempotency: WHERE ip_address_id IS NOT NULL filter.
    -- ---------------------------------------------------------------------
    UPDATE sims s
    SET ip_address_id = NULL,
        updated_at    = NOW()
    WHERE s.ip_address_id IS NOT NULL
      AND NOT EXISTS (SELECT 1 FROM ip_addresses i WHERE i.id = s.ip_address_id);

    GET DIAGNOSTICS nulled_ip_count = ROW_COUNT;
    RAISE NOTICE 'FIX-206: nulled ip_address_id on % SIM(s) with orphan ip_address_id', nulled_ip_count;

    -- ---------------------------------------------------------------------
    -- 5) Safety net: verify NO orphans remain. Abort the whole migration if any slipped through.
    --    This guards Migration B (FK add + VALIDATE) from failing on dirty data.
    -- ---------------------------------------------------------------------
    SELECT COUNT(*) INTO remaining_operator
    FROM sims s
    WHERE NOT EXISTS (SELECT 1 FROM operators o WHERE o.id = s.operator_id);

    SELECT COUNT(*) INTO remaining_apn
    FROM sims s
    WHERE s.apn_id IS NOT NULL
      AND NOT EXISTS (SELECT 1 FROM apns a WHERE a.id = s.apn_id);

    SELECT COUNT(*) INTO remaining_ip
    FROM sims s
    WHERE s.ip_address_id IS NOT NULL
      AND NOT EXISTS (SELECT 1 FROM ip_addresses i WHERE i.id = s.ip_address_id);

    IF remaining_operator > 0 OR remaining_apn > 0 OR remaining_ip > 0 THEN
        RAISE EXCEPTION 'FIX-206: post-cleanup orphans remain — operator=%, apn=%, ip_address=%. Aborting migration to protect Migration B (FK add + VALIDATE).',
            remaining_operator, remaining_apn, remaining_ip;
    END IF;

    RAISE NOTICE 'FIX-206: cleanup complete — 0 orphans remain. Migration B (FK add) is safe to proceed.';
END
$$;

COMMIT;

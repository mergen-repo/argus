-- Migration: 20260427000002_reconcile_policy_assignments
-- Purpose: FIX-231 — reconcile sims.policy_version_id ↔ policy_assignments dual-source drift.
-- Two cases (DEV-350): (a) assignment exists, sims pointer mismatches → assignment wins;
-- (b) sims pointer non-null, no assignment → backfill assignment as 'acked' (no fresh CoA).
-- Runs ONCE; DOWN is no-op (intentional, data-fix is one-way).
-- See docs/stories/fix-ui-review/FIX-231-plan.md (DEV-349, DEV-350).

BEGIN;

-- FIX-231 F-A9 (Gate): a single DO block holds Phase 1, 2, and 3 so the
-- precise per-statement row counts (GET DIAGNOSTICS) replace the prior
-- 10-second wall-clock heuristic that would silently miss rows on a slow
-- box and double-count concurrent writers. The block is still wrapped in
-- the file-level BEGIN/COMMIT, so semantics are unchanged: the entire
-- migration is one transaction.
DO $$
DECLARE
    n_backfilled INTEGER;
    n_reconciled INTEGER;
    n_in_sync    INTEGER;
BEGIN
    -- Phase 1: BACKFILL — sims with policy_version_id but no assignment row.
    --   coa_status='acked' so the existing CoA queue does NOT enqueue
    --   spurious reissues for legacy rows.
    INSERT INTO policy_assignments (sim_id, policy_version_id, assigned_at, coa_status)
    SELECT s.id, s.policy_version_id, NOW(), 'acked'
      FROM sims s
     LEFT JOIN policy_assignments pa ON pa.sim_id = s.id
     WHERE s.policy_version_id IS NOT NULL
       AND pa.id IS NULL
    ON CONFLICT (sim_id) DO NOTHING;
    GET DIAGNOSTICS n_backfilled = ROW_COUNT;

    -- Phase 2: RECONCILE — sims whose policy_version_id mismatches the
    --   assignment. Assignment row wins (canonical). The trigger fires on
    --   this UPDATE but writes the same value, so it is a no-op write.
    UPDATE sims s
       SET policy_version_id = pa.policy_version_id
      FROM policy_assignments pa
     WHERE pa.sim_id = s.id
       AND s.policy_version_id IS DISTINCT FROM pa.policy_version_id;
    GET DIAGNOSTICS n_reconciled = ROW_COUNT;

    -- Phase 3: LOG — invariant check + precise counts.
    SELECT count(*) INTO n_in_sync
      FROM sims s
      JOIN policy_assignments pa ON pa.sim_id = s.id
     WHERE s.policy_version_id IS NOT DISTINCT FROM pa.policy_version_id;
    RAISE NOTICE 'FIX-231 reconciliation: backfilled=%, reconciled=%, total in-sync sims=%',
                 n_backfilled, n_reconciled, n_in_sync;
END $$;

COMMIT;

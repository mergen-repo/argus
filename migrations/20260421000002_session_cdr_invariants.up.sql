-- FIX-207 Migration B: CHECK constraints on sessions + cdrs for data integrity (AC-1, AC-2).
-- Must run AFTER Migration A (20260421000001 retro cleanup) — filename lexical order enforces this.
--
-- AC-1: sessions.ended_at IS NULL OR ended_at >= started_at
-- AC-2: cdrs.duration_sec >= 0
--
-- PG16 + TimescaleDB constraint: plain CHECK only.
--   `ALTER TABLE <hypertable> ADD CONSTRAINT ... NOT VALID CHECK ...` is rejected by PG16 on
--   partitioned / hypertable parents. We therefore use plain `ADD CONSTRAINT ... CHECK`, which
--   holds ACCESS EXCLUSIVE during the full-table scan. Migration A already removed all known
--   violating rows, so the scan will succeed in a single pass.
--
-- Production foot-gun warning (ROUTEMAP D-067):
--   On prod-scale data (10M+ session rows, 100M+ CDR rows across hypertable chunks), the
--   full-table scan under ACCESS EXCLUSIVE will stall live RADIUS/Diameter traffic for minutes.
--   For production rollout, follow a per-chunk CHECK strategy (add per-chunk, then attach),
--   analogous to FIX-206 D-065 per-partition runbook. Out of scope for this story which is
--   backend data-integrity, not production cutover.
--
-- Idempotency: both ALTER statements wrapped in DO-blocks guarded against pg_constraint, so
-- this migration may be re-applied safely during argus-migrate failure recovery.
--
-- Runner behavior: golang-migrate/v4 postgres driver wraps the whole file in a single implicit
-- transaction. Explicit BEGIN/COMMIT is harmless and kept for convention parity with Migration A.

BEGIN;

-- Prod-safety: emit WARNING if row counts exceed thresholds so ops is aware the ACCESS EXCLUSIVE
-- scan will take measurable time. See ROUTEMAP D-067 for the production cutover runbook.
DO $$
DECLARE
    session_count BIGINT;
    cdr_count BIGINT;
BEGIN
    SELECT COUNT(*) INTO session_count FROM sessions;
    SELECT COUNT(*) INTO cdr_count FROM cdrs;
    IF session_count > 100000 THEN
        RAISE WARNING 'FIX-207: sessions has % rows — ALTER TABLE ADD CONSTRAINT will hold ACCESS EXCLUSIVE during scan. See ROUTEMAP D-067 for prod cutover plan.', session_count;
    END IF;
    IF cdr_count > 1000000 THEN
        RAISE WARNING 'FIX-207: cdrs has % rows — ALTER TABLE ADD CONSTRAINT will hold ACCESS EXCLUSIVE during scan. See ROUTEMAP D-067 for prod cutover plan.', cdr_count;
    END IF;
END $$;

-- AC-1: sessions ended_at must be NULL (active session) or on/after started_at.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'chk_sessions_ended_after_started'
    ) THEN
        ALTER TABLE sessions
            ADD CONSTRAINT chk_sessions_ended_after_started
            CHECK (ended_at IS NULL OR ended_at >= started_at);
    END IF;
END $$;

-- AC-2: cdrs duration_sec must be non-negative.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'chk_cdrs_duration_nonneg'
    ) THEN
        ALTER TABLE cdrs
            ADD CONSTRAINT chk_cdrs_duration_nonneg
            CHECK (duration_sec >= 0);
    END IF;
END $$;

COMMIT;

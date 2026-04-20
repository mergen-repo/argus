-- FIX-207 Migration B (down): drop CHECK constraints added in up migration.
-- Safe to re-apply; DROP CONSTRAINT IF EXISTS is idempotent.
-- Pair with 20260421000001 (Migration A) down migration — dropping constraints here
-- does NOT restore rows already removed/quarantined by Migration A.

BEGIN;
ALTER TABLE cdrs DROP CONSTRAINT IF EXISTS chk_cdrs_duration_nonneg;
ALTER TABLE sessions DROP CONSTRAINT IF EXISTS chk_sessions_ended_after_started;
COMMIT;

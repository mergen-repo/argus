-- FIX-207 Migration A (down): drop session_quarantine table.
-- Note: cannot restore rows that were deleted from sessions/cdrs by the up migration.
--       Quarantine snapshots in row_data are discarded with the table drop.
-- Mirrors FIX-206 Migration A down-migration precedent (data repair is one-way).

BEGIN;
DROP TABLE IF EXISTS session_quarantine CASCADE;
COMMIT;

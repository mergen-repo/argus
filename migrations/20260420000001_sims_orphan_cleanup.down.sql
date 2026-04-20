-- FIX-206 Migration A (down): intentionally a no-op.
-- Data repair is one-way — suspended SIMs stay suspended (manual operator triage).
-- Rollback path = restore from pg_dump taken before the up migration.
SELECT 1;

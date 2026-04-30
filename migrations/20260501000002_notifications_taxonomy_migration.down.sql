-- Migration: 20260501000002_notifications_taxonomy_migration (DOWN)
--
-- FIX-237 — IRREVERSIBLE: this migration's UP step deletes historical
-- notification rows under the operator-opted-in env gate. Deleted rows
-- CANNOT be restored. Running this DOWN file is a no-op.
--
-- Rationale:
--   The deleted rows were ALL Tier 1 (per FIX-237 plan Section 2.3) — pure
--   per-SIM activity records (session.started, sim.state_changed, etc.) that
--   are not user-actionable. Recovery from a backup would only re-introduce
--   noise; admins would re-run the UP migration to re-purge.
--
-- AC-10 RAISE NOTICEs in the UP file emit only when notification_preferences
-- rows reference Tier 1 events; they have no DDL effect to revert.
--
-- This file exists to satisfy golang-migrate's up/down convention and to
-- explicitly DOCUMENT the irreversibility.

DO $$
BEGIN
    RAISE NOTICE 'FIX-237: down migration is intentionally a no-op. The Tier 1 notification row purge from the UP step is IRREVERSIBLE. See migrations/20260501000002_notifications_taxonomy_migration.up.sql header for context.';
END
$$;

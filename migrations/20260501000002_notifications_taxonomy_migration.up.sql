-- Migration: 20260501000002_notifications_taxonomy_migration
--
-- FIX-237 — M2M event taxonomy + notification redesign.
-- This migration:
--   1. Optionally purges existing notification rows whose event_type belongs
--      to Tier 1 (per FIX-237 plan Section 2.3 — these will never persist
--      under the new tier guard, so leftover historical rows are noise).
--   2. Always RAISE NOTICEs the count of orphaned notification_preferences
--      rows referencing Tier 1 / removed event_types so admins know their
--      override is now ineffective at runtime (AC-10).
--
-- Safety:
--   * Purge is env-gated: only runs when current_setting('argus.drop_tier1_notifications', true) = 'true'.
--     Default OFF — operators must opt in via `psql -v argus.drop_tier1_notifications=true -f ...`.
--   * Pre-purge count is RAISE NOTICE'd so logs reflect actual data even when skipping.
--   * AC-10 orphan-preference NOTICE always runs (no env gate) — pure observability.
--
-- IRREVERSIBILITY:
--   The DELETE in this migration is destructive and CANNOT be undone by the
--   down migration (deleted notification rows cannot be restored). The down
--   migration is a documentation-only no-op.

DO $purge$
DECLARE
    v_should_purge      BOOLEAN;
    v_pre_count         BIGINT;
    v_dropped_count     BIGINT;
    v_orphan_pref_count INT;
BEGIN
    v_should_purge := COALESCE(current_setting('argus.drop_tier1_notifications', true), 'false') = 'true';

    -- Count BEFORE any DELETE so the operator can see the impact even when skipping.
    SELECT count(*) INTO v_pre_count
      FROM notifications
     WHERE event_type IN (
        'session_started','session_ended','sim_state_change','auth_attempt',
        'heartbeat_ok','usage_threshold','ip_reclaimed','ip_released',
        'session.started','session.updated','session.ended','sim.state_changed',
        'auth.attempt','heartbeat.ok','usage.threshold','ip.reclaimed','ip.released',
        'notification.dispatch','data_portability_ready','data_portability.ready'
     );

    RAISE NOTICE 'FIX-237: Tier 1 notification rows currently in table: %', v_pre_count;

    IF v_should_purge THEN
        DELETE FROM notifications
         WHERE event_type IN (
            'session_started','session_ended','sim_state_change','auth_attempt',
            'heartbeat_ok','usage_threshold','ip_reclaimed','ip_released',
            'session.started','session.updated','session.ended','sim.state_changed',
            'auth.attempt','heartbeat.ok','usage.threshold','ip.reclaimed','ip.released',
            'notification.dispatch','data_portability_ready','data_portability.ready'
         );
        GET DIAGNOSTICS v_dropped_count = ROW_COUNT;
        RAISE NOTICE 'FIX-237: deleted % Tier 1 notification rows (env gate ON)', v_dropped_count;
    ELSE
        RAISE NOTICE 'FIX-237: argus.drop_tier1_notifications NOT set — skipping purge. To opt in, run with `psql -v argus.drop_tier1_notifications=true -f migrations/20260501000002_notifications_taxonomy_migration.up.sql`. Tier guard will suppress new rows regardless.';
    END IF;

    -- AC-10: surface user template overrides referencing Tier 1 / removed events.
    -- These remain in the table for historical purposes but are runtime-ineffective.
    SELECT count(*) INTO v_orphan_pref_count
      FROM notification_preferences
     WHERE event_type IN (
        'session_started','session.started','sim_state_change','sim.state_changed',
        'session_ended','session.ended','heartbeat_ok','heartbeat.ok',
        'auth_attempt','auth.attempt','usage_threshold','usage.threshold',
        'ip_reclaimed','ip.reclaimed','ip_released','ip.released',
        'data_portability_ready','data_portability.ready','notification.dispatch'
     );

    IF v_orphan_pref_count > 0 THEN
        RAISE NOTICE 'FIX-237 AC-10: % notification_preferences row(s) reference Tier 1 / removed event types. These overrides are NOW INEFFECTIVE — the FIX-237 tier guard suppresses Tier 1 / removed events regardless of preference. Admins should clean up via UI.', v_orphan_pref_count;
    END IF;
END
$purge$;

-- No DDL — purely DML + observability. CHECK constraints unaffected.

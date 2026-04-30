-- Migration: 20260423000001_alerts_dedup_statemachine
--
-- Purpose: Enable FIX-210 edge-triggered dedup + cooldown state machine on the
-- alerts table shipped by FIX-209. Adds occurrence_count/first_seen_at/last_seen_at/
-- cooldown_until columns, and replaces the non-unique idx_alerts_dedup partial
-- index with a UNIQUE partial index over the active state set (open/acknowledged/
-- suppressed) so ON CONFLICT upsert has a deterministic conflict target.
--
-- See docs/stories/fix-ui-review/FIX-210-plan.md §Decisions (D4, D5) for the
-- index-uniqueness + state-scope rationale.

ALTER TABLE alerts
  ADD COLUMN occurrence_count INT NOT NULL DEFAULT 1,
  ADD COLUMN first_seen_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  ADD COLUMN last_seen_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  ADD COLUMN cooldown_until   TIMESTAMPTZ NULL;

-- Backfill first/last seen from fired_at for any existing rows (idempotent).
UPDATE alerts
   SET first_seen_at = fired_at,
       last_seen_at  = fired_at
 WHERE first_seen_at <> fired_at OR last_seen_at <> fired_at;

-- Replace non-unique partial index with UNIQUE partial index over active states.
DROP INDEX IF EXISTS idx_alerts_dedup;
CREATE UNIQUE INDEX idx_alerts_dedup_unique
  ON alerts (tenant_id, dedup_key)
  WHERE dedup_key IS NOT NULL
    AND state IN ('open', 'acknowledged', 'suppressed');

-- Cooldown lookup index: resolved rows with an active cooldown window.
CREATE INDEX idx_alerts_cooldown_lookup
  ON alerts (tenant_id, dedup_key, cooldown_until)
  WHERE state = 'resolved' AND cooldown_until IS NOT NULL;

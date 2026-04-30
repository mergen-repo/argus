-- Migration: 20260421000003_severity_taxonomy_unification
--
-- Purpose: Unify the severity taxonomy to 5 canonical values across all tables
-- that store severity. FIX-211 — see docs/stories/fix-ui-review/FIX-211-plan.md
-- and docs/architecture/ERROR_CODES.md "Severity Taxonomy" section.
--
-- Canonical values (strictly ordered): info < low < medium < high < critical
--
-- Tables covered (4 constraints): anomalies, policy_violations,
-- notifications, notification_preferences.

-- ============================================================
-- STEP 1: Data migration (must run BEFORE the CHECK adds, idempotent).
-- ============================================================

-- policy_violations: warning -> medium
UPDATE policy_violations SET severity = 'medium' WHERE severity = 'warning';

-- notifications: warning -> medium, error -> high
UPDATE notifications SET severity = CASE severity
  WHEN 'warning' THEN 'medium'
  WHEN 'error'   THEN 'high'
  ELSE severity
END
WHERE severity IN ('warning', 'error');

-- notification_preferences: same map on severity_threshold
UPDATE notification_preferences SET severity_threshold = CASE severity_threshold
  WHEN 'warning' THEN 'medium'
  WHEN 'error'   THEN 'high'
  ELSE severity_threshold
END
WHERE severity_threshold IN ('warning', 'error');

-- ============================================================
-- STEP 2: Fail-fast guards (any remaining non-canonical row aborts).
-- ============================================================

DO $$ BEGIN
  IF (SELECT count(*) FROM anomalies WHERE severity NOT IN ('critical','high','medium','low','info') OR severity IS NULL) > 0 THEN
    RAISE EXCEPTION 'chk_anomalies_severity violation: non-canonical rows exist';
  END IF;
END $$;

DO $$ BEGIN
  IF (SELECT count(*) FROM policy_violations WHERE severity NOT IN ('critical','high','medium','low','info') OR severity IS NULL) > 0 THEN
    RAISE EXCEPTION 'chk_policy_violations_severity violation: non-canonical rows exist';
  END IF;
END $$;

DO $$ BEGIN
  IF (SELECT count(*) FROM notifications WHERE severity NOT IN ('critical','high','medium','low','info') OR severity IS NULL) > 0 THEN
    RAISE EXCEPTION 'chk_notifications_severity violation: non-canonical rows exist';
  END IF;
END $$;

DO $$ BEGIN
  IF (SELECT count(*) FROM notification_preferences WHERE severity_threshold NOT IN ('critical','high','medium','low','info') OR severity_threshold IS NULL) > 0 THEN
    RAISE EXCEPTION 'chk_notif_prefs_severity_threshold violation: non-canonical rows exist';
  END IF;
END $$;

-- ============================================================
-- STEP 3: Drop the old anomalies CHECK (it omitted 'info').
-- PostgreSQL renders IN-lists as "= ANY (ARRAY[...])" in pg_get_constraintdef,
-- so we match on "severity" appearing in the constraint body and exclude the
-- new constraint name. Drop every severity-scoped CHECK except the new one.
-- ============================================================

ALTER TABLE anomalies DROP CONSTRAINT IF EXISTS anomalies_severity_check;

DO $$
DECLARE
  cons_name TEXT;
BEGIN
  FOR cons_name IN
    SELECT conname
    FROM pg_constraint
    WHERE conrelid = 'anomalies'::regclass
      AND contype  = 'c'
      AND conname <> 'chk_anomalies_severity'
      AND pg_get_constraintdef(oid) ILIKE '%severity%'
  LOOP
    EXECUTE format('ALTER TABLE anomalies DROP CONSTRAINT %I', cons_name);
  END LOOP;
END $$;

-- ============================================================
-- STEP 4: Add new 5-value CHECKs.
-- ============================================================

ALTER TABLE anomalies ADD CONSTRAINT chk_anomalies_severity
  CHECK (severity IN ('critical','high','medium','low','info'));

ALTER TABLE policy_violations ADD CONSTRAINT chk_policy_violations_severity
  CHECK (severity IN ('critical','high','medium','low','info'));

ALTER TABLE notifications ADD CONSTRAINT chk_notifications_severity
  CHECK (severity IN ('critical','high','medium','low','info'));

ALTER TABLE notification_preferences ADD CONSTRAINT chk_notif_prefs_severity_threshold
  CHECK (severity_threshold IN ('critical','high','medium','low','info'));

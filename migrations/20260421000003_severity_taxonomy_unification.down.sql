-- Down migration for 20260421000003_severity_taxonomy_unification
--
-- Reverses the CHECK constraints. Data is NOT reverted (warning/error values
-- no longer legal to reintroduce — keep the 5-value data, just drop the guards).
-- anomalies gets its original 4-value CHECK back (without 'info').

ALTER TABLE notification_preferences DROP CONSTRAINT IF EXISTS chk_notif_prefs_severity_threshold;
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS chk_notifications_severity;
ALTER TABLE policy_violations DROP CONSTRAINT IF EXISTS chk_policy_violations_severity;
ALTER TABLE anomalies DROP CONSTRAINT IF EXISTS chk_anomalies_severity;

-- Any 'info' anomalies inserted post-deploy would block the original 4-value
-- CHECK. Collapse them to 'low' so the constraint can be reapplied without
-- manual intervention during rollback. Non-destructive (info ≈ low semantically).
UPDATE anomalies SET severity = 'low' WHERE severity = 'info';

-- Restore anomalies' original 4-value CHECK to match pre-migration schema.
-- (The original auto-generated name cannot be preserved; the constraint definition is.)
ALTER TABLE anomalies ADD CONSTRAINT anomalies_severity_check
  CHECK (severity IN ('critical','high','medium','low'));

-- Reverse of 20260426000001_alert_suppressions
DROP INDEX IF EXISTS idx_alerts_dedup_all_states;
DROP TABLE IF EXISTS alert_suppressions;

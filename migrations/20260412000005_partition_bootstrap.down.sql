-- Detaches and drops the bootstrapped partitions for 2026_07..2027_03
-- in reverse order. After this runs, the Go partition_creator cron (Task 4)
-- or a re-run of the up migration must be used to restore coverage.

-- ============================================================
-- sim_state_history partitions: 2027_03 down to 2026_07
-- ============================================================
ALTER TABLE sim_state_history DETACH PARTITION sim_state_history_2027_03;
DROP TABLE IF EXISTS sim_state_history_2027_03;

ALTER TABLE sim_state_history DETACH PARTITION sim_state_history_2027_02;
DROP TABLE IF EXISTS sim_state_history_2027_02;

ALTER TABLE sim_state_history DETACH PARTITION sim_state_history_2027_01;
DROP TABLE IF EXISTS sim_state_history_2027_01;

ALTER TABLE sim_state_history DETACH PARTITION sim_state_history_2026_12;
DROP TABLE IF EXISTS sim_state_history_2026_12;

ALTER TABLE sim_state_history DETACH PARTITION sim_state_history_2026_11;
DROP TABLE IF EXISTS sim_state_history_2026_11;

ALTER TABLE sim_state_history DETACH PARTITION sim_state_history_2026_10;
DROP TABLE IF EXISTS sim_state_history_2026_10;

ALTER TABLE sim_state_history DETACH PARTITION sim_state_history_2026_09;
DROP TABLE IF EXISTS sim_state_history_2026_09;

ALTER TABLE sim_state_history DETACH PARTITION sim_state_history_2026_08;
DROP TABLE IF EXISTS sim_state_history_2026_08;

ALTER TABLE sim_state_history DETACH PARTITION sim_state_history_2026_07;
DROP TABLE IF EXISTS sim_state_history_2026_07;

-- ============================================================
-- audit_logs partitions: 2027_03 down to 2026_07
-- ============================================================
ALTER TABLE audit_logs DETACH PARTITION audit_logs_2027_03;
DROP TABLE IF EXISTS audit_logs_2027_03;

ALTER TABLE audit_logs DETACH PARTITION audit_logs_2027_02;
DROP TABLE IF EXISTS audit_logs_2027_02;

ALTER TABLE audit_logs DETACH PARTITION audit_logs_2027_01;
DROP TABLE IF EXISTS audit_logs_2027_01;

ALTER TABLE audit_logs DETACH PARTITION audit_logs_2026_12;
DROP TABLE IF EXISTS audit_logs_2026_12;

ALTER TABLE audit_logs DETACH PARTITION audit_logs_2026_11;
DROP TABLE IF EXISTS audit_logs_2026_11;

ALTER TABLE audit_logs DETACH PARTITION audit_logs_2026_10;
DROP TABLE IF EXISTS audit_logs_2026_10;

ALTER TABLE audit_logs DETACH PARTITION audit_logs_2026_09;
DROP TABLE IF EXISTS audit_logs_2026_09;

ALTER TABLE audit_logs DETACH PARTITION audit_logs_2026_08;
DROP TABLE IF EXISTS audit_logs_2026_08;

ALTER TABLE audit_logs DETACH PARTITION audit_logs_2026_07;
DROP TABLE IF EXISTS audit_logs_2026_07;

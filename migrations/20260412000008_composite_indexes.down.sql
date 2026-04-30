-- Reverse AC-10: Drop composite indexes added for per-SIM ordered list hot paths
DROP INDEX IF EXISTS idx_sessions_sim_started;
DROP INDEX IF EXISTS idx_cdrs_sim_timestamp;

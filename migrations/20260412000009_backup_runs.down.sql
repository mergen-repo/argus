DROP INDEX IF EXISTS idx_backup_verifications_run;
DROP TABLE IF EXISTS backup_verifications;

DROP INDEX IF EXISTS idx_backup_runs_state;
DROP INDEX IF EXISTS idx_backup_runs_kind_time;
DROP TABLE IF EXISTS backup_runs;

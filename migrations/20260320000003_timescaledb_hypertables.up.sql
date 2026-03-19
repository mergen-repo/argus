-- Convert sessions, cdrs, and operator_health_logs to TimescaleDB hypertables

-- Sessions hypertable (TBL-17)
SELECT create_hypertable('sessions', 'started_at', migrate_data => true, if_not_exists => true);

-- Session indexes (must be created after hypertable conversion)
CREATE INDEX IF NOT EXISTS idx_sessions_sim_active ON sessions (sim_id) WHERE session_state = 'active';
CREATE INDEX IF NOT EXISTS idx_sessions_tenant_active ON sessions (tenant_id) WHERE session_state = 'active';
CREATE INDEX IF NOT EXISTS idx_sessions_tenant_operator ON sessions (tenant_id, operator_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_acct_session ON sessions (acct_session_id) WHERE acct_session_id IS NOT NULL;

-- Sessions compression policy (compress after 30 days)
ALTER TABLE sessions SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'tenant_id, operator_id',
    timescaledb.compress_orderby = 'started_at DESC'
);
SELECT add_compression_policy('sessions', INTERVAL '30 days', if_not_exists => true);

-- CDRs hypertable (TBL-18)
SELECT create_hypertable('cdrs', 'timestamp', migrate_data => true, if_not_exists => true);

-- CDR indexes (must be created after hypertable conversion)
CREATE INDEX IF NOT EXISTS idx_cdrs_session ON cdrs (session_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_cdrs_tenant_time ON cdrs (tenant_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_cdrs_tenant_operator_time ON cdrs (tenant_id, operator_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_cdrs_sim_time ON cdrs (sim_id, timestamp DESC);

-- CDR compression policy (compress after 7 days)
ALTER TABLE cdrs SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'tenant_id, operator_id',
    timescaledb.compress_orderby = 'timestamp DESC'
);
SELECT add_compression_policy('cdrs', INTERVAL '7 days', if_not_exists => true);

-- Operator health logs hypertable (TBL-23)
SELECT create_hypertable('operator_health_logs', 'checked_at', migrate_data => true, if_not_exists => true);

-- Operator health logs compression (compress after 7 days)
ALTER TABLE operator_health_logs SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'operator_id',
    timescaledb.compress_orderby = 'checked_at DESC'
);
SELECT add_compression_policy('operator_health_logs', INTERVAL '7 days', if_not_exists => true);

-- Retention policy for operator_health_logs (90 days)
SELECT add_retention_policy('operator_health_logs', INTERVAL '90 days', if_not_exists => true);

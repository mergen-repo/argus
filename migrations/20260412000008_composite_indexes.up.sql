-- Composite indexes on hot paths
-- AC-10: Add missing composite indexes for per-SIM ordered list queries
--
-- NOTE: idx_audit_tenant_time and idx_notifications_tenant_time already exist
-- in migrations/20260320000002_core_schema.up.sql (lines 470 and 529).
-- They are NOT recreated here.
--
-- NOTE: CREATE INDEX CONCURRENTLY cannot run inside a transaction.
-- golang-migrate wraps each migration in a transaction, so we use plain
-- CREATE INDEX IF NOT EXISTS here. For zero-downtime production environments
-- the CONCURRENTLY variant should be applied manually outside a transaction
-- in a separate migration (out of scope for this story).
--
-- EXPLAIN output to be verified in gate step (no live DB available at migration
-- authoring time). Expected plan for each query below: Index Scan, not Seq Scan.

-- Serves API-051: GET /api/v1/sims/:id/sessions (per-SIM recent sessions)
-- Query: SELECT ... FROM sessions WHERE sim_id = $1 ORDER BY started_at DESC LIMIT N
-- Without this index → Seq Scan on sessions (10M+ rows across the table)
-- With this index → Index Scan on idx_sessions_sim_started (< 50ms target)
-- EXPLAIN ANALYZE SELECT * FROM sessions WHERE sim_id = '00000000-0000-0000-0000-000000000000' ORDER BY started_at DESC LIMIT 10;
-- -- EXPLAIN output to be verified in gate step
CREATE INDEX IF NOT EXISTS idx_sessions_sim_started
    ON sessions (sim_id, started_at DESC);

-- Serves API-114: GET /api/v1/cdrs (per-SIM CDR time-range queries)
-- Query: SELECT ... FROM cdrs WHERE sim_id = $1 ORDER BY timestamp DESC LIMIT N
-- Without this index → Seq Scan on cdrs hypertable (10M+ rows)
-- With this index → Index Scan on idx_cdrs_sim_timestamp (< 50ms target)
-- EXPLAIN ANALYZE SELECT * FROM cdrs WHERE sim_id = '00000000-0000-0000-0000-000000000000' ORDER BY timestamp DESC LIMIT 10;
-- -- EXPLAIN output to be verified in gate step
CREATE INDEX IF NOT EXISTS idx_cdrs_sim_timestamp
    ON cdrs (sim_id, timestamp DESC);

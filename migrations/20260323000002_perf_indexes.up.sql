-- Performance optimization indexes
-- Identified by perf-optimizer analysis
--
-- NOTE: CREATE INDEX CONCURRENTLY cannot run inside a transaction.
-- golang-migrate wraps each migration in a transaction, so we use plain
-- CREATE INDEX IF NOT EXISTS here. These indexes are created at schema-bootstrap
-- time before any production load, so the zero-downtime property of CONCURRENTLY
-- is not required. For out-of-band rebuilds in production, run
-- CREATE INDEX CONCURRENTLY manually. See STORY-079 D-014 (AC-2) for context.

-- Enable pg_trgm for trigram indexes on SIM search fields
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- SIMs: composite index for list query ORDER BY created_at DESC, id DESC
-- The sim list query uses WHERE tenant_id = $1 ... ORDER BY created_at DESC, id DESC
-- Current idx_sims_tenant_state only covers (tenant_id, state)
CREATE INDEX IF NOT EXISTS idx_sims_tenant_created
    ON sims (tenant_id, created_at DESC, id DESC);

-- SIMs: ILIKE search on iccid/imsi/msisdn needs trigram index for 10M+ scale
-- Without this, the Q search parameter causes full partition scans
CREATE INDEX IF NOT EXISTS idx_sims_tenant_iccid_trgm
    ON sims USING gin (iccid gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_sims_tenant_imsi_trgm
    ON sims USING gin (imsi gin_trgm_ops);

-- Sessions: session_state filter used in CountActive (no tenant filter)
-- Current idx_sessions_tenant_active is partial on tenant_id only
CREATE INDEX IF NOT EXISTS idx_sessions_state
    ON sessions (session_state) WHERE session_state = 'active';

-- Sessions: GetByAcctSessionID queries by acct_session_id + session_state='active'
-- with ORDER BY started_at DESC -- covering index
CREATE INDEX IF NOT EXISTS idx_sessions_acct_active
    ON sessions (acct_session_id, started_at DESC)
    WHERE session_state = 'active' AND acct_session_id IS NOT NULL;

-- Anomalies: composite index for ListByTenant (tenant_id + detected_at DESC, id DESC)
CREATE INDEX IF NOT EXISTS idx_anomalies_tenant_detected
    ON anomalies (tenant_id, detected_at DESC, id DESC);

-- Anomalies: HasRecentAnomaly query filter
CREATE INDEX IF NOT EXISTS idx_anomalies_tenant_sim_type_active
    ON anomalies (tenant_id, sim_id, type, detected_at DESC)
    WHERE state IN ('open', 'acknowledged');

-- CDRs: session_id lookup for GetCumulativeSessionBytes
-- Current idx_cdrs_session is (session_id, timestamp), SUM query benefits from it
-- but adding a covering index with bytes columns would help

-- Audit: GetLastHash query uses ORDER BY id DESC LIMIT 1 per tenant
CREATE INDEX IF NOT EXISTS idx_audit_tenant_id_desc
    ON audit_logs (tenant_id, id DESC);

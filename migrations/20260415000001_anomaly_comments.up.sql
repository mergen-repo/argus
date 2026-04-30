-- STORY-072 Task 1: Anomaly comment thread
-- Creates anomaly_comments table for per-alert discussion threads.

CREATE TABLE IF NOT EXISTS anomaly_comments (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL,
    anomaly_id   UUID NOT NULL REFERENCES anomalies(id) ON DELETE CASCADE,
    user_id      UUID NOT NULL REFERENCES users(id),
    body         TEXT NOT NULL CHECK (char_length(body) BETWEEN 1 AND 2000),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_anomaly_comments_anomaly
    ON anomaly_comments (anomaly_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_anomaly_comments_tenant
    ON anomaly_comments (tenant_id, created_at DESC);

ALTER TABLE anomaly_comments ENABLE ROW LEVEL SECURITY;
ALTER TABLE anomaly_comments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_anomaly_comments ON anomaly_comments
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- STORY-053: Data Volume Optimization
-- CDR retention, S3 archival tracking, storage monitoring

-- Table to track S3 archival history
CREATE TABLE IF NOT EXISTS s3_archival_log (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL,
    table_name      TEXT NOT NULL,
    chunk_name      TEXT NOT NULL,
    chunk_range_start TIMESTAMPTZ NOT NULL,
    chunk_range_end   TIMESTAMPTZ NOT NULL,
    s3_bucket       TEXT NOT NULL,
    s3_key          TEXT NOT NULL,
    size_bytes      BIGINT NOT NULL DEFAULT 0,
    row_count       BIGINT NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'pending',
    error_message   TEXT,
    archived_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_s3_archival_tenant ON s3_archival_log (tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_s3_archival_status ON s3_archival_log (status) WHERE status != 'completed';

-- Table for per-tenant data retention configuration
CREATE TABLE IF NOT EXISTS tenant_retention_config (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL UNIQUE REFERENCES tenants(id),
    cdr_retention_days    INT NOT NULL DEFAULT 365,
    session_retention_days INT NOT NULL DEFAULT 365,
    audit_retention_days   INT NOT NULL DEFAULT 730,
    s3_archival_enabled   BOOLEAN NOT NULL DEFAULT false,
    s3_archival_bucket    TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tenant_retention_tenant ON tenant_retention_config (tenant_id);

-- Add retention policy for CDRs (default 365 days, managed per-tenant via job)
SELECT add_retention_policy('cdrs', INTERVAL '730 days', if_not_exists => true);

-- Add retention policy for sessions (730 days max, per-tenant management via job)
SELECT add_retention_policy('sessions', INTERVAL '730 days', if_not_exists => true);

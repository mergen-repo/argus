CREATE TABLE IF NOT EXISTS sla_reports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    operator_id UUID REFERENCES operators(id),           -- NULL = tenant-wide aggregate
    window_start TIMESTAMPTZ NOT NULL,
    window_end   TIMESTAMPTZ NOT NULL,
    uptime_pct       NUMERIC(5,2)  NOT NULL,             -- 0.00 to 100.00
    latency_p95_ms   INTEGER       NOT NULL DEFAULT 0,
    incident_count   INTEGER       NOT NULL DEFAULT 0,
    mttr_sec         INTEGER       NOT NULL DEFAULT 0,
    sessions_total   BIGINT        NOT NULL DEFAULT 0,
    error_count      INTEGER       NOT NULL DEFAULT 0,
    details          JSONB         NOT NULL DEFAULT '{}'::jsonb,
    generated_at     TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    CONSTRAINT sla_window_valid CHECK (window_end > window_start)
);

CREATE INDEX IF NOT EXISTS idx_sla_reports_tenant_time  ON sla_reports (tenant_id, window_end DESC);
CREATE INDEX IF NOT EXISTS idx_sla_reports_operator     ON sla_reports (operator_id, window_end DESC) WHERE operator_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_sla_reports_generated_at ON sla_reports (generated_at DESC);

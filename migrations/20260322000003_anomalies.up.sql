CREATE TABLE IF NOT EXISTS anomalies (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id),
    sim_id          UUID,
    type            TEXT NOT NULL CHECK (type IN ('sim_cloning', 'data_spike', 'auth_flood', 'nas_flood')),
    severity        TEXT NOT NULL CHECK (severity IN ('critical', 'high', 'medium', 'low')),
    state           TEXT NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'acknowledged', 'resolved', 'false_positive')),
    details         JSONB NOT NULL DEFAULT '{}',
    source          TEXT,
    detected_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    acknowledged_at TIMESTAMPTZ,
    resolved_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_anomalies_tenant_id ON anomalies(tenant_id);
CREATE INDEX idx_anomalies_sim_id ON anomalies(sim_id) WHERE sim_id IS NOT NULL;
CREATE INDEX idx_anomalies_type ON anomalies(type);
CREATE INDEX idx_anomalies_severity ON anomalies(severity);
CREATE INDEX idx_anomalies_state ON anomalies(state);
CREATE INDEX idx_anomalies_detected_at ON anomalies(detected_at DESC);
CREATE INDEX idx_anomalies_tenant_state ON anomalies(tenant_id, state);

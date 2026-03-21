CREATE TABLE IF NOT EXISTS ota_commands (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id),
    sim_id          UUID NOT NULL REFERENCES sims(id),
    command_type    VARCHAR(30) NOT NULL CHECK (command_type IN ('UPDATE_FILE', 'INSTALL_APPLET', 'DELETE_APPLET', 'READ_FILE', 'SIM_TOOLKIT')),
    channel         VARCHAR(10) NOT NULL DEFAULT 'sms_pp' CHECK (channel IN ('sms_pp', 'bip')),
    status          VARCHAR(15) NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'sent', 'delivered', 'executed', 'confirmed', 'failed')),
    apdu_data       BYTEA,
    security_mode   VARCHAR(10) NOT NULL DEFAULT 'none' CHECK (security_mode IN ('none', 'kic', 'kid', 'kic_kid')),
    payload         JSONB NOT NULL DEFAULT '{}',
    response_data   JSONB,
    error_message   TEXT,
    job_id          UUID REFERENCES jobs(id),
    retry_count     INT NOT NULL DEFAULT 0,
    max_retries     INT NOT NULL DEFAULT 3,
    created_by      UUID REFERENCES users(id),
    sent_at         TIMESTAMPTZ,
    delivered_at    TIMESTAMPTZ,
    executed_at     TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ota_commands_tenant_id ON ota_commands(tenant_id);
CREATE INDEX idx_ota_commands_sim_id ON ota_commands(sim_id);
CREATE INDEX idx_ota_commands_job_id ON ota_commands(job_id) WHERE job_id IS NOT NULL;
CREATE INDEX idx_ota_commands_status ON ota_commands(status) WHERE status IN ('queued', 'sent');
CREATE INDEX idx_ota_commands_sim_created ON ota_commands(sim_id, created_at DESC);

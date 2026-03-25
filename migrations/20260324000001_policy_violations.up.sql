CREATE TABLE IF NOT EXISTS policy_violations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL,
    sim_id          UUID NOT NULL,
    policy_id       UUID NOT NULL,
    version_id      UUID NOT NULL,
    rule_index      INT NOT NULL DEFAULT 0,
    violation_type  TEXT NOT NULL,
    action_taken    TEXT NOT NULL,
    details         JSONB NOT NULL DEFAULT '{}',
    session_id      UUID,
    operator_id     UUID,
    apn_id          UUID,
    severity        TEXT NOT NULL DEFAULT 'info',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_policy_violations_tenant ON policy_violations(tenant_id, created_at DESC);
CREATE INDEX idx_policy_violations_sim ON policy_violations(sim_id, created_at DESC);
CREATE INDEX idx_policy_violations_policy ON policy_violations(policy_id, created_at DESC);
CREATE INDEX idx_policy_violations_type ON policy_violations(tenant_id, violation_type, created_at DESC);

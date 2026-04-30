CREATE TABLE IF NOT EXISTS roaming_agreements (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    operator_id UUID NOT NULL REFERENCES operators(id),
    partner_operator_name VARCHAR(200) NOT NULL,
    agreement_type VARCHAR(20) NOT NULL,
    sla_terms JSONB NOT NULL DEFAULT '{}'::jsonb,
    cost_terms JSONB NOT NULL DEFAULT '{}'::jsonb,
    start_date DATE NOT NULL,
    end_date DATE NOT NULL,
    auto_renew BOOLEAN NOT NULL DEFAULT false,
    state VARCHAR(20) NOT NULL DEFAULT 'draft',
    notes TEXT,
    terminated_at TIMESTAMPTZ,
    created_by UUID REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT roaming_agreements_type_chk CHECK (agreement_type IN ('national','international','MVNO')),
    CONSTRAINT roaming_agreements_state_chk CHECK (state IN ('draft','active','expired','terminated')),
    CONSTRAINT roaming_agreements_dates_chk CHECK (end_date > start_date)
);

CREATE INDEX IF NOT EXISTS idx_roaming_agreements_tenant ON roaming_agreements (tenant_id);
CREATE INDEX IF NOT EXISTS idx_roaming_agreements_tenant_op ON roaming_agreements (tenant_id, operator_id);
CREATE INDEX IF NOT EXISTS idx_roaming_agreements_tenant_state ON roaming_agreements (tenant_id, state);
CREATE INDEX IF NOT EXISTS idx_roaming_agreements_expiry ON roaming_agreements (tenant_id, end_date) WHERE state = 'active';
CREATE UNIQUE INDEX IF NOT EXISTS idx_roaming_agreements_active_unique
  ON roaming_agreements (tenant_id, operator_id)
  WHERE state = 'active';

ALTER TABLE roaming_agreements ENABLE ROW LEVEL SECURITY;
ALTER TABLE roaming_agreements FORCE ROW LEVEL SECURITY;
CREATE POLICY roaming_agreements_tenant_isolation ON roaming_agreements
  USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

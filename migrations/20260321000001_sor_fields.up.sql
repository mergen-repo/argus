-- Add SoR (Steering of Roaming) fields to operator_grants
ALTER TABLE operator_grants ADD COLUMN IF NOT EXISTS sor_priority INTEGER NOT NULL DEFAULT 100;
ALTER TABLE operator_grants ADD COLUMN IF NOT EXISTS cost_per_mb DECIMAL(8,4) DEFAULT 0.0;
ALTER TABLE operator_grants ADD COLUMN IF NOT EXISTS region VARCHAR(50);

-- Index for SoR priority-based queries
CREATE INDEX IF NOT EXISTS idx_operator_grants_tenant_sor ON operator_grants (tenant_id, sor_priority) WHERE enabled = true;

-- Add SoR decision field to sessions
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS sor_decision JSONB;

-- Remove SoR decision field from sessions
ALTER TABLE sessions DROP COLUMN IF EXISTS sor_decision;

-- Remove SoR index from operator_grants
DROP INDEX IF EXISTS idx_operator_grants_tenant_sor;

-- Remove SoR fields from operator_grants
ALTER TABLE operator_grants DROP COLUMN IF EXISTS region;
ALTER TABLE operator_grants DROP COLUMN IF EXISTS cost_per_mb;
ALTER TABLE operator_grants DROP COLUMN IF EXISTS sor_priority;

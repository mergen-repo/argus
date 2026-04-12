DROP INDEX IF EXISTS idx_operator_grants_rat_types_gin;

ALTER TABLE operator_grants DROP COLUMN IF EXISTS supported_rat_types;

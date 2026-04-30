BEGIN;
ALTER TABLE policy_assignments DROP COLUMN IF EXISTS coa_failure_reason;
COMMIT;

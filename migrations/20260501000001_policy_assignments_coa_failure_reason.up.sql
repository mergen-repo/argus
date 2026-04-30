BEGIN;
ALTER TABLE policy_assignments
  ADD COLUMN IF NOT EXISTS coa_failure_reason TEXT;
COMMENT ON COLUMN policy_assignments.coa_failure_reason IS
  'Free-text reason captured when CoA dispatch fails (FIX-242 D-145 fold-in). Populated by sendCoAForSIM error path. Example: "diameter timeout", "no session", "RADIUS NAS unreachable".';
COMMIT;

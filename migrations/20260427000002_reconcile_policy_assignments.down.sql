-- Reverse of 20260427000002_reconcile_policy_assignments
-- Intentional no-op: data fix is one-way. The schema migration (20260427000001) DOWN
-- drops the sync trigger, so reverting to old code paths is safe even with reconciled data.
SELECT 1;

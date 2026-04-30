-- SEED-07: Seed SIM state history rows (STORY-082 follow-up)
--
-- RADIUS auth/stop handlers do not currently write to sim_state_history
-- (InsertHistory is only called from bulk_state_change + import jobs).
-- Seed enough history so UI's "SIM detail → History" tab shows data.
--
-- Idempotent via WHERE NOT EXISTS predicates on sim_id + reason.
--
-- Each SIM gets 3 rows reflecting a realistic-looking lifecycle:
--   t-7d : ordered → active (manual activation)
--   t-2d : active → active (policy auto-assigned, via STORY-079 matcher)
--   t-1h : active → active (last auth success marker)

BEGIN;

-- Row 1: ordered → active (7 days ago, manual activation)
INSERT INTO sim_state_history (sim_id, from_state, to_state, reason, triggered_by, user_id, created_at)
SELECT s.id, 'ordered', 'active', 'manual_seed_test_activation', 'user',
       '00000000-0000-0000-0000-000000000010',
       NOW() - INTERVAL '7 days'
FROM sims s
WHERE s.state = 'active'
  AND NOT EXISTS (
      SELECT 1 FROM sim_state_history h
      WHERE h.sim_id = s.id AND h.reason = 'manual_seed_test_activation'
  );

-- Row 2: active → active (2 days ago, policy auto-assigned)
INSERT INTO sim_state_history (sim_id, from_state, to_state, reason, triggered_by, created_at)
SELECT s.id, 'active', 'active', 'policy_auto_assigned_by_matcher', 'system',
       NOW() - INTERVAL '2 days'
FROM sims s
WHERE s.state = 'active'
  AND s.policy_version_id IS NOT NULL  -- only SIMs with a matched policy
  AND NOT EXISTS (
      SELECT 1 FROM sim_state_history h
      WHERE h.sim_id = s.id AND h.reason = 'policy_auto_assigned_by_matcher'
  );

-- Row 3: active → active (1 hour ago, last auth success marker)
INSERT INTO sim_state_history (sim_id, from_state, to_state, reason, triggered_by, created_at)
SELECT s.id, 'active', 'active', 'radius_auth_success', 'system',
       NOW() - INTERVAL '1 hour'
FROM sims s
WHERE s.state = 'active'
  AND NOT EXISTS (
      SELECT 1 FROM sim_state_history h
      WHERE h.sim_id = s.id AND h.reason = 'radius_auth_success'
  );

COMMIT;

-- Verification:
-- SELECT sim_id, COUNT(*) FROM sim_state_history GROUP BY sim_id ORDER BY 1 LIMIT 5;
-- SELECT reason, COUNT(*) FROM sim_state_history GROUP BY reason ORDER BY 2 DESC;

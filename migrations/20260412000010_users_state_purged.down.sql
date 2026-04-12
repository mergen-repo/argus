-- Reverse: drop expanded check and restore the three-state constraint.
-- Note: if any users are currently in state='purged' the ADD will fail; the
-- operator must reassign those rows first (not expected — purge is one-way).

ALTER TABLE users DROP CONSTRAINT IF EXISTS chk_users_state;

ALTER TABLE users ADD CONSTRAINT chk_users_state
  CHECK (state IN ('active','disabled','invited'));

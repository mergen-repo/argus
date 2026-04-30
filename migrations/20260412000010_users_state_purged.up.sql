-- STORY-067 Task 7: extend users.state CHECK to allow 'purged' for GDPR erasure.
-- After argusctl user purge runs, the user's PII is nulled and state becomes
-- 'purged' so downstream gates (login, session revocation) can filter.

ALTER TABLE users DROP CONSTRAINT IF EXISTS chk_users_state;

ALTER TABLE users ADD CONSTRAINT chk_users_state
  CHECK (state IN ('active','disabled','invited','purged'));

-- STORY-077 Task 2 rollback
-- Note: we keep the pg_trgm extension as it may be used by other queries.
DROP INDEX IF EXISTS idx_policy_versions_dsl_trgm;

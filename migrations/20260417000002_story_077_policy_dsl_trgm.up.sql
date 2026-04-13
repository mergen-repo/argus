-- STORY-077 Task 2 (D-007): pg_trgm extension + GIN trigram index on policy_versions.dsl_compiled
-- Enables fast APN cross-reference lookups via ILIKE on compiled DSL text.

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX IF NOT EXISTS idx_policy_versions_dsl_trgm
    ON policy_versions USING gin ((compiled_rules::text) gin_trgm_ops);

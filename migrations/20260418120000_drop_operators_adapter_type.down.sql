-- STORY-090 Wave 2 Task 3b rollback — restore operators.adapter_type
-- as NULLABLE (no NOT NULL constraint) per D2-B rollback-safety spec.
-- Pre-090 NOT NULL + CHECK constraints are intentionally NOT restored
-- here: rolling back Wave 2 on a populated DB must succeed without a
-- backfill step. Operators touched post-090 keep their nested
-- adapter_config intact; readers either tolerate NULL adapter_type
-- (post-090 paths) or pick the primary protocol from adapter_config
-- (pre-090 paths re-introduced by the rollback).

ALTER TABLE operators ADD COLUMN IF NOT EXISTS adapter_type VARCHAR(30);

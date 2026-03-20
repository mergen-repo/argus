DROP INDEX IF EXISTS idx_sessions_protocol_type;
ALTER TABLE sessions DROP COLUMN IF EXISTS slice_info;
ALTER TABLE sessions DROP COLUMN IF EXISTS protocol_type;

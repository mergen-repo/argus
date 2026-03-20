ALTER TABLE sessions ADD COLUMN IF NOT EXISTS protocol_type VARCHAR(20) NOT NULL DEFAULT 'radius';
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS slice_info JSONB;

CREATE INDEX IF NOT EXISTS idx_sessions_protocol_type ON sessions (protocol_type) WHERE session_state = 'active';

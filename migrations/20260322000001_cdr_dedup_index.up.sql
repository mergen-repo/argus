-- Add unique index for CDR deduplication
-- Same session_id + timestamp + record_type should not produce duplicate CDRs
CREATE UNIQUE INDEX IF NOT EXISTS idx_cdrs_dedup ON cdrs (session_id, timestamp, record_type);

-- Add rotation support columns to api_keys (TBL-04)
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS previous_key_hash VARCHAR(255);
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS key_rotated_at TIMESTAMPTZ;

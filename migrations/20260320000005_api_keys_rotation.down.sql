-- Remove rotation support columns from api_keys
ALTER TABLE api_keys DROP COLUMN IF EXISTS previous_key_hash;
ALTER TABLE api_keys DROP COLUMN IF EXISTS key_rotated_at;

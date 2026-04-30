-- STORY-068 Task 1: Reverse enterprise auth schema hardening
-- Drops tables and columns in reverse dependency order.

DROP TABLE IF EXISTS user_backup_codes;
DROP TABLE IF EXISTS password_history;

ALTER TABLE tenants DROP COLUMN IF EXISTS max_api_keys;
DROP INDEX IF EXISTS idx_api_keys_allowed_ips_gin;
ALTER TABLE api_keys DROP COLUMN IF EXISTS allowed_ips;

ALTER TABLE users DROP COLUMN IF EXISTS password_changed_at;
ALTER TABLE users DROP COLUMN IF EXISTS password_change_required;

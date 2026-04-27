-- Migration: 20260502000001_tenant_plan_and_quota_defaults (DOWN)
--
-- Reverses: plan column + 3 quota columns + plan index

BEGIN;

DROP INDEX IF EXISTS idx_tenants_plan;

ALTER TABLE tenants
  DROP COLUMN IF EXISTS plan,
  DROP COLUMN IF EXISTS max_sessions,
  DROP COLUMN IF EXISTS max_api_rps,
  DROP COLUMN IF EXISTS max_storage_bytes;

COMMIT;

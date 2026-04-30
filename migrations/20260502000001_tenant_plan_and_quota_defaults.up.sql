-- Migration: 20260502000001_tenant_plan_and_quota_defaults
--
-- Purpose: FIX-246 — Add tenants.plan enum column + 3 new quota columns
-- (max_sessions, max_api_rps, max_storage_bytes) with M2M-realistic defaults.
-- GREATEST semantics ensure no existing tenant quota is ever lowered (AC-6).
-- Plan backfill heuristic on max_sims (AC-8).
--
-- Plan defaults:
--   starter:    SIMs 10K,  sessions 2K,   API RPS 2K,   storage 100 GB  (107374182400)
--   standard:   SIMs 100K, sessions 20K,  API RPS 5K,   storage 1 TB    (1099511627776)
--   enterprise: SIMs 1M,   sessions 200K, API RPS 20K,  storage 10 TB   (10995116277760)

BEGIN;

-- Step 1: Add plan column with DEFAULT + CHECK (already NOT NULL via default)
ALTER TABLE tenants
  ADD COLUMN plan VARCHAR(20) NOT NULL DEFAULT 'standard'
    CONSTRAINT chk_tenants_plan CHECK (plan IN ('starter', 'standard', 'enterprise'));

-- Step 2: Add new quota columns with conservative M2M defaults (starter-tier)
ALTER TABLE tenants
  ADD COLUMN max_sessions     INTEGER NOT NULL DEFAULT 2000,
  ADD COLUMN max_api_rps      INTEGER NOT NULL DEFAULT 2000,
  ADD COLUMN max_storage_bytes BIGINT NOT NULL DEFAULT 107374182400;

-- Step 3: Backfill plan from existing max_sims (AC-8 heuristic)
UPDATE tenants SET plan = CASE
  WHEN max_sims <=  20000 THEN 'starter'
  WHEN max_sims <= 200000 THEN 'standard'
  ELSE 'enterprise'
END;

-- Step 4: Raise all quota columns to plan minimums via GREATEST — never lowers (AC-6)
UPDATE tenants SET
  max_sims          = GREATEST(max_sims,          CASE plan WHEN 'starter' THEN 10000   WHEN 'standard' THEN 100000  ELSE 1000000  END),
  max_sessions      = GREATEST(max_sessions,      CASE plan WHEN 'starter' THEN 2000    WHEN 'standard' THEN 20000   ELSE 200000   END),
  max_api_rps       = GREATEST(max_api_rps,       CASE plan WHEN 'starter' THEN 2000    WHEN 'standard' THEN 5000    ELSE 20000    END),
  max_storage_bytes = GREATEST(max_storage_bytes, CASE plan WHEN 'starter' THEN 107374182400 WHEN 'standard' THEN 1099511627776 ELSE 10995116277760 END);

-- Step 5: Index for plan-based filtering
CREATE INDEX idx_tenants_plan ON tenants (plan);

COMMIT;

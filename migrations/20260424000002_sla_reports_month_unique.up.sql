-- Migration: 20260424000002_sla_reports_month_unique
--
-- Purpose: Add a unique index on sla_reports to allow idempotent monthly rollup upserts (FIX-215).
-- operator_id is nullable (NULL = tenant-wide aggregate); COALESCE maps NULL to a sentinel UUID
-- so that the index treats each NULL as a single distinct value per window.

CREATE UNIQUE INDEX IF NOT EXISTS sla_reports_month_key
ON sla_reports (
    tenant_id,
    COALESCE(operator_id, '00000000-0000-0000-0000-000000000000'::uuid),
    window_start,
    window_end
);

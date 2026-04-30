-- FIX-207 Migration A: create session_quarantine table + retro cleanup (AC-6, prep for AC-1/AC-2)
-- Idempotent: running twice inserts no additional rows (uses NOT EXISTS guard).
-- Non-destructive: original rows copied into quarantine BEFORE deletion from hypertables.

BEGIN;

-- 1. Schema
CREATE TABLE IF NOT EXISTS session_quarantine (
    id BIGSERIAL PRIMARY KEY,
    original_table TEXT NOT NULL CHECK (original_table IN ('sessions', 'cdrs')),
    original_id TEXT NOT NULL,
    tenant_id UUID,
    violation_reason TEXT NOT NULL,
    row_data JSONB NOT NULL,
    quarantined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    quarantined_by TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_session_quarantine_table_time
  ON session_quarantine (original_table, quarantined_at DESC);
CREATE INDEX IF NOT EXISTS idx_session_quarantine_tenant
  ON session_quarantine (tenant_id, quarantined_at DESC) WHERE tenant_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_session_quarantine_reason
  ON session_quarantine (violation_reason);

-- 2. Count summary (diagnostic NOTICE)
DO $$
DECLARE
    bad_sessions INTEGER;
    bad_cdrs INTEGER;
BEGIN
    SELECT COUNT(*) INTO bad_sessions FROM sessions
      WHERE ended_at IS NOT NULL AND ended_at < started_at;
    SELECT COUNT(*) INTO bad_cdrs FROM cdrs WHERE duration_sec < 0;
    RAISE NOTICE 'FIX-207 retro: % sessions with ended_at<started_at; % cdrs with duration_sec<0',
      bad_sessions, bad_cdrs;
END $$;

-- 3. Quarantine sessions (ended_at before started_at) -- only those NOT already quarantined
INSERT INTO session_quarantine (original_table, original_id, tenant_id, violation_reason, row_data, quarantined_by)
SELECT 'sessions',
       s.id::text,
       s.tenant_id,
       'ended_before_started',
       to_jsonb(s.*),
       'fix207_retro'
FROM sessions s
WHERE s.ended_at IS NOT NULL AND s.ended_at < s.started_at
  AND NOT EXISTS (
    SELECT 1 FROM session_quarantine q
    WHERE q.original_table = 'sessions' AND q.original_id = s.id::text
      AND q.violation_reason = 'ended_before_started'
  );

-- 4. Quarantine cdrs (duration_sec < 0) -- only those NOT already quarantined
INSERT INTO session_quarantine (original_table, original_id, tenant_id, violation_reason, row_data, quarantined_by)
SELECT 'cdrs',
       c.id::text,
       c.tenant_id,
       'negative_duration',
       to_jsonb(c.*),
       'fix207_retro'
FROM cdrs c
WHERE c.duration_sec < 0
  AND NOT EXISTS (
    SELECT 1 FROM session_quarantine q
    WHERE q.original_table = 'cdrs' AND q.original_id = c.id::text
      AND q.violation_reason = 'negative_duration'
  );

-- 5. Delete quarantined source rows so CHECK constraint can be added in Migration B
--    Use EXISTS join against quarantine so we never delete rows we haven't first preserved.
DELETE FROM sessions
 WHERE ended_at IS NOT NULL AND ended_at < started_at
   AND EXISTS (
     SELECT 1 FROM session_quarantine q
     WHERE q.original_table = 'sessions' AND q.original_id = sessions.id::text
   );

DELETE FROM cdrs
 WHERE duration_sec < 0
   AND EXISTS (
     SELECT 1 FROM session_quarantine q
     WHERE q.original_table = 'cdrs' AND q.original_id = cdrs.id::text
   );

COMMIT;

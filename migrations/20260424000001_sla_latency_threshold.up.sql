-- Migration: 20260424000001_sla_latency_threshold
--
-- Purpose: Add per-operator SLA latency threshold column to operators table (FIX-215).
-- Breach detection uses this value: p95 latency > sla_latency_threshold_ms triggers a breach.
-- Default 500 ms; range 50–60000 ms enforced by CHECK constraint.
--
-- Idempotent: column ADD uses IF NOT EXISTS; constraint add guarded by pg_constraint lookup.

ALTER TABLE operators
  ADD COLUMN IF NOT EXISTS sla_latency_threshold_ms INTEGER NOT NULL DEFAULT 500;

-- Add CHECK constraint idempotently (ADD CONSTRAINT IF NOT EXISTS not supported in PG 16).
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
     WHERE conname = 'operators_sla_latency_threshold_ms_check'
       AND conrelid = 'operators'::regclass
  ) THEN
    ALTER TABLE operators
      ADD CONSTRAINT operators_sla_latency_threshold_ms_check
      CHECK (sla_latency_threshold_ms BETWEEN 50 AND 60000);
  END IF;
END;
$$;

COMMENT ON COLUMN operators.sla_latency_threshold_ms IS
  'Per-operator latency threshold (ms) — breach when p95 exceeds this (FIX-215)';

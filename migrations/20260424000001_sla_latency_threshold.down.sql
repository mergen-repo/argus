-- Migration: 20260424000001_sla_latency_threshold (DOWN)
--
-- Reverses the addition of sla_latency_threshold_ms column.
-- CHECK constraint is dropped implicitly with the column.

ALTER TABLE operators DROP COLUMN IF EXISTS sla_latency_threshold_ms;

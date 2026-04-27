-- Migration: 20260503000003_esim_profile_state_failed
--
-- Purpose: Extend chk_esim_profile_state to allow 'failed' as a valid profile state.
-- The OTA dispatcher marks a profile as 'failed' after 5 exhausted retries or terminal NACK.
-- This is a strict superset of the previous enum — existing rows remain valid.
-- FIX-235 — AC-8 (F-176: state filter Failed enum)

ALTER TABLE esim_profiles DROP CONSTRAINT IF EXISTS chk_esim_profile_state;
ALTER TABLE esim_profiles ADD CONSTRAINT chk_esim_profile_state
  CHECK (profile_state IN ('available','enabled','disabled','deleted','failed'));

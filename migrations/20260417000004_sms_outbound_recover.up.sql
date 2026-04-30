-- STORY-086 AC-2: recover sms_outbound after live-env drift.
-- Context: on the demo/live DB, schema_migrations=20260417000003 dirty=f but
-- public.sms_outbound was absent. Root cause (DEV-239): the original migration
-- 20260413000001_story_069_schema.up.sql:144 declares
-- `sim_id UUID NOT NULL REFERENCES sims(id)`, but `sims` has been partitioned
-- since day 1 (20260320000002_core_schema.up.sql) with composite PK
-- (id, operator_id). A FK to sims(id) alone is not satisfiable — PostgreSQL
-- rejects it with "no unique constraint matching given keys for referenced
-- table 'sims'". The original CREATE TABLE has therefore never succeeded.
-- This repair restores the table WITHOUT the unsatisfiable FK, consistent
-- with STORY-064's precedent (esim_profiles / ip_addresses / ota_commands
-- use `check_sim_exists` BEFORE triggers instead of FKs on partitioned sims)
-- and with sibling STORY-069 tables that do not reference sims at all.
-- A DB trigger (check_sim_exists BEFORE INSERT/UPDATE — created below)
-- enforces the sim_id → sims relationship at write time; the handler layer
-- (internal/api/sms/handler.go Send handler validates via simStore.GetByID
-- before Insert) also validates as belt-and-suspenders. Idempotent via
-- IF NOT EXISTS / DROP POLICY IF EXISTS / DROP TRIGGER IF EXISTS so it is
-- safe on fresh volumes or re-runs.

-- DDL: mirror 20260413000001_story_069_schema.up.sql:141-158 with the
-- unsatisfiable REFERENCES sims(id) clause removed.
CREATE TABLE IF NOT EXISTS sms_outbound (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    sim_id UUID NOT NULL,
    msisdn VARCHAR(20) NOT NULL,
    text_hash VARCHAR(64) NOT NULL,
    text_preview VARCHAR(80),
    status VARCHAR(20) NOT NULL DEFAULT 'queued',
    provider_message_id VARCHAR(255),
    error_code VARCHAR(50),
    queued_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sent_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_sms_outbound_tenant_sim_time ON sms_outbound (tenant_id, sim_id, queued_at DESC);
CREATE INDEX IF NOT EXISTS idx_sms_outbound_provider_id ON sms_outbound (provider_message_id) WHERE provider_message_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_sms_outbound_status ON sms_outbound (status);

-- RLS: mirror migrations/20260413000002_story_069_rls.up.sql:34-38
ALTER TABLE sms_outbound ENABLE ROW LEVEL SECURITY;
ALTER TABLE sms_outbound FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS sms_outbound_tenant_isolation ON sms_outbound;
CREATE POLICY sms_outbound_tenant_isolation ON sms_outbound
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- STORY-086 Gate F-A1/F-A3: install check_sim_exists trigger to mirror
-- STORY-064/DEV-169 precedent (esim_profiles / ip_addresses / ota_commands).
-- Without this, sim_id integrity would depend solely on handler-layer
-- validation, which the handler can satisfy but direct DB writes (jobs,
-- seeds, future callers) would bypass. Function definition is copied
-- verbatim from migrations/20260412000007_fk_integrity_triggers.up.sql:4-16
-- and is idempotent via CREATE OR REPLACE. PostgreSQL does not support
-- CREATE TRIGGER IF NOT EXISTS, so we DROP IF EXISTS + CREATE for idempotency.
CREATE OR REPLACE FUNCTION check_sim_exists()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.sim_id IS NULL THEN
        RETURN NEW;  -- nullable sim_id (e.g. ip_addresses) OK
    END IF;
    IF NOT EXISTS (SELECT 1 FROM sims WHERE id = NEW.sim_id) THEN
        RAISE EXCEPTION 'FK violation: sim_id % does not exist in sims', NEW.sim_id
            USING ERRCODE = 'foreign_key_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_sms_outbound_check_sim ON sms_outbound;
CREATE TRIGGER trg_sms_outbound_check_sim
    BEFORE INSERT OR UPDATE OF sim_id ON sms_outbound
    FOR EACH ROW EXECUTE FUNCTION check_sim_exists();

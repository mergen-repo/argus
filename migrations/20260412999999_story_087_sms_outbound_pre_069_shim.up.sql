-- STORY-087 D-032: pre-069 sims-compat shim for fresh-volume bootstrap.
-- Context: 20260413000001_story_069_schema.up.sql:144 declares an
-- unsatisfiable FK `sim_id UUID NOT NULL REFERENCES sims(id)` against
-- the LIST-partitioned `sims` table (composite PK (id, operator_id),
-- created at 20260320000002_core_schema.up.sql:275-300). A correctly-
-- sequenced fresh-volume `argus migrate up` therefore fails inside
-- 20260413000001's auto-wrapped transaction before the six sibling
-- STORY-069 tables are committed. STORY-086 shipped an idempotent
-- repair at 20260417000004_sms_outbound_recover.up.sql (table without
-- FK + check_sim_exists trigger) but that migration is never reached
-- on a fresh volume because the chain aborts at 20260413000001.
--
-- This shim pre-creates `sms_outbound` with the STORY-086-approved
-- column set (no FK on sim_id) BEFORE 20260413000001 runs. Because
-- 20260413000001:141 uses `CREATE TABLE IF NOT EXISTS`, PostgreSQL
-- emits a NOTICE and the broken column list (including line 144's
-- unsatisfiable FK) is never parsed -- the whole CREATE TABLE is a
-- no-op. 20260413000001 then installs the indexes (lines 155-157)
-- and 20260413000002 installs the RLS policy (lines 34-38).
-- 20260417000004 idempotently reconciles the trigger + any drift.
--
-- On live DBs whose schema_migrations version is >= 20260413000001,
-- golang-migrate's Up() calls sourceDrv.Next(curVersion) and never
-- visits source files with versions below curVersion. This shim is
-- therefore invisible on live DBs -- no state change, no checksum
-- drift (golang-migrate tracks version numbers only, not content
-- hashes).
--
-- DO NOT add CREATE INDEX here: 20260413000001:155-157 installs three
-- indexes WITHOUT `IF NOT EXISTS`; a duplicate here would fail on
-- fresh volumes. DO NOT add RLS policy here: `CREATE POLICY` does
-- not accept `IF NOT EXISTS`. DO NOT add check_sim_exists trigger
-- here: installed idempotently by 20260417000004:57-74.
--
-- References: ROUTEMAP D-032, decisions.md DEV-239, STORY-086 repair
-- (migrations/20260417000004_sms_outbound_recover.up.sql).

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

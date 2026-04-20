-- FIX-206 Migration B: add FK constraints to sims after Migration A cleared all orphans.
--
-- FK direction: FROM partitioned (sims) INTO non-partitioned parents
-- (operators/apns/ip_addresses). This direction is supported on LIST-partitioned
-- tables with composite PK (id, operator_id) — the FK is added on the partitioned
-- parent and cascades to every partition automatically.
-- The inverse direction (INTO sims) uses the check_sim_exists BEFORE trigger
-- instead (see migrations/20260412000007_fk_integrity_triggers.up.sql and
-- 20260417000004_sms_outbound_recover.up.sql — DEV-169 / STORY-086 precedent).
--
-- PG limitation (tested empirically on PG16):
--   `ALTER TABLE <partitioned> ADD FOREIGN KEY ... NOT VALID` fails with
--   "cannot add NOT VALID foreign key on partitioned table". So the plan's
--   intended NOT VALID + VALIDATE split is not possible here. We use plain
--   ADD CONSTRAINT (implicit validation at add time).
--
-- Online-safe rollout note (D-065 — documented in ROUTEMAP Tech Debt):
--   On fresh volume / small datasets (≤10k rows) this migration is fast and
--   fine. For production 10M-row cutover, the plain ADD CONSTRAINT below
--   holds ACCESS EXCLUSIVE for minutes and will trip deadlocks with live
--   RADIUS/Diameter traffic. Production rollout must use a per-partition
--   strategy: (a) add FK to each partition individually with NOT VALID +
--   VALIDATE (partitions are plain tables and support NOT VALID), and (b)
--   detach+reattach the partitioned parent so it inherits already-valid
--   partition FKs. See D-065 for the full runbook — out of scope for this
--   story which is backend data-integrity, not production rollout.
--
-- Runner behavior: golang-migrate/v4 postgres driver wraps the whole file in
-- a single implicit transaction. No explicit BEGIN/COMMIT needed.

-- Production foot-gun warning: the plain ADD CONSTRAINT below holds
-- ACCESS EXCLUSIVE on sims for the full validation scan. On a 10M-row table
-- this will pause live RADIUS/Diameter traffic for minutes. Follow the D-065
-- per-partition runbook before running this migration in production.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM sims LIMIT 1) AND
       (SELECT COUNT(*) FROM sims) > 100000 THEN
        RAISE WARNING 'FIX-206 Migration B: plain ADD CONSTRAINT on sims (>100k rows detected) holds ACCESS EXCLUSIVE and will stall live AAA traffic. Follow ROUTEMAP D-065 per-partition runbook for production cutover.';
    END IF;
END
$$;

-- 1. sims.operator_id -> operators(id) — RESTRICT: prevents accidental operator delete
ALTER TABLE sims
    ADD CONSTRAINT fk_sims_operator
    FOREIGN KEY (operator_id) REFERENCES operators(id) ON DELETE RESTRICT;

-- 2. sims.apn_id -> apns(id) — SET NULL: admin can delete APN; SIMs fall back to default at next session
ALTER TABLE sims
    ADD CONSTRAINT fk_sims_apn
    FOREIGN KEY (apn_id) REFERENCES apns(id) ON DELETE SET NULL;

-- 3. sims.ip_address_id -> ip_addresses(id) — SET NULL: IP release should not block
ALTER TABLE sims
    ADD CONSTRAINT fk_sims_ip_address
    FOREIGN KEY (ip_address_id) REFERENCES ip_addresses(id) ON DELETE SET NULL;

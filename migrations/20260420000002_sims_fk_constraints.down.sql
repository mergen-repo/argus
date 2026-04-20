-- FIX-206 Migration B rollback: drop the three sims FK constraints.
-- golang-migrate/v4 postgres driver wraps this file in an implicit transaction;
-- no explicit BEGIN/COMMIT needed. DROP CONSTRAINT on a partitioned parent
-- cascades to every partition automatically.

ALTER TABLE sims DROP CONSTRAINT IF EXISTS fk_sims_ip_address;
ALTER TABLE sims DROP CONSTRAINT IF EXISTS fk_sims_apn;
ALTER TABLE sims DROP CONSTRAINT IF EXISTS fk_sims_operator;

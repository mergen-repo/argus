# STORY-064: Database Hardening & Partition Automation

## User Story
As a database engineer preparing Argus for 10M+ SIMs and multi-year production operation, I want every tenant-scoping hole fixed, every partition auto-created ahead of time, every enum constrained at the DB level, and PostgreSQL Row-Level Security enforced as defense-in-depth, so that a single application bug or SQL mistake cannot leak one tenant's data to another.

## Description
DB audit flagged a Jul-2026 partition cliff (audit_logs, sim_state_history partitions pre-created only through June 2026), a `GetByIMSI` that crosses tenant boundaries without filtering, esim.go methods missing tenant_id scope, 9+ enum columns without CHECK constraints, a missing `operator_grants.supported_rat_types` column, 3 undocumented supporting tables, no Row-Level Security policies, and several frequently-queried stores without cursor pagination.

## Architecture Reference
- Services: SVC-03 (Core API), SVC-10 (Audit)
- Packages: internal/store/*, migrations/, internal/model/
- Source: Phase 10 DB audit (6-agent scan 2026-04-11)
- Spec: docs/architecture/db/_index.md, docs/architecture/db/platform-services.md

## Screen Reference
- SCR-120 (System Health — partition status widget)
- Invisible for RLS and scoping fixes, but feeds all tenant-isolated screens.

## Acceptance Criteria
- [ ] AC-1: **Tenant scoping fixes**:
  - `store/sim.go:734` `GetByIMSI(imsi, tenantID)` — signature takes tenantID, WHERE filters on both
  - `store/sim.go` `GetByICCID(iccid, tenantID)` — same
  - `store/esim.go` `GetBySimID` and all other Get/Update/Delete methods take tenantID and scope on it (via JOIN sims OR direct column if we add one)
  - Audit sweep: grep every `QueryRow` and `Query` in store/*.go for missing tenant_id in WHERE; fix all violations
- [ ] AC-2: **Partition auto-creation** for `audit_logs`, `sim_state_history`, `sessions`, any other time-partitioned table. Implementation choice: pg_partman extension (preferred) OR Go-side cron job that runs monthly and calls `CREATE TABLE IF NOT EXISTS <parent>_YYYYMM PARTITION OF <parent> FOR VALUES FROM ... TO ...`. Pre-creates next 3 months ahead. Runs nightly as part of cron scheduler.
- [ ] AC-3: **Enum CHECK constraints migration** (`20260411000001_enum_constraints.up.sql`). Adds CHECK constraints to enum columns where missing:
  - `tenants.state IN ('active','suspended','terminated')`
  - `users.role IN ('super_admin','tenant_admin','sim_manager','policy_editor','compliance_officer','auditor','api_user')`
  - `users.state IN ('active','disabled','invited')`
  - `sims.state IN ('ordered','active','suspended','terminated','stolen_lost','available')` (+ any from STORY-061)
  - `sims.sim_type IN ('physical','esim')`
  - `apns.state IN ('active','archived')`
  - `policies.state IN ('active','disabled','archived')`
  - `policy_versions.state IN ('draft','active','rolling_out','superseded','archived')`
  - `operators.state IN ('active','disabled')`
- [ ] AC-4: **operator_grants.supported_rat_types column added** (migration `20260411000002_operator_grants_rat_types.up.sql`). `ALTER TABLE operator_grants ADD COLUMN supported_rat_types TEXT[] NOT NULL DEFAULT '{}'` + GIN index. Store methods `ListGrantsWithOperators` and friends return the field. SoR engine consults grant-level RAT types first, operator-level as fallback.
- [ ] AC-5: **Row-Level Security (RLS) policies** enabled on multi-tenant tables. Migration `20260411000003_rls_policies.up.sql` enables RLS and adds policies for: `sims`, `apns`, `ip_pools`, `ip_addresses`, `sessions` (via sim_id JOIN), `cdrs`, `policies`, `policy_assignments`, `policy_rollouts`, `jobs`, `notifications`, `notification_configs`, `sim_segments`, `esim_profiles`, `ota_commands`, `anomalies`, `policy_violations`, `user_sessions`. Session user sets `SET LOCAL app.current_tenant = '<uuid>'` from gateway middleware; policies use `current_setting('app.current_tenant')::uuid`. super_admin role bypasses via explicit SET.
- [ ] AC-6: **Undocumented tables documented**: `s3_archival_log`, `tenant_retention_config`, `policy_violations`. Add to `docs/architecture/db/_index.md` table listing and create entries in the appropriate domain doc (`platform-services.md` or new). Create Go models + minimal store if missing. Fold into STORY-062 doc drift if easier.
- [ ] AC-7: **TBL-26 (ota_commands) + TBL-27 (anomalies) + TBL-28 (sla_reports from STORY-063)** added to `docs/architecture/db/_index.md` table listing. Renumber consistently.
- [ ] AC-8: **Cursor pagination** added to currently-unpaginated stores that are queried with growing datasets:
  - `notifications` store `List()` — cursor by `(created_at DESC, id DESC)`
  - `jobs` store `List()` — same
  - `user_sessions` store `List()` — same
  - `notification_configs` store `List()` — (limit 100 hard cap acceptable)
  - `api_keys` store `List()` — same
  - Handlers return `next_cursor` in meta
- [ ] AC-9: **Missing FK constraints** added where referential integrity matters and partitioning allows:
  - `esim_profiles.sim_id` — deferred FK to `sims` (with partitioning workaround: trigger-based integrity check if hard FK impossible)
  - `ip_addresses.sim_id` — same
  - `ota_commands.sim_id` — same (if not already)
- [ ] AC-10: **Composite indexes on hot paths** audited:
  - `sessions (sim_id, started_at DESC)` — for per-SIM session query (API-051)
  - `cdrs (sim_id, timestamp DESC)` — for per-SIM usage query (API-052)
  - `notifications (tenant_id, created_at DESC)` — unfiltered list
  - `audit_logs (tenant_id, created_at DESC)` — unfiltered list
  - Run `EXPLAIN ANALYZE` on each newly-indexed query, document in migration comment
- [ ] AC-11: **SELECT *** — grep `store/*.go` for any remaining `SELECT *` and replace with explicit columns. Compiler-like strictness.

## Dependencies
- Blocked by: STORY-056 (some fixes overlap), STORY-063 (session DB coverage)
- Blocks: Phase 10 Gate

## Test Scenarios
- [ ] Unit: `GetByIMSI(imsi, tenantA)` returns nil when SIM belongs to tenantB. `GetByIMSI(imsi, tenantB)` returns the SIM.
- [ ] Integration: Cron runs partition creator → next 3 months of audit_logs partitions exist in pg_partitions.
- [ ] Integration: INSERT into `sims` with `state='foo'` → PostgreSQL rejects with CHECK violation.
- [ ] Integration: operator_grant created with `supported_rat_types='{5G_SA,LTE}'` → SoR decision for 4G session on that operator respects grant RAT types.
- [ ] Integration: Connect as tenantA, `SELECT * FROM sims` → only tenantA rows returned (RLS enforced). Without SET LOCAL → empty set.
- [ ] Integration: `GET /api/v1/notifications?limit=50` → returns `next_cursor` when >50 exist; subsequent request with cursor returns next page.
- [ ] Load: Per-SIM session query on table with 10M sessions → < 50ms with composite index (EXPLAIN Index Scan, not Seq Scan).
- [ ] Migration: `make db-migrate && make db-rollback && make db-migrate` round-trips cleanly through the new migrations.

## Effort Estimate
- Size: L
- Complexity: High (RLS is careful work; partition automation is production-critical)

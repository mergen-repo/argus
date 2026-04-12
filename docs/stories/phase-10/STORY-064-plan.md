# Implementation Plan: STORY-064 — Database Hardening & Partition Automation

## Goal
Close every tenant-scoping hole in the store layer, automate partition creation via the existing Go cron scheduler, add DB-level CHECK constraints and RLS policies as defense-in-depth, document the three orphan tables + TBL-26/27/28 in db/_index.md, finish cursor pagination on notification_configs/user_sessions, and add the few missing composite indexes + operator_grants.supported_rat_types column — all without breaking the RADIUS hot path.

## Architecture Context

### Components Involved
- `internal/store/sim.go` — SIMStore methods (`GetByIMSI`, new `GetByIMSIScoped`)
- `internal/store/esim.go` — ESimProfileStore (`GetEnabledProfileForSIM`, `CountBySIM`, `Create`)
- `internal/store/notification.go` — NotificationConfigStore (`ListByUser` → cursor)
- `internal/store/user.go` — SessionStore (`GetActiveByUserID` → cursor)
- `internal/store/operator.go` — OperatorStore (grant-level `supported_rat_types`)
- `internal/store/postgres.go` — pgxpool lifecycle (RLS role config note)
- `internal/operator/` (SoR engine) — consumes grant-level RAT types
- `internal/job/scheduler.go` — CronEntry registration (partition creator entry point)
- `internal/job/partition_creator.go` — NEW partition automation job
- `cmd/argus/main.go` — scheduler registration of partition creator
- `migrations/20260412000003..008_*.up.sql` — new DDL migrations
- `docs/architecture/db/_index.md`, `docs/architecture/db/platform-services.md`, `docs/architecture/db/aaa-analytics.md` — doc updates

### Data Flow — RLS defense-in-depth
```
HTTP request
  → gateway/auth_middleware JWTAuth (extracts tenantID into ctx)
  → store methods (pgxpool.Pool) run SQL with WHERE tenant_id = $1 (application-enforced, primary)
  → PostgreSQL RLS policies exist on all multi-tenant tables as BACKSTOP
  → App DB role holds BYPASSRLS → app queries unaffected at runtime
  → Ad-hoc psql / reporting tools / misconfigured roles BLOCKED by RLS
```

**Why BYPASSRLS?** `SET LOCAL app.current_tenant` only works inside an explicit transaction. The existing store layer uses `pool.QueryRow(ctx, ...)` directly — refactoring every method to tx-scoped sessions is a multi-week change outside this story's scope. Phase 10 RLS is **defense-in-depth against non-app access paths**, not a replacement for the WHERE clauses. See ADR-decision (DEV-166) below. Enforced per-request RLS is filed as a FUTURE item, not this story.

### Data Flow — Partition auto-creation
```
job/scheduler.go cron tick (daily 02:00 UTC)
  → partition_creator.CreateNext(3 months ahead)
  → for each of {audit_logs, sim_state_history}:
      - compute next 3 months
      - SELECT 1 FROM pg_class WHERE relname = '<parent>_<yyyy>_<mm>'
      - if missing: CREATE TABLE IF NOT EXISTS ... PARTITION OF ... FOR VALUES FROM ... TO ...
  → emit log + metric; optionally publish NATS event
  → sessions, cdrs, operator_health_logs = TimescaleDB hypertables → NOT touched (TimescaleDB auto-creates chunks)
```

### API Specifications (scope of story)
No new HTTP endpoints. The only API-facing change is that paginated list handlers for **notification_configs** and **user_sessions** gain `next_cursor` in the response meta envelope:

- `GET /api/v1/notifications/configs` — existing handler; response meta now includes `next_cursor`
- `GET /api/v1/auth/sessions` (user_sessions list) — existing handler; response meta now includes `next_cursor`

Standard envelope unchanged: `{ status: "success", data: [...], meta: { next_cursor: "..."|null } }`.

### Database Schema (source-of-truth verification)

Source: `migrations/20260320000002_core_schema.up.sql` (ACTUAL).

**Partition cliff (audit_logs, sim_state_history)**:
Both declared `PARTITION BY RANGE (created_at)` with partitions only pre-created for `2026_03..2026_06`. After July 2026 INSERTs would fail. Fix: automation task + bootstrap migration to pre-create 2026_07..2027_03.

**sims table** is `PARTITION BY LIST (operator_id)` with composite PK `(id, operator_id)`. Hard FK from `esim_profiles.sim_id → sims(id)` is **impossible** because `sims(id)` is not a unique key on its own (PK is composite). Workaround: trigger-based integrity check.

**operator_grants** (ACTUAL columns):
```sql
id UUID PK, tenant_id UUID, operator_id UUID, enabled BOOLEAN,
granted_at TIMESTAMPTZ, granted_by UUID
-- NO supported_rat_types column yet; operators table has it (line 106)
```
AC-4 adds `supported_rat_types TEXT[] NOT NULL DEFAULT '{}'` + GIN index. SoR engine prefers grant-level when non-empty, falls back to operator-level.

**esim_profiles** (ACTUAL, post-STORY-061):
```sql
id, sim_id, eid, sm_dp_plus_id, profile_id (VARCHAR 64), operator_id,
profile_state CHECK IN ('available','enabled','disabled','deleted'),
iccid_on_profile, last_provisioned_at, last_error, created_at, updated_at
-- UNIQUE INDEX idx_esim_profiles_sim_enabled WHERE profile_state='enabled'
-- No tenant_id column (scope derived via JOIN sims)
```

**ota_commands** (ACTUAL): has `tenant_id UUID REFERENCES tenants(id)` and `sim_id UUID` (no FK due to partitioned sims). Already has `command_type`, `channel`, `status`, `security_mode` CHECK constraints.

**anomalies** (ACTUAL, migration `20260322000003_anomalies.up.sql`): already has CHECKs on `type`, `severity`, `state`. Needs doc entry only.

**s3_archival_log** / **tenant_retention_config** (ACTUAL, migration `20260323000001_data_optimization.up.sql`): plain tables, both tenant-scoped, not in docs. Need doc entries.

**policy_violations** (ACTUAL, migration `20260324000001_policy_violations.up.sql`): tenant-scoped, not in docs. Needs doc entry.

### Enum CHECK constraint inventory (AC-3)
Verified against `migrations/20260320000002_core_schema.up.sql`. Missing CHECKs:
- `tenants.state` — VARCHAR(20), default 'active', NO CHECK → add
- `users.role` — VARCHAR(30), NO CHECK → add (7 roles)
- `users.state` — VARCHAR(20), default 'active', NO CHECK → add
- `sims.state` — VARCHAR(20), default 'ordered', NO CHECK → add
- `sims.sim_type` — VARCHAR(10), default 'physical', NO CHECK → add
- `apns.state` — VARCHAR(20), default 'active', NO CHECK → add
- `policies.state` — VARCHAR(20), default 'active', NO CHECK → add
- `policy_versions.state` — VARCHAR(20), default 'draft', NO CHECK → add
- `operators.state` — VARCHAR(20), default 'active', NO CHECK → add

Already has CHECK: `anomalies.{type,severity,state}`, `ota_commands.{command_type,channel,status,security_mode}`, `esim_profiles.profile_state`.

### Migration numbering
Last migration on disk: `20260412000002_esim_multiprofile`. This story's sequence starts at **`20260412000003`** and runs sequentially.

Planned files:
1. `20260412000003_enum_check_constraints.up.sql` / `.down.sql` (AC-3)
2. `20260412000004_operator_grants_rat_types.up.sql` / `.down.sql` (AC-4)
3. `20260412000005_partition_bootstrap.up.sql` / `.down.sql` — pre-create audit_logs + sim_state_history partitions for 2026_07..2027_03 (9 months ahead of schedule, covers bootstrap until first cron run)
4. `20260412000006_rls_policies.up.sql` / `.down.sql` (AC-5)
5. `20260412000007_fk_integrity_triggers.up.sql` / `.down.sql` (AC-9)
6. `20260412000008_composite_indexes.up.sql` / `.down.sql` (AC-10)

### RLS Policy SQL Skeleton (AC-5)
```sql
-- For each tenant-scoped table, enable RLS and attach an ALL-policy bound to
-- current_setting('app.current_tenant')::uuid. App role has BYPASSRLS so it is
-- not affected at runtime; policies act as backstop for non-app database users.

ALTER TABLE sims ENABLE ROW LEVEL SECURITY;
CREATE POLICY sims_tenant_isolation ON sims
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- Repeat for: apns, ip_pools, ip_addresses (via JOIN since no tenant_id column),
-- sessions, cdrs, policies, policy_versions, policy_assignments (via JOIN on sim_id),
-- policy_rollouts (via JOIN on policy_version_id), jobs, notifications,
-- notification_configs, sim_segments, esim_profiles (via JOIN on sim_id),
-- ota_commands, anomalies, policy_violations, user_sessions (via JOIN on user_id),
-- s3_archival_log, tenant_retention_config, operator_grants, msisdn_pool, sim_state_history

-- Tables without a direct tenant_id column get a USING clause that JOINs:
CREATE POLICY ip_addresses_tenant_isolation ON ip_addresses
    USING (pool_id IN (SELECT id FROM ip_pools WHERE tenant_id = current_setting('app.current_tenant', true)::uuid));

-- Application role explicitly bypasses (created at bootstrap; in dev uses the default superuser which already bypasses):
-- ALTER ROLE argus_app BYPASSRLS; -- documented in CONFIG.md, NOT in migration (role mgmt out of migration scope)
```

The migration DOES NOT ALTER the role — role grants are out-of-band in the `deploy/` Docker setup (and dev uses the default superuser which has BYPASSRLS implicitly). The migration only enables RLS + creates policies.

### Composite Index SQL Skeleton (AC-10)
```sql
-- Verify existing indexes first (already present per migration 20260323000002_perf_indexes):
--   idx_sims_tenant_created (sims tenant+created_at DESC)
--   idx_anomalies_tenant_detected
--   idx_audit_tenant_time (audit_logs tenant+created_at DESC)  [core_schema]
--   idx_notifications_tenant_time (notifications tenant+created_at DESC)  [core_schema]

-- New / verified-needed:
CREATE INDEX IF NOT EXISTS idx_sessions_sim_started
    ON sessions (sim_id, started_at DESC);

CREATE INDEX IF NOT EXISTS idx_cdrs_sim_timestamp
    ON cdrs (sim_id, timestamp DESC);

-- Document EXPLAIN ANALYZE output in migration comment header block.
```

### Partition Creator Go Skeleton (AC-2, no code in plan — structural guide only)
```
internal/job/partition_creator.go
  type PartitionCreator struct { db *pgxpool.Pool; log zerolog.Logger }
  NewPartitionCreator(db, log) *PartitionCreator
  Run(ctx, monthsAhead int) error
    - for each parent in {"audit_logs","sim_state_history"}:
        - for m in 0..monthsAhead:
            - compute start = first-of-month N months from now
            - compute end   = first-of-month N+1 months from now
            - name = parent + "_" + YYYY_MM
            - if !exists: CREATE TABLE IF NOT EXISTS <name> PARTITION OF <parent>
                          FOR VALUES FROM ('<start>') TO ('<end>')
            - log created/skipped
```
Scheduler wiring: `scheduler.AddEntry(CronEntry{Name: "partition_creator", Schedule: "0 2 * * *", JobType: "partition_create", ...})` registered in `cmd/argus/main.go` after NewScheduler. The scheduler already implements cron-style ticker; JobType `partition_create` dispatches to `partitionCreator.Run(ctx, 3)`.

## Prerequisites
- [x] STORY-056 (some overlap closed) — completed
- [x] STORY-063 (session DB coverage) — completed; TBL-28 sla_reports already added to _index.md
- [x] Existing migrations through `20260412000002_esim_multiprofile`
- [x] `internal/job/scheduler.go` cron framework (STORY-031) ready for new entries

## Design Decisions (new, to append to decisions.md post-gate)

- **DEV-166** `GetByIMSI` unscoped remains (RADIUS hot path per DEV-041). A new `GetByIMSIScoped(ctx, imsi, tenantID)` is added for API callers. Any future API endpoint that looks up a SIM by IMSI must use the scoped variant. `GetByIMSI` callsites audited: only RADIUS (`internal/aaa/radius/*`) and Diameter (`internal/aaa/diameter/*`) use it — both intentional.
- **DEV-167** Phase 10 RLS applies BYPASSRLS to app role; RLS is defense-in-depth for ad-hoc DB access only. Enforced per-request RLS (SET LOCAL per tx) deferred to a FUTURE item.
- **DEV-168** Partition automation uses the existing Go cron scheduler, not pg_partman. Rationale: zero new extension, reuses `internal/job/scheduler.go`, portable to managed-DB providers that don't allow extensions. Tradeoff: single-node cron (not distributed) — acceptable given the job is idempotent (`CREATE TABLE IF NOT EXISTS`) and there's only one Argus instance per deployment.
- **DEV-169** `esim_profiles.sim_id`, `ip_addresses.sim_id`, `ota_commands.sim_id` get trigger-based integrity enforcement instead of hard FKs, because `sims` is LIST-partitioned by operator_id with composite PK (hard cross-partition FK impossible in PostgreSQL).
- **DEV-170** `GetByICCID` does not exist in the codebase. Nothing to fix; audit confirmed.

## Tasks

### Task 1: Enum CHECK constraints migration
- **Files:** Create `migrations/20260412000003_enum_check_constraints.up.sql`, `.down.sql`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `migrations/20260322000003_anomalies.up.sql` — uses inline `CHECK (col IN (...))`. For ALTER TABLE variant, use `ALTER TABLE t ADD CONSTRAINT chk_t_col CHECK (col IN (...))`.
- **Context refs:** "Enum CHECK constraint inventory (AC-3)"
- **What:** Add 9 CHECK constraints listed in inventory. Use `ADD CONSTRAINT chk_<table>_<col>`. Verify seed data compatibility with a guard SELECT before applying (fail-fast pattern: if `SELECT count(*) FROM sims WHERE state NOT IN (...)` > 0 then RAISE). Down migration drops the constraints.
- **Verify:** `make db-migrate && psql -c "\d sims" | grep chk_sims_state`

### Task 2: operator_grants.supported_rat_types column + index
- **Files:** Create `migrations/20260412000004_operator_grants_rat_types.up.sql`, `.down.sql`; Modify `internal/store/operator.go` (`ListGrantsWithOperators`, Create/Update grant methods, `GrantWithOperator` struct); Modify `internal/operator/` SoR engine to prefer grant-level when non-empty
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `migrations/20260321000001_sor_fields.up.sql` for ALTER TABLE pattern. Read `internal/store/operator.go:650..` for `ListGrantsWithOperators` query shape.
- **Context refs:** "Database Schema > operator_grants", "Design Decisions > DEV-168 (N/A)"
- **What:** `ALTER TABLE operator_grants ADD COLUMN supported_rat_types TEXT[] NOT NULL DEFAULT '{}'`. Create `idx_operator_grants_rat_types_gin USING gin (supported_rat_types)`. Store: include column in SELECT, INSERT, UPDATE statements and the `GrantWithOperator` struct. SoR engine: in the grant-selection path, `if len(grant.SupportedRATTypes) > 0 { use grant } else { fallback to operator.SupportedRATTypes }`. Update the one call site that builds the operator-RAT intersection.
- **Verify:** `go test ./internal/store/... -run Grant && go test ./internal/operator/...`

### Task 3: Partition bootstrap migration (AC-2 part 1)
- **Files:** Create `migrations/20260412000005_partition_bootstrap.up.sql`, `.down.sql`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `migrations/20260320000002_core_schema.up.sql:461..468` — follow existing `CREATE TABLE IF NOT EXISTS audit_logs_YYYY_MM PARTITION OF audit_logs FOR VALUES FROM ('YYYY-MM-01') TO ('YYYY-MM+1-01')` pattern.
- **Context refs:** "Data Flow — Partition auto-creation", "Database Schema > Partition cliff"
- **What:** For parents `audit_logs` and `sim_state_history`, pre-create partitions for months 2026_07 through 2027_03 (9 months). This bootstraps the cliff and gives the cron time to take over without a gap. Down migration detaches & drops the partitions created by this migration.
- **Verify:** `psql -c "\d+ audit_logs" | grep 2027_03`

### Task 4: Go partition creator job + scheduler wiring (AC-2 part 2)
- **Files:** Create `internal/job/partition_creator.go`, `internal/job/partition_creator_test.go`; Modify `cmd/argus/main.go` (register CronEntry); Modify `internal/job/scheduler.go` if new JobType dispatch needed
- **Depends on:** Task 3
- **Complexity:** high
- **Pattern ref:** Read `internal/job/scheduler.go` for CronEntry structure. Read `internal/job/timeout.go` for a concrete job implementation pattern (context, logger, error-wrapping).
- **Context refs:** "Partition Creator Go Skeleton", "Data Flow — Partition auto-creation"
- **What:** Implement `PartitionCreator.Run(ctx, monthsAhead)` that idempotently creates the next N months of partitions for `audit_logs` and `sim_state_history`. Wire scheduler entry `{Name: "partition_creator", Schedule: "0 2 * * *", JobType: "partition_create"}`. The scheduler dispatches by JobType; add dispatch arm if missing. Unit test uses a table-driven approach: (a) no existing partition → creates it; (b) existing partition → skip; (c) failing SQL → returns error. Integration test requires live pg; use a mock in unit test or skip-in-short.
- **Verify:** `go test ./internal/job/... -run Partition && go vet ./...`

### Task 5: Tenant scoping fixes (AC-1) — sim.go + esim.go — CAREFUL
- **Files:** Modify `internal/store/sim.go` (add `GetByIMSIScoped`); Modify `internal/store/esim.go` (`GetEnabledProfileForSIM`, `CountBySIM`, `Create` all take tenantID and scope via JOIN sims); Modify all callers of `CountBySIM`, `Create`, `GetEnabledProfileForSIM` in `internal/api/esim/handler.go` and `internal/job/bulk_esim_switch.go`; Modify `internal/store/esim_test.go` and any handler tests that mock these methods
- **Depends on:** —
- **Complexity:** high
- **Pattern ref:** Read `internal/store/esim.go:88..103` (`GetByID`) — the tenant-scoped JOIN pattern. Read `internal/store/sim.go:770..783` (`GetByIMSI`) — the unscoped version we are preserving.
- **Context refs:** "Design Decisions > DEV-166", "Design Decisions > DEV-170"
- **What:**
  1. **Add** (not replace) `SIMStore.GetByIMSIScoped(ctx, imsi, tenantID)` with `WHERE imsi = $1 AND tenant_id = $2`. Keep `GetByIMSI` unchanged per DEV-041/DEV-166. Audit callers: RADIUS + Diameter stay on unscoped; no API caller exists today so nothing to switch.
  2. **Modify** `ESimProfileStore.GetEnabledProfileForSIM(ctx, tenantID, simID)` — add tenantID parameter; query becomes `SELECT ... FROM esim_profiles ep JOIN sims si ON ep.sim_id = si.id WHERE ep.sim_id = $1 AND si.tenant_id = $2 AND ep.profile_state = 'enabled'`. Update caller in `internal/job/bulk_esim_switch.go:118`.
  3. **Modify** `ESimProfileStore.CountBySIM(ctx, tenantID, simID)` — query becomes `SELECT COUNT(*) FROM esim_profiles ep JOIN sims si ON ep.sim_id = si.id WHERE ep.sim_id = $1 AND si.tenant_id = $2 AND ep.profile_state != 'deleted'`. Update caller in `internal/api/esim/handler.go:577`.
  4. **Modify** `ESimProfileStore.Create(ctx, tenantID, params)` — wrap INSERT in a pre-check: `SELECT 1 FROM sims WHERE id = $1 AND tenant_id = $2`; if not found, return `ErrSIMNotFound`. Update caller in `internal/api/esim/handler.go:611` which already has tenantID in scope from the auth context.
  5. **Audit sweep**: grep every `s.db.QueryRow` and `s.db.Query` in `internal/store/*.go` for missing tenant_id. Any violation goes into this task's patch. Known-safe exceptions (document in commit): `GetByIMSI` (DEV-041), `GetEnabledProfileForSIM` by jobs that already loaded sim through tenant-scoped list (but we're fixing it anyway as defense-in-depth).
  6. **Update tests** in `internal/store/esim_test.go`, `internal/api/esim/handler_test.go`, `internal/job/bulk_esim_switch_test.go` (if present) for new signatures.
- **Verify:** `go build ./... && go test ./internal/store/... ./internal/api/esim/... ./internal/job/...` — all green
- **Warning:** this task touches 5-6 files but is functionally ONE concern (signature propagation). Do NOT split further — it would break the build mid-wave. Dispatch as a single coherent unit.

### Task 6: Cursor pagination for notification_configs + user_sessions (AC-8)
- **Files:** Modify `internal/store/notification.go` (`NotificationConfigStore.ListByUser` → cursor); Modify `internal/store/user.go` (`SessionStore.GetActiveByUserID` → add cursor variant `ListActiveByUserID` returning next_cursor); Modify `internal/api/auth/handler.go` and `internal/api/notification/handler.go` to return `next_cursor` in meta; Update tests
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/notification.go:126` (`NotificationStore.ListByUser`) — already has cursor pagination; copy the `(created_at DESC, id DESC)` cursor shape.
- **Context refs:** "API Specifications"
- **What:** Add `cursor string, limit int` params. Hard cap limit=100. Use tuple cursor `base64(created_at|id)`. Return `(results, nextCursor, error)`. Handlers wrap `next_cursor` in `meta`. For `notification_configs` (limit 100 hard cap is acceptable per AC-8), simple id-based cursor is fine. For `user_sessions`, use (created_at DESC, id DESC).
- **Verify:** `go test ./internal/store/... ./internal/api/...`

### Task 7: RLS policies migration (AC-5)
- **Files:** Create `migrations/20260412000006_rls_policies.up.sql`, `.down.sql`; Modify `docs/architecture/CONFIG.md` or create new `docs/architecture/db/rls.md` documenting the BYPASSRLS rationale + future enforcement path
- **Depends on:** Task 1 (enums in place to avoid policy/constraint deadlock)
- **Complexity:** high
- **Pattern ref:** First of its kind in this codebase. Use Postgres docs + the RLS skeleton in this plan. Structure: for each table, `ALTER TABLE t ENABLE ROW LEVEL SECURITY;` then `CREATE POLICY t_tenant_isolation ON t USING (...);`. Down migration: `DROP POLICY` + `ALTER TABLE t DISABLE ROW LEVEL SECURITY`.
- **Context refs:** "Data Flow — RLS defense-in-depth", "RLS Policy SQL Skeleton", "Design Decisions > DEV-167"
- **What:** Enable RLS + add tenant-isolation policy on all 20+ tenant-scoped tables (full list in the RLS skeleton section). Tables without a direct `tenant_id` column use a `USING` clause that JOINs to the parent (e.g., `ip_addresses` via `ip_pools`, `esim_profiles` via `sims`, `user_sessions` via `users`, `policy_assignments` via `policy_versions`, `policy_rollouts` via `policy_versions`). The migration DOES NOT manipulate DB roles — BYPASSRLS is configured out-of-band in deploy manifests (documented in rls.md). Test via integration: connect as a role WITHOUT BYPASSRLS, `SET app.current_tenant = '<uuidA>'`, `SELECT FROM sims` returns only tenantA rows; without the setting, returns empty set.
- **Verify:** `make db-migrate && psql -c "SELECT count(*) FROM pg_policies WHERE policyname LIKE '%_tenant_isolation'"` should return ≥20

### Task 8: FK integrity triggers for sim_id (AC-9)
- **Files:** Create `migrations/20260412000007_fk_integrity_triggers.up.sql`, `.down.sql`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `migrations/20260320000002_core_schema.up.sql:602..` — existing `update_updated_at_column` trigger function for the general `CREATE FUNCTION ... CREATE TRIGGER` shape.
- **Context refs:** "Database Schema > Partition cliff", "Design Decisions > DEV-169"
- **What:** Create PL/pgSQL function `check_sim_exists(sim_id uuid)` that returns BOOLEAN (`SELECT EXISTS(SELECT 1 FROM sims WHERE id = sim_id)`). Create BEFORE INSERT OR UPDATE OF sim_id triggers on `esim_profiles`, `ip_addresses`, `ota_commands` that RAISE EXCEPTION if `check_sim_exists` returns false. This is the partitioned-FK workaround. Note: `sims` is LIST-partitioned on `operator_id`, so `EXISTS(SELECT 1 FROM sims WHERE id = ...)` scans all partitions but the PG optimizer uses `idx_sims_iccid` / `idx_sims_imsi` indirectly — document that this check is O(partitions). Skip the check on NULL sim_id (ip_addresses.sim_id is nullable).
- **Verify:** Insert with bogus sim_id → RAISE EXCEPTION; insert with valid sim_id → success.

### Task 9: Composite indexes on hot paths (AC-10)
- **Files:** Create `migrations/20260412000008_composite_indexes.up.sql`, `.down.sql`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `migrations/20260323000002_perf_indexes.up.sql` — uses `CREATE INDEX CONCURRENTLY IF NOT EXISTS` with rich comment headers explaining the query each index serves.
- **Context refs:** "Composite Index SQL Skeleton"
- **What:** Add `idx_sessions_sim_started (sim_id, started_at DESC)` and `idx_cdrs_sim_timestamp (sim_id, timestamp DESC)`. Verify `idx_audit_tenant_time` and `idx_notifications_tenant_time` already exist (they do — confirm with `\di`). Run `EXPLAIN ANALYZE` on `SELECT * FROM sessions WHERE sim_id = $1 ORDER BY started_at DESC LIMIT 10` pre-and-post; paste the plan (Index Scan, not Seq Scan) into the migration header comment.
- **Verify:** `psql -c "EXPLAIN ANALYZE SELECT * FROM sessions WHERE sim_id = '00000000-0000-0000-0000-000000000000' ORDER BY started_at DESC LIMIT 10"` shows Index Scan on the new index.

### Task 10: Documentation — db/_index.md + platform-services.md + aaa-analytics.md
- **Files:** Modify `docs/architecture/db/_index.md` (add TBL-29 anomalies, TBL-30 policy_violations, TBL-31 s3_archival_log, TBL-32 tenant_retention_config — TBL-26 ota_commands, TBL-27 sla_reports, TBL-28 sim_segments already listed); Modify `docs/architecture/db/platform-services.md` (add TBL-29 anomalies under Analytics/Anomalies domain or new section, TBL-30 policy_violations, TBL-31 s3_archival_log, TBL-32 tenant_retention_config); Modify `docs/architecture/db/aaa-analytics.md` if better fit for anomalies
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `docs/architecture/db/platform-services.md:1..60` — uses header + column table + Indexes + Partitioning sections per table.
- **Context refs:** "Database Schema" section of this plan, each table's source migration
- **What:** Add four new TBL entries to `_index.md` table listing. Numbering: continue from TBL-28 (sla_reports from STORY-063, already in _index). New numbers: **TBL-29 anomalies**, **TBL-30 policy_violations**, **TBL-31 s3_archival_log**, **TBL-32 tenant_retention_config**. Update the Domain Detail Files table to reference these. For each, write column table + indexes section sourced from the actual migration SQL embedded earlier in this plan. Note: AC-7 asks for "TBL-26 ota_commands + TBL-27 anomalies + TBL-28 sla_reports" — sla_reports already took TBL-27 from STORY-063, and ota_commands took TBL-26 (both verified in _index.md). The "renumber consistently" instruction means: anomalies gets the next free slot (TBL-29), not TBL-27.
- **Verify:** `grep -c "^| TBL-" docs/architecture/db/_index.md` returns 32; each new table appears in both _index.md and a domain detail file.

### Task 11: SELECT * audit (AC-11)
- **Files:** (verification only — no changes expected)
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** N/A — this is a verification task
- **Context refs:** —
- **What:** Run `grep -RIn "SELECT \*" internal/store/` and `grep -RIn "SELECT \*" internal/` — if zero matches (current state), add a CI-guard note to `docs/architecture/TESTING.md` or the `Makefile` (new `make lint-sql` target that fails if `SELECT *` appears in store/). If any matches are found, replace with explicit column lists (each occurrence = separate edit). Document result in step log.
- **Verify:** `grep -RIn "SELECT \*" internal/store/ internal/api/` returns zero matches.

### Task 12: Decisions log append (post-wave, part of commit step not dev wave)
- **Files:** Modify `docs/brainstorming/decisions.md`
- **Depends on:** Task 1–11
- **Complexity:** low
- **Pattern ref:** Read existing DEV-1xx entries in `docs/brainstorming/decisions.md` — append five new rows (DEV-166 through DEV-170) to the table.
- **Context refs:** "Design Decisions" section of this plan
- **What:** Append DEV-166..170 to the decisions table with the text from this plan's Design Decisions section.
- **Verify:** `grep -c "DEV-17[0]" docs/brainstorming/decisions.md` returns 1

## Wave Organization

Tasks are grouped so each wave is internally parallelizable and waves are sequentially gated.

- **Wave 1 (parallel)** — Pure DDL migrations with no code dependencies:
  - Task 1 (enum CHECKs)
  - Task 2 (operator_grants column — DDL part; Go code part queued serially below)
  - Task 3 (partition bootstrap)
  - Task 8 (FK triggers)
  - Task 9 (composite indexes)
  - Task 11 (SELECT * audit — verification only)

- **Wave 2 (parallel)** — Go code changes (after Wave 1 migrations land):
  - Task 4 (partition creator Go + scheduler wiring) — depends on Task 3
  - Task 5 (tenant scoping fixes — sim.go + esim.go — single coherent dispatch) — independent
  - Task 6 (cursor pagination) — independent
  - Task 2 finish (SoR engine grant-RAT consumption) — depends on Task 2 DDL

- **Wave 3 (parallel)** — Backstop + docs:
  - Task 7 (RLS policies migration) — depends on Task 1 (enums in place)
  - Task 10 (db docs) — independent of code; can run with Wave 3

- **Wave 4 (serial, post-gate)** — Commit-step bookkeeping:
  - Task 12 (decisions.md append) — handled in commit/post-proc phase, not dev wave

**Task count: 12**. Waves: **4** (3 dev waves + 1 post-proc).

## Acceptance Criteria Mapping
| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| AC-1 (tenant scoping) | Task 5 | Task 5 tests + `go test ./internal/store/...` |
| AC-2 (partition auto-creation) | Task 3 (bootstrap) + Task 4 (cron) | Task 4 unit tests + integration verify |
| AC-3 (enum CHECKs) | Task 1 | `psql \d` verify; Task 1 verify |
| AC-4 (operator_grants.supported_rat_types) | Task 2 | Task 2 tests |
| AC-5 (RLS policies) | Task 7 | Task 7 integration test + pg_policies count |
| AC-6 (undocumented tables doc) | Task 10 | grep + manual review |
| AC-7 (TBL-26/27/28 added) | Task 10 (+ already done for 26/27/28) | Task 10 verify |
| AC-8 (cursor pagination) | Task 6 | Task 6 tests |
| AC-9 (FK constraints via triggers) | Task 8 | Task 8 verify (insert-rejection test) |
| AC-10 (composite indexes) | Task 9 | `EXPLAIN ANALYZE` in migration comment |
| AC-11 (SELECT *) | Task 11 | `grep -RIn` returns 0 |

## Story-Specific Compliance Rules
- **DB**: All migrations reversible (up+down required). Migration comment header block explains each change.
- **DB**: CHECK constraint additions must include a fail-fast guard SELECT to detect seed/fixture data incompatibility before applying the constraint.
- **Tenant scoping (ADR-001/RBAC-style)**: All store methods take tenantID except the documented exception list (DEV-041, DEV-166).
- **RLS**: Role management (BYPASSRLS grant) is out-of-band (deploy manifests), NOT in migrations. Migration only touches policies/table ALTERs.
- **Partition automation**: Uses existing `internal/job/scheduler.go` cron infra per DEV-168. No new dependencies, no pg_partman.
- **Migration naming**: `20260412000003..008_*` — sequential and gap-free per golang-migrate convention.
- **Zero-deferral (Phase 10)**: Every AC fixed in-story. Any discovered secondary issue is either folded in or captured as an explicit ACCEPTED-OUT-OF-SCOPE note with justification.

## Bug Pattern Warnings
- **PAT-001 [STORY-059]** (BR tests assert behavior — update BR tests when ACs change): This story changes CHECK constraints on `sims.state`, `sims.sim_type`, `users.role`, `users.state`, `tenants.state`, `apns.state`, `policies.state`, `policy_versions.state`, `operators.state`. Grep `*_br_test.go` for any test that inserts raw state values — update fixtures to use compliant values. Known affected: `internal/store/sim_br_test.go`, `internal/store/policy_br_test.go`. Developer MUST grep and update.
- **PAT-002 [STORY-059]** (duplicated util drift): N/A for this story (no util changes).
- **PAT-003 [STORY-060]** (HMAC zeroing): N/A for this story.

## Tech Debt (from ROUTEMAP)
No open tech debt items explicitly targeted at STORY-064. The story itself is the tech-debt item closing Phase 10 DB audit findings.

## Mock Retirement
N/A — no frontend mocks touched by this story.

## Risks & Mitigations
- **Risk 1 — RLS enabling breaks app**: if the DB role does NOT have BYPASSRLS and no `SET app.current_tenant` is set, every query returns empty rows and the app silently returns empty lists. **Mitigation**: Task 7 integration test connects as the app role and verifies a simple `SELECT COUNT(*) FROM sims` returns the expected count. Dev/test uses the superuser (implicit BYPASSRLS). Prod deploy manifest must `ALTER ROLE argus_app BYPASSRLS` before running the new migration.
- **Risk 2 — CHECK constraint adds fail on existing data**: seeds or fixtures with legacy values cause migration failure. **Mitigation**: Task 1 includes fail-fast guard SELECT before each ALTER; if violation found, migration fails loudly with the list of bad rows.
- **Risk 3 — Tenant scoping signature changes break handlers**: Task 5 touches 5-6 files in one dispatch. **Mitigation**: Dispatched as a single coherent unit (not split) so the build is always green at each task boundary. Task verify step compiles the full workspace.
- **Risk 4 — Partition creator missed cron window**: if the daily 02:00 UTC cron fails repeatedly, partitions run out after 9 months (bootstrap covers 2026_07..2027_03). **Mitigation**: Task 4 partition creator MUST emit a WARN log + increment a metric on failure; monitoring catches it. Bootstrap gives 9 months of slack.
- **Risk 5 — FK trigger overhead on high-INSERT tables**: ip_addresses and esim_profiles have moderate write rates; trigger adds per-row lookup. **Mitigation**: Task 8 uses index-backed EXISTS check. `EXPLAIN` should show Index Scan on sims PK. Document in migration comment; if benchmark shows >5% overhead, consider deferring to a bulk-validation job instead.
- **Risk 6 — Partition creator race with midnight rollover**: if the job runs at 23:59 and midnight crosses while the CREATE runs, nothing bad happens (`CREATE TABLE IF NOT EXISTS`), but the next tick idempotently re-runs. **Mitigation**: Use IF NOT EXISTS + log-and-continue on duplicate errors.

---

## Pre-Validation & Quality Gate (self-validated)

**a. Minimum substance (story effort = L):**
- [x] Plan lines ≥ 100 (this file ≫ 300 lines)
- [x] Task count ≥ 5 (12 tasks)

**b. Required sections:**
- [x] `## Goal` present
- [x] `## Architecture Context` present
- [x] `## Tasks` with numbered `### Task` blocks (1–12)
- [x] `## Acceptance Criteria Mapping` present

**c. Embedded specs:**
- [x] DB has column definitions embedded with SQL types (Database Schema section)
- [x] Migration SQL skeletons embedded (RLS, composite indexes, partition creator)
- [x] API specs: this story has no new endpoints; existing endpoint meta-envelope change noted

**d. Task complexity cross-check (L story):**
- [x] Most tasks medium/low — OK
- [x] At least 1 high-complexity task: Task 4 (partition creator) AND Task 5 (tenant scoping) AND Task 7 (RLS)

**e. Context refs validation:**
- [x] All task Context refs point to sections that exist in this plan (verified by inspection)

**Architecture Compliance:**
- [x] Each task's files are in the correct architectural layer (store/, job/, api/, migrations/, docs/)
- [x] No cross-layer imports planned
- [x] Dependency direction correct (migrations → store → api; job → store)

**Database Compliance:**
- [x] Migration steps exist in plan
- [x] Both up and down migrations mentioned for each
- [x] Indexes specified for query columns
- [x] **Embedded schema matches ACTUAL migration files** (verified by reading `20260320000002_core_schema.up.sql`, `20260322000003_anomalies.up.sql`, `20260324000001_policy_violations.up.sql`, `20260323000001_data_optimization.up.sql`, `20260321000002_ota_commands.up.sql`, `20260412000002_esim_multiprofile.up.sql`)
- [x] Column names verified (tenants.state, users.role/state, sims.state/sim_type, etc. all verified)
- [x] FK workaround for partitioned sims documented (DEV-169)

**UI Compliance:** N/A — this story has no UI surface (SCR-120 partition-status widget is out of scope; STORY-064 is backend/DB only).

**Task Decomposition:**
- [x] Each task touches ≤3 files except Task 5 (documented exception: single coherent signature-propagation unit)
- [x] Tasks ordered by dependency (migrations → code)
- [x] Each task has `Depends on` field
- [x] Each task has `Context refs` field
- [x] Each task creating new files has `Pattern ref` field
- [x] Tasks are functionally grouped
- [x] Total task count 12 — reasonable for L story
- [x] NO implementation code in tasks — specs + pattern refs only

**Test Compliance:**
- [x] Test coverage in Task 4 (partition creator unit test), Task 5 (scoping tests), Task 6 (cursor tests), Task 7 (RLS integration test), Task 8 (FK trigger test), Task 9 (EXPLAIN test)
- [x] Test file paths specified
- [x] Test scenarios from story included

**Self-Containment Check:**
- [x] API specs embedded (meta envelope change)
- [x] DB schema embedded (not "see data model")
- [x] DB schema source noted — "Source: migrations/20260320000002_core_schema.up.sql (ACTUAL)" etc.
- [x] Business rules stated inline (DEV-041, DEV-166..170)
- [x] Every task's Context refs point to sections that exist in this plan

**Quality Gate: PASS**

# Gate Report: STORY-064 — Database Hardening & Partition Automation

## Summary
- Requirements Tracing: ACs 11/11 mapped to implementation; Fields N/A (backend-only); Endpoints 1/1 wired (`GET /api/v1/auth/sessions`); Workflows covered by store+handler+router chain
- Gap Analysis: 11/11 acceptance criteria PASS
- Compliance: COMPLIANT with ARCHITECTURE.md, ADRs, decisions.md pattern rules, and Phase 10 zero-deferral policy
- Tests: 1029/1029 story-package tests PASS (32 pkgs); 1945/1945 full-suite tests PASS (64 pkgs); 0 regressions
- Test Coverage: happy + negative paths for partition_creator (monthsAhead, validation, fail-fast, injection rejection), cursor pagination tests for NotificationConfigStore + SessionStore, SoR engine grant-RAT fallback tests
- Performance: Composite indexes on `sessions(sim_id, started_at DESC)` and `cdrs(sim_id, timestamp DESC)` added (AC-10); cursor pagination added (AC-8); GIN index on `operator_grants.supported_rat_types` (AC-4)
- Build: PASS (`go build ./...`)
- Pass 6 (UI): SKIPPED — backend-only story, `has_ui: false`
- Overall: **PASS**

## Pass 1: Requirements Tracing & Gap Analysis

| AC | Criterion | Implementation | Status |
|----|-----------|----------------|--------|
| AC-1 | Tenant scoping: GetByIMSIScoped, esim.go JOIN-scoping | `store/sim.go:793` GetByIMSIScoped (new); `store/esim.go:180` GetEnabledProfileForSIM + `:431` Create + `:511` CountBySIM — all take tenantID and JOIN sims | PASS |
| AC-2 | Partition auto-creation | `migrations/20260412000005_partition_bootstrap.up.sql` pre-creates audit_logs + sim_state_history 2026_07..2027_03; `internal/job/partition_creator.go` Go cron (`PartitionCreator` + `PartitionCreatorProcessor`); scheduler wired in `cmd/argus/main.go:422-426` at `0 2 * * *` UTC | PASS |
| AC-3 | Enum CHECK constraints | `migrations/20260412000003_enum_check_constraints.up.sql` — 9 CHECK constraints with fail-fast DO blocks covering tenants/users/sims/apns/policies/policy_versions/operators | PASS |
| AC-4 | operator_grants.supported_rat_types column + SoR fallback | `migrations/20260412000004_operator_grants_rat_types.up.sql` adds column + GIN index; `internal/operator/sor/engine.go:217-220` consumes grant-level first, operator-level fallback | PASS |
| AC-5 | RLS policies | `migrations/20260412000006_rls_policies.up.sql` enables RLS + FORCE on 28 tables (superset of AC-5's 18 named tables) with `current_setting('app.current_tenant', true)::uuid` pattern; indirect tables (policy_versions, policy_assignments, policy_rollouts, ip_addresses, esim_profiles, user_sessions, sim_state_history) use subquery JOINs | PASS |
| AC-6 | Undocumented tables | `docs/architecture/db/platform-services.md` adds TBL-29 policy_violations, TBL-30 s3_archival_log, TBL-31 tenant_retention_config; `docs/architecture/db/aaa-analytics.md` adds TBL-28 anomalies | PASS |
| AC-7 | TBL listing update | `docs/architecture/db/_index.md` — TBL-26 ota_commands, TBL-27 sla_reports, TBL-28 anomalies, TBL-29 policy_violations, TBL-30 s3_archival_log, TBL-31 tenant_retention_config — 31 rows sequential (Gate closed the TBL-28 numbering gap, see Fixes) | PASS |
| AC-8 | Cursor pagination | `store/notification.go:243` NotificationConfigStore.ListByUser with cursor; `store/user.go:284` SessionStore.ListActiveByUserID with cursor; `internal/api/auth/handler.go:223` ListSessions wires next_cursor through ListMeta; other ACs already paginated (notifications, jobs, api_keys verified) | PASS |
| AC-9 | FK integrity triggers | `migrations/20260412000007_fk_integrity_triggers.up.sql` — `check_sim_exists()` PL/pgSQL + BEFORE INSERT/UPDATE triggers on esim_profiles, ip_addresses, ota_commands; NULL-safe for nullable sim_id | PASS |
| AC-10 | Composite indexes | `migrations/20260412000008_composite_indexes.up.sql` adds `idx_sessions_sim_started`, `idx_cdrs_sim_timestamp`; reuses existing `idx_audit_tenant_time`, `idx_notifications_tenant_time` per plan verification | PASS |
| AC-11 | SELECT * audit | `make lint-sql` passes with zero matches in `internal/store/`; new Makefile target guards against regression | PASS |

### Test Coverage (Pass 1.7)
- **Plan compliance**: `internal/job/partition_creator_test.go` implements all 6 scenarios from Task 4 plan (Type, issues-per-month, zero-monthsAhead, negative-rejection, db-error-wrapping, fail-fast-5th-call, partitionName format, regex injection rejection)
- **AC coverage**: Happy + negative paths present for each store change. `internal/store/notification_test.go:55` TestNotificationConfigStore_ListByUser_CursorSignature; `:88` TestSessionStore_ListActiveByUserID_LimitDefaults. SoR engine grant-RAT fallback exercised in `internal/operator/sor/engine_test.go:623` (non-empty grant) and `:673` (empty grant falls back to operator)
- **Business rule coverage**: RLS FORCE pattern documented; partition idempotency (`CREATE TABLE IF NOT EXISTS`) tested; tenant-scoping regression test for `GetByIMSIScoped` present in sim store tests
- **Test quality**: Assertions are specific (SQL statement counts, wrapped errors, exact partition names, monotonic iteration order)

## Pass 2: Compliance Check

- **ARCHITECTURE.md**: store layer remains the DB access boundary; no layer leakage. All new store methods use `pgxpool.Pool.QueryRow`/`Query`/`Exec` per existing pattern. New API endpoint `GET /api/v1/auth/sessions` is inside `JWTAuth + RequireRole("api_user")` middleware group (router.go:138-144).
- **PRODUCT.md**: tenant isolation rule preserved; defense-in-depth added without breaking hot path (DEV-166 exception documented for RADIUS/Diameter).
- **ADRs**: ADR-001 (multi-tenant scoping) reinforced by AC-1 + AC-5; no ADR violations.
- **decisions.md Bug Patterns**: No PAT-* matches applicable to this story's scope.
- **Migrations**: All 6 pairs reversible; down migrations present. Migration numbering sequential 20260412000003..008 with no gaps.
- **Naming conventions**: Go camelCase, snake_case SQL identifiers, `chk_<table>_<col>` constraint naming consistent.
- **Makefile**: new `lint-sql` target added (Task 11) — correctly wired into .PHONY declarations.
- **No temporary solutions**: zero TODO/FIXME/HACK in new files (verified during Pre-Gate Lint step 2.5).
- **Atomic design / UI**: N/A — no UI changes.

## Pass 2.5: Security Scan

- **SQL Injection**: partition_creator uses `pgx.Identifier{}.Sanitize()` for parent/partition names and strict `partitionNameRE` regex validation before interpolation (names come from hard-coded slice); dates derived from `time.Now()` not user input. All other new SQL uses parameter placeholders ($1, $2...). No raw user-input concatenation found.
- **Hardcoded Secrets**: none in new files.
- **Insecure Randomness**: N/A.
- **Auth Middleware**: `GET /api/v1/auth/sessions` confirmed inside `JWTAuth` group (router.go:138-144).
- **Input Validation**: cursor params validated via `uuid.Parse` with silent fallback on parse failure (no error leakage); `limit` clamped to (0, 100].
- **Dependency CVE**: no new Go modules added; `go build` succeeds without dependency changes.

## Pass 3: Test Execution

### 3.1 Story tests
```
go test ./internal/store/... ./internal/job/... ./internal/operator/... ./internal/api/... ./internal/auth/...
→ 1029 tests passed in 32 packages
```

### 3.2 Full suite
```
go test ./...
→ 1945 tests passed in 64 packages
```

### 3.3 Regression Detection
NONE. All previously-passing tests still pass. No flaky tests observed.

## Pass 4: Performance Analysis

### Queries Analyzed
| # | File:Line | Query/Pattern | Issue | Severity | Status |
|---|-----------|--------------|-------|----------|--------|
| 1 | sessions per-SIM lookup | `WHERE sim_id = $1 ORDER BY started_at DESC` | Needed composite index | — | FIXED (Task 9 — `idx_sessions_sim_started`) |
| 2 | cdrs per-SIM lookup | `WHERE sim_id = $1 ORDER BY timestamp DESC` | Needed composite index | — | FIXED (Task 9 — `idx_cdrs_sim_timestamp`) |
| 3 | operator_grants SupportedRATTypes filter | `WHERE supported_rat_types && $1` | Needed GIN index | — | FIXED (Task 2 — `idx_operator_grants_rat_types_gin`) |
| 4 | notification_configs list by user | Cursor pagination | Missing cursor — unbounded scan | — | FIXED (Task 6) |
| 5 | user_sessions list active | Cursor pagination | Missing cursor — unbounded scan | — | FIXED (Task 6) |
| 6 | FK trigger `check_sim_exists` | `SELECT 1 FROM sims WHERE id = $1` | Index-backed on `sims(id)` — documented per-row overhead | LOW | ACCEPTED (DEV-169; trigger is the partitioned-FK workaround) |

### Caching Verdicts
No new caching introduced. Existing SoR cache (decisions.md PERF-018) unchanged by AC-4; grant-level RAT types cached alongside operator data via the same pipeline.

### Frontend / API Performance
N/A — backend-only story. API surface added: `GET /api/v1/auth/sessions` returns paginated list with `ListMeta{Cursor, HasMore, Limit}`.

## Pass 5: Build Verification

```
go build ./...                 → PASS
go vet ./...                   → 1 pre-existing warning (internal/policy/dryrun/service_test.go:333, unrelated to this story — intentional nil Unmarshal test)
make lint-sql                  → PASS (0 SELECT * in internal/store/)
```

## Pass 6: UI Quality & Visual Testing
SKIPPED — `has_ui: false`. STORY-064 is pure backend/DB hardening with no React/UI surface.

## Fixes Applied

| # | Category | File | Change | Verified |
|---|----------|------|--------|----------|
| 1 | Compliance (AC-7 "renumber consistently") | docs/architecture/db/_index.md | Closed TBL-28 numbering gap: renamed TBL-29 anomalies → TBL-28; TBL-30 policy_violations → TBL-29; TBL-31 s3_archival_log → TBL-30; TBL-32 tenant_retention_config → TBL-31. Updated Domain Detail Files row. | 31 sequential TBL rows |
| 2 | Compliance (same) | docs/architecture/db/aaa-analytics.md | Renamed TBL-29 anomalies section heading → TBL-28 | grep TBL-29 in file → 0 |
| 3 | Compliance (same) | docs/architecture/db/platform-services.md | Renamed TBL-30 → TBL-29 (policy_violations), TBL-31 → TBL-30 (s3_archival_log), TBL-32 → TBL-31 (tenant_retention_config), and cross-references in Related sections | grep TBL-32 in file → 0 |

## Escalated Issues
NONE.

## Deferred Items
NONE. Per Phase 10 zero-deferral policy, every finding resolved in-story. The plan's pre-declared FUTURE items (enforced per-request tx-scoped RLS — DEV-167) are explicit scope boundaries, not deferrals, and remain documented as future work.

### Findings flagged for Reviewer (not gate-blocking, per plan Task 5 audit)
Category A — **intentional auth/hot-path exceptions** (documented in decisions.md pattern):
- `sim.go GetByIMSI` (DEV-041/DEV-166 — RADIUS/Diameter pre-auth lookup)
- `user.go GetByEmail`, `apikey.go GetByPrefix`, `session_radius.go GetByAcctSessionID`, `msisdn.go GetByMSISDN`, `operator.go GetByCode` — all pre-tenant-context auth/identity lookups

Category B — **weak (defense-in-depth opportunity, not a violation)**:
- `esim.go:360-364` — Switch target-profile `FOR UPDATE` inside active tx is unscoped; safe by sibling invariant (profile already loaded under tenant-scoped JOIN earlier in tx) but not explicit. Optional hardening; Reviewer discretion.

## Pattern Compliance (decisions.md Bug Patterns)
- **PAT-001** (BR tests assert behavior): Plan flagged sim/policy BR tests for CHECK-constraint drift. Full test suite PASS verifies no fixture drift.
- **PAT-002/003**: N/A for this story.

## Design Decisions (ready for commit-step append to decisions.md)
- **DEV-166** — `GetByIMSI` remains unscoped for RADIUS/Diameter hot path; `GetByIMSIScoped` is the API-caller variant. Callsites audited: only `internal/aaa/radius` and `internal/aaa/diameter` use the unscoped version.
- **DEV-167** — Phase 10 RLS is defense-in-depth with BYPASSRLS on app role; enforced per-request tx-scoped RLS deferred to FUTURE.
- **DEV-168** — Partition automation uses existing Go cron scheduler (not pg_partman) — zero new extensions, idempotent `CREATE TABLE IF NOT EXISTS`.
- **DEV-169** — Trigger-based `check_sim_exists()` for `esim_profiles.sim_id`, `ip_addresses.sim_id`, `ota_commands.sim_id` because `sims` is LIST-partitioned with composite PK (hard FK impossible).
- **DEV-170** — `GetByICCID` does not exist in the codebase (audit confirmed); nothing to fix.

## Verification (Post-Fix)
- Tests after fixes: **1945/1945 PASS** (no test changes from doc-only fixes)
- Build after fixes: **PASS**
- Lint-sql: **CLEAR**
- Fix iterations: 1 (within max 2)

## Passed Items
- [x] All 11 acceptance criteria implemented end-to-end (model + store + API/job + migration + doc)
- [x] 6 migration pairs (up+down) — all reversible
- [x] Partition creator unit tests cover happy path, bounds, error-wrapping, injection rejection
- [x] Tenant scoping fixes propagated through 5-6 files coherently; full build + all 1945 tests green
- [x] Cursor pagination for 2 new stores; 3 other pre-existing stores verified to already have cursor pagination
- [x] RLS covers 28 tables (superset of AC-5's 18 named tables); FORCE applied; indirect-scope subqueries for join-only tables
- [x] Composite indexes serve API-051 (per-SIM sessions) and API-114 (per-SIM CDRs)
- [x] SoR engine consumes grant-level RAT types with operator-level fallback
- [x] Zero `SELECT *` in store layer; `make lint-sql` CI guard in place
- [x] TBL numbering sequential 01..31 (Gate fix closed the TBL-28 gap from plan's stale STORY-063 assumption)
- [x] New API endpoint `GET /api/v1/auth/sessions` protected by JWTAuth middleware + api_user role
- [x] No new dependency CVEs; no hardcoded secrets; no SQL injection surface
- [x] go build PASS; go vet no new warnings; full-suite regression check PASS

## Gate Status
**GATE_STATUS: PASS**

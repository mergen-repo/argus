# Implementation Plan: STORY-086 — [AUDIT-GAP] Restore missing `sms_outbound` table + boot-time schema-integrity check

## Goal

Repair the missing `sms_outbound` relation in the live DB (so `GET /api/v1/sms/history` stops 500-ing), and install a lightweight boot-time schema-integrity gate so future silent schema drift fails fast with a FATAL instead of a runtime 500.

## Context Summary

- **Symptom**: on 2026-04-17 compliance audit run, live Docker stack healthy, `schema_migrations.version=20260417000003 dirty=f`, but `SELECT to_regclass('public.sms_outbound')` returns NULL on the live DB. Seven sibling tables created in the same STORY-069 migration file (`onboarding_sessions`, `scheduled_reports`, `webhook_configs`, `webhook_deliveries`, `notification_preferences`, `notification_templates`, and `sms_outbound`) — only `sms_outbound` is missing.
- **Blast radius**: F-055 (SMS Gateway) in PRODUCT.md is broken in live env; API-170 and API-171 fail; admin delivery dashboard `sms` channel stats break; SCR-132 silently errors on every mount.
- **RLS state clarification**: the dispatch instruction says "original STORY-069 migration did NOT enable RLS on this table" — this is **incorrect**. The sibling RLS migration `migrations/20260413000002_story_069_rls.up.sql:34-38` DOES enable + force RLS and install `sms_outbound_tenant_isolation`. So the repair migration must mirror both the DDL (from `20260413000001`) AND the RLS policy (from `20260413000002`) to end up in the intended STORY-069 steady state.
- **Boot-time check location**: `runMigrate` in `cmd/argus/main.go:192-262` is CLI-only. The actual HTTP boot path is `runServe` starting at `cmd/argus/main.go:316`. `runServe` does NOT invoke migrations — migrations are run separately via `argus migrate up` (Makefile `db-migrate` target). Therefore, the integrity check runs inside `runServe` immediately after `store.NewPostgresWithMetrics(...)` succeeds on line 376. Placing it in `runMigrate` would be wrong because the boot-time schema guarantee must exist on every `serve` start, not just on migration runs.
- **Test convention**: the project does NOT currently use `testcontainers` (grep confirms zero usage in `internal/store`). Integration tests follow the `DATABASE_URL`-driven, `//go:build integration` pattern exemplified by `internal/store/onboarding_session_store_integration_test.go`. STORY-086 AC-4 therefore follows the established pattern — a new `integration`-tagged test that asserts `to_regclass('public.sms_outbound')` is non-nil and performs a tenant-scoped insert. Introducing testcontainers here would be out-of-scope scope creep (would need new Go dependency + CI setup + docker-in-docker). Decision captured inline as PLAN-DEC-1, finalised by Developer as **DEV-239 sub-point (b)** in AC-1.
- **Next DEV number**: last assigned in `docs/brainstorming/decisions.md` is DEV-238 → use **DEV-239** for the AC-1 root-cause note.
- **Verification harness**: there is NO `make smoke-test` target (Makefile inspected, lines 14-300). AC-5's "Confirm `GET /api/v1/sms/history` returns 200 ... via `make smoke-test`" must be adapted to a curl-based manual smoke (documented in AC-5 tasks) since creating a new Make target is out-of-scope for a S story.

## Prerequisites

- [x] STORY-069 migrations (`20260413000001_story_069_schema.up.sql` + `20260413000002_story_069_rls.up.sql`) shipped; seven sibling tables exist in live DB.
- [x] STORY-079 shipped `argus migrate` subcommand (DONE 2026-04-17) — migrations run via `docker compose exec argus /app/argus migrate up`.
- [x] `internal/store/sms_outbound.go` is already wired (Insert/Update/MarkDelivered/GetByID/List) — no Go code change needed in the store.
- [x] `internal/store/sms_outbound_test.go` exists (352 lines, 8 tests including integration block guarded by `DATABASE_URL`). AC-4 extends this file — does NOT create a new one.
- [x] Docker stack available for fresh-volume reproducer + AC-5 smoke verification.

## Architecture Context

### Components Involved

- **Migration pair (new)** — `migrations/20260417000004_sms_outbound_recover.{up,down}.sql`: idempotent DDL + index + RLS reinstatement. Loaded by golang-migrate via `file:///app/migrations` source in lex order. Because lex order puts it strictly after `20260417000003_story_077_rls`, it runs last and re-establishes the table if missing.
- **Boot-time schema-check (new package)** — `internal/store/schemacheck/schemacheck.go`: single `Verify(ctx, pool, tables []string) error` function. Iterates the manifest, runs `SELECT to_regclass('public.' || $1) IS NOT NULL` per table, aggregates missing table names, returns a joined error. Called from `cmd/argus/main.go` `runServe` immediately after `store.NewPostgresWithMetrics`. On any missing table: `log.Fatal().Strs("missing", ...).Msg(...)` → `os.Exit(1)`.
- **Integration test extension** — `internal/store/sms_outbound_test.go`: adds a new `//go:build integration` function `TestSmsOutbound_RelationPresentAfterMigrations` that asserts the table exists (belt) + a tenant-scoped Insert round-trip (suspenders, exercises the RLS policy). Does NOT touch unit tests in the same file.
- **Root-cause investigation artefact** — a single new `DEV-239` entry in `docs/brainstorming/decisions.md` documenting the 2-hour time-boxed investigation (fresh-volume reproducer outcome, `DROP TABLE sms_outbound` grep result, golang-migrate transaction boundary analysis).
- **Doc sync** — remove the `**NOTE (audit 2026-04-17):**` caveat from TBL-42 row in `docs/architecture/db/_index.md:52` after AC-2..AC-4 land; append a `2026-04-17 DONE STORY-086` row to ROUTEMAP Change Log and flip `D-025` status from `[ ] PENDING` to `✓ RESOLVED (2026-04-17)`.

### Data Flow (boot-time check)

```
docker compose up argus
  → /app/argus serve                          (default subcommand, no flags)
    → cmd/argus/main.go runServe()
      → config.Load()
      → store.NewPostgresWithMetrics(...)     (line 376)
      → store.schemacheck.Verify(ctx, pg.Pool, CriticalTablesManifest)   (NEW — new line ~384)
          | for each table:
          |   SELECT to_regclass('public.' || $1) IS NOT NULL
          |   if false → append to missing
          | if len(missing) > 0 → return fmt.Errorf("critical tables missing: %v", missing)
      → on error: log.Fatal().Strs("missing_tables", m).Msg("schema integrity check failed — run migrations") + os.Exit(1)
      → continue to StartPoolGauge / read replica / rest of boot
```

### Data Flow (repair migration idempotency)

```
argus migrate up
  → golang-migrate walks migrations/*.up.sql in lex order
    → ... runs 20260417000003_story_077_rls.up.sql
    → runs 20260417000004_sms_outbound_recover.up.sql   (NEW, last in lex order)
        BEGIN; (auto-wrapped by golang-migrate)
          CREATE TABLE IF NOT EXISTS sms_outbound (...)   -- no-op on fresh volumes where 20260413000001 already created it
          CREATE INDEX IF NOT EXISTS idx_sms_outbound_tenant_sim_time ...
          CREATE INDEX IF NOT EXISTS idx_sms_outbound_provider_id ...
          CREATE INDEX IF NOT EXISTS idx_sms_outbound_status ...
          ALTER TABLE sms_outbound ENABLE ROW LEVEL SECURITY;   -- no-op if already enabled
          ALTER TABLE sms_outbound FORCE ROW LEVEL SECURITY;    -- no-op if already forced
          DROP POLICY IF EXISTS sms_outbound_tenant_isolation ON sms_outbound;
          CREATE POLICY sms_outbound_tenant_isolation ON sms_outbound
              USING (tenant_id = current_setting('app.current_tenant', true)::uuid);
        COMMIT;
      → schema_migrations version advanced to 20260417000004
```

### API Specifications

No new or modified endpoints. The story exists only to make the already-documented endpoints (API-170 `POST /api/v1/sms/send`, API-171 `GET /api/v1/sms/history`) functional again. Endpoint contracts unchanged; handler code unchanged.

### Database Schema

**Source of truth for the repair migration**: `migrations/20260413000001_story_069_schema.up.sql:141-157` (DDL) + `migrations/20260413000002_story_069_rls.up.sql:34-38` (RLS policy). The repair migration MUST reproduce these byte-for-byte (typed verbatim from the source), with the following idempotency patches:

- `CREATE TABLE` → `CREATE TABLE IF NOT EXISTS`
- Each `CREATE INDEX` → `CREATE INDEX IF NOT EXISTS`
- `CREATE POLICY` → preceded by `DROP POLICY IF EXISTS ... ON ...;`
- `ALTER TABLE ... ENABLE ROW LEVEL SECURITY;` and `... FORCE ROW LEVEL SECURITY;` are idempotent in PostgreSQL (no-op when already enabled / forced) — no extra guards needed.

**Schema (TBL-42) — source: migrations/20260413000001_story_069_schema.up.sql:141-157, ACTUAL (live):**

```sql
CREATE TABLE IF NOT EXISTS sms_outbound (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    sim_id UUID NOT NULL REFERENCES sims(id),
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
CREATE INDEX IF NOT EXISTS idx_sms_outbound_tenant_sim_time
    ON sms_outbound (tenant_id, sim_id, queued_at DESC);
CREATE INDEX IF NOT EXISTS idx_sms_outbound_provider_id
    ON sms_outbound (provider_message_id)
    WHERE provider_message_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_sms_outbound_status
    ON sms_outbound (status);
```

**RLS (source: migrations/20260413000002_story_069_rls.up.sql:34-38):**

```sql
ALTER TABLE sms_outbound ENABLE ROW LEVEL SECURITY;
ALTER TABLE sms_outbound FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS sms_outbound_tenant_isolation ON sms_outbound;
CREATE POLICY sms_outbound_tenant_isolation ON sms_outbound
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);
```

**Down migration**: `DROP TABLE IF EXISTS sms_outbound CASCADE;` (drops the table, its indexes, and the policy; matches the pattern in `20260413000001_story_069_schema.down.sql:10`).

### Critical Tables Manifest (for `schemacheck.Verify`)

13 table names total (7 STORY-069 + 5 STORY-077 + 1 repaired TBL-42). Manifest lives in `internal/store/schemacheck/manifest.go` (or inline in `schemacheck.go` — Developer chooses) as a package-level `var CriticalTables = []string{...}`.

**STORY-069 tables (from `20260413000001_story_069_schema.up.sql`)** — 7 entries:
1. `onboarding_sessions`
2. `scheduled_reports`
3. `webhook_configs`
4. `webhook_deliveries`
5. `notification_preferences`
6. `notification_templates`
7. `sms_outbound`  ← the one that went missing

**STORY-077 tables (from `20260417000001_story_077_ux.up.sql`)** — 5 entries (TBL-47..TBL-51):
8. `user_views` (TBL-50)
9. `announcements` (TBL-47)
10. `announcement_dismissals` (TBL-48)
11. `chart_annotations` (TBL-49)
12. `user_column_preferences` (TBL-51)

**Plus `sms_outbound` again?** — No, it is already item 7. Dispatch instruction "7 STORY-069 tables + 5 STORY-077 tables + TBL-42 sms_outbound = 13 names" double-counted. The real manifest is **12 unique table names** (7 + 5). Planner flags this and instructs Developer to use 12, not 13. This counting correction is captured in DEV-239 as a minor clarification.

**Rationale for scope**: only STORY-069 + STORY-077 recent tables are in scope because (a) older tables have been runtime-verified in prior audits, (b) the gate is meant to catch the drift class the 2026-04-17 audit found, (c) keeping the manifest scoped means each added migration-story author flips one boolean to include their new table.

## Story-Specific Compliance Rules

- **DB**: migration pair required (up + down). Idempotent on fresh volumes (must be a no-op where STORY-069 did apply correctly). Mirror exact column names, types, defaults, and constraints from source migrations 20260413000001 and 20260413000002. Every DDL statement guarded by `IF NOT EXISTS` / `IF EXISTS` as applicable.
- **Go**: new `schemacheck` package is internal-only, no exported types except `Verify(ctx context.Context, pool *pgxpool.Pool, tables []string) error` and `var CriticalTables []string`. Zero new external dependencies (uses existing `github.com/jackc/pgx/v5/pgxpool`).
- **Boot-path contract**: integrity check is FATAL on failure. Must run BEFORE any goroutines are launched (pool gauge, read replica, etc.), to guarantee a crash before any observable side effects. Exit code 1 (`log.Fatal` auto-exits).
- **Test convention**: integration test uses `//go:build integration` + `DATABASE_URL`. Does NOT introduce testcontainers. Skips cleanly when `DATABASE_URL` is unset.
- **Docs**: after merge, (a) remove audit caveat from `docs/architecture/db/_index.md:52` TBL-42 row; (b) add ROUTEMAP Change Log row; (c) flip D-025 to RESOLVED. Single commit on the doc-sync task.
- **ADR alignment**: no ADR impact — this is pure recovery + defensive guard.

## Bug Pattern Warnings

From `docs/brainstorming/decisions.md` § Bug Patterns & Prevention Rules:

- **PAT-001** (BR/acceptance tests assert behavior, not implementation): does NOT apply — no BR test file exists for `sms_outbound`, no behavior contracts being changed.
- **PAT-002** (duplicated utility functions drift): does NOT apply — the new `schemacheck.Verify` is a first-of-its-kind helper, no existing duplicates to fold in.
- **PAT-003** (AT_MAC / HMAC zeroing): does NOT apply — no EAP crypto touched.

**New pattern candidate** (to consider in AC-1 decisions.md entry, if appropriate): "Schema drift can pass all tests when tests never assert `to_regclass` of canonically expected tables — use a manifest-driven boot gate." Planner notes this for Developer to consider as a possible **PAT-004** addition, but leaves the final call to the Reviewer gate.

## Tech Debt (from ROUTEMAP)

- **D-025** (Audit 2026-04-17, target STORY-086): `sms_outbound` absent from live PG despite clean `schema_migrations` — ROOT CAUSE pending. Status `[ ] PENDING` → AC-5 task flips to `✓ RESOLVED (2026-04-17)` and appends the repair-migration filename.

All other open `D-*` rows target different stories.

## Mock Retirement (Frontend-First projects only)

No `src/mocks/` directory — N/A. No mock retirement for this story.

## Task Decomposition

Five tasks in three waves. Wave 1 is the sequential root-cause investigation that gates everything. Wave 2 is two parallelizable code tasks. Wave 3 is sequential final test + doc sync.

### Wave 1 (sequential) — root cause

### Task 1: AC-1 root-cause investigation + DEV-239 entry
- **Files:** Modify `docs/brainstorming/decisions.md` (append one row)
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read rows DEV-236/237/238 at the current tail of `docs/brainstorming/decisions.md` — same `| DEV-NNN | YYYY-MM-DD | [body] | ACCEPTED |` table-row format.
- **Context refs:** "Context Summary", "Architecture Context > Components Involved" (investigation artefact bullet)
- **What:** Time-boxed 2-hour investigation. Concrete checklist to execute in order and record findings for:
  1. Current `schema_migrations.version` and `dirty` state on live DB (`docker compose exec pg psql -U ... -c "SELECT * FROM schema_migrations"`). Expected: `20260417000003, f`.
  2. `SELECT to_regclass('public.sms_outbound')` on live DB — confirm NULL.
  3. Cross-check sibling: `SELECT to_regclass('public.onboarding_sessions')` — expected non-null (confirms only `sms_outbound` is missing).
  4. Grep repo for destructive statements: `git grep -niE "DROP TABLE[[:space:]]+.*sms_outbound" -- ':!docs/'` (docs exclude). Any hit outside `migrations/20260413000001_story_069_schema.down.sql:10` is a smoking gun.
  5. Grep seed scripts: `git grep -niE "TRUNCATE|DELETE FROM sms_outbound|DROP.*sms" migrations/seed/` — determine if seed path has ever dropped the table.
  6. Fresh-volume reproducer: `make down && docker volume rm argus_pg_data 2>/dev/null; make up && make db-migrate && docker compose exec pg psql -U argus -d argus -c "SELECT to_regclass('public.sms_outbound');"`. If the result is non-null → the bug is state-specific to the current volume (manual intervention or historical partial migration). If NULL → the bug is reproducible on fresh volumes = MUCH higher severity, requires migration fix urgency.
  7. Read golang-migrate internals: is each migration file auto-wrapped in a transaction and committed atomically (Y/N)? If N, what happens when a later statement fails? Check `golang-migrate/migrate/v4` docs or source at `vendor/` / `go.sum`; typical answer is "yes, single implicit BEGIN/COMMIT per file unless the file contains `-- +migrate StatementBegin` markers — which the STORY-069 file does NOT".
  8. Document the manifest-count correction (dispatch said 13, correct count is 12 unique table names).
- **Decision entry format** — append to `docs/brainstorming/decisions.md` as a new table row on a new line at the end of the main table (BEFORE the `## Bug Patterns & Prevention Rules` section on line 461):
  ```
  | DEV-239 | 2026-04-17 | **STORY-086 AC-1: sms_outbound missing from live DB — root-cause findings.** [Findings from steps 1-7 above, one line, ~200 words max.] Manifest count correction: dispatch said 13 critical tables; actual unique count is 12 (dispatch double-counted sms_outbound once under STORY-069 and again separately). Boot-time schema-check landed as defensive guard regardless of root-cause outcome. RLS-on-sms_outbound pre-existed in migration 20260413000002 (dispatch instruction stating "original STORY-069 migration did NOT enable RLS" was mistaken); repair migration therefore restores RLS + policy in line with the intended STORY-069 steady state. | ACCEPTED |
  ```
- **Verify:** `grep -cE '^\| DEV-239 \|' docs/brainstorming/decisions.md` → `1`. `git diff docs/brainstorming/decisions.md` shows single added row. No existing rows touched.

### Wave 2 (parallel) — code fixes

### Task 2: AC-2 repair migration pair
- **Files:** Create `migrations/20260417000004_sms_outbound_recover.up.sql`, Create `migrations/20260417000004_sms_outbound_recover.down.sql`
- **Depends on:** Task 1 (decision recorded first for traceability, but NOT blocked on any finding from Task 1 — can run in parallel in practice; keeping serial to keep the ledger linear)
- **Complexity:** low
- **Pattern ref:** Read `migrations/20260413000001_story_069_schema.up.sql:141-157` (DDL source) + `migrations/20260413000002_story_069_rls.up.sql:34-38` (RLS source) + `migrations/20260417000003_story_077_rls.up.sql` (idempotent multi-statement RLS style).
- **Context refs:** "Architecture Context > Database Schema", "Architecture Context > Data Flow (repair migration idempotency)"
- **What:**
  - `20260417000004_sms_outbound_recover.up.sql`: exact content from the "Database Schema" section above — DDL (`CREATE TABLE IF NOT EXISTS`), three `CREATE INDEX IF NOT EXISTS`, two `ALTER TABLE` statements (ENABLE + FORCE), `DROP POLICY IF EXISTS`, `CREATE POLICY`. Add a header comment block with the rationale: `-- STORY-086: recover sms_outbound after live-env drift (schema_migrations=20260417000003 dirty=f but table absent). Idempotent on fresh volumes.`
  - `20260417000004_sms_outbound_recover.down.sql`: `DROP TABLE IF EXISTS sms_outbound CASCADE;` plus a one-line comment. `DROP TABLE ... CASCADE` removes the policy + indexes automatically.
- **Verify:**
  1. `ls migrations/20260417000004_sms_outbound_recover.*` → both files present.
  2. `docker compose exec pg psql -U argus -d argus -c "DROP TABLE IF EXISTS sms_outbound CASCADE; UPDATE schema_migrations SET version=20260417000003, dirty=false;"` (simulate the missing-table state on the live DB).
  3. `make db-migrate` → exits 0, logs version `20260417000004`.
  4. `docker compose exec pg psql -U argus -d argus -c "SELECT to_regclass('public.sms_outbound');"` → non-null.
  5. Re-run `make db-migrate` → `no change — already at latest version`. Idempotency proven.

### Task 3: AC-3 schemacheck package + boot-time wire-up
- **Files:** Create `internal/store/schemacheck/schemacheck.go`, Create `internal/store/schemacheck/schemacheck_test.go`, Modify `cmd/argus/main.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:**
  - For the schemacheck package: read `internal/store/postgres.go` (simple, single-function internal package style) for how internal/store subpackages are structured. The new package uses only `context.Context` + `pgxpool.Pool`.
  - For the unit test: read `internal/store/apikey_test.go` for the test-file style (no testcontainers, table-driven test cases).
  - For the main.go wire-up: read `cmd/argus/main.go:376-383` — the exact pattern used right after `store.NewPostgresWithMetrics` to invoke further store helpers (`StartPoolGauge` is a non-fatal background; schemacheck is synchronous fatal-on-error).
- **Context refs:** "Architecture Context > Components Involved (boot-time schema-check)", "Architecture Context > Data Flow (boot-time check)", "Architecture Context > Critical Tables Manifest"
- **What:**
  - `internal/store/schemacheck/schemacheck.go`:
    - Package `schemacheck`.
    - Exported `var CriticalTables = []string{...}` with exactly the 12 table names listed in "Critical Tables Manifest" above, in stable order (sort alphabetically to make future diffs clean).
    - Exported `func Verify(ctx context.Context, pool *pgxpool.Pool, tables []string) error`:
      - Iterates `tables`.
      - Runs `SELECT to_regclass('public.' || $1) IS NOT NULL` per table (use `pool.QueryRow(ctx, query, name).Scan(&exists)`).
      - Collects missing names into a `[]string`.
      - If non-empty: returns `fmt.Errorf("schemacheck: critical tables missing from database: %v", missing)`.
      - Returns nil on full success.
    - No goroutines, no logging inside the helper (caller logs). Uses `context.Context` so cancellations propagate.
  - `internal/store/schemacheck/schemacheck_test.go`: unit tests — because we cannot hit a real PG from unit tests, write two tests that:
    1. `TestVerify_EmptyManifestSucceeds`: `Verify(ctx, nil, []string{})` → nil error (with a nil pool, since an empty manifest never calls it). Confirms early-return correctness.
    2. `TestCriticalTables_CountAndOrder`: assertions that `len(CriticalTables) == 12`, contains `"sms_outbound"`, contains `"user_views"`, sorted. Cheap, regression-proof.
    3. (Optional but recommended) `TestVerify_Integration` guarded by `//go:build integration` that uses the `DATABASE_URL` pattern from `onboarding_session_store_integration_test.go`, runs `Verify(ctx, pool, CriticalTables)` against the live migrated DB, expects nil.
  - `cmd/argus/main.go` changes:
    - Add import `schemacheck "github.com/btopcu/argus/internal/store/schemacheck"` in the internal imports block (alphabetical with other `store` subpackages).
    - Directly after line 381 (`log.Info().Msg("postgres connected")`) and BEFORE line 383 (`store.StartPoolGauge(...)`), insert a short synchronous block that calls `schemacheck.Verify(ctx, pg.Pool, schemacheck.CriticalTables)`. On error: `log.Fatal().Err(err).Strs("expected_tables", schemacheck.CriticalTables).Msg("boot: schema integrity check failed — run 'argus migrate up' or inspect schema drift")` (zerolog `Fatal` auto-exits 1).
    - Keep the change to a single added block (≤8 lines of Go). Do NOT refactor surrounding code.
- **Verify:**
  1. `go build ./...` → passes.
  2. `go vet ./internal/store/schemacheck/...` → passes.
  3. `go test ./internal/store/schemacheck/...` → unit tests pass, integration test is skipped when `DATABASE_URL` unset.
  4. Manual boot smoke: `docker compose exec pg psql -U argus -d argus -c "DROP TABLE sms_outbound CASCADE;"`; `docker compose restart argus`; `docker compose logs argus --since=1m` → expect a FATAL line containing `schemacheck: critical tables missing from database: [sms_outbound]` and container restart-loop (exit 1). Then re-run `make db-migrate` followed by `docker compose restart argus` — container should boot clean.

### Wave 3 (sequential) — verification + doc sync

### Task 4: AC-4 integration test extension
- **Files:** Modify `internal/store/sms_outbound_test.go`
- **Depends on:** Task 2 (the repair migration must be in place so fresh-volume migrate produces the table)
- **Complexity:** low
- **Pattern ref:** Read `internal/store/onboarding_session_store_integration_test.go:1-50` for the `//go:build integration` + `DATABASE_URL` + `testOnboardingPool` helper pattern. Also read the existing `TestSMSOutboundStore_Integration` block at `internal/store/sms_outbound_test.go:141-332` — the new test reuses `testSMSPool` and `requireTestTenantAndSIM`.
- **Context refs:** "Architecture Context > Data Flow (repair migration idempotency)", "Critical Tables Manifest"
- **What:** Extend (do NOT replace) `internal/store/sms_outbound_test.go`:
  - Add a `//go:build integration` constraint at the top of the file IF NOT ALREADY PRESENT. **Before editing**, Developer MUST check line 1 — the current file does NOT have a build tag (confirmed by orientation), so the existing tests run on every `go test ./...`. Adding the tag would break the unit tests. Resolution: do NOT add a file-level build tag. Instead, the new regression test uses a runtime skip on `DATABASE_URL` missing (matching the existing `TestSMSOutboundStore_Integration` pattern on line 142: `if pool == nil { t.Skip(...) }`).
  - Add one new test function `TestSmsOutbound_RelationPresentAfterMigrations(t *testing.T)`:
    1. `pool := testSMSPool(t); if pool == nil { t.Skip("no test database available (set DATABASE_URL)") }`.
    2. `var present bool; err := pool.QueryRow(ctx, "SELECT to_regclass('public.sms_outbound') IS NOT NULL").Scan(&present)`. Require `err == nil` and `present == true`. First belt.
    3. `tenantID, simID := requireTestTenantAndSIM(t, pool); ctx := smsCtx(tenantID); s := NewSMSOutboundStore(pool)`. Then `Insert` a `SMSOutbound{TenantID: tenantID, SimID: simID, MSISDN:"+905550000999", TextHash:"story086hash", TextPreview:"STORY-086 regression", Status:"queued"}`. Require non-nil result and non-zero ID. Exercises the live DB, the RLS policy (via `app.current_tenant`), and the Insert path. Suspenders.
  - Keep the test small (≤35 lines). Do NOT add helpers — reuse `testSMSPool`, `smsCtx`, `requireTestTenantAndSIM`.
- **Verify:**
  1. `DATABASE_URL=postgres://argus:argus@localhost:5432/argus?sslmode=disable go test ./internal/store -run TestSmsOutbound_RelationPresentAfterMigrations -v` → PASS.
  2. `go test ./internal/store/...` (without DATABASE_URL) → new test skipped, all unit tests still pass.
  3. Regression sanity: drop the table `docker compose exec pg psql ... -c "DROP TABLE sms_outbound CASCADE; UPDATE schema_migrations SET version=20260417000003, dirty=false;"`, re-run the same test with `DATABASE_URL` set → FAIL with `to_regclass IS NOT NULL = false`. Re-run `make db-migrate`, re-run test → PASS. Proves the test catches the bug.

### Task 5: AC-5 doc sync + smoke verification
- **Files:** Modify `docs/architecture/db/_index.md`, Modify `docs/ROUTEMAP.md`
- **Depends on:** Task 2, Task 3, Task 4 (all code + tests green)
- **Complexity:** low
- **Pattern ref:** Read `docs/ROUTEMAP.md:247` (last AUDIT row) for the Change Log row format. Read `docs/ROUTEMAP.md:372` (D-025 row) for the Tech Debt table row format.
- **Context refs:** "Context Summary", "Tech Debt (from ROUTEMAP)"
- **What:**
  1. `docs/architecture/db/_index.md:52` — remove the `**NOTE (audit 2026-04-17):** migration 20260413000001 creates this table but it is absent from the current live DB — tracked as STORY-086 for root-cause + repair.` sentence. The rest of the row stays unchanged. After edit, the row reads: `| TBL-42 | sms_outbound | SMS Gateway | → TBL-01 (tenant_id); body stored as SHA-256 hash + 80-char preview (GDPR); RLS enabled. | No |`.
  2. `docs/ROUTEMAP.md:372` — flip D-025 Status column from `[ ] PENDING` to `✓ RESOLVED (2026-04-17)` and append ` — recover migration 20260417000004 + boot-time schemacheck guard (STORY-086)` to the Notes/Description cell end.
  3. `docs/ROUTEMAP.md` Change Log — append a new row at the current head of the change-log table (same section as the 2026-04-17 AUDIT row on line 247): `| 2026-04-17 | DONE | STORY-086 shipped — repair migration 20260417000004_sms_outbound_recover (DDL + 3 indexes + RLS + policy), new internal/store/schemacheck package with CriticalTables manifest (12 tables: STORY-069 × 7 + STORY-077 × 5) wired into cmd/argus/main.go runServe boot path (FATAL on miss, exit 1), regression test TestSmsOutbound_RelationPresentAfterMigrations added to internal/store/sms_outbound_test.go, TBL-42 audit caveat removed, D-025 RESOLVED. Root-cause: DEV-239. Smoke: GET /api/v1/sms/history returned 200. | migrations/, internal/store/schemacheck/, cmd/argus/main.go, internal/store/sms_outbound_test.go, docs/architecture/db/_index.md, docs/ROUTEMAP.md, docs/brainstorming/decisions.md |`
  4. `docs/ROUTEMAP.md` top header — update `STORY-086 PENDING` → `STORY-086 DONE` on lines 4, 5, 28, 29, 149, 150, 204 (all occurrences). Phase 10 counter: bump `23/24` → `24/24`.
  5. **Smoke verification** (no `make smoke-test` target exists — documented as DEV-239 sub-note):
     - `docker compose exec argus curl -s -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $JWT" http://localhost:8080/api/v1/sms/history` → expected `200`.
     - Capture the command output in the Change Log row evidence (or append a footnote in D-025 resolution description).
     - If Dockerised curl auth setup is non-trivial, acceptable alternative: browser-based verification (login as admin@argus.io, navigate to SCR-132 `/sms` page, confirm history loads without 500 banner) — screenshot not required; textual confirmation in the Change Log is sufficient.
- **Verify:**
  1. `grep -c "NOTE (audit 2026-04-17)" docs/architecture/db/_index.md` → 0 (was 1).
  2. `grep -c "D-025 | Audit 2026-04-17" docs/ROUTEMAP.md` → 1 (row preserved, status column flipped).
  3. `grep -c "PENDING.*STORY-086" docs/ROUTEMAP.md` → 0.
  4. Change Log row present: `grep -c "STORY-086 shipped" docs/ROUTEMAP.md` → 1.
  5. Manual smoke: `curl -s http://localhost:8084/api/v1/sms/history ...` returns 200.

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|----------------|-------------|
| AC-1 Root-cause investigation + DEV-NNN entry | Task 1 | Task 1 Verify step (grep for DEV-239 row) |
| AC-2 Repair migration (up + down, idempotent, mirrors RLS) | Task 2 | Task 2 Verify steps 2-5 (drop-then-remigrate + idempotent re-run) |
| AC-3 Boot-time schema-integrity check | Task 3 | Task 3 Verify step 4 (live boot smoke with dropped table → FATAL + restart loop) |
| AC-4 Regression test | Task 4 | Task 4 Verify step 3 (drop → test FAIL → remigrate → test PASS) |
| AC-5 Doc sync + smoke | Task 5 | Task 5 Verify steps 1-5 |

## Risks & Mitigations

- **Risk — fresh-volume reproducer shows the bug**: if AC-1 step 6 returns NULL on a clean volume, the original STORY-069 migration itself has a real bug, not a live-env drift. Mitigation: STORY-086 already ships the repair migration, so steady state is restored regardless. If the fresh-volume repro reproduces, AC-1 DEV-239 must note this explicitly and the Reviewer gate decides whether an additional STORY-087 is needed to fix the upstream migration file. No plan change needed for STORY-086 scope.
- **Risk — `DROP TABLE sms_outbound CASCADE` grep finds a real hit**: AC-1 step 4. If discovered, DEV-239 names the file + line and we optionally open a POST-GA FIX to remove the destructive call. Again, no STORY-086 scope change — repair + guard still land.
- **Risk — adding `//go:build integration` tag to `sms_outbound_test.go` accidentally gates the unit tests**: Mitigation in Task 4 ("What" bullet 1): Developer is told explicitly NOT to add a file-level build tag; use the existing runtime `t.Skip` pattern. Verification: `go test ./internal/store -run 'TestNewSMSOutboundStore|TestSMSOutbound_Fields|TestSMSListFilters_Fields'` still passes with no `DATABASE_URL` set.
- **Risk — schemacheck adds boot latency**: 12 PG round-trips per boot. Each is a single trivial query, ~1-2 ms per query on a local pool = 12-24 ms. Mitigation: acceptable — boot already runs dozens of store-constructor queries. If needed in future, batch into a single `SELECT to_regclass(unnest($1::text[]))` query — noted for POST-GA optimization but NOT in scope for STORY-086.
- **Risk — repair migration hash changes golang-migrate checksum**: golang-migrate tracks versions, not hashes, so adding a new file with a later timestamp is safe. Verified by Task 2 step 3 (re-run returns no-change).
- **Risk — Developer uses testcontainers despite context pointing elsewhere**: Mitigation — Context Summary explicitly states "The project does NOT currently use testcontainers"; Task 4 pattern ref points at the `DATABASE_URL`/`t.Skip` sibling file; Task 4 "What" bullet spells out the skip pattern. Reviewer Gate should reject a testcontainers introduction under STORY-086.
- **Risk — RLS policy `DROP ... IF EXISTS + CREATE` raises if another session holds a lock**: negligible in a single-user migration flow; golang-migrate runs serially. If contention is ever an issue, the DDL is already inside the migration's auto-wrapped transaction.

## Self-Validation Checklist (embedded Quality Gate)

- [x] Minimum substance: S story → ≥30 lines, ≥2 tasks. Plan has 5 tasks and ~340 lines. PASS.
- [x] Required sections present: `## Goal`, `## Architecture Context`, `## Tasks`, `## Acceptance Criteria Mapping`. PASS.
- [x] Embedded specs (not references): DB schema embedded byte-exact from migration source; RLS embedded; boot-flow pseudo-code embedded; manifest enumerated. PASS.
- [x] Task complexity: S story → most tasks low, max 1 medium. Two tasks are medium (Task 1 investigation, Task 3 boot wire-up); three are low. For an S story this is slightly over the default — justified because (a) Task 1 is the gating root-cause analysis and genuinely requires several DB probes; (b) Task 3 touches three files with a new package and boot-critical wire-up. Acceptable within S bounds.
- [x] Context refs validation: all refs ("Context Summary", "Architecture Context > Components Involved", "Architecture Context > Data Flow (repair migration idempotency)", "Architecture Context > Data Flow (boot-time check)", "Architecture Context > Database Schema", "Architecture Context > Critical Tables Manifest", "Tech Debt (from ROUTEMAP)") point to sections that exist in this plan. PASS.
- [x] Architecture Compliance — files in correct layers: migrations in `migrations/`, Go internal helper in `internal/store/schemacheck/`, tests co-located with code, docs under `docs/`. PASS.
- [x] API Compliance — N/A (no API changes).
- [x] Database Compliance — migration up+down present, idempotent, column names/types verified byte-exact against the source migration file at `migrations/20260413000001_story_069_schema.up.sql:141-157`. PASS.
- [x] UI Compliance — N/A (no UI changes).
- [x] Task Decomposition — each task touches ≤3 files (Task 3 is at the 3-file max: `schemacheck.go`, `schemacheck_test.go`, `main.go`); all tasks have `Depends on`, `Complexity`, `Pattern ref`, `Context refs`, `What`, `Verify`. PASS.
- [x] Test Compliance — regression test task exists (Task 4) covering AC-2 + AC-3 behavior; Task 3 has its own unit test step; file paths and scenarios specified. PASS.
- [x] Self-Containment — every Context ref resolves to an embedded section above; no "see ARCHITECTURE.md" deferrals. DB schema source noted (`migrations/20260413000001_story_069_schema.up.sql:141-157`). PASS.
- [x] Bug Patterns: checked decisions.md § Bug Patterns, three existing PAT-001/002/003 evaluated against scope — none apply; noted candidate PAT-004 for Reviewer consideration.
- [x] Tech Debt: D-025 flagged as the STORY-086 target row; resolution plan is Task 5 step 2.

All checks PASS. Plan ready for Developer dispatch.

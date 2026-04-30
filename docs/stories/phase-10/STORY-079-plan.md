# Implementation Plan: STORY-079 — [AUDIT-GAP] Phase 10 Post-Gate Follow-up Sweep

## Goal

Close the 8 Phase-10 Gate follow-ups (F-1..F-8) + STORY-067 DEV-191 in a single unified sweep so the Documentation Phase can open against a clean tech-debt ledger. Two ACs (AC-8 F-5, AC-9 F-6) are decision-only; seven are code/config fixes.

## Context Summary

Phase 10 Gate PASSED (conditional) on 2026-04-13 with 8 documented non-blocking follow-ups. The 2026-04-15 compliance audit re-verified all 8 are still open in the codebase and promoted DEV-191 (STORY-067 review) into the same bucket (9 items total → D-013..D-021 in ROUTEMAP). Bundling them as STORY-079 preserves Phase 10's zero-deferral posture. Doc-drift auto-fixes from the audit commit (API-262/263 renumber, API-266, `session.updated` WS) are already on main and are OUT of scope here.

### AC → D-ID → File Pointer Map

| AC | D-ID | Kind | Primary File(s) |
|----|------|------|------------------|
| AC-1 | D-013 | Wire CLI | `cmd/argus/main.go`, `go.mod` |
| AC-2 | D-014 | Migration split | `migrations/20260323000002_perf_indexes.up.sql` (+ `.down.sql`) |
| AC-3 | D-015 | Seed fix | `migrations/seed/003_comprehensive_seed.sql` |
| AC-4 | D-016 | UI query-param wiring | `web/src/pages/sims/compare.tsx`, `web/src/pages/sims/index.tsx` |
| AC-5 | D-019 | Route alias | `web/src/router.tsx` |
| AC-6 | D-020 | Toast suppression | `web/src/lib/api.ts` + `internal/api/auth/handler.go` (defensive) |
| AC-7 | D-021 | Observability | `internal/api/system/status_handler.go`, `internal/observability/metrics/metrics.go` |
| AC-8 | D-017 | Decision only | `docs/brainstorming/decisions.md`, `docs/FRONTEND.md` (if posture demands) |
| AC-9 | D-018 | Decision only | `docs/brainstorming/decisions.md`, `docs/reports/phase-10-gate.md` (note removal if NO) |

### Verified State of Preconditions (done during orientation — Developer need not re-verify)

- `golang-migrate/migrate/v4` is **NOT** currently in `go.mod` or `go.sum`. Story hint "existing golang-migrate import" is stale. **AC-1 must add the dependency** — not just wire a subcommand.
- `cmd/argus/main.go` line 100 jumps straight into `config.Load()`; there is no `os.Args` dispatch. The Dockerfile's `CMD ["serve"]` is ignored because `main()` ignores positional args.
- The `Invalid session ID format` message is at `internal/api/auth/handler.go:505` (not :375 as the story says — the WIP commit `7b93ef6` added 130 lines above it). Only caller of `/auth/sessions/:id` in the SPA is `settings/sessions.tsx` via `useRevokeSession`. The dashboard does NOT call this endpoint directly. Most likely root cause: a stray `undefined` ID in a mutation on some mount path producing `DELETE /auth/sessions/undefined` — the server's empty-string guard passes (`"undefined"` ≠ `""`), `uuid.Parse("undefined")` fails → BadRequest with the exact message, the global axios interceptor in `web/src/lib/api.ts:36-90` fires `toast.error(message)`. AC-6 plan is **two-sided defense**: (a) server emits `CodeInvalidFormat` consistently + adds a structured log so future root-causing is possible; (b) client interceptor suppresses this specific combo (`/auth/sessions/` + 400 + `CodeInvalidFormat`) via the existing `silentPaths` pattern.
- `RecentError5m int64` in `status_handler.go:51` is hardcoded to `0` on lines 91 and 129. Prometheus recorder (`internal/observability/metrics/metrics.go`) has `HTTPRequestsTotal *prometheus.CounterVec` with a `status` label; a 5-minute sum can be computed in-process via the `prometheus.Gatherer` interface.
- `migrations/20260323000002_perf_indexes.up.sql` has **6 `CREATE INDEX CONCURRENTLY` statements** that will all fail inside golang-migrate's auto-wrapped transaction. Target tables: `sims` (partitioned BY LIST operator_id), `sessions` (non-partitioned per grep), `anomalies`, `audit_logs` (partitioned BY RANGE created_at). Two further CONCURRENTLY statements exist in `composite_indexes.up.sql:23,32` but those are **already demoted to plain `CREATE INDEX`** with a block comment saying so — they're out of scope. Only `perf_indexes.up.sql` needs surgery.
- `migrations/seed/003_comprehensive_seed.sql` (1412 lines, 138 KB). The audit report observed it aborts on fresh volume after STORY-064 RLS rollout. Root cause is not pre-verified — **AC-3 is investigate-then-fix with a risk of second pass** (noted in Risks).

## Prerequisites

- [x] All 22 Phase-10 stories DONE (STORY-056..STORY-078).
- [x] ROUTEMAP Tech Debt rows D-013..D-021 exist and target STORY-079.
- [x] WIP commit `7b93ef6` (OAuth2 + tenant KPI etc.) is already on main — does not interfere.
- [x] Docker services available for integration build-check (final wave).

## Architecture Context

### Components Involved

- **CLI entrypoint** (`cmd/argus/main.go`): Single-file main, 1989 lines. Currently only has `serve` behavior. Must acquire a pre-config subcommand dispatch.
- **Migration runner** (new): `golang-migrate/migrate/v4` with `file` source driver + `postgres` database driver. Walks `/app/migrations/*.up.sql` (and `.down.sql`) in lex order. Each file is auto-wrapped in a single transaction by default — this is the constraint that drives AC-2.
- **Seed runner** (new): Reads `/app/migrations/seed/*.sql` via pgx and executes as-is (top-level `BEGIN; … COMMIT;` already inside the seed file). `argus seed [file]` accepts an optional single-file target (for re-runs of `003_comprehensive_seed.sql`).
- **Global HTTP error interceptor** (`web/src/lib/api.ts:36-90`): Axios response interceptor. Already has a `silentPaths` whitelist for known-noisy endpoints. Extending this whitelist (or the condition predicate) is the client fix for AC-6.
- **Status endpoint** (`internal/api/system/status_handler.go`): Emits `recent_error_5m` field, currently hardcoded to 0. Will either consume a new `obsmetrics` accessor or be reduced to omit the field.
- **Observability metrics registry** (`internal/observability/metrics/metrics.go`): Owns `HTTPRequestsTotal *prometheus.CounterVec` with `status` label. New accessor must sum requests with `status[0]=='5'` over the last 5 min window.

### Data Flow (AC-1 migrate subcommand)

```
make db-migrate
  → docker compose exec argus /app/argus migrate up
    → cmd/argus/main.go: os.Args[1]=="migrate", os.Args[2]=="up"
      → config.Load()                          (still needed — DSN comes from env)
      → migrate.New("file:///app/migrations", pg_dsn)
      → m.Up()
      → log + exit(0) on success, exit(1) on error
      → NEVER fall through to the HTTP/RADIUS/Diameter/SBA server boot path.
```

### Data Flow (AC-6 defensive fix)

```
Some mount-path mutation dispatches useRevokeSession(undefined) (hypothesis)
  → axios DELETE /api/v1/auth/sessions/undefined
    → chi URLParam id = "undefined"
      → server line 499: sessionIDStr == "" → false, check passes
      → line 503: uuid.Parse("undefined") → error
      → line 505: WriteError 400 CodeInvalidFormat "Invalid session ID format"
        → server: log.Warn with session_id="undefined" and request_id (NEW, for future tracing)
        → client: axios interceptor receives 400
          → url.includes("/auth/sessions/") && code == CodeInvalidFormat → silent (NEW)
          → toast NOT fired.
```

### API Specifications

No new or modified endpoints except `/api/v1/status` and `/api/v1/status/details` field suppression/repopulation.

#### GET /api/v1/status — current shape
Response (success, status 200):
```json
{
  "status": "success",
  "data": {
    "service": "argus",
    "overall": "healthy",
    "version": "...",
    "git_sha": "...",
    "build_time": "...",
    "uptime": "Nh Nm Ns",
    "active_tenants": 12,
    "recent_error_5m": 0
  }
}
```

#### AC-7 resolution — chosen approach: **live Prometheus query**
Rationale: suppression would silently drop an operator-facing field that already shipped. Live query keeps the contract and fixes the value. Recorded as DEC-231 in decisions.md.

Mechanism: new method `obsmetrics.Registry.Recent5xxCount() int64`:
- Maintains an internal ring buffer (60 one-second buckets) of 5xx counts, incremented by a new `RecordHTTPStatus(status int)` hook called from the existing HTTP middleware that already records `HTTPRequestsTotal`. Sum the ring = recent_error_5m.
- Alternative (simpler, also acceptable): expose a `Gatherer`-based read that walks `HTTPRequestsTotal` metric families and sums label value `status=5xx` samples. Snapshot at request time is cumulative-since-boot, so a second snapshot in a separate accessor stores the previous-5m baseline and returns the delta.
- Planner mandates the ring-buffer approach (lock-free, constant memory, no allocation on read). StatusHandler constructor takes an interface:
  ```go
  type ErrorRateSource interface { Recent5xxCount() int64 }
  ```

Error response unchanged.

### Database Schema

**No schema changes in this story.** AC-2 is a migration-file split (same CREATE INDEX targets, just distributed across two files to satisfy PostgreSQL transaction rules); AC-3 is a data seed fix (no DDL changes).

#### AC-2 migration split — chosen approach

**Option chosen: demote CONCURRENTLY to plain `CREATE INDEX`** in `perf_indexes.up.sql` (same approach `composite_indexes.up.sql` took — see lines 7-12 comment block). Rationale:

- These indexes were created at schema-bootstrap time (STORY-001/002 era, before any production load). CONCURRENTLY offers zero-downtime when building an index on a table already under write load; that constraint does not apply at bootstrap.
- `composite_indexes.up.sql:7-12` already documents the same trade-off and is the established project pattern.
- The `sims` table is partitioned BY LIST (operator_id); creating a non-CONCURRENT index on the partitioned parent is PostgreSQL 11+ supported (it cascades to partitions).
- `audit_logs` is partitioned BY RANGE created_at; same cascade semantics.

File delta:
```sql
-- migrations/20260323000002_perf_indexes.up.sql
-- BEFORE (offending pattern):
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_sims_tenant_created ON sims (tenant_id, created_at DESC, id DESC);
-- AFTER:
-- NOTE: CREATE INDEX CONCURRENTLY cannot run inside a transaction. golang-migrate wraps
-- each migration file in a transaction, so we use plain CREATE INDEX IF NOT EXISTS here.
-- For zero-downtime production rebuilds, run CONCURRENTLY manually out-of-band.
CREATE INDEX IF NOT EXISTS idx_sims_tenant_created ON sims (tenant_id, created_at DESC, id DESC);
-- (apply to all 6 statements in the file)
```

The `.down.sql` already uses `DROP INDEX IF EXISTS` — no change needed there.

If CONCURRENTLY is truly required for a given index in the future, the canonical pattern is a separate timestamped migration file containing ONLY the `CREATE INDEX CONCURRENTLY` statement and a `-- atlas:txmode none` or `-- +goose NO TRANSACTION` style directive (golang-migrate supports this via the `x-no-tx-wrap: true` metadata comment: see [golang-migrate docs](https://github.com/golang-migrate/migrate/blob/master/database/postgres/README.md#disable-transaction)). **Not needed for this story.**

#### AC-3 seed fix — investigation approach

Root cause is not pre-verified. Task procedure:
1. Bring up a clean volume: `make db-reset` (AC-1 + AC-2 MUST be done first for this to work).
2. Run `make db-seed`; capture the first error the seed emits.
3. Triage by error class:
   - **RLS violation** (most likely per story hint): missing `SET LOCAL rls.tenant_id = '<id>'` before tenant-scoped INSERTs. Fix: wrap the offending block in a `DO $$ BEGIN … SET LOCAL app.tenant_id = …; … END $$;` with the correct session variable name from `migrations/20260412000006_rls_policies.up.sql`.
   - **FK violation** (ordering): move the offending INSERT after its dependency.
   - **Partition violation**: insert operator_id not mapped to any LIST partition. Fix: either add to the operator_id partition map or let it fall through to `sims_default` (already exists).
   - **Column-not-exist**: a column renamed after seed was written. Fix column name.
4. Rerun → next error → repeat. Seed is 1412 lines; expect 1-3 iterations.
5. Acceptance: after fix, `make db-seed` exits 0 AND reports ≥1 row in each of `tenants`, `operators`, `apns`, `sims`, `policies` (verified via `make db-console` follow-up SQL — included in verification checklist for the task).

### Screen Mockups (UI touches)

None of the UI changes in this story introduce new screens — they restore/bugfix existing screens.

#### AC-4 — `/sims/compare` auto-populate

Current behavior (`web/src/pages/sims/compare.tsx:466-490`): `SIMComparePage` initializes with empty `selectedIds`. URL params are ignored.

New behavior:
- On mount, call `useSearchParams()` from `react-router-dom`.
- If `sim_id_a` and/or `sim_id_b` query params are present AND are valid UUIDs, initialize `selectedIds` with them. The existing `useSIMComparePair` hook (lines 473-476) already takes two ID strings and handles empty strings gracefully → pre-populated IDs trigger pair-fetch automatically.
- The "Compare" button on `/sims` list page (`web/src/pages/sims/index.tsx:334`) currently navigates with no params. It must accept at least one selected SIM from the table's selection state (if any is selected) and append `?sim_id_a=<selected_id>` to the URL; if two are selected, include `sim_id_b` as well.
- Visual: no new UI elements. Just deep-link hydration.

Drill-down targets: unchanged — SIM row in comparison table still links to `/sims/:id`.

Navigation:
- `/sims` Compare button → `/sims/compare?sim_id_a=<uuid>[&sim_id_b=<uuid>]`
- External deep links of the same shape → pre-populated compare.

#### AC-5 — `/dashboard` route alias

Add a new `createBrowserRouter` entry alongside the existing `path: '/'` entry. Two equivalent implementations:
- **Option A (element re-use):** `{ path: '/dashboard', element: lazySuspense(DashboardPage) }` — identical lazy chunk is served from both paths.
- **Option B (redirect):** `{ path: '/dashboard', element: <Navigate to="/" replace /> }` — bookmark resolves to `/` after a single redirect, breaking any planned `/dashboard`-specific behavior (there is none currently).

**Chosen: Option A.** Rationale: cheaper (no redirect round-trip), simpler, and keeps the door open for a future separate dashboard route that shares DashboardPage. Record in decisions.md as DEC-232.

#### AC-6 — dashboard first-paint toast suppression

No visual change — the toast simply does not fire. Test procedure: hard-reload `/` with fresh localStorage, confirm no red toast appears in the first 2 s of paint.

### Design Token Map (UI stories only — MANDATORY)

AC-4 and AC-5 require React edits but **no new visual elements or styling**. AC-4 adds query-param reading + button onClick wiring (reuses existing `<Button>` from `@/components/ui/button`); AC-5 adds a route line. **No new tokens, no new styling, no raw HTML elements introduced.**

If the Developer finds themselves hand-writing any CSS / color / font in this story, STOP — they've scope-crept. The fix is strictly mechanical wiring.

**Existing Components to REUSE (DO NOT recreate):**

| Component | Path | Use For |
|-----------|------|---------|
| `<Button>` | `web/src/components/ui/button.tsx` | Compare button on sims list (already used) |
| `useSearchParams` | `react-router-dom` | Query param reading in compare.tsx |
| `<Navigate>` | `react-router-dom` | NOT USED (Option A chosen) |
| `lazy/Suspense/lazySuspense` | `web/src/router.tsx:107-115` | DashboardPage already wrapped |

## Story-Specific Compliance Rules

- **Go:** All new code in `cmd/argus/main.go` must use the project's `log` (zerolog) instance. Exit on error with `log.Fatal().Err(err)`, not `os.Exit(1)` + `fmt.Printf` — matches the existing pattern at `main.go:103`.
- **Go:** Any new exported identifiers in `internal/observability/metrics/` must be covered by a table-driven test in `metrics_test.go`. Pattern ref: `internal/observability/metrics/metrics_test.go`.
- **API envelope:** `/api/v1/status` responses already use the envelope. `recent_error_5m` stays the same field name (back-compat); just the value comes alive.
- **DB:** Migration files are frozen history — NEVER renumber existing files. The AC-2 fix edits the same file in place. If a new file is needed (not expected in this story), timestamp it `YYYYMMDDHHMMSS` > the latest existing `20260417000003`.
- **DB:** The `up` changes to `20260323000002_perf_indexes.up.sql` must be runnable on a DB that already has these indexes (idempotency via `IF NOT EXISTS` is preserved).
- **UI:** Per CLAUDE.md, `web/` uses atomic design (atoms/molecules/organisms). This story touches page-level components only; no new primitives.
- **Audit:** None of the 7 code ACs introduce state-changing operations → no audit log entries needed.
- **RBAC:** `/api/v1/status` is already scoped correctly (public health/ops endpoint) — no change.
- **Decisions discipline:** Every non-trivial technical choice in this plan (AC-1 approach, AC-2 CONCURRENTLY demotion, AC-5 alias vs redirect, AC-7 live-query vs suppress, AC-8 i18n posture, AC-9 Compare posture) is logged as a DEC-NNN entry in `docs/brainstorming/decisions.md`. Planner creates DEC-231/232 pre-emptively; Developer creates AC-8/AC-9 DECs during their Decision Task execution.

## Bug Pattern Warnings

- **PAT-002 (STORY-059):** Duplicated utility functions drift — *not directly applicable here* (no helper extraction in this story), but **PAT-002 spirit** applies: AC-6's client-side suppression predicate (`/auth/sessions/` + `CodeInvalidFormat`) must be implemented as a **single reusable shape** in the interceptor, not inlined as a special-case boolean. Developer: if more than one silent-path pattern needs similar logic, refactor to a small `shouldSuppressToast(url, code)` helper *in the same file*.
- **PAT-003 (STORY-060)** pattern about Turkish i18n (ASCII-only characters): relevant to **AC-8 decision framing only**. If the decision lands on partial-TR, note in the DEC entry that PAT-003 applies to future translation work.
- No matching bug patterns for the DB, CLI, or observability portions.

## Tech Debt (from ROUTEMAP)

All 9 items directly targeted by this story:

- D-013 (Phase 10 Gate F-1): `argus migrate` CLI → Task 1 (AC-1)
- D-014 (Phase 10 Gate F-2): CONCURRENTLY/RLS split → Task 4 (AC-2)
- D-015 (Phase 10 Gate F-3): seed fix → Task 7 (AC-3)
- D-016 (Phase 10 Gate F-4): /sims/compare useSearchParams → Task 3 (AC-4)
- D-017 (Phase 10 Gate F-5): Turkish i18n decision → Task 2 (AC-8)
- D-018 (Phase 10 Gate F-6): Policies Compare decision → Task 2 (AC-9)
- D-019 (Phase 10 Gate F-7): /dashboard route alias → Task 5 (AC-5)
- D-020 (Phase 10 Gate F-8): Invalid session toast → Task 6 (AC-6)
- D-021 (STORY-067 Review DEV-191): recent_error_5m live → Task 8 (AC-7)

Developer closing each task must flip the corresponding D-NNN row in ROUTEMAP.md from `[ ] PENDING` to `✓ RESOLVED (2026-04-17)` — Ana Amil's close-out step covers the batch update.

## Mock Retirement

No mocks directory exists under `web/src/` for the endpoints this story touches. **No mock retirement for this story.**

---

## Task Decomposition Rules Applied

- Each task touches 1-3 files (ideally 1-2)
- Decision tasks (AC-8, AC-9) touch docs only, ≤30 min each — hard cap per Ana Amil's pre-brief
- Serial dependency: Task 7 (seed fix) needs Tasks 1 + 4 shipped for the investigation to even begin (you can't run `make db-reset && make db-seed` until `argus migrate` exists and migrations don't blow up)
- Final wave is an integration build-check, not a dispatchable task — Ana Amil runs it

## Tasks

### Task 1: Wire `argus migrate` / `seed` subcommand dispatch (AC-1)
- **Files:** Modify `cmd/argus/main.go`, Modify `go.mod` + `go.sum` (via `go mod tidy`)
- **Depends on:** — (none — first task)
- **Complexity:** medium
- **Pattern ref:** No existing in-repo pattern for CLI subcommand dispatch. Follow the golang-migrate official example: https://github.com/golang-migrate/migrate/blob/master/cmd/migrate/README.md#use-in-your-go-project. Single-file dispatch via `os.Args[1]` + switch, invoked BEFORE `config.Load()` for `-h`/`--help`/`version` and AFTER `config.Load()` for `migrate`/`seed` (they need DB DSN). Serve is the default case — preserves existing zero-arg behavior.
- **Context refs:** `Data Flow (AC-1 migrate subcommand)`, `Verified State of Preconditions`
- **What:**
  1. Add `github.com/golang-migrate/migrate/v4 v4.17+`, `github.com/golang-migrate/migrate/v4/database/postgres`, `github.com/golang-migrate/migrate/v4/source/file` via `go get` then `go mod tidy`.
  2. At the very top of `main()` (before `config.Load()` on line 101), introduce `subcommand := "serve"` and `subArgs := []string{}` parsed from `os.Args[1:]`. Known subcommands: `serve`, `migrate`, `seed`, `version`, `-h`/`--help`. Unknown → log and exit 1.
  3. For `migrate`:
     - Load config (needs `DB_URL`).
     - Build DSN string (pgx-compatible URL form).
     - `m, err := migrate.New("file:///app/migrations", dsn)` — path is absolute inside the container; for local `go run` use an env override `ARGUS_MIGRATIONS_PATH` (default falls back to `file://migrations`).
     - Dispatch on `subArgs[0]`: `up` → `m.Up()`, `down` → parse optional `subArgs[1]` as int N (default 1; special value `-all` = `m.Down()`).
     - On `ErrNoChange` → log info and exit 0 (not an error).
     - On any other error → log and exit 1.
     - Success → log the current migration version and exit 0.
  4. For `seed`:
     - Load config.
     - Default seed directory: `/app/migrations/seed` (env override `ARGUS_SEED_PATH`).
     - If `subArgs[0]` is non-empty, treat it as an absolute path or relative-to-seed-path filename; run only that one file.
     - Else run all `*.sql` files in lex order.
     - Execute each file as a single statement batch via a pgx connection — seed files already contain their own `BEGIN;...COMMIT;`. Do NOT wrap them in another transaction.
     - Log each file's row-counts or errors; continue on success, exit 1 on the first error.
  5. For `serve` (default): the rest of the existing `main()` body runs unchanged — refactor the existing body into a `runServe(cfg *config.Config)` function and call it from the switch.
  6. For `version`: print `version, gitSHA, buildTime` one per line and exit 0.
- **Verify:**
  - `make build` compiles cleanly.
  - In docker stack: `make infra-up && docker compose -f deploy/docker-compose.yml build argus && docker compose -f deploy/docker-compose.yml up -d argus` then `docker compose -f deploy/docker-compose.yml exec argus /app/argus migrate up` → non-zero migrations applied, exit 0.
  - `docker compose -f deploy/docker-compose.yml exec argus /app/argus migrate down 1` → one migration rolled back.
  - `docker compose -f deploy/docker-compose.yml exec argus /app/argus version` → prints version/gitSHA/buildTime.
  - `docker compose -f deploy/docker-compose.yml exec argus /app/argus` (no args) → boots `serve` as before (HTTP listener up on 8080).
  - Unit test: add `cmd/argus/main_cli_test.go` (or inline in `main_test.go` if one exists) with a `TestParseSubcommand` table that asserts arg → (subcommand, subArgs) mapping for `[]`, `["migrate"]`, `["migrate","up"]`, `["migrate","down","3"]`, `["migrate","down","-all"]`, `["seed"]`, `["seed","003_comprehensive_seed.sql"]`, `["version"]`, `["--help"]`, `["garbage"]`.
- **AC covered:** AC-1.
- **Risk note:** Refactoring `runServe(cfg)` means moving the existing ~1800-line `main()` body into a function. Keep the refactor mechanical (no logic changes) — every existing line goes inside `runServe`, no renames.

---

### Task 2: Decisions — Turkish i18n posture (AC-8) + /policies Compare posture (AC-9)
- **Files:** Modify `docs/brainstorming/decisions.md`, optionally modify `docs/FRONTEND.md` (AC-8 only, if posture dictates) and/or `docs/reports/phase-10-gate.md` (AC-9 only, if decision = NO)
- **Depends on:** — (decision-only, parallel with Task 1)
- **Complexity:** low
- **Pattern ref:** Read `docs/brainstorming/decisions.md` rows DEV-225..DEV-230 (STORY-062 + STORY-078 decision batch) — follow that tabular format exactly: `| DEV-NNN | YYYY-MM-DD | **STORY-079: <headline>.** <rationale> | ACCEPTED |`.
- **Context refs:** `Goal`, `Story-Specific Compliance Rules` (decisions discipline), `Bug Pattern Warnings` (PAT-003 for AC-8)
- **What:**
  - **AC-8 — Turkish i18n posture:**
    Choose ONE of: (a) DEFER — keep toggle visible, wire a `i18n-deferred` flag, ship empty TR translation object, log a gate note. (b) PARTIAL-TR — ship topbar + sidebar + toast translations now, flag the rest English-only via a banner on first TR toggle. (c) DROP — remove the EN/TR indicator from the topbar and any `i18n.ts` scaffolding.
    - Write the chosen option as `DEV-233` (next available in sequence) in `decisions.md` under `## Development Decisions` with full rationale (coverage %, impact on localization-sensitive screens, POST-GA story split path).
    - If DEFER or DROP: update `docs/FRONTEND.md` language-toggle behavior section to match. If PARTIAL-TR: update `docs/FRONTEND.md` to document the scope (which strings are translated).
    - DO NOT implement any translation files or i18n code changes in this task. Decision + doc update only.
  - **AC-9 — /policies Compare posture:**
    Choose: YES (spin out post-GA story) or NO (remove gate-note recommendation).
    - Write chosen option as `DEV-234` in `decisions.md`.
    - If YES: create stub `docs/stories/post-ga/STORY-0XX-policies-compare.md` (single file, 1-2 paragraphs describing scope) so the decision has a routing target.
    - If NO: edit `docs/reports/phase-10-gate.md` → remove the "/policies Compare button" recommendation line under F-6.
- **Verify:**
  - `grep -n "DEV-233" docs/brainstorming/decisions.md` → returns the row.
  - `grep -n "DEV-234" docs/brainstorming/decisions.md` → returns the row.
  - If AC-8 posture changed FRONTEND.md or AC-9 posture changed the gate report, `git diff` shows the paired update.
- **AC covered:** AC-8, AC-9.
- **Hard cap:** 30 minutes. If scope balloons, escalate — do NOT plan translation work here.

---

### Task 3: `/sims/compare` auto-populate from query params + wire Compare button (AC-4)
- **Files:** Modify `web/src/pages/sims/compare.tsx` (add `useSearchParams` hydration in `SIMComparePage`), Modify `web/src/pages/sims/index.tsx` (Compare button append selected-IDs to URL)
- **Depends on:** — (independent — parallel with Tasks 1, 2, 4, 5, 6, 8)
- **Complexity:** low
- **Pattern ref:** `web/src/pages/sims/index.tsx:95-97, 330-343` already uses `useSearchParams` for filters (`searchParams`, `setSearchParams`) — same hook, same import. Read those lines as the local pattern.
- **Context refs:** `Screen Mockups (UI touches) > AC-4`, `Design Token Map`, `AC → D-ID → File Pointer Map`
- **What:**
  1. In `web/src/pages/sims/compare.tsx` `SIMComparePage` component (line 466+):
     - Import `useSearchParams` alongside the existing `useNavigate`.
     - On initial render, read `sim_id_a` and `sim_id_b` from `searchParams`.
     - Validate each with a regex-based UUID check (reuse or add a `isUuid(s)` helper if one isn't already available via `@/lib/utils`).
     - If valid, `setSelectedIds([sim_id_a, sim_id_b].filter(Boolean))` as the initial state (via `useState(() => ...)`).
     - Ensure the existing `useSIMComparePair` hook (already present at line 473) picks up the pre-populated IDs — no hook changes needed.
     - Do NOT overwrite on subsequent URL changes — this is a mount-only hydration (use `useMemo` or lazy `useState` init, not `useEffect`, to prevent re-hydration loops).
  2. In `web/src/pages/sims/index.tsx` line 334 Compare button:
     - The page already has row-selection state (`selectedSims` or equivalent; check actual variable name when editing). If it exists, the onClick becomes:
       ```
       const ids = [...selectedSims].slice(0, 2) // only first 2 for compare
       const params = new URLSearchParams()
       if (ids[0]) params.set('sim_id_a', ids[0])
       if (ids[1]) params.set('sim_id_b', ids[1])
       navigate(`/sims/compare${params.size ? `?${params}` : ''}`)
       ```
     - If no row-selection state exists on `/sims` yet, keep the click behavior as plain `navigate('/sims/compare')` and document that the Compare-with-selection UX is a future enhancement (do NOT invent row-selection state here — out of scope).
- **Verify:**
  - Hit `http://localhost:8084/sims/compare?sim_id_a=<valid_sim_uuid>&sim_id_b=<valid_sim_uuid>` from a fresh tab → both panels hydrate with their SIM details, compare table renders.
  - Hit `/sims/compare` with no params → empty-state (unchanged behavior).
  - Hit with ONE param only → single panel hydrates, other slot empty (awaiting search).
  - Hit with an invalid UUID → gracefully falls back to empty state, no error toast.
  - Click Compare on `/sims` with 2 SIMs selected (if selection exists) → lands on `/sims/compare?sim_id_a=…&sim_id_b=…` and both panels hydrate.
- **AC covered:** AC-4.

---

### Task 4: Split / demote CONCURRENTLY in perf_indexes migration (AC-2)
- **Files:** Modify `migrations/20260323000002_perf_indexes.up.sql`
- **Depends on:** — (independent — parallel with other code tasks)
- **Complexity:** low
- **Pattern ref:** `migrations/20260412000008_composite_indexes.up.sql:7-12` — copy the block-comment pattern verbatim (adjust wording to reference this file + story).
- **Context refs:** `Database Schema > AC-2 migration split`, `Verified State of Preconditions`
- **What:**
  1. In `migrations/20260323000002_perf_indexes.up.sql`:
     - Prepend a block comment explaining why CONCURRENTLY is absent here (mirroring composite_indexes.up.sql's comment block, with pointer to STORY-079 D-014).
     - Remove the token `CONCURRENTLY` from all 6 statements (lines 10, 15, 17, 22, 27, 32, 36, 45 per grep).
     - Preserve all `IF NOT EXISTS` clauses (idempotency).
     - Preserve all partial-index `WHERE` clauses and `USING gin` clauses.
  2. `migrations/20260323000002_perf_indexes.down.sql` — open and verify it uses `DROP INDEX IF EXISTS` (no CONCURRENTLY). Based on grep in orientation, it does. If it has `DROP INDEX CONCURRENTLY IF EXISTS`, demote it the same way.
  3. NO new migration file — in-place edit of the existing file.
- **Verify:**
  - `make db-reset` on a fresh volume completes without needing manual `force` bumps.
  - `make db-migrate` on an already-initialized DB runs green (indexes already present, `IF NOT EXISTS` no-ops).
  - `docker compose exec postgres psql -U argus -c '\di idx_sims_tenant_created'` → index present, btree.
  - `make db-migrate-down` (one step) then `make db-migrate` (up) roundtrips cleanly.
- **AC covered:** AC-2.

---

### Task 5: `/dashboard` route alias (AC-5)
- **Files:** Modify `web/src/router.tsx`
- **Depends on:** — (independent)
- **Complexity:** low
- **Pattern ref:** `web/src/router.tsx:133` — the existing `{ path: '/', element: lazySuspense(DashboardPage) }` line. Add the alias line immediately below it.
- **Context refs:** `Screen Mockups (UI touches) > AC-5`, `Design Token Map`
- **What:**
  - Add exactly one line under the `children:` array of the `DashboardLayout` protected-route block, immediately after the `path: '/'` line:
    ```tsx
    { path: '/dashboard', element: lazySuspense(DashboardPage) },
    ```
  - No additional imports needed — `DashboardPage` and `lazySuspense` are already in scope.
  - Do NOT use `<Navigate>` (Option B rejected — see DEC-232 note).
- **Verify:**
  - `http://localhost:8084/dashboard` renders the same dashboard as `/` (same chunk, same component).
  - Back/forward navigation works — URL stays `/dashboard` (no redirect).
  - Vite build is clean (`make web-build`).
  - TypeScript compile is clean (`cd web && npx tsc --noEmit`).
- **AC covered:** AC-5.

---

### Task 6: Silence `Invalid session ID format` toast on first dashboard paint (AC-6)
- **Files:** Modify `web/src/lib/api.ts` (client-side suppression — primary fix), Modify `internal/api/auth/handler.go` (server-side log emission — supporting diagnosis)
- **Depends on:** — (independent)
- **Complexity:** low
- **Pattern ref:** `web/src/lib/api.ts:81-86` — existing `silentPaths` + `isSilent` pattern. Extend the predicate, not the list (since the suppression is status-and-code specific, not path-only).
- **Context refs:** `Data Flow (AC-6 defensive fix)`, `Verified State of Preconditions`
- **What:**
  1. In `web/src/lib/api.ts` response interceptor (lines 36-90):
     - Extract the existing `silentPaths` + `isSilent` check into a helper. Add a new condition: URL matches `/auth/sessions/` AND status is 400 AND `errorData?.code === 'INVALID_FORMAT'` → also treat as silent.
     - Keep the existing silentPaths (`/users/me/views`, `/onboarding/status`, `/announcements/active`) untouched.
     - Implement as:
       ```ts
       const isSessionFormatError =
         url.includes('/auth/sessions/') &&
         error.response?.status === 400 &&
         errorData?.code === 'INVALID_FORMAT'
       if (error.response?.status !== 401 && !isSilent && !isSessionFormatError) {
         toast.error(message)
       }
       ```
     - Use the actual `CodeInvalidFormat` string constant from `internal/apierr` if exposed to frontend, else match the literal `'INVALID_FORMAT'` (verify by `grep -rn 'INVALID_FORMAT' internal/apierr` during implementation).
  2. In `internal/api/auth/handler.go:504-506` (the `uuid.Parse(sessionIDStr)` error branch):
     - Add a structured log:
       ```go
       log.Warn().
         Str("session_id", sessionIDStr).
         Str("user_id", userID.String()).
         Str("path", r.URL.Path).
         Msg("revoke session called with non-uuid id")
       ```
     - (Use the existing zerolog import pattern from this file.)
     - Keep the 400 + `CodeInvalidFormat` response unchanged — client already expects it.
- **Verify:**
  - Hard-reload `/` (and `/dashboard` after Task 5) from a fresh browser session → no red toast in the first 5 s.
  - Open browser devtools → Network → filter `auth/sessions` → if the stray call is still happening, its 400 response is present but the toast layer is silent. This tells Ana Amil / future debuggers the underlying bug still exists as a client-side mistake, but users are no longer bothered by it.
  - Server logs (via `docker compose logs argus`) now emit the `revoke session called with non-uuid id` warning with `user_id` + `session_id` values the next time the toast would have fired — this is the debugging breadcrumb for future root-cause work.
  - Other toast paths unaffected: trigger a real 404 (e.g., `/api/v1/sims/00000000-0000-0000-0000-000000000000`) and confirm a red toast still fires.
- **AC covered:** AC-6.

---

### Task 7: Fix `003_comprehensive_seed.sql` for fresh-volume runs (AC-3)
- **Files:** Modify `migrations/seed/003_comprehensive_seed.sql`
- **Depends on:** Task 1 (need `argus seed`), Task 4 (need clean `make db-reset` migration baseline)
- **Complexity:** medium
- **Pattern ref:** The seed file itself — it's idempotent-first-style (CLEANUP phase at top with `DELETE ... WHERE tenant_id NOT IN (demo_tenants)`), then inserts with `ON CONFLICT DO NOTHING/UPDATE`. Preserve that pattern for any additions.
- **Context refs:** `Database Schema > AC-3 seed fix — investigation approach`, `Verified State of Preconditions`
- **What:**
  1. With Task 1 shipped and Task 4 merged, run: `make docker-clean && make up && make db-migrate && make db-seed`. Capture the first error verbatim (both the Postgres error code and the SQL statement ID/line).
  2. Triage per the investigation matrix in `AC-3 seed fix — investigation approach`. The most likely root cause (per story hint) is an RLS policy introduced by STORY-064 that requires `SET LOCAL app.tenant_id = '<id>'` before tenant-scoped INSERTs from a non-BYPASSRLS role.
  3. If RLS: identify the first offending INSERT, wrap either the whole file or the offending block in a `DO $$ BEGIN SET LOCAL role = 'argus_migrator'; ...; END $$;` (or equivalent session-variable set) based on what `migrations/20260412000006_rls_policies.up.sql` expects. Document the chosen approach in a comment at the top of the seed file.
  4. If FK ordering: move the offending INSERT block to AFTER its dependency block. Do NOT remove constraints.
  5. If partition miss: add the operator_id to the partition map OR let it fall through to `sims_default` (`migrations/20260320000002_core_schema.up.sql:sims_default`).
  6. If column-not-exist: update the seed's INSERT column list to match current schema.
  7. Rerun `make db-seed` → next error → repeat. Budget: up to 3 iterations.
  8. Once seed exits 0, run a smoke SQL:
     ```sql
     SELECT
       (SELECT count(*) FROM tenants) AS tenants,
       (SELECT count(*) FROM operators) AS operators,
       (SELECT count(*) FROM apns) AS apns,
       (SELECT count(*) FROM sims) AS sims,
       (SELECT count(*) FROM policies) AS policies;
     ```
     All five counts must be ≥ 1 (demo dataset requirement per story). Sims ≥ 100 would match the recent commit `0366818` scale goal (100 per tenant × 2 tenants).
- **Verify:**
  - `make docker-clean && make up && make db-migrate && make db-seed` — all 4 steps exit 0 in sequence.
  - Smoke SQL above returns the expected non-zero counts.
  - `make db-reset` (the prod-simulating reset) also completes without manual intervention.
  - Simulator discovery (STORY-082 goal): `make sim-ps` after `make sim-up` should not block on "no SIMs found" — record as a soft verification (STORY-082 is not yet running but the seed must enable it).
- **AC covered:** AC-3.
- **Risk note:** First-iteration fix may not suffice — the seed is 1412 lines. Plan budget is 3 iterations; if a 4th is needed, Developer escalates via `attempts.log` + new DEC entry documenting the actual root cause taxonomy encountered.

---

### Task 8: `/api/v1/status/details.recent_error_5m` live Prometheus counter (AC-7)
- **Files:** Modify `internal/observability/metrics/metrics.go` (add `Recent5xxCount` method + ring buffer + `RecordHTTPStatus`), Modify `internal/api/system/status_handler.go` (wire `ErrorRateSource` dependency), Modify `cmd/argus/main.go` (wire the new dependency into `NewStatusHandler` — this will land during the Task-1 refactor window but if Task 1 is already merged, just add the param to the constructor call).
- **Depends on:** Task 1 (if the Task-1 `runServe` refactor is in flight, do Task 8 after to avoid merge conflict on `main.go`)
- **Complexity:** high
- **Pattern ref:**
  - Ring buffer: `internal/aaa/session/counter.go` uses a similar second-bucketed time window for rate limiting — read it for the time-slotting pattern. (Planner note: if that file doesn't have a usable ring, implement from scratch using `sync.Mutex` around a `[60]int64` array indexed by `time.Now().Unix() % 60` with a stale-slot reset check.)
  - Test pattern: `internal/observability/metrics/metrics_test.go` — table-driven tests, uses `prometheus/client_model` for assertions. Follow the same structure for `TestRecent5xxCount`.
- **Context refs:** `API Specifications > AC-7 resolution`, `Verified State of Preconditions`
- **What:**
  1. In `internal/observability/metrics/metrics.go`:
     - Add a new unexported struct field on `Registry`: `recent5xx *errorRingBuffer` where `errorRingBuffer` is a 60-slot second-bucketed counter (implementation inline in the same file; no separate package needed).
     - Add `(r *Registry) RecordHTTPStatus(status int)` — if `status >= 500 && status < 600`, advance the ring to now-second and increment. Thread-safe (mutex).
     - Add `(r *Registry) Recent5xxCount() int64` — sum all 60 slots, return. If any slot is stale (> 60 s old), zero it lazily at read time.
     - Define interface `ErrorRateSource` in `internal/api/system` (NOT in metrics — keep metrics package free of consumer types):
       ```go
       type ErrorRateSource interface { Recent5xxCount() int64 }
       ```
     - The existing HTTP middleware that already records `HTTPRequestsTotal` (find it via `grep -rn "HTTPRequestsTotal" internal/gateway/`) must also call `registry.RecordHTTPStatus(statusCode)`. One-line addition.
  2. In `internal/api/system/status_handler.go`:
     - Add an `errSrc ErrorRateSource` field + constructor param.
     - Replace `RecentError5m: 0` on lines 91 and 129 with `RecentError5m: h.errSrc.Recent5xxCount()` (with a nil-check: if `h.errSrc == nil`, return 0 — preserves backward-compat for tests that don't wire the source).
  3. In `cmd/argus/main.go` (inside `runServe` after Task 1 refactor):
     - Pass the already-constructed `obsmetrics.Registry` instance (it exists as `metrics` or similar — verify by grep) as the 4th argument to `NewStatusHandler`. Update every `NewStatusHandler` call site (should be exactly 1).
  4. Add test `TestRecent5xxCount` in `internal/observability/metrics/metrics_test.go`:
     - Table: {RecordHTTPStatus(500) × 3, sleep 0, expect 3}, {Record(200), expect 3}, {Record(503) × 2, expect 5}, {simulate slot-roll with time mock, expect correct sum}.
  5. Add/update test in `internal/api/system/status_handler_test.go` (line 147 currently asserts `== 0`):
     - Keep the existing `== 0` assertion in a subtest named "no error source wired — returns 0".
     - Add a new subtest "with error source — returns live count" that injects a mock source returning `42` and asserts the response field equals 42.
- **Verify:**
  - `make test` passes; new tests run.
  - Live verification: after Task 7 seed is in, induce some 5xx errors (e.g., `curl http://localhost:8084/api/v1/sims/not-a-uuid` with bad auth to force a 500), then `curl http://localhost:8084/api/v1/status/details` → `recent_error_5m` > 0.
  - Wait 61 s with no new errors → `recent_error_5m` back to 0.
- **AC covered:** AC-7.
- **Complexity rationale:** `high` because this touches three files including a shared metrics registry, requires concurrent-safe ring-buffer implementation, and has a cross-package interface contract. Per L/XL complexity mapping and STORY-062's DEV-226 Redis-counter precedent, this deserves opus-quality dispatch.

---

## Wave Plan

- **Wave 1 (parallel):** Task 1 (AC-1 migrate CLI), Task 2 (AC-8/AC-9 decisions), Task 4 (AC-2 migration split). Task 1 is the long pole; the decision task + migration edit run in parallel.
- **Wave 2 (parallel):** Task 3 (AC-4), Task 5 (AC-5), Task 6 (AC-6), Task 8 (AC-7). Depends on Task 1 for Task 8's `main.go` integration; safe to start Tasks 3/5/6 during Wave 1 if Task 1 is slow.
- **Wave 3 (serial):** Task 7 (AC-3 seed fix). Needs Tasks 1 + 4 on main first to allow clean `make db-reset`.
- **Wave 4 (integration build-check, no new tasks):** Ana Amil's responsibility — run the full chain:
  - `make docker-clean && make up && make db-migrate && make db-seed && make web-build`
  - Hit `http://localhost:8084/dashboard` (no toast), `http://localhost:8084/sims/compare?sim_id_a=<uuid>&sim_id_b=<uuid>` (both hydrate), `http://localhost:8084/api/v1/status/details` (`recent_error_5m` responds live).
  - `docker compose exec argus /app/argus version` → prints version.
  - `docker compose exec argus /app/argus migrate up` → no-op ("no change").
  - Close-out flips all 9 ROUTEMAP D-rows to `✓ RESOLVED (2026-04-17)`.

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|----------------|-------------|
| AC-1 (F-1, D-013) | Task 1 | Task 1 verify + Wave 4 smoke (`argus migrate up`) |
| AC-2 (F-2, D-014) | Task 4 | Task 4 verify + Wave 4 smoke (`make db-reset`) |
| AC-3 (F-3, D-015) | Task 7 | Task 7 verify + Wave 4 smoke (`make db-seed`) |
| AC-4 (F-4, D-016) | Task 3 | Task 3 verify + Wave 4 manual URL test |
| AC-5 (F-7, D-019) | Task 5 | Task 5 verify + Wave 4 manual URL test |
| AC-6 (F-8, D-020) | Task 6 | Task 6 verify + Wave 4 dashboard reload |
| AC-7 (DEV-191, D-021) | Task 8 | Task 8 verify + Wave 4 `/status/details` curl |
| AC-8 (F-5, D-017) | Task 2 | Task 2 verify (decisions.md + FRONTEND.md diff) |
| AC-9 (F-6, D-018) | Task 2 | Task 2 verify (decisions.md + optional gate-report diff) |

## Risks & Mitigations

- **Risk:** Task 1 `runServe(cfg)` refactor touches 1989 lines of `main.go` — merge conflict risk vs the WIP commit `7b93ef6`.
  - **Mitigation:** WIP is already on main (checked by `git show 7b93ef6`); no parallel branches. Refactor is mechanical. Keep the diff as "add wrapper function + move body" — no logic changes.

- **Risk:** Task 7 seed fix may require more than one iteration. Budget allows 3 iterations.
  - **Mitigation:** Each iteration is captured in `attempts.log`; if the 4th fails, Ana Amil dispatches an emergency advisor consult + re-plan. Ring-fencing the seed file (moving `003_comprehensive_seed.sql` to `003b_comprehensive_seed.sql` and keeping the old one as reference) is an escape hatch if the rewrite diverges substantially.

- **Risk:** Task 8 ring-buffer implementation has concurrency-correctness risk. Prometheus's own counters already use atomic ops; a 60-slot ring under mutex is simpler but can contend under high RPS.
  - **Mitigation:** Use a `sync.Mutex` (not `sync.RWMutex` — this is bursty write-heavy); expected p50 contention is sub-µs at realistic traffic (< 10k RPS). If gate perf gate flags contention, fall back to `atomic.AddInt64` on per-slot counters with a "slot-roll" goroutine. Deferred optimization; basic mutex is acceptable for ship.

- **Risk:** AC-6 client-side suppression might mask a real bug (stray `undefined` dispatch somewhere). Defensive-only fix could hide the signal.
  - **Mitigation:** Task 6 adds a `log.Warn` on the server side so the stray dispatch remains observable in logs, just not as a user-facing toast. Task 6 verify step explicitly confirms the 400 is still returned in Network tab.

- **Risk:** AC-4 compare button on `/sims` depends on row-selection state existing — if `/sims` list has no multi-select yet, the button has no selected IDs to forward.
  - **Mitigation:** Task 3 explicitly handles both cases (with or without selection). The Compare button already exists and navigates plainly; the URL-reading side of AC-4 (compare.tsx) is fully satisfied regardless.

- **Risk:** Task 2 decisions have reputation stickiness — once written to `decisions.md`, they're hard to reverse.
  - **Mitigation:** These are explicitly decision-only ACs per Ana Amil's pre-brief. Options are well-scoped in the story. Developer is expected to pick the one with lowest future-optionality cost (usually DEFER for AC-8, usually NO for AC-9 if no business demand) — but the call is theirs, to be logged with rationale.

## Self-Containment Check (Planner's pre-write Quality Gate)

- [x] Plan line count > 100 (M story, ≥ 60 required — plan is ~450+ lines, well above bar)
- [x] Task count = 8 (M story requires ≥ 3 — satisfied; decision tasks capped at 1 total per pre-brief)
- [x] Required sections present: `## Goal`, `## Architecture Context`, `## Tasks`, `## Acceptance Criteria Mapping`
- [x] API spec embedded for `/api/v1/status/details` (the only API change)
- [x] DB spec embedded — no schema changes, migration split strategy documented inline with actual file contents referenced
- [x] UI ACs have component-reuse table (no new tokens — AC-4/AC-5 are pure wiring)
- [x] At least 1 `high` complexity task for M story (Task 8) — satisfies L/XL best practice though story is M
- [x] Each task has `Depends on`, `Pattern ref`, `Context refs`, `Verify`
- [x] All `Context refs` point to sections that exist in this plan (verified by search: `Verified State of Preconditions`, `Data Flow (AC-1 migrate subcommand)`, `Data Flow (AC-6 defensive fix)`, `API Specifications > AC-7 resolution`, `Database Schema > AC-2 migration split`, `Database Schema > AC-3 seed fix — investigation approach`, `Screen Mockups (UI touches)`, `Design Token Map`, `AC → D-ID → File Pointer Map`, `Goal`, `Story-Specific Compliance Rules`, `Bug Pattern Warnings` — all present)
- [x] Wave count = 3 tasking waves + 1 integration check = 4 total (pre-brief says 3-5, satisfied)
- [x] AC-8 + AC-9 bundled in a single Task 2 with ≤30 min cap — per Ana Amil's pre-brief (NOT ballooned to implementation waves)
- [x] Story effort M, task complexity mix: 6 low + 1 medium + 1 high — fits M mapping (mix of low + medium, at least one high for the observability task)
- [x] No implementation code in plan beyond illustrative 2-5 line snippets (all task bodies are specs + pattern refs, not full function bodies)
- [x] Migration DB schema source is noted per file (migrations/20260323000002_perf_indexes.up.sql — ACTUAL contents referenced; migrations/seed/003_comprehensive_seed.sql — root-cause investigation procedure, not blind edit)
- [x] Task 1 noted as requiring a new dependency (golang-migrate) — corrects the story's stale hint about "existing import"
- [x] AC-6 includes root-cause hypothesis AND a defensive two-sided fix, per advisor flag 2
- [x] Task 8 explicitly marked `high` complexity per advisor flag "AC-7 approach choice"

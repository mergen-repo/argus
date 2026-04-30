# Implementation Plan: FIX-245 — Remove 5 Admin Sub-pages + Kill Switches → ENV

## Goal
Delete 5 obsolete admin sub-pages (Cost, Compliance, DSAR, Maintenance, Kill Switches admin UI) end-to-end (FE + BE + CLI + DB) and refactor the kill-switch substrate from DB-backed to env-var-backed with a TTL-cached reader, preserving the existing `IsEnabled(key) bool` interface so RADIUS/Notification/Bulk hot paths stay untouched.

## Architecture Context

### Components Involved (existing — to remove or refactor)

**Frontend (web/src)**
- `pages/admin/cost.tsx`, `pages/admin/compliance.tsx`, `pages/admin/dsar.tsx`, `pages/admin/maintenance.tsx`, `pages/admin/kill-switches.tsx` — pages to delete (5)
- `pages/compliance/data-portability.tsx` — page to delete
- `hooks/use-admin.ts` — hooks `useKillSwitches`, `useToggleKillSwitch`, `useMaintenanceWindows`, `useCreateMaintenanceWindow`, `useDeleteMaintenanceWindow`, `useCostByTenant`, `useDSARQueue`, plus the compliance hooks (lines 109+/126+) — surgical hook removal
- `hooks/use-data-portability.ts` — delete
- `components/layout/sidebar.tsx` — remove DSAR Queue (L124), Cost (L125), Compliance (L126), Kill Switches (L132), Maintenance (L133)
- `router.tsx` — remove 6 lazy imports + 6 route entries (cost L86/L197, compliance L87/L198, dsar L91/L202, kill-switches L94/L205, maintenance L95/L206, data-portability L71/L180)

**Backend (internal/, cmd/)**
- `internal/api/admin/handler.go` — drop `ksSvc` field (L27, L54), drop `KillSwitchSvc` from deps; remove handler refs to removed routes
- `internal/api/admin/kill_switch.go` — delete (admin UI handler — no longer needed)
- `internal/api/admin/cost_by_tenant.go` — delete
- `internal/api/admin/dsar_queue.go` — delete
- `internal/api/admin/maintenance_window.go` — delete
- `internal/api/compliance/handler.go`, `internal/api/compliance/data_portability.go`, `internal/api/compliance/data_portability_test.go`, `internal/api/compliance/handler_test.go` — delete entire `internal/api/compliance/` package (no remaining handlers)
- `internal/compliance/` (compliance service used by handler) — verify usage; if only feeds the deleted handler, delete; if used elsewhere keep
- `internal/store/maintenance_window.go` — delete
- `internal/store/killswitch.go` — delete (DB-backed store no longer needed)
- `internal/store/compliance.go`, `internal/store/compliance_test.go` — delete IF only used by the deleted compliance package; verify
- `internal/store/cost_analytics.go` — verify usage (only used by deleted cost_by_tenant?); delete IF orphan
- `internal/gateway/router.go` — remove route registrations:
  - L796..L800 + L806 (compliance routes — 6 routes)
  - L985 cost/by-tenant
  - L989..L993 kill-switches GET, kill-switches PATCH, maintenance-windows GET/POST/DELETE
  - L1010 dsar/queue
  - L17 import `complianceapi`
  - L166 (`/api/v1/admin/kill-switches` allowlist string in `KillSwitchMiddleware` allowPrefixes — keep allowlist for future ops endpoints? **DROP** — no admin endpoint anymore)
- `internal/gateway/killswitch_middleware.go` — keep file unchanged (uses `killSwitchChecker` interface — agnostic to backend). Update L18 doc comment removing `/api/v1/admin/kill-switches` reference.
- `internal/killswitch/service.go` — REFACTOR: replace DB-backed cache with env-var reader (preserve `IsEnabled(key) bool`). Drop `Toggle`/`Reload`/`GetAll` methods. New constructor `NewService(logger zerolog.Logger) *Service`. Drop `audit` and `store` deps.
- `internal/killswitch/service_test.go` — REWRITE for env-backed implementation
- `cmd/argus/main.go` — line 88 import (keep — refactor consumes), line 1496 (`store.NewKillSwitchStore` — DELETE), line 1498 (`killswitch.NewService(...)` — change signature), lines 1521/1523/1524 (callers — unchanged), line 1747 (`KillSwitchSvc:` in admin deps struct — DELETE)
- `cmd/argusctl/cmd/compliance.go`, `cmd/argusctl/cmd/compliance_test.go` — delete; remove registration from `cmd/argusctl/cmd/root.go`

**Database (migrations/)**
- New forward migration drops `kill_switches` and `maintenance_windows` tables (no data preserve — Risk 2 documented)
- New forward migration removes `data_portability_ready` from notification_templates allowed type list AND deletes any seeded row matching that type (seed/004_notification_templates.sql line 4)
- Cost-related columns (`operator_grants.cost_per_mb`, `operator_grants.sor_priority`, `operator_grants.region`) introduced in `20260321000001_sor_fields.up.sql` — **KEEP**; `cost_per_mb` is used by `internal/cost/service.go` (tenant cost analytics), not just the admin sub-page. AC-3 closed by audit (no orphan cost columns).

**Docs**
- `docs/architecture/CONFIG.md` — append `KILLSWITCH_*` env var section
- `docs/operational/EMERGENCY_PROCEDURES.md` — NEW — emergency toggle runbook

### Data Flow (Kill Switch — refactored)

```
RADIUS Access-Request → killSwitchChecker.IsEnabled("radius_auth")
                          ↓
                       killswitch.Service (NEW: env-backed)
                          ↓
                       cache hit (< 30s)? → return cached bool
                          ↓ miss
                       os.Getenv("KILLSWITCH_RADIUS_AUTH")
                          ↓
                       parse: "off"/"false"/"0" → switch ACTIVE (block)
                              "on"/"true"/"1"/"" (default) → switch INACTIVE (permit)
                          ↓
                       cache result with 30s TTL
                          ↓
                       return bool
```

**Semantic inversion note:** Historically `IsEnabled(key) == true` meant "kill switch fired = block traffic". Env var naming is from the operator's perspective: `KILLSWITCH_RADIUS_AUTH=on` reads naturally as "RADIUS auth is on (permitted)". Implementation maps `on → IsEnabled returns false`, `off → IsEnabled returns true` for the four permit-by-default switches. For `read_only_mode` the convention flips: `KILLSWITCH_READ_ONLY_MODE=on` means read-only IS active, mapping `on → IsEnabled returns true`. **This per-key polarity must be encoded in the parser** (table-driven map) and unit-tested for all 5 keys to prevent the operator-vs-internal semantic confusion (PAT-024 territory).

### API Specifications (routes being REMOVED)
- `GET /api/v1/admin/cost/by-tenant` — DELETE
- `GET /api/v1/admin/kill-switches` — DELETE
- `PATCH /api/v1/admin/kill-switches/{key}` — DELETE
- `GET /api/v1/admin/maintenance-windows` — DELETE
- `POST /api/v1/admin/maintenance-windows` — DELETE
- `DELETE /api/v1/admin/maintenance-windows/{id}` — DELETE
- `GET /api/v1/admin/dsar/queue` — DELETE
- `GET /api/v1/compliance/dashboard` — DELETE
- `GET /api/v1/compliance/btk-report` — DELETE
- `PUT /api/v1/compliance/retention` — DELETE
- `GET /api/v1/compliance/dsar/{simId}` — DELETE
- `POST /api/v1/compliance/erasure/{simId}` — DELETE
- `POST /api/v1/compliance/data-portability/{user_id}` — DELETE

After: 13 routes removed. All return 404 post-deploy.

### Database Schema

**Source: migrations/20260416000001_admin_compliance.up.sql (ACTUAL)** — current state of tables to drop:

```sql
CREATE TABLE kill_switches (
    key VARCHAR(64) PRIMARY KEY,
    label VARCHAR(128) NOT NULL,
    description TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT false,
    reason TEXT,
    toggled_by UUID REFERENCES users(id),
    toggled_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE maintenance_windows (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID REFERENCES tenants(id),
    title VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    starts_at TIMESTAMPTZ NOT NULL,
    ends_at TIMESTAMPTZ NOT NULL,
    affected_services VARCHAR[] NOT NULL DEFAULT '{}',
    cron_expression VARCHAR(100),
    notify_plan JSONB NOT NULL DEFAULT '{}',
    state VARCHAR(20) NOT NULL DEFAULT 'scheduled' CHECK (state IN ('scheduled','active','completed','cancelled')),
    created_by UUID REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (ends_at > starts_at)
);
```

**Forward migration `20260504000001_drop_admin_compliance.up.sql`:**
```sql
DROP TABLE IF EXISTS kill_switches CASCADE;
DROP TABLE IF EXISTS maintenance_windows CASCADE;
```

**Down migration `20260504000001_drop_admin_compliance.down.sql`:** re-create both tables verbatim from `20260416000001` (so rollback path stays intact).

**Forward migration `20260504000002_remove_data_portability_template.up.sql`** (AC-9 leftover — verified still present in seed/004):
```sql
DELETE FROM notification_templates WHERE type = 'data_portability_ready';
ALTER TABLE notification_templates DROP CONSTRAINT IF EXISTS notification_templates_type_check;
ALTER TABLE notification_templates ADD CONSTRAINT notification_templates_type_check
  CHECK (type IN ('welcome','sim_state_change','session_login','onboarding_completed'));
```

(Verify the actual constraint name first via `\d notification_templates`. If FIX-237 already changed this constraint, simply DELETE the row.)

Also edit `migrations/seed/004_notification_templates.sql` line 4 — remove `'data_portability_ready'` from the inline allowed-types list.

### KILLSWITCH ENV Var Reference

| Env Var | Default (unset) | "on" semantic | "off" semantic | IsEnabled returns when... |
|---------|-----------------|---------------|----------------|---------------------------|
| `KILLSWITCH_RADIUS_AUTH` | `on` (permit) | RADIUS auth permitted | RADIUS auth blocked | `=off` → true (block) |
| `KILLSWITCH_SESSION_CREATE` | `on` (permit) | session creation permitted | session creation blocked | `=off` → true (block) |
| `KILLSWITCH_BULK_OPERATIONS` | `on` (permit) | bulk ops permitted | bulk ops blocked | `=off` → true (block) |
| `KILLSWITCH_EXTERNAL_NOTIFICATIONS` | `on` (permit) | notifications dispatched | notifications suppressed | `=off` → true (block) |
| `KILLSWITCH_READ_ONLY_MODE` | `off` (mutations OK) | read-only mode active | mutations permitted | `=on` → true (block) |

**TTL cache:** `IsEnabled` reads `os.Getenv` once per key per 30s, stored in a `sync.Map`-style cache keyed by switch name. Cache miss → read env, parse, store. No goroutine, no SIGHUP; restart required to apply env change in production. This is acceptable per AC-17 / Risk 1.

**Parser polarity table** (in code, internal):
```go
var permitByDefault = map[string]bool{
    "radius_auth": true, "session_create": true,
    "bulk_operations": true, "external_notifications": true,
    "read_only_mode": false,
}
```

### Frontend Sidebar — Final State (post-FIX-245)

ADMIN group AFTER removals:
- Tenant Usage (FIX-246)
- Security Events
- Sessions (kept here — FIX-247 is the next story to remove it)
- API Usage
- Purge History
- Delivery Status
- Announcements (preserved per AC-16)
- Impersonate (existing)

Removed: DSAR Queue, Cost, Compliance, Kill Switches, Maintenance.

## Prerequisites
- [x] FIX-237 closed (event taxonomy added; `data_portability_ready` template deletion explicitly deferred to FIX-245 — verified by grep on `seed/004_notification_templates.sql:4`)
- [x] FIX-246 closed (Tenant Usage merged — sidebar position confirmed)
- [x] FIX-240 closed (sidebar reduction pattern established)

## Tasks

(Five waves; 14 tasks total. Most tasks medium; 2 high — env-backed killswitch refactor + main.go rewiring.)

---

### Wave 1 — Migrations & Seed Cleanup (parallel)

#### Task 1: Drop kill_switches + maintenance_windows tables
- **Files:** Create `migrations/20260504000001_drop_admin_compliance.up.sql`, `migrations/20260504000001_drop_admin_compliance.down.sql`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `migrations/20260416000001_admin_compliance.up.sql` (the migration we are reversing) and any other `*_drop_*.up.sql` if present. If none, follow the standard `DROP TABLE IF EXISTS ... CASCADE` pattern.
- **Context refs:** "Database Schema"
- **What:** UP drops both tables (CASCADE — kill_switches has FKs from users; maintenance_windows from users + tenants). DOWN copies the verbatim CREATE TABLE blocks from `20260416000001_admin_compliance.up.sql` including indexes + RLS for maintenance_windows (omit the seed INSERT — it's not needed for rollback).
- **Verify:** `make db-migrate` applies clean; rollback `make db-migrate-down` recreates both tables. Run `psql -c "\dt kill_switches"` → not found post-up; found post-down.

#### Task 2: Remove data_portability_ready notification template
- **Files:** Create `migrations/20260504000002_remove_data_portability_template.up.sql`, `migrations/20260504000002_remove_data_portability_template.down.sql`; Modify `migrations/seed/004_notification_templates.sql` (line 4 — remove `'data_portability_ready'` from inline allowed-types tuple)
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `migrations/seed/004_notification_templates.sql` for the existing CHECK constraint pattern; read recent FIX-237 migration `20260501000002` (per FIX-237 step-log Wave D) for tuple-update style.
- **Context refs:** "Database Schema"
- **What:** UP `DELETE FROM notification_templates WHERE type = 'data_portability_ready'`; if a CHECK constraint enumerates allowed types, redefine without `data_portability_ready`. DOWN re-adds (no-op for the template row — operator can re-seed if needed). Edit the seed file to remove the literal so `make db-seed` stays clean (PAT-007 — never defer seed failures).
- **Verify:** `make db-migrate` clean; `make db-seed` clean; `psql -c "SELECT type FROM notification_templates"` → no `data_portability_ready` row.

---

### Wave 2 — Backend Removals (parallel after Wave 1)

#### Task 3: Delete admin handler files (cost, dsar, maintenance, kill_switch admin)
- **Files:** Delete `internal/api/admin/cost_by_tenant.go`, `internal/api/admin/dsar_queue.go`, `internal/api/admin/maintenance_window.go`, `internal/api/admin/kill_switch.go` plus matching `*_test.go` siblings if present
- **Depends on:** Task 1 (table drop must precede handler delete OR be paired in the same release; for build correctness either order works since deleted handlers don't reference dropped tables at runtime — but plan-wise Wave 2 follows Wave 1)
- **Complexity:** low
- **Pattern ref:** None — pure deletion.
- **Context refs:** "API Specifications"
- **What:** Delete all 4 handler files + tests. Modify `internal/api/admin/handler.go`: drop the `ksSvc *killswitch.Service` field (L27), drop the constructor parameter (L54) — note: this changes `NewHandler` signature → main.go must update (Task 9).
- **Verify:** `go build ./internal/api/admin/...` → must FAIL temporarily until router (Task 5) and main.go (Task 9) update; final `go build ./...` PASS gate is Task 14.

#### Task 4: Delete compliance package + DSAR/data_portability backend
- **Files:** Delete `internal/api/compliance/` (4 files: `handler.go`, `data_portability.go`, `*_test.go`); Delete `internal/store/maintenance_window.go`, `internal/store/killswitch.go`. Audit `internal/store/compliance.go`, `internal/store/cost_analytics.go`, `internal/compliance/` package — delete IF and only IF zero references outside the deleted files. Run `grep -rn 'store.ComplianceStore\|store.CostAnalyticsStore\|compliance\.Service' internal/ cmd/` first.
- **Depends on:** Task 3 (admin handler must shed dependencies first)
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/maintenance_window.go` to confirm orphan-status; same for `cost_analytics.go`.
- **Context refs:** "Components Involved", "API Specifications"
- **What:** Tree-walk dependency audit before deletion — anything imported by `internal/cost/service.go` (used by tenant_usage analytics) MUST stay. The cost_analytics store file is suspect — if used only by `cost_by_tenant.go` handler, delete; if used by `internal/cost/service.go`, keep. Document the keep/delete decision per file in the commit message.
- **Verify:** `go build ./...` PASS after Task 5.

#### Task 5: Remove route registrations + middleware allowlist refs
- **Files:** Modify `internal/gateway/router.go` (delete L17 import, L166 allowlist string, L796..L800 + L806 compliance routes, L985 cost route, L989..L993 kill-switches + maintenance routes, L1010 dsar route); Modify `internal/gateway/killswitch_middleware.go` (update L18 doc comment — remove `/api/v1/admin/kill-switches` reference)
- **Depends on:** Task 3, Task 4
- **Complexity:** medium
- **Pattern ref:** Read `internal/gateway/router.go` lines 950-1020 for the route registration block syntax; read FIX-240 router diff (commit log entry for FIX-240).
- **Context refs:** "API Specifications", "Components Involved"
- **What:** Delete 13 route lines + 1 import + 1 allowlist string + 1 doc comment. After this task plus Task 3/4, `go build ./internal/gateway/...` must pass.
- **Verify:** `go build ./internal/gateway/...` PASS; `grep -n 'admin/cost\|admin/dsar\|admin/maintenance\|admin/kill-switches\|/compliance' internal/gateway/router.go` → zero matches.

#### Task 6: Delete CLI compliance command
- **Files:** Delete `cmd/argusctl/cmd/compliance.go`, `cmd/argusctl/cmd/compliance_test.go`; Modify `cmd/argusctl/cmd/root.go` (remove `rootCmd.AddCommand(complianceCmd)` registration)
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `cmd/argusctl/cmd/compliance.go` to identify exact command variable name; read `cmd/argusctl/cmd/root.go` for AddCommand pattern.
- **Context refs:** "Components Involved"
- **What:** Remove the CLI binding so `argusctl compliance ...` is no longer available.
- **Verify:** `go build ./cmd/argusctl/...` PASS; `./argusctl --help` does not list `compliance`.

---

### Wave 3 — Kill Switch Refactor (sequential after Wave 2)

#### Task 7: Refactor killswitch.Service to env-backed reader
- **Files:** Rewrite `internal/killswitch/service.go`; Rewrite `internal/killswitch/service_test.go`
- **Depends on:** Task 3 (admin handler must no longer reference Toggle/Reload/GetAll)
- **Complexity:** **high**
- **Pattern ref:** Read `internal/config/config.go` (or similar env loader) for the project's env-parsing convention. Read `internal/killswitch/service.go` original to preserve method signature `IsEnabled(key string) bool`. PAT-017 (env var threading) — read `docs/brainstorming/bug-patterns.md` for env var pitfalls.
- **Context refs:** "Architecture Context > Data Flow", "KILLSWITCH ENV Var Reference"
- **What:**
  - New `Service` struct: `cache map[string]cacheEntry`, `mu sync.RWMutex`, `ttl time.Duration` (default 30s), `clock func() time.Time` (test seam), `getenv func(string) string` (test seam — defaults to `os.Getenv`)
  - `cacheEntry { value bool; expiresAt time.Time }`
  - `NewService(logger zerolog.Logger) *Service` (no store, no audit deps)
  - `IsEnabled(key string) bool` — fast path: read RLock, check cache; if entry valid, return value. Slow path: take Lock, double-check, fetch env via `s.getenv("KILLSWITCH_" + strings.ToUpper(key))`, parse against the per-key polarity table (constant map), store in cache with expiresAt = now + ttl. Unknown keys → log Warn once and return false (safe = permit, matches original "unknown keys never block" semantic).
  - Drop `Toggle`, `Reload`, `GetAll` (no callers after Task 3).
  - Tests cover: (a) all 5 known keys with each env value (`on`,`off`,`true`,`false`,`1`,`0`,unset), (b) cache TTL — first read hits getenv, second read within TTL does NOT call getenv (assert via call counter on `s.getenv` test seam), (c) per-key polarity (`KILLSWITCH_RADIUS_AUTH=off` → IsEnabled returns true; `KILLSWITCH_READ_ONLY_MODE=on` → IsEnabled returns true), (d) unknown key returns false, (e) cache expiry triggers re-read.
- **Verify:** `go test ./internal/killswitch/...` all PASS; `go vet ./internal/killswitch/...` clean.

#### Task 8: Verify caller compatibility (RADIUS / Notification / Bulk)
- **Files:** No source changes expected. Audit only. If any caller uses methods other than `IsEnabled`, file a follow-up.
- **Depends on:** Task 7
- **Complexity:** low
- **Pattern ref:** Read `internal/aaa/radius/server.go:40`, `internal/notification/service.go:208-254`, `internal/api/sim/bulk_handler.go:24-83` — confirm all three only consume `IsEnabled(key) bool`.
- **Context refs:** "Components Involved", "Data Flow"
- **What:** Run `grep -n 'killswitch\.' internal/ cmd/` and confirm only `IsEnabled` and `NewService` are referenced post-Task 7. The local `killSwitchChecker interface { IsEnabled(key string) bool }` declarations in 3 caller files satisfy the contract — no changes needed there.
- **Verify:** `go build ./...` PASS; `grep -n '\.Toggle\|\.Reload\|\.GetAll' internal/ cmd/` → zero matches in killswitch context.

#### Task 9: Wire env-backed service in main.go + drop store
- **Files:** Modify `cmd/argus/main.go`
- **Depends on:** Task 7, Task 3
- **Complexity:** **high**
- **Pattern ref:** Read `cmd/argus/main.go` lines 1490-1530 (current killswitch wiring) and lines 1740-1760 (admin deps struct).
- **Context refs:** "Components Involved"
- **What:**
  - Delete L1496 `killSwitchStore := store.NewKillSwitchStore(pg.Pool)`
  - Replace L1498 `killswitch.NewService(killSwitchStore, auditSvc, log.Logger)` → `killswitch.NewService(log.Logger)`
  - Lines 1521/1523/1524 (callers `radiusServer.SetKillSwitch`, `bulkHandler.SetKillSwitch`, `notifSvc.SetKillSwitch`) — UNCHANGED
  - L1747 `KillSwitchSvc: killSwitchSvc` — DELETE (admin handler no longer takes ksSvc per Task 3); fix the admin handler constructor call site to match new signature
  - Audit any `store.NewKillSwitchStore` call site — must be zero post-Task 4
- **Verify:** `go build ./cmd/argus/...` PASS; `make build` PASS; bring up service: `./argus serve` starts without errors; logs show no killswitch-related panics.

---

### Wave 4 — Frontend Removals (parallel after Wave 3 completes; Wave 4 has no Wave 3 dependency but ordering minimizes broken-state windows)

#### Task 10: Delete admin pages + compliance page + hooks
- **Files:** Delete `web/src/pages/admin/cost.tsx`, `web/src/pages/admin/compliance.tsx`, `web/src/pages/admin/dsar.tsx`, `web/src/pages/admin/maintenance.tsx`, `web/src/pages/admin/kill-switches.tsx`, `web/src/pages/compliance/data-portability.tsx`, `web/src/hooks/use-data-portability.ts`. Modify `web/src/hooks/use-admin.ts` — remove hooks: `useKillSwitches`, `useToggleKillSwitch`, `useMaintenanceWindows`, `useCreateMaintenanceWindow`, `useDeleteMaintenanceWindow`, `useCostByTenant`, `useDSARQueue`, plus the compliance hooks declared around lines 109/126.
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read FIX-240 commit (page+hook deletion pattern) and FIX-246 plan for the merge precedent.
- **Context refs:** "Components Involved"
- **What:** Pure deletion. After hook surgery, audit `web/src/types/` for orphan types (`KillSwitch`, `MaintenanceWindow`, `CostTenant`, `DSARQueueItem`, `ComplianceDashboard`, `BTKReport`, `RetentionConfig`, `DataPortability*`) and delete the type definitions (or strip from a shared types file). Run `pnpm tsc --noEmit` after — should expose any leftover imports to fix.
- **Verify:** `pnpm --dir web tsc --noEmit` PASS; `grep -rn 'use-data-portability\|useKillSwitches\|useMaintenanceWindows\|useCostByTenant\|useDSARQueue' web/src` → zero matches.

#### Task 11: Sidebar + Router cleanup
- **Files:** Modify `web/src/components/layout/sidebar.tsx` (remove DSAR Queue L124, Cost L125, Compliance L126, Kill Switches L132, Maintenance L133); Modify `web/src/router.tsx` (remove 6 lazy imports + 6 route entries — see "Components Involved" for line list)
- **Depends on:** Task 10 (deleted pages must be gone before router refs vanish, else Vite throws on missing module during HMR)
- **Complexity:** low
- **Pattern ref:** Read FIX-240 sidebar+router diff for precedent.
- **Context refs:** "Components Involved", "Frontend Sidebar — Final State"
- **What:** Remove 5 sidebar entries; remove 6 route entries (5 admin + 1 compliance/data-portability) + 6 corresponding `lazy()` imports.
- **Verify:** `pnpm --dir web tsc --noEmit` PASS; `pnpm --dir web build` PASS; manual check `grep -n '/admin/cost\|/admin/dsar\|/admin/maintenance\|/admin/compliance\|/admin/kill-switches\|/compliance/data-portability' web/src/router.tsx web/src/components/layout/sidebar.tsx` → zero.

---

### Wave 5 — Tests + Docs

#### Task 12: Update / delete impacted tests
- **Files:** Delete `internal/api/admin/*_test.go` for removed handlers (cost, dsar, maintenance, kill_switch admin); audit `internal/gateway/*_test.go` for tests hitting removed routes; audit `cmd/argusctl/cmd/compliance_test.go` (already deleted in Task 6 — confirm); audit `internal/api/compliance/*_test.go` (already deleted in Task 4 — confirm). Add NEW tests in `internal/killswitch/service_test.go` already covered by Task 7.
- **Depends on:** Task 5, Task 6, Task 7, Task 9
- **Complexity:** medium
- **Pattern ref:** Read existing `internal/gateway/*_test.go` to find route-specific tests.
- **Context refs:** "API Specifications"
- **What:** Sweep `grep -rn '/admin/cost\|/admin/dsar\|/admin/maintenance\|/admin/kill-switches\|/compliance' internal/ cmd/` after all deletions — every remaining hit must be a deliberate keep (e.g., `internal/cost/service.go`) or be cleaned. Adjust integration tests if any seed `kill_switches` rows.
- **Verify:** `make test` PASS; `go test ./... -count=1` PASS.

#### Task 13: Documentation updates
- **Files:** Modify `docs/architecture/CONFIG.md` (append new section "Kill Switch Env Vars" — table from "KILLSWITCH ENV Var Reference"); Create `docs/operational/EMERGENCY_PROCEDURES.md` (NEW); Modify `docs/architecture/ALGORITHMS.md` IF kill-switch algorithm section exists; Modify `docs/SCREENS.md` to remove the 5 removed admin sub-pages from the screen index; Modify `docs/ROUTEMAP.md` Tech Debt section if D-150..D-156 mention compliance/maintenance/cost/dsar as targets.
- **Depends on:** Task 7
- **Complexity:** medium
- **Pattern ref:** Read `docs/architecture/CONFIG.md` for the env var section format; read `docs/architecture/DEPLOYMENT.md` for an example operational runbook style.
- **Context refs:** "KILLSWITCH ENV Var Reference", "Frontend Sidebar — Final State"
- **What:**
  - CONFIG.md: add 5 env vars with default + semantic + restart-required note + TTL (30s).
  - EMERGENCY_PROCEDURES.md: 4 sections — (1) When to use kill switches, (2) How to toggle (export env + `make restart`), (3) Per-switch effect & verification command, (4) Rollback (unset env + restart). Include the exact docker-compose env file path and example `KILLSWITCH_RADIUS_AUTH=off` line.
  - SCREENS.md: remove 5 entries from the index.
  - Release notes block (in plan file or commit body): "kill_switches DB rows preserved? NO — drop is destructive; verify production has no actively-toggled switches before deploy" (Risk 2).
- **Verify:** `markdownlint docs/operational/EMERGENCY_PROCEDURES.md` PASS (if linter present); manual review of CONFIG.md table formatting.

#### Task 14: Final regression gate (full build + lint + grep sweeps)
- **Files:** No code change — gate task.
- **Depends on:** ALL prior tasks
- **Complexity:** medium
- **Pattern ref:** Read FIX-237 step-log Wave F (final gate pattern).
- **Context refs:** —
- **What:** Run in order:
  1. `go build ./...` — PASS
  2. `go test ./... -count=1` — PASS
  3. `make db-migrate` from clean DB → PASS
  4. `make db-seed` → PASS (no `data_portability_ready` errors)
  5. `pnpm --dir web tsc --noEmit` → PASS
  6. `pnpm --dir web build` → PASS
  7. `goimports -l ./...` → zero unimported orphans
  8. `grep -rn 'admin/cost\|admin/dsar\|admin/maintenance\|admin/kill-switches\|/compliance/' internal/ cmd/ web/src/` → all hits must be in deleted-test contexts (zero in source)
  9. Bring service up via `make up`; smoke-test: `curl http://localhost:8084/api/v1/admin/cost/by-tenant` → 404; `curl http://localhost:8084/api/v1/admin/kill-switches` → 404; login as admin and confirm sidebar reduced to expected entries; navigate to `/admin/cost` URL → 404 page or redirect.
  10. Set `KILLSWITCH_RADIUS_AUTH=off` in docker-compose env, restart, send a RADIUS Access-Request → Access-Reject. Restore default; restart → Access-Accept. (Manual or scripted.)
- **Verify:** All 10 checks PASS → ready to commit.

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| AC-1 (delete cost.tsx + types + hooks) | Task 10 | Task 14.5/14.8 |
| AC-2 (cost handler removed + route) | Task 3, Task 5 | Task 14.1/14.8/14.9 |
| AC-3 (cost migration columns audit) | Task 4 (audit) | Task 14.4 — `cost_per_mb` kept (used by `internal/cost`) |
| AC-4 (delete compliance pages + types + hooks) | Task 10 | Task 14.5 |
| AC-5 (delete CLI compliance.go) | Task 6 | Task 14.1 |
| AC-6 (compliance summary removed from UI) | Task 10, Task 11 | Task 14.9 |
| AC-7 (delete dsar.tsx + use-data-portability hook) | Task 10 | Task 14.5 |
| AC-8 (DSAR queue handler + route removed) | Task 3, Task 5 | Task 14.1/14.9 |
| AC-9 (DSAR Admin sub-page UI/hooks/handler/store/CLI) | Task 6 (CLI), Task 3 (handler), Task 4 (store), Task 10 (UI/hooks) — FIX-237 done event-taxonomy half | Task 14.8 |
| AC-10 (delete data_portability.go) | Task 4 | Task 14.1 |
| AC-11 (delete data_portability_ready template from seed) | Task 2 | Task 14.4 |
| AC-12 (delete maintenance.tsx) | Task 10 | Task 14.5 |
| AC-13 (maintenance-windows handler + 3 routes removed) | Task 3, Task 5 | Task 14.1 |
| AC-14 (delete maintenance_window store) | Task 4 | Task 14.1 |
| AC-15 (drop maintenance_windows table) | Task 1 | Task 14.3 |
| AC-16 (announcements PRESERVED) | Task 10/11 explicit non-touch | Task 14.9 (sidebar still has Announcements) |
| AC-17 (env-backed killswitch.Service refactor) | Task 7 | Task 14.2 |
| AC-18 (kill_switches table dropped + admin UI delete) | Task 1, Task 10 | Task 14.3/14.5 |
| AC-19 (RADIUS hot path unchanged) | Task 8 (audit), Task 9 (wiring) | Task 14.10 |
| AC-20 (Notification kill-switch interface stable) | Task 8 | Task 14.2 (passes existing notification tests) |
| AC-21 (main.go env-backed wiring) | Task 9 | Task 14.1/14.10 |
| AC-22 (CONFIG.md + EMERGENCY_PROCEDURES.md docs) | Task 13 | Task 14 visual |
| AC-23 (sidebar 5 ADMIN items removed) | Task 11 | Task 14.9 |
| AC-24 (router 5 routes removed) | Task 11 | Task 14.5/14.6 |
| AC-25 (full regression gate) | Task 12, Task 14 | Task 14 (10 checks) |

All 25 ACs mapped.

## Story-Specific Compliance Rules

- API: Standard envelope still required for all REMAINING admin endpoints — deletions only.
- DB: Forward + down migration pair MANDATORY (Task 1, Task 2). `make db-seed` must stay clean (PAT-007 / no_defer_seed memory) — Task 2 edits the seed file.
- Go: `goimports -w ./...` after deletions (no orphan imports — Task 14.7).
- UI: No new pages introduced (constraint #9). Sidebar grouping preserved (Announcements stays — AC-16).
- Business: Kill switches default to PERMIT for the 4 traffic switches; `read_only_mode` defaults to OFF (mutations allowed). Verified against AC-17.
- ADR: No ADR changes needed — kill switch refactor is implementation-only (interface preserved).

## Bug Pattern Warnings

Read `docs/brainstorming/bug-patterns.md`:

- **PAT-006 (SQL Scan recurrence):** Not directly applicable — no new SQL scans introduced. But Task 1 down-migration must verify column types match exactly (TIMESTAMPTZ not TIMESTAMP, VARCHAR length identical) to avoid scan errors in any rollback test path.
- **PAT-007 (no defer seed):** **APPLIES.** Task 2 MUST remove `data_portability_ready` from `seed/004_notification_templates.sql` line 4 in the same task. Never leave the seed broken with a "fix later" note.
- **PAT-017 (env var threading):** **APPLIES — high impact for Task 7.** All `os.Getenv` calls must be encapsulated in the `Service` (no callers reach into env directly). Provide the `getenv func(string) string` test seam so tests can inject env without setting process env (avoids test parallelism flakes).
- **PAT-024 (Operator vs Internal Semantic Confusion):** **APPLIES — central to Task 7.** Operator says "RADIUS auth is `on`" (= permitted) but internal `IsEnabled` semantic says "the kill switch is `enabled`" (= blocking). The polarity table in `internal/killswitch/service.go` MUST be unit-tested per-key with explicit assertions to prevent future operators reading the docs as "set to off to disable" and accidentally blocking production.
- **PAT-025:** Re-check before commit (whatever pattern numbered 25 currently flags — read once at Task 14).

## Tech Debt (from ROUTEMAP)

D-150..D-156 routed by FIX-237 — confirm none are blocked by FIX-245 implementation. No tech debt items target FIX-245 directly per the routemap inspection at planning time. If any items reference compliance/cost/dsar/maintenance pages — they auto-resolve as the page is deleted; mark as RESOLVED in commit body.

## Mock Retirement
No mocks directory in `web/src/` for the affected pages. No mock retirement needed.

## Risks & Mitigations

- **Risk 1 — Kill switch env change requires restart.** Mitigation: documented in EMERGENCY_PROCEDURES.md (Task 13). Future SIGHUP reload is out of scope (per AC-17 wording).
- **Risk 2 — Existing kill_switches DB rows lost on migration.** Mitigation: Pre-deploy operations checklist in EMERGENCY_PROCEDURES.md release notes section: `psql -c "SELECT key, enabled FROM kill_switches WHERE enabled = true"` BEFORE migration. If any rows return true, set the equivalent env var BEFORE deploy. Documented as one-time migration concern.
- **Risk 3 — Cost/Compliance/DSAR-related notification publishers leftover.** Mitigation: Task 4 grep audit + Task 14 final grep gate. The `notif.Notify(... event="data_portability.ready" ...)` call site (originally inside `internal/api/compliance/data_portability.go`) is removed by Task 4 — no separate cleanup needed.
- **Risk 4 — Maintenance windows referenced by AAA scheduled downtime logic.** Pre-checked via grep: only test references exist (per F-313 finding). No production AAA code consumes `maintenance_windows`.
- **Risk 5 — `internal/store/cost_analytics.go` keep/delete decision.** Mitigation: Task 4 explicitly does the dependency audit. Decision recorded in commit body. If kept, it must compile clean post-removal (no dangling imports).
- **Risk 6 — `internal/api/admin/handler.go` constructor signature change cascades.** Mitigation: Task 3 changes the signature; Task 9 updates the only call site (`cmd/argus/main.go`). Build will be red between Task 3 and Task 9 — Wave 3 sequencing enforces this.
- **Risk 7 — TypeScript orphan types after hook deletion.** Mitigation: Task 10 ends with `tsc --noEmit` to surface every leftover.

## Pre-Validation Self-Check

- [x] Min lines (L story → 100 min): plan ≈ 320 lines — PASS
- [x] Min tasks (L → 5): 14 tasks — PASS
- [x] Required headers: Goal, Architecture Context, Tasks, Acceptance Criteria Mapping — PASS
- [x] Embedded specs: API list embedded; DB schema embedded with source ref; env var table embedded — PASS
- [x] At least 1 high-complexity task: Task 7 + Task 9 marked high — PASS
- [x] Context refs validated against existing section headers — PASS
- [x] All 25 ACs mapped — PASS
- [x] Pattern refs on every new-file task (Task 1, 2, 7, 13) — PASS
- [x] Sidebar/router final state listed — PASS (Announcements preserved)
- [x] No new pages introduced — PASS
- [x] FIX-237 AC-9 verification: `data_portability_ready` STILL in `migrations/seed/004_notification_templates.sql:4` — Task 2 will remove it. PASS.
- [x] Migration drop order safe (CASCADE handles FKs) — PASS
- [x] Test cleanup scope clear (Task 12) — PASS

## Quality Gate: PASS

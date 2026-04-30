# Implementation Plan: FIX-233 — SIM List Policy Column + Rollout Cohort Filter

> Wave 4 of UI Review Remediation. Effort: **M** (touches BE filter+JOIN+migration, FE column+chips+URL+WS, plus a new `GET /policy-rollouts` list endpoint to populate the cohort dropdown). Mode: AUTOPILOT.

## Goal

Make the SIM List page first-class for policy administration and rollout investigation: surface each SIM's active policy + version as a sortable, drill-down column; let admins filter the list by policy, policy version, or a specific rollout cohort (incl. per-stage 1% / 10% / 100% slices); honour deep-link URLs from the rollout panel; and refresh on `policy.rollout_progress` WS events while a cohort filter is active. Fix F-149 (silent invalid-UUID fallback → 400).

## Architecture Reference

### Components Involved

| # | Component | Layer | Path | Responsibility |
|---|-----------|-------|------|----------------|
| C1 | `SIMStore.ListEnriched` | BE store | `internal/store/sim.go` | Add LEFT JOIN to `policy_assignments` for `rollout_id` / `stage_pct` / `coa_status` exposure + new filter predicates. **Use `s.policy_version_id` directly for the policy filters** (FIX-231 trigger keeps it canonical) — JOIN to `policy_assignments` only for cohort/CoA filters. |
| C2 | `SIM` model + `SIMWithNames` | BE store | `internal/store/sim.go` | Add nullable fields `RolloutID *uuid.UUID`, `RolloutStagePct *int`, `CoaStatus *string` populated from JOIN. |
| C3 | `ListSIMsParams` | BE store | `internal/store/sim.go` | Add `PolicyVersionID *uuid.UUID`, `PolicyID *uuid.UUID`, `RolloutID *uuid.UUID`, `RolloutStagePct *int`. |
| C4 | `sim.Handler.handleListSIMs` | BE API | `internal/api/sim/handler.go` | Parse new query params with strict UUID validation → 400 (fix F-149). Map to `ListSIMsParams`. |
| C5 | SIM DTO `toSIMResponse` | BE API | `internal/api/sim/handler.go::toSIMResponse` | Surface `rollout_id`, `rollout_stage_pct`, `coa_status` (FIX-202 already exposes `policy_name`, `policy_version_id`, `policy_version_number`; AC-5 partially satisfied — extend, do not duplicate). |
| C6 | `policy.Handler.ListRollouts` (NEW) | BE API | `internal/api/policy/handler.go` | New `GET /api/v1/policy-rollouts?state=in_progress,paused` returning bounded list of active/recent rollouts with `policy_id`, `policy_name`, `policy_version`, `state`, `current_stage`, `total_sims`, `migrated_sims`. Used by FE cohort dropdown. |
| C7 | `PolicyStore.ListRollouts` (NEW) | BE store | `internal/store/policy.go` | Query: `SELECT … FROM policy_rollouts r JOIN policy_versions pv … JOIN policies pol … WHERE pol.tenant_id = $1 AND r.state = ANY($2) ORDER BY r.created_at DESC LIMIT 100`. |
| C8 | `policy_assignments` schema | DB migration | `migrations/2026MMDDHHMMSS_policy_assignments_stage_pct.{up,down}.sql` | Add `stage_pct INT` column + index `(rollout_id, stage_pct)`. Backfill: NULL for pre-existing rows (correct semantics — cohort filter requires a value, NULL means "stage unknown/legacy"). |
| C9 | `AssignSIMsToVersion` writer | BE store | `internal/store/policy.go::AssignSIMsToVersion` (called from `internal/policy/rollout/service.go::executeStage`) | Accept and persist `stagePct int` so future cohort queries can pinpoint the migration stage. **PAT-011/PAT-017 risk** — enumerate every call site (rollout `executeStage`, any test fixtures, any migration backfill) and pass the param. |
| C10 | `useSIMList` + `SIMListFilters` | FE hook+types | `web/src/hooks/use-sims.ts`, `web/src/types/sim.ts` | Add `policy_id`, `policy_version_id` (already present), `rollout_id`, `rollout_stage_pct`. Forward in `buildListParams`. |
| C11 | `useRolloutList` (NEW) | FE hook | `web/src/hooks/use-policies.ts` | `useQuery(['policy-rollouts', 'list', state])` → `GET /policy-rollouts?state=in_progress,paused`. |
| C12 | SIM list page | FE page | `web/src/pages/sims/index.tsx` | Add Policy column (after APN, before IP Pool); two new filter chips (Policy multi-select + Rollout Cohort dropdown w/ stage submenu); ingest `policy_version_id` / `policy_id` / `rollout_id` / `rollout_stage_pct` from `useSearchParams`; reset filter on Clear All; subscribe to `wsClient.on('policy.rollout_progress')` and `refetch()` when the active filter's `rollout_id` matches. |
| C13 | SIM types | FE types | `web/src/types/sim.ts` | Add `rollout_id?`, `rollout_stage_pct?`, `coa_status?` to `SIM`; add `policy_id?`, `rollout_id?`, `rollout_stage_pct?` to `SIMListFilters`. |

### Data Flow

```
User clicks "View Migrated SIMs" on RolloutActivePanel (already exists, FIX-232)
  → navigates to /sims?rollout_id=<UUID>
SIM List page mounts
  → useSearchParams() reads rollout_id, rollout_stage_pct, policy_id, policy_version_id
  → useSIMList(filters) issues GET /sims?rollout_id=…&rollout_stage_pct=…
  → handleListSIMs parses + validates UUIDs (invalid → 400)
  → SIMStore.ListEnriched joins policy_assignments WHERE pa.rollout_id = $N (and pa.stage_pct = $M when set)
  → DTO returns rows with rollout_id/stage_pct/coa_status populated
  → FE renders Policy column + active filter chips ("Cohort: <Policy v3> stage 1%")
WS event 'policy.rollout_progress' arrives with matching rollout_id
  → SIM list invalidates query → refetches → table updates with new migrated set

Cohort dropdown population:
  Page mount → useRolloutList('in_progress,paused') → GET /policy-rollouts?state=…
  → fills Cohort dropdown with active rollouts (≤100, no pagination needed)
```

### API Specifications

#### `GET /api/v1/sims` (extended)

New query params (all OPTIONAL, all UUIDs validated; `rollout_stage_pct` is integer-validated):

| Param | Type | Validation | Effect |
|-------|------|-----------|--------|
| `policy_id` | UUID | strict UUID parse → 400 | filter rows where `s.policy_version_id IN (SELECT id FROM policy_versions WHERE policy_id = $X)` |
| `policy_version_id` | UUID | strict UUID parse → 400 (replaces F-149 silent-fallback) | filter rows where `s.policy_version_id = $X` |
| `rollout_id` | UUID | strict UUID parse → 400 | adds INNER JOIN to `policy_assignments pa ON pa.sim_id = s.id` and `pa.rollout_id = $X` |
| `rollout_stage_pct` | integer ∈ {1,5,10,25,50,100} (or any int 1..100) | strict int parse → 400 | combined with `rollout_id` only — adds `pa.stage_pct = $X`. **Without `rollout_id` → 400 `INVALID_PARAM`** ("rollout_stage_pct requires rollout_id") |

Success response: standard list envelope `{status, data: SIM[], meta: {cursor, limit, has_more}}`. Error response: `{status:"error", error:{code:"INVALID_FORMAT"\|"INVALID_PARAM", message}}`. Status codes: 200, 400, 401, 500.

DTO additions on each `SIM` element (extend FIX-202 envelope; existing fields untouched):

```jsonc
{
  "policy_name": "Demo Premium",                       // FIX-202 existing — RESOLVED via h.policyStore.GetVersionByID
  "policy_version_id": "…",                            // FIX-202 existing
  "policy_version_number": 3,                          // FIX-202 existing
  "rollout_id": "…",                                   // NEW (nullable — populated only via JOIN)
  "rollout_stage_pct": 1,                              // NEW (nullable)
  "coa_status": "pending"                              // NEW (nullable; values: 'pending'|'sent'|'acked'|'failed')
}
```

#### `GET /api/v1/policy-rollouts` (NEW)

| Param | Type | Effect |
|-------|------|--------|
| `state` | CSV of `pending,in_progress,paused,completed,rolled_back,aborted` | filter; default = `in_progress,paused` |
| `limit` | int ≤ 100 | default 50; bounded |

Response: `{status:"success", data: RolloutSummary[]}` where `RolloutSummary = { id, policy_id, policy_name, policy_version, state, current_stage, total_sims, migrated_sims, started_at, created_at }`. **No cursor pagination** — active rollouts are bounded. Status codes: 200, 400 (invalid state value), 401, 500.

### Database Schema

**Source: `migrations/20260320000002_core_schema.up.sql:343-358` (ACTUAL)** — TBL-15 already exists. This story ADDS one column.

```sql
-- Existing (FIX-231 made this the canonical source for SIM↔policy_version mapping)
CREATE TABLE policy_assignments (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sim_id            UUID NOT NULL,
    policy_version_id UUID NOT NULL REFERENCES policy_versions(id),
    rollout_id        UUID,                                -- FK to policy_rollouts(id), added later in same migration
    assigned_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    coa_sent_at       TIMESTAMPTZ,
    coa_status        VARCHAR(20) DEFAULT 'pending'        -- pending|sent|acked|failed
);
CREATE UNIQUE INDEX idx_policy_assignments_sim ON policy_assignments (sim_id);
CREATE INDEX idx_policy_assignments_version ON policy_assignments (policy_version_id);
CREATE INDEX idx_policy_assignments_rollout ON policy_assignments (rollout_id);
CREATE INDEX idx_policy_assignments_coa ON policy_assignments (coa_status) WHERE coa_status != 'acked';
```

**This story adds:**

```sql
-- New: migrations/2026MMDDHHMMSS_policy_assignments_stage_pct.up.sql
ALTER TABLE policy_assignments ADD COLUMN stage_pct INT;
-- Composite index supports the cohort+stage filter and the SIM-list rollout JOIN
CREATE INDEX IF NOT EXISTS idx_policy_assignments_rollout_stage
    ON policy_assignments (rollout_id, stage_pct);
COMMENT ON COLUMN policy_assignments.stage_pct IS
  'Percent target of the rollout stage at which this SIM was migrated (1, 10, 100, …). NULL for legacy rows pre-FIX-233 — cohort filter excludes them.';
```

```sql
-- New: migrations/2026MMDDHHMMSS_policy_assignments_stage_pct.down.sql
DROP INDEX IF EXISTS idx_policy_assignments_rollout_stage;
ALTER TABLE policy_assignments DROP COLUMN IF EXISTS stage_pct;
```

**Backfill semantics:** legacy rows stay `NULL`. Cohort filter `WHERE pa.rollout_id = $X AND pa.stage_pct = $Y` naturally excludes NULL — correct behaviour: if we don't know which stage migrated a SIM, the filter cannot claim it. Post-migration `make db-seed` MUST still pass — verify on fresh volume per `feedback_no_defer_seed` and PAT-014.

### Screen Mockups

> SCR-020 (SIM List) — embedded selectively; full mockup in `docs/SCREENS.md`.

```
┌────────────────────────────────────────────────────────────────────────────────────────────────┐
│ SIMs                                                              [+ Import]  [Export ▾]       │
│ ───────────────────────────────────────────────────────────────────────────────────────────── │
│ [Search ICCID/IMSI/MSISDN/IP]  [State ▾] [RAT ▾] [Operator ▾] [APN ▾] [Policy ▾] [Cohort ▾]   │
│  ↑ existing chips                                              ↑ NEW   ↑ NEW                  │
│  Active filters: [Cohort: Demo Premium v3 · stage 1%  ✕] [Policy: Demo Premium  ✕]            │
│ ───────────────────────────────────────────────────────────────────────────────────────────── │
│ [✓] ICCID            IMSI            MSISDN     IP        State   Operator  APN     Policy   IP Pool   RAT  Created │
│ [ ] 89014103211… 234150100100001  +90555…  10.0.0.5  ACTIVE  Vodafone   m2m.io  Demo v3 default-pool   4G   2 days │
│ [ ] 89014103211… 234150100100002  +90555…  10.0.0.6  ACTIVE  Vodafone   m2m.io  Demo v3 default-pool   4G   2 days │
│  ↑ new "Policy" column between APN and IP Pool, hidden < 1280px (showed via row hover tooltip) │
└────────────────────────────────────────────────────────────────────────────────────────────────┘
```

Cohort dropdown layout:

```
┌────────────────────────────────────────┐
│ Active Rollouts                        │
│ ────────────────────────────────────── │
│ ◯ All rollouts                         │
│ ● Demo Premium v3 (1% canary)  ← sel.  │
│   ↳ Stage filter:                      │
│      ◯ All migrated                    │
│      ● Stage 1% (2 SIMs)               │
│      ◯ Stage 10% (20 SIMs)             │
│      ◯ Stage 100% (200 SIMs)           │
│      ◯ Pending (not yet migrated)      │
└────────────────────────────────────────┘
```

- **Navigation in:** (a) sidebar `Manage > SIMs`, (b) deep-link from `RolloutActivePanel.View Migrated SIMs` button (`/sims?rollout_id=…`, already implemented at `web/src/components/policy/rollout-active-panel.tsx:368-378` — verified), (c) URL with any combination of `policy_id`, `policy_version_id`, `rollout_id`, `rollout_stage_pct`.
- **Drill-down targets:** Policy column → `/policies/<policy_id>`. Cohort filter chip click → keep filter; ✕ → clear that one chip. Row click → `/sims/<sim_id>` (existing).

### Design Token Map (UI Compliance)

#### Color Tokens (FRONTEND.md, FIX-227 PAT-018 enforced)

| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Primary text (cells, headers) | `text-text-primary` | `text-gray-900`, `text-[#E4E4ED]` |
| Secondary/data text (mono cells) | `text-text-secondary` | `text-gray-500`, `text-[#7A7A95]` |
| Muted ("—" placeholder, em dash) | `text-text-tertiary` | `text-gray-400` |
| Policy name link in column | `text-accent hover:underline` | `text-blue-500`, `text-cyan-400` |
| Cohort chip background | `bg-accent-dim` | `bg-cyan-100`, `bg-[rgba(0,212,255,0.15)]` |
| Cohort chip text | `text-accent` | `text-cyan-500` |
| Filter chip "Policy" border | `border-border` | `border-gray-200` |
| Stage label muted | `text-text-secondary` | `text-gray-500` |
| Active row hover | `bg-bg-hover` | `bg-gray-50` |
| Table header bg | `bg-bg-elevated` | `bg-white`, `bg-slate-50` |
| Empty state text | `text-text-secondary` | `text-gray-500` |
| Danger / invalid input flash | `border-danger` | `border-red-500`, `border-[#FF4466]` |

**PAT-018 grep:** dev MUST run `grep -nE '\btext-(red\|blue\|green\|purple\|pink\|orange\|yellow\|amber\|cyan\|teal\|sky\|indigo\|violet\|fuchsia\|rose)-[0-9]{2,3}\b\|\bbg-(red\|blue\|green\|purple\|pink\|orange\|yellow\|amber\|cyan\|teal\|sky\|indigo\|violet\|fuchsia\|rose)-[0-9]{2,3}\b' web/src/pages/sims/index.tsx web/src/components/sims/` and accept ZERO matches in changed regions.

#### Typography Tokens

| Usage | Token Class |
|-------|-------------|
| Table cell (default 13px) | `text-xs` (Tailwind 12px) for mono cells matches existing pattern in file |
| Policy column "Demo Premium v3" | `text-xs text-accent hover:underline` |
| Filter chip label | `text-xs font-medium` |
| Cohort dropdown header "Active Rollouts" | `text-[10px] uppercase tracking-wider text-text-secondary` (matches existing dropdown header pattern at `index.tsx`) |
| Stage submenu items | `text-xs` |

#### Spacing & Elevation Tokens

| Usage | Token Class |
|-------|-------------|
| Filter chip gap | `gap-2` (project default) |
| Cohort dropdown panel | `rounded-md border border-border bg-bg-elevated p-2` |
| Cohort stage submenu indent | `pl-4` |
| Policy column min-width | `min-w-[120px]` (acceptable — table cell sizing not in token map per FIX-219 D-105 precedent; cite as known-acceptable) |

#### Existing Components to REUSE (DO NOT recreate)

| Component | Path | Use For |
|-----------|------|---------|
| `<Table>`, `<TableHeader>`, `<TableRow>`, `<TableHead>`, `<TableCell>` | `web/src/components/ui/table.tsx` | NEW Policy column header + cell |
| `<Badge>` | `web/src/components/ui/badge.tsx` | If we render coa_status pill anywhere |
| Existing dropdown pattern in `pages/sims/index.tsx:551-700` (State / RAT / Operator / APN / Policy filter chips, anchored `<button>` + `<div role="menu">`) | inline component pattern | Cohort dropdown — follow this exact pattern; do NOT introduce a new `Popover` primitive |
| `<Link>` from `react-router-dom` | already imported via `useNavigate` etc. | Policy name → `/policies/<policy_id>` |
| `wsClient` from `web/src/lib/ws-client.ts` | already used at `rollout-tab.tsx:290` | `wsClient.on('policy.rollout_progress', cb)` |
| `EntityLink` (from FIX-219) | `web/src/components/atoms/entity-link.tsx` | Policy column cell — gives consistent hovercard + right-click copy. **VERIFY entity type 'policy' is supported** before relying on it; fallback to plain `<Link>` if not. |
| `useSearchParams` | `react-router-dom` | URL ingestion — pattern at `pages/sims/index.tsx:104-127` |

**RULE:** Do NOT add `dangerouslySetInnerHTML`, `alert()`, `confirm()`, raw `<button>`, or new color hex literals.

## Prerequisites

- [x] FIX-202 (DONE) — SIM DTO name resolution exposes `policy_name`, `policy_version_id`, `policy_version_number`. AC-5 partially satisfied; this story only ADDS three new fields.
- [x] FIX-230 (DONE) — DSL→SQL whitelist + accurate `total_sims`. NOT in this story's path; D-139 remains OPEN, **do not regress** the typed-wrapper deferral.
- [x] FIX-231 (DONE) — `policy_assignments` is canonical source; `sims.policy_version_id` is trigger-synced. Allows `policy_version_id` filter to use the FAST `s.policy_version_id` path without an extra JOIN.
- [x] FIX-232 (DONE) — `RolloutActivePanel` already renders `<Link to="/sims?rollout_id={id}">` (verified `web/src/components/policy/rollout-active-panel.tsx:368-378`). AC-9 only needs URL ingestion in this story; no FIX-232 changes required.

## Story-Specific Compliance Rules

- **API:** Standard envelope on all `/sims` and new `/policy-rollouts` responses. Strict UUID parsing → 400 `INVALID_FORMAT` (fix F-149); strict int parsing → 400 `INVALID_FORMAT`. Combined-param validation: `rollout_stage_pct` without `rollout_id` → 400 `INVALID_PARAM`.
- **DB:** Migration up + down required. New index `idx_policy_assignments_rollout_stage`. No data backfill (NULL is correct semantics). Run `make db-seed` on fresh volume in Gate per PAT-014 / `feedback_no_defer_seed`.
- **Multi-tenant:** Every JOIN inherits via `s.tenant_id = $1` on the `sims` row (existing `simEnrichedJoin` already enforces this for `apns` / `policies`). New `policy_assignments` JOIN does not need its own `tenant_id` predicate (the table is implicitly tenant-scoped via `sim_id` + the existing `s.tenant_id` predicate). Reviewer: verify with EXPLAIN that planner uses `idx_policy_assignments_sim` on the join.
- **UI tokens:** PAT-018 enforced — no `text-red-500` / `bg-blue-100` style default-palette utilities in changed regions.
- **Atomic design:** New filter chips are inline patterns matching existing chips in same file (NOT new molecules). New Policy column cell is inline JSX (matches APN cell pattern).
- **ADR:** ADR-001 (modular monolith — single binary; new `policy.Handler.ListRollouts` lives in existing `internal/api/policy` package). ADR-002 (JWT) — protected route via existing middleware chain. ADR-003 — N/A.
- **WS event name:** Use the canonical `policy.rollout_progress` (story doc has stale `policy.rollout.progressed` — flag as doc drift). Reference: `internal/bus/nats.go:34`, `internal/policy/rollout/service.go:589`, `internal/ws/hub.go:341`. Documented in DEV-NNN proposal below.
- **Performance (AC-10):** Adding `LEFT JOIN policy_assignments` to `simEnrichedJoin` MUST keep p95 < 150ms at 50-row LIMIT. Already-existing `idx_policy_assignments_sim` (UNIQUE on `sim_id`) makes the JOIN a single-row index lookup per outer row. Plan includes EXPLAIN-plan task as MANDATORY (not advisory) per advisor input.
- **PAT-011/PAT-017 wiring trace:** When adding `stagePct int` to `AssignSIMsToVersion` signature, dev MUST grep every call site and update each one — `grep -rn 'AssignSIMsToVersion(' internal/ cmd/` and ensure all sites pass the new arg.

## Bug Pattern Warnings

Pulled from `docs/brainstorming/bug-patterns.md` — patterns whose `Affected` overlap this story's scope. Dev MUST read each before implementing the cited task.

- **PAT-006 (FIX-201):** Shared payload struct field silently omitted at construction sites. **Applies to:** Task 1 (`ListSIMsParams` field additions) — when developer adds `PolicyVersionID/PolicyID/RolloutID/RolloutStagePct`, grep every `ListSIMsParams{` literal in `internal/api/sim/handler.go` and tests, set each one explicitly. Add a regression test that POSTs with each new param and asserts the SQL predicate fires.
- **PAT-011 + PAT-017 (FIX-207, FIX-210):** Plan-specified wiring missing at construction sites — silent no-op. **Applies to:** Task 4 (`AssignSIMsToVersion` signature change). Dev MUST `grep -rn 'AssignSIMsToVersion(' internal/ cmd/` and update every call site (rollout `executeStage`, any tests). Add an integration test that asserts `stage_pct` actually persists for a real rollout stage execution.
- **PAT-012 (FIX-208):** Cross-surface count drift. **Applies to:** Task 1 — confirm we filter `policy_version_id` from the canonical `s.policy_version_id` (FIX-231 trigger-synced) NOT from `policy_assignments.policy_version_id`, otherwise we'd re-introduce the dual-source split that FIX-231 closed. Comment the SQL with `// canonical: sims.policy_version_id (FIX-231 trigger-synced)`.
- **PAT-009 (FIX-204):** Nullable column scan into Go string panics. **Applies to:** Task 2 (`SIMWithNames` extension) — `coa_status` and `stage_pct` are nullable; scan into `*string` / `*int`, NOT `string` / `int`. The new SELECT list MUST `LEFT JOIN` policy_assignments so missing rows don't drop the SIM.
- **PAT-014 (FIX-211):** Seed-time CHECK violations surface only after migration. **Applies to:** Task 0 (migration) — `stage_pct` is nullable + no CHECK constraint, so seed should not break, but Gate MUST run `make db-seed` on a fresh volume per `feedback_no_defer_seed` to confirm.
- **PAT-015 (FIX-209):** Declared-but-unmounted React component. **Applies to:** Task 7 (FE filter chips) — Policy + Cohort dropdown patterns are inline in `pages/sims/index.tsx` (no new component file expected); but if dev factors out a `<CohortFilter>` molecule, Gate MUST grep mount count vs declaration count.
- **PAT-016 (FIX-209):** Cross-store PK confusion. **Applies to:** Task 7 — Policy column "policy_name" link must navigate to `/policies/<policy_id>` (NOT `/policies/<policy_version_id>`). Verify the DTO carries `policy_id` separately (extend FIX-202 if it doesn't) — see open question OQ-1 below.
- **PAT-018 (FIX-227):** Default Tailwind color utility. **Applies to:** Task 6/7 — grep changed regions for default-palette utilities.

## Tech Debt (from ROUTEMAP)

Reviewed `docs/ROUTEMAP.md` Tech Debt table for items targeting FIX-233 — **none target this story directly**. Adjacent items to be aware of:

- **D-139 (FIX-230 → FIX-243):** typed `dsl.SQLPredicate` wrapper. NOT in this story's path; we don't touch `SelectSIMsForStage` or `CountWithPredicate`. **Don't regress.**
- **D-137, D-138 (FIX-231 → DEFERRED):** trigger perf, test hardening. We rely on the trigger but don't modify it.
- **D-105 (FIX-219, FIX-24x):** arbitrary px tokens (`text-[12px]` etc.) — table-cell density precedent. We continue this precedent for the Policy column min-width without adding a new violation.

No NEW tech debt items planned. Reviewer may add DEV-NNN if cohort dropdown semantics need refinement post-Gate.

## Mock Retirement

`web/src/mocks/` does not exist in this project (verified). N/A.

---

## Tasks

> Granularity: 1-3 files per task. Wave assignment in parentheses (W1=BE foundation, W2=FE filter+column, W3=tests + perf). Tasks within a wave can be parallelized when no dependency edges exist.

### Task 0 (W1): Migration — `policy_assignments.stage_pct`
- **Files:** Create `migrations/<TS>_policy_assignments_stage_pct.up.sql`, `migrations/<TS>_policy_assignments_stage_pct.down.sql`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** `migrations/20260427000002_reconcile_policy_assignments.up.sql` — single-table additive migration with backfill comment.
- **Context refs:** "Database Schema", "Story-Specific Compliance Rules" (DB)
- **What:** Add nullable `stage_pct INT` column. Create composite index `idx_policy_assignments_rollout_stage (rollout_id, stage_pct)`. Add COMMENT documenting NULL semantics. Down: drop index, drop column.
- **Verify:** `make db-migrate` clean on fresh volume; `\d policy_assignments` shows new column + index; `make db-seed` clean (PAT-014 / `feedback_no_defer_seed`).

### Task 1 (W1): `ListSIMsParams` + handler param parsing + 400 on invalid UUID
- **Files:** Modify `internal/store/sim.go` (struct), Modify `internal/api/sim/handler.go` (parser)
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** existing `operator_id` / `apn_id` parse blocks at `internal/api/sim/handler.go:540-558` (strict UUID parse → 400 `INVALID_FORMAT`).
- **Context refs:** "API Specifications > GET /sims (extended)", "Components Involved > C3, C4", "Bug Pattern Warnings > PAT-006"
- **What:** (a) Add fields `PolicyVersionID *uuid.UUID`, `PolicyID *uuid.UUID`, `RolloutID *uuid.UUID`, `RolloutStagePct *int` to `ListSIMsParams`. (b) In handler, parse each query param with `uuid.Parse` (and `strconv.Atoi` for `rollout_stage_pct`); on parse error return `apierr.WriteError(w, 400, CodeInvalidFormat, …)` matching the existing `operator_id` pattern. (c) Validate combined: `rollout_stage_pct` set without `rollout_id` → 400 `INVALID_PARAM` ("rollout_stage_pct requires rollout_id"). (d) Range-check `rollout_stage_pct ∈ [1,100]` → 400 if out of range. (e) Map all four params into the `params := store.ListSIMsParams{…}` literal at line 560 (PAT-006 — explicit assignment).
- **Verify:** unit test cases for invalid UUID, invalid int, stage without rollout, out-of-range stage all return 400 with correct error code.

### Task 2 (W1): Store JOIN + filter predicates + nullable scan
- **Files:** Modify `internal/store/sim.go` (`simEnrichedJoin`, `simEnrichedSelect`, `SIMWithNames`, `buildSIMWhereClause`, `scanSIMWithNames`)
- **Depends on:** Task 0, Task 1
- **Complexity:** **high** (touches the hot list path; SQL+performance critical; multiple scan sites)
- **Pattern ref:** existing `simEnrichedJoin` at `internal/store/sim.go:1404-1408` and `buildSIMWhereClause` at `:1426-1502` — extend, don't rewrite.
- **Context refs:** "Components Involved > C1, C2", "Database Schema", "Bug Pattern Warnings > PAT-009, PAT-012", "Story-Specific Compliance Rules"
- **What:** (a) Extend `simEnrichedJoin` with `LEFT JOIN policy_assignments pa ON pa.sim_id = s.id`. (b) Extend `simEnrichedSelect` with `, pa.rollout_id, pa.stage_pct, pa.coa_status`. (c) Add nullable fields `RolloutID *uuid.UUID`, `RolloutStagePct *int`, `CoaStatus *string` to `SIMWithNames`. (d) Update `scanSIMWithNames` to scan new columns into pointer types (PAT-009). (e) Extend `buildSIMWhereClause` to emit predicates: `s.policy_version_id = $N`; `s.policy_version_id IN (SELECT id FROM policy_versions WHERE policy_id = $N AND tenant_id = $1)`; `pa.rollout_id = $N`; `pa.stage_pct = $N`. **CRITICAL:** filter by `s.policy_version_id` (canonical, FIX-231 trigger-synced) NOT `pa.policy_version_id` (PAT-012). Add an SQL comment to that effect. (f) Apply same JOIN to `Count` and any peer enriched query that participates in the SIM list endpoint. Run `grep -n 'simEnrichedJoin\|simEnrichedSelect' internal/store/sim.go` — found 3 sites at lines 1525, 1558, 1589; verify each still works after column-list change.
- **Verify:** `go build ./...` clean; `go vet ./...` clean; existing `internal/store/sim_test.go` (if present) still passes.

### Task 3 (W1): SIM DTO extension — `rollout_id`, `rollout_stage_pct`, `coa_status`
- **Files:** Modify `internal/api/sim/handler.go` (`simResponse` struct + `toSIMResponse` helper)
- **Depends on:** Task 2
- **Complexity:** low
- **Pattern ref:** existing FIX-202 nullable-field projection at `internal/api/sim/handler.go:166-281` — `PolicyVersionID *string` field + `if s.PolicyVersionID != nil { resp.PolicyVersionID = … }` block.
- **Context refs:** "API Specifications > DTO additions", "Components Involved > C5"
- **What:** (a) Add fields `RolloutID *string \`json:"rollout_id,omitempty"\``, `RolloutStagePct *int \`json:"rollout_stage_pct,omitempty"\``, `CoaStatus string \`json:"coa_status,omitempty"\`` to the SIM response struct. (b) In `toSIMResponse`, project from new `SIMWithNames` fields. (c) Verify no conflict with FIX-202 fields. (d) Resolve open question OQ-1 (policy_id exposure) — add `PolicyID *string \`json:"policy_id,omitempty"\`` populated from `pol.id` (extend `simEnrichedSelect` to include `pol.id AS policy_id` if not already). This unblocks the FE Policy column linking to `/policies/<policy_id>` (PAT-016).
- **Verify:** integration test asserts DTO carries new fields when SIM is in a rollout cohort and omits them when not.

### Task 4 (W1): Persist `stage_pct` on rollout assignment writer
- **Files:** Modify `internal/store/policy.go` (`AssignSIMsToVersion`), Modify `internal/policy/rollout/service.go` (call site at `:307`)
- **Depends on:** Task 0
- **Complexity:** medium
- **Pattern ref:** existing `AssignSIMsToVersion` body at `internal/store/policy.go:1092-1148`. Note existing `value_strings[j] = fmt.Sprintf("($%d, $1, $2, NOW(), 'pending')", argIdx)` pattern — extend the value tuple with `stage_pct` arg.
- **Context refs:** "Components Involved > C9", "Bug Pattern Warnings > PAT-011, PAT-017"
- **What:** (a) Change signature to `AssignSIMsToVersion(ctx, simIDs, versionID, rolloutID uuid.UUID, stagePct int) (int, error)`. (b) Update INSERT VALUES tuple + ON CONFLICT clause to set `stage_pct = EXCLUDED.stage_pct`. (c) Update sole caller at `internal/policy/rollout/service.go:307` to pass `stage.Pct` (the existing `RolloutStage.Pct` int field, sourced from `rollout.Stages[stageIndex].Pct`). (d) `grep -rn 'AssignSIMsToVersion(' internal/ cmd/` — zero remaining old-signature call sites (PAT-011/PAT-017).
- **Verify:** start a new rollout via `POST /policy-versions/{id}/rollout`, advance stage 1 (`POST /policy-rollouts/{id}/advance` if needed), `SELECT stage_pct FROM policy_assignments WHERE rollout_id = X` returns `1` for the migrated rows.

### Task 5 (W1): `GET /policy-rollouts` list endpoint
- **Files:** Modify `internal/api/policy/handler.go` (new `ListRollouts` method), Modify `internal/store/policy.go` (new `ListRollouts` method), Modify `internal/gateway/router.go` (route registration `r.Get("/api/v1/policy-rollouts", deps.PolicyHandler.ListRollouts)`)
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** existing `GetRollout` handler in same file (`internal/api/policy/handler.go`); existing `ListSIMsParams` envelope in this plan; existing rollout route registrations at `internal/gateway/router.go:583-588`.
- **Context refs:** "API Specifications > GET /policy-rollouts (NEW)", "Components Involved > C6, C7"
- **What:** Backend list endpoint with `state` CSV filter (default `in_progress,paused`), `limit` (max 100). Store query joins `policy_versions pv ON pv.id = r.policy_version_id` and `policies pol ON pol.id = pv.policy_id` and filters `pol.tenant_id = $1`. Returns `RolloutSummary` DTO. Standard envelope response.
- **Verify:** curl `GET /api/v1/policy-rollouts` returns active rollouts; passes invalid `state=foo` → 400; tenant scoping verified by integration test.

### Task 6 (W2): SIM List FE — types + hook + URL ingestion
- **Files:** Modify `web/src/types/sim.ts` (`SIM` + `SIMListFilters` extensions), Modify `web/src/hooks/use-sims.ts` (`buildListParams`), Modify `web/src/pages/sims/index.tsx` (URL ingestion in `useMemo` filter + `setFilters` keys array)
- **Depends on:** Task 1, Task 3
- **Complexity:** medium
- **Pattern ref:** existing URL ingestion at `web/src/pages/sims/index.tsx:104-127`; existing `buildListParams` at `web/src/hooks/use-sims.ts:22-37`.
- **Context refs:** "API Specifications", "Screen Mockups", "Components Involved > C10, C13"
- **What:** (a) Extend `SIM` type with `policy_id?`, `rollout_id?`, `rollout_stage_pct?`, `coa_status?`. (b) Extend `SIMListFilters` with `policy_id?`, `rollout_id?`, `rollout_stage_pct?` (`policy_version_id` is already present). (c) `buildListParams` forwards all 4 (build int → string for `rollout_stage_pct`). (d) `pages/sims/index.tsx` `filters` `useMemo` reads all 4 from `searchParams.get(...)`; `setFilters` `keys` array updated to include all 4 so clear/set propagates.
- **Verify:** navigating to `/sims?rollout_id=<UUID>&rollout_stage_pct=1` renders only that cohort; clearing filter removes URL params.

### Task 7 (W2): Policy column + filter chips (Policy multi-select + Cohort dropdown)
- **Files:** Modify `web/src/pages/sims/index.tsx`, Modify `web/src/hooks/use-policies.ts` (add `useRolloutList`)
- **Depends on:** Task 5, Task 6
- **Complexity:** **high** (≈3 distinct UI pieces: column, Policy chip, Cohort chip with stage submenu, plus active-filter chip rendering plus URL/filter-state plumbing). Effort sits at top of medium / bottom of high; mark high to ensure opus.
- **Pattern ref:** existing filter chip implementations in same file at `:551-700` (State, RAT, Operator, APN dropdowns — reuse the inline `<button>` + dropdown menu pattern); existing `<Link>` usage in same file.
- **Context refs:** "Screen Mockups", "Design Token Map", "Components Involved > C11, C12", "Bug Pattern Warnings > PAT-015, PAT-016, PAT-018"
- **What:** (a) Add **Policy column** between APN and IP Pool: header cell `<TableHead>Policy</TableHead>` + body cell rendering `sim.policy_name` joined with `v{policy_version_number}` as `<Link to={\`/policies/\${sim.policy_id}\`}>` (PAT-016 — use `policy_id`, not `policy_version_id`). Empty state: `<span className="text-text-tertiary">—</span>`. Skeleton: add an extra skeleton cell in the loading branch (currently 12 cells; bumping to 13). Update `colSpan={12}` → `colSpan={13}` for the no-data row. (b) Add **Policy filter chip** — multi-select dropdown listing `activePolicies` (already loaded via `usePolicyList(undefined, 'active')` at line 143), with sub-menu "All versions / v1 / v2 / v3" populated from each policy's versions. Sets `policy_id` and/or `policy_version_id`. (c) Add **Cohort filter chip** — dropdown listing `useRolloutList('in_progress,paused')` results; selecting one sets `rollout_id`; reveals a stage sub-menu (1% / 10% / 100% / All migrated). Selecting a stage sets `rollout_stage_pct`; "All migrated" clears it. (d) Add active-filter chip rendering (extend `activeFilters` `useMemo` at `:199-227` to include policy + cohort) so `<RemovableChip />` shows them. (e) Subscribe to `wsClient.on('policy.rollout_progress', cb)` in a `useEffect` keyed on `filters.rollout_id`; on event with matching `rollout_id`, `queryClient.invalidateQueries({queryKey: ['sims']})`. (f) Token discipline: only Design Token Map classes (PAT-018 grep enforced).
- **Note:** invoke `frontend-design` skill mid-task to ensure professional polish on the new dropdowns; verify dark-mode rendering against existing chips for consistency.
- **Verify:** `grep -nE '\btext-(red\|blue\|green\|purple\|pink\|orange\|yellow\|amber\|cyan\|teal\|sky\|indigo\|violet\|fuchsia\|rose)-[0-9]{2,3}\b' web/src/pages/sims/index.tsx` returns ZERO new matches in changed regions; manual smoke: `/sims?rollout_id=…&rollout_stage_pct=1` shows only the canary SIMs and the chip "Cohort: <Policy v3> stage 1%".

### Task 8 (W3): Backend tests — handler validation + store JOIN + DTO
- **Files:** Modify `internal/api/sim/handler_test.go`, Modify (or create) `internal/store/sim_test.go` (one new test func), Modify `internal/store/policy_test.go` (assignment writer test for `stage_pct`)
- **Depends on:** Task 1, Task 2, Task 3, Task 4
- **Complexity:** medium
- **Pattern ref:** existing handler test patterns in `internal/api/sim/handler_test.go` (look for `TestHandler_ListSIMs_*`); existing store test patterns in `internal/store/sim_test.go`.
- **Context refs:** "Acceptance Criteria Mapping", "API Specifications", "Bug Pattern Warnings > PAT-006, PAT-009, PAT-011"
- **What:** Test cases covering: (1) invalid UUID for each new param → 400 INVALID_FORMAT (AC-4 + F-149); (2) `rollout_stage_pct=1` without `rollout_id` → 400 INVALID_PARAM; (3) `rollout_stage_pct=999` → 400 (out of range); (4) valid `rollout_id` filter returns only SIMs with matching `pa.rollout_id` (seed 2 SIMs into a rollout via store test); (5) valid `rollout_id` + `rollout_stage_pct=1` returns only those 2 SIMs (cohort isolation, story Test Plan); (6) DTO carries `rollout_id`, `rollout_stage_pct`, `coa_status` for migrated SIM and omits them for non-migrated (ensures `omitempty` works); (7) `AssignSIMsToVersion` persists `stage_pct` (PAT-011/PAT-017 round-trip). Tenant-scope test: SIM from tenant B not visible when filtering by rollout in tenant A.
- **Verify:** `go test ./internal/api/sim/... ./internal/store/...` PASS; `go test -race ./internal/store/...` PASS.

### Task 9 (W3): Performance EXPLAIN + p95 measurement
- **Files:** Create `docs/stories/fix-ui-review/FIX-233-perf.md` (one-page evidence sheet) — new file
- **Depends on:** Task 2, Task 8
- **Complexity:** low (but **mandatory** per advisor — replaces "advisory" framing in story)
- **Pattern ref:** previous perf evidence sheets exist for FIX-220 / FIX-215; if not found, create from scratch with EXPLAIN ANALYZE output + 50-call timing harness.
- **Context refs:** "Story-Specific Compliance Rules > Performance (AC-10)"
- **What:** Run `EXPLAIN (ANALYZE, BUFFERS) SELECT … FROM sims s …` for: (1) `LIMIT 50` no filter; (2) with `policy_version_id`; (3) with `rollout_id`; (4) with `rollout_id + rollout_stage_pct`. Verify planner uses `idx_policy_assignments_sim` (UNIQUE) for the JOIN. Run a Go bench or shell loop calling `GET /sims?rollout_id=X` 50× → assert p95 < 150ms. Capture EXPLAIN output + p95 in the evidence sheet.
- **Verify:** evidence sheet shows p95 < 150ms; planner uses index nested-loop (not seq scan) on `policy_assignments`.

### Task 10 (W3): FE smoke + WS refetch test
- **Files:** Modify `web/src/pages/sims/__tests__/index.test.tsx` (or create) — vitest + react-testing-library
- **Depends on:** Task 6, Task 7
- **Complexity:** medium
- **Pattern ref:** existing `web/src/components/policy/__tests__/rollout-active-panel.test.tsx` — vitest pattern with mocked `wsClient`.
- **Context refs:** "Acceptance Criteria Mapping > AC-7, AC-8, AC-9"
- **What:** (1) Mount with `?rollout_id=X` URL → assert filter chip rendered + API called with `rollout_id`; (2) WS event `policy.rollout_progress` with matching `rollout_id` → assert refetch fires (mock `useQueryClient.invalidateQueries`); (3) WS event with non-matching `rollout_id` → assert NO refetch; (4) Policy chip "Demo Premium > v3" click → URL gets `policy_version_id=…`.
- **Verify:** `cd web && pnpm test` passes new file; coverage includes WS refetch path.

---

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| **AC-1** `ListSIMsParams` adds 4 fields | Task 1, Task 4 | Task 8 (Test 1) |
| **AC-2** Handler accepts `?policy_version_id`, `?policy_id`, `?rollout_id`, `?rollout_stage_pct` | Task 1 | Task 8 (Tests 1, 4, 5) |
| **AC-3** Store query filters via `policy_assignments` JOIN + `s.policy_version_id` | Task 2 | Task 8 (Tests 4, 5) |
| **AC-4** Invalid UUID → 400 (fix F-149) | Task 1 | Task 8 (Test 1) |
| **AC-5** SIM DTO exposes 6 fields (3 from FIX-202 + 3 new) | Task 3 | Task 8 (Test 6) |
| **AC-6** Policy column with version + drill-down + nullable "—" | Task 7 | Task 10 (Test 4) + manual smoke |
| **AC-7** Policy multi-select + Cohort dropdown w/ stage submenu | Task 5, Task 7 | Task 10 (Test 4) + manual smoke |
| **AC-8** URL deep-linking on first paint | Task 6 | Task 10 (Test 1) |
| **AC-9** "View cohort" link → SIM list pre-filtered | Task 6 (URL ingest only — link already exists FIX-232) | Task 10 (Test 1) + manual smoke |
| **AC-10** p95 < 150ms with new JOIN | Task 2 (index design) | Task 9 (EXPLAIN + benchmark) |
| **F-149** Silent fallback fixed | Task 1 | Task 8 (Test 1) |
| **F-147** Policy column missing | Task 7 | manual smoke + Task 10 |

## Risks & Mitigations

- **Risk 1 — Policy column breaks table width on narrow screens (story Risk 1).** Mitigation: hide column at `<1280px` via Tailwind `hidden lg:table-cell` (matches IP Pool column responsiveness pattern in same file). Tooltip-on-hover shows policy name when hidden.
- **Risk 2 — JOIN+filter perf regression (story Risk 2 + AC-10).** Mitigation: new `idx_policy_assignments_rollout_stage` composite index; EXPLAIN-plan task is mandatory in Wave 3; planner expected to nested-loop via `idx_policy_assignments_sim` UNIQUE for the LEFT JOIN.
- **Risk 3 — Cohort filter dropdown over-queries `/policy-rollouts` (story Risk 3).** Mitigation: `useQuery` `staleTime: 30_000`; rollout list bounded ≤100 rows by `LIMIT`; no debounce needed — dropdown populates once on open.
- **Risk 4 — Stale data while filter active (story Risk 4).** Mitigation: WS subscription on `policy.rollout_progress` (canonical name; story doc had `policy.rollout.progressed` — flag as DEV-NNN). Refetch only when `filters.rollout_id` matches event payload.
- **Risk 5 — `stage_pct` writer regression (PAT-011/PAT-017).** Mitigation: enumerated grep step in Task 4 + integration test asserting round-trip persistence.
- **Risk 6 — F-148 dual-source resurrected (PAT-012).** Mitigation: explicit SQL comment in Task 2 stating `policy_version_id` filter uses `s.policy_version_id` (FIX-231 canonical); reviewer-grep checklist item.
- **Risk 7 — `policy_id` not exposed in DTO** — Task 3 adds it from `pol.id` (verified via `simEnrichedSelect`); without this AC-6 drill-down breaks (PAT-016).
- **Risk 8 — `make db-seed` breaks after migration (PAT-014).** Mitigation: `stage_pct` is nullable, no CHECK; seed should pass. Gate verifies on fresh volume.

## Test Plan

- **Unit (Go):** Task 8 tests — handler param parsers (each new param: invalid → 400, valid → propagated); combined-param validation; store filter predicates; assignment writer `stage_pct` round-trip; tenant-scope.
- **Integration (Go):** Task 8 — seed SIMs into a rollout, advance stage 1, request `?rollout_id=X&rollout_stage_pct=1` → exactly the staged SIMs (story Test Plan exact scenario).
- **Performance:** Task 9 — EXPLAIN ANALYZE all 4 filter combinations; 50× p95 benchmark < 150ms.
- **FE smoke (vitest):** Task 10 — URL deep-link, WS refetch, Policy chip click.
- **Manual (USERTEST scenarios — skeleton):** see below.

## Decisions Log Entries (proposed for Reviewer to log as DEV-NNN; Planner does not edit `decisions.md`)

- **DEV-A** (Reviewer to assign next DEV-NNN): `policy_assignments` adds nullable `stage_pct INT`; legacy rows stay NULL; cohort filter naturally excludes them (correct semantics). Composite index `(rollout_id, stage_pct)` — supports both rollout-only and rollout+stage filters.
- **DEV-B**: `policy_version_id` filter uses `s.policy_version_id` (FIX-231 canonical) NOT `policy_assignments.policy_version_id`. Explicit SQL comment to prevent F-148 dual-source recurrence (PAT-012).
- **DEV-C**: WS event canonical name is `policy.rollout_progress` (underscore). Story doc FIX-233 spec used `policy.rollout.progressed` — doc drift; corrected at Plan time.
- **DEV-D**: `rollout_stage_pct` without `rollout_id` is a 400 `INVALID_PARAM` (rejecting "stage 1% across all rollouts" as a nonsensical query — stages are scoped to rollouts).
- **DEV-E**: New `GET /policy-rollouts` endpoint accepts `state` CSV with default `in_progress,paused`; no cursor pagination (active set bounded ≤100).
- **DEV-F**: `policy_id` exposed on SIM DTO (extending FIX-202 envelope). Required for AC-6 drill-down to `/policies/<policy_id>` (PAT-016 — `policy_id` ≠ `policy_version_id`).
- **DEV-G**: Policy column hidden `<1280px` via `hidden lg:table-cell`; quick-peek tooltip preserves discoverability on narrow screens.

## Tech Debt Candidates (for Reviewer to log if needed)

- **TD-A:** WS refetch fires `invalidateQueries({queryKey: ['sims']})` — invalidates ALL SIM queries. For a multi-tab user, may cause unnecessary refetches on other SIM list views. Acceptable today (single-tab admin assumption); consider `[predicate: q => q.queryKey[0]==='sims' && q.queryKey[1]==='list']` if observed.
- **TD-B:** Cohort dropdown lists rollouts state-filtered but not policy-filtered — picking a rollout from a different policy than the active Policy chip is allowed; UX intent is to investigate any rollout, but a future polish could disable mismatched options.
- **TD-C:** `policy_id` resolution joins `policies pol` for tenant scoping — already in `simEnrichedJoin`, so free; if FIX-202 simplifies the JOIN later, ensure this story's `policy_id` exposure follows.
- **TD-D (recurrence guard):** D-139 (typed `dsl.SQLPredicate` wrapper) NOT addressed in this story — explicitly out of scope; defer to FIX-243 as logged.

## USERTEST Scenarios — Skeleton (final scenarios authored by Reviewer post-Gate)

> Turkish per CLAUDE.md global preference; Reviewer expands.

1. **Rollout Cohort Görünürlüğü:** Bir kullanıcı `/policies/<id>/rollouts` ekranında aktif bir rollout için "View Migrated SIMs" butonuna tıklar → SIM listesi sadece o cohort'taki SIM'leri listeler; üstte "Cohort: <Policy> v<N>" chip'i görünür.
2. **Stage 1% İzolasyonu:** Aynı sayfada Cohort dropdown'ı açılır, "Stage 1% (2 SIM)" seçilir → liste 2 SIM'e iner; URL'e `rollout_stage_pct=1` eklenir.
3. **Deep-Link İlk Yükleme:** `/sims?rollout_id=<UUID>&rollout_stage_pct=1` URL'i bookmark'tan açılır → sayfa ilk render'da filtre uygulanmış olarak gelir.
4. **Geçersiz UUID Davranışı:** `/sims?policy_version_id=not-a-uuid` çağrısı → 400 INVALID_FORMAT ile düşer; FE error toast gösterir, sessizce 50 SIM listelemez (F-149 fix).
5. **Policy Sütunu Drill-Down:** SIM listesinde Policy sütunundaki "Demo Premium v3" linkine tıklanır → `/policies/<policy_id>` policy detay ekranı açılır (NOT version detay).
6. **WS Canlı Güncelleme:** Cohort filtresi aktifken arka planda rollout ilerletilir (advance stage) → liste otomatik refetch'le güncel SIM setini gösterir; chip korunur.
7. **Çoklu Policy Filtresi:** Policy multi-select chip'inden 3 policy seçilir → liste union'ını gösterir; chip'lerin her biri ayrı ✕ ile çıkarılabilir.
8. **Performans (Browser DevTools):** Cohort filtresi açık 50 satır listeyi 5x yenileyince Network panel'de p95 < 150ms görünür (AC-10 sanity).

---

## Self-Validation (Quality Gate)

Run before declaring plan complete.

**a. Min substance (M effort):** plan ≥ 60 lines (✓ this plan ≈ 350+ lines), tasks ≥ 3 (✓ 11 tasks: T0..T10).

**b. Required sections:** Goal ✓, Architecture Context ✓, Tasks ✓, Acceptance Criteria Mapping ✓.

**c. Embedded specs:** API endpoints with request/response/status codes ✓; DB schema embedded with source comment ✓; UI Design Token Map populated ✓.

**d. Task complexity (M effort, mix low+medium expected; one high acceptable):** T0 low, T1 medium, T2 **high** (hot path + perf), T3 low, T4 medium, T5 medium, T6 medium, T7 **high** (multi-piece UI), T8 medium, T9 low, T10 medium. Two highs is appropriate for the most complex pieces (SQL extension + multi-chip UI).

**e. Context refs:** every task references existing sections in this plan ✓.

**Architecture Compliance:** layers respected (store→handler→FE) ✓; no cross-layer imports ✓; multi-tenant scope enforced via existing `s.tenant_id = $1` predicate + new `policy_id` filter joins through `policy_versions` with explicit `tenant_id` ✓.

**API Compliance:** standard envelope ✓; UUID/int validation → 400 ✓; combined-param validation specified ✓; error codes catalogued ✓.

**DB Compliance:** up + down migration ✓; index specified ✓; column source noted "ACTUAL: 20260320000002" ✓; nullable semantics documented ✓; PAT-014 seed-check called out ✓.

**UI Compliance:** screen mockup embedded ✓; Design Token Map populated with NEVER-USE column ✓; Component Reuse table populated ✓; PAT-018 grep mandated ✓; loading/empty/error states acknowledged (existing skeleton + EmptyState patterns extended) ✓; `frontend-design` skill invocation noted in Task 7 ✓.

**Test Compliance:** AC-1..AC-10 + F-149 each mapped to a Task with verification ✓; performance task is mandatory ✓.

**Self-containment:** API specs embedded ✓; DB schema embedded ✓; mockup embedded ✓; business rules inline ✓; Bug Pattern warnings inline (PAT-006/9/11/12/14/15/16/17/18) ✓.

**Open Questions auto-resolved (AUTOPILOT) — flag for Dev sanity-check:**

- **OQ-1 (RESOLVED):** Story spec did not explicitly include `policy_id` on the SIM DTO (only `policy_version_id`). Resolved in Task 3 — add `policy_id` to enable Policy column drill-down to the canonical policy detail page (PAT-016 prevents `policy_version_id`-as-`policy_id` confusion). Planner judgement, not a story requirement; flagged for Reviewer.
- **OQ-2 (RESOLVED):** Story spec said `policy.rollout.progressed`; canonical name is `policy.rollout_progress`. Plan uses canonical; flagged in DEV-C above.
- **OQ-3 (RESOLVED):** Story spec did not explicitly require a new `GET /policy-rollouts` list endpoint, but AC-7's "dropdown of active rollouts" requires it. Endpoint added in Task 5; bounded list (no pagination).
- **OQ-4 (RESOLVED):** Story spec assumed `policy_assignments` already had `stage_pct`; verified IT DOES NOT. Migration in Task 0 + writer change in Task 4 added.
- **OQ-5 (RESOLVED):** `coa_status` exposure in DTO without UI consumer in this story — kept in DTO per AC-5 wording (`coa_status?` listed); future FIX-234 (CoA enum extension) will surface it. Optional `omitempty` ensures no payload bloat.

**Quality Gate result: PASS.** No re-iteration needed.

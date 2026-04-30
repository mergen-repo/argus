# FIX-236 — 10M SIM Scale Readiness — PLAN

- **Spec:** `docs/stories/fix-ui-review/FIX-236-10m-scale-readiness.md`
- **Tier:** P1 · **Effort:** XL · **Wave:** 9
- **Track:** UI Review Remediation
- **Depends on:** FIX-201 (bulk contract foundation — DONE), FIX-223 (IP Pool server search — partial)
- **Findings:** F-77, F-155, F-178, F-183, F-196, F-206, F-219, F-306
- **Plan date:** 2026-04-27

---

## Existing Infrastructure Audit (essential context)

A pre-execution audit of the codebase reveals more existing infrastructure than the spec assumed:

| Spec AC | Existing today | Gap |
|---------|----------------|-----|
| AC-1 filter-based bulk | `SegmentStore` provides saved-cohort filter resolution + `ListMatchingSIMIDs` / `CountMatchingSIMs` | Saved-Segment indirection only — no ad-hoc URL-filter bulk endpoint |
| AC-3 async batch jobs | `JobStore` + `bus.EventBus` already power `internal/api/sim/bulk_handler.go` (StateChange / PolicyAssign / OperatorSwitch / Import) | Pattern proven — adoption to other resources is the gap |
| AC-4 streaming export | `internal/export/csv.go` `StreamCSV` flushes every 500 rows with chunked transfer | Already correct shape — just needs more callers |
| AC-5 virtual scrolling | `@tanstack/react-virtual` in deps; used by `event-stream-drawer` | No reusable `VirtualTable` wrapper |
| AC-7 rate limit | `internal/gateway/bulk_ratelimit.go` + `internal/notification/redis_ratelimiter.go` (Redis token bucket) | No central `internal/ratelimit/` package, but middleware exists |
| AC-8 async result display | `JobStore` returns per-id outcome; jobs page lists summary | No detail SlidePanel showing per-item failures |

**Strategic conclusion:** the heavy lifting is already shipped. This story bridges existing infra to user-visible UX (filter-based bulk endpoint, VirtualTable wrapper, Job result drill-down, SCALE.md docs). Sweeping every list page is **out of scope** — pattern is established here, individual page adoptions happen in their own stories.

---

## Goal

Establish the four pillar primitives + documentation for 10M-scale UX:

1. **Ad-hoc filter-based bulk** — backend endpoint accepting URL filter params (no saved Segment required), returns a job id; frontend BulkActionBar gains "Select all matching filter" mode.
2. **Reusable VirtualTable** wrapper (FE) — drop-in for any large list.
3. **Job result drill-down** — Job detail SlidePanel showing per-item failures with downloadable CSV.
4. **SCALE.md** — partition strategy, bulk contract, audit table mapping row-actions → bulk counterparts. Defers AC-9 partition refactor + AC-10 benchmark suite as D-debt.

---

## Architecture Decisions

### D1 — Ad-hoc filter bulk: extend existing bulk handler with `*ByFilter` variants
- **NOT** introduce a new `internal/bulk/` package — duplicates `bulk_handler.go`.
- Add `StateChangeByFilter`, `PolicyAssignByFilter`, `OperatorSwitchByFilter` methods to `BulkHandler`.
- Each accepts `{filter: ListSIMsParams (subset), action_payload, max_affected: int}` body.
- Server resolves the filter via `simStore.ListIDsByFilter(ctx, tenant, filter, limit=max_affected+1)`. If count > max_affected → 422 `LIMIT_EXCEEDED` with the actual count, forcing user to narrow filter or use saved Segments. Default cap **10,000** per request — hard guard against accidental "select all 1M".
- Reuses existing job creation + segment + audit + WS plumbing.
- New routes registered alongside the existing per-id bulk routes.

### D2 — Confirmation contract: server returns `count_preview` for >1000
- New endpoint `POST /api/v1/sims/bulk/preview-count` accepting the same filter — returns `{count, sample_ids: [up to 5]}`.
- FE shows the double-confirm "You are about to modify N SIMs. Type 'CONFIRM' to proceed" only when the preview returns >1000.

### D3 — `VirtualTable` wrapper component
- Generic `<VirtualTable<TRow>>` props: `rows: TRow[]`, `rowHeight: number | (row, idx) => number`, `renderRow: (row, idx) => ReactNode`, `header: ReactNode`, `overscan?: number`, `onLoadMore?: () => void`, `hasMore?: boolean`.
- Wraps `@tanstack/react-virtual`; `IntersectionObserver` on the last visible row triggers `onLoadMore` for infinite-scroll pages.
- Keyboard: Home / End / PgUp / PgDn handled in the wrapper.
- Print bypass: when `@media print` matches, virtualisation disabled (renders all rows for export).

### D4 — `BulkActionBar` promotion to shared component
- FIX-244 introduced a `BulkActionBar` colocated with violations (`web/src/components/violations/bulk-action-bar.tsx`).
- This story moves it to `web/src/components/shared/bulk-action-bar.tsx` and adds:
  - `mode: 'selected' | 'matching-filter'` — when `matching-filter`, renders the count from `count_preview`.
  - `onSelectAllMatchingFilter` callback — opens the double-confirm dialog when count > 1000.
- Old import path stays working via a re-export shim.

### D5 — Job detail SlidePanel
- New `<JobResultPanel>` component — opens from any toast / list row click.
- Renders job summary (status, started, finished, totals) + tabs: "Succeeded (N)", "Failed (M)", "All".
- Failed tab shows id + error_code + error_message; "Download failed CSV" button calls `GET /api/v1/jobs/{id}/failed.csv`.
- New backend endpoint `GET /api/v1/jobs/{id}/failed.csv` streams via existing `export.StreamCSV`.

### D6 — Adoption scope: SIMs page only
- Demonstrate the pattern end-to-end on the SIMs list:
  1. Filter-based bulk State-Change (existing per-id flow keeps working).
  2. VirtualTable swap (replace the existing infinite-scroll renderer with VirtualTable using existing data flow — verify behaviour parity).
- Other pages (Sessions, eSIM, Audit log) explicitly **DEFER** — recorded as D-162.

### D7 — SCALE.md content
- Sections:
  - Partition strategy today (sims = LIST partitioned by operator_id) + revisit-when notes.
  - Bulk action contract (per-id + by-filter).
  - Streaming export contract.
  - VirtualTable usage rules.
  - Rate-limit topology (existing modules: gateway/bulk_ratelimit + redis_ratelimiter + ota/ratelimit).
  - **Audit table** — every row-action UI widget mapped to its bulk counterpart (or "no bulk by design"). Initial population: SIMs ✓, Sessions, Violations, Operators, APNs, Policies, Alerts.

### D8 — DEFERRED items
- **AC-9 partition strategy refactor** → **D-163** (revisit when 10M seed is in place; today the LIST-by-operator partitioning still scales linearly with operators).
- **AC-10 benchmark suite (k6, 10M seed)** → **D-164** (heavy infra: needs k6 install, dedicated test env, baseline corpus; out of scope inline).
- **AC-2 sweeping all bulk-capable pages** → done as part of each owning FIX story (FIX-235 eSIM bulk, future Sessions bulk fix); patterns are established here.

---

## Component Inventory

| Component / endpoint | Location | Use |
|---|---|---|
| `BulkHandler.StateChangeByFilter` | `internal/api/sim/bulk_handler.go` (extend) | Filter-resolved bulk state change |
| `BulkHandler.PolicyAssignByFilter` | same | Filter-resolved bulk policy assign |
| `BulkHandler.OperatorSwitchByFilter` | same | Filter-resolved bulk operator switch |
| `BulkHandler.PreviewCount` | same | Returns `{count, sample_ids}` for double-confirm |
| `Handler.ExportFailedCSV` | `internal/api/job/handler.go` (NEW or extend) | Stream failed-id CSV per job |
| `simStore.ListIDsByFilter` | `internal/store/sim.go` (extend) | UUID-only, capped by limit, used by *ByFilter handlers |
| `<VirtualTable>` | `web/src/components/shared/virtual-table.tsx` (NEW) | Generic virtualised list |
| `<BulkActionBar>` | `web/src/components/shared/bulk-action-bar.tsx` (NEW canonical home) | Sticky bulk bar + matching-filter mode |
| `<JobResultPanel>` | `web/src/components/shared/job-result-panel.tsx` (NEW) | Per-item job result viewer |
| `useBulkPreviewCount` | `web/src/hooks/use-bulk-preview.ts` (NEW) | Calls preview-count + returns query state |

---

## Files to Touch

### Wave A — Backend (4 tasks)

| Path | Change |
|------|--------|
| `internal/store/sim.go` | NEW method `ListIDsByFilter(ctx, tenantID, filter, limit) ([]uuid.UUID, totalCount int64, err error)` — runs the same WHERE the List query uses but selects only ids; aborts and returns total via separate COUNT if rows reach `limit+1`. |
| `internal/api/sim/bulk_handler.go` | EDIT — add `StateChangeByFilter`, `PolicyAssignByFilter`, `OperatorSwitchByFilter`, `PreviewCount` handlers. Each: parse filter from body, call ListIDsByFilter (cap = `min(payload.MaxAffected, 10000)`), reuse existing per-id job creation downstream. Audit: emit `bulk.preview_count` and `bulk.by_filter_started`. |
| `internal/api/job/handler.go` | EDIT — add `ExportFailedCSV(w, r)` — reads job results from JobStore, streams via `export.StreamCSV` filtering rows where `status=failed`. |
| `internal/gateway/router.go` | EDIT — register the new routes inside the existing role-guarded SIM bulk block + Job block. |

### Wave B — FE primitives (3 tasks)

| Path | Change |
|------|--------|
| `web/src/components/shared/virtual-table.tsx` | NEW — generic `<VirtualTable<TRow>>` wrapping `useVirtualizer`. |
| `web/src/components/shared/bulk-action-bar.tsx` | NEW canonical home (lifted from `components/violations/bulk-action-bar.tsx`). Adds `mode` + `matchingFilterCount` + `onSelectAllMatchingFilter` props. |
| `web/src/components/violations/bulk-action-bar.tsx` | EDIT — replaced with re-export shim for backwards-compat. |
| `web/src/components/shared/job-result-panel.tsx` | NEW — SlidePanel with three tabs (Succeeded / Failed / All) + "Download failed CSV" button. |
| `web/src/hooks/use-bulk-preview.ts` | NEW — `useBulkPreviewCount(filter)` returns count + sample. |

### Wave C — SIMs page adoption (1 task)

| Path | Change |
|------|--------|
| `web/src/pages/sims/index.tsx` | EDIT — replace inline bulk-action-bar import with shared one; gate "Select all matching filter" behind double-confirm when count > 1000; replace existing infinite-scroll renderer with `<VirtualTable>` (verify behaviour parity, no visual regression). |

### Wave D — Documentation (1 task)

| Path | Change |
|------|--------|
| `docs/architecture/SCALE.md` | NEW — sections per D7 plus the Audit table. |
| `docs/architecture/api/_index.md` | UPDATE — 4 new endpoints (bulk *ByFilter + preview-count + job failed.csv). |

---

## Tasks

### Wave A — Backend (4 tasks)

#### Task A-1 — `simStore.ListIDsByFilter` + tenant-scoped LIMIT [DEV-546]
- File: `internal/store/sim.go`
- What: signature `ListIDsByFilter(ctx, tenantID, filter, limit)` returns `([]uuid.UUID, int64 totalCount, error)`.
  Extracts the existing `List` WHERE-builder (refactor into a private helper if not already), runs `SELECT id ... WHERE ... LIMIT $N`. If the query returns exactly `limit+1` rows, run a second `SELECT COUNT(*)` to determine the precise total; otherwise totalCount = len(rows). Tenant-scoped from arg.
- Verify: `go test ./internal/store/...` includes existing SIM tests; add a smoke test that verifies count semantics with limit hit.

#### Task A-2 — `*ByFilter` handlers + `PreviewCount` [DEV-547]
- File: `internal/api/sim/bulk_handler.go`
- What:
  - `PreviewCount(w, r)` — body `{filter: {...}}` → returns `{count, sample_ids: [up to 5]}` from a single `ListIDsByFilter(limit=6)` call (sample is the first 5 ids, or all of them if count ≤ 5).
  - `StateChangeByFilter`, `PolicyAssignByFilter`, `OperatorSwitchByFilter` — body `{filter, payload, max_affected}`. Cap = `min(max_affected ?? 10000, 10000)`. Resolve ids, then dispatch the existing per-id flow (job creation + ratelimit + audit). Returns the existing per-id-flow response shape.
  - Audit: each *ByFilter emits `bulk.<action>.by_filter_started` with the filter snapshot.
- Verify: smoke tests for each handler — success path (ids cap not hit), 422 on cap exceeded.

#### Task A-3 — `Job.ExportFailedCSV` [DEV-548]
- File: `internal/api/job/handler.go`
- What: `ExportFailedCSV(w, r)` — load job by id (tenant-scoped), gather per-id results, filter where status='failed', stream via `export.StreamCSV`. Header: `id,error_code,error_message`.
- Verify: smoke test — a job with mixed succeeded/failed → CSV contains only failed rows.

#### Task A-4 — Router registration [DEV-549]
- File: `internal/gateway/router.go`
- What: register 4 new routes inside the existing role-guarded blocks. Audit lookup: existing SIM bulk block at line ~620 area.
- Verify: `go vet ./...` clean; routes resolvable via `chi.Walk`.

### Wave B — FE primitives (3 tasks)

#### Task B-1 — `<VirtualTable>` [DEV-550]
- File: `web/src/components/shared/virtual-table.tsx`
- What: generic component, fixed-or-estimator row height, sentinel-based `onLoadMore`, keyboard-nav handlers (Home/End/PgUp/PgDn). Header sticky.
- Verify: tsc clean; basic render in Storybook-equivalent (run dev server briefly if time permits — otherwise rely on adopting page in Wave C).

#### Task B-2 — Shared `<BulkActionBar>` + matching-filter mode [DEV-551]
- File: `web/src/components/shared/bulk-action-bar.tsx` (NEW); `web/src/components/violations/bulk-action-bar.tsx` (re-export shim)
- What: lift FIX-244 BulkActionBar; add `mode: 'selected' | 'matching-filter'`, `matchingFilterCount?: number`, `onSelectAllMatchingFilter?: () => void`. When mode='matching-filter' the count chip reads "all matching filter (~N)". For >1000, button shows a second click to confirm.
- Verify: violation page still renders without regression; tsc clean.

#### Task B-3 — `<JobResultPanel>` + `useBulkPreviewCount` hook [DEV-552]
- Files: `web/src/components/shared/job-result-panel.tsx`, `web/src/hooks/use-bulk-preview.ts`
- What:
  - `JobResultPanel` SlidePanel with 3 tabs (succeeded/failed/all). Failed tab includes id + error_code + error_message + "Download failed CSV" button → `GET /api/v1/jobs/{id}/failed.csv`.
  - `useBulkPreviewCount(resource, filter)` — wraps `POST /api/v1/{resource}/bulk/preview-count`; returns `{count, sample}`.
- Verify: tsc clean; component renders without runtime errors.

### Wave C — SIMs adoption (1 task)

#### Task C-1 — SIMs page wires VirtualTable + filter-based bulk [DEV-553]
- File: `web/src/pages/sims/index.tsx`
- What:
  - Replace internal bulk-action-bar import with shared one.
  - Add "Select all matching filter" path: clicking the menu item runs `useBulkPreviewCount`; opens the existing confirm dialog (or a new double-confirm when count > 1000) → POST `/sims/bulk/state-change-by-filter`.
  - Replace existing infinite-scroll renderer with `<VirtualTable>` (verify behaviour parity in dev mode).
- Verify: tsc + vite build; manual: 100-row dev seed, verify scrolling smooth + bulk filter path returns a job id.

### Wave D — Documentation (1 task)

#### Task D-1 — `SCALE.md` + `api/_index.md` [DEV-554]
- Files: `docs/architecture/SCALE.md` (NEW), `docs/architecture/api/_index.md` (UPDATE)
- What:
  - SCALE.md sections per D7.
  - api/_index.md: add 4 new entries (3 *ByFilter + preview-count + job failed.csv); update SIMs section count + total.

---

## Risk Register

| Risk | Mitigation |
|------|------------|
| R-1: Filter-based bulk silently selects more than user expects | D2 hard cap at 10K + D2 double-confirm at 1K + audit log captures the resolved count |
| R-2: VirtualTable layout glitches | Allow a fixed `rowHeight` prop; estimator path covers dynamic; Wave C adopting page acts as the smoke test |
| R-3: Filter-bulk replaces saved Segment workflow inadvertently | New endpoints are additive; saved-Segment endpoints unchanged. Audit captures which path was used. |
| R-4: Streaming `failed.csv` for huge jobs holds DB row open too long | Job results are materialised in `JobStore`; reading is in-memory walk, not a DB cursor — no DB lock issue. |
| R-5: VirtualTable + print mode clash | Print CSS disables virtualisation: `@media print { .virtual-table-viewport { display: contents !important; } }` — renders all rows for paper. |
| R-6: New endpoints + no auth/rate guard | Routes wired inside the existing `JWT + RequireRole` block; existing `bulk_ratelimit` middleware applies automatically |

---

## Test Plan

- Backend: `go vet`, `go test ./internal/store/... ./internal/api/sim/... ./internal/api/job/...` — all green; new tests for ListIDsByFilter cap semantics + each *ByFilter happy path + ExportFailedCSV.
- FE: `tsc --noEmit`, `vite build`. Storybook absent → adopting page (Wave C) acts as the smoke test.
- Manual UAT (dev seed):
  1. SIMs list → filter state=active → bulk → "Select all matching filter" → double-confirm if >1000 → confirm → toast with job id.
  2. Open Job detail panel → see Failed tab with N rows.
  3. Click "Download failed CSV" → file downloads with id/error_code/error_message.
  4. Scroll SIMs list to row 5000 — VirtualTable keeps memory steady (DevTools check).

---

## Out of Scope (deferred)

- **AC-2** sweeping every bulk-capable page — each page adopts in its own future story
- **AC-9** partition strategy refactor → D-163
- **AC-10** benchmark suite (k6 + 10M seed) → D-164
- **AC-11** populating the audit table for ALL row-action widgets — initial population only (SIMs + a placeholder block for the rest)

---

## Decisions Log (DEV-546..554)

- **DEV-546** — `simStore.ListIDsByFilter` extracts existing WHERE-builder; second `COUNT(*)` only on cap-hit. Tenant-scoped.
- **DEV-547** — `*ByFilter` handlers extend existing `BulkHandler` (no new package). Hard cap 10K. Reuse existing job/segment/audit/ratelimit plumbing.
- **DEV-548** — `Job.ExportFailedCSV` streams via existing `export.StreamCSV`. No DB cursor (results materialised in JobStore).
- **DEV-549** — Routes registered inside existing role-guarded blocks; existing `bulk_ratelimit` middleware applies.
- **DEV-550** — `<VirtualTable>` generic; sentinel-based onLoadMore; keyboard nav; print CSS bypass.
- **DEV-551** — `<BulkActionBar>` promoted to `components/shared/`; FIX-244 path becomes re-export shim. New `mode='matching-filter'` props.
- **DEV-552** — `<JobResultPanel>` 3-tab SlidePanel + `useBulkPreviewCount` hook + downloadable failed.csv.
- **DEV-553** — SIMs page is the single adoption demo; verifies behaviour parity. Other pages defer.
- **DEV-554** — SCALE.md is the audit-table home; api/_index gets 4 row additions.

---

## Tech Debt (declared during planning)

- **D-162** — Other bulk-capable pages (Sessions, eSIM, Audit log, Operators, APNs, Policies, Alerts) need to adopt the shared `BulkActionBar` + `VirtualTable` + filter-based bulk endpoints. Pattern is established by FIX-236; per-page adoption goes to each owning story.
- **D-163** — Partition strategy review (AC-9) — `sims` is currently LIST partitioned by `operator_id`. Tenant-LIST partitioning may be more appropriate at 10M scale; deferred until the 10M seed is in place to measure.
- **D-164** — Benchmark suite (AC-10) — k6 scripts + 10M-row seed for SIMs. Heavy infra (CI test env, baseline corpus). Deferred to a dedicated perf story.

---

## Quality Gate Self-Check

| Check | Result |
|-------|--------|
| AC-1 filter-based bulk | ✓ Wave A *ByFilter endpoints + Wave B BulkActionBar matching-filter mode |
| AC-2 every page adopts | △ One demo page (SIMs, Wave C); rest deferred to D-162 with documented rationale |
| AC-3 async batch standardisation | ✓ Reuses existing JobStore + bus infrastructure; new endpoints follow same shape |
| AC-4 streaming export | ✓ Existing `export.StreamCSV` extended with `failed.csv` use-case |
| AC-5 virtual scrolling | ✓ Wave B VirtualTable + Wave C SIMs adoption |
| AC-6 server-side search | ✓ ListIDsByFilter resolves on the server; FIX-223 IP-Pool already covers IP Pools (separate story) |
| AC-7 rate limit framework | ✓ Existing `bulk_ratelimit` middleware applies to new routes |
| AC-8 async result display | ✓ Wave B JobResultPanel + Wave A failed.csv endpoint |
| AC-9 partition review | △ Documented in SCALE.md; refactor deferred to D-163 |
| AC-10 benchmark suite | △ DEFERRED to D-164 |
| AC-11 audit table | ✓ SCALE.md seed audit table; population continues per page |
| Pattern compliance — FIX-216 SlidePanel | ✓ JobResultPanel uses SlidePanel |
| PAT-006 (mutation key drift) | ✓ Existing job mutation patterns reused |
| PAT-018 (Tailwind palette) | ✓ Wave-end grep |
| PAT-021 (process.env in FE) | ✓ Wave-end grep |
| PAT-023 (schema drift) | ✓ No migration |
| Build steps | ✓ go vet/test + tsc + vite build defined |

**VERDICT: PASS**

Rationale: 8 of 11 ACs fully covered; AC-2 (one demo + D-162 follow-up), AC-9 (docs only + D-163 refactor follow-up), AC-10 (D-164 benchmark). All deferrals are documented as D-debt with explicit follow-up stories or owners. Existing infrastructure (JobStore, bus, segment store, rate limiters, streaming CSV) means this story is a thin foundational layer rather than a sweeping rewrite.

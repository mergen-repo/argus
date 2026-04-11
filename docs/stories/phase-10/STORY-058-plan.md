# STORY-058 Implementation Plan — Frontend Consolidation & UX Completeness

## Architecture Context

### Packages (with absolute paths)
- **Shared UI:** `web/src/components/ui/skeleton.tsx`, `web/src/components/ui/button.tsx`, `web/src/components/ui/badge.tsx`, `web/src/components/ui/dialog.tsx`, `web/src/components/ui/select.tsx`, `web/src/components/ui/slide-panel.tsx`
- **Shared Lib:** `web/src/lib/format.ts`, `web/src/lib/sim-utils.ts`, `web/src/lib/constants.ts`, `web/src/lib/ws.ts`, `web/src/lib/utils.ts`, `web/src/lib/api.ts`
- **Layout:** `web/src/components/layout/dashboard-layout.tsx`, `web/src/components/layout/sidebar.tsx`, `web/src/components/layout/topbar.tsx`
- **Error Boundary:** `web/src/components/error-boundary.tsx`
- **Router:** `web/src/router.tsx`
- **Command Palette:** `web/src/components/command-palette/command-palette.tsx`
- **Hooks:** `web/src/hooks/use-sessions.ts`, `web/src/hooks/use-sims.ts`, `web/src/hooks/use-esim.ts`, `web/src/hooks/use-audit.ts`, `web/src/hooks/use-jobs.ts`, `web/src/hooks/use-policies.ts`, `web/src/hooks/use-operators.ts`
- **Types:** `web/src/types/job.ts`, `web/src/types/sim.ts`, `web/src/types/esim.ts`, `web/src/types/audit.ts`, `web/src/types/session.ts`
- **Vite Config:** `web/vite.config.ts`

### Key Pages
| Page | Path | Screens |
|------|------|---------|
| SIM List | `web/src/pages/sims/index.tsx` | SCR-045 |
| SIM Detail | `web/src/pages/sims/detail.tsx` | SCR-075 |
| Sessions | `web/src/pages/sessions/index.tsx` | SCR-070 |
| Jobs | `web/src/pages/jobs/index.tsx` | SCR-071 |
| eSIM | `web/src/pages/esim/index.tsx` | SCR-072 |
| Audit | `web/src/pages/audit/index.tsx` | SCR-080 |
| APN Detail | `web/src/pages/apns/detail.tsx` | SCR-060 |
| Operator Detail | `web/src/pages/operators/detail.tsx` | — |
| Dashboard | `web/src/pages/dashboard/index.tsx` | SCR-010 |
| Policy Editor | `web/src/pages/policies/editor.tsx` | SCR-100 |

### Pre-Existing Consolidations (Verify-Only)
- `Skeleton` already extracted to `web/src/components/ui/skeleton.tsx`, all pages import from there (no local definitions in `web/src/pages/`)
- `RAT_DISPLAY` already consolidated to `web/src/lib/constants.ts` (single definition)
- `stateVariant`/`stateLabel` for SIM state already in `web/src/lib/sim-utils.ts`
- `formatBytes`/`formatDuration`/`timeAgo`/`formatNumber` already in `web/src/lib/format.ts`
- Command palette already has `/settings/notifications` entry with `BellRing` icon (line 50)
- `STATE_COLORS` already includes `stolen_lost: 'var(--color-purple)'` (dashboard/index.tsx line 34)

---

## Design Token Map

| Element | Token | Value | Usage |
|---------|-------|-------|-------|
| RATBadge bg | `--bg-hover` | `#1A1A28` | Badge background |
| RATBadge text | `--text-tertiary` | `#4A4A65` | Badge label |
| RATBadge font | `--font-mono` | JetBrains Mono | Monospace data |
| RATBadge size | `10px` | — | Compact badge text |
| RATBadge radius | `--radius-sm` | `6px` | Pill badge |
| InfoRow label | `--text-secondary` | `#7A7A95` | Left label |
| InfoRow value | `--text-primary` | `#E4E4ED` | Right value |
| InfoRow mono | `--font-mono` at `12px` | — | Technical values (ICCID, IMSI) |
| ErrorBoundary bg | `--danger-dim` | `rgba(255,68,102,0.12)` | Error panel |
| ErrorBoundary border | `--danger/30` | — | Error panel border |
| ErrorBoundary icon | `--danger` | `#FF4466` | Alert icon |
| ErrorBoundary actions | Button `outline` variant | — | Try Again / Go Home |
| Bulk dialog bg | `--bg-elevated` | `#12121C` | Dialog background |
| Bulk dialog border | `--border` | `#1E1E30` | Dialog borders |
| Filter dropdown active | `--accent-dim` | `rgba(0,212,255,0.15)` | Active filter bg |
| Filter dropdown border | `--accent/30` | — | Active filter border |
| Filter dropdown text | `--accent` | `#00D4FF` | Active filter label |
| Success glow | `--success` | `#00FF88` | Live indicator dot |
| Transition | `--transition` | `0.2s cubic-bezier(0.4,0,0.2,1)` | All state transitions |

---

## Bug Patterns (from decisions.md)

| ID | Finding | Resolution |
|----|---------|------------|
| DEV-136.17 | 7 Phase 8 frontend stories without ErrorBoundary | AC-4: Router-level EB in DashboardLayout + per-tab wrappers on SIM Detail |
| DEV-136.18 | Skeleton/RAT_DISPLAY/formatBytes/InfoRow duplication (11/7/3/3 files) | AC-1/2/3: Already consolidated; verify + create RATBadge + extract InfoRow |
| DEV-136.19 | 425KB gzipped single-chunk bundle | AC-5: React.lazy for remaining eager imports, vite chunkSizeWarningLimit |
| E2E Report | ErrorBoundary doesn't reset on navigation | AC-4: Already fixed via `key={location.pathname}` in DashboardLayout |

---

## Tasks

### Task 1: Create RATBadge Component + Extract InfoRow Component
**Complexity:** Low
**Depends on:** None
**Files:**
- NEW: `web/src/components/ui/rat-badge.tsx`
- NEW: `web/src/components/ui/info-row.tsx`
- EDIT: `web/src/pages/sessions/index.tsx` (replace inline RAT rendering)
- EDIT: `web/src/pages/sims/detail.tsx` (remove local InfoRow, import shared; replace inline RAT)
- EDIT: `web/src/pages/apns/detail.tsx` (remove local InfoRow, import shared)
- EDIT: `web/src/pages/operators/detail.tsx` (remove local InfoRow, import shared)
**Pattern ref:** Follows existing ui component pattern (`web/src/components/ui/skeleton.tsx`)
**Context refs:**
- `web/src/lib/constants.ts` — `RAT_DISPLAY` map (source of truth for RAT labels)
- `web/src/pages/sessions/index.tsx:147-150` — inline RAT badge rendering to extract
- `web/src/pages/sims/detail.tsx:210-218` — canonical InfoRow definition
- `web/src/pages/sims/detail.tsx:118` — `RAT_DISPLAY[sim.rat_type]` usage
- Design tokens: `--bg-hover`, `--text-tertiary`, `--font-mono`, `10px` size, `--radius-sm`

**What:**
1. Create `rat-badge.tsx`: Accept `ratType: string` prop, render display label from `RAT_DISPLAY`, styled as compact mono badge (`font-mono text-[10px] px-1.5 py-0.5 rounded bg-bg-hover text-text-tertiary font-medium`). Handle undefined/null gracefully with dash fallback.
2. Create `info-row.tsx`: Accept `{ label: string; value: string; mono?: boolean }` props. Render horizontal flex with label left (text-xs text-text-secondary) and value right (text-sm text-text-primary, font-mono text-xs when mono=true). Match exact pattern from sims/detail.tsx:210-218.
3. Replace inline RAT rendering in sessions/index.tsx (line 147-150) with `<RATBadge ratType={session.rat_type} />`.
4. Replace inline RAT rendering in sims/detail.tsx with `<RATBadge>`.
5. Remove local `InfoRow` from sims/detail.tsx, apns/detail.tsx, operators/detail.tsx. Import from `@/components/ui/info-row`.

**Verify:**
- `rg "function InfoRow" web/src/pages` returns 0 matches
- `rg "const.*RAT_DISPLAY.*=" web/src/pages` returns 0 matches
- All 3 detail pages render correctly with shared InfoRow
- TypeScript compilation succeeds

---

### Task 2: Verify Existing Consolidations (Skeleton, format utils, SIM state utils)
**Complexity:** Low
**Depends on:** None
**Files:**
- VERIFY: `web/src/components/ui/skeleton.tsx`
- VERIFY: `web/src/lib/format.ts`
- VERIFY: `web/src/lib/sim-utils.ts`
- VERIFY: `web/src/lib/constants.ts`
**Pattern ref:** N/A (verification only)
**Context refs:**
- AC-1 expects zero local Skeleton definitions outside `web/src/components/ui/skeleton.tsx`
- AC-3 expects `stateLabel` to use `LOST/STOLEN` everywhere (already: `sim-utils.ts:16`)
- `web/src/lib/format.ts` — `formatBytes`, `formatDuration`, `formatNumber`, `timeAgo`

**What:**
1. Run grep to confirm zero local Skeleton definitions: `rg "function Skeleton|const Skeleton" web/src/pages` → 0 matches.
2. Run grep to confirm zero local formatBytes definitions: `rg "function formatBytes" web/src/pages` → 0 matches.
3. Verify `stateLabel('stolen_lost')` returns `'LOST/STOLEN'` in `sim-utils.ts`.
4. Verify all SIM list/detail pages use `stateLabel()` from `@/lib/sim-utils` (not inline `.toUpperCase()`).
5. If any regressions found, fix the import.

**Verify:**
- All grep checks pass with 0 local duplicates
- `web/src/lib/sim-utils.ts` `stateLabel` switch has `stolen_lost → 'LOST/STOLEN'`

---

### Task 3: Code Splitting — Lazy-Load Remaining Eager Routes
**Complexity:** Medium
**Depends on:** None
**Files:**
- EDIT: `web/src/router.tsx`
- EDIT: `web/vite.config.ts`
**Pattern ref:** Existing `lazySuspense()` pattern in router.tsx (line 67-75)
**Context refs:**
- `web/src/router.tsx:8-15` — DashboardPage and SimListPage are eager imports
- `web/src/router.tsx:59-75` — LazyFallback and lazySuspense patterns
- `web/vite.config.ts:13-34` — current build config with manualChunks
- Phase 9 gate: "Single JS bundle 1.57 MB (449 KB gzipped)"
- AC-5 target: largest initial chunk ≤ 250KB gzipped

**What:**
1. Convert `DashboardPage` import (line 12) to `const DashboardPage = lazy(() => import('@/pages/dashboard/index'))`.
2. Convert `SimListPage` import (line 14) to `const SimListPage = lazy(() => import('@/pages/sims/index'))`.
3. Wrap both route elements with `lazySuspense()` (replacing the manual `<ErrorBoundary><DashboardPage /></ErrorBoundary>` and `<ErrorBoundary><SimListPage /></ErrorBoundary>` blocks).
4. Keep `LoginPage`, `TwoFactorPage`, `OnboardingPage`, `NotFoundPage` as eager imports (auth/error pages must load immediately).
5. In `web/vite.config.ts`, add `chunkSizeWarningLimit: 250` inside the `build` block (aligned with AC-5 target).
6. Verify no other eager page imports remain besides auth/error pages.

**Verify:**
- `npm run build` — no chunk size warnings
- Largest initial chunk ≤ 250KB gzipped
- All lazy routes load with skeleton fallback
- TypeScript compilation succeeds

---

### Task 4: Per-Tab ErrorBoundary on SIM Detail + Policy Editor
**Complexity:** Medium
**Depends on:** Task 1 (InfoRow extracted)
**Files:**
- EDIT: `web/src/pages/sims/detail.tsx`
- EDIT: `web/src/pages/policies/editor.tsx`
- VERIFY: `web/src/components/error-boundary.tsx`
- VERIFY: `web/src/components/layout/dashboard-layout.tsx`
**Pattern ref:** Existing ErrorBoundary component (`web/src/components/error-boundary.tsx`)
**Context refs:**
- `web/src/components/error-boundary.tsx` — already has Try Again + Go Home actions
- `web/src/components/layout/dashboard-layout.tsx:60` — Router-level EB already wraps `<Outlet />` with `key={location.pathname}` (auto-reset on navigate, satisfying STORY-056 AC-8)
- `web/src/pages/sims/detail.tsx` — has 5 tab components: OverviewTab, SessionsTab, UsageTab, HistoryTab, DiagnosticsTab
- `web/src/pages/policies/editor.tsx` — has CodeMirror editor area

**What:**
1. Verify router-level ErrorBoundary in DashboardLayout already wraps `<Outlet />` with `key={location.pathname}` (already done — line 60).
2. In `sims/detail.tsx`, wrap each of the 5 tab content areas (`OverviewTab`, `SessionsTab`, `UsageTab`, `HistoryTab`, `DiagnosticsTab`) in `<ErrorBoundary>` tags inside `<TabsContent>`.
3. In `policies/editor.tsx`, wrap the CodeMirror editor area in `<ErrorBoundary>`.
4. Import `ErrorBoundary` in both files from `@/components/error-boundary`.
5. Ensure the ErrorBoundary in tab wrappers does NOT use `key` (only router-level uses location key). Tab errors should isolate to that tab only.

**Verify:**
- E2E: Inject error in SessionsTab — only that tab shows fallback, other tabs work
- E2E: Navigate after crash — new route renders cleanly (router EB resets via key)
- TypeScript compilation succeeds

---

### Task 5: WebSocket Cache Filter Respect
**Complexity:** Medium-High
**Depends on:** None
**Files:**
- EDIT: `web/src/hooks/use-sessions.ts`
**Pattern ref:** Existing WS handler pattern in `use-sessions.ts:56-112`
**Context refs:**
- `web/src/hooks/use-sessions.ts:88` — Cache key hardcoded to `[...SESSIONS_KEY, 'list', {}]` (ignores active filters)
- `web/src/lib/ws.ts` — `wsClient.on('session.started', handler)` pattern
- `web/src/pages/sessions/index.tsx` — uses `useSessionList({})` with empty filters (client-side filtering only via `filterText`)
- AC-6: "WS events only merge into cache if they match the filter predicate"

**What:**
1. Modify `useRealtimeSessionStarted` to accept a `filters` parameter matching the session list query key structure.
2. When updating the query cache, match the cache key exactly: `[...SESSIONS_KEY, 'list', filters]` instead of hardcoded `{}`.
3. Before merging a new session into cache, check if the session matches the active filter predicate (operator_id, apn_id). If filters are active and the session doesn't match, skip the cache update but still invalidate stats.
4. Apply the same pattern to `useRealtimeSessionEnded` — match the correct cache key.
5. Update the call sites in `web/src/pages/sessions/index.tsx` to pass the filters object to both hooks.

**Verify:**
- E2E: Apply filter `state=active` on Live Sessions. WS `session.started` event for different operator does not appear in filtered table.
- Unfiltered view still receives all WS events.
- TypeScript compilation succeeds

---

### Task 6: eSIM Operator Filter + Audit User Filter + Jobs created_by Column
**Complexity:** Medium
**Depends on:** None
**Files:**
- EDIT: `web/src/pages/esim/index.tsx`
- EDIT: `web/src/pages/audit/index.tsx`
- EDIT: `web/src/pages/jobs/index.tsx`
- EDIT: `web/src/hooks/use-esim.ts` (if operator list needed)
**Pattern ref:** Existing filter dropdown pattern in `web/src/pages/jobs/index.tsx:186-237`
**Context refs:**
- `web/src/pages/esim/index.tsx:71` — `filters` state already has `operator_id` field but no dropdown for it
- `web/src/hooks/use-esim.ts` — `useESimList(filters)` already accepts operator_id
- `web/src/pages/audit/index.tsx:173-176` — `AuditFilters` already has `user_id` field (line 235)
- `web/src/hooks/use-audit.ts` — verify filter params sent to API
- `web/src/types/job.ts:18` — `created_by?: string` field exists on Job type
- `web/src/hooks/use-operators.ts` — `useOperatorList()` for operator dropdown data
- `web/src/hooks/use-settings.ts` — likely has users list for audit dropdown
- Design tokens: filter pill active state `--accent-dim`, `--accent/30`, `--accent`

**What:**
1. **eSIM operator filter:** Add operator dropdown to eSIM filter bar. Import `useOperatorList` from `@/hooks/use-operators`. Render a `DropdownMenu` with operator options (same pill pattern as existing state filter). Wire to `filters.operator_id`. The `useESimList` hook already passes `operator_id` to API.
2. **Audit user filter:** Add user filter dropdown to audit filter bar. Fetch users list (import from settings hook or create inline query). Render dropdown with user options. Wire to `filters.user_id` (already exists in AuditFilters type). Verify the hook sends `user_id` as query param to `/audit` API.
3. **Jobs created_by column:** Replace `Created` column (currently shows `created_at`) with `Created By` column showing `job.created_by` (user name/email). The `created_by` field already exists on Job type. If `created_by` is a UUID, resolve it via a users lookup or show truncated ID. Keep `created_at` as a tooltip or secondary text.

**Verify:**
- E2E: eSIM page — select operator from dropdown, list updates via API call (query param visible in Network tab)
- E2E: Audit page — select user from dropdown, list filters correctly
- E2E: Jobs table — `created_by` column shows user identifier for jobs
- TypeScript compilation succeeds

---

### Task 7: Bulk "Assign Policy" Inline Dialog (High Complexity)
**Complexity:** High
**Depends on:** None
**Files:**
- EDIT: `web/src/pages/sims/index.tsx`
- EDIT: `web/src/hooks/use-sims.ts` (add bulk policy assign mutation if missing)
- EDIT: `web/src/hooks/use-policies.ts` (verify policy list hook)
**Pattern ref:** Existing bulk action dialog pattern in `web/src/pages/sims/index.tsx` (bulk state change dialog)
**Context refs:**
- `web/src/pages/sims/index.tsx:78-79` — `selectedIds` state, `bulkDialog` state
- `web/src/pages/sims/index.tsx:222-236` — `handleBulkAction` pattern
- `web/src/hooks/use-policies.ts` — policy list for picker dropdown
- `web/src/hooks/use-sims.ts` — `useBulkStateChange` pattern to follow for bulk policy assign
- API: POST `/sims/bulk-policy` or similar endpoint (check existing API patterns)
- AC-9 requires: policy picker + dry-run preview + confirm. SIM selection preserved throughout.
- Design tokens: Dialog uses `--bg-elevated`, `--border`, `--radius-lg` (14px modal)

**What:**
1. Add a new bulk action option "Assign Policy" in the SIM list bulk actions bar. Only visible when `selectedIds.size > 0`.
2. Create an inline dialog (using existing `Dialog` component) with:
   - **Step 1 — Policy Picker:** Dropdown/select showing all active policies (from `usePolicyList`). Show policy name + version.
   - **Step 2 — Dry-Run Preview:** After policy selection, call dry-run API endpoint to get preview of affected SIMs (count, any conflicts). Display summary.
   - **Step 3 — Confirm:** Show selected policy, SIM count, confirm button.
3. Add `useBulkPolicyAssign` mutation in `use-sims.ts` (follow `useBulkStateChange` pattern). POST to bulk assign endpoint with `{ sim_ids: string[], policy_version_id: string }`.
4. On success, clear selection and invalidate SIM list query.
5. SIM selection (`selectedIds`) must be preserved during the entire dialog flow — do NOT clear on dialog open.

**Verify:**
- E2E: SIM list → select 5 SIMs → bulk "Assign Policy" → dialog opens inline
- Selection count preserved in dialog
- Confirm triggers bulk job
- TypeScript compilation succeeds

---

### Task 8: "Select All in Segment" + Aria Labels + Cleanup
**Complexity:** Medium
**Depends on:** Task 7 (bulk actions context)
**Files:**
- EDIT: `web/src/pages/sims/index.tsx`
- EDIT: `web/src/pages/sessions/index.tsx`
- EDIT: `web/src/pages/jobs/index.tsx`
- EDIT: `web/src/pages/esim/index.tsx`
- EDIT: `web/src/pages/audit/index.tsx`
- VERIFY: `web/src/components/command-palette/command-palette.tsx`
- VERIFY: `web/src/pages/dashboard/index.tsx`
**Pattern ref:** Existing segment selection in `web/src/pages/sims/index.tsx:204-219`
**Context refs:**
- `web/src/pages/sims/index.tsx:82` — `selectedSegmentId` state
- `web/src/pages/sims/index.tsx:186-192` — `toggleSelectAll` only selects visible rows
- `web/src/hooks/use-sims.ts` — segment hooks
- AC-10: "Select all in segment" selects entire segment server-side, not just visible rows
- AC-11: `aria-label` on all icon-only buttons, unused imports cleanup
- `web/src/pages/sessions/index.tsx:284-287` — search clear button lacks aria-label
- Command palette already has `/settings/notifications` (verify-only)
- Dashboard STATE_COLORS already has `stolen_lost` (verify-only)

**What:**
1. **Select All in Segment:** When a segment is active (`selectedSegmentId` is set), add a "Select all N SIMs in segment" button/link above the table. Clicking it:
   - Calls API to get full SIM IDs in segment (e.g., GET `/segments/{id}/sims?ids_only=true`)
   - Sets `selectedIds` to the full set (server-side count, not just visible rows)
   - Shows confirmation dialog with segment name and total count before applying bulk action
2. **Aria Labels:** Add `aria-label` to all icon-only buttons across these pages:
   - Search clear buttons (`<X>` icon): `aria-label="Clear search"`
   - Row action buttons (disconnect, enable, disable, switch): already have text labels — verify
   - Back navigation buttons: `aria-label="Go back"`
   - Close buttons (dialog, panel): `aria-label="Close"`
   - Refresh buttons: verify they have text or add `aria-label="Refresh"`
3. **Unused Imports:** Audit sessions, jobs, esim, audit pages. Remove any unused icon imports or component imports.
4. **Verify pre-done items:**
   - Command palette has `/settings/notifications` with `BellRing` icon
   - `STATE_COLORS` in dashboard has `stolen_lost`

**Verify:**
- E2E: SIM list → "Select all in segment X" → confirmation shows segment count (not visible row count)
- Accessibility: axe-core audit on 5 key pages — zero icon-button-without-label violations
- No unused imports (run linter or manual check)
- TypeScript compilation succeeds

---

## Acceptance Criteria Mapping

| AC | Task(s) | Notes |
|----|---------|-------|
| AC-1 (Skeleton) | Task 2 | Verify-only; already extracted |
| AC-2 (RAT_DISPLAY + RATBadge) | Task 1 | Create RATBadge component, replace inline usage |
| AC-3 (formatBytes, InfoRow, stateVariant, stateLabel) | Task 1, Task 2 | InfoRow extracted (Task 1), rest verify-only (Task 2) |
| AC-4 (ErrorBoundary) | Task 4 | Per-tab wrappers on SIM Detail + Policy Editor; router-level already done |
| AC-5 (Code splitting) | Task 3 | Lazy-load Dashboard + SimList; Vite chunkSizeWarningLimit |
| AC-6 (WS cache filter) | Task 5 | Fix hardcoded cache key in use-sessions.ts |
| AC-7 (eSIM operator + Audit user filters) | Task 6 | New filter dropdowns |
| AC-8 (Jobs created_by) | Task 6 | Replace created_at column with created_by |
| AC-9 (Bulk Assign Policy) | Task 7 | Inline dialog with policy picker + dry-run + confirm |
| AC-10 (Select all in segment) | Task 8 | Server-side segment selection |
| AC-11 (Aria labels, cleanup, command palette, STATE_COLORS) | Task 8 | Aria labels + cleanup; command palette + STATE_COLORS verify-only |

## Wave Plan

| Wave | Tasks | Parallel? | Rationale |
|------|-------|-----------|-----------|
| Wave 1 | Task 1, Task 2, Task 3 | Yes | Independent extractions + code splitting |
| Wave 2 | Task 4, Task 5 | Yes | ErrorBoundary wrappers + WS fix (independent) |
| Wave 3 | Task 6 | Solo | Filter additions + Jobs column |
| Wave 4 | Task 7, Task 8 | Sequential (8 after 7) | Bulk dialog then segment select + cleanup |

## Test Scenarios (from Story)

| Test | Type | Task |
|------|------|------|
| No file outside `skeleton.tsx` defines local Skeleton | Unit/grep | Task 2 |
| `rg "const.*RAT_DISPLAY.*=" web/src/pages` returns 0 | Unit/grep | Task 1 |
| Inject error on SIM Detail Sessions tab — only that tab shows fallback | E2E | Task 4 |
| Navigate after crash — new route renders cleanly | E2E | Task 4 |
| `npm run build` — largest chunk ≤ 250KB gzipped, no warnings | Build | Task 3 |
| Filter `state=active` — WS event for inactive SIM not in table | E2E | Task 5 |
| eSIM page — select operator dropdown, list updates via API | E2E | Task 6 |
| Audit page — select user dropdown, list filters correctly | E2E | Task 6 |
| Jobs table — `created_by` column shows user name | E2E | Task 6 |
| SIM list → select 5 → bulk "Assign Policy" → dialog, confirm triggers job | E2E | Task 7 |
| SIM list → "Select all in segment" → shows segment count → confirm | E2E | Task 8 |
| axe-core on 5 key pages — zero icon-button-without-label | Accessibility | Task 8 |

# Implementation Plan: FIX-249 — Global React #185 crash: `useFilteredEventsSelector` re-render loop

**Tier:** P0 hotfix | **Effort:** S | **Mode:** AUTOPILOT | **Date:** 2026-04-26
**Story:** `docs/stories/fix-ui-review/FIX-249-react-185-event-stream-loop.md`
**Surfaced by:** `docs/stories/fix-ui-review/FIX-233-gate.md` § F-U1 (verified pre-existing on HEAD~1)

## Goal

Stabilize the global Event Stream drawer selector so it returns a referentially stable result when underlying store data has not changed, eliminating the React #185 ("Maximum update depth exceeded") crash that bricks every protected SPA route.

## Architecture Context

### Problem Surface

The drawer is mounted globally inside `AppShell`, so any render-loop in it crashes every authenticated page. The defect lives at one consumption site:

```ts
// web/src/components/event-stream/event-stream-drawer.tsx:40
const filteredEvents = useEventStore(useFilteredEventsSelector)
```

`useFilteredEventsSelector` (defined in `web/src/stores/events.ts:295`) is a pure selector that returns `s.events.filter(...)` — a fresh array reference on every store read. Zustand v5 internally uses `useSyncExternalStore`; an unstable selector return value triggers an immediate re-render, which re-runs the selector, which returns a new array, which triggers another re-render → React #185.

### Components Involved

- **Consumer (single):** `web/src/components/event-stream/event-stream-drawer.tsx` — mounted globally inside `AppShell`. The crash blast radius = entire authenticated SPA.
- **Selector (definition only):** `web/src/stores/events.ts` § `useFilteredEventsSelector` (line ~295). Pure function `(s: EventState) => LiveEvent[]`.
- **Store:** Zustand v5 (`"zustand": "^5.0.12"`, verified in `web/package.json`). `useShallow` is the v5-canonical mechanism for stabilizing selectors that return derived collections, since v5 removed the `useStore(selector, equalityFn)` overload.

### Verified Context (greps run pre-plan)

- **Zustand version:** v5.0.12 → use `useShallow` from `zustand/react/shallow` (v5-canonical path; `zustand/shallow` is the v4 alias and also still re-exports it, but v5 docs canonicalize `zustand/react/shallow`).
- **Existing useShallow usage:** `grep -rn 'useShallow' web/src/` → **0 matches**. This is the first introduction; no precedent to follow.
- **Selector consumers:** `grep -rn 'useFilteredEventsSelector' web/src/` → exactly 1 definition (`events.ts:295`) + 1 consumer (`event-stream-drawer.tsx:9 import, :40 call`). No other call sites need updating.
- **Risk-2 verification (in-place mutation of `state.events`):** `grep -nE '\.push|\.splice|\.unshift' web/src/stores/events.ts` shows mutations only on **local copies** (`newHisto = [...s.histogram]; newHisto.push(...)`) and **never** on `state.events`. The reducer pattern is consistently immutable. → `useShallow` will work; no reducer refactor needed.

### Path Decision: **Path A (`useShallow` wrap)**

Choice rationale (per advisor reconcile):

| Criterion | Path A (`useShallow` wrap) | Path B (split + `useMemo`) |
|---|---|---|
| Files touched | **1** (drawer only) — selector stays pure | 2 (drawer + filter helper exposed) |
| Blast radius | Single line at consumption site | Multi-line refactor of consumer hook deps |
| v5 idiom | Yes — canonical v5 mechanism | Manual; bypasses store equality machinery |
| Future consumers | Footgun if next caller forgets wrap (logged as Risk) | Same risk — `applyFilters` must be re-called manually |
| Reverts cleanly | Yes — single import + single wrap | More surface to revert |

**Chosen: Path A.** Minimal blast radius is the deciding factor for a global-mount hotfix. The selector definition stays untouched (still a pure function on `EventState`); `useShallow` wraps it at the call site only. Note that this **shrinks the spec's "Files to Touch" from 2 to 1** — `web/src/stores/events.ts` is NOT modified.

### Import Path

```ts
import { useShallow } from 'zustand/react/shallow'
```

Verified present at `web/node_modules/zustand/react/shallow.d.ts`. (The v4-style `'zustand/shallow'` also re-exports `useShallow` in v5 but is not the documented v5 path.)

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| AC-1 — Stable reference; render count bounded after store-unrelated parent renders | Task 1 | Task 2 (browser-smoke proxy: console clean of React #185 across 6 routes — render-bounded by construction since `useShallow` enforces shallow-equal short-circuit; absence of #185 is the operational proof) |
| AC-2 — No React #185 on `/sims`, `/policies`, `/dashboard`, `/sessions` (+ detail routes) | Task 1 | Task 2 (manual browser smoke, console DevTools open) |
| AC-3 — Existing UX preserved: incoming events render, filter toggles apply | Task 1 | Task 2 (live WS event dispatch + filter toggle smoke) |
| AC-4 — TS strict: no `any`, no shape regression on `EventStoreState` | Task 1 | Task 2 (`tsc --noEmit` on `web/`) |

**AC-1 substitution disclosure:** The spec calls for a unit test asserting selector invocation count ≤ 6. The repo has **no vitest** infrastructure (`web/vitest.config.*` absent; `package.json` has no `"test"` script for the SPA — confirmed by FIX-232/FIX-233 precedent). Installing vitest is out of scope for this S-effort hotfix and would dwarf the actual fix. AC-1 is therefore verified by the **browser-smoke proxy**: an unbounded re-render loop crashes with React #185 within milliseconds; if the console is clean across all 6 routes including parent renders triggered by route navigation, the render count is bounded by construction. This downgrade is documented transparently here and in the Decisions log entry below.

## Tasks (1 wave, 2 tasks)

### Task 1: Wrap selector with `useShallow` at the drawer consumption site
- **Files:** Modify `web/src/components/event-stream/event-stream-drawer.tsx`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** No existing `useShallow` consumer in the codebase (grep → 0 matches). Establish pattern. The change is mechanical — add one import line, wrap one selector call. Reference `event-stream-drawer.tsx:30-40` for the surrounding selector-block style (one selector per line, `useEventStore(s => ...)` form).
- **Context refs:** "Architecture Context > Path Decision: Path A", "Architecture Context > Import Path", "Architecture Context > Verified Context"
- **What:**
  1. Add import: `import { useShallow } from 'zustand/react/shallow'` (place it under the existing `useEventStore` import block at lines 7-11, ordered so the relative path-import block stays grouped).
  2. At line 40, change:
     - **From:** `const filteredEvents = useEventStore(useFilteredEventsSelector)`
     - **To:** `const filteredEvents = useEventStore(useShallow(useFilteredEventsSelector))`
  3. Do **NOT** modify `web/src/stores/events.ts` — selector definition remains pure. Do **NOT** touch any other event-stream component (`event-row`, `event-filter-bar`).
- **Verify:**
  - `grep -n 'useShallow' web/src/components/event-stream/event-stream-drawer.tsx` → must show 2 matches (import + call).
  - `grep -n 'useShallow' web/src/stores/events.ts` → must show 0 matches (selector untouched).
  - File diff: exactly 2 lines changed (1 import added, 1 selector call modified). No collateral edits.

### Task 2: Verification — TS-smoke + browser-smoke + live-event-smoke
- **Files:** none modified — verification only
- **Depends on:** Task 1
- **Complexity:** low
- **Context refs:** "Acceptance Criteria Mapping", "Test Plan"
- **What:** Run the three smoke checks below. Any failure = Task 1 must be re-attempted; do NOT proceed to Reviewer with a failing browser smoke.
  1. **TS-smoke (AC-4):** From `web/`, run `npx tsc --noEmit`. Must complete with zero errors. (Existing pre-fix tsc errors unrelated to this change are OK only if they pre-date HEAD; capture the diff if any new error appears.)
  2. **Browser-smoke (AC-1, AC-2):** Start dev server (`make web-dev` or equivalent). With browser DevTools console open, navigate in this exact order: `/dashboard`, `/sims`, `/policies`, `/sessions`, then drill `/policies/<id>` and `/sims/<id>` (use any existing seed row). Console must be clean of:
     - `Minified React error #185`
     - `Maximum update depth exceeded`
     - `getSnapshot should be cached` (a related v5 warning if `useShallow` is misapplied)
  3. **Live-event-smoke (AC-3):** With drawer open on `/dashboard`, dispatch a test event via WebSocket (or wait for any naturally-occurring event from the running stack). Verify the new event renders in the drawer list and that toggling at least one filter chip in `EventFilterBar` updates the visible list as expected.
- **Verify:** All 3 smokes pass. Capture findings in the Reviewer's gate evidence (route URLs visited, console screenshot/text-dump if needed).

## Risks & Mitigations

1. **Buggy fix re-bricks the SPA (drawer is global-mount).** → Mitigation: Browser smoke is mandatory before declaring done (Task 2). The smoke explicitly covers 6 routes, not just one.
2. **Reducer in-place mutation of `state.events` would defeat `useShallow`.** → Mitigation: Pre-plan grep verified all `state.events` writes are immutable replacements (spread/filter/concat). No reducer refactor needed for this fix.
3. **Selector remains a footgun for future consumers.** A new caller doing `useEventStore(useFilteredEventsSelector)` (without `useShallow`) reintroduces the bug. → Mitigation: NOT fixed in this hotfix (out of scope, S-effort). Logged as a Decisions-log entry to consider later (rename to `filteredEventsSelector` without the `use` prefix and/or pre-wrap inside the selector module). Captured in this plan's Decisions section so the Reviewer can promote to a follow-up FIX if needed.
4. **`useShallow` import path mismatch.** v4 used `zustand/shallow`; v5 canonical is `zustand/react/shallow`. → Mitigation: Path explicitly verified (`web/node_modules/zustand/react/shallow.d.ts` exists) and pinned in this plan. Task 1's grep verify catches a typo'd import.
5. **Filter mutation edge case.** If `filters` ever held arrays mutated in place, the selector's filter call could short-circuit incorrectly. → Mitigation: Filter actions in `events.ts` use spread-based replacements (verified by inspection of the reducer block). No additional change needed.

## Test Plan

- **AC-1 (render-count bound):** Browser-smoke proxy — unbounded re-renders crash within ms with React #185; clean console across 6 routes proves render-bounded behavior. Unit test deferred (no vitest in repo; future hardening pass owns this).
- **AC-2 (no React #185 on protected routes):** Manual DevTools console verification across `/dashboard`, `/sims`, `/policies`, `/sessions`, `/policies/<id>`, `/sims/<id>`.
- **AC-3 (UX preserved):** Live-event smoke — open drawer on `/dashboard`, observe a real or test WS event render, toggle at least one filter chip, verify list updates.
- **AC-4 (TS strict):** `npx tsc --noEmit` from `web/`. Zero new errors.

## Decisions Log Entries (Proposed for Reviewer to assign DEV-NNN)

- **DEC-A:** Adopt **Path A (`useShallow` wrap at consumer)** over Path B (split + `useMemo`). Rationale: minimum blast radius for global-mount hotfix; idiomatic Zustand v5; selector definition untouched. Spec's "Files to Touch" list (drawer + store) is reduced to 1 file (drawer only).
- **DEC-B:** Import `useShallow` from **`zustand/react/shallow`** (v5-canonical path), not `zustand/shallow` (v4 alias retained for compat).
- **DEC-C:** **AC-1 unit-test substitution.** Verify "selector invocation count bounded" via browser-smoke proxy (clean console across 6 routes) instead of vitest assertion. No FE test runner is installed in the SPA (`web/vitest.config.*` absent; FIX-232/FIX-233 precedent). Installing vitest is out of scope for this S-effort hotfix and explicitly deferred to a future FE hardening pass.
- **DEC-D:** **Selector-naming footgun deferred.** `useFilteredEventsSelector` is a plain selector (no React state), but its `use*` prefix invites future callers to drop the `useShallow` wrap. Consider a follow-up rename to `filteredEventsSelector` (or pre-wrap inside the selector module) in a future hardening FIX — not patched here to preserve minimum-diff contract.

## USERTEST Scenarios (Skeleton)

1. **Smoke happy path (AC-2):** User logs in, lands on `/dashboard`, opens DevTools console, navigates to `/sims` → `/policies` → `/sessions` → drills into a SIM detail and a Policy detail. Expected: every page renders, no React error overlay, console contains zero `Minified React error #185` and zero `Maximum update depth exceeded`.
2. **Drawer interaction (AC-3 part 1):** User clicks the global Event Stream toggle (header icon or shortcut) on `/dashboard`. Drawer opens. Expected: drawer list either shows existing events or an empty state — no crash, no console errors.
3. **Live event flow (AC-3 part 2):** With drawer open, user waits for or dispatches a new system event (e.g., a SIM activate triggering a `sim.updated` WS event). Expected: new event appears in the drawer list within a couple of seconds; relative-time stamp updates on the 15s tick; no console errors.
4. **Filter toggling (AC-3 part 3):** User clicks any chip in `EventFilterBar` (e.g., severity = "warning"). Expected: visible event list narrows to matching items; clicking again restores; no console errors; no flicker.
5. **Pause/resume (AC-3 part 4):** User clicks pause, observes queued counter increment, then clicks resume. Expected: queued events flush into the visible list; no console errors.
6. **Repro of original bug (negative case):** With the FIX-249 change reverted (or stashed), the same flow as scenario 1 should crash with React #185 — confirms the fix is what unblocks the route. (Reviewer-only; do NOT ship the revert.)

## Story-Specific Compliance Rules

- **UI:** Pure consumer-side change. No new components, no design tokens, no SCREENS.md update needed. `frontend-design` skill not required (no visual change).
- **API:** None.
- **DB:** None. No migrations.
- **Multi-tenant safety:** N/A — pure FE selector stability fix.
- **Pagination:** N/A.
- **Audit log:** N/A.
- **ADR compliance:** No ADR is touched.

## Bug Pattern Warnings

- **PAT-015 ("declared-but-unmounted")** does NOT apply — this fix touches an already-mounted consumer (the drawer is mounted in `AppShell`).
- **No matching prior pattern** for unstable-selector / Zustand v5 selector stability. This fix may itself seed a new pattern (e.g., **PAT-NNN: "Selectors returning derived collections must use `useShallow` under Zustand v5"**) — the Reviewer should consider promoting it into `docs/brainstorming/bug-patterns.md` post-merge.

## Tech Debt (from ROUTEMAP)

No tech debt items currently target FIX-249. (FIX-249 itself is the resolution of a defect surfaced under FIX-233 Gate F-U1; ROUTEMAP entry will be marked resolved on Reviewer pass.)

## Mock Retirement

No mock retirement for this story (pure FE selector fix; no API surface).

## Wave & Task Summary

- **Waves:** 1
- **Tasks:** 2 (Task 1 = code change; Task 2 = verification suite)
- **Total complexity:** low (both tasks)
- **Files modified:** 1 (`web/src/components/event-stream/event-stream-drawer.tsx`)
- **Lines changed:** 2 (1 import added, 1 selector call wrapped)

## Pre-Validation Checklist (Planner self-validates before write)

- [x] Min plan substance for S effort (≥30 lines, ≥2 tasks) — exceeded.
- [x] Required headers present (`## Goal`, `## Architecture Context`, `## Tasks`, `## Acceptance Criteria Mapping`).
- [x] Embedded specs (no "see ARCHITECTURE.md") — selector code, file paths, import path, exact diff embedded inline.
- [x] No DB / API / UI-design surface → those compliance subsections marked N/A explicitly.
- [x] Each task has `Files`, `Depends on`, `Complexity`, `Pattern ref`, `Context refs`, `What`, `Verify`.
- [x] `Context refs` point to actual sections present in this plan.
- [x] Tasks are functionally grouped (code + verify); each touches ≤3 files.
- [x] AUTOPILOT S-effort discipline: 2 tasks, 1 wave, no scope creep into other event-stream files.
- [x] Browser-smoke discipline: explicit route list, explicit console-error vocabulary to grep for.
- [x] Risk Register includes global-mount blast risk + reducer-mutation precondition + future-consumer footgun.

# Implementation Plan: FIX-217 — Timeframe Selector Pill Toggle Unification

## Goal
Unify every timeframe selector across the Argus SPA behind a single, canonical `<TimeframeSelector>` primitive (pill/toggle group), including role-gating, keyboard navigation, and a "Custom" range popover with URL sync.

## Summary
Argus already ships `web/src/components/ui/timeframe-selector.tsx` adopted by 3 pages. Four other surfaces diverge (inline button groups in `admin/api-usage`, `admin/delivery`; `<Select>` dropdowns in `operators/detail` TrafficTab and `apns/detail` TrafficTab). CDRs page uses a bespoke `PRESET_RANGES` + from/to flow that should reuse the unified component for presets while keeping its URL sync. Custom range picker does not exist — we will ship a thin popover with two `datetime-local` inputs (no new dep). Role-gated presets (analyst cannot use `30d` per FIX-215 USERTEST) move from pages into a new `disabledPresets` API on the primitive.

## Scope Decisions (locked)

1. **Primitive name**: Keep `TimeframeSelector` (existing file `web/src/components/ui/timeframe-selector.tsx`). Spec's `<TimeframePills>` name is advisory — renaming 3 import sites adds churn for zero value. Document the canonical primitive in FRONTEND.md as "Timeframe Pattern".
2. **Canonical presets (AC-1)**: `1h | 24h | 7d | 30d | custom`. Default: `24h`. Existing extra presets (`15m`, `6h`) remain available via the optional `options` prop override for operator-traffic / APN-traffic contexts that legitimately need sub-hour granularity.
3. **Custom range (AC-2)**: Emit `{ value: 'custom', from: ISO, to: ISO }` via `onChange`. Popover contains two `<input type="datetime-local">` controls + Apply/Cancel. No new dependency.
4. **URL sync (AC-3)**: Push this to **page level**, not the primitive. The primitive is controlled; pages wire `useSearchParams` to the current value. Provide optional `useTimeframeUrlSync()` hook in `web/src/hooks/use-timeframe-url.ts` that pages may adopt for consistency with CDRs' `?from=...&to=...` pattern.
5. **Adoption targets (AC-4)**:
   - `web/src/pages/admin/api-usage.tsx` — replace inline button group (lines 19-23, 73-92).
   - `web/src/pages/admin/delivery.tsx` — replace inline button group (lines 13-17, ~116-130).
   - `web/src/pages/operators/detail.tsx` — `TrafficTab` `<Select>` dropdown (lines 549-571); AAA-health `HistoryTab` selector (lines 297-300).
   - `web/src/pages/apns/detail.tsx` — `TrafficTab` `<Select>` dropdown (lines 609-631).
   - `web/src/pages/cdrs/index.tsx` — AC-2 exercise page: render primitive on top of existing `PRESET_RANGES` flow; add Custom popover; keep existing URL sync.
   - **NOT in scope (explicit N/A)**: Alerts page (no selector today), Capacity page (no selector today), Dashboard main page (only a static "Last 24h" label). Spec AC-4 lists these but the feature surface doesn't exist; scope creep deferred to a follow-up FIX.
   - **NOT in scope**: SLA page's 6/12/24-month "rolling window" selector — it is a window-length selector, not a timeframe preset; unrelated semantics. Leave unchanged.
6. **Role-gating**: Add `disabledPresets?: TimeframePreset[]` prop. Pages decide policy (e.g., CDRs passes `['30d']` when `user.role === 'analyst'`). Disabled preset is rendered but non-interactive with `aria-disabled="true"` and title "Not available for your role".
7. **A11y (AC-5)**: Root `role="group" aria-label="Timeframe"`. Each button: `aria-pressed={active}`, keyboard `ArrowLeft`/`ArrowRight` cycles focus, `Home`/`End` jump to first/last. Adopt SLA page's `role="group" aria-pressed` convention (already in `pages/sla/index.tsx` lines 252-272).
8. **Typing**: Export `TimeframePreset = '15m' | '1h' | '6h' | '24h' | '7d' | '30d' | 'custom'` as a string-literal union.

## Architecture Context

### Components Involved

| Component | Layer | Path | Responsibility |
|-----------|-------|------|----------------|
| `TimeframeSelector` (atom) | `web/src/components/ui/timeframe-selector.tsx` | Pill toggle group (extend existing) | Pure controlled UI; no side effects |
| `TimeframeCustomPopover` (molecule, internal) | same file | Date-range popover | Renders two `datetime-local` inputs inside `<Popover>` |
| `useTimeframeUrlSync` (hook) | `web/src/hooks/use-timeframe-url.ts` (NEW) | Optional URL sync helper | Reads/writes `?tf=&from=&to=` via `useSearchParams` |
| `Popover` (existing) | `web/src/components/ui/popover.tsx` | Provides anchored overlay | No changes |

### Data Flow (Custom range)

```
User clicks "Custom" pill
  → TimeframeSelector sets internal pending state
  → Popover opens anchored on Custom pill
  → User picks from/to via <input type="datetime-local">
  → Click Apply → onChange({ value: 'custom', from: ISO, to: ISO })
  → Parent page merges into its filters / URL params
  → React Query refetches with new range
```

### API Shape

```ts
export type TimeframePreset = '15m' | '1h' | '6h' | '24h' | '7d' | '30d' | 'custom'

export interface TimeframeValue {
  value: TimeframePreset
  from?: string  // ISO-8601; required when value === 'custom'
  to?: string    // ISO-8601; required when value === 'custom'
}

export interface TimeframeSelectorProps {
  value: TimeframeValue | TimeframePreset     // Accept plain preset for backwards compat with 3 existing pages
  onChange: (v: TimeframeValue) => void
  options?: { value: TimeframePreset; label: string }[]   // Override canonical preset list
  disabledPresets?: TimeframePreset[]                     // Role-gate specific presets
  allowCustom?: boolean                                    // Default true; opt-out for tight strips
  className?: string
}
```

**Back-compat rule:** existing callers pass `value: string` and `onChange: (s: string) => void`. The new signature MUST accept both — if incoming `value` is a string, internally wrap as `{ value }`; when emitting to a legacy `onChange`, call the string-returning variant. Guarded by an overload so TypeScript does not regress 3 existing pages.

### Screen Mockup

```
┌──────────────────────────────────────────────────┐
│ [ 1h ] [ 24h ] [ 7d ] [ 30d ] [ Custom ▾ ]       │
│  aria-pressed aria-disabled (role-gated)         │
└──────────────────────────────────────────────────┘

When "Custom" clicked:

┌──────────────────────────────────┐
│ From  [ 2026-04-22T00:00  ]      │
│ To    [ 2026-04-22T23:59  ]      │
│           [ Cancel ]  [ Apply ]  │
└──────────────────────────────────┘
```

### Design Token Map

| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Group container border | `border border-border` | `border-gray-*`, hex |
| Group container bg | `bg-bg-elevated` | `bg-[#...]`, `bg-white` |
| Container radius | `rounded-[var(--radius-sm)]` | `rounded-md` |
| Active pill bg | `bg-accent` | `bg-blue-*` |
| Active pill text | `text-bg-primary` | `text-white` |
| Inactive pill text | `text-text-secondary` | `text-gray-500` |
| Hover bg | `bg-bg-hover` | `bg-gray-*` |
| Hover text | `text-text-primary` | `text-black` |
| Disabled pill | `opacity-40 cursor-not-allowed` + `aria-disabled` | no visual state |
| Focus ring | `focus-visible:outline-accent focus-visible:outline-2` | `focus:ring-blue` |
| Typography (pill) | `text-xs font-medium` | `text-[11px]` |
| Spacing (pill) | `px-2.5 py-1` | `px-[10px]` |
| Pill radius | `rounded-[3px]` | `rounded-sm` |
| Popover container | reuse `<PopoverContent>` tokens | hand-rolled div |
| Popover input | reuse `<Input>` primitive tokens | raw `<input>` styling |

### Components to REUSE

| Component | Path | Use For |
|-----------|------|---------|
| `<Button>` | `web/src/components/ui/button.tsx` | Apply/Cancel inside Custom popover |
| `<Popover>` / `<PopoverTrigger>` / `<PopoverContent>` | `web/src/components/ui/popover.tsx` | Custom range overlay |
| `<Input>` | `web/src/components/ui/input.tsx` | `datetime-local` inputs (NEVER raw `<input>`) |
| `<TimeframeSelector>` (this story) | `web/src/components/ui/timeframe-selector.tsx` | All timeframe surfaces — no new ad-hoc groups |

## Prerequisites
- [x] FIX-215 complete (SlidePanel hardening + SLA pattern examples)
- [x] FIX-216 complete (Modal Pattern FRONTEND.md section established — follow same documentation style)
- [x] `components/ui/popover.tsx` exists and is used elsewhere
- [x] No new npm dependency required

## Tasks

### Task 1 — Extend `<TimeframeSelector>` primitive (API + a11y)
- **Files:** Modify `web/src/components/ui/timeframe-selector.tsx`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `web/src/pages/sla/index.tsx` lines 243-283 — follow its `role="group"`, `aria-pressed`, `aria-label` pattern. Read current `timeframe-selector.tsx` for starting structure.
- **Context refs:** Architecture Context > API Shape; Design Token Map; Scope Decisions #1, #2, #7
- **What:**
  - Export `TimeframePreset` union and `TimeframeValue` type.
  - Add `disabledPresets`, `allowCustom` props; keep `options` override.
  - Accept both legacy `string` and new `TimeframeValue` for `value`/`onChange` (overloads + internal normalizer).
  - Add `role="group"`, `aria-label="Timeframe"` on container.
  - Each pill: `aria-pressed`, `aria-disabled`, keyboard `ArrowLeft`/`ArrowRight`/`Home`/`End` handlers (roving `tabIndex`).
  - Canonical default preset list becomes `1h | 24h | 7d | 30d`; Custom appended when `allowCustom !== false`.
- **Verify:** `grep -E "#[0-9a-fA-F]{3,6}" web/src/components/ui/timeframe-selector.tsx` returns ZERO matches. TypeScript strict: `cd web && npm run typecheck` passes.

### Task 2 — Add Custom range popover + `useTimeframeUrlSync` hook (AC-2, AC-3)
- **Files:** Modify `web/src/components/ui/timeframe-selector.tsx`; Create `web/src/hooks/use-timeframe-url.ts`
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** Read `web/src/components/event-stream/event-filter-bar.tsx` for existing Popover usage; read `web/src/pages/cdrs/index.tsx` lines 94-135 for the URL-sync pattern to mirror (don't copy-paste; generalize).
- **Context refs:** Architecture Context > Data Flow; API Shape; Scope Decisions #3, #4
- **What:**
  - When `value === 'custom'`, clicking the Custom pill opens `<Popover>` anchored on the pill.
  - Popover body: two `<Input type="datetime-local">` (from/to) with labels; validation — `to > from`; error text via `text-danger` token.
  - Footer: `<Button variant="outline">Cancel</Button>` + `<Button>Apply</Button>`. Apply emits `onChange({ value: 'custom', from, to })`.
  - Display the active custom range on the pill label (e.g., `Custom · Apr 22 → Apr 23`) truncated to 22 chars.
  - Hook `useTimeframeUrlSync(defaultValue)` returns `[TimeframeValue, (v: TimeframeValue) => void]`, keeps `?tf=`, `?from=`, `?to=` in sync via `useSearchParams`, with replace-state to avoid history spam.
- **Verify:** Popover opens/closes correctly; `typecheck` passes; round-trip test (value → URL → parse → value) in Task 7 asserts correctness.

### Task 3 — Adopt in `admin/api-usage` + `admin/delivery` (Wave 2 — parallel)
- **Files:** Modify `web/src/pages/admin/api-usage.tsx`, `web/src/pages/admin/delivery.tsx`
- **Depends on:** Task 1 (Task 2 optional — these pages don't need Custom)
- **Complexity:** low
- **Pattern ref:** Read `web/src/pages/dashboard/analytics.tsx` lines 300-310 — follows the minimal `<TimeframeSelector value onChange />` pattern.
- **Context refs:** Scope Decisions #5; Design Token Map
- **What:**
  - Delete inline `WINDOW_OPTIONS` array + `<Button>` map; replace with `<TimeframeSelector value={window} onChange={setWindow} options={[{value:'1h',label:'Last 1h'},{value:'24h',label:'Last 24h'},{value:'7d',label:'Last 7d'}]} allowCustom={false} />`.
  - Preserve existing `useState` types by passing `onChange={(v) => setWindow((typeof v === 'string' ? v : v.value) as '1h'|'24h'|'7d')}`.
- **Verify:** `grep -n "WINDOW_OPTIONS\\|flex rounded-lg border" web/src/pages/admin/api-usage.tsx web/src/pages/admin/delivery.tsx` returns zero hits in those files; typecheck passes; visual match with dashboard analytics.

### Task 4 — Adopt in `operators/detail` (TrafficTab + HistoryTab) and `apns/detail` TrafficTab (Wave 2 — parallel)
- **Files:** Modify `web/src/pages/operators/detail.tsx`, `web/src/pages/apns/detail.tsx`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `web/src/pages/dashboard/analytics-cost.tsx` lines 22-27, 130-134 — custom preset options for context-specific needs.
- **Context refs:** Scope Decisions #5; Design Token Map
- **What:**
  - `operators/detail.tsx` TrafficTab (lines 549-571): replace `<Select options={periodOptions} ... />` with `<TimeframeSelector options={[{value:'1h',label:'1h'},{value:'6h',label:'6h'},{value:'24h',label:'24h'},{value:'7d',label:'7d'},{value:'30d',label:'30d'}]} allowCustom={false} value={period} onChange={(v) => setPeriod(typeof v==='string'? v : v.value)} />`.
  - Same treatment for HistoryTab's `WINDOW_OPTIONS` (lines 496-501 and usages around 297-300).
  - `apns/detail.tsx` TrafficTab (lines 609-631): identical replacement.
  - Remove labels "Last 1 hour", "Last 6 hours" — switch to terse `1h`, `6h` to match primitive default styling.
- **Verify:** `grep -n "<Select.*periodOptions\\|<Select.*options={WINDOW_OPTIONS" web/src/pages/operators/detail.tsx web/src/pages/apns/detail.tsx` → zero hits; visual parity with dashboard analytics.

### Task 5 — Adopt Custom popover on `cdrs/index.tsx` (AC-2 flagship + role-gating)
- **Files:** Modify `web/src/pages/cdrs/index.tsx`
- **Depends on:** Task 2
- **Complexity:** medium
- **Pattern ref:** Current implementation in same file (lines 52-135) — keep `PRESET_RANGES` semantics but surface via `<TimeframeSelector>`.
- **Context refs:** Scope Decisions #5, #6; API Shape
- **What:**
  - Map `PRESET_RANGES` → `{value: '1h'|'6h'|'24h'|'7d'|'30d', label}`; map "custom" → ISO from/to.
  - Render `<TimeframeSelector options={[1h,6h,24h,7d,30d]} allowCustom disabledPresets={!isAdmin ? ['30d'] : []} value={currentTimeframe} onChange={handleTimeframeChange} />`.
  - `handleTimeframeChange` sets filters.from/filters.to; existing URL sync (`setSearchParams`) handles the rest.
  - Remove the bespoke preset pill markup but keep storage/URL hydration logic.
- **Verify:** CDRs page renders the unified pill strip; analyst role: `30d` pill shows disabled styling + `aria-disabled="true"`; Custom popover opens, Apply sets `?from=&to=`; refresh preserves range.

### Task 6 — FRONTEND.md "Timeframe Pattern" section (doc)
- **Files:** Modify `docs/FRONTEND.md`
- **Depends on:** Task 1, Task 2
- **Complexity:** low
- **Pattern ref:** Existing `## Modal Pattern` section in `docs/FRONTEND.md` lines 108-159 — mirror its structure (When to use / Structure / Visual contract / A11y notes).
- **Context refs:** All Scope Decisions
- **What:** Add `## Timeframe Pattern` after Modal Pattern. Document: canonical presets, when to override `options`, role-gating via `disabledPresets`, Custom popover semantics, URL sync hook, a11y contract, "never hand-roll pill groups" rule. Include a short code snippet of the canonical usage.
- **Verify:** `grep -n "## Timeframe Pattern" docs/FRONTEND.md` → 1 match; section covers all 7 Scope Decisions.

### Task 7 — Tests + visual audit (Wave 3)
- **Files:** Create `web/src/__tests__/timeframe-selector.test.tsx`
- **Depends on:** Task 1, Task 2, Task 3, Task 4, Task 5
- **Complexity:** medium
- **Pattern ref:** Read `web/src/__tests__/event-stream.smoke.test.tsx` — follows the existing vitest + RTL conventions.
- **Context refs:** Acceptance Criteria Mapping
- **What:**
  - Unit: renders all presets; clicking preset emits `{ value }`; `aria-pressed` flips; `disabledPresets` renders `aria-disabled`; keyboard arrow navigation moves focus; Custom opens popover, Apply emits `{ value:'custom', from, to }` with ISO strings.
  - Integration (optional smoke): `useTimeframeUrlSync` round-trips a preset and a custom range through `useSearchParams`.
  - Visual audit checklist added to `docs/stories/fix-ui-review/FIX-217-step-log.txt` (5 pages × light/dark = 10 screenshots referenced).
- **Verify:** `cd web && npx vitest run timeframe-selector` passes; no hex/px escapes in any adopted page (`grep -E "#[0-9a-fA-F]{3,6}" web/src/pages/admin/api-usage.tsx web/src/pages/admin/delivery.tsx web/src/pages/operators/detail.tsx web/src/pages/apns/detail.tsx web/src/pages/cdrs/index.tsx` → no new matches introduced by this story).

## Waves

| Wave | Tasks | Parallelism | Rationale |
|------|-------|-------------|-----------|
| 1 | Task 1, Task 2 | Serial (2 → 1) | Primitive contract must exist before popover; popover depends on it |
| 2 | Task 3, Task 4, Task 5, Task 6 | Parallel (4-way) | Independent page migrations + doc; all consume Task 1/2 output |
| 3 | Task 7 | Serial | Tests + audit validate all of Wave 2 |

## Acceptance Criteria Mapping

| AC | Requirement | Task(s) | Verified By |
|----|-------------|---------|-------------|
| AC-1 | Shared pill component (1h/24h/7d/30d/Custom default 24h) | Task 1 | Task 7 unit tests |
| AC-2 | Custom → date range popover | Task 2, Task 5 | Task 7 popover test; manual QA on `/cdrs` |
| AC-3 | URL sync `?from=&to=` (+ `?tf=` preset) | Task 2 (hook), Task 5 (CDRs adoption) | Task 7 round-trip test; manual deep-link refresh |
| AC-4 | Applied to 5 real surfaces (Dashboard/Analytics/CDRs/Analytics-Cost already covered; this story adds api-usage/delivery/operators/apns; Alerts/Capacity/Dashboard N/A per Scope Decision #5) | Task 3, Task 4, Task 5 | Visual audit in Task 7 |
| AC-5 | Single height/padding + keyboard nav | Task 1 (tokens + arrow keys) | Task 7 keyboard test |

**Scope-creep callout**: Spec's AC-4 list names Alerts, Capacity, Dashboard. Those pages have no timeframe selector today — see Scope Decision #5. AMIL orchestrator should treat AC-4 as PARTIAL-by-design and either accept the N/A rationale or open a follow-up FIX.

## Story-Specific Compliance Rules

- **UI**: Design tokens only — no hex, no px values (per FRONTEND.md). No raw `<input>` / `<button>` outside the primitive file — pages consume the primitive.
- **A11y**: `role="group"` + `aria-label` on container; `aria-pressed` + `aria-disabled` on each pill; roving `tabIndex`; keyboard arrow/Home/End support. Popover must trap focus (reuses `<Popover>` primitive which already handles this).
- **TypeScript**: Strict mode; export types; back-compat overloads for 3 existing call sites (no breaking changes).
- **No new npm deps**: Custom range uses `<input type="datetime-local">` — no react-day-picker.
- **ADR alignment**: No ADR impact (pure client component change).

## Bug Pattern Warnings

No matching bug patterns in `docs/brainstorming/bug-patterns.md` (file not present for this track). However, from prior fix-ui-review findings:
- **Hardcoded colors in reused components**: The existing `timeframe-selector.tsx` currently uses token classes — keep it that way. Reject any `#RRGGBB` or arbitrary `text-[Npx]` introductions.
- **a11y regressions on custom interactive elements**: SLA page already demonstrates the `role="group" aria-pressed` pattern — copy it; don't invent a variant.
- **URL-sync double-write loops**: CDRs page uses `setSearchParams(p, { replace: true })` to avoid history spam — `useTimeframeUrlSync` must do the same.

## Tech Debt (from ROUTEMAP)

No ROUTEMAP tech-debt items target FIX-217. This story itself reduces debt (consolidates 5 divergent timeframe implementations into one).

## Mock Retirement

Not applicable — this is a pure frontend refactor; no backend API endpoints touched.

## Risks & Mitigations

1. **Back-compat break for 3 existing callers** (`sims/detail`, `dashboard/analytics`, `dashboard/analytics-cost`) — Mitigation: Task 1 uses overload to accept both `string` and `TimeframeValue`; callers stay green with zero changes.
2. **Custom popover UX rough without a library** — Mitigation: `datetime-local` is natively supported in all modern browsers; validate `to > from` inline; ship as-is for P2/S. Follow-up enhancement can introduce a proper calendar picker (new FIX).
3. **CDRs adoption risk (largest migration surface)** — Mitigation: isolated to Task 5; keeps existing URL-sync logic; only swaps the UI layer.
4. **Role-gating behavior drift** — Mitigation: `disabledPresets` prop is purely presentational (renders disabled state); the source of truth for access control remains server-side (CDRs API already enforces range limits for analyst role per FIX-215).
5. **Visual inconsistency between adopted pages** — Mitigation: Task 7 explicit visual audit checklist (5 pages × light/dark = 10 screenshots) before closing.

## Pre-Validation Self-Check (Planner Quality Gate)

- [x] Story Effort = S → min 30 lines / 2 tasks; this plan has 7 tasks and ~250 lines. PASS.
- [x] Required sections present: Goal, Architecture Context, Tasks, Acceptance Criteria Mapping. PASS.
- [x] API shape embedded (TypeScript types inline). PASS.
- [x] Design Token Map populated (12 rows). PASS.
- [x] Components-to-reuse table populated. PASS.
- [x] Each task has: files, depends-on, complexity, pattern ref, context refs, verify command. PASS.
- [x] ≥5 tasks (7). ≥2 medium/high (Task 1 medium, Task 2 high, Task 4 medium, Task 5 medium, Task 7 medium). PASS.
- [x] Every AC mapped to at least one task. PASS (AC-4 with explicit N/A callout).
- [x] Explicit scope decisions (7 items). PASS.
- [x] Context refs point to real section headers in this plan. PASS.
- [x] No implementation code blocks (only type signatures + mockup). PASS.
- [x] Back-compat path for existing 3 callers locked. PASS.

**Gate result: PASS.**

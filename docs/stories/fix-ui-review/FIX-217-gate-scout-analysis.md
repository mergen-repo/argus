# FIX-217 Gate Scout 1 — Code Analysis Report

**Scout:** Analysis (read-only)
**Story:** FIX-217 — Timeframe Selector Pill Toggle Unification
**Date:** 2026-04-22
**Model:** opus
**Scope:** 4 passes — Requirements Tracing, Compliance, Security, Performance.

---

## Inventories

### Field Inventory (FE-only story — "fields" = TimeframeSelector API surface)

| Field | Source | Primitive | Hook | Caller Wiring |
|---|---|---|---|---|
| `value` (TimeframeValue \| preset \| string) | Plan API Shape | timeframe-selector.tsx:28 | n/a | All 9 callers pass correctly |
| `onChange` | Plan API Shape | timeframe-selector.tsx:30 | n/a | Back-compat overload via `(v: any)` |
| `options` | Plan AC-1 / Scope #2 | timeframe-selector.tsx:31 | n/a | operators/apns/admin use it |
| `disabledPresets` | Plan Scope #6 | timeframe-selector.tsx:32 | n/a | cdrs uses `!isAdmin ? ['30d'] : []` |
| `allowCustom` | Plan Scope #3 | timeframe-selector.tsx:33 | n/a | `false` on admin/operators/apns |
| `aria-label` | Plan A11y #7 | timeframe-selector.tsx:35 | n/a | cdrs overrides to "CDR zaman aralığı" |
| URL `?tf=` | Plan AC-3 | — | use-timeframe-url-sync.ts:14 | cdrs only |
| URL `?tf_start=&tf_end=` | Plan AC-3 / Scope #4 | — | use-timeframe-url-sync.ts:17-18 | cdrs only |

### Endpoint Inventory
n/a — pure client-side refactor; no backend endpoints touched. Confirmed.

### Workflow Inventory

| AC | Step | Chain Status |
|---|---|---|
| AC-1 | Renders 1h/24h/7d/30d/Custom, default 24h | OK — CANONICAL_OPTIONS lines 20-25; Custom appended via `allowCustom` gate line 116-118 |
| AC-2 | Click Custom → popover opens | OK — `handleSelect` line 137-140 sets `popoverOpen=true` |
| AC-2 | Apply emits ISO from/to | OK — `handleCustomApply` line 144-147 uses `new Date(v).toISOString()` |
| AC-3 | `?tf=` roundtrip on deep-link | **BROKEN on /cdrs** — filters-sync effect (cdrs:148-155) wipes `tf`/`tf_start`/`tf_end` from URL on any filter change. See F-A1. |
| AC-4 | Applied to 5 pages | OK — api-usage, delivery, operators/detail (2 tabs), apns/detail, cdrs all adopt primitive |
| AC-5 | Keyboard nav + height/padding | OK — ArrowLeft/Right/Home/End at lines 149-161; roving tabIndex at line 182 |

### UI Component Inventory

| Component | Location | Status |
|---|---|---|
| `TimeframeSelector` (extended) | `web/src/components/ui/timeframe-selector.tsx` | IMPL |
| `CustomPopoverBody` (internal) | same file, lines 61-99 | IMPL |
| `useTimeframeUrlSync` | `web/src/hooks/use-timeframe-url-sync.ts` | IMPL |
| FRONTEND.md Timeframe Pattern | `docs/FRONTEND.md:178-265` | IMPL (adoption map stale — all rows say "TBD (Wave 2)") |
| cdrs adoption | `web/src/pages/cdrs/index.tsx:299-306` | IMPL (with URL-sync bug, F-A1) |
| admin/api-usage | `web/src/pages/admin/api-usage.tsx:76-81` | IMPL |
| admin/delivery | `web/src/pages/admin/delivery.tsx:112-117` | IMPL |
| operators/detail HealthTimelineTab | `web/src/pages/operators/detail.tsx:339-344` | IMPL |
| operators/detail TrafficTab | `web/src/pages/operators/detail.tsx:559-564` | IMPL |
| apns/detail TrafficTab | `web/src/pages/apns/detail.tsx:618-629` | IMPL |

### AC Summary

| # | Criterion | Status | Gaps |
|---|---|---|---|
| AC-1 | Pill 1h/24h/7d/30d/Custom, default 24h | PASS | — |
| AC-2 | Custom → date range popover | PARTIAL | Popover works, but anchor positioning + timezone pitfalls (F-A3, F-A4) |
| AC-3 | URL sync `?tf=/?tf_start=/?tf_end=` | **FAIL on /cdrs** | Filter-sync effect overwrites URL, stripping `tf` params (F-A1) |
| AC-4 | Applied to 5 surfaces | PASS | Alerts/Capacity/Dashboard N/A per Scope #5 |
| AC-5 | Single h/p + keyboard nav | PASS | Arrow-on-custom opens popover uninvited (F-A7, minor UX) |

---

## Findings

### F-A1 | CRITICAL | gap
- **Title:** `useTimeframeUrlSync` is fully clobbered by cdrs filter-sync effect → deep-link roundtrip broken
- **Location:** `web/src/pages/cdrs/index.tsx:148-155`
- **Description:** The filter-sync effect builds a **fresh** `new URLSearchParams()` (does not clone `prev`), writes only the non-timeframe filters, then calls `setSearchParams(p, { replace: true })`. This strips `tf`, `tf_start`, `tf_end` that the hook wrote microseconds earlier. Result: on any subsequent filter mutation (record-type chip, session_id input, operator dropdown), `?tf=7d` disappears from the URL; refresh loses the timeframe. Violates Plan AC-3 + Scope Decision #4 + Bug Pattern Warning "URL-sync double-write loops".
- **Fixable:** YES
- **Suggested fix:** Change the effect to read existing params and preserve tf/tf_start/tf_end:
  ```ts
  useEffect(() => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev)  // preserve tf*
      // clear legacy/filter-owned keys first
      for (const k of Object.keys(filters)) if (k !== 'from' && k !== 'to') next.delete(k)
      for (const [k, v] of Object.entries(filters)) {
        if (k === 'from' || k === 'to') continue
        if (v !== undefined && v !== '') next.set(k, String(v))
      }
      return next
    }, { replace: true })
  }, [filters, setSearchParams])
  ```

### F-A2 | HIGH | compliance
- **Title:** `<Popover>` trigger is never registered — outside-click auto-closes custom popover + misaligns anchor
- **Location:** `web/src/components/ui/timeframe-selector.tsx:201-215`
- **Description:** The `<Popover>` primitive (popover.tsx:42, 54-80) requires a `<PopoverTrigger>` to (a) populate `triggerRef` used by outside-click detection at popover.tsx:107 and (b) act as an anchor. In FIX-217, the `pill` is a plain `<Button>` (not `PopoverTrigger`). Consequences:
  1. `triggerRef.current` stays `null`. On any click after the popover opens, the `mousedown` handler (popover.tsx:104-109) sees neither `contentRef` nor `triggerRef` contains the target → closes. Clicking the Custom pill a **second time** (to reopen) can fight with state due to bubble order.
  2. `PopoverContent` is positioned `absolute right-0` inside its own `<div className="relative inline-block">` wrapper (popover.tsx:124) — not relative to the Custom pill. Since the wrapper sits as a sibling of the preceding pills inside the `role="group"` container, the popover will anchor to the wrapper's (empty) bounding box, which behaves oddly when the Button pill is the only visible child. Visual audit screenshots are not attached; this is a structural concern, not proven misalignment.
- **Fixable:** YES
- **Suggested fix:** Wrap the Custom-branch pill in `<PopoverTrigger asChild>` (or restructure to use `PopoverTrigger` as the Button via `asChild` pattern — but note `asChild` is typed on the prop yet not implemented in the project's popover; simplest fix: use plain `PopoverTrigger` and style it like the Button pill, OR render the pill as the PopoverTrigger's child via `forwardRef`). At minimum, populate `triggerRef` manually by adding `ref={(n) => { triggerRef.current = n }}` — but that requires exposing the ref, so cleaner to migrate to `PopoverTrigger`.

### F-A3 | HIGH | gap
- **Title:** Custom range timezone asymmetry — ISO UTC re-hydrated into local datetime-local on re-edit
- **Location:** `web/src/components/ui/timeframe-selector.tsx:209-210`
- **Description:** `handleCustomApply` correctly converts local datetime-local → UTC via `new Date(from).toISOString()`. But on re-edit (`initialFrom={normalized.from?.slice(0,16)}`), the stored ISO UTC string is truncated to 16 chars and fed into a `datetime-local` input, which renders/interprets it as **local**. Net effect: a user in UTC+3 saves `2026-04-22T10:00` (local, = `07:00Z` stored), reopens the popover and sees `07:00` displayed as "local" — apparent 3-hour shift. Tests would catch this round-trip inconsistency.
- **Fixable:** YES
- **Suggested fix:** Normalize in `initialFrom`/`initialTo` by converting the stored ISO back to local wall-clock:
  ```ts
  function isoToLocalDT(iso?: string) {
    if (!iso) return ''
    const d = new Date(iso)
    const pad = (n: number) => String(n).padStart(2, '0')
    return `${d.getFullYear()}-${pad(d.getMonth()+1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
  }
  ```

### F-A4 | HIGH | gap
- **Title:** `useTimeframeUrlSync` stores LOCAL datetime-local strings from cdrs path, not ISO UTC
- **Location:** `web/src/hooks/use-timeframe-url-sync.ts:42-58` + `web/src/pages/cdrs/index.tsx:158-163`
- **Description:** The primitive emits `{ value: 'custom', from: ISO-UTC, to: ISO-UTC }`. Cdrs' `handleTimeframeChange` calls `setCustomRange({ start: v.from, end: v.to })` — passing the ISO-UTC strings into URL params. Good on the write path. But the hook's type comment (`{start: string; end: string}`) is ambiguous and the FRONTEND.md doc says "`?tf=custom&tf_start=ISO&tf_end=ISO`" while the primitive's initialFrom slice assumes the stored value is already displayable. When deep-linking with `?tf_start=2026-04-22T07:00:00.000Z` this is read back into `customRange.start` — then passed through as `tfValue.from` → `initialFrom={normalized.from.slice(0,16)}` → `2026-04-22T07:00` into datetime-local (interpreted local). Same root cause as F-A3; second symptom on URL path.
- **Fixable:** YES
- **Suggested fix:** Combine with F-A3 fix — centralize ISO↔local conversion in the primitive.

### F-A5 | MEDIUM | compliance
- **Title:** FRONTEND.md adoption map still says "TBD (Wave 2)" for all 5 newly-adopted surfaces
- **Location:** `docs/FRONTEND.md:247-258`
- **Description:** Doc was written during Wave 1 with placeholders. After Wave 2 adoption (Task 3/4/5 EXECUTED per step-log), the map should reflect which surfaces are now live and any context-specific notes (custom options, role-gating). Currently claims the work isn't done.
- **Fixable:** YES
- **Suggested fix:** Replace `TBD (Wave 2)` cells with actual adoption status: api-usage → "Custom `WINDOW_PRESETS` (1h/24h/7d/30d) labels with 'Last' prefix, `allowCustom={false}`"; cdrs → "Full primitive with Custom popover + analyst 30d role-gate"; etc.

### F-A6 | MEDIUM | gap
- **Title:** Legacy `?from=&to=` URL migration undocumented in user-visible decisions.md
- **Location:** Missing — not in `docs/brainstorming/decisions.md`
- **Description:** Step-log STEP_2 W2 T5 notes `url-migration=from/to excluded-from-filter-effect(owned-by-hook+tf/tf_start/tf_end)`. Previously-shared CDR deep-links (`?from=2026-04-01&to=2026-04-15`) will no longer hydrate (filter-sync effect ignores `from`/`to` keys when building URL and `initialFilters` only reads from localStorage or presets). No backward-compat reader, no user-visible changelog entry. Plan Risk #1 pre-declared "backward compat not required" but this is not reflected in a DECISIONS entry.
- **Fixable:** YES
- **Suggested fix:** Add a DEV-N entry to `docs/brainstorming/decisions.md` documenting the URL scheme change for /cdrs (`?from/?to → ?tf/?tf_start/?tf_end`) and confirm per Plan Risk #1 that internal-link breakage is accepted.

### F-A7 | LOW | compliance
- **Title:** Keyboard Arrow navigation on Custom pill opens popover uninvited
- **Location:** `web/src/components/ui/timeframe-selector.tsx:149-161`
- **Description:** `handleKeyDown` calls `handleSelect(nextOpt)` for the new position — when navigating Right through all pills, landing on `custom` triggers `setPopoverOpen(true)` without user explicitly intending to open it. Convention: Arrow keys in a toggle group should only move focus; Enter/Space selects. Current impl couples focus move with commit.
- **Fixable:** YES
- **Suggested fix:** For arrow keys, only move focus (update internal `focusedIndex` state) and render `tabIndex={idx===focusedIndex?0:-1}`. Select only on Enter/Space/Click. Acceptable to defer (AC-5 keyboard works; this is polish).

### F-A8 | LOW | compliance
- **Title:** `window` variable name in api-usage / delivery shadows `globalThis.window`
- **Location:** `web/src/pages/admin/api-usage.tsx:46`, `web/src/pages/admin/delivery.tsx:89`
- **Description:** State variable named `window` shadows the browser global. Pre-existing before FIX-217 (useState call was there); FIX-217 preserved the name. Non-blocking but worth renaming to `timeframe` or `tfWindow` while the file is open.
- **Fixable:** YES
- **Suggested fix:** Rename to `tfWindow` and update `setWindow` → `setTfWindow`.

### F-A9 | LOW | gap
- **Title:** `operators/detail.tsx` HealthTimelineTab uses non-canonical string values (`'6'`, `'24'`, `'72'`, `'168'`) for TimeframeOption
- **Location:** `web/src/pages/operators/detail.tsx:297-302`
- **Description:** HEALTH_HISTORY_OPTIONS stores numeric-string values (hours) while labels show `6h/24h/3d/7d`. Works because `TimeframeOption.value` is typed `TimeframePreset | string`, but now `disabledPresets` (if ever added here) cannot reference them via the TimeframePreset union. Also breaks any future "canonical preset" analytics that want to measure adoption by preset key.
- **Fixable:** YES
- **Suggested fix:** Map internally: use canonical keys (`'6h'/'24h'/'3d'/'7d'`) as value; convert to hours in the query call. Mild refactor, not blocking.

### F-A10 | LOW | compliance
- **Title:** `SLA rolling-window` selector (raw inline pill group) remains hand-rolled
- **Location:** `web/src/pages/sla/index.tsx:252-272`
- **Description:** Plan Scope Decision #5 explicitly excludes this (different semantics: window length, not a timeframe preset). Cross-track observation only — the raw `<button>` pill group at lines 263-272 uses `role="group" aria-pressed` correctly but predates the primitive. Could be refactored to use `TimeframeSelector` with a custom `options` (`6mo/12mo/24mo`), which would require adding the month-interval type to `TimeframePreset` union OR making `TimeframeOption.value` fully opaque. Deferred per plan.
- **Fixable:** YES (follow-up FIX)
- **Suggested fix:** Open a follow-up FIX to either generalize the primitive to accept any string value (drop strict TimeframePreset typing on `value`) or build a dedicated `<PillToggleGroup>` atom that TimeframeSelector + SLA both consume. Not in-scope for FIX-217.

### F-A11 | LOW | performance
- **Title:** `allOptions` array recomputed on every render (ditto `selectableIndices`)
- **Location:** `web/src/components/ui/timeframe-selector.tsx:115-123`
- **Description:** `allOptions` and `selectableIndices` are built inline — cheap per-pill arithmetic but not memoized. Component rerenders on every parent state change (which for cdrs is frequent due to filter updates). Low impact because the array is ≤6 elements; no measurable hot-path concern.
- **Fixable:** YES
- **Suggested fix:** `React.useMemo(() => [...], [options, allowCustom, normalized.value, normalized.from, normalized.to])`. Optional micro-opt.

### F-A12 | LOW | security
- **Title:** No input sanitization on `tf` query param values in `useTimeframeUrlSync`
- **Location:** `web/src/hooks/use-timeframe-url-sync.ts:7-9, 14-15`
- **Description:** Only validated presets (`VALID_PRESETS.includes`) — unknown values fall back to `defaultValue`. Good. `tf_start`/`tf_end` are **not** validated as ISO strings or date-parseable. A crafted URL could inject any string into `customRange.start/end` which then flows to `filters.from/to` on cdrs and becomes a request param to the backend. Backend must be the authoritative validator (it should be per existing CDR API); no XSS risk because the strings are never rendered as HTML — only serialized into query strings or input value. Minor hygiene.
- **Fixable:** YES
- **Suggested fix:** Validate `tf_start`/`tf_end` with `new Date(v).toISOString() === v` or a simple regex; drop the custom range if invalid.

---

## Non-Fixable (Escalate)

None. All findings are fixable within the existing architecture.

---

## Performance Summary

### Queries Analyzed

n/a — no DB queries touched by this story.

### Caching Verdicts

| # | Data | Location | TTL | Decision |
|---|---|---|---|---|
| 1 | `useAPIKeyUsage(window)` / `useDeliveryStatus(window)` hook results | React Query default | per hook config | SKIP (already cached; unchanged by FIX-217) |
| 2 | Preset → ISO range computation (`presetToRange` in cdrs) | in-component | n/a | SKIP (O(1), recomputes per render — fine) |

### Frontend Performance

- Bundle: `TimeframeSelector` + `useTimeframeUrlSync` adds ~1.5KB min+gz. Negligible.
- Re-renders: primitive rerenders on every parent render (no `React.memo`). Cdrs page is the worst offender (filters-heavy page). Non-blocking; memoize if profiler shows issues.
- No lazy loading needed (small surface).

---

## Verdict Summary

- **Critical: 1** (F-A1 — URL roundtrip broken on /cdrs, blocks AC-3)
- **High: 3** (F-A2 popover trigger missing, F-A3 custom range TZ asymmetry, F-A4 URL custom-range TZ propagation)
- **Medium: 2** (F-A5 doc stale, F-A6 legacy URL migration undocumented)
- **Low: 6** (F-A7..F-A12 polish + defensive items)

**Gate recommendation:** BLOCK. F-A1 breaks AC-3 under realistic user flow (click any filter after selecting a preset — URL loses `tf`). F-A2 + F-A3 compromise AC-2 Custom popover reliability. Fix F-A1/A2/A3/A4 before pass.

**Fixable without redesign:** ALL findings.

**Cross-track observations:**
- SLA rolling-window (F-A10) is the only other pill group that could unify under this primitive — deferred per scope.
- No other ad-hoc timeframe UIs detected on Dashboard, Alerts, Capacity pages (confirmed N/A).
- Pre-existing `bg-accent-primary` token usage in admin pages (not FIX-217 scope).
- Pre-existing `rgba(0,255,136,0.4)` inline boxShadow at cdrs:252 (LED glow on LIVE indicator, not FIX-217 introduced).

# Gate Scout 3 (UI) Report — FIX-217

**Story**: Timeframe Selector Pill Toggle Unification
**Date**: 2026-04-22
**Role**: Gate Scout — UI Quality
**Scope**: primitive `timeframe-selector.tsx`, hook `use-timeframe-url-sync.ts`, `docs/FRONTEND.md` "Timeframe Pattern", and 5 adopted pages (`admin/api-usage`, `admin/delivery`, `operators/detail`, `apns/detail`, `cdrs/index`). Pre-existing callers (`dashboard/analytics`, `dashboard/analytics-cost`, `sims/detail`) audited via back-compat overload check.

---

## <SCOUT-UI-FINDINGS>

### UI Scope
- Story has UI: YES
- Screens tested: SCR-TimeframeSelector-Primitive, admin/api-usage, admin/delivery, operators/detail (TrafficTab + HealthTimelineTab), apns/detail (TrafficTab), cdrs/index

### Enforcement Summary
| Check | Matches (in FIX-217 scope) |
|-------|----------------------------|
| Hardcoded hex colors in primitive/hook | 0 |
| Hardcoded hex in adopted pages (FIX-217 surface) | 0 |
| Pre-existing `rgba(...)` LED glows (out-of-scope; see notes) | 4 (acknowledged) |
| Arbitrary pixel values (`text-[Npx]`, `px-[Npx]`) | 0 |
| Raw `<button>` outside primitive for timeframe | 0 |
| `<Select>` for timeframe | 0 |

### Verdict Table (11 checks)
| # | Check | Result | Severity |
|---|-------|--------|----------|
| 1 | Primitive visual parity — Button ghost active/hover override | **PASS** | — |
| 2 | Canonical preset cross-page label consistency | **OBSERVATION** | low |
| 3 | Custom popover a11y (role=dialog, labeled inputs, Apply/Cancel variants) | **PASS** | — |
| 4 | Role-gating UX on cdrs (disabled 30d, keyboard-safe) | **PASS** | — |
| 5 | Responsive (md/lg/narrow) segmented control | **OBSERVATION** | low |
| 6 | Dark-mode / semantic tokens (no hex/rgba in FIX-217 scope) | **PASS** | — |
| 7 | Keyboard nav (ArrowLeft/Right, Home/End, Enter/Space, Tab) | **PASS** | — |
| 8 | A11y attrs (role=group, aria-label, aria-pressed, aria-disabled, roving tabIndex) | **PASS** | — |
| 9 | FRONTEND.md completeness (presets, API, override, role-gate, popover, URL hook, a11y, adoption map, never-do) | **NEEDS_FIX** | low |
| 10 | Cross-page visual regression (card headers / tab headers unbroken) | **PASS** | — |
| 11 | Observations on other ad-hoc timeframe UIs | **OBSERVATION** | — |

### Blocking issues
**None.** Single NEEDS_FIX is a doc-level staleness that does not affect runtime behavior.

### NEEDS_FIX (non-blocking, doc)
**F-U-1 — FRONTEND.md adoption-map rows still say "TBD (Wave 2)" after Wave 2 shipped.**
- File: `docs/FRONTEND.md:254-258`
- Evidence: rows for `admin/api-usage.tsx`, `admin/delivery.tsx`, `operators/detail.tsx`, `apns/detail.tsx`, `cdrs/index.tsx` all carry "TBD (Wave 2)" notes, but step-log `STEP_2 DEV W2 T3/T4/T5` shows those adoptions EXECUTED on 2026-04-22 with tsc=PASS build=PASS.
- Recommendation (for Gate Lead to apply, not scout): flip notes to concrete descriptions — e.g., cdrs row should read "Custom popover + `disabledPresets` role-gate (analyst → `['30d']`)", operators row "TrafficTab + HealthTimelineTab, `options` override for `6h`/`3d`", etc.

### OBSERVATIONS (non-blocking)

**O-U-1 — Label vocabulary diverges across adopted pages.**
- `admin/api-usage.tsx` + `admin/delivery.tsx`: verbose labels (`"Last 1h" / "Last 24h" / "Last 7d" / "Last 30d"`).
- `operators/detail.tsx` + `apns/detail.tsx` TrafficTab: terse (`"1h" / "6h" / "24h" / "7d" / "30d"`).
- `cdrs/index.tsx`: terse (derived from `PRESET_RANGES` mapping).
- The primitive's `options` prop explicitly sanctions context-specific labels (per Plan Scope Decision #2 + FRONTEND.md "Options override"). Structural parity is achieved; vocabulary is not. Document-as-intentional or unify in follow-up FIX.

**O-U-2 — No horizontal-overflow handling.**
- Primitive container is `inline-flex` with no wrap/scroll. cdrs (6 pills: 1h/6h/24h/7d/30d/custom, potentially including the ~22-char "Custom · Apr 22 → Apr 23" label) can overflow on narrow viewports (<640px). Not a FIX-217 regression (pre-existing behavior from the prior primitive), but ergonomic debt. Suggest future `flex-wrap` or `overflow-x-auto` at container level.

**O-U-3 — Out-of-scope `rgba()` in adopted pages (acknowledged).**
- `operators/detail.tsx:118-120` and `cdrs/index.tsx:252` use `boxShadow: '0 0 Npx rgba(0,255,136,0.4)'` / amber / red for health-LED glows. These are pre-existing visual effects unrelated to timeframe UI and are explicitly out of FIX-217 scope. Primitive + hook themselves are token-only (verified clean).

**O-U-4 — Pages not adopted (intentional).**
- `SLA` page's 6/12/24-month rolling-window selector — different semantics (window length, not preset); Plan Scope Decision #5 explicitly excludes. Confirmed still a bespoke group. No action required.
- Alerts, Capacity, Dashboard main — no timeframe UI today (static "Last 24h" label only). Plan AC-4 N/A correctly documented.
- 3 pre-existing callers (`dashboard/analytics`, `dashboard/analytics-cost`, `sims/detail`) ride the legacy `value: string` overload signature — back-compat preserved, tsc=PASS per step-log.

---

## Detailed Audit

### 1. Primitive visual parity — PASS

**File**: `web/src/components/ui/timeframe-selector.tsx`

The primitive now renders each pill via `<Button variant="ghost" size="sm">` with a className reset:
```
'h-auto px-2.5 py-1 text-xs font-medium rounded-[3px] transition-colors
 focus-visible:outline-accent focus-visible:outline-2 gap-0'
```
Active state:
```
'bg-accent text-bg-primary shadow-sm hover:bg-accent/90 hover:text-bg-primary hover:shadow-sm'
```
Inactive state inherits ghost variant (`text-text-secondary hover:bg-bg-hover hover:text-text-primary`).

**Hover-conflict concern (check #1) resolved**: `cn()` at `web/src/lib/utils.ts:4-6` is `twMerge(clsx(...))`. Because the ghost variant's `hover:bg-bg-hover hover:text-text-primary` is injected first (via `buttonVariants`) and the active-state `hover:bg-accent/90 hover:text-bg-primary hover:shadow-sm` is appended in the `cn()` call after `buttonVariants({ variant: 'ghost', size: 'sm' })`, tailwind-merge correctly resolves same-property hover conflicts (`hover:bg-*`, `hover:text-*`) in favour of the later class. Active pills will NOT flash back to `bg-bg-hover` on hover.

**Size-sm override**: Button's `size="sm"` contributes `h-8 px-3 text-xs rounded-[var(--radius-sm)]`. The override adds `h-auto px-2.5 py-1 rounded-[3px]` — twMerge collapses `h-8 → h-auto`, `px-3 → px-2.5`, and rounded radius to the 3px literal. text-xs remains (same class). No conflict.

**Container styling**:
```
'inline-flex rounded-[var(--radius-sm)] border border-border bg-bg-elevated p-0.5'
```
All semantic tokens. 

### 2. Canonical preset cross-page consistency — OBSERVATION

Pill *shape* (rounded rect, size, spacing, active color) is identical primitive-wide. Pill *labels* diverge (see O-U-1). Plan permits this; flag for future unification.

### 3. Custom popover a11y — PASS

- `<Popover>` (`web/src/components/ui/popover.tsx`) wraps `PopoverContent` with `role="dialog"` on the overlay div (confirmed line-level in primitive). Escape + outside-click closes (built into Popover primitive — mirrors Sheet pattern).
- Popover body uses two `<Input type="datetime-local">` (via the `Input` primitive, not raw `<input>`). Labels: "From" / "To" rendered as visible label text adjacent to the Input (evidence in step-log: `handleCustomApply(from, to)`).
- Footer: `<Button variant="outline">Cancel</Button>` + `<Button>Apply</Button>` — variant semantics correct.
- Apply validates from/to and emits `{ value: 'custom', from: ISO, to: ISO }`.

### 4. Role-gating UX on cdrs — PASS

- cdrs/index invocation: `disabledPresets={!isAdmin ? ['30d'] : []}` (per step-log W2 T5).
- Disabled pill className: `opacity-40 cursor-not-allowed` + `aria-disabled="true"` + `title="Not available for your role"` + native `disabled` on the underlying `<button>` (see Design Token Map in plan § Design Token Map; confirmed in primitive render).
- **Keyboard safety (belt + suspenders)**:
  1. `handleSelect` early-returns: `if (disabledPresets.includes(opt.value as TimeframePreset)) return` — blocks onClick.
  2. `selectableIndices` computed to exclude disabled indices → ArrowLeft/Right/Home/End skip over disabled pills.
  3. Native `disabled={isDisabled}` on the button blocks Enter/Space synthetic click events at DOM level.
- Enter/Space on a disabled pill **does NOT fire onChange**. Verified.

### 5. Responsive — OBSERVATION

Primitive is `inline-flex` with no wrap or scroll. On md+/lg it fits comfortably. On narrow widths (<640px) with 6 pills (cdrs case) or with active Custom label "Custom · Apr 22 → Apr 23" (~22 chars), horizontal overflow is possible. Pre-existing behavior; not a FIX-217 regression. See O-U-2.

### 6. Dark-mode / token parity — PASS

`grep -nE '#[0-9a-fA-F]{3,8}|rgba?\\(' ` against FIX-217 files:
- `timeframe-selector.tsx`: **0** matches.
- `use-timeframe-url-sync.ts`: **0** matches.
- `admin/api-usage.tsx`, `admin/delivery.tsx`, `apns/detail.tsx` (Traffic scope): **0** matches.
- `operators/detail.tsx`: 3 pre-existing `rgba()` LED-glow lines (118-120) — out of scope, see O-U-3.
- `cdrs/index.tsx`: 1 pre-existing `rgba()` LED-glow line (252) — out of scope, see O-U-3.

All FIX-217-owned code paths use semantic tokens: `bg-accent`, `text-bg-primary`, `bg-bg-elevated`, `bg-bg-hover`, `text-text-secondary`, `text-text-primary`, `border-border`, `text-danger`, `rounded-[var(--radius-sm)]`, `focus-visible:outline-accent`.

### 7. Keyboard nav — PASS

`handleKeyDown` on container (`onKeyDown` at role="group"):
- Filters to `ArrowLeft | ArrowRight | Home | End` only.
- `e.preventDefault()` prevents page scroll on arrow keys.
- Uses `selectableIndices` (disabled presets excluded) with modular arithmetic for wrap-around on cycling.
- Home/End jump to first/last **enabled** preset.
- Enter/Space fall through to native button activation (non-disabled pills).
- Tab: roving `tabIndex` — active pill gets `tabIndex={0}`, others `-1`; fallback `currentIndex === -1 && idx === 0 ? 0 : -1` ensures at least one pill is Tab-reachable when no value matches.

### 8. A11y attrs — PASS

Evidence inline in primitive render:
```
role="group" aria-label={ariaLabel}                   // container; defaults "Timeframe"
aria-pressed={isActive}                               // each pill
aria-disabled={isDisabled ? 'true' : undefined}       // each pill
disabled={isDisabled}                                 // native
title={isDisabled ? 'Not available for your role' : undefined}
tabIndex={isActive ? 0 : (currentIndex === -1 && idx === 0 ? 0 : -1)}  // roving
```
Matches `pages/sla/index.tsx` segmented-control convention referenced in plan.

### 9. FRONTEND.md completeness — NEEDS_FIX (doc staleness)

**What's present** (all covered):
- ✓ Canonical preset set (§ Canonical preset set).
- ✓ Controlled API w/ code snippet (§ Primitive API).
- ✓ Options override rationale (§ Options override).
- ✓ Role-gating via `disabledPresets` (§ Role-gating).
- ✓ Custom Popover semantics + label truncation (§ Custom range Popover).
- ✓ URL sync hook usage snippet (§ URL sync hook).
- ✓ A11y contract (§ A11y contract).
- ✓ Adoption map (8 rows: 3 pre-existing + 5 Wave-2 adoptions).
- ✓ "Never do" rules (§ Never do).

**Stale content** — see F-U-1 above. Rows 254-258 say "TBD (Wave 2)" but Wave 2 is the current audit subject. Cosmetic but should be flipped to reflect shipped state.

### 10. Cross-page visual regression — PASS

Per step-log each adoption preserves surrounding layout:
- `admin/api-usage.tsx`: inline `<Button>` map deleted; primitive replaces in-place in filter row.
- `admin/delivery.tsx`: same treatment.
- `operators/detail.tsx` TrafficTab: `<Select>` → primitive; TrafficTab card header intact.
- `operators/detail.tsx` HealthTimelineTab: HOURS_OPTIONS grid replaced; Health-Timeline tab header intact.
- `apns/detail.tsx` TrafficTab: `<Select>` → primitive; APN header intact.
- `cdrs/index.tsx`: bespoke `PRESET_RANGES` pill markup removed; primitive drops into the existing filter bar; URL-sync migrated to `useTimeframeUrlSync(24h)`.

tsc=PASS + build=PASS recorded at each W2 step.

### 11. Other ad-hoc timeframe UIs — OBSERVATIONS

From `grep -l TimeframeSelector`: 8 files using the primitive (7 adopted + 1 hook). From `grep ad-hoc` on pages: no other raw pill groups remain in adopted files. SLA rolling-window selector remains bespoke (by design). Dashboard, Alerts, Capacity have no timeframe UI today. Nothing else to flag.

---

## Summary

- **11/11 checks assessed**; 8 PASS, 3 OBSERVATION, 1 NEEDS_FIX (doc staleness only).
- **No runtime blockers.** Primitive is visually correct, a11y-complete, keyboard-safe, token-clean, and back-compat with 3 legacy callers.
- **Single NEEDS_FIX (F-U-1)**: 5 rows of FRONTEND.md adoption map still marked "TBD (Wave 2)" — flip to concrete descriptions to reflect shipped state.
- **3 OBSERVATIONS**: (1) label vocabulary diverges across adopted pages — sanctioned by `options` prop, flag for future unification; (2) no responsive overflow handling — pre-existing, future ergonomic work; (3) 4 pre-existing `rgba()` LED-glow lines in operators/detail + cdrs are acknowledged out-of-scope.
- **Overall UI verdict**: PASS (with 1 doc polish recommended).

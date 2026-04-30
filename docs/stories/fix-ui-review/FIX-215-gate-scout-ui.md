# FIX-215 Gate Scout — UI Audit Report

Story: FIX-215 (SLA Historical Reports + PDF Export + Drill-down)
Date: 2026-04-22
Scope: Static audit of 6 UI surfaces; no dev server / browser execution.

---

## Files Audited

| Path | Kind | LoC |
|---|---|---|
| `web/src/pages/sla/index.tsx` | page (rewrite) | 405 |
| `web/src/pages/sla/month-detail.tsx` | organism (SlidePanel) | 253 |
| `web/src/pages/sla/operator-breach.tsx` | organism (nested SlidePanel) | 139 |
| `web/src/pages/operators/detail.tsx` | page (SLATargetsSection extension) | +94 lines at 1427–1520 |
| `web/src/hooks/use-sla.ts` | hooks | 82 |
| `web/src/types/sla.ts` | types | 34 |
| `web/src/types/operator.ts` | types (additions) | 90 |

---

## Automated Enforcement Summary

| Check | Matches | Severity | Notes |
|---|---|---|---|
| Hardcoded hex colors | 0 | — | clean |
| Hardcoded rgba() in arbitrary shadow | 3 | MAJOR | `sla/index.tsx:65-67` — three `hover:shadow-[0_0_16px_rgba(…)]` inline values |
| Arbitrary pixel values (`text-[Npx]`, `min-w-[80px]`, `tracking-[1.5px]`) | 13 | MINOR | widespread across sla/*.tsx; same pattern as pre-existing codebase (operators/detail.tsx shows hundreds) — consistency not a regression |
| Raw HTML elements (`<input/>/<button/>`) | 1 | MINOR | `sla/index.tsx:279` rolling-window segmented control uses raw `<button>` inside a custom group — intentional pattern (segmented control); acceptable but should be verified against existing patterns |
| Competing UI library imports | 0 | — | clean |
| Default Tailwind colors (`bg-gray-*`, `text-gray-*`, `bg-slate-*`, `bg-white`) | 0 | — | clean |
| Inline SVG outside atoms | 0 | — | all icons via `lucide-react` |
| `shadow-none` on cards | 0 | — | clean |
| Undefined design tokens (`var(--radius-card)`, `bg-bg-card`) | 2 | CRITICAL | `operators/detail.tsx:1471` — neither token exists in index.css or FRONTEND.md |
| `any` usage in hooks/types | 0 | — | clean type safety |

---

## Per-File Findings

### F-U1 · CRITICAL · Undefined design tokens in SLATargetsSection
- Location: `web/src/pages/operators/detail.tsx:1471`
- Description: Container uses `rounded-[var(--radius-card)]` and `bg-bg-card`. Neither `--radius-card` nor `--bg-card` exist in the design token system (FRONTEND.md lists `--radius-sm/md/lg/xl` and `--bg-primary/--bg-surface/--bg-elevated/--bg-hover`). At runtime this falls back to `0 0` border-radius and transparent background, breaking visual parity with every other card in the project.
- Fixable: YES
- Suggested fix: Replace with `rounded-[var(--radius-md)] border-border bg-bg-surface` to match all sibling sections in detail.tsx (pattern at `operators/detail.tsx:178`, `web/src/pages/sla/index.tsx:95`).

### F-U2 · MAJOR · Nested SlidePanel wrapper pattern is fragile
- Location: `web/src/pages/sla/operator-breach.tsx:56-58, 135-136`
- Description: `SLAOperatorBreachPanel` wraps its `<SlidePanel>` in `<div className="fixed inset-0 z-[60] pointer-events-none"><div className="pointer-events-auto">…</div></div>`. `SlidePanel` already mounts a full-screen fixed overlay at `z-50` and a click-outside backdrop. Wrapping a second fixed+z-60 container (a) double-layers the overlay (mouse captures still leak through because the outer backdrop of the nested panel lives at `z-50` — lower than the wrapper's `z-60`), (b) the outer `pointer-events-none` cancels the SlidePanel's own backdrop click-to-close in the child, (c) arbitrary `z-[60]` hex-adjacent token is not in the z-index scale.
- Fixable: YES
- Suggested fix: Remove the wrapper entirely. Let the nested SlidePanel render its own overlay; stacking order of two sequentially mounted SlidePanels is correct by DOM order. If nested z-index is truly required, introduce a token in FRONTEND.md and apply inside the SlidePanel component's `side=right` branch.

### F-U3 · MAJOR · Rolling-window segmented control is a raw `<button>` cluster (no shadcn primitive)
- Location: `web/src/pages/sla/index.tsx:277-294`
- Description: The 6mo/12mo/24mo toggle uses three raw `<button>` elements inside a div. No shadcn Tabs / ToggleGroup / Segmented atom. Missing `role="group"` on container; `aria-pressed` is set but there's no group label (`aria-label` / `aria-labelledby`).
- Fixable: YES
- Suggested fix: Wrap in a `div` with `role="group" aria-label="Rolling window selection"`, OR migrate to `components/ui/tabs.tsx` if it exists. Keep the visual styling.

### F-U4 · MAJOR · Hardcoded rgba glow shadows bypass design-token system
- Location: `web/src/pages/sla/index.tsx:65-67` (`statusGlow`)
- Description: Three arbitrary Tailwind classes `hover:shadow-[0_0_16px_rgba(0,255,136,0.12)]`, `rgba(255,184,0,0.12)`, `rgba(255,68,102,0.12)` duplicate success/warning/danger colors as raw rgba literals. FRONTEND.md defines `--shadow-glow-success` (and has precedent for variant glows). A future token rename drifts these.
- Fixable: YES
- Suggested fix: Define `--shadow-glow-warning` / `--shadow-glow-danger` alongside existing `--shadow-glow-success`; use `hover:shadow-[var(--shadow-glow-success)]`.

### F-U5 · MAJOR · Missing `meta.affected_sessions_est` UX
- Location: `web/src/pages/sla/operator-breach.tsx:99-130`
- Description: Plan API spec (§API Specs · breaches) defines `breaches[i].affected_sessions_est` AND `totals.breaches_count` / `totals.downtime_seconds` / `totals.affected_sessions_est`. The current response type (`SLABreachesResponse` in `types/sla.ts:30-33`) omits `totals`, and `operator-breach.tsx` renders neither `affected_sessions_est` per row (screen mockup line 377: "87 sessions") nor the totals header (mockup line 374: "Totals: 3 breaches · 30m 40s downtime · ~412 sessions"). Visible mockup deviation vs what ships.
- Fixable: YES
- Suggested fix: Extend `SLABreachesResponse.data[]` with `affected_sessions_est: number | null`, add `totals` object; render both per mockup.

### F-U6 · MAJOR · SlidePanel has no dialog aria semantics (existing component debt surfaced by FIX-215)
- Location: `web/src/components/ui/slide-panel.tsx:40-73` (used by FIX-215 month-detail + operator-breach)
- Description: SlidePanel root `<div className="fixed inset-0 z-50">` has no `role="dialog"` / `aria-modal="true"` / `aria-labelledby` linking to the title. Escape-to-close works; focus trap is absent — tabbing leaks back into the SLA page behind the modal. Affects both FIX-215 drawers.
- Fixable: YES (fix in shared component, not in FIX-215 files)
- Suggested fix: Add `role="dialog" aria-modal="true" aria-labelledby={titleId}`; auto-focus the close button on mount; consider @radix-ui/react-dialog (already a shadcn default) or a small focus-trap.

### F-U7 · MAJOR · PDF download trigger uses `<a download>` but auth token attachment unverified
- Location: `web/src/pages/sla/index.tsx:111-120`, `web/src/pages/sla/month-detail.tsx:112-120`
- Description: PDF uses `<a href="/api/v1/sla/pdf?…" download>`. If the backend requires `Authorization: Bearer` header (standard for this project — `api.ts` uses axios interceptor), raw `<a>` will not attach the token and downloads will 401. FIX-214 CDR export pattern is referenced in plan (Task 8) — verify whether FIX-214 uses cookie auth or a blob-fetch pattern. No loading / error state on the PDF link; spec AC-4 calls out "button states (idle/loading/error)".
- Fixable: YES
- Suggested fix: If bearer auth: swap to `fetch(url, {headers}).then(r => r.blob()).then(blob => saveAs(blob, filename))` (file-saver library) or inline `URL.createObjectURL` download. Add `isPending` state + disabled styling + toast on error. Check cookie vs bearer posture first to avoid redundant change.

### F-U8 · MAJOR · No empty-state on MonthDetail for missing month (AC not covered)
- Location: `web/src/pages/sla/month-detail.tsx:177-237`
- Description: When a month has no SLA reports (e.g. selecting Jan 2020 where no data exists), the API can return 404 with `error.code = sla_month_not_available` (plan §Task 5). `useSLAMonthDetail` throws on error (TanStack default) → `isError` path fires → generic "Failed to load month detail" error box. The distinct "not available" case is indistinguishable from transient failure. Screen quality standard: should show an `EmptyState` ("No SLA data for this month") not an error.
- Fixable: YES
- Suggested fix: Capture the error response in the query; when `error.code === 'sla_month_not_available'`, render `<EmptyState icon={FileBarChart} title="No SLA data" description="…" />` instead of the danger banner.

### F-U9 · MINOR · Year selector is static hardcoded `['2024','2025','2026']`
- Location: `web/src/pages/sla/index.tsx:27-31`
- Description: `YEAR_OPTIONS` is a fixed literal. In 2027 the dropdown will not offer the current year. No 2023 option either (plan allows `year ∈ [2020, now().Year()]`).
- Fixable: YES
- Suggested fix: Compute dynamically: `const currentYear = new Date().getFullYear(); const YEAR_OPTIONS = Array.from({length: 5}, (_,i) => ({ value: String(currentYear - 4 + i), label: String(currentYear - 4 + i) }))`.

### F-U10 · MINOR · `uptimeStatus` helper ignores the `target` parameter (bug)
- Location: `web/src/pages/sla/index.tsx:46-50`
- Description: Function signature accepts `target` but the first branch uses hardcoded `99.9` instead of `target`. An operator with SLA target `99.5%` and uptime `99.7%` incorrectly renders as "compliant" instead of being judged against its own target. Directly contradicts plan Business Rule BR-3 ("`on_track` if `uptime_pct ≥ sla_uptime_target`").
- Fixable: YES
- Suggested fix: Replace `if (uptime >= 99.9) return 'compliant'` with `if (uptime >= target) return 'compliant'`.

### F-U11 · MINOR · `uptimeColorClass` threshold duplication
- Location: `web/src/pages/sla/month-detail.tsx:32-44`
- Description: Second helper duplicates BR-3 threshold math with a different formulation (`delta >= -0.1` for at-risk vs index.tsx `uptime >= target - 0.1 && uptime >= 99.0`). Two screens classify the same row differently.
- Fixable: YES
- Suggested fix: Extract to `web/src/lib/sla.ts` `classifyUptime(uptime, target): UptimeStatus`; reuse in both screens.

### F-U12 · MINOR · Turkish/ASCII copy, date/number format
- Location: Everywhere in FIX-215 surfaces
- Description: All labels are English ("SLA Reports", "Monthly Breakdown", "Breaches", "No breaches recorded"). Format: `formatTs` in operator-breach.tsx uses `en-GB` (DD/MM/YYYY HH:mm) which is OK; numbers use `toLocaleString()` default (thousand separator = environment locale). Project has i18n infrastructure (see FRONTEND.md §Language Toggle). Not blocking, but flagging that FIX-215 ships English-only on a bilingual app.
- Fixable: YES (separate effort)
- Suggested fix: Consistent with FIX-214 (English-first + fallback), no FIX-215 change required; just ensure copy goes through i18n keys later.

### F-U13 · MINOR · Missing breadcrumb in drawers (nav consistency)
- Location: `web/src/pages/sla/month-detail.tsx`, `operator-breach.tsx`
- Description: Plan §9 ("Breadcrumbs/navigation") expects `/sla → /sla/months/:year/:month → operator drill-down` paths consistent. Current implementation uses drawers (no URL update); back navigation is via ESC only. Plan's Screen Mockups explicitly treat these as drawers, so this is acceptable, but URL deep-linking is absent — opening the app at `/sla/months/2026/03` will not reproduce the drawer state.
- Fixable: YES (partial)
- Suggested fix: Either sync drawer open state to URL via `useSearchParams` (`?month=2026-03&operator=uuid`), or document the drawer-only UX in SCREENS.md.

### F-U14 · MINOR · No `role="button"` ARIA on operator row click target
- Location: `web/src/pages/sla/month-detail.tsx:75-134`
- Description: `<TableRow>` acts as click target via nested "View" button (line 122-132) only — good. But the full row is not itself clickable, while the screen mockup (plan line 361) shows `>` at row end implying full-row navigation. Minor UX gap.
- Fixable: YES
- Suggested fix: Either keep button-only (accessible, clear) and remove `>` from mockup, or make row clickable with `role="button" tabIndex={0} onKeyDown` pattern identical to MonthCard.

### F-U15 · MINOR · SLATargetsSection — "Reset defaults" and audit-trail lines missing
- Location: `web/src/pages/operators/detail.tsx:1470-1520`
- Description: Plan Screen Mockup (lines 386-393) calls for:
  - `[Reset defaults] [Save]` button pair — only `[Save]` ships
  - `Last changed: 2 days ago by admin@argus.io` — not rendered
- Fixable: YES
- Suggested fix: Add Reset button (`onClick` resets to spec defaults 99.9 / 500); render last-updated line from operator audit log (or from `operator.updated_at` + `operator.updated_by` if exposed).

### F-U16 · MINOR · SLATargetsSection label-for wiring is minimal
- Location: `web/src/pages/operators/detail.tsx:1479-1493, 1496-1510`
- Description: `<label htmlFor="sla-uptime">` pairs with `<Input id="sla-uptime">` — OK. Missing `aria-describedby` linking to the help text (`"50 – 100%"`) and error text. Screen readers will not announce the range hint.
- Fixable: YES
- Suggested fix: Add `aria-describedby="sla-uptime-help sla-uptime-err"` and IDs on hint/error spans.

### F-U17 · MINOR · KpiChip grid uses `grid-cols-5` without responsive fallback
- Location: `web/src/pages/sla/month-detail.tsx:179-200`
- Description: Fixed `grid-cols-5` — on an `sm` SlidePanel (fits `max-w-4xl` ≈ 896px) this is tight but fine; on a narrower viewport the 5 chips overflow. Spec Check #7 (Responsive).
- Fixable: YES
- Suggested fix: Swap to `grid-cols-2 sm:grid-cols-3 md:grid-cols-5 gap-2`.

### F-U18 · MINOR · Sparkline per-operator row (plan line 347) not implemented
- Location: `web/src/pages/sla/index.tsx` + `month-detail.tsx`
- Description: Plan §Design Token Map lists `Sparkline` as a reusable component, and Screen Mockup line 344-347 explicitly shows a "6M Trend" sparkline column per operator (`▁▃▂▅▂▁`). No Sparkline import in any of the FIX-215 files.
- Fixable: YES
- Suggested fix: Add a 6M Trend column in the operator matrix table; use `components/ui/sparkline.tsx`.

### F-U19 · MINOR · `isDirty` check is numeric equality-with-NaN
- Location: `web/src/pages/operators/detail.tsx:1451`
- Description: If the user clears the uptime input, `Number('')` → `0`, `uptimeNum !== operator.sla_uptime_target` → true, "Save" enables while `validateUptime` flags an error (Save disabled due to `hasErrors`). OK, but `isDirty` flashes the unsaved-changes dot (line 1474) even when the field is invalid, which is misleading. Minor.
- Fixable: YES
- Suggested fix: `const isDirty = !hasErrors && (uptimeNum !== operator.sla_uptime_target || …)`.

### F-U20 · MINOR · `SLAMonthSummary.overall.operator_id` semantics
- Location: `web/src/types/sla.ts:1-20`
- Description: `SLAMonthSummary.overall` is typed as `SLAOperatorMonthAgg`, which requires `operator_id`, `operator_name`, `operator_code`. Plan §API Specs sample response gives `overall` without those fields. Type is over-specified → forces backend to return synthetic placeholder ids for the aggregate, or TypeScript will flag runtime data mismatch (no runtime check, but contracts rot).
- Fixable: YES
- Suggested fix: Split type: `SLAOverallAgg = Omit<SLAOperatorMonthAgg, 'operator_id'|'operator_name'|'operator_code'|'report_id'>`; use `SLAOverallAgg` for `overall`.

---

## Modal Pattern Compliance (Option C per story spec)

| Surface | Expected | Actual | Verdict |
|---|---|---|---|
| Month drill-down | SlidePanel (rich form) | SlidePanel width="xl" | PASS |
| Operator breach drill-down | SlidePanel (nested) | SlidePanel width="lg" with fragile z-wrapper | PASS-with-finding (F-U2) |
| Delete operator confirm | Dialog (compact confirm) | Dialog | PASS |
| SLA target save | Inline toast (sonner) | `toast.success/.error` via sonner | PASS |

---

## Design Token Compliance Table

| Category | FIX-215 Surface | Compliance |
|---|---|---|
| Colors | sla/*.tsx | PASS — all classes via `text-text-*`, `bg-bg-*`, `text-accent`, `text-success/warning/danger/info/purple` |
| Colors | operators/detail.tsx SLATargetsSection | FAIL — `bg-bg-card` + `rounded-[var(--radius-card)]` undefined (F-U1) |
| Radii | sla/*.tsx | PASS — `rounded-[var(--radius-md)]` and `-sm` |
| Shadows | sla/index.tsx status glow | PARTIAL — inline rgba (F-U4) |
| Typography | all | PASS — consistent `text-xs/sm/[10px] font-mono/semibold/bold` hierarchy |
| Spacing | all | PASS — `gap-3/4/6`, `p-6`, `px-4 py-3` aligned to 4-px grid |

---

## A11y Check Results

| Check | Result | Where |
|---|---|---|
| Drawer `role="dialog" aria-modal` | FAIL (shared component) | SlidePanel (F-U6) |
| Drawer focus-trap | FAIL (shared component) | SlidePanel (F-U6) |
| Drawer ESC close | PASS | SlidePanel handles Escape |
| Button-styled div has role/tabIndex/onKeyDown | PASS | MonthCard in sla/index.tsx:89-100 |
| Segmented control `aria-pressed` | PASS | sla/index.tsx:289 |
| Segmented control group label | FAIL | sla/index.tsx:277 (F-U3) |
| Label ↔ Input pair via `htmlFor`/`id` | PASS | detail.tsx:1479, 1496 |
| `aria-describedby` for help/error | FAIL | detail.tsx SLATargetsSection (F-U16) |
| `aria-invalid` on invalid Input | PASS | detail.tsx:1489, 1506 |
| Error message `role="alert"` | PASS | detail.tsx:1493, 1510 |
| Download link `aria-label` | PASS | index.tsx:116 |
| Refresh button `aria-label` | PASS | index.tsx:299 |
| Year selector `aria-label` | PASS | index.tsx:275 |

---

## Loading / Empty / Error State Matrix

| Screen | Loading | Empty | Error |
|---|---|---|---|
| `/sla` (index) | PASS — MonthCardSkeleton × N + KPI skeletons | PASS — `EmptyState` with CTA | PASS — error banner with retry |
| MonthDetail drawer | PASS — KpiChip × 5 + table skeleton | PARTIAL — table-level "No operator data" but no empty state when whole month is missing (F-U8) | PASS — danger banner (but F-U8: conflates 404 with failure) |
| OperatorBreach drawer | PASS — 4 row skeletons | PASS — ShieldCheck + "No breaches recorded" | PASS — danger banner |
| SLATargetsSection | N/A (synchronous) | N/A | PARTIAL — inline error shown, toast on save failure |

---

## Responsive Check (static class inspection)

| Surface | Breakpoint class | Result |
|---|---|---|
| `/sla` top bar | `flex-col gap-3 sm:flex-row sm:items-end sm:justify-between` | PASS |
| KPI grid | `grid-cols-2 md:grid-cols-4 gap-3` | PASS |
| Month card grid | `grid-cols-2 md:grid-cols-3 gap-4` | PASS |
| KpiChip in MonthDetail | `grid-cols-5` (fixed) | FAIL (F-U17) |
| Table in MonthDetail | wraps inside SlidePanel `max-w-4xl` | PARTIAL — no horizontal scroll wrapper on Table |
| SLATargetsSection | `grid-cols-2` (fixed) | MINOR — collapses OK on narrow due to parent container |

---

## Cross-Screen Consistency

| Element | Pattern | Verdict |
|---|---|---|
| Card radius | `rounded-[var(--radius-md)]` | PASS everywhere except F-U1 |
| Status pill styling | `rounded px-2 py-0.5 text-[10px] font-mono font-semibold border` | PASS — same in index.tsx:157 and operator-breach.tsx:107 |
| Bar gauge under uptime number | `h-1 rounded-full bg-bg-hover` | PASS (index.tsx:130, month-detail.tsx:88) |
| Uptime classifier thresholds | Duplicated logic | FAIL (F-U11) |

---

## Screen Mockup Compliance

- Plan Screen Mockup `/sla` — **11/14 elements implemented**. Missing: month strip (separate scrollable row before matrix); 6M Trend sparkline per operator (F-U18); per-row operator matrix table (currently only MonthCard grid at page level — full Operator × Month matrix is delegated to the drawer, diverging from mockup line 343-348).
- Plan Screen Mockup MonthDetailDrawer — **7/8 elements**. Missing Reset button, `[Download month PDF]` button at drawer footer (only per-row PDF link ships).
- Plan Screen Mockup OperatorBreachDrawer — **4/6 elements**. Missing Totals header line (F-U5) and `[Download operator-month PDF]` footer button.
- Plan Screen Mockup Operator Detail SLA Targets — **3/5 elements**. Missing Reset defaults + last-changed line (F-U15).

---

## CRITICAL / MAJOR Summary

- CRITICAL: 1 (F-U1 undefined tokens in production UI)
- MAJOR: 7 (F-U2 nested-drawer wrapper, F-U3 segmented-control a11y, F-U4 rgba shadows, F-U5 missing sessions/totals, F-U6 SlidePanel dialog semantics, F-U7 PDF auth + loading state, F-U8 missing not-available empty state)
- MINOR: 12 (F-U9 … F-U20)

All findings have clear fixes. No fixes applied per scout scope.

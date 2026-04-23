<SCOUT-UI-FINDINGS>

## UI Scope
- Story has UI: YES
- Screens tested: SCR-Analytics (Usage page, `/analytics`)
- In-scope files inspected: `web/src/pages/dashboard/analytics.tsx`, `web/src/components/analytics/two-way-traffic.tsx`, `web/src/components/analytics/usage-chart-tooltip.tsx`, `web/src/types/analytics.ts`, `web/src/lib/format.ts`.

## Enforcement Summary
| Check | Matches |
|-------|---------|
| Hardcoded hex colors | 0 |
| Arbitrary pixel values | 14 (all pre-existing page scaffold — `h-[300px]`, `text-[10px]`, `text-[16px]`, `text-[22px]`, `text-[11px]`, `tracking-[1.5px]`, `h-[200px]`) — documented in plan Design Token Map as "established, keep" |
| Raw HTML elements (table/input/etc.) | 0 |
| Competing UI library imports | 0 |
| Default Tailwind colors (gray/slate/white) | 0 |
| Inline SVG | 0 |
| Missing elevation (shadow-none) | 0 |

All px values are documented tokens in the plan. New files (`two-way-traffic.tsx`, `usage-chart-tooltip.tsx`) introduce ZERO new arbitrary px values — clean.

## Visual Quality Score
| Criterion | Score |
|-----------|-------|
| Design Tokens | PASS |
| Typography | PASS |
| Spacing | PASS |
| Color | PASS |
| Components | PASS |
| Empty States | PASS |
| Loading States | PASS |
| Error States | PASS |
| Interactive States | PASS |
| Tables | PASS |
| Forms | N/A |
| Icons | PASS |
| Shadows/Elevation | PASS |
| Transitions | PASS |
| Responsive | PASS |

## Screen Mockup Compliance
- Top Consumers columns (# / ICCID / IMSI / MSISDN / Operator / APN / IN/OUT / Total / Sessions / Avg Duration): 10/10 implemented per ASCII spec.
- IMSI + MSISDN `hidden md:table-cell` applied (R4 mitigation).
- EntityLink used for ICCID (sim), Operator, APN — FIX-219 compliance confirmed.
- TwoWayTraffic atom used in table IN/OUT cell AND inside chart tooltip (single source).
- Capitalization: `humanizeGroupDim(dim)` applied to Breakdown titles; `humanizeRatType` applied in breakdown rows + chart legend.
- DeltaBadge: refactored to accept `current/previous`, uses `formatDeltaPct`, tone→class map (`text-success` / `text-danger` / `text-text-tertiary`), renders `<TrendingUp>` icon for prev=0 branch, returns `null` for `tone='null'`.
- Empty state: shows date range hint + filter-aware actionable copy (AC-10).
- Group-by zero-groups message: inline in chart card (AC-11).
- AC-14 Export CSV: deferred with inline code comment — verified line 411.

## API Testing
Skipped for this gate scope — story is FE-polish-only; Analysis Scout owns API verification. Types in `analytics.ts` match DTO spec from plan (bytes_in/bytes_out, imsi, msisdn, operator_id, apn_id, avg_duration_sec optional).

## Findings

(No CRITICAL or HIGH findings — implementation matches plan cleanly.)

### F-U1 | LOW | ui
- Title: TwoWayTraffic tooltip uses custom `Tooltip` atom but `role="tooltip"` omitted on the panel
- Location: `web/src/components/ui/tooltip.tsx:28` (pre-existing atom; not in scope but noted for cross-screen consistency)
- Description: The shared `Tooltip` atom renders visible content without `role="tooltip"` or `aria-describedby` wiring. TwoWayTraffic inherits this limitation. AC-5 (accessible tooltip) is satisfied for the chart tooltip (`role="tooltip"` set explicitly in `usage-chart-tooltip.tsx:55` and `:104`), but the byte-cell hover tooltip remains non-AT-announced.
- Fixable: YES (but out of FIX-220 scope — affects every consumer of the shared Tooltip)
- Suggested fix: File a follow-up FIX to add `role="tooltip"` + `id` wiring in `ui/tooltip.tsx`; do NOT patch inline in FIX-220.

### F-U2 | LOW | ui
- Title: Breakdown `humanizeRatType` applied twice in rat_type branch
- Location: `web/src/pages/dashboard/analytics.tsx:590-593`
- Description: `resolveGroupLabel('rat_type', key)` already calls `humanizeRatType` for non-`__unassigned__` keys (see line 94). The breakdown loop then wraps the result again in `humanizeRatType(...)`. For keys like `lte_m`: first call returns `LTE-M`, second call returns `LTE-M` via fallback `.toUpperCase()` because `LTE-M` is not in the map → result is `LTE-M` (unchanged, no regression). But the double-call is dead code — if the map keys ever change case or format, this could mask bugs. Purely cosmetic; no visible defect today.
- Fixable: YES
- Suggested fix: Simplify to `const label = resolveGroupLabel(dim, item.key)` (resolver already handles rat_type). Remove the inner `humanizeRatType` wrap.

### F-U3 | LOW | ui
- Title: EmptyState date-range hint shows "Mar 5 – Mar 12" style; Turkish/locale date format not applied
- Location: `web/src/pages/dashboard/analytics.tsx:167`
- Description: `toLocaleDateString('en-GB', { month: 'short', day: 'numeric' })` yields `5 Mar`. CLAUDE.md global preference notes Turkish UI date format `DD.MM.YYYY`. However the rest of analytics.tsx uses `en-GB` consistently (chart axis tick formatter, line 449) — so this is NOT a regression introduced by FIX-220 and matches current page locale convention. Flag only for future full-Turkish i18n pass.
- Fixable: Deferred
- Suggested fix: Defer to a global i18n pass; no action in FIX-220.

## Evidence
- Typecheck: `npm --prefix web run typecheck` → zero errors.
- Enforcement grep run against all 3 .tsx in-scope files (page + 2 components): zero hex, zero default-tailwind, zero raw HTML, zero competing libs, zero inline SVG.
- New components (`two-way-traffic.tsx`, `usage-chart-tooltip.tsx`) token-clean: no hex, no arbitrary px.
- Plan-to-code alignment: all AC-1..AC-13 items mapped; AC-14 explicitly deferred with inline comment.
- Live browser check skipped — would require dev-browser session; deferred to full UAT gate.

## Gate Verdict
**PASS (advisory)** — 0 CRITICAL, 0 HIGH, 3 LOW. All LOW findings are cosmetic/cross-cutting and do NOT block FIX-220. Story implementation matches plan spec with high fidelity.

</SCOUT-UI-FINDINGS>

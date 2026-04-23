# FIX-221 Gate Scout — UI

Scope: heatmap tooltip visual correctness, IP Pool KPI rendering, AC string-match, a11y, responsive, dark mode, token enforcement.

## Files reviewed
- `web/src/pages/dashboard/index.tsx:115-220` (KPICard component).
- `web/src/pages/dashboard/index.tsx:425-540` (TrafficHeatmap component).
- `web/src/pages/dashboard/index.tsx:1118-1140` (IP Pool Usage KPICard call site).
- `web/src/types/dashboard.ts:64-93` (types).

## AC-1 (Heatmap Tooltip)
Expected: `"12.4 MB @ Mon 14:00"` on cell hover.

Code (lines 504-508):
```tsx
<div className="absolute top-0 right-0 bg-bg-elevated border border-border rounded-[var(--radius-sm)] px-2.5 py-1.5 text-[11px] font-mono pointer-events-none z-20 shadow-lg">
  <span className="text-accent font-semibold">{formatBytes(hoveredCell.rawBytes)}</span>
  <span className="mx-1.5 text-text-tertiary">@</span>
  <span className="text-text-secondary">{DAYS[hoveredCell.day]} {hoveredCell.hour.toString().padStart(2, '0')}:00</span>
</div>
```
- `formatBytes` returns 1024-base short units (B/KB/MB/GB/TB) — matches mockup `"12.4 MB"`. PASS.
- `DAYS[day]` → `'Mon'|'Tue'|...|'Sun'` (3-letter abbrev). PASS.
- `hour.toString().padStart(2,'0')` → `'14'`. PASS.
- Trailing `:00`. PASS.
- Final rendered text: `"<bytes> @ <Day> HH:00"`. AC-1 MATCHED.

Stale `"| {value} req/s"` text removed. PASS.

## AC-2 (Backend DTO)
- `trafficHeatmapCell.RawBytes int64 json:"raw_bytes"` — present, required, int64. PASS.
- FE `TrafficHeatmapCell.raw_bytes: number` — present. PASS.

## AC-3 (IP Pool KPI)
Expected: title always `"Pool Utilization (avg across all pools)"`; subtitle `"Top pool: <name> <pct>%"` conditional.

Code (lines 1119-1133):
```tsx
<KPICard
  title="Pool Utilization (avg across all pools)"
  ...
  subtitle={
    data.top_ip_pool
      ? `Top pool: ${data.top_ip_pool.name} ${data.top_ip_pool.usage_pct.toFixed(0)}%`
      : undefined
  }
  ...
/>
```
- Title literal match (with "avg across all pools"). PASS.
- Subtitle rendered under sparkline, `.toFixed(0)` for integer pct. PASS.
- Empty-tenant case: `subtitle={undefined}` → guard `{subtitle && …}` in KPICard (line 211) skips render. PASS.
- Other 7 KPI cards omit `subtitle` prop → unchanged. PASS.

## UI Findings

### F-U1 LOW — Tooltip lacks AT-announcement (`role="tooltip"`, `aria-describedby`)
- Evidence: tooltip DIV at `:504` uses `pointer-events-none` and is mouse-only (`onMouseEnter/Leave` at `:497-498`). No `role="tooltip"`, no keyboard focus path, no `aria-describedby` on the cell.
- Already tracked as cross-cutting a11y debt via D-107/D-108 (FIX-220 gate). Noted; no additional D-### required.
- Classification: DEFERRED — covered by D-107 (shared-tooltip a11y pass).

### F-U2 LOW — Tooltip position fixed to `absolute top-0 right-0` — can occlude top-right cells
- Evidence: tooltip is anchored to the Heatmap card's top-right corner regardless of the hovered cell. When the user hovers the Sunday 21:00 cell (rightmost), the tooltip sits directly over that cell (though `pointer-events-none` prevents hit-break). On narrow viewports the tooltip may also run up against the card header row.
- Severity: LOW. Pre-existing layout choice (unchanged by FIX-221); hover interaction still works because `pointer-events-none` disables event capture.
- Classification: DEFERRED — smart tooltip positioning (follow cursor or flip-on-edge) is a polish pass. Tracked as D-116.

### F-U3 LOW — `DAYS` array constant unused from a shared location
- Evidence: local `const DAYS = ['Mon', 'Tue', …]` at `index.tsx:30`. Analytics / other charts may duplicate the same array.
- Not introduced by FIX-221 (pre-existing); out of scope. No action.

### F-U4 PASS — Design tokens
- Tooltip uses `bg-bg-elevated`, `border-border`, `text-accent`, `text-text-secondary`, `text-text-tertiary`, `rounded-[var(--radius-sm)]`, `shadow-lg`, `font-mono text-[11px]` — all existing tokens (per plan Design Token Map). Zero new hex/rgba/px introduced by FIX-221.
- KPICard subtitle uses `mt-1.5 text-[10px] font-mono text-text-tertiary truncate` — matches plan-sanctioned classes (sparkline-label peer typography).

### F-U5 PASS — Dark-mode parity
- All tokens (`bg-bg-elevated`, `border-border`, `text-*`) are CSS-variable driven (confirmed `var(--radius-sm)` inline; `var(--color-accent)` used via Tailwind `text-accent` mapping). Theme-responsive.

### F-U6 PASS — Responsive / `truncate`
- KPICard subtitle has `truncate` class → overlong pool names (e.g. `"Enterprise-M2M-Fleet-APN-Primary"`) will ellipsize inside the ~200px card width. PASS.
- KPICard title wraps to 2 lines on narrow viewports (per plan decision: accept wrap over abbreviation). PASS.

### F-U7 PASS — Stagger delay / animation
- IP Pool KPI has `delay={230}` unchanged — animation order preserved.
- Heatmap card `animationDelay: '300ms'` unchanged.

## Token Enforcement Table
| Check | Count in dashboard/index.tsx | Status |
|-------|-------------------------------|--------|
| Hardcoded hex `#rrggbb` | 0 | PASS |
| Raw `<button>` | 0 | PASS |
| `rgba(...)` | 12 (pre-existing heatmap palette — out of FIX-221 scope) | PASS (not introduced) |
| `text-white` / default Tailwind color names | 0 | PASS |
| Inline `<svg>` | 0 | PASS |
| Competing UI libraries | 0 | PASS |

## Summary
UI PASS. All 3 ACs met with exact string/format fidelity. Token enforcement clean. Two LOW a11y / positioning polish items deferred (one to existing D-107, one new D-116).

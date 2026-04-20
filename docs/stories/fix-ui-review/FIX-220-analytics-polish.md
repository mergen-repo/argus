# FIX-220: Analytics Polish — MSISDN Column, IN/OUT Split, Tooltips, Delta Cap, Capitalization

## Problem Statement
Analytics page has multiple UX issues affecting daily usability:
- Per-SIM breakdown missing MSISDN column (only shows ICCID) — matchy with support tickets hard
- Traffic columns merge bytes IN+OUT into single "total" — direction unclear
- Chart tooltip lists only value (no time, no delta, no top-contributor context)
- Delta % uncapped — tooltip shows "+99999%" when growth from zero, unreadable
- Header capitalization inconsistent: some "BYTES IN" (all caps), some "Bytes In"
- Empty state "No data" — not actionable, doesn't suggest filter adjustments

## User Story
As a data analyst, I want Analytics page to render clean, informative columns and charts so I can interpret patterns quickly without reading raw UUIDs or puzzling over abbreviated values.

## Architecture Reference
- FE: `web/src/pages/analytics/*.tsx`
- Backend: DTO may need MSISDN addition (FIX-202 coordination)
- Shared helpers: `formatBytes`, `formatPercent` for consistency

## Findings Addressed
- F-23 (per-SIM MSISDN missing)
- F-26 (IN/OUT merged)
- F-29 (tooltip sparse)
- F-30 (delta uncapped)
- F-32/F-33 (capitalization + formatting)

## Acceptance Criteria

### Per-SIM breakdown table
- [ ] **AC-1:** Columns: ICCID | IMSI | MSISDN | Operator | APN | Bytes In | Bytes Out | Total Bytes | Sessions | Avg Duration
  - MSISDN added (FIX-202 DTO already provides)
  - Bytes In + Bytes Out split (IN=downstream to device, OUT=upstream)
  - Total = IN+OUT (computed column, sortable)
- [ ] **AC-2:** Each byte column uses `formatBytes()` humanize (12.4 MB, 1.2 GB, etc.)
- [ ] **AC-3:** Sessions + Avg Duration columns also present for usage patterns.

### Time series chart tooltip
- [ ] **AC-4:** Hover on chart point shows:
  - Timestamp (absolute + relative)
  - Value (raw + humanized)
  - Delta vs previous bucket (+X.Y% or -X.Y%)
  - If `group_by` active: top contributor for that bucket (e.g., "Top: Turkcell — 320 sessions")
  - Multi-series tooltip — all series listed together
- [ ] **AC-5:** Tooltip accessible (aria, keyboard focusable).

### Delta % capping
- [ ] **AC-6:** Delta % display rules:
  - Range [-100%, 999%]: exact value (e.g., "+42.5%")
  - Value > 999%: ">999% ↑"
  - Value < -100% (impossible but defensive): "—"
  - Value 0%: "0%" with trendline icon
  - Value undefined (first bucket, no previous): "—"
- [ ] **AC-7:** Color coding: positive = green (up), negative = red (down), 0 = gray. Configurable if "down is good" metric (e.g., error rate).

### Capitalization
- [ ] **AC-8:** All column headers, chart legends, tooltip labels use Title Case (e.g., "Bytes In" not "BYTES IN" / "bytes_in").
- [ ] **AC-9:** Enums displayed humanized: `lte_m` → "LTE-M", `nr_5g` → "5G NR".

### Empty states
- [ ] **AC-10:** No-data messages actionable:
  - "No data for the selected filter (2026-04-01 to 2026-04-19). Try expanding the date range or clearing the Operator filter."
- [ ] **AC-11:** Group-by with zero groups also clear: "No groupings found — all values in '__unassigned__' bucket. Configure APN mappings to see breakdown."

### Group-by integration
- [ ] **AC-12:** `group_by=apn` works end-to-end (FIX-204 backend fix) — verified in this story's browser test.
- [ ] **AC-13:** `group_by=operator`, `group_by=rat_type`, `group_by=apn` all render correctly.

### Export
- [ ] **AC-14:** "Export CSV" button on table → uses FIX-236 streaming pattern; includes all visible columns.

## Files to Touch
- `web/src/pages/analytics/*.tsx` — breakdown table, chart component
- `web/src/lib/format.ts` — formatBytes, formatPercent, formatDelta helpers (shared)
- `web/src/components/analytics/tooltip.tsx` (NEW — enhanced chart tooltip)
- Backend (if MSISDN missing): `internal/api/analytics/handler.go` DTO

## Risks & Regression
- **Risk 1 — Large delta displays saturate:** AC-6 cap protects from UI break.
- **Risk 2 — MSISDN column breaks table width:** Responsive column show/hide; MSISDN hidden on narrow screens with tooltip on row.
- **Risk 3 — formatBytes inconsistent if not helpers centralized:** Single source `web/src/lib/format.ts`.

## Test Plan
- Unit: format helpers cover edge cases (0, 1, 1024, 2^40, -1, NaN)
- Browser: each chart type tooltip, delta display, empty state copy
- Group-by: each of 3 groupings renders

## Plan Reference
Priority: P2 · Effort: M · Wave: 6 · Depends: FIX-204 (group_by=apn fix), FIX-202 (MSISDN DTO)

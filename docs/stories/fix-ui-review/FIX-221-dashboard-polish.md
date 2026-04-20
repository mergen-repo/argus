# FIX-221: Dashboard Polish — Heatmap Tooltip, IP Pool KPI Clarity

## Problem Statement
Dashboard 7x24 traffic heatmap lacks tooltip with raw values; IP Pool KPI card semantics unclear.

## User Story
As an operator, I want Dashboard widgets to surface exact values on hover and label metrics clearly.

## Findings Addressed
F-05

## Acceptance Criteria
- [ ] **AC-1:** Heatmap tooltip: "12.4 MB @ Mon 14:00" on cell hover (raw_bytes + day + hour).
- [ ] **AC-2:** Backend `/dashboard` response `traffic_heatmap` includes `raw_bytes` per cell (already normalized 0-1 for color).
- [ ] **AC-3:** IP Pool KPI card label: "Pool Utilization (avg across all pools)" + subtitle "Top pool: Demo M2M 45%".

## Files to Touch
- `internal/api/dashboard/handler.go` — add raw_bytes
- `web/src/components/dashboard/traffic-heatmap.tsx`
- `web/src/components/dashboard/ip-pool-kpi.tsx`

## Risks & Regression
- Cosmetic.

## Test Plan
- Browser: hover heatmap cell → tooltip shows value

## Plan Reference
Priority: P2 · Effort: S · Wave: 6

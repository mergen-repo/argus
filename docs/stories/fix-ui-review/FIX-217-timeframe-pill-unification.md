# FIX-217: Timeframe Selector Pill Toggle Unification

## Problem Statement
Multiple pages have timeframe selectors with different styles/positions (dropdown, pill group, custom). Inconsistent UX.

## User Story
As a user, I want a single consistent timeframe selector (1h / 24h / 7d / 30d / Custom) across Dashboard, Analytics, Alerts, SLA, Traffic pages.

## Findings Addressed
F-61, F-73

## Acceptance Criteria
- [ ] **AC-1:** New shared component `<TimeframePills>` with options: 1h / 24h / 7d / 30d / Custom. Default: 24h.
- [ ] **AC-2:** Custom option → date range picker popover.
- [ ] **AC-3:** URL sync — `?from=...&to=...` query param; deep-linking works.
- [ ] **AC-4:** Applied to: Dashboard, Analytics, Alerts, SLA, Traffic, Capacity (where timeframe exists).
- [ ] **AC-5:** Single height/padding specification; keyboard navigable.

## Files to Touch
- `web/src/components/ui/timeframe-pills.tsx` (NEW)
- 6 page files

## Risks & Regression
- **Risk 1 — Query URL format changes:** backward compat not required (internal page URLs).

## Test Plan
- Browser: consistent pill style across 6 pages; custom range works

## Plan Reference
Priority: P2 · Effort: S · Wave: 5

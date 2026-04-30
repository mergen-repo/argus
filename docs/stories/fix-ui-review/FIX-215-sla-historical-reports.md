# FIX-215: SLA Historical Reports + PDF Export + Drill-down

## Problem Statement
SLA page shows current month but no historical view (last 6 months trend), PDF export broken, no per-operator drill-down for breach investigation.

## User Story
As a contract manager, I want monthly SLA reports with historical trends and drill-down per operator breach so I can defend contractual uptime claims.

## Architecture Reference
- Backend: `internal/api/sla/handler.go` + reports (FIX-248 `sla_monthly`)
- Store: `sla_reports` table (exists)

## Findings Addressed
F-44, F-46, F-47, F-48, F-49

## Acceptance Criteria
- [ ] **AC-1:** SLA page shows monthly summary cards for last 6 months (selectable year).
- [ ] **AC-2:** Per-month drill-down: list of operators with uptime %, incident count, breach list (if any).
- [ ] **AC-3:** Per-operator click → month detail: breach events (start/end timestamps), downtime minutes, affected session count.
- [ ] **AC-4:** PDF export via FIX-248 `sla_monthly` report (post-FIX-248 local storage). On-demand generation triggers job; download link in notification or page.
- [ ] **AC-5:** SLA target configurable per operator (default 99.9%). Editable in Operator Detail (Protocols/SLA tab).
- [ ] **AC-6:** Breach computation — any ≥5min continuous health=down OR latency>threshold counts as breach.
- [ ] **AC-7:** Historical data retention: 24 months minimum (compliance).

## Files to Touch
- `internal/api/sla/handler.go` — history endpoint
- `internal/store/sla.go` — monthly rollup
- `web/src/pages/sla/*` — historical view + drill-down
- `internal/report/sla_monthly.go` — PDF builder (FIX-248 scope)

## Risks & Regression
- **Risk 1 — Missing historical data:** If seed lacks 6-month SLA history, backfill script required.
- **Risk 2 — PDF generator dependency:** May need wkhtmltopdf / chromedp. Document in `docs/architecture/DEPLOYMENT.md`.

## Test Plan
- Integration: 12-month seed data, history query returns 12 rows
- Browser: drill-down flow, PDF generated + downloadable

## Plan Reference
Priority: P1 · Effort: L · Wave: 4

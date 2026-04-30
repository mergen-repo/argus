# FIX-203: Dashboard Operator Health — Uptime/Latency/Activity + WS Push

## Problem Statement
Dashboard Operator Health Matrix displays static 99.9% uptime, 0ms latency, 0 activity for ALL operators because `operatorHealthDTO` only carries `{id, name, status, health_pct}` — missing `latency_ms, active_sessions, auth_rate, last_check, sla_target`. Data IS available in `/operators` endpoint but not joined into dashboard DTO. No WS push for operator health changes — UI requires manual refresh.

## User Story
As an operator, I want the Dashboard Operator Health panel to show real latency, active sessions, and auth success rate per operator, updating live via WebSocket, so I can monitor fleet health without refreshing.

## Architecture Reference
- Backend: `internal/api/dashboard/handler.go:89-94` + `:231-239` (operatorHealthDTO builder)
- NATS: new event `operator.health_changed` publisher in health check worker
- FE: `web/src/hooks/use-dashboard.ts` + WS subscriber

## Findings Addressed
F-03, F-04, F-45, F-50, F-55, F-80

## Acceptance Criteria
- [ ] **AC-1:** `operatorHealthDTO` widened to include: `code, latency_ms, active_sessions, auth_rate (0-100%), last_check (timestamp), sla_target (%), status`.
- [ ] **AC-2:** Dashboard handler queries `operators` table JOIN for latest metrics (leverage FIX-202 enrichment pattern).
- [ ] **AC-3:** Operator health check worker publishes `argus.events.operator.health.changed` NATS event when status/latency changes by threshold (>10% latency delta OR status flip).
- [ ] **AC-4:** WS hub subscribes `operator.health.changed` → relays to `operator.health_changed` client event.
- [ ] **AC-5:** FE `useDashboard` subscribes WS → patches in-place operator row without full dashboard refetch.
- [ ] **AC-6:** UI row shows: operator name + code, status badge (color-coded: healthy green / degraded yellow / down red), latency_ms with sparkline trend (last 1h), active_sessions count, auth_rate %, "Last check: Nm ago" relative time.
- [ ] **AC-7:** SLA target display — if latency > sla_target: red chip "SLA breach". Configurable per operator (default 500ms).
- [ ] **AC-8:** Dashboard polling fallback 30s if WS disconnects — current 30s poll preserved.
- [ ] **AC-9:** Scale — 50+ operators fit in dashboard panel (virtualized or paginated table view).

## Files to Touch
- `internal/api/dashboard/handler.go` — widen DTO + populate fields
- `internal/store/operator.go` — health aggregate query
- `internal/aaa/operator/health_worker.go` — publish NATS event on change
- `internal/bus/nats.go` — add SubjectOperatorHealthChanged (may already exist)
- `internal/ws/hub.go` — subject registration
- `web/src/hooks/use-dashboard.ts` — WS subscribe
- `web/src/types/dashboard.ts` — extended OperatorHealth type
- `web/src/components/dashboard/operator-health.tsx` — render new fields

## Risks & Regression
- **Risk 1 — WS message flood:** Health checks every 30s × 50 operators = frequent noise. AC-3 threshold (>10%) reduces chatter.
- **Risk 2 — Stale data on reconnect:** After WS reconnect, FE triggers full refetch.
- **Risk 3 — Existing 99.9% hardcoded tests:** Test fixtures may assert old values; update.

## Test Plan
- Unit: handler returns all 6 fields; threshold test for event publishing
- Integration: trigger operator state flip → WS client receives event within 2s
- Browser: Dashboard shows live latency sparkline; kill an operator simulator → status changes to degraded within 5s

## Plan Reference
Priority: P0 · Effort: L · Wave: 3

# STORY-070: Frontend Real-Data Wiring

## User Story
As a customer and auditor using the Argus portal, I want every chart, metric, and list to show real data from the backend, every filter to deep-link via URL, every action button to actually work, and error states to be surfaced not silently swallowed, so that the portal is a production tool and not a demo.

## Description
Frontend audit found 15+ pages with fake data via `Math.random()`, hardcoded arrays, or mock generators. Dashboard sparklines, SIM detail usage, APN list stats, APN traffic charts, operator health timeline, operator circuit breaker, capacity targets, SLA metrics, traffic heatmap, and the entire Reports page are all fake. Additionally: filters don't persist in URL (no deep linking), violations page has no remediation actions, topology is static, and several catch blocks silently discard errors.

## Architecture Reference
- Packages: web/src/pages/*, web/src/hooks/*, web/src/components/*, web/src/lib/*
- Source: Phase 10 frontend audit (6-agent scan 2026-04-11)
- Depends on backend: STORY-057 (API-051/052 + dashboard real), STORY-063 (SLA reports), STORY-069 (reports API)

## Screen Reference
- SCR-001 (Dashboard), SCR-041..045 (SIM Detail), SCR-060 (APN List), SCR-061 (APN Detail), SCR-062 (Operator Detail), SCR-073 (SLA), SCR-074 (Capacity), SCR-125..127 (Reports), SCR-085 (Violations), SCR-086 (Topology)

## Acceptance Criteria
- [ ] AC-1: **Dashboard fake data eliminated.**
  - `use-dashboard.ts:52` — no more `Array.from(..., () => Math.random() * 60)`. Real sparkline arrays from `GET /dashboard` API.
  - Dashboard heatmap (`dashboard/index.tsx:56-67`) — real 7×24 traffic matrix from `/dashboard/heatmap` or equivalent (backend adds if missing).
  - Live event stream (`index.tsx:641-661`) — event.id from server, not client-generated `${Date.now()}-${Math.random()}`.
- [ ] AC-2: **SIM Detail UsageTab real CDR data.**
  - `sims/detail.tsx:322-329` `mockUsageData` deleted.
  - `useSIMUsage(simId, period)` hook wired to API-052 (STORY-057 AC-7)
  - Real CDR timeseries chart + top sessions list
  - Period toggle (15m/1h/6h/24h/7d/30d) fetches from real API
- [ ] AC-3: **APN detail real traffic charts.**
  - `apns/detail.tsx:581-597` `generateMockTraffic()` / `generateMockFrequency()` deleted.
  - Backend adds `GET /apns/:id/traffic?period=...` returning bytes_in/out series + auth frequency; handler aggregates from sessions/CDRs (or dedicated metric).
  - Frontend hook + chart components.
- [ ] AC-4: **APN list real stats.**
  - `apns/index.tsx:188-191` `mockSimCount` / `mockTrafficMB` / `mockPoolUsed` / `mockPoolTotal` deleted.
  - API-035 (`GET /apns/:id/sims`) returns count (STORY-057 AC-8) + backend `/apns` list enriches each APN row with aggregated stats (sim_count, traffic_mb_24h, pool_used, pool_total).
- [ ] AC-5: **Operator detail real timeline + breaker state.**
  - `operators/detail.tsx:273-289` `mockTimeline` deleted — real `GET /operators/:id/health-history?hours=24`
  - `operators/detail.tsx:438-456` `mockAuthData` deleted — real `GET /operators/:id/metrics?window=1h`
  - Backend handlers read from `operator_health_logs` + analytics metrics (STORY-065)
- [ ] AC-6: **Capacity page real targets.**
  - `capacity/index.tsx:77` `allocation_rate: Math.round(50 + Math.random() * 200)` removed.
  - `capacity/index.tsx:90-96` hardcoded `sim_capacity: 15_000_000` etc removed.
  - Real `GET /system/capacity` endpoint returns current utilization + configured targets (from `system_config` table or env-sourced).
  - Allocation rate tracked via background job or derived from time-delta SIM count.
- [ ] AC-7: **SLA page real metrics.**
  - `sla/index.tsx:78-88` all `Math.random()` deleted.
  - Backend SLA report from STORY-063 AC-4 feeds this page.
  - `GET /analytics/sla?from=...&to=...` returns real uptime_pct, latency_p95, downtime_minutes, incidents per operator.
- [ ] AC-8: **Reports page real API.**
  - `reports/index.tsx:63-81` `REPORT_DEFINITIONS` / `SCHEDULED_REPORTS` hardcoded arrays deleted.
  - `useReports()` hook queries `GET /reports/definitions`, `useScheduledReports()` queries `GET /reports/scheduled` (from STORY-069 AC-2)
  - `handleGenerate()` (`reports/index.tsx:119-122`) no more `setTimeout`. Real `POST /reports/generate` call, polls job until complete, downloads result.
  - `GenerateReportPanel` submits real POST, shows progress, downloads from response.
- [ ] AC-9: **Violations remediation actions.**
  - `violations/index.tsx` adds context menu / inline actions per row:
    - "Suspend SIM" (links to SIM handler)
    - "Review Policy" (opens policy editor at violating rule)
    - "Dismiss" (marks violation acknowledged)
    - "Escalate" (creates incident notification)
  - Each action hits real API and audits.
- [ ] AC-10: **Topology real flow animation.**
  - `topology/index.tsx` FlowLine component becomes data-driven.
  - Hook polls or subscribes WS for active session counts per operator→APN→pool edge.
  - Line thickness / color / animation speed reflects real traffic.
  - Offline/unreachable links highlighted red with dashed pattern.
- [ ] AC-11: **URL filter persistence.**
  - `useSearchParams()` wiring in: `sims/index.tsx`, `audit/index.tsx`, `sessions/index.tsx`, `jobs/index.tsx`, `esim/index.tsx`, `cdrs/index.tsx`, `anomalies/index.tsx`.
  - All filters (state, operator, segment, q, date range, etc.) reflected in URL query params.
  - Reload preserves state. Shareable deep links work.
  - Browser back/forward navigation reverts filter history.
- [ ] AC-12: **Silent catch blocks surfaced.**
  - `sims/index.tsx:662-678` Reserve IPs — show toast with success/fail counts; if any fail, offer "View failures" detail modal.
  - `audit/index.tsx:230-232` Verify Integrity — errors shown inline in a dismissible banner with details.
  - `onboarding/wizard.tsx:481-485` — per-step error display with clear "retry this step" action.
- [ ] AC-13: **WebSocket connection status indicator.** Small badge in header shows WS state (connected/reconnecting/offline). Tooltip explains impact ("live updates paused"). Clicking reconnects manually.
- [ ] AC-14: **Dead code removal.** `web/src/pages/placeholder.tsx` — if unused, delete. If repurposed, refactor to be a real empty-state pattern.

## Dependencies
- Blocked by: STORY-057 (API-051/052 + dashboard endpoints), STORY-063 (SLA reports), STORY-069 (reports API), STORY-065 (metrics for operator detail)
- Blocks: Phase 10 Gate, Documentation Phase (screenshots must be real data)

## Test Scenarios
- [ ] E2E: `grep -r "Math.random" web/src/pages web/src/hooks` returns zero matches.
- [ ] E2E: Dashboard sparklines show visually-distinct trends (not uniform random noise) across reloads.
- [ ] E2E: SIM detail Usage tab: select 24h period → real CDR totals match backend query.
- [ ] E2E: APN detail traffic chart shows bytes consistent with session data.
- [ ] E2E: Operator detail: kill one operator via admin action → health timeline shows the outage event within 10s.
- [ ] E2E: Capacity page targets match env-configured values.
- [ ] E2E: SLA page numbers match DB SLA report rows.
- [ ] E2E: Generate compliance report → actual PDF downloaded (not 2-second fake wait).
- [ ] E2E: Filter SIMs by state=active → URL contains `?state=active` → reload → filter still applied → share URL with another user → same filter.
- [ ] E2E: Reserve IPs with 50% failing → toast shows "25 succeeded, 25 failed" with "View details" link.
- [ ] E2E: Kill backend → WS badge shows "reconnecting" → restart → badge shows "connected".
- [ ] E2E: Violations page → click "Suspend SIM" on a row → confirmation → SIM state changes + audit entry.
- [ ] E2E: Topology page → operator adapter slows to 2s → line animation slows visibly.

## Effort Estimate
- Size: L
- Complexity: Medium (many small changes, all dependent on backend endpoints from other stories)

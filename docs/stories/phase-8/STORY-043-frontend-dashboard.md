# STORY-043: Frontend Main Dashboard

## User Story
As a user, I want a main dashboard showing key metrics, SIM distribution, operator health, APN traffic, and live alerts, so that I get an instant overview of platform status.

## Description
Main dashboard (SCR-010) with 4 metric cards (total SIMs, active sessions, auth/s, monthly cost) with sparkline trends, SIM distribution pie chart (by state), operator health bars, APN traffic bars, and live alert feed. Data from API-110 (dashboard) + WebSocket (metrics.realtime, alert.new). Auto-refresh every 30s for non-WebSocket data.

## Architecture Reference
- Services: SVC-03 (Core API — dashboard data), SVC-02 (WebSocket — real-time)
- API Endpoints: API-110
- Source: docs/architecture/api/_index.md (Analytics section)

## Screen Reference
- SCR-010: Main Dashboard — 4 metric cards, SIM distribution pie, operator health bars, APN traffic bars, live alert feed

## Acceptance Criteria
- [ ] 4 metric cards at top: Total SIMs, Active Sessions, Auth/s (live), Monthly Cost
- [ ] Each metric card shows: current value, trend sparkline (7-day), delta from previous period
- [ ] Auth/s card updates in real-time via WebSocket metrics.realtime event
- [ ] SIM distribution pie chart: breakdown by state (active, suspended, ordered, terminated)
- [ ] Operator health bars: each operator with health percentage, colored by status (green/yellow/red)
- [ ] APN traffic bars: top 5 APNs by current traffic volume
- [ ] Live alert feed: last 10 alerts with severity icon, message, timestamp, click to detail
- [ ] Alert feed updates in real-time via WebSocket alert.new event
- [ ] Dashboard data from API-110, auto-refresh every 30s for non-WebSocket data
- [ ] TanStack Query caching: stale time 30s, background refetch
- [ ] Loading skeletons while data fetches
- [ ] Error state with retry button if API fails
- [ ] Responsive: 2-column on tablet, 1-column on mobile

## Dependencies
- Blocked by: STORY-041 (scaffold), STORY-042 (auth), STORY-040 (WebSocket server)
- Blocks: None

## Test Scenarios
- [ ] Dashboard loads → 4 metric cards with values from API-110
- [ ] Auth/s card updates every 1s via WebSocket
- [ ] SIM distribution pie shows correct proportions
- [ ] Operator health bars reflect current health status
- [ ] New alert via WebSocket → appears at top of alert feed
- [ ] API failure → error state shown with retry button
- [ ] Auto-refresh after 30s → data updated
- [ ] Click alert → navigates to relevant detail page

## Effort Estimate
- Size: L
- Complexity: Medium

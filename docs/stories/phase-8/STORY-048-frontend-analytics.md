# STORY-048: Frontend Analytics Pages

## User Story
As an analyst, I want analytics pages with usage charts, cost cards, and anomaly tables, so that I can visualize traffic patterns, control costs, and monitor security threats.

## Description
Three analytics pages: Usage (SCR-011) with time-series charts using Recharts, period selector, and filter bar. Cost (SCR-012) with cost cards, carrier comparison, and optimization suggestions panel. Anomalies (SCR-013) with anomaly table, severity badges, expandable detail rows, and acknowledge/resolve actions.

## Architecture Reference
- Services: SVC-07 (Analytics Engine)
- API Endpoints: API-111, API-112, API-113
- Source: docs/architecture/api/_index.md (Analytics section)

## Screen Reference
- SCR-011: Analytics Usage — time-series area/line charts, period selector, group-by toggle, top consumers table
- SCR-012: Analytics Cost — total cost card, carrier comparison bar chart, cost per MB table, optimization suggestions
- SCR-013: Analytics Anomalies — anomaly table with severity, type, SIM, detected time, expandable details

## Acceptance Criteria
- [ ] Usage page: time-series chart (Recharts area chart) showing bytes/sessions/auths over time
- [ ] Usage page: period selector (1h, 24h, 7d, 30d, custom date range)
- [ ] Usage page: group-by toggle (operator, APN, RAT type) → stacked area chart
- [ ] Usage page: top consumers table (top 20 SIMs by usage)
- [ ] Usage page: comparison mode (toggle) → overlay previous period as dashed line
- [ ] Usage page: filter bar (operator, APN, RAT type, segment)
- [ ] Cost page: total cost card with current period value and delta from previous
- [ ] Cost page: carrier comparison horizontal bar chart (cost per operator)
- [ ] Cost page: cost per MB table by operator and RAT type
- [ ] Cost page: optimization suggestions panel with cards (description, savings, action button)
- [ ] Cost page: action button links to bulk operation (e.g., "Switch 500 SIMs" → bulk operator switch)
- [ ] Anomalies page: table with severity badge (color-coded), type, affected SIM (link), detected_at
- [ ] Anomalies page: filter by type, severity, state (open/acknowledged/resolved)
- [ ] Anomalies page: expandable row showing full detail (JSON), NAS IPs, timestamps
- [ ] Anomalies page: acknowledge and resolve actions per anomaly
- [ ] All pages: loading skeletons, error states with retry, empty states
- [ ] All charts: tooltip on hover, responsive sizing

## Dependencies
- Blocked by: STORY-041 (scaffold), STORY-042 (auth), STORY-034 (usage API), STORY-035 (cost API), STORY-036 (anomaly API)
- Blocks: None

## Test Scenarios
- [ ] Usage page: select 24h period → chart renders with 15min buckets
- [ ] Usage page: group by operator → stacked chart with operator colors
- [ ] Usage page: top consumers shows 20 SIMs sorted by usage
- [ ] Cost page: total cost card shows correct sum
- [ ] Cost page: carrier comparison → bars proportional to cost
- [ ] Cost page: optimization suggestion → click action → navigate to bulk ops
- [ ] Anomalies page: critical anomaly → red severity badge
- [ ] Anomalies page: expand row → full detail JSON displayed
- [ ] Anomalies page: acknowledge → state updates to acknowledged
- [ ] Period change → chart reloads with new data
- [ ] Empty analytics → "No data for selected period" message

## Effort Estimate
- Size: XL
- Complexity: High

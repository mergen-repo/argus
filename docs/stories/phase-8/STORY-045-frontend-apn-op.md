# STORY-045: Frontend APN & Operator Pages

## User Story
As an operator manager, I want APN and Operator list/detail pages with health indicators, traffic charts, and configuration panels, so that I can monitor and manage network infrastructure.

## Description
APN list (SCR-030) with card layout showing SIM count, traffic, IP utilization. APN detail (SCR-032) with configuration, IP pool stats, SIM list. Operator list (SCR-040) with health status cards. Operator detail (SCR-041) with health history timeline, circuit breaker state, failover config, traffic charts, connected SIM count.

## Architecture Reference
- Services: SVC-03 (Core API — APN/Operator endpoints)
- API Endpoints: API-020 to API-027, API-030 to API-035
- Source: docs/architecture/api/_index.md (Operators, APNs sections)

## Screen Reference
- SCR-030: APN List — card grid with SIM count, traffic summary, IP utilization bar
- SCR-032: APN Detail — config panel, IP pool stats, connected SIMs table, traffic chart
- SCR-040: Operator List — card grid with health indicator, SIM count, protocol type
- SCR-041: Operator Detail — health timeline, circuit breaker state, failover policy, SoR config, traffic chart

## Acceptance Criteria
- [ ] APN list: card grid layout with name, operator, SIM count, traffic volume, IP pool utilization bar
- [ ] APN list: filter by operator, search by name
- [ ] APN detail: configuration panel (name, operator, network config)
- [ ] APN detail: IP pool stats (total, used, available, utilization %)
- [ ] APN detail: connected SIMs table (paginated, link to SIM detail)
- [ ] APN detail: traffic chart (24h trend, bytes in/out)
- [ ] Operator list: card grid with health status indicator (green/yellow/red dot)
- [ ] Operator list: each card shows SIM count, protocol type, last health check
- [ ] Operator detail: health history timeline (from TBL-23)
- [ ] Operator detail: circuit breaker state (closed/open/half-open) with visual indicator
- [ ] Operator detail: failover policy config (reject/fallback/queue)
- [ ] Operator detail: supported RAT types, SoR priority
- [ ] Operator detail: traffic chart (auth rate, error rate over 24h)
- [ ] Operator detail: "Test Connection" button (API-024)
- [ ] Health status updates via WebSocket operator.health_changed

## Dependencies
- Blocked by: STORY-041 (scaffold), STORY-042 (auth), STORY-009 (operator API), STORY-010 (APN API)
- Blocks: None

## Test Scenarios
- [ ] APN list loads with card grid, IP utilization bars rendered
- [ ] APN filter by operator → filtered cards
- [ ] APN detail → config, IP pool, SIMs, chart all displayed
- [ ] Operator list shows health indicators matching API data
- [ ] Operator health changes via WebSocket → card updated in real-time
- [ ] Operator detail circuit breaker shows correct state
- [ ] Test connection button → loading → success/failure message
- [ ] Operator detail traffic chart renders 24h auth rate data
- [ ] Empty APN list → "No APNs configured" with create button

## Effort Estimate
- Size: L
- Complexity: Medium

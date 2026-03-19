# STORY-034: Usage Analytics Dashboards

## User Story
As an analyst, I want time-series usage analytics with breakdowns by operator, APN, and RAT type, so that I can understand traffic patterns and identify top consumers.

## Description
Usage analytics dashboard powered by TimescaleDB continuous aggregates. Time-series charts show data volume, session count, and auth count over configurable periods. Breakdown by operator, APN, RAT type. Top consumers list. Period selector (1h, 24h, 7d, 30d, custom). All queries use pre-aggregated data for sub-second response times on large datasets.

## Architecture Reference
- Services: SVC-07 (Analytics Engine — internal/analytics)
- API Endpoints: API-111
- Database Tables: TBL-18 (cdrs), TBL-17 (sessions), plus continuous aggregates
- Source: docs/architecture/api/_index.md (Analytics section)

## Screen Reference
- SCR-011: Analytics Usage — time-series charts, operator/APN/RAT breakdown, top consumers table

## Acceptance Criteria
- [ ] GET /api/v1/analytics/usage returns time-series data for requested period
- [ ] Supported periods: 1h (1min buckets), 24h (15min), 7d (1h), 30d (6h), custom
- [ ] Metrics: total_bytes, total_sessions, total_auths, unique_sims
- [ ] Group by: operator, apn, rat_type (selectable)
- [ ] TimescaleDB continuous aggregates for common periods (hourly, daily, monthly)
- [ ] Continuous aggregate refresh policy: real-time for last 2 hours, scheduled for older
- [ ] Top consumers: top 20 SIMs by data usage for selected period
- [ ] Filter: by operator_id, apn_id, rat_type, sim_segment_id
- [ ] Response includes: time_series (array of {timestamp, value}), totals, breakdowns
- [ ] Sub-second query response for 30-day period with millions of CDRs
- [ ] Comparison mode: compare current period with previous period (delta/percentage)

## API Contract

| Ref | Method | Path | Request | Response (data) | Auth | Status Codes |
|-----|--------|------|---------|-----------------|------|-------------|
| API-111 | GET | /api/v1/analytics/usage | `?period&from&to&group_by&operator_id&apn_id&rat_type&segment_id` | `{time_series:[{ts,bytes,sessions,auths}],totals:{},breakdowns:{},top_consumers:[],comparison?:{}}` | JWT(analyst+) | 200 |

## Dependencies
- Blocked by: STORY-032 (CDR processing — data source), STORY-002 (TimescaleDB extension)
- Blocks: STORY-048 (frontend analytics pages)

## Test Scenarios
- [ ] Usage for last 24h → time series with 15min buckets, totals correct
- [ ] Group by operator → separate series per operator
- [ ] Group by RAT type → separate series per RAT
- [ ] Top consumers → top 20 SIMs by bytes, descending
- [ ] Filter by operator → only that operator's data returned
- [ ] 30-day query on 10M CDRs → response under 1 second (continuous aggregate)
- [ ] Comparison mode → current vs previous period with delta percentage
- [ ] Custom date range → correct bucket size auto-calculated
- [ ] Empty period → empty time series with zero totals

## Effort Estimate
- Size: L
- Complexity: Medium

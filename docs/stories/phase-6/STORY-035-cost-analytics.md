# STORY-035: Cost Analytics & Optimization

## User Story
As an analyst, I want cost analytics with total spend, per-carrier breakdown, per-SIM cost tracking, and optimization suggestions, so that I can reduce connectivity expenses.

## Description
Cost analytics dashboard: total cost over time, carrier cost comparison, per-SIM cost breakdown, cost per MB by operator/RAT, and automated optimization suggestions (e.g., "Switch 500 SIMs from Operator A to Operator B to save $2,000/month"). Data sourced from rated CDRs (TBL-18.cost_amount).

## Architecture Reference
- Services: SVC-07 (Analytics Engine — internal/analytics/cost)
- API Endpoints: API-112
- Database Tables: TBL-18 (cdrs — cost_amount, cost_currency), TBL-05 (operators — rate config)
- Source: docs/architecture/api/_index.md (Analytics section)

## Screen Reference
- SCR-012: Analytics Cost — cost cards, carrier comparison chart, optimization suggestions panel

## Acceptance Criteria
- [ ] GET /api/v1/analytics/cost returns cost analytics for requested period
- [ ] Total cost: sum of cost_amount across all CDRs in period
- [ ] Carrier cost breakdown: cost per operator with percentage share
- [ ] Cost per MB: average cost/MB per operator, per RAT type
- [ ] Per-SIM cost: top 20 most expensive SIMs in period
- [ ] Cost trend: time-series of daily/monthly cost
- [ ] Cost comparison: current period vs previous period (absolute + percentage delta)
- [ ] Optimization suggestions engine:
  - Identify SIMs on expensive operator when cheaper operator available
  - Identify SIMs with low usage on high-tier plans
  - Identify inactive SIMs still incurring standing charges
  - Calculate potential savings per suggestion
- [ ] Suggestions include: description, affected_sim_count, potential_savings, action (operator switch, plan downgrade, terminate)
- [ ] Filter: by operator_id, apn_id, rat_type, segment_id
- [ ] Cost data aggregated via TimescaleDB continuous aggregates

## API Contract

| Ref | Method | Path | Request | Response (data) | Auth | Status Codes |
|-----|--------|------|---------|-----------------|------|-------------|
| API-112 | GET | /api/v1/analytics/cost | `?period&from&to&operator_id&apn_id&rat_type&segment_id` | `{total_cost,currency,by_operator:{},cost_per_mb:{},top_expensive_sims:[],trend:[],comparison:{},suggestions:[]}` | JWT(analyst+) | 200 |

## Dependencies
- Blocked by: STORY-032 (CDR processing with rated costs)
- Blocks: STORY-048 (frontend cost analytics page)

## Test Scenarios
- [ ] Total cost for 30 days → sum of all CDR cost_amount values
- [ ] Carrier breakdown → cost per operator with correct percentages
- [ ] Cost per MB per RAT → different rates for 4G vs 5G
- [ ] Top expensive SIMs → sorted by cost descending
- [ ] Cost trend → daily aggregates as time series
- [ ] Comparison → current month vs previous month delta
- [ ] Optimization: 500 SIMs cheaper on Operator B → suggestion generated with savings
- [ ] Optimization: 100 inactive SIMs → suggestion to terminate with savings
- [ ] Filter by operator → only that operator's costs

## Effort Estimate
- Size: L
- Complexity: Medium

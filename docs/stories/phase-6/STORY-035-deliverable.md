# Deliverable: STORY-035 — Cost Analytics & Optimization

## Summary

Implemented cost analytics API with optimization suggestions engine. Total cost, per-carrier breakdown, cost per MB by operator/RAT, top expensive SIMs, cost trend, and comparison mode. Optimization engine identifies SIMs on expensive operators, inactive SIMs, and low-usage SIMs with potential savings calculations.

## Files Changed

### New Files
| File | Purpose |
|------|---------|
| `internal/store/cost_analytics.go` | 8 query methods for cost data |
| `internal/analytics/cost/service.go` | Cost service with optimization suggestions engine |
| `internal/analytics/cost/service_test.go` | 12 service tests |

### Modified Files
| File | Change |
|------|--------|
| `internal/api/analytics/handler.go` | Added GetCost handler |
| `internal/api/analytics/handler_test.go` | 10 cost handler tests |
| `internal/gateway/router.go` | Cost analytics route (analyst+) |
| `cmd/argus/main.go` | Wired CostAnalyticsStore + CostService |

## API Endpoints
| Ref | Method | Path | Auth | Description |
|-----|--------|------|------|-------------|
| API-112 | GET | `/api/v1/analytics/cost` | analyst+ | Cost analytics with optimization suggestions |

## Key Features
- Total cost with carrier breakdown and percentage shares
- Cost per MB per operator per RAT type
- Top 20 most expensive SIMs
- Daily/monthly cost trend time-series
- Comparison mode with delta percentages
- 3 optimization suggestion types: operator_switch, inactive_sims, low_usage
- Suggestions include affected_sim_count, potential_savings, action

## Test Coverage
- 22 new tests (12 service + 10 handler)
- All packages passing, 0 regressions

# Deliverable: STORY-034 — Usage Analytics Dashboards

## Summary

Implemented usage analytics API with TimescaleDB continuous aggregates. Time-series data for data volume, session count, auth count, and unique SIMs over configurable periods (1h/24h/7d/30d/custom). Breakdowns by operator, APN, RAT type. Top 20 consumers. Comparison mode with delta percentages. Pre-aggregated views for sub-second queries on large datasets.

## Files Changed

### New Files
| File | Purpose |
|------|---------|
| `internal/store/usage_analytics.go` | Store — GetTimeSeries, GetTotals, GetBreakdowns, GetTopConsumers |
| `internal/store/usage_analytics_test.go` | 27 tests for period resolution, bucket mapping |
| `internal/api/analytics/handler.go` | GET /api/v1/analytics/usage handler |
| `internal/api/analytics/handler_test.go` | 12 handler tests |
| `migrations/20260322000002_usage_analytics_aggregates.up.sql` | Monthly continuous aggregate, real-time enabled |
| `migrations/20260322000002_usage_analytics_aggregates.down.sql` | Down migration |

### Modified Files
| File | Change |
|------|--------|
| `internal/gateway/router.go` | Analytics usage route (analyst+) |
| `cmd/argus/main.go` | Wired UsageAnalyticsStore and AnalyticsHandler |

## API Endpoints
| Ref | Method | Path | Auth | Description |
|-----|--------|------|------|-------------|
| API-111 | GET | `/api/v1/analytics/usage` | analyst+ | Time-series usage analytics with breakdowns |

## Key Features
- Period-to-aggregate-view resolution: 1h→raw, 24h→cdrs_hourly, 7d/30d→cdrs_daily
- TimescaleDB continuous aggregates with real-time aggregation
- Group by operator/apn/rat_type with SQL injection prevention (dimension allowlist)
- Top 20 consumers by data usage
- Comparison mode: current vs previous period with delta percentages
- Gate fix: segment_id filter removed (dead code), column mappings corrected

## Test Coverage
- 39 new tests across 2 test files
- All packages passing, 0 regressions

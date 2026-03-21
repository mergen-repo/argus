# Deliverable: STORY-032 — CDR Processing & Rating Engine

## Summary

Implemented CDR (Call Detail Record) processing pipeline with rating engine. RADIUS/Diameter/5G SBA accounting events flow via NATS into TimescaleDB hypertable. Rating engine calculates cost per CDR based on operator rates, RAT type multiplier, time-of-day tariff, and volume tiers. REST API for CDR listing and CSV export as background job.

## Files Changed

### New Files
| File | Purpose |
|------|---------|
| `internal/analytics/cdr/rating.go` | Rating engine — cost calculation with 4 multiplier factors |
| `internal/analytics/cdr/rating_test.go` | 14 rating engine tests |
| `internal/analytics/cdr/consumer.go` | NATS consumer — subscribes to session events, creates CDRs |
| `internal/analytics/cdr/consumer_test.go` | Consumer unit tests |
| `internal/store/cdr.go` | CDR store — Create, CreateIdempotent, List, CostAggregation, Export |
| `internal/store/cdr_test.go` | Store tests |
| `internal/api/cdr/handler.go` | REST handler — GET /api/v1/cdrs, POST /api/v1/cdrs/export |
| `internal/api/cdr/handler_test.go` | Handler tests |
| `internal/job/cdr_export.go` | Background CDR export processor (CSV) |
| `migrations/20260322000001_cdr_dedup_index.up.sql` | Deduplication unique index |
| `migrations/20260322000001_cdr_dedup_index.down.sql` | Down migration |

### Modified Files
| File | Change |
|------|--------|
| `internal/job/types.go` | Added JobTypeCDRExport constant |
| `internal/gateway/router.go` | Registered CDR routes (analyst+ role) |
| `cmd/argus/main.go` | Wired CDR store, consumer, handler, export processor |

## API Endpoints
| Ref | Method | Path | Auth | Description |
|-----|--------|------|------|-------------|
| API-114 | GET | `/api/v1/cdrs` | analyst+ | List CDRs with time-range filter, pagination |
| API-115 | POST | `/api/v1/cdrs/export` | analyst+ | Export CDRs to CSV as background job |

## Key Features
- Rating engine: operator base rate × RAT multiplier × time-of-day × volume tier
- NATS consumer: QueueSubscribe on session.started/updated/ended
- Protocol-agnostic: RADIUS, Diameter, 5G SBA all produce CDRs
- Deduplication: ON CONFLICT DO NOTHING on (session_id, timestamp, record_type)
- Cost aggregation: per operator per day/month via continuous aggregate
- CSV export: streaming CDR export as background job

## Test Coverage
- 29 new tests across 5 test files
- 825 total tests passing, 0 failures, 0 regressions
- All 12 acceptance criteria covered

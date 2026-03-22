# Deliverable: STORY-036 — Anomaly Detection Engine

## Summary

Implemented rule-based anomaly detection engine with real-time and batch detection. SIM cloning (2+ NAS IPs within 5min), auth flood (>100/IMSI/min), NAS flood (>1000/NAS/min) detected in real-time via Redis sliding windows. Data usage spikes (>3× 30-day average) detected hourly via batch job. Critical anomalies auto-suspend SIM and trigger alerts. Bulk job source filtering prevents false positives from STORY-030 operations.

## Files Changed

### New Files
| File | Purpose |
|------|---------|
| `internal/analytics/anomaly/types.go` | Types, severities, states, ThresholdConfig |
| `internal/analytics/anomaly/detector.go` | Real-time detector: SIM cloning, auth flood, NAS flood |
| `internal/analytics/anomaly/detector_test.go` | 9 detector tests |
| `internal/analytics/anomaly/batch.go` | Batch detector: hourly data spike detection |
| `internal/analytics/anomaly/engine.go` | Engine: NATS subscriber, anomaly creation, auto-suspend, alerts |
| `internal/analytics/anomaly/engine_test.go` | 5 engine tests |
| `internal/store/anomaly.go` | AnomalyStore: CRUD, list, state transitions, dedup |
| `internal/store/anomaly_test.go` | State transition tests |
| `internal/api/anomaly/handler.go` | REST handler: list, get, patch (state transitions) |
| `internal/api/anomaly/handler_test.go` | 9 handler tests |
| `internal/job/anomaly_batch.go` | Batch processor for cron scheduler |
| `migrations/20260322000003_anomalies.up.sql` | Anomalies table with indexes |
| `migrations/20260322000003_anomalies.down.sql` | Down migration |

### Modified Files
| File | Change |
|------|--------|
| `internal/bus/nats.go` | SubjectAnomalyDetected, SubjectAuthAttempt |
| `internal/job/types.go` | JobTypeAnomalyBatch |
| `internal/gateway/router.go` | Anomaly API routes (analyst+) |
| `cmd/argus/main.go` | Wired engine, batch detector, job processor, cron |

## API Endpoints
| Ref | Method | Path | Auth | Description |
|-----|--------|------|------|-------------|
| API-113 | GET | `/api/v1/analytics/anomalies` | analyst+ | List anomalies with filters |
| — | GET | `/api/v1/analytics/anomalies/:id` | analyst+ | Get anomaly details |
| — | PATCH | `/api/v1/analytics/anomalies/:id` | analyst+ | Update state (acknowledge/resolve/false_positive) |

## Key Features
- 4 anomaly types: SIM_CLONING (critical), DATA_SPIKE (high), AUTH_FLOOD (high), NAS_FLOOD (high)
- Real-time detection via Redis sliding window sorted sets
- Batch hourly detection via CDR aggregates
- Auto-suspend SIM on critical SIM cloning
- Alert publishing to NATS (alert.triggered → notification service)
- State machine: open → acknowledged → resolved / false_positive
- Configurable thresholds per tenant
- Bulk job source filtering (STORY-030 post-note)

## Test Coverage
- 25 new tests across 5 test files
- 899 total tests passing, 0 regressions
- Gate fixes: SQL parameter bug in HasRecentAnomaly, error comparison convention

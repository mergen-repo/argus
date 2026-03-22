# STORY-036 Gate Review: Anomaly Detection Engine

**Date:** 2026-03-22
**Result:** PASS
**Tests:** 899 total (25 story-specific), 0 failures

---

## Pass 1 — Structural Integrity

| Check | Result |
|-------|--------|
| Files present per plan | PASS — All 14 files present (9 new, 5 modified) |
| Migration up/down pair | PASS — `20260322000003_anomalies.up.sql` / `.down.sql` |
| Build (`go build ./...`) | PASS — Clean compile, zero errors |
| Package layout matches project conventions | PASS — `internal/analytics/anomaly/`, `internal/api/anomaly/`, `internal/store/anomaly.go`, `internal/job/anomaly_batch.go` |

## Pass 2 — Acceptance Criteria Verification

| AC# | Criterion | Status | Evidence |
|-----|-----------|--------|----------|
| 1 | SIM cloning: 2+ NAS IPs within 5min -> critical | PASS | `detector.go:checkSIMCloning()` uses Redis sorted set with configurable window (default 300s), returns `SeverityCritical`. Test: `TestCheckSIMCloning_Detected` |
| 2 | Data spike: >3x average -> high | PASS | `batch.go:RunDataSpikeDetection()` queries `FindDataSpikeCandidates(multiplier)`, configurable multiplier (default 3.0). Returns `SeverityHigh`. Test: `TestBatchDetector_RunDataSpikeDetection` |
| 3 | Auth flood: >100/IMSI/min -> high | PASS | `detector.go:checkAuthFlood()` uses sliding window, configurable max (default 100) and window (default 60s). Test: `TestCheckAuthFlood_Detected` |
| 4 | NAS flood: >1000/NAS/min | PASS | `detector.go:checkNASFlood()` uses sliding window, configurable max (default 1000) and window (default 60s). Test: `TestCheckNASFlood_Detected` |
| 5 | Anomaly record with type, severity, JSONB details | PASS | Migration creates `anomalies` table with `type`, `severity`, `details JSONB`, `detected_at`, `resolved_at`, `state` columns. CHECK constraints on type and severity values |
| 6 | GET /api/v1/analytics/anomalies with filters | PASS | `handler.go:List()` supports `?cursor&limit&type&severity&state&sim_id&from&to`. Router registered at correct path with `analyst` role |
| 7 | States: open -> acknowledged -> resolved / false_positive | PASS | `store/anomaly.go:validAnomalyTransitions` map enforces transitions. Test: `TestAnomalyStateTransitions` covers all 10 valid/invalid transitions |
| 8 | Critical -> auto-suspend SIM, alert event | PASS | `engine.go:handleCriticalAnomaly()` calls `SIMSuspender.Suspend()` when `AutoSuspendOnCloning=true`. Alert published via `publishEvents()` |
| 9 | alert -> notification service | PASS | Engine publishes to `SubjectAlertTriggered`. Notification service subscribes to this subject in `main.go:313` |
| 10 | Real-time + batch detection | PASS | Real-time: `engine.go:Start()` subscribes to `argus.events.auth.attempt`. Batch: `anomaly_batch.go` registered as job processor, cron scheduled `@hourly` in `main.go:288` |
| 11 | False positive marking | PASS | `handler.go:UpdateState()` accepts `false_positive` state. State transition validated in store |
| 12 | Configurable thresholds | PASS | `types.go:ThresholdConfig` struct with all threshold fields. `DefaultThresholds()` provides defaults. `SetThresholds()` on both detector and batch detector |

## Pass 3 — Code Quality

| Check | Result | Notes |
|-------|--------|-------|
| Error handling | PASS | All DB/Redis errors wrapped with context, graceful nil-redis handling |
| SQL injection safety | PASS | Parameterized queries throughout, dynamic WHERE built with `$N` placeholders |
| Tenant scoping | PASS | All anomaly queries scoped by `tenant_id` |
| Cursor pagination | PASS | `ListByTenant` uses UUID cursor with `detected_at DESC, id DESC` ordering |
| Deduplication | PASS | `HasRecentAnomaly()` prevents duplicate anomaly creation within window |
| Bulk job filtering | PASS | `FilterBulkJobs` flag in thresholds skips `source=bulk_job` events (per STORY-030 notes) |
| Graceful degradation | PASS | Nil redis returns nil results, nil publisher skips events |
| Interface segregation | PASS | `AnomalyCreator`, `AlertPublisher`, `SIMSuspender` interfaces for testability |

## Pass 4 — Test Quality

| Metric | Value |
|--------|-------|
| Story test count | 25 |
| Test packages | 3 (store, analytics/anomaly, api/anomaly) |
| Unit tests | All 25 pass |
| Edge cases covered | Nil redis, bulk job filter, same-NAS no detection, invalid state, invalid UUID, invalid date formats |
| Mocks | `mockAnomalyStore`, `mockPublisher`, `mockSuspender` — clean interface mocks |

## Pass 5 — Integration & Wiring

| Check | Result |
|-------|--------|
| main.go wiring | PASS — Store, detector, engine, batch detector, handler all created and wired |
| Engine lifecycle | PASS — Start() subscribes to NATS, Stop() unsubscribes. Shutdown in main.go:568-569 |
| Router registration | PASS — GET/GET/{id}/PATCH/{id} under `/api/v1/analytics/anomalies` with `analyst` role |
| Job registration | PASS — `anomalyBatchProc` registered with job runner |
| Cron scheduling | PASS — `anomaly_batch_detection` scheduled `@hourly` |
| NATS subjects | PASS — `SubjectAnomalyDetected` and `SubjectAuthAttempt` added to bus/nats.go |
| Notification integration | PASS — Alert events published to `SubjectAlertTriggered`, notification service subscribes |

## Pass 6 — UI

Skipped (backend-only story).

## Issues Found & Fixed

| # | Severity | Description | Fix |
|---|----------|-------------|-----|
| 1 | HIGH | `HasRecentAnomaly` NULL-sim branch used `$3`/`$4` placeholders but only passed 3 args (`tenantID`, `anomalyType`, `cutoff`) — would cause runtime SQL error for NAS flood dedup | Fixed: Changed to `$2`/`$3` in `internal/store/anomaly.go:372` |
| 2 | LOW | Error comparisons used `==` instead of `errors.Is()` (project convention) in `handler.go` | Fixed: Changed to `errors.Is()` with `errors` import added |

## Files Modified During Gate

- `internal/store/anomaly.go` — Fixed SQL parameter numbering in `HasRecentAnomaly` null-sim branch
- `internal/api/anomaly/handler.go` — Changed `==` to `errors.Is()` for error comparisons, added `errors` import

---

## GATE SUMMARY

**STORY-036: Anomaly Detection Engine — PASS**

- 12/12 ACs verified
- 899 tests, 0 failures
- 2 issues found and fixed (1 high: SQL parameter bug, 1 low: error comparison convention)
- Build: clean
- All anomaly detection types implemented: SIM cloning, data spike, auth flood, NAS flood
- Real-time (Redis sliding window + NATS subscription) and batch (@hourly cron) detection operational
- Critical anomaly auto-suspend with configurable thresholds
- Full API with cursor pagination, state transitions, and analyst role protection

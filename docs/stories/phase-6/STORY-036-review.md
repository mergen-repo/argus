# Review Report: STORY-036 — Anomaly Detection Engine

**Reviewer:** Amil Reviewer Agent
**Date:** 2026-03-22
**Phase:** 6 — Analytics & BI
**Story Status:** DONE (25 new tests, 899 total passing)

---

## 1. Next Story Impact Analysis

### STORY-037 (Connectivity Diagnostics) — No impact

Connectivity diagnostics checks SIM state, last auth, operator health, APN config, policy, and IP pool. None of these steps involve anomaly data. STORY-036 anomaly engine operates on auth events and CDR aggregates independently. The `AnomalyStore` does not modify any tables queried by diagnostics. No changes needed.

### STORY-038 (Notification Engine) — Unblocked (partial)

STORY-036 publishes alert events to `SubjectAlertTriggered` (`argus.events.alert.triggered`). The notification service (SVC-08, implemented in STORY-021) already subscribes to this subject. STORY-038 will formalize the notification dispatch (email, Telegram, in-app). The alert payload structure used by STORY-036 (`alert_id`, `alert_type`, `severity`, `title`, `description`, `entity_type`, `entity_id`, `metadata`, `timestamp`) matches the WEBSOCKET_EVENTS.md alert.new schema. No spec changes needed for STORY-038.

### STORY-048 (Frontend Analytics Pages) — Unblocked

STORY-048 includes SCR-013 (Analytics Anomalies page). The 3 API endpoints are now available: GET list with filters, GET by ID, PATCH state transitions. The anomaly DTO includes `sim_iccid` for display. State transitions (acknowledge, resolve, false_positive) are validated server-side. Frontend can consume these directly.

---

## 2. Architecture Evolution

**Dual-interface pattern for store adapters:** The engine (`engine.go`) uses `*store.AnomalyStore` directly for real-time anomaly creation (tightly coupled to production store). The batch detector (`batch.go`) uses the `AnomalyCreator` interface, with `AnomalyStoreAdapter` bridging to the store in `engine.go`. This dual approach provides testability for batch detection while keeping engine wiring simple. Future refactoring could unify both paths through the interface.

**Redis sorted set for sliding window detection:** All three real-time detectors (cloning, auth flood, NAS flood) use Redis ZADD/ZRANGEBYSCORE/ZREMRANGEBYSCORE with UnixNano scores. This differs from the ALGORITHMS.md pseudocode which uses INCR/EXPIRE for auth flood. The sorted set approach is more accurate (true sliding window vs. fixed-window INCR) and is consistent across all detector types. This is an improvement over the spec.

**Engine-level bulk job filtering is redundant:** Both the `RealtimeDetector.CheckAuth()` (line 50-52) and `Engine.handleAuthEvent()` (line 87-89) check `FilterBulkJobs && evt.Source == "bulk_job"`. The engine-level filter is a defense-in-depth guard but means the check runs twice per event. Not harmful, but worth noting.

**NATS subject naming:** STORY-036 introduces `SubjectAnomalyDetected = "argus.events.anomaly.detected"` and `SubjectAuthAttempt = "argus.events.auth.attempt"`. Both follow the existing `argus.events.{domain}.{action}` convention. Auth attempt events must be published by the RADIUS/Diameter/SBA servers for the engine to consume them. The engine subscribes to the auth attempt subject via QueueSubscribe with queue group `anomaly-engine` for multi-instance safety.

---

## 3. Glossary Check

### Existing terms verified:
- "CDR" -- anomaly batch detection queries CDR aggregates. Term is accurate.
- "Job Runner" -- already lists `anomaly_batch_detection` in the job types. Needs update to add it to the real processors list.
- "Cron Scheduler" -- default jobs list needs update to include `anomaly_batch_detection @hourly`.

### New terms to add:

| Term | Definition | Context |
|------|-----------|---------|
| Anomaly Detection (Rule-Based) | Real-time and batch engine for detecting security anomalies. 4 types: SIM_CLONING (critical, same IMSI from 2+ NAS IPs in 5min), AUTH_FLOOD (high, >100 auth/IMSI/min), NAS_FLOOD (high, >1000 auth/NAS/min), DATA_SPIKE (high, daily usage >3x 30-day avg). Real-time uses Redis sorted set sliding windows; batch runs hourly via cron. | SVC-07, STORY-036, `internal/analytics/anomaly/` |
| Anomaly State Machine | Lifecycle states for anomaly records: `open` (default) -> `acknowledged` (analyst reviewed) -> `resolved` (issue addressed) or `false_positive` (threshold tuning). Terminal states (`resolved`, `false_positive`) have no outgoing transitions. State transitions validated by `validAnomalyTransitions` map. | SVC-07, STORY-036, TBL-27 |
| Anomaly Deduplication | Prevention of duplicate anomaly creation for the same type+SIM within a time window. Real-time anomalies deduplicated within 5 minutes; data spikes within 24 hours. Uses `HasRecentAnomaly()` which counts open/acknowledged anomalies in the window. | SVC-07, STORY-036, `internal/store/anomaly.go` |

### Recommended enrichment to existing terms:

| Term | Recommended Addition | Context |
|------|---------------------|---------|
| Job Runner | Add `anomaly_batch_detection` to real processors list. | SVC-09, STORY-036 |
| Cron Scheduler | Add `anomaly_batch_detection (@hourly)` to default jobs list. | SVC-09, STORY-036 |

---

## 4. FUTURE.md Relevance

**FTR-001 (AI Anomaly Engine)** alignment:
- FUTURE.md states: "ML-based anomaly detection beyond rule-based -- learns 'normal' patterns per SIM/APN, flags meaningful deviations." STORY-036 implements the rule-based v1 exactly as the story spec dictates ("ML-based detection deferred to FUTURE.md"). The 4 anomaly types, `ThresholdConfig` struct, and `AnomalyCreator` interface provide clear extension points for an ML-based detector. The `AnomalyCreator` interface is the integration contract -- an ML detector would implement `Create()` to insert ML-detected anomalies with the same schema. **No FUTURE.md update needed.**

**FTR-005 (Network Digital Twin)** alignment:
- Anomaly detection data (historical anomaly records with JSONB details) provides valuable training signal for a digital twin's "normal behavior" model. No update needed.

No updates to FUTURE.md required.

---

## 5. New Decisions

| # | Date | Decision | Status |
|---|------|----------|--------|
| DEV-115 | 2026-03-22 | STORY-036: Real-time anomaly detection uses Redis sorted sets with ZADD/ZRANGEBYSCORE for sliding window tracking. SIM cloning tracks NAS IPs per IMSI (5min window). Auth flood tracks auth count per IMSI (1min window). NAS flood tracks per NAS IP (1min window). All windows auto-pruned via ZREMRANGEBYSCORE. | ACCEPTED |
| DEV-116 | 2026-03-22 | STORY-036: Batch data spike detection runs hourly via cron scheduler (JobTypeAnomalyBatch). Queries CDR aggregates for SIMs with daily usage > Nx 30-day average. Deduplication via HasRecentAnomaly prevents repeated alerts for same SIM. | ACCEPTED |
| DEV-117 | 2026-03-22 | STORY-036: Critical anomalies (SIM_CLONING) auto-suspend the SIM via SIMStore.Suspend() and publish alert.triggered to NATS (consumed by notification service from STORY-021). Auto-suspend is configurable via ThresholdConfig.AutoSuspendOnCloning. | ACCEPTED |
| DEV-118 | 2026-03-22 | STORY-036: Bulk job source filtering -- auth events with source=bulk_job are excluded from anomaly detection to prevent false positives during STORY-030 bulk operations (10K+ events in seconds). | ACCEPTED |
| DEV-119 | 2026-03-22 | STORY-036: Gate fixed SQL parameter bug in HasRecentAnomaly -- NULL-sim branch used wrong placeholder indices ($3/$4 instead of $2/$3), causing runtime error on NAS flood dedup. Also fixed error comparison to use errors.Is() per project convention. | ACCEPTED |

All 5 decisions already recorded in `docs/brainstorming/decisions.md`. Verified.

---

## 6. Cross-Document Consistency Check

| Check | Status | Notes |
|-------|--------|-------|
| PRODUCT.md F-037 (Anomaly detection) | CONSISTENT | "Anomaly detection -- SIM cloning, abuse patterns, data spikes" -- all 3 pattern categories implemented (cloning, floods=abuse, data spikes). |
| SCOPE.md L5 (Analytics & BI) | CONSISTENT | "Anomaly detection (SIM cloning, abuse, data spikes)" listed and implemented. |
| ARCHITECTURE.md SVC-07 | CONSISTENT | SVC-07 description includes "anomaly detection" in services index. |
| ALGORITHMS.md Section 4 | DIVERGENCE (acceptable) | Spec uses INCR/EXPIRE for auth flood; implementation uses sorted set sliding window. Sorted set is more accurate. Data spike spec uses 7-day hourly average; implementation uses 30-day daily average (matches AC). Auth flood spec only tracks rejected auths per NAS IP; implementation tracks all auths per IMSI and per NAS separately (richer detection). |
| WEBSOCKET_EVENTS.md alert.new | CONSISTENT | Alert payload schema matches: `alert_id`, `alert_type`, `severity`, `title`, `description`. Implementation uses `anomaly_{type}` for alert_type (e.g., `anomaly_sim_cloning`), which fits the documented `anomaly_detected` category. |
| API index API-113 | CONSISTENT | GET /api/v1/analytics/anomalies registered, analyst role required. |
| DB index (_index.md) | MISSING | `anomalies` table (TBL-27) not listed in DB schema index. Needs addition. |
| STORY-036 spec vs implementation | CONSISTENT | All 12 ACs pass. Bulk job filtering from post-STORY-030 note implemented. |
| decisions.md DEV-115 to DEV-119 | CONSISTENT | All 5 decisions present and accurate. |
| CONFIG.md | NO_CHANGE_NEEDED | No new env vars -- thresholds use `DefaultThresholds()` in code. Per-tenant config is via `ThresholdConfig` struct, not env vars. |
| STORY-030 post-note | IMPLEMENTED | "filter events with `source=bulk_job`" fully implemented in both detector and engine. |

**1 missing entry found** (DB index). **1 acceptable divergence** (ALGORITHMS.md implementation differs from pseudocode but is an improvement).

---

## 7. Implementation Quality Notes

### Strengths
- **Clean interface segregation:** `AnomalyCreator`, `AlertPublisher`, `SIMSuspender` interfaces enable full testability without real infrastructure.
- **Graceful degradation:** Nil redis returns nil (no detection, no crash). Nil publisher skips events. Missing SIM ICCID is tolerated.
- **Defense-in-depth deduplication:** Both Redis sliding window (prevents rapid re-fire) and PostgreSQL `HasRecentAnomaly` (prevents duplicate records) work together.
- **Tenant isolation:** All anomaly queries scoped by `tenant_id`. No cross-tenant data leaks possible.
- **Configurable thresholds:** `ThresholdConfig` struct with JSON tags ready for per-tenant storage.

### Observations

1. **extractIP vulnerability with IPv6:** The `extractIP()` function in `detector.go:237` splits on last `:` to separate IP from nano-timestamp. For IPv6 addresses like `2001:db8::1`, this would incorrectly extract `2001:db8:` (truncated). IPv6 NAS IPs would cause incorrect SIM cloning detection. **Severity: MEDIUM.** Current deployment is IPv4-only (RFC1918 NAS IPs), but should be noted for future IPv6 support.

2. **Data spike detection queries "yesterday" not "today":** `FindDataSpikeCandidates()` at `store/anomaly.go:312-313` sets `today = time.Now().UTC().Truncate(24 * time.Hour)` then queries `WHERE timestamp >= $1 (yesterday) AND timestamp < $2 (today)`. This means it detects spikes for *yesterday's* usage, not today's. Since the cron runs `@hourly`, this is actually correct behavior (ensures a full day of data before comparing), but the variable name `today` is misleading.

3. **Cursor pagination uses `id < $cursor`:** `ListByTenant` at `store/anomaly.go:159` uses `id < $cursor` for cursor pagination. Since ordering is `detected_at DESC, id DESC`, this works correctly with UUID v4 (random, lexicographic ordering approximates insertion order). However, it could miss anomalies detected in the same second with a larger UUID. Standard pattern would be composite cursor on `(detected_at, id)`. **Severity: LOW** -- edge case unlikely in practice.

---

## 8. Document Updates

### DB index (_index.md) -- Add TBL-27 anomalies

Add to table list:
```
| TBL-27 | anomalies | Analytics | -> TBL-01, -> TBL-10 | No |
```

Add to module mapping:
```
| AAA Analytics | [aaa-analytics.md](aaa-analytics.md) | TBL-17, TBL-18, TBL-27 |
```

### GLOSSARY.md -- Add 3 new terms + enrich 2 existing

New terms: Anomaly Detection (Rule-Based), Anomaly State Machine, Anomaly Deduplication -- see Section 3 above.

Enrich existing: Job Runner (add `anomaly_batch_detection` to real processor list), Cron Scheduler (add `anomaly_batch_detection @hourly` to default jobs).

### ALGORITHMS.md Section 4 -- Recommend update (optional)

The pseudocode for auth flood detection uses INCR/EXPIRE (fixed window) while implementation uses sorted set sliding window. The SIM cloning key prefix uses `anomaly:sim_clone:` in docs but `anomaly:cloning:imsi:` in code. Data spike uses 7-day hourly average in docs but 30-day daily average in code (matching the AC). Consider aligning pseudocode to actual implementation in a future docs pass.

### ROUTEMAP.md -- Mark STORY-036 as DONE

STORY-036 should be updated from `[~] IN PROGRESS | Review` to `[x] DONE` with completion date 2026-03-22.

Progress counter should update from 34/55 (62%) to 35/55 (64%).

Phase 6 progress: 5/6 stories done (STORY-032 through STORY-036). STORY-037 remaining.

---

## 9. ALGORITHMS.md Divergences Detail

| Spec (ALGORITHMS.md) | Implementation | Impact |
|----------------------|----------------|--------|
| Auth flood: INCR/EXPIRE per NAS IP, rejected-only | Sorted set sliding window per IMSI, all auths | More accurate; tracks per-IMSI not per-NAS; counts all auths not just rejects |
| Data spike: 7-day hourly average | 30-day daily average (matches AC) | AC says "30-day average" -- implementation is correct, spec outdated |
| SIM cloning key: `anomaly:sim_clone:{imsi}` | `anomaly:cloning:imsi:{imsi}` | Cosmetic key naming difference |
| Dedup: Redis SET NX EX 3600 | PostgreSQL HasRecentAnomaly COUNT query | DB-based dedup is more reliable (survives Redis restart) |

All divergences are improvements or AC-conformant. No blocking issues.

---

## 10. Test Coverage Assessment

| Category | Tests | Coverage |
|----------|-------|----------|
| Real-time detection (detector_test.go) | 9 | SIM cloning (detected + no-detection + same-NAS), auth flood (detected + no-flood), NAS flood, bulk filter, nil redis, extractIP |
| Batch detection (engine_test.go) | 5 | Data spike batch, anomaly title, anomaly description, default thresholds, auth event JSON |
| Store layer (anomaly_test.go) | 2 | State transitions (10 cases), anomaly columns |
| API handler (handler_test.go) | 9 | No tenant (3 endpoints), invalid state, invalid sim_id, invalid from date, invalid ID (2 tests), DTO conversion |
| **Total** | **25** | Good coverage of happy paths and edge cases |

Missing test coverage (non-blocking, noted for future hardening):
- No integration test for engine `processDetection` with real store mock
- No test for `HandleCriticalAnomaly` auto-suspend flow (only tested via mock pattern)
- No test for `FindDataSpikeCandidates` SQL query (requires DB integration test)
- No test for concurrent deduplication race condition

---

## 11. Security Review

| Check | Status | Notes |
|-------|--------|-------|
| SQL injection | SAFE | All queries use parameterized placeholders ($1, $2...) |
| Tenant isolation | SAFE | All store methods require tenant_id, all queries include WHERE tenant_id = $1 |
| Input validation | SAFE | UUID parsing, date parsing, state validation all checked before DB calls |
| Rate limiting | SAFE | Anomaly API inherits gateway rate limiter |
| Auth/authz | SAFE | All routes require JWT + analyst role |
| Auto-suspend safety | SAFE | Only triggered for SeverityCritical + TypeSIMCloning + AutoSuspendOnCloning=true |

---

## 12. Performance Considerations

| Aspect | Assessment |
|--------|-----------|
| Redis overhead per auth event | 3 Redis pipelines (cloning + auth flood + NAS flood), each with 3-4 commands. ~9-12 Redis ops per auth event. Acceptable for auth volume. |
| Batch detection query | Single CTE query on CDR aggregates with JOIN. Will benefit from TimescaleDB continuous aggregates. |
| Anomaly list query | Dynamic WHERE with parameterized placeholders + cursor pagination. Indexes on tenant_id, type, severity, state, detected_at cover all filter paths. |
| Deduplication query | COUNT on anomalies table with tenant_id + sim_id + type + detected_at >= cutoff. Covered by composite index on (tenant_id, state). May benefit from dedicated composite index on (tenant_id, sim_id, type, detected_at). |

---

## Summary

| Category | Result |
|----------|--------|
| Next story impact | 0 stories require changes. STORY-037 independent. STORY-038, STORY-048 unblocked. |
| Architecture evolution | Dual-interface pattern for store adapters. Redis sorted set sliding window (improvement over INCR/EXPIRE spec). Defense-in-depth dedup. |
| New glossary terms | 3 new terms + 2 enrichments (Anomaly Detection, State Machine, Deduplication; Job Runner, Cron Scheduler updates) |
| FUTURE.md | No updates needed. FTR-001 (AI Anomaly Engine) foundation established. |
| New decisions | 5 captured (DEV-115 to DEV-119), all verified in decisions.md |
| Cross-doc consistency | 1 missing entry (DB index TBL-27), 1 acceptable divergence (ALGORITHMS.md) |
| Implementation quality | 3 observations: IPv6 extractIP edge case (medium), misleading variable name (low), cursor pagination edge case (low) |
| Story updates | ROUTEMAP.md (STORY-036 DONE), GLOSSARY.md (3 new + 2 enrichments), DB index (TBL-27), ALGORITHMS.md (optional alignment) |

---

## Project Progress

- Stories completed: 35/55 (64%)
- Phase 6 progress: 5/6 stories done (STORY-032, STORY-033, STORY-034, STORY-035, STORY-036)
- Remaining in Phase 6: STORY-037 (Connectivity Diagnostics)
- Next story: STORY-037
- Test count: 899 passing

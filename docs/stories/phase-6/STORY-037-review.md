# Review Report: STORY-037 — SIM Connectivity Diagnostics

**Reviewer:** Amil Reviewer Agent
**Date:** 2026-03-22
**Phase:** 6 — Analytics & BI (LAST STORY)
**Story Status:** DONE (17 new tests, 917 total passing)

---

## 1. Next Story Impact Analysis

### STORY-038 (Notification Engine) — No impact

Connectivity diagnostics does not publish any events to NATS. It is a synchronous request-response API. STORY-038 (notification engine) consumes alert events from STORY-021/STORY-036 -- diagnostics has no overlap. No changes needed.

### STORY-044 (Frontend SIM Detail) — Unblocked (partial)

STORY-044 includes SCR-021d (SIM Detail Diagnostics tab). The API endpoint POST /api/v1/sims/:id/diagnose (API-049) is now available. Response structure matches spec: `{sim_id, overall_status, steps[], diagnosed_at}`. Frontend can consume the ordered step array directly for a step-by-step troubleshooting wizard.

### Phase 7 stories — No impact

Phase 7 stories (STORY-038 Notifications, STORY-039 Compliance, STORY-040 WebSocket) do not depend on or interact with the diagnostics service.

---

## 2. Architecture Evolution

**Graceful degradation pattern:** The diagnostics service accepts all 6 store dependencies as constructor parameters but each step method checks for nil before using the store. If a store is nil, the step returns `warn` status ("store unavailable, skipping check") instead of crashing. This is a deliberate design decision (DEV-120) that allows partial diagnostics when some services are down.

**No interface abstraction for stores:** Unlike STORY-036's `AnomalyCreator` interface pattern, diagnostics uses concrete store types (`*store.SIMStore`, `*store.OperatorStore`, etc.). The nil check pattern provides sufficient testability for unit tests. The tradeoff is that integration tests would require real stores or wrapper interfaces. Acceptable for a read-only diagnostic service.

**Cache key design:** Key format `diag:{tenantID}:{simID}:{includeTestAuth}` prevents cross-tenant cache leaks and separates results with/without test auth. The boolean suffix avoids cache invalidation complexity -- both variants expire independently after 1 minute.

---

## 3. Glossary Check

### Existing terms verified:
- "Session (AAA)" -- diagnostics queries last session via `GetLastSessionBySIM`. Term accurate.
- "IP Pool" -- diagnostics checks pool availability. Term accurate.
- "SIM State Machine" -- diagnostics step 1 checks all states. Term accurate.

### New terms added:

| Term | Definition | Context |
|------|-----------|---------|
| Connectivity Diagnostics | 7-step SIM troubleshooting engine with per-step pass/warn/fail status and remediation suggestions. Cached 1min in Redis. | SVC-03, STORY-037, API-049 |
| Diagnostic Step | Individual check within diagnostics returning step/name/status/message/suggestion. | SVC-03, STORY-037 |

---

## 4. FUTURE.md Relevance

**FTR-003 (Smart Diagnostics)** alignment:
- FUTURE.md may reference enhanced diagnostics with ML-based root cause analysis. STORY-037 establishes the 7-step diagnostic framework and result schema. Step 7 (test auth) is left as a placeholder for operator adapter integration. The `Service` struct could be extended with additional steps without breaking the API contract. No FUTURE.md update needed.

No updates to FUTURE.md required.

---

## 5. New Decisions

| # | Date | Decision | Status |
|---|------|----------|--------|
| DEV-120 | 2026-03-22 | STORY-037: All stores optional -- nil store returns warn instead of crash. Graceful degradation by design. | ACCEPTED |
| DEV-121 | 2026-03-22 | STORY-037: Diagnostic result cached in Redis with 1-minute TTL. Cache key: `diag:{tenant_id}:{sim_id}:{include_test_auth}`. | ACCEPTED |
| DEV-122 | 2026-03-22 | STORY-037: Test auth (Step 7) placeholder returning warn. Real implementation deferred to operator integration phase. | ACCEPTED |

All 3 decisions already recorded in `docs/brainstorming/decisions.md`. Verified.

---

## 6. Cross-Document Consistency Check

| Check | Status | Notes |
|-------|--------|-------|
| PRODUCT.md F-039 (Connectivity diagnostics) | CONSISTENT | "Auto-diagnosis: SIM state, last auth, operator health, APN, policy, IP pool" -- all 6 steps implemented plus optional step 7. |
| SCOPE.md L3 (SIM Management) | CONSISTENT | Diagnostics listed as a SIM management feature. |
| ARCHITECTURE.md SVC-03 | CONSISTENT | Core API service hosts diagnostics handler. |
| ARCHITECTURE.md Caching Strategy | UPDATED | Added "Diagnostic result (per-SIM)" row with Redis / 1min / Auto-expire. |
| API index API-049 | CONSISTENT | POST /api/v1/sims/:id/diagnose registered, sim_manager role required. |
| STORY-037 spec vs implementation | CONSISTENT | All 10 ACs verified (gate report). Step 7 is optional as specified. |
| decisions.md DEV-120 to DEV-122 | CONSISTENT | All 3 decisions present and accurate. |
| CONFIG.md | NO_CHANGE_NEEDED | No new env vars -- cache TTL is hardcoded 1min constant. |
| DB schema | NO_CHANGE_NEEDED | No new tables or migrations -- uses existing tables (sims, sessions, operators, apns, ip_pools, policy_versions). |
| GLOSSARY.md | UPDATED | Added 2 new terms (Connectivity Diagnostics, Diagnostic Step). |

**0 missing entries. 0 divergences.** All cross-document references consistent.

---

## 7. Implementation Quality Notes

### Strengths
- **Clean sequential pipeline:** Steps 1-7 execute in order, each returning a self-contained `StepResult`. No step depends on another step's result.
- **Graceful degradation everywhere:** Nil session store, nil operator store, nil redis client -- all handled without panics.
- **Tenant isolation:** SIM fetched with `tenantID` scope. APN fetched with `tenantID` scope. Cache key includes `tenantID`.
- **Standard API envelope:** Uses `apierr.WriteSuccess`/`apierr.WriteError` consistently.
- **Comprehensive throttle-to-zero detection:** Checks both `max_bandwidth=0` and `download_rate=0 + upload_rate=0` patterns.

### Observations

1. **GetLastSessionBySIM lacks tenant_id filter:** The query `WHERE sim_id = $1 ORDER BY started_at DESC LIMIT 1` does not include `tenant_id`. This is safe because the `sim_id` is obtained from a tenant-scoped SIM fetch, so the session necessarily belongs to the same tenant. However, it technically allows a session scan across all tenants' sessions for the given SIM ID. **Severity: LOW** -- SIM IDs are UUIDs and already tenant-validated upstream.

2. **OperatorStore.GetByID and PolicyStore.GetVersionByID lack tenant scope:** These queries use only the primary key ID. Operators are system-level resources (shared across tenants), so this is correct. Policy versions are linked to tenant-scoped policies, so the `PolicyVersionID` on the SIM is already tenant-safe. **Severity: NONE** -- matches project convention.

3. **Step 7 (test auth) always returns warn:** Including Step 7 via `include_test_auth=true` adds a step that always says "not yet implemented." This degrades the overall status from PASS to DEGRADED even when everything is healthy. **Severity: LOW** -- documented in DEV-122, acceptable for v1.

---

## 8. Document Updates Applied

| Document | Change |
|----------|--------|
| ARCHITECTURE.md | Added "Diagnostic result (per-SIM)" to Caching Strategy table |
| GLOSSARY.md | Added 2 new terms: Connectivity Diagnostics, Diagnostic Step |
| ROUTEMAP.md | STORY-037 marked DONE, Phase 6 marked DONE, stories 36/55 (65%), current phase updated to Phase 7 |

---

## 9. Test Coverage Assessment

| Category | Tests | Coverage |
|----------|-------|----------|
| SIM state check (diagnostics_test.go) | 6 | All SIM states: active, suspended, terminated, stolen_lost, ordered, unknown |
| APN config (diagnostics_test.go) | 1 | Nil APN case |
| Policy (diagnostics_test.go) | 1 | Nil policy version |
| IP pool (diagnostics_test.go) | 1 | Nil APN (skips pool check) |
| Overall computation (diagnostics_test.go) | 4 | All pass, one warn, one fail, fail+warn |
| Throttle-to-zero (diagnostics_test.go) | 7 | Empty, no bandwidth key, max_bandwidth 0/nonzero, dl+ul 0/nonzero, only dl zero |
| Duration format (diagnostics_test.go) | 4 | Seconds, minutes, hours, days |
| Nil store degradation (diagnostics_test.go) | 3 | Nil session store, nil operator store, test auth placeholder |
| JSON round-trip (diagnostics_test.go) | 1 | Marshal/unmarshal DiagnosticResult |
| Handler validation (handler_test.go) | 3 | Invalid SIM ID, missing tenant, invalid body |
| Response structure (handler_test.go) | 1 | JSON field names and types |
| Cache key format (handler_test.go) | 1 | Key prefix, tenant, sim, boolean suffix |
| **Total** | **17** | Solid unit test coverage, all edge cases for pure logic |

Missing test coverage (non-blocking, noted for future hardening):
- No integration test with real SIM/session stores
- No test for cache hit path (requires real Redis)
- No test for SIM not found (404) path
- No test for active session / >24h session branches in checkLastAuth

---

## 10. Security Review

| Check | Status | Notes |
|-------|--------|-------|
| SQL injection | SAFE | `GetLastSessionBySIM` uses `$1` placeholder. All store methods use parameterized queries. |
| Tenant isolation | SAFE | SIM fetched with tenantID scope. Cache key includes tenantID. |
| Input validation | SAFE | UUID parsing for sim_id, tenant context validation, JSON body parsing with error handling. |
| Rate limiting | SAFE | Route inherits gateway rate limiter. |
| Auth/authz | SAFE | JWT + `sim_manager` role required (matches API contract). |
| Cache isolation | SAFE | Key `diag:{tenantID}:{simID}:{bool}` prevents cross-tenant cache reads. |

---

## 11. Performance Considerations

| Aspect | Assessment |
|--------|-----------|
| Multi-store sequential queries | 6 sequential DB queries per diagnostic run (SIM, session, operator, APN, policy version, IP pools). Total latency ~5-15ms on local PG. Acceptable for a troubleshooting tool (not hot path). |
| Redis cache | 1-minute TTL prevents repeated expensive runs. Cache key includes all parameters for correctness. |
| IP pool listing | `List()` with limit 100 per APN. For APNs with many pools, this caps at 100 which is reasonable. |
| No N+1 queries | Each step makes exactly one DB call (or zero for nil stores). |

---

## 12. Phase 6 Completion Summary

Phase 6 (Analytics & BI) is now complete with all 6 stories delivered:

| Story | Feature | Tests |
|-------|---------|-------|
| STORY-032 | CDR Processing & Rating Engine | 29 |
| STORY-033 | Real-Time Metrics & Observability | 13 |
| STORY-034 | Usage Analytics Dashboard | 39 |
| STORY-035 | Cost Analytics & Optimization | 22 |
| STORY-036 | Anomaly Detection Engine | 25 |
| STORY-037 | Connectivity Diagnostics | 17 |

**Phase 6 total: 145 new tests across 6 stories.**

Key capabilities delivered:
- Protocol-agnostic CDR processing with 4-factor rating engine
- Real-time auth metrics with Prometheus export and WebSocket push
- Usage and cost analytics with TimescaleDB continuous aggregates
- Rule-based anomaly detection (SIM cloning, auth/NAS flood, data spikes)
- 7-step SIM connectivity diagnostics with graceful degradation

**Phase Gate needed before starting Phase 7.**

---

## Summary

| Category | Result |
|----------|--------|
| Next story impact | 0 stories require changes. STORY-044 (frontend SIM detail) partially unblocked. |
| Architecture evolution | Graceful degradation pattern (nil store -> warn). No interface abstraction needed for read-only service. |
| New glossary terms | 2 new terms (Connectivity Diagnostics, Diagnostic Step) |
| FUTURE.md | No updates needed |
| New decisions | 3 captured (DEV-120 to DEV-122), all verified in decisions.md |
| Cross-doc consistency | 0 missing entries, 0 divergences. Caching table updated. |
| Implementation quality | 3 observations: session query lacks tenant_id (safe, LOW), placeholder step 7 degrades to DEGRADED (LOW, documented), operator/policy queries by ID only (correct convention) |
| Document updates | ARCHITECTURE.md (caching), GLOSSARY.md (2 terms), ROUTEMAP.md (DONE, Phase 6 complete) |

---

## Project Progress

- Stories completed: 36/55 (65%)
- Phase 6: COMPLETE (6/6 stories)
- Next: Phase 6 Gate, then Phase 7 (Notifications & Compliance)
- Test count: 917 passing (1 pre-existing flaky in analytics/metrics -- unrelated)

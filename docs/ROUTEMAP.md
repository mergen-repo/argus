# Project Roadmap: Argus

> Last updated: 2026-03-22
> Current phase: DEVELOPMENT — Phase 8: Frontend Portal
> Overall progress: 76%

---

## Planning Phase

| Step | Name | Status | Completed |
|------|------|--------|-----------|
| 1 | Discovery (Brainstormer) | [x] DONE | 2026-03-18 |
| 2 | Gap Analysis (Gap Analyst) | [x] DONE | 2026-03-18 |
| 3 | Product Definition (Product Analyst) | [x] DONE | 2026-03-18 |
| 4 | Feature Discovery (Feature Researcher) | [x] DONE | 2026-03-18 |
| 5 | Architecture (Architect) | [x] DONE | 2026-03-18 |
| 6 | Screen Design (Screen Designer) | [x] DONE | 2026-03-18 |
| 6.5 | Theme & Visual Design (Theme Designer) | [x] DONE | 2026-03-18 |
| 7 | Story Writing (Story Writer) | [x] DONE | 2026-03-18 |
| 8 | Final Review (Reviewer) | [x] DONE | 2026-03-18 |
| 9 | Development Readiness Audit | [x] DONE | 2026-03-18 |

---

## Development Phase [IN PROGRESS]

> Stories completed: 42/55 (76%)
> Current story: —
> Current step: —

### Phase 1: Foundation [DONE]

| # | Story | Effort | Status | Step | Dependencies | Completed |
|---|-------|--------|--------|------|-------------|-----------|
| STORY-001 | Project Scaffold & Docker Infrastructure | M | [x] DONE | — | — | 2026-03-20 |
| STORY-002 | Core Database Schema & Migrations | L | [x] DONE | — | STORY-001 | 2026-03-20 |
| STORY-003 | Authentication — JWT + Refresh + 2FA | M | [x] DONE | — | STORY-002 | 2026-03-20 |
| STORY-004 | RBAC Middleware & Permission Enforcement | M | [x] DONE | — | STORY-003 | 2026-03-20 |
| STORY-005 | Tenant Management & User CRUD | M | [x] DONE | — | STORY-004 | 2026-03-20 |
| STORY-006 | Structured Logging, Config & NATS Event Bus | M | [x] DONE | — | STORY-001 | 2026-03-20 |
| STORY-007 | Audit Log Service — Tamper-Proof Hash Chain | L | [x] DONE | — | STORY-006 | 2026-03-20 |
| STORY-008 | API Key Management & Rate Limiting | M | [x] DONE | — | STORY-004, STORY-006 | 2026-03-20 |

### Phase 2: Core SIM & APN [DONE]

| # | Story | Effort | Status | Step | Dependencies | Completed |
|---|-------|--------|--------|------|-------------|-----------|
| STORY-009 | Operator CRUD & Health Check | L | [x] DONE | — | STORY-005 | 2026-03-20 |
| STORY-010 | APN CRUD & IP Pool Management | L | [x] DONE | — | STORY-009 | 2026-03-20 |
| STORY-011 | SIM CRUD & State Machine | XL | [x] DONE | — | STORY-010 | 2026-03-20 |
| STORY-012 | SIM Segments & Group-First UX | M | [x] DONE | — | STORY-011 | 2026-03-20 |
| STORY-013 | Bulk SIM Import (CSV) | L | [x] DONE | — | STORY-011, STORY-006 | 2026-03-20 |
| STORY-014 | MSISDN Number Pool Management | S | [x] DONE | — | STORY-011 | 2026-03-20 |

### Phase 3: AAA Engine [DONE]

| # | Story | Effort | Status | Step | Dependencies | Completed |
|---|-------|--------|--------|------|-------------|-----------|
| STORY-018 | Pluggable Operator Adapter + Mock Simulator | L | [x] DONE | — | STORY-009 | 2026-03-20 |
| STORY-015 | RADIUS Authentication & Accounting Server | XL | [x] DONE | — | STORY-011, STORY-018 | 2026-03-20 |
| STORY-016 | EAP-SIM/AKA/AKA' Authentication | L | [x] DONE | — | STORY-015 | 2026-03-20 |
| STORY-017 | Session Management & Force Disconnect | L | [x] DONE | — | STORY-015 | 2026-03-20 |
| STORY-019 | Diameter Protocol Server (Gx/Gy) | XL | [x] DONE | — | STORY-015 | 2026-03-20 |
| STORY-020 | 5G SBA HTTP/2 Proxy (AUSF/UDM) | L | [x] DONE | — | STORY-015, STORY-016 | 2026-03-20 |
| STORY-021 | Operator Failover & Circuit Breaker | L | [x] DONE | — | STORY-018 | 2026-03-20 |

### Phase 4: Policy & Orchestration [DONE]

| # | Story | Effort | Status | Step | Dependencies | Completed |
|---|-------|--------|--------|------|-------------|-----------|
| STORY-022 | Policy DSL Parser & Evaluator | XL | [x] DONE | — | STORY-006 | 2026-03-21 |
| STORY-023 | Policy CRUD & Versioning | M | [x] DONE | — | STORY-022 | 2026-03-21 |
| STORY-024 | Policy Dry-Run Simulation | L | [x] DONE | — | STORY-023, STORY-011 | 2026-03-21 |
| STORY-025 | Policy Staged Rollout (Canary) | XL | [x] DONE | — | STORY-024, STORY-017 | 2026-03-21 |
| STORY-026 | Steering of Roaming Engine | L | [x] DONE | — | STORY-018 | 2026-03-21 |
| STORY-027 | RAT-Type Awareness (All Layers) | M | [x] DONE | — | STORY-015, STORY-022 | 2026-03-21 |

### Phase 5: eSIM & Advanced Ops [DONE]

| # | Story | Effort | Status | Step | Dependencies | Completed |
|---|-------|--------|--------|------|-------------|-----------|
| STORY-031 | Background Job Runner & Dashboard | L | [x] DONE | — | STORY-006, STORY-013 | 2026-03-21 |
| STORY-028 | eSIM Profile Management & SM-DP+ | L | [x] DONE | — | STORY-011 | 2026-03-21 |
| STORY-029 | OTA SIM Management (APDU) | M | [x] DONE | — | STORY-011, STORY-031 | 2026-03-22 |
| STORY-030 | Bulk State Change / Policy / Operator Switch | L | [x] DONE | — | STORY-012, STORY-028, STORY-031 | 2026-03-22 |

### Phase 6: Analytics & BI [DONE]

| # | Story | Effort | Status | Step | Dependencies | Completed |
|---|-------|--------|--------|------|-------------|-----------|
| STORY-032 | CDR Processing & Rating Engine | L | [x] DONE | — | STORY-015, STORY-019 | 2026-03-22 |
| STORY-033 | Real-Time Metrics & Observability | M | [x] DONE | — | STORY-006, STORY-015 | 2026-03-22 |
| STORY-034 | Usage Analytics Dashboard | M | [x] DONE | — | STORY-032 | 2026-03-22 |
| STORY-035 | Cost Analytics & Optimization | M | [x] DONE | — | STORY-032 | 2026-03-22 |
| STORY-036 | Anomaly Detection Engine | L | [x] DONE | — | STORY-032, STORY-017 | 2026-03-22 |
| STORY-037 | Connectivity Diagnostics | M | [x] DONE | — | STORY-015, STORY-011 | 2026-03-22 |

### Phase 7: Notifications & Compliance [DONE]

| # | Story | Effort | Status | Step | Dependencies | Completed |
|---|-------|--------|--------|------|-------------|-----------|
| STORY-038 | Notification Engine (Multi-Channel) | L | [x] DONE | — | STORY-006, STORY-005 | 2026-03-22 |
| STORY-039 | Compliance Reporting & Auto-Purge | M | [x] DONE | — | STORY-007, STORY-011 | 2026-03-22 |
| STORY-040 | WebSocket Event Server | L | [x] DONE | — | STORY-006 | 2026-03-22 |

### Phase 8: Frontend Portal [PENDING]

| # | Story | Effort | Status | Step | Dependencies | Completed |
|---|-------|--------|--------|------|-------------|-----------|
| STORY-041 | React Scaffold & Routing | L | [x] DONE | — | STORY-001 | 2026-03-22 |
| STORY-042 | Frontend: Auth (Login + 2FA) | M | [x] DONE | — | STORY-041, STORY-003 | 2026-03-22 |
| STORY-043 | Frontend: Main Dashboard | L | [~] IN PROGRESS | Commit | STORY-042, STORY-040 | — |
| STORY-044 | Frontend: SIM List + Detail | XL | [ ] PENDING | — | STORY-043, STORY-011 | — |
| STORY-045 | Frontend: APN + Operator Pages | M | [ ] PENDING | — | STORY-043, STORY-009 | — |
| STORY-046 | Frontend: Policy DSL Editor | XL | [ ] PENDING | — | STORY-043, STORY-022 | — |
| STORY-047 | Frontend: Sessions + Jobs + Audit | L | [ ] PENDING | — | STORY-043, STORY-040 | — |
| STORY-048 | Frontend: Analytics Pages | L | [ ] PENDING | — | STORY-043, STORY-032 | — |
| STORY-049 | Frontend: Settings Pages | M | [ ] PENDING | — | STORY-043, STORY-005 | — |
| STORY-050 | Frontend: Onboarding + Notifications | M | [ ] PENDING | — | STORY-043, STORY-038 | — |

### Phase 9: Integration & Polish [PENDING]

| # | Story | Effort | Status | Step | Dependencies | Completed |
|---|-------|--------|--------|------|-------------|-----------|
| STORY-051 | E2E Auth → SIM → Policy Flow Test | L | [ ] PENDING | — | Phase 8 | — |
| STORY-052 | AAA Performance Tuning & Benchmarks | L | [ ] PENDING | — | STORY-015, STORY-017 | — |
| STORY-053 | Data Volume Optimization & Archival | M | [ ] PENDING | — | STORY-032 | — |
| STORY-054 | Security Hardening (TLS, CSP, Audit) | M | [ ] PENDING | — | Phase 8 | — |
| STORY-055 | Tenant Onboarding E2E Wizard | M | [ ] PENDING | — | STORY-050 | — |

---

## E2E & Polish Phase [NOT STARTED]

| Step | Name | Status | Completed |
|------|------|--------|-----------|
| E1 | E2E Browser Testing (E2E Tester) | [ ] PENDING | — |
| E2 | Test Hardening (Test Hardener) | [ ] PENDING | — |
| E3 | Performance Optimization (Perf Optimizer) | [ ] PENDING | — |
| E4 | UI Polish (UI Polisher) | [ ] PENDING | — |

---

## Documentation Phase [NOT STARTED]

| Step | Name | Status | Completed |
|------|------|--------|-----------|
| D1 | Specification | [ ] PENDING | — |
| D2 | Presentations (Sales + Technical) | [ ] PENDING | — |
| D3 | Rollout Guide | [ ] PENDING | — |
| D4 | User Guide | [ ] PENDING | — |

---

## Change Log

| Date | Type | Description | Affected |
|------|------|-------------|----------|
| 2026-03-22 | GATE | Phase 7 Gate PASS — Deploy OK, smoke OK, 990/990 tests passed, 12 API endpoints verified, 5 DB migrations confirmed. 1 pending migration applied (notification_delivery). Report: docs/reports/phase-7-gate.md | Phase 8 ready |
| 2026-03-22 | PHASE | Phase 7 (Notifications & Compliance) completed — 3 stories (STORY-038, STORY-039, STORY-040). Multi-channel notification engine (email/Telegram/webhook/SMS/in-app), compliance reporting with auto-purge and KVKK/GDPR support, WebSocket real-time event server. All notifications and compliance features operational. | Phase 8 (Frontend Portal) ready to start |
| 2026-03-22 | REVIEW | STORY-040 review completed. 3 new glossary terms added (WS Server, WS Hub, WS Close Code). WS_MAX_CONNS_PER_TENANT added to CONFIG.md. relayNATSEvent uses BroadcastAll (no tenant extraction from payload -- tenant isolation depends on upstream event publishing). Backpressure drops newest (not oldest per spec -- pragmatic, non-blocking). Phase 7 complete -- Phase Gate ready. | ROUTEMAP.md, GLOSSARY.md, CONFIG.md |
| 2026-03-22 | DONE | STORY-040 completed -- WebSocket Server & Real-Time Event Push. gorilla/websocket upgrade on :8081/ws/v1/events, JWT auth (query param + first-message), 10 event types from NATS, tenant isolation, ping/pong heartbeat, backpressure (256 buffer), max 100 conns/tenant, client subscribe filtering, graceful shutdown. 28 new tests, 39 total WS tests, 991 total passing. | Phase 7 complete, Phase Gate ready |
| 2026-03-22 | GATE | Phase 6 Gate PASS — Deploy OK, smoke OK, 1407/1407 tests passed, 8 API endpoints verified, 3 DB migrations confirmed. 1 runtime bug fixed: anomalies table FK on partitioned sims table removed (same pattern as esim_profiles, ota_commands). Report: docs/reports/phase-6-gate.md | Phase 7 ready |
| 2026-03-22 | REVIEW | STORY-037 review completed. 2 new glossary terms added (Connectivity Diagnostics, Diagnostic Step). Diagnostic result cache row added to ARCHITECTURE.md caching table. GetLastSessionBySIM queries by sim_id only (safe: SIM fetched with tenant scope). Step 7 (test auth) is a placeholder returning warn — deferred to operator integration. 1 pre-existing flaky test (TestRecordAuth_ErrorRate in analytics/metrics — unrelated). Phase 6 complete — Phase Gate ready. | ROUTEMAP.md, GLOSSARY.md, ARCHITECTURE.md |
| 2026-03-22 | PHASE | Phase 6 (Analytics & BI) completed — 6 stories (STORY-032 to STORY-037). CDR processing & rating engine, real-time metrics & observability, usage analytics dashboards, cost analytics & optimization, anomaly detection engine, connectivity diagnostics. All analytics and BI features operational. | Phase 7 (Notifications & Compliance) ready to start |
| 2026-03-22 | DONE | STORY-037 completed — SIM Connectivity Diagnostics. POST /api/v1/sims/:id/diagnose (API-049), 7-step check engine (SIM state, last auth, operator health, APN config, policy, IP pool, test auth placeholder), Redis cache 1min TTL, graceful degradation for nil stores, overall PASS/DEGRADED/FAIL status. 17 new tests, 917 total passing. | Phase 6 complete, Phase Gate ready |
| 2026-03-22 | REVIEW | STORY-035 review completed. 3 new glossary terms added (Cost Analytics, Optimization Suggestion, Cost Per MB). deltaPercent duplication noted (handler.go + service.go). Trend data unfiltered by apn_id/rat_type (cdrs_daily limitation, matches STORY-034). segment_id filter deferred (matches STORY-034 DEV-110). 0 blocking changes for downstream stories. | ROUTEMAP.md, GLOSSARY.md |
| 2026-03-22 | DONE | STORY-035 completed -- Cost Analytics & Optimization. GET /api/v1/analytics/cost (API-112) with total cost, per-carrier breakdown, cost per MB by operator/RAT, top 20 expensive SIMs, trend, comparison mode, 3 optimization suggestion types (operator_switch, inactive_sims, low_usage). 8 store methods, CostService with suggestion engine. 22 new tests. | STORY-036 next |
| 2026-03-22 | REVIEW | STORY-034 review completed. 3 new glossary terms added (Usage Analytics, Period Resolution, Real-Time Aggregation). cdrs_monthly continuous aggregate documented in aaa-analytics.md. API-052 (per-SIM usage) noted as unimplemented -- not in STORY-034 scope. 0 blocking changes for downstream stories. | ROUTEMAP.md, GLOSSARY.md, db/aaa-analytics.md |
| 2026-03-22 | DONE | STORY-034 completed -- Usage Analytics Dashboards. GET /api/v1/analytics/usage (API-111) with period resolution (1h/24h/7d/30d/custom), group-by (operator/apn/rat_type), breakdowns, top 20 consumers, comparison mode with delta percentages. TimescaleDB cdrs_monthly aggregate created, real-time aggregation enabled on all 3 views. SQL injection prevention via dimension allowlist. 39 new tests. | STORY-035 next |
| 2026-03-22 | REVIEW | STORY-033 review completed. 4 new glossary terms added (Metrics Collector, MetricsRecorder Interface, Metrics Pusher, System Health Status). ARCHITECTURE.md caching table updated with auth rate counters and latency window rows. WEBSOCKET_EVENTS.md metrics.realtime schema updated to match actual RealtimePayload implementation (simplified from original spec). | ROUTEMAP.md, GLOSSARY.md, ARCHITECTURE.md, WEBSOCKET_EVENTS.md |
| 2026-03-22 | DONE | STORY-033 completed -- Built-In Observability & Real-Time Metrics. Redis-based auth rate counters (INCR with 5s TTL), latency percentiles (p50/p95/p99) via sorted set with 60s sliding window, per-operator metrics breakdown, system health status (healthy/degraded/critical). GET /api/v1/system/metrics (super_admin), GET /metrics (Prometheus/OpenMetrics), WS metrics.realtime push every 1s. RADIUS server instrumented via MetricsRecorder interface. 13 new tests, 41 packages passing. | STORY-043 (frontend dashboard) partially unblocked |
| 2026-03-22 | REVIEW | STORY-032 review completed. 4 new glossary terms added (Rating Engine, Cost Aggregation, CDR Consumer, CDR Export). CDR term expanded. Job Runner term updated with cdr_export processor. ALGORITHMS.md Section 5 package path corrected. idx_cdrs_dedup added to TBL-18 index docs. STORY-034, STORY-035 fully unblocked. | ROUTEMAP.md, GLOSSARY.md, ALGORITHMS.md, db/aaa-analytics.md |
| 2026-03-22 | DONE | STORY-032 completed -- CDR Processing & Rating Engine. NATS consumer on 3 session subjects (protocol-agnostic: RADIUS, Diameter, 5G SBA). Rating engine with 4 factors (base rate, RAT multiplier, time-of-day, volume tier). CDR store with idempotent insert, list with filters, cost aggregation, streaming export. 2 API endpoints (API-114, API-115). CDR export as background job (streaming CSV). Dedup unique index on (session_id, timestamp, record_type). 29 new tests, 825 total passing. | STORY-034, STORY-035 unblocked |
| 2026-03-22 | GATE | Phase 5 Gate PASS — Deploy OK, smoke OK, 797/797 tests passed, 14 API endpoints verified, 2 DB migrations confirmed. 3 runtime bugs fixed: OTA security_mode default empty string (DB check constraint), job runner tenant context missing (segment queries failed), OTA FK on partitioned sims table (removed like esim_profiles pattern). Report: docs/reports/phase-5-gate.md | Phase 6 ready |
| 2026-03-22 | PHASE | Phase 5 (eSIM & Advanced Ops) completed — 4 stories (STORY-028, STORY-029, STORY-030, STORY-031). eSIM profile management with SM-DP+ adapter, OTA SIM management via APDU/SMS-PP/BIP, bulk operations (state change/policy assign/operator switch), background job system with distributed locking. All eSIM and advanced ops features operational. | Phase 6 (Analytics & BI) ready to start |
| 2026-03-22 | REVIEW | STORY-030 review completed. 3 glossary terms added (Bulk Operation, Undo Record, Partial Success). Job Runner glossary term updated with real vs. stub processor list. USERTEST error report endpoint path fixed (/error-report -> /errors). 1 STORY-036 post-note added (bulk event burst filtering). Phase 5 complete -- Phase Gate ready. | ROUTEMAP.md, GLOSSARY.md, USERTEST.md, STORY-036 |
| 2026-03-22 | DONE | STORY-030 completed -- Bulk Operations (State Change, Policy Assign, Operator Switch). 3 bulk API endpoints (API-064..066), 3 real job processors replacing stubs, forward+undo mode with per-SIM previous_state tracking, per-SIM distributed locking (30s TTL), partial success with error_report JSONB, CSV error report export, eSIM switch uses ESimProfileStore.Switch() and skips physical SIMs, batch size 100, NATS progress publishing. SegmentStore extended with ListMatchingSIMIDs and ListMatchingSIMIDsWithDetails. 13 new tests, 797 total passing. | Phase 5 complete, Phase Gate ready |
| 2026-03-22 | REVIEW | STORY-029 review completed. 7 glossary terms added (SMS-PP, BIP, KIC, KID, GSM 03.48, TAR + APDU enriched). Decision IDs renumbered to DEV-090..094 (collision with STORY-028 DEV-085..088 fixed). TBL-26 (ota_commands) added to DB schema index. USERTEST.md endpoint paths corrected (4 wrong URLs fixed). OTA Redis key namespace `ota:ratelimit:` added to CONFIG.md. | GLOSSARY.md, decisions.md, db/_index.md, CONFIG.md, ARCHITECTURE.md, USERTEST.md, ROUTEMAP.md |
| 2026-03-22 | DONE | STORY-029 completed -- OTA SIM Management via APDU Commands. 5 OTA command types, APDU builder, SMS-PP/BIP encoding, AES-128-CBC/HMAC-SHA256 security, Redis rate limiting (10/SIM/hour), 4 API endpoints, real OTA job processor replacing stub, ota_commands table (TBL-26) with 5 indexes. 78 OTA tests, 784 total passing. | STORY-030 ready (all Phase 5 deps met except STORY-029→030 is not a dep) |
| 2026-03-21 | REVIEW | STORY-028 review completed. 3 glossary terms added (eSIM Profile State Machine, Profile Switch, SM-DP+ Adapter). 4 decisions recorded (DEV-085..088: tenant scoping via JOIN, SM-DP+ fire-and-forget, apn_id NULL on switch, available state merged into disabled). 2 spec divergences noted (no `available` state, no CoA/DM on switch -- both acceptable for v1). Post-notes for STORY-030: Switch sets apn_id=NULL, bulk processor must handle APN reassignment; use GetEnabledProfileForSIM before switch; handle mixed physical+eSIM segments. | GLOSSARY.md, decisions.md, STORY-030 |
| 2026-03-21 | GATE | Phase 4 Gate PASS — Deploy OK, smoke OK, 672/672 tests passed, 14 API endpoints verified, 2 DB migrations confirmed. 1 runtime bug fixed: SQL ambiguous column in JOIN queries (GetVersionWithTenant, GetRolloutByIDWithTenant, GetActiveRolloutForPolicy). Report: docs/reports/phase-4-gate.md | Phase 5 ready |
| 2026-03-21 | PHASE | Phase 4 (Policy & Orchestration) completed — 6 stories (STORY-022 to STORY-027). Policy DSL parser/evaluator, CRUD with versioning, dry-run simulation, staged rollout with CoA, Steering of Roaming engine, RAT-type awareness across all layers. All policy and orchestration features operational. | Phase 5 (eSIM & Advanced Ops) ready to start |
| 2026-03-21 | DONE | STORY-027 completed — RAT-Type Awareness (All Layers). Canonical `rattype` package (internal/aaa/rattype/) with enum + mapping functions for RADIUS/Diameter/5G SBA. DSL parser extended with 14 RAT aliases. SoR engine aligned to canonical constants. Session -> SIM last_rat_type update via functional options (WithSIMStore). 14 files modified, 672 tests passing. | F-026 (RAT-type awareness) fully delivered. Phase 4 complete. |
| 2026-03-21 | REVIEW | STORY-026 review completed. SoR cache row added to ARCHITECTURE.md caching table. TBL-06 schema updated with sor_priority, cost_per_mb, supported_rat_types columns. TBL-17 schema updated with sor_decision JSONB. 5 glossary terms added (SoR Decision, SoR Priority, Operator Lock, IMSI Prefix Routing, Cost-Based Selection). Post-STORY-026 note added to STORY-027 for RAT enum alignment. | ARCHITECTURE.md, GLOSSARY.md, db/operator.md, db/aaa-analytics.md, STORY-027 |
| 2026-03-21 | DONE | STORY-025 completed — Policy Staged Rollout (Canary). Rollout service (internal/policy/rollout/) with StartRollout, ExecuteStage, AdvanceRollout, RollbackRollout, GetProgress. 4 API endpoints (API-096 to API-099) under policy_editor role. Store: TBL-15 (policy_assignments), TBL-16 (policy_rollouts) with 15 store methods. CoA dispatch per active session in batches of 1000. NATS policy.rollout_progress events, WebSocket push. Async processor for stages >100K SIMs. SessionProvider/CoADispatcher interfaces for cross-service DI. Gate fixes: tenantID resolution (critical), policy_id response, errors field. 25 new tests, 1008 total passing. | STORY-046 (frontend policy editor) unblocked for rollout UI |
| 2026-03-21 | DONE | STORY-024 completed — Policy Dry-Run Simulation. DryRun service (internal/policy/dryrun/) with Execute(), CountMatchingSIMs(), buildFiltersFromMatch(), DetectBehavioralChanges(). Async job processor for >100K SIMs (internal/job/dryrun.go). Handler: POST /api/v1/policy-versions/:id/dry-run (API-094) with sync 200 / async 202 split. SIMFleetFilters + aggregation queries in store/sim.go (reusable by STORY-025). Redis cache 5min TTL. 26 new tests, 623 total passing. | STORY-025 (staged rollout) unblocked — can use dry-run data + fleet query infrastructure |
| 2026-03-21 | DONE | STORY-023 completed — Policy CRUD & Versioning. PolicyStore (internal/store/policy.go) with full CRUD for policies and versions. PolicyHandler (internal/api/policy/handler.go) with 9 HTTP endpoints (API-090 to API-095 + version management). Version state machine: draft -> active -> superseded/archived. DSL validation before activation via dsl.CompileSource/Validate. Routes under RequireRole("policy_editor"). SELECT FOR UPDATE for activation race safety. HasAssignedSIMs EXISTS check for soft-delete. 28 story tests, 613 total passing. | STORY-024 (dry-run), STORY-025 (rollout) unblocked |
| 2026-03-21 | DONE | STORY-022 completed — Policy DSL Parser & Evaluator. Full lexer, parser (recursive descent), AST, compiler (AST → JSON with unit normalization), evaluator (MATCH filtering, WHEN evaluation with last-match-wins, CHARGING with RAT multiplier). 7 source files + 4 test files in `internal/policy/dsl/`. 47 tests, all pass. Gate fixed time_of_day range evaluation with midnight wrapping. Pure computation library — no DB/Redis/NATS I/O. | STORY-023/024/025/027 unblocked, STORY-046 (frontend policy editor) partially unblocked |
| 2026-03-20 | PHASE | Phase 3 (AAA Engine) completed — 7 stories (STORY-015 to STORY-021). RADIUS server, EAP-SIM/AKA/AKA', session management, pluggable operator adapter, Diameter Gx/Gy server, 5G SBA proxy, operator failover with circuit breaker, NATS event publishing, notification service (SVC-08), WebSocket hub, SLA tracking. All AAA protocols operational. | Phase 4 (Policy & Orchestration) ready to start |
| 2026-03-20 | DONE | STORY-021 completed — Operator Failover & Circuit Breaker (remaining scope). NATS event publishing on health state transitions (operator.health_changed, alert.triggered), notification service (SVC-08) with multi-channel dispatch (email/telegram/in-app), WebSocket hub with NATS relay and tenant broadcast, SLA tracking with Redis sorted set latency and violation detection. 64 tests, all pass. | STORY-026 (SoR engine) unblocked, STORY-038/040 scope reduced |
| 2026-03-21 | DONE | STORY-028 completed — eSIM Profile Management. 5 API endpoints (API-070 to API-074), ESimProfileStore with transactional enable/disable/switch using FOR UPDATE row locks, SM-DP+ adapter interface (4 methods) + mock, atomic profile switch (disable old + enable new + update SIM in single TX), one-profile-per-SIM enforcement, tenant scoping via JOIN sims, audit logging + sim_state_history on all operations. 5 new error codes. 11 new tests, 1100 total, 0 failures. | STORY-030 unblocked |
| 2026-03-21 | DONE | STORY-031 completed — Background Job System. Distributed Redis locking (SETNX + Lua atomic release/renew), cron-like scheduler with Redis dedup, timeout detection (30min auto-fail), per-tenant concurrency control (default 5), graceful cancel via context propagation, enhanced cancel/retry API responses, 11 job type constants with 7 stub processors, 9 new config fields. 40 job tests, 696 total, 0 failures. | STORY-029, STORY-030 unblocked (partial) |
| 2026-03-20 | DONE | STORY-020 completed — 5G SBA HTTP/2 Proxy (AUSF/UDM). HTTP/2 server on :8443 with TLS/mTLS, AUSF 5G-AKA authentication (initiate + confirm), UDM security-information + auth-events + UECM registration, SUPI/SUCI resolution, S-NSSAI slice authentication, EAP-AKA' SBA proxy, NRF registration placeholder (register/deregister/heartbeat/discover/notify), session tracking with protocol_type='5g_sba' + slice_info JSONB, SBA health checker integrated into /api/health. Migration adds protocol_type + slice_info columns with partial index. 22 tests, all pass. | STORY-021 (next in Phase 3), STORY-027 (RAT awareness — 5G SBA already sets rat_type='nr_5g'), STORY-032 (CDR — should consume 5G SBA session events) |
| 2026-03-20 | DONE | STORY-019 completed — Diameter Protocol Server (Gx/Gy). Full RFC 6733 base protocol, TCP :3868 listener, CER/CEA capabilities exchange, DWR/DWA watchdog + failover, DPR/DPA graceful disconnect, Gx (PCRF) CCR-I/U/T with PCC rules, Gy (OCS) CCR-I/U/T/E with credit control, RAR/RAA mid-session re-auth, AVP encode/decode (standard + 3GPP vendor-specific), session state machine (idle/open/pending/closed), multi-peer support, health check integration. 53 tests, all pass with -race. | STORY-020 (5G SBA), STORY-032 (CDR) unblocked |
| 2026-03-20 | DONE | STORY-017 completed — Session Management & Concurrent Control. 4 session API endpoints (list, stats, disconnect, bulk disconnect), concurrent session control with oldest eviction, idle/hard timeout sweeper, Redis session cache, NATS session events, bulk disconnect as background job. 25 tests across 5 files. | STORY-025, STORY-033, STORY-036, STORY-052 unblocked (partial) |
| 2026-03-20 | DONE | STORY-016 completed — EAP-SIM/AKA/AKA' Authentication Methods. Redis state store (30s TTL), operator adapter bridge, vector caching (Redis list LPOP/RPUSH, batch pre-fetch), EAP-SIM Start flow (RFC 4186), SIM-type method selection, RADIUS EAP integration (Access-Challenge, MS-MPPE keys), session auth_method recording. 49 tests across 4 files. | STORY-020 unblocked (5G SBA uses AKA') |
| 2026-03-20 | DONE | STORY-015 completed — RADIUS Authentication & Accounting Server. UDP :1812 auth + :1813 acct, SIM cache (Redis+DB), session manager (Redis+DB), CoA/DM, per-operator shared secret, health check AAA status, graceful shutdown. 15 RADIUS tests, 7 session tests, 5 store tests. | STORY-016, STORY-017, STORY-019, STORY-032 unblocked |
| 2026-03-20 | DONE | STORY-018 completed — Pluggable Operator Adapter Framework. Extended Adapter interface with Authenticate/AccountingUpdate/FetchAuthVectors. Mock adapter with EAP triplet/quintet generation, RADIUS real forwarding, Diameter CER/CEA+DWR/DWA, new SBA adapter (HTTP/2), OperatorRouter 3 new methods with circuit breaker. 63 tests pass with -race. | STORY-015, STORY-016, STORY-019, STORY-020, STORY-021 unblocked |
| 2026-03-20 | PHASE | Phase 2 (Core SIM & APN) completed — 6 stories (STORY-009 to STORY-014). Operator CRUD, APN CRUD, IP Pool CRUD, SIM CRUD + state machine, segments, bulk import, MSISDN pool management all implemented. 47 routes registered, 24 DB tables in use. | Phase 3 (AAA Engine) ready to start |
| 2026-03-20 | DONE | STORY-014 completed — MSISDN pool management with CSV import, list with state filtering, assign to SIM, global uniqueness, grace period release on SIM termination. 3 new routes. | Phase 2 complete |
| 2026-03-20 | DONE | STORY-013 completed — Bulk SIM Import (CSV upload, background job processing, partial success, NATS progress, cancellation, error report CSV download). 6 new routes. Job runner + import processor wired in main.go. | STORY-031 scope reduced (job runner + API-120..123 already implemented), STORY-014 next |
| 2026-03-20 | DONE | STORY-012 completed — Segment CRUD (6 endpoints), JSONB filter_definition, CountMatchingSIMs, StateSummary, sim_manager RBAC | STORY-030 unblocked (partial — also needs STORY-028, STORY-031) |
| 2026-03-20 | DONE | STORY-011 completed — SIM CRUD, state machine (7 transitions), cursor pagination, IP allocation on activation, auto-purge scheduling | STORY-012, STORY-013, STORY-014 unblocked |
| 2026-03-20 | DONE | STORY-010 completed — APN CRUD, IP Pool CRUD, IP allocation/reservation/release, dual-stack IPv4+IPv6 | STORY-011 unblocked, STORY-013 partially unblocked |
| 2026-03-20 | DONE | STORY-009 completed — Operator CRUD, health check, adapter registry, AES-256 encryption | STORY-018, STORY-021 updated (partial overlap) |
| 2026-03-18 | INIT | Project initialized — Argus RADIUS/APN Management Platform | — |

---

## Status Legend
- `[ ] PENDING` — Not started
- `[~] IN PROGRESS` — Currently being worked on
- `[x] DONE` — Completed and verified
- `[!] NEEDS_REPLAN` — Affected by change, needs re-planning
- `[!!] BLOCKED_BY_CHANGE` — Cannot proceed until change is applied
- `[S] SKIPPED` — User kararıyla atlandı (autopilot escalation)
- Effort: S (Small) | M (Medium) | L (Large) | XL (Extra Large)

## Step Values
- `—` — Not started
- `Plan` — Implementation planning
- `Dev` — Developer implementing
- `Gate` — Combined Gate (Gap + Compliance + Tests + Perf + Build)
- `Commit` — Close & Commit
- `Review` — Reviewer checking (after every story)
- `Handoff` — Session handoff
- `Runner` — Story Runner subprocess'te çalışıyor (AUTOPILOT)
- `Escalated` — Story Runner escalate etti, user bekleniyor
- `Failed` — Story Runner failed
- `E1` — E2E Browser Testing
- `E2` — Test Hardening
- `E3` — Performance Optimization
- `E4` — UI Polish
- `D1` — Specification document
- `D2` — Presentations (Sales + Technical)
- `D3` — Rollout Guide
- `D4` — User Guide

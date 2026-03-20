# Post-Story Review: STORY-006 — Structured Logging, Config & NATS Event Bus

> Date: 2026-03-20

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| STORY-007 | Audit log service. Directly depends on STORY-006 (NATS event bus). STORY-007 AC-1 says "Every state-changing API call creates an audit entry via NATS event." The `EventBus.Publish()` and `EventBus.Subscribe()` abstractions are ready. `SubjectSIMUpdated`, `SubjectPolicyChanged`, and other event subjects are defined. STORY-007 will likely need a new subject like `argus.events.audit.entry` or use the existing subjects as triggers. The `GetCorrelationID(ctx)` helper allows STORY-007 to capture correlation IDs in audit entries (AC field: `correlation_id`). No changes needed to STORY-006 outputs. | NO_CHANGE |
| STORY-008 | API key management & rate limiting. Depends on STORY-004 (RBAC) and STORY-006 (Redis for rate limiting). STORY-008 needs Redis sliding window counters -- `cache.NewRedis` with configurable pool size and timeouts is ready. Config already has `RateLimitPerMinute`, `RateLimitPerHour`, `RateLimitAlgorithm`, `RateLimitAuthPerMin`, `RateLimitEnabled` env vars. STORY-008 will add API key auth middleware to the gateway -- the `RouterDeps` pattern and middleware chain are established. No STORY-006 changes needed. | NO_CHANGE |
| STORY-013 | Bulk SIM import. Depends on STORY-006 for NATS job queue. `SubjectJobQueue`, `SubjectJobCompleted`, `SubjectJobProgress` subjects are defined. `EventBus.QueueSubscribe()` with competing consumers is ready for the job runner pattern. `StreamJobs` with `WorkQueuePolicy` ensures exactly-once delivery for job processing. No changes needed. | NO_CHANGE |
| STORY-022 | Policy DSL parser. Depends on STORY-006. `SubjectPolicyChanged` subject is defined for cache invalidation when policies change. The NATS event bus will carry policy change events to trigger cache invalidation across instances (as specified in ARCHITECTURE.md caching strategy). No changes needed. | NO_CHANGE |
| STORY-031 | Background job runner & dashboard. Depends on STORY-006. The `job/runner.go` already uses `EventBus` for `QueueSubscribe` and `Publish` on job completion. The JOBS stream with WorkQueuePolicy and 24h MaxAge provides the durable queue foundation. STORY-031 will extend this with job types, progress tracking, and dashboard APIs. No changes needed. | NO_CHANGE |
| STORY-033 | Real-time metrics & observability. Depends on STORY-006. Zerolog structured logging with correlation IDs, log-level configuration, and the NATS event bus for metrics event streaming are all in place. The `SubjectAlertTriggered` forward declaration will be useful here. No changes needed. | NO_CHANGE |
| STORY-038 | Notification engine. Depends on STORY-006. `SubjectNotification` (`argus.events.notification.dispatch`) is defined. The notification engine will subscribe to this subject via `EventBus.Subscribe()`. No changes needed. | NO_CHANGE |
| STORY-040 | WebSocket event server. Depends on STORY-006. The EVENTS stream with `LimitsPolicy` and 72h `MaxAge` provides the event source. WebSocket server will subscribe to `argus.events.>` wildcard to broadcast to connected clients. No changes needed. | NO_CHANGE |

## Architecture Evolution

### Zerolog as Shared Logger Infrastructure
STORY-006 established zerolog as the global structured logging layer. The `main.go` logger setup pattern (dev=ConsoleWriter, prod=JSON stdout, service field injection) creates a consistent logging foundation. All future services receive the logger via dependency injection (e.g., `RouterDeps.Logger`, `bus.NewNATS(..., logger)`). This is clean -- no global state leakage beyond the `log.Logger` global which is standard zerolog usage.

### Middleware Chain Finalized
The middleware chain in `router.go` is now: `RecoveryWithZerolog -> CorrelationID -> RealIP -> ZerologRequestLogger`. This matches MIDDLEWARE.md positions 1-3 (Recovery, CorrelationID, RealIP) with the logger after RealIP. The ordering is correct: Recovery must be outermost (catches panics from all downstream), CorrelationID must precede the logger (so the logger can emit correlation IDs), and RealIP must precede the logger (so RemoteAddr is correct in logs).

### NATS Bus Abstraction Layer
The `bus` package provides a clean two-tier abstraction: `NATS` (connection management, stream creation, health check) and `EventBus` (publish/subscribe operations). This separation allows future stories to interact only with `EventBus` without knowing NATS internals. The JetStream-based `Publish` ensures message durability for events, while plain NATS `Subscribe`/`QueueSubscribe` keeps the consumer side simple. This is the right tradeoff for Phase 1 -- JetStream consumers can be added later when exactly-once processing is needed for specific subscribers.

### Config Validation at Startup
The `Validate()` method with 7 semantic rules (AppEnv, DeploymentMode, LogLevel, JWTSecret length, BcryptCost range, DatabaseMaxConns, RedisMaxConns) fails fast on misconfiguration. This prevents runtime errors from invalid config. The pattern is extensible -- STORY-008 can add rate limit validation, STORY-015 can add RADIUS port validation, etc.

### Graceful Shutdown Pattern
Ordered shutdown (HTTP server -> NATS -> Redis -> DB) with 10s timeout is now established. The double-close pattern (defer + explicit) is intentional and safe. Future services that need cleanup (e.g., RADIUS/Diameter listeners in STORY-015) can add their shutdown steps to the same sequence in `main.go`.

## Glossary Check

| Term | Status | Notes |
|------|--------|-------|
| Correlation ID | NEW_CANDIDATE | UUID generated per HTTP request, propagated via context, included in all log entries. Used for request tracing across service boundaries. Referenced in G-033 ("structured JSON logging with correlation ID") but not defined in GLOSSARY. |
| JetStream | NOT_NEEDED | NATS JetStream is a technology name, not a domain term. Already referenced in ARCHITECTURE.md tech stack table. |
| EventBus | NOT_NEEDED | Internal implementation pattern, not a domain concept. |
| Graceful Shutdown | NOT_NEEDED | Standard engineering term, not domain-specific. |

**Decision:** Add "Correlation ID" to GLOSSARY.md. It is a cross-cutting concept used by logging, audit (STORY-007), and observability (STORY-033). It has a specific meaning in the Argus context (UUID v4, per-request, propagated via Go context, emitted as `X-Request-ID` header).

## decisions.md Check

New technical decisions from STORY-006:

1. **DEV-010: Zerolog dev/prod output split** -- Development uses `ConsoleWriter` (human-readable colored output), production uses JSON to stdout. No env-var toggle beyond `APP_ENV` -- the decision is implicit in `cfg.IsDev()`. This is a minor operational decision but worth noting for consistency.

2. **DEV-011: JetStream Publish for all event bus messages** -- `EventBus.Publish()` uses JetStream (durable, persistent) rather than plain NATS publish. This means all events go through JetStream streams, providing at-least-once delivery guarantees. The trade-off is slightly higher latency vs plain NATS, but durability is more important for audit events and job processing. This aligns with ADR-002 (NATS JetStream for events/jobs).

3. **DEV-012: SubjectAlertTriggered extends CONFIG.md spec** -- `argus.events.alert.triggered` is defined in code but not in CONFIG.md's NATS subjects table. This is a forward declaration for STORY-033/STORY-036. Falls under `argus.events.>` stream wildcard. CONFIG.md should be updated to include `argus.events.alert.*` and `argus.jobs.completed`/`argus.jobs.progress` subjects.

**Decision:** Add DEV-010, DEV-011, and DEV-012 to decisions.md. CONFIG.md NATS subjects table has minor drift (3 subjects in code not in doc).

## Cross-Doc Consistency

| Check | Status | Notes |
|-------|--------|-------|
| ARCHITECTURE.md tech stack (zerolog, envconfig) | OK | Both listed in tech stack table. Implementation matches. |
| ARCHITECTURE.md project structure | OK | `internal/config/`, `internal/bus/`, `internal/gateway/` all exist as documented. |
| MIDDLEWARE.md middleware chain | OK | Recovery -> CorrelationID -> RealIP -> Logger matches MIDDLEWARE.md positions 1-3 (Recovery at position 1, CorrelationID at 2, RealIP between 2 and 3). |
| CONFIG.md env vars | OK | All env vars in `config.go` match CONFIG.md. Defaults match. Required flags match. |
| CONFIG.md NATS subjects | DRIFT | Code defines 12 subject constants. CONFIG.md lists 7 subjects. Missing from CONFIG.md: `argus.events.alert.*`, `argus.jobs.completed`, `argus.jobs.progress`. `argus.events.sim.updated` and `argus.events.session.started/updated/ended` are covered by wildcards (`session.*`, `sim.*`). Non-blocking -- all subjects fall under configured stream wildcards. |
| CONFIG.md validation rules | NOT_DOCUMENTED | `Validate()` enforces 7 semantic rules. CONFIG.md does not document validation rules (e.g., "JWT_SECRET minimum 32 chars" is implicit from description but not listed as a formal validation). Low priority -- config descriptions are adequate. |
| ERROR_CODES.md | OK | No new error codes introduced. `RecoveryWithZerolog` uses existing `CodeInternalError`. |
| ADR-002 (NATS JetStream) | OK | JetStream used for events/jobs as specified. Redis for cache. Both with connection pooling. |
| PRODUCT.md G-033 (built-in observability) | OK | Structured JSON logging with correlation ID implemented. Built-in metrics dashboard deferred to STORY-033. |
| PRODUCT.md F-068 (background job system) | OK | NATS-backed job queue (JOBS stream with WorkQueuePolicy) is the foundation. Full job dashboard deferred to STORY-031. |
| Story spec AC vs Gate report | OK | All 9 acceptance criteria passed. Gate PASS with 0 fixes. |

## Story Updates

No changes needed to STORY-006 spec. All acceptance criteria met as specified.

**Downstream story specs check:**
- STORY-007: References "NATS event bus" in dependency -- confirmed available. `EventBus.Publish/Subscribe` ready.
- STORY-008: References "Redis for rate limiting" in dependency -- confirmed available. `cache.NewRedis` with pooling ready. Rate limit config vars in place.
- No spec updates needed for any downstream stories.

## Observations

1. **CONFIG.md NATS subjects drift.** Three subjects in code (`argus.events.alert.*`, `argus.jobs.completed`, `argus.jobs.progress`) are not listed in CONFIG.md's NATS subjects table. The `argus.cache.invalidate` subject is listed in CONFIG.md and defined as a constant in code -- consistent. The drift is minor (all subjects fall under configured stream wildcards) but should be updated for documentation accuracy. Recommend updating CONFIG.md in the next story that touches NATS subjects.

2. **responseCapture missing Unwrap/Flusher.** As noted in the gate report, the response writer wrapper in `logging.go` doesn't implement `http.Flusher`, `http.Hijacker`, or `Unwrap()`. This is safe now (WebSocket on separate port :8081, no SSE on :8080). Should be addressed before STORY-040 (WebSocket) or if SSE is introduced on :8080. Tracked as advisory.

3. **NATS integration test gap.** Bus tests cover serialization and constants but not actual NATS roundtrip. This is acceptable for Phase 1. When CI with Docker is configured (or using embedded NATS test server), a live publish/subscribe test should be added. STORY-051 (E2E integration tests) is the natural place for this.

4. **EventBus Subscribe uses plain NATS, not JetStream consumers.** `EventBus.Publish()` uses JetStream (durable), but `Subscribe()` and `QueueSubscribe()` use plain NATS subscriptions. This means subscribers won't receive messages published while they were offline. For STORY-007 (audit), this could mean lost audit events during service restart. When STORY-007 is implemented, the audit subscriber should use a JetStream durable consumer for guaranteed delivery. This is not a STORY-006 bug -- it's a conscious abstraction that provides both durable publishing and flexible subscription patterns.

5. **Global log.Logger usage in main.go.** The `log.Logger` global is set in `main.go` and passed to components via dependency injection (which is good). However, `log.Fatal()` calls in startup still use the global directly. This is standard zerolog practice and not an issue, but worth noting that the global is the authoritative logger.

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| ROUTEMAP.md | STORY-006 marked DONE, progress updated to 6/55 (11%) | PENDING |
| decisions.md | DEV-010 (zerolog dev/prod split), DEV-011 (JetStream for all EventBus publish), DEV-012 (SubjectAlertTriggered extends spec) | PENDING |
| GLOSSARY.md | Add "Correlation ID" definition | PENDING |
| ARCHITECTURE.md | No changes needed | NO_CHANGE |
| FUTURE.md | No new items | NO_CHANGE |
| CONFIG.md | NATS subjects table should add `argus.events.alert.*`, `argus.jobs.completed`, `argus.jobs.progress` | DRIFT_NOTED |
| ERROR_CODES.md | No changes (pre-existing drift from STORY-003/STORY-005 still open) | NO_CHANGE |
| MIDDLEWARE.md | No changes needed (chain already documented) | NO_CHANGE |
| CLAUDE.md | No changes | NO_CHANGE |
| FRONTEND.md | No changes (backend-only story) | NO_CHANGE |

## Project Health

- Stories completed: 6/55 (11%)
- Current phase: Phase 1 -- Foundation (6/8 stories done, 75% of Phase 1)
- Next story: STORY-007 (Audit Log Service -- Tamper-Proof Hash Chain)
- Blockers: None
- Escalations: 1 active (ESC-001: linear RBAC hierarchy, deadline pre-STORY-011)
- Quality: 36 new tests (11 config + 4 correlation + 6 middleware + 4 bus + 11 pre-existing counted by gate), all passing. Full suite green (22 packages). 0 gate fixes needed.
- Cumulative tests: ~86+ test functions across all packages
- Technical debt:
  - Duplicate `createAuditEntry` pattern (from STORY-005, 3 copies expected by Phase 2)
  - Cursor pagination needs composite cursor for high-volume endpoints (from STORY-005)
  - `responseCapture` missing Unwrap/Flusher (new, address pre-STORY-040)
  - CONFIG.md NATS subjects table drift (new, 3 subjects undocumented)
  - EventBus.Subscribe uses plain NATS not JetStream consumers (new, address in STORY-007 for audit durability)
  - ERROR_CODES.md drift (from STORY-003/STORY-005, 2+ codes not in doc)

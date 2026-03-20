# Implementation Plan: STORY-006 - Structured Logging, Config & NATS Event Bus

## Goal

Wire up structured JSON logging with zerolog (correlation ID propagation), enhance config validation, create NATS JetStream streams (EVENTS, JOBS), provide Publish/Subscribe bus abstractions, and implement graceful shutdown for all infrastructure connections.

## Architecture Context

### Components Involved

- **internal/config/config.go**: Already exists with full env var mapping via envconfig. Needs `Validate()` method for semantic validation beyond envconfig defaults.
- **internal/gateway/router.go**: Chi router with chi/middleware.Recoverer, RequestID, RealIP, Logger. Must be replaced with custom zerolog-based middleware for correlation ID propagation and structured JSON logging.
- **internal/bus/nats.go**: Already exists with basic `NewNATS`, `EventBus`, `Publish`, `Subscribe`, `QueueSubscribe`. Needs JetStream stream creation (EVENTS, JOBS) on startup.
- **internal/cache/redis.go**: Already exists with `NewRedis`, connection pooling, health check. Complete — no changes needed.
- **cmd/argus/main.go**: Entry point. Already has signal handling and deferred closes. Needs logger initialization refinement and explicit graceful shutdown ordering.

### Middleware Chain (from MIDDLEWARE.md)

The middleware chain order is critical:
1. Recovery (panic handler) — catches panics, logs stack trace via zerolog
2. RequestID (correlation_id) — generates UUID v4, sets X-Request-ID header, injects into context
3. Logging (zerolog) — logs request start/end with method, path, status, duration_ms, correlation_id
4. CORS
5. RateLimiter
6. Auth
7. TenantContext
8. RBAC
9. Handler

This story implements middleware 1-3 with zerolog. Currently the router uses chi's built-in middleware which logs to stdout in plaintext format — we need structured JSON.

### Context Key for Correlation ID

From MIDDLEWARE.md: `correlation_id` is a `string` (UUID v4) set by RequestID middleware. All downstream logs include this ID. All audit entries reference this ID.

Key already exists in `internal/apierr/apierr.go` as context keys pattern — correlation ID key should follow the same pattern.

### NATS Stream Configuration

From CONFIG.md, NATS subjects:
| Subject Pattern | Purpose |
|---------|---------|
| `argus.events.session.*` | Session start/stop/update events |
| `argus.events.sim.*` | SIM state change events |
| `argus.events.policy.*` | Policy change events |
| `argus.events.operator.*` | Operator health events |
| `argus.events.notification.*` | Notification dispatch |
| `argus.jobs.queue` | Job queue (pull-based) |
| `argus.jobs.completed` | Job completion |
| `argus.jobs.progress` | Job progress |
| `argus.cache.invalidate` | Cache invalidation broadcast |

Two JetStream streams:
- **EVENTS**: subjects `argus.events.>` — all event types
- **JOBS**: subjects `argus.jobs.>` — job queue, completion, progress

### Existing Bus Package Constants

`internal/bus/nats.go` already defines all subject constants:
```go
SubjectSessionStarted       = "argus.events.session.started"
SubjectSessionUpdated       = "argus.events.session.updated"
SubjectSessionEnded         = "argus.events.session.ended"
SubjectSIMUpdated           = "argus.events.sim.updated"
SubjectPolicyChanged        = "argus.events.policy.changed"
SubjectOperatorHealthChanged = "argus.events.operator.health"
SubjectNotification         = "argus.events.notification.dispatch"
SubjectJobQueue             = "argus.jobs.queue"
SubjectJobCompleted         = "argus.jobs.completed"
SubjectJobProgress          = "argus.jobs.progress"
SubjectCacheInvalidate      = "argus.cache.invalidate"
```

### Existing Config (internal/config/config.go)

Full Config struct already exists with all env vars from CONFIG.md. `Load()` uses `envconfig.Process`. Has `IsDev()`, `IsProd()`, `Addr()` helpers. Missing: `Validate()` method for semantic rules (e.g., JWTSecret min length, BcryptCost range).

### Existing main.go Structure

```go
func main() {
    cfg := config.Load()
    // zerolog level parsing + console writer for dev
    // store.NewPostgres → defer Close
    // cache.NewRedis → defer Close
    // bus.NewNATS → defer Close
    // auth service, handlers, router
    // http.Server.ListenAndServe in goroutine
    // signal.Notify(SIGINT, SIGTERM)
    // srv.Shutdown
}
```

Already has basic graceful shutdown structure. Needs: logger setup refinement (global logger with service field), explicit shutdown order logging.

## Prerequisites

- [x] STORY-001 completed (Docker infra, project scaffold, makefile, go.mod with zerolog/nats/redis deps)
- [x] STORY-002 completed (database schema, migrations)
- [x] STORY-003 completed (JWT auth, auth middleware)
- [x] STORY-004 completed (RBAC middleware)
- [x] STORY-005 completed (tenant management CRUD)

## Tasks

### Task 1: Config Validation

- **Files:** Modify `internal/config/config.go`, Create `internal/config/config_test.go`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `internal/config/config.go` — extend existing struct with `Validate()` method
- **Context refs:** Architecture Context > Existing Config
- **What:**
  Add a `Validate()` method to the existing Config struct that enforces semantic validation rules:
  - `JWTSecret` must be >= 32 characters
  - `BcryptCost` must be between 10 and 14
  - `AppEnv` must be one of: `development`, `staging`, `production`
  - `LogLevel` must be a valid zerolog level
  - `DeploymentMode` must be `single` or `cluster`
  - `DatabaseMaxConns` must be > 0
  - `RedisMaxConns` must be > 0
  - Call `Validate()` at the end of `Load()` after `envconfig.Process`

  Create `config_test.go` with table-driven tests:
  - Valid config passes
  - Short JWT secret fails
  - Invalid AppEnv fails
  - Invalid LogLevel fails
  - BcryptCost out of range fails
- **Verify:** `go test ./internal/config/...`

### Task 2: Correlation ID Middleware + Zerolog Request Logger

- **Files:** Create `internal/gateway/correlation.go`, Create `internal/gateway/logging.go`, Create `internal/gateway/correlation_test.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/gateway/auth_middleware.go` — follow same Chi middleware pattern (func returning `func(http.Handler) http.Handler`)
- **Context refs:** Architecture Context > Middleware Chain, Architecture Context > Context Key for Correlation ID
- **What:**

  **correlation.go:**
  - Add `CorrelationIDKey` constant to `apierr` package (type `contextKey`, value `"correlation_id"`) — or define locally in gateway and export
  - Create `CorrelationID()` middleware:
    - Generate UUID v4 via `github.com/google/uuid`
    - Set `X-Request-ID` response header
    - Inject into context via `context.WithValue(ctx, apierr.CorrelationIDKey, id)`
    - Call next handler
  - Create `GetCorrelationID(ctx context.Context) string` helper function to extract from context

  **logging.go:**
  - Create `ZerologRequestLogger(logger zerolog.Logger)` middleware:
    - On request start: extract correlation_id from context (set by CorrelationID middleware which runs before this)
    - Create sub-logger with correlation_id field: `logger.With().Str("correlation_id", id).Logger()`
    - Wrap response writer to capture status code and bytes written
    - On request completion: log structured JSON with fields: `method`, `path`, `status`, `duration_ms`, `bytes`, `remote_addr`, `correlation_id`
    - Use `Info` level for 2xx/3xx, `Warn` for 4xx, `Error` for 5xx
  - Create `RecoveryWithZerolog(logger zerolog.Logger)` middleware:
    - Catches panics
    - Logs stack trace with `Error` level
    - Returns 500 with standard error envelope via `apierr.WriteError`
    - Includes correlation_id in log

  **correlation_test.go:**
  - Test that CorrelationID middleware sets X-Request-ID header
  - Test that correlation_id is in request context
  - Test that GetCorrelationID extracts correctly
  - Test that logging middleware produces valid JSON output
- **Verify:** `go test ./internal/gateway/... -run TestCorrelation`

### Task 3: NATS JetStream Stream Setup

- **Files:** Modify `internal/bus/nats.go`, Create `internal/bus/nats_test.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/bus/nats.go` — extend existing NATS struct
- **Context refs:** Architecture Context > NATS Stream Configuration, Architecture Context > Existing Bus Package Constants
- **What:**
  Add `EnsureStreams(ctx context.Context)` method to the `NATS` struct that creates/updates two JetStream streams:

  **EVENTS stream:**
  - Name: `EVENTS`
  - Subjects: `[]string{"argus.events.>"}`
  - Retention: `LimitsPolicy`
  - MaxAge: 72 hours
  - Storage: `FileStorage`
  - Replicas: 1 (single mode)
  - Discard: `DiscardOld`

  **JOBS stream:**
  - Name: `JOBS`
  - Subjects: `[]string{"argus.jobs.>"}`
  - Retention: `WorkQueuePolicy`
  - MaxAge: 24 hours
  - Storage: `FileStorage`
  - Replicas: 1

  Use `jetstream.CreateOrUpdateStream` so it's idempotent.

  Also add logging to `NewNATS` — accept a `zerolog.Logger` parameter, log connection events.

  Enhance `Publish` on `EventBus` to use JetStream publish (not plain NATS publish) for durability. Keep plain Subscribe for non-durable consumers, add `JetStreamSubscribe` for durable consumers using JetStream consumer API.

  Create `nats_test.go`:
  - Test `EnsureStreams` creates streams (requires NATS test server or mock)
  - Test `Publish` + `Subscribe` roundtrip
  - Use `natsserver "github.com/nats-io/nats-server/v2/test"` for embedded test server, or skip if not available with build tags
- **Verify:** `go build ./internal/bus/...`

### Task 4: Wire Middleware into Router + Update main.go

- **Files:** Modify `internal/gateway/router.go`, Modify `cmd/argus/main.go`
- **Depends on:** Task 1, Task 2, Task 3
- **Complexity:** medium
- **Pattern ref:** Read `internal/gateway/router.go` — modify existing middleware chain; Read `cmd/argus/main.go` — modify existing startup sequence
- **Context refs:** Architecture Context > Middleware Chain, Architecture Context > Existing main.go Structure
- **What:**

  **router.go:**
  - Add `Logger zerolog.Logger` field to `RouterDeps` struct
  - Replace `middleware.Recoverer` with custom `RecoveryWithZerolog(deps.Logger)`
  - Replace `middleware.RequestID` with custom `CorrelationID()`
  - Remove `middleware.RealIP` (keep for later, not part of this story)
  - Replace `middleware.Logger` with custom `ZerologRequestLogger(deps.Logger)`
  - Keep all existing route registrations unchanged

  **main.go:**
  - After config load, set up global zerolog logger properly:
    - Always use `zerolog.New(os.Stdout)` for JSON output in non-dev
    - In dev mode, use `zerolog.ConsoleWriter` on stderr
    - Add `.With().Timestamp().Str("service", "argus").Logger()` to global logger
    - Set as `log.Logger` global
  - Call `cfg.Validate()` after `config.Load()`
  - Pass logger to `bus.NewNATS`
  - Call `ns.EnsureStreams(ctx)` after NATS connection
  - Pass `Logger` in `RouterDeps`
  - Improve graceful shutdown:
    - Log each close step: "closing NATS...", "closing Redis...", "closing DB..."
    - Explicit order: HTTP server first, then NATS, Redis, DB
    - Log "argus stopped gracefully" at end
- **Verify:** `go build ./cmd/argus/...`

### Task 5: Integration Tests

- **Files:** Create `internal/bus/bus_integration_test.go`, Create `internal/gateway/middleware_test.go`
- **Depends on:** Task 2, Task 3, Task 4
- **Complexity:** medium
- **Pattern ref:** Read `internal/auth/auth_test.go` — follow same Go test patterns with testify assertions (if used) or standard testing
- **Context refs:** Architecture Context > Middleware Chain, Architecture Context > NATS Stream Configuration
- **What:**

  **middleware_test.go:**
  - Test full middleware chain: CorrelationID + ZerologRequestLogger produces structured JSON output with all required fields (timestamp, level, correlation_id, method, path, status, duration_ms)
  - Test correlation_id appears in response header (X-Request-ID)
  - Test RecoveryWithZerolog catches panics and returns 500 with standard error envelope
  - Test that correlation_id propagates through to handler (handler can read it from context)

  **bus_integration_test.go:**
  - Test Publish + Subscribe roundtrip (unit test with goroutine, no external NATS needed — use mock or skip with build tag)
  - Test EventBus serialization (publish struct, receive JSON, unmarshal)

  Config tests are already covered in Task 1.
- **Verify:** `go test ./internal/gateway/... ./internal/bus/...`

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| All log entries are JSON with: timestamp, level, correlation_id, service, message, fields | Task 2, Task 4 | Task 5 (middleware_test) |
| Correlation ID generated per HTTP request, propagated through context | Task 2 | Task 5 (middleware_test) |
| Log levels configurable per package via LOG_LEVEL env var | Task 4 (main.go zerolog setup) | Task 1 (config_test validates LOG_LEVEL) |
| Config struct loaded from .env via envconfig with validation | Task 1 | Task 1 (config_test) |
| NATS JetStream connection established on startup | Task 3, Task 4 | Task 3 (build verify) |
| NATS streams created: EVENTS, JOBS | Task 3 | Task 5 (bus_integration_test) |
| Bus package provides Publish/Subscribe abstractions | Task 3 | Task 5 (bus_integration_test) |
| Redis client initialized with connection pooling | Already done (cache/redis.go) | Existing health check |
| Graceful shutdown: close NATS, Redis, DB on SIGTERM/SIGINT | Task 4 | Manual verify (already has defer-based shutdown) |

## Story-Specific Compliance Rules

- Logging: All log output must be valid JSON (zerolog handles this) with fields: timestamp, level, correlation_id, service, message
- Config: All env vars loaded via envconfig with validation on startup — fail fast on invalid config
- Middleware: Correlation ID must be UUID v4, set as X-Request-ID response header
- NATS: Streams must be idempotent (CreateOrUpdate) — safe to restart
- Shutdown: Ordered shutdown within 10s timeout — HTTP server, NATS, Redis, DB
- ADR-001: Modular monolith — all packages in internal/, shared infra used by all services
- ADR-002: NATS JetStream for events/jobs, Redis for cache — both initialized with connection pooling

## Risks & Mitigations

- **Risk:** NATS test server dependency adds complexity to CI
  - **Mitigation:** Use build tags for integration tests, unit tests work without external services
- **Risk:** Changing middleware breaks existing auth flow
  - **Mitigation:** Custom middleware follows exact same Chi middleware pattern; existing route registrations unchanged
- **Risk:** Logger initialization order matters (correlation ID must be set before logging middleware)
  - **Mitigation:** Explicit middleware chain order documented and enforced in router.go

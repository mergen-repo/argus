# Implementation Plan: STORY-015 — RADIUS Authentication & Accounting Server

## Goal

Implement a production-ready RADIUS server (UDP :1812 auth, :1813 accounting) that authenticates IoT SIMs via IMSI lookup, manages session lifecycle with Redis cache + PostgreSQL (TBL-17), sends CoA/DM for mid-session changes, and publishes events via NATS.

## Architecture Context

### Components Involved

- **SVC-04 (AAA Engine)** — `internal/aaa/`: Core AAA service. Contains `session/` (session management, CoA, DM, sweep) and will contain the new `radius/` package for the RADIUS server.
- **Store Layer** — `internal/store/`: PostgreSQL data access. `sim.go` (SIM lookup), `operator.go` (operator config/shared secret), `ippool.go` (IP allocation). Will add `session_store.go` for TBL-17 session CRUD.
- **Cache Layer** — `internal/cache/redis.go`: Redis client wrapper. Used for SIM lookup caching (`sim:imsi:{imsi}`) and session state caching (`session:{id}`).
- **Event Bus** — `internal/bus/nats.go`: NATS JetStream publisher. Subjects: `argus.events.session.started`, `.updated`, `.ended`.
- **Gateway** — `internal/gateway/health.go`: Health handler. Must be extended with AAA status.
- **Config** — `internal/config/config.go`: Already has `RadiusAuthPort`, `RadiusAcctPort`, `RadiusSecret`, `RadiusWorkerPoolSize`, `RadiusCoAPort`.
- **Entry Point** — `cmd/argus/main.go`: Server lifecycle. RADIUS server must be started here and gracefully stopped.

### Data Flow

#### FLW-01: RADIUS Authentication
```
NAS (P-GW) → UDP :1812 Access-Request
  → Decode RADIUS packet (layeh/radius)
  → Validate authenticator (shared secret — per operator from TBL-05, fallback to RADIUS_SECRET env)
  → Extract IMSI from User-Name attribute
  → Redis: GET sim:imsi:{imsi} → SIM config (cache miss → PostgreSQL sims table → cache populate, TTL 5min)
  → Validate SIM state = 'active'
  → Validate operator health_status != 'down'
  → (Future: Policy Engine evaluation — for now, use SIM's timeout values)
  → Build Access-Accept: Framed-IP-Address, Session-Timeout, Idle-Timeout, Filter-Id
  → OR Build Access-Reject: Reply-Message with reason code
  → NATS: publish auth event (async)
  → Send UDP response
```

#### FLW-02: RADIUS Accounting
```
NAS → UDP :1813 Accounting-Request
  → Decode RADIUS packet
  → Validate authenticator
  → Extract Acct-Status-Type (Start=1, Stop=2, Interim=3)
  → Start:
      → Create session in Redis (session:{id}, JSON, TTL = session_timeout)
      → Insert session in TBL-17 (sessions table)
      → NATS: publish session.started
  → Interim-Update:
      → Update bytes_in/bytes_out in Redis session cache
      → Update last_interim_at
  → Stop:
      → Finalize session in TBL-17 (set ended_at, terminate_cause, final counters)
      → Delete session from Redis
      → NATS: publish session.ended
  → Send Accounting-Response
```

### API Specifications

```
GET /api/health
  Response: {
    status: "success",
    data: {
      db: "ok",
      redis: "ok",
      nats: "ok",
      aaa: { radius: "ok"|"stopped", sessions_active: int },
      uptime: "1h23m"
    }
  }
  Status: 200 OK | 503 Service Unavailable
```

### Database Schema

```sql
-- Source: migrations/20260320000002_core_schema.up.sql (ACTUAL — TBL-17 already exists)
CREATE TABLE IF NOT EXISTS sessions (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    sim_id UUID NOT NULL,
    tenant_id UUID NOT NULL,
    operator_id UUID NOT NULL,
    apn_id UUID,
    nas_ip INET,
    framed_ip INET,
    calling_station_id VARCHAR(50),
    called_station_id VARCHAR(100),
    rat_type VARCHAR(10),
    session_state VARCHAR(20) NOT NULL DEFAULT 'active',
    auth_method VARCHAR(20),
    policy_version_id UUID,
    acct_session_id VARCHAR(100),
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at TIMESTAMPTZ,
    terminate_cause VARCHAR(50),
    bytes_in BIGINT NOT NULL DEFAULT 0,
    bytes_out BIGINT NOT NULL DEFAULT 0,
    packets_in BIGINT NOT NULL DEFAULT 0,
    packets_out BIGINT NOT NULL DEFAULT 0,
    last_interim_at TIMESTAMPTZ
);

-- TimescaleDB hypertable partitioned by started_at (from migration 003)
-- Indexes: idx_sessions_sim_active, idx_sessions_tenant_active,
--   idx_sessions_tenant_operator, idx_sessions_acct_session
```

### Redis Key Schema

| Key Pattern | Value | TTL | Purpose |
|-------------|-------|-----|---------|
| `sim:imsi:{imsi}` | JSON SIM object | 5min | SIM lookup cache by IMSI |
| `session:{session_id}` | JSON Session object | session_timeout_sec | Active session state cache |
| `operator:secret:{operator_id}` | shared secret string | 1hr | Per-operator RADIUS shared secret |

### Existing Session Struct (internal/aaa/session/session.go)

The `Session` struct already exists with all needed fields: ID, SimID, TenantID, OperatorID, APNID, IMSI, MSISDN, APN, NASIP, AcctSessionID, FramedIP, SessionState, SessionTimeout, IdleTimeout, RATType, BytesIn, BytesOut, StartedAt, LastInterimAt, EndedAt, TerminateCause.

The `Manager` struct exists with stub methods: Create, Get, GetByAcctSessionID, ListActive, Stats, GetSessionsForSIM, UpdateCounters, Terminate.

### Existing CoA/DM Infrastructure

`internal/aaa/session/coa.go` — CoASender with SendCoA method (fully implemented)
`internal/aaa/session/dm.go` — DMSender with SendDM method (fully implemented)
`internal/aaa/session/sweep.go` — TimeoutSweeper with Start/Stop, uses Manager, DMSender, EventBus, Redis

### Operator Shared Secret

Operators table has `adapter_config JSONB` which can contain `{"radius_secret": "..."}`. The RADIUS server should:
1. Try to extract per-operator secret from adapter_config (matched by NAS-IP or IMSI prefix → operator lookup)
2. Fall back to global `RADIUS_SECRET` env var

## Prerequisites

- [x] STORY-001 (scaffold) — project structure, main.go
- [x] STORY-002 (DB schema) — TBL-17 sessions table exists in migration
- [x] STORY-010 (APN/IP) — IP pool management
- [x] STORY-011 (SIM CRUD) — SIM store with IMSI lookup capability

## Tasks

### Task 1: Session Store — PostgreSQL CRUD for TBL-17

- **Files:** Create `internal/store/session_radius.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/sim.go` — follow same store structure (struct, columns var, scan function, CRUD methods)
- **Context refs:** Database Schema, Architecture Context > Components Involved
- **What:**
  Create a `RadiusSessionStore` (name chosen to avoid collision with existing `SessionStore` in the auth package) with methods:
  - `Create(ctx, params) (*RadiusSession, error)` — INSERT into sessions table
  - `GetByAcctSessionID(ctx, acctSessionID string) (*RadiusSession, error)` — lookup by acct_session_id
  - `GetByID(ctx, id uuid.UUID) (*RadiusSession, error)` — lookup by primary key
  - `UpdateCounters(ctx, id uuid.UUID, bytesIn, bytesOut, packetsIn, packetsOut int64) error` — update byte/packet counters and last_interim_at
  - `Finalize(ctx, id uuid.UUID, terminateCause string, bytesIn, bytesOut, packetsIn, packetsOut int64) error` — set ended_at, terminate_cause, final counters, session_state='closed'
  - `CountActive(ctx) (int64, error)` — COUNT WHERE session_state='active' (for health check)
  - `ListActiveBySIM(ctx, simID uuid.UUID) ([]RadiusSession, error)` — get active sessions for a SIM

  Use a `RadiusSession` struct matching TBL-17 columns exactly. Use `pgxpool.Pool` as db handle. Follow the `scanSIM`/`simColumns` pattern from sim.go.
- **Verify:** `go build ./internal/store/...`

### Task 2: SIM Lookup with Redis Cache

- **Files:** Create `internal/aaa/radius/sim_cache.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/cache/redis.go` — follow Redis client usage pattern; Read `internal/store/sim.go` — understand SIM struct
- **Context refs:** Redis Key Schema, Architecture Context > Data Flow
- **What:**
  Create a `SIMCache` struct that provides `GetByIMSI(ctx, imsi string) (*store.SIM, error)`:
  1. Check Redis key `sim:imsi:{imsi}` — if hit, unmarshal JSON and return
  2. If miss, call `SIMStore.List(ctx, tenantID, ListSIMsParams{IMSI: imsi})` — but since we don't have tenantID at RADIUS level, we need a new method on SIMStore
  3. Add `GetByIMSI(ctx, imsi string) (*SIM, error)` method to the SIMStore (query without tenant_id filter since IMSI is globally unique)
  4. On DB hit, cache to Redis with 5-minute TTL
  5. Return the SIM or ErrSIMNotFound

  The SIMCache needs: `*redis.Client`, `*store.SIMStore`, `zerolog.Logger`.

  Also add `InvalidateIMSI(ctx, imsi string) error` for cache invalidation.

  **Important:** The `GetByIMSI` store method queries `SELECT {simColumns} FROM sims WHERE imsi = $1 LIMIT 1` (no tenant_id filter — IMSI is globally unique per the UNIQUE index on sims).
- **Verify:** `go build ./internal/aaa/... && go build ./internal/store/...`

### Task 3: RADIUS Server Core — Packet Handler + UDP Listeners

- **Files:** Create `internal/aaa/radius/server.go`
- **Depends on:** Task 1, Task 2
- **Complexity:** high
- **Pattern ref:** Read `internal/aaa/session/coa.go` — follow layeh/radius packet construction pattern; Read `internal/aaa/session/sweep.go` — follow Start/Stop lifecycle pattern
- **Context refs:** Architecture Context > Data Flow, Architecture Context > Components Involved, Database Schema, Redis Key Schema
- **What:**
  Create a `Server` struct with:

  **Dependencies (constructor params):**
  - `authAddr string` (":1812")
  - `acctAddr string` (":1813")
  - `defaultSecret string`
  - `workerPoolSize int`
  - `simCache *SIMCache`
  - `sessionStore *store.RadiusSessionStore`
  - `operatorStore *store.OperatorStore`
  - `eventBus *bus.EventBus`
  - `redisClient *redis.Client`
  - `coaSender *session.CoASender`
  - `dmSender *session.DMSender`
  - `logger zerolog.Logger`

  **Server lifecycle:**
  - `Start(ctx context.Context) error` — start two `radius.PacketServer` instances (auth + acct) in goroutines. Use `layeh.com/radius` PacketServer with Handler interface.
  - `Stop(ctx context.Context) error` — graceful shutdown: close listeners, wait for in-flight requests (5s deadline)
  - `Healthy() bool` — returns true if both listeners are running

  **Auth handler (port 1812):**
  - Receive Access-Request
  - Extract IMSI from User-Name attribute (`rfc2865.UserName_Lookup`)
  - Call `simCache.GetByIMSI(ctx, imsi)`
  - If SIM not found → Access-Reject with Reply-Message "SIM_NOT_FOUND"
  - If SIM state != "active" → Access-Reject with Reply-Message "SIM_{STATE}" (e.g., SIM_SUSPENDED)
  - Look up operator via `operatorStore.GetByID(ctx, sim.OperatorID)`
  - If operator health_status == "down" → Access-Reject with Reply-Message "OPERATOR_UNAVAILABLE"
  - Build Access-Accept response:
    - Set Framed-IP-Address from SIM's assigned IP (look up via ip_address_id)
    - Set Session-Timeout from SIM's session_hard_timeout_sec
    - Set Idle-Timeout from SIM's session_idle_timeout_sec
    - Set Filter-Id to "default" (placeholder for policy engine integration)
  - Log with correlation ID (use RADIUS packet Identifier + NAS-IP as correlation)

  **Acct handler (port 1813):**
  - Receive Accounting-Request
  - Extract Acct-Status-Type (`rfc2866.AcctStatusType_Lookup`)
  - Extract Acct-Session-Id, User-Name (IMSI), NAS-IP-Address
  - **Start (type=1):**
    - Look up SIM via simCache
    - Create session in DB via sessionStore.Create
    - Cache session in Redis key `session:{session.ID}` with JSON, TTL = session_hard_timeout_sec
    - Publish `bus.SubjectSessionStarted` via eventBus
  - **Interim-Update (type=3):**
    - Look up session by Acct-Session-Id in Redis first, then DB
    - Extract Acct-Input-Octets, Acct-Output-Octets (+ Gigawords for >4GB)
    - Combine: `totalIn = (gigawordsIn << 32) + inputOctets`
    - Update counters in Redis session cache
    - Update sessionStore.UpdateCounters
  - **Stop (type=2):**
    - Look up session by Acct-Session-Id
    - Extract final counters and Acct-Terminate-Cause
    - Finalize in DB via sessionStore.Finalize
    - Delete session from Redis
    - Publish `bus.SubjectSessionEnded` via eventBus
  - Send Accounting-Response for all types

  **Shared secret resolution:**
  - Create helper `getSecret(operatorID) []byte`: check operator's adapter_config for "radius_secret" key, fall back to defaultSecret
  - The layeh/radius library's `PacketServer` uses a `SecretSource` interface — implement it to resolve per-request

  **Concurrency:**
  - Use a semaphore (buffered channel of size workerPoolSize) to limit concurrent handler goroutines
  - Each request gets its own goroutine from the pool
- **Verify:** `go build ./internal/aaa/...`

### Task 4: Session Manager Implementation — Wire Redis + DB

- **Files:** Modify `internal/aaa/session/session.go`
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** Read `internal/aaa/session/sweep.go` — follow Redis session read/write pattern; Read `internal/store/sim.go` — follow store method pattern
- **Context refs:** Architecture Context > Existing Session Struct, Redis Key Schema, Database Schema
- **What:**
  Replace the stub `Manager` with a real implementation that wires Redis cache + PostgreSQL store:

  **Updated Manager struct fields:**
  - `sessionStore *store.RadiusSessionStore`
  - `redisClient *redis.Client`
  - `logger zerolog.Logger`

  **Updated constructor:** `NewManager(sessionStore *store.RadiusSessionStore, redisClient *redis.Client, logger zerolog.Logger) *Manager`

  **Implement methods:**
  - `Create(ctx, sess *Session) error`:
    1. Insert into DB via sessionStore.Create (map Session fields to CreateRadiusSessionParams)
    2. Cache in Redis: `session:{sess.ID}` → JSON, TTL = sess.SessionTimeout seconds
  - `Get(ctx, id string) (*Session, error)`:
    1. Try Redis `session:{id}` first
    2. Fallback to sessionStore.GetByID
  - `GetByAcctSessionID(ctx, acctSessionID string) (*Session, error)`:
    1. Try Redis SCAN for matching session (or keep an index key `session:acct:{acctSessionID}` → session ID)
    2. Fallback to sessionStore.GetByAcctSessionID
    3. Better approach: maintain a Redis key `session:acct:{acctSessionID}` → session_id, then use Get
  - `UpdateCounters(ctx, id string, bytesIn, bytesOut uint64) error`:
    1. Update Redis session cache (GET, unmarshal, update, SET)
    2. Update DB via sessionStore.UpdateCounters
  - `Terminate(ctx, id string, cause string) error`:
    1. Finalize in DB via sessionStore.Finalize
    2. Delete from Redis: `session:{id}` and `session:acct:{acctSessionID}`
  - `ListActive(ctx, cursor string, limit int, filter SessionFilter) ([]*Session, string, error)`: delegate to DB
  - `Stats(ctx) (*SessionStats, error)`: query DB for aggregate stats
  - `GetSessionsForSIM(ctx, simID string) ([]*Session, error)`: delegate to DB

  **Redis index key pattern:** When creating a session, also SET `session:acct:{acctSessionID}` → `{sessionID}` with same TTL. This enables O(1) lookup by Acct-Session-Id without SCAN.
- **Verify:** `go build ./internal/aaa/...`

### Task 5: Health Handler Extension — AAA Status

- **Files:** Modify `internal/gateway/health.go`
- **Depends on:** Task 3
- **Complexity:** low
- **Pattern ref:** Read `internal/gateway/health.go` — extend existing pattern
- **Context refs:** API Specifications, Architecture Context > Components Involved
- **What:**
  Extend the `HealthHandler` to include AAA RADIUS status:

  1. Add an `AAAHealthChecker` interface to HealthHandler:
     ```
     type AAAHealthChecker interface {
         Healthy() bool
         ActiveSessionCount(ctx context.Context) (int64, error)
     }
     ```
  2. Add `aaa AAAHealthChecker` field to `HealthHandler` (optional — nil if RADIUS not started)
  3. Update `NewHealthHandler` to accept optional AAA checker (or add a `SetAAAChecker` method)
  4. Extend `healthData` struct with `AAA *aaaHealthData` field:
     ```
     type aaaHealthData struct {
         Radius         string `json:"radius"`
         SessionsActive int64  `json:"sessions_active"`
     }
     ```
  5. In `Check` method: if aaa != nil, call `aaa.Healthy()` and `aaa.ActiveSessionCount(ctx)`, populate AAA field

  The RADIUS Server (`internal/aaa/radius/server.go`) will implement this interface.
- **Verify:** `go build ./internal/gateway/...`

### Task 6: Main Integration — Wire RADIUS Server into cmd/argus/main.go

- **Files:** Modify `cmd/argus/main.go`
- **Depends on:** Task 3, Task 4, Task 5
- **Complexity:** medium
- **Pattern ref:** Read `cmd/argus/main.go` — follow existing service initialization and shutdown pattern
- **Context refs:** Architecture Context > Components Involved
- **What:**
  Wire the RADIUS server into the application lifecycle:

  1. After existing service initialization (after `healthChecker` setup):
     - Create `RadiusSessionStore` from pg.Pool
     - Create `SIMCache` with redis client and sim store
     - Create `session.Manager` with RadiusSessionStore and redis client
     - Create `session.CoASender` and `session.DMSender` with config
     - Create RADIUS `Server` with all dependencies
  2. Start RADIUS server: `radiusServer.Start(ctx)`
  3. Wire health: pass RADIUS server to health handler via `SetAAAChecker`
  4. In shutdown sequence (before closing infra):
     - `radiusServer.Stop(shutdownCtx)` — stop accepting, drain in-flight
  5. Only start RADIUS if `cfg.RadiusSecret != ""` (RADIUS is optional)
- **Verify:** `go build ./cmd/argus/...`

### Task 7: RADIUS Server Tests

- **Files:** Create `internal/aaa/radius/server_test.go`, Create `internal/store/session_radius_test.go`
- **Depends on:** Task 3, Task 4
- **Complexity:** high
- **Pattern ref:** Read `internal/aaa/session/coa_test.go` — follow RADIUS test pattern; Read `internal/store/sim_test.go` — follow store test pattern
- **Context refs:** Architecture Context > Data Flow, Database Schema, Acceptance Criteria Mapping
- **What:**
  Write unit tests covering the story's test scenarios:

  **`internal/aaa/radius/server_test.go`:**
  - Test auth handler: valid IMSI → Access-Accept with Framed-IP attributes
  - Test auth handler: unknown IMSI → Access-Reject with SIM_NOT_FOUND
  - Test auth handler: suspended SIM → Access-Reject with SIM_SUSPENDED
  - Test auth handler: operator down → Access-Reject with OPERATOR_UNAVAILABLE
  - Test auth handler: invalid shared secret → silent drop (no response)
  - Test acct handler: Start → session created, event published
  - Test acct handler: Interim → counters updated
  - Test acct handler: Stop → session finalized, event published
  - Test malformed packet → discard with error log

  Use mock interfaces for store, cache, and event bus. Use `layeh.com/radius` to construct test packets.
  Create mock/interface types for SIMCache, RadiusSessionStore, OperatorStore, EventBus to enable unit testing.

  **`internal/store/session_radius_test.go`:**
  - Test Create + GetByAcctSessionID roundtrip
  - Test UpdateCounters
  - Test Finalize
  - Test CountActive

  These tests follow the existing pattern of using real SQL with a test database (see existing `*_test.go` files in store/).
- **Verify:** `go test ./internal/aaa/radius/... && go test ./internal/store/...`

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| RADIUS server listens on UDP :1812 (auth) and :1813 (accounting) | Task 3 | Task 7 |
| Access-Request: parse IMSI from User-Name, lookup SIM | Task 2, Task 3 | Task 7 |
| Access-Request: validate SIM state is ACTIVE, operator healthy | Task 3 | Task 7 |
| Access-Request: delegate to Policy Engine (placeholder) | Task 3 | Task 7 |
| Access-Accept: include Framed-IP, Session-Timeout, QoS attrs | Task 3 | Task 7 |
| Access-Reject: include Reply-Message with reject reason | Task 3 | Task 7 |
| Accounting Start: create session in Redis + TBL-17, publish NATS | Task 3, Task 4 | Task 7 |
| Accounting Interim: update bytes in Redis | Task 3, Task 4 | Task 7 |
| Accounting Stop: finalize TBL-17, remove Redis, publish NATS | Task 3, Task 4 | Task 7 |
| CoA: send mid-session policy update to NAS | Already implemented (coa.go) | Task 7 |
| DM: force disconnect active session | Already implemented (dm.go) | Task 7 |
| Shared secret per operator | Task 3 | Task 7 |
| RADIUS packet logging with correlation ID | Task 3 | Task 7 |
| Health check reports AAA status | Task 5 | Task 7 |
| Graceful shutdown: drain in-flight within 5s | Task 3, Task 6 | Manual verify |

## Story-Specific Compliance Rules

- **RADIUS RFC 2865/2866:** Authenticator validation using shared secret. Access-Reject on invalid secret = silent drop per RFC.
- **layeh/radius library:** Use `radius.PacketServer` for UDP handling. Use `rfc2865.*` and `rfc2866.*` attribute helpers.
- **Redis cache:** SIM cache TTL 5min, session cache TTL = session_timeout. Use JSON serialization.
- **NATS events:** Use existing subjects from `bus/nats.go`: `SubjectSessionStarted`, `SubjectSessionUpdated`, `SubjectSessionEnded`.
- **DB:** Sessions table is a TimescaleDB hypertable. Use `started_at` as the time dimension. No migration needed (table already exists).
- **Naming:** Go = camelCase, DB = snake_case. Store methods follow existing pattern (Create, GetByID, etc.).
- **Graceful shutdown:** RADIUS server Stop must complete within 5 seconds. Use context with deadline.
- **No migration needed:** TBL-17 (sessions) already exists in `20260320000002_core_schema.up.sql` with all required columns.

## Risks & Mitigations

- **Risk: layeh/radius PacketServer API differences** — The library's API for custom secret sources may need adaptation. Mitigation: Read existing usage in coa.go/dm.go for patterns.
- **Risk: Concurrent IMSI access** — Same IMSI authenticating simultaneously could cause race conditions. Mitigation: Use Redis SET NX for session creation dedup, database-level constraints.
- **Risk: Session Manager change breaks sweep.go** — Changing Manager struct/constructor affects TimeoutSweeper. Mitigation: Update sweep.go constructor call in the same task.

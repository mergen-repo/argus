# Implementation Plan: STORY-009 - Operator CRUD & Health Check

## Goal
Implement full CRUD for operators (system-level, super_admin only), operator grants (tenant access), health check heartbeat with circuit breaker state tracking, and a mock simulator adapter for testing.

## Architecture Context

### Components Involved
- **SVC-03 (Core API)**: `internal/api/operator/handler.go` — HTTP handlers for operator CRUD, grants, health, test
- **SVC-06 (Operator Router)**: `internal/operator/` — Health check loop, circuit breaker integration
- **Store Layer**: `internal/store/operator.go` — PostgreSQL data access for operators, grants, health logs
- **Cache Layer**: `internal/cache/` — Redis cache for health status
- **Adapter Layer**: `internal/operator/adapter/` — Already has mock.go, registry.go, types.go

### Data Flow

**Create Operator:**
```
POST /api/v1/operators → JWTAuth → RequireRole(super_admin)
  → handler.Create → validate input → encrypt adapter_config
  → store.Create (INSERT operators) → return operator response
```

**Health Check Loop:**
```
HealthChecker.Start() → per-operator ticker (health_check_interval_sec)
  → adapter.HealthCheck(ctx) → record result
  → circuit breaker update (success/failure)
  → store.InsertHealthLog (INSERT operator_health_logs)
  → cache.SetOperatorHealth (Redis SET)
  → update operators.health_status if changed
```

**Test Connection:**
```
POST /api/v1/operators/:id/test → JWTAuth → RequireRole(super_admin)
  → handler.TestConnection → get adapter from registry
  → adapter.HealthCheck(ctx) → return latency + status
```

### API Specifications

**API-020: GET /api/v1/operators** — List all operators
- Auth: JWT (super_admin)
- Query params: `cursor`, `limit`, `state`
- Success: `{ status: "success", data: [{ id, name, code, mcc, mnc, adapter_type, health_status, supported_rat_types, state, created_at }], meta: { cursor, limit, has_more } }`
- Status codes: 200

**API-021: POST /api/v1/operators** — Create operator
- Auth: JWT (super_admin)
- Request body: `{ name, code, mcc, mnc, adapter_type, adapter_config, supported_rat_types, failover_policy, failover_timeout_ms, circuit_breaker_threshold, circuit_breaker_recovery_sec, health_check_interval_sec, sla_uptime_target, sm_dp_plus_url, sm_dp_plus_config }`
- Success (201): `{ status: "success", data: { id, name, code, mcc, mnc, adapter_type, supported_rat_types, health_status, failover_policy, state, created_at } }`
- Errors: 400 (bad JSON), 409 (duplicate code or mcc+mnc), 422 (validation)

**API-022: PATCH /api/v1/operators/:id** — Update operator
- Auth: JWT (super_admin)
- Request body: partial update fields (name, adapter_config, failover_policy, circuit_breaker_threshold, circuit_breaker_recovery_sec, health_check_interval_sec, sla_uptime_target, state, supported_rat_types)
- Success (200): updated operator
- Errors: 400, 404, 422

**API-023: GET /api/v1/operators/:id/health** — Get health status
- Auth: JWT (operator_manager+)
- Success (200): `{ status: "success", data: { health_status, latency_ms, circuit_state, last_check, uptime_24h, failure_count } }`
- Errors: 404

**API-024: POST /api/v1/operators/:id/test** — Test connection
- Auth: JWT (super_admin)
- Success (200): `{ status: "success", data: { success: bool, latency_ms: int, error?: string } }`
- Errors: 404

**API-026: POST /api/v1/operator-grants** — Grant operator to tenant
- Auth: JWT (super_admin)
- Request body: `{ tenant_id, operator_id }`
- Success (201): `{ status: "success", data: { id, tenant_id, operator_id, enabled, granted_at } }`
- Errors: 409 (already granted), 404 (tenant or operator not found)

**API-025: GET /api/v1/operator-grants** — List grants
- Auth: JWT (tenant_admin+)
- Query params: `tenant_id` (required for super_admin listing specific tenant; auto-scoped for non-super_admin)
- Success (200): grant list with operator name

**API-027: DELETE /api/v1/operator-grants/:id** — Revoke grant
- Auth: JWT (super_admin)
- Success (204)
- Errors: 404

### Database Schema

```sql
-- Source: migrations/20260320000002_core_schema.up.sql (ACTUAL — tables already exist)

-- TBL-05: operators
CREATE TABLE IF NOT EXISTS operators (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL UNIQUE,
    code VARCHAR(20) NOT NULL UNIQUE,
    mcc VARCHAR(3) NOT NULL,
    mnc VARCHAR(3) NOT NULL,
    adapter_type VARCHAR(30) NOT NULL,
    adapter_config JSONB NOT NULL DEFAULT '{}',
    sm_dp_plus_url VARCHAR(500),
    sm_dp_plus_config JSONB DEFAULT '{}',
    supported_rat_types VARCHAR[] NOT NULL DEFAULT '{}',
    health_status VARCHAR(20) NOT NULL DEFAULT 'unknown',
    health_check_interval_sec INTEGER NOT NULL DEFAULT 30,
    failover_policy VARCHAR(20) NOT NULL DEFAULT 'reject',
    failover_timeout_ms INTEGER NOT NULL DEFAULT 5000,
    circuit_breaker_threshold INTEGER NOT NULL DEFAULT 5,
    circuit_breaker_recovery_sec INTEGER NOT NULL DEFAULT 60,
    sla_uptime_target DECIMAL(5,2) DEFAULT 99.90,
    state VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- Indexes: idx_operators_code UNIQUE, idx_operators_mcc_mnc UNIQUE, idx_operators_state

-- TBL-06: operator_grants
CREATE TABLE IF NOT EXISTS operator_grants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    operator_id UUID NOT NULL REFERENCES operators(id),
    enabled BOOLEAN NOT NULL DEFAULT true,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    granted_by UUID REFERENCES users(id)
);
-- Indexes: idx_operator_grants_tenant_op UNIQUE(tenant_id, operator_id), idx_operator_grants_tenant

-- TBL-23: operator_health_logs (TimescaleDB hypertable)
CREATE TABLE IF NOT EXISTS operator_health_logs (
    id BIGSERIAL,
    operator_id UUID NOT NULL,
    checked_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    status VARCHAR(20) NOT NULL,
    latency_ms INTEGER,
    error_message TEXT,
    circuit_state VARCHAR(20) NOT NULL
);
-- Index: idx_op_health_operator_time on (operator_id, checked_at DESC)
```

### Encryption Approach for adapter_config

The `adapter_config` JSONB field contains sensitive data (shared secrets, API keys). For v1:
- Use AES-256-GCM encryption at the application level
- Store encrypted blob in adapter_config
- Decrypt on read in the store layer
- Add `ENCRYPTION_KEY` env var to config (32-byte hex key)
- Mask sensitive fields in API responses (return `"***"` for secret values)

### Existing Code to Integrate With

- `internal/store/stubs.go` has a stub `OperatorStore` with `GetByCode` — must be replaced with full implementation
- `internal/operator/adapter/registry.go` has adapter factory pattern — reuse for health check
- `internal/operator/circuit_breaker.go` has `CircuitBreaker` type — integrate with health check
- `internal/operator/adapter/mock.go` has `MockAdapter.HealthCheck()` — already implemented
- `internal/gateway/router.go` needs new route group for operators

## Prerequisites
- [x] STORY-005 completed (tenant management, user CRUD — provides tenant store, user store, audit service, RBAC middleware)
- [x] Migration files exist with operators, operator_grants, operator_health_logs tables
- [x] Adapter framework exists (mock, radius, diameter adapters)
- [x] Circuit breaker exists in `internal/operator/circuit_breaker.go`

## Tasks

### Task 1: Operator Store — Full CRUD implementation
- **Files:** Modify `internal/store/operator.go` (new file replacing stubs in stubs.go), Modify `internal/store/stubs.go`
- **Depends on:** —
- **Complexity:** high
- **Pattern ref:** Read `internal/store/tenant.go` — follow same store structure (struct, params, CRUD methods, cursor pagination, dynamic UPDATE)
- **Context refs:** Database Schema, API Specifications, Encryption Approach for adapter_config
- **What:**
  - Create full `Operator` struct with ALL columns from TBL-05 (id, name, code, mcc, mnc, adapter_type, adapter_config, sm_dp_plus_url, sm_dp_plus_config, supported_rat_types, health_status, health_check_interval_sec, failover_policy, failover_timeout_ms, circuit_breaker_threshold, circuit_breaker_recovery_sec, sla_uptime_target, state, created_at, updated_at). Use `json.RawMessage` for JSONB fields, `[]string` for VARCHAR[], `*float64` for DECIMAL, `time.Time` for TIMESTAMPTZ.
  - Create `OperatorGrant` struct with all TBL-06 columns.
  - Create `OperatorHealthLog` struct with all TBL-23 columns.
  - Create `CreateOperatorParams`, `UpdateOperatorParams` structs.
  - Implement `Create`, `GetByID`, `GetByCode`, `List` (cursor-based pagination with state filter), `Update` (dynamic SET like tenant.go pattern).
  - Implement `UpdateHealthStatus(ctx, id, status)` to update `operators.health_status`.
  - Implement grant methods: `CreateGrant`, `ListGrants` (by tenant_id), `DeleteGrant`, `GetGrantByID`.
  - Implement health log methods: `InsertHealthLog`, `GetLatestHealth` (latest log for operator), `GetHealthLogs` (last N logs for operator), `CountFailures24h` (count failures in last 24h for uptime calc).
  - Remove the stub `Operator` struct and `OperatorStore` from `stubs.go`.
  - Use `::text` cast for any INET columns in SELECT; all TIMESTAMPTZ columns use `time.Time` in Go structs.
  - For supported_rat_types (VARCHAR[] in PG), use pgx array scanning.
  - For sla_uptime_target (DECIMAL), use `*float64`.
- **Verify:** `go build ./internal/store/...`

### Task 2: Encryption utility for adapter_config
- **Files:** Create `internal/crypto/aes.go`, Modify `internal/config/config.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/config/config.go` — follow same config pattern for new env var
- **Context refs:** Encryption Approach for adapter_config
- **What:**
  - Create `internal/crypto/aes.go` with `Encrypt(plaintext []byte, key []byte) ([]byte, error)` and `Decrypt(ciphertext []byte, key []byte) ([]byte, error)` using AES-256-GCM.
  - `Encrypt`: generate random 12-byte nonce, seal with GCM, prepend nonce to ciphertext, return base64-encoded result.
  - `Decrypt`: base64-decode, extract nonce (first 12 bytes), open with GCM.
  - Add `EncryptionKey string` field to `config.Config` struct (`envconfig:"ENCRYPTION_KEY"`). Not required (empty means no encryption — dev mode).
  - Add helper `EncryptJSON(data json.RawMessage, key string) (json.RawMessage, error)` and `DecryptJSON(data json.RawMessage, key string) (json.RawMessage, error)` that handle the string conversion.
  - If key is empty, return data as-is (passthrough for dev).
- **Verify:** `go build ./internal/crypto/... && go build ./internal/config/...`

### Task 3: Operator API Handler — CRUD + Grants
- **Files:** Create `internal/api/operator/handler.go`
- **Depends on:** Task 1, Task 2
- **Complexity:** high
- **Pattern ref:** Read `internal/api/tenant/handler.go` — follow same handler structure (Handler struct, request/response types, toResponse converter, validation, audit entry, apierr usage)
- **Context refs:** API Specifications, Architecture Context > Components Involved, Database Schema
- **What:**
  - Create `Handler` struct with `operatorStore`, `tenantStore`, `auditSvc`, `encryptionKey`, `adapterRegistry`, `logger` fields.
  - Implement `List` (GET /api/v1/operators) — cursor pagination, state filter.
  - Implement `Create` (POST /api/v1/operators) — validate required fields (name, code, mcc, mnc, adapter_type), validate adapter_type is registered in registry, encrypt adapter_config before store, create audit entry.
  - Implement `Update` (PATCH /api/v1/operators/:id) — partial update, encrypt adapter_config if changed, create audit entry with before/after.
  - Implement `GetHealth` (GET /api/v1/operators/:id/health) — get latest health from store, calculate uptime_24h from failure count.
  - Implement `TestConnection` (POST /api/v1/operators/:id/test) — get or create adapter from registry, call HealthCheck, return result.
  - Implement `CreateGrant` (POST /api/v1/operator-grants) — validate tenant_id and operator_id exist, create grant, audit.
  - Implement `ListGrants` (GET /api/v1/operator-grants) — for super_admin: require tenant_id query param; for others: auto-scope to own tenant.
  - Implement `DeleteGrant` (DELETE /api/v1/operator-grants/:id) — soft-delete or hard-delete, audit.
  - Response types: `operatorResponse` (mask adapter_config sensitive fields), `grantResponse`, `healthResponse`.
  - All responses use standard envelope via `apierr.WriteSuccess`, `apierr.WriteList`, `apierr.WriteError`.
- **Verify:** `go build ./internal/api/operator/...`

### Task 4: Health Check Service — Background loop + Redis cache
- **Files:** Create `internal/operator/health.go`
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** Read `internal/operator/circuit_breaker.go` — follow same package pattern; Read `internal/operator/adapter/mock.go` — understand HealthCheck interface
- **Context refs:** Architecture Context > Data Flow, Database Schema
- **What:**
  - Create `HealthChecker` struct with fields: `store` (OperatorStore), `registry` (adapter.Registry), `redisClient`, `breakers` map[uuid.UUID]*CircuitBreaker, `logger`, `stopCh`, `wg`.
  - `Start(ctx)`: load all active operators from store, for each start a goroutine that ticks every `health_check_interval_sec`.
  - Per-tick: call `adapter.HealthCheck(ctx)`, update circuit breaker (RecordSuccess/RecordFailure), insert health log, update operator health_status if state changed, cache health status in Redis (`operator:health:{id}` key, JSON value, TTL = 2 * interval).
  - Health status logic: if circuit breaker state is `open` → `down`; if `half_open` → `degraded`; if `closed` and last check success → `healthy`; if `closed` but last check failed → `degraded`.
  - `Stop()`: close stopCh, wait for all goroutines.
  - `GetCachedHealth(ctx, operatorID) (*CachedHealth, error)`: read from Redis, fallback to DB.
  - `RefreshOperator(operatorID)`: called when operator config changes — restart its health check goroutine with new config.
  - Circuit breaker per operator: created from operator's `circuit_breaker_threshold` and `circuit_breaker_recovery_sec`.
  - Redis key format: `operator:health:{operator_id}` with JSON value `{ status, latency_ms, circuit_state, checked_at }`.
- **Verify:** `go build ./internal/operator/...`

### Task 5: Router Integration + main.go wiring
- **Files:** Modify `internal/gateway/router.go`, Modify `cmd/argus/main.go`
- **Depends on:** Task 3, Task 4
- **Complexity:** medium
- **Pattern ref:** Read `internal/gateway/router.go` — follow same route group pattern; Read `cmd/argus/main.go` — follow same dependency wiring
- **Context refs:** API Specifications, Architecture Context > Components Involved
- **What:**
  - Add `OperatorHandler *operatorapi.Handler` to `RouterDeps` struct.
  - Add route groups for operator endpoints:
    - `r.Group` with `JWTAuth` + `RequireRole("super_admin")`: POST /api/v1/operators, PATCH /api/v1/operators/{id}, POST /api/v1/operators/{id}/test, POST /api/v1/operator-grants, DELETE /api/v1/operator-grants/{id}
    - `r.Group` with `JWTAuth` + `RequireRole("super_admin")`: GET /api/v1/operators (super_admin only per story)
    - `r.Group` with `JWTAuth` + `RequireRole("operator_manager")`: GET /api/v1/operators/{id}/health
    - `r.Group` with `JWTAuth` + `RequireRole("api_user")`: GET /api/v1/operator-grants (tenant_admin+ enforced in handler)
  - In `main.go`:
    - Create `operatorStore := store.NewOperatorStore(pg.Pool)`
    - Create `adapterRegistry := adapter.NewRegistry()`
    - Create `operatorHandler := operatorapi.NewHandler(operatorStore, tenantStore, auditSvc, cfg.EncryptionKey, adapterRegistry, log.Logger)`
    - Create `healthChecker := operator.NewHealthChecker(operatorStore, adapterRegistry, rdb.Client, log.Logger)`
    - Start health checker: `healthChecker.Start(ctx)`
    - Add `OperatorHandler: operatorHandler` to RouterDeps
    - Shutdown: `healthChecker.Stop()` before closing connections
- **Verify:** `go build ./cmd/argus/...`

### Task 6: Unit Tests — Store, Handler, Health Check
- **Files:** Create `internal/store/operator_test.go`, Create `internal/api/operator/handler_test.go`, Create `internal/crypto/aes_test.go`, Create `internal/operator/health_test.go`
- **Depends on:** Task 1, Task 2, Task 3, Task 4
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/tenant_test.go` — follow same test pattern (struct tests, error sentinel tests); Read `internal/api/tenant/handler_test.go`
- **Context refs:** API Specifications, Database Schema
- **What:**
  - `operator_test.go`: Test Operator struct fields, CreateOperatorParams defaults, UpdateOperatorParams optional fields, error sentinel values (ErrOperatorNotFound, ErrOperatorCodeExists).
  - `handler_test.go`: Test request validation (missing name → 422, missing code → 422, invalid adapter_type → 422), test response structure matches API spec, test duplicate code → 409 scenario.
  - `aes_test.go`: Test encrypt/decrypt roundtrip, test empty key passthrough, test tampered ciphertext fails.
  - `health_test.go`: Test health status logic (circuit states → health status mapping), test CachedHealth struct.
  - Test scenarios from story:
    - Create operator with mock adapter → validates struct
    - Grant operator to tenant → validates grant struct
    - Health check failure increments circuit breaker counter (use existing circuit_breaker_test.go)
    - 5 consecutive failures → circuit opens (already tested in circuit_breaker.go)
    - Duplicate operator code → 409 (handler test)
- **Verify:** `go test ./internal/store/... ./internal/api/operator/... ./internal/crypto/... ./internal/operator/...`

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| POST /api/v1/operators creates operator with adapter config (encrypted) | Task 1 (store), Task 2 (crypto), Task 3 (handler) | Task 6 |
| GET /api/v1/operators lists all operators with health status | Task 1 (store), Task 3 (handler) | Task 6 |
| PATCH /api/v1/operators/:id updates config, failover, circuit breaker | Task 1 (store), Task 3 (handler) | Task 6 |
| POST /api/v1/operators/:id/test sends test via adapter | Task 3 (handler) | Task 6 |
| POST /api/v1/operator-grants grants tenant access | Task 1 (store), Task 3 (handler) | Task 6 |
| Health check runs every interval per operator | Task 4 (health checker) | Task 6 |
| Health status persisted to TBL-23 and cached in Redis | Task 4 (health checker) | Task 6 |
| Circuit breaker state tracked | Task 4 (health checker) + existing circuit_breaker.go | Task 6 |
| Mock simulator responds to health checks | Already implemented in adapter/mock.go | Task 6 |
| adapter_config encrypted at rest (AES-256) | Task 2 (crypto), Task 3 (handler) | Task 6 |

## Story-Specific Compliance Rules

- **API:** Standard envelope `{ status, data, meta?, error? }` for all responses
- **DB:** No new migrations needed — tables already exist in core_schema. New Go code only.
- **Auth:** Operators are super_admin only (create, update, delete, test, grant). Health check is operator_manager+. Grant listing is tenant_admin+ (auto-scoped).
- **Security:** adapter_config encrypted with AES-256-GCM, masked in API responses
- **Cursor pagination:** Same pattern as tenant list — ORDER BY created_at DESC, id DESC
- **pgx INET fix:** Use `::text` cast in SELECT for INET columns, `::inet` in INSERT/UPDATE
- **TIMESTAMPTZ:** All timestamp fields use `time.Time` / `*time.Time` in Go structs, never string

## Risks & Mitigations

- **Risk:** OperatorStore in stubs.go is referenced by import.go (SIM bulk import) — must keep backward compatibility when replacing.
  **Mitigation:** Keep same struct name and method signatures, add new methods alongside.
- **Risk:** Health checker goroutines could leak on shutdown.
  **Mitigation:** Use context cancellation + sync.WaitGroup for clean shutdown.
- **Risk:** Encryption key management in dev vs prod.
  **Mitigation:** Empty key = passthrough (no encryption in dev mode).

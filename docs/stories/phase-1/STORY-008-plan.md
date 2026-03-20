# Implementation Plan: STORY-008 - API Key Management, Rate Limiting & OAuth2

## Goal
Implement full CRUD for API keys with SHA-256 hashing, scope-based access control, Redis sliding window rate limiting, key rotation with grace period, and revocation — enabling M2M integrations with configurable rate limits.

## Architecture Context

### Components Involved
- **internal/store/apikey.go**: Data access layer for TBL-04 (api_keys) — CRUD operations with tenant scoping
- **internal/api/apikey/handler.go**: HTTP handlers for API-150 through API-154 — key management endpoints
- **internal/gateway/apikey_auth.go**: API key authentication middleware — parses X-API-Key header, validates SHA-256 hash
- **internal/gateway/ratelimit.go**: Rate limiting middleware — Redis sliding window counter
- **internal/gateway/router.go**: Route registration — adds API key routes and middleware

### Data Flow
```
Create API Key:
  POST /api/v1/api-keys (JWT auth, tenant_admin+)
  → handler validates request (name, scopes, rate limits)
  → generates random key: argus_{prefix}_{secret}
  → stores key_prefix + SHA-256(full_key) in DB
  → returns full key ONCE in response
  → subsequent GET only shows prefix

API Key Auth:
  Request with X-API-Key: argus_{prefix}_{secret}
  → auth middleware extracts prefix from key
  → looks up key_prefix in api_keys table
  → validates SHA-256(full_key) == stored key_hash
  → checks: not revoked, not expired, tenant active
  → injects tenant_id, api_key_id, scopes into context
  → rate limiter checks per-key limits via Redis sliding window
  → handler processes request

Rate Limiting:
  → identifies caller: api_key_id or user_id or IP
  → Redis sliding window counter per identifier+endpoint+window
  → on limit exceeded: 429 + Retry-After header + RATE_LIMITED error
  → response headers: X-RateLimit-Limit, X-RateLimit-Remaining, X-RateLimit-Reset
```

### API Specifications

#### API-151: POST /api/v1/api-keys — Create API Key
- Auth: JWT (tenant_admin+)
- Request body:
  ```json
  {
    "name": "string (required, 1-100 chars)",
    "scopes": ["string"] (required, e.g. ["sims:read", "cdrs:*"]),
    "rate_limit_per_minute": "integer (optional, default 1000)",
    "rate_limit_per_hour": "integer (optional, default 30000)",
    "expires_at": "string ISO8601 (optional)"
  }
  ```
- Success response (201):
  ```json
  {
    "status": "success",
    "data": {
      "id": "uuid",
      "name": "string",
      "prefix": "8f",
      "key": "argus_8f_a1b2c3d4e5f6...",
      "scopes": ["sims:read", "cdrs:*"],
      "rate_limits": {
        "per_minute": 1000,
        "per_hour": 30000
      },
      "expires_at": "2026-12-31T23:59:59Z",
      "created_at": "2026-03-20T10:00:00Z"
    }
  }
  ```
- Error responses: 400 INVALID_FORMAT, 422 VALIDATION_ERROR

#### API-150: GET /api/v1/api-keys — List API Keys
- Auth: JWT (tenant_admin+)
- Query params: `?cursor=<uuid>&limit=<int>`
- Success response (200):
  ```json
  {
    "status": "success",
    "data": [
      {
        "id": "uuid",
        "name": "string",
        "prefix": "8f",
        "scopes": ["sims:read"],
        "rate_limits": { "per_minute": 1000, "per_hour": 30000 },
        "usage_count": 12345,
        "last_used_at": "2026-03-20T09:30:00Z",
        "expires_at": null,
        "revoked_at": null,
        "created_at": "2026-03-01T10:00:00Z"
      }
    ],
    "meta": { "cursor": "...", "limit": 50, "has_more": false }
  }
  ```

#### API-152: PATCH /api/v1/api-keys/:id — Update API Key
- Auth: JWT (tenant_admin+)
- Request body:
  ```json
  {
    "name": "string (optional)",
    "scopes": ["string"] (optional),
    "rate_limit_per_minute": "integer (optional)",
    "rate_limit_per_hour": "integer (optional)"
  }
  ```
- Success response (200): same shape as create (without key field)
- Error responses: 400, 404 NOT_FOUND, 422 VALIDATION_ERROR

#### API-153: POST /api/v1/api-keys/:id/rotate — Rotate API Key
- Auth: JWT (tenant_admin+)
- No request body
- Success response (200):
  ```json
  {
    "status": "success",
    "data": {
      "id": "uuid",
      "name": "string",
      "prefix": "2a",
      "key": "argus_2a_...",
      "grace_period_ends": "2026-03-21T10:00:00Z"
    }
  }
  ```
- During grace period: both old and new key_hash are valid
- After grace period: old key_hash stops working
- Implementation: add `previous_key_hash` and `key_rotated_at` columns conceptually tracked via a separate rotation approach — store old key hash temporarily

#### API-154: DELETE /api/v1/api-keys/:id — Revoke API Key
- Auth: JWT (tenant_admin+)
- Success response: 204 No Content
- Error responses: 404 NOT_FOUND

### Database Schema

```sql
-- Source: migrations/20260320000002_core_schema.up.sql (ACTUAL — TBL-04)
CREATE TABLE IF NOT EXISTS api_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    name VARCHAR(100) NOT NULL,
    key_prefix VARCHAR(8) NOT NULL,
    key_hash VARCHAR(255) NOT NULL,
    scopes JSONB NOT NULL DEFAULT '["*"]',
    rate_limit_per_minute INTEGER NOT NULL DEFAULT 1000,
    rate_limit_per_hour INTEGER NOT NULL DEFAULT 30000,
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    usage_count BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by UUID REFERENCES users(id)
);

CREATE INDEX IF NOT EXISTS idx_api_keys_tenant ON api_keys (tenant_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_prefix ON api_keys (key_prefix);
CREATE INDEX IF NOT EXISTS idx_api_keys_active ON api_keys (tenant_id) WHERE revoked_at IS NULL;
```

**Rotation support**: A new migration adds `previous_key_hash` (VARCHAR(255)) and `key_rotated_at` (TIMESTAMPTZ) columns to support the 24h grace period during key rotation.

### Rate Limiting Algorithm (from ALGORITHMS.md Section 3)

```
FUNCTION check_rate_limit(identifier, endpoint, limit, window_seconds) → (allowed, remaining, reset_at)

1. current_time = NOW() as Unix timestamp (seconds)
2. window_start = current_time - (current_time % window_seconds)
3. current_key = "ratelimit:{identifier}:{endpoint}:{window_start}"
4. previous_key = "ratelimit:{identifier}:{endpoint}:{window_start - window_seconds}"

5. Redis MULTI/EXEC pipeline:
     a. GET previous_key → prev_count (default 0)
     b. INCR current_key → curr_count
     c. EXPIRE current_key window_seconds * 2

6. Weighted count (sliding window approximation):
     elapsed_in_window = current_time - window_start
     weight = (window_seconds - elapsed_in_window) / window_seconds
     weighted_count = (prev_count * weight) + curr_count

7. IF weighted_count > limit:
     remaining = 0
     DECR current_key (undo INCR since rejected)
     RETURN (false, 0, window_start + window_seconds)

8. remaining = limit - ceil(weighted_count)
   RETURN (true, remaining, window_start + window_seconds)
```

Rate limit resolution order:
1. API key-specific limit (TBL-04 api_keys.rate_limit_per_minute)
2. Global default (env: RATE_LIMIT_DEFAULT_PER_MINUTE)

### Error Codes Used
- `INVALID_CREDENTIALS` (401) — invalid or revoked API key
- `RATE_LIMITED` (429) — rate limit exceeded, includes Retry-After header
- `SCOPE_DENIED` (403) — API key missing required scope
- `VALIDATION_ERROR` (422) — request validation failed
- `NOT_FOUND` (404) — API key not found
- `RESOURCE_LIMIT_EXCEEDED` (422) — tenant max API keys reached

## Prerequisites
- [x] STORY-004 completed (RBAC middleware — `RequireRole`, `RequireScope` in gateway/rbac.go)
- [x] STORY-006 completed (Redis cache layer in internal/cache/redis.go, NATS event bus)
- [x] TBL-04 api_keys table exists in core_schema migration
- [x] apierr package has context keys: AuthTypeKey, ScopesKey

## Tasks

### Task 1: API Key Store — CRUD operations
- **Files:** Create `internal/store/apikey.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/user.go` — follow same store structure (struct, NewXStore, pgxpool, cursor pagination, TenantIDFromContext)
- **Context refs:** ["Database Schema", "API Specifications"]
- **What:**
  - Define `APIKey` struct matching TBL-04 columns exactly: id, tenant_id, name, key_prefix, key_hash, scopes ([]string from JSONB), rate_limit_per_minute, rate_limit_per_hour, expires_at, revoked_at, last_used_at, usage_count, created_at, created_by, plus rotation fields: previous_key_hash (*string), key_rotated_at (*time.Time)
  - Define `CreateAPIKeyParams` struct: Name, KeyPrefix, KeyHash, Scopes, RateLimitPerMinute, RateLimitPerHour, ExpiresAt, CreatedBy
  - Define `UpdateAPIKeyParams` struct: Name, Scopes, RateLimitPerMinute, RateLimitPerHour (all pointer types for partial update)
  - Implement `APIKeyStore` with methods:
    - `Create(ctx, params) (*APIKey, error)` — INSERT with TenantIDFromContext
    - `GetByID(ctx, id) (*APIKey, error)` — SELECT by id + tenant_id
    - `GetByPrefix(ctx, prefix) (*APIKey, error)` — SELECT by key_prefix (no tenant filter — used by auth middleware). Include previous_key_hash and key_rotated_at
    - `ListByTenant(ctx, cursor, limit) ([]APIKey, string, error)` — cursor-based pagination, tenant-scoped, ordered by created_at DESC
    - `Update(ctx, id, params) (*APIKey, error)` — dynamic SET with tenant_id check
    - `Revoke(ctx, id) error` — SET revoked_at = NOW() WHERE tenant_id AND id
    - `Rotate(ctx, id, newPrefix, newHash) (*APIKey, error)` — SET previous_key_hash = key_hash, key_hash = newHash, key_prefix = newPrefix, key_rotated_at = NOW()
    - `UpdateUsage(ctx, id) error` — UPDATE usage_count = usage_count + 1, last_used_at = NOW() (called on every API key auth)
    - `CountByTenant(ctx, tenantID) (int, error)` — count active (non-revoked) keys
  - Error sentinels: `ErrAPIKeyNotFound`
- **Verify:** `go build ./internal/store/...`

### Task 2: API Key Migration — Add rotation columns
- **Files:** Create `migrations/20260320000005_api_keys_rotation.up.sql`, Create `migrations/20260320000005_api_keys_rotation.down.sql`
- **Depends on:** —
- **Complexity:** low
- **Context refs:** ["Database Schema"]
- **What:**
  - Up migration: ALTER TABLE api_keys ADD COLUMN previous_key_hash VARCHAR(255), ADD COLUMN key_rotated_at TIMESTAMPTZ
  - Down migration: ALTER TABLE api_keys DROP COLUMN IF EXISTS previous_key_hash, DROP COLUMN IF EXISTS key_rotated_at
- **Verify:** Files exist with valid SQL syntax

### Task 3: API Key Handler — CRUD endpoints
- **Files:** Create `internal/api/apikey/handler.go`
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** Read `internal/api/user/handler.go` — follow same handler structure (Handler struct, NewHandler, request/response types, validation, audit logging, apierr usage)
- **Context refs:** ["API Specifications", "Error Codes Used", "Architecture Context > Data Flow"]
- **What:**
  - Handler struct with: apiKeyStore, tenantStore (for resource limits), auditSvc, logger
  - `Create(w, r)`:
    - Parse + validate request (name required 1-100 chars, scopes required non-empty array, rate limits optional with defaults)
    - Check tenant resource limit (DefaultMaxAPIKeys from config or tenant settings)
    - Generate key: `crypto/rand` 32 bytes for secret, first 2 bytes hex = prefix, full key = `argus_{prefix}_{hex(secret)}`
    - SHA-256 hash the full key string
    - Store key_prefix + hash in DB via store.Create
    - Return full key ONCE in response (201)
    - Audit log: apikey.create
  - `List(w, r)`:
    - Parse cursor/limit query params
    - Call store.ListByTenant
    - Map to response (NO key/hash fields — only prefix shown)
    - Return with cursor pagination meta (200)
  - `Update(w, r)`:
    - Parse ID from URL, parse + validate request body
    - Call store.Update
    - Audit log: apikey.update
    - Return updated key (200)
  - `Rotate(w, r)`:
    - Parse ID from URL
    - Generate new key (same format as Create)
    - Call store.Rotate with new prefix + hash
    - Return new key + grace_period_ends (NOW + 24h) (200)
    - Audit log: apikey.rotate
  - `Delete(w, r)`:
    - Parse ID from URL
    - Call store.Revoke
    - Audit log: apikey.revoke
    - Return 204
  - Helper: createAuditEntry (same pattern as user handler)
  - Scope validation: scopes must be strings matching pattern `resource:action` or `resource:*` or `*`
- **Verify:** `go build ./internal/api/apikey/...`

### Task 4: API Key Auth Middleware
- **Files:** Create `internal/gateway/apikey_auth.go`
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** Read `internal/gateway/auth_middleware.go` — follow same middleware pattern (closure returning http.Handler, context value injection)
- **Context refs:** ["Architecture Context > Data Flow", "Error Codes Used", "Database Schema"]
- **What:**
  - `APIKeyAuth(store *store.APIKeyStore) func(http.Handler) http.Handler`:
    - Check for X-API-Key header
    - Parse key format: must match `argus_{prefix}_{secret}` (split by underscore, validate 3 parts, prefix = parts[1], secret = parts[2])
    - Look up by prefix: `store.GetByPrefix(ctx, prefix)`
    - Compute SHA-256 of full key string, compare with stored key_hash
    - If hash mismatch: check previous_key_hash (for rotation grace period)
      - If previous_key_hash matches AND key_rotated_at + 24h > now: allow (grace period active)
      - Otherwise: 401 INVALID_CREDENTIALS
    - Check revoked_at is nil
    - Check expires_at is nil or > now
    - Parse scopes from JSONB
    - Inject into context: auth_type="api_key", tenant_id, api_key_id, scopes
    - Call store.UpdateUsage async (goroutine with background context to not block request)
    - Call next handler
  - `CombinedAuth(jwtSecret string, apiKeyStore *store.APIKeyStore) func(http.Handler) http.Handler`:
    - Checks Authorization header first → JWT auth
    - If no Bearer token, checks X-API-Key header → API key auth
    - If neither: 401 INVALID_CREDENTIALS
    - This replaces JWTAuth for authenticated routes that accept both auth methods
- **Verify:** `go build ./internal/gateway/...`

### Task 5: Rate Limiting Middleware
- **Files:** Create `internal/gateway/ratelimit.go`
- **Depends on:** —
- **Complexity:** high
- **Pattern ref:** Read `internal/gateway/auth_middleware.go` — follow middleware closure pattern
- **Context refs:** ["Rate Limiting Algorithm", "Error Codes Used"]
- **What:**
  - `RateLimiter(redisClient *redis.Client, defaultPerMin, defaultPerHour int) func(http.Handler) http.Handler`:
    - Skip rate limiting for /api/health
    - Determine identifier:
      - If api_key auth: `apikey:{api_key_id}`
      - If JWT auth: `user:{tenant_id}:{user_id}`
      - If unauthenticated: `ip:{remote_addr}`
    - Determine limits:
      - If api_key auth: read rate_limit_per_minute and rate_limit_per_hour from context (set by API key auth middleware) — requires adding rate limit values to context in Task 4
      - Otherwise: use defaultPerMin / defaultPerHour from config
    - Call `checkRateLimit(client, identifier, "per_minute", limit, 60)` for per-minute check
    - If per-minute passes, call `checkRateLimit(client, identifier, "per_hour", limitHour, 3600)` for per-hour check
    - On limit exceeded:
      - Set response headers: Retry-After (seconds), X-RateLimit-Limit, X-RateLimit-Remaining (0), X-RateLimit-Reset (unix timestamp)
      - Return 429 with RATE_LIMITED error code and standard error envelope
    - On pass:
      - Set response headers: X-RateLimit-Limit, X-RateLimit-Remaining, X-RateLimit-Reset
      - Call next handler
  - `checkRateLimit(client, identifier, window, limit, windowSec) (allowed, remaining, resetAt, error)`:
    - Implements sliding window counter algorithm per ALGORITHMS.md Section 3
    - Uses Redis pipeline: GET prev_key, INCR curr_key, EXPIRE curr_key
    - Computes weighted count
    - Returns allowed bool, remaining count, reset timestamp
- **Verify:** `go build ./internal/gateway/...`

### Task 6: Router Integration — Wire routes and middleware
- **Files:** Modify `internal/gateway/router.go`
- **Depends on:** Task 3, Task 4, Task 5
- **Complexity:** medium
- **Pattern ref:** Read `internal/gateway/router.go` — extend existing router pattern
- **Context refs:** ["API Specifications", "Architecture Context > Components Involved"]
- **What:**
  - Add to RouterDeps: `APIKeyHandler *apikeyapi.Handler`, `APIKeyStore *store.APIKeyStore`, `RedisClient *redis.Client`
  - Add RateLimiter middleware to global middleware chain (after CORS, before Auth per MIDDLEWARE.md spec)
  - Replace `JWTAuth` with `CombinedAuth` for authenticated route groups (so both JWT and API key auth work on all protected routes)
  - Add API key management routes (JWT-only, tenant_admin+):
    ```
    GET    /api/v1/api-keys           → APIKeyHandler.List
    POST   /api/v1/api-keys           → APIKeyHandler.Create
    PATCH  /api/v1/api-keys/{id}      → APIKeyHandler.Update
    POST   /api/v1/api-keys/{id}/rotate → APIKeyHandler.Rotate
    DELETE /api/v1/api-keys/{id}      → APIKeyHandler.Delete
    ```
  - API key management routes use JWTAuth (not CombinedAuth) since only portal users manage keys
- **Verify:** `go build ./internal/gateway/...`

### Task 7: Tests — Store, Handler, Middleware
- **Files:** Create `internal/store/apikey_test.go`, Create `internal/api/apikey/handler_test.go`, Create `internal/gateway/ratelimit_test.go`
- **Depends on:** Task 1, Task 3, Task 4, Task 5, Task 6
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/user/handler_test.go` — follow same test patterns
- **Context refs:** ["API Specifications", "Rate Limiting Algorithm", "Error Codes Used"]
- **What:**
  - **apikey_test.go**: Test APIKey struct serialization, key generation helper tests
  - **handler_test.go**:
    - Test Create: valid request → 201 with key shown once
    - Test Create: missing name → 422 VALIDATION_ERROR
    - Test Create: invalid scopes → 422 VALIDATION_ERROR
    - Test List: returns prefix, not secret
    - Test Update: partial update works
    - Test Rotate: returns new key + grace_period_ends
    - Test Delete: returns 204
  - **ratelimit_test.go**:
    - Test sliding window counter logic (unit test with mock Redis or miniredis)
    - Test rate limit headers set on response
    - Test 429 returned when limit exceeded
    - Test Retry-After header value
    - Test /api/health exempt from rate limiting
- **Verify:** `go test ./internal/store/... ./internal/api/apikey/... ./internal/gateway/...`

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| POST creates key, returns plaintext ONCE | Task 3 (Create handler) | Task 7 (handler test) |
| Only key_prefix and SHA-256 stored in DB | Task 1 (store), Task 3 (handler) | Task 7 (store test) |
| GET lists keys with prefix, not secret | Task 3 (List handler) | Task 7 (handler test) |
| API key auth via X-API-Key header | Task 4 (auth middleware) | Task 7 (middleware test) |
| Scopes restrict access | Task 4 (auth middleware) + existing RequireScope | Task 7 (handler test) |
| Rate limiting per key: configurable limits | Task 5 (rate limiter) | Task 7 (ratelimit test) |
| Redis sliding window counters | Task 5 (rate limiter) | Task 7 (ratelimit test) |
| POST rotate: new key, 24h grace period | Task 3 (Rotate handler), Task 4 (grace check) | Task 7 (handler test) |
| DELETE revokes immediately | Task 3 (Delete handler) | Task 7 (handler test) |
| 429 with Retry-After when rate limited | Task 5 (rate limiter) | Task 7 (ratelimit test) |
| Usage count and last_used_at updated | Task 4 (UpdateUsage call) | Task 7 (store test) |

## Story-Specific Compliance Rules

- **API**: Standard envelope `{ status, data, meta? }` for all responses. Standard error envelope `{ status: "error", error: { code, message, details? } }`.
- **DB**: Migration script required for rotation columns (up + down). All queries scoped by tenant_id (enforced in store layer via TenantIDFromContext).
- **Security**: Never store plaintext API keys. SHA-256 hash only. Key shown once at creation. Rotation uses separate previous_key_hash column for grace period.
- **Auth**: API key format: `argus_{2-char-hex-prefix}_{hex-secret}`. Middleware sets context keys matching apierr package constants.
- **Rate Limiting**: Sliding window counter algorithm per ALGORITHMS.md. Redis keys auto-expire. Response headers always set (X-RateLimit-Limit, X-RateLimit-Remaining, X-RateLimit-Reset).
- **Cursor Pagination**: All list endpoints use cursor-based pagination (not offset). Pattern: fetch limit+1, use last ID as next cursor.
- **Audit**: Every state-changing operation (create, update, rotate, revoke) creates an audit log entry via audit.Auditor interface.

## Risks & Mitigations

- **Race condition on rate limit counters**: Mitigated by Redis atomic INCR + pipeline. Sliding window approximation is inherently safe for concurrent access.
- **Key rotation grace period**: Previous key hash stored in DB column. After 24h check is done at auth time (comparing key_rotated_at + 24h vs now). No background job needed.
- **Redis unavailability**: Rate limiter should fail-open (allow request) if Redis is unreachable, to avoid blocking all API traffic. Log warning.

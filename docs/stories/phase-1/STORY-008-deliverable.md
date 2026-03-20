# Deliverable: STORY-008 — API Key Management & Rate Limiting

## Summary
Implemented API key management (CRUD, rotation with grace period, scope-based access control) and Redis sliding window rate limiting middleware.

## Files Changed

### New Files
- `internal/store/apikey.go` — APIKeyStore: Create, GetByID, GetByPrefix, ListByTenant, Update, Revoke, Rotate, UpdateUsage, CountByTenant
- `internal/store/apikey_test.go` — Store struct tests, key generation/parsing
- `internal/api/apikey/handler.go` — HTTP handlers: Create (API-151), List (API-150), Update (API-152), Rotate (API-153), Delete (API-154)
- `internal/api/apikey/handler_test.go` — Handler validation and response tests
- `internal/gateway/apikey_auth.go` — API key authentication middleware, CombinedAuth (JWT+APIKey)
- `internal/gateway/apikey_auth_test.go` — Auth middleware tests (7 tests)
- `internal/gateway/ratelimit.go` — Redis sliding window counter rate limiter per ALGORITHMS.md Section 3
- `internal/gateway/ratelimit_test.go` — Rate limiter tests (identifier resolution, headers, 429)
- `migrations/20260320000005_api_keys_rotation.up.sql` — previous_key_hash + key_rotated_at columns
- `migrations/20260320000005_api_keys_rotation.down.sql` — Reverse migration

### Modified Files
- `internal/gateway/router.go` — APIKeyHandler, APIKeyStore, RedisClient deps + rate limiter in global chain + API key routes
- `internal/gateway/rbac.go` — RequireScope updated with wildcard support (*, resource:*)
- `internal/apierr/apierr.go` — Added APIKeyIDKey context key and CodeRateLimited error code
- `cmd/argus/main.go` — Wired APIKeyStore, APIKeyHandler, RedisClient, rate limit config

## Architecture References Fulfilled
- API-150: GET /api/v1/api-keys — list with cursor pagination
- API-151: POST /api/v1/api-keys — create with argus_{prefix}_{secret} format
- API-152: PATCH /api/v1/api-keys/{id} — partial update (name, scopes, rate limits, expiry)
- API-153: POST /api/v1/api-keys/{id}/rotate — rotation with 24h grace period
- API-154: DELETE /api/v1/api-keys/{id} — immediate revoke
- TBL-04: api_keys table fully utilized
- ALGORITHMS.md Section 3: Redis sliding window rate limiting

## Key Features
- Key format: `argus_{8-char-prefix}_{32-char-secret}` — prefix for display, secret hashed with SHA-256
- Rotation: new key generated, old key_hash moved to previous_key_hash, 24h grace period
- Rate limiting: per-minute and per-hour sliding windows, X-RateLimit headers, Retry-After on 429
- Scope-based access: wildcard support (*, resource:*), ScopeCheck middleware
- CombinedAuth middleware: accepts both JWT and API key authentication
- Fails open: if Redis unavailable, rate limiter passes through

## Test Coverage
- Store struct and key generation tests
- Handler validation (missing fields, invalid scopes, invalid dates)
- Auth middleware (missing header, invalid format, combined auth)
- Rate limiter (identifier resolution, limit resolution, headers, 429, health skip, nil Redis)
- Scope wildcards (*, resource:*, different resource)
- Full suite: 22 packages pass, 0 regressions

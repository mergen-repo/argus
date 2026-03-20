# Implementation Plan: STORY-003 - Authentication — JWT + Refresh Token + 2FA

## Goal
Implement complete JWT authentication with refresh tokens, account lockout, TOTP-based 2FA, and gateway auth middleware for the Argus platform.

## Architecture Context

### Components Involved
- **internal/auth/**: JWT token generation/validation, password verification, TOTP 2FA logic. New package — core auth service.
- **internal/store/**: UserStore for credential lookup, session CRUD. Extends existing store layer.
- **internal/gateway/**: Auth middleware for JWT extraction on protected routes, router updates to register auth endpoints.
- **internal/api/auth/**: HTTP handler layer for auth endpoints (login, refresh, logout, 2fa/setup, 2fa/verify).
- **internal/apierr/**: Add auth-specific error codes.
- **internal/config/**: Already has JWT/auth config fields (JWTSecret, JWTExpiry, JWTRefreshExpiry, BcryptCost, LoginMaxAttempts, LoginLockoutDur).

### Data Flow

**Login Flow:**
```
POST /api/v1/auth/login
  → AuthHandler.Login
  → Validate request body (email, password)
  → UserStore.GetByEmail(tenant_id=nil, email) — login doesn't know tenant yet, look up by email
  → Verify password: bcrypt.CompareHashAndPassword(user.password_hash, password)
  → Check account lock: user.locked_until > now? → 403 ACCOUNT_LOCKED
  → Check failed attempts >= 5 → lock account
  → If totp_enabled: issue partial JWT (no tenant context), return requires_2fa=true
  → If !totp_enabled: create session in TBL-03, issue full JWT + refresh token cookie
  → Audit log: login/failed_login event
```

**Refresh Flow:**
```
POST /api/v1/auth/refresh
  → Read refresh_token from httpOnly cookie
  → UserStore.GetSessionByToken(hash) — bcrypt compare
  → Check session not revoked, not expired
  → Revoke old session, create new session (rotate)
  → Issue new JWT + new refresh token cookie
```

**Auth Middleware Flow:**
```
HTTP Request with Authorization: Bearer <jwt>
  → Extract token from header
  → Validate signature (HS256), expiry, issuer
  → Extract claims: user_id, tenant_id, role
  → Inject into request context
  → Next handler
```

### API Specifications

#### API-001: POST /api/v1/auth/login
- **Request:** `{ "email": "string", "password": "string" }`
- **Success (200):** `{ "status": "success", "data": { "user": { "id": "uuid", "email": "string", "name": "string", "role": "string" }, "token": "jwt_string", "requires_2fa": false } }`
- **Success with 2FA (200):** `{ "status": "success", "data": { "user": { "id": "uuid", "email": "string", "name": "string", "role": "string" }, "token": "partial_jwt", "requires_2fa": true } }`
- **Error 401:** `{ "status": "error", "error": { "code": "INVALID_CREDENTIALS", "message": "Invalid email or password" } }`
- **Error 403:** `{ "status": "error", "error": { "code": "ACCOUNT_LOCKED", "message": "Account locked due to too many failed attempts. Try again in N minutes.", "details": [{ "retry_after_seconds": N, "failed_attempts": 5 }] } }`
- **Side effect:** Sets `refresh_token` httpOnly cookie (7d, Secure, SameSite=Strict, Path=/api/v1/auth)

#### API-002: POST /api/v1/auth/refresh
- **Request:** Cookie `refresh_token` (httpOnly)
- **Success (200):** `{ "status": "success", "data": { "token": "new_jwt_string" } }`
- **Error 401:** `{ "status": "error", "error": { "code": "INVALID_REFRESH_TOKEN", "message": "Refresh token is invalid or has been revoked" } }`
- **Side effect:** Rotates refresh token cookie

#### API-003: POST /api/v1/auth/logout
- **Auth:** JWT required
- **Request:** Cookie `refresh_token`
- **Success (204):** empty body
- **Error 401:** standard auth error
- **Side effect:** Revokes session in TBL-03, clears refresh token cookie

#### API-004: POST /api/v1/auth/2fa/setup
- **Auth:** JWT required
- **Success (200):** `{ "status": "success", "data": { "secret": "base32_string", "qr_uri": "otpauth://totp/Argus:user@email?secret=XXX&issuer=Argus" } }`
- **Side effect:** Stores TOTP secret in user record (not enabled yet until verify)

#### API-005: POST /api/v1/auth/2fa/verify
- **Auth:** Partial JWT (from login with 2FA) OR full JWT (for initial setup verification)
- **Request:** `{ "code": "123456" }`
- **Success (200):** `{ "status": "success", "data": { "token": "full_jwt_string" } }`
- **Error 401:** `{ "status": "error", "error": { "code": "INVALID_2FA_CODE", "message": "Invalid or expired 2FA code" } }`
- **Side effects:** If first-time setup → sets totp_enabled=true. Creates session in TBL-03, sets refresh token cookie.

### Database Schema

**Source: migrations/20260320000002_core_schema.up.sql (ACTUAL)**

```sql
-- TBL-02: users
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    email VARCHAR(255) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    name VARCHAR(100) NOT NULL,
    role VARCHAR(30) NOT NULL,
    totp_secret VARCHAR(255),
    totp_enabled BOOLEAN NOT NULL DEFAULT false,
    state VARCHAR(20) NOT NULL DEFAULT 'active',
    last_login_at TIMESTAMPTZ,
    failed_login_count INTEGER NOT NULL DEFAULT 0,
    locked_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- Indexes: idx_users_tenant_email (UNIQUE on tenant_id, email), idx_users_tenant_role, idx_users_state

-- TBL-03: user_sessions
CREATE TABLE IF NOT EXISTS user_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    refresh_token_hash VARCHAR(255) NOT NULL,
    ip_address INET,
    user_agent TEXT,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- Indexes: idx_user_sessions_user, idx_user_sessions_expires (partial WHERE revoked_at IS NULL)
```

**Key columns for auth queries:**
- `users.email` — login lookup (note: UNIQUE per tenant, login must find user across tenants by email alone or require tenant context)
- `users.password_hash` — bcrypt hash
- `users.totp_secret` — TOTP shared secret (nullable, set during 2FA setup)
- `users.totp_enabled` — whether 2FA is active
- `users.failed_login_count` — increment on failure, reset on success
- `users.locked_until` — set to now()+15m when count reaches 5
- `user_sessions.refresh_token_hash` — bcrypt hash of refresh token
- `user_sessions.revoked_at` — set on logout or token rotation

## Prerequisites
- [x] STORY-002 completed (users + user_sessions tables exist in migration)
- [x] Config fields for JWT already in internal/config/config.go
- [x] apierr package with WriteSuccess, WriteError, WriteJSON helpers
- [x] store package with Postgres pool, TenantIDFromContext

## Tasks

### Task 1: Auth Error Codes & User/Session Store
- **Files:** Modify `internal/apierr/apierr.go`, Create `internal/store/user.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/stubs.go` — follow same store struct pattern with pgxpool.Pool
- **Context refs:** Database Schema, API Specifications (error codes)
- **What:**
  - Add auth error code constants to apierr: `CodeInvalidCredentials`, `CodeAccountLocked`, `CodeAccountDisabled`, `CodeInvalid2FACode`, `CodeTokenExpired`, `CodeInvalidRefreshToken`
  - Create `UserStore` in `internal/store/user.go` with methods:
    - `GetByEmail(ctx, email string) (*User, error)` — finds user by email (for login — across tenants since we don't know tenant at login time)
    - `GetByID(ctx, id uuid.UUID) (*User, error)` — finds user by ID
    - `UpdateLoginSuccess(ctx, id uuid.UUID) error` — sets last_login_at=now, failed_login_count=0, locked_until=nil
    - `IncrementFailedLogin(ctx, id uuid.UUID, lockUntil *time.Time) error` — increments failed_login_count, optionally sets locked_until
    - `SetTOTPSecret(ctx, id uuid.UUID, secret string) error` — stores TOTP secret
    - `EnableTOTP(ctx, id uuid.UUID) error` — sets totp_enabled=true
  - Create `SessionStore` in same file (or separate `internal/store/session_store.go`) with methods:
    - `CreateSession(ctx, params CreateSessionParams) (*UserSession, error)` — insert into user_sessions
    - `GetValidSession(ctx, userID uuid.UUID) ([]UserSession, error)` — get non-revoked, non-expired sessions
    - `RevokeSession(ctx, sessionID uuid.UUID) error` — set revoked_at=now
    - `RevokeAllUserSessions(ctx, userID uuid.UUID) error` — revoke all sessions for user
    - `FindByRefreshTokenHash(ctx, hash string) (*UserSession, error)` — lookup session by token hash
  - User struct fields MUST match actual migration columns exactly
- **Verify:** `go build ./internal/store/...`

### Task 2: Auth Service (JWT + Password + TOTP)
- **Files:** Create `internal/auth/auth.go`, Create `internal/auth/jwt.go`, Create `internal/auth/totp.go`
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** Read `internal/audit/audit.go` — follow service pattern; Read `internal/config/config.go` for config fields
- **Context refs:** Architecture Context > Data Flow, API Specifications, Database Schema
- **What:**
  - `internal/auth/jwt.go`:
    - `GenerateToken(userID, tenantID uuid.UUID, role string, expiry time.Duration, partial bool) (string, error)` — HS256 JWT with claims: sub=user_id, tenant_id, role, exp, iss="argus", partial (bool claim for 2FA pending)
    - `ValidateToken(tokenString, secret string) (*Claims, error)` — parse and validate JWT, return claims
    - Claims struct: UserID, TenantID, Role, Partial, ExpiresAt
  - `internal/auth/totp.go`:
    - `GenerateTOTPSecret(email string) (secret string, qrURI string, error)` — using pquerna/otp library
    - `ValidateTOTPCode(secret, code string) bool` — validate 6-digit code
  - `internal/auth/auth.go` — Service struct tying it together:
    - `Login(ctx, email, password string, ipAddr, userAgent string) (*LoginResult, error)` — full login flow: find user, verify password, check lock, handle 2FA, create session, return JWT
    - `Refresh(ctx, refreshToken string, ipAddr, userAgent string) (*RefreshResult, error)` — validate refresh token, rotate session, issue new JWT
    - `Logout(ctx, refreshToken string) error` — find and revoke session
    - `Setup2FA(ctx, userID uuid.UUID) (*Setup2FAResult, error)` — generate TOTP secret, store on user
    - `Verify2FA(ctx, userID uuid.UUID, code string, ipAddr, userAgent string) (*Verify2FAResult, error)` — validate code, enable TOTP if first time, create session, return full JWT
  - Must add `golang.org/x/crypto` (already in go.sum for bcrypt) and `github.com/pquerna/otp` dependency
  - Refresh token: generate 32-byte random, base64url encode, store bcrypt hash in DB
  - Account lockout: 5 failed attempts → lock for 15 minutes (configurable via config)
- **Verify:** `go build ./internal/auth/...`

### Task 3: Auth HTTP Handlers
- **Files:** Create `internal/api/auth/handler.go`
- **Depends on:** Task 2
- **Complexity:** medium
- **Pattern ref:** Read `internal/gateway/health.go` — follow handler pattern with http.ResponseWriter/Request; Read `internal/apierr/apierr.go` for response helpers
- **Context refs:** API Specifications (all 5 endpoints), Architecture Context > Data Flow
- **What:**
  - Create `AuthHandler` struct with dependency on auth.Service
  - `Login(w, r)` — decode JSON body {email, password}, call service.Login, set refresh cookie, return JWT
  - `Refresh(w, r)` — read refresh_token cookie, call service.Refresh, set new cookie, return new JWT
  - `Logout(w, r)` — read refresh_token cookie, call service.Logout, clear cookie, return 204
  - `Setup2FA(w, r)` — extract user_id from context (JWT claims), call service.Setup2FA, return secret+qr_uri
  - `Verify2FA(w, r)` — extract user_id from context, decode {code}, call service.Verify2FA, set cookie, return JWT
  - Cookie settings: httpOnly=true, Secure=true (false in dev), SameSite=Strict, Path=/api/v1/auth, MaxAge=7days
  - Use apierr.WriteSuccess, apierr.WriteError for all responses
  - Input validation: email required+format, password required+min length
- **Verify:** `go build ./internal/api/auth/...`

### Task 4: Gateway Auth Middleware & Router Integration
- **Files:** Create `internal/gateway/auth_middleware.go`, Modify `internal/gateway/router.go`, Modify `cmd/argus/main.go`
- **Depends on:** Task 2, Task 3
- **Complexity:** high
- **Pattern ref:** Read `internal/gateway/router.go` — follow existing chi router pattern; Read `internal/gateway/health.go` — handler registration
- **Context refs:** Architecture Context > Components Involved, Architecture Context > Data Flow (Auth Middleware Flow)
- **What:**
  - `internal/gateway/auth_middleware.go`:
    - `JWTAuth(secret string) func(http.Handler) http.Handler` — middleware that:
      1. Reads `Authorization: Bearer <token>` header
      2. Calls auth.ValidateToken
      3. Rejects partial tokens (requires_2fa) on non-2FA routes
      4. Sets user_id, tenant_id, role in request context using apierr context keys
      5. On failure: returns 401 with TOKEN_EXPIRED or INVALID_CREDENTIALS
  - Modify `internal/gateway/router.go`:
    - Update `NewRouter` signature to accept auth handler and JWT secret
    - Register public routes: POST /api/v1/auth/login, POST /api/v1/auth/refresh
    - Register authenticated routes group with JWTAuth middleware: POST /api/v1/auth/logout, POST /api/v1/auth/2fa/setup, POST /api/v1/auth/2fa/verify
  - Modify `cmd/argus/main.go`:
    - Wire up UserStore, SessionStore, auth.Service, AuthHandler
    - Pass auth handler and config to NewRouter
- **Verify:** `go build ./cmd/argus/...`

### Task 5: Auth Tests
- **Files:** Create `internal/auth/auth_test.go`, Create `internal/auth/jwt_test.go`
- **Depends on:** Task 2, Task 4
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/segment_test.go` or `internal/operator/router_test.go` — follow Go test pattern with testify
- **Context refs:** API Specifications, Database Schema
- **What:**
  - `internal/auth/jwt_test.go`:
    - TestGenerateToken — generates valid JWT with correct claims
    - TestValidateToken — validates good token, rejects expired, rejects bad signature
    - TestPartialToken — partial flag set correctly for 2FA pending
  - `internal/auth/auth_test.go`:
    - Test Login flow with mock UserStore/SessionStore:
      - Valid login returns JWT + sets session
      - Invalid password returns INVALID_CREDENTIALS
      - 6th failed attempt locks account (ACCOUNT_LOCKED)
      - Locked account returns ACCOUNT_LOCKED with retry_after
      - Login with 2FA enabled returns partial token + requires_2fa=true
    - Test Refresh flow:
      - Valid refresh returns new JWT + rotates session
      - Expired/revoked refresh returns INVALID_REFRESH_TOKEN
    - Test Logout:
      - Revokes session successfully
    - Test TOTP:
      - Setup generates valid secret + QR URI
      - Verify with correct code succeeds
      - Verify with wrong code returns INVALID_2FA_CODE
  - Use interfaces for store dependencies to enable mocking
- **Verify:** `go test ./internal/auth/...`

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| POST /api/v1/auth/login validates credentials, returns JWT (15min) + refresh token (7d) | Task 2 (service), Task 3 (handler) | Task 5, TestLogin* |
| JWT contains: user_id, tenant_id, role, exp | Task 2 (jwt.go) | Task 5, TestGenerateToken |
| Refresh token stored as bcrypt hash in TBL-03 | Task 1 (SessionStore), Task 2 (service) | Task 5, TestLogin* |
| POST /api/v1/auth/refresh rotates refresh token, issues new JWT | Task 2 (service), Task 3 (handler) | Task 5, TestRefresh* |
| POST /api/v1/auth/logout revokes session in TBL-03 | Task 2 (service), Task 3 (handler) | Task 5, TestLogout |
| Account locks after 5 consecutive failed logins for 15 minutes | Task 2 (service) | Task 5, TestLockout |
| POST /api/v1/auth/2fa/setup generates TOTP secret + QR code URI | Task 2 (totp.go, service) | Task 5, TestSetup2FA |
| POST /api/v1/auth/2fa/verify validates 6-digit TOTP code | Task 2 (totp.go, service) | Task 5, TestVerify2FA |
| If 2FA enabled, login returns partial token that requires 2FA verification | Task 2 (jwt.go partial flag) | Task 5, TestLogin2FA |
| All auth events logged to audit | Task 2 (service calls audit) | Task 5 |
| Gateway middleware extracts JWT, sets tenant context on all /api/v1/* routes | Task 4 (auth_middleware.go) | Task 5, build verification |

## Story-Specific Compliance Rules

- **API:** Standard envelope `{ status, data, meta?, error? }` for all responses
- **Auth:** JWT signed with HS256 using JWT_SECRET env var, issuer="argus"
- **Auth:** Refresh token as httpOnly cookie, NOT in response body
- **Security:** bcrypt for password_hash (cost=12 from config), bcrypt for refresh_token_hash
- **Security:** Account lockout: 5 attempts / 15min (configurable via LOGIN_MAX_ATTEMPTS, LOGIN_LOCKOUT_DURATION)
- **Business:** Failed login attempts logged to audit (per PRODUCT.md BR-7)
- **ADR:** JWT auth per architecture, bcrypt hashing per security requirements

## Risks & Mitigations

- **Risk:** Login by email alone (no tenant context) — email unique per tenant but not globally. **Mitigation:** Login searches by email; if multiple users with same email across tenants, return error asking for tenant context. In practice, admin@argus.io seed user is unique. For v1, email is effectively unique since we create one tenant.
- **Risk:** TOTP library dependency needs adding. **Mitigation:** `pquerna/otp` is the standard Go TOTP library, well-maintained.
- **Risk:** Refresh token rotation race condition. **Mitigation:** Database-level revocation check before issuing new token.

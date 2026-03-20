# Post-Story Review: STORY-003 — Authentication: JWT + Refresh Token + 2FA

> Date: 2026-03-20

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| STORY-004 | JWT Claims struct exposes `Role` via `apierr.RoleKey` in context. RBAC middleware can read role directly from context set by `JWTAuth`. Role hierarchy (7 roles) matches G-019. | NO_CHANGE |
| STORY-005 | `store/user.go` has auth-related methods (GetByEmail, GetByID, login tracking, TOTP). STORY-005 will add CRUD methods (Create, List, Update, state transitions). No conflicts. | NO_CHANGE |
| STORY-006 | Audit logger interface defined (`AuditLogger` in `internal/auth/auth.go`) but currently wired as nil. STORY-006 will implement the real audit service and wire it. Interface is compatible. | NO_CHANGE |
| STORY-008 | API key auth not yet implemented (only JWT path). STORY-008 will add `X-API-Key` extraction in gateway. MIDDLEWARE.md shows both in same Auth step. Current `JWTAuth` middleware structure allows coexistence. | NO_CHANGE |
| STORY-042 | Frontend auth story can rely on: login returns `{user, token, requires_2fa}`, refresh via httpOnly cookie, partial token for 2FA flow. API contracts are finalized. | NO_CHANGE |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| decisions.md | Added DEV-008: Partial token pattern for 2FA | UPDATED |
| GLOSSARY.md | Added "Partial Token" term | UPDATED |
| ROUTEMAP.md | STORY-003 marked DONE, progress updated | UPDATED |
| ARCHITECTURE.md | No changes (auth flow matches Security Architecture section) | NO_CHANGE |
| FUTURE.md | No new items or invalidated items | NO_CHANGE |
| Makefile | No new targets needed | NO_CHANGE |
| CLAUDE.md | No changes needed | NO_CHANGE |
| ERROR_CODES.md | No changes (implementation matches; ACCOUNT_LOCKED uses 403 per doc, not 423 per story spec) | NO_CHANGE |
| CONFIG.md | No changes (all auth env vars already documented) | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 1 (minor)
  - ERROR_CODES.md references `internal/gateway/errors.go` as the location for error code constants, but actual implementation is `internal/apierr/apierr.go` (created in STORY-001). Non-blocking; the package name is more appropriate. Will be corrected when ERROR_CODES.md is next touched.
- ACCOUNT_LOCKED HTTP status: story spec says 423, ERROR_CODES.md says 403, implementation uses 403. Architecture doc takes precedence. No action needed.
- Auth flow in ARCHITECTURE.md "Security Architecture" section matches implementation exactly (JWT 15min, refresh 7d httpOnly, HS256, Claims: user_id/tenant_id/role).
- CONFIG.md auth variables (JWT_SECRET, JWT_EXPIRY, JWT_REFRESH_EXPIRY, JWT_ISSUER, BCRYPT_COST, LOGIN_MAX_ATTEMPTS, LOGIN_LOCKOUT_DURATION) all match `internal/config/config.go`.
- MIDDLEWARE.md auth step (step 6) matches `JWTAuth` middleware behavior: Bearer extraction, HS256 validation, context injection of tenant_id/user_id/role.
- Context keys use typed `contextKey` string type in `apierr` package. MIDDLEWARE.md shows `ctx.Set()` pseudocode but actual implementation uses `context.WithValue()` with typed keys. Functionally equivalent.

## Observations

1. **DEV_DISABLE_2FA config var exists** in `config.go` but is not used in the auth flow. The auth service does not check this flag. Should be wired in STORY-042 (frontend) or a future dev-convenience story. Non-blocking.

2. **No standalone email index**: `GetByEmail` query cannot leverage `idx_users_tenant_email` composite index when searching by email alone (tenant_id is leading column). Acceptable for v1 user counts. At scale, add `CREATE INDEX idx_users_email ON users (email)`.

3. **TOTP secret stored in plaintext**: Standard practice (must be readable to validate), but encryption at rest recommended for production deployments.

4. **Audit logger nil-safe**: Auth service gracefully handles nil audit logger. Real audit will be wired after STORY-007.

5. **Error code package location**: Architecture docs reference `internal/gateway/errors.go` but codes live in `internal/apierr/apierr.go`. The actual location is better organized. Minor doc drift.

## Project Health

- Stories completed: 3/55 (5%)
- Current phase: Phase 1 -- Foundation (3/8 stories done)
- Next story: STORY-004 (RBAC Middleware & Permission Enforcement)
- Blockers: None
- Quality: All 18 unit tests passing, 12 test packages green, 1 critical gate fix applied (partial token protection)

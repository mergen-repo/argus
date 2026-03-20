# Implementation Plan: STORY-004 - RBAC Middleware & Permission Enforcement

## Goal
Implement RBAC middleware that checks user role from JWT claims against endpoint permission requirements, enforcing role hierarchy and API key scope checks on all `/api/v1/*` routes.

## Architecture Context

### Components Involved
- **internal/gateway/rbac.go** (NEW): RBAC middleware — reads role from context (set by JWTAuth), checks against per-route required role
- **internal/gateway/router.go** (MODIFY): Apply RBAC middleware via `rbac.Require(role)` wrapper on all authenticated route registrations
- **internal/apierr/apierr.go** (MODIFY): Add authorization error codes: `FORBIDDEN`, `INSUFFICIENT_ROLE`, `SCOPE_DENIED`

### Role Hierarchy (from ARCHITECTURE.md RBAC Matrix + G-019)
Roles ordered from highest to lowest privilege:
```
super_admin (level 7) — can do everything, cross-tenant
tenant_admin (level 6) — manage users, full tenant access
operator_manager (level 5) — manage APNs, IP pools, operators
sim_manager (level 4) — manage SIMs, eSIM, sessions
policy_editor (level 3) — manage policies
analyst (level 2) — read-only analytics
api_user (level 1) — M2M service account, scope-restricted
```

A user with role X can access any endpoint requiring role Y where level(X) >= level(Y).

### RBAC Permission Matrix (from ARCHITECTURE.md)

| Action | Minimum Role |
|--------|-------------|
| Manage tenants | super_admin |
| Manage operators (system-level) | super_admin |
| Manage users | tenant_admin |
| View audit logs | tenant_admin |
| Manage APNs | operator_manager |
| Manage IP pools | operator_manager |
| Manage SIMs | sim_manager |
| Manage eSIM | sim_manager |
| Force disconnect | sim_manager |
| Manage policies | policy_editor |
| View analytics | analyst |
| System config | super_admin |

### Middleware Data Flow
```
HTTP Request (already passed JWTAuth middleware)
  → RBAC middleware reads role from ctx (apierr.RoleKey)
  → Route has required minimum role (set via rbac.Require("sim_manager"))
  → Compare: roleLevel(user.role) >= roleLevel(required_role)
  → If YES → next handler
  → If NO → 403 INSUFFICIENT_ROLE with details {required_role, current_role}
  → For api_user: check scopes from context against endpoint scope requirement
  → If scope missing → 403 SCOPE_DENIED with details {required_scope, available_scopes}
```

### Error Responses (from ERROR_CODES.md)

**INSUFFICIENT_ROLE (403):**
```json
{
  "status": "error",
  "error": {
    "code": "INSUFFICIENT_ROLE",
    "message": "This action requires tenant_admin role or higher",
    "details": [{"required_role": "tenant_admin", "current_role": "sim_manager"}]
  }
}
```

**SCOPE_DENIED (403):**
```json
{
  "status": "error",
  "error": {
    "code": "SCOPE_DENIED",
    "message": "API key does not have the required scope",
    "details": [{"required_scope": "sims:write", "available_scopes": ["sims:read", "analytics:read"]}]
  }
}
```

### Context Keys (from existing apierr package)
- `apierr.RoleKey` — user's role string, set by JWTAuth middleware
- `apierr.TenantIDKey` — tenant UUID, set by JWTAuth middleware
- `apierr.UserIDKey` — user UUID, set by JWTAuth middleware
- Need to add: `apierr.AuthTypeKey` — "jwt" or "api_key"
- Need to add: `apierr.ScopesKey` — []string for API key scopes

## Prerequisites
- [x] STORY-003 completed — JWTAuth middleware sets RoleKey, TenantIDKey, UserIDKey in context
- [x] apierr package with WriteError, context keys
- [x] Chi router with route groups

## Tasks

### Task 1: Add Authorization Error Codes & Context Keys
- **Files:** Modify `internal/apierr/apierr.go`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `internal/apierr/apierr.go` — follow existing error code constant pattern
- **Context refs:** Error Responses, Context Keys
- **What:**
  - Add error code constants: `CodeForbidden = "FORBIDDEN"`, `CodeInsufficientRole = "INSUFFICIENT_ROLE"`, `CodeScopeDenied = "SCOPE_DENIED"`
  - Add context keys: `AuthTypeKey contextKey = "auth_type"`, `ScopesKey contextKey = "scopes"`
- **Verify:** `go build ./internal/apierr/...`

### Task 2: RBAC Package — Role Hierarchy & Require Middleware
- **Files:** Create `internal/gateway/rbac.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/gateway/auth_middleware.go` — follow same middleware function signature pattern (returns `func(http.Handler) http.Handler`)
- **Context refs:** Role Hierarchy, RBAC Permission Matrix, Middleware Data Flow, Error Responses
- **What:**
  - Define role level map: `var roleLevels = map[string]int{"api_user": 1, "analyst": 2, "policy_editor": 3, "sim_manager": 4, "operator_manager": 5, "tenant_admin": 6, "super_admin": 7}`
  - `RoleLevel(role string) int` — returns numeric level for a role (0 if unknown)
  - `HasRole(userRole, requiredRole string) bool` — returns true if userRole level >= requiredRole level
  - `RequireRole(minRole string) func(http.Handler) http.Handler` — Chi middleware:
    1. Extract role from `ctx.Value(apierr.RoleKey)`
    2. If role missing → 403 FORBIDDEN "Authentication required"
    3. Call `HasRole(role, minRole)`
    4. If false → 403 INSUFFICIENT_ROLE with details `{required_role, current_role}`
    5. If true → call next handler
  - `RequireScope(scope string) func(http.Handler) http.Handler` — for API key scope checks:
    1. Extract auth_type from context; if "jwt" → pass through (JWT users use role-based, not scope-based)
    2. Extract scopes from `ctx.Value(apierr.ScopesKey)`
    3. If required scope not in scopes list → 403 SCOPE_DENIED with details
    4. If found → call next handler
- **Verify:** `go build ./internal/gateway/...`

### Task 3: Router Integration — Apply RBAC to All Routes
- **Files:** Modify `internal/gateway/router.go`
- **Depends on:** Task 2
- **Complexity:** medium
- **Pattern ref:** Read `internal/gateway/router.go` — follow existing chi route group pattern with `r.With()`
- **Context refs:** RBAC Permission Matrix, Role Hierarchy
- **What:**
  - Existing authenticated routes (logout, 2fa/setup) already in JWTAuth group — add `RequireRole("api_user")` as the base role for all authenticated routes
  - The existing routes need RBAC annotations:
    - `POST /api/v1/auth/logout` — `RequireRole("api_user")` (any authenticated user)
    - `POST /api/v1/auth/2fa/setup` — `RequireRole("api_user")` (any authenticated user)
  - Comment placeholders for future endpoint groups:
    - tenant routes → `RequireRole("super_admin")`
    - user routes → `RequireRole("tenant_admin")`
    - operator routes → `RequireRole("super_admin")`
    - APN routes → `RequireRole("operator_manager")`
    - SIM routes → `RequireRole("sim_manager")`
    - policy routes → `RequireRole("policy_editor")`
    - analytics routes → `RequireRole("analyst")`
    - audit routes → `RequireRole("tenant_admin")`
    - system config routes → `RequireRole("super_admin")`
  - The 2FA verify route uses JWTAuthAllowPartial and should NOT have RBAC (it's a pre-auth step)
- **Verify:** `go build ./cmd/argus/...`

### Task 4: RBAC Unit Tests
- **Files:** Create `internal/gateway/rbac_test.go`
- **Depends on:** Task 2
- **Complexity:** medium
- **Pattern ref:** Read `internal/auth/jwt_test.go` — follow Go stdlib testing pattern
- **Context refs:** Role Hierarchy, RBAC Permission Matrix, Error Responses
- **What:**
  - `TestRoleLevel` — verify all 7 roles return correct levels, unknown returns 0
  - `TestHasRole` — verify hierarchy: super_admin >= all, tenant_admin >= all except super_admin, api_user >= api_user only
  - `TestRequireRole_Allowed` — simulate request with super_admin role accessing tenant_admin-required endpoint → passes
  - `TestRequireRole_Denied` — simulate request with analyst role accessing sim_manager endpoint → 403 INSUFFICIENT_ROLE
  - `TestRequireRole_ExactMatch` — sim_manager accessing sim_manager endpoint → passes
  - `TestRequireRole_MissingRole` — no role in context → 403 FORBIDDEN
  - `TestRequireScope_JWTBypass` — JWT auth type bypasses scope check
  - `TestRequireScope_Allowed` — api_key with matching scope → passes
  - `TestRequireScope_Denied` — api_key without required scope → 403 SCOPE_DENIED
  - Use `httptest.NewRecorder()` and `httptest.NewRequest()` for HTTP testing
  - Set context values using `context.WithValue` to simulate auth middleware output
- **Verify:** `go test ./internal/gateway/...`

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| RBAC middleware reads role from JWT claims | Task 2 (RequireRole reads apierr.RoleKey from ctx) | Task 4, TestRequireRole_* |
| Every API endpoint has a required minimum role | Task 3 (router applies RequireRole per route group) | Task 4, build verification |
| Role hierarchy: super_admin > tenant_admin > operator_manager > sim_manager > policy_editor > analyst > api_user | Task 2 (roleLevels map) | Task 4, TestRoleLevel, TestHasRole |
| 403 FORBIDDEN returned when role insufficient | Task 2 (RequireRole returns 403 INSUFFICIENT_ROLE) | Task 4, TestRequireRole_Denied |
| api_user role checked against API key scopes | Task 2 (RequireScope checks scopes from context) | Task 4, TestRequireScope_* |
| Permission matrix matches ARCHITECTURE.md RBAC table | Task 3 (route registration with correct roles) | Task 4, code review |
| Tenant scoping: all queries filtered by tenant_id from JWT | Already enforced at store layer (TenantIDKey in context) | Existing store layer |

## Story-Specific Compliance Rules

- **API:** Standard error envelope `{ status: "error", error: { code, message, details? } }` for 403 responses
- **Middleware:** RBAC runs AFTER JWTAuth (position 8 in middleware chain per MIDDLEWARE.md)
- **Auth:** Role hierarchy numeric comparison, not string comparison
- **Business:** api_user is scope-based, not role-hierarchy-based for endpoint access
- **ADR:** Multi-tenant isolation enforced at store layer, RBAC is an additional layer

## Risks & Mitigations

- **Risk:** Future endpoints need consistent RBAC annotations. **Mitigation:** Router pattern uses `r.With(RequireRole(...))` per group, making it impossible to add a route without specifying a role.
- **Risk:** API key scope checking needs auth_type in context, which JWTAuth doesn't set yet. **Mitigation:** Add AuthTypeKey to context in JWTAuth (minor modification); for now, RequireScope gracefully handles missing auth_type by defaulting to JWT behavior (pass through).

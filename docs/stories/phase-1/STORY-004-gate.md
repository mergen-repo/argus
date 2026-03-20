# Gate Report: STORY-004 — RBAC Middleware & Permission Enforcement

**Date:** 2026-03-20
**Gate Agent:** Claude Opus 4.6
**Status:** PASS (with 1 escalation)

---

## Pass 1: Requirements Tracing

### Acceptance Criteria Tracing

| # | Criterion | Status | Evidence |
|---|-----------|--------|----------|
| AC-1 | RBAC middleware reads role from JWT claims | PASS | `rbac.go:31` reads `apierr.RoleKey` from context (set by `JWTAuth` in `auth_middleware.go:43`). Test: `TestRequireRole_Allowed`, `TestRequireRole_Denied` |
| AC-2 | Every API endpoint has a required minimum role | PASS | `router.go:26` applies `RequireRole("api_user")` to all authenticated routes. Currently only logout and 2fa/setup exist as authenticated endpoints; future endpoints will follow same pattern per plan. |
| AC-3 | Role hierarchy: super_admin > tenant_admin > operator_manager > sim_manager > policy_editor > analyst > api_user | PASS | `rbac.go:10-18` defines `roleLevels` map with levels 7 down to 1. Test: `TestRoleLevel` (9 cases), `TestHasRole` (14 cases), `TestRequireRole_AllRolesHierarchy` (7x7=49 combinations) |
| AC-4 | 403 FORBIDDEN returned when role insufficient | PASS | `rbac.go:39` returns 403 with `INSUFFICIENT_ROLE` code and details `{required_role, current_role}`. Test: `TestRequireRole_Denied` verifies 403 + error code |
| AC-5 | api_user role checked against API key scopes | PASS | `rbac.go:50-72` `RequireScope` checks scopes from context when `auth_type == "api_key"`, passes through for JWT auth. Tests: `TestRequireScope_JWTBypass`, `TestRequireScope_Allowed`, `TestRequireScope_Denied`, `TestRequireScope_EmptyScopes` |
| AC-6 | Permission matrix matches ARCHITECTURE.md RBAC table exactly | ESCALATE | Linear hierarchy cannot represent the ARCHITECTURE.md matrix exactly. See Escalation #1 below. |
| AC-7 | Tenant scoping: all queries filtered by tenant_id from JWT | PASS | Enforced at store layer (existing). JWTAuth sets `TenantIDKey` in context (`auth_middleware.go:41`). Not RBAC middleware responsibility. |

### Issues Found & Fixed

| # | Severity | Issue | Fix |
|---|----------|-------|-----|
| 1 | MINOR | `RequireRole` returned 403 with message "Authentication required" when role is empty. This is semantically incorrect -- 403 is authorization, not authentication. The message is misleading. | **FIXED** -- Changed message to "No role assigned to current user" in `rbac.go:34`. |

---

## Pass 2: Compliance Check

### Architecture Layer Separation
- **apierr package** (`internal/apierr/apierr.go`): Error codes, context keys, response helpers. No business logic. PASS.
- **Gateway layer** (`internal/gateway/rbac.go`): Cross-cutting RBAC middleware. Uses only apierr for error responses. PASS.
- **Router** (`internal/gateway/router.go`): Route registration with `RequireRole()` per group. PASS.
- No new DB tables, no store layer changes. PASS.

### API Envelope Format
- INSUFFICIENT_ROLE: `{ "status": "error", "error": { "code": "INSUFFICIENT_ROLE", "message": "...", "details": [{"required_role": "...", "current_role": "..."}] } }` with 403. Matches ERROR_CODES.md exactly. PASS.
- SCOPE_DENIED: `{ "status": "error", "error": { "code": "SCOPE_DENIED", "message": "...", "details": [{"required_scope": "...", "available_scopes": [...]}] } }` with 403. Matches ERROR_CODES.md exactly. PASS.
- FORBIDDEN: `{ "status": "error", "error": { "code": "FORBIDDEN", "message": "..." } }` with 403. PASS.

### RBAC Table Alignment
- Error codes `FORBIDDEN`, `INSUFFICIENT_ROLE`, `SCOPE_DENIED` match ERROR_CODES.md. PASS.
- Middleware chain position 8 (RBAC after Auth at position 6). In router, `RequireRole` is applied after `JWTAuth`. PASS.
- Role hierarchy matches MIDDLEWARE.md spec. PASS.

### Naming Conventions
- Go: camelCase for variables/fields. PASS.
- Routes: kebab-case. PASS.
- Constants: PascalCase per Go conventions. PASS.

### ADR Compliance
- ADR-001 (Modular Monolith): RBAC is internal package. PASS.
- Chi v5 middleware pattern (`func(http.Handler) http.Handler`). PASS.

---

## Pass 2.5: Security Scan

| Check | Status | Notes |
|-------|--------|-------|
| Auth bypass via missing middleware | PASS | All authenticated routes have `JWTAuth` + `RequireRole("api_user")` applied as group-level middleware. Cannot add a route to the group without both. |
| Role escalation via unknown role | PASS | Unknown roles return level 0, which fails `HasRole` against any valid role. Test: `TestHasRole/unknown_role_cannot_access_anything` |
| Partial token bypass | PASS | `JWTAuth` blocks partial tokens before `RequireRole` runs. Verified in STORY-003 gate. |
| Scope bypass for JWT users | PASS | `RequireScope` correctly passes through when `auth_type != "api_key"`. JWT users are governed by role-based checks, not scope checks. |
| Missing auth_type in context | PASS | When `auth_type` is not set (missing from context), type assertion returns empty string, which is not "api_key", so scope check is bypassed (JWT behavior). Graceful degradation. Test: `TestRequireScope_NoAuthType` |
| Empty scopes for api_key | PASS | API key with no scopes gets denied on any scope check. Test: `TestRequireScope_EmptyScopes` |
| Role case sensitivity | INFO | Role comparison is case-sensitive (Go string comparison). JWTAuth sets role from JWT claims as-is. Role values must be lowercase as defined in `roleLevels` map. Not a vulnerability, but worth noting for API key auth implementation. |
| Null/nil context values | PASS | Both `RequireRole` and `RequireScope` use safe type assertions (`value, _ := ....(type)`) that return zero values on nil. |
| 2FA verify route RBAC | PASS | `/2fa/verify` uses `JWTAuthAllowPartial` (separate group) and correctly does NOT have RBAC applied, since it's a pre-auth step. |

---

## Pass 3: Test Execution

### RBAC Tests (12 tests, 80+ sub-cases)

| Test | Sub-cases | Status |
|------|-----------|--------|
| TestRoleLevel | 9 (7 roles + unknown + empty) | PASS |
| TestHasRole | 14 (hierarchy permutations) | PASS |
| TestRequireRole_Allowed | 1 | PASS |
| TestRequireRole_ExactMatch | 1 | PASS |
| TestRequireRole_Denied | 1 | PASS |
| TestRequireRole_MissingRole | 1 | PASS |
| TestRequireScope_JWTBypass | 1 | PASS |
| TestRequireScope_NoAuthType | 1 | PASS |
| TestRequireScope_Allowed | 1 | PASS |
| TestRequireScope_Denied | 1 | PASS |
| TestRequireScope_EmptyScopes | 1 | PASS |
| TestRequireRole_AllRolesHierarchy | 49 (7x7 matrix) | PASS |

### Full Suite Regression Check
- All test packages: PASS
- No regressions introduced

### Test Coverage Assessment
- **Strong coverage**: Role level mapping (all 7 + unknown + empty), hierarchy logic (14 permutations), full 7x7 matrix, RequireRole (allowed/denied/exact/missing), RequireScope (JWT bypass/no auth type/allowed/denied/empty scopes)
- **Edge cases covered**: Unknown roles, empty roles, empty scopes, missing context values, missing auth_type
- **Not tested (acceptable)**: HTTP response body structure verification for INSUFFICIENT_ROLE details (code is tested, message/details format verified by inspection)

---

## Pass 4: Performance Analysis

- `RoleLevel()`: O(1) map lookup. Negligible overhead.
- `HasRole()`: Two O(1) map lookups + integer comparison. Negligible overhead.
- `RequireRole` middleware: One context value extraction + two map lookups + integer comparison. ~100ns per request. PASS.
- `RequireScope` middleware: One context value extraction + O(n) scope list scan where n = number of scopes per API key (typically <10). Negligible. PASS.
- No allocations on the happy path (no error response construction). PASS.
- No database queries, no Redis calls, no I/O. PASS.

---

## Pass 5: Build Verification

| Command | Status |
|---------|--------|
| `go build ./...` | PASS |
| `go test ./internal/gateway/...` | PASS (12 tests) |
| `go test ./...` | PASS (all packages) |
| `go vet ./...` | PASS (no warnings) |

---

## Pass 6: UI Check

SKIPPED -- Backend-only story, no UI components.

---

## Fixes Applied

1. **`internal/gateway/rbac.go:34`** -- Changed misleading error message from "Authentication required" to "No role assigned to current user" when `RequireRole` encounters an empty role in context. The 403 status code is for authorization failures, not authentication failures.

---

## Escalations

### ESCALATION #1: Linear Role Hierarchy vs. ARCHITECTURE.md RBAC Matrix

**Severity:** DESIGN
**Impact:** Future stories (SIM management, policy management, eSIM)

The ARCHITECTURE.md RBAC matrix defines non-linear permissions that cannot be represented by a strict linear hierarchy:

| Action | operator_manager (L5) | sim_manager (L4) |
|--------|:---------------------:|:-----------------:|
| Manage APNs | ALLOWED | DENIED |
| Manage SIMs | **DENIED** | ALLOWED |
| Manage policies | **DENIED** | DENIED |
| Force disconnect | **DENIED** | ALLOWED |

With the current linear hierarchy (`operator_manager=5 > sim_manager=4`), `RequireRole("sim_manager")` on SIM routes would also allow `operator_manager` access, which contradicts the RBAC matrix.

Similarly, `policy_editor (L3)` can manage policies, but `sim_manager (L4)` and `operator_manager (L5)` cannot -- yet the linear hierarchy would grant them access.

**Current state:** The story AC explicitly mandates a linear hierarchy AND "Permission matrix matches ARCHITECTURE.md RBAC table exactly." These two requirements are mutually contradictory.

**Impact assessment:** This is NOT a problem for STORY-004 itself (current routes only use `api_user` minimum role). It will become a problem when SIM, policy, and eSIM routes are added in later stories.

**Options:**
1. **Permission-based RBAC** -- Replace `RequireRole(minRole)` with `RequirePermission(permission)` using a role-to-permission mapping. Most flexible, matches the matrix exactly.
2. **Branching hierarchy** -- Keep roles but add explicit deny lists per role. More complex.
3. **Accept linear hierarchy** -- Document that operator_manager CAN access SIM routes (differs from ARCHITECTURE.md matrix). Simplest but deviates from spec.

**Recommendation:** Option 1 (permission-based RBAC) to be decided before SIM management story (STORY ~phase 3). No code change needed now.

---

## Observations (Non-Blocking)

1. **JWTAuth does not set `auth_type` in context.** The `RequireScope` middleware handles this gracefully (defaults to JWT pass-through behavior). When API key authentication is implemented (STORY-008), it should set `AuthTypeKey = "api_key"` and `ScopesKey = []string{...}` in context. Documented in plan as known risk.

2. **Router currently has only 2 authenticated endpoints.** `RequireRole("api_user")` is applied as group-level middleware, which means all future routes added to this group automatically get RBAC. The pattern is correct and safe.

3. **No TenantContext middleware yet.** Per MIDDLEWARE.md, TenantContext (position 7) should run between Auth (6) and RBAC (8). This will be added in a later story. Current RBAC works without it.

4. **`super_admin` cross-tenant bypass.** MIDDLEWARE.md says "super_admin bypasses tenant_id scoping for cross-tenant operations." This is not implemented in RBAC middleware (it's a store-layer concern). Acceptable -- noted for future reference.

---

## Summary

| Category | Result |
|----------|--------|
| Requirements Coverage | 6/7 ACs verified, 1 escalated |
| Compliance | PASS |
| Security | PASS (1 minor fix applied) |
| Tests | 12/12 PASS (80+ sub-cases), no regressions |
| Performance | O(1) middleware, no I/O, no allocations on happy path |
| Build | PASS |
| Fixes Applied | 1 (misleading error message) |
| Escalations | 1 (linear hierarchy vs. RBAC matrix) |

**GATE STATUS: PASS**

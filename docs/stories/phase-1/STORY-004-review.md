# Post-Story Review: STORY-004 — RBAC Middleware & Permission Enforcement

> Date: 2026-03-20

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| STORY-005 | Tenant CRUD requires `RequireRole("super_admin")` on tenant endpoints and `RequireRole("tenant_admin")` on user endpoints. Current `RequireRole` works correctly for these linear cases (super_admin > tenant_admin). API-008 PATCH users/:id has "tenant_admin+ or self" logic -- handler must check self-edit permission separately since RBAC middleware only checks minimum role. | NO_CHANGE |
| STORY-008 | API key auth will set `AuthTypeKey = "api_key"` and `ScopesKey = []string{...}` in context. `RequireScope` middleware is already prepared for this -- it checks `auth_type == "api_key"` and validates scope lists. STORY-008 should wire these context keys in the API key auth middleware. | NO_CHANGE |
| STORY-009 | Operator CRUD needs `RequireRole("super_admin")` for manage operators. Linear hierarchy handles this correctly. No RBAC conflict. | NO_CHANGE |
| STORY-011 | SIM CRUD is where the RBAC escalation hits. `RequireRole("sim_manager")` would allow operator_manager (L5) access, but ARCHITECTURE.md says operator_manager CANNOT manage SIMs. Must implement permission-based RBAC before or as part of this story. | ESCALATION_TRACKED |
| STORY-022 | Policy CRUD has the same issue -- policy_editor (L3) can manage policies, but sim_manager (L4) and operator_manager (L5) cannot. Permission-based refinement needed. | ESCALATION_TRACKED |

## Escalation Tracking

### ESC-001: Linear Role Hierarchy vs. Non-Linear RBAC Matrix

- **Origin:** STORY-004 gate report, Escalation #1
- **Severity:** DESIGN
- **Deadline:** Before STORY-011 (SIM CRUD, Phase 2)
- **Decision needed:** Switch from `RequireRole(minRole)` to `RequirePermission(permission)` with a role-to-permission mapping table
- **Current impact:** None -- all Phase 1 stories use either `api_user` (baseline) or `super_admin`/`tenant_admin` (strictly linear). Conflict only surfaces when SIM, operator, and policy routes coexist.
- **Recorded in:** decisions.md as DEV-009

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| decisions.md | Added DEV-009: Linear RBAC hierarchy escalation -- permission-based refinement deferred to pre-STORY-011 | UPDATED |
| ROUTEMAP.md | STORY-004 marked DONE, progress updated to 4/55 (7%) | UPDATED |
| GLOSSARY.md | No new terms (RBAC, role hierarchy already defined) | NO_CHANGE |
| ARCHITECTURE.md | No changes (RBAC matrix in doc is the target; implementation will evolve to match) | NO_CHANGE |
| FUTURE.md | No new items or invalidated items | NO_CHANGE |
| FRONTEND.md | No changes (backend-only story) | NO_CHANGE |
| ERROR_CODES.md | No changes (FORBIDDEN, INSUFFICIENT_ROLE, SCOPE_DENIED already documented; note: doc references `internal/gateway/errors.go` but codes are in `internal/apierr/apierr.go` -- known drift from STORY-003 review) | NO_CHANGE |
| MIDDLEWARE.md | No changes (implementation matches step 8 RBAC spec: role hierarchy, RequireRole, RequireScope, 403 error codes) | NO_CHANGE |
| CONFIG.md | No changes (no new env vars) | NO_CHANGE |
| Makefile | No changes | NO_CHANGE |
| CLAUDE.md | No changes | NO_CHANGE |

## Cross-Doc Consistency

- MIDDLEWARE.md step 8 defines RBAC role hierarchy as `super_admin > tenant_admin > operator_manager > sim_manager > policy_editor > analyst > api_user`. Implementation in `rbac.go:10-18` matches exactly.
- MIDDLEWARE.md step 8 specifies three error codes: FORBIDDEN, INSUFFICIENT_ROLE, SCOPE_DENIED. All three implemented and defined in `apierr/apierr.go:35-37`.
- MIDDLEWARE.md context keys table shows `auth_type`, `scopes` set by Auth step 6. Implementation uses typed `contextKey` constants (`AuthTypeKey`, `ScopesKey`) in apierr package. Functionally correct.
- MIDDLEWARE.md implementation pattern shows `r.With(rbac.Require("sim_manager"))` per-route pattern. Current implementation uses `RequireRole("api_user")` as group-level middleware. Both are valid Chi patterns; per-route overrides will be added as endpoints grow.
- ERROR_CODES.md package reference (`internal/gateway/errors.go`) still doesn't match actual location (`internal/apierr/apierr.go`). Previously noted in STORY-003 review. Non-blocking.
- ARCHITECTURE.md RBAC matrix defines non-linear permissions that the current linear hierarchy cannot enforce. This is the tracked escalation (ESC-001). No contradiction in Phase 1 -- becomes relevant in Phase 2+.
- Router middleware order: Recoverer > RequestID > RealIP > Logger (global), then JWTAuth > RequireRole per group. MIDDLEWARE.md specifies RateLimiter and CORS between logging and auth; these are not yet implemented (STORY-008 for rate limiting, later story for CORS). Acceptable -- features added incrementally.

## Observations

1. **RequireRole as group middleware vs per-route middleware.** Current pattern applies `RequireRole("api_user")` at group level. When more routes exist, finer-grained roles will need per-route `r.With(RequireRole("tenant_admin"))` calls. This aligns with MIDDLEWARE.md implementation pattern.

2. **TenantContext middleware not yet present.** MIDDLEWARE.md positions TenantContext at step 7 (between Auth and RBAC). It will be added in STORY-005. Current RBAC works without it since tenant_id is already in JWT context.

3. **super_admin cross-tenant bypass.** MIDDLEWARE.md says "super_admin bypasses tenant_id scoping for cross-tenant operations." This is a store-layer concern, not RBAC middleware. Will be addressed when TenantContext is implemented (STORY-005).

4. **Role case sensitivity.** Role comparison is case-sensitive (Go string comparison). JWT claims set role as lowercase. API key auth (STORY-008) must also set role as lowercase to be consistent.

5. **Gate found and fixed 1 issue.** RequireRole returned misleading "Authentication required" message for empty role. Fixed to "No role assigned to current user." Correct -- 403 is authorization, not authentication.

## Project Health

- Stories completed: 4/55 (7%)
- Current phase: Phase 1 -- Foundation (4/8 stories done, 50% of Phase 1)
- Next story: STORY-005 (Tenant Management & User CRUD)
- Blockers: None
- Escalations: 1 active (ESC-001: linear RBAC hierarchy, deadline pre-STORY-011)
- Quality: 12 new RBAC tests (80+ sub-cases including 7x7 matrix), all passing. Full suite green. 1 gate fix applied.
- Cumulative tests: ~30 test functions across gateway, auth, apierr packages

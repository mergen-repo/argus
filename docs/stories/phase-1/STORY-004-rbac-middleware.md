# STORY-004: RBAC Middleware & Permission Enforcement

## User Story
As an admin, I want role-based access control enforced on all endpoints, so that users can only access features permitted by their assigned role.

## Description
Implement RBAC middleware that checks user role from JWT against endpoint permission requirements. 7 roles as defined in G-019. Middleware runs after auth, before handler.

## Architecture Reference
- Services: SVC-01 (Gateway RBAC middleware)
- Database Tables: TBL-02 (users.role)
- Source: docs/ARCHITECTURE.md (RBAC Matrix)
- Packages: internal/gateway, internal/auth

## Screen Reference
- None (middleware only, affects all screens)

## Acceptance Criteria
- [ ] RBAC middleware reads role from JWT claims
- [ ] Every API endpoint has a required minimum role
- [ ] Role hierarchy: super_admin > tenant_admin > operator_manager > sim_manager > policy_editor > analyst > api_user
- [ ] 403 FORBIDDEN returned when role insufficient
- [ ] api_user role checked against API key scopes (not JWT role)
- [ ] Permission matrix matches ARCHITECTURE.md RBAC table exactly
- [ ] Tenant scoping: all queries filtered by tenant_id from JWT (enforced at store layer)

## API Contract
- No new endpoints. Middleware applied to all existing /api/v1/* routes.
- Failed authorization: `{ status: "error", error: { code: "FORBIDDEN", message: "Insufficient permissions" } }` with 403

## Dependencies
- Blocked by: STORY-003 (JWT auth)
- Blocks: STORY-005 (tenant management needs admin-only enforcement)

## Test Scenarios
- [ ] super_admin can access /api/v1/tenants
- [ ] tenant_admin cannot access /api/v1/tenants (403)
- [ ] sim_manager can access /api/v1/sims but not /api/v1/policies (POST)
- [ ] analyst can read analytics but cannot modify SIMs (403)
- [ ] api_user with scope "sims:read" can GET /sims but not POST /sims
- [ ] Missing JWT returns 401, not 403
- [ ] Tenant A user cannot see Tenant B data (tenant isolation)

## Effort Estimate
- Size: M
- Complexity: Medium

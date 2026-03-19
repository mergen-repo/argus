# STORY-005: Tenant Management & User CRUD

## User Story
As a super admin, I want to create and manage tenants with resource limits, and as a tenant admin, I want to manage users within my tenant.

## Description
CRUD for tenants (super_admin only) and users (tenant_admin within own tenant). Includes invite flow, resource limit enforcement, and user state management.

## Architecture Reference
- Services: SVC-03 (Core API)
- API Endpoints: API-006 to API-008 (Users), API-010 to API-014 (Tenants)
- Database Tables: TBL-01 (tenants), TBL-02 (users)
- Source: docs/architecture/api/_index.md (Auth & Users, Tenants sections)

## Screen Reference
- SCR-110: Settings — Users & Roles (docs/screens/SCR-110-settings-users.md)
- SCR-121: Tenant Management (docs/screens/SCR-121-tenant-management.md)

## Acceptance Criteria
- [ ] POST /api/v1/tenants creates tenant with name, domain, resource limits
- [ ] GET /api/v1/tenants lists all tenants (super_admin)
- [ ] GET /api/v1/tenants/:id returns tenant detail + stats (SIM count, user count)
- [ ] PATCH /api/v1/tenants/:id updates tenant (name, limits, state)
- [ ] POST /api/v1/users creates user in tenant, sends invite email placeholder
- [ ] GET /api/v1/users lists users in own tenant (filtered by tenant_id)
- [ ] PATCH /api/v1/users/:id updates role, state (disable/enable)
- [ ] Resource limits enforced: cannot create user if max_users reached
- [ ] User creation triggers audit log entry
- [ ] Tenant state transitions: active → suspended → terminated

## API Contract

| Ref | Method | Path | Request | Response (data) | Auth | Status Codes |
|-----|--------|------|---------|-----------------|------|-------------|
| API-010 | GET | /api/v1/tenants | `?cursor&limit&state` | `[{id,name,domain,state,sim_count,user_count}]` | JWT(super_admin) | 200, 401, 403 |
| API-011 | POST | /api/v1/tenants | `{name,domain,contact_email,max_sims,max_apns,max_users}` | `{id,name,domain,...}` | JWT(super_admin) | 201, 400, 409 |
| API-006 | GET | /api/v1/users | `?cursor&limit&role` | `[{id,email,name,role,state,last_login_at}]` | JWT(tenant_admin+) | 200 |
| API-007 | POST | /api/v1/users | `{email,name,role}` | `{id,email,name,role,state:"invited"}` | JWT(tenant_admin+) | 201, 400, 409, 422 |
| API-008 | PATCH | /api/v1/users/:id | `{name?,role?,state?}` | `{id,email,name,role,state}` | JWT(tenant_admin+ or self) | 200, 400, 403, 404 |
| API-012 | GET | /api/v1/tenants/:id | — | `{id,name,domain,state,sim_count,user_count,resource_limits}` | JWT(super_admin or own tenant) | 200, 403, 404 |
| API-013 | PATCH | /api/v1/tenants/:id | `{name?,contact_email?,max_sims?,max_apns?,max_users?,state?}` | `{id,name,domain,state,...}` | JWT(super_admin or tenant_admin) | 200, 400, 403, 404 |
| API-014 | GET | /api/v1/tenants/:id/stats | — | `{sim_count,user_count,apn_count,active_sessions,storage_bytes}` | JWT(tenant_admin+) | 200, 403, 404 |

## Database Changes
- No new tables (TBL-01, TBL-02 from STORY-002)

## Dependencies
- Blocked by: STORY-004 (RBAC)
- Blocks: STORY-006 (config), Phase 2 stories

## Test Scenarios
- [ ] Create tenant with valid data → 201
- [ ] Create tenant with duplicate domain → 409
- [ ] Create user within tenant → 201, user state = "invited"
- [ ] Create user when max_users reached → 422 RESOURCE_LIMIT_EXCEEDED
- [ ] Tenant admin cannot see other tenants → only own tenant data
- [ ] Update user role from sim_manager to analyst → 200
- [ ] Disable user → subsequent login returns 403 ACCOUNT_DISABLED
- [ ] Tenant stats reflect actual SIM/user counts

## Effort Estimate
- Size: M
- Complexity: Medium

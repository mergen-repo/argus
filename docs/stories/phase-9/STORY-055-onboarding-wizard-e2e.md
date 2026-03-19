# STORY-055: Tenant Onboarding Wizard End-to-End Test

## User Story
As a QA engineer, I want to test the complete tenant onboarding flow from creation to first authenticated session, so that I can verify the entire provisioning pipeline works seamlessly.

## Description
End-to-end test of the tenant onboarding journey: super_admin creates tenant → invites admin user → admin logs in → onboarding wizard: connect operator → define APN → import SIMs → assign policy → verify SIM authenticates via RADIUS. Tests the entire provisioning chain including tenant isolation (Tenant A cannot see Tenant B data).

## Architecture Reference
- Services: All services (SVC-01 through SVC-10)
- API Endpoints: API-011 (create tenant), API-007 (create user), API-001 (login), API-024 (test operator), API-031 (create APN), API-063 (import SIMs), API-091 (create policy), API-095 (activate policy)
- Source: All architecture documents

## Screen Reference
- SCR-121: Tenant Management (create tenant)
- SCR-003: Onboarding Wizard (5-step flow)
- SCR-010: Dashboard (verify data after onboarding)
- SCR-020: SIM List (verify imported SIMs)

## Acceptance Criteria
- [ ] Test 1 — Create Tenant: super_admin creates tenant via API-011, receives tenant_id
- [ ] Test 2 — Invite Admin: super_admin creates admin user for new tenant via API-007
- [ ] Test 3 — Admin Login: new admin logs in via API-001, receives JWT with correct tenant_id
- [ ] Test 4 — Operator Connect: admin grants operator access (API-026), tests connection (API-024) → success
- [ ] Test 5 — Create APN: admin creates APN with IP pool (API-031) → APN active
- [ ] Test 6 — Import SIMs: admin uploads CSV with 5 SIMs (API-063) → job completes, 5 SIMs active
- [ ] Test 7 — Create Policy: admin creates policy with DSL (API-091) → draft version
- [ ] Test 8 — Activate Policy: admin activates policy (API-095) → version active
- [ ] Test 9 — Assign Policy: SIMs auto-assigned to APN's default policy
- [ ] Test 10 — RADIUS Auth: send Access-Request for tenant's SIM → Access-Accept
- [ ] Test 11 — Tenant Isolation: Tenant B admin cannot see Tenant A's SIMs (API-040 returns empty)
- [ ] Test 12 — Dashboard: admin's dashboard shows correct SIM count, session data
- [ ] Full flow completes in < 90 seconds
- [ ] Test creates and tears down all resources (idempotent)
- [ ] Test runs in CI pipeline

## Dependencies
- Blocked by: All Phase 1-8 stories (requires full stack including frontend)
- Blocks: None

## Test Scenarios
- [ ] Full onboarding flow succeeds end-to-end
- [ ] Tenant isolation verified: cross-tenant access returns 403 or empty
- [ ] Onboarding wizard steps match API calls (frontend + backend consistency)
- [ ] Invalid operator credentials in Step 2 → error, retry succeeds
- [ ] CSV import with 1 invalid SIM → 4 succeed, 1 in error report
- [ ] After full onboarding: RADIUS auth for tenant's SIM → Access-Accept with correct policy

## Effort Estimate
- Size: L
- Complexity: High

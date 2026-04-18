# FIX-103: Tenant List `sim_count` / `user_count` Always Zero

> Tier 2 (user-visible) — API↔DB divergence. Tenant list UI and any
> dashboard or report consuming `/tenants` shows 0 SIMs and 0 users for
> every tenant, while the DB has the real counts. Every admin who looks
> at the tenant list gets wrong data.

## User Story

As a super_admin viewing `/system/tenants`, I see correct SIM count and
user count for each tenant, matching DB reality.

## Source Finding

- UAT Batch 1 report: `docs/reports/uat-acceptance-batch1-2026-04-18.md` — **F-1 CRITICAL**
- Evidence (verified by hand):
  ```
  API /api/v1/tenants returns:
  UAT Acme: sim_count=0, user_count=0
  Bosphorus: sim_count=0, user_count=0
  Nar: sim_count=0, user_count=0
  Demo: sim_count=0, user_count=0

  DB reality:
  Bosphorus IoT: 27 sims, 5 users
  UAT Acme Corp: 0 sims, 1 user   ← one user, API says 0
  Nar Teknoloji UAT: 577 sims, 6 users
  Argus Demo UAT: 55 sims, 4 users
  ```
- Classification: real bug in `TenantHandler.List` — missing JOIN/aggregation or stale column that is never populated
- Scope: affects tenant LIST endpoint; GET single tenant / GET /tenants/{id}/stats may or may not be affected (verify in plan)

## Acceptance Criteria

- [ ] AC-1: `GET /api/v1/tenants` response includes `sim_count` and `user_count` matching DB counts for each tenant (tolerance: exact)
- [ ] AC-2: Counts are correct regardless of caller role (super_admin sees all tenants' counts; tenant_admin sees only their own tenant's count — within the scoped list)
- [ ] AC-3: Query uses efficient aggregation: either `LATERAL` subqueries, a single join with `GROUP BY`, or denormalised counter columns maintained by triggers. Plan MUST surface the decision — N+1 per-tenant COUNT queries are NOT acceptable at any tenant-count beyond 10.
- [ ] AC-4: `GET /api/v1/tenants/{id}` (single) returns the same `sim_count` / `user_count` fields consistently
- [ ] AC-5: `GET /api/v1/tenants/{id}/stats` (if exists, per API-014 in `_index.md`) agrees with list endpoint
- [ ] AC-6: Regression test: seed fixture → call `/tenants` → assert counts match `SELECT COUNT(*) FROM sims GROUP BY tenant_id` exactly
- [ ] AC-7: UI `/system/tenants` renders the correct numbers without client-side recomputation

## Out of Scope

- Denormalised counter maintenance (trigger vs app-level) is a plan decision; if chosen, it lives inside this story — not a separate FIX
- Performance benchmarking beyond correctness

## Dependencies

- Blocked by: —
- Blocks: UAT-001 verify 6 (dashboard counts), any tenant-list-dependent dashboard

## Architecture Reference

- Backend: `internal/api/tenant/handler.go` — `List` handler
- Store: `internal/store/tenant.go` — `ListTenants` / query construction
- Frontend: `web/src/pages/system/tenants/` — list page
- DB schema: `tenants` table (no count columns today — grep to confirm) + `sims.tenant_id` + `users.tenant_id`

## Test Scenarios

- [ ] Unit: `ListTenants` returns populated `sim_count/user_count` in the row struct
- [ ] Integration: insert N SIMs for tenant T → GET `/tenants` → that row shows `sim_count=N`
- [ ] Integration: tenant with 0 SIMs returns `sim_count=0` (not null, not missing)
- [ ] Regression: UAT-001 verify 6 passes

## Effort

S — single handler + store function + query rework. Can be done in one Dev iteration.

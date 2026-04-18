# FIX-101: Onboarding Flow — End-to-End Completeness

> Tier 2 (user-visible) — UAT-001 is fundamentally unusable today. Wizard
> cannot advance past Step 2 because the UI does not collect admin
> credentials that the backend requires. Multiple related defects stack
> (tenant create modal missing fields, auto-admin not created, role scope
> blocks wizard actions).

## User Story

As a newly-onboarded Tenant Admin, I can complete the full 5-step wizard
end-to-end using only the credentials delivered by the invite email,
without any step requiring super_admin role or silently dropping data the
backend needs.

## Source Findings Bundled

- UAT Batch 1 report: `docs/reports/uat-acceptance-batch1-2026-04-18.md`
- **F-9 CRITICAL** — Wizard Step 2 submits without `admin_email`, `admin_name`, `admin_password`; backend rejects `422 VALIDATION_ERROR`
- **F-2 HIGH** — Create-Tenant modal (`/system/tenants`) missing required inputs: `contact_email`, `domain`, `contact_phone`, `max_apns`
- **F-3 HIGH** — `POST /tenants` does not auto-create the tenant_admin user; `slug` field returned by API but not a DB column
- **F-4 HIGH** — `POST /users` ignores `tenant_id` in body; always assigns caller's tenant. Super_admin cannot provision a user in a tenant they don't belong to
- **F-5 HIGH** — Wizard calls `POST /operators/{id}/test` which requires super_admin, returns 403 for tenant_admin — wizard unusable by the role it's designed for

## Acceptance Criteria

### A. Tenant creation (F-2, F-3)

- [ ] AC-1: `POST /api/v1/tenants` (super_admin only) accepts a single payload that creates the tenant row AND a bootstrap admin user atomically. Required body: `name`, `slug` (if column exists) OR `domain`, `contact_email`, `contact_phone`, resource limits (`max_sims`, `max_apns`, `max_users`), `admin_name`, `admin_email`, `admin_initial_password` (OR send-invite flag).
- [ ] AC-2: Create-Tenant modal (`web/src/pages/system/tenants/*`) collects every field the API requires. No silent defaults that hide required inputs from the operator.
- [ ] AC-3: After a successful create, DB has: 1 `tenants` row, 1 `users` row with `role='tenant_admin'`, linked by `tenant_id`. Audit log captures both events through the canonical writer (FIX-104).
- [ ] AC-4: `slug` field is either a real DB column (add migration) OR removed from API/UI responses. Pick one, document in plan.
- [ ] AC-5: Duplicate detection: creating a tenant with an existing `slug`/`domain` returns `409 CONFLICT` with actionable error.

### B. Cross-tenant user provisioning (F-4)

- [ ] AC-6: `POST /api/v1/users` honors `tenant_id` in body when caller has `super_admin` role. For other roles, body `tenant_id` is silently ignored (caller's tenant enforced). Validation returns 403 if super_admin omits `tenant_id` unexpectedly.
- [ ] AC-7: Regression test: super_admin creates a user in tenant B while logged into tenant A → row inserted with `tenant_id = B`.

### C. Onboarding wizard Step 2 (F-9)

- [ ] AC-8: Wizard data-model mirrors `/api/v1/onboarding/{session_id}/step/2` contract. Fields collected in the UI match fields required by backend schema, verified by a shared TypeScript type (generated from Go struct tags or kept in `web/src/types/api.ts`).
- [ ] AC-9: If the backend actually needs `admin_email/name/password` at step 2, the UI collects them; otherwise the backend schema is relaxed. Plan must surface which side changes and why — both sides cannot remain mismatched.
- [ ] AC-10: Wizard can advance from Step 2 → Step 3 → ... → Step 5 end-to-end as the tenant_admin delivered by the invite email, with zero 4xx responses.

### D. Operator test-in-wizard role scope (F-5)

- [ ] AC-11: `POST /api/v1/operators/{id}/test` accepts `tenant_admin` role IF the operator is already granted to that tenant (or about to be, via pending wizard state). Alternative: wizard uses a different endpoint scoped to the onboarding session.
- [ ] AC-12: The wizard "Test" button for each operator row succeeds for a tenant_admin within their legitimate scope.

### E. End-to-end green (meta)

- [ ] AC-13: Rerun UAT-001 — all 10 steps pass, all 8 verify checks pass.

## Out of Scope

- Multi-protocol adapter test-connection bug (F-6, F-7, F-8) — see FIX-106
- SIM bulk-import completeness (F-11..F-14) — see FIX-102

## Dependencies

- Blocked by: FIX-104 (audit chain) — because AC-3 asserts audit rows write through canonical chain; unblocked once FIX-104 lands or FIX-104 AC-1 lands as a partial (same single-writer pattern)
- Blocks: UAT-001 rerun

## Architecture Reference

- Backend tenant: `internal/api/tenant/` (handler + store)
- Backend user: `internal/api/user/` or `internal/api/users/`
- Backend onboarding: `internal/api/onboarding/` — session state machine, step validation
- Backend operator test: `internal/api/operator/handler.go` — the `Test` handler (line ~1151 per prior audit)
- Frontend tenant modal: `web/src/pages/system/tenants/`
- Frontend wizard: `web/src/pages/onboarding/` or similar (SCR-003)
- Related: STORY-001 (tenants), STORY-002 (users), STORY-005 (auth), STORY-003 (onboarding wizard)

## Test Scenarios

- [ ] Integration: tenant create API test covers AC-1..AC-5
- [ ] Integration: `POST /users` with `tenant_id` as super_admin vs tenant_admin (AC-6, AC-7)
- [ ] E2E browser: wizard full flow — dispatch dev-browser agent post-Dev to exercise AC-8..AC-12
- [ ] Regression: UAT-001 entire flow (AC-13) — run by acceptance tester on completion

## Effort

XL — touches 4 Go packages and 2 React page bundles, plus migration for potential `slug` column and onboarding schema realignment. Split into multiple Dev iterations if needed, but keep under one story umbrella for atomic Gate.

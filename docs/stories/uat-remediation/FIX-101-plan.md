# FIX-101 Plan: Onboarding Flow — End-to-End Completeness

> **Bug**: FIX-101 — UAT-001 onboarding flow fundamentally broken
> **Effort**: XL (4 Go packages, 2 React pages, step renumbering)
> **Planner**: Amil FIX Mode
> **Date**: 2026-04-19

## Root Cause Analysis

### F-9 CRITICAL — Wizard step-number misalignment (frontend vs backend)

**Root Cause**: Frontend wizard has 5 steps: (1) Tenant Profile, (2) Operator Connection, (3) APN Config, (4) SIM Import, (5) Policy Setup. Backend `handleStep1–5` expects: (1) Tenant Profile, (2) Admin User Creation, (3) Operator Grants, (4) APN, (5) SIM Import. Every step >= 2 sends the wrong payload schema, causing `422 VALIDATION_ERROR`.

- Frontend wizard step 2 sends `{operator_grants: [...]}` — backend step 2 expects `{admin_email, admin_name, admin_password}`
- Frontend has no admin-creation step at all
- Frontend has Policy as step 5 — backend has no step 5 for policy
- **Files**: `web/src/components/onboarding/wizard.tsx` (steps array, `payloadForStep`), `internal/api/onboarding/handler.go:402-461` (`handleStep2` step2Request)

### F-2 HIGH — Create-Tenant modal missing required fields

**Root Cause**: `web/src/pages/system/tenants.tsx:56-61` — `createForm` only collects `name`, `slug`, `plan`, `max_sims`, `max_users`. Missing: `contact_email` (required by backend), `contact_phone`, `domain`, `max_apns`. Backend handler at `internal/api/tenant/handler.go:155-158` requires `name` and `contact_email`.

- **Files**: `web/src/pages/system/tenants.tsx:56-61` (createForm state), `web/src/pages/system/tenants.tsx:241-299` (SlidePanel form)

### F-3 HIGH — POST /tenants does not auto-create tenant_admin; slug not a real column

**Root Cause (auto-admin)**: `internal/api/tenant/handler.go:147-193` — `Create` handler only inserts a tenant row via `store.Create`. No user creation occurs. The onboarding flow per G-022 decision requires: "Super Admin creates tenant -> auto-create Tenant Admin -> invite email -> wizard". This is unimplemented.

**Root Cause (slug)**: No `slug` column exists in `tenants` table (grep across all migrations confirms zero matches). `internal/api/tenant/handler.go:95` derives slug via `slugify(t.Name)` — pure Go string operation. Frontend form at `tenants.tsx:57` collects and sends a `slug` field the backend silently ignores (not in `createTenantRequest`). Frontend type at `web/src/types/settings.ts:132` declares `slug: string` in `Tenant` interface.

- **Files**: `internal/api/tenant/handler.go:147-193`, `internal/store/tenant.go:55-64` (`CreateTenantParams`), `internal/store/tenant.go:95-126` (`Create`)

### F-4 HIGH — POST /users ignores tenant_id in body

**Root Cause**: `internal/api/user/handler.go:130-134` — `createUserRequest` struct has only `email`, `name`, `role`. No `tenant_id` field. The store method `CreateUserWithPassword` at `internal/store/user.go:408-409` calls `TenantIDFromContext(ctx)` which reads from JWT context (`apierr.TenantIDKey`). A super_admin logged into tenant A cannot create a user in tenant B — the JWT's tenant_id is always used.

- **Files**: `internal/api/user/handler.go:130-134` (`createUserRequest`), `internal/api/user/handler.go:355` (tenant_id from context), `internal/store/user.go:408-439` (`CreateUserWithPassword`)

### F-5 HIGH — Operator test requires super_admin, wizard blocked

**Root Cause**: `internal/gateway/router.go:337-346` — `POST /operators/{id}/test` and `POST /operators/{id}/test/{protocol}` are in a group with `RequireRole("super_admin")`. The onboarding wizard at `wizard.tsx:210` calls `api.post('/operators/${opId}/test')` as tenant_admin, receiving 403.

Per G-028 decision: operators are system-level (super_admin manages), tenants get access grants. HealthCheck is a read-only probe that touches no tenant-scoped data — safe for tenant_admin.

- **Files**: `internal/gateway/router.go:337-343` (middleware group), `web/src/components/onboarding/wizard.tsx:210` (Test button call)

## Key Decisions

| ID | Decision | Rationale |
|----|----------|-----------|
| AC-4 | **slug** is a derived read-only field, NOT a DB column. Remove from create request form; keep in response as computed `slugify(name)`. No migration needed. | Zero migration risk. slug was never persisted. UI can render derived slug as preview. |
| AC-1 | **POST /tenants** auto-creates a bootstrap tenant_admin atomically. New `CreateTenantWithAdmin` store method wraps both INSERTs in a single `pgxpool.BeginTx`. Handler accepts `admin_name`, `admin_email`, `admin_initial_password`. | Matches G-022 decision. Atomic guarantees no orphan tenants without admins. |
| AC-9 | **Realign backend wizard steps to match frontend**. Delete `handleStep2` (admin creation) from onboarding handler. Shift: backend step 2 = Operator Grants, step 3 = APN, step 4 = SIM Import, step 5 = Policy Setup. Admin user is created at tenant-creation time (AC-1), not during wizard. | tenant_admin is already authenticated when running wizard — re-collecting admin creds in step 2 is nonsensical. G-022: "Super Admin creates tenant -> auto-create Tenant Admin -> invite email -> wizard" clearly puts admin creation before wizard. |
| AC-11 | **Move `POST /operators/{id}/test` and `/test/{protocol}` to the tenant_admin middleware group** in router.go. No grant-based conditional logic needed — HealthCheck is a read-only probe. Keep Create/Update/Delete at super_admin. | Simplest fix. HealthCheck touches no tenant-scoped data (G-028). Operator adapter test is safe for any authenticated user with at least tenant_admin role. |

## Regression Notes

- **In-flight onboarding sessions**: Step renumbering changes the semantics of `onboarding_sessions.step_data[0..4]` JSONB arrays. Any active (incomplete) sessions created before this fix will have step_data keyed to old step meanings. **Mitigation**: At dev time, verify no in-flight sessions exist in dev DB. In production, this is pre-release — no real sessions exist. If needed, add a one-time cleanup migration to archive incomplete sessions.
- **Seed data**: `migrations/seed/001_admin_user.sql` creates a bootstrap super_admin. The new auto-admin path creates tenant_admin — no conflict. Verify seed doesn't duplicate.

## Tasks

### Task 1: Store — CreateTenantWithAdmin atomic method

**Files**: `internal/store/tenant.go`

- Add `CreateTenantWithAdminParams` struct extending `CreateTenantParams` with `AdminName`, `AdminEmail`, `AdminPasswordHash` fields
- Add `CreateTenantWithAdmin(ctx, params) (*Tenant, *User, error)` method
- Use `s.db.BeginTx` → INSERT tenants → INSERT users (role=`tenant_admin`, state=`active`, password_change_required=true) → Commit
- On duplicate domain → `ErrDomainExists`; on duplicate email → `ErrEmailExists`
- Return both `*Tenant` and `*User`

**Blocked by**: nothing
**AC coverage**: AC-1, AC-3, AC-5 (partial — domain uniqueness)

---

### Task 2: Tenant handler — expand create with admin fields

**Files**: `internal/api/tenant/handler.go`

- Expand `createTenantRequest` with: `AdminName string`, `AdminEmail string`, `AdminInitialPassword string`, `Domain *string` (already present), `ContactPhone *string` (already present)
- Add validation for admin fields (email format, password min length)
- Call `store.CreateTenantWithAdmin` instead of `store.Create`
- Hash admin password with bcrypt before calling store
- Return response including `admin_user_id` in result
- Dual audit: `tenant.create` + `user.create` entries
- Handle `ErrEmailExists` → 409 for admin_email conflict

**Blocked by**: Task 1
**AC coverage**: AC-1, AC-2 (backend), AC-3, AC-5

---

### Task 3: Tenant modal UI — slug cleanup (frontend only)

**Files**: `web/src/pages/system/tenants.tsx`

- `tenantResponse.Slug` stays as derived read-only (already correct at handler.go:95 — backend needs no changes)
- `createTenantRequest` already has no slug field (correct — backend needs no changes)
- Frontend `createForm` at tenants.tsx:56-61: remove `slug` from state and submit payload
- Keep slug display in table and detail panel as read-only derived from name
- Remove slug input from create dialog form (lines 253-259)

**Blocked by**: nothing
**AC coverage**: AC-4

---

### Task 4: Tenant modal UI — add missing fields

**Files**: `web/src/pages/system/tenants.tsx`, `web/src/hooks/use-settings.ts`

- Expand `createForm` state with: `contact_email`, `contact_phone`, `domain`, `max_apns`, `admin_name`, `admin_email`, `admin_password`
- Remove `slug` and `plan` from create form (slug is derived; plan not in backend API)
- Add form inputs: Contact Email (required), Contact Phone, Domain, Max APNs, Admin Name (required), Admin Email (required), Admin Password (required, min 8 chars)
- Update `handleCreate` to pass all new fields
- Update `useCreateTenant` mutation in `use-settings.ts` if needed to match new API shape
- Ensure submit button disabled until required fields filled

**Blocked by**: Task 2 (backend must accept new fields), Task 3 (slug cleanup)
**AC coverage**: AC-2 (frontend), AC-4 (UI slug cleanup)

---

### Task 5: User handler — cross-tenant provisioning for super_admin

**Files**: `internal/api/user/handler.go`, `internal/store/user.go`

- Add `TenantID *string` to `createUserRequest` struct
- In `Create` handler: if caller is `super_admin` and `req.TenantID` is set, parse it and override context tenant_id for the store call
- If caller is NOT super_admin, silently ignore `req.TenantID` (use JWT context tenant)
- Add `CreateUserInTenant(ctx, tenantID uuid.UUID, params, passwordHash) (*User, error)` to UserStore that takes explicit tenantID instead of reading from context
- Existing `CreateUserWithPassword` delegates to `CreateUserInTenant` with `TenantIDFromContext`
- Tenant resource limit check must use the target tenant_id, not caller's tenant

**Blocked by**: nothing
**AC coverage**: AC-6, AC-7

---

### Task 6: Onboarding backend — realign steps to match frontend

**Files**: `internal/api/onboarding/handler.go`

- Delete `handleStep2` (admin user creation) — admin is now created at tenant-creation time
- Renumber step handlers:
  - `handleStep1` stays (Tenant Profile)
  - NEW `handleStep2` = current `handleStep3` (Operator Grants) — adjust signature
  - NEW `handleStep3` = APN Configuration — see field alignment below
  - NEW `handleStep4` = SIM Import — see field alignment below
  - NEW `handleStep5` = Policy Setup (new handler)
- Update `step()` switch-case to match new numbering
- Remove `step2Request` struct (admin fields)

**Step 3 (APN) field alignment — backend must match frontend payload**:

Frontend sends: `{ apn_name, apn_type, ip_cidr }`.
Old backend `step4Request` expects: `{ apn_name, realm (required), ip_pool_cidr, auth_type (required) }`.

Fix:
- Rename `step4Request` → new `step3Request`
- Add `APNType string` field (`json:"apn_type"`)
- Rename `IPPoolCIDR` JSON tag from `ip_pool_cidr` to `ip_cidr` to match frontend
- Drop `Realm` and `AuthType` as required fields — make optional with defaults (realm="" or derived from apn_name, auth_type="pap")
- Pass `APNType` to `APNService.Create` params; pass defaults for realm/auth_type in settings JSON

**Step 4 (SIM Import) field alignment — backend must match frontend payload**:

Frontend sends: `{ import_mode, iccids: [...], csv_s3_key: '' }`.
Old backend `step5Request` expects: `{ csv_s3_key (required non-empty) }`.

Fix:
- Replace `step5Request` with new `step4Request`:
  - `ImportMode string` (`json:"import_mode"`) — "csv" or "manual"
  - `ICCIDs []string` (`json:"iccids"`) — for manual mode
  - `CSVS3Key string` (`json:"csv_s3_key"`) — for csv mode
- If `import_mode == "manual"` and `iccids` has entries, create SIMs directly or enqueue a job from inline data
- If `import_mode == "csv"` and `csv_s3_key` is non-empty, call `BulkImport.EnqueueImport`
- If both are empty/absent (step is non-mandatory), store step data and succeed silently (skip-friendly)

**Step 5 (Policy Setup) — new handler**:
- Add `step5Request` struct: `PolicyName string` (`json:"policy_name"`), `DSLSource string` (`json:"dsl_source"`)
- `handleStep5` (Policy): if PolicyService is nil or fields empty, store step data and succeed without side effect (skip-friendly per STEPS array `mandatory: false`)
- If PolicyService is wired and fields non-empty, call policy creation

**Blocked by**: Task 1 (admin creation moved to tenant create)
**AC coverage**: AC-8, AC-9, AC-10

---

### Task 7: Router — move operator test to tenant_admin group

**Files**: `internal/gateway/router.go`

- Move lines 342-343 (`POST /operators/{id}/test` and `POST /operators/{id}/test/{protocol}`) from the `super_admin` group (router.go:337-346) into a new `tenant_admin` group
- Create a new `r.Group` block with `RequireRole("tenant_admin")` containing only the two test endpoints
- Keep `POST /operators` (Create), `PATCH /operators/{id}` (Update), `POST /operator-grants`, `DELETE /operator-grants/{id}` in super_admin group

**Blocked by**: nothing
**AC coverage**: AC-11, AC-12

---

### Task 8: Integration tests

**Files**: `internal/api/tenant/handler_test.go`, `internal/api/user/handler_test.go`, `internal/api/onboarding/handler_test.go`

- **Tenant create with admin**: POST /tenants with admin fields → assert 201, DB has 1 tenant + 1 user row, user role=tenant_admin, user tenant_id matches
- **Duplicate domain**: POST /tenants with existing domain → assert 409
- **Cross-tenant user create**: super_admin POST /users with `tenant_id` in body → assert user created in target tenant
- **tenant_admin POST /users with tenant_id**: assert tenant_id is silently ignored
- **Wizard step flow**: step 1 (tenant profile) → step 2 (operators) → step 3 (APN) → step 4 (SIM) → step 5 (policy) → complete → all succeed as tenant_admin
- **Operator test as tenant_admin**: POST /operators/{id}/test → assert 200 (not 403)

**Blocked by**: Tasks 1-7
**AC coverage**: AC-1 through AC-13

## Dependency Graph

```
Task 1 (Store: CreateTenantWithAdmin)
  └─→ Task 2 (Tenant handler: admin fields)
       └─→ Task 4 (Tenant modal UI)
  └─→ Task 6 (Onboarding: step realignment)

Task 3 (Slug cleanup) ─→ Task 4 (Tenant modal UI)

Task 5 (User cross-tenant) — independent

Task 7 (Router: operator test role) — independent

Task 8 (Tests) ← all tasks
```

## Files Affected Summary

| File | Changes |
|------|---------|
| `internal/store/tenant.go` | +CreateTenantWithAdmin method |
| `internal/api/tenant/handler.go` | Expand create request, call new store method, dual audit |
| `internal/api/user/handler.go` | Add TenantID to createUserRequest, super_admin override |
| `internal/store/user.go` | +CreateUserInTenant method |
| `internal/api/onboarding/handler.go` | Delete handleStep2 (admin), renumber steps, add Policy step |
| `internal/gateway/router.go` | Move test endpoints to tenant_admin group |
| `web/src/pages/system/tenants.tsx` | Add missing form fields, remove slug from submit |
| `web/src/types/settings.ts` | Possible minor type adjustments |
| `web/src/hooks/use-settings.ts` | Update mutation payload shape |
| `internal/api/tenant/handler_test.go` | Add auto-admin test cases |
| `internal/api/user/handler_test.go` | Add cross-tenant test cases |
| `internal/api/onboarding/handler_test.go` | Update step numbering in tests |

**Total files affected**: 12

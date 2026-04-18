# UAT Acceptance Report — Batch 1 (UAT-001 .. UAT-003)

> Date: 2026-04-18
> Scope: UAT-001 (Tenant Onboarding), UAT-002 (SIM Bulk Import), UAT-003 (SIM Lifecycle)
> Method: 3-layer consistency (UI + API + DB)
> Tester: Functional Acceptance Tester (Claude Opus 4.7)
> Fresh deploy: post Runtime Alignment 2026-04-18
> Evidence dir: /tmp/uat-batch1/

## Summary

| UAT | Steps PASS | Verifies PASS | UI | API | DB | Overall |
|-----|------------|---------------|----|----|----|---------|
| UAT-001 | 2/10 | 3/8 | FAIL | PARTIAL | PARTIAL | **FAIL** |
| UAT-002 | 8/10 | 2/6 | PASS | PARTIAL | FAIL | **FAIL** |
| UAT-003 | 7/11 | 3/7 | N/A (API) | PARTIAL | PARTIAL | **FAIL** |

**Release readiness: BLOCKED.** 3 CRITICAL, 12 HIGH, 3 MEDIUM, 2 UAT_DRIFT findings. Onboarding flow is fundamentally unusable; bulk import allocates state but not audit/IP/policy/notification; SIM activation blocked by empty IP pool seed.

## Per-UAT Detail

### UAT-001: Tenant Onboarding → First Dashboard

Super Admin creates tenant → Tenant Admin login → 5-step wizard → dashboard.

#### Steps (3-layer table)

| # | Actor / Action | UI | API | DB | Consistency | Severity |
|---|----------------|----|----|----|-------------|----------|
| 1 | SA creates tenant via `/system/tenants` modal | FAIL — modal missing `contact_email`, `domain`, `contact_phone`, `max_apns` | 422 VALIDATION_ERROR `contact_email required` on submit; works via direct API | row inserted with full payload via API | **DIVERGE UI↔API** | **HIGH (F-2)** |
| 1.5 | SA-created tenant visible in list | PASS (3 existing tenants shown) | `/api/v1/tenants` returns `sim_count=0`, `user_count=0` for ALL tenants | DB: Nar=80 SIMs/6 users, Bosphorus=27/5, Demo=55/4 | **DIVERGE API↔DB** | **CRITICAL (F-1)** |
| 2 | System sends invite email | SKIP (infra) | no trigger observed | no email service configured | N/A | — |
| 3 | TA login with generated creds | PASS (existing tenant_admin `ahmet.yilmaz@nar.com.tr`/`password123`) | 200 token issued | users row ok | AGREE | — |
| 3.5 | Auto-created admin user | FAIL — `POST /tenants` does NOT create admin user | after tenant create 0 users | 0 users in tenant | AGREE (all 3 wrong) | **HIGH (F-3 related)** |
| 4 | Wizard Step 2 select operators MockTurkcell/MockVodafone | FAIL — operators are named `Turkcell`, `Vodafone TR`, `Turk Telekom`, `Mock Simulator` (no "Mock" prefix) | `/api/v1/operators` returns `Turkcell` etc. | same | AGREE but UAT drift | **UAT_DRIFT (D-1)** |
| 4.5 | Tenant Admin clicks Test for Vodafone | FAIL — "This action requires super_admin role or higher" | `POST /operators/{id}/test` returns 403 INSUFFICIENT_ROLE | user role `tenant_admin` (correct) | AGREE | **HIGH (F-5) — wizard unusable** |
| 4.6 | As Super Admin, Test Turkcell | FAIL — "Connection failed: Failed to create adapter" | `POST /test` → 500 INTERNAL_ERROR (`Failed to create adapter`); `POST /test/mock` → 200 success | operator has `enabled_protocols=[diameter,radius,mock]`; default protocol selector is broken | **DIVERGE UI success vs API 500** | **HIGH (F-6)** |
| 4.7 | As SA, Test Mock Simulator | FAIL — "Operator has no enabled protocol" | `POST /test` → 400 | `adapter_config={"host":"localhost","port":1812}` — flat legacy format, not nested STORY-090 schema | AGREE UI↔API; seed-data stale | **HIGH (F-8) — seed not Runtime-Alignment-compliant** |
| 5-9 | Advance from Operator step to APN/SIM/Policy step | FAIL — `POST /onboarding/{id}/step/2` → 422 VALIDATION_ERROR: missing `admin_email`, `admin_name`, `admin_password` (min 8 chars) | backend expects admin account fields | UI wizard never collects these | **DIVERGE UI↔API — wizard fatally broken** | **CRITICAL (F-9)** |
| 10 | Dashboard shows counts (UAT requires "100 SIMs, 1 APN, 2 operators" top-row widgets) | **PARTIAL** — SIM count widget present (80); APN count widget / Operator count widget NOT present in dashboard top row (only `top_apns` list and `operator_health` table exist) | `/api/v1/dashboard` returns `total_sims, active_sessions, monthly_cost, ip_pool_usage_pct, sim_by_state, operator_health[], top_apns[]` — no scalar APN or operator count fields | DB has 6 APNs / 3 operator_grants for Nar | **UI↔UAT.md mismatch — MEDIUM (F-20)** |

#### Verify Checks

| # | Check | UI | API | DB | Result |
|---|-------|----|----|----|--------|
| 1 | Tenant record with correct resource limits | N/A | ok | `max_sims=10000, max_apns=10, max_users=25` correct | PASS |
| 2 | Tenant Admin user has `tenant_admin` role | — | ok | F-4: `POST /users` ignores `tenant_id` in body, defaults to caller's tenant; had to fix via SQL | **FAIL** |
| 3 | Operator grants for selected operators | N/A (wizard blocked) | n/a | no grants for UAT tenant | FAIL (blocked by wizard bug) |
| 4 | APN ACTIVE and scoped to tenant | N/A | n/a | no APN created | FAIL (blocked) |
| 5 | 100 SIMs ACTIVE with IP and policy | N/A | n/a | no SIMs | FAIL (blocked) |
| 6 | Dashboard widget counts match | PASS for existing | dash=80, DB=80 | 80 | PASS (for existing tenant Nar) |
| 7 | Audit log has tenant_created, user_created, operator_grant_created, apn_created, sim_bulk_import | — | n/a | `tenant.create` + `user.create` present; no `operator_grant`, no `apn.create`, no `sim_bulk_import` (since wizard blocked AND bulk import doesn't audit) | **FAIL (F-11)** |
| 8 | All data scoped by tenant_id | — | — | enforced in store layer (existing data ok) | PASS |
| Extra | Audit log hash chain integrity | — | `/api/v1/audit-logs/verify` → `verified:false, first_invalid:502` | 256 entries | **FAIL (F-10) — CRITICAL tamper-chain broken** |

---

### UAT-002: SIM Bulk Import → Dashboard Reflection

CSV upload 500 SIMs (5 dupe ICCIDs) → job progress → notification → error report → SIM list → dashboard delta → audit log.

Executed as `ahmet.yilmaz@nar.com.tr` (Nar Teknoloji tenant_admin).

#### Steps

| # | Action | UI | API | DB | Consistency | Severity |
|---|--------|----|----|----|-------------|----------|
| 1 | Click Import SIMs, upload 500-row CSV | PASS modal opens & accepts file | `POST /api/v1/sims/bulk/import` → 202 `{job_id}` | — | AGREE | — |
| 2 | Navigate to /jobs — job visible | SKIP (verified via API only) | `GET /api/v1/jobs/{id}` returns completed state | jobs row inserted | AGREE (UI not inspected) | — |
| 3 | Job runner creates SIMs | SKIP | — | 495 new SIMs all `state=active`, `ip_address_id=NULL`, `policy_version_id=NULL` | — | **HIGH (F-13, F-14)** |
| 4 | Progress updates | SKIP | `progress_pct=100` immediately (sub-1s job) | — | AGREE (job fast) | — |
| 5 | Notification sent | SKIP | no notification created | `SELECT * FROM notifications WHERE tenant_id=...AND created_at>NOW()-10m` → 0 rows | **DIVERGE expected↔reality** | **HIGH (F-12)** |
| 6 | Check notification bell | SKIP (bell not opened) | `unread-count` = 3 (pre-existing) | no row | API↔DB AGREE (both null) | HIGH (F-12) |
| 7 | Download error report | SKIP | `/api/v1/jobs/{id}/errors` → 5 rows with `ICCID X already exists` | `error_report` column populated | AGREE | PASS |
| 8 | SIM list shows 495 new SIMs | PASS (list count increases visibly post-import) | `/api/v1/sims` paginated | 575 (80 previous + 495 new) | AGREE | PASS |
| 9 | Dashboard SIM count incremented by 495 | SKIP (API only) | `/api/v1/dashboard total_sims=575` | 575 | API↔DB AGREE | PASS |
| 10 | Audit log shows 495 sim_created + 495 sim_activated | SKIP | audit list | `SELECT COUNT(*) FROM audit_logs WHERE action LIKE '%sim%' AND created_at>NOW()-10m` → only 1 `sim.bulk_import` row; 990 `sim_state_history` rows | **DIVERGE — state_history captured, audit_logs not** | **HIGH (F-11)** |

#### Verify Checks

| # | Check | Result | Evidence |
|---|-------|--------|----------|
| 1 | 495 SIMs ACTIVE, each with IP + policy | **FAIL** | 495 active but `ip_address_id IS NULL` AND `policy_version_id IS NULL` for ALL (F-13, F-14) |
| 2 | 5 failed rows with error reasons | PASS | error_report JSON ok |
| 3 | IP pool utilization updated | **FAIL** | Pools unchanged (no IPs actually allocated) |
| 4 | Dashboard SIM count reflects total | PASS | 575=575 |
| 5 | Job status completed, progress 100% | PASS | state=completed, progress_pct=100 |
| 6 | Audit log hash chain integrity across 990+ entries | **FAIL** | 0 audit entries created for import (F-11) + hash chain already broken (F-10) |

---

### UAT-003: SIM Full Lifecycle (State Machine)

ORDERED → ACTIVE → SUSPENDED → ACTIVE → TERMINATED → PURGED, tested via API (UI `Create SIM` not exposed for single-SIM create — only bulk).

#### Steps

| # | Action | API | DB | Severity |
|---|--------|-----|----|----|
| 1 | Create SIM via `POST /sims` | 200 `state=ordered` (requires `sim_type` — undocumented) | sims row inserted | PASS |
| 2 | Activate | **FAIL** `422 POOL_EXHAUSTED — No IP addresses available` | SIM stays `ordered`, no IP | **CRITICAL (F-15)** |
| 3 | RADIUS session (happy path only) | SKIP (SIM not active) | — | — |
| 4 | Suspend (from ordered — should fail) | 422 `Cannot suspend SIM in 'ordered' state` ✓ | — | PASS (negative test) |
| 5 | Check Live Sessions | SKIP | — | — |
| 6 | Resume (from ordered) | 200 `state=active`, **IP still NULL, policy still NULL** | no IP, no policy assigned | **HIGH (F-17) — Resume bypasses Activate's IP/policy allocation** |
| 7 | Report Stolen/Lost | 200 `state=stolen_lost` | `sim_state_history` entry | PASS |
| 8 | Terminate | 200 `state=terminated, terminated_at, purge_at set` | ok | PASS |
| 9 | Auto-purge | SKIP (time-based) | — | — |
| 10 | State history | — | 3 rows: ordered→active, active→stolen_lost, stolen_lost→terminated | PASS |
| 11 | Audit log | — | 4 entries: sim.create, sim.resume, sim.report_lost, sim.terminate | PASS |

Invalid-transition tests: `POST /suspend` on ORDERED → 422 ✓; `POST /terminate` on ORDERED → 422 ✓.

#### Verify Checks

| # | Check | Result |
|---|-------|--------|
| 1 | Each state transition in `sim_state_history` | PASS |
| 2 | IP allocated on ACTIVE, retained on SUSPEND, grace on TERMINATE, reclaimed on PURGE | **FAIL** — no IP ever allocated due to F-15 |
| 3 | CoA/DM sent on SUSPEND/STOLEN/TERMINATE | UNVERIFIED (no real session existed) |
| 4 | Policy cleared on TERMINATE | UNVERIFIED (no policy ever assigned) |
| 5 | After PURGE: IMSI/MSISDN hashed | UNVERIFIED |
| 6 | Audit log hash chain valid | **FAIL (F-10)** `verified:false first_invalid=502` |
| 7 | Invalid transitions rejected | PASS |

---

## Findings Summary

### Critical (block release)

| # | UAT | Step | Description | Layers diverge | Root cause guess | Route |
|---|-----|------|-------------|----------------|------------------|-------|
| F-1 | UAT-001 | verify | Tenant list API returns `sim_count=0`, `user_count=0` for all tenants | API=0,0 vs DB=80/6, 27/5, 55/4 | N+1 aggregation or broken JOIN in `TenantHandler.List` | FIX_STORY |
| F-9 | UAT-001 | 4→5 | Onboarding wizard blocked — wizard submits step 2 without `admin_email/admin_name/admin_password` | UI omits fields backend requires | Wizard UI component schema drift from `/onboarding/step/2` contract | FIX_STORY |
| F-10 | UAT-001 | verify | Audit log hash chain `verified:false` at row id=502 | `audit_logs` id=500 has sim.bulk_import (chain tail `917c89…`), but id=501 `operator.update` has `prev_hash='0000000000…'` — chain RESET instead of continuing from id=500 | Seed/migration breaks chain when switching partition or module boundary; `hash_chain` integrity not enforced on insert | FIX_STORY |
| F-15 | UAT-003 | 2 | SIM Activate → POOL_EXHAUSTED at first-run — no ip_addresses seeded | `ip_pools` rows exist (70000000-...); `ip_addresses` table is empty for Nar tenant | Seed creates pool rows but does not pre-generate address inventory | FIX_STORY |

### High

| # | UAT | Step | Description | Layers diverge | Route |
|---|-----|------|-------------|----------------|-------|
| F-2 | UAT-001 | 1 | Create Tenant modal missing required fields (`contact_email`, `domain`, `contact_phone`, `max_apns`) | UI<API | FIX_STORY |
| F-3 | UAT-001 | 1 | POST /tenants does NOT auto-create admin user despite UAT expectation; also UI/API return `slug` which is not a DB column | UI/API<DB | FIX_STORY |
| F-4 | UAT-001 | 3 | `POST /users` ignores `tenant_id` in body — super_admin cannot provision users in other tenants | API silently assigns caller's tenant | FIX_STORY |
| F-5 | UAT-001 | 4 | Wizard calls `/operators/{id}/test` which requires super_admin — wizard unusable by tenant_admin | UI role mismatch | FIX_STORY |
| F-6 | UAT-001 | 4 | `POST /operators/{id}/test` on multi-protocol operator returns 500 "Failed to create adapter" | confirmed in `internal/api/operator/handler.go:1151` — `DerivePrimaryProtocol` picks an enabled protocol (likely diameter/radius), then `testConnectionForProtocol` fails during adapter construction (returns non-nil error on line 1183). Adapter registry missing constructor for the derived primary. `POST /test/mock` works → mock adapter constructor is present, others are not | FIX_STORY |
| F-7 | UAT-001 | 4 | Mock Simulator operator has no enabled protocol → Test fails | seed issue | FIX_STORY |
| F-8 | UAT-001 | 4 | Mock Simulator adapter_config in flat legacy format (not nested per STORY-090) | seed data not updated during Runtime Alignment | FIX_STORY |
| F-11 | UAT-002 | 10 | Bulk SIM import creates 495 SIMs + 990 `sim_state_history` entries (ordered→active for each = 2 rows/SIM oddly) but writes only ONE `sim.bulk_import` summary audit row — ZERO per-SIM audit events | `sim_state_history` pipeline works; `audit_logs` per-SIM pipeline missing in bulk path; also double state_history (990 for 495 SIMs) suggests ordered→ordered redundant history row | FIX_STORY |
| F-12 | UAT-002 | 5/6 | Bulk import completion does NOT create a notification (no row in `notifications`) | notification pipeline missing for bulk_import | FIX_STORY |
| F-13 | UAT-002 | 3 | All 495 imported SIMs activated with `ip_address_id=NULL` | IP allocation skipped in bulk path | FIX_STORY |
| F-14 | UAT-002 | 3 | All 495 imported SIMs have `policy_version_id=NULL` despite APN having `default_policy_id` | policy auto-assign missing in bulk path | FIX_STORY |
| F-17 | UAT-003 | 6 | `POST /sims/{id}/resume` accepts ordered→active transition but bypasses IP & policy assignment that Activate performs | state machine allows ordered→active via Resume; Resume code path skips allocation | FIX_STORY |

### Medium

| # | UAT | Step | Description | Route |
|---|-----|------|-------------|-------|
| F-18 | UAT-003 | 10 | `sim_state_history` records "ordered→active" for both Activate and Resume calls with identical shape — lost attribution of WHICH endpoint caused transition | INLINE_FIX in handler: pass distinct reason label |
| F-19 | UAT-003 | 1 | `POST /sims` requires `sim_type` but field is not documented in UAT / API doc | INLINE_FIX in docs |
| F-20 | UAT-001 | 10 | Dashboard lacks explicit APN-count and Operator-count widgets (only lists/tables); UAT.md specifies "1 APN, 2 operators" as discrete widget values. Either the dashboard is missing widgets, or UAT is out of date — either way the 3-layer check cannot be satisfied at UI level. | FIX_STORY (add widgets) OR UAT_DRIFT |

### UAT Drift (Runtime Alignment — update UAT.md)

| # | UAT | Step | Current UAT says | Reality | Suggested UAT edit |
|---|-----|------|------------------|---------|--------------------|
| D-1 | UAT-001 | 4 | "select MockTurkcell, MockVodafone" | operators are `Turkcell`, `Vodafone TR`, `Turk Telekom`, `Mock Simulator` (no "Mock" prefix on real operators; one dedicated "Mock Simulator" operator) | Change to `select Turkcell, Vodafone TR` |
| D-2 | UAT-002 | 3 | "allocate IP from pool" | Runtime Alignment STORY-092 introduced dynamic IP allocation pipeline but pool pre-population missing from seed; expected allocation does not happen for bulk import | Either (a) clarify UAT "IP allocated at first auth" or (b) require seed to pre-populate `ip_addresses` inventory |

---

## Routing Classification

### INLINE_FIX (≤5 lines, single file, no logic)
- F-18: `internal/store/sim.go` Resume — pass `"resume"` (vs `"activate"`) as the actor-reason in `insertStateHistory` to distinguish which endpoint moved the state.
- F-19: `docs/architecture/api/API-XXX.md` (or OpenAPI) — document required `sim_type` field on `POST /sims`.

### FIX_STORY candidates (proposed)
- FIX-101-tenant-list-counts: fix `TenantHandler.List` to populate `sim_count`/`user_count` correctly (F-1).
- FIX-102-audit-hashchain-repair: repair audit hash chain validator / regenerate seed chain (F-10).
- FIX-103-ip-pool-prepopulation: seed `ip_addresses` inventory for each created `ip_pool`; required for activation path (F-15).
- FIX-104-onboarding-wizard-fields: add admin account fields to wizard Step 1 UI and wire to backend (F-9). Also fix role check: wizard operator-test should succeed for tenant_admin within their grant scope (F-5).
- FIX-105-operator-test-default-protocol: `POST /operators/{id}/test` should pick first enabled protocol or return 400 with actionable list (F-6).
- FIX-106-mock-operator-adapter-schema: update Mock Simulator seed to nested adapter_config structure (F-7, F-8).
- FIX-107-create-tenant-modal-complete: add `contact_email`, `domain`, `contact_phone`, `max_apns` inputs to modal (F-2).
- FIX-108-tenant-create-bootstrap: auto-create tenant_admin user on tenant create, OR create dedicated onboarding endpoint that receives admin creds (F-3).
- FIX-109-user-create-cross-tenant: `POST /users` must honor `tenant_id` in body when caller is super_admin (F-4).
- FIX-110-bulk-import-audit: emit `sim.create` + `sim.activate` audit events per imported row (F-11).
- FIX-111-bulk-import-notifications: emit completion notification via SVC-08 when job finishes (F-12).
- FIX-112-bulk-import-ip-policy: bulk import path must run the same IP allocation & policy auto-match as the Activate handler (F-13, F-14).
- FIX-113-resume-state-machine: disallow `ordered→active` via Resume (return 422) OR make Resume perform allocation identically to Activate (F-17).

### UAT_DRIFT — update docs/UAT.md
- D-1: line 26 replace "MockTurkcell, MockVodafone" with "Turkcell, Vodafone TR".
- D-2: verify check line 73 qualify IP allocation timing against Runtime Alignment semantics.

---

## Evidence (all under /tmp/uat-batch1/)

- `uat001-00-login.png` — initial login
- `uat001-step1a..e-*.png` — tenant create modal + submission (missing fields)
- `uat001-step3-ahmet-login.png` — tenant_admin dashboard
- `uat001-step4a..i-*.png` — onboarding wizard attempts (blocked on step 2)
- `uat001-step5-apn.png` — wizard blocked state
- `uat002-step1..2-*.png` — import modal + submission
- `sims-500.csv` — the 500-row test CSV (last 5 dup ICCIDs)
- `job-poll.json` — complete job record with error_report and created_sim_ids
- `tenant-id.txt`, `sim-lifecycle-id.txt`, `*token.txt` — resource IDs

# STORY-078: [AUDIT-GAP] SIM Compare Endpoint & System Config Endpoint Backfill

## Source
- Gap Type: MISSING (2 endpoints)
- Doc References: API-053, API-182
- Audit Report: docs/reports/compliance-audit-report.md
- Added by: Compliance Auditor [2026-04-12]

## Description
Two documented backend endpoints were never implemented despite being listed in `docs/architecture/api/_index.md` and, in the case of API-053, having a fully built frontend consumer:

1. **API-053 `POST /api/v1/sims/compare`** — Documented owner: STORY-011 (DONE). The frontend page `web/src/pages/sims/compare.tsx` (~17 KB) exists and was shipped as part of Phase 10 frontend work, but no backend handler exists (grep `Compare|compare` in `internal/api/sim` returns zero results). The frontend is calling a non-existent endpoint.

2. **API-182 `GET /api/v1/system/config`** — Documented owner: STORY-001 (DONE). Declared in the api index for super_admin system-configuration introspection. No handler registered in `internal/gateway/router.go`. No frontend consumer yet, but the endpoint is part of the super_admin/system-config surface that SCR-120/SCR-121 (system health & tenant mgmt) should be able to lean on.

This story backfills both. They are small, isolated handlers with no cross-service impact and no new schema; they fit naturally in the Phase 10 Wave-5 cleanup window ahead of the Documentation Phase.

## Architecture Reference
- Services: SVC-01 (HTTP gateway), SVC-03 (Core CRUD — SIM handler), SVC-10 (Audit — sim compare emits audit event)
- APIs: API-053, API-182
- Packages: `internal/api/sim`, `internal/api/system` (new), `internal/gateway/router.go`, `internal/config`

## Screen Reference
- SCR-020 / SCR-021 (SIM List + Detail) — compare feature reachable via `/sims/compare` route
- SCR-120 (System Health) — future consumer of `/system/config`

## Acceptance Criteria

- [ ] AC-1: `POST /api/v1/sims/compare` implemented in `internal/api/sim/compare.go`. Request body: `{ "sim_id_a": "<uuid>", "sim_id_b": "<uuid>" }`. Auth: JWT (sim_manager+). Tenant-scoped: both SIMs must belong to the caller's tenant (else 403 `FORBIDDEN_CROSS_TENANT`).

- [ ] AC-2: Response body returns a side-by-side diff structure comparing SIM A and SIM B across: ICCID, IMSI, MSISDN, state, state_changed_at, assigned_operator, assigned_apn, policy_version_id, static_ip, eSIM profile summary, last-session summary, last-auth result, segment memberships, recent bulk-op participation. Field-level equality marker `{ value_a, value_b, equal: bool }`. Standard envelope `{ status: "success", data: { ... } }`.

- [ ] AC-3: Request validation: both IDs required, valid UUIDs, not equal. Error codes `VALIDATION_ERROR` (422), `SIM_NOT_FOUND` (404) when either SIM missing. All tenant-scoping checks audited.

- [ ] AC-4: Audit log entry emitted on successful compare (action=`sim.compare`, entity_type=`sim`, entity_id=`sim_id_a`, metadata containing `sim_id_b`). Hash chain preserved.

- [ ] AC-5: Unit tests in `internal/api/sim/compare_test.go`: happy path, same-id rejection, cross-tenant rejection, missing SIM, malformed UUID. ≥ 85% line coverage for the handler.

- [ ] AC-6: `GET /api/v1/system/config` implemented in `internal/api/system/config.go`. Auth: JWT (super_admin only — `RequireRole("super_admin")`). Returns redacted runtime config: feature flags, protocol enablement (RADIUS/Diameter/5G-SBA on/off), build info (version, git commit, build date), active config source (env/file), boot-time timestamp.

- [ ] AC-7: Secrets NEVER returned: ENCRYPTION_KEY, JWT_SECRET, DB_PASSWORD, SMTP_PASSWORD, TELEGRAM_TOKEN, TWILIO_AUTH_TOKEN, SMDP_API_KEY, and any `*_PASSWORD`/`*_SECRET`/`*_TOKEN`/`*_KEY` variable is either omitted or masked as `***` in the response payload. Unit test with explicit redaction assertion.

- [ ] AC-8: `GET /api/v1/system/config` has integration test covering: super_admin 200, tenant_admin 403, unauthenticated 401, redaction assertion.

- [ ] AC-9: Frontend `/sims/compare` page updated to consume API-053 response and render the diff. If the page already speculatively calls the endpoint, error handling (currently 404) should be swapped for real-data display.

- [ ] AC-10: `api/_index.md` API-053 and API-182 descriptions re-checked (no path mismatch like API-110). `ERROR_CODES.md` includes `FORBIDDEN_CROSS_TENANT` if not already present.

- [ ] AC-11: USERTEST.md entry added for both flows (sim compare + super_admin config view).

## Technical Notes
- Related DONE stories: STORY-011 (sim CRUD — owner of API-053 originally), STORY-001 (owner of API-182)
- Compare endpoint piggybacks on existing SIM store methods (`GetByID`, `GetHistory`, `GetLastSession`); no new store methods required.
- System config endpoint reads `internal/config` struct; add a `Redact()` method for safe marshaling.
- No DB migration required.

## Test Scenarios
- Integration: POST /sims/compare with valid pair of SIMs in same tenant → 200 with full diff payload; cross-tenant → 403; same id → 422.
- Integration: GET /system/config as super_admin → 200 with redacted payload (secrets literally absent from JSON); as tenant_admin → 403.
- Unit: config `Redact()` asserts every known-secret env var never appears in marshaled JSON (table-driven test with 12+ secret names).
- Frontend: `/sims/compare?a=...&b=...` renders the diff; 404 handling no longer triggered on valid pair.

## Dependencies
- Blocked by: — (STORY-011 DONE, STORY-001 DONE — both owners)
- Blocks: Documentation Phase (API/SCR docs should be final before D1 spec writing)

## Priority
MEDIUM — Frontend page already exists and is broken (404); super_admin config view is nice-to-have but lightweight.

## Effort
S (Small) — 2 handlers, ~200 LOC backend + minor frontend adjustment + tests. Single-day work.

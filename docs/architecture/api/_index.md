# API Index — Argus

> **Implementation note:** Full request/response specifications are in the story files (`docs/stories/phase-N/STORY-NNN.md`) under the "API Contract" section. For implementation, read both this index AND the relevant story. Error codes and envelope format are documented in [ERROR_CODES.md](../ERROR_CODES.md).

> Base URL: `/api/v1`
> Auth: JWT (portal), API Key (M2M), OAuth2 (3rd party)
> Response format: Standard envelope `{ status, data, meta?, error? }`
> Pagination: Cursor-based (default 50/page)

## Auth & Users (20 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-001 | POST | /api/v1/auth/login | User login (JWT + refresh token). Fully-authenticated response includes `expires_in` (seconds) and `refresh_expires_in` (seconds); refresh_token remains httpOnly `SameSite=Strict` cookie only. | None | See [STORY-003](../../stories/phase-1/STORY-003-auth-jwt.md) |
| API-002 | POST | /api/v1/auth/refresh | Refresh JWT token. Response body: `{token, expires_in, refresh_expires_in}`. Rate-limited: 60 req/min per session (keyed on SHA-256 of refresh cookie, in-handler Redis sliding window). FE interceptor: single-flight + BroadcastChannel cross-tab sync + pre-emptive scheduler (5 min before JWT `exp`). | Refresh Token (httpOnly cookie) | See [STORY-003](../../stories/phase-1/STORY-003-auth-jwt.md); extended by [FIX-205](../../stories/fix-ui-review/FIX-205-token-refresh-auto-retry.md) |
| API-003 | POST | /api/v1/auth/logout | Revoke session | JWT | See [STORY-003](../../stories/phase-1/STORY-003-auth-jwt.md) |
| API-004 | POST | /api/v1/auth/2fa/setup | Enable TOTP 2FA | JWT | See [STORY-003](../../stories/phase-1/STORY-003-auth-jwt.md) |
| API-005 | POST | /api/v1/auth/2fa/verify | Verify TOTP code | JWT | See [STORY-003](../../stories/phase-1/STORY-003-auth-jwt.md) |
| API-006 | GET | /api/v1/users | List users in tenant | JWT (tenant_admin+) | See [STORY-005](../../stories/phase-1/STORY-005-tenant-management.md) |
| API-007 | POST | /api/v1/users | Create user + invite | JWT (tenant_admin+) | See [STORY-005](../../stories/phase-1/STORY-005-tenant-management.md) |
| API-008 | PATCH | /api/v1/users/:id | Update user | JWT (tenant_admin+ or self) | See [STORY-005](../../stories/phase-1/STORY-005-tenant-management.md) |
| API-195 | DELETE | /api/v1/users/:id?gdpr=1 | GDPR right-to-erasure: nulls PII, sets state=purged, emits system audit event | JWT (super_admin) | See [STORY-067](../../stories/phase-10/STORY-067-cicd-ops.md) (scope addition) |
| API-186 | GET | /api/v1/auth/sessions | List active portal sessions for current user (cursor-paginated) | JWT (api_user+) | See [STORY-064](../../stories/phase-10/STORY-064-db-hardening.md) |
| API-267 | DELETE | /api/v1/auth/sessions/:id | Revoke a specific portal session for the current user (self-service) | JWT (api_user+) | See [STORY-068](../../stories/phase-10/STORY-068-enterprise-auth.md); doc entry added by audit 2026-04-17 |
| API-268 | GET | /api/v1/auth/2fa/backup-codes/remaining | Remaining (unused) TOTP backup code count + `totp_enabled` flag for current user | JWT | See [STORY-068](../../stories/phase-10/STORY-068-enterprise-auth.md) AC-4; doc entry added by audit 2026-04-17 |
| API-196 | POST | /api/v1/auth/password/change | Change password (current → new) with history/complexity checks; rotates session on success | JWT (partial allowed) | See [STORY-068](../../stories/phase-10/STORY-068-enterprise-auth.md) (AC-1, AC-2, AC-3) |
| API-197 | POST | /api/v1/auth/2fa/backup-codes | Generate/regenerate 10 TOTP backup codes (plaintext returned once, bcrypt-stored) | JWT | See [STORY-068](../../stories/phase-10/STORY-068-enterprise-auth.md) (AC-4) |
| API-198 | POST | /api/v1/users/:id/unlock | Admin unlock locked account; clears failed_login_count + locked_until | JWT (tenant_admin+) | See [STORY-068](../../stories/phase-10/STORY-068-enterprise-auth.md) (AC-10) |
| API-199 | POST | /api/v1/users/:id/revoke-sessions | Revoke all sessions for user (self or tenant_admin); optional include_api_keys, WS drop | JWT (self or tenant_admin+) | See [STORY-068](../../stories/phase-10/STORY-068-enterprise-auth.md) (AC-6) |
| API-200 | POST | /api/v1/users/:id/reset-password | Admin reset: issue temp password (returned once), set force-change flag | JWT (tenant_admin+) | See [STORY-068](../../stories/phase-10/STORY-068-enterprise-auth.md) (AC-3) |
| API-257 | GET | /api/v1/users/:id | Get user detail (profile + totp_enabled + state + last_login + locked_until); cross-tenant 404 on mismatch | JWT (tenant_admin+) | See [STORY-075](../../stories/phase-10/STORY-075-cross-entity-context.md) |
| API-258 | GET | /api/v1/users/:id/activity | Get user audit-log activity (cursor-paginated, filtered to actor_id); cross-tenant 404 | JWT (tenant_admin+) | See [STORY-075](../../stories/phase-10/STORY-075-cross-entity-context.md) |
| API-264 | POST | /api/v1/auth/switch-tenant | Super_admin-only: activate a tenant context so all subsequent tenant-scoped endpoints operate as if scoped to `{tenant_id}`. Returns new JWT with `active_tenant` claim. Target tenant must have `state='active'` (403 CodeTenantSuspended otherwise). Emits `tenant.context_switched` audit entry under target tenant. | JWT (super_admin) | Post-Phase-10 on-prem UX; lives in `internal/api/admin/switch_tenant.go`. Home tenant remains in `tenant_id` claim for audit. |
| API-265 | POST | /api/v1/auth/exit-tenant-context | Super_admin-only: clear active tenant context and return to System View. Idempotent. Returns new JWT with `active_tenant` cleared. Emits `tenant.context_exited` audit entry under the previously-active tenant (if any). | JWT (super_admin) | Post-Phase-10 on-prem UX; lives in `internal/api/admin/switch_tenant.go`. |
| API-317 | POST | /api/v1/auth/password-reset/request | Self-service password reset request. Body: `{email: string}`. Always returns HTTP 200 `{status:"success", data:{message:"If that email exists…"}}` regardless of whether the email is registered (enumeration defense). 429 `RATE_LIMITED` if more than `PASSWORD_RESET_RATE_LIMIT_PER_HOUR` requests in the rolling window. Audit: `auth.password_reset_requested`. FIX-228. | None (public) | `internal/api/auth/password_reset.go` |
| API-318 | POST | /api/v1/auth/password-reset/confirm | Confirm password reset with token + new password. Body: `{token: string, password: string}`. Returns 200 on success; 400 `PASSWORD_RESET_INVALID_TOKEN` on bad/expired/used token; 422 with existing policy codes (`PASSWORD_TOO_SHORT`, `PASSWORD_MISSING_CLASS`, `PASSWORD_REPEATING_CHARS`, `PASSWORD_REUSED`) on policy violation. Token is single-use (row deleted on success). Audit: `auth.password_reset_completed`. FIX-228. | None (public) | `internal/api/auth/password_reset.go` |

## Tenants (5 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-010 | GET | /api/v1/tenants | List all tenants | JWT (super_admin) | See [STORY-005](../../stories/phase-1/STORY-005-tenant-management.md) |
| API-011 | POST | /api/v1/tenants | Create tenant | JWT (super_admin) | See [STORY-005](../../stories/phase-1/STORY-005-tenant-management.md) |
| API-012 | GET | /api/v1/tenants/:id | Get tenant detail | JWT (super_admin or own tenant) | See [STORY-005](../../stories/phase-1/STORY-005-tenant-management.md) |
| API-013 | PATCH | /api/v1/tenants/:id | Update tenant | JWT (super_admin or tenant_admin) | See [STORY-005](../../stories/phase-1/STORY-005-tenant-management.md) |
| API-014 | GET | /api/v1/tenants/:id/stats | Tenant statistics | JWT (tenant_admin+) | See [STORY-005](../../stories/phase-1/STORY-005-tenant-management.md) |

## Operators (10 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-020 | GET | /api/v1/operators | List operators | JWT (super_admin) | See [STORY-009](../../stories/phase-2/STORY-009-operator-crud.md) |
| API-021 | POST | /api/v1/operators | Create operator | JWT (super_admin) | See [STORY-009](../../stories/phase-2/STORY-009-operator-crud.md) |
| API-306 | GET | /api/v1/operators/:id | Get operator detail — returns full nested `adapter_config` (secrets masked to `"****"`) + per-protocol `enabled_protocols` array | JWT (super_admin) | Added STORY-090 Gate F-A2 (VAL-029); list endpoint (API-020) omits `adapter_config` for payload-slim reasons |
| API-022 | PATCH | /api/v1/operators/:id | Update operator config | JWT (super_admin) | See [STORY-009](../../stories/phase-2/STORY-009-operator-crud.md) |
| API-023 | GET | /api/v1/operators/:id/health | Get operator health status | JWT (operator_manager+) | See [STORY-009](../../stories/phase-2/STORY-009-operator-crud.md), [STORY-021](../../stories/phase-3/STORY-021-operator-failover.md) |
| API-024 | POST | /api/v1/operators/:id/test | Test operator connection (legacy — fires first enabled protocol adapter) — returns `PROTOCOL_NOT_CONFIGURED` (422) when zero protocols enabled. See per-protocol variant API-307. | JWT (super_admin) | See [STORY-018](../../stories/phase-3/STORY-018-operator-adapter.md) |
| API-307 | POST | /api/v1/operators/:id/test/:protocol | Per-protocol connection test — `protocol` ∈ {radius, diameter, sba, http, mock}; returns `PROTOCOL_NOT_CONFIGURED` (422) when the requested protocol is not enabled | JWT (super_admin) | Added STORY-090 Wave 3 Task 7a |
| API-025 | GET | /api/v1/operator-grants | List tenant operator grants | JWT (tenant_admin+) | See [STORY-009](../../stories/phase-2/STORY-009-operator-crud.md) |
| API-026 | POST | /api/v1/operator-grants | Grant operator access to tenant | JWT (super_admin) | See [STORY-009](../../stories/phase-2/STORY-009-operator-crud.md) |
| API-027 | DELETE | /api/v1/operator-grants/:id | Revoke operator grant | JWT (super_admin) | See [STORY-009](../../stories/phase-2/STORY-009-operator-crud.md) |

## APNs (7 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-030 | GET | /api/v1/apns | List APNs | JWT (sim_manager+) | See [STORY-010](../../stories/phase-2/STORY-010-apn-crud.md) |
| API-031 | POST | /api/v1/apns | Create APN | JWT (tenant_admin+) | See [STORY-010](../../stories/phase-2/STORY-010-apn-crud.md) |
| API-032 | GET | /api/v1/apns/:id | Get APN detail + stats | JWT (sim_manager+) | See [STORY-010](../../stories/phase-2/STORY-010-apn-crud.md) |
| API-033 | PATCH | /api/v1/apns/:id | Update APN | JWT (tenant_admin+) | See [STORY-010](../../stories/phase-2/STORY-010-apn-crud.md) |
| API-034 | DELETE | /api/v1/apns/:id | Archive APN (soft-delete) | JWT (tenant_admin) | See [STORY-010](../../stories/phase-2/STORY-010-apn-crud.md) |
| API-035 | GET | /api/v1/apns/:id/sims | List SIMs on this APN | JWT (sim_manager+) | See [STORY-057](../../stories/phase-10/STORY-057-data-accuracy-endpoints.md) |
| API-270 | GET | /api/v1/apns/:id/referencing-policies | Policies whose DSL references this APN by name (ILIKE match on policy_versions.dsl_compiled, cursor-paginated) | JWT (policy_editor+) | See [STORY-077](../../stories/phase-10/STORY-077-enterprise-ux-polish.md) D-007; doc entry added by audit 2026-04-17 |

## SIMs (15 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-040 | GET | /api/v1/sims | List/search SIMs (cursor paged). DTO includes enriched fields: `operator_name`, `operator_code`, `apn_name`, `policy_name`, `policy_version_number` (via single LEFT JOIN; null for orphan rows). FIX-233: added filter params `policy_version_id`, `policy_id`, `rollout_id`, `rollout_stage_pct` (all optional UUID/int, invalid UUID → 400 `INVALID_PARAM`); DTO extended with `policy_id`, `rollout_id?`, `rollout_stage_pct?`, `coa_status?`. | JWT (sim_manager+) | See [STORY-011](../../stories/phase-2/STORY-011-sim-crud.md); enrichment added [FIX-202](../../stories/fix-ui-review/FIX-202-sim-dto-name-resolution.md); filter+DTO extended [FIX-233](../../stories/fix-ui-review/FIX-233-sim-list-policy-cohort-filter.md) |
| API-041 | GET | /api/v1/sims/:id | Get SIM detail (full). Same enriched DTO fields as API-040 including FIX-233 additions (`policy_id`, `rollout_id?`, `rollout_stage_pct?`, `coa_status?`). | JWT (sim_manager+) | See [STORY-011](../../stories/phase-2/STORY-011-sim-crud.md); enrichment added [FIX-202](../../stories/fix-ui-review/FIX-202-sim-dto-name-resolution.md); DTO extended [FIX-233](../../stories/fix-ui-review/FIX-233-sim-list-policy-cohort-filter.md) |
| API-041b | GET | /api/v1/sims?ids=uuid1,uuid2,… | Batch SIM lookup by comma-separated UUIDs (max 100). Returns `{iccid, imsi, msisdn?}` summary DTOs used for CDR Explorer MSISDN column resolution. Tenant-scoped. Added FIX-214. | JWT (sim_manager+) | See [FIX-214](../../stories/fix-ui-review/FIX-214-cdr-explorer-page.md) |
| API-042 | POST | /api/v1/sims | Create single SIM (required body: `iccid`, `imsi`, `msisdn`, `apn_id`, `sim_type` ∈ {`physical`,`esim`}). Returns 400 `INVALID_REFERENCE` if `operator_id` / `apn_id` / `ip_address_id` fails FK check (defensive; primary path is 404 `NOT_FOUND` from handler-layer `GetByID` validation — FIX-206). | JWT (sim_manager+) | See [STORY-011](../../stories/phase-2/STORY-011-sim-crud.md); FK error path added [FIX-206](../../stories/fix-ui-review/FIX-206-orphan-operator-cleanup.md) |
| API-043 | PATCH | /api/v1/sims/:id | Update SIM metadata | JWT (sim_manager+) | See [STORY-057](../../stories/phase-10/STORY-057-data-accuracy-endpoints.md) |
| API-044 | POST | /api/v1/sims/:id/activate | Activate SIM | JWT (sim_manager+) | See [STORY-011](../../stories/phase-2/STORY-011-sim-crud.md) |
| API-045 | POST | /api/v1/sims/:id/suspend | Suspend SIM | JWT (sim_manager+) | See [STORY-011](../../stories/phase-2/STORY-011-sim-crud.md) |
| API-046 | POST | /api/v1/sims/:id/resume | Resume suspended SIM | JWT (sim_manager+) | See [STORY-011](../../stories/phase-2/STORY-011-sim-crud.md) |
| API-047 | POST | /api/v1/sims/:id/terminate | Terminate SIM | JWT (tenant_admin) | See [STORY-011](../../stories/phase-2/STORY-011-sim-crud.md) |
| API-048 | POST | /api/v1/sims/:id/report-lost | Report SIM stolen/lost | JWT (sim_manager+) | See [STORY-011](../../stories/phase-2/STORY-011-sim-crud.md) |
| API-049 | POST | /api/v1/sims/:id/diagnose | Run connectivity diagnostics | JWT (sim_manager+) | See [STORY-037](../../stories/phase-6/STORY-037-connectivity-diagnostics.md) |
| API-050 | GET | /api/v1/sims/:id/history | Get SIM state history | JWT (sim_manager+) | See [STORY-011](../../stories/phase-2/STORY-011-sim-crud.md) |
| API-051 | GET | /api/v1/sims/:id/sessions | Get SIM session history | JWT (sim_manager+) | See [STORY-057](../../stories/phase-10/STORY-057-data-accuracy-endpoints.md) |
| API-052 | GET | /api/v1/sims/:id/usage | Get SIM usage analytics | JWT (analyst+) | See [STORY-057](../../stories/phase-10/STORY-057-data-accuracy-endpoints.md) |
| API-053 | POST | /api/v1/sims/compare | Compare 2 SIMs side-by-side | JWT (sim_manager+) | See [STORY-011](../../stories/phase-2/STORY-011-sim-crud.md) |
| API-269 | GET | /api/v1/sims/:id/ip-current | Current IP lease + pool metadata for a SIM's active session (nil when no active IP) | JWT (sim_manager+) | See [STORY-075](../../stories/phase-10/STORY-075-cross-entity-context.md); doc entry added by audit 2026-04-17 |

## SIM Segments & Bulk (10 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-060 | GET | /api/v1/sim-segments | List saved segments | JWT (sim_manager+) | See [STORY-012](../../stories/phase-2/STORY-012-sim-segments.md) |
| API-061 | POST | /api/v1/sim-segments | Create saved segment (filter) | JWT (sim_manager+) | See [STORY-012](../../stories/phase-2/STORY-012-sim-segments.md) |
| API-061b | GET | /api/v1/sim-segments/:id | Get segment detail | JWT (sim_manager+) | See [STORY-012](../../stories/phase-2/STORY-012-sim-segments.md) |
| API-061c | DELETE | /api/v1/sim-segments/:id | Delete segment | JWT (sim_manager+) | See [STORY-012](../../stories/phase-2/STORY-012-sim-segments.md) |
| API-062 | GET | /api/v1/sim-segments/:id/count | Count SIMs in segment | JWT (sim_manager+) | See [STORY-012](../../stories/phase-2/STORY-012-sim-segments.md) |
| API-062b | GET | /api/v1/sim-segments/:id/summary | State summary for segment | JWT (sim_manager+) | See [STORY-012](../../stories/phase-2/STORY-012-sim-segments.md) |
| API-063 | POST | /api/v1/sims/bulk/import | Bulk SIM import (CSV upload) | JWT (sim_manager+) | See [STORY-013](../../stories/phase-2/STORY-013-bulk-import.md) |
| API-064 | POST | /api/v1/sims/bulk/state-change | Bulk state change — dual-shape (sim_ids or segment_id). Async 202; rate-limited 1 req/s per tenant. | JWT (sim_manager+) | See [STORY-030](../../stories/phase-5/STORY-030-bulk-operations.md); full spec: [bulk-actions.md](bulk-actions.md) |
| API-065 | POST | /api/v1/sims/bulk/policy-assign | Bulk policy assign — dual-shape (sim_ids or segment_id). Triggers per-SIM CoA dispatch for active sessions. Job result includes CoA counters: `coa_sent_count`, `coa_acked_count`, `coa_failed_count` (omitted when 0). CoA dispatched outside distLock after release. | JWT (policy_editor+) | See [STORY-030](../../stories/phase-5/STORY-030-bulk-operations.md); CoA dispatch: [STORY-060](../../stories/phase-10/STORY-060-aaa-protocol-correctness.md); full spec: [bulk-actions.md](bulk-actions.md) |
| API-066 | POST | /api/v1/sims/bulk/operator-switch | Bulk eSIM operator switch — dual-shape (sim_ids or segment_id). Non-eSIM SIMs reported as NOT_ESIM in job error_report. | JWT (tenant_admin) | See [STORY-030](../../stories/phase-5/STORY-030-bulk-operations.md); full spec: [bulk-actions.md](bulk-actions.md) |

## eSIM (7 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-070 | GET | /api/v1/esim-profiles | List eSIM profiles (supports sim_id filter for multi-profile view). DTO includes enriched fields: `operator_name`, `operator_code` (via LEFT JOIN operators — FIX-202). | JWT (sim_manager+) | See [STORY-028](../../stories/phase-5/STORY-028-esim-profiles.md); enrichment added [FIX-202](../../stories/fix-ui-review/FIX-202-sim-dto-name-resolution.md) |
| API-071 | GET | /api/v1/esim-profiles/:id | Get eSIM profile detail. Same enriched `operator_name`, `operator_code` fields as API-070 (FIX-202). | JWT (sim_manager+) | See [STORY-028](../../stories/phase-5/STORY-028-esim-profiles.md); enrichment added [FIX-202](../../stories/fix-ui-review/FIX-202-sim-dto-name-resolution.md) |
| API-072 | POST | /api/v1/esim-profiles/:id/enable | Enable eSIM profile (accepts `available` or `disabled` state) | JWT (sim_manager+) | See [STORY-028](../../stories/phase-5/STORY-028-esim-profiles.md); state machine: [STORY-061](../../stories/phase-10/STORY-061-esim-model-evolution.md) |
| API-073 | POST | /api/v1/esim-profiles/:id/disable | Disable eSIM profile (operator-deactivation → `disabled` state) | JWT (sim_manager+) | See [STORY-028](../../stories/phase-5/STORY-028-esim-profiles.md) |
| API-074 | POST | /api/v1/esim-profiles/:id/switch | Switch to different operator profile. Dispatches DM (RFC 5176) for active sessions before switching. Old profile transitions to `available` (not `disabled`, per DEV-164). Releases IP for new APN; clears policy. Response includes `disconnected_sessions` count. `force=true` bypasses DM on NAK. Returns 409 `SESSION_DISCONNECT_FAILED` on NAK without force. | JWT (sim_manager+) | See [STORY-028](../../stories/phase-5/STORY-028-esim-profiles.md); DM dispatch: [STORY-060](../../stories/phase-10/STORY-060-aaa-protocol-correctness.md); switch evolution: [STORY-061](../../stories/phase-10/STORY-061-esim-model-evolution.md) |
| API-075 | POST | /api/v1/esim-profiles | Load (create) a new eSIM profile on a SIM. SIM must be eSIM type. Max 8 profiles (PROFILE_LIMIT_EXCEEDED 422). Calls SM-DP+ DownloadProfile. Profile created in `available` state. | JWT (sim_manager+) | See [STORY-061](../../stories/phase-10/STORY-061-esim-model-evolution.md) |
| API-076 | DELETE | /api/v1/esim-profiles/:id | Soft-delete an eSIM profile. Returns 409 CANNOT_DELETE_ENABLED_PROFILE if profile is in `enabled` state. Calls SM-DP+ DeleteProfile before soft-delete. | JWT (sim_manager+) | See [STORY-061](../../stories/phase-10/STORY-061-esim-model-evolution.md) |

## IP Pools (6 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-080 | GET | /api/v1/ip-pools | List IP pools | JWT (operator_manager+) | See [STORY-010](../../stories/phase-2/STORY-010-apn-crud.md) (IP pool section) |
| API-081 | POST | /api/v1/ip-pools | Create IP pool | JWT (tenant_admin+) | See [STORY-010](../../stories/phase-2/STORY-010-apn-crud.md) (IP pool section) |
| API-082 | GET | /api/v1/ip-pools/:id | Get pool detail + utilization | JWT (operator_manager+) | See [STORY-010](../../stories/phase-2/STORY-010-apn-crud.md) (IP pool section) |
| API-083 | PATCH | /api/v1/ip-pools/:id | Update pool settings | JWT (tenant_admin+) | See [STORY-010](../../stories/phase-2/STORY-010-apn-crud.md) (IP pool section) |
| API-084 | GET | /api/v1/ip-pools/:id/addresses | List addresses in pool. `?q=<≤64>` server-side search across address_v4/iccid/imsi/msisdn (FIX-223). DTO includes `sim_iccid`, `sim_imsi`, `sim_msisdn`, `last_seen_at` (omitempty). | JWT (operator_manager+) | See [STORY-010](../../stories/phase-2/STORY-010-apn-crud.md) (IP pool section) |
| API-085 | POST | /api/v1/ip-pools/:id/addresses/reserve | Reserve static IP for SIM | JWT (sim_manager+) | See [STORY-010](../../stories/phase-2/STORY-010-apn-crud.md) (IP pool section) |

## Policies (12 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-090 | GET | /api/v1/policies | List policies | JWT (policy_editor+) | See [STORY-023](../../stories/phase-4/STORY-023-policy-crud.md) |
| API-091 | POST | /api/v1/policies | Create policy | JWT (policy_editor+) | See [STORY-023](../../stories/phase-4/STORY-023-policy-crud.md) |
| API-092 | GET | /api/v1/policies/:id | Get policy with versions | JWT (policy_editor+) | See [STORY-023](../../stories/phase-4/STORY-023-policy-crud.md) |
| API-093 | POST | /api/v1/policies/:id/versions | Create new version (draft) | JWT (policy_editor+) | See [STORY-023](../../stories/phase-4/STORY-023-policy-crud.md) |
| API-094 | POST | /api/v1/policy-versions/:id/dry-run | Dry-run simulation | JWT (policy_editor+) | See [STORY-024](../../stories/phase-4/STORY-024-policy-dryrun.md) |
| API-095 | POST | /api/v1/policy-versions/:id/activate | Activate version (immediate) | JWT (policy_editor+) | See [STORY-023](../../stories/phase-4/STORY-023-policy-crud.md) |
| API-096 | POST | /api/v1/policy-versions/:id/rollout | Start staged rollout | JWT (policy_editor+) | See [STORY-025](../../stories/phase-4/STORY-025-policy-rollout.md) |
| API-097 | POST | /api/v1/policy-rollouts/:id/advance | Advance to next rollout stage | JWT (policy_editor+) | See [STORY-025](../../stories/phase-4/STORY-025-policy-rollout.md) |
| API-098 | POST | /api/v1/policy-rollouts/:id/rollback | Rollback rollout | JWT (policy_editor+) | See [STORY-025](../../stories/phase-4/STORY-025-policy-rollout.md) |
| API-098b | POST | /api/v1/policy-rollouts/{id}/abort | Abort an in-progress rollout (state→aborted; does NOT revert assignments) | JWT (policy_editor+) | FIX-232 |
| API-099 | GET | /api/v1/policy-rollouts/:id | Get rollout status. FIX-234: response now includes optional `coa_counts` object with 6-key breakdown: `{pending, queued, acked, failed, no_session, skipped}` (all int, omitempty). | JWT (policy_editor+) | See [STORY-025](../../stories/phase-4/STORY-025-policy-rollout.md); `coa_counts` added FIX-234 |
| API-099b | GET | /api/v1/policy-versions/{id1}/diff/{id2} | Diff two policy versions (unified text diff of DSL) | JWT (policy_editor+) | See [STORY-023](../../stories/phase-4/STORY-023-policy-crud.md) |
| API-326 | GET | /api/v1/policy-rollouts | List active rollouts (filtered by state CSV). Query params: `state` (CSV of `pending,in_progress,completed,aborted,rolled_back`; default `pending,in_progress`), `limit` (1..100, default 20). Returns `RolloutSummary[]` with `id`, `policy_id`, `policy_version_id`, `state`, `current_stage`, `started_at`, and optional `coa_counts` object (6-key breakdown per FIX-234, omitempty). 400 `INVALID_PARAM` on out-of-range limit. FIX-233/FIX-234. | JWT (policy_editor+) | `internal/api/policy/handler.go ListRollouts`; used by FE SIM list Cohort chip dropdown; `coa_counts` per-row added FIX-234 (N+1 D-144) |

## Sessions (5 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-100 | GET | /api/v1/sessions | List active sessions. DTO includes enriched fields: `operator_name`, `operator_code`, `policy_name`, `policy_version_number` (batch-enriched via `GetManyByIDsEnriched` — 1 query per page). | JWT (sim_manager+) | See [STORY-017](../../stories/phase-3/STORY-017-session-management.md); enrichment added [FIX-202](../../stories/fix-ui-review/FIX-202-sim-dto-name-resolution.md) |
| API-101 | GET | /api/v1/sessions/stats | Real-time session statistics | JWT (analyst+) | See [STORY-017](../../stories/phase-3/STORY-017-session-management.md), [STORY-033](../../stories/phase-6/STORY-033-realtime-metrics.md) |
| API-102 | POST | /api/v1/sessions/:id/disconnect | Force disconnect session (CoA/DM) | JWT (sim_manager+) | See [STORY-017](../../stories/phase-3/STORY-017-session-management.md) |
| API-103 | POST | /api/v1/sessions/bulk/disconnect | Bulk disconnect on segment | JWT (tenant_admin) | See [STORY-017](../../stories/phase-3/STORY-017-session-management.md), [STORY-030](../../stories/phase-5/STORY-030-bulk-operations.md) |
| API-256 | GET | /api/v1/sessions/:id | Get session detail with enriched SIM + operator + APN context; cross-tenant 404 on mismatch. Includes `operator_code`, `policy_name`, `policy_version_number` (FIX-202). | JWT (sim_manager+) | See [STORY-075](../../stories/phase-10/STORY-075-cross-entity-context.md); enrichment added [FIX-202](../../stories/fix-ui-review/FIX-202-sim-dto-name-resolution.md) |

## Analytics & CDR (7 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-110 | GET | /api/v1/dashboard | Tenant dashboard data (note: implementation uses `/dashboard`, not `/analytics/dashboard`; path corrected by compliance audit 2026-04-12). `operator_health[]` carries `code`, `sla_target`, `active_sessions`, `last_health_check` (FIX-202) + `latency_ms` (latest probe ms), `auth_rate` (0-100 pct, 100×(1-errorRate)), `latency_sparkline` (12-float 5-min buckets over last 1h) (FIX-203). WS `operator.health_changed` event patches operator rows live on status flip or latency delta >10% (no page refresh needed); 30s poll fallback retained. `traffic_heatmap[]` cells carry `raw_bytes` (int64, actual aggregated bytes for that 7-day/hour bucket) in addition to normalized `value` [0,1] — used for heatmap cell hover tooltip (FIX-221). `top_ip_pool` (omitempty, null when tenant has no active pools): `{ id, name, usage_pct }` — most-utilized active pool by `(used_addresses/total_addresses)*100` (FIX-221). | JWT (any) | See [STORY-033](../../stories/phase-6/STORY-033-realtime-metrics.md); DTO widened [FIX-202](../../stories/fix-ui-review/FIX-202-sim-dto-name-resolution.md), [FIX-203](../../stories/fix-ui-review/FIX-203-dashboard-operator-health.md), [FIX-221](../../stories/fix-ui-review/FIX-221-plan.md) |
| API-111 | GET | /api/v1/analytics/usage | Usage analytics (time-series). Response envelope: `totals` (total_bytes, total_sessions, total_auths, unique_sims + delta fields), `time_series[]` (UsageTimePoint: bucket, total_bytes, bytes_in, bytes_out, sessions, auths, unique_sims — bytes_in/out are 0 on 30d cdrs_daily per DEV-294), `top_consumers[]` (TopConsumer: sim_id, iccid, imsi, msisdn, operator_id, apn_id, bytes_in, bytes_out, total_bytes, sessions, avg_duration_sec — enriched via sims JOIN, FIX-220), `breakdowns[]`. Filter params: `period` (1h/24h/7d/30d), `operator_id`, `apn_id`, `rat_type`, `group_by`. 30d apn_id/rat_type filters now correctly applied (cdrs_daily guard removed FIX-220/DEV-293). | JWT (analyst+) | See [STORY-034](../../stories/phase-6/STORY-034-usage-analytics.md), [FIX-220](../../stories/fix-ui-review/FIX-220-plan.md) |
| API-112 | GET | /api/v1/analytics/cost | Cost analytics + optimization | JWT (analyst+) | See [STORY-035](../../stories/phase-6/STORY-035-cost-analytics.md) |
| API-113 | GET | /api/v1/analytics/anomalies | Anomaly detection results | JWT (analyst+) | See [STORY-036](../../stories/phase-6/STORY-036-anomaly-detection.md) |
| API-113b | GET | /api/v1/analytics/anomalies/{id} | Get single anomaly detail | JWT (analyst+) | See [STORY-036](../../stories/phase-6/STORY-036-anomaly-detection.md) |
| API-114 | GET | /api/v1/cdrs | List CDRs (time-range query); extended in FIX-214 with filters: `sim_id`, `operator_id`, `apn_id`, `record_type` (start/interim/stop/auth/auth_fail/reject), `rat_type`, `session_id`; cursor pagination; required date range (max 30d). MSISDN added to CDR row via batch SIM lookup. | JWT (analyst+) | See [STORY-032](../../stories/phase-6/STORY-032-cdr-processing.md), [FIX-214](../../stories/fix-ui-review/FIX-214-cdr-explorer-page.md) |
| API-114b | GET | /api/v1/cdrs/stats | CDR aggregate stats for filter window: record_count, unique_sims, unique_sessions, total_bytes_in, total_bytes_out. Delegates to Aggregates facade. Same filter params as API-114. | JWT (analyst+) | See [FIX-214](../../stories/fix-ui-review/FIX-214-cdr-explorer-page.md) |
| API-114c | GET | /api/v1/cdrs/by-session/{session_id} | All CDRs for a single session ordered by timestamp ASC; used by SessionTimelineDrawer. Tenant-scoped (cross-tenant → 404). | JWT (analyst+) | See [FIX-214](../../stories/fix-ui-review/FIX-214-cdr-explorer-page.md) |
| API-115 | POST | /api/v1/cdrs/export | Export CDRs to CSV; extended in FIX-214 with unconditional 30d cap, whitelist validation for record_type/rat_type, queues `cdr_export` job, returns 202 + job_id | JWT (analyst+) | See [STORY-032](../../stories/phase-6/STORY-032-cdr-processing.md), [FIX-214](../../stories/fix-ui-review/FIX-214-cdr-explorer-page.md) |

## Jobs (5 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-120 | GET | /api/v1/jobs | List jobs | JWT (sim_manager+) | See [STORY-013](../../stories/phase-2/STORY-013-bulk-import.md), [STORY-031](../../stories/phase-5/STORY-031-job-runner.md) |
| API-121 | GET | /api/v1/jobs/:id | Get job detail + progress | JWT (sim_manager+) | See [STORY-013](../../stories/phase-2/STORY-013-bulk-import.md), [STORY-031](../../stories/phase-5/STORY-031-job-runner.md) |
| API-122 | POST | /api/v1/jobs/:id/cancel | Cancel running job | JWT (tenant_admin) | See [STORY-013](../../stories/phase-2/STORY-013-bulk-import.md), [STORY-031](../../stories/phase-5/STORY-031-job-runner.md) |
| API-123 | POST | /api/v1/jobs/:id/retry | Retry failed items | JWT (sim_manager+) | See [STORY-013](../../stories/phase-2/STORY-013-bulk-import.md), [STORY-031](../../stories/phase-5/STORY-031-job-runner.md) |
| API-124 | GET | /api/v1/jobs/:id/errors | Download job error report (JSON or CSV) | JWT (sim_manager+) | See [STORY-013](../../stories/phase-2/STORY-013-bulk-import.md) |

## Notifications (6 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-130 | GET | /api/v1/notifications | List notifications (unread first) | JWT (any) | See [STORY-038](../../stories/phase-7/STORY-038-notification-engine.md) |
| API-130b | GET | /api/v1/notifications/unread-count | Get count of unread notifications | JWT (api_user+) | See [STORY-038](../../stories/phase-7/STORY-038-notification-engine.md) |
| API-131 | PATCH | /api/v1/notifications/:id/read | Mark notification as read | JWT (any) | See [STORY-038](../../stories/phase-7/STORY-038-notification-engine.md) |
| API-132 | POST | /api/v1/notifications/read-all | Mark all as read | JWT (any) | See [STORY-038](../../stories/phase-7/STORY-038-notification-engine.md) |
| API-133 | GET | /api/v1/notification-configs | Get notification preferences | JWT (any) | See [STORY-038](../../stories/phase-7/STORY-038-notification-engine.md) |
| API-134 | PUT | /api/v1/notification-configs | Update notification preferences | JWT (any) | See [STORY-038](../../stories/phase-7/STORY-038-notification-engine.md) |

## Audit (5 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-140 | GET | /api/v1/audit-logs | Search audit logs | JWT (tenant_admin+) | See [STORY-007](../../stories/phase-1/STORY-007-audit-log.md) |
| API-140b | GET | /api/v1/audit | Alias for /api/v1/audit-logs (backward compat; same handler) | JWT (tenant_admin+) | See [STORY-007](../../stories/phase-1/STORY-007-audit-log.md) |
| API-141 | GET | /api/v1/audit-logs/verify | Verify hash chain integrity | JWT (tenant_admin+) | See [STORY-007](../../stories/phase-1/STORY-007-audit-log.md) |
| API-142 | POST | /api/v1/audit-logs/export | Export audit logs (date range) | JWT (tenant_admin+) | See [STORY-007](../../stories/phase-1/STORY-007-audit-log.md) |
| API-194 | POST | /api/v1/audit/system-events | Emit infra-level system event into hash chain (TenantID=uuid.Nil) | JWT (super_admin) | See [STORY-067](../../stories/phase-10/STORY-067-cicd-ops.md) (AC-4, AC-8) |

## API Keys (5 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-150 | GET | /api/v1/api-keys | List API keys | JWT (tenant_admin+) | See [STORY-008](../../stories/phase-1/STORY-008-api-key-management.md) |
| API-151 | POST | /api/v1/api-keys | Create API key | JWT (tenant_admin+) | See [STORY-008](../../stories/phase-1/STORY-008-api-key-management.md) |
| API-152 | PATCH | /api/v1/api-keys/:id | Update API key (scopes, rate limit) | JWT (tenant_admin+) | See [STORY-008](../../stories/phase-1/STORY-008-api-key-management.md) |
| API-153 | POST | /api/v1/api-keys/:id/rotate | Rotate API key | JWT (tenant_admin+) | See [STORY-008](../../stories/phase-1/STORY-008-api-key-management.md) |
| API-154 | DELETE | /api/v1/api-keys/:id | Revoke API key | JWT (tenant_admin+) | See [STORY-008](../../stories/phase-1/STORY-008-api-key-management.md) |

## MSISDN Pool (3 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-160 | GET | /api/v1/msisdn-pool | List MSISDN pool | JWT (sim_manager+) | See [STORY-014](../../stories/phase-2/STORY-014-msisdn-pool.md) |
| API-161 | POST | /api/v1/msisdn-pool/import | Bulk import MSISDNs | JWT (tenant_admin+) | See [STORY-014](../../stories/phase-2/STORY-014-msisdn-pool.md) |
| API-162 | POST | /api/v1/msisdn-pool/:id/assign | Assign MSISDN to SIM | JWT (sim_manager+) | See [STORY-014](../../stories/phase-2/STORY-014-msisdn-pool.md) |

## SMS Gateway (2 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-170 | POST | /api/v1/sms/send | Send SMS to SIM | JWT (sim_manager+) | See [STORY-069](../../stories/phase-10/STORY-069-onboarding-reporting.md) (AC-12; fully implemented) |
| API-171 | GET | /api/v1/sms/history | SMS delivery history (cursor-paginated) | JWT (sim_manager+) | See [STORY-069](../../stories/phase-10/STORY-069-onboarding-reporting.md) (AC-12) |

## Compliance & Data Governance (5 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-175 | GET | /api/v1/compliance/dashboard | Compliance dashboard: BTK/KVKK/GDPR status, pending DSARs, audit hash-chain health | JWT (tenant_admin+) | See [STORY-039](../../stories/phase-7/STORY-039-compliance-reporting.md) |
| API-176 | GET | /api/v1/compliance/btk-report | Monthly BTK SIM inventory report (query `?format=json\|csv\|pdf`) | JWT (tenant_admin+) | See [STORY-039](../../stories/phase-7/STORY-039-compliance-reporting.md), [STORY-059](../../stories/phase-10/STORY-059-security-compliance.md) (PDF format added) |
| API-177 | PUT | /api/v1/compliance/retention | Update tenant data retention policy (days per entity type) | JWT (tenant_admin) | See [STORY-039](../../stories/phase-7/STORY-039-compliance-reporting.md) |
| API-178 | GET | /api/v1/compliance/dsar/{simId} | Data Subject Access Request — export all PII for a SIM (KVKK/GDPR Art. 15) | JWT (tenant_admin+) | See [STORY-039](../../stories/phase-7/STORY-039-compliance-reporting.md) |
| API-179 | POST | /api/v1/compliance/erasure/{simId} | Right to Erasure — pseudonymize audit logs + purge PII (KVKK/GDPR Art. 17) | JWT (tenant_admin) | See [STORY-039](../../stories/phase-7/STORY-039-compliance-reporting.md), [STORY-059](../../stories/phase-10/STORY-059-security-compliance.md) (salted hash unification) |

## SLA Reports (6 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-183 | GET | /api/v1/sla-reports | List SLA reports (cursor-paginated) | JWT (tenant_admin+) | See [STORY-063](../../stories/phase-10/STORY-063-backend-completeness.md) |
| API-184 | GET | /api/v1/sla-reports/{id} | Get single SLA report detail | JWT (tenant_admin+) | See [STORY-063](../../stories/phase-10/STORY-063-backend-completeness.md) |
| API-320 | GET | /api/v1/sla/history?months=N | Rolling-window SLA history (≤24 months). Response: `{months: MonthSummary[], overall: SLAOverallAgg}`. `meta.months_requested`, `meta.months_returned`. Tenant-scoped via `operator_grants`. | JWT (tenant_admin+) | See [FIX-215](../../stories/fix-ui-review/FIX-215-sla-historical-reports.md) |
| API-321 | GET | /api/v1/sla/months/:year/:month | Month detail: all operators with SLA data for given calendar month. Response: `{month, operators: OperatorMonthRow[], overall: SLAOverallAgg}`. Returns 404 + `sla_month_not_available` when no data. Tenant-scoped. | JWT (tenant_admin+) | See [FIX-215](../../stories/fix-ui-review/FIX-215-sla-historical-reports.md) |
| API-322 | GET | /api/v1/sla/operators/:operatorId/months/:year/:month/breaches | Per-operator breach list for a calendar month. `meta.breach_source` = `"live"` (≤90d) or `"persisted"` (>90d). Per-breach `affected_sessions_est`. Returns 404 + `sla_month_not_available` when beyond retention with no persisted row. Tenant-scoped. | JWT (tenant_admin+) | See [FIX-215](../../stories/fix-ui-review/FIX-215-sla-historical-reports.md) |
| API-323 | GET | /api/v1/sla/pdf?year=YYYY&month=MM[&operator_id=UUID] | Synchronous PDF export for a calendar month (all operators or single). `Content-Disposition: attachment`. 404 if no data. Tenant-scoped via `operator_grants`. Bearer-auth blob download. | JWT (tenant_admin+) | See [FIX-215](../../stories/fix-ui-review/FIX-215-sla-historical-reports.md) |

## Notifications — SMS Webhook (1 endpoint)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-185 | POST | /api/v1/notifications/sms/status | Twilio SMS delivery status callback (HMAC-SHA256 verified) | Twilio Signature | See [STORY-063](../../stories/phase-10/STORY-063-backend-completeness.md) |

## System Health (11 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-180 | GET | /api/health | Legacy health check (DB, Redis, NATS, AAA) — kept for backward compat | None | See [STORY-001](../../stories/phase-1/STORY-001-project-scaffold.md), [STORY-015](../../stories/phase-3/STORY-015-radius-server.md) (AAA status) |
| API-181 | GET | /api/v1/system/metrics | Built-in metrics (auth/s, latency, sessions) | JWT (super_admin) | See [STORY-033](../../stories/phase-6/STORY-033-realtime-metrics.md) |
| API-182 | GET | /api/v1/system/config | System configuration | JWT (super_admin) | See [STORY-001](../../stories/phase-1/STORY-001-project-scaffold.md) |
| API-187 | GET | /health/live | Liveness probe — goroutine-only check, always 200 when process is alive | None | See [STORY-066](../../stories/phase-10/STORY-066-reliability.md) (AC-3) |
| API-188 | GET | /health/ready | Readiness probe — full dependency check (DB, Redis, NATS, AAA) + disk space probe; 200=healthy/degraded, 503=unhealthy | None | See [STORY-066](../../stories/phase-10/STORY-066-reliability.md) (AC-3, AC-4) |
| API-189 | GET | /health/startup | Startup probe — 60-second grace period; returns 503 during startup, then delegates to /health/ready | None | See [STORY-066](../../stories/phase-10/STORY-066-reliability.md) (AC-3) |
| API-190 | GET | /api/v1/system/backup-status | Backup run history and latest verification result | JWT (super_admin) | See [STORY-066](../../stories/phase-10/STORY-066-reliability.md) (AC-1, AC-2) |
| API-191 | GET | /api/v1/system/jwt-rotation-history | JWT key rotation audit log (last 10 detections) | JWT (super_admin) | See [STORY-066](../../stories/phase-10/STORY-066-reliability.md) (AC-7) |
| API-192 | GET | /api/v1/status | Aggregate service status (public, no auth) — component health, uptime, version, recent_error_5m | None | See [STORY-067](../../stories/phase-10/STORY-067-cicd-ops.md) (AC-7) |
| API-193 | GET | /api/v1/status/details | Detailed service status (auth-gated) — per-dependency latency, disk, queue depth | JWT (super_admin) | See [STORY-067](../../stories/phase-10/STORY-067-cicd-ops.md) (AC-7) |
| API-201 | POST | /api/v1/system/revoke-all-sessions | Admin tenant-wide force-logout; bulk session revoke + WS disconnect + optional notify | JWT (super_admin or tenant_admin for own tenant) | See [STORY-068](../../stories/phase-10/STORY-068-enterprise-auth.md) (AC-7) |

## Observability Endpoints (1 endpoint)

These endpoints do not follow the standard `{ status, data }` JSON envelope and are not versioned under `/api/v1`. They are technical infrastructure endpoints.

| Path | Method | Format | Auth | Description | Detail |
|------|--------|--------|------|-------------|--------|
| `/metrics` | GET | Prometheus text exposition | None (scrape endpoint) | Prometheus metrics registry — all 17 argus_* vectors + Go runtime + process collectors. Added by STORY-065. | See [STORY-065](../../stories/phase-10/STORY-065-observability.md) |

## WebSocket Events

See [WEBSOCKET_EVENTS.md](../WEBSOCKET_EVENTS.md) for full payload schemas.

| Channel | Event | Description | Auth |
|---------|-------|-------------|------|
| ws://host/ws/v1/events | session.started | New AAA session created | JWT |
| | session.ended | AAA session terminated | JWT |
| | sim.state_changed | SIM state transition | JWT |
| | operator.health_changed | Operator health status change | JWT |
| | alert.new | New anomaly/SLA alert | JWT |
| | job.progress | Job progress update | JWT |
| | job.completed | Job finished | JWT |
| | notification.new | New notification | JWT |
| | policy.rollout_progress | Rollout stage progress | JWT |
| | metrics.realtime | Real-time auth/s, session count (1s interval) | JWT |

Implementation: See [STORY-040](../../stories/phase-7/STORY-040-websocket-events.md)

---

## Onboarding (5 endpoints) — STORY-069

| ID | Method | Path | Description | Auth | Notes |
|----|--------|------|-------------|------|-------|
| API-273 | GET | /api/v1/onboarding/status | Latest onboarding session state for the caller's tenant (nil if none) | JWT (api_user) | See [STORY-069](../../stories/phase-10/STORY-069-onboarding-reporting.md); doc entry added by audit 2026-04-17 |
| API-202 | POST | /api/v1/onboarding/start | Create a new onboarding session for the caller's tenant | JWT (api_user) | Returns `{session_id, current_step:1, steps_total:5}` |
| API-203 | GET | /api/v1/onboarding/:id | Hydrate session state for resume | JWT (api_user, same tenant) | Returns `current_step` + `data_by_step` map |
| API-204 | POST | /api/v1/onboarding/:id/step/:n | Submit step n payload (1..5); atomic side-effects per step | JWT (api_user, same tenant) | 422 with `details[]` on validation failure |
| API-205 | POST | /api/v1/onboarding/:id/complete | Finalise the wizard, fire welcome notification, NATS event | JWT (api_user, same tenant) | Idempotent |

## Reports (5 endpoints) — STORY-069

| ID | Method | Path | Description | Auth | Notes |
|----|--------|------|-------------|------|-------|
| API-206 | POST | /api/v1/reports/generate | Enqueue an on-demand report run | JWT (api_user) | Returns 202 `{job_id, status:"queued"}` |
| API-207 | GET | /api/v1/reports/scheduled | List scheduled report definitions (cursor) | JWT (api_user) | — |
| API-208 | POST | /api/v1/reports/scheduled | Create a scheduled report (cron + recipients[]) | JWT (tenant_admin+) | `next_run_at` computed via `NextRunAfter` |
| API-209 | PATCH | /api/v1/reports/scheduled/:id | Update schedule, recipients, filters, state | JWT (tenant_admin+) | `state ∈ {active, paused}` |
| API-210 | DELETE | /api/v1/reports/scheduled/:id | Delete a scheduled report | JWT (tenant_admin+) | — |

## Webhooks (6 endpoints) — STORY-069

| ID | Method | Path | Description | Auth | Notes |
|----|--------|------|-------------|------|-------|
| API-211 | GET | /api/v1/webhooks | List webhook configs (cursor) | JWT (tenant_admin+) | — |
| API-212 | POST | /api/v1/webhooks | Create config (https only, secret stored encrypted) | JWT (tenant_admin+) | Secret returned once on creation |
| API-213 | PATCH | /api/v1/webhooks/:id | Update url/secret/event_types/enabled | JWT (tenant_admin+) | — |
| API-214 | DELETE | /api/v1/webhooks/:id | Delete config (cascades deliveries via FK) | JWT (tenant_admin+) | — |
| API-215 | GET | /api/v1/webhooks/:id/deliveries | List deliveries (cursor, last 20 default) | JWT (tenant_admin+) | Includes signature, response_status, final_state |
| API-216 | POST | /api/v1/webhooks/:id/deliveries/:delivery_id/retry | Force re-send a single delivery | JWT (tenant_admin+) | Persists a new attempt |

## Notification Preferences & Templates (4 endpoints) — STORY-069

| ID | Method | Path | Description | Auth | Notes |
|----|--------|------|-------------|------|-------|
| API-217 | GET | /api/v1/notification-preferences | Per-tenant matrix (event_type × channels + severity threshold) | JWT (api_user) | — |
| API-218 | PUT | /api/v1/notification-preferences | Bulk upsert preferences[] | JWT (tenant_admin+) | Replaces missing rows |
| API-219 | GET | /api/v1/notification-templates | List templates (filter by event_type, locale) | JWT (api_user) | — |
| API-220 | PUT | /api/v1/notification-templates/:event_type/:locale | Upsert subject/body_text/body_html | JWT (tenant_admin+) | Go template syntax |

## Compliance — Data Portability (1 endpoint) — STORY-069

| ID | Method | Path | Description | Auth | Notes |
|----|--------|------|-------------|------|-------|
| API-223 | POST | /api/v1/compliance/data-portability/:user_id | Enqueue user data export (zip = data.json + summary.pdf) | JWT (self OR tenant_admin+) | 202 `{job_id, status:"queued"}`; signed URL emailed when ready (7d TTL) |

## Operator Detail Analytics (2 endpoints) — STORY-070

| ID | Method | Path | Description | Auth | Notes |
|----|--------|------|-------------|------|-------|
| API-224 | GET | /api/v1/operators/:id/health-history | Operator health check history (last N results, cursor) | JWT (api_user) | — |
| API-225 | GET | /api/v1/operators/:id/metrics | CDR-based operator metrics (auth rate, latency, bytes) in hourly buckets | JWT (api_user) | Sourced from `cdrs_hourly` materialized view |

## APN Detail Analytics (1 endpoint) — STORY-070

| ID | Method | Path | Description | Auth | Notes |
|----|--------|------|-------------|------|-------|
| API-226 | GET | /api/v1/apns/:id/traffic | APN CDR traffic series (bytes in/out by hour, configurable period) | JWT (api_user) | Sourced from `cdrs_hourly`/`cdrs_daily` materialized views |
| API-271 | GET | /api/v1/operators/:id/sessions | Active sessions scoped to operator (cursor-paginated) | JWT (operator_manager+) | See [STORY-075](../../stories/phase-10/STORY-075-cross-entity-context.md); doc entry added by audit 2026-04-17 |
| API-272 | GET | /api/v1/operators/:id/traffic | Operator CDR traffic series (bytes in/out by hour, configurable period) | JWT (api_user) | Sourced from `cdrs_hourly`/`cdrs_daily`; doc entry added by audit 2026-04-17 |

## Violation Remediation (1 endpoint) — STORY-070

| ID | Method | Path | Description | Auth | Notes |
|----|--------|------|-------------|------|-------|
| API-227 | POST | /api/v1/policy-violations/:id/acknowledge | Acknowledge a policy violation with optional note | JWT (operator+) | Adds `acknowledged_at`, `acknowledged_by`, `acknowledged_note` columns; audited |

## Report Definitions (1 endpoint) — STORY-070

| ID | Method | Path | Description | Auth | Notes |
|----|--------|------|-------------|------|-------|
| API-228 | GET | /api/v1/reports/definitions | List available report type definitions (id, label, formats) | JWT (api_user) | Returns 8 hardcoded definitions; staleTime 5min on client |

## System Capacity (1 endpoint) — STORY-070

| ID | Method | Path | Description | Auth | Notes |
|----|--------|------|-------------|------|-------|
| API-229 | GET | /api/v1/system/capacity | Platform capacity targets and current utilisation (SIMs, sessions, auth/s, monthly growth) | JWT (super_admin) | Targets from `ARGUS_CAPACITY_*` env vars; current counts from DB |

---

## Roaming Agreements (6 endpoints) — STORY-071

| ID | Method | Path | Description | Auth | Notes |
|----|--------|------|-------------|------|-------|
| API-230 | POST | /api/v1/roaming-agreements | Create a new roaming agreement | JWT (operator_manager) | Validates dates, currency, cost_per_mb; enforces single-active-per-operator via partial unique index; audit: roaming_agreement.create |
| API-231 | GET | /api/v1/roaming-agreements | List roaming agreements (cursor-paginated) | JWT (api_user) | Filters: operator_id, state, agreement_type; cursor pagination (limit+1 trick) |
| API-232 | GET | /api/v1/roaming-agreements/{id} | Get single roaming agreement | JWT (api_user) | 404 roaming_agreement_not_found on missing/wrong-tenant |
| API-233 | PATCH | /api/v1/roaming-agreements/{id} | Update roaming agreement fields | JWT (operator_manager) | State guard: terminated agreements cannot be updated; audit: roaming_agreement.update |
| API-234 | DELETE | /api/v1/roaming-agreements/{id} | Terminate a roaming agreement | JWT (operator_manager) | Sets state=terminated, terminated_at=now; audit: roaming_agreement.terminate |
| API-235 | GET | /api/v1/operators/{id}/roaming-agreements | List agreements for a specific operator | JWT (api_user) | Operator must be granted to tenant; 403 roaming_agreement_operator_not_granted otherwise |

---

## Ops Endpoints (3 endpoints) — STORY-072

| ID | Method | Path | Description | Auth | Notes |
|----|--------|------|-------------|------|-------|
| API-236 | GET | /api/v1/ops/metrics/snapshot | Platform metrics snapshot (HTTP p50/p95/p99, AAA auth rate, active sessions, error rate, memory/goroutines) | JWT (super_admin) | 5s in-memory TTL cache; sources Prometheus Registry + Redis counters |
| API-237 | GET | /api/v1/ops/infra-health | Infrastructure health detail (DB pool stats, NATS stream info + per-consumer lag, Redis memory/hit rate) | JWT (super_admin) | Sub-5s TTL on Redis section; DB/NATS info queried live |
| API-238 | GET | /api/v1/ops/incidents | Tenant incident timeline (anomalies + audit events merged, sorted by severity+time, LIMIT 200) | JWT (super_admin) | Per-tenant scoping via store layer; cursor pagination |

## Anomaly Lifecycle Endpoints (3 endpoints) — STORY-072

| ID | Method | Path | Description | Auth | Notes |
|----|--------|------|-------------|------|-------|
| API-239 | GET | /api/v1/analytics/anomalies/{id}/comments | List comments on an anomaly (chronological, LEFT JOIN users for email) | JWT (tenant_admin) | Indexed on (anomaly_id, created_at DESC); no N+1 |
| API-240 | POST | /api/v1/analytics/anomalies/{id}/comments | Add comment to an anomaly (body 1..2000 chars) | JWT (tenant_admin) | Writes to TBL-44; RLS via app.current_tenant |
| API-241 | POST | /api/v1/analytics/anomalies/{id}/escalate | Escalate anomaly with note (≤500 chars); sets state=escalated | JWT (tenant_admin) | State-transition note persisted as comment; audited |

---

## Admin Endpoints (14 endpoints) — STORY-073

> All under `/api/v1/admin/`. Role: `super_admin` unless noted. Auth: JWT.

| ID | Method | Path | Description | Auth | Notes |
|----|--------|------|-------------|------|-------|
| API-242 | GET | /api/v1/admin/tenants/resources | Per-tenant resource summary: SIM count, API RPS, active sessions, CDR volume, storage used, spark arrays | JWT (super_admin) | N+1 accepted (PERF-073); React Query 60s refetch |
| API-243 | GET | /api/v1/admin/tenants/quotas | Per-tenant quota progress: max_sims, max_apns, max_users, max_api_keys vs. current with ok/warning/danger thresholds | JWT (super_admin) | Color thresholds: <80% ok, <95% warning, ≥95% danger |
| API-244 | GET | /api/v1/admin/cost/by-tenant | Monthly cost breakdown per tenant: total, radius_cost, operator_cost, sms_cost, storage_cost, 6-month trend | JWT (super_admin) | React Query 5min staleTime |
| API-245 | GET | /api/v1/admin/sessions/active | List all active portal sessions globally; tenant_admin scoped to own tenant | JWT (tenant_admin+) | Fields: user_email, ip, browser, os, device, login_at, last_seen_at, idle_duration |
| API-246 | POST | /api/v1/admin/sessions/{session_id}/revoke | Force-logout a specific session; emits session.force_logout audit event | JWT (super_admin) | Invalidates token; user redirected to login on next request |
| API-247 | GET | /api/v1/admin/api-keys/usage | Per-API-key usage stats: request rate, rate-limit consumption, top endpoints, error rate, anomaly flag | JWT (super_admin) | Redis counters per key; cursor-paginated (limit=50) |
| API-248 | GET | /api/v1/admin/kill-switches | List all 5 canonical kill switches with current state and last toggled metadata | JWT (super_admin) | 15s TTL in-memory cache (PERF-074); returns array |
| API-249 | PATCH | /api/v1/admin/kill-switches/{key} | Toggle a kill switch on/off; requires reason field; emits killswitch.toggled audit event | JWT (super_admin) | Keys: radius_auth, session_create, bulk_ops, read_only, external_notifications |
| API-250 | GET | /api/v1/admin/maintenance-windows | List scheduled and historical maintenance windows | JWT (super_admin) | RLS on maintenance_windows; cursor-paginated |
| API-251 | POST | /api/v1/admin/maintenance-windows | Schedule a new maintenance window with start/end time, affected services, notification plan | JWT (super_admin) | Emits maintenance.scheduled audit event |
| API-252 | DELETE | /api/v1/admin/maintenance-windows/{id} | Cancel (delete) a scheduled maintenance window | JWT (super_admin) | Emits maintenance.cancelled audit event |
| API-253 | GET | /api/v1/admin/delivery/status | Per-channel notification delivery stats: success rate, failure rate, retry depth, latency p50/p95/p99 | JWT (super_admin) | 5 channels: email, sms, webhook, in-app, telegram; React Query 60s refetch |
| API-254 | GET | /api/v1/admin/purge-history | Chronological list of purged SIM records: iccid, msisdn, tenant, actor, purged_at | JWT (super_admin) | Sourced from audit_logs + sims JOIN; cursor-paginated |
| API-255 | GET | /api/v1/admin/dsar/queue | DSAR job queue: data-portability + KVKK-purge + right-to-erasure jobs with SLA countdown | JWT (tenant_admin+) | SLA tracked in hours (72h KVKK default); tenant_admin scoped to own tenant |

---

## Policy Violations (5 endpoints) — STORY-025, STORY-075

| ID | Method | Path | Description | Auth | Notes |
|----|--------|------|-------------|------|-------|
| API-262 | GET | /api/v1/policy-violations | List policy violations (cursor-paginated). DTO includes enriched fields: `iccid`, `imsi`, `msisdn`, `operator_name`, `operator_code`, `apn_name`, `policy_name`, `policy_version_number` (via `ListEnriched` LEFT JOINs — FIX-202). | JWT (api_user+) | See [STORY-025](../../stories/phase-4/STORY-025-policy-rollout.md); enrichment added [FIX-202](../../stories/fix-ui-review/FIX-202-sim-dto-name-resolution.md) |
| API-263 | GET | /api/v1/policy-violations/counts | Violation counts grouped by type/severity | JWT (api_user+) | See [STORY-025](../../stories/phase-4/STORY-025-policy-rollout.md) |
| API-266 | GET | /api/v1/policy-violations/export.csv | Stream policy violations as CSV (respects current filters) | JWT (api_user+) | Code already wired in `internal/gateway/router.go:622`; doc entry added by audit (2026-04-15) |
| API-259 | GET | /api/v1/policy-violations/:id | Get violation detail with enriched SIM + policy context; cross-tenant 404 on mismatch. Enriched fields: `iccid`, `operator_name`, `operator_code`, `apn_name`, `policy_name`, `policy_version_number` (FIX-202). | JWT (sim_manager+) | See [STORY-075](../../stories/phase-10/STORY-075-cross-entity-context.md); enrichment added [FIX-202](../../stories/fix-ui-review/FIX-202-sim-dto-name-resolution.md) |
| API-260 | POST | /api/v1/policy-violations/:id/remediate | Remediate violation: `action ∈ {suspend_sim, escalate, dismiss}`; emits violation.remediated/escalated/dismissed audit events | JWT (sim_manager+) | See [STORY-075](../../stories/phase-10/STORY-075-cross-entity-context.md); suspend_sim calls simStore.Suspend (409 on invalid transition) |

---

## OTA Commands (3 endpoints) — STORY-029

| ID | Method | Path | Description | Auth | Notes |
|----|--------|------|-------------|------|-------|
| API-172 | GET | /api/v1/ota-commands/{commandId} | Get OTA command detail by ID | JWT (sim_manager+) | See [STORY-029](../../stories/phase-5/STORY-029-ota-commands.md) |
| API-173 | POST | /api/v1/sims/{id}/ota | Send OTA command to a single SIM | JWT (sim_manager+) | See [STORY-029](../../stories/phase-5/STORY-029-ota-commands.md) |
| API-174 | POST | /api/v1/sims/bulk/ota | Bulk send OTA command to a segment of SIMs | JWT (tenant_admin) | See [STORY-029](../../stories/phase-5/STORY-029-ota-commands.md) |

---

## Universal Search (1 endpoint) — STORY-076

| ID | Method | Path | Description | Auth | Notes |
|----|--------|------|-------------|------|-------|
| API-261 | GET | /api/v1/search | Cross-entity full-text search: `?q=<query>&types=sim,apn,operator,policy,user&limit=<n>`. Returns grouped results `{type,id,label,sub}`. Tenant-scoped. 500ms context timeout. Limit capped at 20. Rate-limited via gateway middleware. | JWT (api_user+) | See [STORY-076](../../stories/phase-10/STORY-076-universal-search-nav.md) (AC-1) |

---

## Saved Views & User Preferences (5 endpoints) — STORY-077

| ID | Method | Path | Description | Auth | Notes |
|----|--------|------|-------------|------|-------|
| API-274 | GET | /api/v1/users/me/views?page=<page> | List the current user's saved views for a page (max 20/page). | JWT (any) | See [STORY-077](../../stories/phase-10/STORY-077-enterprise-ux-polish.md) AC-1 |
| API-275 | POST | /api/v1/users/me/views | Create a saved view: `{page, name, filters_json}`. Unique per (user_id,page,name); partial unique on default. | JWT (any) | See [STORY-077](../../stories/phase-10/STORY-077-enterprise-ux-polish.md) AC-1 |
| API-276 | PATCH | /api/v1/users/me/views/:view_id | Rename / update filters_json on an owned view. | JWT (owner) | See [STORY-077](../../stories/phase-10/STORY-077-enterprise-ux-polish.md) AC-1 |
| API-277 | DELETE | /api/v1/users/me/views/:view_id | Delete a saved view. | JWT (owner) | See [STORY-077](../../stories/phase-10/STORY-077-enterprise-ux-polish.md) AC-1 |
| API-278 | POST | /api/v1/users/me/views/:view_id/default | Mark a view as the default for its page (enforced by partial unique index). | JWT (owner) | See [STORY-077](../../stories/phase-10/STORY-077-enterprise-ux-polish.md) AC-1 |
| API-279 | PATCH | /api/v1/users/me/preferences | Upsert the caller's UI preferences (table density, column visibility, language). Payload merged into `user_column_preferences`. | JWT (any) | See [STORY-077](../../stories/phase-10/STORY-077-enterprise-ux-polish.md) AC-8/9 |

## Undo (1 endpoint) — STORY-077

| ID | Method | Path | Description | Auth | Notes |
|----|--------|------|-------------|------|-------|
| API-280 | POST | /api/v1/undo/:action_id | Execute the inverse of a previously-registered destructive action (bulk state change, policy delete, apikey revoke, segment delete). Idempotent; expires after 5 minutes. Emits `audit.undone` event. | JWT (original actor) | See [STORY-077](../../stories/phase-10/STORY-077-enterprise-ux-polish.md) AC-2 |

## Announcements (5 endpoints) — STORY-077

| ID | Method | Path | Description | Auth | Notes |
|----|--------|------|-------------|------|-------|
| API-281 | GET | /api/v1/announcements/active | Active (un-dismissed) announcements for the caller's tenant. Joins `announcement_dismissals`. | JWT (any) | See [STORY-077](../../stories/phase-10/STORY-077-enterprise-ux-polish.md) AC-10 |
| API-282 | POST | /api/v1/announcements/:id/dismiss | Dismiss an announcement for the current user. | JWT (any) | See [STORY-077](../../stories/phase-10/STORY-077-enterprise-ux-polish.md) AC-10 |
| API-283 | GET | /api/v1/announcements | List all announcements (admin view, cursor-paginated). | JWT (tenant_admin+) | See [STORY-077](../../stories/phase-10/STORY-077-enterprise-ux-polish.md) AC-10 |
| API-284 | POST | /api/v1/announcements | Create announcement: `{severity, target(all|tenant), starts_at, ends_at, body, dismissible}`. | JWT (super_admin for target=all; tenant_admin for own tenant) | See [STORY-077](../../stories/phase-10/STORY-077-enterprise-ux-polish.md) AC-10 |
| API-285 | PATCH | /api/v1/announcements/:id | Update fields (body, starts_at, ends_at, severity, dismissible). | JWT (super_admin / tenant_admin) | See [STORY-077](../../stories/phase-10/STORY-077-enterprise-ux-polish.md) AC-10 |
| API-286 | DELETE | /api/v1/announcements/:id | Hard-delete an announcement. | JWT (super_admin / tenant_admin) | See [STORY-077](../../stories/phase-10/STORY-077-enterprise-ux-polish.md) AC-10 |

## Chart Annotations (3 endpoints) — STORY-077

| ID | Method | Path | Description | Auth | Notes |
|----|--------|------|-------------|------|-------|
| API-287 | GET | /api/v1/analytics/charts/:chart_key/annotations | List annotations for a chart (tenant-scoped). | JWT (analyst+) | See [STORY-077](../../stories/phase-10/STORY-077-enterprise-ux-polish.md) AC-11 |
| API-288 | POST | /api/v1/analytics/charts/:chart_key/annotations | Create an annotation: `{timestamp, label, severity, body}`. | JWT (analyst+) | See [STORY-077](../../stories/phase-10/STORY-077-enterprise-ux-polish.md) AC-11 |
| API-289 | DELETE | /api/v1/analytics/charts/:chart_key/annotations/:annotation_id | Delete an annotation owned by the caller (tenant_admin can delete any). | JWT (owner or tenant_admin+) | See [STORY-077](../../stories/phase-10/STORY-077-enterprise-ux-polish.md) AC-11 |

## Impersonation (2 endpoints) — STORY-077

| ID | Method | Path | Description | Auth | Notes |
|----|--------|------|-------------|------|-------|
| API-290 | POST | /api/v1/admin/impersonate/:user_id | Start impersonation of a target user: issues a new JWT with `act.sub=original_admin_id` and `impersonated=true`, 1h TTL, read-only enforcement in middleware. | JWT (super_admin) | See [STORY-077](../../stories/phase-10/STORY-077-enterprise-ux-polish.md) AC-12 |
| API-291 | POST | /api/v1/admin/impersonate/exit | Exit impersonation; re-issues the original admin JWT via the server-held refresh binding. | JWT (impersonated session) | See [STORY-077](../../stories/phase-10/STORY-077-enterprise-ux-polish.md) AC-12 |

## CSV Export — List Resources (13 endpoints) — STORY-062 + STORY-077

Streaming CSV export for every list resource. Each endpoint reuses its list handler's filter parsing and cursor-paginates internally via `export.StreamCSV`. Response: `Content-Type: text/csv`, `Content-Disposition: attachment; filename=<resource>_<filters>_<date>.csv`. No in-memory buffering — memory stays flat under 10M-row exports.

| ID | Method | Path | Resource | Auth | Notes |
|----|--------|------|----------|------|-------|
| API-292 | GET | /api/v1/sims/export.csv | SIMs | JWT (sim_manager+) | See [STORY-077](../../stories/phase-10/STORY-077-enterprise-ux-polish.md) AC-4 |
| API-293 | GET | /api/v1/apns/export.csv | APNs | JWT (sim_manager+) | See [STORY-077](../../stories/phase-10/STORY-077-enterprise-ux-polish.md) AC-4 |
| API-294 | GET | /api/v1/operators/export.csv | Operators | JWT (operator_manager+) | See [STORY-077](../../stories/phase-10/STORY-077-enterprise-ux-polish.md) AC-4 |
| API-295 | GET | /api/v1/policies/export.csv | Policies | JWT (policy_editor+) | See [STORY-077](../../stories/phase-10/STORY-077-enterprise-ux-polish.md) AC-4 |
| API-296 | GET | /api/v1/sessions/export.csv | Sessions | JWT (sim_manager+) | See [STORY-062](../../stories/phase-10/STORY-062-perf-doc-drift.md) D-010 |
| API-297 | GET | /api/v1/jobs/export.csv | Jobs | JWT (sim_manager+) | See [STORY-077](../../stories/phase-10/STORY-077-enterprise-ux-polish.md) AC-4 |
| API-298 | GET | /api/v1/audit-logs/export.csv | Audit logs | JWT (tenant_admin+) | See [STORY-077](../../stories/phase-10/STORY-077-enterprise-ux-polish.md) AC-4 |
| API-299 | GET | /api/v1/cdrs/export.csv | CDRs | JWT (analyst+) | See [STORY-077](../../stories/phase-10/STORY-077-enterprise-ux-polish.md) AC-4 |
| API-300 | GET | /api/v1/notifications/export.csv | Notifications | JWT (any) | See [STORY-077](../../stories/phase-10/STORY-077-enterprise-ux-polish.md) AC-4 |
| API-301 | GET | /api/v1/analytics/anomalies/export.csv | Anomalies | JWT (analyst+) | See [STORY-062](../../stories/phase-10/STORY-062-perf-doc-drift.md) D-010 (FE useExport alias fix) |
| API-302 | GET | /api/v1/users/export.csv | Users | JWT (tenant_admin+) | See [STORY-077](../../stories/phase-10/STORY-077-enterprise-ux-polish.md) AC-4 |
| API-303 | GET | /api/v1/api-keys/export.csv | API keys | JWT (tenant_admin+) | See [STORY-077](../../stories/phase-10/STORY-077-enterprise-ux-polish.md) AC-4 |
| (API-266) | GET | /api/v1/policy-violations/export.csv | Policy violations | JWT (api_user+) | Already indexed under Policy Violations section (D-024) |

## 5G SBA — Nsmf Mock (2 endpoints) — STORY-092

Minimal mock for 5G SMF (Session Management Function) `Nsmf_PDUSession` service — Create + Release only. Allocates UE IPs end-to-end via the same `AllocateIP`/`ReleaseIP` store pipeline as the RADIUS and Diameter Gx paths. Scope strictly limited per STORY-092 D3-B: no PATCH, no QoS update, no PCF, no UPF selection. STORY-089 (operator-SoR simulator) is the logical long-term home — the mock will be absorbed into `cmd/operator-sim` once that container ships.

| ID | Method | Path | Description | Auth | Notes |
|----|--------|------|-------------|------|-------|
| API-304 | POST | /nsmf-pdusession/v1/sm-contexts | Create SM Context — allocates UE IP from APN pool, persists `sim.ip_address_id`, invalidates SIM cache, returns 201 + `Location: /nsmf-pdusession/v1/sm-contexts/{smContextRef}`. 3GPP-native ProblemDetails on error (USER_NOT_FOUND, SERVING_NETWORK_NOT_AUTHORIZED, SYSTEM_FAILURE). | TLS mTLS per operator (or disabled in test harness) | See STORY-092 plan D3-B + `internal/aaa/sba/nsmf.go:HandleCreate` |
| API-305 | DELETE | /nsmf-pdusession/v1/sm-contexts/{smContextRef} | Release SM Context — releases dynamic IP (static preserved via `allocation_type` gate), returns 204 No Content. Unknown `smContextRef` returns 204 (idempotent). | TLS mTLS per operator (or disabled in test harness) | See STORY-092 plan D3-B + `internal/aaa/sba/nsmf.go:HandleRelease` |

## 5G SBA — AUSF / UDM / NRF (5 endpoints) — STORY-020

Pre-existing 5G SBA endpoints shipped by STORY-020 implementing AUSF authentication, UDM UEAU/UECM, and NRF NFManagement service roles. These endpoints are served by the argus-app listener and registered in the SBA router alongside Nsmf. Indexed here under D-039 re-sweep (STORY-089).

| ID | Method | Path | Description | Auth | Notes |
|----|--------|------|-------------|------|-------|
| API-308 | POST | /nausf-auth/v1/ue-authentications | AUSF UE authentication initiate — bootstraps 5G AKA authentication challenge for a UE, returns 201 with `authCtxId` and EAP payload. | TLS mTLS per operator (or disabled in test harness) | See `internal/aaa/sba/ausf.go` |
| API-309 | GET | /nudm-ueau/v1/{supi}/security-information | UDM UEAU — retrieve authentication vectors for the given SUPI; returns 200 with HE AV set. | TLS mTLS per operator (or disabled in test harness) | See `internal/aaa/sba/udm.go` |
| API-310 | POST | /nudm-uecm/v1/{ueId}/registrations/amf-3gpp-access | UDM UECM — register AMF 3GPP access for a UE; returns 201 on first registration, 200 on update. | TLS mTLS per operator (or disabled in test harness) | See `internal/aaa/sba/udm.go` |
| API-311 | GET | /nnrf-nfm/v1/nf-instances | NRF NFManagement — discover NF instances matching optional query params (nf-type, status); returns 200 with NF profile list. | TLS mTLS per operator (or disabled in test harness) | See `internal/aaa/sba/nrf.go` |
| API-312 | PATCH | /nnrf-nfm/v1/nf-instances/{nfInstanceId} | NRF NFManagement — NF heartbeat/patch; updates NF profile fields (status, load) via JSON Merge Patch, returns 200. | TLS mTLS per operator (or disabled in test harness) | See `internal/aaa/sba/nrf.go` |

---

## Alerts (3 endpoints) — FIX-209 + FIX-210

| ID | Method | Path | Description | Auth | Notes |
|----|--------|------|-------------|------|-------|
| API-313 | GET | /api/v1/alerts | List alerts with filters (type, severity, source, state, sim/operator/apn, date range, substring, cursor). FIX-209. | JWT (analyst+) | Cursor-paginated; tenant-scoped; 7 filter params. Alert response shape expanded by FIX-210: includes `dedup_key`, `occurrence_count`, `first_seen_at`, `last_seen_at`, `cooldown_until`. |
| API-314 | GET | /api/v1/alerts/{id} | Get alert by ID (tenant-scoped). FIX-209. | JWT (analyst+) | 404 `ALERT_NOT_FOUND` on missing or cross-tenant. Alert response shape expanded by FIX-210: includes `dedup_key`, `occurrence_count`, `first_seen_at`, `last_seen_at`, `cooldown_until`. |
| API-315 | PATCH | /api/v1/alerts/{id} | Transition alert state (`open`→`acknowledged`, `open`/`ack`→`resolved`). FIX-209. | JWT (sim_manager+) | `suppressed` NOT settable via this endpoint — managed by dedup state machine (FIX-210). |
| API-316 | GET | /api/v1/events/catalog | Read-only canonical event catalog. Returns list of all 14 in-scope NATS subjects with `type`, `source`, `default_severity`, `entity_type`, `meta_schema`. FIX-212 AC-5. | JWT (any authenticated) | No FE consumer until FIX-240 (Notification Preferences). Implementation: `internal/api/events/catalog_handler.go`. |
| API-319 | POST | /api/v1/alerts/suppressions | Create an alert suppression (ad-hoc mute or saved rule). FIX-229 AC-1 + AC-5. | JWT (sim_manager+) | Body: `{scope_type, scope_value, duration\|expires_at, reason, rule_name?}`. 201 `{data: suppression, applied_count}`. 409 `DUPLICATE` on duplicate `rule_name`. 503 when suppression store unavailable. |
| API-320 | GET | /api/v1/alerts/suppressions | List active suppressions for tenant (cursor-paginated). FIX-229 AC-5. | JWT (sim_manager+) | Filters: `rule_name`, `scope_type`, `active_only` (default true). Returns suppression rows incl. `rule_name`, `expires_at`, `applied_count`. |
| API-321 | DELETE | /api/v1/alerts/suppressions/{id} | Delete (unmute) a suppression by ID. FIX-229 AC-1 + AC-5. | JWT (sim_manager+) | 204 on success. 404 `SUPPRESSION_NOT_FOUND` on missing or cross-tenant. |
| API-322 | GET | /api/v1/alerts/{id}/similar | List alerts similar to the given anchor alert. FIX-229 AC-3. | JWT (analyst+) | Query: `limit=20` (1..50). Match: same `dedup_key` (all states) OR same `type`+`source` fallback. Excludes anchor. 404 `ALERT_NOT_FOUND` if anchor missing. Returns `{data: [], strategy: "dedup_key"\|"type_source"}`. |
| API-323 | GET | /api/v1/alerts/export.csv | Export alerts as CSV (inline download). FIX-229 AC-2. | JWT (analyst+) | Same filters as `GET /alerts`. Server cap: 10 000 rows. `Content-Disposition: attachment; filename=alerts.csv`. 404 `ALERT_NO_DATA` when 0 rows match. |
| API-324 | GET | /api/v1/alerts/export.json | Export alerts as JSON (inline download). FIX-229 AC-2. | JWT (analyst+) | Same filters as `GET /alerts`. Server cap: 10 000 rows. `Content-Disposition: attachment; filename=alerts.json`. 404 `ALERT_NO_DATA` when 0 rows match. |
| API-325 | GET | /api/v1/alerts/export.pdf | Export alerts as PDF via report engine. FIX-229 AC-2. | JWT (analyst+) | Same filters as `GET /alerts`. Server cap: 10 000 rows. `Content-Disposition: attachment; filename=alerts.pdf`. 404 `ALERT_NO_DATA` when 0 rows match. 503 when report engine unavailable. |

---

**Total: 257 REST endpoints + 11 WebSocket event types**

> Index updated 2026-04-17 by compliance audit — 37 row additions (API-267..303 + Onboarding/Sessions/Traffic/SIM-IP fillers) cover STORY-077 (saved views, preferences, undo, announcements, chart annotations, impersonation, CSV exports), STORY-068 (backup-codes/remaining, session delete), STORY-069 (onboarding/status), STORY-070 (operator traffic), STORY-075 (operator sessions, sim ip-current), STORY-077 (APN referencing-policies). See `docs/reports/compliance-audit-report.md`.
> Index updated 2026-04-18 by STORY-089 D-039 re-sweep — 5 row additions (API-308..312) index pre-existing AUSF/UDM/NRF endpoints shipped by STORY-020; pending note removed.
> Index updated 2026-04-21 by FIX-212 review — 1 row addition (API-316) for event catalog endpoint shipped by FIX-212 AC-5.
> Index updated 2026-04-25 by FIX-228 Wave 5 docs — 2 row additions (API-317..318) for password reset request + confirm endpoints. Auth & Users count updated 20→22.
> Index updated 2026-04-25 by FIX-229 review — 7 row additions (API-319..325) for alert suppressions CRUD, similar-alerts, and tri-format export (CSV/JSON/PDF). Total updated 249→256.
> Index updated 2026-04-26 by FIX-233 review — 1 row addition (API-326) for `GET /policy-rollouts` list active rollouts; API-040/041 updated with new filter params + DTO additions (`policy_id`, `rollout_id?`, `rollout_stage_pct?`, `coa_status?`); Policies count 11→12. Total updated 256→257.

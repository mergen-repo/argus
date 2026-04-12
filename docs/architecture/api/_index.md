# API Index — Argus

> **Implementation note:** Full request/response specifications are in the story files (`docs/stories/phase-N/STORY-NNN.md`) under the "API Contract" section. For implementation, read both this index AND the relevant story. Error codes and envelope format are documented in [ERROR_CODES.md](../ERROR_CODES.md).

> Base URL: `/api/v1`
> Auth: JWT (portal), API Key (M2M), OAuth2 (3rd party)
> Response format: Standard envelope `{ status, data, meta?, error? }`
> Pagination: Cursor-based (default 50/page)

## Auth & Users (9 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-001 | POST | /api/v1/auth/login | User login (JWT + refresh token) | None | See [STORY-003](../../stories/phase-1/STORY-003-auth-jwt.md) |
| API-002 | POST | /api/v1/auth/refresh | Refresh JWT token | Refresh Token | See [STORY-003](../../stories/phase-1/STORY-003-auth-jwt.md) |
| API-003 | POST | /api/v1/auth/logout | Revoke session | JWT | See [STORY-003](../../stories/phase-1/STORY-003-auth-jwt.md) |
| API-004 | POST | /api/v1/auth/2fa/setup | Enable TOTP 2FA | JWT | See [STORY-003](../../stories/phase-1/STORY-003-auth-jwt.md) |
| API-005 | POST | /api/v1/auth/2fa/verify | Verify TOTP code | JWT | See [STORY-003](../../stories/phase-1/STORY-003-auth-jwt.md) |
| API-006 | GET | /api/v1/users | List users in tenant | JWT (tenant_admin+) | See [STORY-005](../../stories/phase-1/STORY-005-tenant-management.md) |
| API-007 | POST | /api/v1/users | Create user + invite | JWT (tenant_admin+) | See [STORY-005](../../stories/phase-1/STORY-005-tenant-management.md) |
| API-008 | PATCH | /api/v1/users/:id | Update user | JWT (tenant_admin+ or self) | See [STORY-005](../../stories/phase-1/STORY-005-tenant-management.md) |
| API-195 | DELETE | /api/v1/users/:id?gdpr=1 | GDPR right-to-erasure: nulls PII, sets state=purged, emits system audit event | JWT (super_admin) | See [STORY-067](../../stories/phase-10/STORY-067-cicd-ops.md) (scope addition) |
| API-186 | GET | /api/v1/auth/sessions | List active portal sessions for current user (cursor-paginated) | JWT (api_user+) | See [STORY-064](../../stories/phase-10/STORY-064-db-hardening.md) |

## Tenants (5 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-010 | GET | /api/v1/tenants | List all tenants | JWT (super_admin) | See [STORY-005](../../stories/phase-1/STORY-005-tenant-management.md) |
| API-011 | POST | /api/v1/tenants | Create tenant | JWT (super_admin) | See [STORY-005](../../stories/phase-1/STORY-005-tenant-management.md) |
| API-012 | GET | /api/v1/tenants/:id | Get tenant detail | JWT (super_admin or own tenant) | See [STORY-005](../../stories/phase-1/STORY-005-tenant-management.md) |
| API-013 | PATCH | /api/v1/tenants/:id | Update tenant | JWT (super_admin or tenant_admin) | See [STORY-005](../../stories/phase-1/STORY-005-tenant-management.md) |
| API-014 | GET | /api/v1/tenants/:id/stats | Tenant statistics | JWT (tenant_admin+) | See [STORY-005](../../stories/phase-1/STORY-005-tenant-management.md) |

## Operators (8 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-020 | GET | /api/v1/operators | List operators | JWT (super_admin) | See [STORY-009](../../stories/phase-2/STORY-009-operator-crud.md) |
| API-021 | POST | /api/v1/operators | Create operator | JWT (super_admin) | See [STORY-009](../../stories/phase-2/STORY-009-operator-crud.md) |
| API-022 | PATCH | /api/v1/operators/:id | Update operator config | JWT (super_admin) | See [STORY-009](../../stories/phase-2/STORY-009-operator-crud.md) |
| API-023 | GET | /api/v1/operators/:id/health | Get operator health status | JWT (operator_manager+) | See [STORY-009](../../stories/phase-2/STORY-009-operator-crud.md), [STORY-021](../../stories/phase-3/STORY-021-operator-failover.md) |
| API-024 | POST | /api/v1/operators/:id/test | Test operator connection | JWT (super_admin) | See [STORY-018](../../stories/phase-3/STORY-018-operator-adapter.md) |
| API-025 | GET | /api/v1/operator-grants | List tenant operator grants | JWT (tenant_admin+) | See [STORY-009](../../stories/phase-2/STORY-009-operator-crud.md) |
| API-026 | POST | /api/v1/operator-grants | Grant operator access to tenant | JWT (super_admin) | See [STORY-009](../../stories/phase-2/STORY-009-operator-crud.md) |
| API-027 | DELETE | /api/v1/operator-grants/:id | Revoke operator grant | JWT (super_admin) | See [STORY-009](../../stories/phase-2/STORY-009-operator-crud.md) |

## APNs (6 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-030 | GET | /api/v1/apns | List APNs | JWT (sim_manager+) | See [STORY-010](../../stories/phase-2/STORY-010-apn-crud.md) |
| API-031 | POST | /api/v1/apns | Create APN | JWT (tenant_admin+) | See [STORY-010](../../stories/phase-2/STORY-010-apn-crud.md) |
| API-032 | GET | /api/v1/apns/:id | Get APN detail + stats | JWT (sim_manager+) | See [STORY-010](../../stories/phase-2/STORY-010-apn-crud.md) |
| API-033 | PATCH | /api/v1/apns/:id | Update APN | JWT (tenant_admin+) | See [STORY-010](../../stories/phase-2/STORY-010-apn-crud.md) |
| API-034 | DELETE | /api/v1/apns/:id | Archive APN (soft-delete) | JWT (tenant_admin) | See [STORY-010](../../stories/phase-2/STORY-010-apn-crud.md) |
| API-035 | GET | /api/v1/apns/:id/sims | List SIMs on this APN | JWT (sim_manager+) | See [STORY-057](../../stories/phase-10/STORY-057-data-accuracy-endpoints.md) |

## SIMs (14 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-040 | GET | /api/v1/sims | List/search SIMs (cursor paged) | JWT (sim_manager+) | See [STORY-011](../../stories/phase-2/STORY-011-sim-crud.md) |
| API-041 | GET | /api/v1/sims/:id | Get SIM detail (full) | JWT (sim_manager+) | See [STORY-011](../../stories/phase-2/STORY-011-sim-crud.md) |
| API-042 | POST | /api/v1/sims | Create single SIM | JWT (sim_manager+) | See [STORY-011](../../stories/phase-2/STORY-011-sim-crud.md) |
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
| API-064 | POST | /api/v1/sims/bulk/state-change | Bulk state change on segment | JWT (sim_manager+) | See [STORY-030](../../stories/phase-5/STORY-030-bulk-operations.md) |
| API-065 | POST | /api/v1/sims/bulk/policy-assign | Bulk policy assign on segment. Job result includes CoA counters: `coa_sent_count`, `coa_acked_count`, `coa_failed_count` (omitted when 0). CoA dispatched outside distLock after release. | JWT (policy_editor+) | See [STORY-030](../../stories/phase-5/STORY-030-bulk-operations.md); CoA dispatch: [STORY-060](../../stories/phase-10/STORY-060-aaa-protocol-correctness.md) |
| API-066 | POST | /api/v1/sims/bulk/operator-switch | Bulk eSIM operator switch | JWT (tenant_admin) | See [STORY-030](../../stories/phase-5/STORY-030-bulk-operations.md) |

## eSIM (7 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-070 | GET | /api/v1/esim-profiles | List eSIM profiles (supports sim_id filter for multi-profile view) | JWT (sim_manager+) | See [STORY-028](../../stories/phase-5/STORY-028-esim-profiles.md) |
| API-071 | GET | /api/v1/esim-profiles/:id | Get eSIM profile detail | JWT (sim_manager+) | See [STORY-028](../../stories/phase-5/STORY-028-esim-profiles.md) |
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
| API-084 | GET | /api/v1/ip-pools/:id/addresses | List addresses in pool | JWT (operator_manager+) | See [STORY-010](../../stories/phase-2/STORY-010-apn-crud.md) (IP pool section) |
| API-085 | POST | /api/v1/ip-pools/:id/addresses/reserve | Reserve static IP for SIM | JWT (sim_manager+) | See [STORY-010](../../stories/phase-2/STORY-010-apn-crud.md) (IP pool section) |

## Policies (10 endpoints)

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
| API-099 | GET | /api/v1/policy-rollouts/:id | Get rollout status | JWT (policy_editor+) | See [STORY-025](../../stories/phase-4/STORY-025-policy-rollout.md) |

## Sessions (4 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-100 | GET | /api/v1/sessions | List active sessions | JWT (sim_manager+) | See [STORY-017](../../stories/phase-3/STORY-017-session-management.md) |
| API-101 | GET | /api/v1/sessions/stats | Real-time session statistics | JWT (analyst+) | See [STORY-017](../../stories/phase-3/STORY-017-session-management.md), [STORY-033](../../stories/phase-6/STORY-033-realtime-metrics.md) |
| API-102 | POST | /api/v1/sessions/:id/disconnect | Force disconnect session (CoA/DM) | JWT (sim_manager+) | See [STORY-017](../../stories/phase-3/STORY-017-session-management.md) |
| API-103 | POST | /api/v1/sessions/bulk/disconnect | Bulk disconnect on segment | JWT (tenant_admin) | See [STORY-017](../../stories/phase-3/STORY-017-session-management.md), [STORY-030](../../stories/phase-5/STORY-030-bulk-operations.md) |

## Analytics & CDR (6 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-110 | GET | /api/v1/dashboard | Tenant dashboard data (note: implementation uses `/dashboard`, not `/analytics/dashboard`; path corrected by compliance audit 2026-04-12) | JWT (any) | See [STORY-033](../../stories/phase-6/STORY-033-realtime-metrics.md) |
| API-111 | GET | /api/v1/analytics/usage | Usage analytics (time-series) | JWT (analyst+) | See [STORY-034](../../stories/phase-6/STORY-034-usage-analytics.md) |
| API-112 | GET | /api/v1/analytics/cost | Cost analytics + optimization | JWT (analyst+) | See [STORY-035](../../stories/phase-6/STORY-035-cost-analytics.md) |
| API-113 | GET | /api/v1/analytics/anomalies | Anomaly detection results | JWT (analyst+) | See [STORY-036](../../stories/phase-6/STORY-036-anomaly-detection.md) |
| API-114 | GET | /api/v1/cdrs | List CDRs (time-range query) | JWT (analyst+) | See [STORY-032](../../stories/phase-6/STORY-032-cdr-processing.md) |
| API-115 | POST | /api/v1/cdrs/export | Export CDRs to CSV | JWT (analyst+) | See [STORY-032](../../stories/phase-6/STORY-032-cdr-processing.md) |

## Jobs (5 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-120 | GET | /api/v1/jobs | List jobs | JWT (sim_manager+) | See [STORY-013](../../stories/phase-2/STORY-013-bulk-import.md), [STORY-031](../../stories/phase-5/STORY-031-job-runner.md) |
| API-121 | GET | /api/v1/jobs/:id | Get job detail + progress | JWT (sim_manager+) | See [STORY-013](../../stories/phase-2/STORY-013-bulk-import.md), [STORY-031](../../stories/phase-5/STORY-031-job-runner.md) |
| API-122 | POST | /api/v1/jobs/:id/cancel | Cancel running job | JWT (tenant_admin) | See [STORY-013](../../stories/phase-2/STORY-013-bulk-import.md), [STORY-031](../../stories/phase-5/STORY-031-job-runner.md) |
| API-123 | POST | /api/v1/jobs/:id/retry | Retry failed items | JWT (sim_manager+) | See [STORY-013](../../stories/phase-2/STORY-013-bulk-import.md), [STORY-031](../../stories/phase-5/STORY-031-job-runner.md) |
| API-124 | GET | /api/v1/jobs/:id/errors | Download job error report (JSON or CSV) | JWT (sim_manager+) | See [STORY-013](../../stories/phase-2/STORY-013-bulk-import.md) |

## Notifications (5 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-130 | GET | /api/v1/notifications | List notifications (unread first) | JWT (any) | See [STORY-038](../../stories/phase-7/STORY-038-notification-engine.md) |
| API-131 | PATCH | /api/v1/notifications/:id/read | Mark notification as read | JWT (any) | See [STORY-038](../../stories/phase-7/STORY-038-notification-engine.md) |
| API-132 | POST | /api/v1/notifications/read-all | Mark all as read | JWT (any) | See [STORY-038](../../stories/phase-7/STORY-038-notification-engine.md) |
| API-133 | GET | /api/v1/notification-configs | Get notification preferences | JWT (any) | See [STORY-038](../../stories/phase-7/STORY-038-notification-engine.md) |
| API-134 | PUT | /api/v1/notification-configs | Update notification preferences | JWT (any) | See [STORY-038](../../stories/phase-7/STORY-038-notification-engine.md) |

## Audit (4 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-140 | GET | /api/v1/audit-logs | Search audit logs | JWT (tenant_admin+) | See [STORY-007](../../stories/phase-1/STORY-007-audit-log.md) |
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
| API-170 | POST | /api/v1/sms/send | Send SMS to SIM | JWT (sim_manager+) | See [STORY-029](../../stories/phase-5/STORY-029-ota-sim.md) |
| API-171 | GET | /api/v1/sms/history | SMS delivery history | JWT (sim_manager+) | See [STORY-029](../../stories/phase-5/STORY-029-ota-sim.md) |

## Compliance & Data Governance (5 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-175 | GET | /api/v1/compliance/dashboard | Compliance dashboard: BTK/KVKK/GDPR status, pending DSARs, audit hash-chain health | JWT (tenant_admin+) | See [STORY-039](../../stories/phase-7/STORY-039-compliance-reporting.md) |
| API-176 | GET | /api/v1/compliance/btk-report | Monthly BTK SIM inventory report (query `?format=json\|csv\|pdf`) | JWT (tenant_admin+) | See [STORY-039](../../stories/phase-7/STORY-039-compliance-reporting.md), [STORY-059](../../stories/phase-10/STORY-059-security-compliance.md) (PDF format added) |
| API-177 | PUT | /api/v1/compliance/retention | Update tenant data retention policy (days per entity type) | JWT (tenant_admin) | See [STORY-039](../../stories/phase-7/STORY-039-compliance-reporting.md) |
| API-178 | GET | /api/v1/compliance/dsar/{simId} | Data Subject Access Request — export all PII for a SIM (KVKK/GDPR Art. 15) | JWT (tenant_admin+) | See [STORY-039](../../stories/phase-7/STORY-039-compliance-reporting.md) |
| API-179 | POST | /api/v1/compliance/erasure/{simId} | Right to Erasure — pseudonymize audit logs + purge PII (KVKK/GDPR Art. 17) | JWT (tenant_admin) | See [STORY-039](../../stories/phase-7/STORY-039-compliance-reporting.md), [STORY-059](../../stories/phase-10/STORY-059-security-compliance.md) (salted hash unification) |

## SLA Reports (2 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-183 | GET | /api/v1/sla-reports | List SLA reports (cursor-paginated) | JWT (tenant_admin+) | See [STORY-063](../../stories/phase-10/STORY-063-backend-completeness.md) |
| API-184 | GET | /api/v1/sla-reports/{id} | Get single SLA report detail | JWT (tenant_admin+) | See [STORY-063](../../stories/phase-10/STORY-063-backend-completeness.md) |

## Notifications — SMS Webhook (1 endpoint)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-185 | POST | /api/v1/notifications/sms/status | Twilio SMS delivery status callback (HMAC-SHA256 verified) | Twilio Signature | See [STORY-063](../../stories/phase-10/STORY-063-backend-completeness.md) |

## System Health (10 endpoints)

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

**Total: 118 REST endpoints + 10 WebSocket event types**

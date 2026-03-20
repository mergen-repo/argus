# API Index — Argus

> **Implementation note:** Full request/response specifications are in the story files (`docs/stories/phase-N/STORY-NNN.md`) under the "API Contract" section. For implementation, read both this index AND the relevant story. Error codes and envelope format are documented in [ERROR_CODES.md](../ERROR_CODES.md).

> Base URL: `/api/v1`
> Auth: JWT (portal), API Key (M2M), OAuth2 (3rd party)
> Response format: Standard envelope `{ status, data, meta?, error? }`
> Pagination: Cursor-based (default 50/page)

## Auth & Users (8 endpoints)

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
| API-035 | GET | /api/v1/apns/:id/sims | List SIMs on this APN | JWT (sim_manager+) | See [STORY-011](../../stories/phase-2/STORY-011-sim-crud.md) (requires SIM store) |

## SIMs (14 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-040 | GET | /api/v1/sims | List/search SIMs (cursor paged) | JWT (sim_manager+) | See [STORY-011](../../stories/phase-2/STORY-011-sim-crud.md) |
| API-041 | GET | /api/v1/sims/:id | Get SIM detail (full) | JWT (sim_manager+) | See [STORY-011](../../stories/phase-2/STORY-011-sim-crud.md) |
| API-042 | POST | /api/v1/sims | Create single SIM | JWT (sim_manager+) | See [STORY-011](../../stories/phase-2/STORY-011-sim-crud.md) |
| API-043 | PATCH | /api/v1/sims/:id | Update SIM metadata | JWT (sim_manager+) | See [STORY-011](../../stories/phase-2/STORY-011-sim-crud.md) |
| API-044 | POST | /api/v1/sims/:id/activate | Activate SIM | JWT (sim_manager+) | See [STORY-011](../../stories/phase-2/STORY-011-sim-crud.md) |
| API-045 | POST | /api/v1/sims/:id/suspend | Suspend SIM | JWT (sim_manager+) | See [STORY-011](../../stories/phase-2/STORY-011-sim-crud.md) |
| API-046 | POST | /api/v1/sims/:id/resume | Resume suspended SIM | JWT (sim_manager+) | See [STORY-011](../../stories/phase-2/STORY-011-sim-crud.md) |
| API-047 | POST | /api/v1/sims/:id/terminate | Terminate SIM | JWT (tenant_admin) | See [STORY-011](../../stories/phase-2/STORY-011-sim-crud.md) |
| API-048 | POST | /api/v1/sims/:id/report-lost | Report SIM stolen/lost | JWT (sim_manager+) | See [STORY-011](../../stories/phase-2/STORY-011-sim-crud.md) |
| API-049 | POST | /api/v1/sims/:id/diagnose | Run connectivity diagnostics | JWT (sim_manager+) | See [STORY-037](../../stories/phase-6/STORY-037-connectivity-diagnostics.md) |
| API-050 | GET | /api/v1/sims/:id/history | Get SIM state history | JWT (sim_manager+) | See [STORY-011](../../stories/phase-2/STORY-011-sim-crud.md) |
| API-051 | GET | /api/v1/sims/:id/sessions | Get SIM session history | JWT (sim_manager+) | See [STORY-017](../../stories/phase-3/STORY-017-session-management.md) |
| API-052 | GET | /api/v1/sims/:id/usage | Get SIM usage analytics | JWT (analyst+) | See [STORY-034](../../stories/phase-6/STORY-034-usage-analytics.md) |
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
| API-065 | POST | /api/v1/sims/bulk/policy-assign | Bulk policy assign on segment | JWT (policy_editor+) | See [STORY-030](../../stories/phase-5/STORY-030-bulk-operations.md) |
| API-066 | POST | /api/v1/sims/bulk/operator-switch | Bulk eSIM operator switch | JWT (tenant_admin) | See [STORY-030](../../stories/phase-5/STORY-030-bulk-operations.md) |

## eSIM (5 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-070 | GET | /api/v1/esim-profiles | List eSIM profiles | JWT (sim_manager+) | See [STORY-028](../../stories/phase-5/STORY-028-esim-profiles.md) |
| API-071 | GET | /api/v1/esim-profiles/:id | Get eSIM profile detail | JWT (sim_manager+) | See [STORY-028](../../stories/phase-5/STORY-028-esim-profiles.md) |
| API-072 | POST | /api/v1/esim-profiles/:id/enable | Enable eSIM profile | JWT (sim_manager+) | See [STORY-028](../../stories/phase-5/STORY-028-esim-profiles.md) |
| API-073 | POST | /api/v1/esim-profiles/:id/disable | Disable eSIM profile | JWT (sim_manager+) | See [STORY-028](../../stories/phase-5/STORY-028-esim-profiles.md) |
| API-074 | POST | /api/v1/esim-profiles/:id/switch | Switch to different operator profile | JWT (sim_manager+) | See [STORY-028](../../stories/phase-5/STORY-028-esim-profiles.md) |

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
| API-101 | GET | /api/v1/sessions/stats | Real-time session statistics | JWT (analyst+) | See [STORY-033](../../stories/phase-6/STORY-033-realtime-metrics.md) |
| API-102 | POST | /api/v1/sessions/:id/disconnect | Force disconnect session (CoA/DM) | JWT (sim_manager+) | See [STORY-017](../../stories/phase-3/STORY-017-session-management.md) |
| API-103 | POST | /api/v1/sessions/bulk/disconnect | Bulk disconnect on segment | JWT (tenant_admin) | See [STORY-030](../../stories/phase-5/STORY-030-bulk-operations.md) |

## Analytics & CDR (6 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-110 | GET | /api/v1/analytics/dashboard | Tenant dashboard data | JWT (any) | See [STORY-033](../../stories/phase-6/STORY-033-realtime-metrics.md) |
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

## Audit (3 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-140 | GET | /api/v1/audit-logs | Search audit logs | JWT (tenant_admin+) | See [STORY-007](../../stories/phase-1/STORY-007-audit-log.md) |
| API-141 | GET | /api/v1/audit-logs/verify | Verify hash chain integrity | JWT (tenant_admin+) | See [STORY-007](../../stories/phase-1/STORY-007-audit-log.md) |
| API-142 | POST | /api/v1/audit-logs/export | Export audit logs (date range) | JWT (tenant_admin+) | See [STORY-007](../../stories/phase-1/STORY-007-audit-log.md) |

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

## System Health (3 endpoints)

| ID | Method | Path | Description | Auth | Detail |
|----|--------|------|-------------|------|--------|
| API-180 | GET | /api/health | Health check (DB, Redis, NATS, AAA) | None | See [STORY-001](../../stories/phase-1/STORY-001-project-scaffold.md) |
| API-181 | GET | /api/v1/system/metrics | Built-in metrics (auth/s, latency, sessions) | JWT (super_admin) | See [STORY-033](../../stories/phase-6/STORY-033-realtime-metrics.md) |
| API-182 | GET | /api/v1/system/config | System configuration | JWT (super_admin) | See [STORY-001](../../stories/phase-1/STORY-001-project-scaffold.md) |

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

**Total: 108 REST endpoints + 10 WebSocket event types**

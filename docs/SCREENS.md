# Screen Index — Argus

> Total: 22 screens (+ 4 SIM detail tabs)
> Pattern Library: [screens/_patterns.md](screens/_patterns.md)
> Design: Dark-first, data-dense, group-first UX, premium visual quality

| ID | Screen | Module | Route | Auth |
|----|--------|--------|-------|------|
| SCR-001 | [Login](screens/SCR-001-login.md) | Auth | /login | None |
| SCR-002 | [2FA Verification](screens/SCR-002-2fa.md) | Auth | /login/2fa | Partial |
| SCR-003 | [Onboarding Wizard](screens/SCR-003-onboarding.md) | Auth | /setup | JWT |
| SCR-010 | [Main Dashboard](screens/SCR-010-dashboard.md) | Dashboard | / | JWT (any) |
| SCR-011 | [Analytics — Usage](screens/SCR-011-analytics-usage.md) | Analytics | /analytics | JWT (analyst+) |
| SCR-012 | [Analytics — Cost](screens/SCR-012-analytics-cost.md) | Analytics | /analytics/cost | JWT (analyst+) |
| SCR-013 | [Analytics — Anomalies](screens/SCR-013-analytics-anomalies.md) | Analytics | /analytics/anomalies | JWT (analyst+) |
| SCR-020 | [SIM List](screens/SCR-020-sim-list.md) | SIM | /sims | JWT (sim_manager+) |
| SCR-021 | [SIM Detail — Overview](screens/SCR-021-sim-detail.md) | SIM | /sims/:id | JWT (sim_manager+) |
| SCR-021b | [SIM Detail — Sessions](screens/SCR-021b-sim-sessions.md) | SIM | /sims/:id#sessions | JWT (sim_manager+) |
| SCR-021c | [SIM Detail — Usage](screens/SCR-021c-sim-usage.md) | SIM | /sims/:id#usage | JWT (sim_manager+) |
| SCR-021d | [SIM Detail — Diagnostics](screens/SCR-021d-sim-diagnostics.md) | SIM | /sims/:id#diagnostics | JWT (sim_manager+) |
| SCR-021e | [SIM Detail — History](screens/SCR-021e-sim-history.md) | SIM | /sims/:id#history | JWT (sim_manager+) |
| SCR-030 | [APN List](screens/SCR-030-apn-list.md) | APN | /apns | JWT (sim_manager+) |
| SCR-032 | [APN Detail](screens/SCR-032-apn-detail.md) | APN | /apns/:id | JWT (sim_manager+) |
| SCR-040 | [Operator List](screens/SCR-040-operator-list.md) | Operator | /operators | JWT (op_manager+) |
| SCR-041 | [Operator Detail](screens/SCR-041-operator-detail.md) | Operator | /operators/:id | JWT (op_manager+) |
| SCR-050 | [Live Sessions](screens/SCR-050-session-list.md) | Sessions | /sessions | JWT (sim_manager+) |
| SCR-060 | [Policy List](screens/SCR-060-policy-list.md) | Policy | /policies | JWT (policy_editor+) |
| SCR-062 | [Policy Editor](screens/SCR-062-policy-editor.md) | Policy | /policies/:id | JWT (policy_editor+) |
| SCR-070 | [eSIM Profiles](screens/SCR-070-esim-list.md) | eSIM | /esim | JWT (sim_manager+) |
| SCR-080 | [Job List](screens/SCR-080-job-list.md) | Jobs | /jobs | JWT (sim_manager+) |
| SCR-090 | [Audit Log](screens/SCR-090-audit-log.md) | Audit | /audit | JWT (tenant_admin+) |
| SCR-100 | [Notifications](screens/SCR-100-notifications.md) | Notifications | /notifications | JWT (any) |
| SCR-110 | [Users & Roles](screens/SCR-110-settings-users.md) | Settings | /settings/users | JWT (tenant_admin+) |
| SCR-111 | [API Keys](screens/SCR-111-settings-apikeys.md) | Settings | /settings/api-keys | JWT (tenant_admin+) |
| SCR-112 | [IP Pools](screens/SCR-112-settings-ippools.md) | Settings | /settings/ip-pools | JWT (op_manager+) |
| SCR-113 | [Notification Config](screens/SCR-113-settings-notifications.md) | Settings | /settings/notifications | JWT (any) |
| SCR-120 | [System Health](screens/SCR-120-system-health.md) | System | /system/health | JWT (super_admin) |
| SCR-121 | [Tenant Management](screens/SCR-121-tenant-management.md) | System | /system/tenants | JWT (super_admin) |

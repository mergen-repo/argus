# Screen Index — Argus

> Total: 79 screens (+ 4 SIM detail tabs) — includes 4 added by STORY-068; 7 added by STORY-069; 2 added by STORY-071; 10 added by STORY-072; 12 added by STORY-073; 5 added by STORY-075; 2 added by STORY-077; 12 backfilled by audit 2026-04-17 (SCR-180..191); 1 added by FIX-214 (SCR-192)
> Note: SCR-130..134 are assigned to STORY-069 screens. STORY-072 ops screens use SCR-160..169. STORY-073 admin screens use SCR-140..149, SCR-152, SCR-153 (SCR-150/151 are reserved for STORY-071 Roaming Agreements).
> Pattern Library: [screens/_patterns.md](screens/_patterns.md)
> Design: Dark-first, data-dense, group-first UX, premium visual quality

| ID | Screen | Module | Route | Auth | Notes |
|----|--------|--------|-------|------|-------|
| SCR-001 | [Login](screens/SCR-001-login.md) | Auth | /login | None | |
| SCR-002 | [2FA Verification](screens/SCR-002-2fa.md) | Auth | /login/2fa | Partial | |
| SCR-003 | [Onboarding Wizard](screens/SCR-003-onboarding.md) | Auth | /setup | JWT | STORY-069: rebuilt as 5-step wizard (Tenant Profile / Operator / APN / SIM Import / Policy Setup) with localStorage resume |
| SCR-015 | 2FA Setup & Backup Codes | Auth/Security | /settings/security#2fa | JWT (any) | STORY-068 AC-4 |
| SCR-018 | Force Password Change | Auth | /auth/change-password | Partial (force-change) | STORY-068 AC-3 |
| SCR-019 | User Settings — Security Tab | Settings | /settings/security | JWT (any) | STORY-068 AC-3/4 |
| SCR-010 | [Main Dashboard](screens/SCR-010-dashboard.md) | Dashboard | / | JWT (any) | FIX-209: Recent Alerts panel (AlertFeed component) added; source chip next to severity badge; dashboard handler now reads from alertStore (was anomalyStore). FIX-213: Live Event Stream Drawer (right-side Sheet) refreshed — envelope-aware rows (title/message from bus.Envelope), sticky filter bar with catalog-driven chips (type/severity/entity/source/date), Severity Pill inline toggles, pause/resume + queue badge (N yeni olay), virtual scrolling >100 events, clickable entity nav (display_name → route), bytes chips for session.* types, Details link gated on meta.alert_id, Turkish UI chrome. |
| SCR-011 | [Analytics — Usage](screens/SCR-011-analytics-usage.md) | Analytics | /analytics | JWT (analyst+) | |
| SCR-012 | [Analytics — Cost](screens/SCR-012-analytics-cost.md) | Analytics | /analytics/cost | JWT (analyst+) | |
| SCR-013 | [Analytics — Anomalies](screens/SCR-013-analytics-anomalies.md) | Analytics | /analytics/anomalies | JWT (analyst+) | |
| SCR-020 | [SIM List](screens/SCR-020-sim-list.md) | SIM | /sims | JWT (sim_manager+) | |
| SCR-021 | [SIM Detail — Overview](screens/SCR-021-sim-detail.md) | SIM | /sims/:id | JWT (sim_manager+) | |
| SCR-021b | [SIM Detail — Sessions](screens/SCR-021b-sim-sessions.md) | SIM | /sims/:id#sessions | JWT (sim_manager+) | |
| SCR-021c | [SIM Detail — Usage](screens/SCR-021c-sim-usage.md) | SIM | /sims/:id#usage | JWT (sim_manager+) | |
| SCR-021d | [SIM Detail — Diagnostics](screens/SCR-021d-sim-diagnostics.md) | SIM | /sims/:id#diagnostics | JWT (sim_manager+) | |
| SCR-021e | [SIM Detail — History](screens/SCR-021e-sim-history.md) | SIM | /sims/:id#history | JWT (sim_manager+) | |
| SCR-030 | [APN List](screens/SCR-030-apn-list.md) | APN | /apns | JWT (sim_manager+) | |
| SCR-032 | [APN Detail](screens/SCR-032-apn-detail.md) | APN | /apns/:id | JWT (sim_manager+) | |
| SCR-040 | [Operator List](screens/SCR-040-operator-list.md) | Operator | /operators | JWT (op_manager+) | |
| SCR-041 | [Operator Detail](screens/SCR-041-operator-detail.md) | Operator | /operators/:id | JWT (op_manager+) | |
| SCR-050 | [Live Sessions](screens/SCR-050-session-list.md) | Sessions | /sessions | JWT (sim_manager+) | |
| SCR-060 | [Policy List](screens/SCR-060-policy-list.md) | Policy | /policies | JWT (policy_editor+) | |
| SCR-062 | [Policy Editor](screens/SCR-062-policy-editor.md) | Policy | /policies/:id | JWT (policy_editor+) | |
| SCR-070 | [eSIM Profiles](screens/SCR-070-esim-list.md) | eSIM | /esim | JWT (sim_manager+) | |
| SCR-080 | [Job List](screens/SCR-080-job-list.md) | Jobs | /jobs | JWT (sim_manager+) | |
| SCR-090 | [Audit Log](screens/SCR-090-audit-log.md) | Audit | /audit | JWT (tenant_admin+) | |
| SCR-100 | [Notifications](screens/SCR-100-notifications.md) | Notifications | /notifications | JWT (any) | |
| SCR-110 | [Users & Roles](screens/SCR-110-settings-users.md) | Settings | /settings/users | JWT (tenant_admin+) | |
| SCR-111 | [API Keys](screens/SCR-111-settings-apikeys.md) | Settings | /settings/api-keys | JWT (tenant_admin+) | IP whitelist per key (STORY-068 AC-5) |
| SCR-115 | Active Sessions | Settings | /settings/sessions | JWT (any) | STORY-068 AC-6 |
| SCR-112 | [IP Pools](screens/SCR-112-settings-ippools.md) | Settings | /settings/ip-pools | JWT (op_manager+) | |
| SCR-113 | [Notification Config](screens/SCR-113-settings-notifications.md) | Settings | /settings/notifications | JWT (any) | STORY-069: extended with Preferences matrix tab + Templates editor tab |
| SCR-120 | [System Health](screens/SCR-120-system-health.md) | System | /system/health | JWT (super_admin) | |
| SCR-121 | [Tenant Management](screens/SCR-121-tenant-management.md) | System | /system/tenants | JWT (super_admin) | |
| SCR-130 | Reports | Reporting | /reports | JWT (api_user+) | STORY-069 AC-2/3: on-demand generate + scheduled report table; format pdf/csv/xlsx |
| SCR-131 | Webhooks | Integrations | /settings/webhooks | JWT (tenant_admin+) | STORY-069 AC-5/6: webhook configs list + delivery slide-panel + retry button |
| SCR-132 | SMS Gateway | Communications | /sms | JWT (sim_manager+) | STORY-069 AC-12: send form + outbound history table |
| SCR-133 | Data Portability | Compliance | /compliance/data-portability | JWT (self or tenant_admin+) | STORY-069 AC-9: GDPR export request form + status |
| SCR-134 | Notification Preferences | Settings | /settings/notifications#preferences | JWT (tenant_admin+) | STORY-069 AC-7/8: preferences matrix + templates editor (tabs on SCR-113) |
| SCR-150 | Roaming Agreements List | Operator | /roaming-agreements | JWT (api_user+) | STORY-071: list with state/type badges, operator column, cursor pagination, empty state; New Agreement slide-panel; operator_manager can create |
| SCR-151 | Roaming Agreement Detail | Operator | /roaming-agreements/:id | JWT (api_user+) | STORY-071: SLA terms, cost terms, validity timeline progress bar, auto-renew checkbox, notes; operator_manager can update/terminate |
| SCR-160 | Ops Performance | Operations (SRE) | /ops/performance | JWT (super_admin) | STORY-072 SCR-130 alias: HTTP p50/p95/p99 latency trend, AAA auth rate, error rate sparklines; 15s polling + WS realtime invalidation |
| SCR-161 | Ops Errors | Operations (SRE) | /ops/errors | JWT (super_admin) | STORY-072 SCR-131 alias: Error rate histogram, top error codes table, 4xx/5xx breakdown; sourced from Ops Snapshot |
| SCR-162 | Ops AAA Traffic | Operations (SRE) | /ops/aaa-traffic | JWT (super_admin) | STORY-072 SCR-132 alias: AAA auth volume, active sessions gauge, success/failure ratio ring chart; WebSocket-fed realtime + 5s poll |
| SCR-163 | Ops Infra — NATS Panel | Operations (SRE) | /ops/infra#nats | JWT (super_admin) | STORY-072 SCR-133 alias: NATS stream bytes/consumers/pending; per-consumer lag list; sourced from Infra Health |
| SCR-164 | Ops Infra — DB Panel | Operations (SRE) | /ops/infra#db | JWT (super_admin) | STORY-072 SCR-134 alias: PG pool open/idle connections, acquired/wait duration counters; sourced from Infra Health |
| SCR-165 | Ops Infra — Redis Panel | Operations (SRE) | /ops/infra#redis | JWT (super_admin) | STORY-072 SCR-135 alias: Redis memory used, hit ratio, key-count; sourced from Infra Health |
| SCR-166 | Ops Job Queue | Operations (SRE) | /ops/jobs | JWT (super_admin) | STORY-072 SCR-136 alias: Job queue depth, running/queued/failed counts, recent job rows; sourced from /api/v1/jobs |
| SCR-167 | Ops Backup | Operations (SRE) | /ops/backup | JWT (super_admin) | STORY-072 SCR-137 alias: Last backup run status, size, checksum, S3 key; backup_runs table summary; sourced from /api/v1/system/backups |
| SCR-168 | Ops Deploys | Operations (SRE) | /ops/deploys | JWT (super_admin) | STORY-072 SCR-138 alias: Deployment history list (color/version/timestamp/initiator); sourced from /api/v1/system/deploys |
| SCR-169 | Ops Incidents Timeline | Operations (SRE) | /ops/incidents | JWT (super_admin) | STORY-072 SCR-139 alias: Severity-sorted merged anomaly+audit incident feed (LIMIT 200); sourced from API-238 |
| SCR-140 | Tenant Resource Dashboard | Admin | /admin/resources | JWT (super_admin) | STORY-073: per-tenant SIM/session/API-RPS/storage cards + sparkbars + table toggle |
| SCR-141 | Quota Breakdown | Admin | /admin/quotas | JWT (super_admin) | STORY-073: per-tenant quota progress bars with ok/warning/danger thresholds |
| SCR-142 | Cost by Tenant | Admin | /admin/cost | JWT (super_admin) | STORY-073: 6-month RADIUS/operator/SMS/storage cost breakdown table + sparklines |
| SCR-143 | Security Events | Admin | /admin/security-events | JWT (tenant_admin+) | STORY-073: auth failures, role changes, kill-switch activity; sourced from /audit?actions= |
| SCR-144 | Global Sessions | Admin | /admin/sessions | JWT (tenant_admin+) | STORY-073: all active sessions with idle timer, force-logout button; tenant_admin scoped |
| SCR-145 | API Key Usage | Admin | /admin/api-usage | JWT (super_admin) | STORY-073: per-key rate limit consumption bars, error rate, anomaly flag |
| SCR-146 | DSAR Queue | Admin | /admin/dsar | JWT (tenant_admin+) | STORY-073: data portability / KVKK purge / SIM erasure job queue with SLA countdown |
| SCR-147 | Compliance Overview | Admin | /admin/compliance | JWT (tenant_admin+) | STORY-073: compliance posture cards for read-only mode, quotas, audit trail, DSAR pipeline |
| SCR-148 | Purge History | Admin | /admin/purge-history | JWT (super_admin) | STORY-073: permanently purged SIM records with iccid/msisdn/tenant/actor/reason |
| SCR-149 | Kill Switches | Admin | /admin/kill-switches | JWT (super_admin) | STORY-073: 5 canonical circuit breakers with enable/disable slide-panel + reason field |
| SCR-152 | Maintenance Windows | Admin | /admin/maintenance | JWT (super_admin) | STORY-073: schedule/cancel maintenance windows with affected services + notify plan |
| SCR-153 | Delivery Channel Status | Admin | /admin/delivery | JWT (super_admin) | STORY-073: per-channel health cards (webhook/email/sms/in-app/telegram) with latency p50/p95/p99 |
| SCR-170 | Session Detail | Sessions | /sessions/:id | JWT (sim_manager+) | STORY-075: SoR/policy/quota/audit/alerts tabs + force-disconnect dialog |
| SCR-171 | User Detail | Settings | /settings/users/:id | JWT (tenant_admin+) | STORY-075: overview/activity/sessions/permissions/notifications tabs + unlock/reset/revoke actions |
| SCR-172 | Alert Detail | Alerts | /alerts/:id | JWT (sim_manager+) | STORY-075: overview/similar/audit tabs + ack/resolve/escalate dialogs |
| SCR-173 | Violation Detail | Violations | /violations/:id | JWT (sim_manager+) | STORY-075: overview/audit tabs + suspend_sim/escalate/dismiss dialogs |
| SCR-174 | Tenant Detail | System | /system/tenants/:id | JWT (super_admin) | STORY-075: AnimatedCounter stats, overview/audit/alerts tabs, super_admin guard |
| SCR-175 | Announcements | Admin | /admin/announcements | JWT (super_admin) | STORY-077: CRUD for system announcements (info/warning/critical), target all or specific tenant, starts_at/ends_at scheduling, dismissible flag |
| SCR-176 | Impersonate User | Admin | /admin/impersonate | JWT (super_admin) | STORY-077: user list with "Impersonate" button per row; triggers 1h read-only JWT + purple banner |
| SCR-180 | SIM Compare | SIM | /sims/compare | JWT (sim_manager+) | STORY-078/077: two-SIM side-by-side diff; `?sim_id_a=&sim_id_b=` pre-populate (F-4 = D-016 OPEN against STORY-079) |
| SCR-181 | Operator Compare | Operator | /operators/compare | JWT (operator_manager+) | STORY-077: two-operator side-by-side comparison |
| SCR-182 | Policy Compare | Policy | /policies/compare | JWT (policy_editor+) | STORY-077: two-policy version diff (DSL + metadata) |
| SCR-183 | Alerts List | Alerts | /alerts | JWT (sim_manager+) | STORY-075/077: alert feed with severity filters; drill-down to SCR-172. FIX-209: unified multi-source feed (sim/operator/infra/policy/system), Source chip per row, source filter param (valid enum required), backed by TBL-53 alerts table (was anomalies only). |
| SCR-184 | Violations List | Violations | /violations | JWT (sim_manager+) | STORY-070/075: policy violations list with acknowledge/remediate actions; drill-down to SCR-173 |
| SCR-185 | SLA Dashboard | Analytics/SLA | /sla | JWT (tenant_admin+) | STORY-072/063: operator SLA reports, uptime trend, violation events. FIX-215: rewritten as historical view — rolling-window segmented selector (3/6/12/24m), per-month summary cards (uptime%, breach minutes, incident count, sessions), PDF export button (`useSLAPDFDownload`), MonthDetail drawer (SCR-185a) with per-operator table, OperatorBreach drawer (SCR-185b) with per-breach rows + `affected_sessions_est`. SLANotAvailableError empty state. Dark-first. |
| SCR-185a | SLA Month Detail (drawer) | Analytics/SLA | /sla (drawer state) | JWT (tenant_admin+) | FIX-215: SlidePanel drawer showing all operators for a given calendar month — uptime%, breach_minutes, incident_count, SLA target, status pill. Opens from SCR-185 month card. |
| SCR-185b | SLA Operator Breach (drawer) | Analytics/SLA | /sla (drawer state) | JWT (tenant_admin+) | FIX-215: SlidePanel drawer showing individual breach events for one operator-month — start/end time, duration, type (down/latency), affected_sessions_est, totals row. Opens from SCR-185a operator row. |
| SCR-186 | Topology | System | /topology | JWT (super_admin) | STORY-072: live topology — tenants ↔ operators ↔ APNs ↔ pools with health tinting |
| SCR-187 | Capacity Planner | Analytics | /capacity | JWT (super_admin) | STORY-070: SIMs / sessions / auth-rate / monthly-growth vs `ARGUS_CAPACITY_*` targets |
| SCR-188 | Reports (ad-hoc + scheduled) | Reporting | /reports | JWT (api_user+) | STORY-069 AC-2/3: alias to SCR-130; route-level indexing for router parity |
| SCR-189 | Webhooks (list) | Integrations | /webhooks | JWT (tenant_admin+) | STORY-069 AC-5/6: alias to SCR-131 via `/webhooks` route; kept separate for router parity |
| SCR-190 | Knowledge Base | Settings | /settings/knowledgebase | JWT (any) | STORY-077: in-app help articles / troubleshooting guide |
| SCR-191 | Reliability | Settings | /settings/reliability | JWT (super_admin) | STORY-066: backup/restore history, PITR runbook link, JWT rotation status |
| SCR-192 | CDR Explorer | Analytics | /cdrs | JWT (analyst+) | FIX-214: filter bar (SIM/Operator/APN/record_type chip ToggleGroup/timeframe), 4 stat cards (Records/Unique SIMs/Unique Sessions/Total Bytes), infinite-scroll table with ICCID/IMSI/MSISDN/Operator/APN/record_type badge/Bytes/Timestamp, LIVE pip, row click → SessionTimelineDrawer (SlidePanel), export button → POST /api/v1/cdrs/export → toast, deep-link from /sessions/:id CDR button |

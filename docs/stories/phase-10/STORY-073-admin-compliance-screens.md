# STORY-073: Multi-Tenant Admin & Compliance Screens

## User Story
As a super_admin managing multiple enterprise tenants and a compliance officer ensuring regulatory posture, I want dedicated screens for per-tenant resource consumption, quota management, security event monitoring, DSAR queue, compliance posture, data purge history, kill switch controls, and notification delivery status, so that I can manage, audit, and secure the platform at scale.

## Description
Enterprise screen audit found 10+ SaaS-essential admin/compliance screens completely missing. Tenant page shows a flat list but no per-tenant drill-down. No security event feed. No DSAR queue. No compliance posture dashboard. No kill switch. No notification delivery ops board.

## Architecture Reference
- Services: SVC-01 (Gateway — admin), SVC-03 (Core API), SVC-08 (Notification), SVC-10 (Audit — compliance)
- Packages: web/src/pages/admin/* (new), internal/api/admin/* (new endpoints), internal/store/admin
- Source: Phase 10 enterprise screen audit (2026-04-11)

## Screen Reference
- SCR-140: Multi-Tenant Resource Dashboard (new)
- SCR-141: Tenant Quota Breakdown (expand SCR-121)
- SCR-142: Cost per Tenant (new)
- SCR-143: Security Event Feed (new)
- SCR-144: Active User Sessions (new)
- SCR-145: API Usage / Rate Limit Dashboard (new)
- SCR-146: DSAR Queue (new)
- SCR-147: Compliance Posture Dashboard (new)
- SCR-148: Data Purge History (new)
- SCR-149: Kill Switch / Degrade Mode (new)
- SCR-150: Maintenance Windows (new)
- SCR-151: Notification Delivery Status Board (new)

## Acceptance Criteria
- [ ] AC-1: **Multi-Tenant Resource Dashboard (SCR-140):** Per-tenant cards showing SIM count, API req/s, active sessions, CDR volume, storage used. Sortable by any metric. Spark trend per metric. Click tenant → tenant detail.
- [ ] AC-2: **Tenant Quota Breakdown (expand SCR-141):** Each tenant: max_sims vs current, max_apns vs current, max_users vs current, max_api_keys vs current. Progress bars with color thresholds (green < 80%, yellow < 95%, red ≥ 95%). "Approaching limit" warning banner. Click to edit limits.
- [ ] AC-3: **Cost per Tenant (SCR-142):** Monthly cost breakdown by operator × APN × RAT type. Trend chart (last 6 months). Top 10 cost contributors (SIMs). Budget alerts configuration (threshold → notification). Currency display per tenant config.
- [ ] AC-4: **Security Event Feed (SCR-143):** Real-time feed: failed logins, 2FA failures, account lockouts, API key abuse, IP blocks, privilege escalations, password resets. Severity badges. Filter by tenant/user/event type/time. Click event → audit detail. Auto-refresh via WS.
- [ ] AC-5: **Active User Sessions (SCR-144):** List of all active browser sessions: user, IP, device/browser (from UA), location (GeoIP lookup), login time, last activity, idle duration. "Force Logout" button per session. "Revoke All" per user. Tenant-scoped for tenant_admin, global for super_admin.
- [ ] AC-6: **API Usage Dashboard (SCR-145):** Per-API-key request rate (last 1h/24h/7d). Rate limit consumption (current/max). Top endpoints by API key. Error rate per key. Anomaly detection (sudden spike). Block/throttle controls per key.
- [ ] AC-7: **DSAR Queue (SCR-146):** List of pending GDPR/KVKK data subject access requests. Status: received → processing → completed → delivered. Assigned-to field. SLA timer (must respond within 30 days). Generate response button (triggers data export job). Audit trail per request.
- [ ] AC-8: **Compliance Posture Dashboard (SCR-147):** KVKK compliance score (% of requirements met). GDPR compliance score. BTK compliance score. Per-requirement checklist with status (compliant/non-compliant/partial). Retention health (expired data purged? pending purge count). Last audit date. Export posture report (PDF).
- [ ] AC-9: **Data Purge History (SCR-148):** Chronological list of all purge runs: date, tenant, records pseudonymized/deleted, retention policy applied, initiator (cron/manual/DSAR). Dry-run results for upcoming purges. "Run Purge Now" button (with confirmation).
- [ ] AC-10: **Kill Switch / Degrade Mode (SCR-149):** Toggle switches for: disable all RADIUS auth (emergency), disable new session creation, disable bulk operations, enable read-only mode, disable external notifications. Each toggle: confirmation dialog + reason field + audit entry. Current state badges visible. Recovery procedure link.
- [ ] AC-11: **Maintenance Windows (SCR-150):** Schedule: start/end time, affected services, notification plan. Active windows shown as banner in portal (STORY-077). History of past windows. Recurring schedule support.
- [ ] AC-12: **Notification Delivery Status Board (SCR-151):** Per-channel (email/SMS/webhook/telegram/in-app) success rate, failure rate, retry queue depth, last delivery timestamp. Failed delivery list with retry button. Delivery latency percentiles. Channel health indicator (green/yellow/red).
- [ ] AC-13: Sidebar "Admin" section (super_admin only) added with links to all new screens. Tenant-admin sees subset (quota, security events for own tenant, DSAR queue for own tenant).

## Dependencies
- Blocked by: STORY-068 (auth hardening — session management, audit logging), STORY-069 (DSAR, KVKK purge)
- Blocks: Phase 10 Gate

## Test Scenarios
- [ ] E2E: super_admin opens Multi-Tenant Resource → sees all tenants with SIM counts matching reality.
- [ ] E2E: Tenant at 95% SIM quota → yellow progress bar + "Approaching Limit" banner.
- [ ] E2E: 5 failed login attempts → Security Event Feed shows 5 entries with severity=HIGH.
- [ ] E2E: Admin clicks "Force Logout" on active session → session invalidated, user redirected to login.
- [ ] E2E: Create DSAR request → status "received" → trigger export → status "processing" → complete → "delivered".
- [ ] E2E: Toggle "disable bulk operations" kill switch → bulk SIM suspend returns 503 SERVICE_DEGRADED.
- [ ] E2E: Schedule maintenance window → banner appears for all users in affected tenant.

## Effort Estimate
- Size: L
- Complexity: Medium-High (many screens but most are read-heavy dashboard views)

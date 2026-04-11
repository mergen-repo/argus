# STORY-075: Cross-Entity Context & Detail Page Completeness

## User Story
As an enterprise operator, I want every entity detail page to show all related data inline — audit history, notifications, violations, related entities, quick actions — so that I never need to leave the current context to find connected information. One click from any ID to its detail page.

## Description
Cross-entity audit found 61 missing related-data views across 10 entity types. Every detail page is isolated — SIM detail doesn't show its audit log, policy detail doesn't show assigned SIMs, operator detail doesn't show connected APNs. 5 detail pages don't exist at all (session, user, alert, violation, tenant). Audit log entity_id is plain text, not clickable. This story makes Argus feel like Datadog/Linear: every entity is connected, every ID is a link.

## Architecture Reference
- Packages: web/src/components/shared/* (new reusable components), web/src/pages/*/detail.tsx (enrichment), internal/api/search (new global search endpoint)
- Source: Phase 10 cross-entity context audit (2026-04-11)

## Acceptance Criteria

### Shared Reusable Components (build once, embed N times)
- [ ] AC-1: **`<RelatedAuditTab entityId entityType />`** — embeds filtered audit log (last 20 entries, expandable, "View All" → `/audit?entity_id=X&entity_type=Y`). Uses existing `useAuditList` hook with `entity_id` filter. Reusable on: SIM, APN, Operator, Policy, User, Job detail pages.
- [ ] AC-2: **`<RelatedNotificationsPanel entityId />`** — shows notifications about this entity (filter by `resource_id`). Count badge + last 5 entries + "View All" link. Reusable on: SIM, Operator, Policy detail.
- [ ] AC-3: **`<RelatedAlertsPanel entityId entityType />`** — active + recent alerts scoped to entity. Severity badges, runbook link, ack button. Reusable on: SIM, Operator, APN detail.
- [ ] AC-4: **`<RelatedViolationsTab entityId />`** — policy violations WHERE sim_id or policy_id = entityId. Inline remediation: suspend SIM, review policy, dismiss, escalate. Reusable on: SIM, Policy detail.
- [ ] AC-5: **`<EntityLink entityType entityId label />`** — renders clickable link that navigates to entity detail. Auto-resolves route: `sim` → `/sims/:id`, `policy` → `/policies/:id`, `operator` → `/operators/:id`, `apn` → `/apns/:id`, `user` → `/settings/users/:id`, `job` → `/jobs/:id`, `session` → `/sessions/:id`. Fallback: plain text if route unknown. Used across: audit log, notification list, alert list, violation list, session list, job list, anywhere entity_id appears.
- [ ] AC-6: **`<CopyableId value label />`** — click-to-copy with checkmark feedback. For: ICCID, IMSI, MSISDN, IP, UUID, API key (masked + reveal). Mono font. Tooltip "Click to copy." Applied everywhere entity IDs appear.

### SIM Detail Enrichment (+8 views)
- [ ] AC-7: SIM detail adds tabs/sections: Related Audit (AC-1), Notifications (AC-2), Violations (AC-4), Anomalies (filter `/anomalies?sim_id=X`), Policy Assignment History (query `policy_assignments` + `policy_rollouts` for this SIM, timeline view), IP Allocation History (query `ip_addresses` WHERE `sim_id`), Current Policy Rule Preview (inline DSL snippet of matching rules from current policy version), Cost Attribution (monthly cost from CDR aggregate for this SIM).

### APN Detail Enrichment (+4 views)
- [ ] AC-8: APN detail adds: Policies Referencing (query policies WHERE DSL contains `apn = "this_apn"`), Related Audit (AC-1), CDR Aggregate (total bytes/sessions via this APN, top SIMs), Operators Hosting (operators linked via this APN's operator_id + grants).

### Operator Detail Enrichment (+6 views)
- [ ] AC-9: Operator detail adds: Connected SIMs Count + State Breakdown (pie chart + paginated list), Hosted APNs list, SLA Report Summary (from STORY-063 AC-4 SLA reports), Related Audit (AC-1), Tenant Grants (which tenants have access, SoR priority per grant), Active Alerts (AC-3), Cost Per Unit Summary.

### Policy Detail Enrichment (+6 views)
- [ ] AC-10: Policy detail adds: Assigned SIMs Count + Paginated List (by segment breakdown), Assignment History Timeline (who activated which version when), Violations By This Policy (AC-4), Related Segments (which segments use this policy), Related Audit (AC-1), Clone/Duplicate Button (create new policy from current version DSL), Export Button (JSON + DSL text download).

### NEW: Session Detail Page
- [ ] AC-11: `/sessions/:id` page showing: SIM (EntityLink), Operator, APN, NAS IP, RAT type, protocol, IP allocated, started_at, duration (live counter if active), bytes in/out, SoR Decision (which operator chosen + scoring breakdown), Policy Applied (which rules matched), Current Quota Usage Bar, Force Disconnect Button (if active), Related Sessions (same SIM, last 10), CDRs Generated (from this session), Anomaly Flags, Audit Log Entries for session lifecycle.

### NEW: User Detail Page
- [ ] AC-12: `/settings/users/:id` page showing: email, name, role, state, created_by, created_at, last_login, locale. Tabs: Activity Timeline (all audit entries WHERE actor_id = user_id, chronological), API Keys (owned keys with usage stats), Active Sessions (browser logins with IP/device/last_active + force-logout button), Permissions Matrix (role → resources → allowed actions grid), Notifications (sent to this user), Account Events (login, lockout, unlock, password reset, 2FA setup), 2FA State (enabled/disabled, backup codes remaining count).

### NEW: Alert Detail Page
- [ ] AC-13: `/alerts/:id` page showing: type, severity, triggered_at, resource (EntityLink), Current State (open/acked/resolved) with state transition history timeline, Ack/Resolve/Escalate buttons with note field, Comments Thread (chronological notes from team members), Runbook Link (clickable), Related SIMs/Operators/Policies Affected, Similar Alerts (same type in past 30 days), Resolution Actions (context-dependent: "Suspend SIM", "Restart Service", "Open Ticket").

### NEW: Violation Detail Page
- [ ] AC-14: `/violations/:id` page showing: type, severity, SIM (EntityLink), Policy (EntityLink), rule that triggered, Session (EntityLink), Occurred At, Current State (open/acknowledged/remediated/dismissed). Remediation Actions: "Suspend SIM" → confirmation → execute + audit, "Review Policy" → jump to policy editor at offending rule, "Dismiss" → mark acknowledged with reason, "Escalate" → create incident notification. Related Violations (same SIM/policy/rule in past 30 days). Timeline (when detected, when actioned, by whom).

### NEW: Tenant Detail Page (super_admin)
- [ ] AC-15: `/system/tenants/:id` page showing: name, slug, state, created_at, config. Dashboard cards: SIM count, APN count, User count, Operator count, Active Sessions, Monthly Cost, Storage Used. Quota utilization bars. Live Traffic sparkline. Recent Audit (AC-1 with tenant scope). Active Alerts for this tenant. SLA Compliance summary. Connected Operators with health status.

### Audit Log Entity Linking
- [ ] AC-16: Audit log table: `entity_id` column renders as `<EntityLink>` (clickable navigation to entity detail based on `entity_type`). `actor` column renders as `<EntityLink entityType="user">`. JSON diff "before/after" panel shows entity-specific labels (not raw JSON keys).

## Dependencies
- Blocked by: STORY-057 (API-051/052), STORY-063 (SLA reports, notification store), STORY-065 (metrics for SoR decision display), STORY-068 (user sessions, permissions matrix)
- Blocks: STORY-077 (UX polish builds on connected detail pages)

## Test Scenarios
- [ ] E2E: Open SIM detail → "Audit" tab shows last 20 entries for that SIM, expandable with JSON diff.
- [ ] E2E: Audit log → click entity_id "sim-abc-123" → navigates to `/sims/sim-abc-123`.
- [ ] E2E: Click ICCID on SIM detail → clipboard contains full ICCID, checkmark shows.
- [ ] E2E: Open Session detail → SoR Decision section shows operator scoring breakdown.
- [ ] E2E: Open User detail → Activity Timeline shows all actions by that user, chronological.
- [ ] E2E: Open Alert detail → click "Ack" → add note → state transitions to "acknowledged" → timeline updates.
- [ ] E2E: Open Violation detail → click "Suspend SIM" → confirmation → SIM state changes → audit entry created.
- [ ] E2E: super_admin opens `/system/tenants/abc` → quota bars show real values, live traffic sparkline.

## Effort Estimate
- Size: XL
- Complexity: High (6 shared components + 4 page enrichments + 5 new pages)

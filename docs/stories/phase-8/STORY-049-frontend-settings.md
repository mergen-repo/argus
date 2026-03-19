# STORY-049: Frontend Settings & System Pages

## User Story
As a tenant admin, I want settings pages for user management, API keys, IP pools, notification config, system health, and tenant management, so that I can administer the platform.

## Description
Settings pages with sub-navigation: Users & Roles (SCR-110), API Keys (SCR-111), IP Pools (SCR-112), Notification Config (SCR-113). System pages: System Health (SCR-120), Tenant Management (SCR-121). Role-based visibility: some pages only for tenant_admin, some only for super_admin.

## Architecture Reference
- Services: SVC-03 (Core API), SVC-01 (Gateway — RBAC)
- API Endpoints: API-006 to API-008, API-150 to API-154, API-080 to API-085, API-133 to API-134, API-181, API-010 to API-014
- Source: docs/architecture/api/_index.md (Auth, API Keys, IP Pools, Notifications, System sections)

## Screen Reference
- SCR-110: Users & Roles — user table with role, status, invite button, role editor
- SCR-111: API Keys — key table with scopes, rate limit, rotate/revoke actions, create dialog
- SCR-112: IP Pools — pool list with utilization bars, address table, reserve IP action
- SCR-113: Notification Config — channel toggles (email, Telegram, webhook, SMS), event subscriptions, threshold sliders
- SCR-120: System Health — service status cards, auth/s gauge, latency chart, session count, error rate
- SCR-121: Tenant Management — tenant table, create tenant dialog, tenant config panel

## Acceptance Criteria
- [ ] Settings sub-navigation: Users, API Keys, IP Pools, Notifications tabs
- [ ] Users page: user table with name, email, role, status, last login
- [ ] Users page: invite user button → dialog with email, role selection
- [ ] Users page: edit user role (dropdown), deactivate user
- [ ] Users page: visible to tenant_admin+ only
- [ ] API Keys page: key table with name, prefix (last 4 chars), scopes, rate limit, created, expires
- [ ] API Keys page: create key → dialog with name, scopes checkboxes, rate limit → show key once
- [ ] API Keys page: rotate key, revoke key (confirmation)
- [ ] IP Pools page: pool cards with name, CIDR, total, used, available, utilization bar
- [ ] IP Pools page: click pool → address table with IP, state (available/assigned/reserved), assigned SIM
- [ ] IP Pools page: reserve IP button → assign to specific SIM
- [ ] Notification Config page: channel toggles per delivery method
- [ ] Notification Config page: event subscription checkboxes (grouped by category)
- [ ] Notification Config page: threshold sliders (e.g., "Alert at 80% quota usage")
- [ ] System Health page: service status cards (DB, Redis, NATS, AAA — green/red)
- [ ] System Health page: auth/s gauge (real-time via WebSocket)
- [ ] System Health page: latency chart (p50, p95, p99 lines)
- [ ] System Health page: visible to super_admin only
- [ ] Tenant Management page: tenant table with name, SIM count, user count, plan
- [ ] Tenant Management page: create tenant dialog, edit tenant config (retention days, limits)
- [ ] Tenant Management page: visible to super_admin only

## Dependencies
- Blocked by: STORY-041 (scaffold), STORY-042 (auth), STORY-004 (RBAC), STORY-008 (API keys), STORY-033 (metrics)
- Blocks: None

## Test Scenarios
- [ ] Settings nav shows correct tabs for tenant_admin role
- [ ] Users page: invite user → email sent, user appears in table
- [ ] API Keys: create key → key shown once → copy to clipboard
- [ ] API Keys: rotate → new key generated, old invalidated
- [ ] IP Pools: utilization bar shows correct percentage
- [ ] IP Pools: reserve IP → IP assigned to SIM
- [ ] Notification Config: toggle email channel off → save → email disabled
- [ ] System Health: all services healthy → green indicators
- [ ] System Health: auth/s gauge updates via WebSocket
- [ ] Tenant Management: visible only to super_admin, 403 for others
- [ ] Tenant Management: create tenant → tenant appears in table

## Effort Estimate
- Size: XL
- Complexity: Medium

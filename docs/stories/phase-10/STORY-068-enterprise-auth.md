# STORY-068: Enterprise Auth & Access Control Hardening

## User Story
As a security-conscious enterprise customer, I want password complexity with history enforcement, forced password change flows, 2FA backup recovery codes, API key IP whitelisting, session revoke-all, enforced tenant resource limits, and audit logs on every mutation endpoint, so that Argus meets the access control controls required by ISO 27001, KVKK, and enterprise procurement checklists.

## Description
Current state: bcrypt cost is enforced but no password complexity policy, no password history, no force-change mechanism, no 2FA backup codes (lost device = locked out forever), API keys have no IP whitelist, no session revoke-all, tenant resource limit columns exist in schema but are not enforced at runtime, and 13 mutation endpoints lack audit log entries. This story closes every enterprise-grade access control gap.

## Architecture Reference
- Services: SVC-01 (Gateway — auth middleware), SVC-03 (Core API — user/api-key/session handlers)
- Packages: internal/auth, internal/api/auth, internal/api/user, internal/api/apikey, internal/gateway/api_key_auth, internal/store/user, internal/store/apikey, internal/audit, migrations
- Source: Phase 10 business logic audit + API coverage audit (6-agent scan 2026-04-11)

## Screen Reference
- SCR-011 (Login), SCR-015 (2FA Setup + Backup Codes), SCR-018 (Force Password Change), SCR-019 (User Settings — Security tab), SCR-114 (API Keys), SCR-115 (Active Sessions — revoke-all)

## Acceptance Criteria
- [ ] AC-1: **Password complexity policy.** Configurable via env vars:
  - `PASSWORD_MIN_LENGTH=12`
  - `PASSWORD_REQUIRE_UPPER=true`
  - `PASSWORD_REQUIRE_LOWER=true`
  - `PASSWORD_REQUIRE_DIGIT=true`
  - `PASSWORD_REQUIRE_SYMBOL=true`
  - `PASSWORD_MAX_REPEATING=3`
  Enforced at: user create, password change, admin reset, invite-complete. Error codes: `PASSWORD_TOO_SHORT`, `PASSWORD_MISSING_CLASS`, `PASSWORD_REPEATING_CHARS`.
- [ ] AC-2: **Password history.** New migration adds `password_history` table (`user_id`, `password_hash`, `created_at`). On password change, check last N (configurable `PASSWORD_HISTORY_COUNT=5`) hashes — reject reuse. Trim history to N entries after insert.
- [ ] AC-3: **Force password change flag.**
  - `users.password_change_required BOOLEAN DEFAULT false` column added
  - Set to `true` on: invited user first activation, admin-triggered reset, password expiry (configurable `PASSWORD_MAX_AGE_DAYS`)
  - Login with flag=true returns `partial: true` JWT + error `PASSWORD_CHANGE_REQUIRED`; user redirected to change-password screen; only password-change endpoint accessible
  - Flag cleared on successful change
- [ ] AC-4: **2FA backup/recovery codes.**
  - On 2FA setup: generate 10 single-use backup codes, display once, user confirms storage
  - Codes stored hashed (bcrypt) in `user_backup_codes` table (`user_id`, `code_hash`, `used_at`)
  - Login with 2FA: accept TOTP OR backup code; used code marked; warn when <3 remaining
  - Regenerate endpoint invalidates all previous codes and issues 10 new
- [ ] AC-5: **API key IP whitelisting.**
  - `api_keys.allowed_ips TEXT[]` column added (CIDR notation supported)
  - API key auth middleware rejects requests from non-whitelisted IPs with `API_KEY_IP_NOT_ALLOWED`
  - Frontend API Keys page adds IP whitelist editor (CIDR validation client-side + server-side)
  - Empty array = any IP (backwards compat)
- [ ] AC-6: **Session revoke-all.**
  - `POST /api/v1/users/:id/revoke-sessions` — admin endpoint (tenant_admin or self)
  - Invalidates all refresh tokens for user; forces re-login
  - Optional scope: `?include_api_keys=true` also revokes keys
  - Audit entry created
  - Active WebSocket connections for that user dropped (via hub user lookup)
- [ ] AC-7: **Admin force-logout all users.**
  - `POST /api/v1/system/revoke-all-sessions?tenant=X` — super_admin only (or tenant_admin scoped to own tenant)
  - Emergency breach response tool
  - Logs audit + notifies affected users (if email configured)
- [ ] AC-8: **Tenant resource limits enforced.**
  - Middleware intercepts create operations: SIM, APN, user, api_key
  - Reads `tenants.max_sims`, `max_apns`, `max_users`, `max_api_keys` from DB (cached 5min)
  - Current counts from store CountByTenant methods
  - Rejects with `TENANT_LIMIT_EXCEEDED` + current/max/resource fields in error payload
  - Limits 0 = unlimited (backwards compat)
- [ ] AC-9: **Audit logging on 13 mutation endpoints** that currently lack it (from API coverage audit):
  - `POST /api/v1/cdrs/export`
  - `POST /api/v1/compliance/erasure/:sim_id`
  - `POST /api/v1/msisdn-pool/import`
  - `POST /api/v1/msisdn-pool/:id/assign`
  - `PATCH /api/v1/analytics/anomalies/:id`
  - `PUT /api/v1/compliance/retention`
  - `POST/DELETE /api/v1/segments/*`
  - `POST /api/v1/notification-configs/*`
  - `POST /api/v1/jobs/:id/cancel`
  - `POST /api/v1/jobs/:id/retry`
  - `POST /api/v1/apikeys/:id/rotate` (new from AC-5 IP list update)
  - `POST /api/v1/users/:id/revoke-sessions` (new from AC-6)
  - `POST /api/v1/system/revoke-all-sessions` (new from AC-7)
  - Each creates audit entry via `auditor.Record(ctx, action, entityType, entityID, before, after)`
- [ ] AC-10: **Account lockout + unlock workflow.**
  - After N failed logins (configurable `LOGIN_MAX_ATTEMPTS=5`), account locked for `LOGIN_LOCKOUT_DURATION=15m`
  - Locked accounts get clear error code `ACCOUNT_LOCKED` with retry-after
  - Tenant admin can manually unlock via `POST /api/v1/users/:id/unlock`
  - Auto-unlock on lockout expiry
  - Audit entries on lock + unlock

## Dependencies
- Blocked by: STORY-063 (notification service for email/backup code delivery)
- Blocks: Phase 10 Gate, production deployment

## Test Scenarios
- [ ] Unit: Password "short1A!" rejected (<12 chars). "ValidLongPass1!" accepted.
- [ ] Unit: Same password used twice → reject on 2nd change (history).
- [ ] Integration: Admin invites user → first login returns PASSWORD_CHANGE_REQUIRED → change password → full JWT issued.
- [ ] Integration: 2FA setup → 10 codes displayed → login with code → marked used → same code fails on retry.
- [ ] Integration: Create API key with `allowed_ips=["192.168.1.0/24"]` → call from 10.0.0.1 → 403 API_KEY_IP_NOT_ALLOWED. From 192.168.1.5 → 200.
- [ ] Integration: Revoke sessions → user's refresh token invalidated → /auth/refresh returns 401.
- [ ] Integration: Super-admin revoke-all-sessions for tenantX → all tenantX users logged out, notification sent, audit created.
- [ ] Integration: Tenant with max_sims=10 already has 10 SIMs → POST /sims returns 422 TENANT_LIMIT_EXCEEDED with current=10 max=10.
- [ ] Integration: Every listed mutation endpoint produces an audit_logs entry verifiable via GET /audit.
- [ ] Integration: 5 failed logins → 6th returns ACCOUNT_LOCKED. Wait 15min → succeed.

## Effort Estimate
- Size: L
- Complexity: Medium (lots of small features, all well-understood)

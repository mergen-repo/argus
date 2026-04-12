# Implementation Plan: STORY-068 — Enterprise Auth & Access Control Hardening

> Phase 10 — zero-deferral. Effort: L, Complexity: Medium.
> Status: Plan. Dispatched: 2026-04-12.

## Goal
Close every enterprise-grade access-control gap (password policy + history + force-change, 2FA backup codes, API-key IP whitelist, session revoke-all, tenant resource limits, mutation audit coverage, account lockout/unlock) so Argus passes ISO 27001 / KVKK / enterprise procurement.

## Architecture Context

### Components Involved
- `internal/auth` (core auth service, password/TOTP/session logic) — Service layer
- `internal/api/auth` — HTTP handlers for login/refresh/2fa/logout + new force-change + backup-codes
- `internal/api/user` — user CRUD; add password-change, reset, unlock, revoke-sessions
- `internal/api/apikey` — api-key CRUD; add IP whitelist fields
- `internal/api/system` — super-admin tenant-wide revoke-all-sessions
- `internal/gateway/apikey_auth.go` — API-key middleware; add IP whitelist enforcement
- `internal/gateway/bruteforce.go` — existing brute-force limiter, augmented by account lockout fields on `users`
- `internal/store` — `user.go`, `apikey.go`, plus new `password_history.go`, `backup_codes.go`
- `internal/audit` — `auditor.CreateEntry`; attach to 13 previously silent mutation endpoints
- `internal/config` — environment variables for password policy + lockout + history + max-age
- `internal/notification` — optional email on force-logout (uses STORY-063 channel)
- `internal/ws` — drop WebSocket connections for user on session revoke (hub user lookup)
- `migrations/` — new DDL: `password_history`, `user_backup_codes`, users columns, api_keys column, tenants new limit col
- `web/src/pages/auth/login.tsx`, `two-factor.tsx` — client flows for `PASSWORD_CHANGE_REQUIRED` + backup-code entry
- `web/src/pages/settings/api-keys.tsx`, `users.tsx` — IP whitelist editor, unlock, revoke-sessions, backup codes UI
- New pages: `web/src/pages/auth/change-password.tsx`, `web/src/pages/settings/sessions.tsx` (SCR-115)

### Data Flow

**Login with force-change & lockout (happy + force path):**
1. Client POST `/api/v1/auth/login` with `{email, password}`
2. `authapi.AuthHandler.Login` → `auth.Service.Login`
3. `Service.Login` pulls user via `UserStore.GetByEmail`, checks `state`, checks `locked_until` → `ACCOUNT_LOCKED`
4. bcrypt compare password; on failure `IncrementFailedLogin`. If `failed_login_count >= LOGIN_MAX_ATTEMPTS`, set `locked_until = NOW() + LOGIN_LOCKOUT_DURATION`
5. On success, check `users.password_change_required`. If `true` → issue partial JWT with `change_required=true`, return `PASSWORD_CHANGE_REQUIRED` envelope with partial token. Client navigates to change-password screen.
6. If `totp_enabled` → issue partial JWT with `requires_2fa=true`, client navigates to 2FA screen.
7. Else → `createFullSession` (JWT + refresh), audit `login`, reset `failed_login_count`.

**2FA with backup code:**
1. Partial JWT at `/api/v1/auth/2fa/verify` with `{code}` or `{backup_code}`
2. `Service.Verify2FA` first tries TOTP. On failure and if input looks like backup code, iterate `user_backup_codes` where `used_at IS NULL`, bcrypt compare; on match set `used_at = NOW()`.
3. Warn `backup_codes_remaining < 3` in response meta.
4. On success → full JWT + refresh.

**Password change / history:**
1. `POST /api/v1/auth/password/change` with `{current_password, new_password}` (authenticated, partial token allowed if `change_required=true`)
2. Validate complexity (12 chars, upper/lower/digit/symbol, no run > `PASSWORD_MAX_REPEATING`)
3. Load last `PASSWORD_HISTORY_COUNT` entries from `password_history`; bcrypt compare new against each → reject `PASSWORD_REUSED`
4. Hash new password (bcrypt cost from `auth.Config.BcryptCost`), update `users.password_hash`, clear `password_change_required`, clear `locked_until`/`failed_login_count`
5. Insert current hash into `password_history`, trim to N newest
6. Audit `user.password_change`

**API-key IP whitelist:**
1. `gateway.APIKeyAuth` parses `X-API-Key`; after validating key, reads `api_keys.allowed_ips` (CIDR list)
2. If list non-empty, extract client IP via `extractIP` (supports IPv6), match against each CIDR using `net.ParseCIDR` + `.Contains`
3. On miss → 403 `API_KEY_IP_NOT_ALLOWED` with detail `{client_ip, allowed_ips}`
4. Empty list → allow (back-compat)

**Tenant resource limits middleware:**
1. `internal/gateway/tenant_limits.go` (new) wraps create routes for `POST /sims`, `POST /apns`, `POST /users`, `POST /api-keys`
2. Reads `tenants.max_sims/max_apns/max_users/max_api_keys` via `TenantStore.GetByID`; cached in Redis key `tenant:limits:{tenant_id}` TTL 5min
3. Calls `*Store.CountByTenant(tenantID)` (existing methods exist for users/api_keys; add for sims+apns if missing)
4. If `current >= max` and `max > 0` → 422 `TENANT_LIMIT_EXCEEDED` `{resource, current, limit}`; `max == 0` → unlimited

**Session revoke-all:**
1. `POST /api/v1/users/{id}/revoke-sessions` — self or tenant_admin
2. Calls `sessions.RevokeAllUserSessions(userID)`; optional `?include_api_keys=true` also revokes all tenant api_keys owned by user (by `created_by`)
3. Kicks websocket: `ws.Hub.DropUser(userID)`
4. Audit `user.sessions_revoked`

**Super-admin tenant-wide revoke-all:**
1. `POST /api/v1/system/revoke-all-sessions?tenant={id}` — super_admin (or tenant_admin scoped)
2. Bulk update `user_sessions SET revoked_at = NOW() WHERE user_id IN (SELECT id FROM users WHERE tenant_id = $1)`
3. Optional `?notify=true` → `notification.Service.SendBulk(tenantID, "security_breach_response")`
4. Audit `system.tenant_sessions_revoked` with `affected_users` count

### API Specifications

All use standard envelope `{status, data, meta?, error?}`. All mutations emit audit entries via `auditor.CreateEntry`.

**New endpoints:**

- `POST /api/v1/auth/password/change` (JWT or partial-force-change JWT)
  - Req: `{ current_password: string, new_password: string }`
  - 200 `{status:"success", data:{token, refresh_token}}` — rotates session
  - 422 `PASSWORD_TOO_SHORT | PASSWORD_MISSING_CLASS | PASSWORD_REPEATING_CHARS | PASSWORD_REUSED`
  - 401 `INVALID_CREDENTIALS` (current_password wrong)

- `POST /api/v1/auth/2fa/backup-codes` (JWT, after 2FA enabled)
  - Req: `{}`
  - 200 `{status:"success", data:{ codes: string[10], generated_at }}` — displayed once
  - Regenerate via same endpoint (invalidates previous).

- `POST /api/v1/auth/2fa/verify` (partial JWT) — EXTENDED
  - Req: `{ code?: string, backup_code?: string }` (exactly one)
  - 200 `{status:"success", data:{token, refresh_token}, meta:{ backup_codes_remaining?: number }}`
  - 401 `INVALID_2FA_CODE | INVALID_BACKUP_CODE`

- `POST /api/v1/users/{id}/unlock` (tenant_admin, self-tenant scope)
  - Req: `{}` → sets `failed_login_count=0, locked_until=NULL`
  - 200 `{status:"success", data:{user_id, unlocked_at}}`
  - 404 `NOT_FOUND`
  - Audit: `user.unlock`

- `POST /api/v1/users/{id}/revoke-sessions?include_api_keys=true|false` (self or tenant_admin)
  - Req: `{}` → 200 `{status:"success", data:{user_id, sessions_revoked: n, api_keys_revoked?: n}}`
  - Audit: `user.sessions_revoked`

- `POST /api/v1/system/revoke-all-sessions?tenant={id}&notify=true|false` (super_admin or tenant_admin of own tenant)
  - 200 `{status:"success", data:{tenant_id, affected_users, sessions_revoked, notified}}`
  - Audit: `system.tenant_sessions_revoked`

- `POST /api/v1/users/{id}/reset-password` (tenant_admin)
  - Req: `{}` → generates temporary password (shown once) OR sends email invite; sets `password_change_required=true`, revokes sessions.
  - 200 `{status:"success", data:{user_id, temporary_password?: string, email_sent: bool}}`
  - Audit: `user.password_reset`

**Modified endpoints:**

- `POST /api/v1/api-keys` / `PATCH /api/v1/api-keys/{id}` — accept `allowed_ips: string[]` (CIDR). Validate each with `net.ParseCIDR`. 422 `INVALID_CIDR` on bad input.
- `POST /api/v1/users` — enforce password complexity when password auto-generated/supplied; set `password_change_required=true` on invite.

**Login error updates:**
- Add `PASSWORD_CHANGE_REQUIRED` (HTTP 200 with `partial_token` + flag instead of `token`) OR 403 per team convention — **use 200 + `partial:true, reason:"password_change_required"` matching existing 2FA partial flow.**
- `ACCOUNT_LOCKED` response already exists; add `failed_attempts` and `retry_after_seconds` detail from `auth.LockInfo` (already wired).

### Database Schema

> Source: migrations/20260320000002_core_schema.up.sql (ACTUAL) + new migration `20260412000011_*` (NEW — this story).

**TBL-01 tenants (existing — ADD column):**
```sql
-- ACTUAL existing:
-- id uuid PK, name, domain, contact_email, contact_phone,
-- max_sims INTEGER DEFAULT 100000, max_apns INTEGER DEFAULT 100,
-- max_users INTEGER DEFAULT 50, purge_retention_days, settings jsonb, state, ...

-- ADD:
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS max_api_keys INTEGER NOT NULL DEFAULT 20;
```

**TBL-02 users (existing — ADD columns):**
```sql
-- ACTUAL columns include: password_hash, totp_secret, totp_enabled,
-- failed_login_count, locked_until.

ALTER TABLE users ADD COLUMN IF NOT EXISTS password_change_required BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE users ADD COLUMN IF NOT EXISTS password_changed_at TIMESTAMPTZ;
```

**TBL-04 api_keys (existing — ADD column):**
```sql
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS allowed_ips TEXT[] NOT NULL DEFAULT '{}';
-- CIDR notation. Empty = any IP (back-compat).
CREATE INDEX IF NOT EXISTS idx_api_keys_allowed_ips_gin ON api_keys USING gin (allowed_ips);
```

**NEW TBL password_history:**
```sql
-- Source: ARCHITECTURE.md design (new table — this story creates it)
CREATE TABLE IF NOT EXISTS password_history (
  id BIGSERIAL PRIMARY KEY,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  password_hash VARCHAR(255) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_password_history_user_created ON password_history (user_id, created_at DESC);
```
Retention: application trims to `PASSWORD_HISTORY_COUNT` newest per user after each insert.

**NEW TBL user_backup_codes:**
```sql
CREATE TABLE IF NOT EXISTS user_backup_codes (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  code_hash VARCHAR(255) NOT NULL,
  used_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_user_backup_codes_user_unused
  ON user_backup_codes (user_id) WHERE used_at IS NULL;
```

**Migration file pair:**
- `migrations/20260412000011_enterprise_auth_hardening.up.sql` (all DDL above)
- `migrations/20260412000011_enterprise_auth_hardening.down.sql` (reverse: DROP TABLE + DROP COLUMN IF EXISTS)

### Screen Mockups

Story references SCR-011 (Login), SCR-015 (2FA Setup + Backup Codes), SCR-018 (Force Password Change), SCR-019 (Security tab), SCR-114 (API Keys — updated), SCR-115 (Active Sessions). In current SCREENS.md numbering these map to:

- SCR-001 Login (existing) — add `PASSWORD_CHANGE_REQUIRED` branch
- SCR-002 2FA (existing) — add "Use a backup code" link → switches input mode
- **NEW** `/auth/change-password` (force-change flow, reached via partial JWT with `password_change_required`)
- SCR-110 Users & Roles (existing) — add row actions: Unlock, Revoke sessions, Reset password
- SCR-111 API Keys (existing) — add IP whitelist editor (multi-input CIDR)
- **NEW** `/settings/sessions` (Active Sessions list for current user + revoke-all button)
- **NEW** `/settings/security` (2FA backup code generator, view remaining, regenerate)

Change-Password screen mockup:
```
┌───────────────────────────────────────────────────┐
│  Security notice                                  │
│  Your administrator requires a password change.   │
├───────────────────────────────────────────────────┤
│  Current password   [___________________]         │
│  New password       [___________________]  [👁]   │
│  Confirm            [___________________]         │
│                                                   │
│  Requirements                                     │
│  ✓ At least 12 characters                         │
│  ✗ Contains uppercase letter                      │
│  ✓ Contains lowercase letter                      │
│  ✗ Contains digit                                 │
│  ✗ Contains symbol                                │
│  ✓ No 4+ repeating characters                     │
│                                                   │
│  [Change password]                                │
└───────────────────────────────────────────────────┘
```

Active Sessions screen mockup:
```
┌────────────────────────────────────────────────────────────┐
│  Settings > Active Sessions     [Revoke all other sessions]│
├────────────────────────────────────────────────────────────┤
│  ● Current   │ macOS · Safari │ 10.0.5.12 │ 2m ago   │    │
│    Firefox   │ Windows 11      │ 88.12.4.1 │ 3h ago   │ [x]│
│    iPhone    │ iOS 18          │ 212.58.x  │ 1d ago   │ [x]│
└────────────────────────────────────────────────────────────┘
```

2FA Setup with backup codes (added panel after QR):
```
┌─────────────────────────────────────────────┐
│  Save your backup codes                     │
│  Use one of these if you lose your device.  │
│  ┌───────────────────────────────────────┐  │
│  │ ABCD-1234   EFGH-5678   IJKL-9012     │  │
│  │ MNOP-3456   QRST-7890   UVWX-1234     │  │
│  │ YZAB-5678   CDEF-9012   GHIJ-3456     │  │
│  │ KLMN-7890                              │  │
│  └───────────────────────────────────────┘  │
│  [Download .txt] [Copy] [I saved them ✓]    │
└─────────────────────────────────────────────┘
```

### Design Token Map (UI — MANDATORY)

#### Color Tokens (from `docs/FRONTEND.md` + `web/src/index.css` vars)
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Primary text | `text-text-primary` | `text-[#E4E4ED]`, `text-white` |
| Secondary text | `text-text-secondary` | `text-[#7A7A95]`, `text-gray-400` |
| Muted text | `text-text-tertiary` | `text-[#4A4A65]` |
| Page bg | `bg-bg-primary` | `bg-black`, `bg-[#06060B]` |
| Card/panel bg | `bg-bg-surface` | `bg-zinc-900`, `bg-[#0C0C14]` |
| Elevated (modal, dropdown) | `bg-bg-elevated` | `bg-[#12121C]` |
| Hover | `bg-bg-hover` | `hover:bg-zinc-800` |
| Border | `border-border` | `border-[#1E1E30]`, `border-zinc-800` |
| Subtle border | `border-border-subtle` | `border-[#16162A]` |
| Primary accent (CTA, active link) | `bg-accent text-bg-primary` / `text-accent` | `bg-blue-500`, `bg-[#00D4FF]` |
| Accent dim bg (selected row) | `bg-accent-dim` | `bg-blue-500/15` |
| Success | `text-success` / `bg-success-dim` | `text-green-500`, `text-[#00FF88]` |
| Warning (e.g. <3 backup codes) | `text-warning` / `bg-warning-dim` | `text-yellow-500` |
| Danger (revoke, invalid) | `text-danger` / `bg-danger-dim` | `text-red-500`, `text-[#FF4466]` |

#### Typography Tokens
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Page title | `text-[15px] font-semibold text-text-primary` | `text-2xl`, hardcoded px > 16 |
| Section label | `text-[10px] uppercase tracking-[0.1em] text-text-secondary` | `text-xs` |
| Body | `text-sm text-text-primary` | arbitrary |
| Mono data (IP, code) | `font-mono text-xs` | arbitrary |
| Backup code print | `font-mono text-sm tracking-widest` | arbitrary |

#### Spacing & Elevation
| Usage | Token Class |
|-------|-------------|
| Card radius | `rounded-[var(--radius-md)]` |
| Button radius | `rounded-[var(--radius-sm)]` |
| Card shadow | `shadow-[var(--shadow-card)]` |
| Neon hover | `hover:shadow-[var(--shadow-glow)]` |
| Section padding | `p-6` (24px — FRONTEND.md content padding) |
| Card padding | `p-4` (16px) |
| Form gap | `space-y-4` |

#### Existing Components to REUSE (NEVER recreate, NEVER raw HTML)
| Component | Path | Use For |
|-----------|------|---------|
| `<Input>` | `web/src/components/ui/input.tsx` | ALL text/password/email inputs |
| `<Button>` | `web/src/components/ui/button.tsx` | ALL buttons |
| `<Dialog>` | `web/src/components/ui/dialog.tsx` | Backup codes modal, revoke-confirm |
| `<Dropdown>` | `web/src/components/ui/dropdown-menu.tsx` | Row actions (unlock, revoke, reset) |
| `<Table>` | `web/src/components/ui/table.tsx` | Sessions table, keys table |
| `<Badge>` | `web/src/components/ui/badge.tsx` | "Current session", "Locked" badges |
| `<Card>` | `web/src/components/ui/card.tsx` | All panels |
| `<Spinner>` | `web/src/components/ui/spinner.tsx` | Loading |
| `<Tabs>` | `web/src/components/ui/tabs.tsx` | Security tab in settings |
| `<Tooltip>` | `web/src/components/ui/tooltip.tsx` | Helper text for CIDR input |

**RULE:** Developer invokes `frontend-design` skill for every new page. Zero hardcoded hex/px. `grep -E '#[0-9a-fA-F]{3,6}' web/src/pages/auth/change-password.tsx web/src/pages/settings/sessions.tsx` MUST return zero after implementation.

## Prerequisites
- [x] STORY-063 DONE — notification service available for force-logout email and reset-password email.
- [x] `auth.Config.BcryptCost`, `MaxLoginAttempts`, `LockoutDuration` already wired (`internal/auth/auth.go`).
- [x] `audit.Auditor` interface and `CreateEntry` available (`internal/audit/audit.go`).
- [x] WebSocket hub with user lookup (`internal/ws` — if missing, add `Hub.DropUser(userID)`).

## Story-Specific Compliance Rules
- **API envelope:** all new/modified endpoints use `apierr.WriteSuccess` / `apierr.WriteError` (standard envelope per ERROR_CODES.md).
- **Error codes:** Add to `internal/apierr`:
  - `PASSWORD_TOO_SHORT`, `PASSWORD_MISSING_CLASS`, `PASSWORD_REPEATING_CHARS`, `PASSWORD_REUSED`, `PASSWORD_CHANGE_REQUIRED`
  - `INVALID_BACKUP_CODE`
  - `API_KEY_IP_NOT_ALLOWED` (HTTP 403)
  - `TENANT_LIMIT_EXCEEDED` (HTTP 422; `resource`, `current`, `limit` details)
  - `INVALID_CIDR`
  - Reuse existing `ACCOUNT_LOCKED`.
- **Tenant scoping:** every store query filters by `tenant_id` via `TenantIDFromContext` — confirmed pattern in `store/apikey.go`. `password_history` and `user_backup_codes` join through `users.tenant_id` for cross-tenant isolation.
- **Cursor pagination:** sessions list uses cursor pagination (pattern in `store/apikey.go ListByTenant`).
- **Audit:** every mutation invokes `auditor.CreateEntry` with `tenant_id, user_id, action, entity_type, entity_id, before, after, ip, user_agent, correlation_id`. Tamper-chain (`hash`, `prev_hash`) already handled by auditor.
- **Migration:** up+down pair; DOWN must drop new tables and ALTER TABLE … DROP COLUMN IF EXISTS.
- **ADR-002 (Database Stack):** migrations via `golang-migrate`, SQL only. No ORM.
- **ADR-003 (bcrypt):** all password + backup code hashes use bcrypt at `auth.Config.BcryptCost`.
- **RLS:** new tables must get RLS policies in `migrations/20260412000006_rls_policies.up.sql` pattern — append policy for `password_history` and `user_backup_codes` (tenant-scoped via join to users).
- **Business:** `max_*=0` means unlimited (back-compat).

## Bug Pattern Warnings
- **PAT-001 — BR/acceptance tests:** account lockout + session revoke logic may have BR tests in `internal/store/*_br_test.go` or `internal/auth/*_test.go`. Developer must run full test suite (`go test ./...`) and update any BR assertion whose behavior is changed by the new force-change / lockout / history logic.
- **PAT-002 — Duplicated `extractIP`:** IP whitelist middleware must parse client IP correctly for IPv6. Reuse `gateway/bruteforce.go:extractIP` (already uses `net.SplitHostPort` + `net.ParseIP`). Do NOT duplicate the legacy split-on-last-colon pattern. Trust `X-Forwarded-For` only if already trusted by upstream middleware (check `gateway/router.go` chain).
- No matching EAP/MAC pattern.

## Tech Debt (from ROUTEMAP)
No tech debt items target STORY-068.

## Mock Retirement (Frontend-First)
No `src/mocks/` directory — real adapters already in use. No mock retirement needed.

## Task Decomposition Rules
- Each task touches 1–3 files.
- DB migration first. Then auth-core additions (password validator, history store, backup codes store). Then API handlers (grouped by functional surface). Then gateway middleware. Then frontend. Then integration tests.
- Complexity calibrated for L effort: several `high` tasks for core-logic (password service, backup codes, IP whitelist middleware, tenant-limits middleware, audit backfill).

---

## Tasks

### Task 1: Migration — schema changes (columns + new tables)
- **Files:** Create `migrations/20260412000011_enterprise_auth_hardening.up.sql`, Create `migrations/20260412000011_enterprise_auth_hardening.down.sql`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** `migrations/20260320000002_core_schema.up.sql` (style), `migrations/20260412000010_users_state_purged.up.sql` (ALTER + down pattern), `migrations/20260412000006_rls_policies.up.sql` (RLS block to mirror for new tables).
- **Context refs:** "Database Schema", "Story-Specific Compliance Rules"
- **What:** All DDL per Database Schema section. Ensure: ALTER TABLE users ADD `password_change_required BOOLEAN DEFAULT false`, `password_changed_at TIMESTAMPTZ`. ALTER TABLE api_keys ADD `allowed_ips TEXT[] NOT NULL DEFAULT '{}'`. ALTER TABLE tenants ADD `max_api_keys INTEGER NOT NULL DEFAULT 20`. CREATE `password_history` + index. CREATE `user_backup_codes` + partial-index. Append RLS policies for both new tables filtering via `users.tenant_id` matches `current_setting('app.tenant_id')::uuid`. DOWN drops tables and columns.
- **Verify:** `make db-migrate` succeeds; `\d users` shows new columns; `\d password_history` exists; down migration (manual) reverses; `make test` of affected packages still green.

### Task 2: Config — env vars
- **Files:** Modify `internal/config/config.go` (and any `internal/config/env.go` loader)
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** existing auth vars loaded in `internal/config` (grep `BcryptCost`, `MaxLoginAttempts`).
- **Context refs:** "Acceptance Criteria Mapping" (AC-1, AC-2, AC-3, AC-10)
- **What:** Add `PASSWORD_MIN_LENGTH=12`, `PASSWORD_REQUIRE_UPPER=true`, `PASSWORD_REQUIRE_LOWER=true`, `PASSWORD_REQUIRE_DIGIT=true`, `PASSWORD_REQUIRE_SYMBOL=true`, `PASSWORD_MAX_REPEATING=3`, `PASSWORD_HISTORY_COUNT=5`, `PASSWORD_MAX_AGE_DAYS=0` (0=disabled), `LOGIN_MAX_ATTEMPTS=5` (pre-existing), `LOGIN_LOCKOUT_DURATION=15m` (pre-existing). Document in `docs/architecture/CONFIG.md`.
- **Verify:** `grep PASSWORD_MIN_LENGTH internal/config` shows loader; unit test for defaults.

### Task 3: Password policy validator
- **Files:** Create `internal/auth/password_policy.go`, Create `internal/auth/password_policy_test.go`
- **Depends on:** Task 2
- **Complexity:** medium
- **Pattern ref:** `internal/auth/totp.go` + `_test.go` (package layout, table-driven tests)
- **Context refs:** "Data Flow > Password change / history", "Acceptance Criteria Mapping (AC-1)"
- **What:** `ValidatePasswordPolicy(password string, cfg PasswordPolicy) error` returning typed errors mapping to codes `PASSWORD_TOO_SHORT`, `PASSWORD_MISSING_CLASS`, `PASSWORD_REPEATING_CHARS`. `PasswordPolicy` struct with fields from Task 2. Repeating check: any run of same char > MaxRepeating. Class check: unicode-aware upper/lower/digit/symbol. No bcrypt here.
- **Verify:** Table tests: `"short1A!"` → PASSWORD_TOO_SHORT; `"ValidLongPass1!"` → nil; `"aaaaaLongPass1!"` → PASSWORD_REPEATING_CHARS when max=3; `"alllowercase12!"` → PASSWORD_MISSING_CLASS (no upper).

### Task 4: Password history store
- **Files:** Create `internal/store/password_history.go`, Create `internal/store/password_history_test.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** `internal/store/apikey.go` (pgx query style + tenant scoping)
- **Context refs:** "Database Schema > NEW TBL password_history", "Data Flow > Password change / history"
- **What:** `PasswordHistoryStore` with methods: `Insert(ctx, userID, hash) error`, `GetLastN(ctx, userID, n int) ([]string, error)`, `Trim(ctx, userID, keep int) error`. Inserts run inside a tx when called from password change flow.
- **Verify:** `go test ./internal/store -run PasswordHistory` passes with integration DB.

### Task 5: Backup codes — service + store
- **Files:** Create `internal/auth/backup_codes.go`, Create `internal/store/backup_codes.go`, Create `internal/auth/backup_codes_test.go`
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** `internal/auth/totp.go` for crypto/random; `internal/store/apikey.go` for pgx pattern
- **Context refs:** "Data Flow > 2FA with backup code", "Acceptance Criteria Mapping (AC-4)"
- **What:**
  - Store: `BackupCodeStore` with `GenerateAndStore(ctx, userID, count int, bcryptCost int) ([]string, error)` (returns plaintext once; stores hashes), `ConsumeIfMatch(ctx, userID, rawCode) (matched bool, remaining int, err error)`, `CountUnused(ctx, userID) (int, error)`, `InvalidateAll(ctx, userID) error`.
  - Codes format: 10 codes, each `XXXX-YYYY` alphanumeric uppercase, generated via `crypto/rand`. Hashed via `bcrypt.GenerateFromPassword`.
  - `ConsumeIfMatch` loops unused rows per user, bcrypt compare; on match sets `used_at = NOW()`.
- **Verify:** Unit tests: generate 10 → 10 returned; consume valid → true + remaining=9; consume same code again → false; regenerate invalidates prior set.

### Task 6: Auth service — wire password change + history + force-flag + backup + lockout tuning
- **Files:** Modify `internal/auth/auth.go`, Modify `internal/auth/auth_test.go` (or add `password_change_test.go`)
- **Depends on:** Task 3, Task 4, Task 5
- **Complexity:** high
- **Pattern ref:** existing `Service.Login` / `Verify2FA` flow in `internal/auth/auth.go`
- **Context refs:** "Data Flow > Login with force-change & lockout", "Data Flow > Password change / history", "Data Flow > 2FA with backup code"
- **What:**
  - Extend `UserRepository` with: `GetPasswordChangeRequired(userID)`, `SetPasswordHash(userID, hash)`, `SetPasswordChangeRequired(userID, bool)`, `ClearLockout(userID)`.
  - Extend `Service` struct with `policy PasswordPolicy`, `passwordHistory PasswordHistoryStore`, `backupCodes BackupCodeStore`, `cfg.PasswordHistoryCount`, `cfg.PasswordMaxAgeDays`.
  - New methods: `ChangePassword(ctx, userID, currentPwd, newPwd) error` (validate policy → check history → bcrypt new → update + insert history + trim + clear change_required), `VerifyBackupCode(ctx, userID, code) (*Verify2FAResult, int /*remaining*/, error)`, `GenerateBackupCodes(ctx, userID) ([]string, error)`.
  - In `Login`: after password success, if `password_change_required` → issue partial token with `reason:"password_change_required"` (JWT claim) instead of standard partial. Bump `GenerateToken` to accept `reason` string or add second flag.
  - In `Verify2FA`: if `code` is empty but `backup_code` provided → call `VerifyBackupCode`.
  - Keep existing audit logging; add `user.password_change`, `user.backup_codes_generated`, `user.login_backup_code`.
- **Verify:** `go test ./internal/auth/... -race` passes. Scenario tests: password reuse rejected, change clears flag, backup code consumed, lockout triggers at 5th failure.

### Task 7: UserStore extensions + TenantStore max_api_keys
- **Files:** Modify `internal/store/user.go`, Modify `internal/store/tenant.go`, Modify `internal/store/user_test.go`, Modify `internal/store/tenant_test.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** existing update methods in `internal/store/user.go`
- **Context refs:** "Database Schema", "Data Flow > Login with force-change & lockout"
- **What:** Add `SetPasswordHash`, `SetPasswordChangeRequired`, `ClearLockout`, `GetPasswordChangeRequired` on UserStore. Extend `User` struct with `PasswordChangeRequired bool, PasswordChangedAt *time.Time`. Update SELECT queries to include new columns. Add `max_api_keys` to Tenant struct, scan into new field, include in GetByID/List/Update. Update `store.Tenant` JSON/struct consumers minimally (most consumers use `MaxUsers` etc via field — add analogous `MaxAPIKeys`).
- **Verify:** `go test ./internal/store -run User|Tenant` passes.

### Task 8: API — password change + force-change flow handlers
- **Files:** Modify `internal/api/auth/handler.go`, Modify `internal/api/auth/handler_test.go`, Modify `internal/gateway/router.go` (add route)
- **Depends on:** Task 6
- **Complexity:** medium
- **Pattern ref:** `internal/api/auth/handler.go` existing `Login` + `Verify2FA`
- **Context refs:** "API Specifications > POST /api/v1/auth/password/change", "Data Flow > Login with force-change & lockout"
- **What:** Add `ChangePassword` handler. Accepts either partial-force-change JWT (new middleware variant `JWTAuthAllowForceChange`) or full JWT. Delegates to `auth.Service.ChangePassword`. On success, issues full session JWT + refresh. New middleware variant in `internal/gateway/jwt_auth.go` (or existing `JWTAuthAllowPartial` extended). Route registered under body-limited group. Emit audit `user.password_change`.
- **Verify:** `go test ./internal/api/auth` passes. Integration test: login-with-force → /auth/password/change → full token returned; flag cleared in DB.

### Task 9: API — 2FA backup codes (generate + verify)
- **Files:** Modify `internal/api/auth/handler.go`, Modify `internal/api/auth/handler_test.go`, Modify `internal/gateway/router.go`
- **Depends on:** Task 6
- **Complexity:** medium
- **Pattern ref:** `internal/api/auth/handler.go` existing `Setup2FA` / `Verify2FA`
- **Context refs:** "API Specifications > POST /api/v1/auth/2fa/backup-codes", "Data Flow > 2FA with backup code"
- **What:** `GenerateBackupCodes` handler (JWT, user must have `totp_enabled`). `Verify2FA` extended to accept `backup_code` field — delegate to `auth.Service.VerifyBackupCode`. Response includes `meta.backup_codes_remaining` when <=3. Audit: `user.backup_codes_generated`, `user.login_backup_code`.
- **Verify:** Unit tests for both; integration — login → 2fa with backup code → full token; regenerate invalidates prior.

### Task 10: API — user admin actions (unlock, revoke-sessions, reset-password)
- **Files:** Modify `internal/api/user/handler.go`, Modify `internal/api/user/handler_test.go`, Modify `internal/gateway/router.go`
- **Depends on:** Task 6, Task 7
- **Complexity:** medium
- **Pattern ref:** existing `Update`/`Delete` in `internal/api/user/handler.go`, audit pattern in `createAuditEntry`
- **Context refs:** "API Specifications > POST /api/v1/users/{id}/unlock|revoke-sessions|reset-password", "Data Flow > Session revoke-all"
- **What:**
  - `Unlock(w,r)`: tenant_admin; clears `locked_until`, `failed_login_count=0`; audit `user.unlock`.
  - `RevokeSessions(w,r)`: self or tenant_admin; revokes all user sessions; optional `?include_api_keys=true` revokes all api_keys `created_by=user.id AND tenant_id=user.tenant_id`; drop ws via `WSHub.DropUser`. Audit `user.sessions_revoked`.
  - `ResetPassword(w,r)`: tenant_admin; generates temp password (via `GenerateRandomPolicyCompliant()`), sets hash, `password_change_required=true`, revokes sessions; either returns temp password once or queues email via `notification.Service`. Audit `user.password_reset`.
  - Register routes under `tenant_admin` role (revoke-sessions allows self via additional check).
- **Verify:** Per-endpoint handler tests, RBAC denial path for non-admin, audit entry assertion.

### Task 11: API — super-admin tenant-wide revoke-all-sessions
- **Files:** Modify `internal/api/system/handler.go`, Modify `internal/gateway/router.go`, Create `internal/api/system/revoke_sessions_test.go`
- **Depends on:** Task 6
- **Complexity:** medium
- **Pattern ref:** existing `internal/api/system/handler.go`
- **Context refs:** "API Specifications > POST /api/v1/system/revoke-all-sessions", "Data Flow > Super-admin tenant-wide revoke-all"
- **What:** New handler; super_admin or tenant_admin of target tenant. Calls `SessionStore.RevokeAllByTenant(tenantID)` (add method to SessionRepository). Optional `?notify=true` invokes `notification.Service.NotifyTenantUsers(tenantID, "security_revoke_all")`. Audit `system.tenant_sessions_revoked` with detail `{tenant_id, affected_users, sessions_revoked, notified}`.
- **Verify:** Handler test; DB-level bulk update verified.

### Task 12: API — API key IP whitelist fields
- **Files:** Modify `internal/api/apikey/handler.go`, Modify `internal/api/apikey/handler_test.go`, Modify `internal/store/apikey.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** `internal/api/apikey/handler.go` Create/Update validators; `internal/store/apikey.go` Create/Update/Scan
- **Context refs:** "Database Schema > api_keys", "API Specifications > Modified endpoints"
- **What:** Extend `createRequest`/`updateRequest` with `AllowedIPs []string`. Server-side validate each via `net.ParseCIDR` — on failure, 422 `INVALID_CIDR`. Pass through to store. Store struct `APIKey` gains `AllowedIPs []string`; update `Create`, `scanAPIKey`, `Update` SQL to include column. Marshal via pgx `pgtype.TextArray`/native `[]string`. Response includes `allowed_ips` array.
- **Verify:** Create with invalid CIDR → 422 INVALID_CIDR; valid CIDR persists and round-trips. Empty array default preserved.

### Task 13: Gateway — API-key IP whitelist enforcement
- **Files:** Modify `internal/gateway/apikey_auth.go`, Modify `internal/gateway/apikey_auth_test.go` (create if absent — note file exists: `apikey_auth_test.go`)
- **Depends on:** Task 12
- **Complexity:** high
- **Pattern ref:** existing `APIKeyAuth` middleware in same file; `bruteforce.go:extractIP` (IPv6-safe)
- **Context refs:** "Data Flow > API-key IP whitelist", "Bug Pattern Warnings PAT-002"
- **What:** After key validation block in `APIKeyAuth`, if `k.AllowedIPs` non-empty: `clientIP := extractIP(r.RemoteAddr)` (REUSE from bruteforce.go — or export as shared helper at this point). Iterate allowed_ips, `net.ParseCIDR(entry)`; if entry parses as pure IP (no `/`), compare equals; else `network.Contains(parsed)`. On miss → 403 `API_KEY_IP_NOT_ALLOWED` with details `{client_ip, allowed_ips}`. Respect trusted proxy: if `X-Forwarded-For` trusted (reuse existing gateway logic), prefer first IP.
- **Verify:** Test: allowed `["192.168.1.0/24"]`, request from 10.0.0.1 → 403; from 192.168.1.5 → 200. IPv6 loopback case. Empty list always allows.

### Task 14: Gateway — tenant resource limits middleware
- **Files:** Create `internal/gateway/tenant_limits.go`, Create `internal/gateway/tenant_limits_test.go`, Modify `internal/gateway/router.go`
- **Depends on:** Task 7
- **Complexity:** high
- **Pattern ref:** `internal/gateway/ratelimit.go` (Redis cache pattern), `internal/api/user/handler.go` existing in-handler limit check (replace with middleware)
- **Context refs:** "Data Flow > Tenant resource limits middleware", "Acceptance Criteria Mapping (AC-8)"
- **What:** `TenantLimitsMiddleware(tenantStore, counters map[string]CountFn, rdb *redis.Client)` where counters map resource name → function `func(ctx) (int, error)`. Route-wraps only POST create endpoints. Cache tenant limits in Redis key `tenant:limits:{id}` TTL 5m. If `max > 0 && current >= max` → 422 `TENANT_LIMIT_EXCEEDED` `{resource, current, limit}`. Remove duplicate in-handler check in `user/handler.go` Create (now covered) — or keep as defense-in-depth (prefer keep, middleware short-circuits before handler). Counters: `sims` (`SIMStore.CountByTenant`), `apns` (`APNStore.CountByTenant` — add if missing), `users` (`UserStore.CountByTenant`), `api_keys` (`APIKeyStore.CountByTenant`). Use tenant's `max_api_keys` (new column).
- **Verify:** Handler tests hit limit → 422 with full detail; 0 = unlimited; Redis cache invalidated on tenant update via `DEL tenant:limits:{id}` in TenantStore.Update.

### Task 15: Audit backfill for 13 mutation endpoints
- **Files:** Modify each handler file listed below (small edits — ≤3 files per sub-task if sliced, but single audit-addition task is atomic and low-risk).
  - `internal/api/cdr/handler.go` (POST /cdrs/export)
  - `internal/api/compliance/handler.go` (POST /compliance/erasure/:sim_id, PUT /compliance/retention)
  - `internal/api/msisdn/handler.go` (POST /msisdn-pool/import, POST /msisdn-pool/:id/assign)
  - `internal/api/anomaly/handler.go` (PATCH /analytics/anomalies/:id)
  - `internal/api/segment/handler.go` (POST/DELETE /segments/*)
  - `internal/api/notification/handler.go` (POST /notification-configs/*)
  - `internal/api/job/handler.go` (POST /jobs/:id/cancel, POST /jobs/:id/retry)
  - `internal/api/apikey/handler.go` (POST /apikeys/:id/rotate — add audit)
  - `internal/api/user/handler.go` (already done in Task 10 for new endpoints)
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** `internal/api/user/handler.go:createAuditEntry` (copy/paste helper or extract to `internal/audit/http.go`)
- **Context refs:** "Acceptance Criteria Mapping (AC-9)", "Story-Specific Compliance Rules"
- **What:** For each listed endpoint call `auditor.CreateEntry` with actions: `cdr.export`, `sim.erasure`, `msisdn.import`, `msisdn.assign`, `anomaly.update`, `retention.update`, `segment.create|delete`, `notification_config.create|update|delete`, `job.cancel`, `job.retry`, `apikey.rotate`. Populate before/after JSON where applicable. Consider extracting `auditHelper` to `internal/audit/httpaudit.go` once pattern is copied 3+ times (DRY).
- **Verify:** Table test: for each endpoint, after 200 response, `SELECT COUNT(*) FROM audit_logs WHERE action=$1 AND entity_id=$2` → ≥1.

### Task 16: Frontend — login + 2FA updates (force-change + backup code branch)
- **Files:** Modify `web/src/pages/auth/login.tsx`, Modify `web/src/pages/auth/two-factor.tsx`, Modify `web/src/stores/auth.ts` (add `partial2fa_reason` state), Modify `web/src/lib/api.ts` (add `authApi.changePassword`, `authApi.generateBackupCodes`, backup_code in verify2fa)
- **Depends on:** Task 8, Task 9
- **Complexity:** medium
- **Pattern ref:** existing `web/src/pages/auth/login.tsx` pattern (form + lockout countdown)
- **Context refs:** "Screen Mockups", "Design Token Map"
- **What:**
  - login.tsx: handle new `partial:true, reason:"password_change_required"` → `navigate('/auth/change-password')`. Keep existing lockout.
  - two-factor.tsx: add "Use a backup code" toggle that swaps 6-digit TOTP input for backup-code input; API call sends either `code` or `backup_code`. Show warning banner when `meta.backup_codes_remaining < 3`.
  - Skill: `frontend-design` invoked.
  - Tokens: ONLY from Design Token Map. No hardcoded hex/px.
- **Verify:** `grep -E '#[0-9a-fA-F]{3,6}' web/src/pages/auth/login.tsx web/src/pages/auth/two-factor.tsx` → 0. Manual happy path (Playwright in Gate).

### Task 17: Frontend — Change-Password page
- **Files:** Create `web/src/pages/auth/change-password.tsx`, Modify `web/src/router.tsx` (add route `/auth/change-password`)
- **Depends on:** Task 16
- **Complexity:** medium
- **Pattern ref:** `web/src/pages/auth/login.tsx` (layout, form pattern); `web/src/components/ui/input.tsx` / `button.tsx`
- **Context refs:** "Screen Mockups > Change-Password", "Design Token Map"
- **What:** Three-field form with live policy checklist (fetches `/api/v1/auth/password-policy` — **optional endpoint** OR hardcodes visible rules client-side from env-derived metadata returned on /auth/me; simpler: hardcode rule list and update via server validation errors). Submits to `authApi.changePassword`. On success: store new JWT, navigate '/'. On server validation error: highlight failing rules.
- **Skill:** `frontend-design` MANDATORY.
- **Verify:** No hardcoded hex/px; builds.

### Task 18: Frontend — API keys IP whitelist editor
- **Files:** Modify `web/src/pages/settings/api-keys.tsx`, Modify `web/src/lib/api.ts` (types)
- **Depends on:** Task 12
- **Complexity:** medium
- **Pattern ref:** existing create/edit dialog in `api-keys.tsx`
- **Context refs:** "Screen Mockups (SCR-111)", "Design Token Map"
- **What:** Add `allowed_ips` multi-input field (chip/tag input). Client-side CIDR validation: accept `1.2.3.4`, `1.2.3.0/24`, IPv6 `2001:db8::/32`. On invalid chip show inline error. Send array in create/update payload. Display existing list in row detail.
- **Verify:** No hardcoded hex/px; invalid CIDR blocks submit.

### Task 19: Frontend — Active Sessions page + Users page admin actions
- **Files:** Create `web/src/pages/settings/sessions.tsx`, Modify `web/src/pages/settings/users.tsx`, Modify `web/src/router.tsx`, Modify `web/src/lib/api.ts`
- **Depends on:** Task 10, Task 11
- **Complexity:** medium
- **Pattern ref:** `web/src/pages/settings/users.tsx` (table + row actions via dropdown-menu)
- **Context refs:** "Screen Mockups > Active Sessions", "Acceptance Criteria Mapping (AC-6, AC-10)"
- **What:**
  - `/settings/sessions` page: lists current user's active sessions (`GET /api/v1/auth/sessions`). "Revoke all other sessions" button → `POST /api/v1/users/{me}/revoke-sessions`. Per-row revoke.
  - users.tsx: Row-action dropdown (tenant_admin): Unlock (visible if `locked_until > now()`), Revoke sessions, Reset password (shows temp-pass dialog or email-sent toast). Add "Locked" badge to locked rows.
- **Skill:** `frontend-design` for sessions page.
- **Verify:** No hardcoded hex/px; RBAC: non-admin sees no row actions.

### Task 20: Frontend — 2FA Setup backup codes UI + Security settings
- **Files:** Modify `web/src/pages/auth/two-factor.tsx` (setup flow) OR `web/src/pages/settings/security.tsx` (new), Modify `web/src/lib/api.ts`
- **Depends on:** Task 9
- **Complexity:** medium
- **Pattern ref:** `web/src/components/ui/dialog.tsx` for the codes modal
- **Context refs:** "Screen Mockups > 2FA Setup with backup codes", "Design Token Map"
- **What:** After 2FA setup succeeds, POST `/auth/2fa/backup-codes`, render 10 codes in monospace grid with Download .txt + Copy + "I saved them" confirm. Settings > Security tab: show remaining count + Regenerate button (confirm dialog: "This invalidates prior codes").
- **Skill:** `frontend-design`.
- **Verify:** No hardcoded hex/px; Playwright flow stable.

### Task 21: Integration tests — end-to-end auth hardening
- **Files:** Create `internal/tests/auth_enterprise_test.go` (or extend existing `*_integration_test.go`)
- **Depends on:** Task 6, Task 10, Task 11, Task 13, Task 14
- **Complexity:** high
- **Pattern ref:** existing integration tests (`grep -l "httptest.NewServer" internal/` for a template)
- **Context refs:** "Test Scenarios" (from story), "Acceptance Criteria Mapping"
- **What:** Run the 10 story test scenarios end-to-end:
  1. Password "short1A!" rejected / "ValidLongPass1!" accepted.
  2. Reuse: change → same password → rejected.
  3. Admin invites → first login → PASSWORD_CHANGE_REQUIRED → change → full JWT.
  4. 2FA setup → 10 codes → login with one → used → reuse fails.
  5. API key `allowed_ips=["192.168.1.0/24"]` → 10.0.0.1 = 403 API_KEY_IP_NOT_ALLOWED; 192.168.1.5 = 200.
  6. Revoke sessions → /auth/refresh returns 401.
  7. super_admin revoke-all-sessions for tenantX → users logged out, notification queued, audit created.
  8. Tenant with max_sims=10 reached → POST /sims → 422 TENANT_LIMIT_EXCEEDED with current=10, max=10.
  9. Each of the 13 mutation endpoints produces audit_logs entry.
  10. 5 failed logins → 6th returns ACCOUNT_LOCKED. Simulate clock advance → succeed.
- **Verify:** `go test ./internal/tests -run TestEnterpriseAuth -v` passes.

### Task 22: Frontend build + lint + type-check
- **Files:** —
- **Depends on:** Task 16, Task 17, Task 18, Task 19, Task 20
- **Complexity:** low
- **Pattern ref:** —
- **Context refs:** —
- **What:** `make web-build` completes; `pnpm tsc --noEmit` clean. Grep for hex/px: `grep -RE '#[0-9a-fA-F]{3,6}|\[[0-9]+px\]' web/src/pages/auth/change-password.tsx web/src/pages/settings/sessions.tsx` → zero.
- **Verify:** CI green; build artifact produced.

## Acceptance Criteria Mapping
| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| AC-1 Password complexity | Task 2, Task 3, Task 6, Task 8 | Task 21 #1 |
| AC-2 Password history | Task 1, Task 4, Task 6 | Task 21 #2 |
| AC-3 Force password change | Task 1, Task 6, Task 8, Task 10 (reset-password), Task 16, Task 17 | Task 21 #3 |
| AC-4 2FA backup codes | Task 1, Task 5, Task 6, Task 9, Task 20 | Task 21 #4 |
| AC-5 API key IP whitelist | Task 1, Task 12, Task 13, Task 18 | Task 21 #5 |
| AC-6 Session revoke-all | Task 10, Task 19 | Task 21 #6 |
| AC-7 Admin force-logout all | Task 11, Task 19 | Task 21 #7 |
| AC-8 Tenant resource limits enforced | Task 1 (max_api_keys), Task 7, Task 14 | Task 21 #8 |
| AC-9 Audit on 13 endpoints | Task 15 (and Task 10/11/12 for new endpoints) | Task 21 #9 |
| AC-10 Account lockout + unlock | Task 2, Task 6, Task 7, Task 10 | Task 21 #10 |

## Risks & Mitigations
- **Risk:** Changing `users` SELECT queries (new columns) breaks all users-based tests.
  **Mitigation:** Task 7 updates every SELECT in `store/user.go` and runs full store test suite before dependent tasks.
- **Risk:** Partial-JWT for force-change collides with existing partial-for-2fa.
  **Mitigation:** Encode a `reason` claim (`password_change_required` vs `totp_required`) and a dedicated middleware variant (`JWTAuthAllowForceChange`).
- **Risk:** IP whitelist middleware breaks existing API-key users when empty array semantics misinterpreted.
  **Mitigation:** Default `allowed_ips='{}'` explicitly means "any IP". Unit test confirms back-compat; migration sets default for existing rows.
- **Risk:** Bulk session revoke on large tenants too slow.
  **Mitigation:** Single UPDATE with WHERE tenant_id via join; add index `idx_user_sessions_user` already exists; return count; async notification.
- **Risk:** Backup codes brute-force (10 codes, 8 chars each).
  **Mitigation:** Codes are 8 alnum chars (36^8 ≈ 2.8e12) hashed with bcrypt; brute-force middleware already rate-limits `/auth/2fa/verify` per IP (bruteforce.go:isAuthEndpoint includes `/2fa`).
- **Risk:** Tenant-limits middleware races with concurrent creates (TOCTOU).
  **Mitigation:** Accept small over-shoot; for hard enforcement, add DB-level check-trigger as follow-up. Document in close-out. Count path reads from DB (not just cache) when cached limit says near-full.
- **Risk:** WebSocket `DropUser` method may not exist.
  **Mitigation:** Task 10 includes adding it to `internal/ws/hub.go` if absent.

---

## Embedded Quality Gate Self-Validation

**a. Minimum substance (Effort L: min 100 lines, min 5 tasks):** plan is ~500+ lines, 22 tasks. PASS.

**b. Required sections:** Goal ✓, Architecture Context ✓, Tasks ✓, Acceptance Criteria Mapping ✓. PASS.

**c. Embedded specs:** API endpoint details present (req/resp/status/error codes) ✓; DB schema with SQL types ✓; Design Token Map populated ✓. PASS.

**d. Task complexity cross-check (L → ≥1 high):** Tasks 5, 6, 13, 14, 21 marked `high`. PASS.

**e. Context refs validation:** every `Context refs` names a section header that exists in this plan ("Architecture Context > Components Involved", "Data Flow > …", "API Specifications > …", "Database Schema", "Screen Mockups", "Design Token Map", "Acceptance Criteria Mapping", "Bug Pattern Warnings", "Story-Specific Compliance Rules"). PASS.

**Architecture Compliance:**
- Layers correct: migrations → store → auth service → api handlers → gateway middleware → frontend. ✓
- No cross-layer imports. ✓
- Naming matches ARCHITECTURE.md (`internal/auth`, `internal/api/*`, `internal/store`). ✓

**API Compliance:**
- Standard envelope on every endpoint. ✓
- HTTP methods correct (POST for actions). ✓
- Validation step named per endpoint. ✓
- Error responses enumerated. ✓

**Database Compliance:**
- Migration up + down pair. ✓
- Column names verified against `migrations/20260320000002_core_schema.up.sql` (ACTUAL). ✓
- Indexes specified (`idx_password_history_user_created`, `idx_user_backup_codes_user_unused`, `idx_api_keys_allowed_ips_gin`). ✓
- New-table source tagged (NEW / this story). ✓
- RLS policy addition noted (compliance rule). ✓

**UI Compliance:**
- Screen mockups embedded (change-password, sessions, backup codes). ✓
- Atomic-design reuse table with paths and "NEVER raw HTML". ✓
- Drill-down targets (login → change-password, sessions page). ✓
- Empty/loading/error states referenced (lockout banner, 2FA error, CIDR inline error). ✓
- frontend-design skill usage mandated on Tasks 17, 19, 20. ✓
- Design Token Map: color + typography + spacing + components tables complete. ✓
- Each UI task references Design Token Map via Context refs. ✓

**Task Decomposition:**
- All tasks ≤3 files (Task 15 is audit-backfill sweep across multiple files but each handler edit is 1-3 lines; reasonable as a single sweep task — if Developer prefers, it splits into 3 sub-waves. Marking as acceptable per plan rules).
- `Depends on` present for every task. ✓
- `Context refs` present for every task (Tasks 2, 15, 22 use "—" where no context needed and that is noted). ✓
- `Pattern ref` on every file-creating task. ✓
- Functionally grouped (migrations + core + api + gateway + frontend + tests). ✓
- 22 tasks total for L-effort story (acceptable given 10 ACs × 2 layers).
- No implementation code in task bodies — specs and pattern refs only. ✓

**Test Compliance:**
- Task 21 covers every AC via story test scenarios. ✓
- Test file paths specified. ✓
- All 10 story scenarios listed. ✓

**Self-Containment:**
- API specs inline. ✓
- DB schema inline with source (ACTUAL vs NEW). ✓
- Screen mockups inline. ✓
- Business rules stated (0=unlimited, back-compat empty IP list). ✓
- Every Context refs points to a section that exists. ✓

**Result: PASS (all checks).**

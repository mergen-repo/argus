# STORY-003: Authentication — JWT + Refresh Token + 2FA

## User Story
As a user, I want to log in securely with email/password and optional 2FA, so that my account is protected.

## Description
Implement JWT authentication with refresh tokens, account lockout on failed attempts, and TOTP-based 2FA setup/verification. Refresh tokens stored in TBL-03, httpOnly cookie for refresh.

## Architecture Reference
- Services: SVC-01 (Gateway auth middleware), SVC-03 (auth handlers)
- API Endpoints: API-001 to API-005
- Database Tables: TBL-02 (users), TBL-03 (user_sessions)
- Source: docs/architecture/api/_index.md (Auth & Users section)
- Packages: internal/auth, internal/gateway

## Screen Reference
- SCR-001: Login (docs/screens/SCR-001-login.md)
- SCR-002: 2FA Verification (docs/screens/SCR-002-2fa.md)

## Acceptance Criteria
- [ ] POST /api/v1/auth/login validates credentials, returns JWT (15min) + refresh token (7d)
- [ ] JWT contains: user_id, tenant_id, role, exp
- [ ] Refresh token stored as bcrypt hash in TBL-03
- [ ] POST /api/v1/auth/refresh rotates refresh token, issues new JWT
- [ ] POST /api/v1/auth/logout revokes session in TBL-03
- [ ] Account locks after 5 consecutive failed logins for 15 minutes
- [ ] POST /api/v1/auth/2fa/setup generates TOTP secret + QR code URI
- [ ] POST /api/v1/auth/2fa/verify validates 6-digit TOTP code
- [ ] If 2FA enabled, login returns partial token that requires 2FA verification
- [ ] All auth events logged to audit (login, logout, failed_login)
- [ ] Gateway middleware extracts JWT, sets tenant context on all /api/v1/* routes

## API Contract

| Ref | Method | Path | Request | Response (data) | Auth | Status Codes |
|-----|--------|------|---------|-----------------|------|-------------|
| API-001 | POST | /api/v1/auth/login | `{ email: string, password: string }` | `{ user: {id,email,name,role}, token: string, requires_2fa: bool }` | None | 200, 401, 423 |
| API-002 | POST | /api/v1/auth/refresh | Cookie: refresh_token | `{ token: string }` | Refresh | 200, 401 |
| API-003 | POST | /api/v1/auth/logout | — | `{}` | JWT | 204, 401 |
| API-004 | POST | /api/v1/auth/2fa/setup | — | `{ secret: string, qr_uri: string }` | JWT | 200, 401 |
| API-005 | POST | /api/v1/auth/2fa/verify | `{ code: string }` | `{ token: string }` | Partial JWT | 200, 401 |

## Database Changes
- No new tables (TBL-02, TBL-03 from STORY-002)
- Verify indexes: idx_users_tenant_email, idx_user_sessions_user

## Dependencies
- Blocked by: STORY-002 (users + sessions tables)
- Blocks: STORY-004 (RBAC), STORY-005 (tenant mgmt)

## Test Scenarios
- [ ] Valid login returns JWT + sets refresh cookie
- [ ] Invalid password returns 401 INVALID_CREDENTIALS
- [ ] 6th failed attempt returns 423 ACCOUNT_LOCKED
- [ ] Refresh with valid cookie returns new JWT
- [ ] Refresh with expired/revoked cookie returns 401
- [ ] Logout invalidates session, subsequent refresh fails
- [ ] 2FA setup returns valid TOTP secret
- [ ] 2FA verify with correct code issues full JWT
- [ ] 2FA verify with wrong code returns 401
- [ ] Expired JWT returns 401 on protected routes

## Effort Estimate
- Size: M
- Complexity: Medium

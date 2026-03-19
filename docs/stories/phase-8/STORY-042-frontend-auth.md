# STORY-042: Frontend Authentication Flow

## User Story
As a user, I want to log in via the portal with email/password, complete 2FA if enabled, and have my session persist with automatic token refresh, so that I can securely access the platform.

## Description
Login page (SCR-001), 2FA verification page (SCR-002), JWT storage in memory (not localStorage for XSS protection), refresh token in httpOnly cookie, Axios interceptor for automatic token refresh on 401, protected route wrapper that redirects to /login when unauthenticated.

## Architecture Reference
- Services: SVC-01 (API Gateway — auth endpoints)
- API Endpoints: API-001 to API-005
- Source: docs/architecture/api/_index.md (Auth section)

## Screen Reference
- SCR-001: Login — email/password form, error states, "remember me" option
- SCR-002: 2FA Verification — 6-digit code input, auto-submit, countdown timer

## Acceptance Criteria
- [ ] Login page: email + password form, validation, error display
- [ ] Login success without 2FA → store JWT in Zustand, redirect to /
- [ ] Login success with 2FA required → redirect to /login/2fa with partial token
- [ ] 2FA page: 6-digit code input with auto-focus advancing
- [ ] 2FA success → store full JWT, redirect to /
- [ ] 2FA failure → error message, allow retry
- [ ] JWT stored in memory (Zustand), not localStorage
- [ ] Refresh token handled via httpOnly cookie (automatic by browser)
- [ ] Axios interceptor: on 401, attempt refresh via API-002, retry original request
- [ ] Refresh failure → clear auth state, redirect to /login
- [ ] Protected route wrapper: check auth state, redirect to /login if missing
- [ ] Logout: call API-003, clear Zustand auth state, redirect to /login
- [ ] "Remember me": extend refresh token TTL (7d vs 24h)
- [ ] Account locked (423 response) → display lockout message with countdown
- [ ] Loading state during login/2FA API calls

## Dependencies
- Blocked by: STORY-041 (React scaffold), STORY-003 (backend auth endpoints)
- Blocks: STORY-043 (dashboard requires auth)

## Test Scenarios
- [ ] Valid login → JWT stored, redirected to dashboard
- [ ] Invalid password → error "Invalid credentials" displayed
- [ ] Account locked → 423 message with countdown shown
- [ ] 2FA flow → code input, success redirects to dashboard
- [ ] Wrong 2FA code → error displayed, can retry
- [ ] Token expired → interceptor refreshes, request succeeds transparently
- [ ] Refresh token expired → redirected to login
- [ ] Visit protected route without auth → redirected to login
- [ ] Logout → auth cleared, redirected to login, back button shows login

## Effort Estimate
- Size: M
- Complexity: Medium

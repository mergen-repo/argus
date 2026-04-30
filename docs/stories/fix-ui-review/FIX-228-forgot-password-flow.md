# FIX-228: Login — Forgot Password Flow + Version Footer

## Problem Statement
Login page has no "Forgot password?" link. No visible version number.

## User Story
As a user, I want to reset my forgotten password via email-based token flow; and see the Argus version in the page footer for support.

## Findings Addressed
F-01

## Acceptance Criteria
- [ ] **AC-1:** Login page shows "Forgot password?" link below password input.
- [ ] **AC-2:** Click → form: email input → submit. Response shows generic "If that email exists, a reset link has been sent" (no account enumeration).
- [ ] **AC-3:** Backend: `POST /api/v1/auth/password-reset/request` — creates token (1h TTL) + sends email (via notification service).
- [ ] **AC-4:** Reset link: `/auth/reset?token=<jwt>` → form: new password + confirm → `POST /api/v1/auth/password-reset/confirm` with token + password.
- [ ] **AC-5:** Token single-use + stored in `password_reset_tokens` table.
- [ ] **AC-6:** Audit event `auth.password_reset_requested` + `auth.password_reset_completed`.
- [ ] **AC-7:** Rate limit: 5 reset requests per email per hour.
- [ ] **AC-8:** Footer shows "Argus v{version}" (from build info).

## Files to Touch
- `migrations/YYYYMMDDHHMMSS_password_reset_tokens.up.sql`
- `internal/api/auth/password_reset.go` (NEW)
- `web/src/pages/login/*.tsx`
- `web/src/pages/auth/reset.tsx` (NEW)
- Notification template: `password_reset_email`

## Risks & Regression
- **Risk 1 — Token leakage via email:** Use HTTPS, short TTL, single-use.
- **Risk 2 — Enumeration:** AC-2 generic response.

## Test Plan
- E2E: request reset → email received (check notif DB) → click link → set new password → login

## Plan Reference
Priority: P3 · Effort: M · Wave: 7

# FIX-205: Token Refresh Auto-retry on 401

## Problem Statement
JWT tokens expire after N minutes. Current FE behavior: user gets 401 → hook error state → user sees "Unauthorized" screen → manual re-login. Backend already exposes `/api/v1/auth/refresh` endpoint — FE HTTP client doesn't use it. Result: user bounced from page mid-task.

## User Story
As a user, I want Argus to transparently refresh my session when my token expires so I don't lose context mid-workflow.

## Architecture Reference
- Backend: `/api/v1/auth/refresh` endpoint (already implemented)
- Frontend: `web/src/lib/api.ts` axios interceptor OR fetch wrapper

## Findings Addressed
F-35

## Acceptance Criteria
- [ ] **AC-1:** FE HTTP client implements 401 response interceptor — on 401, call `/auth/refresh` with refresh token, retry original request with new access token.
- [ ] **AC-2:** Refresh token storage — httpOnly cookie (secure against XSS) preferred; fallback localStorage with explicit security note.
- [ ] **AC-3:** Single-flight refresh — if multiple requests 401 simultaneously, only ONE refresh call fires; others wait for result. No refresh storm.
- [ ] **AC-4:** Refresh failure handling — if refresh returns 401/403, clear session + redirect to /login with `?reason=session_expired&return_to=<current_path>`.
- [ ] **AC-5:** Refresh token TTL — documented (default 7d). Expiration time returned in access-token response; FE schedules pre-emptive refresh 5 minutes before expiry.
- [ ] **AC-6:** Silent refresh — no UI spinner/flash for auto-refresh; user continues uninterrupted.
- [ ] **AC-7:** Login response payload extended: `{token, refresh_token, expires_in, refresh_expires_in}`.
- [ ] **AC-8:** Backend `/auth/refresh` rate limit — 60 req/min per user (prevents abuse).

## Files to Touch
- `web/src/lib/api.ts` — interceptor
- `web/src/stores/auth.ts` — token + refresh_token storage, single-flight state
- `internal/api/auth/handler.go` — refresh endpoint response schema (if not complete)

## Risks & Regression
- **Risk 1 — Refresh loop:** Bug where refresh also 401s → infinite loop. Mitigation: detect refresh response 401 → hard logout.
- **Risk 2 — Concurrent tabs:** Tab A refreshes token, tab B uses stale. Mitigation: BroadcastChannel OR re-read from storage on each request.
- **Risk 3 — CSRF with cookie refresh:** If using cookie, implement CSRF token. Mitigation: keep refresh token as Bearer header initially; cookie as future hardening.

## Test Plan
- Unit: Single-flight behavior with mocked 401s + refresh mock
- Integration: 401 → auto-refresh → retry → success (E2E)
- Browser: Leave tab open past token expiry → verify no login prompt + API calls continue

## Plan Reference
Priority: P0 · Effort: S · Wave: 1

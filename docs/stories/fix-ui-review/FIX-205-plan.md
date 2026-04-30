# Implementation Plan: FIX-205 — Token Refresh Auto-retry on 401

## Goal

Extend the existing FE axios interceptor (already has single-flight) to add pre-emptive refresh scheduling and correct redirect-on-failure; extend the backend `/auth/refresh` response to include `expires_in` and `refresh_expires_in`; add per-session rate limiting to `/auth/refresh`.

---

## Architecture Context

### Components Involved

- **FE HTTP Client**: `web/src/lib/api.ts` — axios instance, single-flight interceptor (already implemented), response interceptor.
- **FE Auth Store**: `web/src/stores/auth.ts` — Zustand persist store; holds `token`, `user`, `permissions`, `isAuthenticated`.
- **FE JWT Util**: `web/src/lib/jwt.ts` — `decodeToken()` reads `exp` claim (display-only, not trust decisions).
- **BE Auth Handler**: `internal/api/auth/handler.go` — `Refresh()` handler; reads httpOnly cookie, returns `{token}`.
- **BE Middleware**: `internal/gateway/` — global RateLimiter runs BEFORE auth (step 5 in MIDDLEWARE.md chain). `/auth/refresh` is a public route (no JWT auth required).

### Data Flow — Reactive (401 intercept)

```
Any API call → 401 response
→ interceptor catches (already exists)
→ if not _retry and not isRefreshing → POST /auth/refresh (withCredentials, sends cookie)
→ BE verifies refresh_token cookie → returns { token, expires_in, refresh_expires_in }
→ FE: setToken(newToken) + setTokenExpiresAt(now + expires_in)
→ retry original request with new Authorization header
→ if refresh itself returns 401 → hard logout with ?reason=session_expired&return_to=<path>
```

### Data Flow — Pre-emptive (scheduler)

```
setAuth() / setToken() called
→ schedulePreemptiveRefresh(tokenExpiresAt - 5 min)
→ clearTimeout(existingTimer) + setTimeout(fn, delay)
→ fn calls refreshAccessToken() (same single-flight as reactive path)
→ on success: token + timer updated silently
→ on failure: hard logout (same redirect)
```

### API Specifications

#### `POST /api/v1/auth/refresh` (already exists — extend response)

- **Auth:** None (public route; reads httpOnly `refresh_token` cookie)
- **Request body:** `{}` (refresh token is in `SameSite=Strict HttpOnly` cookie)
- **Current response:**
  ```json
  { "status": "success", "data": { "token": "..." } }
  ```
- **Extended response (after T3a):**
  ```json
  {
    "status": "success",
    "data": {
      "token": "eyJ...",
      "expires_in": 3600,
      "refresh_expires_in": 604800
    }
  }
  ```
- **Error response:** `{ "status": "error", "error": { "code": "INVALID_REFRESH_TOKEN", "message": "Refresh token is invalid or has been revoked" } }`
- **Status codes:** 200 OK, 401 Unauthorized (bad/expired cookie), 429 Too Many Requests (rate limit)

> **SECURITY DECISION — AC-7 reconciliation:** `refresh_token` is NOT returned in the response body. It is already stored in an httpOnly `SameSite=Strict` cookie (Path `/api/v1/auth`) per the existing implementation in `handler.go:602-612`. Returning it in the body would be a security regression. AC-7 is interpreted as: add `expires_in` + `refresh_expires_in` fields; refresh_token stays in the cookie.

### Rate Limit Identity for `/auth/refresh` — AC-8

MIDDLEWARE.md step 5 (RateLimiter) runs BEFORE step 6 (Auth). `/auth/refresh` is a public route with no JWT auth — no `user_id` is available at the middleware layer. Options evaluated:

| Option | Pros | Cons |
|--------|------|------|
| IP-based at middleware | Fits existing pattern, zero handler change | Shared NAT/proxy collapses multiple users to one limit |
| SHA256(refresh_token cookie) at middleware | Per-session, computable pre-auth | Requires cookie read inside middleware |
| Inside handler after cookie parse | Per-user (user_id extractable), targeted | Bypasses standard chain, ad-hoc |

**Decision:** Use **in-handler rate limiting** (option 3). After parsing the cookie and before calling `svc.Refresh()`, extract the refresh token value, hash it, and check/increment a Redis key `ratelimit:refresh:{sha256[:16]}` with a 60/min sliding window. This is identical to the pattern used by `BulkRateLimiter` (wired via `r.With()`). Return `429` with standard envelope if exceeded. This is the safest approach with no middleware coupling changes.

---

## Prerequisites

- [x] FIX-204 completed (analytics fixes, independent of auth)
- [x] `web/src/lib/api.ts` has working single-flight framework (`isRefreshing`, `failedQueue`, `processQueue`)
- [x] `web/src/lib/jwt.ts` exposes `decodeToken()` with `exp` claim
- [x] `internal/api/auth/handler.go` `Refresh()` handler exists and reads httpOnly cookie

---

## What Already Exists (do NOT rewrite)

`web/src/lib/api.ts` lines 19–72 already implement:
- `isRefreshing` flag + `failedQueue` array — AC-3 single-flight
- On 401: queue pending requests, fire `POST /api/v1/auth/refresh`
- On refresh success: `setToken(newToken)`, process queue, retry original
- On refresh failure: `processQueue(error)`, `logout()`, `window.location.href = '/login'`

**Real gaps to fix:**
1. AC-4: redirect is `/login` without `?reason=session_expired&return_to=<path>`
2. AC-5: no pre-emptive refresh scheduler using JWT `exp` claim
3. AC-7/BE: `refreshResponse` struct only returns `token` — missing `expires_in`, `refresh_expires_in`
4. AC-8: no rate limit on `/auth/refresh`
5. Auth store: no `tokenExpiresAt` state field; `setAuth`/`setToken` don't trigger scheduler
6. Risk 2 (cross-tab): no BroadcastChannel / storage listener

---

## Tasks

### Task 1: FE interceptor — redirect fix + loop guard (extend existing)
- **Files:** Modify `web/src/lib/api.ts`
- **Depends on:** — (none)
- **Complexity:** low
- **Pattern ref:** Read `web/src/lib/api.ts` lines 36–94 — extend existing interceptor, do not rewrite
- **Context refs:** "Architecture Context > Data Flow — Reactive", "API Specifications", "What Already Exists"
- **What:**
  1. In the `catch (refreshError)` block (line 64), change `window.location.href = '/login'` to `window.location.href = \`/login?reason=session_expired&return_to=${encodeURIComponent(window.location.pathname + window.location.search)}\``
  2. Update `AuthRefreshResponse` interface at line 113 to add `expires_in?: number` and `refresh_expires_in?: number`
  3. In the try block after `const newToken = res.data.data.token`, also read `res.data.data.expires_in` and call `useAuthStore.getState().setTokenExpiresAt(Date.now() + (res.data.data.expires_in ?? 3600) * 1000)` — this method is added in Task 2
  4. Refresh loop guard: the existing `_retry` flag already prevents looping (a refresh failure throws and goes to catch → logout). Confirm via code comment but no code change needed.
- **Verify:** `grep -n 'return_to' web/src/lib/api.ts` returns a match

### Task 2: FE auth store — tokenExpiresAt + pre-emptive scheduler
- **Files:** Modify `web/src/stores/auth.ts`
- **Depends on:** Task 1 (uses `setTokenExpiresAt`)
- **Complexity:** medium
- **Pattern ref:** Read `web/src/stores/auth.ts` — follow same Zustand state + action patterns; read `web/src/lib/jwt.ts` for `decodeToken` usage
- **Context refs:** "Architecture Context > Data Flow — Pre-emptive", "What Already Exists", "Story-Specific Compliance Rules"
- **What:**
  1. Add `tokenExpiresAt: number | null` to `AuthState` interface and initial state (null)
  2. Add `setTokenExpiresAt: (ms: number) => void` action that sets `tokenExpiresAt` and calls `schedulePreemptiveRefresh(ms)`
  3. Add `refreshAccessToken: () => Promise<void>` action — delegates to the interceptor's same single-flight: if `isRefreshing` is already true (module-level var in api.ts), do nothing; else call `POST /api/v1/auth/refresh` directly and call `setToken()` + `setTokenExpiresAt()`
  4. Add module-level (outside store) `let refreshTimer: ReturnType<typeof setTimeout> | null = null`
  5. Add `schedulePreemptiveRefresh(expiresAtMs: number)` — clears existing timer, computes `delay = expiresAtMs - Date.now() - 5 * 60 * 1000`, if `delay > 0` set a new timer; on fire call `refreshAccessToken()`
  6. In `setAuth()`, after setting state, derive `tokenExpiresAt` from JWT `exp` claim via `decodeToken(token)?.exp * 1000` and call `schedulePreemptiveRefresh()`
  7. In `setToken()`, same derivation
  8. In `logout()`, clear the timer (`clearTimeout(refreshTimer)`)
  9. Add `tokenExpiresAt` to the `partialize` list so it persists across page reloads (allows scheduler to re-arm on mount via a `useEffect` in `App.tsx` or `main.tsx`)
  10. Add BroadcastChannel listener: on mount (module init), listen to `argus-auth-broadcast` channel; when `{type:'token_refreshed', token, expiresAt}` message received, call `setToken(token)` + `setTokenExpiresAt(expiresAt)` to sync other tabs
  11. In refreshAccessToken success path, broadcast `{type:'token_refreshed', token: newToken, expiresAt: ...}` via same channel
- **Verify:** `npx tsc --noEmit` passes; store has `tokenExpiresAt` field

### Task 3a: BE — extend refresh response schema (expires_in, refresh_expires_in)
- **Files:** Modify `internal/api/auth/handler.go`
- **Depends on:** — (none, independent of FE tasks)
- **Complexity:** low
- **Pattern ref:** Read `internal/api/auth/handler.go` lines 75–78 (`refreshResponse` struct) and lines 273–296 (`Refresh()` handler) — extend in place
- **Context refs:** "API Specifications", "Architecture Context > Components Involved", "SECURITY DECISION — AC-7 reconciliation"
- **What:**
  1. Update `refreshResponse` struct to add `ExpiresIn int \`json:"expires_in"\`` and `RefreshExpiresIn int \`json:"refresh_expires_in"\``
  2. In `Refresh()` handler after calling `h.svc.Refresh()`, populate `ExpiresIn: int(h.jwtExpiry.Seconds())` and `RefreshExpiresIn: h.refreshMaxAge`
  3. Update `loginResponse` struct similarly: add `ExpiresIn int \`json:"expires_in,omitempty"\`` and `RefreshExpiresIn int \`json:"refresh_expires_in,omitempty"\``. Populate in `Login()` handler when `!result.Requires2FA` (fully authenticated path only)
  4. Do NOT add `refresh_token` to any response body — it stays httpOnly cookie only
- **Verify:** `go build ./internal/api/auth/...` passes; `go vet ./internal/api/auth/...` clean

### Task 3b: BE — in-handler rate limit for /auth/refresh
- **Files:** Modify `internal/api/auth/handler.go`; may create `internal/api/auth/ratelimit.go` if helper grows beyond 20 lines
- **Depends on:** Task 3a (adds to `Refresh()` handler)
- **Complexity:** medium
- **Pattern ref:** Read `internal/gateway/bulk_ratelimit.go` for existing Redis sliding-window pattern; read `internal/api/auth/handler.go` `Refresh()` handler for insertion point
- **Context refs:** "Rate Limit Identity for /auth/refresh — AC-8", "Architecture Context > Components Involved"
- **What:**
  1. Inject `redis.Client` into `AuthHandler` (add field `redis redis.Cmdable`, wire in `NewAuthHandler` or add a `WithRedis(r redis.Cmdable)` builder method following the `WithAPIKeyStore` pattern at line 47)
  2. In `Refresh()` handler, AFTER extracting the cookie value and BEFORE calling `svc.Refresh()`:
     - Compute key: `sha := sha256.Sum256([]byte(cookie.Value)); key := fmt.Sprintf("ratelimit:refresh:%x", sha[:8])` (16-char hex = 8 bytes)
     - Use Redis INCR + EXPIRE (sliding window, 60s window, limit 60): if count > 60 → write 429 with standard error envelope (`apierr.WriteError(w, http.StatusTooManyRequests, apierr.CodeRateLimited, "Too many refresh requests")`) and return
  3. If `redis` is nil (legacy/test setup without Redis), skip rate limit silently — do not panic
  4. No changes to the global middleware chain
- **Verify:** `go build ./internal/api/auth/...` passes; `go test ./internal/api/auth/...` passes

### Task 4: Tests — FE single-flight + redirect + scheduler unit tests
- **Files:** Create `web/src/lib/__tests__/api-interceptor.test.ts`; optionally extend `web/src/stores/__tests__/auth.test.ts` if it exists
- **Depends on:** Task 1, Task 2
- **Complexity:** low
- **Pattern ref:** Glob `web/src/**/*.test.ts` or `web/src/**/*.spec.ts` for existing test patterns; use vitest + msw or axios-mock-adapter
- **Context refs:** "What Already Exists", "Architecture Context > Data Flow — Reactive", "Architecture Context > Data Flow — Pre-emptive", "Acceptance Criteria Mapping"
- **What:**
  1. **Single-flight test (AC-3):** Fire 2 concurrent requests that both get 401 mocked → mock refresh returning new token → assert refresh endpoint called exactly once → both original requests retried with new token
  2. **Redirect test (AC-4):** Simulate refresh returning 401 (double-fail) → assert `window.location.href` set to `/login?reason=session_expired&return_to=...` with current path encoded
  3. **Loop guard test (Risk 1):** If refresh POST returns 401 (the refresh call itself) → verify logout called once, no recursive refresh attempt
  4. **Scheduler test (AC-5):** Call `setAuth(user, token)` with a JWT whose `exp` is 10 min from now → assert `schedulePreemptiveRefresh` was scheduled with ~5 min delay (mocked setTimeout); fast-forward timer → assert refresh called once
  5. **BroadcastChannel test (Risk 2):** Simulate `message` event on `argus-auth-broadcast` with `{type:'token_refreshed', token:'newTok', expiresAt: ...}` → assert `token` in store updated to `'newTok'`
- **Verify:** `npx vitest run web/src/lib/__tests__/api-interceptor.test.ts` all pass

---

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| AC-1: 401 → refresh → retry | Existing interceptor (Task 1 extends) | Task 4 single-flight test |
| AC-2: refresh_token in httpOnly cookie | Existing handler (no change) | Existing handler_test.go |
| AC-3: single-flight (no refresh storm) | Existing interceptor (unchanged) | Task 4 single-flight test |
| AC-4: refresh failure → redirect with reason+return_to | Task 1 | Task 4 redirect test |
| AC-5: pre-emptive refresh 5 min before expiry | Task 2 (scheduler) | Task 4 scheduler test |
| AC-6: silent refresh (no spinner) | Existing interceptor bypasses React state; scheduler calls API directly | Visual review |
| AC-7: expires_in + refresh_expires_in in response | Task 3a | Task 4 (reads from mock response) |
| AC-8: /auth/refresh rate limit 60/min per session | Task 3b | Task 3b verify (go test) |

---

## Story-Specific Compliance Rules

- **Security:** `refresh_token` MUST NOT appear in any response body. It is already an httpOnly `SameSite=Strict` cookie. Adding it to a response body would be a regression against G-036 (JWT + refresh token security).
- **AC-7 interpretation:** "Login response payload extended: `{token, refresh_token, expires_in, refresh_expires_in}`" — `refresh_token` is interpreted as "refresh mechanism" (cookie-based). Only `expires_in` and `refresh_expires_in` are added to the JSON body.
- **API:** All BE responses use standard envelope (`apierr.WriteSuccess`, `apierr.WriteError`) — no raw JSON writes.
- **FE:** `decodeToken()` in `lib/jwt.ts` is display-only — NEVER use for trust decisions. Pre-emptive scheduler uses `exp` to compute timer delay only; the actual token validation is server-side.
- **Rate limit:** Custom in-handler implementation (not middleware chain change). If Redis unavailable, degrade gracefully (skip limit, log warning).
- **Cross-tab:** BroadcastChannel `argus-auth-broadcast` — message schema `{type:'token_refreshed', token: string, expiresAt: number}`.

---

## Bug Pattern Warnings

- **PAT-001** (double-writer on metrics): Not applicable — this story has no metrics layer.
- **PAT-007** (mutex ≠ happens-before for goroutines): The single-flight `isRefreshing` flag in `api.ts` is module-level and synchronous within the JS event loop — no race condition possible. No changes to Go concurrent code in this story.
- **PAT-005** (masked-secret sentinel round-trip): Not applicable — no secret fields in auth refresh flow.
- **General refresh-loop risk:** Existing `_retry` flag in `api.ts` prevents recursive refresh. Task 4 test 3 explicitly validates this path.

---

## Tech Debt

No tech debt items in ROUTEMAP targeting FIX-205.

---

## Mock Retirement

No `src/mocks/` directory exists. No mock retirement needed.

---

## Risks & Mitigations

| Risk | Severity | Mitigation |
|------|----------|-----------|
| Infinite refresh loop | High | `_retry` flag already prevents; Task 4 test validates; refresh 401 → catch → logout |
| Concurrent tab token staleness | Medium | BroadcastChannel in Task 2 syncs tabs; fallback: interceptor always re-reads store state from `useAuthStore.getState()` on each request |
| Pre-emptive scheduler timer leak | Medium | `logout()` calls `clearTimeout(refreshTimer)`; page unload clears all timers automatically |
| Redis unavailable (rate limit) | Low | Nil-check guard in Task 3b → degrade silently |
| AC-7 refresh_token body exposure | Critical | Explicitly NOT done — cookie stays httpOnly. Plan documents decision. |
| Scheduler fires after logout | Low | `clearTimeout` in `logout()` + guard: if `!isAuthenticated` when timer fires, skip refresh |

# api-interceptor Manual Verification

FE test harness (vitest/jsdom) is not configured in this project. These scenarios serve as step-by-step verification instructions executable in a browser DevTools session against a running Argus instance (`make up`).

Prerequisite: Log in as admin@argus.io and have Chrome/Firefox DevTools open on the Network tab.

---

## Scenario 1 — Single-flight (AC-3): Two concurrent 401s trigger exactly 1 refresh

**Code path:** `web/src/lib/api.ts` — `isRefreshing` flag + `failedQueue` array.

**Steps:**

1. Open the app at http://localhost:8084 and log in.
2. In DevTools Console, paste:
   ```js
   // Force-expire the token so the next API calls return 401
   const s = JSON.parse(localStorage.getItem('argus-auth'))
   s.state.token = 'expired.token.value'
   localStorage.setItem('argus-auth', JSON.stringify(s))
   ```
3. Reload the page (so Zustand rehydrates the expired token).
4. Navigate to a page that fires multiple API calls on mount (e.g., Dashboard at `/`).
5. In the Network tab, filter by `auth/refresh`.

**Expected result:**
- Exactly **one** POST to `/api/v1/auth/refresh` appears, regardless of how many concurrent 401 responses were received.
- All original failing requests complete successfully after the refresh (their status shows 200 in the Network tab after retry).

**Code reference:** `api.ts` lines 42-51 — when `isRefreshing === true`, subsequent 401s push to `failedQueue` instead of calling refresh. The queue is drained in `processQueue()` once the single refresh resolves.

---

## Scenario 2 — Redirect on refresh failure (AC-4)

**Code path:** `web/src/lib/api.ts` catch block (lines 66-71) — `logout()` + `window.location.href` redirect.

**Steps:**

1. Open http://localhost:8084/sims?filter=active and ensure you are logged in.
2. In DevTools Network tab, add a request block rule for `/api/v1/auth/refresh` (right-click → Block request URL in Chrome) OR in the Console, intercept axios:
   ```js
   // Simulate refresh endpoint returning 401
   const orig = window.axios?.post
   // Alternatively, use the DevTools "Override" feature to return HTTP 401 for /api/v1/auth/refresh
   ```
   Simpler alternative: Stop the backend server (`make down`) then trigger a request.
3. Force-expire the token (step 2 from Scenario 1), reload the page to rehydrate.
4. Wait for the page to attempt an API call → 401 → attempt refresh → refresh also fails.

**Expected result:**
- Browser navigates to:
  ```
  /login?reason=session_expired&return_to=%2Fsims%3Ffilter%3Dactive
  ```
- URL contains both `reason=session_expired` and `return_to` with the URL-encoded original path+query.
- The login page shows a "session expired" message (if implemented) or at minimum the URL parameters are correct.

**Code reference:** `api.ts` line 70 — `window.location.href = '/login?reason=session_expired&return_to=' + encodeURIComponent(window.location.pathname + window.location.search)`.

---

## Scenario 3 — Loop guard (Risk 1): Refresh 401 does not retry infinitely

**Code path:** `web/src/lib/api.ts` — `_retry` flag on `originalRequest`.

**Steps:**

1. With the refresh endpoint blocked (same setup as Scenario 2, step 2), observe the Network tab.
2. Trigger a 401 (force-expired token + page reload).

**Expected result:**
- Network tab shows exactly **one** POST to `/api/v1/auth/refresh`.
- There is no second refresh attempt. The interceptor does NOT retry the refresh response itself because:
  - The refresh call uses the plain `axios` instance (not `api`), so the response interceptor is not invoked on it.
  - Even if `api` were used, `originalRequest._retry = true` (set at line 54) prevents re-entry.
- Browser redirects to `/login?reason=session_expired&...` once.

**Code reference:** `api.ts` line 54 — `originalRequest._retry = true` set before the refresh attempt. Line 41 — `!originalRequest._retry` guards re-entry. Line 58 — refresh uses bare `axios.post`, not `api.post`, so the response interceptor is bypassed.

---

## Scenario 4 — Scheduler (AC-5): setAuth schedules preemptive refresh 5 minutes before expiry

**Code path:** `web/src/stores/auth.ts` — `schedulePreemptiveRefresh()` + `setTimeout`.

**Steps:**

1. Open DevTools → Application → Local Storage → `argus-auth`. Note the `tokenExpiresAt` value after login.
2. In DevTools Console:
   ```js
   const store = window.__ZUSTAND_DEVTOOLS_STORE__ // if zustand devtools is wired
   // Alternative: observe directly
   const s = JSON.parse(localStorage.getItem('argus-auth'))
   const expiresAt = s.state.tokenExpiresAt
   const fiveMinBefore = new Date(expiresAt - 5 * 60 * 1000)
   console.log('Preemptive refresh fires at:', fiveMinBefore.toISOString())
   console.log('That is in:', Math.round((fiveMinBefore - Date.now()) / 1000 / 60), 'minutes')
   ```
3. To verify the timer fires: use a JWT with a short expiry (e.g., 6 minutes). Craft one or modify the backend `JWT_EXPIRY` config for testing. Then wait 1 minute after login and observe a POST to `/api/v1/auth/refresh` in the Network tab.

**Alternative (fast path) — Console simulation:**
```js
// Manually call setAuth with a token expiring in 6 minutes
const now = Math.floor(Date.now() / 1000)
// A real decoded-valid JWT is needed; use the current token but override tokenExpiresAt
import('/src/stores/auth.js').then(m => {
  m.useAuthStore.getState().setTokenExpiresAt(Date.now() + 6 * 60 * 1000)
})
// Then wait 1 minute; the preemptive refresh fires at 5 minutes before expiry (i.e., 1 minute from now)
```

**Expected result:**
- Approximately 1 minute after calling `setTokenExpiresAt(now + 6min)`, a POST to `/api/v1/auth/refresh` fires automatically.
- No user interaction required.

**Code reference:** `auth.ts` lines 12-24 — `schedulePreemptiveRefresh` computes `delay = expiresAtMs - Date.now() - 5*60*1000` and sets a `setTimeout`.

---

## Scenario 5 — BroadcastChannel (Risk 2): Token update propagates across tabs

**Code path:** `web/src/stores/auth.ts` — `broadcastChannel.addEventListener('message', ...)` at lines 194-203.

**Steps:**

1. Open http://localhost:8084 in **two separate tabs** (Tab A and Tab B), both logged in.
2. In Tab A's DevTools Console:
   ```js
   const ch = new BroadcastChannel('argus-auth-broadcast')
   const newToken = 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ0ZXN0IiwiZXhwIjo5OTk5OTk5OTk5fQ.test'
   ch.postMessage({
     type: 'token_refreshed',
     token: newToken,
     expiresAt: Date.now() + 60 * 60 * 1000
   })
   ```
3. Switch to **Tab B**. In Tab B's Console:
   ```js
   JSON.parse(localStorage.getItem('argus-auth')).state.token
   ```

**Expected result:**
- Tab B's localStorage shows the new token value (`eyJhbGci...`) after the message.
- Tab B did not need to call the refresh endpoint — it received the token via `BroadcastChannel`.

**Note:** `BroadcastChannel` messages are same-origin, same-browser. They do NOT work across different browsers or incognito/normal mode pairs.

**Code reference:** `auth.ts` lines 194-203 — the listener calls `useAuthStore.getState().setToken(msg.token)` and updates `tokenExpiresAt` when a `token_refreshed` message arrives.

---

## Notes for Future Vitest Setup

When the FE test harness is wired (vitest + jsdom + @testing-library/react), these scenarios should be ported to `api-interceptor.test.ts` using:
- `axios-mock-adapter` for mocking HTTP requests
- `vi.useFakeTimers()` for the scheduler scenario
- A `BroadcastChannel` polyfill or vitest `jsdom` built-in (available in jsdom >= 20)
- `localStorage` clearing between tests to reset Zustand persistence

Suggested devDependencies to add at that time:
```json
{
  "vitest": "^1.x",
  "@vitest/ui": "^1.x",
  "@testing-library/react": "^14.x",
  "@testing-library/user-event": "^14.x",
  "jsdom": "^24.x",
  "axios-mock-adapter": "^1.x"
}
```

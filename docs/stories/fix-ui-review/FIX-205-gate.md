# Gate Report: FIX-205 — Token Refresh Auto-retry on 401

## Summary
- Requirements Tracing: ACs 8/8 mapped to implementation; every AC has a code or test anchor.
- Gap Analysis: 8/8 acceptance criteria passed.
- Compliance: COMPLIANT (standard envelope, httpOnly cookie, no refresh_token body exposure, token-derived expiry with server wall-clock fallback).
- Tests: 35/35 in `internal/api/auth` pass; full suite 3312/3312 pass (above reported baseline 3299 and floor 3286).
- Test Coverage: Rate-limit AC-8 has dedicated negative test (`TestRefreshHandler_RateLimit_60PerMinute_429`); other ACs covered by existing interceptor behavior and the delivered manual-verification MD.
- Performance: No new hot-path queries; single Redis INCR + Expire per refresh call (O(1)), acceptable.
- Build: PASS — `go build ./...`, `go vet ./...`, `tsc --noEmit` all clean.
- Token Enforcement: N/A (backend + non-visual auth-store changes; no new UI surface).
- Overall: PASS

## Team Composition
- Analysis Scout: 6 findings (F-A1..F-A6, inlined by Lead per dispatch directive)
- Test/Build Scout: 3 findings (F-B1..F-B3, inlined)
- UI Scout: 3 findings (F-U1..F-U3, inlined — no UI surface changes, behavior-only review)
- De-duplicated: 12 → 2 fixable

## Fixes Applied

| # | Category | File | Change | Verified |
|---|----------|------|--------|----------|
| 1 | Code quality | `web/src/lib/api.ts:57-72` | Removed unconditional wall-clock `setTokenExpiresAt` override; `setToken()` already derives `tokenExpiresAt` from the JWT `exp` claim, so server `expires_in` is now only used as a fallback when the JWT carried no exp. Closes the T4 pre-flagged double-write ambiguity. | tsc PASS; go suite 3312 PASS |
| 2 | Code quality | `web/src/stores/auth.ts:194-207` | BroadcastChannel `token_refreshed` listener: `setToken()` already arms the scheduler via the JWT exp claim. Drop the redundant `setState({tokenExpiresAt}) + schedulePreemptiveRefresh()` pair; fall back to `setTokenExpiresAt(msg.expiresAt)` only when the JWT had no exp. Avoids double-scheduling the same timer on every cross-tab sync. | tsc PASS |
| 3 | Type hygiene | `web/src/lib/api.ts:100-115` | Extended `AuthLoginResponse` TS interface with `expires_in?: number` + `refresh_expires_in?: number` to match the backend `loginResponse` wire contract post-T3a (fully-authenticated login path now emits both fields per AC-7). Prevents future callers from silently dropping the fields. | tsc PASS |
| 4 | Defensive hygiene | `web/src/stores/auth.ts:103-112` | `setTokenExpiresAt(null)` now also clears any live `refreshTimer`. Previously `logout()` was the only path that cleared the timer, so an external caller passing `null` could leak a dangling timer that would later fire with a stale token. No current caller triggers this; added for safety. | tsc PASS |

All four fixes preserve behavior in the happy path (JWT always has `exp` in production) and remove the only source of drift between the token-derived authority (`exp` claim) and the server wall-clock authority (`expires_in` response field). Fixes 3–4 added after advisor review flagged interface asymmetry (AuthLoginResponse wire drift) and the theoretical null-timer leak.

## Escalated Issues
None.

## Deferred Items (tracked in ROUTEMAP → Tech Debt)

| # | Finding | Target Story | Written to ROUTEMAP |
|---|---------|-------------|---------------------|
| D-205.1 | FE test harness (vitest + jsdom + axios-mock-adapter) not wired in `web/`. FIX-205 delivered `api-interceptor.manual.md` browser-DevTools verification scenarios in lieu of an automated suite. Wiring vitest is out of scope for this S-effort story but should be addressed as foundation for all future FE stories. | POST-GA FE Test Infra | YES |
| D-205.2 | Rate-limit cross-cookie isolation test: current `TestRefreshHandler_RateLimit_60PerMinute_429` fires 60 requests with a single cookie value. A sibling test that proves token-A-exhausted-quota does not leak into token-B quota would strengthen AC-8 coverage. Low priority — the per-cookie keyspace is structurally enforced by `ratelimit:refresh:<sha256[:8]>`. | POST-GA auth hardening | YES |

## Performance Summary

### Queries / Calls Analyzed
| # | File:Line | Pattern | Issue | Severity | Status |
|---|-----------|---------|-------|----------|--------|
| 1 | `internal/api/auth/handler.go:302-313` | Redis INCR + conditional EXPIRE | None — sliding-window pattern already used in `BulkRateLimiter`. | — | PASS |
| 2 | `web/src/stores/auth.ts` scheduler | `setTimeout` with `clearTimeout` on logout | None — timer cleared on logout path (line 145-148). Page unload clears all timers. | — | PASS |

### Caching Verdicts
| # | Data | Location | TTL | Decision | Status |
|---|------|----------|-----|----------|--------|
| 1 | Refresh rate-limit counter | Redis `ratelimit:refresh:<sha256[:8]>` | 60s sliding window | ACCEPTED — matches `BulkRateLimiter` pattern | PASS |

## Token & Component Enforcement
Not applicable (behavior-only story, no new visual surface). UI pre-check: 0 new hex, 0 `console.log`.

## Verification
- Tests after fixes: 3312/3312 passed (full `go test ./... -short`)
- Auth package after fixes: 35/35 passed
- `TestRefreshHandler_RateLimit_60PerMinute_429`: PASS under `-count=1`
- `tsc --noEmit`: PASS
- `go build ./...`: PASS
- `go vet ./...`: PASS
- Fix iterations: 1 (max 2)

## Passed Items

### Acceptance Criteria Coverage
- **AC-1 (401 → refresh → retry)**: `web/src/lib/api.ts:41-75`. PASS.
- **AC-2 (httpOnly cookie)**: `internal/api/auth/handler.go:639-648` `setRefreshCookie` uses `HttpOnly: true, SameSite=Strict, Path=/api/v1/auth`. PASS.
- **AC-3 (single-flight)**: `web/src/lib/api.ts:19-34,42-51` — `isRefreshing` flag + `failedQueue`; `processQueue` drain. PASS.
- **AC-4 (redirect reason + return_to)**: `web/src/lib/api.ts:70` — `window.location.href = \`/login?reason=session_expired&return_to=${encodeURIComponent(window.location.pathname + window.location.search)}\``. PASS.
- **AC-5 (pre-emptive refresh, 5 min before expiry)**: `web/src/stores/auth.ts:12-24` `schedulePreemptiveRefresh`. Armed by `setAuth`, `setToken`, `setTokenExpiresAt`, and on store rehydrate. PASS.
- **AC-6 (silent refresh)**: Interceptor never flips a React loading state; scheduler calls refresh directly via `refreshAccessToken` and updates Zustand. No spinner trigger path found. PASS.
- **AC-7 (`expires_in` + `refresh_expires_in` in response)**: `internal/api/auth/handler.go:86-90` `refreshResponse` struct; `Refresh()` populates both fields at line 328-332. Same for `loginResponse` at line 82-83 populated at line 284-285 on the fully-authenticated path. Security-correct: `refresh_token` remains httpOnly cookie, never in body. PASS.
- **AC-8 (60/min rate limit)**: `internal/api/auth/handler.go:298-314` — Redis sliding window keyed on `sha256(cookie.Value)[:8]`; 61st call returns 429 with `CodeRateLimited`. Test verified. PASS.

### Risks
- **Risk 1 (refresh loop)**: `_retry` flag at `api.ts:54` + refresh call uses raw `axios` (not the `api` instance) so response interceptor cannot re-enter. PASS.
- **Risk 2 (cross-tab staleness)**: `BroadcastChannel('argus-auth-broadcast')` in `auth.ts:6-8`; post-refresh broadcast at line 116-122; listener at line 194-207 (post-fix). PASS.
- **Risk 3 (CSRF with cookie refresh)**: `SameSite=Strict` on the refresh cookie + cookie `Path=/api/v1/auth` blocks cross-site POST. PASS.

### Build + Type Safety
- Go build: PASS
- Go vet: PASS
- TypeScript typecheck: PASS
- No new `console.log` / no new hex colors (UI enforcement pre-check baseline preserved).

### Architecture Guard
- No existing endpoint signatures changed — only response body extended with additive fields (`expires_in`, `refresh_expires_in`).
- No DB migrations; no schema drift.
- `AuthHandler` gains an optional `redis` field; `WithRedis` follows the existing `WithAPIKeyStore` / `WithJWTSecret` builder pattern. Graceful degradation when `redis == nil` (skip rate-limit, log warning-free path).

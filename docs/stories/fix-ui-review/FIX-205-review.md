# Post-Story Review: FIX-205 — Token Refresh Auto-retry on 401

> Date: 2026-04-20

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| FIX-206 | Orphan cleanup + FK — no auth-layer overlap. Independent. | NO_CHANGE |
| FIX-207 | Session/CDR data integrity — no auth-layer overlap. Independent. | NO_CHANGE |
| FIX-208 | Cross-tab aggregation — no auth-layer overlap. Independent. | NO_CHANGE |
| FIX-228 | Forgot Password Flow — FE pages will use the `api` axios instance and therefore inherit the single-flight 401 interceptor automatically. No code change needed in FIX-228; just note that the `/auth/password-reset/*` endpoints are public routes (no JWT) and should NOT trigger the 401→refresh path. The interceptor already guards on `!originalRequest._retry` and the refresh failure path exits cleanly. | NO_CHANGE (report-only note) |
| FIX-245 | Remove 5 admin sub-pages — unrelated to auth flow. | NO_CHANGE |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| `docs/architecture/api/_index.md` | API-001 description updated: notes `expires_in` + `refresh_expires_in` in fully-authenticated login response; refresh_token stays httpOnly cookie. API-002 description updated: notes response body `{token, expires_in, refresh_expires_in}`, rate-limit (60/min SHA-256 sliding window), FE single-flight + BroadcastChannel + pre-emptive scheduler; detail ref extended to FIX-205 story. | UPDATED |
| `docs/ARCHITECTURE.md` | `## Security Architecture → ### Authentication Flow` block extended: Token Refresh path now documents in-handler rate limit, `expires_in`/`refresh_expires_in` in response body, FE Refresh Interceptor description (single-flight, setToken JWT-exp authority, failure redirect, pre-emptive scheduler, BroadcastChannel cross-tab sync). | UPDATED |
| `docs/USERTEST.md` | `## FIX-205:` section appended — 6 browser DevTools scenarios (single-flight AC-3, redirect AC-4, loop guard Risk 1, scheduler AC-5, BroadcastChannel Risk 2, rate-limit AC-8 bash script). Sourced directly from `web/src/lib/__tests__/api-interceptor.manual.md`. | UPDATED |
| `docs/brainstorming/decisions.md` | DEV-260: single-flight interceptor (module-level mutex + failedQueue + bare-axios bypass + BE Redis rate limit). DEV-261: BroadcastChannel message schema `{type, token, expiresAt}`. DEV-262: pre-emptive scheduler pattern (module-level timer, 5-min window, logout guard). DEV-263: JWT-exp-priority-over-expires_in (JWT `exp` wins; `expires_in` is defensive fallback). | UPDATED |
| `docs/brainstorming/bug-patterns.md` | PAT-010: single-flight pattern for dependency-triggered refresh — module-level mutex + failedQueue prevents N parallel refreshes from the same expiry trigger. | UPDATED |
| `docs/ROUTEMAP.md` | FIX-205 status `[~] IN PROGRESS (Review)` → `[x] DONE (2026-04-20)`. REVIEW changelog entry added. | UPDATED |
| `docs/ARCHITECTURE.md` | NO_CHANGE on Makefile, CLAUDE.md, SCREENS.md, FRONTEND.md, GLOSSARY.md, FUTURE.md — behavior-only story with no new Docker services, ports, env vars, visual surfaces, or domain terms. | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 0
- API index API-002 previously described only "Refresh JWT token / Refresh Token" — now aligned with the extended response and rate-limit. API-001 similarly updated for login response field additions. No contradiction with STORY-003 which still owns the canonical implementation reference.
- ARCHITECTURE.md auth flow now matches the implementation delivered by FIX-205 and the VAL-035..038 decisions.

## Decision Tracing

- Decisions checked: VAL-035 (AC-7 refresh_token cookie-only), VAL-036 (JWT-exp-priority), VAL-037 (in-handler rate limit), VAL-038 (FE test harness deferral)
- All 4 VAL decisions: reflected in implementation (Gate verified), documented in ARCHITECTURE.md (updated this review), captured as DEV-260..263 (written this review)
- Orphaned (approved but not applied): 0

## USERTEST Completeness

- Entry exists: YES (appended in this review)
- Type: UI scenarios (6 manual DevTools scenarios + 1 curl-based backend rate-limit scenario)

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting this story: 2 (D-053, D-054) — both deferred by gate, both written to ROUTEMAP correctly
- Already ✓ RESOLVED by Gate: 0 (both are OPEN by design — deferred to POST-GA)
- Resolved by Reviewer (Gate missed marking): 0
- NOT addressed (CRITICAL): 0

| ID | Finding | Status | Note |
|----|---------|--------|------|
| D-053 | FE test harness (vitest + jsdom + axios-mock-adapter) not wired | OPEN | Target: POST-GA FE test infra. Gate correctly deferred. `api-interceptor.manual.md` serves as interim evidence. |
| D-054 | Per-cookie rate-limit isolation test (token A quota does not leak into token B) | OPEN | Target: POST-GA auth hardening. Structurally enforced by SHA-256 keyspace; no data coincidence possible without code change. |

## Mock Status

- No `src/mocks/` directory exists. Not applicable.

## Issues

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | `docs/architecture/api/_index.md` API-001 and API-002 rows did not mention `expires_in`/`refresh_expires_in` fields or the FE interceptor architecture added by FIX-205. | NON-BLOCKING | FIXED | API-001 description updated to note `expires_in` + `refresh_expires_in` on fully-authenticated login path. API-002 description updated to document response body fields, rate-limit mechanism, and FE single-flight + BroadcastChannel + pre-emptive scheduler. Detail ref includes link to FIX-205 story. |
| 2 | `docs/ARCHITECTURE.md` Authentication Flow block did not reflect FE interceptor architecture, pre-emptive scheduler, BroadcastChannel cross-tab sync, or in-handler rate limit added by FIX-205. | NON-BLOCKING | FIXED | `### Authentication Flow` block extended with `FE Refresh Interceptor` section documenting all four behaviors. |
| 3 | `docs/USERTEST.md` had no FIX-205 entry — UI story (behavior-affecting auth changes) without test scenarios. | NON-BLOCKING | FIXED | Appended `## FIX-205:` section with 6 manual DevTools scenarios covering all ACs and risk paths. |
| 4 | `docs/brainstorming/decisions.md` had no DEV-NNN entries for FIX-205 implementation patterns (single-flight, BroadcastChannel, scheduler, JWT-exp priority). VAL-035..038 captured Gate reconciliation decisions but not the architectural implementation patterns. `docs/brainstorming/bug-patterns.md` had no FE single-flight pattern entry. | NON-BLOCKING | FIXED | DEV-260..263 added to decisions.md covering all four patterns. PAT-010 added to bug-patterns.md for the single-flight dependency-triggered refresh pattern (PAT-007..009 already taken in that file by FIX-202/203/204). |

## Project Health

- Stories completed: 4/30 (FIX-201..205, first 4 of Wave 1 done; FIX-205 = Wave 1 story 4)
- Current phase: UI Review Remediation — Wave 1
- Next story: FIX-206 (Orphan Operator IDs Cleanup + FK Constraints + Seed Fix) — Wave 1, P0, M
- Blockers: None (FIX-206 is independent; D-053/D-054 deferred to POST-GA with no impact on upcoming stories)

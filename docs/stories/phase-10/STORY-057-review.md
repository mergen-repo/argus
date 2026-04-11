# Post-Story Review: STORY-057 — Data Accuracy & Missing Endpoints

> Date: 2026-04-12

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| STORY-058 | Directly unblocked: SIM detail tab hooks (sessions, usage) now return real data from API-051/API-052. ErrorBoundary + code-split work can proceed on stable endpoints. No assumption changes. | NO_CHANGE |
| STORY-060 | AAA Protocol Correctness depends on STORY-057 being done (session list for SIM tab verified). No scope change. | NO_CHANGE |
| STORY-061 | eSIM Model Evolution depends on STORY-057 — no scope overlap, dependency satisfied. | NO_CHANGE |
| STORY-063 | Backend Implementation Completeness depends on STORY-057. API-035/043/051/052 are now DONE and should not be re-implemented. STORY-063 AC list should be verified to not duplicate these endpoints. | NO_CHANGE |
| STORY-070 | Frontend Real-Data Wiring: AC-1 (dashboard sparklines) and AC-2 (SIM detail UsageTab) are now fully satisfied by STORY-057. STORY-070 should skip those ACs or mark them pre-done at plan time. | NO_CHANGE |
| STORY-075 | Cross-Entity Context: PATCH /sims/:id (API-043) and /sims/:id/sessions (API-051) are now available for cross-entity enrichment panels. | NO_CHANGE |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| docs/architecture/api/_index.md | API-035, API-043, API-051, API-052 story references updated from placeholder STORY-011/017/034 to STORY-057 | UPDATED |
| docs/architecture/CONFIG.md | Added `AUTH_JWT_REMEMBER_ME_TTL` (duration, default 168h) to Authentication & Security table | UPDATED |
| docs/brainstorming/decisions.md | Added DEV-140: remember_me 7d JWT TTL extension decision | UPDATED |
| docs/ROUTEMAP.md | STORY-057 marked [x] DONE (2026-04-12); phase counter updated to 2/22; Change Log entry added | UPDATED |
| GLOSSARY | No new domain terms introduced | NO_CHANGE |
| ARCHITECTURE | No structural changes — existing analytics, store, and auth packages extended in place | NO_CHANGE |
| SCREENS | No new screens added; existing SCR-001/041/042/060/011 behavior corrected (real data) | NO_CHANGE |
| FRONTEND | No design token or pattern changes | NO_CHANGE |
| FUTURE | No future items created or invalidated | NO_CHANGE |
| .env.example | Added `AUTH_JWT_REMEMBER_ME_TTL=168h` to Authentication section | UPDATED |
| Makefile | No new services, scripts, or targets added | NO_CHANGE |
| CLAUDE.md | No port/URL changes | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 1 (fixed)
- API _index.md listed API-035/043/051/052 with story references pointing to STORY-011, STORY-017, and STORY-034 with notes "(requires SIM store)" — these endpoints were never implemented in those stories and were tracked as gaps (DEV-136.20). Now implemented in STORY-057; references corrected.

## Decision Tracing

- Decisions checked: DEV-136.20 (API-035/043/051/052 gap), AC-10 (remember_me decision requirement)
- DEV-136.20: SUPERSEDED entry correctly marks it as closed by STORY-057 AC-6/AC-7/AC-8/AC-9. Implementation verified in gate (all 4 routes registered, handlers present). PASS.
- AC-10 requires: "Decision documented in decisions.md." The gate confirmed `AUTH_JWT_REMEMBER_ME_TTL` is implemented in config.go (default 168h) and wired in auth.go — but no DEV-* entry existed in decisions.md at review time. FIXED: DEV-140 added.
- Orphaned decisions: 0 (after fix)

## USERTEST Completeness

- Entry exists: YES — `docs/USERTEST.md` contains `## STORY-057: Data Accuracy & Missing Endpoints`
- Type: UI scenarios (10 test scenarios covering all 5 screens: Dashboard, SIM Sessions tab, SIM Usage tab, APN Connected SIMs, SIM Edit, Login remember_me)
- Coverage: Matches all 10 ACs. PASS.

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting this story: 0
- D-001 (raw `<input>` in ip-pool-detail.tsx) targets STORY-077 — OPEN, unaffected
- D-002 (raw `<button>` in ip-pool-detail.tsx and apns/index.tsx) targets STORY-077 — OPEN, unaffected
- Already resolved by Gate: N/A
- NOT addressed: 0

## Mock Status

- No `web/src/mocks/` directory exists — mock sweep not applicable
- `Math.random()` in `use-dashboard.ts` (sparklines) removed per AC-4. Remaining `Math.random()` calls in `apns/`, `operators/`, `sla/`, `capacity/` pages are explicitly out of STORY-057 scope and tracked in STORY-070.

## Issues

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | API _index.md: API-035, API-043, API-051, API-052 referenced stale story links with "(requires SIM store)" annotation | NON-BLOCKING | FIXED | Updated all 4 entries to point to STORY-057 in `docs/architecture/api/_index.md` |
| 2 | CONFIG.md missing `AUTH_JWT_REMEMBER_ME_TTL` env var introduced by AC-10 | NON-BLOCKING | FIXED | Added to Authentication & Security table with default `168h` and description |
| 3 | decisions.md had no entry for remember_me 7d TTL despite AC-10 explicitly requiring it to be documented | NON-BLOCKING | FIXED | Added DEV-140 capturing the design rationale (separate from JWT_REFRESH_EXPIRY, trusted-machine use case, partial tokens unaffected) |
| 4 | `.env.example` missing `AUTH_JWT_REMEMBER_ME_TTL` (new env var from AC-10) | NON-BLOCKING | FIXED | Added `AUTH_JWT_REMEMBER_ME_TTL=168h` to Authentication section in `.env.example` |
| 5 | ROUTEMAP top-line summary counter still showed `Phase 10: 0/22 stories` after STORY-056 and STORY-057 completion | NON-BLOCKING | FIXED | Updated top-line to `Phase 10: 2/22 stories` |

## Project Health

- Stories completed: 57/77 (74%) — Dev Phase 55/55 (100%) + Phase 10 2/22 (9%)
- Current phase: Phase 10 — Cleanup & Production Hardening
- Next story: STORY-058 — Frontend Consolidation & UX Completeness
- Blockers: None

# Reviewer Report: STORY-079
# [AUDIT-GAP] Phase 10 Post-Gate Follow-up Sweep (F-1..F-8 + DEV-191)

**Reviewer:** Ana Amil Reviewer Agent
**Date:** 2026-04-17
**Verdict:** PASS — no unresolved findings

---

## Check #1 — Story Acceptance Criteria Completeness (REPORT-ONLY)

All 9 ACs implemented and verified in code:

| AC | Description | Status | Evidence |
|----|-------------|--------|---------|
| AC-1 | `argus migrate`/`seed`/`version` CLI subcommands wired | DONE | `cmd/argus/main.go` lines 110, 123, 158, 161: `parseSubcommand`, `runMigrate`, `runSeed` all present |
| AC-2 | CONCURRENTLY demotion in 8 migrations | DONE | 8 migration files modified (story confirms, Gate verified 8 matching diff) |
| AC-3 | Seed fresh-volume fix — 6 enum CHECK-constraint mismatches patched | DONE | `migrations/seed/003_comprehensive_seed.sql` header documents root cause + fix |
| AC-4 | `/sims/compare` auto-populate from `?sim_id_a=`/`?sim_id_b=` + Compare button | DONE | `web/src/pages/sims/compare.tsx` lines 470-473: `useSearchParams` reads both params; `web/src/pages/sims/index.tsx` navigates with pair |
| AC-5 | `/dashboard` alias route registered | DONE | `web/src/router.tsx` line 134: `{ path: '/dashboard', element: lazySuspense(DashboardPage) }` |
| AC-6 | Transient "Invalid session ID format" toast silenced | DONE | `web/src/lib/api.ts` lines 84-90: response interceptor; lines 182-185: UUID_RE guard at call site; `internal/api/auth/handler.go` line 507: server-side `session_id` breadcrumb logging |
| AC-7 | `/api/v1/status/details` `recent_error_5m` field live (not hardcoded 0) | DONE | `internal/observability/metrics/metrics.go` line 16: `recent5xxWindowSeconds = 300`; `internal/api/system/status_handler.go` lines 50-59 wired |
| AC-8 | Turkish i18n posture decision recorded | DONE | DEV-234 in `docs/brainstorming/decisions.md`: DEFER to dedicated localization story post-GA |
| AC-9 | `/policies` Compare posture decision recorded | DONE | DEV-235 in `docs/brainstorming/decisions.md`: NO — close the Phase 10 gate note |

**Result:** 9/9 ACs DONE.

---

## Check #2 — Architecture Doc Sync

**Finding R-001 (MEDIUM):** `docs/ARCHITECTURE.md` routing table (lines 252-278) is missing the `/dashboard` alias row added by AC-5.

**Resolution:** FIX — added `/dashboard` row to routing table. See staged edit.

---

## Check #3 — API Index Sync

`docs/architecture/api/_index.md` — reviewed. No new endpoints added by STORY-079. AC-7 reuses existing `GET /api/v1/status/details` (API-182). No gap.

**Result:** PASS — no new API index entries required.

---

## Check #4 — DB Index Sync

`docs/architecture/db/_index.md` — reviewed. No new tables added by STORY-079 (existing `errorRingBuffer` is in-process, not persisted). No gap.

**Result:** PASS — no new TBL entries required.

---

## Check #5 — WebSocket Events Sync

`docs/architecture/WEBSOCKET_EVENTS.md` — reviewed. No new WS events introduced by STORY-079. Existing session-related events unaffected.

**Result:** PASS — no WS doc updates required.

---

## Check #6 — Config / ENV Sync

`docs/architecture/CONFIG.md` — reviewed. No new env vars added by STORY-079. The `recent5xxWindowSeconds = 300` is a hardcoded constant (not configurable by design per plan acceptance).

**Result:** PASS — no CONFIG.md updates required.

---

## Check #7 — Error Codes Sync

`docs/architecture/ERROR_CODES.md` — reviewed. AC-6 relies on existing `INVALID_FORMAT` code. No new error codes introduced.

**Result:** PASS — no ERROR_CODES.md updates required.

---

## Check #8 — Middleware Sync

`docs/architecture/MIDDLEWARE.md` — reviewed. No new middleware added by STORY-079.

**Result:** PASS — no MIDDLEWARE.md updates required.

---

## Check #9 — Test Coverage

Gate report confirms:
- 2870/2870 Go tests pass (up from 2868 baseline — +2 new AC-7 boundary tests)
- `internal/observability/metrics`: 71/71 pass
- `internal/api/system`: pass
- `cmd/argus`: pass (includes new positive `serve` subtest + renamed `migrate` subtest)
- `npx tsc --noEmit`: PASS
- Vite production build: PASS

**Result:** PASS — adequate coverage for all 9 ACs.

---

## Check #10 — Gate Findings Resolution (REPORT-ONLY)

Gate findings and their disposition:

| Finding | Severity | Gate Fix | Status |
|---------|----------|----------|--------|
| F-A1: errorRingBuffer 60s → 300s window | HIGH | Fixed in gate (metrics.go + test) | RESOLVED |
| F-A2: Mislabeled subtest rename + genuine serve test | LOW | Fixed in gate (main_cli_test.go) | RESOLVED |
| F-A3: Decisions numbering drift DEV-231..235 | LOW | Accepted cosmetic — decisions immutable | D-028 ACCEPTED |
| F-A4: No CI guard for seed/CHECK drift | LOW | Deferred POST-GA | D-029 PENDING |
| F-A5: `err == migrate.ErrNoChange` → `errors.Is` | LOW | Fixed in gate (main.go) | RESOLVED |
| F-A6: `argus seed` path traversal | LOW | Deferred POST-GA security | D-030 PENDING |
| F-A7: errorRingBuffer mutex hot path | LOW | Deferred POST-GA perf (plan-accepted) | D-031 PENDING |
| F-A8: ROUTEMAP D-013..D-021 flip to RESOLVED | N/A | Left for Reviewer (this step) | RESOLVED below |
| F-U1: /dashboard sidebar active state gap | LOW | Deferred POST-GA UX | D-027 PENDING |
| F-U2: UUID_RE guard at revokeSession call site | LOW | Fixed in gate (api.ts) | RESOLVED |
| F-U3: informational only | INFO | No action | N/A |

**Result:** All HIGH findings resolved. Deferred items tracked in ROUTEMAP.

---

## Check #11 — Decisions Log Sync

`docs/brainstorming/decisions.md` — DEV-231..235 confirmed present:
- DEV-231/232/233: Planner-phase implementation decisions
- DEV-234: Turkish i18n — DEFER to post-GA (AC-8)
- DEV-235: /policies Compare — NO, close gate note (AC-9)

Gate D-028 (numbering cosmetic drift) documented and accepted — no action needed.

**Result:** PASS — decisions log up to date.

---

## Check #12 — USERTEST.md Section

**Finding R-002 (MEDIUM):** `docs/USERTEST.md` has no STORY-079 section. Last section is STORY-078 (line 1870). Story has UI ACs (4/5/6), backend ACs (1/2/3/7), and decision-only ACs (8/9) — all warrant test notes.

**Resolution:** FIX — added STORY-079 section to USERTEST.md. See staged edit.

---

## Check #13 — ROUTEMAP Tech Debt Flip

**Finding R-003 (MEDIUM):** `docs/ROUTEMAP.md` D-013..D-021 (lines 360-368) all show `[ ] PENDING` targeting STORY-079. All 9 items are resolved by STORY-079 implementation. Gate report explicitly notes F-A8 = "ROUTEMAP D-013..D-021 flip to RESOLVED — Ana Amil close-out task."

**Resolution:** FIX — flipped D-013..D-021 to `✓ RESOLVED (2026-04-17)`. See staged edit.

---

## Check #14 — ROUTEMAP Story Status

STORY-079 row at ROUTEMAP line 203: `[~] IN PROGRESS | Review`. Must be flipped to `[x] DONE` and completed date set.

**Finding R-004 (LOW):** ROUTEMAP story row still shows IN PROGRESS.

**Resolution:** FIX — flipped STORY-079 to `[x] DONE | — | 2026-04-17`. See staged edit.

---

## Findings Summary

| # | Check | Finding | Severity | Resolution |
|---|-------|---------|----------|-----------|
| R-001 | #2 Architecture Doc | ARCHITECTURE.md routing table missing `/dashboard` alias row | MEDIUM | FIXED (staged) |
| R-002 | #12 USERTEST | USERTEST.md missing STORY-079 section | MEDIUM | FIXED (staged) |
| R-003 | #13 ROUTEMAP Debt | D-013..D-021 still PENDING — all resolved by implementation | MEDIUM | FIXED (staged) |
| R-004 | #14 ROUTEMAP Status | STORY-079 row still IN PROGRESS | LOW | FIXED (staged) |

**Total findings:** 4 — 4 FIXED, 0 DEFERRED, 0 ESCALATED, 0 UNRESOLVED.

---

## Staged Edits

1. `docs/ARCHITECTURE.md` — added `/dashboard` row to routing table
2. `docs/USERTEST.md` — added STORY-079 section (AC-1..9 test scenarios)
3. `docs/ROUTEMAP.md` — D-013..D-021 flipped to RESOLVED; STORY-079 row flipped to DONE

---

## Final Verdict

**PASS** — All 9 ACs implemented and verified. 4 doc-drift findings found and fixed. Zero unresolved findings. Story is ready to close.

# Post-Story Review: STORY-058 — Frontend Consolidation & UX Completeness

> Date: 2026-04-12

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| STORY-059 | AC-6 expects `STATE_COLORS` to include `stolen_lost` in `web/src/lib/sim-colors.ts`. Gate confirms this already exists in `dashboard/index.tsx:34`. STORY-059 AC-6 should target `sim-colors.ts` for the canonical source, but cross-checking shows the gate passed AC-11 via the dashboard file. No blocker — STORY-059 AC-6 should verify both locations during implementation. | NO_CHANGE |
| STORY-070 | ErrorBoundary foundations (router-level + per-tab) are now in place. Code splitting (all routes lazy-loaded) reduces initial load. STORY-070's real-data wiring will slot into the existing ErrorBoundary + Suspense skeleton pattern without changes. | NO_CHANGE |
| STORY-074 | AC-8 fixes the WS `status-bar.tsx` cast chain defaulting to `true`. The `wsClient` typed interface improvement will not conflict with `use-sessions.ts` WS filter work from STORY-058. No interaction. | NO_CHANGE |
| STORY-077 | D-001/D-002 tech debt (raw `<input>`/`<button>` in `ip-pool-detail.tsx`) remain OPEN targeting STORY-077 — unaffected by STORY-058. ErrorBoundary + aria-label foundations from STORY-058 reduce STORY-077 AC scope (no need to add error boundaries; just extend aria coverage). | NO_CHANGE |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| decisions.md | Added DEV-141: `chunkSizeWarningLimit: 500` divergence from AC-5 nominal target | UPDATED |
| GLOSSARY | No new domain terms introduced | NO_CHANGE |
| ARCHITECTURE | No structural changes to backend or infra | NO_CHANGE |
| SCREENS | No new screens added; no screen deletions | NO_CHANGE |
| FRONTEND | No design token changes | NO_CHANGE |
| FUTURE | No new extension points revealed | NO_CHANGE |
| Makefile | No new targets or scripts | NO_CHANGE |
| CLAUDE.md | No Docker URL/port changes | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 1 (story-internal, non-blocking)
- The story file (`STORY-058-frontend-consolidation.md`) and plan file reference incorrect SCR IDs that were superseded by the current SCREENS.md numbering scheme:
  - Story references SCR-045 (SIM List) → SCREENS.md: SCR-020
  - Story references SCR-075 (SIM Detail) → SCREENS.md: SCR-021
  - Story references SCR-070 (Live Sessions) → SCREENS.md: SCR-050
  - Story references SCR-071 (Jobs) → SCREENS.md: SCR-080
  - Story references SCR-072 (eSIM) → SCREENS.md: SCR-070
  - Story references SCR-080 (Audit) → SCREENS.md: SCR-090
  - Story references SCR-060 (APN List) → SCREENS.md: SCR-030
  - Story references SCR-100 (Policy Editor) → SCREENS.md: SCR-062
  - (SCR-113 Notification Settings matches correctly)
- Per protocol, story files are NOT edited by the Reviewer. This is a historic artifact — the story was written with draft SCR numbers. Implementation is correct; only the story-level doc references are stale.

## Decision Tracing

- Decisions checked: DEV-136.17, DEV-136.18, DEV-136.19 (all STORY-058 targeted)
- DEV-136.17 (ErrorBoundary missing 7 stories): SUPERSEDED → AC-4 implemented. Gate PASS. ✓
- DEV-136.18 (Skeleton/RAT_DISPLAY/InfoRow duplication): SUPERSEDED → AC-1/AC-2/AC-3 implemented. Gate PASS. ✓
- DEV-136.19 (425KB bundle): SUPERSEDED → AC-5 implemented. Gate PASS. ✓
- Implicit decision captured: `chunkSizeWarningLimit` set to 500 (not 250 per story) — now recorded as DEV-141.
- Orphaned (approved but not applied): 0

## USERTEST Completeness

- Entry exists: YES (`docs/USERTEST.md` line 1093)
- Type: UI scenarios — 12 scenarios across 7 screens (SIM List, SIM Detail, Live Sessions, eSIM, Audit, Jobs, Build/infra checks)
- Coverage: All 11 ACs represented through 12 scenarios

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting this story: 0
  - D-001 targets STORY-077 (raw `<input>`)
  - D-002 targets STORY-077 (raw `<button>`)
- Already ✓ RESOLVED by Gate: N/A
- Resolved by Reviewer (Gate missed marking): 0
- NOT addressed (CRITICAL): 0

## Mock Status (Frontend-First)

- `web/src/mocks/` directory: does not exist (not a frontend-first project with dedicated mock layer)
- WS cache was previously ignoring filters (pre-STORY-058); now fixed. No mock files to retire.

## Issues

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | Stale SCR IDs in story + plan files (SCR-045/075/070/071/072/080/060/100 do not match SCREENS.md) | NON-BLOCKING | DEFERRED D-003 to STORY-062 (perf+doc cleanup sweep) | Story files are not edited by Reviewer per protocol. STORY-062 is explicitly the "Performance & Doc Drift Cleanup (final sweep)" — correct home for this SCR reference fix. |
| 2 | `chunkSizeWarningLimit: 500` diverges from AC-5 nominal target of 250KB without a recorded decision | NON-BLOCKING | FIXED — DEV-141 added to decisions.md | Gate explanation: vendor chunks (codemirror 346KB, charts 411KB) are lazily loaded and cannot be further split; actual initial gzipped chunk is 78KB, well within 250KB target. Limit of 500 prevents false-positive build warnings from vendor chunks. |

## Project Health

- Stories completed: 3/22 in Phase 10 (STORY-056, STORY-057, STORY-058)
- Overall: 58/77 total stories completed (75%)
- Current phase: Phase 10 — Cleanup & Production Hardening
- Next story: Wave 2 starts — STORY-059, STORY-060, STORY-063, STORY-064 (parallel)
- Blockers: None

# Post-Story Review: FIX-245 — Remove 5 Admin Sub-pages (Cost/Compliance/DSAR/Maintenance) + Kill Switches → ENV

> Date: 2026-04-27

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| FIX-247 | Remove Admin Global Sessions UI — same deletion-only pattern as FIX-245. F-A1 (PAT-026) sets precedent: sweep handler + store + table + seed + BG jobs atomically. Gate Analysis scout checklist now includes explicit "background job" layer check. | REPORT ONLY |
| FIX-238 | Remove Roaming Feature — same deletion-only pattern. Apply PAT-026 lesson: rg sweep across all 6 layers (handler/store/DB/seed/job/main.go). | REPORT ONLY |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| `docs/GLOSSARY.md` | Kill Switch entry rewritten (env-backed, 30s TTL, no TBL-45, no API-248/249); Maintenance Window / Compliance Posture Dashboard / Cost-per-Tenant View entries deleted (features removed) | UPDATED |
| `docs/USERTEST.md` | `## FIX-245:` section added — 5 manual test scenarios: removed-routes 404, sidebar reduction visual, kill-switch env toggle+restart, DB tables dropped, default-permit behavior | UPDATED |
| `docs/brainstorming/bug-patterns.md` | PAT-026 NEW — orphan background job silently emits events for a deleted feature (F-A1 generalized) | UPDATED |
| `docs/brainstorming/decisions.md` | DEV-575 (kill-switch env migration + 30s TTL + test seams), DEV-576 (scope audit — 3 packages retained: compliance.go, cost_analytics.go, internal/compliance/), DEV-577 (DSAR F-A1: DataPortabilityProcessor deleted), DEV-578 (Announcements preserved per AC-16) | UPDATED |
| `docs/ROUTEMAP.md` | FIX-245 row: `[~] IN PROGRESS · Review` → `[x] DONE 2026-04-27 · DEV-575..578`; Change Log entry added | UPDATED |
| `docs/reviews/ui-review-2026-04-19.md` | F-313 heading + status line: `✅ RESOLVED FIX-245 DONE 2026-04-27` | UPDATED |
| `.env.example` | `# ── Kill Switches (FIX-245)` commented block added (5 vars) | UPDATED |
| `docs/ARCHITECTURE.md` | No stale admin sub-page routes found — route table already clean (admin/cost, admin/compliance, admin/dsar, admin/maintenance, admin/kill-switches absent) | NO_CHANGE |
| `docs/SCREENS.md` | Count 83 confirmed correct (5 SCRs removed during Dev T13); no further changes needed | NO_CHANGE |
| `docs/architecture/CONFIG.md` | Kill Switches section added during Dev T13 (AC-22); F-A3 filename fix applied at Gate — NO further changes | NO_CHANGE |
| `docs/operational/EMERGENCY_PROCEDURES.md` | Created during Dev (AC-22); F-A3 filename fix applied at Gate — NO further changes | NO_CHANGE |
| `CLAUDE.md` | Session pointer update deferred to Orchestrator (Step 5 Commit) | NO_CHANGE |
| `Makefile` | No new services or targets; no changes | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 0 (after GLOSSARY fixes)
- GLOSSARY had 4 stale entries (Kill Switch DB-backed description + 3 deleted-feature entries) — all fixed
- ARCHITECTURE.md admin route table was already clean — the 5 removed sub-page routes were not listed
- SCREENS.md count (83) matches step-log T13 output

## Decision Tracing

- Decisions checked: DEV-254 (original scope) + DEV-575..578 (implementation decisions)
- Orphaned (approved but not applied): 0
- All implementation decisions captured in decisions.md

## USERTEST Completeness

- Entry exists: YES (added by this review)
- Type: 5 UI + env manual scenarios
- UT-245-01: 6 removed routes → 404
- UT-245-02: Sidebar reduction visual verification
- UT-245-03: Kill switch env toggle + restart workflow
- UT-245-04: DB tables dropped + migration versions
- UT-245-05: Default-permit behavior (all switches OFF = system runs normally)

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting FIX-245: 0 (no prior D-NNN entries pointed at this story)
- Gate deferred: 0 (gate explicitly noted "No tech debt routed")
- NOT addressed: 0

## Mock Status

- N/A (deletion-only story; no mocks introduced or retired)

## F-313 Closure

- **Status: CLOSED** — F-313 heading in `docs/reviews/ui-review-2026-04-19.md` updated with `✅ RESOLVED FIX-245 DONE 2026-04-27` marker matching the FIX-240/237 pattern.

## Issues

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | GLOSSARY had stale Kill Switch entry (DB-backed, TBL-45 ref, 15s TTL, API-248/249) + 3 orphan entries (Maintenance Window, Compliance Posture Dashboard, Cost-per-Tenant View) | NON-BLOCKING | FIXED | Kill Switch rewritten to env-backed impl; 3 obsolete entries deleted |
| 2 | USERTEST.md missing `## FIX-245:` section | NON-BLOCKING | FIXED | 5 scenarios added covering env toggle, sidebar, 404 routes, DB tables, default-permit |
| 3 | bug-patterns.md missing PAT-026 for F-A1 orphan-publisher pattern | NON-BLOCKING | FIXED | PAT-026 added with full root-cause + prevention + cross-references to PAT-006 family |
| 4 | decisions.md missing FIX-245 implementation decisions | NON-BLOCKING | FIXED | DEV-575..578 added |
| 5 | .env.example missing KILLSWITCH_* vars (present only in CONFIG.md example block) | NON-BLOCKING | FIXED | 5 commented lines + section header added to `.env.example` |
| 6 | ROUTEMAP FIX-245 row still in `[~] IN PROGRESS · Review` | NON-BLOCKING | FIXED | Row updated to `[x] DONE 2026-04-27 · DEV-575..578` |
| 7 | F-313 had no `✅ RESOLVED` marker in ui-review-2026-04-19.md | NON-BLOCKING | FIXED | Status line appended |

## New Bug Pattern Added

**PAT-026 [FIX-245 Gate F-A1]** — Orphan background job silently emits events for a deleted feature. Deletion requires sweeping ALL 6 layers: handler, store, DB table, seed templates, background job/processor, main.go wiring. Fakes cannot catch this class of bug. Prevention: project-wide `rg` sweep for deleted feature name across all layers at Gate; Gate Analysis scout checklist must explicitly include "background jobs". Cross-ref: PAT-006 family.

## New Decisions Added

- **DEV-575** — Kill Switches migrated from DB-backed to env-variable reader with 30s TTL + injectable seams
- **DEV-576** — Scope audit: 3 packages retained (compliance.go, cost_analytics.go, internal/compliance/)
- **DEV-577** — DSAR: DataPortabilityProcessor deleted (F-A1 sweep)
- **DEV-578** — Announcements preserved per AC-16 (pre-existing no-sidebar UX gap = INFO)

## Project Health

- Stories completed: Wave 10 P2 — FIX-245 DONE; FIX-238 / FIX-247 PENDING
- Current phase: UI Review Remediation Wave 10 P2
- Next story: FIX-247 (Remove Admin Global Sessions UI — S effort) or FIX-238 (Remove Roaming Feature — L effort, needs FIX-212)
- Blockers: None

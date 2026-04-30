# Post-Story Review: FIX-238 — Remove Roaming Feature (full stack, L)

> Date: 2026-04-30

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| FIX-247 | Remove Admin Global Sessions UI (backend retain) — same "removal" pattern; PAT-026 6-layer sweep applies | NO_CHANGE (already using PAT-026 from FIX-238/FIX-245) |
| Phase 11+ | No roaming-specific dependencies identified in upcoming stories | NO_CHANGE |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| docs/ROUTEMAP.md | FIX-238 row: `[ ] PENDING` → `[x] DONE 2026-04-30 · DEV-579 · F-229 closed` | UPDATED |
| docs/reviews/ui-review-2026-04-19.md | F-229 block: appended `Status: RESOLVED — FIX-238 DONE 2026-04-30` closure stamp | UPDATED |
| docs/GLOSSARY.md | 5 stale roaming entries (lines 117-121: Roaming Agreement, Agreement State, SLA Terms (Roaming), Cost Terms (Roaming), SoR Agreement Hook) marked REMOVED FIX-238; SessionContext entry: removed `roaming` from field list; Alert Source entry: removed `roaming renewal` publisher reference | UPDATED |
| docs/USERTEST.md | Added `## FIX-238: Remove Roaming Feature — Removal Verification` section (4 scenarios: 404 check, sidebar check, ?tab=agreements redirect, AC-10 archiver behavior) | UPDATED |
| CLAUDE.md | Active Session: FIX-238 marked DONE, story/step cleared, wave 10 P2 updated to 5/6 | UPDATED |
| decisions.md | DEV-579 entry confirmed present | NO_CHANGE |
| docs/brainstorming/bug-patterns.md | PAT-026 RECURRENCE [FIX-238] confirmed present | NO_CHANGE |
| docs/ARCHITECTURE.md | FIX-238 cleanup already applied upstream (W5) — no further changes needed | NO_CHANGE |
| docs/PRODUCT.md | FIX-238 cleanup already applied upstream (W5, line 479) | NO_CHANGE |
| docs/architecture/api/_index.md | API-230..235 REMOVED block applied by Gate | NO_CHANGE |
| docs/architecture/db/_index.md | TBL-43 REMOVED annotation applied by Gate | NO_CHANGE |
| docs/architecture/DSL_GRAMMAR.md | roaming MATCH/WHEN fields + examples removed by Gate | NO_CHANGE |
| docs/architecture/CONFIG.md | ROAMING_RENEWAL_* section + sample env removed by Gate | NO_CHANGE |
| docs/architecture/WEBSOCKET_EVENTS.md | roaming.agreement.renewal_due removed by Gate | NO_CHANGE |
| docs/architecture/EVENTS.md | roaming.agreement.renewal_due Tier 3 entry removed by Gate | NO_CHANGE |
| docs/architecture/ERROR_CODES.md | roaming publisher + DSL example cleaned by Gate | NO_CHANGE |
| docs/SCREENS.md | SCR-150/151 marked REMOVED; count 83→81 by Gate | NO_CHANGE |
| Makefile | No roaming-related targets existed — no change | NO_CHANGE |
| .env.example | ROAMING_RENEWAL_* env vars removed by upstream W1 | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 0 (post-fix)
- Prior to this review: GLOSSARY.md had 7 stale entries describing removed feature internals — all corrected.

## Decision Tracing

- DEV-579 (`FIX-238 AC-10 — Roaming Agreements archive strategy: boot-time ARCHIVE`) — confirmed present in decisions.md (line 735). Applied in `internal/job/roaming_keyword_archiver.go` + main.go boot wiring. PASS.
- PAT-026 RECURRENCE (`[FIX-238]: Feature removal requires a 6-layer sweep`) — confirmed present in bug-patterns.md (line 36). Applied: L1 handler, L2 store, L3 DB, L4 seed, L5 job, L6 main + L7/L8 event catalog / publisher source map. PASS.

## USERTEST Completeness

- STORY-071 entry: REMOVED note at line 1640 (correct — no user-test scenarios for the old story).
- FIX-238 entry: MISSING before this review.
- Action: Added `## FIX-238` section with 4 manual verification scenarios (404, sidebar, redirect, archiver).
- Type: UI removal verification scenarios — ADDED.

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting FIX-238: 0 (no D-NNN rows in the Tech Debt table target FIX-238).
- No deferred items were added by FIX-238 (Gate confirms 0 D-NNN entries).

## Mock Status

Not applicable — no `src/mocks/` directory in project.

## Issues

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | GLOSSARY.md: 5 entries (Roaming Agreement, Agreement State, SLA Terms (Roaming), Cost Terms (Roaming), SoR Agreement Hook) still described the removed feature as if it existed | NON-BLOCKING | FIXED | Entries marked `~~REMOVED FIX-238 (2026-04-30)~~` with redirect to retained SoR cost-based selection; Gate covered architecture/ sub-docs but missed top-level GLOSSARY.md |
| 2 | GLOSSARY.md SessionContext entry listed `roaming` as a struct field but the actual struct (evaluator.go:9-22) has no Roaming field post-FIX-238 | NON-BLOCKING | FIXED | Removed `roaming` from field list; added `(roaming field removed by FIX-238.)` note |
| 3 | GLOSSARY.md Alert Source entry still listed `roaming renewal` as an operator-source publisher example | NON-BLOCKING | FIXED | Removed `/ roaming renewal` from the operator source description; added parenthetical note |
| 4 | ROUTEMAP FIX-238 row still showed `[ ] PENDING` | NON-BLOCKING | FIXED | Updated to `[x] DONE 2026-04-30 · DEV-579 · F-229 closed` |
| 5 | F-229 finding in ui-review-2026-04-19.md had no closure stamp | NON-BLOCKING | FIXED | Appended `Status: RESOLVED — FIX-238 DONE 2026-04-30` closure annotation |
| 6 | USERTEST had no FIX-238 verification scenarios — only STORY-071 REMOVED note | NON-BLOCKING | FIXED | Added 4-scenario `## FIX-238` verification section covering 404, sidebar, redirect, AC-10 archiver |

## Project Health

- Stories completed: Wave 10 P2 — 5/6 (FIX-240, FIX-246, FIX-235, FIX-245, FIX-238 DONE)
- Current phase: UI Review Remediation Wave 10 P2 [IN PROGRESS]
- Next story: FIX-247 — Remove Admin Global Sessions UI (backend retain, P2, S)
- Blockers: None

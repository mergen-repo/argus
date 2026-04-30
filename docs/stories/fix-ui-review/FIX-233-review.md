# Post-Story Review: FIX-233 — SIM List Policy column + Rollout Cohort filter

> Date: 2026-04-26
> Gate report: `docs/stories/fix-ui-review/FIX-233-gate.md` (PASS, Loop 1, 2 fixes applied by Lead)
> Reviewer: Amil Reviewer (AUTOPILOT mode)

---

## 14-Check Results

| Check | Status | Action Taken | Files Edited |
|-------|--------|--------------|--------------|
| 1. Story spec accuracy (REPORT ONLY) | PASS | AC-1..AC-6,AC-8,AC-9,AC-10 PASS; AC-7 PARTIAL (Policy chip version submenu deferred to D-141 — spec ACs accurately reflect reality). No spec edits. | — |
| 2. Plan ↔ implementation drift | PASS | AC-9: plan said "new button"; dev replaced existing "View Migrated SIMs" link with enriched Link adding `rollout_stage_pct` — non-regressive, original target preserved. No other drift. | — |
| 3. API index | FIXED | API-326 added for `GET /api/v1/policy-rollouts` (list active rollouts). API-040 description updated with new filter params + DTO fields. API-041 updated for enriched DTO parity. Policies section bumped to 12 endpoints. Total updated 256→257. | `docs/architecture/api/_index.md` |
| 4. DB index + policy.md | FIXED | TBL-15 in `db/_index.md` annotated with FIX-233 `stage_pct` column + migration ref. `db/policy.md` TBL-15 table updated: `stage_pct INT` column row added; composite index `idx_policy_assignments_rollout_stage` documented. | `docs/architecture/db/_index.md`, `docs/architecture/db/policy.md` |
| 5. Error codes | FIXED | `INVALID_PARAM` row added to Validation Errors table in ERROR_CODES.md. `CodeInvalidParam` constant added to Go const block (FIX-233 AC-4 / T1-fix context). | `docs/architecture/ERROR_CODES.md` |
| 6. SCREENS.md | FIXED | SCR-020 (SIM List) Notes column updated with Policy column, Policy chip, Cohort chip, URL deep-linking additions. SCR-062 (Policy Editor) Notes updated with "View cohort" link on RolloutActivePanel. | `docs/SCREENS.md` |
| 7. FRONTEND.md | NO_CHANGE | New chips reuse existing dropdown/filter-chip patterns from FIX-216. PAT-018 grep confirms 0 raw color tokens introduced. No new design tokens. | — |
| 8. GLOSSARY.md | FIXED | 3 new terms added: "Rollout Cohort", "Cohort Filter", "Active Rollouts Endpoint". FIX-231/FIX-232 entries already cover staged-rollout core concepts; new terms fill the UI-specific filter gap. | `docs/GLOSSARY.md` |
| 9. decisions.md | FIXED | DEV-365..DEV-373 appended (9 decisions covering: migration stage_pct, wire shape current_stage, OQ-1..OQ-5 resolved, F-A1 fix, F-B1 fix). | `docs/brainstorming/decisions.md` |
| 10. bug-patterns.md (REPORT ONLY) | NO_CHANGE | F-B1 (test seed FK closure) is a PAT-014 recurrence — PAT-014 already covers "Seed-time invariant violations surface only under FK enforcement". PAT-019 is occupied (FIX-228 Gate C-01). No new pattern; PAT-014 reinforcement noted in report. | — |
| 11. USERTEST.md | FIXED | "## FIX-233: SIM List Policy column + Rollout Cohort filter" section appended. 10 AC scenario groups in Turkish; AC-7 marked PARTIAL; UI smoke annotated BLOCKED by FIX-249 with curl-equivalent fallback paths. | `docs/USERTEST.md` |
| 12. ROUTEMAP | FIXED | FIX-233 row flipped to `[x] DONE (2026-04-26)`. Activity log row appended with doc-update list and story impact summary. | `docs/ROUTEMAP.md` |
| 13. CLAUDE.md | FIXED | Active Session: Story → FIX-249, Step → Plan. | `/Users/btopcu/workspace/argus/CLAUDE.md` |
| 14. Story Impact | PASS | 5 upcoming stories analyzed. See Story Impact section below. | — |

---

## Findings

| ID | Title | Action | Status |
|----|-------|--------|--------|
| R-01 | API-040/041 missing new filter/DTO params | Updated both rows in api/_index.md | FIXED |
| R-02 | API-326 (GET /policy-rollouts) not indexed | Added API-326 | FIXED |
| R-03 | TBL-15 stage_pct column not documented | Added column + index in db/policy.md + db/_index.md annotation | FIXED |
| R-04 | CodeInvalidParam missing from ERROR_CODES.md | Added row + constant | FIXED |
| R-05 | SCR-020 SIM list screen notes stale | Updated SCREENS.md SCR-020 + SCR-062 notes | FIXED |
| R-06 | 3 new domain terms not in GLOSSARY | Added Rollout Cohort, Cohort Filter, Active Rollouts Endpoint | FIXED |
| R-07 | 9 plan-era + fix decisions not in decisions.md | DEV-365..DEV-373 appended | FIXED |
| R-08 | USERTEST.md missing FIX-233 section | Section appended (10 scenario groups) | FIXED |
| R-09 | F-B1 pattern coverage (PAT-014 recurrence) | REPORT ONLY — PAT-014 already covers this; no new pattern created | NO_CHANGE |
| R-10 | FIX-233 not marked DONE in ROUTEMAP | Marked DONE (2026-04-26) | FIXED |

---

## Story Impact

| Story | Impact Label | Notes |
|-------|-------------|-------|
| FIX-249 — Global React #185 crash | NO_CHANGE | Orthogonal fix; `useEventStore`/`useSyncExternalStore` re-render loop unrelated to SIM list Policy column. |
| FIX-250 — Vite env in info-tooltip | NO_CHANGE | XS cleanup; `process.env.NODE_ENV` → `import.meta.env.PROD` isolated to info-tooltip.tsx. |
| FIX-234 — CoA enum + idle SIM handling | POTENTIAL | FIX-233 already exposes `coa_status` in SIM DTO (`omitempty`). FIX-234 plan adding new enum values (`no_session`, `skipped`) + UI counters will find the DTO plumbing-ready; no backend DTO re-work needed. Spec confirmation recommended. |
| FIX-242 — Session Detail extended DTO | POTENTIAL | FIX-233's pattern of LEFT JOIN extension to store query + DTO projection blocks is reusable for FIX-242's session detail DTO enrichment. No direct API change required; the pattern is documented in step-log W1B T2/W1C T3. |
| FIX-243 — DSL realtime validate | POTENTIAL | D-141 (Policy chip version submenu) targets FIX-243. If FIX-243 surfaces version metadata via DSL endpoint, the version submenu gap (AC-7) could be folded in. Planner should note D-141 in FIX-243 spec. |

---

## Cross-Doc Consistency

- Contradictions found: 0
- `current_stage` field name consistent across BE handler, FE types, and test fixture (post F-A1 fix).
- `idx_policy_assignments_rollout_stage` index name consistent between migration file and db/policy.md documentation.

---

## Decision Tracing

- Plan decisions A..G logged as DEV-365..DEV-371.
- F-A1 fix (wire shape) logged as DEV-372.
- F-B1 fix (test seed FK) logged as DEV-373.
- All APPROVED decisions reflected in implementation (step-log confirms each wave).
- Orphaned decisions: 0.

---

## USERTEST Completeness

- Entry exists: YES (appended this review)
- Type: UI scenarios (Turkish, 10 groups) + BLOCKED annotation for FIX-249 + curl-equivalent fallback paths

---

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting FIX-233: 0 pre-existing
- D-141, D-142, D-143 added by Gate (not pre-existing — added correctly by Gate Lead)
- Already ✓ RESOLVED by Gate: N/A
- NOT addressed (CRITICAL): 0

---

## Mock Status

- No `src/mocks/` directory for `/policy-rollouts` — new endpoint serves live data from day 1.
- No mocks to retire for this story.

---

## Issues

No issues found that require Developer re-dispatch.

---

## Project Health

- Current phase: UI Review Remediation [IN PROGRESS]
- Wave 7 stories: FIX-230 DONE, FIX-231 DONE, FIX-232 DONE, FIX-233 DONE
- Next story: FIX-249 (P0 — Global React #185 crash)
- Blockers: F-U1 (FIX-249) blocks UI smoke for FIX-233 manual test; network-layer curl fallback provided in USERTEST.md.

---

## Final Verdict: PASS

Story FIX-233 passes Reviewer check. All 10 documentation updates applied. No rework required. ROUTEMAP marked DONE. CLAUDE.md advanced to FIX-249/Plan.

# Post-Story Review: FIX-249 — Global React #185 crash — `useFilteredEventsSelector` re-render loop

> Date: 2026-04-26
> Gate ref: docs/stories/fix-ui-review/FIX-249-gate.md (PASS, 3 findings all DEFER, AC 4/4 PASS)
> Mode: AUTOPILOT — UI Review Remediation

## 14-Check Table

| # | Check | Status | Action Taken | Files Edited |
|---|-------|--------|--------------|-------------|
| 1 | Story spec accuracy (REPORT ONLY) | PASS | Spec accurately describes the fix: wrap `useFilteredEventsSelector` consumer in `useShallow` from `zustand/react/shallow` — Path A executed as specified. No drift. | NONE |
| 2 | Plan ↔ implementation drift | PASS | Path A chosen + executed; spec listed Path A as one of two acceptable options. No drift. | NONE |
| 3 | API index (`docs/architecture/api/_index.md`) | NO_CHANGE | FE-only fix, no API changes. | NONE |
| 4 | DB index | NO_CHANGE | FE-only fix, no DB changes. | NONE |
| 5 | Error codes | NO_CHANGE | FE-only fix, no new error codes. | NONE |
| 6 | SCREENS.md | NO_CHANGE | SCR-010 already carries FIX-213 event-stream-drawer entry. FIX-249 is a selector-stability internal fix with zero UX delta — adding an implementation-detail note would muddy the screen contract. | NONE |
| 7 | FRONTEND.md | NO_CHANGE | No "State Management" or "Zustand" section exists in FRONTEND.md. PAT-020 in bug-patterns.md carries the Zustand v5 + React 19 selector-stability lesson authoritatively. | NONE |
| 8 | GLOSSARY.md | NO_CHANGE | No new domain terms introduced. `useShallow`, `useSyncExternalStore` are library internals, not Argus domain terms. | NONE |
| 9 | decisions.md | UPDATED | Appended DEV-374..DEV-376 (Path A rationale, v5 import path, vitest deferral acceptance). | docs/brainstorming/decisions.md |
| 10 | bug-patterns.md | UPDATED | PAT-020 added (dedup grep: 0 matches for `useShallow`/`zustand v5`/`useSyncExternalStore` — no prior equivalent). | docs/brainstorming/bug-patterns.md |
| 11 | USERTEST.md | UPDATED | `## FIX-249` section added in Turkish — 5 scenarios (console clean check, drawer UX, live event, idle observation, navigation stability). | docs/USERTEST.md |
| 12 | ROUTEMAP | UPDATED | FIX-249 marked `[x] DONE (2026-04-26)`. Activity log row appended. | docs/ROUTEMAP.md |
| 13 | CLAUDE.md | UPDATED | Active Session story → FIX-250, Step → Plan. | CLAUDE.md |
| 14 | Story Impact | REPORTED | 5 stories analyzed (see below). | NONE |

## Findings Table

| ID | Title | Action | Status |
|----|-------|--------|--------|
| F-A1 | PAT-020 candidate — Zustand v5 selector returning derived collection requires `useShallow` wrap with React 19 `useSyncExternalStore` | Added PAT-020 to `docs/brainstorming/bug-patterns.md` | FIXED |
| F-U1 | Stale "An unexpected error occurred" toasts on `/sims` — swallowed-error path in `useSimsQuery`/`useSelectedRows` (pre-existing, unrelated to FIX-249) | Spec FIX-251 created by Gate Lead; deferred to P3/XS queue | DEFERRED-FIX-251 |
| F-U2 | `POST /api/v1/sims/{id}/activate` returns 500 — likely IP-pool allocation failure on reactivate; pre-existing backend bug | Spec FIX-252 created by Gate Lead; deferred to P2/S queue | DEFERRED-FIX-252 |

## Story Impact

| Story | Impact | Action |
|-------|--------|--------|
| FIX-250 | Vite-native env access in info-tooltip — independent of FIX-249; no selector work involved | NO_CHANGE |
| FIX-251 | Stale-toast fix surfaced by FIX-249 UI Scout but unrelated to selector stability — implementation differs entirely | NO_CHANGE |
| FIX-252 | Backend 500 on SIM activate — IP-pool backend bug, no FE state management involvement | NO_CHANGE |
| FIX-234 | CoA enum extension — independent backend + FE enum update; no event-stream or selector involvement | NO_CHANGE |
| FIX-237 | Event taxonomy redesign (Phase 2 P0) — **POTENTIAL**: FIX-237 will touch the event-stream subsystem where FIX-249 lives. Any new derived-collection selectors introduced by FIX-237 MUST follow the `useShallow` discipline established by FIX-249 / PAT-020. Not a blocker; just a reference for FIX-237 plan. | POTENTIAL — reference PAT-020 in FIX-237 plan |

## Decision Tracing

- Decisions DEV-374, DEV-375, DEV-376 appended to decisions.md.
- No approved decisions from prior stories were left orphaned by FIX-249.

## USERTEST Completeness

- Entry: ADDED (`## FIX-249` section, 5 scenarios, Turkish).
- Type: UI scenarios (manual browser + console verification).

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting FIX-249: 0
- No tech debt items from prior stories targeted FIX-249.

## Mock Status

- Not applicable — no mocks introduced or retired.

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| docs/stories/fix-ui-review/FIX-249-review.md | Created (this file) | CREATED |
| docs/brainstorming/bug-patterns.md | PAT-020 added | UPDATED |
| docs/brainstorming/decisions.md | DEV-374..DEV-376 added | UPDATED |
| docs/USERTEST.md | FIX-249 section added (5 scenarios, Turkish) | UPDATED |
| docs/ROUTEMAP.md | FIX-249 marked DONE; activity log row appended | UPDATED |
| CLAUDE.md | Active Session advanced to FIX-250 / Step=Plan | UPDATED |
| docs/stories/fix-ui-review/FIX-249-step-log.txt | STEP_4 REVIEW line appended | UPDATED |
| docs/architecture/api/_index.md | FE-only story — NO_CHANGE | NO_CHANGE |
| docs/architecture/db/_index.md | FE-only story — NO_CHANGE | NO_CHANGE |
| docs/architecture/ERROR_CODES.md | FE-only story — NO_CHANGE | NO_CHANGE |
| docs/SCREENS.md | Internal selector fix, zero UX delta — NO_CHANGE | NO_CHANGE |
| docs/FRONTEND.md | No Zustand/state-management section to add to — NO_CHANGE | NO_CHANGE |
| docs/GLOSSARY.md | No new domain terms — NO_CHANGE | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 0

## Project Health

- UI Review Remediation: FIX-249 DONE; Wave 7 rollout-deep-fix continuation in progress
- Next story: FIX-250 (Vite-native env access in info-tooltip — P2 / XS)
- Blockers: None

---

**REVIEW VERDICT: PASS**

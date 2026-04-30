# Post-Story Review: FIX-217 — Timeframe Selector Pill Toggle Unification

> Date: 2026-04-22

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| FIX-222 (Operator/APN Detail Polish) | Touches `operators/detail.tsx` + `apns/detail.tsx`. The Health tab AC-4 references a "24h/7d/30d toggle" — this must use `<TimeframeSelector>` (not a hand-rolled control). FIX-217 already converted TrafficTab + HealthTimelineTab; FIX-222 must not regress those or introduce new `<Select>` dropdowns for any timeframe. | REPORT ONLY |
| FIX-24x (UI polish / FE test infra) | D-091..D-095 all target FIX-24x. Planner for FIX-24x must account for: keyboard arrow-key fix (D-092), `window` shadow rename (D-093), HealthTimelineTab numeric-string preset fix (D-094), memoize `allOptions`/`selectableIndices` (D-095), and wire up the test runner for `TimeframeSelector` unit tests (D-091). | REPORT ONLY |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| `docs/FRONTEND.md` | Timeframe Pattern section (~80 lines) added in DEV W1 T2 | NO_CHANGE (already done in DEV) |
| `docs/brainstorming/decisions.md` | DEV-291 appended (CDR URL scheme migration `?from/?to` → `?tf/?tf_start/?tf_end`) by Gate Fix 1 | NO_CHANGE (already done in GATE) |
| `docs/ROUTEMAP.md` | D-092..D-095 appended by Gate | NO_CHANGE (already done in GATE) |
| `docs/GLOSSARY.md` | No new domain terms introduced (FE implementation terms, not domain vocabulary) | NO_CHANGE |
| `docs/ARCHITECTURE.md` | Pure FE primitive unification; no architectural changes | NO_CHANGE |
| `docs/SCREENS.md` | No new screens; existing screen behavior changed but screen purpose unchanged | NO_CHANGE |
| `docs/USERTEST.md` | FIX-217 section added (6 scenarios — pill rendering, Custom popover, role-gate, URL deep-link, back-compat, keyboard nav) | RESOLVED |
| `Makefile` | No new services, scripts, or targets | NO_CHANGE |
| `CLAUDE.md` | No Docker URL/port changes | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 1 (story file vs implementation — see Issues #1; REPORT ONLY)
- All other docs consistent with implementation

## Decision Tracing

- Decisions checked: DEV-291 (FIX-217 gate-appended)
- DEV-291 confirmed in `docs/brainstorming/decisions.md` line 545
- Orphaned (approved but not applied): 0

## USERTEST Completeness

- FIX-217 section in `docs/USERTEST.md`: MISSING
- Type required: UI scenarios (this is a pure UI refactor with keyboard nav, role-gating, Custom popover, and URL sync)

## Tech Debt Pickup (from ROUTEMAP)

- Pre-existing items whose Target was FIX-217: 0 (ROUTEMAP row for FIX-217 only tracks story status)
- Items CREATED by this gate targeting FIX-24x: D-092, D-093, D-094, D-095 (all OPEN, correctly targeting future story)
- D-091 (FE test infra) also targets FIX-24x, not FIX-217 — OPEN, correctly deferred
- Gate missed marking as RESOLVED: 0
- NOT addressed (CRITICAL): 0

## Mock Status

- `web/src/mocks/` directory: does not exist — N/A

## Issues

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | Story file AC-1 names `<TimeframePills>` (new component) and AC-3 specifies `?from=...&to=...` URL scheme, but implementation extended existing `<TimeframeSelector>` and uses `?tf=/?tf_start=/?tf_end=` (documented in DEV-291) | NON-BLOCKING | REPORT ONLY (no story file edits per check #1 rule) | Story file predates plan — plan correctly superseded it. No downstream confusion risk: DEV-291 documents the URL decision; FRONTEND.md documents the component API. Flagged for Story Impact agent to update story ACs to match implementation. |
| 2 | `docs/USERTEST.md` has no `## FIX-217:` section | NON-BLOCKING | RESOLVED | Appended 6-scenario section to docs/USERTEST.md: (1) canonical pill rendering on 5 adopted surfaces, (2) Custom popover apply → `?tf=custom&tf_start=...&tf_end=...` + TZ roundtrip verification (F-A3/A4), (3) `disabledPresets` analyst 30d role-gate (click + keyboard safety), (4) URL deep-link sync + filter-preserve (F-A1), (5) back-compat: dashboard/analytics + analytics-cost + sims/detail unchanged, (6) keyboard nav Arrow/Home/End/Enter/Space. |

## Project Health

- Stories completed: FIX-201..FIX-217 (17/44 in UI Review Remediation track)
- Current phase: UI Review Remediation — Wave 5 done
- Next story: FIX-218 (Views Button Global Removal + Checkbox Cleanup — Operators)
- Blockers: None

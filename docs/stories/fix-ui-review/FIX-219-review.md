# Post-Story Review: FIX-219 — Name Resolution + Clickable Cells Everywhere

> Date: 2026-04-22

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| FIX-220 | `analytics-cost.tsx` chart label fallback and `analytics.tsx` top_consumers were rewritten by FIX-219 Gate fix #3/#4. FIX-220 lands on a cleaner surface — no UUID-slice fallback to undo; MSISDN column and IN/OUT split can be added without EntityLink conflicts. | NO_CHANGE |
| FIX-222 | `operators/detail.tsx` and `apns/detail.tsx` both received EntityLink cell rewrites in FIX-219. FIX-222 KPI row + tab consolidation work must preserve existing EntityLink cells — do not re-inline UUID slices during tab restructuring. | NO_CHANGE |
| FIX-224 | `sims/index.tsx` and `sims/compare.tsx` now use EntityLink for APN/operator columns. FIX-224 state filter + bulk bar changes must not regress these columns. | NO_CHANGE |
| FIX-225 | Stories touching audit, jobs, or notifications pages now inherit enriched DTO types (`user_email`, `created_by_name`, `actor_email`). No further backend enrichment work needed for those columns. | NO_CHANGE |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| GLOSSARY.md | Updated "Entity Link" entry (10-value union → 14-value, new props listed); added "Entity Hover Card" term | UPDATED |
| docs/brainstorming/decisions.md | Added DEV-292: purge_history triggered_by VARCHAR→UUID bug fix + behavior impact | UPDATED |
| docs/USERTEST.md | Added `## FIX-219` section with 7 scenario groups (EntityLink per page, HoverCard, orphan em-dash, right-click copy, keyboard nav+a11y, UUID slice grep, backend DTO smoke) | UPDATED |
| FRONTEND.md | No changes — Entity Reference Pattern section (8 subsections) is complete and accurate | NO_CHANGE |
| SCREENS.md | No EntityLink/HoverCard screen-level doc changes needed — component pattern doc lives in FRONTEND.md | NO_CHANGE |
| ARCHITECTURE.md | No arch changes — purge_history column fix is a bug fix, not a new service | NO_CHANGE |
| FUTURE.md | No changes — no new extension points introduced | NO_CHANGE |
| Makefile / .env.example | No new services or env vars | NO_CHANGE |
| CLAUDE.md | Not modified — Active Session pointer is Ana Amil territory | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 1 (FIXED — GLOSSARY "Entity Link" listed 10-value union, code has 14 values since FIX-219)
- FRONTEND.md "Entity Reference Pattern" D-104 cross-reference note (ROUTEMAP entry D-104 matches the FRONTEND.md:213 violation route doc nit documented in gate): consistent.
- D-090 (ESLint deferral) cross-reference in Modal Pattern section still reads correctly — no conflict introduced.
- Step-log claims "subsections=9"; actual section has 8 subsections. Step-log is not normative; section content is complete and matches plan Task 3 requirements.

## Decision Tracing

- Decisions checked: FIX-219 plan decisions D1..D6 (extend-existing-EntityLink, 3-component-boundary, backend-DTO-patches, strict-orphan-rule, hover-card-opt-in, right-click-preventDefault)
- Orphaned (approved but not applied): 0 — all 6 plan decisions are reflected in the shipped code and/or FRONTEND.md
- DEV-292 added for purge_history schema bug (implicit behavioral change not previously documented)

## USERTEST Completeness

- Entry exists: YES (added this review)
- Type: 7 UI + backend scenario groups covering EntityLink appearance, HoverCard delay+offline, orphan em-dash, right-click copy, keyboard nav+a11y, UUID slice grep check, DTO smoke

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting this story: 9 (D-097..D-105)
- Already ✓ RESOLVED by Gate: 0 (all 9 are POST-GA deferred — Gate correctly left them OPEN)
- Resolved by Reviewer (Gate missed marking): 0
- NOT addressed (CRITICAL): 0
- All 9 entries (D-097..D-105) confirmed present in ROUTEMAP Tech Debt table with full descriptions and OPEN status

## Mock Status

- Not applicable — this project does not use a `src/mocks/` directory.

## Issues

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | GLOSSARY "Entity Link" entry stated "10-value TypeScript string-literal union" — FIX-219 extended to 14 values; definition also omitted new props (showIcon, hoverCard, copyOnRightClick) and the orphan em-dash rule | NON-BLOCKING | FIXED | Updated GLOSSARY.md line 300: union count corrected to 14, props listed, orphan rule noted; EntityHoverCard term added as new row |
| 2 | GLOSSARY missing `EntityHoverCard` term — new component shipped in FIX-219 with distinct behavior (200ms delay, lazy fetch, offline guard) warranting its own entry | NON-BLOCKING | FIXED | Added EntityHoverCard row to GLOSSARY.md Cross-Entity Context & Navigation Terms table |
| 3 | USERTEST.md had no FIX-219 section — UI-heavy story (23 pages rewritten) with no manual test scenarios | NON-BLOCKING | FIXED | Added 7 scenario groups covering all AC areas |
| 4 | decisions.md missing DEV-NNN for purge_history triggered_by VARCHAR→UUID bug fix — a silent behavioral change (actor_id was always null pre-fix; now populated for human-initiated purges) | NON-BLOCKING | FIXED | Added DEV-292 documenting the column semantic change, join path, historical null impact, and FE type update |

## Project Health

- Stories completed: FIX-201..FIX-219 (19 of 44 FIX stories in UI Review Remediation track)
- Current phase: UI Review Remediation [IN PROGRESS]
- Next story: FIX-220 (Analytics Polish — MSISDN Column, IN/OUT Split, Tooltips, Delta Cap, Capitalization) — per ROUTEMAP; unblocked
- Blockers: None

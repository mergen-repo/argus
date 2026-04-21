# Post-Story Review: FIX-210 — Alert Deduplication + State Machine (Edge-triggered, Cooldown)

> Date: 2026-04-21

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| FIX-212 | Unified Event Envelope — publisher payload normalization is explicitly out-of-scope for FIX-210 (PAT-006 discipline preserved). FIX-212 can proceed without changes; dedup_key computation in `handleAlertPersist` is additive and does not conflict with future envelope unification. | NO_CHANGE |
| FIX-213 | Live Event Stream UX depends on FIX-212. Dedup fields (`occurrence_count`, `first_seen_at`, `last_seen_at`) are additive to the alert payload — FIX-213 alert body display can render them directly once FIX-212 lands. No plan update needed. | NO_CHANGE |
| FIX-229 | Alert enhancements (mute, export, etc.) inherit `occurrence_count` and `cooldown_until` fields from the live DTO shape. `SuppressAlert`/`UnsuppressAlert` store methods are available as extension points. No plan update needed. | NO_CHANGE |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| docs/USERTEST.md | FIX-210 section added — 5 scenarios (dedup badge, alert detail occurrence + cooldown banner, publisher edge-trigger, cooldown drop via REST, suppressed state PATCH rejection) | UPDATED |
| docs/GLOSSARY.md | 4 new terms added: Dedup Key, Cooldown Window, Occurrence Count, Alert Edge Trigger — placed in Argus Platform Terms section | UPDATED |
| docs/ARCHITECTURE.md | `alertstate/` subdirectory added to file tree (new package, D-076 consolidation); `notification/` and `api/alert/` entries updated for FIX-210; `store/alert.go` entry updated with new methods | UPDATED |
| docs/brainstorming/decisions.md | DEV-273..DEV-278 added (D1 column name, D2 nullability, D3 severity excluded from hash, D5 index state scope, D6 publisher edge-trigger scope, F-A1 cooldown wiring critical fix) | UPDATED |
| docs/brainstorming/bug-patterns.md | PAT-017 added: config parameter threaded to store but not REST handler constructor (PAT-011 recurrence — FIX-210 Gate F-A1) | UPDATED |
| docs/ROUTEMAP.md | FIX-210 status → DONE (2026-04-21); REVIEW changelog row added; D-076 already RESOLVED confirmed | UPDATED |
| CLAUDE.md | Story pointer advanced from FIX-210 → FIX-212 (next PENDING in Wave 3) | UPDATED |
| docs/PRODUCT.md | No dedicated alerts feature section exists in PRODUCT.md — dedup/cooldown behavior not documented there; adding a section would be out-of-scope for a reviewer-only pass | NO_CHANGE |
| docs/architecture/ERROR_CODES.md | Confirmed updated by Gate Task 7 (§Alerts Taxonomy + §Dedup & Cooldown sections present) | NO_CHANGE (verified) |
| docs/architecture/CONFIG.md | Confirmed `ALERT_COOLDOWN_MINUTES` row present (Gate Task 7) | NO_CHANGE (verified) |
| docs/architecture/api/_index.md | Confirmed API-313/314/315 updated with 5-field expansion note (Gate Task 7) | NO_CHANGE (verified) |
| docs/architecture/db/_index.md | Confirmed TBL-53 updated: +4 columns + 2 FIX-210 indices (Gate Task 7) | NO_CHANGE (verified) |
| .env.example | Confirmed `ALERT_COOLDOWN_MINUTES=5` present (Gate Task 7) | NO_CHANGE (verified) |
| Makefile | No new services or targets introduced | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 0
- D-076 RESOLVED status confirmed in ROUTEMAP Tech Debt table (stamped 2026-04-21 by Gate Task 7 / story step-log)
- `dedup_key` column name consistent across: migration, Go struct, TypeScript type, GLOSSARY, ERROR_CODES, db/_index.md (D1 decision applied uniformly)
- `ALERT_COOLDOWN_MINUTES` consistent across: config.go, .env.example, CONFIG.md, handler constructor parameter
- API contract preservation confirmed: `suppressed` state NOT settable via `PATCH /alerts/{id}` — API-315 note updated accordingly; handler `IsUpdateAllowed` gates on `alertstate` package

## Decision Tracing

- Decisions checked: 7 (D1–D6 from plan §Decisions + DEV-278 F-A1 gate fix)
- Applied in code (confirmed by gate grep gates): D1 (dedup_key name), D2 (nullable), D3 (severity excluded from hash), D4 (unique index via migration), D5 (state scope IN open/ack/suppressed), D6 (edge-trigger scope = 2 publishers), D7 (fired_at not updated on dedup hit)
- Orphaned (approved but not applied): 0
- New decisions captured this review: DEV-273..DEV-278 (6 entries)

## USERTEST Completeness

- Entry exists: YES (added in this review)
- Type: UI + backend scenarios (5 scenarios)
- Covers: dedup badge, occurrence detail, cooldown banner, cooldown metric probe, PATCH suppressed rejection, edge-trigger metric verification

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting this story: 1 (D-076)
- Already `✓ RESOLVED` by Gate (Gate Task 7 + step-log): 1 — D-076 CLOSED 2026-04-21 (FIX-210)
- Resolved by Reviewer (Gate missed marking): 0
- NOT addressed (CRITICAL): 0

## Mock Status

N/A — this is not a Frontend-First project. No `src/mocks/` directory.

## Issues

No issues found.

## Project Health

- Stories completed in Wave 2 (Alert Architecture): FIX-209 DONE, FIX-210 DONE, FIX-211 DONE — Wave 2 complete
- Current phase: UI Review Remediation [IN PROGRESS]
- Next story: FIX-212 — Unified Event Envelope + Name Resolution + Missing Publishers (Wave 3, P1, XL, depends on FIX-202 which is DONE)
- Blockers: None — FIX-212 dependency FIX-202 is DONE

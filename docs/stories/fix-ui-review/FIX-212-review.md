# Post-Story Review: FIX-212 — Unified Event Envelope + Name Resolution + Missing Publishers

> Date: 2026-04-21

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| FIX-213 | Consumes bus.Envelope directly. Entity.display_name + event_version + type field all available. Filter chips by type use `events.TypeForSubject` canonical map. No AC changes needed. | NO_CHANGE |
| FIX-219 | Name resolution infra complete: EntityRef.display_name authored at publisher, Resolver wired. FE side (EntityLink component + page audit) proceeds on stable DTO shape. Scope alignment confirmed. | NO_CHANGE |
| FIX-240 | Events catalog endpoint (API-316) ready. GET /api/v1/events/catalog returns canonical type+severity+entity_type. FIX-240 can wire FE consumer without backend work. | NO_CHANGE |
| FIX-237 | M2M Event Taxonomy story — benefits from TypeForSubject map and per-subject meta_schema in catalog. No plan changes needed; catalog is the input. | NO_CHANGE |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| docs/architecture/WEBSOCKET_EVENTS.md | Removed stale pre-FIX-212 `## Event Envelope` section (lines 178-197) that described old `{type,id,timestamp,data}` shape; canonical shape is in the new `## Event Envelope (FIX-212)` section at top | UPDATED |
| docs/GLOSSARY.md | Added 5 terms: Event Envelope, EntityRef, Display Name Resolver, SystemTenantID, Legacy Shape Metric. Updated Alert Source term to reflect FIX-212 closure (removed "until FIX-212" forward-reference) | UPDATED |
| docs/brainstorming/decisions.md | Added DEV-279..DEV-287 (FIX-212 D1..D9 decisions — snake_case, hybrid resolver, event_version shim, dedup_key pointer, SystemTenantID, 14-subject scope, catalog API-only, 1-entity cardinality, sim.updated-only new publisher) | UPDATED |
| docs/USERTEST.md | Added FIX-212 backend/altyapi stub after FIX-211 section with 4 shell verification commands | UPDATED |
| docs/architecture/api/_index.md | Added API-316: GET /api/v1/events/catalog (FIX-212 AC-5). Updated total count to 247. | UPDATED |
| docs/ARCHITECTURE.md | Added `bus/` annotation for bus.Envelope + WEBSOCKET_EVENTS.md pointer; added `events/` subdirectory entry (TypeForSubject map, Resolver, catalog handler API-316) | UPDATED |
| docs/ROUTEMAP.md | FIX-212 marked DONE (2026-04-21); changelog row added; D-075 confirmed RESOLVED; D-077/D-078/D-079 confirmed OPEN | UPDATED |
| CLAUDE.md | Story pointer advanced to FIX-213 / Step: Plan; WEBSOCKET_EVENTS.md description updated to note bus.Envelope canonical shape | UPDATED |
| decisions.md | See above | UPDATED |

## Cross-Doc Consistency

- Contradictions found: 1 — RESOLVED
  - `WEBSOCKET_EVENTS.md` had two conflicting envelope definitions: legacy `{type,id,timestamp,data}` shape (old `## Event Envelope` section, pre-FIX-212) AND new canonical `bus.Envelope` shape (new `## Event Envelope (FIX-212)` section). Developer added the new section but did not remove the old. Stale section removed in this review.

## Decision Tracing

- FIX-212 plan decisions checked: D1..D9 (9 decisions)
- Orphaned decisions at start of review: 9 (none were in decisions.md)
- Resolved by reviewer: 9 (DEV-279..DEV-287 added)

## USERTEST Completeness

- Entry exists: YES (added in this review)
- Type: backend/altyapi note with shell verification commands
- Consistent with FIX-206/207/209/210/211 backend pattern

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting this story: 2 (D-075, D-048)
- D-075 (systemTenantID sentinel removal): ✓ RESOLVED by Gate — confirmed; ROUTEMAP row already shows RESOLVED 2026-04-21
- D-048 (entity_refs display_name empty): ✓ RESOLVED by Gate — confirmed; ROUTEMAP row already shows RESOLVED 2026-04-21
- Items opened by this story: D-077 (deferred subjects), D-078 (legacy shim removal), D-079 (job publishers)
- All three confirmed OPEN in ROUTEMAP with correct target stories and gate conditions

## Mock Status

Not applicable — no `src/mocks/` directory; backend-only story; FE consumes envelope via existing WS hub contract.

## Issues

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | `docs/architecture/EVENT_ENVELOPE.md` does not exist on disk. Gate F-A14 mentioned it as a new file but Developer Task 8 placed the canonical spec inline in WEBSOCKET_EVENTS.md §Event Envelope (FIX-212). | NON-BLOCKING | RESOLVED — Option (a) selected | AUTOPILOT decision 2026-04-21: WEBSOCKET_EVENTS.md §Event Envelope (FIX-212) is the canonical home for the envelope spec — the section is comprehensive, cross-referenced from CLAUDE.md, ARCHITECTURE.md, and GLOSSARY.md, and matches Plan §Canonical Event Envelope. Gate F-A14 reference to a separate EVENT_ENVELOPE.md was bookkeeping drift (no spec-divergence). No standalone file needed. |
| 2 | FIX-212 decisions D1..D9 not recorded in decisions.md at gate close. All prior FIX stories added their decisions to decisions.md before review. | NON-BLOCKING | FIXED — DEV-279..DEV-287 added | 9 decision entries added covering all plan D-class decisions. |
| 3 | USERTEST.md missing FIX-212 section. Every prior FIX story (FIX-206..FIX-211) added a backend/altyapi stub. | NON-BLOCKING | FIXED | Backend note section added after FIX-211 with 4 shell verification commands. |
| 4 | GLOSSARY.md missing 5 FIX-212 terms: Event Envelope, EntityRef, Display Name Resolver, SystemTenantID, Legacy Shape Metric. | NON-BLOCKING | FIXED | 5 terms added to the Event Bus & Real-Time Terms section with full definitions and context refs. |
| 5 | GLOSSARY.md `Alert Source` term stale: text said "Until FIX-212 normalizes publisher envelopes, a systemTenantID sentinel fallback is used…" — FIX-212 is now done. | NON-BLOCKING | FIXED | Term updated to past tense; references D-075 RESOLVED. |
| 6 | api/_index.md missing API-316 (GET /api/v1/events/catalog). Endpoint is registered in router (`gateway/router.go:655`) and tested. | NON-BLOCKING | FIXED | API-316 row added; total count updated from 246 to 247. |
| 7 | ARCHITECTURE.md bus/ directory had no mention of bus.Envelope or the FIX-212 canonical envelope. events/ subdirectory (TypeForSubject, Resolver, catalog handler) absent from file tree. | NON-BLOCKING | FIXED | bus/ annotation added with WEBSOCKET_EVENTS.md pointer; events/ subdirectory entry added. |
| 8 | WEBSOCKET_EVENTS.md had two conflicting envelope definitions: pre-FIX-212 legacy section (lines 178-197, `{type,id,timestamp,data}`) coexisted with new FIX-212 canonical section (lines 8-82, `bus.Envelope`). Developer added new section but did not remove the old. | NON-BLOCKING | FIXED | Legacy duplicate section removed. |

## Project Health

- UI Review Remediation stories completed: 12/44 (FIX-201..FIX-212 done)
- Current phase: UI Review Remediation Wave 3 [partial] — FIX-213 next
- Next story: FIX-213 (Live Event Stream UX — filter chips, usage display, alert body)
- Blockers: EVENT_ENVELOPE.md escalated — does NOT block FIX-213 (canonical spec lives in WEBSOCKET_EVENTS.md §Event Envelope (FIX-212) and is complete)

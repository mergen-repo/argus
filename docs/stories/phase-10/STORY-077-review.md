# Post-Story Review: STORY-077 — Enterprise UX Polish & Ergonomics

> Date: 2026-04-13

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| STORY-062 | New tables (user_views, announcements, announcement_dismissals, chart_annotations, user_column_preferences) must be added to `docs/architecture/db/_index.md`. New API endpoints (saved views CRUD, undo, announcements CRUD, impersonate, chart annotations, export for 12 entities) must be added to `docs/architecture/api/_index.md`. The API count header in `docs/ARCHITECTURE.md` ("204 APIs") is now stale. Two gate escalations (DEV-223 ImpersonateExit no-JWT, DEV-224 `act_sub` claim path) are candidates for code fixes. | FLAG_FOR_STORY-062 |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| docs/SCREENS.md | Counter updated `64 → 66 screens`; added STORY-077 to counts note; added SCR-175 (Announcements `/admin/announcements`) and SCR-176 (Impersonate User `/admin/impersonate`) | UPDATED |
| docs/GLOSSARY.md | Added "Enterprise UX Terms" section with 5 new terms: Saved View, Impersonation Session, Announcement, Chart Annotation, GeoIP Lookup | UPDATED |
| docs/ARCHITECTURE.md | Project structure tree: added `internal/undo/`, `internal/geoip/`, `internal/export/`, `internal/middleware/impersonation.go`, `internal/api/announcement/`, `internal/api/undo/` | UPDATED |
| docs/USERTEST.md | Added STORY-077 section: 16 scenarios (backend 6, frontend 10) | UPDATED |
| docs/brainstorming/decisions.md | Added DEV-222 (sessions/alerts CSV gap), DEV-223 (ImpersonateExit no JWT), DEV-224 (`act_sub` claim path mismatch) | UPDATED |
| docs/ROUTEMAP.md | Tech debt D-001, D-002, D-006, D-007, D-008, D-009 all marked `✓ RESOLVED (2026-04-13)` | UPDATED |
| docs/FUTURE.md | No changes | NO_CHANGE |
| Makefile | No changes — no new services or build targets required | NO_CHANGE |
| CLAUDE.md | No changes — Docker URLs/ports unchanged | NO_CHANGE |
| docs/FRONTEND.md | No changes | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 0
- API count in ARCHITECTURE.md header ("204 APIs") is stale — flagged for STORY-062 (adds 20+ endpoints for new entities); not updated here to avoid mid-story count drift
- SCREENS.md counter now reflects 66 screens; consistent with new SCR-175/176 entries
- GLOSSARY new terms match implementation paths cited in gate report
- ROUTEMAP tech debt table: 6 resolved items closed, 2 remaining open items (D-003 for STORY-062)
- decisions.md: DEV-221 (D-008 deferred to STORY-077) now resolved; DEV-222/223/224 document the gate escalations as accepted decisions

## Decision Tracing

- Decisions checked: Gate report documents 1 partial gap + 2 escalations
- DEV-222: Sessions and alerts CSV export gap — ACCEPTED (12/14 entities shipped, 2 deferred to STORY-062)
- DEV-223: ImpersonateExit returns message-only, no JWT restoration — ACCEPTED (re-login workaround acceptable for now, STORY-062)
- DEV-224: `impersonatedBy` always null due to `act_sub` vs `payload.act?.sub` mismatch — ACCEPTED (no current consumer broken; STORY-062)
- Pre-existing decisions DEV-217 through DEV-221 (STORY-076 trade-offs) remain ACCEPTED; D-008 and D-009 fully resolved by this story
- Orphaned approved decisions not yet applied: 0

## USERTEST Completeness

- Entry exists: YES (added in this review cycle)
- Type: Functional + API scenarios (16 total: 6 backend API scenarios + 10 frontend interaction scenarios)
- Covers: saved views round-trip, undo toast with countdown, inline edit + optimistic rollback, CSV export streaming, empty state CTAs, data freshness indicator, impersonation banner + read-only enforcement, announcements dismiss, language toggle TR/EN, table density and column customization

## Tech Debt Pickup

Items targeting STORY-077 from ROUTEMAP.md:

| Item | Source | Status |
|------|--------|--------|
| D-001 | Raw `<input>` in ip-pool-detail.tsx | RESOLVED — grep confirms 0 raw `<input>` remain |
| D-002 | Raw `<button>` in ip-pool-detail.tsx + apns/index.tsx | RESOLVED — grep confirms 0 raw `<button>` remain |
| D-006 | GeoIP lookup for sessions location field | RESOLVED — `internal/geoip/lookup.go` wired in sessions_global.go |
| D-007 | APN detail "Policies Referencing" tab | RESOLVED — store `ListReferencingAPN` + trigram index + `apns/detail.tsx:PoliciesReferencingTab` |
| D-008 | Flat search response shape | RESOLVED — search handler now returns per-type DTOs with enriched fields |
| D-009 | `data-row-index`/`data-href` not on list pages | RESOLVED — 14 list pages annotated (gate fix #7) |

New tech debt items introduced by STORY-077:

| Item | Source | Description | Target | Status |
|------|--------|-------------|--------|--------|
| D-010 | DEV-222 | Sessions and alerts CSV export missing (2/14 entities) | STORY-062 | DEFERRED |
| D-011 | DEV-223 | ImpersonateExit no JWT in response body (forces re-login) | STORY-062 | DEFERRED |
| D-012 | DEV-224 | `impersonatedBy` always null (`act_sub` vs `payload.act?.sub`) | STORY-062 | DEFERRED |

## Mock Status

- No `web/src/mocks/` directory. Project does not use mock files.
- New hooks/pages checked: `use-announcements.ts`, `use-impersonation.ts`, `use-saved-views.ts`, `announcements.tsx`, `impersonate-list.tsx` — 0 hardcoded mock data found.

## Issues

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | D-001/D-002: Raw HTML elements in ip-pool-detail + apns | NON-BLOCKING | RESOLVED | Grep confirms 0 raw `<input>` or `<button>` remain in affected files |
| 2 | D-006–D-009: Four deferred tech debt items | NON-BLOCKING | RESOLVED | All four addressed in this story per gate report evidence |
| 3 | SCREENS.md missing SCR-175 (Announcements) and SCR-176 (Impersonate) | NON-BLOCKING | FIXED | Entries added; counter updated to 66 |
| 4 | ARCHITECTURE.md project structure missing 5 new packages | NON-BLOCKING | FIXED | `internal/undo/`, `internal/geoip/`, `internal/export/`, `internal/middleware/impersonation.go`, `internal/api/announcement/`, `internal/api/undo/` added to tree |
| 5 | GLOSSARY.md missing new UX domain terms | NON-BLOCKING | FIXED | "Enterprise UX Terms" section added with 5 terms |
| 6 | USERTEST.md missing STORY-077 section | NON-BLOCKING | FIXED | 16-scenario section added |
| 7 | decisions.md missing gate escalations | NON-BLOCKING | FIXED | DEV-222, DEV-223, DEV-224 added |
| 8 | AC-4 partial: sessions and alerts CSV export not implemented | NON-BLOCKING | DEFERRED DEV-222 | 12/14 entities covered. Sessions and alerts export requires new handler methods. Deferred to STORY-062. |
| 9 | AC-9: ImpersonateExit forces re-login (no JWT restoration) | NON-BLOCKING | DEFERRED DEV-223 | Server-side original-JWT storage needed. Current re-login behavior is functional. Deferred to STORY-062. |
| 10 | `impersonatedBy` always null (`act_sub` claim path mismatch) | NON-BLOCKING | DEFERRED DEV-224 | No current consumer affected. Claim read path fix is one-line. Deferred to STORY-062. |

## Project Health

- Stories completed: 19/22 (86%) — STORY-077 now closes to 20/22 after orchestrator update
- Phase 10 remaining: STORY-062 (Performance & Doc Drift Cleanup — final sweep)
- Blockers: None
- STORY-062 scope impact: +5 new tables to db/_index.md, +20+ new endpoints to api/_index.md, 3 code fixes (DEV-222/223/224)

# Post-Story Review: STORY-017 — Session Management & Concurrent Control

> Date: 2026-03-20

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| STORY-025 | STORY-017 provides the CoA/DM infrastructure and `session.Manager.Terminate` that STORY-025 will use for mass CoA during policy rollout/rollback. `DMSender.SendDM` is the exact function rollout will call per active session. No changes needed to STORY-025 spec -- assumptions are valid. | NO_CHANGE |
| STORY-033 | STORY-017 already implemented API-101 (`GET /sessions/stats`) which STORY-033 lists as a dependency. STORY-033 can build on this: add Redis sliding-window auth counters, latency percentiles, and extend the existing stats DTO with real-time metrics. The `session.Manager.CountActive` method is ready for use. STORY-033 scope slightly reduced since session stats endpoint already exists. | NO_CHANGE (scope note added to changelog) |
| STORY-036 | STORY-017 provides session event stream (`session.started`, `session.ended` via NATS) that STORY-036 needs for real-time anomaly detection (SIM cloning, auth floods). Session data in Redis is available for sliding-window checks. No spec changes needed. | NO_CHANGE |
| STORY-052 | STORY-017 is now unblocked. Session management is the last dependency. Performance tuning can now benchmark the full auth->session->disconnect flow. | NO_CHANGE |
| STORY-019 | No direct impact. Diameter server will use the same `session.Manager` for Diameter accounting sessions. The session management layer is protocol-agnostic (good design). | NO_CHANGE |
| STORY-047 | Frontend Sessions page can now be built against the 4 session API endpoints. All endpoints follow standard envelope format. | NO_CHANGE |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| ROUTEMAP.md | STORY-017 marked DONE, counter 18/55 (33%), next story STORY-019, changelog entry added | UPDATED |
| GLOSSARY.md | Added 5 terms: Concurrent Session Control, Idle Timeout, Hard Timeout, Timeout Sweeper, Bulk Disconnect | UPDATED |
| api/_index.md | API-101 and API-103 now reference STORY-017 as implementing story (alongside future STORY-033/STORY-030 for extended features) | UPDATED |
| ERROR_CODES.md | No changes needed -- handler uses existing codes (NOT_FOUND, CONFLICT, VALIDATION_ERROR, INTERNAL_ERROR, INVALID_FORMAT) | NO_CHANGE |
| ARCHITECTURE.md | No changes needed -- session management already in project structure, SVC-04 description covers sessions | NO_CHANGE |
| SCREENS.md | No changes needed -- SCR-050 (Live Sessions) already defined | NO_CHANGE |
| FRONTEND.md | No changes needed | NO_CHANGE |
| FUTURE.md | No changes needed -- no new future opportunities revealed | NO_CHANGE |
| Makefile | No changes needed -- no new targets required | NO_CHANGE |
| CLAUDE.md | No changes needed -- no new ports or Docker services | NO_CHANGE |
| decisions.md | No new decisions to capture -- implementation followed G-023 (session management spec) exactly | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 1 (minor, fixed)
- API-051 (`GET /api/v1/sims/:id/sessions`) is listed in the API index referencing STORY-017, but the STORY-017 spec only defines API-100 to API-103. API-051 is a SIM-specific session history endpoint that is conceptually separate -- it queries session history (including closed sessions) for a specific SIM, while API-100 lists active sessions globally. This is not a true contradiction; API-051 remains unimplemented and will likely be addressed in STORY-044 (Frontend SIM Detail) or a future story. No fix needed at this time.
- API-101 and API-103 references in api/_index.md were pointing only to STORY-033 and STORY-030 respectively, but the basic implementations were delivered in STORY-017. Fixed by adding STORY-017 as co-reference.
- All 4 session endpoints in router.go match the story spec paths exactly.
- Auth roles in router.go match API contract: sim_manager (list, disconnect), analyst (stats), tenant_admin (bulk disconnect).
- NATS subjects `session.started` and `session.ended` are properly defined in `internal/bus/nats.go` and used in server.go and handler.go.

## Code Quality Observations

- **Layer separation is clean**: Store -> Manager -> Handler -> Router follows the established pattern.
- **Duplicate DM send + NAS IP parsing logic** appears in 4 places (handler.go:231-244, handler.go:316-325, bulk_disconnect.go:93-102, sweep.go:183-198). Consider extracting a `disconnectSessionViaDM(ctx, sess)` helper in the session package for STORY-019 or later.
- **`Terminate` zeroes counters**: `session.Manager.Terminate` calls `store.Finalize` with `bytesIn=0, bytesOut=0`. This is correct for admin-initiated disconnects where final counters are unknown, but the distinction between `Terminate` (no counters) and `TerminateWithCounters` (from RADIUS Acct-Stop) is important for CDR accuracy in STORY-032.
- **`TestHandler_Disconnect_Success` is skipped**: The stub Manager (nil store + nil Redis) cannot round-trip Create/Get. This is acceptable for now; a proper integration test will cover this in STORY-051.

## Project Health

- Stories completed: 18/55 (33%)
- Current phase: Phase 3 — AAA Engine
- Phase 3 progress: 4/7 stories done (STORY-018, STORY-015, STORY-016, STORY-017)
- Next story: STORY-019 (Diameter Protocol Server)
- Blockers: None
- Test health: 30/30 packages pass, 0 regressions

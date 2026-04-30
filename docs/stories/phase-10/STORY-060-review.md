# Post-Story Review: STORY-060 — AAA Protocol Correctness

> Date: 2026-04-12

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| STORY-061 | AC-3 explicitly depends on STORY-060 AC-6 (`disconnectActiveSessionsForSwitch` + DM dispatch). Interface is `SetSessionDeps` on `esim/handler.go`. No changes to the interface — STORY-061 can wire directly to the already-wired `SessionDeps` from main.go. `force=true` bypass and 409 `SESSION_DISCONNECT_FAILED` path are now implemented and tested — STORY-061 AC-3 test scenarios can rely on them. | NO_CHANGE |
| STORY-063 | Backend Implementation Completeness story. RAT enum canonical alignment (AC-9) consolidates `rattype` as single source of truth. DSL parser now imports `internal/aaa/rattype` — STORY-063 must NOT re-introduce local RAT type maps in any new package it creates. `IsRecognized` + `AllCanonical` helpers available for reuse. | NO_CHANGE |
| STORY-064 | DB Hardening. Migration `20260411000001_normalize_rat_type_values` normalizes `sessions.rat_type`, `sims.last_rat_type`, `cdrs.rat_type`. STORY-064 partitioning/index work on these tables should run AFTER this migration lands. Migration is idempotent (safe regardless of order, but logical sequencing matters for schema audits). | NO_CHANGE |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| GLOSSARY.md | WS Server: 10s → 90s pong deadline, added WS_PONG_TIMEOUT + per-user limit info + 4029 close code + BroadcastReconnect. WS Hub: drops newest → drops oldest. WS Close Code: added 4029 (MaxConnsPerUser). | UPDATED |
| ARCHITECTURE.md | Caching Strategy table: added MSK stash (EAP session) row — In-memory sync.Map, 10s TTL, LoadAndDelete single-use semantics. | UPDATED |
| decisions.md | VAL-007 marked SUPERSEDED by DEV-149. DEV-045 marked SUPERSEDED by DEV-148. PAT-003 added (AT_MAC-not-zeroed HMAC bug pattern). | UPDATED |
| api/_index.md | API-074: added `disconnected_sessions`, `force=true`, 409 `SESSION_DISCONNECT_FAILED` note + STORY-060 ref. API-065: added CoA counters (`coa_sent_count`, `coa_acked_count`, `coa_failed_count`) note + STORY-060 ref. | UPDATED |
| WEBSOCKET_EVENTS.md | No changes needed — pong 90s, drop-oldest, reconnect message, per-user limit all correctly documented by Gate. | NO_CHANGE |
| CONFIG.md | No changes needed — WS_PONG_TIMEOUT, WS_MAX_CONNS_PER_USER, DIAMETER_TLS_* all present (confirmed by Gate). | NO_CHANGE |
| SCREENS.md | No changes — story has no UI (has_ui: false). | NO_CHANGE |
| FRONTEND.md | No changes. | NO_CHANGE |
| FUTURE.md | No changes. | NO_CHANGE |
| Makefile | No changes. | NO_CHANGE |
| CLAUDE.md | No changes. | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 6 (all fixed — see Issues table)
- GLOSSARY.md had 3 stale entries from STORY-040 that were never updated when STORY-060 changed WS behavior
- ARCHITECTURE.md missing MSK in-memory stash (architectural performance change)
- decisions.md had 2 superseded decisions still marked ACCEPTED without supersession notice
- api/_index.md had 2 endpoints missing STORY-060 behavioral additions

## Decision Tracing

- Decisions checked: DEV-148 through DEV-157 (10 decisions)
- Orphaned (approved but not applied): 0
- All 10 DEV-148..157 decisions verified reflected in gate report code paths
- VAL-007 (dual-MAC EAP acceptance) and DEV-045 (Redis-based MSK retrieval) correctly marked SUPERSEDED

## USERTEST Completeness

- Entry exists: YES
- Type: Backend/protocol note (Turkish) confirming this is a backend/protocol story — correct per reviewer protocol
- 14 test scenarios listed (WS pong, drop-oldest, per-user limit 4029, reconnect message, EAP-SIM HMAC, Diameter TLS, RAT canonical, eSIM DM, bulk CoA)

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting this story: 0
- Already resolved by Gate: N/A
- Resolved by Reviewer (Gate missed marking): N/A
- NOT addressed (CRITICAL): 0

## Mock Status

- N/A — this is a backend-only story (has_ui: false). No mock retirement applicable.

## Issues

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | GLOSSARY.md "WS Server" still said `10s pong deadline` (STORY-040 original value). STORY-060 AC-2 changed it to 90s configurable via `WS_PONG_TIMEOUT`. | NON-BLOCKING | FIXED | Updated to `90s pong deadline configurable via WS_PONG_TIMEOUT`, added per-user limit 5/WS_MAX_CONNS_PER_USER, close code 4029, BroadcastReconnect reference. |
| 2 | GLOSSARY.md "WS Hub" still said `drops newest on overflow` (STORY-040 original). STORY-060 AC-3 changed backpressure to drop-oldest. | NON-BLOCKING | FIXED | Updated to `drops oldest on overflow`. Also added BroadcastReconnect to the Methods list. |
| 3 | GLOSSARY.md "WS Close Code" entry listed only 4001/4002/4003/4004. STORY-060 AC-4 adds close code 4029 (MaxConnsPerUser). | NON-BLOCKING | FIXED | Added `4029 (MaxConnsPerUser — per-user connection limit exceeded, oldest connection evicted)` to close code list. |
| 4 | ARCHITECTURE.md Caching Strategy table had no row for MSK in-memory stash. STORY-060 AC-1 moved MSK from Redis GETDEL to in-memory `sync.Map` with 10s TTL + 30s sweeper — a significant hot-path architectural change (eliminates Redis round-trip from RADIUS Access-Accept path). | NON-BLOCKING | FIXED | Added row: `MSK stash (EAP session) \| In-memory sync.Map \| 10s (30s sweeper) \| LoadAndDelete on consume (single-use)`. |
| 5 | decisions.md VAL-007 (EAP-SIM dual-MAC acceptance) still marked ACCEPTED with no supersession note. STORY-060 DEV-149 removes the dual-MAC path entirely — VAL-007 is now invalid. | NON-BLOCKING | FIXED | Appended `→ SUPERSEDED by DEV-149 (STORY-060)` to VAL-007 status. |
| 6 | decisions.md DEV-045 (Redis-based MSK retrieval with silent nil on race) still marked ACCEPTED. STORY-060 DEV-148 replaces this pattern with in-memory sync.Map. | NON-BLOCKING | FIXED | Appended `→ SUPERSEDED by DEV-148 (STORY-060)` to DEV-045 status. |
| 7 | decisions.md Bug Patterns had PAT-001 and PAT-002 but no PAT-003 for AT_MAC-not-zeroed HMAC bug (RFC 4186 §10.15 pattern). This was the root cause that necessitated STORY-060 AC-1. | NON-BLOCKING | FIXED | Added PAT-003: AT_MAC field MUST be zeroed before computing HMAC-SHA1 in EAP-SIM/AKA. Root cause, prevention, and affected packages documented. |
| 8 | api/_index.md API-074 one-liner missing STORY-060 behavioral additions: `disconnected_sessions` response field, `force=true` bypass flag, 409 `SESSION_DISCONNECT_FAILED` error code. | NON-BLOCKING | FIXED | Added behavioral details and STORY-060 ref to API-074 row. |
| 9 | api/_index.md API-065 one-liner missing STORY-060 behavioral additions: CoA counters (`coa_sent_count`, `coa_acked_count`, `coa_failed_count`) in async job result, CoA dispatch outside distLock. | NON-BLOCKING | FIXED | Added CoA counter details and STORY-060 ref to API-065 row. |

## Project Health

- Stories completed: 5/22 (23%) in Phase 10 (STORY-056, 057, 058, 059, 060)
- Current phase: Phase 10 — Cleanup & Production Hardening
- Next story: STORY-061 (eSIM Model Evolution), STORY-063 (Backend Implementation Completeness) — parallel Wave 2
- Blockers: None. STORY-061 dependency on STORY-060 AC-6 is satisfied.

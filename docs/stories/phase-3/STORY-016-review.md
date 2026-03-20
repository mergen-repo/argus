# Post-Story Review: STORY-016 — EAP-SIM/AKA/AKA' Authentication Methods

> Date: 2026-03-20

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| STORY-017 (Session Management) | Sessions now have `auth_method` populated (EAP-SIM/EAP-AKA/EAP-AKA'). The Session struct already has `AuthMethod` field, and `radiusSessionToSession` maps it. API-100 response should include `auth_method` when implementing the session list endpoint. No blocker, just an enrichment. | NO_CHANGE |
| STORY-019 (Diameter Server) | EAP state machine and method handlers are protocol-agnostic. Diameter can reuse the same `eap.StateMachine` for EAP-based auth if needed. The session auth_method recording pattern (sync.Map bridge) may need adaptation for Diameter's session model. | NO_CHANGE |
| STORY-020 (5G SBA AUSF/UDM) | **Dependency satisfied.** STORY-020 was blocked by STORY-016 for EAP-AKA' implementation. AKA' handler with AT_KDF/AT_KDF_INPUT is now complete. STORY-020 can proxy to internal EAP-AKA' implementation as planned (AC: "EAP-AKA' flow via SBA: proxy to internal EAP-AKA' implementation"). **Note:** AKA' KDF input currently uses hardcoded network name `argus.eap.5g` (see VAL-008). STORY-020 should pass the real serving network name from the 5G authentication context. | NO_CHANGE |
| STORY-021 (Operator Failover) | No direct impact. Circuit breaker and failover routing operate at the operator adapter level, below EAP. EAP uses the adapter via `AdapterVectorProvider` which will naturally participate in failover when the adapter layer routes requests. | NO_CHANGE |
| STORY-027 (RAT-Type Awareness) | Session `RATType` field already exists in `session.Session` struct (added in STORY-015). STORY-027 needs to extract RAT-type from RADIUS 3GPP-RAT-Type AVP in `handleAuth` and pass it through to session creation. The EAP auth flow in `handleEAPAuth` should also extract RAT-type from the original Access-Request. | NO_CHANGE |
| STORY-052 (AAA Performance Tuning) | EAP adds 1-2 extra RADIUS round-trips (Access-Challenge) compared to direct auth. This is protocol-inherent. Vector caching with batch pre-fetch mitigates adapter latency. Benchmarks in STORY-052 should include EAP flow scenarios alongside direct auth. | NO_CHANGE |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| decisions.md | DEV-044..DEV-047, VAL-007..VAL-008, PERF-015..PERF-016, TEST-006..TEST-008 already added during story execution | NO_CHANGE |
| GLOSSARY.md | Added 5 terms: MSK, EMSK, MS-MPPE, EAP State Machine, Vector Cache | UPDATED |
| ARCHITECTURE.md | Added 2 entries to Caching Strategy table: "EAP session state" (Redis 30s TTL) and "Auth vector pre-fetch" (Redis list 5min TTL) | UPDATED |
| ROUTEMAP.md | STORY-016 marked DONE, progress 17/55 (31%), next story STORY-017, changelog entry added | UPDATED |
| SCREENS.md | No changes — STORY-016 is backend-only, no UI | NO_CHANGE |
| FRONTEND.md | No changes | NO_CHANGE |
| FUTURE.md | No changes — EAP implementation doesn't reveal new future opportunities beyond existing FTR items | NO_CHANGE |
| Makefile | No changes — no new build targets or services | NO_CHANGE |
| CLAUDE.md | No changes — no Docker port changes | NO_CHANGE |
| ERROR_CODES.md | No changes — EAP errors are protocol-level (RADIUS Access-Reject), not REST API errors | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 0
- STORY-016 story doc references SVC-04, TBL-17, TBL-10 -- all correct and exist in architecture
- STORY-016 story doc references SCR-050 (Live Sessions) and SCR-041 (Operator Detail) -- both exist in SCREENS.md
- STORY-016 dependency "Blocked by: STORY-015, STORY-018" -- both completed before STORY-016
- STORY-016 dependency "Blocks: STORY-020 (5G SBA uses AKA')" -- STORY-020 story file confirms `Blocked by: STORY-016 (EAP-AKA')`
- ARCHITECTURE.md caching table now includes EAP entries, consistent with implementation
- GLOSSARY.md now covers all key EAP terms used in implementation
- ROUTEMAP changelog and progress counter are consistent
- `session.Session.AuthMethod` field confirmed in code, `radiusSessionToSession` maps it from `store.RadiusSession.AuthMethod`, `CreateRadiusSessionParams.AuthMethod` passes it to DB -- full layer chain verified
- `handleAcctStart` uses `sync.Map.LoadAndDelete` to bridge EAP auth result from Access-Accept to Accounting-Start -- documented in DEV-044

## Observations

1. **MSK retrieval race condition (DEV-045):** `GetSessionMSK` reads from Redis state store, but `handleChallenge` deletes the session on success. The code works because `sendEAPAccept` runs synchronously before the delete propagates, but this is fragile. A future improvement could save the MSK separately or pass it directly from the state machine result rather than re-fetching from store.

2. **EAP-SIM dual MAC verification (VAL-007):** `handleChallengeResponse` accepts both HMAC MAC and simple SRES concatenation via `verifySimpleSRES`. This is intentional for test client compatibility but should be configurable in production. Tracked in decisions.md.

3. **AKA' network name hardcoded (VAL-008):** KDF input uses `argus.eap.5g` instead of real serving network name. STORY-020 should address this when implementing 5G SBA.

4. **Test coverage gap:** `CachedVectorProvider` cache hit/miss with actual Redis (not nil Redis passthrough) is not tested at unit level. Deferred to integration tests (TEST-007). Acceptable for v1.

## Project Health

- Stories completed: 17/55 (31%)
- Current phase: Phase 3 (AAA Engine)
- Next story: STORY-017 (Session Management & Concurrent Control)
- Phase 3 remaining: STORY-017 (L), STORY-019 (XL), STORY-020 (L), STORY-021 (M, scope reduced)
- Blockers: None
- Phase 3 progress: 3/7 stories complete (STORY-018, STORY-015, STORY-016)

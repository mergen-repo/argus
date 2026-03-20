# Post-Story Review: STORY-018 — Pluggable Operator Adapter Framework & Mock Simulator

> Date: 2026-03-20

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| STORY-015 | Adapter interface now provides `Authenticate`, `AccountingUpdate`, `FetchAuthVectors` methods that RADIUS server can call via `OperatorRouter`. RADIUS adapter already implements real UDP forwarding for auth/acct. STORY-015 should use `OperatorRouter.Authenticate()` and `OperatorRouter.ForwardAuth()` for operator-side forwarding when proxying requests. Dependency chain confirmed: STORY-015 depends on STORY-018 (adapter framework) + STORY-011 (SIM CRUD). Removed stale "Blocks: STORY-018" from STORY-015 spec since STORY-018 is now complete. | UPDATED |
| STORY-016 | `FetchAuthVectors` is now available on the Adapter interface. Mock adapter generates deterministic EAP triplets/quintets (SHA256-seeded). STORY-016 can use `OperatorRouter.FetchAuthVectors()` to obtain vectors from operator HLR/AuC. For mock/test flows, the mock adapter already returns valid triplets (RAND=16B, SRES=4B, Kc=8B) and quintets (RAND=16B, AUTN=16B, XRES=8B, CK=16B, IK=16B). RADIUS/Diameter adapters return `ErrUnsupportedProtocol` for vector fetch — STORY-016 should handle this by using dedicated S6a/MAP interface or falling back to mock vectors for testing. | NO_CHANGE |
| STORY-019 | Diameter adapter now has CER/CEA handshake (on first `getConnection`), DWR/DWA watchdog (30s interval), `Authenticate`/`AccountingUpdate` methods, and CCR/CCA + RAR/RAA forwarding. STORY-019 (Diameter server) can reuse `internal/aaa/diameter` message encoding/decoding already imported by the adapter. The adapter's `ForwardAuth`/`ForwardAcct` provide the client-side Diameter forwarding; STORY-019 builds the server-side listener. No spec changes needed. | NO_CHANGE |
| STORY-020 | SBA adapter (`internal/operator/adapter/sba.go`) now exists with HTTP/2 client, AUSF auth endpoint (`/nausf-auth/v1/ue-authentications`), UDM vector endpoint (`/nudm-ueau/v1/{imsi}/security-information/generate-auth-data`), and accounting endpoint (`/npcf-smpolicycontrol/v1/sm-policies`). STORY-020 builds the server-side SBA proxy; the adapter provides client-side forwarding to external 5G core NFs. Note: SBA adapter `FetchAuthVectors` currently returns zero-filled vector stubs (decision VAL-003) — real UDM response parsing will be needed in STORY-020. | NO_CHANGE |
| STORY-021 | Circuit breaker, failover policies (reject/fallback_to_next/queue_with_timeout), `FailoverEngine`, `ForwardAuthWithFailover`, `ForwardAuthWithPolicy`, and `ExecuteAcct` with failover are all already implemented in `router.go` and `failover.go`. STORY-021's remaining scope is: (a) NATS event publishing on state transitions, (b) SVC-08 notification alerts, (c) WebSocket push for health changes, (d) SLA violation detection. Effort estimate should reduce from L to M since core failover routing is complete. | UPDATED |
| STORY-026 | SoR engine (Phase 4) depends on adapter framework for operator routing. STORY-018 provides `OperatorRouter.Authenticate()` and `OperatorRouter.FetchAuthVectors()` which SoR can use to route auth to preferred operator. No spec changes needed. | NO_CHANGE |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| GLOSSARY.md | Added 5 terms: Authentication Vector, Triplet, Quintet, CER/CEA, DWR/DWA | UPDATED |
| ROUTEMAP.md | Marked STORY-018 as DONE, updated progress to 15/55 (27%), set next story to STORY-015, added changelog entry | UPDATED |
| decisions.md | Updated DEV-022 from ACCEPTED to RESOLVED (SBA factory now implemented by STORY-018) | UPDATED |
| STORY-018-operator-adapter.md | Removed stale dependency "Blocked by: STORY-015" (STORY-018 was implemented before STORY-015, confirming ROUTEMAP order is correct) | UPDATED |
| STORY-015-radius-server.md | Removed "STORY-018" from Blocks list (STORY-018 is now complete) | UPDATED |
| ARCHITECTURE.md | No changes needed — SVC-06 description already includes "operator adapters" | NO_CHANGE |
| SCREENS.md | No changes — backend-only story, no UI impact | NO_CHANGE |
| FRONTEND.md | No changes — no frontend work | NO_CHANGE |
| FUTURE.md | No changes — mock simulator noted in FTR-005/FTR-007 as extensible for digital twin, still valid | NO_CHANGE |
| Makefile | No changes — no new targets, services, or env vars | NO_CHANGE |
| CLAUDE.md | No changes — no Docker URL/port changes | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 1 (fixed)
  - **STORY-018 spec** listed "Blocked by: STORY-015" but STORY-018 was implemented before STORY-015. The ROUTEMAP correctly showed STORY-018 first. Fixed by removing the stale STORY-015 dependency from STORY-018 spec and removing "Blocks: STORY-018" from STORY-015 spec.

- Consistency verifications performed:
  - API-024 reference in `api/_index.md` correctly points to STORY-018 spec
  - `adapter_type` column in `docs/architecture/db/operator.md` lists `mock, radius, diameter, sba` — matches registry factories
  - SVC-06 in services index mentions "operator adapters" — matches implementation
  - Gate decisions (VAL-001, VAL-002, VAL-003, TEST-001, TEST-002, TEST-003) all recorded in decisions.md
  - STORY-021 note correctly references STORY-009 for pre-existing circuit breaker + health check

## Decisions Captured

No new implicit decisions to capture. All STORY-018 decisions were already recorded in decisions.md during the gate phase (VAL-001 through VAL-003, TEST-001 through TEST-003).

## Project Health

- Stories completed: 15/55 (27%)
- Current phase: Phase 3 (AAA Engine) — 1/7 stories complete
- Next story: STORY-015 (RADIUS Authentication & Accounting Server)
- Blockers: None
- Phase 3 remaining: STORY-015 (XL), STORY-016 (L), STORY-017 (L), STORY-019 (XL), STORY-020 (L), STORY-021 (L, scope reduced to M)
- Key observation: STORY-021 scope significantly reduced — core failover routing (circuit breaker, 3 policies, FailoverEngine) already implemented across STORY-009 + STORY-018. Remaining work is event/notification integration only.

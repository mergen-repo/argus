# STORY-060: AAA Protocol Correctness

## User Story
As a protocol engineer and interoperability auditor, I want every deliberate test-compat/pragmatic shortcut in our AAA and WebSocket stacks replaced with spec-correct behavior, so that Argus passes protocol compliance audits and interoperates with third-party RADIUS/Diameter/5G peers and standard WS clients.

## Description
Close all protocol DEFER items identified in reviews: EAP-SIM dual-MAC compat, WebSocket pong timeout / drop semantics / per-user connection limit / reconnect control message, Profile Switch CoA/DM triggers, Bulk policy assign CoA dispatch, Diameter/TLS wiring, and RAT enum canonical alignment across DSL, SoR, and STORY-027 `rattype` package. After this story, zero protocol-level shortcuts remain.

## Architecture Reference
- Services: SVC-02 (WebSocket), SVC-04 (AAA — RADIUS/EAP/Diameter), SVC-05 (Policy), SVC-06 (Operator)
- Packages: internal/aaa/eap, internal/aaa/radius, internal/aaa/diameter, internal/aaa/rattype, internal/ws, internal/policy/rollout, internal/policy/bulk, internal/esim, internal/operator/sor
- Source: docs/architecture/PROTOCOLS.md, docs/architecture/WEBSOCKET_EVENTS.md, docs/architecture/ALGORITHMS.md, reviews: STORY-016, STORY-022, STORY-026, STORY-028, STORY-030, STORY-040, STORY-054 (Diameter/TLS deferral), STORY-027 (RAT enum alignment)

## Screen Reference
- SCR-070 (Live Sessions — CoA/DM triggers visible in session audit)
- SCR-100 (Policy Editor — rollout status shows CoA dispatch count)
- SCR-072 (eSIM — profile switch reflects session disconnect)

## Acceptance Criteria
- [ ] AC-1: EAP-SIM authentication removes dual-MAC test-compat path. Only RFC 4186 HMAC MAC accepted. Test fixtures regenerated to produce spec-correct MAC. `GetSessionMSK` race condition fixed with Redis `GETDEL`.
- [ ] AC-2: WebSocket pong timeout aligned to spec 90s (currently 10s/40s effective). `PONG_TIMEOUT` config var added, default 90s. Existing WEBSOCKET_EVENTS.md spec is authoritative.
- [ ] AC-3: WebSocket backpressure drops **oldest** messages (not newest) when client buffer full. Metric increment on drop. Aligns with WEBSOCKET_EVENTS.md spec.
- [ ] AC-4: WebSocket enforces `max_connections_per_user = 5` (configurable) in addition to per-tenant 100. 6th connection by same user evicts oldest. Graceful close frame with code 4029.
- [ ] AC-5: WebSocket supports `reconnect` control message (documented in spec but not implemented). Server sends `reconnect` with optional `after_ms` on maintenance; client recognizes and schedules reconnect.
- [ ] AC-6: eSIM Profile Switch triggers CoA/DM for active sessions before re-enabling new profile. If session exists: send DM (disconnect message) via RADIUS/Diameter, wait for ack with timeout, then proceed. Optional bypass flag `force=true` for maintenance.
- [ ] AC-7: Bulk policy assign dispatches CoA for each active session on affected SIMs (post-assign event handler). Batched by 1000, async via job runner. Rollout reuses existing CoA dispatcher from STORY-025.
- [ ] AC-8: Diameter/TLS wired on TCP :3868 (TLS variant). Config var `DIAMETER_TLS_ENABLED`, `DIAMETER_TLS_CERT`, `DIAMETER_TLS_KEY`, `DIAMETER_TLS_CA` for peer mTLS. Falls back to plain TCP when disabled. Interop test with openssl s_client.
- [ ] AC-9: RAT enum canonical alignment. `internal/aaa/rattype` package is the single source of truth. DSL parser (`internal/policy/dsl`) and SoR engine (`internal/operator/sor`) import from this package instead of defining local constants. All aliases (`nb_iot`, `NB_IOT`, `NB-IoT`, `CAT_M1`, `lte_m`, `LTE-M`, `4G`, `LTE`, `5G_SA`, `nr_5g`, `5G_NSA`, `2G`, `3G`) map to canonical constants. Migration of stored `rat_type` column values where inconsistent.

## Dependencies
- Blocked by: STORY-056, STORY-057 (unblocks full testing)
- Blocks: Phase 10 Gate, Documentation Phase (protocol docs must reflect final behavior)

## Test Scenarios
- [ ] Unit: EAP-SIM with simple-SRES-path test case — rejected. Only HMAC MAC accepted.
- [ ] Integration: WebSocket client sends no pong for 95s → connection closed with timeout. 85s with pong → still alive.
- [ ] Integration: Slow WS client — buffer fills, oldest messages dropped, drop counter increments.
- [ ] Integration: Same user opens 6 connections → 1st closed with code 4029.
- [ ] Integration: Server sends `reconnect` → test client schedules reconnection after `after_ms`.
- [ ] Integration: Active session on eSIM + profile switch → DM dispatched, session terminated, new profile enabled.
- [ ] Integration: Bulk policy assign over 100 SIMs with 50 active sessions → 50 CoA requests sent (verified via radclient).
- [ ] Integration: Diameter peer connects via TLS on :3868 → CER/CEA handshake succeeds.
- [ ] Integration: Diameter peer with invalid cert → connection rejected.
- [ ] Unit: DSL `WHEN rat_type == "NB_IOT"` evaluates same as `"nb_iot"` (canonical alignment).
- [ ] Unit: SoR decision for session with `rat_type=5G_NSA` matches canonical.

## Effort Estimate
- Size: XL
- Complexity: High (protocol changes across 3 AAA layers + WS + canonical refactor)

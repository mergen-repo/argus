# STORY-019: Diameter Protocol Server (Gx/Gy)

## User Story
As a platform operator, I want Argus to support the Diameter protocol for policy control (Gx) and online charging (Gy), so that operators using Diameter-based infrastructure can integrate with Argus.

## Description
Implement a Diameter protocol server listening on TCP :3868 per RFC 6733. Support Gx interface (PCRF — policy and charging rules function) for QoS/policy installation and Gy interface (OCS — online charging system) for credit-control. Handle CER/CEA (capabilities exchange), DWR/DWA (watchdog), DPR/DPA (disconnect), CCR/CCA (credit-control), RAR/RAA (re-auth). Integrates with the operator adapter framework for Diameter-based operator forwarding.

## Architecture Reference
- Services: SVC-04 (AAA Engine — internal/aaa/diameter)
- Database Tables: TBL-17 (sessions), TBL-18 (cdrs)
- Packages: internal/aaa/diameter, internal/protocol/diameter
- Source: docs/architecture/services/_index.md (SVC-04), docs/architecture/flows/_index.md

## Screen Reference
- SCR-050: Live Sessions (Diameter sessions alongside RADIUS)
- SCR-041: Operator Detail (Diameter peer status)
- SCR-120: System Health (Diameter server status)

## Acceptance Criteria
- [ ] Diameter server listens on TCP :3868
- [ ] CER/CEA: capabilities exchange with peer, negotiate supported applications (Gx, Gy)
- [ ] DWR/DWA: respond to watchdog requests, detect peer failures
- [ ] DPR/DPA: graceful peer disconnection
- [ ] Gx (CCR-I/CCR-U/CCR-T): install/modify/remove PCC rules (QoS, rate limit)
- [ ] Gy (CCR-I/CCR-U/CCR-T): grant/update/terminate credit for online charging
- [ ] RAR/RAA: push re-authorization to enforce mid-session policy changes
- [ ] AVP encoding/decoding for standard and 3GPP vendor-specific AVPs
- [ ] Diameter session state machine: idle → open → pending → closed
- [ ] Multi-peer support: maintain connections to multiple operator Diameter peers
- [ ] Failover: detect peer down via DWR timeout, mark operator unhealthy
- [ ] Session mapping: Diameter Session-Id ↔ internal session (TBL-17)
- [ ] Diameter events mapped to same NATS topics as RADIUS (session.started, etc.)

## Dependencies
- Blocked by: STORY-015 (RADIUS server — shared session model), STORY-018 (operator adapter)
- Blocks: STORY-021 (operator failover includes Diameter peers), STORY-032 (CDR from Diameter)

## Test Scenarios
- [ ] CER/CEA handshake with mock Diameter peer → connection established
- [ ] DWR/DWA exchange → peer marked healthy
- [ ] DWR timeout → peer marked unhealthy
- [ ] CCR-I (Gx) → PCC rules installed, session created
- [ ] CCR-U (Gy) → credit updated, usage reported
- [ ] CCR-T → session terminated, final accounting recorded
- [ ] RAR sent to NAS → RAA received, policy updated mid-session
- [ ] Unknown application-id → Diameter error DIAMETER_APPLICATION_UNSUPPORTED
- [ ] Malformed AVP → Diameter error DIAMETER_INVALID_AVP_VALUE
- [ ] Concurrent Diameter sessions across multiple peers

## Effort Estimate
- Size: XL
- Complexity: Very High

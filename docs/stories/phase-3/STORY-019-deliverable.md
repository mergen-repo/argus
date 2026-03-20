# STORY-019 Deliverable: Diameter Protocol Server (Gx/Gy)

## Summary

Full Diameter protocol server per RFC 6733 with TCP listener on :3868, Gx (PCRF) and Gy (OCS) interfaces, base protocol (CER/CEA, DWR/DWA, DPR/DPA), credit-control (CCR/CCA), re-authorization (RAR/RAA), AVP encoding/decoding, session state machine, multi-peer support, and failover detection.

## Acceptance Criteria Status

| # | Criteria | Status |
|---|---------|--------|
| 1 | Diameter server listens on TCP :3868 | DONE |
| 2 | CER/CEA capabilities exchange | DONE |
| 3 | DWR/DWA watchdog + peer failure detection | DONE |
| 4 | DPR/DPA graceful peer disconnection | DONE |
| 5 | Gx CCR-I/U/T: PCC rules (QoS, rate limit) | DONE |
| 6 | Gy CCR-I/U/T: credit control | DONE |
| 7 | RAR/RAA mid-session policy changes | DONE |
| 8 | AVP encoding/decoding (standard + 3GPP) | DONE |
| 9 | Diameter session state machine | DONE |
| 10 | Multi-peer support | DONE |
| 11 | Failover: DWR timeout → operator unhealthy | DONE |
| 12 | Session mapping: Session-Id ↔ TBL-17 | DONE |
| 13 | NATS events (same topics as RADIUS) | DONE |

## Files Changed

| File | Change |
|------|--------|
| `internal/aaa/diameter/gx.go` | MODIFIED — nil-check fix in handleTermination |
| `internal/aaa/diameter/gy.go` | MODIFIED — nil-checks in handleInitial/Update/Termination |
| `internal/aaa/diameter/diameter_test.go` | MODIFIED — 20 new tests (Gy flow, Gx flow, RAR, errors, watchdog, multi-peer, lifecycle) |
| `docs/stories/phase-3/STORY-019-plan.md` | NEW — Implementation plan |

## Architecture References Fulfilled

- SVC-04: AAA Engine — Diameter server
- RFC 6733: Diameter base protocol
- ADR-003: Custom Go implementation (no external Diameter libs)
- TBL-17: Session mapping via Session-Id
- Gx (3GPP TS 29.212) + Gy (3GPP TS 32.299)

## Gate Results

- Gate Status: PASS
- Fixes Applied: 0
- Escalated: 0

## Test Coverage

- 53 Diameter tests (33 pre-existing + 20 new)
- All tests pass with -race flag
- Full suite: 30/30 packages pass

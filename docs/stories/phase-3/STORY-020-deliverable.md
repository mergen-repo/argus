# STORY-020 Deliverable: 5G SBA HTTP/2 Proxy (AUSF/UDM)

## Summary

5G SBA HTTP/2 proxy implementing AUSF and UDM interfaces per 3GPP TS 29.509/29.503. Supports 5G-AKA and EAP-AKA' authentication, SUPI/SUCI resolution, network slice (S-NSSAI) authentication, NRF registration placeholder, and session tracking with protocol_type and slice_info.

## Acceptance Criteria Status

| # | Criteria | Status |
|---|---------|--------|
| 1 | HTTP/2 server on :8443 with TLS (mTLS optional) | DONE |
| 2 | AUSF: POST /nausf-auth/v1/ue-authentications | DONE |
| 3 | AUSF: PUT .../5g-aka-confirmation | DONE |
| 4 | UDM: GET /nudm-ueau/v1/{supiOrSuci}/security-information | DONE |
| 5 | UDM: POST /nudm-ueau/v1/{supiOrSuci}/auth-events | DONE |
| 6 | SUPI/SUCI identifier resolution | DONE |
| 7 | Network slice authentication (S-NSSAI) | DONE |
| 8 | 5G-AKA flow | DONE |
| 9 | EAP-AKA' flow via SBA proxy | DONE |
| 10 | Session with protocol_type='5g_sba' and slice_info | DONE |
| 11 | JSON per 3GPP TS 29.509/29.503 | DONE |
| 12 | NRF registration placeholder | DONE |

## Files Changed

| File | Change |
|------|--------|
| `internal/aaa/sba/eap_proxy.go` | NEW — EAP-AKA' proxy handler (177 lines) |
| `internal/aaa/sba/nrf.go` | NEW — NRF registration placeholder (140 lines) |
| `internal/aaa/sba/server.go` | MODIFIED — EAP proxy + NRF routes, Healthy(), ActiveSessionCount() |
| `internal/aaa/sba/ausf.go` | MODIFIED — protocol_type='5g_sba', slice_info in sessions |
| `internal/aaa/sba/udm.go` | MODIFIED — session recording with protocol_type |
| `internal/aaa/sba/server_test.go` | MODIFIED — 7 new tests (NRF, EAP proxy) |
| `internal/aaa/session/session.go` | MODIFIED — ProtocolType + SliceInfo fields, constants |
| `internal/store/session_radius.go` | MODIFIED — protocol_type/slice_info columns |
| `internal/gateway/health.go` | MODIFIED — SBA health checker |
| `cmd/argus/main.go` | MODIFIED — SBA wiring (SBA_ENABLED gate) |
| `internal/config/config.go` | MODIFIED — SBA_ENABLE_MTLS env var |
| `migrations/20260320000006_session_protocol_type.up.sql` | NEW — DB migration |
| `migrations/20260320000006_session_protocol_type.down.sql` | NEW — Rollback |

## Gate Results

- Gate Status: PASS
- Fixes Applied: 3 (mTLS config wiring, .env.example)
- Escalated: 0

## Test Coverage

- 22 SBA tests (AUSF, UDM, slice, EAP proxy, NRF, concurrent)
- Full suite: 31/31 packages pass

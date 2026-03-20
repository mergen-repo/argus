# STORY-016 Deliverable: EAP-SIM/AKA/AKA' Authentication Methods

## Summary

Implemented EAP-SIM (RFC 4186), EAP-AKA (RFC 4187), and EAP-AKA' (RFC 5448) authentication methods with Redis-backed state management, vector caching, SIM-type-based method selection, and full RADIUS server integration including MSK key derivation.

## Acceptance Criteria Status

| # | Criteria | Status |
|---|---------|--------|
| 1 | EAP-SIM: Identity → SIM-Start → SIM-Challenge → SIM-Success | DONE |
| 2 | EAP-AKA: AKA-Identity → AKA-Challenge → AKA-Success | DONE |
| 3 | EAP-AKA': AKA'-Challenge with AT_KDF and AT_KDF_INPUT | DONE |
| 4 | EAP state machine with Redis (30s TTL) | DONE |
| 5 | Challenge vectors from operator adapter (SVC-06) | DONE |
| 6 | Authentication vector caching (batch pre-fetch) | DONE |
| 7 | EAP-Failure on AUTN mismatch / MAC failure | DONE |
| 8 | MSK/EMSK derived and in Access-Accept | DONE |
| 9 | Method selection by SIM type (SIM→EAP-SIM, USIM→AKA') | DONE |
| 10 | Auth method recorded in TBL-17 (eap_method column) | DONE |
| 11 | Pluggable method registry | DONE |

## Files Changed

| File | Change |
|------|--------|
| `internal/aaa/eap/redis_store.go` | NEW — Redis EAP state store (30s TTL, JSON serialization) |
| `internal/aaa/eap/adapter_provider.go` | NEW — Bridge EAP↔Adapter vector interfaces |
| `internal/aaa/eap/vector_cache.go` | NEW — Cached vector provider (Redis LPOP/RPUSH, batch pre-fetch) |
| `internal/aaa/eap/state.go` | MODIFIED — SIM-type method selection, StateSIMStart |
| `internal/aaa/eap/sim.go` | MODIFIED — EAP-SIM Start flow (AT_VERSION_LIST, AT_NONCE_MT) |
| `internal/aaa/radius/server.go` | MODIFIED — EAP integration, Access-Challenge, MS-MPPE keys |
| `internal/aaa/session/session.go` | MODIFIED — AuthMethod field |
| `internal/aaa/eap/redis_store_test.go` | NEW — 5 tests |
| `internal/aaa/eap/adapter_provider_test.go` | NEW — 4 tests |
| `internal/aaa/eap/vector_cache_test.go` | NEW — 4 tests |
| `internal/aaa/eap/eap_test.go` | MODIFIED — 7 new tests (method selection, concurrent, sync failure, MSK) |

## Architecture References Fulfilled

- SVC-04: AAA Engine — EAP authentication methods
- RFC 4186 (EAP-SIM), RFC 4187 (EAP-AKA), RFC 5448 (EAP-AKA')
- TBL-17 sessions: auth_method column

## Gate Results

- Gate Status: PASS
- Fixes Applied: 0
- Escalated: 0

## Test Coverage

- 49 EAP tests (state store, adapter provider, vector cache, method selection, flows)
- Full suite: 31 packages pass

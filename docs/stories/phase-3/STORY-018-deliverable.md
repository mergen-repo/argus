# STORY-018 Deliverable: Pluggable Operator Adapter Framework & Mock Simulator

## Summary

Extended the operator adapter framework with full AAA methods (Authenticate, AccountingUpdate, FetchAuthVectors), enhanced mock simulator with configurable success rate/latency and EAP triplet/quintet generation, upgraded RADIUS and Diameter adapters, and added new SBA adapter for 5G HTTP/2 integration.

## Acceptance Criteria Status

| # | Criteria | Status |
|---|---------|--------|
| 1 | Adapter interface: HealthCheck | DONE (STORY-009) |
| 2 | Adapter interface: Authenticate, AccountingUpdate, FetchAuthVectors | DONE |
| 3 | Adapter registry: register by protocol type | DONE (STORY-009) + SBA added |
| 4 | Adapter selection via Registry.GetOrCreate | DONE (STORY-009) |
| 5 | Mock adapter: configurable success_rate, latency_ms, error_type | DONE |
| 6 | Mock adapter: EAP triplets/quintets for test IMSIs | DONE |
| 7 | Mock adapter: accounting acceptance simulation | DONE |
| 8 | RADIUS adapter: forwards Access-Request | DONE |
| 9 | Diameter adapter: CER/CEA, DWR/DWA | DONE |
| 10 | API-024 test connection via adapter | DONE (STORY-009) |
| 11 | Adapter timeout: configurable per operator (default 3s) | DONE |
| 12 | Adapter error wrapping: operator_id + protocol | DONE |

## Files Changed

| File | Change |
|------|--------|
| `internal/operator/adapter/types.go` | MODIFIED — Extended Adapter interface with 3 AAA methods, new request/response types |
| `internal/operator/adapter/mock.go` | MODIFIED — Full mock: success_rate, latency, EAP vector generation (SHA256 seeded) |
| `internal/operator/adapter/radius.go` | MODIFIED — Real Access-Request forwarding, AccountingUpdate |
| `internal/operator/adapter/diameter.go` | MODIFIED — CER/CEA handshake, DWR/DWA watchdog (30s interval) |
| `internal/operator/adapter/sba.go` | NEW — SBA adapter: HTTP/2 client, AUSF/UDM endpoints |
| `internal/operator/adapter/registry.go` | MODIFIED — SBA factory registered |
| `internal/operator/router.go` | MODIFIED — 3 new methods with circuit breaker + error wrapping |
| `internal/operator/adapter/mock_test.go` | MODIFIED — 23 tests with -race flag |
| `internal/operator/adapter/registry_test.go` | MODIFIED — SBA factory test |
| `internal/operator/router_test.go` | MODIFIED — Updated for new interface |

## Architecture References Fulfilled

- SVC-06: Operator Router — full AAA forwarding capability
- TBL-05: operators — protocol config drives adapter selection
- 4 adapter types: mock, radius, diameter, sba

## Gate Results

- Gate Status: PASS
- Fixes Applied: 0
- Escalated: 0

## Test Coverage

- 23 adapter tests (mock authenticate, accounting, vector gen, timeout, concurrent)
- 40 operator package tests
- All tests pass with -race flag

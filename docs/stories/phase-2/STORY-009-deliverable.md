# Deliverable: STORY-009 — Operator CRUD & Health Check

## Summary
Implemented operator management (CRUD, grants, health monitoring) with AES-256-GCM encrypted adapter config, pluggable adapter registry, background health checks with circuit breaker integration, and Redis-cached health status.

## Files Changed

### New Files
- `internal/store/operator.go` — OperatorStore: Create, GetByID, List, Update, CreateGrant, ListGrants, DeleteGrant, InsertHealthLog
- `internal/crypto/aes.go` — AES-256-GCM encryption/decryption for adapter_config at rest
- `internal/api/operator/handler.go` — HTTP handlers: List (API-020), Create (API-021), Update (API-022), GetHealth (API-023), TestConnection (API-024), CreateGrant (API-025), ListGrants (API-026), DeleteGrant (API-027)
- `internal/operator/health.go` — Background health checker with per-operator goroutines, circuit breaker, Redis cache
- `internal/store/operator_test.go` — Store struct and error tests
- `internal/crypto/aes_test.go` — Encrypt/decrypt roundtrip, tampered data, empty key passthrough
- `internal/api/operator/handler_test.go` — Validation, response conversion, negative test cases
- `internal/operator/health_test.go` — Health status logic, circuit breaker integration

### Modified Files
- `internal/store/stubs.go` — Removed old Operator/OperatorStore stubs
- `internal/config/config.go` — Added EncryptionKey env var
- `internal/gateway/router.go` — Added operator route groups (super_admin, operator_manager, api_user)
- `cmd/argus/main.go` — Wired operator store, adapter registry, handler, health checker

## Architecture References Fulfilled
- API-020 to API-027: All 8 operator endpoints implemented
- TBL-05 (operators), TBL-06 (operator_grants), TBL-23 (operator_health_logs) fully utilized
- SVC-06: Multi-operator routing with pluggable adapter registry
- ALGORITHMS.md: Circuit breaker integration for operator health

## Test Coverage
- 32 unit tests across 4 packages
- Crypto: roundtrip, tampered data, empty key passthrough
- Store: struct validation, error sentinels
- Handler: validation (create/update/grant), invalid IDs, response structure, negative cases
- Health: status logic, circuit breaker, struct tests

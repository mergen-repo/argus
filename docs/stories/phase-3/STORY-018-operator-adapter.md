# STORY-018: Pluggable Operator Adapter Framework & Mock Simulator

## User Story
As a platform operator, I want a pluggable adapter framework for operator connectivity, so that Argus can forward AAA requests to different operators via RADIUS, Diameter, or custom protocols through a uniform interface.

## Description
Define the Adapter interface in internal/operator/adapter with methods for authentication vector fetch, RADIUS forwarding, Diameter forwarding, and health check. Implement a mock simulator adapter in internal/operator/mock that simulates operator responses (configurable success/failure rates, latency) for development and testing. The adapter registry selects the correct adapter based on operator protocol configuration (TBL-05).

> **Note (post-STORY-009):** STORY-009 already implemented the adapter registry (`internal/operator/adapter/registry.go`), Adapter interface (`types.go`), mock/radius/diameter adapter stubs, `GetOrCreate`/`HasFactory` pattern, health check via adapter, and API-024 test connection. This story should focus on **extending** the existing adapters with full AAA methods (Authenticate, AccountingUpdate, FetchAuthVectors) and enhancing the mock simulator with EAP triplet/quintet generation. Effort may be reduced from L to M.

## Architecture Reference
- Services: SVC-06 (Operator Router — internal/operator/adapter)
- Database Tables: TBL-05 (operators — protocol, endpoint config)
- Packages: internal/operator/adapter, internal/operator/mock, internal/operator/radius, internal/operator/diameter
- Source: docs/architecture/services/_index.md (SVC-06)

## Screen Reference
- SCR-041: Operator Detail — connection test, protocol type display
- SCR-040: Operator List — health status per operator

## Acceptance Criteria
- [x] Adapter interface defined: `HealthCheck(ctx) error` — done in STORY-009. Extend with `Authenticate(ctx, req) (resp, error)`, `AccountingUpdate(ctx, req) error`, `FetchAuthVectors(ctx, imsi, count) ([]Vector, error)`
- [x] Adapter registry: register adapters by protocol type (radius, diameter, mock) — done in STORY-009. Add `sba` factory.
- [x] Adapter selection: SVC-06 picks adapter via `Registry.GetOrCreate` — done in STORY-009
- [ ] Mock adapter: configurable success_rate (0-100%), latency_ms, error_type — extend existing mock
- [ ] Mock adapter: returns valid EAP triplets/quintets for test IMSIs
- [ ] Mock adapter: simulates accounting acceptance
- [ ] RADIUS adapter: forwards Access-Request to operator RADIUS endpoint (upgrade from stub)
- [ ] Diameter adapter: forwards CER/CEA, DWR/DWA to operator Diameter peer (upgrade from stub)
- [x] POST /api/v1/operators/:id/test (API-024) triggers health check via adapter — done in STORY-009
- [ ] Adapter timeout: configurable per operator (default 3s)
- [ ] Adapter error wrapping: all adapter errors include operator_id and protocol type
- [x] Thread-safe: adapters are safe for concurrent use (Registry uses sync.RWMutex) — done in STORY-009

## Dependencies
- Blocked by: STORY-009 (operator CRUD), STORY-015 (RADIUS server)
- Blocks: STORY-016 (EAP vector fetch), STORY-019 (Diameter server), STORY-021 (operator failover)

## Test Scenarios
- [ ] Mock adapter returns Access-Accept with configured success rate
- [ ] Mock adapter returns Access-Reject when success_rate < random threshold
- [ ] Mock adapter health check always succeeds
- [ ] Adapter registry returns correct adapter for protocol="radius"
- [ ] Adapter registry returns mock adapter for protocol="mock"
- [ ] Unknown protocol type → error UNSUPPORTED_PROTOCOL
- [ ] Adapter timeout → error with ErrAdapterTimeout
- [ ] Concurrent adapter calls → no race conditions (go test -race)
- [ ] API-024 test connection → adapter.HealthCheck called, result returned

## Effort Estimate
- Size: L
- Complexity: Medium

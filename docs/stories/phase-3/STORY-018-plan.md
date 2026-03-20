# Implementation Plan: STORY-018 - Pluggable Operator Adapter Framework & Mock Simulator

## Goal

Extend the existing adapter framework with full AAA methods (Authenticate, AccountingUpdate, FetchAuthVectors), enhance the mock simulator with EAP triplet/quintet generation, upgrade RADIUS/Diameter adapter stubs to real forwarding, register SBA factory, and add configurable adapter timeout with proper error wrapping.

## Architecture Context

### Components Involved

- **SVC-06 (Operator Router)**: `internal/operator/` — Routes AAA requests to correct operator adapter. Already has `OperatorRouter` with circuit breaker, failover, `ForwardAuth`, `ForwardAcct`, `SendCoA`, `SendDM` methods.
- **Adapter Registry**: `internal/operator/adapter/registry.go` — Thread-safe registry with `GetOrCreate`, `HasFactory`, `RegisterFactory`. Currently registers `mock`, `radius`, `diameter` factories.
- **Adapter Interface**: `internal/operator/adapter/types.go` — Defines `Adapter` interface: `Type()`, `HealthCheck()`, `ForwardAuth()`, `ForwardAcct()`, `SendCoA()`, `SendDM()`. Also defines request/response types, error types, and `WrapError` helper.
- **Mock Adapter**: `internal/operator/adapter/mock.go` — Configurable success_rate, latency_ms, fail_rate, error_type. Implements all Adapter methods with simulated latency and probabilistic success/failure.
- **RADIUS Adapter**: `internal/operator/adapter/radius.go` — Real UDP packet forwarding for auth, acct, CoA, DM. Builds/parses RADIUS packets manually.
- **Diameter Adapter**: `internal/operator/adapter/diameter.go` — TCP connection with CCR/CCA, RAR/RAA forwarding. Uses `internal/aaa/diameter` package for message encoding/decoding.
- **EAP Package**: `internal/aaa/eap/` — `AuthVectorProvider` interface with `GetSIMTriplets` and `GetAKAQuintets`. `MockVectorProvider` generates deterministic vectors from IMSI.

### Existing Adapter Interface (from types.go)

```go
type Adapter interface {
    Type() string
    HealthCheck(ctx context.Context) HealthResult
    ForwardAuth(ctx context.Context, req AuthRequest) (*AuthResponse, error)
    ForwardAcct(ctx context.Context, req AcctRequest) error
    SendCoA(ctx context.Context, req CoARequest) error
    SendDM(ctx context.Context, req DMRequest) error
}
```

### What Needs to Be Extended

The story requires adding these NEW methods to the Adapter interface:

1. **`Authenticate(ctx, AuthenticateRequest) (*AuthenticateResponse, error)`** — Higher-level auth that includes EAP vector fetch + auth decision. Different from `ForwardAuth` which is raw protocol forwarding.
2. **`AccountingUpdate(ctx, AccountingUpdateRequest) error`** — Higher-level accounting that wraps `ForwardAcct` with additional metadata.
3. **`FetchAuthVectors(ctx, imsi string, count int) ([]AuthVector, error)`** — Fetch EAP SIM triplets or AKA quintets from the operator.

### New Types to Add (types.go)

```go
type AuthenticateRequest struct {
    IMSI       string
    MSISDN     string
    APN        string
    RATType    string
    VisitedPLMN string
}

type AuthenticateResponse struct {
    Success    bool
    Code       string
    SessionID  string
    Attributes map[string]interface{}
}

type AccountingUpdateRequest struct {
    IMSI         string
    SessionID    string
    StatusType   string
    InputOctets  uint64
    OutputOctets uint64
    SessionTime  int
    RATType      string
}

type AuthVector struct {
    Type     string  // "triplet" or "quintet"
    RAND     []byte
    SRES     []byte  // triplet only
    Kc       []byte  // triplet only
    AUTN     []byte  // quintet only
    XRES     []byte  // quintet only
    CK       []byte  // quintet only
    IK       []byte  // quintet only
}
```

### Data Flow

```
AAA Engine → OperatorRouter.Authenticate(ctx, operatorID, req)
    → registry.Get(operatorID) → adapter
    → circuitBreaker.ShouldAllow()
    → adapter.Authenticate(ctx, req)
        [mock] → simulateLatency → shouldSucceed → return simulated response
        [radius] → build Access-Request → UDP send → parse response
        [diameter] → build CCR → TCP send → parse CCA
        [sba] → HTTP/2 POST → parse JSON response
    → recordResult(cb, err)
    → WrapError if error
```

### SBA Adapter Factory

A new `sba` factory must be registered in the `Registry.NewRegistry()`. The SBA adapter will forward auth requests via HTTP/2 to 5G core network functions. For now, it creates a stub adapter similar to the existing pattern.

### Adapter Timeout

Each adapter already has `TimeoutMs` in its config. The story requires:
- Default timeout of 3000ms (3s) — already the default for RADIUS and Diameter configs
- Mock adapter should also respect `timeout_ms` config for context deadlines
- Timeout errors must be wrapped with `WrapError(operatorID, protocolType, ErrAdapterTimeout)`

### Error Wrapping

The `AdapterError` struct and `WrapError` helper already exist in `types.go`. The story requires ensuring ALL adapter errors are consistently wrapped with `operator_id` and `protocol_type`. The `OperatorRouter` methods already call `WrapError` — but the individual adapters should also ensure proper error context.

## Prerequisites

- [x] STORY-009 completed (operator CRUD, adapter registry, health check, API-024)
- [x] STORY-015 completed (RADIUS server — `internal/aaa/` packages exist)
- [x] EAP package exists with `MockVectorProvider`, `SIMTriplets`, `AKAQuintets`
- [x] Diameter package exists with message encoding/decoding

## Tasks

### Task 1: Extend Adapter interface with new AAA methods and types

- **Files:** Modify `internal/operator/adapter/types.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/operator/adapter/types.go` — follow existing type/interface pattern
- **Context refs:** Architecture Context > What Needs to Be Extended, Architecture Context > New Types to Add
- **What:**
  - Add `AuthenticateRequest`, `AuthenticateResponse`, `AccountingUpdateRequest`, `AuthVector` types
  - Extend `Adapter` interface with 3 new methods: `Authenticate(ctx, AuthenticateRequest) (*AuthenticateResponse, error)`, `AccountingUpdate(ctx, AccountingUpdateRequest) error`, `FetchAuthVectors(ctx, imsi string, count int) ([]AuthVector, error)`
  - AuthVector.Type should be "triplet" or "quintet"
  - Keep all existing methods unchanged — this is additive
- **Verify:** `go build ./internal/operator/adapter/...` — will fail until all adapters implement new methods (expected)

### Task 2: Implement new AAA methods in Mock adapter with EAP vector generation

- **Files:** Modify `internal/operator/adapter/mock.go`
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** Read `internal/operator/adapter/mock.go` — follow existing mock pattern; Read `internal/aaa/eap/mock_provider.go` — follow deterministic vector generation
- **Context refs:** Architecture Context > Components Involved, Architecture Context > New Types to Add, Architecture Context > Existing Adapter Interface
- **What:**
  - Implement `Authenticate(ctx, AuthenticateRequest)`: simulate latency, use shouldSucceed(), return AuthenticateResponse with success/reject. On success, include a generated SessionID.
  - Implement `AccountingUpdate(ctx, AccountingUpdateRequest)`: simulate latency, use shouldSucceed(), return nil or AdapterError.
  - Implement `FetchAuthVectors(ctx, imsi string, count int)`: generate deterministic EAP triplets and quintets using same SHA256-seed approach as `internal/aaa/eap/mock_provider.go`. Return `count` vectors — odd-indexed as triplets, even-indexed as quintets (or all triplets if count <= 3). Use `crypto/sha256` for deterministic generation from IMSI seed.
  - The mock adapter should generate vectors inline (not import the eap package to avoid circular deps) using the same deterministic algorithm: `sha256(seed + index)` truncated to required length.
  - Vector generation: RAND=16 bytes, SRES=4 bytes, Kc=8 bytes for triplets; RAND=16, AUTN=16, XRES=8, CK=16, IK=16 bytes for quintets.
- **Verify:** `go build ./internal/operator/adapter/...`

### Task 3: Implement new AAA methods in RADIUS adapter

- **Files:** Modify `internal/operator/adapter/radius.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/operator/adapter/radius.go` — follow existing ForwardAuth/ForwardAcct patterns
- **Context refs:** Architecture Context > Components Involved, Architecture Context > Existing Adapter Interface, Architecture Context > Adapter Timeout
- **What:**
  - Implement `Authenticate(ctx, AuthenticateRequest)`: Build an Access-Request from AuthenticateRequest fields (IMSI as User-Name, NAS-Identifier from config host), send via UDP, parse response. Return AuthenticateResponse with Success=true for Access-Accept, Success=false for Access-Reject.
  - Implement `AccountingUpdate(ctx, AccountingUpdateRequest)`: Build Accounting-Request from AccountingUpdateRequest, forward via UDP to acct port. Map StatusType to Acct-Status-Type attribute.
  - Implement `FetchAuthVectors(ctx, imsi, count)`: RADIUS does not natively support vector fetch — return `fmt.Errorf("%w: RADIUS does not support direct vector fetch", ErrUnsupportedProtocol)`.
  - Use existing `buildRADIUSAccessRequest` and `buildRADIUSAcctRequest` helpers where possible, or create AuthenticateRequest-specific builders.
- **Verify:** `go build ./internal/operator/adapter/...`

### Task 4: Implement new AAA methods in Diameter adapter

- **Files:** Modify `internal/operator/adapter/diameter.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/operator/adapter/diameter.go` — follow existing ForwardAuth/ForwardAcct patterns
- **Context refs:** Architecture Context > Components Involved, Architecture Context > Existing Adapter Interface, Architecture Context > Adapter Timeout
- **What:**
  - Implement `Authenticate(ctx, AuthenticateRequest)`: Build a CCR-Initial (like existing ForwardAuth), send via TCP, parse CCA. Map Diameter result codes to AuthenticateResponse.Success.
  - Implement `AccountingUpdate(ctx, AccountingUpdateRequest)`: Build CCR with appropriate CC-Request-Type based on StatusType (start→Initial, interim→Update, stop→Termination). Forward via TCP. Similar to existing ForwardAcct but uses AccountingUpdateRequest.
  - Implement `FetchAuthVectors(ctx, imsi, count)`: Diameter does not natively support vector fetch — return `fmt.Errorf("%w: Diameter does not support direct vector fetch", ErrUnsupportedProtocol)`.
  - Ensure CER/CEA handshake happens on first connection (add `performCER` method that sends CER and reads CEA during `getConnection` if not already done). Track `cerDone bool` field.
  - Add DWR/DWA keepalive: `startWatchdog` goroutine that sends DWR periodically (every 30s) when connection is alive. Handle DWA response.
- **Verify:** `go build ./internal/operator/adapter/...`

### Task 5: Add SBA adapter stub and register factory

- **Files:** Create `internal/operator/adapter/sba.go`, Modify `internal/operator/adapter/registry.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/operator/adapter/mock.go` — follow adapter creation pattern; Read `internal/operator/adapter/registry.go` — follow factory registration
- **Context refs:** Architecture Context > SBA Adapter Factory, Architecture Context > Components Involved
- **What:**
  - Create `SBAConfig` struct: `Host string`, `Port int`, `TLSEnabled bool`, `TimeoutMs int`, `NfInstanceID string`
  - Create `SBAAdapter` struct implementing all Adapter interface methods
  - `NewSBAAdapter(raw json.RawMessage)` — parse config, validate host, default port=8443, default timeout=3000ms
  - `Type()` returns `"sba"`
  - `HealthCheck` — HTTP GET to `{host}:{port}/health` with configured timeout
  - `ForwardAuth` — HTTP POST to NF endpoint, similar pattern to Diameter adapter but over HTTP/2
  - `ForwardAcct` — HTTP POST for accounting
  - `SendCoA`, `SendDM` — return ErrUnsupportedProtocol (SBA doesn't use CoA/DM)
  - `Authenticate` — HTTP POST to AUSF endpoint, return AuthenticateResponse
  - `AccountingUpdate` — HTTP POST for accounting update
  - `FetchAuthVectors` — HTTP GET to UDM endpoint for auth vectors
  - Register `"sba"` factory in `NewRegistry()` and in `NewAdapter()` switch statement
- **Verify:** `go build ./internal/operator/adapter/...`

### Task 6: Add OperatorRouter methods for new AAA operations

- **Files:** Modify `internal/operator/router.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/operator/router.go` — follow existing ForwardAuth/ForwardAcct pattern
- **Context refs:** Architecture Context > Data Flow, Architecture Context > Error Wrapping
- **What:**
  - Add `Authenticate(ctx, operatorID, AuthenticateRequest)` method: resolve adapter + circuit breaker, call `adapter.Authenticate()`, record result, wrap errors. Follow exact same pattern as `ForwardAuth`.
  - Add `AccountingUpdate(ctx, operatorID, AccountingUpdateRequest)` method: same pattern as `ForwardAcct`.
  - Add `FetchAuthVectors(ctx, operatorID, imsi, count)` method: same pattern, return `[]AuthVector`.
  - All methods use `resolveWithCircuitBreaker` and `recordResult` like existing methods.
- **Verify:** `go build ./internal/operator/...`

### Task 7: Comprehensive tests for adapter extensions

- **Files:** Modify `internal/operator/adapter/registry_test.go`, Create `internal/operator/adapter/mock_test.go`
- **Depends on:** Task 2, Task 3, Task 4, Task 5, Task 6
- **Complexity:** high
- **Pattern ref:** Read `internal/operator/adapter/registry_test.go` — follow existing test pattern
- **Context refs:** Architecture Context > Components Involved, Architecture Context > New Types to Add
- **What:**
  - **mock_test.go:**
    - `TestMockAuthenticateSuccess` — 100% success_rate, verify AuthenticateResponse.Success=true
    - `TestMockAuthenticateReject` — 0% success_rate, verify AuthenticateResponse.Success=false
    - `TestMockAuthenticateConfigurableRate` — 50% rate, run 100 times, verify approximately 50% success (within margin)
    - `TestMockAccountingUpdateSuccess` — verify no error returned
    - `TestMockAccountingUpdateReject` — 0% success_rate, verify error returned
    - `TestMockFetchAuthVectorsTriplets` — count=3, verify all triplet type with correct field sizes
    - `TestMockFetchAuthVectorsQuintets` — count=2, verify quintet fields
    - `TestMockFetchAuthVectorsDeterministic` — same IMSI returns same vectors
    - `TestMockHealthCheckAlwaysSucceeds` — with default config, health check succeeds
    - `TestMockAdapterTimeout` — use context with very short deadline, verify timeout
    - `TestMockAdapterConcurrent` — go test -race: concurrent Authenticate + FetchAuthVectors calls
  - **registry_test.go additions:**
    - `TestRegistryCreateSBAAdapter` — verify sba factory exists and creates adapter with type "sba"
    - `TestRegistryHasFactorySBA` — verify HasFactory("sba") returns true
  - Run with `go test -race ./internal/operator/adapter/...`
- **Verify:** `go test -v -race ./internal/operator/adapter/...`

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| Extend Adapter interface with Authenticate, AccountingUpdate, FetchAuthVectors | Task 1 | Task 7 (build verification) |
| Add sba factory to registry | Task 5 | Task 7 (TestRegistryCreateSBAAdapter) |
| Mock adapter: configurable success_rate, latency_ms, error_type | Task 2 (extend existing) | Task 7 (TestMockAuthenticateConfigurableRate) |
| Mock adapter: returns valid EAP triplets/quintets | Task 2 | Task 7 (TestMockFetchAuthVectors*) |
| Mock adapter: simulates accounting acceptance | Task 2 | Task 7 (TestMockAccountingUpdate*) |
| RADIUS adapter: forwards Access-Request (upgrade) | Task 3 | Task 7 (build verification) |
| Diameter adapter: CER/CEA, DWR/DWA forwarding | Task 4 | Task 7 (build verification) |
| Adapter timeout: configurable per operator (default 3s) | Task 2, 3, 4, 5 | Task 7 (TestMockAdapterTimeout) |
| Adapter error wrapping: all errors include operator_id, protocol type | Task 6 | Task 7 (verified via WrapError in router) |
| Thread-safe: adapters safe for concurrent use | Existing (STORY-009) | Task 7 (TestMockAdapterConcurrent) |

## Story-Specific Compliance Rules

- **Architecture:** All code stays in `internal/operator/adapter/` (adapter implementations) and `internal/operator/` (router). No cross-layer imports.
- **Interface:** New methods are additive — existing Adapter interface methods remain unchanged.
- **Error handling:** All adapter errors must be wrapped via `WrapError(operatorID, protocolType, err)` at the router level.
- **Concurrency:** All adapters must be safe for concurrent use. Mock uses `sync.Mutex`, RADIUS uses `sync.RWMutex`, Diameter uses `sync.Mutex`.
- **No circular deps:** Mock adapter must NOT import `internal/aaa/eap` — duplicate the deterministic vector generation logic locally.
- **ADR-003:** Custom Go AAA engine — adapter framework aligns with custom implementation decision.

## Risks & Mitigations

- **Interface breaking change**: Adding methods to an existing interface breaks all implementors. Mitigation: all adapters are updated in the same story, all in the same package. Tasks 2-5 implement the new methods before Task 7 verifies everything compiles.
- **Circular dependency**: Mock adapter needing EAP types. Mitigation: duplicate the simple SHA256-based vector generation in mock.go rather than importing eap package.
- **CER/CEA complexity**: Diameter peer connection management is non-trivial. Mitigation: keep CER/CEA as a best-effort handshake; if it fails, the connection is still usable for message forwarding (some Diameter peers don't require CER).

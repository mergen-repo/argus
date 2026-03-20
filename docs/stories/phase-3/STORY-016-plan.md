# Implementation Plan: STORY-016 - EAP-SIM/AKA/AKA' Authentication Methods

## Goal

Enhance the existing EAP scaffold to production quality: add Redis-backed state store with 30s TTL, integrate operator adapter for auth vector fetching, implement vector caching/pre-fetch, add EAP-SIM Start subtype (version negotiation), wire EAP into the RADIUS server's Access-Request handler, record auth method in session records, and add SIM-type-based method selection.

## Architecture Context

### Components Involved

- **SVC-04 AAA Engine / EAP**: `internal/aaa/eap/` — EAP packet codec, state machine, method handlers (SIM, AKA, AKA'). Scaffold exists with MemoryStateStore, MockVectorProvider, full encode/decode, method registration.
- **SVC-04 AAA Engine / RADIUS**: `internal/aaa/radius/server.go` — RADIUS server handles Access-Request/Accept/Reject. Currently does SIM lookup + direct accept, no EAP integration.
- **SVC-06 Operator Adapter**: `internal/operator/adapter/` — `Adapter.FetchAuthVectors(ctx, imsi, count)` returns `[]AuthVector` (triplet/quintet). Mock adapter generates deterministic vectors.
- **Redis Cache**: `internal/cache/redis.go` — Redis client wrapper. Used by SIMCache pattern in `internal/aaa/radius/sim_cache.go`.
- **Session Manager**: `internal/aaa/session/session.go` — Manages RADIUS sessions. Session struct has no EAP method field currently. DB has `auth_method VARCHAR(20)` column in `sessions` table.
- **Store**: `internal/store/session_radius.go` — `RadiusSession` struct has `AuthMethod *string` field. `CreateRadiusSessionParams` has `AuthMethod *string`.

### Data Flow

```
RADIUS Access-Request (with EAP-Message attribute)
  → radius.Server.handleAuth()
  → Detect EAP-Message → delegate to EAP StateMachine
  → StateMachine.ProcessPacket()
    → Identity phase: extract IMSI, select method based on SIM type
    → Challenge phase: fetch vectors from operator adapter (via bridge)
    → Response phase: verify challenge response
  → If EAP-Success: Access-Accept + MS-MPPE keys (MSK)
  → If EAP-Challenge: Access-Challenge + EAP-Message + State
  → If EAP-Failure: Access-Reject
  → Record eap_method in session on success
```

### Existing EAP Code Analysis

**Already implemented (scaffold):**
- `eap.go`: Packet struct, Decode/Encode, NewRequest/Response/Success/Failure/NAK/Identity helpers
- `state.go`: StateMachine with ProcessPacket, handleIdentity/handleMethodNegotiation/handleChallenge, selectMethod, MemoryStateStore, EAPSession/SIMChallengeData/AKAChallengeData types, AuthVectorProvider/StateStore/MethodHandler interfaces
- `sim.go`: SIMHandler — StartChallenge (fetches triplets, derives MSK), HandleResponse (verifies MAC/SRES), buildSIMChallengeRequest, deriveSIMMSK
- `aka.go`: AKAHandler — StartChallenge (fetches quintets, derives MSK), HandleResponse (verifies RES/XRES, handles sync failure), buildChallengeRequest (with AKA' KDF/KDFInput attributes), deriveAKAMSK
- `mock_provider.go`: MockVectorProvider (deterministic), RandomVectorProvider
- `eap_test.go`: Full flow tests for SIM, AKA, AKA', auth failure, timeout, NAK negotiation

**Gaps to fill:**
1. RedisStateStore (production state persistence with TTL)
2. Operator adapter bridge (AuthVectorProvider → adapter.Adapter.FetchAuthVectors)
3. Auth vector caching (pre-fetch batch, cache in Redis)
4. EAP-SIM Start subtype handling (version negotiation per RFC 4186)
5. RADIUS server EAP integration (Access-Challenge cycle)
6. Session auth_method recording
7. SIM-type-based method selection

### Database Schema

```sql
-- Source: migrations/20260320000002_core_schema.up.sql (ACTUAL)
-- sessions table already has auth_method column
CREATE TABLE IF NOT EXISTS sessions (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    sim_id UUID NOT NULL,
    tenant_id UUID NOT NULL,
    operator_id UUID NOT NULL,
    apn_id UUID,
    nas_ip INET,
    framed_ip INET,
    calling_station_id VARCHAR(50),
    called_station_id VARCHAR(100),
    rat_type VARCHAR(10),
    session_state VARCHAR(20) NOT NULL DEFAULT 'active',
    auth_method VARCHAR(20),           -- ← EAP method recorded here
    policy_version_id UUID,
    acct_session_id VARCHAR(100),
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at TIMESTAMPTZ,
    terminate_cause VARCHAR(50),
    bytes_in BIGINT NOT NULL DEFAULT 0,
    bytes_out BIGINT NOT NULL DEFAULT 0,
    packets_in BIGINT NOT NULL DEFAULT 0,
    packets_out BIGINT NOT NULL DEFAULT 0,
    last_interim_at TIMESTAMPTZ
);
```

No new migration needed — `auth_method` column already exists.

### Store Model

```go
// Source: internal/store/session_radius.go (ACTUAL)
type RadiusSession struct {
    // ... other fields ...
    AuthMethod *string `json:"auth_method"`
}

type CreateRadiusSessionParams struct {
    // ... other fields ...
    AuthMethod *string
}
```

### Adapter AuthVector Type

```go
// Source: internal/operator/adapter/types.go (ACTUAL)
type AuthVector struct {
    Type string   // "triplet" or "quintet"
    RAND []byte
    SRES []byte   // triplet only
    Kc   []byte   // triplet only
    AUTN []byte   // quintet only
    XRES []byte   // quintet only
    CK   []byte   // quintet only
    IK   []byte   // quintet only
}
```

### EAP State Machine Interfaces

```go
// Source: internal/aaa/eap/state.go (ACTUAL)
type AuthVectorProvider interface {
    GetSIMTriplets(ctx context.Context, imsi string) (*SIMTriplets, error)
    GetAKAQuintets(ctx context.Context, imsi string) (*AKAQuintets, error)
}

type StateStore interface {
    Save(ctx context.Context, session *EAPSession) error
    Get(ctx context.Context, id string) (*EAPSession, error)
    Delete(ctx context.Context, id string) error
}

type MethodHandler interface {
    Type() MethodType
    HandleResponse(ctx context.Context, session *EAPSession, pkt *Packet) (*Packet, error)
    StartChallenge(ctx context.Context, session *EAPSession, provider AuthVectorProvider) (*Packet, error)
}
```

### Redis Cache Pattern

```go
// Source: internal/aaa/radius/sim_cache.go (ACTUAL)
// Pattern: Redis GET with JSON unmarshal, fallback to DB, SET with TTL
const simIMSICachePrefix = "sim:imsi:"
const simCacheTTL = 5 * time.Minute

type SIMCache struct {
    redis    *redis.Client
    simStore *store.SIMStore
    logger   zerolog.Logger
}
```

### SIM Store Model

```go
// Source: internal/store/sim.go (referenced by sim_cache.go)
// SIM has SimType field used for method selection
type SIM struct {
    // ... ID, TenantID, OperatorID, ICCID, IMSI, etc.
    SimType string // "sim", "usim", "esim", "isim"
}
```

## Prerequisites

- [x] STORY-015 completed (RADIUS server with Access-Request/Accept/Reject handling)
- [x] STORY-018 completed (operator adapter with FetchAuthVectors)
- [x] EAP scaffold exists (state machine, method handlers, tests)
- [x] Redis cache infrastructure exists
- [x] sessions table has auth_method column

## Tasks

### Task 1: Redis-backed EAP StateStore

- **Files:** Create `internal/aaa/eap/redis_store.go`
- **Depends on:** — (none)
- **Complexity:** medium
- **Pattern ref:** Read `internal/aaa/radius/sim_cache.go` — follow same Redis GET/SET/DEL pattern with JSON marshal
- **Context refs:** Architecture Context > EAP State Machine Interfaces, Architecture Context > Redis Cache Pattern, Architecture Context > Existing EAP Code Analysis
- **What:**
  - Implement `RedisStateStore` that satisfies the `StateStore` interface
  - Key format: `eap:session:{sessionID}`
  - TTL: 30 seconds (matching `DefaultStateTTL` in state.go)
  - `Save`: JSON marshal `EAPSession`, SET with 30s TTL
  - `Get`: GET key, JSON unmarshal, return nil if not found (redis.Nil)
  - `Delete`: DEL key
  - Constructor: `NewRedisStateStore(client *redis.Client, logger zerolog.Logger) *RedisStateStore`
  - Handle redis.Nil as "not found" (return nil, nil) — same pattern as SIMCache
- **Verify:** `cd /Users/btopcu/workspace/argus && go build ./internal/aaa/eap/...`

### Task 2: Operator adapter bridge for auth vector fetching

- **Files:** Create `internal/aaa/eap/adapter_provider.go`
- **Depends on:** — (none)
- **Complexity:** medium
- **Pattern ref:** Read `internal/aaa/eap/mock_provider.go` — follow same AuthVectorProvider interface implementation
- **Context refs:** Architecture Context > EAP State Machine Interfaces, Architecture Context > Adapter AuthVector Type, Architecture Context > Existing EAP Code Analysis
- **What:**
  - Implement `AdapterVectorProvider` that satisfies `AuthVectorProvider` interface
  - Constructor: `NewAdapterVectorProvider(adapter adapter.Adapter, logger zerolog.Logger) *AdapterVectorProvider`
  - `GetSIMTriplets`: Call `adapter.FetchAuthVectors(ctx, imsi, 3)`, filter for triplet vectors, convert `[]adapter.AuthVector` → `*SIMTriplets` (copy RAND/SRES/Kc byte slices into fixed arrays)
  - `GetAKAQuintets`: Call `adapter.FetchAuthVectors(ctx, imsi, 1)`, filter for quintet vectors, convert to `*AKAQuintets` (copy RAND/AUTN/XRES/CK/IK)
  - Error handling: return wrapped errors if adapter returns error or insufficient vectors
  - Import path: `github.com/btopcu/argus/internal/operator/adapter`
- **Verify:** `cd /Users/btopcu/workspace/argus && go build ./internal/aaa/eap/...`

### Task 3: Auth vector caching with Redis pre-fetch

- **Files:** Create `internal/aaa/eap/vector_cache.go`
- **Depends on:** Task 2
- **Complexity:** high
- **Pattern ref:** Read `internal/aaa/radius/sim_cache.go` — follow Redis cache-aside pattern
- **Context refs:** Architecture Context > Redis Cache Pattern, Architecture Context > Adapter AuthVector Type, Architecture Context > EAP State Machine Interfaces
- **What:**
  - Implement `CachedVectorProvider` wrapping `AuthVectorProvider` with Redis cache
  - Constructor: `NewCachedVectorProvider(inner AuthVectorProvider, redis *redis.Client, logger zerolog.Logger, opts ...CacheOption) *CachedVectorProvider`
  - Cache key format: `eap:vectors:{imsi}:{type}` where type is "triplet" or "quintet"
  - Cache TTL: 5 minutes (configurable via CacheOption)
  - **Pre-fetch batch strategy:**
    - On `GetSIMTriplets`: check Redis cache first; if miss, fetch batch of 3 triplet sets from inner provider (count=9 vectors), cache all, return first set
    - On `GetAKAQuintets`: check Redis cache first; if miss, fetch batch of 3 quintets from inner provider (count=3), cache all, return first
    - Cached vectors stored as JSON array in Redis list; LPOP on each get, replenish when empty
  - `CacheOption` functional options: `WithVectorTTL(d time.Duration)`, `WithBatchSize(n int)`
  - If cache returns empty and inner provider fails, return error
  - If Redis is nil/unavailable, fall through to inner provider directly (graceful degradation)
- **Verify:** `cd /Users/btopcu/workspace/argus && go build ./internal/aaa/eap/...`

### Task 4: EAP-SIM Start subtype with version negotiation

- **Files:** Modify `internal/aaa/eap/sim.go`
- **Depends on:** — (none)
- **Complexity:** medium
- **Pattern ref:** Read `internal/aaa/eap/sim.go` — enhance existing SIMHandler
- **Context refs:** Architecture Context > Existing EAP Code Analysis, Architecture Context > EAP State Machine Interfaces
- **What:**
  - Add SIM-Start subtype handling per RFC 4186 flow: Identity → Start → Challenge → Success
  - Modify `SIMHandler.StartChallenge` to first send SIM-Start request (not directly Challenge):
    - SIM-Start contains AT_VERSION_LIST attribute with supported versions [1]
    - After receiving SIM-Start response (containing AT_NONCE_MT, AT_SELECTED_VERSION), proceed to send Challenge
  - Add new session state tracking: add `StartData *SIMStartData` to `EAPSession` for nonce storage
  - Define `SIMStartData` struct: `NonceMT [16]byte`, `SelectedVersion uint16`
  - Modify `HandleResponse` to handle both `SimSubtypeStart` (extract nonce, then issue challenge) and `SimSubtypeChallenge` (existing verification)
  - When handling Start response: extract AT_NONCE_MT and AT_SELECTED_VERSION from response data, store in session, then fetch vectors and build challenge request
  - Build SIM-Start request: subtype=10 (Start), AT_VERSION_LIST=[1]
  - Update `buildSIMChallengeRequest` to include AT_MAC computed with nonce_mt
- **Verify:** `cd /Users/btopcu/workspace/argus && go test ./internal/aaa/eap/... -run TestSIM -v`

### Task 5: SIM-type-based EAP method selection

- **Files:** Modify `internal/aaa/eap/state.go`
- **Depends on:** — (none)
- **Complexity:** medium
- **Pattern ref:** Read `internal/aaa/eap/state.go` — enhance existing selectMethod
- **Context refs:** Architecture Context > EAP State Machine Interfaces, Architecture Context > SIM Store Model, Architecture Context > Data Flow
- **What:**
  - Add `SIMTypeLookup` callback to StateMachine: `func(ctx context.Context, imsi string) (string, error)` — returns SIM type ("sim", "usim", "esim", "isim")
  - Add `SetSIMTypeLookup(fn func(ctx context.Context, imsi string) (string, error))` method on StateMachine
  - Modify `selectMethod` to accept `ctx context.Context` and `identity string`:
    - If SIMTypeLookup is set, call it to get SIM type
    - Selection rules: "sim" → MethodSIM, "usim"/"esim"/"isim" → MethodAKAPrime (prefer), fallback to MethodAKA
    - If lookup fails or returns unknown type → fall back to current priority logic (AKA' > AKA > SIM)
  - Update `handleIdentity` to pass ctx to selectMethod
  - Add `EAPMethod` field to `EAPSession` JSON for recording which method was used
- **Verify:** `cd /Users/btopcu/workspace/argus && go build ./internal/aaa/eap/...`

### Task 6: RADIUS server EAP integration

- **Files:** Modify `internal/aaa/radius/server.go`
- **Depends on:** Task 1, Task 2, Task 3, Task 5
- **Complexity:** high
- **Pattern ref:** Read `internal/aaa/radius/server.go` — enhance existing handleAuth
- **Context refs:** Architecture Context > Data Flow, Architecture Context > Components Involved, Architecture Context > EAP State Machine Interfaces, Architecture Context > Existing EAP Code Analysis
- **What:**
  - Add `eapMachine *eap.StateMachine` field to `Server` struct
  - Update `NewServer` to accept and store EAP StateMachine (or accept factory params to create it internally)
  - In `handleAuth`:
    - Extract EAP-Message attribute from RADIUS packet (attribute type 79)
    - If no EAP-Message present: continue with existing direct-auth flow (backward compatible)
    - If EAP-Message present:
      - Use RADIUS State attribute as EAP session ID (or generate one for first request)
      - Call `eapMachine.ProcessPacket(ctx, sessionID, eapMessage)`
      - If result is EAP-Success: send Access-Accept with EAP-Message=Success + MS-MPPE-Send-Key/MS-MPPE-Recv-Key derived from MSK (via `GetSessionMSK`)
      - If result is EAP-Request (challenge): send Access-Challenge with EAP-Message=challenge + State=sessionID
      - If result is EAP-Failure: send Access-Reject with EAP-Message=Failure
  - Add helper to extract/set EAP-Message from RADIUS packet (attribute type 79, byte slice)
  - Add helper to build MS-MPPE key attributes (attribute types 16/17 in vendor-specific 311/Microsoft)
  - Record `eap_method` in session creation: get method from `eapMachine.GetSessionMethod()`, pass as AuthMethod to session manager
- **Verify:** `cd /Users/btopcu/workspace/argus && go build ./internal/aaa/radius/...`

### Task 7: Session auth_method recording in session manager

- **Files:** Modify `internal/aaa/session/session.go`, Modify `internal/aaa/radius/server.go`
- **Depends on:** Task 6
- **Complexity:** medium
- **Pattern ref:** Read `internal/aaa/session/session.go` — follow existing session field patterns
- **Context refs:** Architecture Context > Store Model, Architecture Context > Database Schema
- **What:**
  - Add `AuthMethod string` field to `session.Session` struct (json tag: `"auth_method,omitempty"`)
  - In `Manager.Create`: pass `sess.AuthMethod` to `CreateRadiusSessionParams.AuthMethod` (use `nilIfEmpty`)
  - In `radiusSessionToSession`: map `rs.AuthMethod` to `sess.AuthMethod` (if non-nil)
  - In `radius/server.go` `handleAcctStart`: after EAP authentication succeeds, set `sess.AuthMethod` to the EAP method name string (e.g., "EAP-SIM", "EAP-AKA", "EAP-AKA'")
  - Values: use `eap.MethodType.String()` which already returns correct names
- **Verify:** `cd /Users/btopcu/workspace/argus && go build ./internal/aaa/...`

### Task 8: Comprehensive EAP integration tests

- **Files:** Create `internal/aaa/eap/redis_store_test.go`, Create `internal/aaa/eap/adapter_provider_test.go`, Create `internal/aaa/eap/vector_cache_test.go`, Modify `internal/aaa/eap/eap_test.go`
- **Depends on:** Task 1, Task 2, Task 3, Task 4, Task 5
- **Complexity:** high
- **Pattern ref:** Read `internal/aaa/eap/eap_test.go` — follow existing test patterns (testLogger, MockVectorProvider, flow tests)
- **Context refs:** Architecture Context > Existing EAP Code Analysis, Architecture Context > EAP State Machine Interfaces
- **What:**
  - **redis_store_test.go**: Test RedisStateStore with miniredis (or mock). Save/Get/Delete/TTL expiry. If miniredis not available, use MemoryStateStore comparison tests.
  - **adapter_provider_test.go**: Test AdapterVectorProvider with mock adapter. Triplet conversion, quintet conversion, error propagation, insufficient vector handling.
  - **vector_cache_test.go**: Test CachedVectorProvider. Cache hit, cache miss + replenish, graceful degradation when Redis nil.
  - **eap_test.go additions**:
    - Test EAP-SIM full flow with Start subtype: Identity → Start → Challenge → Success
    - Test SIM-type-based method selection: "sim" → EAP-SIM, "usim" → AKA', "esim" → AKA'
    - Test concurrent EAP sessions (goroutines, different session IDs, no state leakage)
    - Test EAP-AKA sync failure (AUTN mismatch) → Synchronization-Failure notification → EAP-Failure
    - Test unknown EAP method → NAK with supported methods list (already exists, enhance)
  - All tests use standard Go testing package + testify where already used in project
- **Verify:** `cd /Users/btopcu/workspace/argus && go test ./internal/aaa/eap/... -v -count=1`

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| EAP-SIM: Identity → Start → Challenge → Success flow | Task 4 | Task 8 (SIM flow with Start) |
| EAP-AKA: Identity → Challenge → Success flow | Existing (aka.go) | Task 8 (existing AKA test) |
| EAP-AKA': Challenge with AT_KDF and AT_KDF_INPUT | Existing (aka.go) | Task 8 (existing AKA' test) |
| EAP state machine tracks pending challenges in Redis (TTL 30s) | Task 1 | Task 8 (redis_store_test) |
| Challenge vectors fetched from operator adapter (SVC-06) | Task 2 | Task 8 (adapter_provider_test) |
| Authentication vector caching: pre-fetch batches | Task 3 | Task 8 (vector_cache_test) |
| EAP-Failure on AUTN mismatch or MAC failure | Existing + Task 4 | Task 8 (failure tests) |
| Session key (MSK/EMSK) derived in Access-Accept | Task 6 | Task 8 (RADIUS integration) |
| EAP method selection based on SIM type | Task 5 | Task 8 (method selection test) |
| Auth method recorded in TBL-17 session record | Task 7 | Task 8 |
| Pluggable method registry | Existing (state.go RegisterMethod) | Task 8 (registration test) |

## Story-Specific Compliance Rules

- **AAA**: EAP packets embedded in RADIUS EAP-Message attribute (type 79), multi-round-trip via Access-Challenge
- **Cache**: Redis state store uses 30s TTL for pending EAP challenges; vector cache uses 5m TTL
- **Protocol**: EAP-SIM per RFC 4186, EAP-AKA per RFC 4187, EAP-AKA' per RFC 5448
- **DB**: No migration needed — auth_method column already exists in sessions table
- **ADR-003**: Custom Go AAA engine — all EAP handling in Go, using layeh/radius for RADIUS protocol
- **Session**: auth_method values match MethodType.String() output: "EAP-SIM", "EAP-AKA", "EAP-AKA'"
- **Backward compat**: RADIUS handleAuth must continue working for non-EAP requests (no EAP-Message → direct auth)

## Risks & Mitigations

- **Risk**: EAP-SIM Start subtype adds complexity to existing flow → **Mitigation**: Keep existing flow working (tests pass), add Start as enhancement with careful state transitions
- **Risk**: Redis unavailable during EAP auth → **Mitigation**: CachedVectorProvider falls through to direct adapter; RedisStateStore is required (no fallback for state — fail-fast is correct for auth)
- **Risk**: Concurrent EAP sessions leaking state → **Mitigation**: Session ID is unique per RADIUS transaction; Redis keys are session-scoped; test concurrent sessions explicitly
- **Risk**: MPPE key derivation complexity → **Mitigation**: Use existing MSK from session data; MPPE wrapping follows standard format (RFC 2548)

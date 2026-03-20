# Implementation Plan: STORY-007 - Audit Log Service — Tamper-Proof Hash Chain

## Goal
Implement a production-ready audit log service (SVC-10) with tamper-proof SHA-256 hash chain, NATS-based event consumption, search/filter/export APIs, and KVKK pseudonymization support.

## Architecture Context

### Components Involved
- **SVC-10 Audit Service** (`internal/audit/`): Core audit logging, hash computation, chain verification, NATS consumer
- **Audit Store** (`internal/store/audit.go`): PostgreSQL data access for audit_logs table (TBL-19)
- **Audit API Handler** (`internal/api/audit/handler.go`): HTTP handlers for API-140, API-141, API-142
- **NATS Event Bus** (`internal/bus/nats.go`): Event publishing/subscribing — already implemented in STORY-006
- **API Gateway Router** (`internal/gateway/router.go`): Route registration for new audit endpoints

### Data Flow

**Audit Entry Creation (via NATS):**
1. Any state-changing handler (e.g., tenant.Create, user.Update) publishes audit event to NATS subject `argus.events.audit.create`
2. Audit service subscribes to `argus.events.audit.create` with exclusive consumer per tenant
3. Consumer fetches previous entry's hash for the tenant
4. Computes SHA-256 hash: `SHA256(tenant_id|user_id|action|entity_type|entity_id|created_at(RFC3339Nano)|prev_hash)`
5. Inserts entry into `audit_logs` table with hash + prev_hash
6. Genesis entry uses prev_hash = `"0000000000000000000000000000000000000000000000000000000000000000"` (64 zeros)

**Audit Log Query (API-140):**
1. Client sends GET /api/v1/audit-logs with filter params
2. Handler validates params, extracts tenant_id from JWT context
3. Store queries audit_logs with tenant_id scope + filters + cursor pagination
4. Returns standard list envelope with entries

**Chain Verification (API-141):**
1. Client sends GET /api/v1/audit-logs/verify?count=100
2. Handler fetches last N entries for tenant ordered by id ASC
3. For each consecutive pair, recomputes hash and verifies chain linkage
4. Returns verification result

**CSV Export (API-142):**
1. Client sends POST /api/v1/audit-logs/export with date range
2. Handler queries entries within range
3. Generates CSV content and returns download URL (or direct stream)

### API Specifications

#### API-140: List Audit Logs
- `GET /api/v1/audit-logs`
- Query params: `from` (ISO date), `to` (ISO date), `user_id` (UUID), `action` (string), `entity_type` (string), `entity_id` (string), `cursor` (string), `limit` (int, default 50, max 100)
- Auth: JWT with role `tenant_admin` or higher
- Success response (200):
```json
{
  "status": "success",
  "data": [
    {
      "id": 12345,
      "user_id": "uuid-or-null",
      "user_name": "user display name",
      "action": "create",
      "entity_type": "sim",
      "entity_id": "uuid-string",
      "diff": {"field": {"from": "old", "to": "new"}},
      "ip_address": "192.168.1.1",
      "created_at": "2026-03-18T14:02:00.123456789Z"
    }
  ],
  "meta": {"cursor": "12344", "limit": 50, "has_more": true}
}
```
- Error: 401/403 standard auth errors

#### API-141: Verify Hash Chain
- `GET /api/v1/audit-logs/verify?count=100`
- Query params: `count` (int, default 100, max 10000)
- Auth: JWT with role `tenant_admin` or higher
- Success response (200):
```json
{
  "status": "success",
  "data": {
    "verified": true,
    "entries_checked": 100,
    "first_invalid": null
  }
}
```
- If chain broken: `verified: false, first_invalid: 456` (ID of first invalid entry)

#### API-142: Export Audit Logs
- `POST /api/v1/audit-logs/export`
- Request body: `{"from": "2026-03-01", "to": "2026-03-20"}`
- Auth: JWT with role `tenant_admin` or higher
- Success response (202):
```json
{
  "status": "success",
  "data": {
    "download_url": "/api/v1/audit-logs/export/download/{token}"
  }
}
```
- For v1, generate CSV inline and stream it as attachment (simplify: return 200 with CSV directly)

### Database Schema

Source: `migrations/20260320000002_core_schema.up.sql` (ACTUAL — table already exists)

```sql
CREATE TABLE IF NOT EXISTS audit_logs (
    id BIGSERIAL,
    tenant_id UUID NOT NULL,
    user_id UUID,
    api_key_id UUID,
    action VARCHAR(50) NOT NULL,
    entity_type VARCHAR(50) NOT NULL,
    entity_id VARCHAR(100) NOT NULL,
    before_data JSONB,
    after_data JSONB,
    diff JSONB,
    ip_address INET,
    user_agent TEXT,
    correlation_id UUID,
    hash VARCHAR(64) NOT NULL,
    prev_hash VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);
```

Existing indexes:
- `idx_audit_tenant_time` on (tenant_id, created_at DESC)
- `idx_audit_tenant_entity` on (tenant_id, entity_type, entity_id)
- `idx_audit_tenant_user` on (tenant_id, user_id)
- `idx_audit_tenant_action` on (tenant_id, action)
- `idx_audit_correlation` on (correlation_id)

Monthly partitions already created: `audit_logs_2026_03` through `audit_logs_2026_06`.

### Hash Chain Algorithm

From ALGORITHMS.md Section 2:

```
compute_hash(entry, prev_hash) → string:
1. data = tenant_id|user_id|action|entity_type|entity_id|created_at(RFC3339Nano)|prev_hash
   - tenant_id: UUID string (lowercase, hyphenated)
   - user_id: UUID string or "system" if null
   - action: string as-is
   - entity_type: string as-is
   - entity_id: string as-is
   - created_at: time.RFC3339Nano format
   - prev_hash: 64-character hex string
2. hash = SHA256([]byte(data))
3. return hex.EncodeToString(hash[:])
```

Genesis prev_hash: `"0000000000000000000000000000000000000000000000000000000000000000"` (64 zeros)

Chain verification:
```
verify_chain(tenant_id, from_id, to_id):
1. SELECT entries ORDER BY id ASC
2. For each entry (from second): recompute hash, compare with stored hash
3. Also verify entry.prev_hash == previous_entry.hash
```

### Pseudonymization

For KVKK purge compliance:
```sql
UPDATE audit_logs SET
    before_data = anonymize_jsonb(before_data, ARRAY['imsi', 'msisdn', 'iccid']),
    after_data = anonymize_jsonb(after_data, ARRAY['imsi', 'msisdn', 'iccid']),
    diff = anonymize_jsonb(diff, ARRAY['imsi', 'msisdn', 'iccid'])
WHERE entity_type = 'sim' AND tenant_id = $1 AND entity_id IN (...);
```

The `anonymize_jsonb` function replaces sensitive field values with their SHA-256 hash.

### NATS Subject & Concurrency

Per ALGORITHMS.md:
- Subject: `argus.audit.write.{tenant_id}` (but for simplicity in v1, use single subject `argus.events.audit.create`)
- Audit writes are serialized per tenant to ensure sequential hash chain integrity
- NATS consumer with durable name ensures at-least-once delivery

### Existing Bus Constants (from internal/bus/nats.go)

Already defined subjects:
- `SubjectSessionStarted`, `SubjectSessionUpdated`, `SubjectSessionEnded`
- `SubjectSIMUpdated`, `SubjectPolicyChanged`, `SubjectOperatorHealthChanged`
- `SubjectNotification`, `SubjectAlertTriggered`
- `SubjectJobQueue`, `SubjectJobCompleted`, `SubjectJobProgress`
- `SubjectCacheInvalidate`

New subject needed: `SubjectAuditCreate = "argus.events.audit.create"`

Streams: `EVENTS` stream already covers `argus.events.>` — audit subject fits.

## Prerequisites
- [x] STORY-006 completed (NATS event bus with JetStream, structured logging)
- [x] Database schema exists (audit_logs table with partitions in core_schema.up.sql)
- [x] Existing audit.Service stub in `internal/audit/audit.go`

## Tasks

### Task 1: Audit hash computation and model types
- **Files:** Modify `internal/audit/audit.go`
- **Depends on:** —
- **Complexity:** high
- **Pattern ref:** Read `internal/store/tenant.go` — follow same struct/type patterns
- **Context refs:** Hash Chain Algorithm, Database Schema
- **What:**
  - Define complete `AuditEntry` struct matching all TBL-19 columns: ID (int64), TenantID (uuid.UUID), UserID (*uuid.UUID), APIKeyID (*uuid.UUID), Action (string), EntityType (string), EntityID (string), BeforeData (json.RawMessage), AfterData (json.RawMessage), Diff (json.RawMessage), IPAddress (*string), UserAgent (*string), CorrelationID (*uuid.UUID), Hash (string), PrevHash (string), CreatedAt (time.Time)
  - Define `CreateEntryParams` struct with all fields needed for creating an entry (TenantID, UserID, APIKeyID, Action, EntityType, EntityID, BeforeData, AfterData, IPAddress, UserAgent, CorrelationID)
  - Implement `ComputeHash(entry AuditEntry, prevHash string) string` — concatenate fields with `|` separator per algorithm spec, SHA-256 hash, return hex string. user_id uses "system" when nil.
  - Implement `ComputeDiff(before, after json.RawMessage) json.RawMessage` — compute field-level diff between before/after JSON objects. For each key that differs, store `{"field": {"from": oldVal, "to": newVal}}`.
  - Define `GenesisHash` constant = 64 zero characters
  - Define `VerifyResult` struct: Verified (bool), EntriesChecked (int), FirstInvalid (*int64)
  - Define `AuditEvent` struct for NATS message payload: TenantID, UserID, APIKeyID, Action, EntityType, EntityID, BeforeData, AfterData, IPAddress, UserAgent, CorrelationID
- **Verify:** `go build ./internal/audit/...`

### Task 2: Audit store — CRUD and chain operations
- **Files:** Create `internal/store/audit.go`
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** Read `internal/store/tenant.go` — follow same pgxpool.Pool pattern, error handling, cursor pagination
- **Context refs:** Database Schema, API Specifications, Hash Chain Algorithm
- **What:**
  - Create `AuditStore` struct with `db *pgxpool.Pool`
  - `NewAuditStore(db *pgxpool.Pool) *AuditStore`
  - `Create(ctx, entry *audit.AuditEntry) (*audit.AuditEntry, error)` — INSERT into audit_logs with all columns, RETURNING id, created_at. Use the partition-compatible composite PK (id, created_at).
  - `GetLastHash(ctx, tenantID uuid.UUID) (string, error)` — SELECT hash FROM audit_logs WHERE tenant_id = $1 ORDER BY id DESC LIMIT 1. Return GenesisHash if no entries found.
  - `List(ctx, tenantID uuid.UUID, params ListAuditParams) ([]audit.AuditEntry, string, error)` — cursor-based pagination (cursor is string of last entry ID). Support filters: from/to (created_at range), user_id, action, entity_type, entity_id. Build dynamic WHERE clause. Scoped by tenant_id.
  - Define `ListAuditParams` struct: Cursor string, Limit int, From *time.Time, To *time.Time, UserID *uuid.UUID, Action string, EntityType string, EntityID string
  - `GetRange(ctx, tenantID uuid.UUID, count int) ([]audit.AuditEntry, error)` — SELECT last N entries ordered by id ASC (for chain verification). Used by verify endpoint.
  - `GetByDateRange(ctx, tenantID uuid.UUID, from, to time.Time) ([]audit.AuditEntry, error)` — for CSV export
  - `Pseudonymize(ctx, tenantID uuid.UUID, entityIDs []string) error` — UPDATE before_data, after_data, diff replacing imsi/msisdn/iccid values with SHA-256 hashes. Implement anonymize logic in Go (parse JSONB, find keys, hash values, update).
- **Verify:** `go build ./internal/store/...`

### Task 3: Audit service — NATS consumer and chain writer
- **Files:** Create `internal/audit/service.go`, Modify `internal/bus/nats.go`
- **Depends on:** Task 1, Task 2
- **Complexity:** high
- **Pattern ref:** Read `internal/bus/nats.go` — follow same EventBus pattern for subscription
- **Context refs:** NATS Subject & Concurrency, Existing Bus Constants, Hash Chain Algorithm, Data Flow
- **What:**
  - Add `SubjectAuditCreate = "argus.events.audit.create"` constant to `internal/bus/nats.go`
  - Rewrite `internal/audit/service.go` (replacing the stub in audit.go or alongside it):
    - `Service` struct holds: store (*store.AuditStore), bus (*bus.EventBus), logger (zerolog.Logger), mu (sync.Mutex for hash chain serialization)
    - `NewService(store, bus, logger) *Service`
    - `Start(ctx context.Context) error` — subscribe to `SubjectAuditCreate` via bus.QueueSubscribe with queue group "audit-writers"
    - `handleAuditEvent(subject string, data []byte)` — unmarshal AuditEvent, call processEntry
    - `processEntry(ctx, event AuditEvent) error`:
      1. Lock mutex (per-tenant serialization — use sync.Map of per-tenant mutexes for concurrency)
      2. GetLastHash from store
      3. Build AuditEntry with all fields, set CreatedAt = time.Now()
      4. ComputeDiff(before, after) and set on entry
      5. ComputeHash(entry, prevHash) and set hash + prev_hash
      6. store.Create(entry)
      7. Unlock mutex
    - `PublishAuditEvent(ctx, event AuditEvent) error` — helper to publish audit event to NATS (used by handlers instead of direct audit.CreateEntry)
    - `VerifyChain(ctx, tenantID uuid.UUID, count int) (*VerifyResult, error)` — fetch last N entries, iterate and verify hashes
  - Update existing `internal/audit/audit.go`: keep only types and hash functions, remove the old stub Service/CreateEntry
- **Verify:** `go build ./internal/audit/... && go build ./internal/bus/...`

### Task 4: Audit API handlers
- **Files:** Create `internal/api/audit/handler.go`
- **Depends on:** Task 2, Task 3
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/tenant/handler.go` — follow same handler pattern with apierr.WriteSuccess/WriteList/WriteError, chi.URLParam, context extraction
- **Context refs:** API Specifications, Database Schema
- **What:**
  - Create `Handler` struct: auditStore (*store.AuditStore), auditSvc (*audit.Service), logger (zerolog.Logger)
  - `NewHandler(auditStore, auditSvc, logger) *Handler`
  - `List(w, r)` — API-140:
    - Extract tenant_id from context
    - Parse query params: from, to (time.Parse), user_id (uuid.Parse), action, entity_type, entity_id, cursor, limit
    - Validate limit (default 50, max 100)
    - Call auditStore.List with params
    - Map entries to response DTOs (id, user_id, action, entity_type, entity_id, diff, ip_address, created_at)
    - Return with apierr.WriteList
  - `Verify(w, r)` — API-141:
    - Extract tenant_id from context
    - Parse count param (default 100, max 10000)
    - Call auditSvc.VerifyChain(ctx, tenantID, count)
    - Return result with apierr.WriteSuccess
  - `Export(w, r)` — API-142:
    - Extract tenant_id from context
    - Parse from/to from request body JSON
    - Call auditStore.GetByDateRange
    - Generate CSV with headers: id, user_id, action, entity_type, entity_id, before_data, after_data, diff, ip_address, user_agent, created_at
    - Set Content-Type: text/csv, Content-Disposition: attachment
    - Stream CSV response directly (no file URL in v1 — simplify)
  - Define response DTOs: auditLogResponse struct with json tags
- **Verify:** `go build ./internal/api/audit/...`

### Task 5: Router integration and handler wiring
- **Files:** Modify `internal/gateway/router.go`
- **Depends on:** Task 4
- **Complexity:** medium
- **Pattern ref:** Read `internal/gateway/router.go` — follow same pattern of adding handler to RouterDeps and registering routes
- **Context refs:** API Specifications, Architecture Context > Components Involved
- **What:**
  - Add `AuditHandler *auditapi.Handler` to `RouterDeps` struct
  - Add import for `auditapi "github.com/btopcu/argus/internal/api/audit"`
  - Register routes in authenticated group with RequireRole("tenant_admin"):
    - `GET /api/v1/audit-logs` → AuditHandler.List
    - `GET /api/v1/audit-logs/verify` → AuditHandler.Verify
    - `POST /api/v1/audit-logs/export` → AuditHandler.Export
  - Wire conditionally: `if deps.AuditHandler != nil { ... }` (same pattern as TenantHandler)
- **Verify:** `go build ./internal/gateway/...`

### Task 6: Update existing handlers to publish audit events via NATS
- **Files:** Modify `internal/api/tenant/handler.go`, Modify `internal/api/user/handler.go`
- **Depends on:** Task 3
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/tenant/handler.go` — existing createAuditEntry pattern to replace
- **Context refs:** NATS Subject & Concurrency, Data Flow
- **What:**
  - In tenant handler:
    - Add `bus *bus.EventBus` field to Handler struct
    - Update `NewHandler` to accept bus parameter
    - Replace `createAuditEntry` method to publish via NATS instead of direct service call:
      - Build `audit.AuditEvent` with all fields
      - Call `bus.Publish(ctx, bus.SubjectAuditCreate, event)`
    - Keep backward compatibility: if bus is nil, fall back to existing audit service call
  - In user handler:
    - Same changes: add bus field, update NewHandler, replace createAuditEntry to publish via NATS
  - Ensure correlation_id is extracted from request context and included in audit event
- **Verify:** `go build ./internal/api/tenant/... && go build ./internal/api/user/...`

### Task 7: Tests — hash chain, store, and handler tests
- **Files:** Create `internal/audit/audit_test.go`, Create `internal/store/audit_test.go`, Create `internal/api/audit/handler_test.go`
- **Depends on:** Task 1, Task 2, Task 3, Task 4
- **Complexity:** high
- **Pattern ref:** Read `internal/store/tenant_test.go` or `internal/api/tenant/handler_test.go` — follow same test patterns
- **Context refs:** Hash Chain Algorithm, API Specifications, Database Schema, Pseudonymization
- **What:**
  - `internal/audit/audit_test.go`:
    - Test ComputeHash: known input → known output (deterministic)
    - Test ComputeHash with nil user_id → uses "system"
    - Test ComputeDiff: before/after → correct diff output
    - Test ComputeDiff with nil before (create) → diff shows all fields as new
    - Test GenesisHash constant is 64 zero chars
    - Test hash chain: create 3 sequential entries, verify chain links correctly
    - Test tamper detection: modify one entry's data, recompute shows mismatch
  - `internal/store/audit_test.go`:
    - Test store Create + List with cursor pagination (mock or integration)
    - Test GetLastHash returns GenesisHash when empty
    - Test List with date range filters
    - Test List with action/entity_type filters
  - `internal/api/audit/handler_test.go`:
    - Test List handler returns correct envelope format
    - Test Verify handler returns verification result
    - Test Export handler returns CSV content-type
    - Test unauthorized access returns 403
- **Verify:** `go test ./internal/audit/... ./internal/store/... ./internal/api/audit/...`

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| Every state-changing API creates audit entry via NATS | Task 6 | Task 7 (handler tests) |
| Entry contains all required fields | Task 1, Task 2 | Task 7 (audit_test.go) |
| Hash computed per algorithm spec | Task 1 | Task 7 (ComputeHash tests) |
| prev_hash links to previous entry | Task 3 | Task 7 (chain tests) |
| GET /api/v1/audit-logs with filters | Task 4 | Task 7 (handler_test.go) |
| GET /api/v1/audit-logs/verify | Task 4 | Task 7 (handler_test.go) |
| POST /api/v1/audit-logs/export CSV | Task 4 | Task 7 (handler_test.go) |
| Table partitioned by month | Already in migration | N/A (pre-existing) |
| Pseudonymization function | Task 2 | Task 7 (store tests) |

## Story-Specific Compliance Rules

- **API**: Standard envelope `{ status, data, meta?, error? }` for all endpoints
- **DB**: audit_logs table already exists in migration — use ACTUAL column names from migration
- **Auth**: JWT with `tenant_admin` role minimum for all audit endpoints
- **Hash Chain**: Must follow ALGORITHMS.md Section 2 exactly — SHA-256, pipe-separated, RFC3339Nano timestamps
- **Concurrency**: Per-tenant mutex to ensure sequential hash chain writes
- **NATS**: Use existing EVENTS stream (covers `argus.events.>`)
- **Pagination**: Cursor-based (not offset) — cursor is entry ID

## Risks & Mitigations

- **Hash chain ordering**: BIGSERIAL guarantees monotonic IDs within a partition. Per-tenant mutex ensures sequential writes. Risk: concurrent writes from multiple instances. Mitigation: NATS queue group ensures single consumer.
- **Partition management**: Monthly partitions are pre-created in migration. Risk: running out of partitions. Mitigation: noted for future auto-partition creation job.
- **CSV export memory**: Large date ranges could produce huge CSV. Mitigation: stream rows directly to response writer, don't buffer in memory.

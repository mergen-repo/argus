# Implementation Plan: STORY-029 — OTA SIM Management via APDU Commands

## Goal
Enable Over-The-Air SIM management via APDU commands with SMS-PP/BIP delivery, security (KIC/KID), rate limiting, bulk operations via job runner, and full delivery status tracking.

## Architecture Context

### Components Involved
- **SVC-03 (Core API)**: `internal/api/ota/handler.go` — HTTP endpoints for OTA command CRUD
- **SVC-09 (Job Runner)**: `internal/job/ota.go` — Bulk OTA processor replacing stub
- **OTA Package**: `internal/ota/` — APDU builder, delivery encoding, security, rate limiting, types
- **Store Layer**: `internal/store/ota.go` — PostgreSQL data access for ota_commands table
- **Gateway**: `internal/gateway/router.go` — OTA route registration
- **Main**: `cmd/argus/main.go` — Wiring OTA handler + replacing stub processor

### Data Flow
1. User sends POST `/api/v1/sims/{id}/ota` with command_type + payload
2. Handler validates request, checks rate limit via Redis
3. APDU builder constructs byte sequence from payload
4. OTA command stored in `ota_commands` table with status=queued
5. Audit entry created
6. For bulk: POST `/api/v1/sims/bulk/ota` creates a job → OTAProcessor processes each SIM

### API Specifications

**POST /api/v1/sims/{id}/ota** — Send OTA to single SIM
- Role: sim_manager
- Request: `{ command_type: string, channel?: string, security_mode?: string, payload: object, max_retries?: int }`
- Success: 201 `{ status: "success", data: { id, tenant_id, sim_id, command_type, channel, status, ... } }`
- Errors: 400 (bad ID), 404 (SIM not found), 422 (validation), 429 (rate limit)

**POST /api/v1/sims/bulk/ota** — Bulk OTA via job
- Role: tenant_admin
- Request: `{ sim_ids: string[], segment_id?: string, command_type, channel, security_mode, payload, max_retries? }`
- Success: 202 `{ status: "success", data: { job_id, state, total_sims } }`

**GET /api/v1/sims/{id}/ota** — List OTA history for SIM
- Role: sim_manager
- Query: cursor, limit, command_type, status, channel
- Success: 200 `{ status: "success", data: [...], meta: { cursor, limit, has_more } }`

**GET /api/v1/ota-commands/{commandId}** — Get single OTA command
- Role: sim_manager
- Success: 200 `{ status: "success", data: { ... } }`

### Database Schema

```sql
-- Source: migrations/20260321000002_ota_commands.up.sql (ACTUAL)
CREATE TABLE IF NOT EXISTS ota_commands (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id),
    sim_id          UUID NOT NULL REFERENCES sims(id),
    command_type    VARCHAR(30) NOT NULL CHECK (command_type IN ('UPDATE_FILE', 'INSTALL_APPLET', 'DELETE_APPLET', 'READ_FILE', 'SIM_TOOLKIT')),
    channel         VARCHAR(10) NOT NULL DEFAULT 'sms_pp' CHECK (channel IN ('sms_pp', 'bip')),
    status          VARCHAR(15) NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'sent', 'delivered', 'executed', 'confirmed', 'failed')),
    apdu_data       BYTEA,
    security_mode   VARCHAR(10) NOT NULL DEFAULT 'none' CHECK (security_mode IN ('none', 'kic', 'kid', 'kic_kid')),
    payload         JSONB NOT NULL DEFAULT '{}',
    response_data   JSONB,
    error_message   TEXT,
    job_id          UUID REFERENCES jobs(id),
    retry_count     INT NOT NULL DEFAULT 0,
    max_retries     INT NOT NULL DEFAULT 3,
    created_by      UUID REFERENCES users(id),
    sent_at         TIMESTAMPTZ,
    delivered_at    TIMESTAMPTZ,
    executed_at     TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- Indexes: tenant_id, sim_id, job_id (partial), status (partial), (sim_id, created_at DESC)
```

## Prerequisites
- [x] STORY-011 completed (SIM CRUD — `store.SIMStore`, `store.SIM` model)
- [x] STORY-031 completed (Job Runner — `job.Runner`, `job.Processor` interface, `store.JobStore`)

## Existing Code Status

All core files exist from a previous incomplete attempt. Code is functional but:
1. **Build error**: `otaHandler` declared but not used in `cmd/argus/main.go:168` — not wired into `RouterDeps`
2. **Stub not replaced**: OTA stub processor still registered instead of real `OTAProcessor`
3. **No tests**: Zero test files for `internal/ota/`, `internal/api/ota/`, `internal/store/ota.go`, `internal/job/ota.go`

## Tasks

### Task 1: Wire OTA handler and replace stub processor in main.go
- **Files:** Modify `cmd/argus/main.go`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `cmd/argus/main.go` lines 166-196 — follow existing wiring pattern
- **Context refs:** Architecture Context > Components Involved, Architecture Context > Data Flow
- **What:**
  1. Add `OTAHandler: otaHandler` to the `RouterDeps` struct literal (after `PolicyHandler: policyHandler,` around line 389)
  2. Create the real OTA processor: `otaProcessor := job.NewOTAProcessor(jobStore, otaStore, simStore, otaRateLimiter, eventBus, log.Logger)` and register it with `jobRunner.Register(otaProcessor)`
  3. Remove the OTA stub: delete the `otaCommandStub` line (line 187) and its `jobRunner.Register(otaCommandStub)` (line 194)
  4. The real processor must be registered BEFORE `jobRunner.Start()` is called
- **Verify:** `cd /Users/btopcu/workspace/argus && go build ./...`

### Task 2: OTA types and APDU builder tests
- **Files:** Create `internal/ota/types_test.go`, Create `internal/ota/apdu_test.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/job/runner_test.go` — follow Go testing patterns
- **Context refs:** Architecture Context > Components Involved, Database Schema
- **What:**
  - `types_test.go`: Test `CommandType.Validate()` for all 5 valid types + invalid type. Test `DeliveryChannel.Validate()` for valid + invalid. Test `BulkOTAPayload` JSON marshaling.
  - `apdu_test.go`: Test `BuildAPDU` for each command type:
    - UPDATE_FILE: valid payload produces non-empty bytes, missing content returns error
    - INSTALL_APPLET: valid payload with package_aid + applet_aid, missing fields returns error
    - DELETE_APPLET: valid AID produces correct TLV structure
    - READ_FILE: valid payload with file_id + length
    - SIM_TOOLKIT: envelope command with tag + data
    - Invalid command type returns error
    - Test `APDU.Bytes()` produces correct header format [CLA, INS, P1, P2, Lc, Data...]
- **Verify:** `cd /Users/btopcu/workspace/argus && go test ./internal/ota/...`

### Task 3: OTA security and delivery tests
- **Files:** Create `internal/ota/security_test.go`, Create `internal/ota/delivery_test.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/job/runner_test.go` — follow Go testing patterns
- **Context refs:** Architecture Context > Components Involved
- **What:**
  - `security_test.go`: Test `SecureAPDU`:
    - SecurityNone: returns data unchanged, no MAC
    - SecurityKIC: returns encrypted data (different from input), no MAC
    - SecurityKID: returns original data + 8-byte MAC
    - SecurityKICKID: returns encrypted data + 8-byte MAC
    - Nil keys with non-none mode returns error-free (SecurityNone fallback)
    - Test `VerifyMAC` with correct/incorrect MAC
    - Test `encryptAES` with valid 16/24/32-byte keys + invalid key length
  - `delivery_test.go`: Test `EncodeSMSPP`:
    - SecurityNone: SPI=[0x00,0x00]
    - SecurityKIC: SPI has encrypt flag
    - Oversized data (>140 bytes) returns error
    - Counter correctly encoded in CNTR field
    - Test `EncodeBIP`: produces correct header (channel, transport, port, length, data)
- **Verify:** `cd /Users/btopcu/workspace/argus && go test ./internal/ota/...`

### Task 4: OTA rate limiter tests
- **Files:** Create `internal/ota/ratelimit_test.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/job/lock_test.go` — follow Redis-dependent test patterns
- **Context refs:** Architecture Context > Components Involved
- **What:**
  - Test `NewRateLimiter` with default (0) and custom maxPerHour
  - Test `MaxPerHour()` returns configured value
  - Test `Allow` and `Remaining` require a real or mock Redis client. Since these are integration tests that need Redis, write unit-testable scenarios:
    - Verify `RateLimiter` struct fields are set correctly
    - Test key format: `ota:ratelimit:{simID}`
  - If the project has a Redis test helper (check `internal/job/lock_test.go`), use it. Otherwise, test constructor + MaxPerHour only.
- **Verify:** `cd /Users/btopcu/workspace/argus && go test ./internal/ota/...`

### Task 5: OTA store and handler tests + job processor test
- **Files:** Create `internal/store/ota_test.go`, Create `internal/job/ota_test.go`
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** Read `internal/store/sim_test.go` — follow store test patterns; Read `internal/job/import_test.go` — follow job processor test pattern
- **Context refs:** Architecture Context > Components Involved, Database Schema, API Specifications
- **What:**
  - `internal/store/ota_test.go`: Follow existing store test patterns. Test:
    - `CreateOTACommandParams` struct field validation
    - `OTACommandFilter` struct initialization
    - `OTACommand` model field coverage
    - If store tests use a test DB (check `internal/store/sim_test.go`), create integration tests for Create, GetByID, ListBySimID, UpdateStatus, IncrementRetry, CountBySimInWindow, ListByJobID
    - If store tests are unit-only, test model serialization and filter construction
  - `internal/job/ota_test.go`: Follow `import_test.go` pattern. Test:
    - `OTAProcessor.Type()` returns `"ota_command"`
    - `NewOTAProcessor` constructor sets all fields
    - Payload unmarshal with valid/invalid JSON
    - BulkOTAResult JSON serialization
- **Verify:** `cd /Users/btopcu/workspace/argus && go test ./internal/store/... ./internal/job/...`

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| OTA command types (5 types) | `internal/ota/types.go` (existing) | Task 2 |
| Single SIM OTA | `internal/api/ota/handler.go` SendToSIM (existing) | Task 1 (build), Task 5 |
| Bulk OTA via job runner | `internal/api/ota/handler.go` BulkSend + `internal/job/ota.go` (existing) | Task 1 (wiring), Task 5 |
| APDU command builder | `internal/ota/apdu.go` (existing) | Task 2 |
| OTA delivery via SMS-PP | `internal/ota/delivery.go` EncodeSMSPP (existing) | Task 3 |
| OTA delivery via BIP | `internal/ota/delivery.go` EncodeBIP (existing) | Task 3 |
| Delivery status tracking | `internal/store/ota.go` UpdateStatus (existing) | Task 5 |
| OTA security (KIC, KID) | `internal/ota/security.go` (existing) | Task 3 |
| Bulk OTA partial success | `internal/job/ota.go` error collection (existing) | Task 5 |
| Rate limiting | `internal/ota/ratelimit.go` (existing) | Task 4 |
| OTA command history | `internal/store/ota.go` ListBySimID + handler ListHistory (existing) | Task 5 |

## Story-Specific Compliance Rules
- API: Standard envelope `{ status, data, meta? }` — already implemented in handler
- DB: Migration exists at `migrations/20260321000002_ota_commands.up.sql` — no changes needed
- Business: Rate limit default 10 per SIM per hour, configurable via `ota.DefaultMaxOTAPerSimPerHour`
- Job: OTA processor replaces stub per DEV-083 decision

## Risks & Mitigations
- **Risk**: Redis tests may skip if no test Redis available → test constructors and pure functions only
- **Risk**: Store integration tests require test DB → follow existing store test pattern (likely unit tests with model validation)

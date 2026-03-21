# Implementation Plan: STORY-032 - CDR Processing & Rating Engine

## Goal
Implement an async CDR processing pipeline that subscribes to NATS session events, creates CDR records in TimescaleDB (TBL-18), calculates costs via a rating engine, and exposes REST API endpoints for listing and exporting CDRs.

## Architecture Context

### Components Involved
- **SVC-07 Analytics Engine** (`internal/analytics/cdr/`): CDR consumer, rating engine — new package
- **Store Layer** (`internal/store/cdr.go`): CDR CRUD + cost aggregation queries — new file
- **API Layer** (`internal/api/cdr/handler.go`): REST handler for API-114, API-115 — new package
- **Job Layer** (`internal/job/cdr_export.go`): Background CDR export processor — new file
- **Bus Layer** (`internal/bus/nats.go`): Already has session event subjects
- **Gateway** (`internal/gateway/router.go`): Route registration for CDR endpoints
- **Main** (`cmd/argus/main.go`): Wiring CDR consumer, store, handler, job processor

### Data Flow

```
RADIUS Accounting / Diameter CCR / 5G SBA
  → Session Manager updates session in TBL-17
  → Publishes NATS event: session.started / session.updated / session.ended
  → CDR Consumer (SVC-07) subscribes to these NATS topics
  → Creates CDR record in TBL-18 (cdrs)
  → Rating Engine calculates cost:
      1. Lookup operator grant → cost_per_mb
      2. Apply RAT type multiplier
      3. Apply time-of-day tariff
      4. Apply volume tier
  → CDR stored with usage_cost and carrier_cost
  → TimescaleDB continuous aggregates auto-refresh hourly/daily
```

### API Specifications

#### API-114: GET /api/v1/cdrs — List CDRs
- Auth: JWT (analyst+)
- Query params: `cursor`, `limit` (max 100, default 50), `sim_id`, `operator_id`, `from` (RFC3339), `to` (RFC3339), `min_cost`
- Success response:
```json
{
  "status": "success",
  "data": [{
    "id": 12345,
    "session_id": "uuid",
    "sim_id": "uuid",
    "operator_id": "uuid",
    "apn_id": "uuid",
    "rat_type": "lte",
    "record_type": "stop",
    "bytes_in": 1048576,
    "bytes_out": 524288,
    "duration_sec": 3600,
    "usage_cost": "5.2500",
    "carrier_cost": "3.1500",
    "rate_per_mb": "0.0100",
    "rat_multiplier": "1.00",
    "timestamp": "2026-03-22T10:00:00Z"
  }],
  "meta": { "cursor": "12344", "limit": 50, "has_more": true }
}
```
- Error: 400 invalid params, 401 unauthorized
- Status codes: 200

#### API-115: POST /api/v1/cdrs/export — Export CDRs to CSV
- Auth: JWT (analyst+)
- Request body:
```json
{
  "from": "2026-03-01T00:00:00Z",
  "to": "2026-03-22T00:00:00Z",
  "operator_id": "uuid (optional)",
  "format": "csv"
}
```
- Success response (202):
```json
{
  "status": "success",
  "data": {
    "job_id": "uuid",
    "status": "queued"
  }
}
```
- Error: 400 invalid params, 422 validation error
- Status codes: 202

### Database Schema

Source: `migrations/20260320000002_core_schema.up.sql` (ACTUAL — table already exists)

```sql
CREATE TABLE IF NOT EXISTS cdrs (
    id BIGSERIAL,
    session_id UUID NOT NULL,
    sim_id UUID NOT NULL,
    tenant_id UUID NOT NULL,
    operator_id UUID NOT NULL,
    apn_id UUID,
    rat_type VARCHAR(10),
    record_type VARCHAR(20) NOT NULL,       -- 'start', 'interim', 'stop'
    bytes_in BIGINT NOT NULL DEFAULT 0,
    bytes_out BIGINT NOT NULL DEFAULT 0,
    duration_sec INTEGER NOT NULL DEFAULT 0,
    usage_cost DECIMAL(12,4),
    carrier_cost DECIMAL(12,4),
    rate_per_mb DECIMAL(8,4),
    rat_multiplier DECIMAL(4,2) DEFAULT 1.0,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

Source: `migrations/20260320000003_timescaledb_hypertables.up.sql` (ACTUAL)
- Already converted to hypertable: `SELECT create_hypertable('cdrs', 'timestamp', ...)`
- Indexes: `idx_cdrs_session`, `idx_cdrs_tenant_time`, `idx_cdrs_tenant_operator_time`, `idx_cdrs_sim_time`
- Compression: after 7 days, segmentby tenant_id+operator_id

Source: `migrations/20260320000004_continuous_aggregates.up.sql` (ACTUAL)
- `cdrs_hourly` and `cdrs_daily` materialized views already exist

### Operator Rate Config

Source: `migrations/20260320000002_core_schema.up.sql` — `operator_grants` table (ACTUAL)
- `cost_per_mb DECIMAL` nullable column on `operator_grants` (per-tenant operator grant)
- Rate lookup: `SELECT cost_per_mb FROM operator_grants WHERE tenant_id = $1 AND operator_id = $2 AND enabled = true`

### Session Event NATS Payloads

From existing code (`internal/aaa/radius/`, `internal/aaa/diameter/`, `internal/aaa/sba/`):
- Subject: `argus.events.session.started` — payload: `{session_id, sim_id, tenant_id, operator_id, apn_id, rat_type, protocol_type, ...}`
- Subject: `argus.events.session.updated` — payload: `{session_id, sim_id, tenant_id, operator_id, bytes_in, bytes_out, duration_sec, ...}`
- Subject: `argus.events.session.ended` — payload: `{session_id, sim_id, tenant_id, operator_id, bytes_in, bytes_out, duration_sec, terminate_cause, ...}`

### RAT Type Multipliers

From ALGORITHMS.md Section 5 and rattype package (`internal/aaa/rattype/`):
- Canonical values: `utran`, `geran`, `lte`, `nb_iot`, `lte_m`, `nr_5g`, `nr_5g_nsa`
- Default multipliers (configurable per-operator via adapter_config JSONB):
  - `utran` (3G): 1.0
  - `geran` (2G): 0.5
  - `lte` (4G): 1.0
  - `nb_iot`: 0.3
  - `lte_m`: 0.5
  - `nr_5g` (5G): 1.5
  - `nr_5g_nsa`: 1.2
  - Default/unknown: 1.0

### Time-of-Day Tariff

- Peak hours: 08:00-20:00 UTC → multiplier 1.0
- Off-peak hours: 20:00-08:00 UTC → multiplier 0.7
- Configurable via operator adapter_config JSONB `tariff` field

### Volume Tiers

- Per ALGORITHMS.md Section 5, volume tier is based on cumulative session bytes:
  - Tier 1: 0-1GB → base rate
  - Tier 2: 1-10GB → 0.8x base rate
  - Tier 3: 10GB+ → 0.5x base rate
- Applied per-session (cumulative bytes_in + bytes_out across CDRs for same session_id)

### Existing Patterns

- Store pattern: `internal/store/session_radius.go` — struct, columns var, scan func, CRUD methods with pgxpool
- API handler pattern: `internal/api/session/handler.go` — chi handler with DTO mapping, apierr envelope
- Job processor pattern: `internal/job/ota.go` — implements `Processor` interface with `Type()` and `Process()` methods
- NATS consumer pattern: `internal/audit/service.go` — QueueSubscribe with JSON unmarshal

## Prerequisites
- [x] STORY-015: RADIUS server publishes session events to NATS
- [x] STORY-019: Diameter server publishes session events to NATS
- [x] STORY-009: Operator store with cost_per_mb in operator_grants
- [x] TBL-18 cdrs table and hypertable already created in migrations
- [x] Continuous aggregates (cdrs_hourly, cdrs_daily) already created
- [x] Job runner available (STORY-031)

## Tasks

### Task 1: CDR Store — Database Access Layer
- **Files:** Create `internal/store/cdr.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/session_radius.go` — follow same store structure (struct, columns, scan, CRUD)
- **Context refs:** Database Schema, Operator Rate Config
- **What:**
  - Define `CDR` struct matching all columns from TBL-18 (id BIGSERIAL, session_id UUID, sim_id UUID, tenant_id UUID, operator_id UUID, apn_id *UUID, rat_type *string, record_type string, bytes_in int64, bytes_out int64, duration_sec int, usage_cost *float64, carrier_cost *float64, rate_per_mb *float64, rat_multiplier *float64, timestamp time.Time)
  - Define `CreateCDRParams` struct for insert
  - Define `CDRStore` with `*pgxpool.Pool`
  - `Create(ctx, params) (*CDR, error)` — INSERT INTO cdrs RETURNING all columns
  - `CreateIdempotent(ctx, params) (*CDR, error)` — INSERT with ON CONFLICT (session_id, timestamp, record_type) DO NOTHING; requires unique index (add via new migration)
  - `ListByTenant(ctx, tenantID, params ListCDRParams) ([]CDR, string, error)` — cursor-based pagination with filters: sim_id, operator_id, from, to, min_cost. Cursor is `id` (BIGINT, descending). Use `idx_cdrs_tenant_time` index
  - `GetCostAggregation(ctx, tenantID, from, to, operatorID) ([]CostAggRow, error)` — aggregate usage_cost, carrier_cost grouped by operator_id, day using cdrs_daily view
  - `CountForExport(ctx, tenantID, from, to, operatorID) (int64, error)` — count CDRs matching export filter
  - `StreamForExport(ctx, tenantID, from, to, operatorID, callback) error` — iterate CDRs in batches of 1000 for CSV export, calling callback per row
- **Verify:** `go build ./internal/store/...`

### Task 2: CDR Deduplication Migration
- **Files:** Create `migrations/20260322000001_cdr_dedup_index.up.sql`, Create `migrations/20260322000001_cdr_dedup_index.down.sql`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `migrations/20260320000003_timescaledb_hypertables.up.sql` — follow same migration pattern for TimescaleDB table modifications
- **Context refs:** Database Schema
- **What:**
  - UP: Create unique index on cdrs for deduplication: `CREATE UNIQUE INDEX IF NOT EXISTS idx_cdrs_dedup ON cdrs (session_id, timestamp, record_type)` — this enables ON CONFLICT for idempotent inserts
  - DOWN: `DROP INDEX IF EXISTS idx_cdrs_dedup`
  - Note: TimescaleDB unique indexes must include the partitioning column (timestamp), which is already included
- **Verify:** Files exist and have valid SQL syntax

### Task 3: Rating Engine — Cost Calculation
- **Files:** Create `internal/analytics/cdr/rating.go`, Create `internal/analytics/cdr/rating_test.go`
- **Depends on:** —
- **Complexity:** high
- **Pattern ref:** Read `internal/aaa/rattype/rattype.go` — follow same pure-function pattern (no external deps)
- **Context refs:** RAT Type Multipliers, Time-of-Day Tariff, Volume Tiers, Operator Rate Config
- **What:**
  - Define `RatingConfig` struct: `CostPerMB float64`, `RATMultipliers map[string]float64`, `PeakHoursStart/End int`, `PeakMultiplier/OffPeakMultiplier float64`, `VolumeTiers []VolumeTier`
  - Define `VolumeTier` struct: `UpToBytes int64`, `Multiplier float64`
  - Define `RatingResult` struct: `UsageCost float64`, `CarrierCost float64`, `RatePerMB float64`, `RATMultiplier float64`, `TotalMB float64`
  - `DefaultRATMultipliers() map[string]float64` — returns default RAT multipliers from Architecture Context
  - `DefaultVolumeTiers() []VolumeTier` — returns 3-tier volume config
  - `NewRatingConfig(costPerMB float64) *RatingConfig` — creates config with defaults
  - `(rc *RatingConfig) Calculate(bytesIn, bytesOut int64, ratType string, timestamp time.Time, cumulativeSessionBytes int64) *RatingResult` — implements ALGORITHMS.md Section 5:
    1. totalBytes = bytesIn + bytesOut
    2. totalMB = totalBytes / (1024*1024)
    3. ratMultiplier = lookup from config.RATMultipliers[ratType], default 1.0
    4. timeMultiplier = peak/off-peak based on timestamp hour
    5. volumeMultiplier = based on cumulativeSessionBytes tier
    6. usageCost = totalMB * costPerMB * ratMultiplier * timeMultiplier * volumeMultiplier
    7. carrierCost = totalMB * costPerMB (raw carrier rate, no multipliers)
  - Tests: 500MB at $0.01/MB = $5.00; 5G at 1.5x = $7.50; off-peak at 0.7x; volume tier 2 at 0.8x; zero bytes = zero cost; nil/unknown RAT = 1.0x
- **Verify:** `go test ./internal/analytics/cdr/...`

### Task 4: CDR Consumer — NATS Event Subscriber
- **Files:** Create `internal/analytics/cdr/consumer.go`
- **Depends on:** Task 1, Task 3
- **Complexity:** high
- **Pattern ref:** Read `internal/audit/service.go` — follow QueueSubscribe pattern for NATS consumer
- **Context refs:** Data Flow, Session Event NATS Payloads, Database Schema, RAT Type Multipliers
- **What:**
  - Define `Consumer` struct with dependencies: `cdrStore *store.CDRStore`, `operatorStore *store.OperatorStore`, `eventBus *bus.EventBus`, `logger zerolog.Logger`
  - `NewConsumer(cdrStore, operatorStore, eventBus, logger) *Consumer`
  - `Start(subscriber) error` — QueueSubscribe to 3 NATS subjects with queue group `cdr-consumer`:
    - `argus.events.session.started` → `handleSessionStarted` → creates CDR with record_type='start', no cost yet
    - `argus.events.session.updated` → `handleSessionUpdated` → creates CDR with record_type='interim', calculates delta bytes, rates cost
    - `argus.events.session.ended` → `handleSessionEnded` → creates CDR with record_type='stop', final totals, full cost rating
  - `Stop()` — unsubscribe
  - Event payload parsing: unmarshal JSON, extract session_id, sim_id, tenant_id, operator_id, apn_id, rat_type, bytes_in, bytes_out, duration_sec, terminate_cause
  - Rating lookup: query `operator_grants` for `cost_per_mb` by tenant_id + operator_id. If nil, use default rate 0.0 (no cost)
  - Rating config: build from operator adapter_config JSONB (rat_type_multipliers, tariff, volume_tiers) with fallback to defaults
  - Idempotent insert via `CreateIdempotent` — same session_id + timestamp + record_type → no duplicate
  - Log errors but don't crash consumer — continue processing next event
- **Verify:** `go build ./internal/analytics/cdr/...`

### Task 5: CDR API Handler — List and Export Endpoints
- **Files:** Create `internal/api/cdr/handler.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/session/handler.go` — follow same handler pattern (struct, NewHandler, DTO, List, apierr envelope)
- **Context refs:** API Specifications
- **What:**
  - Define `Handler` struct with `cdrStore *store.CDRStore`, `jobStore *store.JobStore`, `eventBus *bus.EventBus`, `logger zerolog.Logger`
  - `NewHandler(cdrStore, jobStore, eventBus, logger) *Handler`
  - `List(w, r)` — GET /api/v1/cdrs
    - Extract tenant_id from JWT context
    - Parse query params: cursor, limit (default 50, max 100), sim_id, operator_id, from, to (parse as RFC3339), min_cost (parse as float64)
    - Call `cdrStore.ListByTenant`
    - Map to DTO: id, session_id, sim_id, operator_id, apn_id, rat_type, record_type, bytes_in, bytes_out, duration_sec, usage_cost (string), carrier_cost (string), rate_per_mb (string), rat_multiplier (string), timestamp (RFC3339)
    - Return standard envelope with `apierr.WriteList`
  - `Export(w, r)` — POST /api/v1/cdrs/export
    - Parse JSON body: from, to (required, RFC3339), operator_id (optional UUID), format (must be "csv")
    - Validate: from < to, format == "csv"
    - Create job via `jobStore.Create` with type `cdr_export`, payload `{from, to, operator_id, format}`
    - Publish to `argus.jobs.queue`
    - Return 202 with `{job_id, status: "queued"}`
- **Verify:** `go build ./internal/api/cdr/...`

### Task 6: CDR Export Job Processor
- **Files:** Create `internal/job/cdr_export.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/job/ota.go` — follow same Processor interface implementation
- **Context refs:** API Specifications, Database Schema
- **What:**
  - Define `CDRExportProcessor` implementing `Processor` interface
  - `Type() string` → return `"cdr_export"` (add constant to `internal/job/types.go`)
  - `Process(ctx, job) error`:
    1. Parse payload: from, to (time.Time), operator_id (*uuid.UUID)
    2. Count matching CDRs → set job.TotalItems
    3. Stream CDRs via `cdrStore.StreamForExport`
    4. Build CSV in memory: columns = id, session_id, sim_id, operator_id, apn_id, rat_type, record_type, bytes_in, bytes_out, duration_sec, usage_cost, carrier_cost, rate_per_mb, rat_multiplier, timestamp
    5. Store CSV data as base64 in job result JSONB (same pattern as STORY-007 audit export)
    6. Complete job with result containing `download_url` pointing to job result
  - Add `JobTypeCDRExport = "cdr_export"` constant to `internal/job/types.go`
- **Verify:** `go build ./internal/job/...`

### Task 7: CDR Store Tests
- **Files:** Create `internal/store/cdr_test.go`
- **Depends on:** Task 1, Task 2
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/session_radius_test.go` — follow same test pattern
- **Context refs:** Database Schema, API Specifications
- **What:**
  - Test `Create` — insert CDR, verify all fields returned
  - Test `CreateIdempotent` — insert same session_id+timestamp+record_type twice, verify no error and no duplicate
  - Test `ListByTenant` — insert multiple CDRs, verify cursor pagination, time range filter, sim_id filter, operator_id filter, min_cost filter
  - Test `GetCostAggregation` — insert CDRs for multiple operators, verify aggregation results
  - Test `CountForExport` — verify count matches filter criteria
- **Verify:** `go test ./internal/store/... -run TestCDR`

### Task 8: Wiring — Router, Main, Integration
- **Files:** Modify `internal/gateway/router.go`, Modify `cmd/argus/main.go`
- **Depends on:** Task 4, Task 5, Task 6
- **Complexity:** medium
- **Pattern ref:** Read `internal/gateway/router.go` — follow existing handler registration pattern; Read `cmd/argus/main.go` — follow existing wiring pattern
- **Context refs:** Data Flow, API Specifications
- **What:**
  - **router.go:**
    - Add `CDRHandler *cdrapi.Handler` to `RouterDeps`
    - Add import for `cdrapi "github.com/btopcu/argus/internal/api/cdr"`
    - Register routes in analyst+ group:
      ```
      r.Group(func(r chi.Router) {
          r.Use(JWTAuth(deps.JWTSecret))
          r.Use(RequireRole("analyst"))
          r.Get("/api/v1/cdrs", deps.CDRHandler.List)
          r.Post("/api/v1/cdrs/export", deps.CDRHandler.Export)
      })
      ```
  - **main.go:**
    - Import `cdrapi "github.com/btopcu/argus/internal/api/cdr"` and `cdrsvc "github.com/btopcu/argus/internal/analytics/cdr"`
    - Create `cdrStore := store.NewCDRStore(pg.Pool)`
    - Create `cdrConsumer := cdrsvc.NewConsumer(cdrStore, operatorStore, eventBus, log.Logger)`
    - Start consumer: `cdrConsumer.Start(&eventBusSubscriber{eventBus})`
    - Create `cdrHandler := cdrapi.NewHandler(cdrStore, jobStore, eventBus, log.Logger)`
    - Add `CDRHandler: cdrHandler` to RouterDeps
    - Create `cdrExportProc := job.NewCDRExportProcessor(jobStore, cdrStore, eventBus, log.Logger)`
    - Register: `jobRunner.Register(cdrExportProc)`
    - Add `cdrConsumer.Stop()` to shutdown sequence
  - **job/types.go:** Add `JobTypeCDRExport = "cdr_export"` to constants and `AllJobTypes` slice
- **Verify:** `go build ./cmd/argus/...`

### Task 9: CDR Handler and Consumer Tests
- **Files:** Create `internal/api/cdr/handler_test.go`, Create `internal/analytics/cdr/consumer_test.go`
- **Depends on:** Task 3, Task 4, Task 5
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/session/handler_test.go` — follow same httptest pattern
- **Context refs:** API Specifications, Data Flow, Session Event NATS Payloads
- **What:**
  - **handler_test.go:**
    - Test List: valid request → 200 with standard envelope, empty list → 200 with empty array
    - Test List with filters: sim_id, operator_id, from/to, min_cost
    - Test List invalid params: bad cursor, bad date format
    - Test Export: valid request → 202 with job_id
    - Test Export validation: missing from/to, invalid format, from > to
  - **consumer_test.go:**
    - Test handleSessionStarted: event → CDR created with record_type='start', no cost
    - Test handleSessionUpdated: event → CDR created with record_type='interim', cost calculated
    - Test handleSessionEnded: event → CDR created with record_type='stop', final cost
    - Test rating: 500MB at $0.01/MB = $5.00
    - Test deduplication: same event twice → one CDR
    - Test missing cost_per_mb: no operator grant → zero cost
- **Verify:** `go test ./internal/api/cdr/... ./internal/analytics/cdr/...`

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| RADIUS Accounting-Start → CDR record (type='start') | Task 4 (consumer handleSessionStarted) | Task 9 (consumer_test) |
| RADIUS Accounting-Interim → CDR with delta bytes | Task 4 (consumer handleSessionUpdated) | Task 9 (consumer_test) |
| RADIUS Accounting-Stop → CDR with final totals | Task 4 (consumer handleSessionEnded) | Task 9 (consumer_test) |
| Diameter CCR → equivalent CDR records | Task 4 (same NATS topics) | Task 9 (consumer_test) |
| Rating engine: cost calculation | Task 3 (rating.go) | Task 3 (rating_test.go) |
| CDR fields match TBL-18 | Task 1 (CDR struct) | Task 7 (store_test) |
| TBL-18 is TimescaleDB hypertable | Already exists in migration 000003 | Pre-existing |
| GET /api/v1/cdrs | Task 5 (handler List) | Task 9 (handler_test) |
| POST /api/v1/cdrs/export | Task 5 + Task 6 | Task 9 (handler_test) |
| Carrier cost aggregation | Task 1 (GetCostAggregation) | Task 7 (store_test) |
| CDR processing async via NATS | Task 4 (consumer) | Task 9 (consumer_test) |
| CDR deduplication | Task 2 (dedup index) + Task 1 (CreateIdempotent) | Task 7 (store_test) |

## Story-Specific Compliance Rules

- API: Standard envelope `{ status, data, meta?, error? }` for all endpoints
- API: Cursor-based pagination (not offset) for CDR list
- DB: All CDR queries scoped by tenant_id (enforced in store layer)
- DB: CDR table already exists as TimescaleDB hypertable — no schema changes to existing table, only add dedup index
- DB: Migration naming: `YYYYMMDDHHMMSS_description.up.sql` / `.down.sql`
- Business: Cost calculation per ALGORITHMS.md Section 5
- Business: RAT type uses canonical values from `internal/aaa/rattype/` package
- ADR-002: TimescaleDB for CDR time-series, NATS for async event processing

## Bug Pattern Warnings

No matching bug patterns.

## Risks & Mitigations

- **Risk:** High CDR volume could overwhelm consumer → **Mitigation:** Queue group (`cdr-consumer`) allows horizontal scaling; idempotent inserts prevent duplicates on reprocessing
- **Risk:** Operator grant missing cost_per_mb → **Mitigation:** Default to 0.0 cost, log warning, CDR still created
- **Risk:** CDR export for large date ranges → **Mitigation:** Background job with streaming, not inline response

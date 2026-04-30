# Implementation Plan: FIX-235 — M2M eSIM Provisioning Pipeline (SGP.22 → SGP.02 + SM-SR + Bulk Operations)

## Goal
Refactor the consumer-grade SGP.22 (QR/LPA pull) eSIM model into a M2M-grade SGP.02 (SM-SR push) pipeline with platform-driven OTA command queue, EID stock management, secure callback, bulk operations, and the eSIM polish/coverage items (F-172..F-184) — without breaking existing single-profile Enable/Disable/Switch APIs.

## Story Context
- **Effort**: XL (12 ACs, 7 waves, 22 tasks)
- **Findings addressed**: F-172 (architectural protocol shift), F-173 (operator name), F-174 (EID masking), F-175 (ICCID clickable), F-176 (state filter Failed enum), F-177 (Allocate from Stock), F-178 (bulk ops), F-180 (lifecycle audit), F-181 (stock UI), F-182 (SIM detail eSIM card), F-184 (Operator detail eSIM tab)
- **Dependencies**: FIX-209 alerts table (DONE), FIX-211 severity taxonomy (DONE), FIX-212 bus envelope (DONE), FIX-246 patterns (just-shipped — bulk endpoint, alerter cron, alert source canonical)
- **Bug pattern guards**: PAT-024 (fake stores hide DB CHECK constraints — alert `source` MUST be canonical), PAT-022 (CHECK enum drift — Go const ↔ DB constraint sync), PAT-017 (config not propagated to handler), PAT-019 (interface fakes for unit tests)

## Architecture Touch — eSIM Data Model Deep-Dive

### Existing infra to extend (DO NOT recreate)
| File | Status | Action in this story |
|------|--------|---------------------|
| `migrations/20260320000002_core_schema.up.sql:252-270` | TBL-12 esim_profiles (created) | Add CHECK update via new migration to allow `failed` state |
| `migrations/20260412000002_esim_multiprofile.up.sql` | Multi-profile + state CHECK | Migration extends CHECK to add `failed` |
| `internal/store/esim.go` | ESimProfileStore (full CRUD + Switch + ListEnriched) | Add `MarkFailed(profileID)`, used by dispatcher on terminal OTA failure |
| `internal/api/esim/handler.go` | List/Create/Get/Enable/Disable/Switch/Delete | Add bulk-switch route + callback route + stock summary route |
| `internal/job/bulk_esim_switch.go` | EXISTING bulk processor — SYNC switch, distLock, audit, undo | **REFACTOR**: replace direct `esimStore.Switch()` with `otaCommandStore.Insert(queued)`. Dispatcher does the actual switch on ACK. Existing wire contract preserved. |
| `internal/job/types.go:9` | `JobTypeOTACommand = "ota_command"` | REUSE as the dispatcher worker job type |
| `internal/esim/smdp.go` | SGP.22 SMDPAdapter (consumer flow) | Coexist — kept for existing per-profile Enable/Disable callsites |
| `internal/notification/webhook.go:89-96` | `ComputeHMAC` / `VerifyHMAC` (HMAC-SHA256, hex) | REUSE for callback signature |
| `internal/audit/audit.go` Auditor.CreateEntry | Standard interface | REUSE for AC-11 |
| `internal/bus/nats.go:21-46` Subject* constants | bus envelope wire format (FIX-212) | ADD 3 subjects: `SubjectESimCommandIssued`, `SubjectESimCommandAcked`, `SubjectESimCommandFailed` |

### NEW packages / files
- `internal/smsr/` (NEW) — SM-SR client interface + mock
- `internal/store/esim_ota.go` (NEW) — OTA command store
- `internal/store/esim_stock.go` (NEW) — Profile stock store
- `internal/job/esim_ota_dispatcher.go` (NEW) — dequeue queued → SMSR.Push → mark sent
- `internal/job/esim_ota_timeout_reaper.go` (NEW) — sent>N min → timeout
- `internal/job/esim_stock_alert.go` (NEW) — available<10% → alert (PAT-024 source='system')

### Database Schema

**TBL-25 esim_ota_commands** (Source: NEW — this story creates it)
```sql
CREATE TABLE esim_ota_commands (
  id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id             UUID NOT NULL REFERENCES tenants(id),
  eid                   VARCHAR(32) NOT NULL,
  profile_id            UUID REFERENCES esim_profiles(id),
  command_type          VARCHAR(20) NOT NULL,
  target_operator_id    UUID REFERENCES operators(id),
  source_profile_id     UUID REFERENCES esim_profiles(id),
  target_profile_id     UUID REFERENCES esim_profiles(id),
  status                VARCHAR(20) NOT NULL DEFAULT 'queued',
  smsr_command_id       VARCHAR(128),
  retry_count           INT NOT NULL DEFAULT 0,
  last_error            TEXT,
  job_id                UUID REFERENCES jobs(id),
  correlation_id        UUID,
  created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  sent_at               TIMESTAMPTZ,
  acked_at              TIMESTAMPTZ,
  next_retry_at         TIMESTAMPTZ,
  CONSTRAINT chk_esim_ota_command_type   CHECK (command_type IN ('enable','disable','switch','delete')),
  CONSTRAINT chk_esim_ota_command_status CHECK (status       IN ('queued','sent','acked','failed','timeout'))
);
CREATE INDEX idx_esim_ota_status_created ON esim_ota_commands (status, created_at);
CREATE INDEX idx_esim_ota_eid_created    ON esim_ota_commands (eid, created_at DESC);
CREATE INDEX idx_esim_ota_tenant_status  ON esim_ota_commands (tenant_id, status);
CREATE INDEX idx_esim_ota_sent_partial   ON esim_ota_commands (sent_at) WHERE status = 'sent';
ALTER TABLE esim_ota_commands ENABLE ROW LEVEL SECURITY;
ALTER TABLE esim_ota_commands FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_esim_ota ON esim_ota_commands
  USING (tenant_id = current_setting('app.current_tenant', true)::uuid);
```

**TBL-26 esim_profile_stock** (Source: NEW)
```sql
CREATE TABLE esim_profile_stock (
  tenant_id      UUID NOT NULL REFERENCES tenants(id),
  operator_id    UUID NOT NULL REFERENCES operators(id),
  total          BIGINT NOT NULL DEFAULT 0 CHECK (total >= 0),
  allocated      BIGINT NOT NULL DEFAULT 0 CHECK (allocated >= 0),
  available      BIGINT NOT NULL GENERATED ALWAYS AS (total - allocated) STORED,
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (tenant_id, operator_id),
  CONSTRAINT chk_stock_alloc_le_total CHECK (allocated <= total)
);
ALTER TABLE esim_profile_stock ENABLE ROW LEVEL SECURITY;
ALTER TABLE esim_profile_stock FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_stock ON esim_profile_stock
  USING (tenant_id = current_setting('app.current_tenant', true)::uuid);
```

**ALTER esim_profiles state CHECK** — extend to allow `failed` (Source: ALTER existing constraint)
```sql
ALTER TABLE esim_profiles DROP CONSTRAINT IF EXISTS chk_esim_profile_state;
ALTER TABLE esim_profiles ADD CONSTRAINT chk_esim_profile_state
  CHECK (profile_state IN ('available','enabled','disabled','deleted','failed'));
```

### State Machine — esim_ota_commands

```
queued -- dispatcher --> sent -- callback ACK --> acked   [terminal]
                          |
                          +-- callback NACK --> failed    [terminal] (retry_count<5: queued; ≥5: terminal)
                          |
                          +-- (sent_at + N min < NOW) --> timeout [reaper] (then queued again or terminal if retry_count≥5)

queued -- dispatcher.Push() error --> queued (retry with exponential backoff: 30s, 1m, 2m, 4m, 8m; cap 5 attempts)
```
- N timeout min: env `ESIM_OTA_TIMEOUT_MINUTES=10` (default 10).
- Reaper cron `esim_ota_timeout_reaper` runs `*/2 * * * *`.
- On `failed`/`timeout` terminal: profile row `profile_state='failed'`, `last_error` populated.
- On `acked`: dispatcher applies the actual `esimStore.Switch()` and stock decrement.

### Concurrent Stock Allocation Strategy
**No advisory locks. Atomic UPDATE with RETURNING:**
```sql
UPDATE esim_profile_stock
   SET allocated = allocated + 1, updated_at = NOW()
 WHERE tenant_id = $1 AND operator_id = $2 AND (total - allocated) > 0
 RETURNING total, allocated, (total - allocated) AS available;
```
Zero rows returned ⇒ stockout (return ErrStockExhausted to caller). PostgreSQL row-level write lock during UPDATE is sufficient — no deadlock window because only one row updated per request.

### SM-SR Client Interface (`internal/smsr/client.go`)
```go
// Stable interface — mock + real vendor adapters implement it.
type Client interface {
    Push(ctx context.Context, req PushRequest) (PushResponse, error)
    Health(ctx context.Context) error
}
type CommandType string  // enable | disable | switch | delete
type PushRequest struct {
    EID            string
    CommandType    CommandType
    SourceProfile  string  // ICCID or profile_id
    TargetProfile  string  // ICCID for switch/enable
    CommandID      string  // ota_commands.id
    CorrelationID  string
}
type PushResponse struct {
    SMSRCommandID string  // vendor's tracking id
    AcceptedAt    time.Time
}
```
**Implementations**:
- `internal/smsr/mock_client.go` — `MockClient` accepts everything, returns generated `SMSRCommandID`. Configurable failure injection via `MOCK_SMSR_FAIL_RATE=0.0` env.
- Future real vendor: HTTPS POST to vendor SM-SR API behind same interface.

### HMAC Callback Verification
- Algorithm: **HMAC-SHA256** (REUSE `notification.ComputeHMAC` + `VerifyHMAC`).
- Header: `X-SMSR-Signature: <hex>`
- Header: `X-SMSR-Timestamp: <unix>` (reject if delta > 300s — replay protection)
- Body: raw JSON; signature computed over `<timestamp>.<body>`.
- Secret: env `SMSR_CALLBACK_SECRET` (≥32 chars; main.go validates non-empty in production)
- Failure: 401 Unauthorized, audit log `entity_type=esim_profile action=ota.callback_rejected`.

### Rate Limiting Strategy
- Token bucket per-operator (in-memory `golang.org/x/time/rate`), refill rate `ESIM_OTA_RATE_LIMIT_PER_SEC=100` (default 100/sec, configurable).
- Burst = same as rate (no fancy headroom).
- When limited, dispatcher sleeps until token available (max 30s) or re-queues with backoff if context canceled.

### API Specifications

#### `POST /api/v1/esim-profiles/bulk-switch` (extend existing semantics)
- **Auth**: JWT + role `sim_manager`
- Request body (one of):
```json
{ "filter": { "operator_id": "<uuid>", "apn_id": "<uuid?>" }, "target_operator_id": "<uuid>", "reason": "..." }
{ "eids": ["E1","E2"], "target_operator_id": "<uuid>", "reason": "..." }
{ "sim_ids": ["...","..."], "target_operator_id": "<uuid>", "reason": "..." }
```
- Response 202: `{ status:"success", data:{ job_id, affected_count, mode:"ota" } }`
- Errors: 400 (no selection), 422 (target operator stockout), 401, 403

#### `POST /api/v1/esim-profiles/callbacks/ota-status` (NEW, public + HMAC)
- **Auth**: HMAC-only (no JWT — exposed for vendor SM-SR)
- Headers: `X-SMSR-Signature`, `X-SMSR-Timestamp`
- Body:
```json
{ "command_id":"<uuid>", "smsr_command_id":"...", "status":"acked|failed", "error":"...", "device_timestamp":"..." }
```
- Response 200: `{ status:"success", data:{ command_id, new_status } }`
- Response 401 on signature mismatch or timestamp drift

#### `GET /api/v1/esim-profiles/stock-summary` (NEW)
- **Auth**: JWT + role `sim_manager`
- Query: `?operator_id=<uuid>` (optional)
- Response 200:
```json
{ "status":"success", "data":[
  { "operator_id":"...","operator_name":"Turkcell","total":50000,"allocated":30000,"available":20000,"utilization_pct":60.0 }
] }
```

#### `GET /api/v1/esim-profiles/{id}/ota-history` (NEW)
- Lists OTA commands for a profile (per-EID page).
- Response: `{ status:"success", data:[{id, command_type, status, created_at, sent_at, acked_at, error}], meta:{ next_cursor } }`

### Screen Mockups

**eSIM List `/esim` (extends existing page)**
```
┌──────────────────────────────────────────────────────────────────────────┐
│ eSIM Profiles                              [Filter] [Allocate from Stock]│
│ ☐ ICCID            EID                Operator     State    Last Prov.  │
│ ☐ 89...8745     8900...971523[copy]  Turkcell    enabled   2026-04-25  │
│ ☑ 89...3389     8900...445201[copy]  Vodafone TR disabled  -           │
│ ...                                                                      │
├──────────────────────────────────────────────────────────────────────────┤
│ [STICKY 2 selected] Bulk Switch Operator ▼  Bulk Disable  Cancel         │
└──────────────────────────────────────────────────────────────────────────┘
```
- ICCID column: clickable link → `/sims/{sim_id}`. "SIM ID" UUID column REMOVED.
- Operator column: NAME (e.g. "Turkcell" not `20000000`).
- EID: head-tail mask `89000000...971523` + copy button + tooltip full EID.
- State filter dropdown adds **failed** option (now legitimate post-migration).
- Top action: "Allocate from Stock" replaces "Create Profile" (single-entry retired).

**Operator Detail `/operators/{id}` — NEW eSIM Profiles tab**
```
┌─Tabs: Overview | Protocols | Health | ... | SIMs | eSIM Profiles | Audit─┐
│ eSIM Profiles tab content:                                                │
│  ┌─Stock Card─────────┐  ┌─Stats Card──────────┐                          │
│  │ Total:   50,000    │  │ Enabled:    5,000   │                          │
│  │ Alloc:   30,000    │  │ Disabled:   25,000  │                          │
│  │ Avail:   20,000    │  │ Available:  20,000  │                          │
│  │ Util:    60%       │  │ Failed:     0       │                          │
│  └────────────────────┘  └─────────────────────┘                          │
│  [Bulk Switch all enabled to ▼ ...]                                       │
│  Linked profiles list (filtered by this operator_id, paginated)           │
└──────────────────────────────────────────────────────────────────────────┘
```

**SIM Detail `/sims/{id}` — NEW eSIM Profile card** (only when sim_type='esim')
```
┌─eSIM Profile──────────────────────────────────┐
│ EID: 8900...971523 [copy]                      │
│ Current Profile: enabled · Turkcell            │
│ Last Provisioned: 2026-04-25 14:32             │
│ [Switch Profile] [Disable] [View History →]    │
└────────────────────────────────────────────────┘
```

### Design Token Map (FRONTEND.md auto-derived Tailwind v4 utilities)

#### Color Tokens
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Primary text | `text-text-primary` | `text-[#E4E4ED]`, `text-white` |
| Secondary text | `text-text-secondary` | `text-[#7A7A95]`, `text-gray-400` |
| Tertiary/muted | `text-text-tertiary` | `text-[#4A4A65]` |
| Page bg | `bg-bg-primary` | `bg-[#06060B]`, `bg-black` |
| Card bg | `bg-bg-surface` | `bg-[#0C0C14]` |
| Elevated panel | `bg-bg-elevated` | `bg-[#12121C]` |
| Hover state | `bg-bg-hover` | `bg-[#1A1A28]` |
| Border subtle | `border-border-subtle` | `border-[#16162A]` |
| Danger | `text-danger` / `bg-danger-dim` / `border-danger` | `text-red-500` |
| Success | `text-success` / `bg-success-dim` | `text-green-500` |
| Warning | `text-warning` / `bg-warning-dim` | `text-yellow-500` |
| Accent (primary CTA) | `text-accent` / `bg-accent-dim` / `bg-accent` | `text-blue-500` |

#### Typography Tokens
| Usage | Token Class |
|-------|-------------|
| Page heading | `text-heading-lg font-bold` |
| Section heading | `text-heading-md font-semibold` |
| Body | `text-body-md` |
| Caption | `text-caption` |
| Mono (EID/ICCID) | `font-mono text-body-md` |

#### Spacing & Elevation
| Usage | Token Class |
|-------|-------------|
| Card | `rounded-card shadow-card` |
| Section padding | `p-section` |

#### Components to REUSE (DO NOT recreate)
| Component | Path | Use For |
|-----------|------|---------|
| `<Button>` | `web/src/components/atoms/Button.tsx` | ALL buttons |
| `<Input>` | `web/src/components/atoms/Input.tsx` | ALL form fields |
| `<DataTable>` | `web/src/components/organisms/DataTable.tsx` | eSIM list table |
| `<StatCard>` | `web/src/components/molecules/StatCard.tsx` | Stock/Stats KPI cards |
| `<SlidePanel>` | (existing) | Allocate from Stock form panel |
| `<ConfirmDialog>` | (existing) | Bulk switch confirmation |
| `<Tooltip>` | (existing) | EID full-value hover |
| `<CopyButton>` | (existing — used for IMSI) | EID copy |
| `<Tabs>` | (existing) | Operator Detail eSIM Profiles tab |

**RULE: zero hex/px in any new component file.** Quality Gate command: `grep -rE '#[0-9a-fA-F]{3,8}|\[[0-9]+px\]' web/src/{pages,components}/**esim** web/src/pages/operators/_tabs/esim-tab.tsx web/src/pages/sims/esim-tab.tsx` → ZERO matches.

## AC Mapping

| AC | What | Implemented in Task(s) | Verified by |
|----|------|------------------------|-------------|
| AC-1 | esim_ota_commands table | T1 migration | T22 integration |
| AC-2 | esim_profile_stock table | T2 migration | T22 integration |
| AC-3 | SM-SR Client interface + mock | T4 (smsr/client.go), T5 (smsr/mock_client.go) | T20 unit |
| AC-4 | OTA dispatcher worker (5x backoff) | T7 (esim_ota_dispatcher.go), T8 (timeout reaper) | T20 unit + T22 integration |
| AC-5 | bulk-switch endpoint (extend existing) | T9 (refactor bulk_esim_switch.go), T10 (handler route extension) | T22 integration |
| AC-6 | callback endpoint + HMAC | T11 (callback handler), T12 (router wire + middleware) | T20 unit |
| AC-7 | UI changes (3 places) | T15 (eSIM list bulk bar), T17 (Operator detail eSIM tab), T18 (SIM detail eSIM card) | T22 browser |
| AC-8 | polish (operator name, EID mask, ICCID link, state filter+failed) | T3 (state CHECK migration), T6 (store enriched DTO), T16 (FE polish) | T22 browser |
| AC-9 | Allocate from Stock UI (replace Create Profile) | T19 (Allocate slidepanel + handler endpoint) | T22 browser |
| AC-10 | stock low alert (<10%) | T13 (esim_stock_alert cron) — source='system' | T20 unit (assert source literal) |
| AC-11 | audit coverage on dispatch + callback | T7 (dispatcher emits), T11 (callback emits), T9 (bulk extension) | T22 integration |
| AC-12 | PROTOCOLS.md eSIM SGP.02 chapter | T21 docs | T22 manual review |

## Wave / Task Decomposition (7 waves, 22 tasks)

> Each task gets a fresh Developer subagent. `Pattern ref` files MUST be read by the Developer.

### Wave 1 — Migrations + Types (parallel)
Foundation; no Go deps between these.

#### Task 1: Create `esim_ota_commands` migration
- **Files**: Create `migrations/20260503000001_esim_ota_commands.up.sql` + `.down.sql`
- **Depends on**: —
- **Complexity**: medium
- **Pattern ref**: `migrations/20260422000001_alerts_table.up.sql` (CHECK constraint shape, RLS policy, indices)
- **Context refs**: "Database Schema" → esim_ota_commands
- **What**: Embed exactly the SQL block under TBL-25 above. Include CHECK constraints `chk_esim_ota_command_type` and `chk_esim_ota_command_status`. Include RLS policy. Include all 4 indices.
- **Verify**: `make db-migrate` clean apply; rollback works; `\dt+ esim_ota_commands` shows table; `\d esim_ota_commands` shows constraints.

#### Task 2: Create `esim_profile_stock` migration
- **Files**: Create `migrations/20260503000002_esim_profile_stock.up.sql` + `.down.sql`
- **Depends on**: —
- **Complexity**: low
- **Pattern ref**: `migrations/20260422000001_alerts_table.up.sql`
- **Context refs**: "Database Schema" → esim_profile_stock
- **What**: PRIMARY KEY (tenant_id, operator_id). `available` GENERATED column. CHECK `allocated <= total`. RLS policy.
- **Verify**: `make db-migrate`; insert + query works; CHECK rejects allocated>total.

#### Task 3: Extend `chk_esim_profile_state` to include `failed`
- **Files**: Create `migrations/20260503000003_esim_profile_state_failed.up.sql` + `.down.sql`
- **Depends on**: —
- **Complexity**: low
- **Pattern ref**: `migrations/20260412000002_esim_multiprofile.up.sql` (DROP+ADD CHECK pattern)
- **Context refs**: "Database Schema" → ALTER esim_profiles
- **What**: DROP existing `chk_esim_profile_state`, ADD with `failed` value included.
- **Verify**: Manual `INSERT INTO esim_profiles (..., profile_state) VALUES (..., 'failed')` succeeds.

### Wave 2 — Backend foundation (SM-SR client + stores) — parallel
Depends on Wave 1.

#### Task 4: Create SM-SR `Client` interface
- **Files**: Create `internal/smsr/client.go`
- **Depends on**: —
- **Complexity**: medium
- **Pattern ref**: `internal/esim/smdp.go` (similar interface shape — Request/Response structs + Client interface)
- **Context refs**: "SM-SR Client Interface"
- **What**: Define `Client` interface, `CommandType`, `PushRequest`, `PushResponse`, `Health` method. Add error sentinels: `ErrSMSRConnectionFailed`, `ErrSMSRRateLimit`, `ErrSMSRRejected`.
- **Verify**: `go build ./internal/smsr/...` clean.

#### Task 5: Implement Mock SM-SR client
- **Files**: Create `internal/smsr/mock_client.go` + `internal/smsr/mock_client_test.go`
- **Depends on**: T4
- **Complexity**: medium
- **Pattern ref**: `internal/esim/smdp.go` (mock pattern in existing SMDPAdapter mock implementations)
- **Context refs**: "SM-SR Client Interface"
- **What**: `MockClient` struct implementing Client. Configurable `FailRate float64` from env `MOCK_SMSR_FAIL_RATE`. Generates `SMSRCommandID` as UUID. Records all calls in slice for test assertions.
- **Verify**: `go test ./internal/smsr/...` passes.

#### Task 6: Create `EsimOTACommandStore`
- **Files**: Create `internal/store/esim_ota.go` + `internal/store/esim_ota_test.go`
- **Depends on**: T1
- **Complexity**: high
- **Pattern ref**: `internal/store/esim.go` (ESimProfileStore structure — scanner, CRUD, RLS scoping)
- **Context refs**: "Database Schema → esim_ota_commands", "State Machine"
- **What**: Methods: `Insert(ctx, params)`, `MarkSent(id, smsr_id)`, `MarkAcked(id)`, `MarkFailed(id, err, retryable bool)`, `MarkTimeout(id)`, `IncrementRetry(id, nextRetryAt)`, `ListQueued(limit, now)` (status='queued' AND (next_retry_at IS NULL OR next_retry_at<=now)), `ListSentBefore(cutoff)` (timeout reaper), `ListByEID(eid, cursor, limit)`, `GetByID(id)`. Use parameterized queries; scope by tenant_id where applicable.
- **Verify**: Unit tests cover all transitions including invalid (e.g. acked→queued rejected at app level).

#### Task 7: Create `EsimProfileStockStore`
- **Files**: Create `internal/store/esim_stock.go` + `internal/store/esim_stock_test.go`
- **Depends on**: T2
- **Complexity**: medium
- **Pattern ref**: `internal/store/esim.go`
- **Context refs**: "Database Schema → esim_profile_stock", "Concurrent Stock Allocation Strategy"
- **What**: Methods: `Allocate(ctx, tenantID, operatorID) (Stock, error)` (atomic UPDATE+RETURNING; returns ErrStockExhausted on 0 rows), `Deallocate(...)`, `SetTotal(tenantID, operatorID, total)`, `Get(tenantID, operatorID)`, `ListSummary(tenantID)`. Define `var ErrStockExhausted = errors.New("esim_stock: exhausted")`.
- **Verify**: Concurrent Allocate test: 100 goroutines, total=50, exactly 50 succeed and 50 return ErrStockExhausted.

### Wave 3 — Workers + Endpoints (depends Wave 2)

#### Task 8: OTA Dispatcher worker
- **Files**: Create `internal/job/esim_ota_dispatcher.go` + `internal/job/esim_ota_dispatcher_test.go`
- **Depends on**: T4, T5, T6, T7
- **Complexity**: high
- **Pattern ref**: `internal/job/quota_breach_checker.go` (cron-driven processor with min-interface deps PAT-019), `internal/job/coa_failure_alerter.go` (sweep+process pattern)
- **Context refs**: "State Machine", "Rate Limiting Strategy", "API Specifications", PAT-019, PAT-024
- **What**:
  - JobType: REUSE `JobTypeOTACommand` (already in types.go).
  - Define minimal interfaces: `dispatcherCommandStore`, `dispatcherProfileStore`, `dispatcherStockStore` (PAT-019).
  - Process loop: `ListQueued(limit=batch_size)` → for each, acquire rate token (per-operator) → call `smsr.Push()` → on success: MarkSent + audit dispatch; on transient error: IncrementRetry with exp backoff (30s, 60s, 120s, 240s, 480s); on terminal (5x exceeded): MarkFailed + profile.MarkFailed + emit `esim.command.failed`.
  - Audit (AC-11): every Push attempt creates `audit_logs entity_type='esim_profile' action='ota.dispatch'`.
  - Bus emit on dispatch: `SubjectESimCommandIssued` envelope.
  - Env: `ESIM_OTA_RATE_LIMIT_PER_SEC=100`, `ESIM_OTA_BATCH_SIZE=200`, `ESIM_OTA_MAX_RETRIES=5`.
- **Verify**: Unit tests cover: success path, transient retry, terminal failure after 5x, rate limit blocking. Test asserting `audit.CreateEntry` called per dispatch.

#### Task 9: OTA Timeout Reaper worker
- **Files**: Create `internal/job/esim_ota_timeout_reaper.go` + test
- **Depends on**: T6
- **Complexity**: medium
- **Pattern ref**: `internal/job/coa_failure_alerter.go` (stuck-row sweep pattern; FIX-234 reaper)
- **Context refs**: "State Machine"
- **What**: Cron `*/2 * * * *`. `ListSentBefore(NOW - ESIM_OTA_TIMEOUT_MINUTES*min)` → for each, MarkTimeout. If retry_count<5: re-queue (status='queued', increment retry_count, set next_retry_at). If ≥5: terminal `failed` + profile.MarkFailed + audit + bus emit `SubjectESimCommandFailed`.
- **Verify**: Test inserts a row with sent_at=15 min ago → reaper transitions correctly.

#### Task 10: Refactor `bulk_esim_switch.go` to enqueue OTA commands
- **Files**: Modify `internal/job/bulk_esim_switch.go`
- **Depends on**: T6, T7
- **Complexity**: high
- **Pattern ref**: existing file is the pattern; preserve structure, lock semantics, undo records, audit emit.
- **Context refs**: "Existing infra to extend", "API Specifications → bulk-switch", "State Machine"
- **What**: Replace per-SIM `esimStore.Switch()` synchronous call with: (1) atomic stock.Allocate(target_operator_id) — abort row with ErrStockExhausted handling; (2) commandStore.Insert(command_type='switch', source_profile_id, target_profile_id|null pending allocation, target_operator_id) — single batch INSERT for the page (no N+1); (3) audit `bulk.ota_enqueue`. Switch result is later reflected by dispatcher's ACK path. Keep undo records but interpret them as "issue reverse OTA commands" (undo path also enqueues).
- **Verify**: Existing `bulk_esim_switch_test.go` updated; new test asserts no synchronous esimStore.Switch call AND that exactly N rows inserted into ota_commands for N input SIMs.

#### Task 11: Bulk-switch handler endpoint extension
- **Files**: Modify `internal/api/esim/handler.go` (add `BulkSwitch` method) + handler_test.go
- **Depends on**: T10
- **Complexity**: medium
- **Pattern ref**: `internal/api/esim/handler.go` existing `Switch` method (single profile) — JSON binding, auth, audit emit, response envelope
- **Context refs**: "API Specifications → POST /esim-profiles/bulk-switch"
- **What**: Parse one of `filter|eids|sim_ids`. Validate target_operator_id exists. Pre-check stock available (fast 422 fail-fast). Enqueue Job of type `JobTypeBulkEsimSwitch` (already exists). Return 202 with `{job_id, affected_count, mode:"ota"}`.
- **Verify**: Handler test: 202 returned with valid job_id; 422 on stockout; 400 on missing selection.

#### Task 12: Callback endpoint + HMAC verification
- **Files**: Modify `internal/api/esim/handler.go` (add `OTACallback` method) + handler_test.go
- **Depends on**: T6
- **Complexity**: high
- **Pattern ref**: `internal/notification/webhook.go` (HMAC-SHA256 ComputeHMAC/VerifyHMAC), `internal/api/esim/handler.go` (handler structure)
- **Context refs**: "HMAC Callback Verification", "API Specifications → callback"
- **What**:
  - Parse headers `X-SMSR-Signature`, `X-SMSR-Timestamp`. Reject if missing/malformed (401).
  - Replay protection: timestamp delta > 300s → 401.
  - Read raw body. Compute expected sig over `<timestamp>.<body>` with `SMSR_CALLBACK_SECRET`. `VerifyHMAC` constant-time compare → 401 on mismatch.
  - Parse JSON; lookup command by id; transition: acked → MarkAcked + apply esimStore.Switch + bus emit `SubjectESimCommandAcked` + audit `ota.callback_acked`. failed → MarkFailed + profile.MarkFailed + bus emit `SubjectESimCommandFailed` + audit `ota.callback_failed`.
  - Audit even on rejected (action=`ota.callback_rejected`).
- **Verify**: Tests: valid sig accepted; bad sig 401; timestamp drift 401; replay (same nonce twice) accepted twice (state machine handles idempotency — if already acked, return 200 no-op).

#### Task 13: Stock-summary + ota-history endpoints
- **Files**: Modify `internal/api/esim/handler.go` + handler_test.go
- **Depends on**: T6, T7
- **Complexity**: low
- **Pattern ref**: `internal/api/esim/handler.go` existing `List` (cursor pagination, response envelope)
- **Context refs**: "API Specifications → stock-summary", "API Specifications → ota-history"
- **What**: `GET /esim-profiles/stock-summary` calls `stockStore.ListSummary`, joins operator name. `GET /esim-profiles/{id}/ota-history` calls `commandStore.ListByEID`.
- **Verify**: 200 with envelope.

#### Task 14: Stock low alerter cron (PAT-024)
- **Files**: Create `internal/job/esim_stock_alert.go` + test
- **Depends on**: T7
- **Complexity**: medium
- **Pattern ref**: `internal/job/quota_breach_checker.go` lines 182-220 (`upsertBreachAlert` with `Source: "system"`, dedup_key, severity ordinals)
- **Context refs**: "AC Mapping → AC-10", PAT-024
- **What**:
  - Cron `*/15 * * * *` (env `CRON_ESIM_STOCK_ALERT="*/15 * * * *"`).
  - For each tenant, list stock summary. For each operator with `available/total < 0.10`: dedup_key=`esim_stock:{tenant_id}:{operator_id}`, severity `medium` (<10%) or `high` (<5%), `Source: "system"`, type=`esim_stock_low`.
  - When recovers ≥10%: resolve open alert by dedup key.
  - **REGRESSION TEST**: `TestESimStockAlerter_AlertSourceIsSystem` asserts every UpsertWithDedup carries `Source="system"` (PAT-024 mirror).
- **Verify**: Test passes; integration sanity insert with low stock triggers alert; restoring stock resolves it.

#### Task 15: Wire routes + cron + main.go DI
- **Files**: Modify `internal/gateway/router.go` + `cmd/argus/main.go`
- **Depends on**: T8, T9, T11, T12, T13, T14
- **Complexity**: medium (PAT-011/PAT-017 risk)
- **Pattern ref**: `cmd/argus/main.go:812-958` (quota_breach_checker DI + cron entry)
- **Context refs**: "Existing infra to extend", "API Specifications"
- **What**:
  - Routes: bulk-switch (POST), callbacks/ota-status (POST, no JWT — outside JWT group), stock-summary (GET), {id}/ota-history (GET).
  - Wire `smsrClient`, `otaCommandStore`, `stockStore` in main.go. Construct `ESimOTADispatcherProcessor`, `ESimOTATimeoutReaperProcessor`, `ESimStockAlerterProcessor`. Register cron entries: `esim_ota_dispatcher` (configurable, default `* * * * *`), `esim_ota_timeout_reaper` (`*/2 * * * *`), `esim_stock_alert` (`*/15 * * * *`).
  - **PAT-017 cross-check**: trace each new env var (SMSR_CALLBACK_SECRET, ESIM_OTA_*, MOCK_SMSR_FAIL_RATE, CRON_ESIM_STOCK_ALERT) through Config struct → main.go init → constructor parameter → field assignment → usage. All 5 hops present.
  - Add `SMSR_CALLBACK_SECRET` validation: required non-empty in production env.
- **Verify**: `go build ./...` passes; `make test` passes; manual `curl` to each new route returns expected envelope.

### Wave 4 — Frontend hooks + bulk components (parallel after Wave 3)

#### Task 16: TypeScript types + hooks
- **Files**: Modify `web/src/types/esim.ts`, modify `web/src/hooks/use-esim.ts`
- **Depends on**: T11, T12, T13
- **Complexity**: medium
- **Pattern ref**: existing `web/src/hooks/use-esim.ts` (existing hooks for List/Enable/Disable/Switch using project's data fetching pattern)
- **Context refs**: "API Specifications" (all 4 new endpoints), "AC Mapping"
- **What**: Add types `OTACommand`, `OTAStatus`, `StockSummary`, `BulkSwitchRequest`, `BulkSwitchResponse`. Add hooks: `useBulkSwitchEsim()`, `useEsimStockSummary(operatorId?)`, `useEsimOTAHistory(profileId)`. Add `formatEID(eid)` util: `${eid.slice(0,8)}...${eid.slice(-6)}`.
- **Verify**: tsc passes.

### Wave 5 — Frontend pages

#### Task 17: eSIM list page polish + bulk bar
- **Files**: Modify `web/src/pages/esim/index.tsx`
- **Depends on**: T16
- **Complexity**: high
- **Pattern ref**: `web/src/pages/sims/index.tsx` (existing bulk bar pattern from FIX-201, sticky bottom bar with selected count)
- **Context refs**: "Screen Mockups → eSIM List", "Design Token Map"
- **What**:
  - Remove "SIM ID" UUID column.
  - Make ICCID column a `<Link to="/sims/{sim_id}">`.
  - Operator column: render `operator_name` (data already enriched via FIX-219; verify field present).
  - EID column: `formatEID(eid)` + Tooltip(full EID) + CopyButton.
  - State filter: keep "Failed" option (now legitimate post-T3).
  - Add row checkbox column. Selected rows feed sticky bottom bar with [Bulk Switch Operator ▼] (opens ConfirmDialog with operator picker → calls useBulkSwitchEsim).
  - Replace "Create Profile" button with "Allocate from Stock" (T19 wires the panel).
- **Tokens**: Use ONLY classes from Design Token Map.
- **Verify**: `grep -rE '#[0-9a-fA-F]{3,8}|\[[0-9]+px\]' web/src/pages/esim/index.tsx` → ZERO matches. Manual UAT 3-way (UI + API + DB) for bulk switch flow.

#### Task 18: Operator Detail eSIM Profiles tab
- **Files**: Create `web/src/pages/operators/_tabs/esim-tab.tsx`, modify `web/src/pages/operators/detail.tsx` (add tab)
- **Depends on**: T16
- **Complexity**: medium
- **Pattern ref**: existing tabs in `web/src/pages/operators/_tabs/` (StatCard layout + filtered list, e.g. SIMs tab)
- **Context refs**: "Screen Mockups → Operator Detail eSIM tab", "Design Token Map"
- **What**: Two `<StatCard>` blocks for stock + state breakdown. Bulk Switch CTA (filter pre-set to this operator). Linked profiles list (filtered by operator_id). Tab inserted between SIMs and Audit per F-184.
- **Tokens**: zero hex/px.
- **Verify**: Browser UAT.

#### Task 19: SIM Detail eSIM Profile card + Allocate from Stock SlidePanel
- **Files**: Modify `web/src/pages/sims/esim-tab.tsx` (existing — extend), Create `web/src/components/esim/allocate-from-stock-panel.tsx`
- **Depends on**: T16
- **Complexity**: medium
- **Pattern ref**: `web/src/pages/sims/esim-tab.tsx` (existing — for shape), FIX-216 SlidePanel pattern (Option C — mentioned in CLAUDE.md decisions)
- **Context refs**: "Screen Mockups → SIM Detail eSIM card", "Design Token Map"
- **What**:
  - SIM detail eSIM card: only render when `sim.sim_type === 'esim'`. Shows EID(masked+copy+tooltip), current operator name, profile_state badge, last_provisioned. Quick actions Switch/Disable + "View History" link.
  - AllocateFromStockPanel: target SIM (pre-filled if opened from SIM detail) + operator dropdown + profile_id (optional, auto if blank). Submit calls existing `POST /esim-profiles` (Create) — no new backend route.
- **Tokens**: zero hex/px.
- **Verify**: UAT for allocation flow.

### Wave 6 — Tests + Docs (parallel)

#### Task 20: Unit + integration tests round-up
- **Files**: tests scattered (already created above per task); ensure coverage
- **Depends on**: T8, T10, T12, T14
- **Complexity**: medium
- **Pattern ref**: `internal/job/quota_breach_checker_test.go` (PAT-024 regression test pattern)
- **Context refs**: "AC Mapping", PAT-024
- **What**: Verify regression tests exist for: (a) `TestESimStockAlerter_AlertSourceIsSystem` (PAT-024), (b) callback HMAC validation (valid/invalid/replay/drift), (c) dispatcher state transitions including 5x retry terminal, (d) stock concurrent allocation, (e) bulk-switch enqueues N rows in single batch (no N+1 — assert via store call counter).
- **Verify**: `go test ./internal/job/... ./internal/store/... ./internal/api/esim/... ./internal/smsr/...` passes; coverage report shows ≥75% for new files.

#### Task 21: Integration + Load test scenarios
- **Files**: Create `internal/api/esim/integration_test.go` (build tag `integration`)
- **Depends on**: T15
- **Complexity**: high
- **Pattern ref**: existing `//go:build integration` tests in `internal/api/`
- **Context refs**: "Test Plan"
- **What**: Bulk switch 100 profiles → 100 ota_commands → mock dispatcher processes → states update → stock decremented. Load: 10K bulk insert, measure throughput. CHECK constraint regression: insert with invalid status returns 23514.
- **Verify**: `go test -tags=integration ./internal/api/esim/...` passes.

#### Task 22: PROTOCOLS.md eSIM SGP.02 chapter + ARCHITECTURE.md eSIM update
- **Files**: Modify `docs/architecture/PROTOCOLS.md`, modify `docs/ARCHITECTURE.md`
- **Depends on**: —
- **Complexity**: medium
- **Pattern ref**: existing chapters in `docs/architecture/PROTOCOLS.md` (RADIUS/Diameter/5G — section structure)
- **Context refs**: "State Machine", "SM-SR Client Interface", "HMAC Callback Verification", "API Specifications"
- **What**: New `## eSIM (SGP.02 M2M)` section: SM-SR push model, OTA command lifecycle (state machine ASCII), bulk operations flow, callback contract (HMAC algorithm + headers + replay window), rate limiting, stock allocation. ARCHITECTURE.md eSIM chapter: add TBL-25, TBL-26 references. Document the SGP.22 SMDP coexistence (kept for backward compat).
- **Verify**: Docs build clean (no broken anchors); manual review.

## Test Plan

### Unit
- SMSR mock client: success, fail rate injection, command id generation
- ESimOTACommandStore: every state transition; invalid transition rejection
- ESimProfileStockStore: concurrent Allocate (100 goroutines, total=50)
- OTA dispatcher: success path, transient retry with exp backoff, terminal after 5x, rate limit token wait
- Timeout reaper: sent>10min → timeout; retry budget honored
- Callback HMAC: valid sig, bad sig, missing header, timestamp drift, idempotent replay
- Stock alerter: <10% triggers alert with source='system' (PAT-024 regression)
- Bulk switch: enqueues N rows in single batch INSERT (assert no N+1)

### Integration (`//go:build integration`)
- DB CHECK enforcement: insert ota_command with status='bogus' → 23514 (PAT-022)
- Bulk-switch endpoint → 100 commands → mock dispatcher processes → 100 acks → stock decremented
- Callback endpoint with real HMAC sig (compute then post)
- Migration up/down round-trip clean

### Browser (UAT 3-way: API + DB + UI agreement)
- eSIM list: bulk select 5 → bulk switch → confirm dialog → toast → list refreshes → DB shows 5 ota_commands
- Operator detail eSIM tab: stock cards match `SELECT * FROM esim_profile_stock WHERE operator_id=...`
- SIM detail eSIM card: only renders for sim_type='esim'; correct operator name
- Allocate from Stock: panel submit → row appears in eSIM list with state='available'
- Failed state filter returns rows with profile_state='failed'

### Load
- 10K bulk-switch enqueue: <5s wall time
- Dispatcher throughput: ≥100 cmd/sec (default rate limit)

## Risk Mitigation

| Risk | Mitigation |
|------|------------|
| **R1 — SGP.22 callsite breakage** | Keep `internal/esim/smdp.go` and existing per-profile Enable/Disable/Switch handlers intact. New OTA pipeline is opt-in via bulk-switch endpoint only. Existing tests must pass without modification. |
| **R2 — SM-SR vendor lock-in** | `Client` interface stable; mock for dev; real vendor swap via DI. No vendor types leak into stores or handlers. |
| **R3 — 10M eUICC volume / OTA flood** | Token-bucket rate limit per operator (default 100/sec). Queue-backed; bulk surge absorbed by `esim_ota_commands`. Dispatcher batch_size capped (default 200/cron run). |
| **R4 — Callback security** | HMAC-SHA256 over `<timestamp>.<body>`; 5min replay window; constant-time compare; reject + audit on failure; secret env validated non-empty in prod. |
| **R5 — Partial bulk failure** | Per-EID OTA history endpoint exposes failure reasons; `last_error` populated; UI history view (T19) shows per-command status; failed profiles enter `profile_state='failed'` enabling filter. |
| **R6 (NEW) — PAT-024 alert source drift** | T14 explicit regression test asserts `source="system"` literal; gate scout MUST grep `Source:` in `esim_stock_alert.go`. |
| **R7 (NEW) — Stock concurrency races** | Atomic UPDATE+RETURNING with WHERE available>0; T7 includes 100-goroutine concurrent test. |
| **R8 (NEW) — Migration breaks existing CHECK** | T3 uses DROP+ADD pattern (matches existing FIX-234 migration); rollback verified manually. |

## Acceptance Criteria Mapping (full)

| Criterion | Implemented in | Verified by |
|-----------|---------------|-------------|
| AC-1 esim_ota_commands table | T1 | T20 (DB constraint) + T21 (round-trip) |
| AC-2 esim_profile_stock table | T2 | T7 unit + T21 |
| AC-3 SM-SR Client + mock | T4, T5 | T20 unit |
| AC-4 OTA dispatcher 5x retry | T8, T9 | T20 unit (state transitions, retry budget) |
| AC-5 bulk-switch endpoint | T10, T11 | T20 (no N+1) + T21 integration |
| AC-6 callback + HMAC | T12 | T20 (HMAC matrix) |
| AC-7 UI 3 places | T17, T18, T19 | UAT 3-way |
| AC-8 polish 4 sub-items | T3, T16, T17 | UAT browser |
| AC-9 Allocate from Stock | T19 | UAT |
| AC-10 stock low alert | T14 | T20 (PAT-024 regression) |
| AC-11 audit on dispatch + callback | T8, T12, T10 | T21 (audit_logs entries assertion) |
| AC-12 PROTOCOLS.md SGP.02 | T22 | manual review |

## Story-Specific Compliance Rules
- **API**: All new endpoints use standard envelope `{status, data, meta?, error?}` (CLAUDE.md convention).
- **DB**: Migrations include up/down; tenant_id-scoped queries; RLS policies on new tables.
- **Tenant scoping**: Every store query MUST scope by tenant_id (CLAUDE.md).
- **UI**: Atomic design; zero hex/px; reuse atoms from FRONTEND.md.
- **Audit**: Every state-changing op writes audit_logs entry (CLAUDE.md).
- **Pagination**: Cursor-based for ota-history list.
- **ADR-001..ADR-003**: No conflict (eSIM domain not directly addressed by ADRs).

## Bug Pattern Warnings
- **PAT-024 (FIX-246)**: Stock alerter `Source` field MUST be `"system"` (canonical CHK set). T14 includes regression test asserting this. Scout grep at gate: `rg -n 'Source:' internal/job/esim_stock_alert.go` MUST show only `"system"`.
- **PAT-022 (FIX-234)**: Two new CHECK constraints (`chk_esim_ota_command_type`, `chk_esim_ota_command_status`). Go const sets MUST mirror DB CHECK exactly. T20 includes invalid-value insert test → 23514.
- **PAT-019**: Stores accessed by workers via minimal interface — `dispatcherCommandStore`, etc. (not full struct) → unit tests inject fakes.
- **PAT-017 (FIX-210)**: Every config env var (5 new) traced through Config → constructor → field → usage in T15. Gate must verify all 5 hops.
- **PAT-011**: New cron entries explicitly added to `cmd/argus/main.go` cronScheduler.AddEntry block.

## Tech Debt (from ROUTEMAP)
No prior tech debt items target FIX-235.

## Mock Retirement
- The new `MockClient` (T5) is retained — it is the dev SM-SR. NOT a frontend mock to retire. Real SM-SR vendor integration is FUTURE scope (out of this story).

## Quality Gate Self-Check

- [x] All 12 ACs mapped to concrete files + functions
- [x] esim_ota_commands state machine documented (transitions + timeouts)
- [x] esim_profile_stock concurrent allocation safety: atomic UPDATE+RETURNING (no advisory locks); concurrent test required
- [x] SM-SR Client interface stable; mock implements all methods; future real path = swap impl
- [x] HMAC: HMAC-SHA256 over `<timestamp>.<body>`, header `X-SMSR-Signature`, secret env `SMSR_CALLBACK_SECRET`, 300s replay window
- [x] Bulk endpoint async; returns job_id; processor enqueues; no synchronous wait
- [x] Audit coverage rules clear: dispatch (T8) / callback (T12) / bulk-switch (T10) — all logged with `entity_type=esim_profile`
- [x] PAT-024 alert source = `"system"` canonical + regression test
- [x] FIX-209 alerts dedup key format: `esim_stock:{tenant_id}:{operator_id}` (mirrors quota_breach pattern)
- [x] FIX-212 event envelope: 3 new subjects `SubjectESimCommandIssued/Acked/Failed`
- [x] No N+1 in bulk: single batch INSERT in T10; T20 asserts via call counter
- [x] Frontend zero hex/px enforced via grep gate
- [x] Backwards compatibility: per-profile Enable/Disable/Switch APIs untouched; SMDPAdapter retained
- [x] Risks 1-5 from spec addressed + 3 new risks (R6-R8) added
- [x] PROTOCOLS.md eSIM section structure: SM-SR push model, lifecycle SM, callback contract, rate limit, stock allocation

**Pre-Validation: PASS** (XL: ≥120 lines ✓, ≥6 tasks ✓, all required sections ✓, embedded specs ✓, ≥1 high-complexity task ✓ (4 high), Context refs valid ✓, pattern refs present on every new-file task ✓.)

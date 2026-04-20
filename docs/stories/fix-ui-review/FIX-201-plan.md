# Implementation Plan: FIX-201 — Bulk Actions Contract Fix (Accept `sim_ids` Array)

## Goal
Extend the three SIM bulk endpoints (`state-change`, `policy-assign`, `operator-switch`) so they accept an ad-hoc `sim_ids: [uuid, ...]` array in addition to the existing `segment_id` shape, with strict tenant-isolation, per-SIM audit, CoA dispatch for policy assignment, a polished sticky bulk bar on the SIM list UI, and full test + docs coverage. Production-grade: every AC fully implemented, no half-measures.

## Architecture Context

### Components Involved

| Component | Layer | Responsibility | File |
|-----------|-------|----------------|------|
| `BulkHandler` | HTTP handler (SVC-03) | Decode + dual-shape validate + tenant-isolate + enqueue job | `internal/api/sim/bulk_handler.go` |
| `BulkStateChangeProcessor` | Job (SVC-09) | Iterate SIM IDs, distLock+TransitionState, per-SIM audit | `internal/job/bulk_state_change.go` |
| `BulkPolicyAssignProcessor` | Job (SVC-09) | Iterate SIM IDs, SetIPAndPolicy, per-SIM audit, CoA dispatch | `internal/job/bulk_policy_assign.go` |
| `BulkEsimSwitchProcessor` | Job (SVC-09) | Iterate SIM IDs, esim switch, per-SIM audit | `internal/job/bulk_esim_switch.go` |
| `SIMStore` | Store | Tenant-scoped SIM queries + new `FilterSIMIDsByTenant` helper | `internal/store/sim.go` |
| `JobStore` | Store | Job row CRUD — already supports `TotalItems` | `internal/store/job.go` |
| `audit.Auditor` | Audit service | Per-SIM audit entry via `CreateEntry` | `internal/audit/service.go` |
| `apierr` | Errors | Error code constants | `internal/apierr/apierr.go` |
| `Gateway router` | Routing/RBAC | Bulk routes + rate limit middleware | `internal/gateway/router.go` |
| SIM list page | FE | Sticky bulk bar, optimistic state, selection across filter changes | `web/src/pages/sims/index.tsx` |
| `use-sims.ts` | FE hook | Already sends `sim_ids` when no segment — verify types only | `web/src/hooks/use-sims.ts` |

### Data Flow (sim_ids branch)

```
User: selects N rows → clicks Suspend/Resume/AssignPolicy/OperatorSwitch
  → FE SlidePanel/Dialog → mutation { sim_ids: [...], target_state / policy_version_id / target_operator_id, reason? }
  → POST /api/v1/sims/bulk/{state-change|policy-assign|operator-switch}
  → BulkHandler:
      1. Kill-switch check (`bulk_operations`)
      2. Decode JSON; dual-shape validation (AC-4, AC-5)
      3. Array sanity: 1..10000 entries, each a valid UUID, list offending indices
      4. Tenant isolation: `sims.FilterSIMIDsByTenant(ctx, tenantID, ids)` — any missing/foreign → 403 FORBIDDEN_CROSS_TENANT with list
      5. Create job with `TotalItems = len(sim_ids)`, payload = {sim_ids, ...}
      6. Publish job to NATS
      7. Respond 202 {job_id, total_sims}
  → Job runner dequeues → Processor:
      For each sim_id:
        a. Distributed lock
        b. State change / policy assign / esim switch (existing calls)
        c. Emit audit row via auditor.CreateEntry (Action + before/after + CorrelationID=jobID)
        d. (policy-assign only) dispatchCoAForSIM
        e. UpdateProgress every 100 items, publish progress event
      Complete job with result + error_report
  → FE (AC-11): mutation returns job_id; selected rows show "processing" spinner; poll/refetch on job completion event; invalidate query cache
```

### API Specifications

All three endpoints: Content-Type `application/json`, JWT auth, existing per-endpoint RBAC preserved.

#### `POST /api/v1/sims/bulk/state-change`
RBAC: `sim_manager+` (unchanged).
**Request (new shape):**
```json
{ "sim_ids": ["uuid1", "uuid2", ...], "target_state": "suspended", "reason": "maintenance" }
```
**Request (legacy shape — backward compat, AC-1):**
```json
{ "segment_id": "uuid", "target_state": "suspended", "reason": "maintenance" }
```
Exactly one of `sim_ids` or `segment_id` (AC-4).
Valid `target_state`: `active | suspended | terminated | stolen_lost`.
**Success 202:**
```json
{ "status": "success", "data": { "job_id": "uuid", "total_sims": 250, "status": "queued" } }
```
**Errors:**
- 400 `INVALID_FORMAT` — JSON decode fail
- 400 `VALIDATION_ERROR` — dual-shape violation, empty array, array > 10000, invalid UUID at index, invalid target_state. Details list `{"offending_indices": [3,7]}` when array-item validation fails.
- 403 `FORBIDDEN_CROSS_TENANT` — any sim_id not owned by caller tenant. Details `{"violations": ["uuid","uuid"]}`.
- 503 `SERVICE_DEGRADED` — kill-switch active.

#### `POST /api/v1/sims/bulk/policy-assign`
RBAC: `policy_editor+` (unchanged).
**Request new shape:** `{ "sim_ids": [...], "policy_version_id": "uuid", "reason": "..." }`
**Legacy:** `{ "segment_id": "...", "policy_version_id": "...", "reason": "..." }`
Same error shape set + additional `VALIDATION_ERROR` when `policy_version_id` missing.
Response body identical to state-change response.

#### `POST /api/v1/sims/bulk/operator-switch`
RBAC: `tenant_admin` (unchanged).
**Request new shape:** `{ "sim_ids": [...], "target_operator_id": "uuid", "target_apn_id": "uuid", "reason": "..." }`
**Legacy:** `{ "segment_id": "...", "target_operator_id": "...", "target_apn_id": "...", "reason": "..." }`
Response body identical.
Note: only eSIM rows will be switched (non-eSIM in the list get a per-SIM `NOT_ESIM` error in the job's error_report — existing behaviour preserved).

### Database Schema (ACTUAL — verified against migrations)

> Source: `migrations/20260320000002_core_schema.up.sql` (lines 479-508)

```sql
CREATE TABLE IF NOT EXISTS jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    type VARCHAR(50) NOT NULL,
    state VARCHAR(20) NOT NULL DEFAULT 'queued',
    priority INTEGER NOT NULL DEFAULT 5,
    payload JSONB NOT NULL,
    total_items INTEGER NOT NULL DEFAULT 0,
    processed_items INTEGER NOT NULL DEFAULT 0,
    failed_items INTEGER NOT NULL DEFAULT 0,
    progress_pct DECIMAL(5,2) NOT NULL DEFAULT 0,
    error_report JSONB,
    result JSONB,
    max_retries INTEGER NOT NULL DEFAULT 3,
    retry_count INTEGER NOT NULL DEFAULT 0,
    retry_backoff_sec INTEGER NOT NULL DEFAULT 30,
    scheduled_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by UUID REFERENCES users(id),
    locked_by VARCHAR(100),
    locked_at TIMESTAMPTZ
);
```
Column names used by the plan: `total_items`, `processed_items`, `failed_items`, `type`, `payload`, `error_report`, `result`. **No migration needed.** Job progress API (`UpdateProgress`) already advances these.

> `audit_logs` — not duplicated here; interesting field for AC-8 is `correlation_id UUID`. Audit `Entry` struct (`internal/audit/audit.go` line 21-38) exposes `CorrelationID *uuid.UUID`. We carry `jobID` via `CorrelationID`. **No audit schema change.**

### Dual-Shape Validation Logic (AC-4, AC-5)

Pseudocode (embed in handler refactor; Developer writes real Go):

```
decode body into shared DTO: { sim_ids []uuid.UUID, segment_id uuid.UUID, + action-specific fields }

let hasSimIDs   = len(sim_ids) > 0
let hasSegment  = segment_id != uuid.Nil

// AC-4 mutual exclusion
if !hasSimIDs && !hasSegment
    400 VALIDATION_ERROR "one of sim_ids or segment_id is required"
if hasSimIDs && hasSegment
    400 VALIDATION_ERROR "sim_ids and segment_id are mutually exclusive"

// AC-5 array bounds + per-element validation
if hasSimIDs:
    if len > 10000
        400 VALIDATION_ERROR "sim_ids exceeds maximum of 10000"
    // Unmarshal into []uuid.UUID already ensures format; but when decoding into []string first, report offending_indices
    // The handler accepts string JSON and parses per-element so we can collect indices
    invalidIndices := []
    parsedIDs := []uuid.UUID{}
    for i, raw in sim_ids:
        if id, err := uuid.Parse(raw); err != nil
            invalidIndices = append(invalidIndices, i)
        else
            parsedIDs = append(parsedIDs, id)
    if len(invalidIndices) > 0
        400 VALIDATION_ERROR "invalid UUIDs in sim_ids" details={offending_indices}
```

### Tenant Isolation (AC-6) — MUST run before any job creation

New helper on `SIMStore`:
```
// FilterSIMIDsByTenant returns (ownedIDs, missingOrCrossTenantIDs, err).
// ownedIDs = those whose sims.tenant_id == tenantID.
// missingOrCrossTenantIDs = everything else (NULL row OR different tenant — same 403 because we must not reveal existence).
func (s *SIMStore) FilterSIMIDsByTenant(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) ([]uuid.UUID, []uuid.UUID, error)
```
Handler calls it BEFORE `jobs.Create`:
```
owned, violations, err := sims.FilterSIMIDsByTenant(ctx, tenantID, parsedIDs)
if len(violations) > 0
    403 FORBIDDEN_CROSS_TENANT details={violations: [...uuids]}
```
Query: `SELECT id FROM sims WHERE tenant_id = $1 AND id = ANY($2)` — then diff vs input. Chunk into 500-ID batches if input > 500 to keep pg parameters reasonable.

### Per-SIM Audit with bulk_job_id Grouping (AC-8)

- Inject `audit.Auditor` (interface `CreateEntry(ctx, p CreateEntryParams) (*Entry, error)`) into all 3 processors via a new setter method (mirrors `SetCoADispatcher`).
- On each successful SIM mutation, emit:
  ```
  auditor.CreateEntry(ctx, audit.CreateEntryParams{
      TenantID:      j.TenantID,
      UserID:        j.CreatedBy,
      Action:        "sim.state_change" | "sim.policy_assign" | "sim.operator_switch",
      EntityType:    "sim",
      EntityID:      sim.ID.String(),
      BeforeData:    json.Marshal({state: previousState}),         // action-specific
      AfterData:     json.Marshal({state: payload.TargetState, reason: payload.Reason}),
      CorrelationID: &j.ID,                                         // << bulk_job_id
  })
  ```
- `bulk_job_id` is stored via `CorrelationID` column on `audit_logs`. UI can filter audit by correlation_id to reconstruct "all audit rows from bulk job X". **No schema change.**
- Failed SIM mutations do NOT emit audit (error_report entry is the record). This keeps audit_logs to successful state transitions only.
- Hash-chain impact: `audit.ComputeHash` (`audit.go` line 75) uses TenantID, UserID, Action, EntityType, EntityID, CreatedAt, PrevHash — it does NOT hash CorrelationID, so adding it is hash-safe.

### Batching / AC-12 Interpretation

ARCHITECTURAL CONSTRAINT: the existing processors use **per-SIM distributed locking** (via `distLock.Acquire`/`Release`) wrapped around `TransitionState` / `SetIPAndPolicy` / `Switch`. Abandoning per-SIM locks to batch 100 SIMs into one SQL transaction would break the SIM lock invariant that every other flow relies on.

Interpretation of AC-12: **progress is flushed every 100 items** (already the case via `publishProgress` + `UpdateProgress`), and the 10K/30s p95 target is met via **parallelism-friendly, lock-only-when-needed iteration**. We will NOT refactor `TransitionState` or `SetIPAndPolicy` into multi-row INSERT patterns. What WE DO:

- `FilterSIMIDsByTenant` batches its lookup in chunks of 500 SIM IDs per `ANY($2)` query (like `msisdn.BulkImport:bulkImportBatchSize=500`). This saves handler latency on tenant check.
- Processors continue per-SIM loop but accept `sim_ids` directly from payload (no `ListMatchingSIMIDsWithDetails` segment resolution).
- Processors batch-fetch SIM details once: add `SIMStore.GetSIMsByIDs(ctx, tenantID, ids) ([]SIMDetail, error)` returning existing state/iccid/etc. so the loop doesn't make 10K round-trips to load SIMs one-by-one.
- Pattern ref: the store-side batch resolve already exists for segments via `segments.ListMatchingSIMIDsWithDetails` — we add the same shape for explicit IDs.

Under this interpretation, p95<30s for 10K SIMs is the target; the existing segment path hits it today. The new sim_ids path stays on the same critical path.

### Screen Mockups (SCR-080 SIM Cards List — bulk bar region)

Reference from `docs/mockups/02-sim-list.html` and current `sims/index.tsx:732-814`.

```
+------------------------------------------------------------+
| SIM Management                  [Export] [Compare] [+Import]|
+------------------------------------------------------------+
| [All SIMs ▼] [Search...] [State▼] [RAT▼] [Op▼] [APN▼]       |
+------------------------------------------------------------+
| [ ] ICCID        IMSI     MSISDN   IP     State  Op  APN ...|
| [x] 8990...001  ...       +90...   10...  [Act]  TT  iot   |
| [x] 8990...002  ...       +90...   10...  [Act]  TT  iot   |
| [x] 8990...003  ...       +90...   10...  [Sus]  VF  m2m   |
| ...                                                         |
+------------------------------------------------------------+
| ┌──────────────────────────────────────────────────────┐   |  ← bulk bar (sticky-bottom)
| │ 3 selected (47 hidden by filter) │ [Suspend][Resume] │   |     slides up w/ shadow when
| │                                  │ [Assign Policy]    │   |     selectedIds.size > 0
| │                                  │ [Reserve IPs]      │   |
| │ [Terminate]                                           │   |
| └──────────────────────────────────────────────────────┘   |
+------------------------------------------------------------+
```

**Bar behaviour (AC-10, F-106 fix):**
- When `selectedIds.size > 0` OR `selectAllSegment === true`: bar is fixed to the bottom of the viewport (within main content area, not overlapping sidebar) with `z-index` above table, drop-shadow separator.
- When cleared: bar unmounts (slide-out animation); table regains full height.
- Responsive: mobile (≤767px) — bar stays full-width, action chips wrap.
- Background: `bg-accent-dim`, top border `border-t border-accent/20`, shadow `shadow-[0_-4px_12px_rgba(0,0,0,0.35)]` (design token below).

**Selection-across-filter indicator (Risk 6):**
- Selection is a `Set<string>` keyed by SIM ID (already in state).
- On filter change, visibleSelected = selectedIds ∩ allSims; hiddenSelected = selectedIds size − visibleSelected.
- Bar text: when both nonzero — `"3 selected (1 visible, 2 hidden by filter)"`; when only hidden — `"3 selected (0 visible — adjust filters to see them)"`.

**Optimistic per-row processing (AC-11):**
- On bulk mutation fire, set `processingIds = new Set([...selectedIds])`.
- Row renders: if `processingIds.has(sim.id)` show small `<Spinner class="h-3 w-3 ml-1" />` next to state badge.
- On `onSuccess` of mutation → start polling `/api/v1/jobs/:job_id` (existing endpoint) every 2s until `state === 'completed' | 'failed'`.
- On completion: clear processingIds, invalidate SIM list query, toast summary `"Processed X, failed Y"`; if failed > 0 link to `/jobs/:id/errors`.
- On network error before job_id returned: clear processingIds and surface API error toast (existing interceptor handles it).

### Design Token Map (UI — MANDATORY)

The codebase uses Tailwind semantic classes that map to CSS variables defined in the theme. Use EXACT classes below — no hex, no `text-gray-*`, no arbitrary `[Npx]`.

#### Color Tokens (semantic Tailwind classes already present in codebase)
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Primary text | `text-text-primary` | `text-[#E4E4ED]`, `text-gray-900`, `text-white` |
| Secondary text | `text-text-secondary` | `text-[#7A7A95]`, `text-gray-500` |
| Tertiary/muted text | `text-text-tertiary` | `text-[#4A4A65]`, `text-gray-400` |
| Accent (links/CTAs) | `text-accent`, `bg-accent`, `border-accent` | `text-[#00D4FF]`, `text-cyan-400` |
| Accent dim (selected row, bulk bar bg) | `bg-accent-dim`, `border-accent/20`, `border-accent/30` | `bg-cyan-500/10` |
| Danger (terminate) | `text-danger`, `border-danger/30`, `bg-danger-dim` | `text-red-500`, `bg-red-500/10` |
| Success (healthy, active) | `text-success`, `bg-success-dim` | `text-green-500` |
| Warning | `text-warning`, `bg-warning-dim` | `text-yellow-500` |
| Card/panel bg | `bg-bg-surface`, `bg-bg-elevated` | `bg-white`, `bg-slate-900` |
| Hover bg | `bg-bg-hover` | `bg-gray-100` |
| Border | `border-border`, `border-border-subtle` | `border-[#1E1E30]`, `border-gray-700` |

#### Typography Tokens
| Usage | Class | NEVER |
|-------|-------|-------|
| Page title | `text-[16px] font-semibold text-text-primary` (matches existing SIM page) | `text-2xl` |
| Table data | `text-xs text-text-secondary` | `text-sm` |
| Mono data (ICCID/IMSI/UUID) | `font-mono text-xs text-text-secondary` | any serif/sans |
| Bulk bar label | `text-sm font-semibold text-accent` | `text-lg` |

#### Spacing / Elevation Tokens
| Usage | Class | NEVER |
|-------|-------|-------|
| Card radius | `rounded-[var(--radius-md)]` (10px) | `rounded-md`, `rounded-lg` |
| Button radius | `rounded-[var(--radius-sm)]` (6px) | `rounded` |
| Bulk bar inner padding | `px-4 py-2.5` (matches existing line 734) | arbitrary `p-[Npx]` |
| Bulk bar sticky shadow | `shadow-[0_-4px_12px_rgba(0,0,0,0.35)]` | `shadow-lg` |

#### Existing Components to REUSE (NEVER raw HTML)
| Component | Path | Use For |
|-----------|------|---------|
| `<Button>` | `web/src/components/ui/button.tsx` | All buttons — NEVER `<button>` |
| `<Input>` | `web/src/components/ui/input.tsx` | All inputs (incl. checkboxes that need styling) |
| `<Card>` | `web/src/components/ui/card.tsx` | Table wrapper |
| `<Dialog>` + `<SlidePanel>` | `web/src/components/ui/dialog.tsx`, `slide-panel.tsx` | Modal choice: confirm → Dialog, rich form → SlidePanel (FIX-216 Option C) |
| `<Spinner>` | `web/src/components/ui/spinner.tsx` | All loaders — NEVER inline SVG |
| `<Toast>` via `sonner` | `sonner` package | All success/error feedback |
| `<Badge>` | `web/src/components/ui/badge.tsx` | State chips |

**RULE for every FE task:** classes only from the table; import components only from the reuse list.

## Story-Specific Compliance Rules

- **API (envelope):** All three endpoints return `{status, data, meta?, error?}` (via `apierr.WriteSuccess` / `apierr.WriteError`).
- **API (error codes):** use constants from `apierr/apierr.go`. New constant required: `CodeForbiddenCrossTenant = "FORBIDDEN_CROSS_TENANT"` (documented in `ERROR_CODES.md` line 69 but not yet in code — T1).
- **DB:** No migration. Uses existing `jobs` and `audit_logs` tables; `correlation_id` already present.
- **Tenant scoping:** All DB queries MUST go through `TenantIDFromContext(ctx)` or explicit `tenantID` parameter (ADR-001 tenant isolation). Cross-tenant access: 403 with listed violations (ADR-001 allows explicit listing for bulk flows per ERROR_CODES.md line 69).
- **Audit:** Every state-changing per-SIM operation creates an audit log entry (ADR-003 audit requirement). Bulk operations = N entries, grouped via `correlation_id = job.id`.
- **FE design tokens:** classes only from the Design Token Map above (FRONTEND.md).
- **RBAC (unchanged):** state-change = sim_manager+, policy-assign = policy_editor+, operator-switch = tenant_admin. Set in `gateway/router.go:480-520`.
- **Rate limit (AC-14):** new `LimitBulk` middleware constant — 1 req/sec per tenant, wired in router for all 3 bulk endpoints.

## Prerequisites
- [x] FE hooks already send `sim_ids` (see `use-sims.ts:244-247` and `:270-273`) — no blocking hook change.
- [x] `jobs` table supports `total_items`, `processed_items`, `error_report` — verified against migration.
- [x] Per-SIM distLock pattern established — preserved.
- [x] CoA dispatcher wired for policy-assign — extend to sim_ids branch.
- [~] FIX-206 (orphan cleanup) — NOT a blocker; tenant-isolation logic returns violations regardless of orphan state.

## Tasks

### Task 1: Add `CodeForbiddenCrossTenant` constant + `LimitBulk` rate-limit key
- **Files:** Modify `internal/apierr/apierr.go`, `internal/apierr/apierr_test.go`, `internal/gateway/rate_limit.go` (or wherever `limitFor`/`LimitUsers` lives — grep first).
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** `internal/apierr/apierr.go:50-54` for error code constant; existing `LimitUsers` usage in `gateway/router.go:276` for rate-limit key constant.
- **Context refs:** "Story-Specific Compliance Rules", "API Specifications"
- **What:** Add `CodeForbiddenCrossTenant = "FORBIDDEN_CROSS_TENANT"` after `CodeScopeDenied`. Add `LimitBulk` constant mirroring `LimitUsers` (1 req/sec per tenant — match existing middleware contract). Extend `apierr_test.go` enumerated-codes test to include the new code.
- **Verify:** `go build ./...` passes; `go test ./internal/apierr/...` passes; grep `FORBIDDEN_CROSS_TENANT` finds the constant.

### Task 2: Add `SIMStore.FilterSIMIDsByTenant` + `GetSIMsByIDs` helpers
- **Files:** Modify `internal/store/sim.go`, Create `internal/store/sim_bulk_filter_test.go`.
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** `internal/store/sim.go:180` `GetByID(ctx, tenantID, id)` — follow same tenant-scoped query shape. Batching pattern from `internal/store/msisdn.go:175-250` `BulkImport` (chunk size 500).
- **Context refs:** "Tenant Isolation (AC-6)", "Batching / AC-12 Interpretation"
- **What:**
  - `FilterSIMIDsByTenant(ctx, tenantID, ids []uuid.UUID) (owned, violations []uuid.UUID, err error)` — query `SELECT id FROM sims WHERE tenant_id = $1 AND id = ANY($2)` in chunks of 500; diff to produce violations.
  - `GetSIMsByIDs(ctx, tenantID, ids []uuid.UUID) ([]SIMDetail, error)` (return type must cover at least `ID, ICCID, IMSI, State, PolicyVersionID, OperatorID, SimType` — fields used by the 3 processors). Tenant-scoped.
  - Unit tests: happy path, cross-tenant ID filtered out as violation, empty input returns empty slices, duplicate input IDs handled, chunk boundary at 500.
- **Verify:** `go test ./internal/store/... -run SIMStore` passes.

### Task 3: Refactor `BulkHandler.StateChange` to dual-shape + tenant check
- **Files:** Modify `internal/api/sim/bulk_handler.go`, Modify `internal/api/sim/bulk_handler_test.go`.
- **Depends on:** Task 1, Task 2
- **Complexity:** high
- **Pattern ref:** existing `StateChange` at `internal/api/sim/bulk_handler.go:208-276` for skeleton; `apierr.WriteError` for responses.
- **Context refs:** "API Specifications > POST /sims/bulk/state-change", "Dual-Shape Validation Logic", "Tenant Isolation (AC-6)", "Architecture Context > Data Flow"
- **What:**
  - New DTO with both `SimIDs []string` and `SegmentID uuid.UUID` fields.
  - Decode → validate target_state → mutual-exclusion (AC-4) → array bounds + per-UUID parse with `offending_indices` detail (AC-5) → tenant check via `FilterSIMIDsByTenant` (AC-6) → build payload `{sim_ids, target_state, reason}` when new shape else legacy `{segment_id, ...}` → `jobs.Create` with `TotalItems = len(owned)` → publish → 202 `{job_id, total_sims}`.
  - Tests (7 cases × state-change): sim_ids accepted, segment_id still accepted (backward compat), both present = 400, neither = 400, empty array = 400, 10001-item = 400, cross-tenant uuid = 403 with listed violations.
- **Verify:** `go test ./internal/api/sim/... -run TestBulkStateChange` all pass.

### Task 4: Refactor `BulkHandler.PolicyAssign` + `.OperatorSwitch` (same dual-shape pattern)
- **Files:** Modify `internal/api/sim/bulk_handler.go`, Modify `internal/api/sim/bulk_handler_test.go`.
- **Depends on:** Task 3 (establishes pattern)
- **Complexity:** medium
- **Pattern ref:** Task 3 handler, applied to `PolicyAssign` (lines 278-343) and `OperatorSwitch` (lines 345-416). Extract common validation into a private helper `parseBulkTarget(r) (ids []uuid.UUID, segmentID uuid.UUID, violations []uuid.UUID, writeErrResp func() bool)` to avoid duplication across all 3 handlers — add at end of file.
- **Context refs:** "API Specifications > POST /sims/bulk/policy-assign", "API Specifications > POST /sims/bulk/operator-switch", "Dual-Shape Validation Logic"
- **What:** apply same dual-shape validation + tenant check. Preserve action-specific required fields: `policy_version_id` (policy-assign), `target_operator_id` + `target_apn_id` (operator-switch). Payload now carries `sim_ids` OR `segment_id`. Tests: full 7×2 = 14 cases matching state-change test shape.
- **Verify:** `go test ./internal/api/sim/...` all pass.

### Task 5: Extend `BulkStateChangeProcessor` with sim_ids branch + per-SIM audit
- **Files:** Modify `internal/job/bulk_state_change.go`, Modify `internal/job/bulk_types.go`, Modify `internal/job/bulk_state_change_test.go`, Modify `cmd/argus/main.go` (inject auditor).
- **Depends on:** Task 2, Task 3
- **Complexity:** high
- **Pattern ref:** existing `processForward` at `bulk_state_change.go:65-145`; CoA injection pattern in `bulk_policy_assign.go:84-94` (`SetCoADispatcher` setter). Audit call shape: `audit.FullService.CreateEntry(ctx, CreateEntryParams)` at `internal/audit/service.go:170-200`.
- **Context refs:** "Per-SIM Audit with bulk_job_id Grouping (AC-8)", "Batching / AC-12 Interpretation", "Architecture Context > Data Flow"
- **What:**
  - Extend `BulkStateChangePayload` to include `SimIDs []uuid.UUID` alongside existing `SegmentID`.
  - In `Process`, branch: if `len(payload.SimIDs) > 0` → use `sims.GetSIMsByIDs(ctx, j.TenantID, payload.SimIDs)`; else existing segment path.
  - Add `auditor audit.Auditor` field + `SetAuditor(a)` setter. After each successful `TransitionState`, emit audit entry with `Action="sim.state_change"`, `EntityType="sim"`, `EntityID=sim.ID.String()`, `BeforeData={state: previousState}`, `AfterData={state: payload.TargetState, reason: payload.Reason}`, `CorrelationID=&j.ID`.
  - On audit error: log warning, continue (audit failures must not block state change — KVKK/SOX audit is tracked via hash-chain verify job, not per-write gating).
  - Wire auditor in `cmd/argus/main.go` where `NewBulkStateChangeProcessor` is constructed — pass the existing `audit.FullService`.
  - Tests: TestProcessForward_SimIDs_Loops (new); TestProcessForward_AuditEntryEmitted (new, fake auditor); TestProcessForward_AuditFailure_ContinuesProcessing; existing segment tests unaffected.
- **Verify:** `go test ./internal/job/... -run BulkStateChange` passes; grep audit writes in log during a 100-SIM simulated job.

### Task 6: Extend `BulkPolicyAssignProcessor` with sim_ids + audit + CoA continuity
- **Files:** Modify `internal/job/bulk_policy_assign.go`, Modify `internal/job/bulk_types.go`, Modify `internal/job/bulk_policy_assign_test.go`, Modify `cmd/argus/main.go` (auditor injection).
- **Depends on:** Task 5 (mirrors the pattern established)
- **Complexity:** high
- **Pattern ref:** Task 5 deliverable; for CoA: existing `dispatchCoAForSIM` at `bulk_policy_assign.go:199-243` — call it from the sim_ids loop exactly as the segment loop does now (line 185).
- **Context refs:** "Per-SIM Audit with bulk_job_id Grouping (AC-8)", "Hot-path CoA enforcement (AC-9)", "Architecture Context > Data Flow"
- **What:**
  - Extend `BulkPolicyAssignPayload` with `SimIDs`. Branch in `processForward` as Task 5. Reuse `dispatchCoAForSIM` — produces CoA counters on the same result shape.
  - Audit per successful `SetIPAndPolicy`: `Action="sim.policy_assign"`, `BeforeData={policy_version_id: previousPolicyID}`, `AfterData={policy_version_id: payload.PolicyVersionID}`.
  - Wire auditor via `SetAuditor`.
  - Tests: TestProcessForward_SimIDs_PolicyAssign; TestCoADispatched_ForSimIDsBranch (fake coaDispatcher counters); TestAuditEntries_Emitted; existing segment tests preserved.
- **Verify:** `go test ./internal/job/... -run BulkPolicyAssign` passes; CoA sent_count reflects mock calls.

### Task 7: Extend `BulkEsimSwitchProcessor` with sim_ids + audit
- **Files:** Modify `internal/job/bulk_esim_switch.go`, Modify `internal/job/bulk_types.go`, Modify `internal/job/` test file (create `bulk_esim_switch_test.go` if missing — confirm via glob before task runs), Modify `cmd/argus/main.go`.
- **Depends on:** Task 5
- **Complexity:** medium
- **Pattern ref:** Task 5 + existing `BulkEsimSwitchProcessor.processForward` at `bulk_esim_switch.go:59-199`.
- **Context refs:** "Per-SIM Audit with bulk_job_id Grouping (AC-8)", "Architecture Context > Data Flow"
- **What:** Extend `BulkEsimSwitchPayload` with `SimIDs`. Branch in `processForward`. Non-eSIM SIMs in input → same `NOT_ESIM` error handling (preserved). Audit per successful switch: `Action="sim.operator_switch"`, `BeforeData={operator_id: previousOperatorID, profile_id: enabledProfile.ID}`, `AfterData={operator_id: payload.TargetOperatorID, profile_id: targetProfile.ID}`. Wire auditor. Tests covering the new branch.
- **Verify:** `go test ./internal/job/... -run BulkEsimSwitch` passes.

### Task 8: Wire `LimitBulk` rate-limit middleware on 3 bulk endpoints
- **Files:** Modify `internal/gateway/router.go`.
- **Depends on:** Task 1 (adds constant)
- **Complexity:** low
- **Pattern ref:** `internal/gateway/router.go:276` — `r.With(limitFor(LimitUsers)).Post(...)` shape. Apply same wrapper to the 3 bulk POST routes at lines 499, 500, 509, 518.
- **Context refs:** "Story-Specific Compliance Rules > Rate limit"
- **What:** Wrap each bulk endpoint with `r.With(limitFor(LimitBulk))`. Leave `bulk/import` untouched (already rate-limited elsewhere or intentionally unlimited — preserve current behaviour).
- **Verify:** integration test: 2 POSTs to `/sims/bulk/state-change` within 500ms from same tenant → second returns 429 `RATE_LIMITED`.

### Task 9: FE — sticky bulk bar, selection-across-filter indicator, optimistic per-row spinner
- **Files:** Modify `web/src/pages/sims/index.tsx`, Modify `web/src/types/sim.ts` (update `BulkActionRequest` type if present — grep first). NO change to `web/src/hooks/use-sims.ts` (hooks already send `sim_ids`).
- **Depends on:** Task 3, Task 4 (backend accepts the shape before FE can exercise it end-to-end)
- **Complexity:** medium
- **Pattern ref:** existing bulk bar at `sims/index.tsx:732-814`. Existing Spinner usage at `sims/index.tsx:820`.
- **Context refs:** "Screen Mockups", "Design Token Map", "Architecture Context > Data Flow"
- **Tokens:** ONLY classes from Design Token Map — zero hardcoded hex/px.
- **Components:** `<Button>`, `<Spinner>`, `<SlidePanel>` / `<Dialog>` per FIX-216 Option C. NEVER raw `<button>`/`<input>`.
- **Note:** Invoke `frontend-design` skill during implementation for quality pass.
- **What:**
  1. **Sticky bar (AC-10):** Extract the bulk bar block (lines 732-814) into `<StickyBulkBar>` component placed OUTSIDE the `<Card>` as a fixed-position element anchored to viewport bottom within the main content column. Use `fixed bottom-0 left-[var(--sidebar-w)] right-0` with `z-30`, `bg-accent-dim`, `border-t border-accent/20`, `shadow-[0_-4px_12px_rgba(0,0,0,0.35)]`. Content table gets `pb-16` when bar is visible to avoid overlap with the last row. Animates in with `animate-in slide-in-from-bottom-2`.
  2. **Selection indicator (Risk 6):** Compute `visibleSelected = new Set([...selectedIds].filter(id => allSims.some(s => s.id === id)))`; `hiddenCount = selectedIds.size - visibleSelected.size`. Bar label: when hiddenCount > 0 → `"${selectedIds.size} selected (${visibleSelected.size} visible, ${hiddenCount} hidden by filter)"`; else existing behaviour.
  3. **Optimistic per-row processing (AC-11):** Add `const [processingIds, setProcessingIds] = useState<Set<string>>(new Set())`. On `bulkMutation.mutateAsync` call: `setProcessingIds(new Set(selectedIds))` BEFORE the promise. On success: start polling `/api/v1/jobs/${job_id}` (use new `useJobPolling(jobId, enabled)` hook — create in `web/src/hooks/use-jobs.ts` if missing) every 2s; on completion clear processingIds, refetch SIMs, toast `"${processed_count} processed, ${failed_count} failed"` (link to job-errors when failed > 0). On mutation error: clear processingIds, error toast.
  4. Row render: next to state badge, conditionally render `{processingIds.has(sim.id) && <Spinner className="h-3 w-3 ml-1 text-accent" />}`.
  5. **Type update:** If `BulkActionRequest` exists in `web/src/types/sim.ts`, update to `{ sim_ids?: string[]; segment_id?: string; target_state?: string; policy_version_id?: string; ... }` (dual-shape).
- **Verify:**
  - `cd web && npm run typecheck` passes.
  - Browser (dev-browser): select 3 rows → bar is sticky at bottom, visible; click Suspend → confirm → rows show spinner; after job completes, state chips update; filter to "suspended" → bar shows `"3 selected (3 visible, 0 hidden)"`; clear state filter + change to operator filter that excludes those 3 → `"3 selected (0 visible, 3 hidden by filter)"`.
  - `grep -r '#[0-9a-fA-F]\{3,6\}' web/src/pages/sims/index.tsx` → no hex hits.

### Task 10: API + error-code documentation
- **Files:** Modify `docs/architecture/api/_index.md`, Create `docs/architecture/api/bulk-actions.md`.
- **Depends on:** Task 3, Task 4
- **Complexity:** low
- **Pattern ref:** existing sections in `docs/architecture/api/_index.md` lines 103-106 for endpoint entry format; `docs/architecture/ERROR_CODES.md` rows for error code doc style.
- **Context refs:** "API Specifications", "Story-Specific Compliance Rules"
- **What:**
  - Create `docs/architecture/api/bulk-actions.md` with: full request/response schema for all 3 endpoints (both shapes), full error catalog, sample curl for sim_ids branch, sample curl for segment_id branch, sample 403 cross-tenant response.
  - Update `_index.md` rows 104-106 notes to reference the new doc + mention dual-shape.
  - Add `MIDDLEWARE.md` note: "Bulk SIM endpoints rate-limited at `LimitBulk` (1 req/sec per tenant) via `limitFor(LimitBulk)` middleware."
- **Verify:** docs build (if applicable); visual read to confirm examples match actual handler response shape.

### Task 11: Integration + Regression Tests
- **Files:** Create `internal/api/sim/bulk_integration_test.go` (if preferred to keep in handler package), OR modify existing `internal/job/bulk_*_test.go`. Modify `internal/gateway/router_test.go` (rate-limit integration).
- **Depends on:** Task 5, Task 6, Task 7, Task 8, Task 9
- **Complexity:** medium
- **Pattern ref:** existing processor tests in `bulk_policy_assign_test.go` — use fake stores and faker auditor for isolation.
- **Context refs:** "Per-SIM Audit with bulk_job_id Grouping (AC-8)", "API Specifications", "Architecture Context > Data Flow"
- **What:**
  - Integration: submit 100-SIM `state-change` via handler → job row created with `total_items=100` → processor executes → `processed_items=100`, 100 audit rows all with `correlation_id = jobID` (verify via fake auditor's captured entries).
  - Integration: submit `policy-assign` with `sim_ids` → CoA dispatcher mock captures N calls (1 per SIM with active session) → result JSON has `coa_sent_count=N`.
  - Rate-limit integration: 2 bulk calls within 500ms → second 429.
  - Cross-tenant: submit sim_ids with 1 foreign SIM → 403 `FORBIDDEN_CROSS_TENANT` with that UUID listed in details.
  - Regression: segment-based calls remain working (all existing tests pass without change).
- **Verify:** `make test` green; no flaky tests.

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|----------------|-------------|
| AC-1 dual-shape state-change | Task 3 | Task 3 tests, Task 11 integration |
| AC-2 dual-shape policy-assign | Task 4 | Task 4 tests, Task 11 |
| AC-3 dual-shape operator-switch | Task 4 | Task 4 tests, Task 11 |
| AC-4 mutual exclusion validation | Task 3 (shared helper), Task 4 | Task 3/4 tests |
| AC-5 array bounds + offending_indices | Task 3 | Task 3 tests |
| AC-6 tenant isolation | Task 2, Task 3, Task 4 | Task 2 unit tests, Task 3/4 handler tests, Task 11 integration |
| AC-7 job row with total_items per request | Task 3, Task 4 | Task 3/4 tests verify `jobs.Create` call + job persisted |
| AC-8 per-SIM audit + bulk_job_id grouping | Task 5, Task 6, Task 7 | Task 5/6/7 tests assert captured audit entries |
| AC-9 CoA dispatch for policy-assign sim_ids | Task 6 | Task 6 CoA test |
| AC-10 sticky bulk bar, filter-aware selection indicator | Task 9 | Task 9 browser regression |
| AC-11 optimistic per-row processing | Task 9 | Task 9 browser regression |
| AC-12 performance (10K SIMs, p95<30s) | Task 2 (GetSIMsByIDs batch-fetch), existing per-SIM loop with progress flushing every 100 | Load test (manual verification — 10K run) |
| AC-13 error_report populated with per-SIM failures | Existing code (already does this); verified preserved | Task 5/6/7 tests |
| AC-14 docs update + rate limit | Task 8, Task 10 | Task 10 visual read, Task 11 rate-limit test |

## Bug Pattern Warnings
No matching bug patterns. PAT-001..005 cover metric double-writers, polling loop deadlines, metric label fanout, goroutine cardinality, and masked-secret PATCH — none overlap with bulk handler / job processor / audit writes.

## Tech Debt (from ROUTEMAP)
No tech debt items for this story. All D-001..D-038 listed in `docs/ROUTEMAP.md` Tech Debt table are `✓ RESOLVED`.

## Mock Retirement
No mock retirement for this story. The repo has no `src/mocks/` directory — FE calls the real backend directly via axios. No FE handler replacement required; the existing hook already sends `sim_ids` when no segment.

## Risks & Mitigations

| # | Risk | Mitigation |
|---|------|------------|
| 1 | Backward-compat break for segment-based integrations | Dual-shape preserved in all 3 handlers. 7 existing `segment_id` tests remain untouched in Tasks 3/4/5/6/7. |
| 2 | Tenant isolation bypass via spoofed sim_ids | `FilterSIMIDsByTenant` runs BEFORE any mutation or job creation. Foreign + missing = both 403. No silent skip. |
| 3 | Lock contention under 10K batch | Per-SIM distLock preserved. `GetSIMsByIDs` batches the initial fetch. Progress flushed every 100 items (existing). |
| 4 | Audit flood (10K entries per bulk job) | Per-SIM audit is REQUIRED for SOX/ISO compliance — non-negotiable. Retention policy (already in place) covers growth. |
| 5 | CoA storm on bulk policy-assign | Preserved existing behaviour (fire-and-forget per SIM); FIX-234 refines. Not expanded in this story. |
| 6 | UI selection lost on filter change | Selection = `Set<string>` keyed by SIM ID (already; no change). New: show "N hidden by filter" indicator so user is not confused. |
| 7 | Stale FE callsites using old shape | Full grep over `web/src` in Task 9: only `use-sims.ts:244` and `:270` send bulk requests; both already dual-shape aware. |
| 8 | Audit write failure blocking mutation | Per-SIM audit is fire-and-log-on-error inside processor. Hash-chain integrity verified by the scheduled `audit_verify` job. |
| 9 | Rate limit breaks legitimate high-throughput automation | 1 req/sec aligns with existing `LimitUsers` posture. Enterprise integrations use segment_id (1 call covers 10K SIMs) — not throttled by this limit in practice. |
| 10 | `sim_manager` vs `policy_editor` vs `tenant_admin` RBAC drift | Router RBAC unchanged; handler-layer tenant check runs AFTER RBAC middleware. |

## Wave Plan (for Amil orchestrator)

- **Wave A (parallel):** Task 1, Task 2, Task 10 (docs may start with API spec embedded here).
- **Wave B (parallel after A):** Task 3, Task 8.
- **Wave C (after B):** Task 4.
- **Wave D (parallel after C):** Task 5, Task 6, Task 7.
- **Wave E (after C, parallel with D):** Task 9 (FE can be developed once the handler contract is in place — does not need processor completion).
- **Wave F (after D + E):** Task 11 integration.

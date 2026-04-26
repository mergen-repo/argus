# Implementation Plan: FIX-230 — Rollout DSL Match Integration

## Goal
Make the policy rollout pipeline honor the DSL `MATCH { ... }` clause when selecting SIMs to migrate AND when computing `total_sims`, so stage targets and migration semantics reflect the matching SIM cohort (not all-tenant SIMs).

## Architecture Context

### Components Involved
- **DSL package** (`internal/policy/dsl/`): adds new `sql_predicate.go` translator alongside existing `evaluator.go` (in-memory) — same AST, two backends.
- **Policy store** (`internal/store/policy.go`): `SelectSIMsForStage` gains predicate parameters; `CreateVersion` callers fetch `affected_sim_count` via predicate before insert.
- **SIM store** (`internal/store/sim.go`): new `CountWithPredicate(ctx, tenantID, predicate, args)` helper — parallel to `CountByFilters`.
- **Rollout service** (`internal/policy/rollout/service.go`): `StartRollout` replaces `CountByFilters({})` fallback with `CountWithPredicate(...)` derived from compiled DSL match. `ExecuteStage` wires DSL predicate into `SelectSIMsForStage` per AC-2 (call-site change).
- **Policy API handler** (`internal/api/policy/handler.go::CreateVersion`): after compile, computes affected count via `CountWithPredicate` and persists it (AC-4).

### Data Flow

```
User → POST /api/v1/policies/{id}/versions
  Handler: CompileSource(dsl) → compiled.Match
         → dsl.ToSQLPredicate(compiled.Match) → (sql, args)
         → simStore.CountWithPredicate(tenantID, sql, args) → affected
         → policyStore.CreateVersion(... affected_sim_count: affected)

User → POST /api/v1/policies/{id}/versions/{vid}/rollout
  Service.StartRollout:
    GetVersion → compiled DSL → dsl.ToSQLPredicate(compiled.Match)
    totalSIMs := version.AffectedSIMCount  (cached in AC-4)
    if totalSIMs <= 0 → totalSIMs := CountWithPredicate(tenantID, predicate, args)  ← was CountByFilters({})
    CreateRollout(... TotalSIMs: totalSIMs)
    ExecuteStage(...): targetMigrated = ceil(totalSIMs * pct / 100)

  Service.ExecuteStage (per batch):
    SelectSIMsForStage(tenantID, rolloutID, prevVersionID, dslPredicate, dslArgs, batchCount)
    → SQL: WHERE tenant=$1 AND state='active'
                AND id NOT IN (already-in-rollout)
                AND policy_version_id = $prev
                AND <dslPredicate>     ← NEW
            ORDER BY random() LIMIT $n FOR UPDATE SKIP LOCKED
    AssignSIMsToVersion → CoA dispatch
```

### DSL → SQL Predicate Translator Spec

**Source AST (from `internal/policy/dsl/compiler.go`)**
The compiler emits `CompiledMatch.Conditions []CompiledMatchCondition`. Each condition has:
```go
type CompiledMatchCondition struct {
    Field  string        // "apn" | "operator" | "imsi_prefix" | "rat_type" | "sim_type"
    Op     string        // "eq" | "neq" | "in"  (normalized; "=" → "eq", "IN" → "in")
    Value  interface{}   // single value (eq/neq)
    Values []interface{} // list of values (in)
}
```
Multiple match conditions are AND-joined (the in-memory `evaluator.matchesPolicy` short-circuits on any false). The translator MUST mirror this: top-level conditions joined by ` AND `.

**Note on AND/OR/NOT in MATCH:** The current compiler `compileMatch` flattens the `MatchBlock.Clauses` list — each clause becomes one `CompiledMatchCondition`, no nesting. The story AC-1 mentions "Nested AND/OR/NOT supported" — this maps to the AND-join across clauses (the only nesting MATCH supports today). The translator therefore handles the present surface; if a future grammar revision adds compound MATCH conditions, extend `translateCondition` (`*CompiledCondition` recursion already shown in `evaluator.evaluateCondition`).

**Translator API (NEW file `internal/policy/dsl/sql_predicate.go`)**
```go
// ToSQLPredicate builds a parameterized SQL WHERE fragment from a CompiledMatch.
// startArgIdx is the next $N placeholder to use; the caller appends `args` to its
// own args slice and continues numbering.
//
// Returns:
//   sqlFragment — e.g. "(s.apn_id = (SELECT id FROM apns WHERE tenant_id = $1 AND name = $3))"
//                 or "TRUE" when match is empty
//   args        — values bound to placeholders, in order
//   err         — non-nil on unknown field or unsupported operator
func ToSQLPredicate(match *CompiledMatch, tenantArgIdx int, startArgIdx int) (sqlFragment string, args []interface{}, nextArgIdx int, err error)
```

**Identifier whitelist (SQL-injection defense — AC-9)**
Allowed `Field` values:

| Field         | SQL fragment template                                                                          | Notes                                       |
|---------------|------------------------------------------------------------------------------------------------|---------------------------------------------|
| `apn`         | `s.apn_id = (SELECT id FROM apns WHERE tenant_id = $T AND name = $N)`                          | tenant-scoped sub-select                    |
| `operator`    | `s.operator_id = (SELECT id FROM operators WHERE code = $N)`                                   | operators are global (no tenant_id column)  |
| `imsi_prefix` | `s.imsi LIKE $N`                                                                               | value passed as `<prefix> + '%'`            |
| `rat_type`    | `s.rat_type = $N`                                                                              | direct string column                        |
| `sim_type`    | `s.sim_type = $N`                                                                              | direct string column                        |

For `IN` operator: emit `IN (SELECT id FROM apns WHERE tenant_id = $T AND name = ANY($N))` with `args = []interface{}{[]string{...}}` (pgx bound to `text[]`). For `imsi_prefix IN`, expand to `(s.imsi LIKE $N1 OR s.imsi LIKE $N2 ...)`. For `neq`, replace `=` with `<>` and wrap in `NOT (...)` as appropriate.

**REJECTION rules:**
- Unknown `Field` → return `err = fmt.Errorf("dsl: field %q not allowed in MATCH→SQL", field)`. NEVER concatenate the field into SQL.
- Unsupported `Op` (anything not in `eq`/`neq`/`in`) → return error.
- All literal VALUES bound via `$N` placeholders only — no `fmt.Sprintf("'%s'", val)` ever.

**Empty MATCH (AC-5):** if `match == nil` or `len(match.Conditions) == 0` → return `("TRUE", nil, startArgIdx, nil)`. Caller AND's against base predicate; effectively no narrowing.

### Updated SQL — `SelectSIMsForStage`

Source: existing `internal/store/policy.go:958`. New signature (AC-2):
```go
func (s *PolicyStore) SelectSIMsForStage(
    ctx context.Context,
    tenantID, rolloutID uuid.UUID,
    previousVersionID *uuid.UUID,
    dslPredicate string,            // NEW — "TRUE" if empty
    dslArgs []interface{},          // NEW — appended after existing args
    targetCount int,
) ([]uuid.UUID, error)
```
Generated SQL (placeholders are $1..$N where N is computed dynamically as today):
```sql
SELECT s.id FROM sims s
WHERE s.tenant_id = $1
  AND s.state = 'active'
  AND s.id NOT IN (SELECT sim_id FROM policy_assignments WHERE rollout_id = $2)
  [ AND s.policy_version_id = $3 ]    -- only if previousVersionID != nil
  AND (<dslPredicate>)                -- NEW; uses $4.. (or $3.. if no prevVer)
ORDER BY random()
LIMIT $LAST
FOR UPDATE SKIP LOCKED
```

### `CountWithPredicate` Helper (NEW in `internal/store/sim.go`)

```go
func (s *SIMStore) CountWithPredicate(
    ctx context.Context,
    tenantID uuid.UUID,
    dslPredicate string,    // "TRUE" when DSL match is empty
    dslArgs []interface{},
) (int, error)
```
Generated SQL:
```sql
SELECT COUNT(*) FROM sims s
WHERE s.tenant_id = $1
  AND s.state = 'active'
  AND (<dslPredicate>)
```
Caller responsibility: build `dslPredicate` via `dsl.ToSQLPredicate(..., tenantArgIdx=1, startArgIdx=2)` so `$1 = tenantID` and `$2..$N = dslArgs`.

### Database Schema (verified — Source: migrations/20260320000002_core_schema.up.sql)

Already present, NO new migration:
```sql
-- TBL-14: policy_versions
affected_sim_count INTEGER  -- nullable; populated by AC-4

-- TBL-10: sims (partitioned by operator_id)
tenant_id UUID NOT NULL
operator_id UUID NOT NULL
apn_id UUID
imsi VARCHAR(15) NOT NULL
rat_type VARCHAR(10)
sim_type VARCHAR(10) NOT NULL DEFAULT 'physical'
policy_version_id UUID
state VARCHAR(20) NOT NULL DEFAULT 'ordered'
-- indexes used by predicate: idx_sims_tenant_apn, idx_sims_tenant_state, idx_sims_imsi (unique on (imsi, operator_id))
-- LIKE '<prefix>%' uses the unique imsi index when anchored prefix; OK for typical 5-digit MCC+MNC

-- TBL-07: apns
id UUID PRIMARY KEY
tenant_id UUID NOT NULL
name VARCHAR(100)  -- DSL value matched against this
-- idx_apns_tenant_name (tenant_id, operator_id, name)

-- TBL-05: operators
id UUID PRIMARY KEY
code VARCHAR(20) UNIQUE  -- DSL "operator = turkcell" matches this
-- NO tenant_id on operators (global)
```

> **PAT-016 guard:** translator MUST reference `s.apn_id` against `apns.id` (NOT `policies.id`/`policy_versions.id`); `s.operator_id` against `operators.id`. Each field-template above has exact PK column embedded.

## Prerequisites
- [x] FIX-231 (version state machine) — landed (commit cebc439). `CreateVersion` already returns version with `state='draft'`; rollout service already validates state.
- [x] DSL package `internal/policy/dsl/` exists with `compiler.go`, `evaluator.go`, AST nodes (verified — see `internal/policy/dsl/ast.go:36 MatchBlock`, `compiler.go:18 CompiledMatch`).
- [x] `policy_versions.affected_sim_count` column exists (verified — migration line 169).

## Tasks

### Task 1: DSL → SQL Predicate Translator + Unit Tests
- **Files:** Create `internal/policy/dsl/sql_predicate.go`; Create `internal/policy/dsl/sql_predicate_test.go`
- **Depends on:** —
- **Complexity:** high
- **Pattern ref:** Read `internal/policy/dsl/evaluator.go` (in-memory equivalent, same AST shape). Read `internal/policy/dsl/compiler_test.go` (if present — match its `t.Run` table-driven style). Use `internal/store/sim.go::buildFleetFilterClauses` (line 1108) as the dynamic-arg-numbering pattern.
- **Context refs:** "DSL → SQL Predicate Translator Spec", "Database Schema"
- **What:**
  - Implement `ToSQLPredicate(match *CompiledMatch, tenantArgIdx int, startArgIdx int) (string, []interface{}, int, error)`.
  - Whitelist fields: `apn`, `operator`, `imsi_prefix`, `rat_type`, `sim_type`. Unknown field → error.
  - Whitelist ops: `eq`, `neq`, `in`. Unknown op → error.
  - Empty match → `"TRUE", nil, startArgIdx, nil`.
  - For `imsi_prefix`: append `'%'` to value before binding. Reject non-string values with error.
  - For `apn IN (...)`: emit `s.apn_id IN (SELECT id FROM apns WHERE tenant_id = $T AND name = ANY($N))` with `[]string{...}` arg.
  - All values via `$N` placeholders — NEVER `fmt.Sprintf` literal interpolation.
  - Multiple top-level conditions → join with ` AND `.
- **Tests** (10+ scenarios per AC-1, AC-9):
  - `apn = "data.demo"` → exact SQL fragment + args check
  - `operator IN ("turkcell", "vodafone_tr")` → `ANY($N)` with []string arg
  - `imsi_prefix = "28601"` → LIKE `'28601%'` (verify `%` appended)
  - `rat_type = "lte"`, `sim_type = "physical"`
  - Compound: two clauses → AND-joined
  - Empty match → `TRUE`
  - **Injection:** `apn = "x' OR 1=1 --"` → value reaches args slice unmodified (string), SQL fragment unchanged (no concatenation). Assert no `'` or `--` appears in returned SQL.
  - Unknown field `iccid = "..."` → error returned, NOT silently ignored.
  - `imsi_prefix = 12345` (number) → error.
  - `apn != "blocked"` → uses `<>` operator.
- **Verify:** `go test ./internal/policy/dsl/ -run SQLPredicate -v` → all pass; `grep -nE "fmt.Sprintf.*%[sd].*WHERE|%v.*=" internal/policy/dsl/sql_predicate.go` → ZERO matches (no string-formatted values into SQL).

### Task 2: `CountWithPredicate` Helper on SIMStore
- **Files:** Modify `internal/store/sim.go` (add method near `CountByFilters` at line 1111)
- **Depends on:** Task 1 (so the predicate string format is finalized)
- **Complexity:** low
- **Pattern ref:** Read `internal/store/sim.go:1111-1123` (`CountByFilters`). Mirror its structure — same `db.QueryRow(ctx, sql, args...).Scan(&count)` pattern, same error wrap style.
- **Context refs:** "`CountWithPredicate` Helper", "Database Schema"
- **What:**
  - Signature: `func (s *SIMStore) CountWithPredicate(ctx, tenantID, predicate string, args []interface{}) (int, error)`.
  - Defensive: if `predicate == ""` treat as `"TRUE"`.
  - Build full args slice: `append([]interface{}{tenantID}, args...)`.
  - SQL exactly as in plan §"`CountWithPredicate` Helper".
  - Wrap error: `fmt.Errorf("store: count sims with predicate: %w", err)`.
- **Verify:** Compile passes (`go build ./internal/store/...`). Add a focused test in `internal/store/sim_test.go` if a fixture DB pattern exists; otherwise covered by Task 5 integration test.

### Task 3: `SelectSIMsForStage` — Inject DSL Predicate
- **Files:** Modify `internal/store/policy.go` (lines 958-998)
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Existing function body lines 958-998 — preserve dynamic-`argIdx` pattern; insert predicate condition + extended args before the LIMIT placeholder.
- **Context refs:** "Updated SQL — `SelectSIMsForStage`", "DSL → SQL Predicate Translator Spec"
- **What:**
  - Extend signature exactly as in plan §"Updated SQL — `SelectSIMsForStage`".
  - Renumber: caller passes `dslPredicate` and `dslArgs` already numbered relative to the function's `argIdx` sequence — easiest pattern: caller passes raw predicate template with `$P1, $P2, ...` placeholders rewritten by the store, OR caller passes a string built with `tenantArgIdx=1` and starting `argIdx` = `nextFreeArgIdx` from the existing builder. Plan recommends: store function accepts `dslPredicate` already correctly numbered for its position AND `dslArgs` slice — store appends args in order. Caller (rollout service) must call `dsl.ToSQLPredicate(match, tenantArgIdx=1, startArgIdx=<prev_arg_count + 1>)` where prev_arg_count = 2 (no prevVer) or 3 (with prevVer). Document this contract in a Go comment above the function.
  - Append `AND (<dslPredicate>)` to the conditions slice.
  - Append `dslArgs...` to args slice BEFORE appending `targetCount`.
  - Recompute `limitPlaceholder` from `argIdx + len(dslArgs)`.
  - When `dslPredicate == ""` → treat as `"TRUE"`.
- **Verify:** All existing rollout tests still pass: `go test ./internal/store/ -run Rollout -v`. Compile passes.

### Task 4: `StartRollout` — Use `CountWithPredicate` + Wire Predicate Through `ExecuteStage`
- **Files:** Modify `internal/policy/rollout/service.go` (StartRollout @105, ExecuteStage @194)
- **Depends on:** Task 1, Task 2, Task 3
- **Complexity:** high
- **Pattern ref:** Existing `service.go:105-192` (StartRollout) and `:194-260` (ExecuteStage). Existing imports for `dsl` package — add `"argus/internal/policy/dsl"` import path (verify exact module path via `go list -m`).
- **Context refs:** "Data Flow", "Updated SQL — `SelectSIMsForStage`", "DSL → SQL Predicate Translator Spec"
- **What:**
  - In `StartRollout` after `GetVersionWithTenant`:
    - Decode `version.CompiledRules` JSON (raw `json.RawMessage`) into `dsl.CompiledPolicy{}`.
    - Build predicate: `predicate, predArgs, _, err := dsl.ToSQLPredicate(&compiled.Match, 1, 2)`. On error → return wrapped error (DSL with bad MATCH should never have compiled, defensive).
    - Replace lines 127-137 totalSIMs computation:
      - `totalSIMs := 0; if version.AffectedSIMCount != nil && *version.AffectedSIMCount > 0 { totalSIMs = *version.AffectedSIMCount }`
      - `if totalSIMs == 0 { totalSIMs, err = s.simStore.CountWithPredicate(ctx, tenantID, predicate, predArgs); if err != nil ... }`
    - Persist `predicate` and `predArgs` into the rollout context via the rollout struct (NOT DB — store on `*Service` execution map keyed by `rolloutID`, OR re-derive in `ExecuteStage` from `version.CompiledRules`). **Plan recommends re-derive** to avoid stateful caching:
      - In `ExecuteStage`, fetch the version (via `policyStore.GetVersionByID(ctx, rollout.PolicyVersionID)`), recompile predicate, pass into `SelectSIMsForStage`. One extra GET per stage execution — acceptable.
  - In `ExecuteStage` (line 223 call site):
    - Before the for-loop: derive `predicate, predArgs` from the version's `CompiledRules`.
    - Update the `SelectSIMsForStage` call to pass `predicate, predArgs` after `previousVersionID`.
- **Verify:** Existing rollout integration tests pass: `go test ./internal/policy/rollout/ -v`. Manual: insert a policy with `MATCH { apn = "data.demo" }`, start rollout 1%, assert 1 SIM migrated of the matching cohort (covered by Task 6).

### Task 5: `CreateVersion` Handler — Auto-Compute & Persist `affected_sim_count`
- **Files:** Modify `internal/api/policy/handler.go` (CreateVersion @494-585); Modify `internal/store/policy.go` (CreateVersion @327 — add optional `AffectedSIMCount *int` field to `CreateVersionParams` + insert column)
- **Depends on:** Task 1, Task 2
- **Complexity:** medium
- **Pattern ref:** Existing handler block lines 547-580. Pattern for store helper: `internal/store/policy.go:327-340`. Use the existing `userIDFromContext(r)` and tenant-from-context patterns at line 495.
- **Context refs:** "Data Flow", "DSL → SQL Predicate Translator Spec", "`CountWithPredicate` Helper"
- **What:**
  - Handler: after `compiled := ...` (line 547) and before `CreateVersion` call (line 570):
    - `predicate, predArgs, _, err := dsl.ToSQLPredicate(&compiled.Match, 1, 2)`. On error → 422 INVALID_DSL with field details.
    - `affectedCount, err := h.simStore.CountWithPredicate(r.Context(), tenantID, predicate, predArgs)`. On DB error → 500.
    - Pass `&affectedCount` into `CreateVersionParams.AffectedSIMCount` (new field).
  - Store: extend `CreateVersionParams` struct (line 76) with `AffectedSIMCount *int`. Update INSERT SQL (line 328) to include `affected_sim_count` column when non-nil. Pattern: use a conditional INSERT like the existing UPDATE at line 498.
  - Handler dependency injection: add `simStore *store.SIMStore` to `Handler` struct in `handler.go`; thread through `NewHandler` constructor. **PAT-017 guard:** verify `cmd/argus/main.go` policy handler construction site is updated to pass the existing `simStore` instance.
- **Verify:** `grep -n "h.simStore\|policyHandler\|NewHandler" cmd/argus/main.go internal/api/policy/handler.go internal/gateway/router.go` → 4 sites all coherent (struct field, constructor param, constructor body assignment, main.go construction). Integration: POST a new version with `MATCH { apn = "data.demo" }` → response has `affected_sim_count: 7` (or seed-dependent matching count).

### Task 6: Integration Test — Rollout with DSL Match
- **Files:** Create `internal/policy/rollout/dsl_match_integration_test.go`
- **Depends on:** Task 4, Task 5
- **Complexity:** medium
- **Pattern ref:** Find existing rollout integration tests via `find internal/policy/rollout -name '*_test.go'`. If a `pgxtest`/test-DB harness exists, follow its `setupTestDB` pattern. If only unit tests exist, create the first integration test using `pgxtest` with the standard migrations applied.
- **Context refs:** "Data Flow", "DSL → SQL Predicate Translator Spec"
- **What:**
  - Setup: tenant with 7 SIMs on `apn=data.demo` and 146 SIMs on other APNs (matching seed reality, 153 total).
  - Create a policy version with DSL `MATCH { apn = "data.demo" }`.
  - Assert: response `affected_sim_count == 7` (AC-4).
  - Start rollout with stages `[1, 50, 100]`.
  - Assert: rollout `total_sims == 7` (NOT 153) — AC-3.
  - Execute stage 0: `targetMigrated = ceil(7 * 1 / 100) = 1`. Assert exactly 1 SIM migrated AND that SIM has `apn=data.demo` (AC-7).
  - Regression case (AC-8): policy version with empty MATCH `{}` → `affected_sim_count == 153`, rollout migrates from full tenant pool.
  - Edge case: policy with `MATCH { operator = "turkcell" }` → uses operators sub-query.
- **Verify:** `go test ./internal/policy/rollout/ -run TestRollout_DSLMatch -v` → PASS.

### Complexity Guide
- **low:** Task 2 (CountWithPredicate helper — single SQL + scan).
- **medium:** Task 3 (SelectSIMsForStage refactor), Task 5 (handler + store wiring), Task 6 (integration test).
- **high:** Task 1 (translator + 10 tests + injection defense), Task 4 (rollout service refactor + ExecuteStage wiring).

## Acceptance Criteria Mapping
| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| AC-1 (translator + match types + AND/OR/NOT) | Task 1 | Task 1 unit tests |
| AC-2 (`SelectSIMsForStage` predicate param) | Task 3 | Task 6 integration |
| AC-3 (`totalSIMs` from DSL count) | Task 4 | Task 6 integration |
| AC-4 (`affected_sim_count` auto-populated) | Task 5 | Task 6 integration |
| AC-5 (empty MATCH → all SIMs) | Task 1, Task 4 | Task 1 + Task 6 regression |
| AC-6 (`ExecuteStage` unchanged semantics) | Task 4 (call-site only) | Task 6 |
| AC-7 (apn=data.demo → 1 of 7) | All | Task 6 |
| AC-8 (regression: no DSL still works) | Task 1 (TRUE branch), Task 4 | Task 6 regression case |
| AC-9 (SQL injection safety) | Task 1 (whitelist + params) | Task 1 injection test |

## Story-Specific Compliance Rules
- **API:** `POST /api/v1/policies/{id}/versions` continues to return standard envelope; new field `affected_sim_count` MUST appear in `data` payload (already in `toVersionResponse`).
- **DB:** No migration needed — `affected_sim_count` column pre-exists. INSERT updated to populate it.
- **Tenant scoping:** Every SQL fragment binds `tenant_id` to `$1`. The translator's `apn` template explicitly scopes the sub-select by `apns.tenant_id = $T`. **NEVER omit tenant scoping** in any new query path.
- **Code style:** Errors wrapped with `fmt.Errorf("...: %w", err)`. UUID imports via existing `"github.com/google/uuid"`.
- **No new env knobs** — translator behavior is deterministic from DSL.

## Bug Pattern Warnings
- **PAT-016 (cross-store PK confusion):** translator MUST reference `s.apn_id` against `apns.id` and `s.operator_id` against `operators.id`. The plan's identifier-whitelist table embeds the exact column references — Developer must use them verbatim, NOT introduce `policies.id` / `policy_versions.id`.
- **PAT-017 (config wiring trace):** Task 5 adds `simStore` dependency to the policy `Handler`. Developer MUST verify the full chain: `cmd/argus/main.go` instantiates `simStore` → passes into `policy.NewHandler` → struct field assigned → handler method uses it. Run `rg -n "simStore" cmd/argus/main.go internal/api/policy/handler.go` to confirm 3+ sites coherent before declaring done.
- **PAT-019 (typed-nil interface):** Low risk this story (no new interface params), but if Task 5 introduces a new interface for `simStore` injection, ensure the disabled branch passes literal `nil` (not a typed-nil pointer). Recommend: pass the concrete `*store.SIMStore` directly — no interface needed.
- **PAT-018 (default Tailwind palette):** N/A — backend-only story.

## Tech Debt (from ROUTEMAP)
No tech debt items target FIX-230 specifically. (FIX-231's state-machine tech debt was resolved in cebc439.)

## Mock Retirement
N/A — backend-only changes; no `src/mocks/` impact.

## Risks & Mitigations
- **R1 — SQL injection via DSL value:** Mitigated by Task 1 design (whitelist-only fields, all values via `$N` placeholders, fuzz/injection unit test in Task 1). Defense-in-depth: PostgreSQL prepared statements (pgx default) reject multi-statement injection regardless.
- **R2 — In-progress rollouts (mid-flight semantics change):** Existing rollouts have already-persisted `total_sims`; `ExecuteStage` reads from the row, so behavior is locked-in for in-flight rollouts. Only NEW rollouts (StartRollout) get the new semantics. Documented in story Risks; no migration of existing rows.
- **R3 — Predicate count performance:** Indexes `idx_sims_tenant_apn`, `idx_sims_tenant_state`, `idx_sims_imsi`, `idx_apns_tenant_name` all present in core schema. The `apn` sub-select uses an index lookup. `imsi_prefix LIKE '28601%'` uses anchored prefix match — efficient with the unique imsi index. AC-4 caches `affected_sim_count` per version → counted once at version create, reused at every rollout for that version.
- **R4 — Re-deriving predicate per ExecuteStage call:** Adds one `GetVersionByID` per batch loop. Acceptable: stage execution is rare (manual/timer-driven, not RADIUS-hot-path). Alternative (caching predicate on the rollout struct) introduces stateful complexity; defer to a separate optimization if perf data warrants.

## Self-Containment Check
- API specs embedded (CreateVersion handler change, predicate→count flow): YES.
- DB schema embedded (sims, apns, operators, policy_versions columns): YES, with migration source noted.
- Pattern refs on every NEW-file task: Task 1 (`evaluator.go`), Task 6 (existing rollout tests).
- Every task's Context refs point to existing plan section headers: VERIFIED.
- Embedded SQL fragments per match type: YES (identifier whitelist table).
- Function signatures specified: `ToSQLPredicate`, `CountWithPredicate`, `SelectSIMsForStage` (extended), `CreateVersionParams.AffectedSIMCount`.

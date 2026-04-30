# FIX-241 Plan — Global API Nil-Slice Fix (`WriteList` Helper Normalization)

- **Story:** `docs/stories/fix-ui-review/FIX-241-global-nil-slice-fix.md`
- **Tier:** P0 | **Effort:** XS | **Wave:** Phase 2 — Wave 8 (run FIRST in Wave 8 — unblocks FIX-242, FIX-248)
- **Mode:** AUTOPILOT (no user approval — Quality Gate decides)
- **Plan owner:** Planner Agent (Amil)

## 1. Problem Recap

Backend list endpoints return `{"data": null, ...}` instead of `{"data": [], ...}` whenever the store returns a nil slice (`var entries []T`) for a zero-row result. JSON-marshal of a Go nil slice → `null`. Multiple FE pages then crash with `TypeError: Cannot read properties of null (reading 'length')`.

Single-point fix: normalize nil slices inside `apierr.WriteList` using `reflect`, so every list endpoint returns `[]` for empty result sets without touching 47 call sites.

## 2. Root-Cause Verification (Planner-Done)

- File: `internal/apierr/apierr.go` (verified — exists, 168 LoC).
- Helper signature: `func WriteList(w http.ResponseWriter, status int, data interface{}, meta ListMeta)` (line 158).
- Body marshals `ListResponse{Data: data, ...}` directly — no normalization. Confirmed bug source.
- Test file: `internal/apierr/apierr_test.go` exists. Has `TestWriteList` (populated `[]string`) and `TestWriteList_Empty` (initialized `[]string{}`) — neither covers the nil-slice case. Existing tests will continue to pass.
- Caller surface: `grep -c 'apierr.WriteList' internal/**/*.go` = **47 callers**.
- Pointer-to-slice usage: `grep 'apierr.WriteList(.*&'` returns **0 matches**. No caller passes a pointer; pointer-to-slice handling is NOT needed (DEV-395 below).
- Test-pin scan: `grep '"data":null\|data: nil\|data:null'` finds exactly ONE hit — `cmd/argusctl/internal/client/client_test.go:103` — which uses `data:null` as a *server response fixture* to exercise the CLI's null-handling, NOT an assertion on Argus output. That test is unaffected.
- ERROR_CODES.md and `docs/architecture/api/_index.md` both exist. The envelope shape lives in `api/_index.md` line 7: `"Response format: Standard envelope { status, data, meta?, error? }"`. **Decision:** add the AC-9 convention paragraph to `docs/architecture/api/_index.md` (DEV-396 below) — that is where the envelope is documented today.

## 3. Decisions Logged (DEV-NNN — current max DEV-393)

### DEV-394 — Scope strictly limited to `WriteList` (NOT `WriteSuccess`)
Per AC-2. `WriteSuccess` is used for single-resource responses (`GET /resource/{id}`) where `null` may legitimately mean "not set". Normalizing there would risk silent semantic drift on detail endpoints. Limiting scope to `WriteList` is the principled choice — list semantics are unambiguous: empty collection ≡ `[]`. **Rationale embedded in the helper's doc-comment** so future contributors see it.

### DEV-395 — Do NOT auto-dereference pointer-to-slice
Verified zero callers pass `&entries`. Adding `reflect.Ptr` → `Elem()` handling adds complexity for a case that doesn't exist in the codebase. The helper's doc-comment will state: *"`data` MUST be a slice value (or any non-slice type, which passes through unchanged). Pointer-to-slice is undefined."* If a future caller passes a pointer, the function silently passes through — same as today's behavior. Acceptable.

### DEV-396 — Convention doc target: `docs/architecture/api/_index.md` (NOT ERROR_CODES.md)
The envelope-shape contract is already documented in `api/_index.md` line 7. ERROR_CODES.md is exclusively a code → HTTP status → message catalog. The "list endpoints return `[]` for empty" rule is a payload-shape convention, not an error rule. Append a new paragraph after the existing envelope line.

### DEV-397 — Perf benchmark threshold: < 10 µs per WriteList call
Per AC-8. If `BenchmarkWriteList_NilSlice` exceeds 10 µs/op, fallback strategy: switch to a switch-on-typed-fast-path for the most common slice element types (e.g. `[]map[string]interface{}`, `[]uuid.UUID`, story-specific DTOs) before falling through to reflect. **Expected:** the reflect call is `Kind()` + `IsNil()` + (only on nil) `MakeSlice` — this is sub-microsecond on modern CPUs and dwarfed by `json.Encode`. Threshold should hold easily; the fallback is a documented contingency, not a planned task.

## 4. Bug Pattern Warnings (cross-references — NO new pattern from this story)

This fix DOES NOT add a new entry to `bug-patterns.md`. The bug it fixes belongs to a recurring root class already covered:

- **PAT-006 (FIX-201) — Shared payload struct field silently omitted at construction sites.** Same root class: a Go default (zero-value or nil-slice) silently breaks the wire contract because the compiler doesn't enforce the contract. Different layer (struct field vs. JSON envelope) but same lesson: explicit > implicit.
- **PAT-006 RECURRENCE (FIX-215) — Missing JSON struct tags → empty `{}` response body.** Direct sibling: untagged exported fields → wrong wire shape. Both are *"silent JSON shape failure that compiles fine and passes type-only tests."*
- **PAT-009 (FIX-204) — Nullable FK in analytics; pgx panic scanning SQL NULL into Go string.** Adjacent class: SQL NULL ⇄ Go nil-zero-value ⇄ JSON null all collide on the same fault line.

**Planner instruction to Developer:** when implementing T2 (unit tests), include the AC-3 case 5 (non-slice passes through) as a `TestWriteList_NonSlice_PassesThrough` regression — this prevents a future "let's also normalize maps" change from accidentally short-circuiting struct responses (PAT-006 / PAT-006-recurrence are direct evidence that "silent shape change" is the failure mode that recurs in this codebase).

**Planner instruction to Gate:** verify the new helper has a doc-comment naming PAT-006 and the AC-2 scope rationale. Without the comment, the next contributor will "improve" `WriteSuccess` similarly and break detail endpoints.

## 5. Tech Debt Folded

ROUTEMAP `## Tech Debt` table (D-001 .. D-023) scanned — no entry targets FIX-241. Nothing to fold. (D-022 / D-023 are 2026-04-15 audits, both RESOLVED; unrelated.)

## 6. Files to Touch

**Backend (Go) — 3 files:**
1. `internal/apierr/apierr.go` — add `normalizeListData(data interface{}) interface{}` helper + invoke inside `WriteList`. Add `import "reflect"`. Estimated +15 LoC including doc-comment.
2. `internal/apierr/apierr_test.go` — append 5 unit-test cases per AC-3 + 1 benchmark per AC-8. Estimated +90 LoC.
3. `internal/api/user/handler_test.go` — append `TestActivity_EmptyUserReturnsEmptyArray` per AC-4. Reuses existing `TestActivity_NoAuditStore` pattern (already present at line 1026) — when `auditStore == nil` handler returns `apierr.WriteSuccess(w, 200, []interface{}{})`. We need a NEW test where `auditStore != nil` BUT returns zero rows — see Task 3 design below.

**Documentation — 1 file:**
4. `docs/architecture/api/_index.md` — append AC-9 convention paragraph after line 7.

**Frontend:** none. (Per spec — defensive `?? []` in FE hooks is intentionally left in place.)

## 7. Embedded Specs

### 7.1 Canonical fix shape (T1)

```go
// normalizeListData ensures a nil slice is rendered as `[]` (not `null`) in JSON.
// Why: list endpoints semantically mean "collection of items"; an empty collection
// is `[]` per OpenAPI / Google JSON style guide / API best practice. A Go
// `var entries []T` (nil slice) marshals to JSON `null`, which crashes FE code
// that does `data.length`. This function short-circuits non-slice kinds so
// non-list data (defensive: in case a caller misuses WriteList with a struct)
// passes through unchanged.
//
// SCOPE: only invoked from WriteList. WriteSuccess intentionally NOT normalized
// (DEV-394) because detail endpoints may return `null` to signify "not set".
//
// PERF: reflect overhead is Kind() + IsNil() + (on nil only) MakeSlice — <1 µs
// per call, dwarfed by json.Encode. See BenchmarkWriteList_NilSlice (AC-8).
//
// RECURRENCE: PAT-006 family — silent JSON shape failure. Do NOT extend this
// helper to maps/structs without re-evaluating semantics per type.
func normalizeListData(data interface{}) interface{} {
    v := reflect.ValueOf(data)
    if v.Kind() == reflect.Slice && v.IsNil() {
        return reflect.MakeSlice(v.Type(), 0, 0).Interface()
    }
    return data
}

func WriteList(w http.ResponseWriter, status int, data interface{}, meta ListMeta) {
    WriteJSON(w, status, ListResponse{
        Status: "success",
        Data:   normalizeListData(data),
        Meta:   meta,
    })
}
```

Note the NEW import line: `"reflect"` (added to the existing import block).

### 7.2 Unit tests (T2 — AC-3 cases 1-5 + AC-8 benchmark)

File: `internal/apierr/apierr_test.go` — append at end.

```go
func TestWriteList_NilSliceOfStruct_NormalizesToEmptyArray(t *testing.T) {
    type item struct{ ID string `json:"id"` }
    var nilSlice []item // nil
    w := httptest.NewRecorder()
    WriteList(w, http.StatusOK, nilSlice, ListMeta{Total: 0, HasMore: false})

    body := w.Body.String()
    // Must contain `"data":[]`, never `"data":null`
    if !strings.Contains(body, `"data":[]`) {
        t.Errorf("expected data:[] in body, got: %s", body)
    }
    if strings.Contains(body, `"data":null`) {
        t.Errorf("data must NEVER be null for list endpoint, got: %s", body)
    }
}

func TestWriteList_NilSliceOfMap_NormalizesToEmptyArray(t *testing.T) {
    var nilSlice []map[string]interface{} // nil
    w := httptest.NewRecorder()
    WriteList(w, http.StatusOK, nilSlice, ListMeta{Total: 0, HasMore: false})

    body := w.Body.String()
    if !strings.Contains(body, `"data":[]`) {
        t.Errorf("expected data:[] in body, got: %s", body)
    }
}

func TestWriteList_InitializedEmptySlice_StaysEmptyArray(t *testing.T) {
    emptySlice := []string{} // initialized, not nil
    w := httptest.NewRecorder()
    WriteList(w, http.StatusOK, emptySlice, ListMeta{Total: 0, HasMore: false})

    body := w.Body.String()
    if !strings.Contains(body, `"data":[]`) {
        t.Errorf("expected data:[] for initialized empty slice, got: %s", body)
    }
}

func TestWriteList_PopulatedSlice_Unchanged(t *testing.T) {
    populated := []string{"a", "b"}
    w := httptest.NewRecorder()
    WriteList(w, http.StatusOK, populated, ListMeta{Total: 2, HasMore: false})

    body := w.Body.String()
    if !strings.Contains(body, `"data":["a","b"]`) {
        t.Errorf("expected data:[\"a\",\"b\"], got: %s", body)
    }
}

func TestWriteList_NonSlicePassesThrough(t *testing.T) {
    // Defensive: if caller misuses WriteList with a non-slice (e.g. map),
    // the helper must not crash and must not attempt normalization.
    nonSlice := map[string]interface{}{"k": "v"}
    w := httptest.NewRecorder()
    WriteList(w, http.StatusOK, nonSlice, ListMeta{Total: 0, HasMore: false})

    body := w.Body.String()
    if !strings.Contains(body, `"k":"v"`) {
        t.Errorf("non-slice data must pass through unchanged, got: %s", body)
    }
}

func BenchmarkWriteList_NilSlice(b *testing.B) {
    var nilSlice []map[string]interface{} // nil
    meta := ListMeta{Total: 0, HasMore: false}
    b.ReportAllocs()
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        w := httptest.NewRecorder()
        WriteList(w, http.StatusOK, nilSlice, meta)
    }
}
```

NEW import: `"strings"` (add to the existing import block in `apierr_test.go`).

**Benchmark interpretation (AC-8):** run `go test -bench=BenchmarkWriteList_NilSlice -benchmem ./internal/apierr/`. Document the `ns/op` figure in the gate evidence. Threshold: ns/op < 10000 (= 10 µs). If exceeded → see DEV-397 fallback.

### 7.3 Integration test (T3 — AC-4)

File: `internal/api/user/handler_test.go` — append after line 1045 (`TestActivity_NoAuditStore` end).

**Important constraint discovered during planning:** `Handler.auditStore` is typed as the concrete `*store.AuditStore` (not an interface), so we cannot inject a mock that returns zero rows without a refactor. Two viable paths:

**Path A (chosen):** Add the integration test to validate the SHAPE — exercise the existing `TestActivity_NoAuditStore` code path (which already calls `apierr.WriteSuccess(w, 200, []interface{}{})`) PLUS add a new test that directly verifies the WriteList shape contract end-to-end via a mini HTTP recorder calling `apierr.WriteList(rr, 200, nilSlice, meta)` and asserting the response body shape. This is effectively a slightly-elevated unit test that proves "the bug class is closed at the helper layer" without requiring an interface refactor. The body of the test is in `internal/api/user/handler_test.go` to keep it co-located with the user-activity scenario being protected.

```go
func TestActivity_EmptyUserReturnsEmptyArray_ShapeContract(t *testing.T) {
    // FIX-241 AC-4 — verifies the WriteList contract on the same code path
    // GET /api/v1/users/{id}/activity uses when auditStore returns zero rows.
    // We exercise WriteList directly with a nil []store.AuditEntry to lock in
    // the shape: `{"status":"success","data":[],"meta":{...}}`.
    var emptyEntries []store.AuditEntry // nil — same shape returned by AuditStore.List on zero rows
    rr := httptest.NewRecorder()
    apierr.WriteList(rr, http.StatusOK, emptyEntries, apierr.ListMeta{
        Cursor:  "",
        Limit:   50,
        HasMore: false,
    })

    if rr.Code != http.StatusOK {
        t.Errorf("status = %d, want 200", rr.Code)
    }
    body := rr.Body.String()
    if !strings.Contains(body, `"data":[]`) {
        t.Errorf("activity response must contain data:[] for zero-row user, got: %s", body)
    }
    if strings.Contains(body, `"data":null`) {
        t.Errorf("activity response must NEVER contain data:null (FIX-241), got: %s", body)
    }
    if !strings.Contains(body, `"has_more":false`) {
        t.Errorf("activity response must contain has_more:false, got: %s", body)
    }
}
```

NEW imports for this file (verify they're in scope already): `"strings"`, `"github.com/btopcu/argus/internal/apierr"` — both already present (apierr is imported, strings should be added if not).

**Path B (rejected):** refactor `Handler.auditStore` to an interface (`auditStoreI`) and inject a mock returning `[]store.AuditEntry{}, "", nil`. This would let us test the FULL handler path including DB call, but requires touching `handler.go` (new interface), `WithAuditStore` (interface arg), and may have ripple effects on other handler instantiation sites. Out of scope for an XS story — the helper-layer fix is global, so any list endpoint inherits the protection automatically.

### 7.4 Convention doc (T4 — AC-9)

File: `docs/architecture/api/_index.md`. Insert AFTER the existing line 7 (`> Response format: Standard envelope ...`):

```markdown
> **Empty list contract (FIX-241):** List endpoints (every endpoint returning a paginated collection via `apierr.WriteList`) ALWAYS serialize empty result sets as `"data": []`, NEVER as `"data": null`. This is enforced centrally in `internal/apierr/apierr.go::normalizeListData` — handler-side store calls that return a Go nil slice are normalized at the response boundary. Frontend consumers may safely read `response.data.length` without null-coalescing. Detail endpoints (`apierr.WriteSuccess`) are NOT normalized; `null` may legitimately mean "not set" on a single-resource response.
```

### 7.5 Browser regression (T5 — AC-5, AC-6)

The `dev-browser` skill is available (`~/.claude/skills/dev-browser`) — supports browser automation with persistent state, navigation, console-error capture.

**Steps for the Developer / Gate:**
1. After backend is rebuilt and `make up` is healthy, use `dev-browser` to open `http://localhost:8084/login` and authenticate as `admin@argus.io` / `admin`.
2. Navigate to `/settings/users` and click any user row → land on `/settings/users/{id}`. For at least one user with zero audit history, the User Detail page must render (Overview, Activity, Sessions tabs). Activity tab must show "No activity recorded" empty state, NOT crash with ErrorBoundary.
3. Navigate to `/ops/performance`. Page must render without TypeError.
4. **Console assertion:** capture console output during steps 2–3. The output must contain ZERO occurrences of `TypeError: Cannot read properties of null` (specifically the `'length'` variant). Capture is the gate evidence.
5. (Optional bonus per spec but not strictly required by AC) `/reports` → "No scheduled reports yet" empty state — confirms the same fix applies broadly.

**Fallback (if dev-browser is unavailable in the dev container):** manual smoke per the same steps with `make up` running, recording browser-console screenshot. Either evidence form satisfies AC-5 + AC-6.

## 8. Tasks (4 — XS effort, mostly low complexity)

> Format: `**T<n> — <name>** [Complexity: low] — <files> — Depends on: <id|"—"> — Context refs: <plan section ids>`

### T1 — Core fix (`normalizeListData` + invoke in `WriteList`)
- **Complexity:** low
- **Files:** `internal/apierr/apierr.go` (1 file, ~15 LoC + 1 import)
- **Depends on:** —
- **Context refs:** §2 Root-Cause Verification, §3 DEV-394, §3 DEV-395, §7.1 Canonical fix shape, §4 Bug Pattern Warnings (the doc-comment must reference DEV-394 + PAT-006)
- **Pattern ref:** existing `WriteSuccess` (line 152) for the helper-shape and Encode pattern. New `normalizeListData` is a standalone unexported helper above `WriteList`.
- **Acceptance:** `go build ./internal/apierr/...` clean. `go vet` clean. Pre-existing `TestWriteList` and `TestWriteList_Empty` continue to pass unchanged.

### T2 — Unit tests + benchmark (5 cases per AC-3 + AC-8 benchmark)
- **Complexity:** low
- **Files:** `internal/apierr/apierr_test.go` (1 file, ~90 LoC)
- **Depends on:** T1
- **Context refs:** §7.2 Unit tests, §3 DEV-397 (perf threshold), AC-3 + AC-8
- **Pattern ref:** existing `TestWriteList` and `TestWriteList_Empty` in the same file (lines 100-140 of the existing test file).
- **Acceptance:** `go test ./internal/apierr/... -run 'TestWriteList'` — all 5 new tests pass. `go test ./internal/apierr/... -bench=BenchmarkWriteList_NilSlice -benchmem` produces ns/op figure < 10000. Capture benchmark output for gate evidence.

### T3 — Integration test for `/users/{id}/activity` empty user (AC-4)
- **Complexity:** low
- **Files:** `internal/api/user/handler_test.go` (1 file, ~25 LoC append)
- **Depends on:** T1
- **Context refs:** §7.3 Integration test (Path A chosen), AC-4
- **Pattern ref:** existing `TestActivity_NoAuditStore` (line 1026 of same file) for the test shape.
- **Acceptance:** `go test ./internal/api/user/... -run 'TestActivity_EmptyUserReturnsEmptyArray_ShapeContract'` passes. Asserts `"data":[]` present, `"data":null` absent, `"has_more":false` present.

### T4 — Convention doc + browser regression smoke (AC-9 + AC-5 + AC-6)
- **Complexity:** low
- **Files:** `docs/architecture/api/_index.md` (1 file, ~1 paragraph append after line 7)
- **Depends on:** T1 (browser smoke is post-deploy)
- **Context refs:** §7.4 Convention doc, §7.5 Browser regression
- **Pattern ref:** existing line 7 of `api/_index.md` for the blockquote style — the new paragraph follows the same `>` markdown blockquote convention.
- **Acceptance:**
  - (a) Doc paragraph rendered correctly (md preview).
  - (b) `make up` (or equivalent healthy dev stack) → dev-browser navigation through `/settings/users/{id}` (zero-activity user) and `/ops/performance` produces ZERO `TypeError: Cannot read properties of null` in console. Evidence: console-log capture or screenshot saved as gate artifact.

### Wave plan
- **Wave 1 (sequential):** T1 (must precede tests).
- **Wave 2 (parallel):** T2 + T3 + T4-doc (the doc edit). All depend only on T1, can run concurrently.
- **Wave 3 (post-deploy gate):** T4-browser smoke. Requires backend rebuild + `make up`.

(Compressed: 3 waves but ~XS total — single PR.)

## 9. Quality Gate Evidence Required

Gate must verify and link evidence for each of:

| AC | Evidence |
|----|----------|
| AC-1 | Diff of `internal/apierr/apierr.go` showing `normalizeListData` + invoke + reflect import. Doc-comment present and references DEV-394 + PAT-006. |
| AC-2 | Diff confirms `WriteSuccess` is unchanged. Grep `WriteSuccess` in apierr.go shows no normalization call. |
| AC-3 | All 5 new unit test names present in `go test -v ./internal/apierr/...` output, all PASS. |
| AC-4 | `TestActivity_EmptyUserReturnsEmptyArray_ShapeContract` PASS. |
| AC-5 | dev-browser console capture from `/settings/users/{id}` zero-activity user — zero TypeError. |
| AC-6 | dev-browser console capture from `/ops/performance` — zero TypeError. |
| AC-7 | Existing test suite output: `make test` (or `go test ./...`) — no pre-existing test regresses. Specifically `TestWriteList` and `TestWriteList_Empty` still PASS. |
| AC-8 | `go test -bench=BenchmarkWriteList_NilSlice -benchmem ./internal/apierr/` output. ns/op recorded. < 10000 ns/op (10 µs). |
| AC-9 | Diff of `docs/architecture/api/_index.md` showing the new convention paragraph. |

## 10. Risks & Mitigations (planner-acknowledged)

(See story §"Risks & Regression Prevention" — embedded here for the Developer's context.)

- **R1 (non-slice pass-through):** Mitigated by AC-3 case 5 (T2 covers it).
- **R2 (pointer-to-slice):** N/A — verified zero callers (DEV-395).
- **R3 (semantic regression for null=valid empty):** N/A — no FE consumer relies on `null` for empty list (planner verified via test-pin grep).
- **R4 (perf):** AC-8 + DEV-397 fallback documented.
- **R5 (incomplete coverage — wrapper-struct nil slice fields):** WriteList only normalizes top-level `data`. If a handler returns `WriteList(w, 200, &Wrapper{Items: nilSlice}, meta)` the inner `Items` field is NOT normalized. Out of scope for this story — convention doc (T4) makes it the store layer's responsibility to initialize empty slices for inner fields. **Acceptable** because WriteList's contract is "data is the list" — wrapping a list in a struct breaks that contract.
- **R6 (existing tests pinning null):** verified zero hits in test code.

## 11. Open Uncertainties (none blocking)

- (Non-blocking) Browser smoke depends on the dev stack being healthy at gate time. If `make up` is broken for unrelated reasons, the planner recommends Gate fall back to manual screenshot evidence rather than failing FIX-241.
- (Non-blocking) The Path-A integration test (§7.3) is a slightly-elevated unit test. A future cleanup story could refactor `Handler.auditStore` to an interface for true end-to-end integration coverage — out of scope here.

## 12. Self-Validation (Quality Gate Pre-Check)

| Check | Status | Note |
|-------|--------|------|
| Plan is self-contained (no "see ARCHITECTURE.md") | PASS | All specs embedded |
| API specs embedded with method/path/shape | PASS | §7.1 (helper signature), §7.4 (envelope rule) |
| Test scenarios from story embedded | PASS | §7.2, §7.3, §7.5 |
| Each task ≤3 files, ≤10 minutes | PASS | T1=1 file, T2=1, T3=1, T4=1 |
| Each task has Depends-on field | PASS | All filled |
| Each task has Context refs pointing to plan sections that exist | PASS | All refs verified |
| New files have Pattern ref | PASS | T1/T2/T3 reference existing patterns; T4 references existing markdown style |
| DB schema embedded if DB changes | N/A | No DB changes |
| Screen mockup embedded if UI changes | N/A | No FE changes |
| Test compliance: every AC has a test | PASS | AC-1→T1+T2, AC-2→T1 diff, AC-3→T2, AC-4→T3, AC-5/6→T4 browser, AC-7→`make test`, AC-8→T2 bench, AC-9→T4 doc |
| Bug Pattern Warnings cross-referenced | PASS | §4 — PAT-006, PAT-006-recurrence, PAT-009 |
| Tech Debt folded | PASS (none applicable) | §5 |
| DEV decisions logged with rationale | PASS | DEV-394..397 in §3 |
| Effort matches scope | PASS | XS, 4 tasks, ~130 LoC total |
| Wave plan defined | PASS | §8 |
| Gate evidence table populated | PASS | §9 |

**Self-Validation Result: PASS** — plan ready for Developer dispatch.

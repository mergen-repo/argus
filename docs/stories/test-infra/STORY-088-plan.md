# Implementation Plan: STORY-088 — [TECH-DEBT] D-033 `go vet` non-pointer `json.Unmarshal` fix

## Goal

Eliminate the single `go vet` warning that has followed the tree since STORY-024:

```
internal/policy/dryrun/service_test.go:333:30: call of Unmarshal passes non-pointer as second argument
```

After STORY-088 ships, `go vet ./...` returns exit 0 with zero warnings.

## Architecture Context

### Offending code (verified 2026-04-17)

`internal/policy/dryrun/service_test.go:327-337`:

```go
func TestIsDSLError(t *testing.T) {
    dslErr := &DSLError{Message: "test error"}
    if !IsDSLError(dslErr) {
        t.Error("IsDSLError should return true for *DSLError")
    }

    regularErr := json.Unmarshal([]byte("invalid"), nil)
    if IsDSLError(regularErr) {
        t.Error("IsDSLError should return false for non-DSLError")
    }
}
```

### Intent (reverse-engineered)

The test's purpose is to obtain ANY non-`*DSLError` error and feed it to `IsDSLError()` to confirm the predicate returns `false`. The `json.Unmarshal([]byte("invalid"), nil)` call was chosen because it reliably returns an error — but the second argument is a nil interface, which is what `go vet` flags. The returned error is `*json.InvalidUnmarshalError`, which is a legitimate non-DSL error for the test's purposes.

### Fix

Replace the nil-target call with a pointer-target call whose input is still malformed JSON, so Unmarshal returns a `*json.SyntaxError` — semantically equivalent for the test (non-DSL error), passes `go vet`.

```go
var target any
regularErr := json.Unmarshal([]byte("invalid"), &target)
```

Or more explicit:

```go
var target struct{}
regularErr := json.Unmarshal([]byte("invalid"), &target)
```

Either works. Prefer `var target any` (minimal, idiomatic for throwaway targets).

## Tasks

Single task, single file.

### Task 1: Fix the non-pointer `json.Unmarshal` call

- **What**: Change `internal/policy/dryrun/service_test.go:333` from `regularErr := json.Unmarshal([]byte("invalid"), nil)` to use a pointer target. Prefer:
  ```go
  var target any
  regularErr := json.Unmarshal([]byte("invalid"), &target)
  ```
  Keep the surrounding lines (test name, dslErr block, IsDSLError check) unchanged.
- **Files**:
  - MOD `internal/policy/dryrun/service_test.go`
- **Tests**: `TestIsDSLError` must still pass after the edit (trivial — the test's assertion is that IsDSLError returns false for a non-DSL error, which is true whether Unmarshal returns `*InvalidUnmarshalError` or `*SyntaxError`).
- **Depends on**: —

## Acceptance Criteria

- **AC-1**: `go vet ./...` returns exit code 0 with zero warnings (previous run flagged `internal/policy/dryrun/service_test.go:333`; after fix, clean).
- **AC-2**: `go test ./internal/policy/dryrun/...` passes (`TestIsDSLError` in particular).
- **AC-3**: Full test suite `go test ./...` remains at 3000+ PASS baseline (no regression).
- **AC-4**: Test intent preserved — the error returned from `json.Unmarshal` is still a non-`*DSLError` error, so `IsDSLError(regularErr)` still returns `false`.

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| AC-1 | Task 1 (pointer target) | `go vet ./...` exit 0 |
| AC-2 | Task 1 | `go test ./internal/policy/dryrun/...` |
| AC-3 | Task 1 (non-behavioural change) | `go test ./...` |
| AC-4 | Task 1 (preserved error path) | `TestIsDSLError` semantics review |

## Story-Specific Compliance Rules

- **No behaviour change** to any production code. Only the test file is modified.
- **No new third-party dependencies.** Stdlib `encoding/json` only.
- **No new tests added.** The existing `TestIsDSLError` is sufficient; its assertion semantics are preserved.
- **Minimal diff.** Two-line change (add `var target any`; change `nil` to `&target`).

## Bug Pattern Warnings

This is effectively the reverse of PAT-001/PAT-002 — a minor style drift (passing `nil` to a function that documents "must be a non-nil pointer"). Not worth a new pattern entry; the `go vet` output is the runtime warning system.

## Tech Debt (from ROUTEMAP)

This STORY resolves D-033.

## Mock Retirement

N/A.

## Risks & Mitigations

- **Risk**: changing the Unmarshal target to an `any` might not produce an error on some Go versions. **Mitigation**: `json.Unmarshal([]byte("invalid"), &any)` always returns `*json.SyntaxError` on invalid JSON across all Go versions since 1.8. Verified by Go documentation.
- **Risk**: a future reader might wonder why we decode into a discarded `any`. **Mitigation**: add a short inline comment: `// Produce a non-DSLError for the IsDSLError contract test.`

## Dependencies

None. This is a single-file, single-line-substitution fix.

## Out of Scope

- Re-architecting `IsDSLError` or `DSLError`.
- Migrating the test to `testify/require` or other helpers.
- Fixing any OTHER vet warnings (there are none at the time of writing; D-033 is the only one).

## Quality Gate (plan self-validation)

Run before dispatching Dev. Plan FAILS if any check is FALSE.

### Substance

- [x] Goal is measurable (`go vet ./...` exit 0).
- [x] Root cause identified at exact file:line.
- [x] Fix specified precisely (two-line edit).
- [x] AC mapping is concrete.

### Required Sections

- [x] `## Goal`
- [x] `## Architecture Context`
- [x] `## Tasks` (single task)
- [x] `## Acceptance Criteria`
- [x] `## Story-Specific Compliance Rules`
- [x] `## Bug Pattern Warnings` (with rationale for omission)
- [x] `## Tech Debt (from ROUTEMAP)` (resolves D-033)
- [x] `## Risks & Mitigations`
- [x] `## Dependencies`
- [x] `## Out of Scope`
- [x] `## Quality Gate (plan self-validation)`

### Embedded Specs (self-contained)

- [x] Original code excerpt included.
- [x] Proposed fix included.
- [x] Intent explained.

### Code-State Validation

- [x] Line number 333 verified against `internal/policy/dryrun/service_test.go` as of 2026-04-17.
- [x] Warning text verified via `go vet ./...` output pre-fix.
- [x] No drift from ROUTEMAP D-033 description.

### Task Decomposition

- [x] Single task, single file, two-line change. Appropriate for XS effort.

### Test Coverage

- [x] Existing `TestIsDSLError` provides full regression coverage for the fix.
- [x] AC-1 uses `go vet` as verification, not a new test. Appropriate for a vet-warning fix.

### Drift Notes

None. Problem statement from ROUTEMAP D-033 matches reality.

### Result

**PASS** — plan is self-consistent, minimum-viable, and ready for Dev dispatch.

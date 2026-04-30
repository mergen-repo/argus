# FIX-241: Global API Nil-Slice Fix — `WriteList` Helper Normalization

## Problem Statement

Multiple frontend pages crash with `TypeError: Cannot read properties of null (reading 'length')` because backend endpoints return `{data: null}` instead of `{data: []}` for empty list results. The root cause is a single code pattern in `internal/apierr/apierr.go::WriteList` — it passes the `data` parameter through to JSON marshaling without normalizing nil slices. In Go, a nil slice (`var entries []X`) marshals to JSON `null`, while an initialized empty slice (`entries := []X{}`) marshals to `[]`.

**Concrete crash evidence (verified 2026-04-19):**
- `/settings/users/{id}` (User Detail) — `/api/v1/users/{id}/activity` returns `data: null` → FE `activity.length` → TypeError → ErrorBoundary (F-243)
- `/ops/performance` — `/api/v1/ops/metrics/snapshot` has at least one nil slice field → FE `.length` → crash (F-277)
- `/reports/scheduled` — returns `data: null` for empty list (F-328)
- `/roaming-agreements` — same pattern (before F-238 removal)
- Potentially: other list endpoints that haven't been hit with empty data yet (latent bugs)

**This is a single-point global fix that unblocks multiple critical UI crashes.** One file change, tested across the entire API surface.

## User Story

As a platform operator, I want every list endpoint in Argus to return a consistent JSON representation for empty lists (`[]` never `null`), so that no frontend page crashes with null-reference errors when data is absent.

## Architecture Reference

- **Package:** `internal/apierr` (API response helpers)
- **Affected callers:** Every handler that calls `apierr.WriteList()` — ~60+ call sites across `internal/api/*`
- **Upstream root cause:** Store layer `List(...)` functions that return `var results []T` (nil slice on zero rows) without `make([]T, 0)` initialization. The fix centralizes normalization at the API response boundary.
- **Pattern comparison:** `internal/api/ippool/handler.go:420` already uses defensive `items := make([]addressResponse, 0, len(addresses))` — that handler is immune. This FIX applies the same defense globally at the helper layer.

## Screen Reference

Multiple pages affected by the downstream crashes:
- `/settings/users/{id}` (User Detail) — SCR-User-Detail
- `/ops/performance` — SCR-Ops-Performance
- `/reports` (Scheduled Reports list) — SCR-Reports
- Any future list endpoint with empty results (latent coverage)

## Findings Addressed

- **F-243** — User Detail page crash (`null.length` on activity endpoint)
- **F-277** — Ops/Performance page crash (same pattern)
- **F-302** — Global nil-slice pattern analysis (this story is the fix)
- **F-328** — `/reports/scheduled` returns `data: null`

## Acceptance Criteria

- [ ] **AC-1:** `apierr.WriteList(w, status, data, meta)` normalizes nil slices: before JSON marshal, if `data` is a nil slice (any element type), replace with an empty slice of the same type. Implementation uses `reflect.Value.IsNil()` + `reflect.MakeSlice(v.Type(), 0, 0).Interface()`. Non-slice types pass through unchanged.

- [ ] **AC-2:** `apierr.WriteSuccess(w, status, data)` NOT modified — this helper is used for non-list responses (single objects, operation results). Scope of the fix is deliberately limited to `WriteList` to avoid breaking semantic null responses (e.g., `GET /resource/{id}` where null may legitimately mean "not set").

- [ ] **AC-3:** Unit test `TestWriteList_NormalizesNilSlice` added to `internal/apierr/apierr_test.go` with three cases:
  1. Nil `[]struct` → JSON `{"data": [], ...}`
  2. Nil `[]map[string]interface{}` → JSON `{"data": [], ...}`
  3. Initialized empty `[]T{}` → JSON `{"data": [], ...}` (unchanged)
  4. Non-nil populated `[]T` → JSON `{"data": [...], ...}` (unchanged)
  5. Non-slice `map[string]interface{}` → passes through (no normalization attempted)

- [ ] **AC-4:** Integration test: `GET /api/v1/users/{id}/activity` for a user with ZERO audit entries returns `{"status":"success","data":[],"meta":{...}}` (not `data: null`). Test added to `internal/api/user/handler_test.go`.

- [ ] **AC-5:** Browser regression test: `/settings/users/{id}` loads without TypeError in console. User Detail tabs (Overview, Activity, Sessions) render correctly for users with empty activity history. Verified with dev-browser snapshot.

- [ ] **AC-6:** Browser regression test: `/ops/performance` loads without TypeError. Performance metrics render cleanly. Verified with dev-browser snapshot.

- [ ] **AC-7:** No behavioral change for populated lists — existing tests that assert specific list content must continue to pass unchanged.

- [ ] **AC-8:** Performance: Reflect-based normalization overhead measured and documented. Target: < 10 µs per WriteList call (negligible vs. JSON marshal cost). If measurement exceeds this threshold, switch to interface-based detection (optional type assertion for `[]T` where T is known).

- [ ] **AC-9:** `docs/architecture/ERROR_CODES.md` OR `docs/architecture/API.md` gains a short section: "List endpoints always return an array, never null. Empty result sets are serialized as `[]`."

## Pre-conditions / Dependencies

- **None.** This story has no dependencies — it is a foundation fix that unblocks others.
- **Unblocks:** FIX-242 (Session Detail DTO) will populate new fields whose downstream behavior depends on consistent list shape. FIX-248 (Reports) depends on this for scheduled_reports list rendering.

## Files to Touch

**Backend (Go):**
- `internal/apierr/apierr.go` — add `normalizeListData` helper + invoke in `WriteList`
- `internal/apierr/apierr_test.go` — add 5 test cases per AC-3
- `internal/api/user/handler_test.go` — integration test per AC-4 (may already exist, extend)

**Frontend (no changes):** The fix is backend-only. FE code that defensively handled null (e.g., `data ?? []` coalescing in some hooks) will continue to work — they were patching symptoms, now the source is fixed. Do not refactor FE defensive code in this story — keep scope minimal.

**Documentation:**
- `docs/architecture/ERROR_CODES.md` or `docs/architecture/API.md` — AC-9 convention note

## Risks & Regression Prevention

### Risk 1: Breaking endpoints that return non-slice `data`
- **Scenario:** `WriteList` is called (or miscalled) with a map, struct, or scalar in `data`. The reflect-based normalization must short-circuit for non-slice kinds.
- **Prevention:** Unit test AC-3 case 5 (non-slice passes through unchanged). `reflect.Value.Kind() == reflect.Slice` check precedes any normalization.

### Risk 2: Pointer-to-slice edge case
- **Scenario:** Handler passes `&entries` (pointer to slice) instead of `entries`. Reflect would see `Kind() == reflect.Ptr`, not `Slice`.
- **Prevention:** `WriteList` contract in its doc comment states `data` must be a slice value, not a pointer. Existing callers pass values (`entries`), not pointers. Add test case if needed.

### Risk 3: Semantic regression — null WAS valid
- **Scenario:** Some consumer (external API integration, script) depends on receiving `null` for "empty" to distinguish from "unset"/"not applicable".
- **Mitigation:** List endpoints semantically mean "collection of items"; empty is `[]`, never `null`. This is API best practice (OpenAPI spec, Google JSON style guide). No legitimate consumer should rely on null for empty list.
- **Verification:** Search codebase for explicit `data === null` checks in FE — if any rely on null, refactor to `.length === 0` (defensive anyway).

### Risk 4: Performance regression
- **Scenario:** Reflect is slower than direct type check; high-traffic endpoints (e.g., `/dashboard` 30s polling) accumulate latency.
- **Mitigation:** AC-8 benchmark. If overhead is material, optimization path: registered type assertion fast-path for common slice types.

### Risk 5: Incomplete coverage — stores with non-nil-but-empty slice bug
- **Scenario:** Some store returns `[]T{}` but wrapped in another struct that has nil slice field. `WriteList` only normalizes top-level `data`.
- **Prevention:** AC-8 benchmark is a smoke test. Future finding: document convention for store layer — every `List` function MUST return initialized empty slice `make([]T, 0)`, not nil. This can be a follow-up lint rule.

### Risk 6: Global helper behavior change impact on existing tests
- **Scenario:** Existing integration tests that explicitly asserted `data: null` for empty lists will fail.
- **Mitigation:** Grep for `"data":null` pattern in test assertions. If any test pins null-for-empty, it was asserting a bug — update to `[]`.

## Test Plan

### Unit Tests (internal/apierr/apierr_test.go)
```go
func TestWriteList_NilSliceNormalized(t *testing.T) {
    // 5 cases per AC-3
}
```

### Integration Tests (internal/api/user/handler_test.go)
```go
func TestActivityEndpoint_EmptyUserReturnsEmptyArray(t *testing.T) {
    // POST /api/v1/users with fresh user (0 activity)
    // GET /api/v1/users/{id}/activity
    // assert response.data is [] array, not null
    // assert response.meta.has_more == false
}
```

### Browser Regression (dev-browser)
Using `skills/dev-browser`:
1. Navigate to `/settings/users/{id}` for a user with 0 activity entries — page loads, Activity tab shows "No activity recorded".
2. Navigate to `/ops/performance` — dashboard renders without crash.
3. Navigate to `/reports` — scheduled reports section renders "No scheduled reports yet" empty state.
4. Check browser console — 0 errors (specifically no `TypeError: Cannot read properties of null`).

### Regression Test Suite
- `make test` — all Go tests pass
- `cd web && npm run typecheck` — no TS errors
- Visual inspection across 5+ list pages (SIMs, Sessions, Policies, Violations, Notifications) — all render correctly.

## Rollout

- **Size:** XS (effort: < 30 LoC change)
- **Deploy risk:** LOW (single file, unit-tested, backward compatible for all legitimate consumers)
- **Feature flag:** Not needed
- **Rollback plan:** Revert single commit; behavior returns to prior (null for empty). FE crashes return but not a production blocker — this is a pure improvement.

## Plan Reference

- **Plan:** `docs/reviews/ui-review-remediation-plan.md` → "Phase 2 Review Additions" → "P0 Global Pattern / Critical Bugs"
- **Priority:** P0 (global crash unblock)
- **Effort:** XS (1-line core change + tests + docs)
- **Wave:** Wave 8 — recommended to run FIRST in Phase 2 execution (unblocks FIX-242 and others)

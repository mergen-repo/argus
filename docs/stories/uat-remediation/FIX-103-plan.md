# FIX-103 Plan: Tenant List sim_count/user_count Always Zero

**Effort:** S | **Risk:** Low | **Architecture Guard:** N/A (pre-release)

## Root Cause

`toTenantResponse()` in `internal/api/tenant/handler.go` hardcodes `SimCount: 0, UserCount: 0` (lines 104-105). The `List` handler calls `tenantStore.List()` which only queries `tenants` table columns -- counts are never fetched or joined. The `Get` and `Stats` handlers already call `GetStats()` and populate counts correctly, so AC-4 and AC-5 are already passing.

## Aggregation Decision (AC-3)

**Choice: LATERAL subqueries** in the existing `SELECT ... FROM tenants` query.

Rationale:
- No migration needed (no new columns, no triggers)
- Composes cleanly with existing cursor pagination (`WHERE id < $n`) and state filter
- Denormalized counters would require migration + trigger maintenance -- exceeds S effort
- GROUP BY + JOIN would require restructuring the cursor/limit logic
- Tenant count is small (typically < 100); LATERAL overhead is negligible

## Filter Consistency Note

`GetStats` excludes `sims.state != 'purged'`, `users.state != 'terminated'`, `apns.state != 'archived'`. The new LATERAL subqueries MUST apply the same filters to ensure AC-5 consistency (List agrees with Stats).

## AC-7: Frontend

No frontend change required. `web/src/types/settings.ts` Tenant interface already defines `sim_count`/`user_count`, and `web/src/pages/system/tenants.tsx` already renders them (lines 213, 216). The UI will display correct values once the API returns them.

## Tasks

### T1: Store — `ListWithCounts` method

**File:** `internal/store/tenant.go`

- Add `TenantWithCounts` struct (embeds `Tenant` + `SimCount int`, `UserCount int`)
- Add `ListWithCounts(ctx, cursor, limit, stateFilter)` method that uses the same query structure as `List` but adds two LATERAL subqueries:
  ```sql
  SELECT t.*, sc.cnt AS sim_count, uc.cnt AS user_count
  FROM tenants t
  LEFT JOIN LATERAL (
      SELECT COUNT(*) AS cnt FROM sims WHERE tenant_id = t.id AND state != 'purged'
  ) sc ON true
  LEFT JOIN LATERAL (
      SELECT COUNT(*) AS cnt FROM users WHERE tenant_id = t.id AND state != 'terminated'
  ) uc ON true
  WHERE ...
  ORDER BY t.created_at DESC, t.id DESC
  LIMIT $n
  ```
- Preserve cursor pagination and state filter logic exactly as in `List`

### T2: Handler — Wire `ListWithCounts` into `List` handler

**File:** `internal/api/tenant/handler.go`

- Update `List` handler to call `tenantStore.ListWithCounts()` instead of `tenantStore.List()`
- Update `toTenantResponse` (or add a `toTenantWithCountsResponse` variant) to populate `SimCount` and `UserCount` from the store result instead of hardcoded 0
- Confirm `Get`, `Stats`, `Update` handlers remain untouched

### T3: Tests — Store unit + regression

**Files:** `internal/store/tenant_test.go`, `internal/api/tenant/handler_test.go`

- Store unit test: seed N tenants with varied SIM/user counts (including a tenant with 0 sims) -> call `ListWithCounts` -> assert counts match
- Handler integration test: call `GET /api/v1/tenants` -> verify response includes correct `sim_count`/`user_count` per tenant
- **Test authoring note (AC-6):** The spec says "match `SELECT COUNT(*) FROM sims GROUP BY tenant_id` exactly" -- but the implementation filters `state != 'purged'`. Seed fixtures must contain no purged sims (which is the standard case), or the test assertion must apply the same filter. Prefer no-purged-sims in fixtures for simplicity.

## Out of Scope

- Refactoring `GetStats` (5 sequential COUNTs per call) -- separate follow-up if desired
- Performance benchmarking beyond correctness
- Denormalized counter triggers

## Bug Pattern Candidate

New pattern for `bug-patterns.md` at Gate:
> PAT-006: DTO field hardcoded to zero/empty when data source was never wired up. Prevention: check every literal in `to*Response` builders against the struct field's semantic meaning.

## Files Affected

| File | Change |
|------|--------|
| `internal/store/tenant.go` | Add `TenantWithCounts` struct + `ListWithCounts` method |
| `internal/api/tenant/handler.go` | Wire `ListWithCounts`, populate counts in response |
| `internal/store/tenant_test.go` | Store unit test for counts |
| `internal/api/tenant/handler_test.go` | Handler integration test |

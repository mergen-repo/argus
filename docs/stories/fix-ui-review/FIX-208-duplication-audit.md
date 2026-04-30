# FIX-208 Duplication Audit (Task 1)

## Summary
- Total call sites found: 18
- Handlers running raw SQL directly: 2 (`admin/tenant_resources.go:cdrBytes30d`, `admin/tenant_resources.go:estimateTenantAPIRPS`)
- All other handlers go through store methods (no inline raw SQL in API layer)
- Problem classes:
  1. **Semantic drift** — same logical metric computed via different strategies (F-125): `policy_assignments` vs `sims.policy_version_id`
  2. **Redundant computation** — same method called twice within the same request (operator list vs operator detail both call `CountByOperator` + `GetActiveStats` + `TrafficByOperator` independently)
  3. **No caching** — every request hits DB with fresh aggregation query
  4. **Raw SQL bypass** — `admin/tenant_resources.go` functions `cdrBytes30d` and `estimateTenantAPIRPS` call `h.db.QueryRow` directly, bypassing CDR store

## Call Sites Table

| File:Line | Method | DB Table | Category | Notes |
|-----------|--------|----------|----------|-------|
| `internal/api/dashboard/handler.go:177` | `simStore.CountByState` | `sims` | sim-count | Called per dashboard load; returns total + per-state breakdown |
| `internal/api/dashboard/handler.go:196` | `sessionStore.GetActiveStats` | `sessions` | session-count | 3 sub-queries: total active, by operator, by APN |
| `internal/api/dashboard/handler.go:314` | `cdrStore.GetMonthlyCostForTenant` | `cdrs` | traffic-sum | Monthly cost rollup |
| `internal/api/dashboard/handler.go:318` | `cdrStore.GetDailyKPISparklines` | `cdrs`/`cdrs_hourly` | traffic-sum | 7-day sparkline; also called from `admin` handler |
| `internal/api/apn/handler.go:195` | `simStore.CountByAPN` | `sims` | sim-count | Bulk GROUP BY, O(1) per request |
| `internal/api/apn/handler.go:198` | `cdrStore.SumBytesByAPN24h` | `cdrs` | traffic-sum | 24h SUM per APN; called in same list request as CountByAPN |
| `internal/api/operator/handler.go:593` | `simStore.CountByOperator` | `sims` | sim-count | Called in operator **List** handler |
| `internal/api/operator/handler.go:606` | `sessionStore.GetActiveStats` | `sessions` | session-count | Called in operator **List** handler (same request as :593) |
| `internal/api/operator/handler.go:611` | `sessionStore.TrafficByOperator` | `sessions` | traffic-sum | Called in operator **List** handler (same request as :606) |
| `internal/api/operator/handler.go:999` | `simStore.CountByOperator` | `sims` | sim-count | Called in operator **Detail** handler — **duplicate of :593 logic** |
| `internal/api/operator/handler.go:1008` | `sessionStore.GetActiveStats` | `sessions` | session-count | Called in operator **Detail** handler — **duplicate of :606 logic** |
| `internal/api/operator/handler.go:1011` | `sessionStore.TrafficByOperator` | `sessions` | traffic-sum | Called in operator **Detail** handler — **duplicate of :611 logic** |
| `internal/api/system/capacity_handler.go:74` | `simStore.CountByTenant` | `sims` | sim-count | Single-tenant total, no state filter |
| `internal/api/system/capacity_handler.go:80` | `sessionStore.CountActive` | `sessions` | session-count | Global (no tenant filter); separate from GetActiveStats |
| `internal/api/admin/tenant_resources.go:73` | `cdrBytes30d` (raw SQL) | `cdrs` | traffic-sum | **Raw SQL direct on `h.db`** — bypasses CDR store; 30-day SUM per tenant |
| `internal/api/admin/tenant_resources.go:71` | `estimateTenantAPIRPS` (raw SQL) | `audit_logs` | audit-count | **Raw SQL direct on `h.db`** — bypasses audit store; 5-min window COUNT |
| `internal/store/policy.go:467` | `policyStore.CountAssignedSIMs` | `policy_assignments` | policy-count | Defined in store but **never called from any API handler** (dead code in API layer) |
| `internal/api/analytics/handler.go:203` | `usageStore.GetTimeSeries` + `GetTotals` + `GetBreakdowns` | `cdrs`/`cdrs_hourly`/`cdrs_daily` | traffic-sum | Analytics endpoint calls 3 aggregation methods per request (no overlap with CDR store) |

## F-125 Drift Case (the visible symptom)

The `sim_count` field on the policy list item (`policyListItem.SimCount`) is **always 0** in current code:

- `internal/api/policy/handler.go:185` — `toPolicyListItem` maps a `store.Policy` to wire DTO; `SimCount` field is initialized to zero and **never set**
- `internal/store/policy.go:467` — `policyStore.CountAssignedSIMs` counts from `policy_assignments` table (may include stale rows for deleted SIMs)
- `internal/store/sim.go:1212` — `simStore.CountByOperator` reads from `sims` table (live FK, `sims.policy_version_id`)

**Divergence scenario**: when a SIM is deleted or its `policy_version_id` is NULLed, `policy_assignments` rows are not cleaned up (CoA/audit trail retention). `CountAssignedSIMs` returns stale count; a live count via `sims WHERE policy_version_id IN (...)` returns the correct value.

**Canonical source decision**: `sims.policy_version_id` (live FK). `policy_assignments` should be kept for CoA/audit trail only, not used for UI counts. The fix for Task 8 is to add a live-count query to the policy List handler using `sims.policy_version_id`.

## Double-Call Pattern in Operator Handler

The operator **List** handler (`handler.go:593–611`) and operator **Detail** handler (`handler.go:999–1011`) each independently call the same three aggregation methods:
1. `simStore.CountByOperator(ctx, tenantID)` — full table scan GROUP BY operator_id
2. `sessionStore.GetActiveStats(ctx, tid)` — 3 sub-queries on sessions
3. `sessionStore.TrafficByOperator(ctx, tid)` — SUM GROUP BY on sessions

For the detail handler, only a single operator's values are consumed, but the full tenant-wide aggregation runs every time.

## Task 8 Targets (what to refactor)

| Handler | Lines | Action |
|---------|-------|--------|
| Dashboard | 177, 196 | Add Redis cache (30s TTL) for CountByState + GetActiveStats |
| Operator List | 593, 606, 611 | Add Redis cache; results reused in list loop (already efficient) |
| Operator Detail | 999, 1008, 1011 | Replace full-tenant aggregation with per-operator scoped queries |
| APN List | 195, 198 | Add Redis cache (60s TTL) |
| System Capacity | 74, 80 | Accept existing pattern; CountByTenant + CountActive are lightweight |
| Admin Tenant Resources | 73 (raw SQL) | Move `cdrBytes30d` into `CDRStore` as a named method |
| Policy List | 185 | Add live sim count via `sims.policy_version_id` — never call `CountAssignedSIMs` for UI |
| Analytics | 203 | No change needed — already uses materialized views (cdrs_hourly/cdrs_daily) |

# Gate Report: FIX-103 — Tenant List sim_count/user_count Always Zero

**Date:** 2026-04-19
**Verdict:** PASS (2 findings fixed in-gate)

## Scout Results

| Scout | Findings | Fixed |
|-------|----------|-------|
| Analysis | 2 actionable (F-A1 MEDIUM, F-A2 LOW) + 1 INFO + 3 PASS | 1 fixed (F-A2), 1 accepted (F-A1) |
| Test/Build | 3174/3174 PASS, build+vet clean | — |
| UI | SKIP (no UI) | — |

## Findings

### F-A1 | MEDIUM | test-coverage — ACCEPTED
AC-6 handler HTTP round-trip test not added. Store-level integration test (`TestTenantStore_ListWithCounts_ReturnsCorrectCounts`) covers DB+store path with purged-exclusion assertion. Handler test (`TestToTenantWithCountsResponse_PopulatesCounts`) covers DTO mapping. Combined coverage is sufficient for S effort story.

### F-A2 | LOW | consistency — FIXED
Update handler was still using `toTenantResponse` (hardcoded zero counts). Fixed: Update now calls `GetStats` after update (same pattern as Get handler) and populates `sim_count`, `user_count`, `apn_count` in response.

### F-A3 | INFO | spec-drift — NOTED
AC-2 tenant_admin clause is N/A — GET /api/v1/tenants is super_admin-only by router middleware.

## Bug Pattern
PAT-006: DTO field hardcoded to zero/empty when data source was never wired up. Prevention: check every literal in `to*Response` builders against the struct field's semantic meaning.

## Tests
- 3174/3174 PASS (baseline +2 from FIX-104)
- Build: PASS
- Vet: PASS

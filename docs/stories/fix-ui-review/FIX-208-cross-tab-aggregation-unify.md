# FIX-208: Cross-Tab Data Aggregation Unify

## Problem Statement
Same logical metric (e.g., "SIM count per operator") is computed differently across UI tabs, producing inconsistent numbers:
- Policy Versions tab: SIM count from `sims.policy_version_id`
- Policy List page: SIM count from `policy_assignments`
- Dashboard: from `anomalies` join
- Sessions stats: from `sessions` table

Each surface queries DB directly with slightly different filter → drift. F-125 is most visible symptom (list 0 vs detail 10 SIMs).

## User Story
As an operator, I want identical metrics to show identical numbers across every UI surface so I trust the data.

## Architecture Reference
- Canonical aggregator service: new `internal/analytics/aggregates/` package
- All handlers consume same service, not raw SQL

## Findings Addressed
F-95, F-96, F-65, F-51, F-24, F-25

## Acceptance Criteria
- [ ] **AC-1:** New `analytics.Aggregates` service exposing: `SIMCountByOperator(tenantID)`, `SIMCountByPolicy(tenantID, policyID)`, `SessionCountByAPN(...)`, `TrafficByOperator(...)`, etc.
- [ ] **AC-2:** Every handler that previously executed its own count query now calls `Aggregates.X()`.
- [ ] **AC-3:** Caching layer — Redis TTL 60s default, invalidated on write events via NATS (`sim.updated`, `policy.changed`).
- [ ] **AC-4:** Audit: list every aggregate query duplication in codebase; consolidate into service.
- [ ] **AC-5:** FE sanity test — open same metric on 3+ pages (Dashboard, Analytics, Operator Detail) → identical number.
- [ ] **AC-6:** Performance — aggregation cached, p95 < 50ms per call (on hit).

## Files to Touch
- `internal/analytics/aggregates/service.go` (NEW)
- `internal/analytics/aggregates/cache.go` (NEW)
- `internal/api/dashboard/handler.go`, `policy/handler.go`, `session/handler.go`, `apn/handler.go` — use service
- Tests: `aggregates_test.go`

## Risks & Regression
- **Risk 1 — Cache stale on writes:** AC-3 NATS invalidation. Fallback: TTL enforces refresh.
- **Risk 2 — Per-tenant cache keys:** ensure keys include tenant_id to prevent leaks.
- **Risk 3 — Large-scale aggregation cost:** 10M SIMs count query needs index (`sims.operator_id`, `sims.policy_version_id`).

## Test Plan
- Unit: service returns consistent numbers across invocations
- Integration: write SIM → cache invalidation → next read returns updated count
- Browser: Dashboard + Analytics show same operator SIM count

## Plan Reference
Priority: P0 · Effort: L · Wave: 2

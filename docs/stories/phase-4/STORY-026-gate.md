# Gate Report: STORY-026 — Steering of Roaming (SoR) Engine

**Date:** 2026-03-21
**Phase:** 4
**Result:** PASS

## Summary

SoR engine within SVC-06 (Operator Router) implementing IMSI-prefix routing, RAT-type preference, cost-based operator selection, Redis caching, circuit breaker integration, and NATS health event subscription for cache invalidation. Backend-only story (Pass 6 SKIPPED).

## Test Results

| Suite | Tests | Result |
|-------|-------|--------|
| `internal/operator/sor/...` | 16 (21 incl. subtests) | ALL PASS |
| Full suite (`go test ./...`) | 34 packages | ALL PASS, 0 failures |
| Build (`go build ./...`) | - | CLEAN |
| Vet (`go vet ./...`) | - | CLEAN |

## Pass 1: Requirements Tracing (11 ACs)

| # | Acceptance Criterion | Status | Implementation |
|---|---------------------|--------|----------------|
| 1 | IMSI-prefix routing: map IMSI prefix ranges to preferred operator(s) | PASS | `engine.go:filterByIMSIPrefix` + `matchIMSIPrefix` — MCC+MNC string prefix comparison |
| 2 | SoR routing table stored in Redis for O(1) lookup | PASS | `cache.go:SoRCache` with `sor:result:{tenant_id}:{imsi}` keys |
| 3 | RAT-type preference: configurable preference order | PASS | `types.go:SoRConfig.RATPreferenceOrder`, `DefaultRATPreferenceOrder`, `engine.go:sortCandidates` uses `bestRATRank` |
| 4 | RAT-type awareness: only route to operators supporting requested RAT | PASS | `engine.go:filterByRAT` filters candidates by supported RAT types |
| 5 | Cost-based selection: prefer lowest cost_per_mb | PASS | `engine.go:sortCandidates` sorts by cost_per_mb ASC as tertiary criterion |
| 6 | SoR priority: explicit priority field on operator_grants | PASS | Migration adds `sor_priority INTEGER NOT NULL DEFAULT 100`, `store/operator.go:OperatorGrant.SoRPriority` |
| 7 | SoR invoked on: (1) initial auth, (2) failover | PASS | `engine.go:Evaluate` — single entry point; circuit breaker filtering excludes downed operators for failover scenario |
| 8 | SoR result cached per SIM in Redis (TTL configurable, default 1h) | PASS | `cache.go:Set` with configurable TTL, default 1h in `DefaultConfig` |
| 9 | SoR override: per-SIM operator lock bypasses SoR | PASS | `engine.go:checkOperatorLock` reads `sims.metadata.operator_lock`, returns immediately with `ReasonManualLock` |
| 10 | SoR decision logged in session record | PASS | `store/session_radius.go:SoRDecision json.RawMessage` field, migration adds `sor_decision JSONB` to sessions table |
| 11 | Bulk re-evaluation: trigger SoR recalculation for segment | PASS | `engine.go:BulkRecalculate` invalidates all tenant cache entries; `subscriber.go` handles NATS health events + cache invalidation |

## Pass 2: Compliance

| Check | Status | Detail |
|-------|--------|--------|
| Package naming | PASS | `internal/operator/sor/` — Go camelCase, correct sub-package under SVC-06 |
| Layer separation | PASS | Engine uses `GrantProvider` interface for store access; no direct DB calls from SoR package |
| Migration format | PASS | `20260321000001_sor_fields.up.sql` / `.down.sql` — correct naming convention |
| Migration up/down symmetry | PASS | Up adds 3 columns + index + session column; down drops all in reverse order |
| Tenant scoping | PASS | `ListGrantsWithOperators` scoped by `tenant_id = $1`; cache keys include tenant_id |
| Parameterized queries | PASS | All SQL uses `$N` placeholders, no string interpolation in queries |
| Error wrapping | PASS | All errors wrapped with `fmt.Errorf("sor: ...: %w", err)` pattern |
| Interface-based DI | PASS | `CircuitBreakerChecker` and `GrantProvider` interfaces for testability |

## Pass 2.5: Security

| Check | Status | Detail |
|-------|--------|--------|
| SQL injection | PASS | No raw SQL in SoR package; store queries use parameterized `$N` |
| Input validation | PASS | IMSI length check (`len(imsi) < 5`), UUID parse validation for operator_lock |
| Redis key injection | PASS | Cache keys use `uuid.UUID.String()` (safe) + IMSI (alphanumeric by protocol) |
| Error information leakage | PASS | Errors contain internal identifiers only (tenant_id, operator_id), no user-facing exposure |
| Nil safety | PASS | Nil checks on cache, cbCheck, SimMetadata, CostPerMB pointer |

## Pass 3: Tests

16 test functions (21 assertions with subtests) covering all 9 story test scenarios:

| Test | Story Scenario | Status |
|------|---------------|--------|
| `TestSoR_IMSIPrefixRouting` | IMSI prefix 234-10 routes to Operator A (priority 1) | PASS |
| `TestSoR_IMSIPrefixNoMatch` | No prefix match -> default operator (lowest priority) | PASS |
| `TestSoR_CostBasedSelection` | Two operators, cheaper one selected | PASS |
| `TestSoR_CircuitBreakerOpen` | Operator A down -> selects Operator B | PASS |
| `TestSoR_ManualOperatorLock` | SIM has operator lock -> SoR bypassed | PASS |
| `TestSoR_RATPreference` | 4G preferred, only op B supports -> selects B | PASS |
| `TestSoR_SortByPriorityThenCost` | Multi-operator sort: priority ASC, then cost ASC | PASS |
| `TestSoR_NoAvailableOperators_AllCircuitOpen` | All circuits open -> error | PASS |
| `TestSoR_NoGrants` | No grants available -> error | PASS |
| `TestSoR_MatchIMSIPrefix` | 5 sub-tests: exact 5/6 digit, no match, short, empty | PASS |
| `TestSoR_FilterByRAT` | Filter by 4G, 5G, 6G (unsupported) | PASS |
| `TestSoR_SortCandidates` | Priority > RAT rank > cost ordering | PASS |
| `TestSoR_DefaultConfig` | Default TTL=1h, 6 RAT types | PASS |
| `TestSoR_ManualLockInvalidUUID` | Invalid UUID in operator_lock -> SoR proceeds normally | PASS |
| `TestSoR_FallbackOperatorsList` | Correct fallback list built from non-primary operators | PASS |
| `TestSoR_RATPreference_NoSupportedFallsBackToAll` | No RAT match -> falls back to all candidates | PASS |

Full regression: 34 packages, 0 failures.

## Pass 4: Performance

| Check | Status | Detail |
|-------|--------|--------|
| IMSI prefix O(1) | PASS | `strings.HasPrefix` — O(n) on prefix length (5-6 chars), effectively O(1) |
| Redis caching | PASS | GET/SET with configurable TTL (default 1h), cache key `sor:result:{tenant}:{imsi}` |
| SCAN pagination | PASS | `DeleteByOperator` and `DeleteAllForTenant` use SCAN with COUNT 100 |
| Bulk invalidation | PASS | `BulkRecalculate` -> `DeleteAllForTenant` uses SCAN pattern, lazy re-evaluation on next auth |
| No N+1 queries | PASS | `ListGrantsWithOperators` JOINs grants+operators in single query |
| Sort stability | PASS | `sort.SliceStable` preserves insertion order for equal elements |

## Pass 5: Build

- `go build ./...` — CLEAN, no compilation errors
- `go vet ./internal/operator/sor/...` — CLEAN, no warnings

## Pass 6: UI/Frontend

SKIPPED — Backend-only story, no frontend components.

## Fixes Applied

None required. All checks passed without intervention.

## Files Reviewed

| File | Type | Lines |
|------|------|-------|
| `migrations/20260321000001_sor_fields.up.sql` | New | 11 |
| `migrations/20260321000001_sor_fields.down.sql` | New | 11 |
| `internal/store/operator.go` | Modified | 641 |
| `internal/store/session_radius.go` | Modified | 425 |
| `internal/operator/sor/types.go` | New | 59 |
| `internal/operator/sor/engine.go` | New | 319 |
| `internal/operator/sor/cache.go` | New | 136 |
| `internal/operator/sor/subscriber.go` | New | 146 |
| `internal/operator/sor/engine_test.go` | New | 609 |

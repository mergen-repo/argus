# Implementation Plan: FIX-208 — Cross-Tab Data Aggregation Unify

## Goal
Introduce a canonical `internal/analytics/aggregates` facade service (Redis-cached, NATS-invalidated) that fronts the existing store-layer count/aggregation methods so every UI surface reads the same numbers for the same metric, and resolves F-125 by picking a single source of truth for "SIMs on policy X".

## Problem Context

### What the audit actually showed (grep-confirmed)

The story description says "every handler executes own count query." The code says otherwise — **no HTTP handler runs raw COUNT/SUM SQL**. Every count already flows through a store method:

| Call site | Store method called | Underlying table |
|-----------|--------------------|-------------------|
| `internal/api/dashboard/handler.go:177` | `SIMStore.CountByState` | `sims` |
| `internal/api/dashboard/handler.go:196` | `RadiusSessionStore.GetActiveStats` | `sessions` |
| `internal/api/operator/handler.go:593` (List) | `SIMStore.CountByOperator` | `sims` |
| `internal/api/operator/handler.go:606` (List) | `RadiusSessionStore.GetActiveStats` | `sessions` |
| `internal/api/operator/handler.go:611` (List) | `RadiusSessionStore.TrafficByOperator` | `sessions` |
| `internal/api/operator/handler.go:999` (Detail) | `SIMStore.CountByOperator` | `sims` |
| `internal/api/operator/handler.go:1008` (Detail) | `RadiusSessionStore.GetActiveStats` | `sessions` |
| `internal/api/operator/handler.go:1011` (Detail) | `RadiusSessionStore.TrafficByOperator` | `sessions` |
| `internal/api/apn/handler.go:195` | `SIMStore.CountByAPN` | `sims` |
| `internal/api/system/capacity_handler.go:74` | `SIMStore.CountByTenant` | `sims` |
| `internal/api/policy/handler.go` (policy list) | `PolicyStore.CountAssignedSIMs` (list loop) | `policy_assignments` |

The real problems the story targets are:

1. **Semantic drift (F-125, the list-0 vs detail-10 symptom)** — `SIMStore.CountByOperator` groups by `sims.policy_version_id` column, while `PolicyStore.CountAssignedSIMs` joins `policy_assignments`. Two different sources for "SIMs on policy X": the live FK on `sims` (updated by `rollout.Apply*` at `internal/store/policy.go:960/987/1006/1016`) and the historical assignment log (`policy_assignments`). When a rollout is mid-flight, these diverge by design — but the UI treats them as the same number.
2. **Redundant computation** — `operator/handler.go:593` runs `CountByOperator` for the list view, then `:999` runs it **again** inside the detail response just to pick one entry out of the returned map. `GetActiveStats` is called three times per operator list+detail render. No endpoint memoizes.
3. **No caching** — store methods hit Postgres every call. A single dashboard open fires 4 separate `sims` aggregations + 3 `sessions` aggregations.
4. **No single consumption point** — as new surfaces are added (operator card, APN tab, policy tab, analytics), each one re-wires against the store layer independently. That's how F-95/F-96/F-65/F-51/F-24/F-25 accumulated.

### What this plan delivers

A **facade** named `Aggregates`:
- Does NOT replace store methods or rewrite SQL.
- Delegates to existing store methods (`SIMStore.CountByOperator`, `GetActiveStats`, etc.).
- Wraps every call in a Redis 60s cache with tenant-scoped keys.
- Subscribes to NATS write events and invalidates cache keys.
- Picks one canonical source per metric name. For `SIMCountByPolicy` that canonical source is `sims.policy_version_id` (the live FK). `policy_assignments` continues to exist for CoA/audit tracking, but the UI number comes from `sims`.
- Exposes a Prometheus histogram for p95 measurement (AC-6).

## Architecture Context

### Components Involved

- `internal/analytics/aggregates/service.go` (NEW) — `Aggregates` interface + concrete implementation `dbAggregates` that delegates to store methods.
- `internal/analytics/aggregates/cache.go` (NEW) — `cachedAggregates` decorator that wraps a concrete impl with Redis (go-redis/v9 client).
- `internal/analytics/aggregates/invalidator.go` (NEW) — NATS subscriber that listens on write subjects and purges tenant-scoped cache keys.
- `internal/analytics/aggregates/metrics.go` (NEW) — helper to record cache hit/miss + duration histogram into the shared `metrics.Registry`.
- `internal/observability/metrics/metrics.go` — add `AggregatesCacheHits / AggregatesCacheMisses / AggregatesCallDuration` vectors.
- `cmd/argus/main.go` — construct one `Aggregates` instance after store + redis + eventBus are ready; wire into handlers.
- Handlers to refactor (all via a new constructor option, never raw wiring):
  - `internal/api/dashboard/handler.go` — `WithAggregates(...)` option; replace `CountByState`, `GetActiveStats` call sites.
  - `internal/api/operator/handler.go` — `WithAggregates(...)` option; replace `CountByOperator`, `GetActiveStats`, `TrafficByOperator` (both List and Detail).
  - `internal/api/apn/handler.go` — `WithAggregates(...)` option; replace `CountByAPN`.
  - `internal/api/policy/handler.go` — `WithAggregates(...)` option; replace `CountAssignedSIMs` with `Aggregates.SIMCountByPolicy` (the F-125 fix).
  - `internal/api/system/capacity_handler.go` — replace `CountByTenant`.

### Data Flow

```
HTTP handler
  → Aggregates.Method(ctx, tenantID, ...)              (facade call)
    → cachedAggregates.Method                          (decorator)
      → Redis GET argus:aggregates:v1:{tenant}:{method}:{argsHash}
        HIT  → deserialize → return (record hit + duration histogram)
        MISS → dbAggregates.Method                     (underlying impl)
                → SIMStore.CountByOperator / GetActiveStats / etc.
                → Redis SETEX key value 60
                → return (record miss + duration histogram)

Write path (orthogonal):
  Handler → store.Update → EventBus.Publish("argus.events.sim.updated", {tenant_id, ...})
    → NATS
      → aggregates invalidator (queue "aggregates-invalidator")
      → Redis DEL by pattern argus:aggregates:v1:{tenant}:*   (SCAN + UNLINK, or per-method targeted DEL)
```

### API Specifications

No new HTTP endpoints. Handlers continue to serve their existing routes. The contract change is purely internal (handler → service boundary).

### Service Interface

```
package aggregates

type Aggregates interface {
    // SIM aggregates (all tenant-scoped; backed by sims table via SIMStore).
    SIMCountByTenant(ctx, tenantID)              → int, error
    SIMCountByOperator(ctx, tenantID)            → map[uuid.UUID]int, error
    SIMCountByAPN(ctx, tenantID)                 → map[uuid.UUID]int64, error
    SIMCountByPolicy(ctx, tenantID, policyID)    → int, error
        // Canonical source: sims.policy_version_id JOIN policy_versions ON policy_id.
        // NOT policy_assignments — that table is CoA history.
    SIMCountByState(ctx, tenantID)               → total int, []store.SIMStateCount, error

    // Session aggregates (backed by sessions table via RadiusSessionStore).
    ActiveSessionStats(ctx, tenantID)            → *store.SessionStatsResult, error
        // Returns the full struct — TotalActive + ByOperator + ByAPN + AvgDurationSec + AvgBytes.
        // Consumers pick the slice they need; single query per tenant per 60s.
    TrafficByOperator(ctx, tenantID)             → map[uuid.UUID]int64, error
}
```

All methods take `context.Context` and a `uuid.UUID tenantID`. Nil tenantID is rejected with `ErrInvalidTenant` — Aggregates never sees admin-scope lookups (those paths keep calling the store directly, documented).

### Cache Layout

Key format: `argus:aggregates:v1:{tenant_id}:{method}:{argsHash}`

| Method | Key example |
|--------|-------------|
| SIMCountByTenant | `argus:aggregates:v1:{tid}:sim_count_by_tenant` |
| SIMCountByOperator | `argus:aggregates:v1:{tid}:sim_count_by_operator` |
| SIMCountByAPN | `argus:aggregates:v1:{tid}:sim_count_by_apn` |
| SIMCountByPolicy | `argus:aggregates:v1:{tid}:sim_count_by_policy:{policyID}` |
| SIMCountByState | `argus:aggregates:v1:{tid}:sim_count_by_state` |
| ActiveSessionStats | `argus:aggregates:v1:{tid}:active_session_stats` |
| TrafficByOperator | `argus:aggregates:v1:{tid}:traffic_by_operator` |

- TTL: 60s default (exposed as `Options.TTL`, never < 10s).
- Encoding: JSON via `encoding/json`. Map-of-UUID values: use `map[string]T` on the wire, deserialize back.
- Tenant ID is ALWAYS the second segment — ADR-001 compliance (a leak means a non-prefixed SCAN finds cross-tenant keys).
- Version prefix `v1` permits future schema breaks.

### Invalidation Event Map

| NATS subject (bus.Subject*) | Aggregator keys invalidated (pattern on tenant_id from payload) |
|-----------------------------|------------------------------------------------------------------|
| `argus.events.sim.updated` (`SubjectSIMUpdated`) | `*:sim_count_by_tenant`, `*:sim_count_by_operator`, `*:sim_count_by_apn`, `*:sim_count_by_policy:*`, `*:sim_count_by_state` |
| `argus.events.policy.changed` (`SubjectPolicyChanged`) | `*:sim_count_by_policy:*` |
| `argus.events.session.started` (`SubjectSessionStarted`) | `*:active_session_stats`, `*:traffic_by_operator` |
| `argus.events.session.ended` (`SubjectSessionEnded`) | `*:active_session_stats`, `*:traffic_by_operator` |

Invalidator payload parse: every event payload must carry `{ "tenant_id": "<uuid>" }`. Missing tenant_id → log debug + skip (same pattern as `dashboard/invalidator.go:31`). Queue group name: `aggregates-invalidator` (distinct from `dashboard-invalidator` so both layers receive each event).

Deletion strategy: **targeted DEL per affected key list**, not SCAN+MATCH. Each subject statically maps to a fixed key set for that tenant. This avoids O(n) SCAN in production and the thundering-herd risk of SCAN under load. For policy-scoped keys (`sim_count_by_policy:{policyID}`) we do one cheap SCAN with `MATCH argus:aggregates:v1:{tid}:sim_count_by_policy:*` with COUNT=100 — acceptable because the set is small (≤ number of policies per tenant).

### Hypertable / time-bounds note

None of the aggregator methods in this first cut read from `cdrs` (a TimescaleDB hypertable). All reads are against `sims` and `sessions` (both regular tables). Any future extension that adds a `cdrs`-based aggregator MUST document an explicit time bound (e.g. LAST 24h / 7d / 30d) — flagged in `service.go` doc comment. Today's session-traffic aggregator is `WHERE session_state = 'active'` so naturally bounded by active-session set.

### F-125 canonical source decision (resolved inline — AUTOPILOT, no deferral)

Question: does "SIMs on policy X" come from `sims.policy_version_id` or `policy_assignments`?

Decision: **`sims.policy_version_id`** is canonical. Rationale:
- It is the live FK used by the AAA engine for policy lookup at auth time (`policyMatcher` → `sims.policy_version_id`).
- It is updated atomically by `rollout.Apply*` (`internal/store/policy.go:960 / 987 / 1006 / 1016`) when a rollout stage completes.
- `policy_assignments` is the history of the apply operation + CoA state (`coa_status`, `coa_sent_at`). It contains rows for assignments even after the SIM was removed from the policy.

Implementation of `SIMCountByPolicy(tenantID, policyID)`:

```
SELECT COUNT(*) FROM sims
 WHERE tenant_id = $1
   AND state != 'purged'
   AND policy_version_id IN (SELECT id FROM policy_versions WHERE policy_id = $2)
```

`policy_version_id IS NULL` is implicitly excluded (PAT-009: nullable FK — IS NULL is naturally filtered by the `IN (...)` predicate, never NULL-scanned into a Go string). `PolicyStore.CountAssignedSIMs` stays for internal rollout accounting only and gets a doc-comment warning that UI code MUST NOT call it.

### Store Methods Referenced (existing — no schema changes)

```
// internal/store/sim.go
SIMStore.CountByOperator(ctx, tenantID)    → map[uuid.UUID]int
SIMStore.CountByTenant(ctx, tenantID)      → int  (excludes state='purged')
SIMStore.CountByAPN(ctx, tenantID)         → map[uuid.UUID]int64
SIMStore.CountByState(ctx, tenantID)       → int, []SIMStateCount

// internal/store/session_radius.go
RadiusSessionStore.GetActiveStats(ctx, *uuid.UUID) → *SessionStatsResult{TotalActive, ByOperator map[string]int64, ByAPN map[string]int64, AvgDurationSec, AvgBytes}
RadiusSessionStore.TrafficByOperator(ctx, *uuid.UUID) → map[uuid.UUID]int64

// NEW (Task 2 adds this)
SIMStore.CountByPolicyID(ctx, tenantID, policyID) → int
```

## Prerequisites
- [x] FIX-206 completed — `sims.operator_id NOT NULL FK`, `sims.apn_id` nullable FK. Aggregation queries must respect PAT-009 for `apn_id` (use `IS NOT NULL` filter — `SIMStore.CountByAPN` already does this at line 1250).
- [x] Redis client in main.go (`rdb.Client`) is already wired and passed into dashboard/diag/anomaly.
- [x] NATS EventBus is running with `argus.events.sim.updated` / `argus.events.policy.changed` / `argus.events.session.started` / `argus.events.session.ended` subjects live (confirmed in `internal/bus/nats.go:20-42`).
- [x] Metrics Registry pattern established by FIX-207 (`internal/observability/metrics/metrics.go`).
- [x] Pattern file for invalidator: `internal/api/dashboard/invalidator.go`.

## Main.go Wiring Sites (PAT-011 — enumerated, not buried)

When the Aggregates service is constructed, EVERY handler that needs it must receive it via a functional option at its `NewHandler(...)` call site. PAT-011 failure mode: option added but call sites not updated → silent zero-value → dead wire at runtime.

| Line (as of 2026-04-20) | Construction call | Option to add |
|--------------------------|-------------------|----------------|
| `cmd/argus/main.go:525` | `operatorapi.NewHandler(...)` | `operatorapi.WithAggregates(aggSvc)` |
| `cmd/argus/main.go:531` | `apnapi.NewHandler(...)` | `apnapi.WithAggregates(aggSvc)` |
| `cmd/argus/main.go:571` | `simapi.NewHandler(...)` | `simapi.WithAggregates(aggSvc)` (only if sim detail/list uses counts — Task 0 audit confirms) |
| `cmd/argus/main.go:574` | `policyapi.NewHandler(...)` | `policyapi.WithAggregates(aggSvc)` |
| `cmd/argus/main.go:1161` | `metricsapi.NewHandler(...)` | N/A (observability, not aggregation) |
| `cmd/argus/main.go:1182` | `dashboardapi.NewHandler(...)` | `dashboardapi.WithAggregates(aggSvc)` |
| `cmd/argus/main.go:~1175` (system capacity, find via grep) | `systemapi.NewCapacityHandler(...)` | constructor positional arg (small handler — replace simStore call directly) |

Gate F-A step: `go build ./...` then grep each handler file for residual direct `simStore.CountBy*` / `sessionStore.GetActiveStats` / `sessionStore.TrafficByOperator` calls in the read paths. Any remaining → dead wire.

Construction point in main.go: right after `rdb.Client`, `eventBus`, `simStore`, `simSessionStore` are all ready (around line 570–575 before handlers are built):

```go
aggSvc := aggregates.New(
    simStore, simSessionStore,
    rdb.Client,
    metricsReg,
    log.Logger,
    aggregates.WithTTL(60*time.Second),
)
if err := aggregates.RegisterInvalidator(eventBus, rdb.Client, log.Logger); err != nil {
    log.Fatal().Err(err).Msg("register aggregates invalidator")
}
```

## Design Token Map

N/A — backend-only story (no React/Tailwind changes). AC-5's "FE consistency" check is served by the BE returning identical numbers; no component changes.

## Tasks

### Task 1: Audit duplication + publish reference table (AC-4)
- **Files:** Create `docs/stories/fix-ui-review/FIX-208-duplication-audit.md` (reference only — will be deleted at story close; alternatively inline into the plan if small).
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** None — greenfield doc.
- **Context refs:** "Problem Context > What the audit actually showed"
- **What:** Run the 4 grep patterns from the dispatch (`SELECT COUNT(*) FROM sims`, `... FROM sessions`, `... FROM cdrs`, `SUM(bytes_in`, `GROUP BY operator_id|apn_id|policy_version_id`). Capture the exact results. Build a table: `{call site file:line, store method reached, DB table}`. Confirm "no handler runs raw SQL". List the 3 real problem classes: (a) semantic drift (F-125 example with file:line), (b) redundant computation (operator list+detail double-call), (c) no caching. Mark canonical source decision for `SIMCountByPolicy`.
- **Verify:** File contains ≥ 11 call-site entries matching the Problem Context table; F-125 drift documented with both code locations (`CountByOperator:1212`, `CountAssignedSIMs:467`).

### Task 2: Add `SIMStore.CountByPolicyID` (the new canonical SQL)
- **Files:** Modify `internal/store/sim.go` (append after `CountByState` ~line 1295), modify `internal/store/sim_test.go` if it exists.
- **Depends on:** Task 1
- **Complexity:** low
- **Pattern ref:** Read `internal/store/sim.go:1212-1244` (`CountByOperator` + `CountByTenant`) — follow the same signature shape, error wrap, and pgx QueryRow pattern.
- **Context refs:** "Architecture Context > F-125 canonical source decision"
- **What:** New method `CountByPolicyID(ctx, tenantID, policyID uuid.UUID) → (int, error)`. Query exactly the SQL in the F-125 decision block. Handle `ErrNoRows` → return 0 (policy with no SIMs is valid). Wrap error with `store: count sims by policy:`.
- **Verify:** `go build ./...`; grep must find exactly one new function. A unit test seeding 3 SIMs on policy_version P1 and 2 on P2 → P1 returns 3, P2 returns 2, a non-existent policy returns 0.

### Task 3: Define `Aggregates` interface + `dbAggregates` concrete impl
- **Files:** Create `internal/analytics/aggregates/service.go`, create `internal/analytics/aggregates/doc.go`.
- **Depends on:** Task 2
- **Complexity:** medium
- **Pattern ref:** Read `internal/analytics/anomaly/engine.go` (if present) or `internal/operator/health.go` for the store-delegating service pattern; `internal/api/dashboard/handler.go:67-88` for the `HandlerOption`/functional-option convention used throughout.
- **Context refs:** "Architecture Context > Components Involved", "Service Interface", "Store Methods Referenced", "F-125 canonical source decision"
- **What:** Define the `Aggregates` interface exactly as in "Service Interface". Implement `dbAggregates` as a struct holding `*store.SIMStore`, `*store.RadiusSessionStore` and a logger. Each method delegates to the corresponding store call. `SIMCountByPolicy` calls the new `CountByPolicyID`. No caching here — pure delegation. Add `doc.go` describing the facade pattern and the F-125 decision so future readers don't re-debate it.
- **Verify:** `go build ./internal/analytics/aggregates/...`. Interface compiles; all 7 methods return correct types; errors wrapped with `aggregates:` prefix.

### Task 4: Cache decorator (`cachedAggregates`) + Redis serialization
- **Files:** Create `internal/analytics/aggregates/cache.go`, create `internal/analytics/aggregates/cache_test.go`.
- **Depends on:** Task 3
- **Complexity:** medium
- **Pattern ref:** Read `internal/cache/redis.go` for the client shape; `internal/api/dashboard/handler.go:155-163` for cache key + GET/SET pattern against `*redis.Client`.
- **Context refs:** "Architecture Context > Cache Layout", "Service Interface"
- **What:** `cachedAggregates` struct that embeds an `Aggregates` and holds `*redis.Client`, `ttl time.Duration`, `reg *metrics.Registry`, `logger`. For each method: (1) compute the exact key per "Cache Layout"; (2) GET; on hit `json.Unmarshal`, record metrics (`AggregatesCacheHits.WithLabelValues(method).Inc()`, duration observed), return; (3) on miss or unmarshal error, fall through to inner, SET with `SETEX ttl`, record miss, return. `map[uuid.UUID]T` values serialize through a shim `map[string]T` so JSON roundtrips cleanly. Expose `New(inner, client, ttl, reg, logger) Aggregates`. Constructor option `WithTTL` on the outer `aggregates.New`.
- **Verify:** `cache_test.go` with a miniredis or fake client: first call → miss → result cached; second call → hit → inner NOT called; invalidate key → third call → miss again. `cache_test.go` also asserts the key string format literally matches "Cache Layout" for `SIMCountByOperator` and `SIMCountByPolicy`.

### Task 5: NATS invalidator
- **Files:** Create `internal/analytics/aggregates/invalidator.go`, create `internal/analytics/aggregates/invalidator_test.go`.
- **Depends on:** Task 4
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/dashboard/invalidator.go` — copy the structure verbatim: `QueueSubscribeCtx` per subject, log on parse failure, `Del` on success. The only differences are (a) different queue group name, (b) a subject→key-list map table instead of one fixed key, (c) one SCAN+DEL branch for policy-scoped keys.
- **Context refs:** "Architecture Context > Invalidation Event Map", "Cache Layout"
- **What:** Function `RegisterInvalidator(eb *bus.EventBus, rc *redis.Client, logger zerolog.Logger) error`. Internal constant `invalidatorQueue = "aggregates-invalidator"`. Map `{subject → []keyTemplate}`. Subscribe to each of the 4 subjects. On delivery: parse `{tenant_id}`, skip if missing (debug log), build the list of concrete keys for that tenant, call `rc.Del(ctx, keys...).Err()`, log warn on failure. For `sim_count_by_policy:*` use a `SCAN MATCH argus:aggregates:v1:{tid}:sim_count_by_policy:* COUNT 100` once then `UNLINK` the batch (UNLINK > DEL for non-blocking delete).
- **Verify:** `invalidator_test.go` with a fake EventBus + fake Redis: publish a `sim.updated` event with tenant X → assert the 5 sim_count_by_* keys for X were deleted. Publish `session.started` → assert session keys deleted, sim keys left alone. Publish event missing `tenant_id` → no DEL, one debug log.

### Task 6: Metrics fields (Registry) + instrumentation
- **Files:** Modify `internal/observability/metrics/metrics.go`, modify `internal/analytics/aggregates/cache.go` (inject the 3 instruments).
- **Depends on:** Task 4
- **Complexity:** low
- **Pattern ref:** Read `internal/observability/metrics/metrics.go:182-196` (Redis hits/misses pattern) — add 3 new vectors following the identical structure.
- **Context refs:** "Architecture Context > Components Involved"
- **What:** Add to `Registry`:
  - `AggregatesCacheHits *prometheus.CounterVec` labels `{method}` — metric name `argus_aggregates_cache_hits_total`
  - `AggregatesCacheMisses *prometheus.CounterVec` labels `{method}` — metric name `argus_aggregates_cache_misses_total`
  - `AggregatesCallDuration *prometheus.HistogramVec` labels `{method, cache}` (`cache` in `{hit, miss}`) — metric name `argus_aggregates_call_duration_seconds`, buckets `[0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0]` (tuned for the AC-6 p95<50ms target).
  Register in `NewRegistry`. In `cache.go`, observe duration with `time.Since(start)` on every method path, and increment hit/miss.
- **Verify:** `go build ./...`. `/metrics` exposes the three series after an HTTP test call. Unit test (or existing metrics test style) asserts the new vectors are non-nil on `NewRegistry()`.

### Task 7: Wire Aggregates in main.go + all handler construction sites (PAT-011)
- **Files:** Modify `cmd/argus/main.go`; add `WithAggregates` option to each of the 4 handler files (`dashboard`, `operator`, `apn`, `policy`). System-capacity handler gets a different treatment (small, direct positional param).
- **Depends on:** Tasks 3, 4, 5, 6
- **Complexity:** high
- **Pattern ref:** Read `internal/api/dashboard/handler.go:37-65` for the HandlerOption convention; `cmd/argus/main.go:1182-1190` for the construction with options followed by `RegisterDashboardInvalidator` call.
- **Context refs:** "Main.go Wiring Sites (PAT-011)", "Architecture Context > Data Flow"
- **What:**
  1. Add `WithAggregates(a aggregates.Aggregates) HandlerOption` to each handler package (4 files, ~5 lines each).
  2. In main.go (around line 575 — after stores, rdb, eventBus, simSessionStore all exist), construct `aggSvc := aggregates.New(simStore, simSessionStore, rdb.Client, metricsReg, log.Logger, aggregates.WithTTL(60*time.Second))`.
  3. Register the invalidator immediately after: `if err := aggregates.RegisterInvalidator(eventBus, rdb.Client, log.Logger); err != nil { log.Fatal()... }`.
  4. Append `xxxapi.WithAggregates(aggSvc)` to the `NewHandler` variadic args at each of the 5 construction sites enumerated in "Main.go Wiring Sites".
  5. For system capacity handler (positional, no options): change signature to accept `aggregates.Aggregates` and replace the `CountByTenant` call.
- **Verify:** `go build ./...` passes. Grep check (documented below) returns ZERO non-test hits outside the aggregates package for `simStore.CountByTenant`, `simStore.CountByOperator`, `simStore.CountByAPN`, `simStore.CountByState`, `sessionStore.GetActiveStats` (from read-path handlers — write-path usage in `aaasession.Manager` stays), `sessionStore.TrafficByOperator`, `policyStore.CountAssignedSIMs`.

  Exact command:
  ```
  rg -n 'simStore\.(CountByTenant|CountByOperator|CountByAPN|CountByState)|sessionStore\.(GetActiveStats|TrafficByOperator)|policyStore\.CountAssignedSIMs' internal/api/ cmd/ | grep -v _test.go
  ```
  Expected: empty. If anything remains, the wiring is incomplete (PAT-011 trap).

### Task 8: Refactor handler read paths to call Aggregates
- **Files:** Modify `internal/api/dashboard/handler.go`, `internal/api/operator/handler.go`, `internal/api/apn/handler.go`, `internal/api/policy/handler.go`, `internal/api/system/capacity_handler.go`.
- **Depends on:** Task 7
- **Complexity:** high
- **Pattern ref:** Each handler already has a store-call line to replace. Swap `h.simStore.CountByOperator(ctx, tenantID)` → `h.agg.SIMCountByOperator(ctx, tenantID)`. The return types MATCH by design (Task 3 mirrored the shapes) so no caller-side unpacking changes.
- **Context refs:** "Problem Context > What the audit actually showed", "Service Interface", "Data Flow"
- **What:** For each file, replace the specific call sites enumerated in the audit table. Dashboard handler: lines 177 and 196. Operator handler: 593, 606, 611, 999, 1008, 1011 (the list↔detail duplication naturally collapses to single cached calls — when both fire within 60s, the second is a cache hit). APN handler: 195. Policy handler: replace `CountAssignedSIMs` call with `SIMCountByPolicy(tenantID, policyID)` at each list-loop site.
- **Verify:** `go build ./...`; handler unit tests still pass (`go test ./internal/api/dashboard/... ./internal/api/operator/... ./internal/api/apn/... ./internal/api/policy/...`); the Task 7 grep still returns empty.

### Task 9: AC-5 cross-surface consistency integration test
- **Files:** Create `internal/analytics/aggregates/integration_test.go`.
- **Depends on:** Tasks 7, 8
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/sim_fk_integration_test.go` for the pgx integration test pattern used by FIX-206 (DB setup, tenant seed, assertion style).
- **Context refs:** "Service Interface", "F-125 canonical source decision"
- **What:** Integration test `TestAggregates_F125_SameOperatorSIMCountEverywhere`: seed 1 tenant, 1 operator, 1 policy-version, 10 SIMs on that policy-version. Call `SIMCountByOperator` → map[opID] = 10. Call `SIMCountByPolicy(policyID)` → 10. Now create the F-125 drift: insert a `policy_assignments` row for a deleted SIM → `PolicyStore.CountAssignedSIMs(policyID)` would return 11, but `Aggregates.SIMCountByPolicy` MUST still return 10 (canonical = `sims.policy_version_id`). Second test `TestAggregates_CacheConsistency`: call `SIMCountByOperator` three times within 60s → Prometheus counter `AggregatesCacheHits` increments twice, `AggregatesCacheMisses` increments once.
- **Verify:** `go test -tags=integration ./internal/analytics/aggregates/... -run F125` passes.

### Task 10: AC-6 performance measurement + bug-pattern entry
- **Files:** Create `internal/analytics/aggregates/perf_test.go` (benchmark + latency assertion), append to `docs/brainstorming/bug-patterns.md`.
- **Depends on:** Task 9
- **Complexity:** medium
- **Pattern ref:** Read `internal/observability/metrics/metrics_test.go` for histogram observation; bug-patterns entries like PAT-011 for the append format.
- **Context refs:** "Architecture Context > Components Involved", "Problem Context"
- **What:**
  1. `perf_test.go`: prime a cache key, then assert p95 of 1000 `SIMCountByOperator` calls on cache-hit is < 50ms (AC-6). Use `time.Since` samples + sort + index `[p95]`. Also benchmark via `Benchmark` function.
  2. Append PAT-012 to `docs/brainstorming/bug-patterns.md`: "Cross-surface count drift — two different strategies (FK column vs assignment-log table) returning different numbers for the 'same' logical metric. Prevention: Every aggregate metric has ONE canonical source; document it in the `Aggregates` doc.go; non-canonical sources carry a `// CoA/audit only — NOT UI truth` banner. UI handlers MUST consume via the `Aggregates` facade — raw store calls are banned in read paths. Covered by FIX-208 Gate grep (Task 7 Verify)."
- **Verify:** `go test ./internal/analytics/aggregates/... -run Perf -v` passes with p95 < 50ms. `bug-patterns.md` contains PAT-012. `docs/brainstorming/bug-patterns.md` line count increased by ≥ 1 entry.

## Task DAG / Waves

```
Wave A: Task 1
Wave B: Task 2                       (after 1)
Wave C: Task 3                       (after 2)
Wave D: Task 4, Task 6               (parallel — after 3)
Wave E: Task 5                       (after 4)
Wave F: Task 7                       (after 3,4,5,6)
Wave G: Task 8                       (after 7)
Wave H: Task 9, Task 10              (parallel — after 8)
```

## Acceptance Criteria Mapping

| Criterion | Implemented in | Verified by |
|-----------|----------------|-------------|
| AC-1 (Aggregates service with N methods) | Task 3 | Task 3 Verify, Task 9 |
| AC-2 (every duplicated handler path calls the service) | Task 7, Task 8 | Task 8 Verify (grep) |
| AC-3 (Redis 60s TTL + NATS invalidation) | Task 4, Task 5 | Task 4 Verify, Task 5 Verify |
| AC-4 (audit + consolidate) | Task 1, Task 3, Task 8 | Task 1 Verify |
| AC-5 (3+ pages show same number for same metric) | Task 8 (single code path) | Task 9 (F-125 integration test) |
| AC-6 (p95 < 50ms on cache hit) | Task 4, Task 6 | Task 10 perf_test |

## Story-Specific Compliance Rules

- **API:** No endpoint contract changes; responses still envelope `{status, data, meta?, error?}`.
- **DB:** Only additive change is the new method `SIMStore.CountByPolicyID` (no migration — uses existing `sims.policy_version_id` column and `policy_versions` table).
- **Tenant isolation (ADR-001):** Every Aggregates method takes `tenantID uuid.UUID` and includes it in the cache key. `tenantID == uuid.Nil` → `ErrInvalidTenant` (admin-scope paths continue to hit stores directly and are out of scope for this facade).
- **PAT-006 (shared struct literal):** When adding `WithAggregates` option, do not introduce a shared options struct; stick to the functional-option pattern already in each handler.
- **PAT-009 (nullable FK):** `SIMCountByAPN` already filters `apn_id IS NOT NULL` in `SIMStore.CountByAPN:1250`. `SIMCountByPolicy` uses `IN (...)` which excludes NULL implicitly — no `COALESCE` on FK joins (banned per PAT-009).
- **PAT-011 (functional-option wiring):** Gate checklist step is the `rg` command in Task 7 Verify. Zero hits = all sites wired.

## Bug Pattern Warnings

- **PAT-006:** Adding `WithAggregates` to multiple handler packages — the functional-option pattern keeps each package independent; no shared struct literal to forget a field in. But if Task 7 introduces a common `aggregates.Config` struct later, every handler site must explicitly set every field. Today: not applicable.
- **PAT-009:** `apn_id` nullable FK — `SIMCountByAPN` delegates to `CountByAPN` which already uses `IS NOT NULL`. Do NOT switch to `COALESCE(apn_id::text, '__unassigned__')` — the aggregator explicitly excludes unassigned SIMs, matching the current behavior.
- **PAT-011:** MANDATORY: Task 7 includes the `rg` grep verification. If that grep returns any hits outside `_test.go` and outside `internal/aaa/session/session.go:476` (internal engine use, not a UI read path), the plan's wiring is incomplete and Gate fails.

## Tech Debt (from ROUTEMAP)
No tech debt items in `docs/ROUTEMAP.md` explicitly target FIX-208. The canonical-source decision documented above addresses the class of drift captured as F-95/F-96/F-65/F-51/F-24/F-25.

## Mock Retirement
No FE mocks for these endpoints — all tabs already consume live backend responses. No mock retirement for this story.

## Risks & Mitigations

- **R1 — Invalidator SCAN cost for policy keys.** Mitigation: UNLINK over DEL; COUNT=100 batch; key set is bounded by number of policies per tenant (typically < 100). Fallback: deliberate deletion of a fixed set of "all policy keys invalidated" if SCAN pressure shows up in production.
- **R2 — Race between cache SET and an in-flight invalidation (thundering herd on a hot key).** Mitigation: 60s TTL is long enough to absorb; invalidation arriving after a SET simply re-invalidates. No single-flight needed at this scale; can be added later if p95 shows drift.
- **R3 — The F-125 canonical decision conflicts with an existing caller that specifically needs the `policy_assignments` count.** Mitigation: `PolicyStore.CountAssignedSIMs` is NOT removed; it's documented as CoA/audit only via doc-comment banner. Only the UI path swaps.
- **R4 — Cache key TTL too short, cache churn overwhelms Redis.** Mitigation: 60s is conservative; if misses > hits in production, tune up via `WithTTL`. Option exposed for ops.
- **R5 — Dead-wire bug (PAT-011 recurrence).** Mitigation: Task 7 Verify `rg` grep is the gate; zero non-test hits required.

## Pre-Validation Checklist (self-validated)

- [x] Minimum substance (L effort → ≥ 100 lines, ≥ 5 tasks) — plan is 10 tasks, ~350 lines.
- [x] Required sections: Goal, Architecture Context, Tasks, Acceptance Criteria Mapping — all present.
- [x] Embedded specs: service interface embedded, cache key format embedded, invalidation map embedded, store methods embedded.
- [x] At least one `Complexity: high` task — Tasks 7 and 8 marked high.
- [x] Context refs point to actual section headers in this plan — verified each refers to a `##`/`###` heading above.
- [x] Architecture compliance: new `internal/analytics/aggregates/` package fits SVC-07 analytics bucket; handlers stay in SVC-03 api layer; no cross-layer imports (handlers import aggregates, aggregates imports store/cache/bus).
- [x] DB compliance: schema source noted (existing migration `20260320000002_core_schema.up.sql` + FIX-206 FK migration); no new migration required.
- [x] Task decomposition: each task touches 1–3 files (Task 7 touches more handlers but splits work to "add option" = 5 small mechanical edits + main.go construction — counted as one logical unit per PAT-011 rule).
- [x] Each new-file task has Pattern ref.
- [x] PAT-011 addressed with dedicated "Main.go Wiring Sites" section AND a Verify grep command.
- [x] PAT-009 addressed (nullable FK handled by existing store queries; no COALESCE on FK).
- [x] F-125 canonical source resolved inline (AUTOPILOT, no deferral).
- [x] Hypertable awareness documented (no cdrs reads in this cut).
- [x] AC-6 measurement plan present (Task 10 perf_test with histogram).

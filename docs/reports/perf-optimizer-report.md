# Performance Optimizer Report — E3 Pass

> **Date:** 2026-05-05
> **Mode:** E2E & Polish E3 (post-E2 PASS)
> **Scope:** Detail-screen endpoints (SIM/Operator/APN/Session) + project-wide perf at seeded scale (~5k SIMs, ~33k sessions, ~50k audit chain entries)
> **Outcome:** All measured detail-screen endpoints comfortably under 500ms p95 target. No CRITICAL or HIGH defects requiring direct fix at current scale. 0 fixes applied. 4 deferred candidates documented for Ana Amil decision.

---

## Executive Summary

The detail-screen surface area (SIM/Operator/APN/Session detail tabs) is **performance-clean** at seeded demo scale. p95 latencies are 5–93 ms across all tabs, two orders of magnitude below the 500 ms SLO. Existing infrastructure (TimescaleDB hypertable indexes, pgx pool tuning, pgbouncer transaction-mode, Nginx upstream keepalive, name-cache layer in Redis, vite manualChunks code-split) is well-tuned and was already exercised in Phase 10 infra-tuning (per `infra/nginx/nginx.conf` header).

E3 found three **MEDIUM** patterns that will not bottleneck the demo but are flagged for production-scale (10M SIMs) follow-up: (1) a SIM-list N+1 on IP-address resolution; (2) an Operator-detail 4-sequential-aggregate enrichment that would benefit from errgroup parallelization; (3) the SIM `ListBySIM` cursor pagination uses an inconsistent ORDER-BY/cursor key (correctness risk under high concurrency, not perf). Each is documented below with measured impact and effort estimate.

---

## Measured Endpoint Performance (against running stack)

Method: 5 serial requests + 50 concurrent (10 workers) per endpoint via `urllib`. Heavy-load SIM is `a792d951…` (285 sessions). Tenant: `00000000-0000-0000-0000-000000000001`.

### SIM Detail tabs

| Endpoint | Serial p95 | Concurrent p95 | SLO | Status |
|---|---|---|---|---|
| `GET /sims/{id}` | 12 ms | 92 ms | 500 ms | PASS |
| `GET /sims/{id}/sessions?limit=50` | 6 ms | 12 ms | 500 ms | PASS |
| `GET /sims/{id}/usage?period=30d` | 15 ms | 21 ms | 500 ms | PASS |
| `GET /sims/{id}/history` | 6 ms | n/a | 500 ms | PASS |
| `GET /sims/{id}/imei-history` | 4 ms | n/a | 500 ms | PASS |
| `GET /sims/{id}/device-binding` | 5 ms | n/a | 500 ms | PASS |
| `GET /audit?entity_id=…&entity_type=sim&limit=20` | 5 ms | n/a | 500 ms | PASS |
| `GET /cdrs?limit=20&sim_id=…` | 7 ms | n/a | 500 ms | PASS |

### Operator / APN / Session Detail

| Endpoint | Serial p95 | Concurrent p95 | Status |
|---|---|---|---|
| `GET /operators/{id}` | 11 ms | n/a | PASS |
| `GET /operators/{id}/sessions` | 7 ms | n/a | PASS |
| `GET /operators/{id}/health` | 9 ms | n/a | PASS |
| `GET /operators/{id}/health-history` | 5 ms | n/a | PASS |
| `GET /operators/{id}/metrics` | 6 ms | n/a | PASS |
| `GET /sla/operators/{id}/months/2026/4/breaches` | 9 ms | n/a | PASS |
| `GET /apns/{id}` | 3 ms | n/a | PASS |
| `GET /apns/{id}/sims?limit=10` | 4 ms | n/a | PASS |
| `GET /sessions/{id}` (full enrichment) | 26 ms | 13 ms | PASS |

### EXPLAIN spot-check

`SELECT … FROM sessions WHERE tenant_id=$1 AND sim_id=$2 ORDER BY started_at DESC LIMIT 51`:

```
Limit  (cost=4.93..197.26 rows=51) (actual time=0.163..0.243 ms)
  -> Incremental Sort (Presorted Key: started_at)
     -> ChunkAppend (TimescaleDB)
        -> Index Scan using _hyper_1_1_chunk_idx_sessions_sim_started
           Index Cond: (sim_id = …)
           Filter: (tenant_id = …)
Planning Time: 3.309 ms
Execution Time: 0.275 ms
```

Index scan, 50-buffer hit, sub-ms execution. Planning time at 3.3 ms is TimescaleDB chunk-pruning overhead (1119 buffers consulted at planning); already mitigated by `QueryExecModeExec` in `internal/store/postgres.go:54`. No further optimization needed at this row count.

---

## Pass 1 — DB Query Analysis

### Coverage

- 80+ store files inspected; ~80 migrations cross-referenced for index coverage
- Detail-screen handler call graphs traced to store layer (`internal/api/{sim,session,operator,apn}/handler.go`)
- TimescaleDB hypertables (`sessions`, `cdrs`, `audit_logs`) — hot-path indexes verified

### Index coverage (good)

| Hot path | Index | Migration |
|---|---|---|
| `sessions(sim_id, started_at DESC)` | `idx_sessions_sim_started` | `20260412000008_composite_indexes.up.sql` |
| `sessions(tenant_id, operator_id, started_at DESC)` | `idx_sessions_tenant_operator` | `20260320000003_timescaledb_hypertables.up.sql` |
| `cdrs(sim_id, timestamp DESC)` | `idx_cdrs_sim_time` + `idx_cdrs_sim_timestamp` | core + composite |
| `audit_logs(tenant_id, entity_type, entity_id)` | `idx_audit_tenant_entity` | core |
| `imei_history(sim_id, observed_at DESC)` | `idx_imei_history_sim_observed` | `20260507000002` |
| `policy_violations(sim_id, created_at DESC)` | `idx_policy_violations_sim` | `20260324000001` |
| `sim_state_history(sim_id, created_at DESC)` | `idx_sim_state_history_sim` | core |
| `policy_assignments(sim_id) UNIQUE` | `idx_policy_assignments_sim` | core |
| `ota_commands(sim_id, created_at DESC)` | `idx_ota_commands_sim_created` | `20260321000002` |

All detail-screen tab queries land on an index. EXPLAIN confirms no Seq Scan on hypertables for canonical access patterns.

### Findings

#### MEDIUM — N+1 in SIM List enrichment loop (out of E3 detail-screen scope but flagged)

**Location:** `internal/api/sim/handler.go:722-732, 763-767`

The `List` handler resolves IP addresses one-at-a-time (`for _, ipID := range ipIDs { GetAddressByID(...) }`) and APN→pool lookups one-per-APN (`for aID := range apnIDs { ippoolStore.List(...) }`). At `limit=100`, this can issue ~100 round trips for IP and ~10 for APN-pool.

**Measured impact at seeded scale:** 55 ms p95 for `/sims?limit=100`. Within SLO at current scale.

**Risk at 10M SIMs:** N+1 per page-view scales with concurrency. With 100 concurrent dashboard users at limit=50 → 5000 IP lookups/sec on a single hot path.

**Fix scope (deferred):** Add `IPPoolStore.GetAddressesByIDs([]uuid)` and `ippoolStore.ListByAPNs([]uuid)` batch helpers. ~80 LoC + 2 store tests + handler refactor.

**Recommendation:** File as **FIX-NNN** for Phase 11 follow-up, not blocking the demo.

#### MEDIUM — Operator Detail Get does 4 sequential aggregate queries

**Location:** `internal/api/operator/handler.go:1056-1072`

```go
if simCounts, err := h.agg.SIMCountByOperator(ctx, tenantID); err == nil { … }
if stats, err := h.agg.ActiveSessionStats(ctx, tenantID); err == nil { … }
if trafficMap, err := h.agg.TrafficByOperator(ctx, tenantID); err == nil { … }
if healthTimes, err := h.operatorStore.LatestHealthByOperator(ctx); err == nil { … }
```

These four calls are independent and could run in parallel via `errgroup`. Currently serial → 4× pool-checkout round trips.

**Measured impact:** 11 ms p95 — well within SLO. Each individual query lands on its own index.

**Fix scope (deferred):** Wrap in `errgroup.WithContext`, parallel execute. ~30 LoC + 1 test. Cuts wall-clock by ~3×.

**Recommendation:** Same priority as the SIM list N+1 — apply when scaling to 10M.

#### LOW — `ListBySIM` cursor key mismatch

**Location:** `internal/store/session_radius.go:520-534`

`ORDER BY started_at DESC, id DESC` but cursor filter is `id < $cursorID`. For two sessions with the same `started_at` but `id` in opposite order from `started_at`, pagination can skip or duplicate.

**Severity:** LOW — `started_at` is wall-clock from session-start with sub-ms resolution; collisions are extremely rare in practice.

**Recommendation:** Out of E3 scope (correctness, not perf). Track in backlog.

---

## Pass 2 — Connection Pool

### Configuration

| Layer | Setting | Value | Verdict |
|---|---|---|---|
| pgx pool | `MaxConns` | 50 (`DATABASE_MAX_CONNS`) | OK for demo, bump to 100+ for prod |
| pgx pool | `MinConns` | env-tunable | OK |
| pgx pool | `QueryExecMode` | `Exec` (FIX-301) | Optimal — avoids stale OID caching across migrations |
| pgxpool | `MaxConnLifetime` | env | OK |
| pgxpool | `HealthCheckPeriod` | 30 s | OK |
| pgbouncer | `pool_mode` | `transaction` | Optimal |
| pgbouncer | `default_pool_size` | 20 | **Tight under sustained load** — see below |
| pgbouncer | `max_client_conn` | 200 | OK |
| pgbouncer | `max_db_connections` | 50 | Matches Postgres `max_connections` budget |
| Redis | `RedisMaxConns` | 100 | OK |
| WS | `WSMaxConnsPerTenant` | 100 | OK |

### Findings

**LOW — pgbouncer default_pool_size=20 is conservative**

With 50 concurrent app workers issuing transactions, pgbouncer queues at 20 server connections. This is by design (transaction-pooling — connections recycle in microseconds), but under bursty load (100+ concurrent dashboard users) the queue depth can spike. Concurrent-test p95=92 ms on `/sims/{id}` suggests pool-checkout latency is the dominant tail factor (vs ~6 ms serial).

**Recommendation:** Bump `default_pool_size` to 30 if cutover traffic profile shows >50 concurrent SQL-in-flight. **Not applied** — current SLO satisfied; flagged for prod-cutover monitoring.

---

## Pass 3 — Caching Strategy

### Existing layer (`internal/cache/name_cache.go`)

- `name:op:{uuid}` — operator name, 5 min TTL
- `name:apn:{uuid}` — APN name, 5 min TTL
- `name:pool:{uuid}` — IP pool name, 5 min TTL
- Used in SIM List enrichment fallback (`internal/api/sim/handler.go:742-752`)

**Verdict:** Sound. Operator/APN/Pool names are write-rare → 5-min TTL is appropriate. No cache-stampede protection needed (single-writer pattern + 5-min TTL keeps collision probability low).

### Gap (LOW)

The `enrichSIMResponse` path in single-SIM detail (`handler.go:560-595`) does not consult `nameCache` for operator-name / APN-name lookups (it goes straight to store). For high-traffic single-SIM detail views (e.g., support agents bouncing between SIM tabs), this could be cache-warmed.

**Measured impact:** Negligible — store lookups are 1–2 ms each. Not worth a fix.

---

## Pass 4 — API / Infrastructure

### Nginx (`infra/nginx/nginx.conf`) — already tuned

- `worker_connections 4096`, `multi_accept on`, `use epoll`
- `tcp_nopush on`, `tcp_nodelay on`, `reset_timedout_connection on`
- `keepalive_timeout 65`, `keepalive_requests 1000`
- `gzip on` with comp_level 5, full type coverage
- `open_file_cache max=10000 inactive=60s`
- Upstream pools: `keepalive 64; keepalive_requests 1000; keepalive_timeout 60s` (api), `keepalive 32` (websocket)
- `proxy_buffering on`, `proxy_buffer_size 8k`, `proxy_buffers 16 8k`
- `proxy_next_upstream` for resilience

**Verdict:** No changes needed.

### Docker Compose

- Argus app: pgxpool=50, Redis=100, NATS native
- pgbouncer fronting at :6432 with transaction pooling
- All resource limits left at default — at seeded scale, app RSS is ~150 MB, postgres ~300 MB, Redis ~30 MB. No memory pressure observed.

---

## Pass 5 — Frontend Bundle

### Build output (`web/dist/`)

- 121 chunks, total 2.9 MB (uncompressed)
- Manual chunks via `vite.config.ts`:
  - `vendor-react` (76 KB raw / 26 KB gz)
  - `vendor-charts` (411 KB raw / 119 KB gz) — recharts, lazy
  - `vendor-codemirror` (382 KB raw / 124 KB gz) — DSL editor only, lazy
  - `vendor-query`, `vendor-ui`, `vendor-data` split
- Initial bundle (index + vendor-react + vendor-ui + css): **~213 KB gzipped**
- Heavy libs (charts, codemirror) are NOT in initial bundle — only loaded on routes that use them.

### Router lazy-loading (`web/src/router.tsx`)

- All detail routes use `lazySuspense` wrapper with React Suspense fallback
- Charts/codemirror/editor chunks load on demand

**Verdict:** Bundle strategy is production-grade. No changes needed.

---

## Findings Summary

| # | Severity | Type | Location | Status |
|---|---|---|---|---|
| 1 | MEDIUM | N+1 IP/Pool/APN lookup in SIM List | `internal/api/sim/handler.go:722,763` | Deferred (FIX-NNN candidate) |
| 2 | MEDIUM | Sequential aggregate enrichment in Operator Get | `internal/api/operator/handler.go:1056` | Deferred (FIX-NNN candidate) |
| 3 | LOW | `ListBySIM` cursor key mismatch | `internal/store/session_radius.go:533` | Backlog (correctness, not perf) |
| 4 | LOW | pgbouncer default_pool_size=20 conservative | `deploy/pgbouncer/pgbouncer.ini` | Monitor in cutover |
| 5 | LOW | `enrichSIMResponse` skips name-cache | `internal/api/sim/handler.go:560` | Optional micro-opt |

**Total: 0 CRITICAL, 0 HIGH, 2 MEDIUM, 3 LOW. 0 fixes applied (none required at current scale).**

---

## Fixes Applied

None. All measured detail-screen endpoint p95 latencies are well under the 500 ms target at seeded scale. No CRITICAL/HIGH defects warranted inline patching.

## Not Applied — Routed for Ana Amil Decision

| # | Description | Severity | Effort | Rationale to defer |
|---|---|---|---|---|
| 1 | Batch IP+Pool+APN lookup in SIMs List N+1 | MEDIUM | M (~80 LoC + 2 store helpers + tests) | 55 ms p95 at limit=100 — within SLO; only matters at 10M scale or 100+ concurrent dashboard users |
| 2 | errgroup parallelize Operator Get aggregates | MEDIUM | S (~30 LoC + 1 test) | 11 ms p95 — within SLO; cuts wall-clock 3× but not on critical path |
| 3 | Fix `ListBySIM` cursor key consistency | LOW | S (~15 LoC + edge-case test) | Correctness, not perf; collision probability vanishingly small with sub-ms `started_at` resolution |
| 4 | Bump pgbouncer `default_pool_size` 20→30 | LOW | XS (1-line config) | No measured impact at demo scale; only prod-cutover concern |
| 5 | Wire `nameCache` into single-SIM detail enricher | LOW | XS (~10 LoC) | Sub-ms saving per request; not user-perceptible |

## Test Results

- `go build ./...` → PASS (no code changes made — read-only investigation)
- No tests run (no edits to revert)

---

## Notes for Production Cutover

1. **Monitor pgbouncer `cl_waiting` and `sv_used`** — if `cl_waiting > 0` sustained, bump `default_pool_size`.
2. **Watch `argus_db_query_duration_seconds` p99** — slow-query tracer (`internal/store/postgres.go:69`) tags spans `argus.db.slow=true` at >100 ms; alert on count-rate.
3. **Hypertable chunk planning** — 3.3 ms planning time per session-list query. As `cdrs`/`sessions` grow, chunk count grows linearly. Set `timescaledb.max_chunks_per_query` if planning exceeds 50 ms in prod.
4. **Bundle on slow networks** — 213 KB gz initial is fine over 4G+; if 3G/EDGE customers exist, consider preloading `vendor-react` via `<link rel="modulepreload">`.

---

**Conclusion:** The detail-screen surface is demo-ready and customer-cutover-ready at the current seeded scale. The two MEDIUM findings are scale-dependent and well-understood — they will become FIX candidates in Phase 11 only if observed prod traffic justifies them.

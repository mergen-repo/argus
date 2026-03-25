# Performance Optimizer Report (Pass 2)

**Date:** 2026-03-23
**Scope:** Deep DB query analysis, N+1 fixes, missing indexes, pool tuning, Nginx optimization

## Summary

Analyzed all 45+ store files, 13 migrations, connection pool configs, caching layer, Nginx config, and frontend build. Applied 6 fixes across 7 files. All 57 test packages pass (1598+ tests).

---

## Pass 1: DB Query Analysis

### CRITICAL: N+1 Query in PolicyStore.AssignSIMsToVersion

**File:** `internal/store/policy.go:885`

The original code executed 2 SQL statements per SIM in a loop (INSERT + UPDATE) inside a transaction. For a rollout with 10,000 SIMs, this means 20,000 round-trips to PostgreSQL.

**Fix applied:** Batched INSERTs with VALUES(...),(...) and single UPDATE with WHERE id IN (...), processing 500 SIMs per batch. Reduces 20,000 round-trips to ~40.

### CRITICAL: N+1 Query in PolicyStore.RevertRolloutAssignments

**File:** `internal/store/policy.go:954`

Same pattern -- loop over simIDs with individual UPDATE/DELETE per SIM. For a 10,000 SIM rollout rollback, this was 20,000+ queries.

**Fix applied:** Replaced loop with two bulk queries:
- Single UPDATE/DELETE on policy_assignments WHERE rollout_id = $1
- Single UPDATE on sims WHERE id IN (SELECT sim_id FROM policy_assignments WHERE rollout_id = $1)

Reduces to exactly 2 queries regardless of SIM count.

### HIGH: Audit Pseudonymize Row-Lock During Iteration

**File:** `internal/store/audit.go:257`

Original code iterated rows from a SELECT and issued UPDATE per row while the cursor was still open, holding the DB connection longer than necessary.

**Fix applied:** Collect all rows first, close the cursor, then issue updates. This releases the connection from the pool sooner.

### MEDIUM: Missing Indexes Identified

| Table | Query Pattern | Missing Index |
|-------|---------------|---------------|
| sims | `ORDER BY created_at DESC, id DESC` (SIM list) | `(tenant_id, created_at DESC, id DESC)` |
| sims | `ILIKE '%term%'` on iccid/imsi (SIM search) | GIN trigram on iccid, imsi |
| sessions | `COUNT(*) WHERE session_state='active'` (CountActive) | Partial index on session_state |
| sessions | `WHERE acct_session_id=$1 AND session_state='active'` | Composite partial index |
| anomalies | `ORDER BY detected_at DESC, id DESC` per tenant | `(tenant_id, detected_at DESC, id DESC)` |
| anomalies | HasRecentAnomaly filter on (tenant,sim,type,state) | Composite partial index |
| audit_logs | `ORDER BY id DESC LIMIT 1` per tenant (GetLastHash) | `(tenant_id, id DESC)` |

**Fix applied:** Created migration `20260323000002_perf_indexes.up.sql` with 8 new indexes including pg_trgm extension for trigram search support.

### Existing Good Practices (No Action Needed)

- All list queries use cursor-based pagination with `LIMIT N+1` pattern
- All queries select explicit columns (no SELECT * anti-patterns found)
- CDRs and sessions are TimescaleDB hypertables with compression policies
- Continuous aggregates (`cdrs_hourly`, `cdrs_daily`, `cdrs_monthly`) avoid raw scans for analytics
- SIMs partitioned by operator_id, audit_logs and sim_state_history by date range
- All state-changing operations use proper transactions with `SELECT ... FOR UPDATE`

---

## Pass 2: Connection Pool Analysis

### PostgreSQL Pool (pgxpool)

**File:** `internal/store/postgres.go`, `internal/config/config.go`

| Setting | Before | After | Rationale |
|---------|--------|-------|-----------|
| MaxConns | 50 | 50 (unchanged) | Appropriate for PgBouncer with MAX_DB_CONNECTIONS=50 |
| MinConns (MaxIdleConns) | 10 | **25** | Was too low (20% of max), causing frequent connection churn under load. 50% is standard. |
| MaxConnLifetime | 30m | 30m (unchanged) | Good for PgBouncer rotation |
| MaxConnIdleTime | 5m | 5m (already set in E3) | Prevents stale connections |
| HealthCheckPeriod | 30s | 30s (already set in E3) | Catches dead connections |

**Fix applied:** Changed `DATABASE_MAX_IDLE_CONNS` default from 10 to 25.

### PgBouncer

**File:** `deploy/docker-compose.yml`

Already configured with `POOL_MODE=transaction`, `DEFAULT_POOL_SIZE=20`, `MAX_DB_CONNECTIONS=50`. This is well-tuned for a modular monolith pattern. No changes needed.

### Redis Pool

**File:** `internal/cache/redis.go`

Already configured (in E3) with `MinIdleConns = poolSize/4`, `ConnMaxIdleTime = 5min`. Good configuration. No changes needed.

### NATS

**File:** `internal/bus/nats.go`, `internal/config/config.go`

Configured with `MaxReconnect=60`, `ReconnectWait=2s`. Standard settings. No changes needed.

---

## Pass 3: Caching Strategy

### SIM Lookups (RADIUS Hot Path) -- GOOD

**File:** `internal/aaa/radius/sim_cache.go`

Redis cache with 5-minute TTL on IMSI lookups (key: `sim:imsi:{imsi}`). Write-through pattern: cache miss -> DB lookup -> cache set. This is critical for the 10M+ SIM scale -- every RADIUS auth request triggers an IMSI lookup.

### SoR (Steering-of-Roaming) Decisions -- GOOD

**File:** `internal/operator/sor/cache.go`

Redis cache with configurable TTL (default 1 hour) on SoR decisions (key: `sor:result:{tenant}:{imsi}`). Includes invalidation methods per operator and per tenant. This avoids re-evaluation of routing rules per authentication.

### Policy Dry-Run Results -- GOOD

**File:** `internal/policy/dryrun/service.go`

Redis cache for dry-run results.

### EAP Vector Cache -- GOOD

**File:** `internal/aaa/eap/vector_cache.go`

Redis cache for authentication vectors.

### Dashboard Aggregations -- NOT CACHED (Recommendation)

**File:** `internal/api/dashboard/handler.go`

The dashboard endpoint fires 4 parallel DB queries on every load. While already parallelized (from E3), these results don't change frequently. A 30-second Redis cache would eliminate repeated DB load from multiple users viewing the dashboard.

**Status:** Not applied (needs discussion -- cache invalidation strategy on SIM state changes).

### Analytics Queries -- MITIGATED

Usage analytics queries go through TimescaleDB continuous aggregates (`cdrs_hourly`, `cdrs_daily`, `cdrs_monthly`), which act as a materialized cache. The read replica routing is also in place for analytics queries. No additional Redis cache needed.

---

## Pass 4: API & Infrastructure

### Nginx Compression

**File:** `deploy/nginx/nginx.conf`

| Setting | Before | After |
|---------|--------|-------|
| gzip_comp_level | (default 1) | **5** |
| gzip_vary | (missing) | **on** |
| gzip_proxied | (missing) | **any** |
| gzip_min_length | 1000 | **256** |
| gzip_types | 6 types | **13 types** (added SVG, fonts, XML variants) |

**Fix applied:** Better compression ratio, proxy support, smaller threshold, font/SVG support.

### Pagination -- GOOD

All list endpoints enforce `limit` bounds (default 50, max 100). Cursor-based pagination throughout. No offset pagination found.

### Rate Limiting -- GOOD

Redis-based rate limiting in gateway middleware (`internal/gateway/ratelimit.go`). API keys have per-minute and per-hour limits. OTA commands have per-SIM-per-hour rate limiting.

### Frontend Bundle -- GOOD (Already Optimized)

**File:** `web/vite.config.ts`

Manual chunks already split into 6 vendor bundles (react, charts, codemirror, query, ui, data). Code splitting with React.lazy already implemented. No further action needed.

---

## Fixes Applied

| # | Severity | Category | Description | File(s) |
|---|----------|----------|-------------|---------|
| 1 | CRITICAL | N+1 Query | Batch policy assignment (2N -> 2*ceil(N/500) queries) | `internal/store/policy.go` |
| 2 | CRITICAL | N+1 Query | Batch rollout revert (2N+1 -> 3 queries) | `internal/store/policy.go` |
| 3 | HIGH | Missing Index | 8 new indexes for SIM search, session lookup, anomaly filter, audit hash | `migrations/20260323000002_perf_indexes.up.sql` |
| 4 | MEDIUM | Pool Tuning | Increase min idle connections from 10 to 25 (50% of max) | `internal/config/config.go` |
| 5 | MEDIUM | Nginx | Compression level 5, vary header, proxy support, font/SVG types, lower threshold | `deploy/nginx/nginx.conf` |
| 6 | LOW | Connection Hold | Close audit cursor before issuing updates | `internal/store/audit.go` |

## Recommendations Not Applied (Need Approval)

| # | Severity | Description | Reason |
|---|----------|-------------|--------|
| R1 | HIGH | Dashboard Redis cache (30s TTL) | Needs cache invalidation strategy discussion |
| R2 | MEDIUM | MSISDN bulk import: batch INSERT instead of loop | Low frequency operation, correctness risk with partial failures |
| R3 | MEDIUM | CDR export: add server-side cursor with FETCH FORWARD for large exports | Needs streaming response refactor |
| R4 | LOW | CountActive sessions: replace full-table count with Redis counter maintained via NATS events | Architectural change |
| R5 | LOW | Audit GetByDateRange: add LIMIT to prevent unbounded result sets | Business requirement to return all records in range |

## Test Results

```
57 packages tested
0 failures
All 1598+ tests passing
Build: OK
```

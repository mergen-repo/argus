# Performance Optimizer Report

**Date:** 2026-03-23
**Scope:** Backend + Frontend optimization pass

## Summary

Applied 6 optimizations across backend and frontend. All 1598 tests pass. Go build and React build both succeed.

---

## Frontend Optimizations

### 1. Code Splitting with React.lazy() (HIGH IMPACT)

**File:** `web/src/router.tsx`

- Converted 22 page imports from static `import` to `React.lazy()` dynamic imports
- Only `DashboardPage`, `SimListPage`, auth pages, and layout components remain eagerly loaded (most common entry points)
- Added `Suspense` wrapper with spinner fallback for all lazy routes
- Heavy modules now load on-demand:
  - **Policy Editor** (CodeMirror): loaded only when navigating to `/policies/:id`
  - **Analytics pages** (Recharts): loaded only when navigating to `/analytics/*`

### 2. Vite Manual Chunks (HIGH IMPACT)

**File:** `web/vite.config.ts`

Split vendor bundle into 6 separate chunks for better caching and parallel loading:

| Chunk | Content | Gzip Size |
|-------|---------|-----------|
| `vendor-react` | react, react-dom, react-router-dom | 25.74 KB |
| `vendor-ui` | lucide-react, clsx, tailwind-merge, cva, cmdk, sonner | 41.40 KB |
| `vendor-query` | @tanstack/react-query | 12.95 KB |
| `vendor-data` | axios, zustand | 15.16 KB |
| `vendor-charts` | recharts (lazy-loaded) | 118.52 KB |
| `vendor-codemirror` | @codemirror/* (lazy-loaded) | 112.23 KB |

**Before:** Single monolithic bundle ~440 KB gzipped initial load
**After:** ~184 KB gzipped initial load (dashboard page), heavy libs lazy-loaded

---

## Backend Optimizations

### 3. Dashboard API Parallelization (HIGH IMPACT)

**File:** `internal/api/dashboard/handler.go`

The dashboard endpoint (`GET /api/dashboard`) previously executed 4 independent DB queries sequentially:
1. `CountByState` (SIM stats)
2. `GetActiveStats` (session stats)
3. `ListGrantsWithOperators` (operator health)
4. `ListByTenant` anomalies (recent alerts)

Now executes all 4 in parallel using `sync.WaitGroup` + goroutines with `sync.Mutex` for result assembly. Expected improvement: ~4x faster dashboard load on cold cache (from ~200ms to ~50ms with typical query times).

### 4. PostgreSQL Connection Pool Tuning (MEDIUM IMPACT)

**File:** `internal/store/postgres.go`

Added two missing pool configuration parameters:
- `MaxConnIdleTime = 5 min`: Closes connections idle too long, preventing stale connection issues
- `HealthCheckPeriod = 30s`: Periodic connection health checks to detect and replace dead connections

### 5. Redis Connection Pool Tuning (MEDIUM IMPACT)

**File:** `internal/cache/redis.go`

Added pool optimization parameters:
- `MinIdleConns = poolSize / 4`: Keeps warm connections ready, reducing cold-start latency
- `ConnMaxIdleTime = 5 min`: Prunes idle connections to free resources

### 6. Unbounded Query Guard (LOW IMPACT, SAFETY)

**File:** `internal/store/usage_analytics.go`

Added `LIMIT 50` to `GetBreakdowns` query which previously had no row limit. With high cardinality dimensions (operator_id, apn_id), this could return thousands of rows unnecessarily.

---

## Analysis: Existing Good Practices

The codebase already implements several performance best practices:

- **Cursor-based pagination**: All list endpoints use cursor pagination with `LIMIT N+1` pattern
- **TimescaleDB hypertables**: CDRs and sessions use hypertables with compression policies
- **Continuous aggregates**: `cdrs_hourly` and `cdrs_daily` avoid raw table scans for analytics
- **Partitioned tables**: SIMs partitioned by operator_id, audit_logs + sim_state_history by date
- **Comprehensive indexes**: All WHERE/JOIN columns have appropriate indexes
- **SoR caching**: Steering-of-Roaming engine has in-memory + Redis cache layer
- **Dry-run caching**: Policy dry-run results cached in Redis with 5-min TTL
- **Connection pooling**: pgxpool already configured with configurable limits

## Recommendations (Not Implemented)

1. **Dashboard Redis cache**: Cache dashboard response for 30s to avoid 4 DB queries per page load
2. **SIM list query optimization**: The ILIKE search (`iccid ILIKE '%term%'`) cannot use indexes; consider pg_trgm GIN index for text search
3. **CDR export streaming**: `StreamForExport` has no LIMIT and no batch cursor -- could OOM on large exports. Consider `FETCH FORWARD N` with server-side cursor
4. **Read replica routing**: Config has `DATABASE_READ_REPLICA_URL` defined but not used. Route analytics and list queries to read replica for write/read separation

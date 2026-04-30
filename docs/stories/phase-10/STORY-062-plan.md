# STORY-062 — Implementation Plan

## Goal
Apply the final five performance follow-ups flagged by perf-optimizer (R1–R5), sweep every documentation drift accumulated across 55 story reviews, document 11 undocumented backend endpoints, and close the 4 open Tech Debt items (D-003, D-010, D-011, D-012) — in a single zero-deferral pass so the Documentation Phase can trust both code and docs.

---

## Architecture Context

### Components Involved
- **SVC-01 API Gateway** — dashboard handler cache TTL + NATS invalidation wiring
- **SVC-07 Analytics** — CDR export (already streams; verify + harden), audit cursor/bounds
- **SVC-10 Audit** — `GetByDateRange` guard + cursor path, streaming export (already CSV-stream, bounds check needed)
- **SVC-04/05 AAA** — session active-count Redis counter fed by `session.started`/`session.ended` NATS events
- **Admin impersonation** — `ImpersonateExit` returns fresh admin JWT (D-011); frontend claim access fix (D-012)
- **Docs subsystem** — `docs/architecture/*` markdown files, `docs/GLOSSARY.md`, `docs/ROUTEMAP.md`, story/plan files

### Data Flow
- **Dashboard cache**: `GET /api/v1/dashboard` → Redis `GET dashboard:{tenant}` → hit: return; miss: run 6 parallel queries → `SET dashboard:{tenant} EX 30`. Invalidation: NATS subscribers on `sim.*`, `session.*`, `operator.health_changed`, `cdr.recorded` → `DEL dashboard:{tenant}`.
- **Active sessions counter**: NATS `session.started` → `INCR sessions:active:count:{tenant}`; `session.ended` → `DECR`. Hourly reconciler runs `SELECT COUNT(*) WHERE session_state='active' GROUP BY tenant_id` → `SET` the counter. Dashboard `GetActiveStats` reads the counter first, DB fallback.
- **Audit export**: already streams via `cursor + LIMIT 500` loop; add `from`/`to` required validation with 90-day cap + `INVALID_DATE_RANGE` error.
- **MSISDN bulk import**: replace per-row `INSERT` loop with chunked `INSERT ... VALUES (...),(...),... ON CONFLICT DO NOTHING` in batches of 500; collect skipped rows via `RETURNING id` diff.
- **ImpersonateExit**: read `act_sub` from current Claims → `userStore.GetByIDGlobal(adminID)` → `auth.GenerateToken(adminID, …, 60m)` → `{jwt, user, tenant}` envelope.

### API Specifications
| Method | Path | Auth | Purpose | AC |
|--------|------|------|---------|-----|
| GET | `/api/v1/dashboard` | tenant+role | Cached 30s + NATS-invalidated | AC-1 |
| POST | `/api/v1/msisdn-pool/import` | msisdn_admin | Batch insert (existing path, new impl) | AC-2 |
| GET | `/api/v1/cdrs/export.csv` | analyst+ | Verified already streaming (no change) | AC-3 |
| GET | `/api/v1/dashboard` (active_sessions field) | tenant+role | Redis counter read | AC-4 |
| GET | `/api/v1/audit-logs?from=&to=` | audit_viewer | `from`/`to` required, ≤90 days | AC-5 |
| GET | `/api/v1/audit-logs/export.csv?from=&to=` | audit_viewer | Same bounds + stream (already streams) | AC-5 |
| GET | `/api/v1/sessions/export.csv` | session_viewer | **NEW** streaming CSV | D-010 |
| POST | `/api/v1/admin/impersonate/exit` | super_admin (impersonating) | Returns restored admin JWT | D-011 |

### Database Schema
No schema changes. One optional index (already in `20260323000002_perf_indexes.up.sql`): partial index on `sessions(session_state)` backs the hourly reconciler full-count.

### Existing Components to REUSE
- `internal/export/csv.go` → `StreamCSV` + `BuildFilename` (reuse for new session export)
- `internal/apierr/apierr.go` → `WriteError`, `CodeInvalidDateRange` (add new code if absent)
- `internal/bus/nats.go` → `SubscribeDurable` wrapper for cache invalidation subscribers
- `internal/cache/redis.go` → `RedisClient` (already injected into dashboard handler)
- `internal/store/session_radius.go` → `List`, `CountActive` (for export + reconciler)
- Existing ListCDRParams/ListAuditParams cursor loop pattern in `api/cdr/export.go` and `api/audit/export.go`
- `auth.GenerateToken(secret, adminID, adminTenantID, "super_admin", 60m, false)` for D-011
- Frontend `useExport('sessions')` already calls `/api/v1/sessions/export.csv` — no FE change needed once backend lands

---

## Prerequisites
- STORY-056..061 merged (code baseline stable).
- No migrations to run. Redis, NATS, PG already up.
- Frontend build green before/after `use-impersonation.ts` claim fix (D-012).

---

## Tech Debt (from ROUTEMAP)
All 4 open items are absorbed into this story:
- **D-003** (STORY-058 Review, OPEN): Stale SCR IDs (SCR-045/075/070/071/072/080/060/100) in story+plan files — doc drift only. Replace with current SCREENS.md numbering via `grep -l` + `sed`-style edits.
- **D-010** (STORY-077 Gate, OPEN): Sessions + alerts CSV export gap.
  - **Sessions**: no backend handler/route (FE `useExport('sessions')` 404s). Add `internal/api/session/export.go` + route `GET /api/v1/sessions/export.csv` under existing session auth.
  - **Alerts**: FE calls `useExport('anomalies')` → `/api/v1/anomalies/export.csv`, but router mounts under `/api/v1/analytics/anomalies/export.csv`. Fix by changing `web/src/pages/alerts/index.tsx:650` to `useExport('analytics/anomalies')` (resource-path passthrough — `use-export.ts:22` interpolates the string verbatim).
- **D-011** (STORY-077 Gate, OPEN): `ImpersonateExit` returns only `{message}`. Rewrite to extract `act_sub` from Claims, fetch admin user via `userStore.GetByIDGlobal`, generate fresh admin JWT via `auth.GenerateToken`, return `{jwt, user{id,email,role}, tenant{id,name}}` — same envelope as `Impersonate`. Frontend `exitImpersonation.onSuccess` already reads `data.jwt`, no FE change required.
- **D-012** (STORY-077 Gate, OPEN): `use-impersonation.ts:28` reads `payload.act?.sub`; JWT serializes claim as flat `act_sub`. One-line frontend fix: `payload.act_sub ?? null`. Keep Go struct tag as-is (fewer downstream touches).

Mark all 4 RESOLVED in ROUTEMAP Tech Debt table at commit time.

---

## Story-Specific Compliance Rules
- **Project conventions**: API envelope `{status,data,meta?,error?}`; cursor pagination (not offset) everywhere; tenant scoping enforced in store; kebab-case routes; snake_case DB; camelCase Go; PascalCase React.
- **Zero-deferral** (Phase 10 mandate): every AC and every D-* item closed in-story; no "follow-up" bullets.
- **Doc-code parity verification** must be automated at close-out:
  - `grep -r "internal/gateway/errors.go" docs/` → 0 matches.
  - Every `ERROR_CODES.md` code exists in `internal/apierr/apierr.go` and vice-versa (script or manual grep).
  - Every `CONFIG.md` env var appears as a struct tag in `internal/config/config.go`.
  - Every endpoint in `api/_index.md` maps to a `r.Get/Post/Put/Delete` in `internal/gateway/router.go`.
- **No schema migration**: if the 30s dashboard TTL or Redis counter requires new keys, reuse existing namespaces (`dashboard:*`, `sessions:active:count:*`) and document them in CONFIG.md (AC-7 already covers this).
- **Audit trail**: no state-changing admin action without an audit entry — ImpersonateExit must emit `admin.impersonate_exit` entry (mirrors `admin.impersonate`).

---

## Bug Pattern Warnings
From `docs/brainstorming/decisions.md` and prior-story review history:
- **Cursor pagination**: when chunking audit/session/MSISDN, remember `LIMIT N+1` pattern and advance cursor from the last-returned row's `(created_at, id)` tuple. Don't break early on `len < limit` mid-chunk.
- **Cache invalidation race**: NATS subscriber must `DEL` **before** the next DB-refresh request; put the `DEL` at message receipt, not after any downstream processing. If subscriber is async via JetStream, use `AckNone` queue-group fan-out — we want every replica to invalidate its own local view (but with centralized Redis, one `DEL` per message is sufficient).
- **NATS consumer durability**: the dashboard-cache invalidator should use a **non-durable queue subscription** (ephemeral) — missing events during downtime are harmless because TTL will expire the key within 30s anyway. Don't carry persistent state for a cache-invalidation listener.
- **Bulk INSERT partial failure**: `ON CONFLICT DO NOTHING` silently drops duplicates — capture the diff via `RETURNING id` count vs. input count to populate the skip report. Don't assume success == input size.
- **Redis counter drift**: NATS events can be lost on broker restart → hourly reconciler is mandatory, not optional. Reconciler must scope by tenant and use `SET` (not `INCRBY`) to overwrite drift.
- **JWT flat-vs-nested claim** (D-012 root cause): standard JWT `act` claim is canonically an object `{"sub": "..."}` per RFC 8693. The Go struct uses a flat custom name `act_sub` to sidestep nested struct marshaling. Frontend must match the wire format, not the RFC ideal. Document this in code as the reason.
- **ImpersonateExit claim read** (D-011): before generating the admin JWT, verify `claims.Impersonated == true` and `claims.ImpersonatedBy != nil`; reject with 400 otherwise. The session may have been extended/re-issued and is no longer an impersonation.
- **MSISDN batch commit scope**: per-chunk transaction (not one big TX for 10K rows) to avoid long-held locks on `msisdn_pool`. Each 500-row chunk gets its own `BEGIN/COMMIT`.

---

## Risks & Mitigations
| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| NATS invalidator missed event → stale dashboard | Low | Low | 30s TTL caps staleness window; reconciler not needed for cache |
| Redis counter drift under NATS outage | Medium | Low | Hourly reconciler overwrites counter with ground-truth COUNT(*) |
| MSISDN batch rollback silently drops rows | Low | High | `RETURNING id` + row-by-row skip report; per-chunk TX |
| Audit `from`/`to` required breaks internal callers | Medium | Medium | Grep repo for `GetByDateRange` callers; update to supply bounds; add alias/fallback that caps at 30d if args empty (only if callers found) |
| D-012 claim fix breaks impersonation flag on other pages | Low | Medium | `isImpersonating` still checks `payload.impersonated` (separate flag); only `impersonatedBy` display changes |
| ImpersonateExit JWT shaped differently than login JWT | Medium | Low | Use same `GenerateToken` + role lookup as login; add integration test |
| Doc drift sweep misses a file | Medium | Low | Post-sweep grep assertions in Test Scenarios are the safety net |
| `api/_index.md` total count skew after 11 endpoint additions | High | Low | Recompute footer total after each section edit; reconcile at close |

---

## Tasks

### Wave 1 — Doc sweep (parallelizable, no code deps)

**T1 — [D-003] Replace stale SCR IDs across story/plan files**
- Files: `docs/stories/phase-*/STORY-*-plan.md`, `docs/stories/phase-*/STORY-*-review.md` (34 matches from grep)
- Depends on: —
- Complexity: low
- Pattern ref: n/a (mechanical replace)
- Context: Tech Debt § D-003
- What: For each stale SCR ID (SCR-045/075/070/071/072/080/060/100), look up current number in `docs/SCREENS.md` and replace in-place. Preserve surrounding prose.
- Verify: `grep -rE "SCR-(045|075|070|071|072|080|060|100)\b" docs/stories/` returns 0 matches; SCREENS.md untouched.

**T2 — AC-6 ERROR_CODES.md drift fix**
- Files: `docs/architecture/ERROR_CODES.md`, `internal/apierr/apierr.go` (read-only verify)
- Depends on: —
- Complexity: low
- Pattern ref: existing ERROR_CODES table format
- Context: Story AC-6
- What: Replace file reference `internal/gateway/errors.go` → `internal/apierr/apierr.go`. Add missing codes: `MSISDN_NOT_FOUND`, `MSISDN_NOT_AVAILABLE`, `RESOURCE_LIMIT_EXCEEDED`, `TENANT_SUSPENDED`, and 5 eSIM codes (`PROFILE_ALREADY_ENABLED`, `NOT_ESIM`, `INVALID_PROFILE_STATE`, `SAME_PROFILE`, `DIFFERENT_SIM`). Each row: `CODE | HTTP | Description | Since story`.
- Verify: `grep "internal/gateway/errors.go" docs/ERROR_CODES.md` returns 0; every listed code exists in `apierr.go` grep.

**T3 — AC-7 CONFIG.md drift fix**
- Files: `docs/architecture/CONFIG.md`, `.env.example`, `internal/config/config.go` (read-only verify)
- Depends on: —
- Complexity: low
- Pattern ref: existing CONFIG sections
- Context: Story AC-7
- What:
  - NATS subjects section: add `alert.triggered`, `job.completed`, `job.progress`, `audit.created`.
  - Background Jobs: add `JOB_MAX_CONCURRENT_PER_TENANT`, `JOB_TIMEOUT_MINUTES`, `JOB_TIMEOUT_CHECK_INTERVAL`, `JOB_LOCK_TTL`, `JOB_LOCK_RENEW_INTERVAL`, `CRON_PURGE_SWEEP`, `CRON_IP_RECLAIM`, `CRON_SLA_REPORT`, `CRON_ENABLED`.
  - Redis namespaces: add `ENCRYPTION_KEY`, `ota:ratelimit:`, `operator:health:`, `sessions:active:count:`, `dashboard:` (latter two land as part of AC-1/AC-4 but doc them here).
  - Fix `DEPLOYMENT_MODE` authoritative values `single | cluster`; reconcile `.env.example`.
  - Rate-limit: `RATE_LIMIT_ALGORITHM`, `RATE_LIMIT_AUTH_PER_MINUTE`, `RATE_LIMIT_ENABLED`.
- Verify: each env var listed also appears in `config.go` struct tag grep; CI-style check: `go run ./scripts/verify-config.go` if present else manual.

**T4 — AC-8 ARCHITECTURE / DSL_GRAMMAR / ALGORITHMS drift**
- Files: `docs/ARCHITECTURE.md`, `docs/architecture/DSL_GRAMMAR.md`, `docs/architecture/ALGORITHMS.md`
- Depends on: —
- Complexity: low
- Pattern ref: existing Caching Strategy table
- Context: Story AC-8
- What:
  - ARCHITECTURE.md Caching Strategy table: rows for SoR cache, auth rate counters, auth latency window, dashboard cache (30s), active sessions counter.
  - Project tree: add `internal/aaa/rattype/`, `internal/ota/`, `internal/api/ota/`, `internal/api/cdr/`, `internal/analytics/cdr/`.
  - API count footer: count `API-\d+` entries in `api/_index.md` after AC-10/12 land, write exact number.
  - Docker services table: add `:8443` (5G SBA).
  - DSL_GRAMMAR.md: package path `internal/policy/dsl/` (was `pkg/dsl/`).
  - ALGORITHMS.md Section 5: path `internal/analytics/cdr/`.
- Verify: tree entries match `ls internal/` output; `grep "pkg/dsl" docs/` empty.

**T5 — AC-9 GLOSSARY.md terms batch**
- Files: `docs/GLOSSARY.md`
- Depends on: —
- Complexity: low
- Pattern ref: existing term format `**Term** — definition (Source: STORY-XXX)`
- Context: Story AC-9 (full term list in story file)
- What: Add ~20 missing terms: eSIM Profile State Machine, Profile Switch, SM-DP+ Adapter, SMS-PP, BIP, KIC, KID, GSM 03.48, TAR, APDU, SoR Decision, SoR Priority, Operator Lock, IMSI Prefix Routing, Cost-Based Selection, Metrics Collector, MetricsRecorder Interface, Metrics Pusher, System Health Status, Connectivity Diagnostics, Diagnostic Step, Usage Analytics, Period Resolution, Real-Time Aggregation, Cost Analytics, Optimization Suggestion, Cost Per MB, Rating Engine, Cost Aggregation, CDR Consumer, CDR Export, Bulk Operation, Undo Record, Partial Success, WS Server, WS Hub, WS Close Code, Pseudonymization Salt. Alphabetical insertion.
- Verify: every term from story AC-9 list appears via grep; glossary remains alphabetized.

**T6 — AC-11 ROUTEMAP.md reconciliation**
- Files: `docs/ROUTEMAP.md`
- Depends on: —
- Complexity: low
- Pattern ref: existing Phase header format
- Context: Story AC-11
- What: Phase 4 header `[PENDING]` → `[DONE]` (stale). Ensure Phase 10 section lists this story + counters updated (55/55 plus Phase 10 21/22 after this story, 22/22 at close). Tech Debt rows: D-003/D-010/D-011/D-012 stay OPEN here; they flip to RESOLVED at close-commit.
- Verify: grep `\[PENDING\]` in Phase 4 header → 0.

### Wave 2 — Perf fixes + D-012 (code changes, runnable independently)

**T7 — [AC-1] Dashboard cache TTL bump + NATS invalidation**
- Files: `internal/api/dashboard/handler.go`, `internal/api/dashboard/invalidator.go` (NEW), `cmd/argus/main.go` (wiring)
- Depends on: —
- Complexity: medium
- Pattern ref: `internal/bus/nats.go` subscribe wrapper; `internal/operator/sor/cache.go` for invalidation pattern
- Context: Architecture Context § Data Flow; Bug Patterns § cache invalidation race, NATS consumer durability
- What: (a) bump TTL from 15s → 30s in `handler.go:334`; (b) new `invalidator.go` exposes `RegisterDashboardInvalidator(nc *nats.Conn, rc *redis.Client, logger zerolog.Logger)` subscribing to `sim.*`, `session.*`, `operator.health_changed`, `cdr.recorded` using queue-group `dashboard-invalidator`; on each message extract `tenant_id` from payload JSON and `DEL dashboard:{tenant_id}` (fall back to cluster-wide DEL by keyspace scan only if tenant_id absent — prefer targeted delete); (c) wire from main.go during bootstrap after `nc` connect.
- Verify: manual — `nats pub session.started '{"tenant_id":"..."}'` → `redis-cli GET dashboard:{tenant}` returns nil; new test `TestDashboardInvalidatorDeletesKey` in `handler_test.go`.

**T8 — [AC-2] MSISDN bulk import batch INSERT**
- Files: `internal/store/msisdn.go`, `internal/store/msisdn_test.go`
- Depends on: —
- Complexity: medium
- Pattern ref: `internal/store/policy.go:885` post-fix (batched VALUES + ON CONFLICT DO NOTHING)
- Context: perf-optimizer-report.md R2; Bug Patterns § Bulk INSERT partial failure, MSISDN batch commit scope
- What: Replace per-row loop (msisdn.go:191-214) with chunks of 500 rows; build `INSERT INTO msisdn_pool (tenant_id, operator_id, msisdn, state) VALUES ($1,$2,$3,'available'), ($4,$5,$6,'available'), ... ON CONFLICT (msisdn) DO NOTHING RETURNING msisdn`; the input-minus-returned set is the skipped/duplicate set; populate `MSISDNImportError` rows with `"duplicate"` message. Per-chunk `BEGIN/COMMIT`.
- Verify: existing `msisdn_test.go` tests pass; add `TestMSISDNStore_BulkImport_BatchSize500` with 1200 rows mixing new+dup.

**T9 — [AC-4] Active sessions Redis counter + reconciler**
- Files: `internal/store/session_radius.go` (add `GetActiveCountCached`), `internal/aaa/session/counter.go` (NEW — subscribers + reconciler), `cmd/argus/main.go` (wiring), `internal/api/dashboard/handler.go` (read counter first)
- Depends on: —
- Complexity: high
- Pattern ref: `internal/analytics/metrics/recorder.go` for INCR/TTL pattern; cron hook pattern in `internal/job/scheduler.go`
- Context: perf-optimizer-report.md R4; Bug Patterns § Redis counter drift
- What: (a) NATS subscribers on `session.started`/`session.ended` → `INCR`/`DECR sessions:active:count:{tenant_id}`; (b) hourly cron (`CRON_SESSION_COUNT_RECONCILE`) runs `SELECT tenant_id, COUNT(*) FROM sessions WHERE session_state='active' GROUP BY tenant_id` and `SET`s each counter; (c) dashboard handler (the goroutine at line 153) reads counter first, falls back to `CountActive` only if Redis returns nil/err; (d) expose counter via `GetActiveStats` as well.
- Verify: integration test spawns 5 session.started events and expects counter == 5; reconciler test forces drift and verifies correction within 1 tick; drift budget <1% asserted.

**T10 — [AC-5] Audit date-range bounds + cursor bounds**
- Files: `internal/store/audit.go`, `internal/api/audit/handler.go`, `internal/apierr/apierr.go` (maybe new `CodeInvalidDateRange`)
- Depends on: —
- Complexity: medium
- Pattern ref: `internal/api/cdr/export.go` streaming; existing handler validation patterns
- Context: Story AC-5; Risks § required bounds breaking callers
- What: (a) `GetByDateRange` signature unchanged but add internal cap (reject if `to-from > 90 days`); better: new `ListByDateRange(ctx, tenant, from, to, cursor, limit)` cursor variant, leave old name as thin wrapper that returns `ErrDateRangeRequired` if zero value; (b) handler-level: return 400 `INVALID_DATE_RANGE` if `from`/`to` missing or > 90d; (c) grep callers of `GetByDateRange` and supply defaults where safe; (d) audit export (`internal/api/audit/export.go`) already streams — just add bounds validation.
- Verify: `GET /api/v1/audit-logs?from=2020-01-01` → 400 `INVALID_DATE_RANGE`; `GET /api/v1/audit-logs?from=X&to=Y` with 180-day span → 400; `GET /api/v1/audit-logs/export.csv?from=X&to=Y` with 1M rows under 90d → heap stable.

**T11 — [AC-3] CDR export cursor verification (no-op + note)**
- Files: `docs/stories/phase-10/STORY-062-step-log.txt`, `docs/architecture/ALGORITHMS.md` (add section)
- Depends on: —
- Complexity: low
- Pattern ref: `internal/api/cdr/export.go` (already-correct impl)
- Context: Story AC-3; perf-optimizer R3
- What: Confirm `internal/api/cdr/export.go:41-80` already streams via cursor pagination (500-row chunks via `ListByTenant`, never buffers). Document in ALGORITHMS.md under new "CDR Export Streaming" subsection (package + flow). Add benchmark or confirm via `pprof` heap profile on 1M-row fixture (if benchmark infra available); else record manual heap check in step-log.
- Verify: code review sign-off; `curl /api/v1/cdrs/export.csv` against 1M fixture → RSS stays flat; ALGORITHMS.md has CDR section.

**T12 — [D-012] Frontend `impersonatedBy` claim access fix**
- Files: `web/src/hooks/use-impersonation.ts`
- Depends on: —
- Complexity: low
- Pattern ref: line 18 `payload.impersonated` reads flat claim correctly
- Context: Tech Debt § D-012; Bug Patterns § JWT flat-vs-nested claim
- What: Change line 28 from `return payload.act?.sub ?? null` to `return payload.act_sub ?? null`. Add inline code comment referencing `internal/auth/jwt.go:25` flat serialization rationale.
- Verify: login as admin → impersonate user → topbar banner shows admin's UUID; `payload.act_sub` debug-logged once matches `Claims.ImpersonatedBy`.

### Wave 3 — D-010 + D-011 + AC-10/AC-12 (depends on Wave 2 stability)

**T13 — [D-010] Sessions CSV export endpoint**
- Files: `internal/api/session/export.go` (NEW), `internal/gateway/router.go`
- Depends on: T11 cursor pattern reference
- Complexity: medium
- Pattern ref: `internal/api/cdr/export.go`, `internal/api/audit/export.go`
- Context: Tech Debt § D-010
- What: New `ExportCSV` handler on session handler struct; cursor-paginate via `sessionStore.List` or `ListByTenant`; CSV columns: `id, sim_id, imsi, operator_id, apn_id, rat_type, session_state, started_at, ended_at, bytes_in, bytes_out, framed_ip`; filter params `operator_id`, `apn_id`, `session_state`, `from`, `to`. Route: `r.Get("/api/v1/sessions/export.csv", deps.SessionHandler.ExportCSV)` under the same role wall as `/sessions`.
- Verify: FE `useExport('sessions')` 200s and downloads file; manual 10K-row export RSS stable.

**T14 — [D-010 pt.2] Alerts export resource-path fix**
- Files: `web/src/pages/alerts/index.tsx:650`
- Depends on: —
- Complexity: low
- Pattern ref: `useExport('analytics/anomalies')` — hook already supports slash-containing resource strings via string interpolation in `use-export.ts:22`
- Context: Tech Debt § D-010
- What: Change `useExport('anomalies')` → `useExport('analytics/anomalies')` so FE URL matches backend mount `/api/v1/analytics/anomalies/export.csv`.
- Verify: alerts page Export button → 200 + CSV download (no 404).

**T15 — [D-011] ImpersonateExit returns admin JWT**
- Files: `internal/api/admin/impersonate.go`, `internal/api/admin/handler.go` (add userStore/jwtSecret wiring if missing)
- Depends on: T12 (ensures FE onSuccess path reads `data.jwt` — already does)
- Complexity: medium
- Pattern ref: `Impersonate` handler in same file (mirror structure)
- Context: Tech Debt § D-011; Bug Patterns § ImpersonateExit claim read
- What: (a) Extract current Claims from context via middleware-injected key; if `!claims.Impersonated || claims.ImpersonatedBy == nil` → 400 `BAD_REQUEST` "not in impersonation session"; (b) `adminID := *claims.ImpersonatedBy`; `admin, err := h.userStore.GetByIDGlobal(ctx, adminID)`; (c) `jwtStr := auth.GenerateToken(h.jwtSecret, admin.ID, admin.TenantID, admin.Role, time.Hour, false)`; (d) audit log `admin.impersonate_exit` with target=former impersonated user; (e) return `{jwt, user_id, email, tenant_id, role}` envelope matching `impersonateResponse`.
- Verify: e2e — impersonate → topbar "Exit" → API returns JWT; frontend stores and redirects; admin banner disappears; second click without impersonation → 400.

**T16 — [AC-10] db/_index + api/_index supplementary entries + USERTEST path fixes**
- Files: `docs/architecture/db/_index.md`, `docs/architecture/api/_index.md`, `docs/USERTEST.md`
- Depends on: T1–T6 (doc sweep baseline)
- Complexity: medium
- Pattern ref: existing API section row format
- Context: Story AC-10
- What:
  - db: add TBL-25, TBL-26 (ota_commands), TBL-27 (anomalies) with columns/indexes summary.
  - api: add supplementary IDs API-061b, API-061c, API-062b (identify owner sections); refresh per-section counts.
  - USERTEST.md: fix OTA paths (mirror corrections made in STORY-029 review); confirm STORY-030 `/errors` vs `/error-report` corrected.
- Verify: each new TBL-* appears in `db/*.md` sub-pages; USERTEST `curl` examples lint.

**T17 — [AC-12] Document 11 undocumented endpoints**
- Files: `docs/architecture/api/_index.md`
- Depends on: T16 (base structure updated first)
- Complexity: medium
- Pattern ref: existing API-### row format, cross-link `(STORY-0XX)`
- Context: Story AC-12; compliance-audit-report.md "Undocumented endpoints in code"
- What: Add rows for:
  - `GET /api/v1/ota-commands/{commandId}` (STORY-029) — assign API-172
  - `POST /api/v1/sims/{id}/ota` (STORY-029) — API-173
  - `POST /api/v1/sims/bulk/ota` (STORY-029) — API-174
  - `GET /api/v1/policy-versions/{id1}/diff/{id2}` (STORY-023) — new API ID under Policies
  - `GET /api/v1/policy-violations` (STORY-025) — new ID
  - `GET /api/v1/policy-violations/counts` (STORY-025) — new ID
  - `GET /api/v1/notifications/unread-count` (STORY-038) — supplementary API-130b
  - `GET /api/v1/analytics/anomalies/{id}` (STORY-036) — API-113b
  - `GET /api/v1/operator-grants/{id}` (STORY-009) — API-026b
  - `GET /api/v1/policy-versions/{id}` (STORY-023) — API-094b
  - `/api/v1/audit` alias (STORY-007) — document as "Alias of `/audit-logs` for backward compatibility" OR remove from `router.go` if never used (grep FE first).
  - Refresh footer: `Total: N REST endpoints` to the actual count post-additions.
- Verify: router.go line count of `r.(Get|Post|Put|Delete|Patch)` equals api/_index.md endpoint count; `grep "/api/v1/audit"` — if FE uses `/audit-logs` only, remove alias; otherwise document.

### Wave 4 — Final validation + ROUTEMAP close

**T18 — Close-out ROUTEMAP tech-debt + step log**
- Files: `docs/ROUTEMAP.md`, `docs/stories/phase-10/STORY-062-step-log.txt`
- Depends on: T1–T17
- Complexity: low
- Pattern ref: prior stories' close-out commits
- Context: Zero-deferral policy
- What: Flip D-003/D-010/D-011/D-012 status OPEN → ✓ RESOLVED (2026-04-13). Run final verification greps from Test Scenarios; record outputs in step-log (zero matches asserted).
- Verify: all 4 debt rows show RESOLVED with date; Test Scenarios section's 3 grep/verify lines all pass; `go build ./... && go test ./... && (cd web && npm run build)` all green.

---

## Acceptance Criteria Mapping

| AC / Debt | Tasks | Primary Files | Test |
|-----------|-------|---------------|------|
| AC-1 Dashboard cache + invalidation | T7 | handler.go, invalidator.go, main.go | `TestDashboardInvalidatorDeletesKey` |
| AC-2 MSISDN batch insert | T8 | msisdn.go | `TestMSISDNStore_BulkImport_BatchSize500` |
| AC-3 CDR export cursor | T11 | export.go (verify), ALGORITHMS.md | heap check |
| AC-4 Active sessions counter | T9 | counter.go, dashboard/handler.go | drift<1% test |
| AC-5 Audit date-range bounds | T10 | audit.go, audit/handler.go | 400 on unbounded |
| AC-6 ERROR_CODES.md | T2 | ERROR_CODES.md | grep assertions |
| AC-7 CONFIG.md | T3 | CONFIG.md, .env.example | struct tag grep |
| AC-8 ARCHITECTURE/DSL/ALGORITHMS | T4 | three .md files | path grep |
| AC-9 GLOSSARY.md | T5 | GLOSSARY.md | term grep |
| AC-10 db/api _index, USERTEST | T16 | db/_index.md, api/_index.md, USERTEST.md | visual |
| AC-11 ROUTEMAP | T6, T18 | ROUTEMAP.md | grep [PENDING] |
| AC-12 11 undocumented endpoints | T17 | api/_index.md | router count match |
| D-003 Stale SCR IDs | T1 | stories/ | grep assertion |
| D-010 Sessions/alerts export | T13, T14 | session/export.go, router.go, alerts/index.tsx | e2e CSV download |
| D-011 ImpersonateExit JWT | T15 | impersonate.go | e2e exit restores admin |
| D-012 impersonatedBy claim | T12 | use-impersonation.ts | banner shows admin ID |

---

## Wave Summary

- **Wave 1 (T1–T6)** — Pure doc edits, all parallel, 0 code risk. ~50% of total LOC touched.
- **Wave 2 (T7–T12)** — Perf + D-012 code changes, each independent. Contains the only **high-complexity** task (T9 — NATS subscribers + hourly reconciler + fallback chain).
- **Wave 3 (T13–T17)** — Depends on Wave 2 patterns (session export reuses cursor loop; ImpersonateExit relies on JWT claim conventions validated in T12). AC-10/AC-12 doc additions land after code to reflect final state.
- **Wave 4 (T18)** — Close-out and verification grep.

Total: **18 tasks, 4 waves**. One high-complexity (T9), six medium-complexity, eleven low-complexity — appropriate spread for an M-sized final cleanup story.

# SCALE — 10M SIM Operational Patterns (FIX-236)

This document records the patterns Argus uses to remain responsive at fleet
sizes of 1M+ SIMs (the design target is 10M). It is the home for the
**bulk action contract**, **streaming export contract**, **virtual scrolling
rules**, and the **audit table mapping row-actions → bulk counterparts** that
FIX-236's `AC-11` calls for.

Maintenance: every list-page UI story that introduces or modifies a row
action MUST update the audit table at the bottom of this file in the same
commit.

---

## 1. Bulk action contract

Bulk operations against the SIM fleet have **three** entry shapes today.
Pages should pick the most permissive shape they can support.

### 1.1 Per-id (`sim_ids[]`)

The legacy entry. Caller passes an explicit list of SIM UUIDs; server
filters tenant-ownership, materialises a job, returns a `job_id`.

| Endpoint | Audit |
|----------|-------|
| `POST /api/v1/sims/bulk/state-change` | `bulk.state_change.started` |
| `POST /api/v1/sims/bulk/policy-assign` | `bulk.policy_assign.started` |
| `POST /api/v1/sims/bulk/operator-switch` | `bulk.operator_switch.started` |
| `POST /api/v1/sims/bulk/import` | `bulk.import.started` |

Cap: `len(sim_ids) ≤ 10000` per request (`maxBulkSimIDs`).

### 1.2 Saved-Segment (`segment_id`)

Caller references a previously-created `sim_segments` row. The Segment
filter is JSON in the row; the bulk handler resolves it via
`SegmentStore.CountMatchingSIMs` / `ListMatchingSIMIDs`. Used for repeated
flows ("apply policy X to fleet-Y every time").

### 1.3 Filter-by-URL (FIX-236, ad-hoc)

Caller passes the same filter shape used by `GET /api/v1/sims` directly into
the bulk endpoint. Server resolves on the spot via
`SIMStore.ListIDsByFilter`. **Per-request cap 10000**; the endpoint returns
a 422 `limit_exceeded` carrying the actual matching count when the cap is
hit, forcing callers to narrow the filter or switch to a saved Segment.

| Endpoint | Audit | Cap |
|----------|-------|-----|
| `POST /api/v1/sims/bulk/preview-count` | (no audit — read-only) | 10000 |
| `POST /api/v1/sims/bulk/state-change-by-filter` | `bulk.state_change.by_filter_started` | 10000 |
| `POST /api/v1/sims/bulk/policy-assign-by-filter` | `bulk.policy_assign.by_filter_started` | 10000 |
| `POST /api/v1/sims/bulk/operator-switch-by-filter` | `bulk.operator_switch.by_filter_started` | 10000 |

**Double-confirm rule:** the FE MUST call `preview-count` before dispatching
a `*-by-filter` action whose result count exceeds **1000**. The preview
returns `{count, sample_ids: up-to-5, capped, cap}` so the dialog can show
the user a recognisable subset.

### 1.4 Per-id audit fidelity

All three shapes terminate in the same JobStore + bus pipeline. Each
processed SIM emits a single audit row in the worker — the bulk path does
not collapse audits.

---

## 2. Streaming export contract

`internal/export/csv.go` provides `StreamCSV(w, filename, header, rowsFn)`:

- `Content-Type: text/csv; charset=utf-8`
- `Content-Disposition: attachment; filename="..."` (sanitised by `BuildFilename`)
- `X-Content-Type-Options: nosniff`
- `Cache-Control: no-cache`
- Flushes the underlying `http.Flusher` every 500 rows so the browser
  receives chunks incrementally; large exports (10K+ rows) start streaming
  the first KB within ~50ms regardless of total size
- Rows are written via a generator callback — the caller controls cursor
  pagination + memory footprint

Endpoints using this contract today:
- `GET /api/v1/sims/export.csv`
- `GET /api/v1/policy-violations/export.csv`
- `GET /api/v1/jobs/export.csv`
- `GET /api/v1/jobs/{id}/errors?format=csv` (streams the failed-id report)

New endpoints SHOULD use `StreamCSV` rather than buffering rows in memory.
For >100K rows, prefer the job-based export pattern (see Reports — FIX-248).

---

## 3. Virtual scrolling rules

`@tanstack/react-virtual` ships in deps; the project provides a wrapper at
`web/src/components/shared/virtual-table.tsx` (FIX-236 DEV-550).

**When to use VirtualTable:**
- Any list that can hold more than **500** visible rows in a session
- Any list whose row height is uniform or predictable

**Rules:**
- Pass an explicit `rowHeight` (number or estimator). Variable estimators
  must cache results; otherwise scroll-jitter kicks in.
- Use the `onLoadMore`/`hasMore` props for infinite-scroll pages — sentinel
  is server-side.
- The component disables virtualisation under `@media print` so paper
  output renders every row. Do NOT add a parallel "expand all" button.
- Sticky headers: pass via `header={...}` — the wrapper applies `sticky top-0`.

**When NOT to use:**
- Lists with always-visible aggregated rows (e.g. dashboards) — virtualisation
  hurts when the row count is small and dynamic-content-driven.

Pages that should adopt over time (each in its own follow-up story):
SIMs, Sessions, Audit Log, Jobs, Alerts, Violations.

---

## 4. Rate-limit topology

Rate limits live in **three** places today:

| Module | Surface | What it limits |
|--------|---------|----------------|
| `internal/gateway/ratelimit.go` | Chi middleware | Per-tenant per-route HTTP request rate |
| `internal/gateway/bulk_ratelimit.go` | Chi middleware (mounted on /sims/bulk/*) | Bulk endpoint ops/sec; 1 req/s default |
| `internal/notification/redis_ratelimiter.go` | Redis token bucket | Notification fan-out (email, telegram, webhook) |
| `internal/ota/ratelimit.go` | In-process | OTA SMS/HTTP push rate |

The **filter-based bulk endpoints** (FIX-236) inherit the existing `bulk_ratelimit`
middleware automatically — they are mounted in the same role-guarded block.

**When you add a new rate limit:** prefer extending `redis_ratelimiter` (it
is the only Redis-distributed bucket today) over a new package. Add metrics
under `argus_ratelimit_rejected_total{kind="..."}`.

---

## 5. Partition strategy (today)

`sims` table: PARTITION BY LIST (`operator_id`).

Rationale at design time:
- M2M operators tend to be a small set (≤ 50 in production); LIST is
  efficient for this cardinality.
- Operator-scoped queries (most-common access pattern from operator portal)
  hit a single partition.

**Open question (D-163):** at 10M SIMs spread across 50 operators, the
average partition is 200K rows — fine. At 10M+ SIMs spread across 5
operators (large M2M deals), one partition holds 2M+ rows and re-partition
benefit erodes. Revisit when seed data exists; consider tenant-LIST or
hash partitioning if benchmarks confirm.

The schema migration scaffolding to re-partition is non-trivial: `pg_partman`
or manual swap-and-rename; both require an operational window. Don't refactor
without a specific p95 regression to chase.

---

## 6. Audit table — row actions ↔ bulk counterparts

Every row-actionable widget should have a bulk counterpart at scale. This
table is the source-of-truth registry. Future stories add rows; renames
update existing rows in-place.

| Page | Row action | Bulk-by-id counterpart | Bulk-by-filter counterpart | Notes |
|------|-----------|------------------------|---------------------------|-------|
| SIMs list | Suspend / Activate / Terminate / Report Lost | `POST /sims/bulk/state-change` | `POST /sims/bulk/state-change-by-filter` | FIX-201 + FIX-236 |
| SIMs list | Assign Policy | `POST /sims/bulk/policy-assign` | `POST /sims/bulk/policy-assign-by-filter` | FIX-201 + FIX-236 |
| SIMs list | Switch Operator (eSIM) | `POST /sims/bulk/operator-switch` | `POST /sims/bulk/operator-switch-by-filter` | FIX-201 + FIX-236 |
| Sessions list | Force Stop (Disconnect) | — (per-row only today) | — | D-162 — adopt in future Sessions FIX |
| Violations | Acknowledge / Dismiss | `POST /policy-violations/bulk/{acknowledge,dismiss}` | — (filter-based deferred to D-157) | FIX-244; cap 100 ids/req |
| Operators list | Toggle live state | — | — | Low-cardinality (rarely >50 rows); no bulk needed |
| APNs list | Bind/unbind | — | — | Low-cardinality; per-row sufficient |
| Policies list | Archive | — | — | D-162 — bulk archive on demand only |
| Alerts | Resolve / Mute | — | — | D-162 — adopt as needed |
| eSIM | Switch profile | — | — | FIX-235 covers eSIM bulk in its own story |
| Audit Log | (no row actions — read-only) | — | — | — |
| Jobs | Cancel / Retry | — | — | Job-level actions are inherently per-id |

---

## 7. Out of scope (deferred)

| ID | Item | Rationale |
|----|------|-----------|
| D-162 | Adoption of shared `BulkActionBar` + `VirtualTable` + filter-based bulk on Sessions / eSIM / Audit / Operators / APNs / Policies / Alerts | Pattern is established by FIX-236 primitives; per-page adoption goes to each owning future FIX |
| D-163 | Partition strategy refactor (tenant-LIST vs operator-LIST) | Decision blocked on 10M-seed benchmark — no signal today |
| D-164 | Benchmark suite (k6 + 10M-row seed) | Heavy infra (CI test env, baseline corpus); separate dedicated perf story |

---

*Last updated 2026-04-27 — FIX-236 DEV-554.*

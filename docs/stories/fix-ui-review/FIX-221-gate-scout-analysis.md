# FIX-221 Gate Scout — Analysis

Scope: backend DTO correctness, SQL correctness, concurrency, cache behavior.

## Files reviewed
- `internal/store/cdr.go` — `GetTrafficHeatmap7x24WithRaw` + `TrafficHeatmapCellRaw` type (lines 952-1017).
- `internal/store/ippool.go` — `TopPoolUsage` + `TopPoolSummary` type (lines 110-138).
- `internal/api/dashboard/handler.go` — `trafficHeatmapCell` DTO, `topIPPoolDTO`, `dashboardDTO`, goroutines at `:389-413` (ippool) and `:440-467` (heatmap).

## Findings

### F-A1 LOW — `GetTrafficHeatmap7x24WithRaw` duplicates SQL + normalization logic of its sibling
- Evidence: `internal/store/cdr.go:966-1016` reuses the exact SQL (COALESCE sum + ISODOW TZ convention) and max-normalization loop present in `GetTrafficHeatmap7x24` (`:1018-1069`). Two near-identical functions — maintenance risk if one is updated (SQL/TZ/index) and the other drifts.
- Severity: LOW. Pure code-quality smell; behaviour correct, shapes differ (matrix vs. flat slice).
- Classification: DEFERRED to FIX-24x cleanup wave (tracked as D-114).
- Note: per plan Risk #2 & Task 1, the old `GetTrafficHeatmap7x24` is intentionally retained to keep `cdr_test.go:329` green. That rationale is sound; we accept the temporary duplication.

### F-A2 LOW — `TopPoolUsage` ORDER BY on a computed expression + no secondary tiebreaker
- Evidence: `internal/store/ippool.go:129-132` `ORDER BY pct DESC NULLS LAST LIMIT 1`. When two pools tie on utilization, ordering is non-deterministic — the "Top pool" label can flip between refreshes for equal-utilization tenants.
- Severity: LOW. Cosmetic only; at 30s cache TTL the flip is barely visible.
- Classification: DEFERRED — add tiebreaker (e.g. `ORDER BY pct DESC NULLS LAST, name ASC` or `created_at ASC`). Tracked as D-115.

### F-A3 PASS — DTO additive + `omitempty` on `top_ip_pool`
- `trafficHeatmapCell.RawBytes int64` is a required field (no `omitempty`) — correct: FE tolerates 0 via `?? 0`.
- `TopIPPool *topIPPoolDTO json:"top_ip_pool,omitempty"` — pointer + omitempty → field omitted from JSON when nil. FE types `top_ip_pool?: TopIPPool | null` handle both `undefined` and `null`. COMPLIANT.

### F-A4 PASS — Goroutine concurrency correct
- `ippool` goroutine (`:389-413`): both `TenantPoolUsage` and `TopPoolUsage` called before `mu.Lock()`; mutation of `resp.IPPoolUsagePct` and `resp.TopIPPool` under lock. No race.
- `heatmap` goroutine (`:440-467`): single call to `GetTrafficHeatmap7x24WithRaw`, build slice, then `mu.Lock()` + assign. No race.
- `TopPoolUsage` error → `logger.Warn` + continue (nil assign skipped). Correct non-fatal behaviour per plan Task 3.

### F-A5 PASS — Redis cache additive-field behavior
- `dashboard:<tenant>` cache key unchanged (per plan Risk #1 "accept 30s window"). Stale cached payloads during rolling deploy lack `raw_bytes`/`top_ip_pool`; FE defaults (`?? 0`, `?? undefined`) tolerate both. Acceptable transient state.

### F-A6 PASS — DOW / TZ semantics preserved
- New query (`cdr.go:972-982`) uses identical `EXTRACT(ISODOW FROM (bucket AT TIME ZONE 'Europe/Istanbul'))::int - 1` formula as legacy `GetTrafficHeatmap7x24`. FE `DAYS = ['Mon','Tue',...,'Sun']` indexed 0..6 matches. No drift.

### F-A7 PASS — Max-normalization semantics
- Both methods compute `max(total)` across all cells, then `normalized = total / maxVal` when `maxVal > 0`. Identical semantics. Value range `[0,1]` preserved.

## Verification Commands Run
| Command | Result |
|---------|--------|
| `go build ./...` | PASS |
| `go vet ./internal/store/... ./internal/api/dashboard/...` | PASS (0 issues) |
| `go test ./internal/store/... ./internal/api/dashboard/...` | PASS (457 tests) |
| `grep -rn 'GetTrafficHeatmap7x24' internal/ web/src/` | Confirms only `cdr.go` (impl+wrapper), `cdr_test.go` (uses legacy), `dashboard/handler.go` (uses new) — no other callers broken. |
| `grep -rn 'TopPoolUsage' internal/` | Only `ippool.go` (impl) + `dashboard/handler.go` (caller). |

## Summary
Analysis PASS with 2 LOW deferrals (code-duplication + tiebreaker ordering). No BLOCKER/HIGH. Additive DTO + pointer/omitempty correct. TZ and normalization semantics match legacy method. Concurrency correct.

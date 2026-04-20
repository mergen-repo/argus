# FIX-236: 10M SIM Scale Readiness — Filter-based Bulk, Async Batch, Streaming Export, Virtual Scrolling

## Problem Statement
Argus positions itself as "10M+ SIM management" (CLAUDE.md). But UI patterns assume per-item clicks:
- Row-by-row Disconnect (Sessions), Enable/Disable/Switch (eSIM), Per-policy actions
- Checkbox-based bulk — at 10K rows impossible to scroll-and-check
- No async batch job status for user-triggered bulk ops
- Export = fetch full list in one response (timeouts at scale)
- Client-side filter/search (F-76 IP Pool pattern, F-306 confirmed)
- 50K+ row rendering crashes browsers (no virtual scrolling)

At realistic fleet sizes (1M+), multi-tenant SaaS deployment, current UX would be unusable.

## User Story
As a platform operator with 10M+ SIMs, I need filter-based bulk operations (select all matching filter, not rows), async job tracking for long operations, streaming exports, and virtualized tables so the UI remains performant and usable at enterprise scale.

## Architecture Reference
- Cross-cutting concern — touches most list pages
- Patterns: ListQuery, BulkJob, VirtualTable components
- Backend: streaming endpoints (SSE/chunked transfer)

## Findings Addressed
- F-183 (10M scale audit — all sub-items)
- F-155, F-196, F-206, F-219 (row actions inert — systemic)
- F-77 (client-side IP Pool search)
- F-178 (bulk eSIM)
- Reinforces: FIX-201 (bulk contract), FIX-223 (IP Pool server search)

## Acceptance Criteria
- [ ] **AC-1:** **Filter-based bulk selection pattern** introduced — `<BulkActionBar>` component gains "Select all matching filter" action (not just checked rows). Request payload sends `{filter: {...}, action: ...}` — backend applies filter server-side to determine targets.
- [ ] **AC-2:** All bulk-capable pages adopt BulkActionBar pattern:
  - SIMs: state change, policy assign, operator switch (extends FIX-201)
  - eSIM: switch, disable, delete (FIX-235)
  - Sessions: bulk disconnect (by filter)
  - Policies: bulk archive (rare but needed)
- [ ] **AC-3:** **Async batch job standardization** — every bulk op creates `jobs` row, UI shows toast with link to Job detail SlidePanel. Progress updates via WS (`job.progress` event).
- [ ] **AC-4:** **Streaming export** — CSV/JSON export endpoints use chunked transfer:
  - `GET /api/v1/sims/export.csv?filter=...` returns `Transfer-Encoding: chunked`, streams rows as generated
  - Large exports proceed without timeout; browser receives incrementally
  - Reports (FIX-248) use job-based path for very-large — streaming for interactive
- [ ] **AC-5:** **Virtual scrolling** — rendering >500 visible rows uses `@tanstack/react-virtual`:
  - SIMs list, Sessions list, Audit log, Jobs list, Alerts, Violations
  - Row height fixed or estimated (dynamic ok)
  - Keyboard navigation (Home/End/PgUp/PgDn) works over 50K rows
- [ ] **AC-6:** **Server-side search everywhere** — F-77/F-306 pattern. No list page filter is client-side if row count > 200 default.
- [ ] **AC-7:** **Rate limit framework** per bulk op type — OTA push, RADIUS CoA, SMS gateway each have configurable ops/sec limits. Backpressure via Redis token bucket.
- [ ] **AC-8:** **Async result display** — bulk jobs show per-item results in detail SlidePanel: N succeeded, M failed (with errors table), downloadable CSV of failed items for retry.
- [ ] **AC-9:** **Partition strategy review** — `sims` table: 10M rows. Assess if LIST partitioning by `tenant_id` beneficial. Document decision in `docs/architecture/DB.md` — today's sims is LIST partitioned by operator_id; revisit for scale.
- [ ] **AC-10:** **Benchmark suite** — seed 10M SIMs, measure:
  - List page TTFB + first-paint
  - Filter change response time
  - Bulk op enqueue latency
  - Export 100K rows timing
  Target: p95 < 500ms for list page; bulk enqueue < 2s; export streaming start < 1s.
- [ ] **AC-11:** **Audit table** in `docs/architecture/` — every row-actionable UI widget documented with its bulk counterpart (or explicit "no bulk, by design").

## Files to Touch
- **Backend:**
  - `internal/api/` — every list endpoint — filter param standardization
  - `internal/api/export/` (NEW or extend) — streaming CSV helper
  - `internal/bulk/` (NEW) — filter-to-query translator shared across bulk ops
  - `internal/ratelimit/` — token bucket framework
- **Frontend:**
  - `web/src/components/bulk-action-bar.tsx` (NEW or enhance)
  - `web/src/components/virtual-table.tsx` (NEW wrapper)
  - `web/src/hooks/use-bulk-*.ts`
- **Tests:** new `loadtest/` directory with k6 or similar scripts
- **Docs:** `docs/architecture/SCALE.md` (NEW)

## Risks & Regression
- **Risk 1 — Breaking changes to bulk contracts:** Each FIX story (FIX-201, FIX-235) adopts pattern separately. This story is the scaffolding/convention.
- **Risk 2 — Virtual scroll visual glitches:** Row height variance breaks layout. Mitigation: fixed rows or measured estimator with cache.
- **Risk 3 — Streaming error handling:** Mid-stream failure leaves user with partial data. Trailer/header signals completion; client verifies.
- **Risk 4 — Filter-based bulk selects more than user expects:** UI shows "You are about to modify 24,312 SIMs. Type 'CONFIRM' to proceed." for >1000 affected — double-confirm pattern.
- **Risk 5 — Rate limit hurts legitimate burst:** AC-7 configurable; defaults conservative; observability on rejected requests.

## Test Plan
- Unit: filter-to-query translator for 5 entity types
- Integration: submit filter-bulk on 10K-row test dataset → job completes, audit matches
- Load: 10M SIM seed; list page p95 < 500ms; export 100K < 10s
- Browser: virtual scroll 50K rows, search works, no freeze

## Plan Reference
Priority: P1 · Effort: XL · Wave: 9 · Depends: FIX-201 (bulk contract foundation), FIX-223 (IP Pool server search — early example)

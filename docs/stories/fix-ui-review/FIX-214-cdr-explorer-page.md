# FIX-214: CDR Explorer Page (Filter, Search, Session Timeline, Export)

## Problem Statement
CDR (Charging Data Record) data is the billing + forensic backbone — Argus stores millions of rows in `cdrs` hypertable. No user-facing UI to query, inspect, or export. Only dashboard aggregates + CSV job export (broken per FIX-248 storage gap).

## User Story
As an analyst, I want a CDR Explorer page where I can filter by SIM/APN/operator/date range, drill into a session's CDR timeline (start → interim updates → stop), and export filtered results, so I can investigate billing disputes and usage patterns.

## Architecture Reference
- Backend: `internal/api/cdr/handler.go` + new search/export endpoints
- Store: `cdrs` TimescaleDB hypertable + rollup views (`cdrs_daily_dimensions`)
- FE: new page `/cdrs` + detail drawer

## Findings Addressed
F-62

## Acceptance Criteria
- [ ] **AC-1:** New page `/cdrs` with table: ICCID, IMSI, MSISDN, Operator, APN, Record Type (start/interim/stop), Bytes In/Out, Session ID, Timestamp.
- [ ] **AC-2:** Filter bar: SIM (ICCID/IMSI search), operator multi-select, APN multi-select, record type, date range (required — default last 24h to bound query).
- [ ] **AC-3:** Server-side pagination (cursor). Default page 50, max 100.
- [ ] **AC-4:** Row click → drawer with "Session Timeline": all CDRs for this session_id chronologically, bytes delta per interim update, session metadata.
- [ ] **AC-5:** Export CSV — button triggers FIX-248 report generation job (`report_type=cdr_export`, filters inherited). Not inline download for large sets.
- [ ] **AC-6:** Aggregate stats card: total CDRs in filter window, total bytes in/out, unique SIMs, unique sessions.
- [ ] **AC-7:** Performance — query uses hypertable chunk pruning (time-bound required); p95 < 2s for 7-day range, 1M rows.
- [ ] **AC-8:** Link from Session Detail page → CDR Explorer filtered to that session.
- [ ] **AC-9:** Sidebar entry: MANAGEMENT → "CDRs" (between Sessions and Policies).

## Files to Touch
- `internal/api/cdr/handler.go` — List + Session timeline endpoints
- `internal/store/cdr.go` — query builder with filter support
- `web/src/pages/cdrs/index.tsx` (NEW)
- `web/src/pages/cdrs/session-timeline.tsx` (drawer)
- `web/src/hooks/use-cdrs.ts` (NEW)
- `web/src/components/layout/sidebar.tsx` — add entry
- `web/src/router.tsx` — route

## Risks & Regression
- **Risk 1 — Unbounded query freezes DB:** AC-2 enforces date range as required filter. Max range 30d (admin override).
- **Risk 2 — Large export:** AC-5 delegates to report job (FIX-248) — doesn't block HTTP.

## Test Plan
- Integration: 1M CDR fixture, query 7d range returns < 2s
- Browser: filter by SIM → timeline drawer → export CSV downloads successfully

## Plan Reference
Priority: P1 · Effort: L · Wave: 4

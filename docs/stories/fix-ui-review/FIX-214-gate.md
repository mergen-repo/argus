# Gate Report: FIX-214 — CDR Explorer Page

## Summary
- **Requirements Tracing**: AC-1..AC-9 implemented; AC-1 MSISDN column added (was missing in scout-as-shipped); all 9 ACs verified
- **Gap Analysis**: 9/9 acceptance criteria passed after remediation
- **Compliance**: COMPLIANT (envelope, tenant scoping, cursor pagination, PAT-012 aggregates, Turkish chrome, token discipline)
- **Tests**: full suite 3509/3509 PASS; targeted packages 597/597 PASS
- **Performance**: inline ExportCSV switched to `StreamForExportFiltered` + unconditional 30d cap; stats facade is now unconditional (no direct-SQL drift path)
- **Build**: Go build PASS, Go vet PASS, FE tsc PASS, FE `npm run build` PASS
- **Screen Mockup Compliance**: page + drawer match plan D1/D14/D15 layout including 4-stat strip, record-type chip row, metadata header, cumulative-bytes chart, delta+cumulative Table
- **UI Quality**: Turkish chrome COMPLETE, hex-literal violations CLEARED, raw-element greps CLEAN, EntityLink applied to SIM/Operator/APN, LIVE pip added
- **Token Enforcement**: 2 hex violations → 0; chart axes use `var(--text-tertiary)` + tick fill tokens
- **Turkish Text**: all page/drawer chrome surfaces rewritten; technical identifiers (ICCID/IMSI/MSISDN/UUID/record_type enum) intentionally kept English
- **Overall**: PASS

## Team Composition
- Analysis Scout: 29 findings (F-A)
- Test/Build Scout: 2 findings (F-B)
- UI Scout: 14 findings (F-U)
- De-duplicated: 45 → 42 unique (F-B1 = F-U1, F-U2 = F-U11 subset, F-A1 verified against FE/BE)

## Fixes Applied

| # | Category | File | Change | Verified |
|---|----------|------|--------|----------|
| 1 | AC-1 BLOCKER | `internal/store/sim.go` | `SIMSummary.MSISDN *string` + SELECT widened | go build + 3509/3509 tests |
| 2 | AC-1 BLOCKER | `internal/api/sim/list_by_ids.go` | DTO adds `msisdn` (omitempty) | tsc + build |
| 3 | AC-1 BLOCKER | `web/src/hooks/use-cdrs.ts` | `SimByIDDTO.msisdn?: string`; removed `min_cost` from `CDRFilters` + `toQueryParams` | tsc |
| 4 | AC-1 BLOCKER | `web/src/pages/cdrs/index.tsx` | Table now renders ICCID / IMSI / MSISDN columns with `simInfo.msisdn` fallback `—` | build |
| 5 | F-A10 HIGH | `internal/api/cdr/handler.go` | Whitelist validation for `record_type` + `rat_type` in `parseFilters` (400 on invalid) | go test |
| 6 | F-A10 HIGH | `internal/api/cdr/export.go` | Same whitelist applied to inline export; all query params now validated (not silently dropped) | go test |
| 7 | F-A4 HIGH | `internal/api/cdr/timeline.go` | Removed direct-SQL Stats fallback; `aggSvc` now required (main.go:625 always wires it) | go build + test |
| 8 | F-A2 / F-A18 HIGH | `internal/api/cdr/export.go` | Unconditional 30d cap + switched loop to `StreamForExportFiltered`; UUID/date parse errors return 400/422 instead of silent drop | go build + test |
| 9 | F-U1 / F-B1 CRITICAL | `web/src/components/cdrs/session-timeline-drawer.tsx` | Recharts XAxis/YAxis hex `#6b7280` → `var(--text-tertiary)` + `tick.fill` token | hex grep 0 |
| 10 | F-U2 CRITICAL | `web/src/pages/cdrs/index.tsx` + drawer | Turkish chrome sweep: title / description / filter labels / button labels / table headers / drawer title / empty state / row-list headers / footer CTA | Turkish grep CLEAN |
| 11 | F-U3 HIGH | `web/src/pages/cdrs/index.tsx` | EmptyState title `Bu filtre için CDR bulunamadı.` + embedded `Filtreleri Temizle` CTA via `onCta` | visual |
| 12 | F-U4 HIGH | `web/src/pages/cdrs/index.tsx` | Page title `text-[18px]` → `text-[15px] font-semibold` (sessions/sims precedent) | visual |
| 13 | F-U5 HIGH | `web/src/pages/cdrs/index.tsx` | 6 stat cards → 4 (consolidated Bytes In/Out → single `Toplam Bayt ↓X ↑Y`); dropped `Toplam Maliyet` (not in plan scope) | visual |
| 14 | F-U6 HIGH | `web/src/components/cdrs/session-timeline-drawer.tsx` | Metadata header card: EntityLink rows for SIM/Operatör/APN + Süre + Başlangıç + Son (derived from first/last items) | visual |
| 15 | F-U7 HIGH | `web/src/components/cdrs/session-timeline-drawer.tsx` | Chart rewired to single `cumulative` series via `buildTimelineRows` reduce + MAX-guard for counter resets + `formatBytes` tick formatter | visual |
| 16 | F-U8 HIGH | `web/src/lib/cdr.ts` (NEW) | `recordTypeBadgeClass(rt)` helper; start=accent, interim/update=info, stop=success, auth=warning, auth_fail/reject=danger. Applied in page + drawer | visual |
| 17 | F-U9 HIGH | `web/src/pages/cdrs/index.tsx` | Removed `Min. Maliyet ($)` filter field + localStorage surface + URL param round-trip | grep |
| 18 | F-U10 HIGH | `web/src/pages/cdrs/index.tsx` | `Record Type` Select → horizontal chip ToggleGroup (Tümü + 6 record types); single-select semantics preserved | visual |
| 19 | F-U11 HIGH | `web/src/components/cdrs/session-timeline-drawer.tsx` | `View Session Detail` → `Oturum detayına git` (covered by F-U2 chrome sweep) | Turkish grep |
| 20 | F-U12 HIGH | `web/src/components/cdrs/session-timeline-drawer.tsx` | Row list flex → `Table` with 7 columns ZAMAN / TİP / ↓ BYTES / Δ↓ / ↑ BYTES / Δ↑ / KÜMÜLATİF using shared `buildTimelineRows` helper (math shared with F-U7 chart) | visual |
| 21 | F-U13 HIGH | `web/src/pages/cdrs/index.tsx` | LIVE pip pulsed dot + label in header (matches FIX-213 `DataFreshness` pattern) | visual |
| 22 | F-U15 HIGH | `web/src/pages/cdrs/index.tsx` | Operator + APN cells wrapped with `EntityLink`; SIM cell already was | visual |
| 23 | F-U16 HIGH | `web/src/pages/cdrs/index.tsx` | `Son 30 gün` preset disabled for non-admin roles via `useAuthStore().user.role ∈ {super_admin, tenant_admin}` gate + `title` hint + toast if clicked | visual |
| 24 | F-A3 LOW | `web/src/pages/sessions/detail.tsx` | Added FIX-248 stub comment above the Explore CDRs button; button label `Explore CDRs` → `CDR Kayıtları` | build |
| 25 | F-A13 LOW | `web/src/pages/cdrs/index.tsx` | `initialFilters()` now clears `session_id` + `sim_id` when stored range is stale (>1h) | build |
| 26 | DEFER D-082 | `docs/ROUTEMAP.md` | Added F-A22 as D-082 Tech Debt row targeting FIX-248 | file read |

## Escalated Issues
None. All F-U and CRITICAL F-A findings were FIXABLE.

## Deferred Items (tracked in ROUTEMAP → Tech Debt)

| # | Finding | Target Story | Written to ROUTEMAP |
|---|---------|-------------|---------------------|
| D-082 | F-A22 — `cdr_export` job buffers full CSV in `bytes.Buffer` + base64; refactor to chunked writes (streaming) when reports subsystem lands | FIX-248 | YES |

## Documented (no code change)
- **F-A20** — `BySession` 404 on zero rows conflates cross-tenant vs empty-session. Decision: keep current 404 semantics (simpler; avoids extra existence-check query). Metadata card in drawer (F-U6) provides enough UX context. Documented here, no action.
- **F-A28** — `CountForExport` filter parity with new list filters: inline export no longer uses it (now streams through `StreamForExportFiltered`). Progress counter concern applies only to the job-based CSV path; covered under D-082 streaming refactor.

## Token & Component Enforcement
| Check | Before | After | Status |
|-------|--------|-------|--------|
| Hardcoded hex colors in `pages/cdrs/` + `components/cdrs/` | 2 | 0 | FIXED |
| Raw HTML elements (`<button>`/`<input>`/`<table>` etc.) | 0 | 0 | CLEAN |
| English chrome leakage (`Export Job`, `Session Timeline`, `View Session Detail`, `Rows`, `Session not found`) | 5+ | 0 | FIXED |
| Missing EntityLink on Operator/APN | 2 | 0 | FIXED |
| Missing MSISDN column (AC-1) | 1 | 0 | FIXED |

## Verification
- **Go build**: PASS
- **Go vet**: PASS
- **Go test** (targeted): `./internal/api/cdr/...`, `./internal/api/sim/...`, `./internal/store/...`, `./internal/analytics/aggregates/...` → 597/597
- **Go test** (full): 3509/3509 passed across 109 packages
- **FE tsc --noEmit**: PASS
- **FE `npm run build`**: PASS (2.62s)
- **Grep gates**:
  - `^\s*<(button|input|select|textarea|dialog|table)[^A-Za-z]` in `pages/cdrs/` + `components/cdrs/` → 0 matches
  - `#[0-9a-fA-F]{3,8}` in `pages/cdrs/` + `components/cdrs/` → 0 matches
  - Turkish chrome English-leak words (word-boundary regex over labels) → 0 matches
- **Fix iterations**: 2
  - Iter 1 post-check: tsc caught Recharts `Tooltip.formatter` type mismatch → widened to accept `ValueType | undefined`
  - Iter 2 post-check: advisor flagged a blind spot in the scout grep — `^\s*<(button|…)[^A-Za-z]` cannot match `<button` + newline. Manual `grep -rEn '<button'` found 2 raw `<button>` chip elements in the record-type chip row. Swapped to shadcn `<Button size="sm" variant="outline">` with conditional className. Final grep `<button|<input[[:space:]>]|<select[[:space:]>]|<textarea[[:space:]>]` → 0 matches.

## Passed Items (evidence)
- AC-1 table schema: ICCID / IMSI / MSISDN / Operator / APN / Record Type / Bytes / Session / Timestamp — all present
- AC-2 filter bar: SIM search, Operatör, APN, RAT, Oturum ID, Başlangıç, Bitiş — all present; Record Type as chip row
- AC-3 cursor pagination: unchanged, plan-compliant (`use-cdrs.ts` infinite query)
- AC-4 session timeline drawer: metadata header + cumulative chart + 7-col delta table + footer CTA
- AC-5 export CSV: job-based path ("Rapor İşi") + inline ("CSV İndir") both hardened
- AC-6 aggregate stats: 4 cards via `useCDRStats` (PAT-012 facade)
- AC-7 performance: 30d cap enforced unconditionally across list + inline + job paths
- AC-8 deep-link from Session Detail: preserved + stub-commented for FIX-248 continuation
- AC-9 sidebar entry: unchanged (already in place)

## Change Manifest (files touched)

Backend (5 files):
- `internal/store/sim.go` — SIMSummary.MSISDN field + SELECT msisdn + nullable scan
- `internal/api/sim/list_by_ids.go` — DTO msisdn (omitempty)
- `internal/api/cdr/handler.go` — record_type + rat_type whitelist validation
- `internal/api/cdr/timeline.go` — removed direct-SQL fallback; aggSvc required
- `internal/api/cdr/export.go` — unconditional 30d cap + StreamForExportFiltered switch + whitelist + strict UUID/date validation

Frontend (5 files):
- `web/src/hooks/use-cdrs.ts` — dropped `min_cost`; `SimByIDDTO.msisdn?`
- `web/src/lib/cdr.ts` (NEW) — `recordTypeBadgeClass` tone-map helper
- `web/src/components/cdrs/session-timeline-drawer.tsx` — complete rewrite: Turkish chrome, metadata header, cumulative chart, delta+cumulative Table, token-only styling
- `web/src/pages/cdrs/index.tsx` — Turkish sweep, 4 stat cards, chip ToggleGroup, MSISDN column, EntityLink on Operator/APN, empty-state CTA, 15px title, LIVE pip, non-admin 30d guard, stale-filter clear
- `web/src/pages/sessions/detail.tsx` — FIX-248 stub comment + CDR Kayıtları button label

Docs:
- `docs/ROUTEMAP.md` — D-082 Tech Debt row added

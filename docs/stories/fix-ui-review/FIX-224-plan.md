# Implementation Plan: FIX-224 — SIM List/Detail Polish

## Goal
Refine the SIM List + Compare + Import surfaces: multi-select state filter, Created column with datetime + relative tooltip, compare cap bumped to 4 with explicit limit UX, client-side CSV preview before commit, and a post-import report (succeeded / failed + per-row errors + CSV export) rendered from the already-existing job `error_report` payload. No backend changes.

## Scope Discipline
- **In scope:**
  - Frontend: `web/src/pages/sims/index.tsx` — STATE_OPTIONS → multi-select checkbox dropdown, Created column datetime + relative tooltip, Import SlidePanel preview + post-import report + job polling + CSV download link.
  - Frontend: `web/src/pages/sims/compare.tsx` — `MAX_SIMS 3 → 4`, 5th-selection disabled with warning copy.
  - Frontend: `web/src/hooks/use-sims.ts::useImportSIMs` — correct response type to match actual backend shape (`{ job_id, tenant_id, status }`).
  - Frontend: `web/src/types/sim.ts` — extend `SIMListFilters.state` to accept `string | string[]` (CSV-joined on the wire).
- **Out of scope (explicit):**
  - **AC-3 Bulk bar sticky** — **SATISFIED-BY-EXISTING (FIX-201)**. `sims/index.tsx:791-795` already renders the bulk bar as `fixed bottom-0 right-0 z-30` with sidebar-aware `left-16`/`left-60` offset. Verified in discovery. AC-3 marked satisfied in the AC matrix, zero code change.
  - Backend — **zero backend work**. `GET /jobs/{id}` already returns `error_report` (JSON of `[]ImportRowError{row, iccid, error}`) and `result` (`ImportResult{total_rows, success_count, failure_count, created_sim_ids}`); `GET /jobs/{id}/errors` already emits CSV. Shipped pre-FIX-224.
  - New relative-time helper — `formatTimestamp` (FIX-220) and `timeAgo` (format.ts:106-114 → English "2d ago") already exist and cover AC-2. No new helper.
  - SIM Detail page — spec title says "List/Detail" but every AC targets List, Compare, or Import. Detail page is untouched.
  - `internal/store/sim.go` / `internal/api/sim/bulk_handler.go` / `validBulkTargetStates` — filter enum expansion is a purely FE display concern; `GET /sims?state=...` already accepts any string (store-side scan filters at call time).
  - CSV parser library (Papa Parse) — not installed; not needed. Native `String.split('\n')` / `split(',')` is sufficient for the 6-column schema.

## Findings Addressed
| Finding | Summary | Addressed by |
|--------:|---------|--------------|
| F-85 | State filter single-select, can't OR across states | AC-1 checkbox dropdown |
| F-86 | Created column shows date only, no time or relative hint | AC-2 datetime + tooltip |
| F-87 | Bulk bar scrolls off-screen during long selection | AC-3 already fixed by FIX-201 (verified) |
| F-91 | Compare allows unlimited SIMs (fits poorly at 5+) | AC-4 cap to 4 + explicit limit UX |
| F-92 | Import errors only visible after commit, no preview | AC-5 client-side preview + AC-6 post-import report |

## Key Decisions

### D1 — Spec `pending` mapped to code `ordered`
Spec AC-1 lists state values `active, suspended, terminated, stolen_lost, pending`. Discovery of `internal/store/sim.go:87-91` confirmed the real enum is `ordered → active → {suspended, stolen_lost, terminated} → purged`. There is no `pending` state. `ordered` is the pre-activation state (SIM provisioned but never activated). The spec's "pending" is the spec author's informal label for `ordered`. Plan uses the 5 real user-facing values: **`ordered, active, suspended, terminated, stolen_lost`**. `purged` is excluded (terminal admin state, not user-relevant). No new enum value introduced, no DB migration. Logged as DEV-### entry.

### D2 — AC-4 "5th replaces oldest OR warns" → **warns + disables**
Spec allows either; silent replacement is surprising (user loses a compare slot without feedback). Chosen: when `selectedIds.length === 4`, the search box's "Add" button disables and a subtle warning chip reads "Compare limit reached (4/4)". User must explicitly remove a slot to add a fifth. Mirrors the warning-not-replace pattern already used by Export queue limits. Logged as DEV-### entry.

### D3 — AC-1 Multi-select URL contract → CSV-joined
URL param `state` moves from a single value to a comma-joined list: `?state=active,suspended`. Empty / omitted means "all states". This keeps `GET /sims?state=<csv>` compatible with the existing backend (which already accepts `state=active`), because a CSV of length 1 is indistinguishable from a single value. For CSV of length ≥2, the backend today matches the **first** token only (store-side `state = $N` predicate). **Acceptable for this FIX** — fleet dashboards and list views that depend on multi-state filtering today use segments, not the URL param. The FE-side state filter predicate is re-run client-side as a secondary filter against the page buffer when a multi-state CSV is requested. Tracked as Tech Debt D-### (server-side `state IN (...)` predicate for full correctness at scale). Note: this matches the pattern DEV-301 used for APN Top-Operator computation (client-side aggregate, backend aggregate tech-debt).

### D4 — AC-5 Preview is client-only (no backend call)
Client parses first 10 rows with native `String.split`, validates column headers (iccid, imsi, msisdn, operator_code, apn_name required; ip_address optional), shows preview table + "N rows detected, estimated ~Xs processing time". Large files: parse only first 100 lines for the preview; keep the raw file for upload. No extra API round trip. Estimated-time rule: `ceil(rows / 200)` seconds (matches observed `BulkImportProcessor` throughput at 200/sec on dev infra — conservative).

### D5 — AC-6 Import report surfaces post-completion via `useJobPolling`
Existing `useJobPolling` hook (already imported `sims/index.tsx:151`) polls `GET /jobs/{id}` every 2s. After `Import SlidePanel` receives `{ job_id, status: "queued" }`, the panel subscribes to the job's lifecycle and, on `status === "completed"`, renders:
- Success count / failure count from `result.success_count` + `result.failure_count`.
- Scrollable list of first 20 row errors from `error_report` (row #, ICCID, reason).
- "Download all errors (CSV)" button linking `GET /api/v1/jobs/{job_id}/errors`.
This fixes a pre-existing FE bug (DEV-###): `useImportSIMs` hook currently types the response as `{ job_id, rows_parsed, errors[] }` but the actual sync Import response is `{ job_id, tenant_id, status }`. The SlidePanel's "Rows parsed: N" / "N validation errors" block is rendering `undefined` / `[]` today for every import — never shipped. Plan corrects the type + replaces the success block with the polling-driven report block.

## Architecture Context

### Components Involved
- **Frontend only** (no backend changes)
  - `web/src/pages/sims/index.tsx` (1100+ LOC, touched in 4 regions: STATE_OPTIONS block L79-86, state filter dropdown + activeFilters memo L185-230, Created table cell L627+L754, Import SlidePanel L968-1100).
  - `web/src/pages/sims/compare.tsx` (MAX_SIMS constant + No-SIMs placeholder copy + limit UX).
  - `web/src/hooks/use-sims.ts::useImportSIMs` (response type correction).
  - `web/src/types/sim.ts::SIMListFilters.state` (string → string | undefined, URL stays CSV).

### Data Flow — Import with preview + report
```
User opens Import SlidePanel
  → picks file or pastes → FE parses first ≤100 rows → renders preview table + validation summary
  → user clicks "Import <N> SIMs"
    → POST /api/v1/sims/bulk/import (multipart) → backend sync-validates headers + row count
    → returns { job_id, tenant_id, status: "queued" }
  → FE state: importing → useJobPolling(job_id) every 2s
    → GET /api/v1/jobs/{id} → poll until status === "completed" | "failed"
  → FE renders report: success/fail counts + first 20 row errors + [Download CSV] → GET /api/v1/jobs/{id}/errors
```

### URL / API Contract
- `GET /api/v1/sims?state=<csv>&...` — existing endpoint, unchanged. FE sends `state=active,suspended`; backend matches first token, FE re-filters client-side for the rest (D3).
- `POST /api/v1/sims/bulk/import` (multipart/form-data: `file`, `reserve_static_ip?`) — unchanged. Returns `202 Accepted` with envelope `{ status:"success", data:{ job_id, tenant_id, status:"queued" } }`.
- `GET /api/v1/jobs/{id}` — unchanged. Returns `{ ..., status, result: { total_rows, success_count, failure_count, created_sim_ids[] } | null, error_report: [{row, iccid, error}] | null }`.
- `GET /api/v1/jobs/{id}/errors` — unchanged. Returns CSV (`Content-Disposition: attachment; filename=error_report.csv`).

### SIM State Token Map (canonical — from `internal/store/sim.go:87-91`)
| Token | Display label | User-visible in filter? |
|-------|--------------|-------------------------|
| `ordered` | Ordered | YES |
| `active` | Active | YES |
| `suspended` | Suspended | YES |
| `terminated` | Terminated | YES |
| `stolen_lost` | Lost/Stolen | YES |
| `purged` | Purged | NO (admin-only terminal state) |

### Import DTO Token Map (canonical — from `internal/job/import.go:32-49` + `internal/api/sim/bulk_handler.go:98-102`)
```go
// Sync response (POST /sims/bulk/import):
bulkImportResponse { JobID string, TenantID string, Status string }
// Async result (embedded in GET /jobs/{id}):
ImportResult    { TotalRows int, SuccessCount int, FailureCount int, CreatedSIMIDs []string }
ImportRowError  { Row int, ICCID string, ErrorMessage string } // JSON key: "error"
```

### Compare Selection Token Map (`web/src/pages/sims/compare.tsx:35`)
| Constant | Current | After FIX-224 |
|----------|---------|---------------|
| `MAX_SIMS` | `3` | `4` |
| Placeholder copy | `up to 3 SIM cards` (L436) | `up to 4 SIM cards` |
| Add-slot guard | `selectedIds.length < MAX_SIMS` (already correct) | unchanged — cap enforcement via MAX_SIMS bump |
| 5th-click behavior | silently ignored (no feedback) | disabled Add button + warning text: "Compare limit reached (4/4) — remove a slot to add another" |

### Design Token Map (from `docs/FRONTEND.md`)
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Filter pill (inactive) | `border-border bg-bg-elevated text-text-secondary` | hex colors |
| Filter pill (active) | `border-accent/30 bg-accent-dim text-accent` | `bg-blue-*` |
| Checkbox indicator | `accent-accent` (on `<Input type="checkbox">`) | raw `accent-blue-500` |
| Preview table header | `bg-bg-elevated text-text-tertiary text-[10px] uppercase tracking-wider` | hardcoded greys |
| Error row highlight | `border-l-2 border-danger/40 bg-danger-dim/30` | `bg-red-*` |
| Success summary | `border-success/30 bg-success-dim` (already used L972) | hex |
| Warning chip | `border-warning/30 bg-warning-dim text-warning` | `bg-yellow-*` |
| Tooltip | reuse `<InfoTooltip>` (FIX-222 `web/src/components/ui/info-tooltip.tsx`) + `<Tooltip>` for Created cell | raw `title=` attribute |
| Dropdown | `<DropdownMenu>`/`<DropdownMenuContent>`/`<DropdownMenuItem>` (existing L503-521) | custom floats |

### Existing Components to REUSE
| Component | Path | Use For |
|-----------|------|---------|
| `DropdownMenu`, `DropdownMenuCheckboxItem` | `web/src/components/ui/dropdown-menu.tsx` | AC-1 multi-select (native radix checkbox item) |
| `SlidePanel` + `SlidePanelFooter` | `web/src/components/ui/slide-panel.tsx` (FIX-216) | Import panel (already in use) |
| `Tooltip`, `TooltipTrigger`, `TooltipContent` | `web/src/components/ui/tooltip.tsx` | AC-2 hover tooltip on Created cell |
| `formatTimestamp` | `web/src/lib/format.ts:95` | AC-2 "4/19/2026 15:59" (pass `'auto'` or a period other than `1h`/`24h`) |
| `timeAgo` | `web/src/lib/format.ts:106` | AC-2 "2d ago" tooltip content |
| `useJobPolling` | `web/src/hooks/use-jobs.ts` (imported L151) | AC-6 job status polling |
| `Table`, `TableHeader`, `TableRow`, `TableCell` | `web/src/components/ui/table.tsx` | AC-5 preview table |
| `Input type="checkbox"` | already used L128, L1013 | AC-1 checkbox items |

## Acceptance Criteria Mapping
| AC | Summary | Implemented in Task | Verified by |
|---:|---------|---------------------|-------------|
| AC-1 | State filter multi-select checkbox dropdown (5 values) | T2 | T7 (manual browser + snapshot) |
| AC-2 | Created: "4/19/2026 15:59" visible, "2d ago" hover tooltip | T3 | T7 |
| AC-3 | Bulk action bar sticky | **SATISFIED-BY-EXISTING** (FIX-201) | T1 (audit) |
| AC-4 | Compare capped at 4; 5th triggers warning (not replacement) | T4 | T7 |
| AC-5 | Import pre-upload: column check + format validate + row count + estimated time + preview | T5 | T7 |
| AC-6 | Post-process: N succeeded / M failed + reasons + export CSV | T6 | T7 |

## Tasks

### Task 1 — Audit: AC-3 satisfied-by-existing
- **Files:** none (audit task) — append audit note to `decisions.md` as DEV-###.
- **Depends on:** —
- **Complexity:** low
- **What:** Confirm bulk bar at `web/src/pages/sims/index.tsx:789-876` is `fixed bottom-0`, sidebar-aware, z-30. Write DEV-### entry stating AC-3 is satisfied by FIX-201 delivery. No code change.
- **Verify:** `grep -n "fixed bottom-0" web/src/pages/sims/index.tsx` returns line 793.
- **Context refs:** Scope Discipline → AC-3 line; Findings Addressed → F-87.

### Task 2 — AC-1 State filter multi-select
- **Files:** Modify `web/src/pages/sims/index.tsx` (STATE_OPTIONS block L79-86, state filter DropdownMenu region L503-521-equivalent, activeFilters memo L185-230, setFilters callback L114-125, filters memo L103-113), modify `web/src/types/sim.ts::SIMListFilters` if `state` field type needs widening.
- **Depends on:** T1 (audit only, no blocker).
- **Complexity:** medium
- **Pattern ref:** Existing APN DropdownMenu at `web/src/pages/sims/index.tsx:520-545` (single-select). Follow that structure but swap `DropdownMenuItem` → `DropdownMenuCheckboxItem` (already exported by `@/components/ui/dropdown-menu`).
- **What:**
  - Change `STATE_OPTIONS` to drop the `{value:'', label:'All States'}` sentinel; instead add `ordered` to the list → final 5: `ordered, active, suspended, terminated, stolen_lost`.
  - Filter value: `filters.state` stays `string | undefined` at the type level; internally represents CSV of selected tokens, or `undefined` when nothing selected.
  - Toggle item: when checked, splice token out of CSV; when unchecked, push into CSV. "Clear" resets to `undefined`.
  - Trigger label: `State` when none, `State: <label>` when one, `State: <N> selected` when ≥2.
  - `activeFilters` memo: render one chip per selected token (remove closes just that one token, not the whole filter).
  - URL contract: single-select URLs like `?state=active` remain valid (1-token CSV).
- **Tokens:** Use ONLY classes from Design Token Map. Active pill = `border-accent/30 bg-accent-dim text-accent`.
- **Verify:** Dev-server. Select two states → URL shows `?state=active,suspended`. List re-filters client-side for secondary tokens. Chips render one per state. "Clear all" still works.
- **Context refs:** SIM State Token Map; Design Token Map; Existing Components to REUSE; D1; D3.

### Task 3 — AC-2 Created datetime + relative tooltip
- **Files:** Modify `web/src/pages/sims/index.tsx` (Created cell around L754; also inspect the tooltip import block).
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** FIX-220 `formatTimestamp` usage in `web/src/pages/dashboard/analytics.tsx`. For tooltip pattern: `web/src/components/ui/tooltip.tsx` wrapping a span.
- **What:**
  - Replace `{new Date(sim.created_at).toLocaleDateString()}` with `<Tooltip><TooltipTrigger asChild><span>{formatTimestamp(sim.created_at, 'day')}</span></TooltipTrigger><TooltipContent>{timeAgo(sim.created_at)}</TooltipContent></Tooltip>`.
  - `formatTimestamp(iso, 'day')` (non-`1h`/`24h` branch) returns "Apr 19, 15:59" which satisfies "date + time". Use `'day'` (any string not `1h`/`24h`).
  - The existing second "Created" field at L699 inside the RowQuickPeek descriptor stays `toLocaleDateString` (quick-peek is compact, no tooltip affordance needed).
- **Tokens:** none new — Tooltip is styled by the primitive.
- **Verify:** Hover a SIM row's Created cell — datetime visible in cell, "2d ago" shows in tooltip.
- **Context refs:** Existing Components to REUSE (formatTimestamp + timeAgo + Tooltip).

### Task 4 — AC-4 Compare cap 3 → 4 + limit UX
- **Files:** Modify `web/src/pages/sims/compare.tsx` (MAX_SIMS L35, placeholder copy L436, Add-SIM button region around L546-556, add warning text block).
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Existing `selectedIds.length < MAX_SIMS` guard at L546. Copy the structure.
- **What:**
  - `const MAX_SIMS = 4` (was 3).
  - Placeholder copy L436: `"Search and add up to 4 SIM cards above to compare their properties side by side."`.
  - When `selectedIds.length >= MAX_SIMS`: the Add-slot button at L546 already hides; additionally render a warning line inside the grid (below the last slot) reading `"Compare limit reached (4/4). Remove a slot to add another SIM."` — styled `text-xs text-warning flex items-center gap-1.5 mt-2` with an `AlertCircle` icon from `lucide-react`.
  - SearchBox `handleSelect`: if `selectedIds.length >= MAX_SIMS` and caller passes a new index beyond range, silently no-op (existing behavior L518-529 already checks `selectedIds.includes(sim.id)`; bump the implicit cap check).
- **Tokens:** `text-warning`, no hex.
- **Verify:** Dev-server. Select 4 SIMs → Add button gone, warning visible. Attempt to paste/search a 5th into the existing slot set — fails silently (or slot not added).
- **Context refs:** Compare Selection Token Map; D2.

### Task 5 — AC-5 Import preview (client-side parse)
- **Files:** Modify `web/src/pages/sims/index.tsx` (Import SlidePanel body L968-1100 — add a preview section above the submit button when `pasteContent` or `importFile` is populated; file case: read first 100 lines via `File.slice + text()`).
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Existing row-count indicator at L1031. Extend into a full preview block.
- **What:**
  - Add state: `const [preview, setPreview] = useState<{ headers: string[]; rows: string[][]; totalRows: number; missingCols: string[]; estSeconds: number } | null>(null)`.
  - On paste change or file change, debounce 300ms then parse: split `\n`, split `,` (also handle `\t` as existing normalization does), first row = headers, next ≤10 = preview rows, remainder counted for `totalRows`.
  - Validate: required columns `iccid, imsi, msisdn, operator_code, apn_name`; optional `ip_address`. Log `missingCols`.
  - Validate format: ICCID digits length 18-22, IMSI 14-15, MSISDN 10-15 (digits, optional leading `+`). Count rows with format issues. Store as `previewIssues: { rowIndex, column, reason }[]`.
  - Render: a `<Table>` block with header row + up to 10 preview rows; red-left-border on rows with format issues. Beneath, a summary: "N rows detected • estimated ~Xs processing time • M validation warnings". If `missingCols.length`, render a red banner disabling the submit button.
  - Estimated time: `Math.max(1, Math.ceil(totalRows / 200))` seconds.
  - Submit button stays enabled only when `missingCols.length === 0`.
- **Tokens:** `border-l-2 border-danger/40 bg-danger-dim/30` for error rows; `border-warning/30 bg-warning-dim text-warning` for format warnings; `bg-bg-elevated text-text-tertiary text-[10px] uppercase tracking-wider` for preview table header.
- **Verify:** Paste CSV missing `msisdn` column → red banner, submit disabled. Paste valid 20-row CSV → preview shows 10 rows + "20 rows • ~1s".
- **Context refs:** Import DTO Token Map; Design Token Map; D4.

### Task 6 — AC-6 Import post-process report + CSV download
- **Files:** Modify `web/src/hooks/use-sims.ts::useImportSIMs` (correct response type L294), modify `web/src/pages/sims/index.tsx` Import SlidePanel success block L971-988 — replace with polling-driven report.
- **Depends on:** T5.
- **Complexity:** medium
- **Pattern ref:** Existing bulk-job polling in `web/src/pages/sims/index.tsx:151-172` (`useJobPolling` with onComplete/onError). Duplicate the pattern for import.
- **What:**
  - `useImportSIMs` return type: `{ job_id: string; tenant_id: string; status: string }`. Remove `rows_parsed` and `errors` fields — they never existed on this response.
  - New state in page component: `const [importJobId, setImportJobId] = useState<string | null>(null)`.
  - On `importMutation.mutateAsync` success, set `importJobId = result.job_id`. Clear `importResult`.
  - Wire a second `useJobPolling(importJobId, {...})` with `onComplete: (job) => { setImportResult({ totalRows: job.result.total_rows, success: job.result.success_count, failure: job.result.failure_count, errors: job.error_report ?? [] }); setImportJobId(null); refetch() }`. `onError` mirrors but records the failure.
  - Change `importResult` shape in the page (local): `{ totalRows: number; success: number; failure: number; errors: ImportRowError[] }`.
  - Report block: "N succeeded • M failed" header; scrollable `<div className="max-h-48 overflow-y-auto">` with first 20 `error_report[]` entries formatted `Row #{row} — ICCID {iccid}: {error}`. Beneath, `<a href={`/api/v1/jobs/${importJobId}/errors`} download>Download full error report (CSV)</a>` button only when `failure > 0`. Because `useJobPolling` already cleared `importJobId`, keep a separate `reportJobId` state to persist the link.
  - While polling: spinner + "Importing… estimated ~Xs" (reuse estSeconds from Task 5 preview).
- **Tokens:** Reuse `border-success/30 bg-success-dim` for the success summary frame (already L972); `text-danger` for failure count; `text-text-tertiary font-mono text-[11px]` for error rows.
- **Verify:**
  - Dev-server. Import CSV with 2 intentional dup-ICCID rows. Panel transitions queued → polling → done. Summary: "8 succeeded • 2 failed". First 2 errors render. "Download full error report (CSV)" link returns a 2-row CSV via the existing backend endpoint.
  - `tsc`: no errors on `useImportSIMs` hook or page file.
- **Context refs:** Import DTO Token Map; D5; Existing Components to REUSE (`useJobPolling`).

### Task 7 — Dev verification + AC matrix evidence
- **Files:** append evidence to step-log (not a code task).
- **Depends on:** T1..T6.
- **Complexity:** low
- **What:**
  - `cd web && npm run type-check && npm run build` — both must pass.
  - Manual browser verification per the Acceptance Criteria Mapping (7 scenarios).
  - Grep no-hex guard on touched files: `grep -nE '#[0-9a-fA-F]{3,8}' web/src/pages/sims/index.tsx web/src/pages/sims/compare.tsx web/src/hooks/use-sims.ts` → zero hits.
- **Verify:** type-check + build PASS; no raw hex colors introduced.
- **Context refs:** Acceptance Criteria Mapping.

### Complexity budget
- T1: low (audit)
- T2: medium (filter rewrite)
- T3: low (tooltip wrap)
- T4: low (const bump)
- T5: medium (preview parse + validation render)
- T6: medium (polling + hook type fix + report render)
- T7: low (verification)
Total: 7 tasks, 3 medium + 4 low — fits M effort.

### Waves
- **Wave 1 (parallel):** T1, T2, T3, T4 — independent UI edits, no shared state.
- **Wave 2:** T5 — new preview section (independent state).
- **Wave 3:** T6 — polling + hook correction (depends on T5's preview footer layout).
- **Wave 4:** T7 — verification.

## Story-Specific Compliance Rules
- **UI:** Design tokens only (from Design Token Map) — no raw hex, no `bg-blue-*`, no inline `style={{color:…}}`.
- **UI:** Drill-down — the "Download full error report (CSV)" link uses `<a href download>` to the existing `GET /api/v1/jobs/{id}/errors` endpoint (no new backend work).
- **API:** Standard envelope (pre-existing) — all reads go through the established API client interceptors.
- **Modal Pattern (FIX-216):** Import stays in `<SlidePanel>` (rich form, multi-field, ≥3 fields, preview table). No conversion to Dialog. Compare page remains a full-page route.
- **Business:** SIM state values from `internal/store/sim.go:87-91` — ordered/active/suspended/terminated/stolen_lost/purged. Only the first 5 user-visible.
- **FRONTEND.md → Entity-Reference-Pattern (FIX-219):** ICCID in preview table + report rows renders as plain text (not `EntityLink`) — preview rows aren't entities yet (not persisted), and report rows are error contexts where a linked drill-down to `/sims/{id}` is meaningless (SIM creation failed).

## Bug Pattern Warnings
- **Checkbox dropdown focus loss:** radix `DropdownMenuCheckboxItem` closes on select by default. Use `onSelect={(e) => e.preventDefault()}` inside each item so the menu stays open while toggling multiple states.
- **URL-state racing:** `setSearchParams` inside the checkbox toggle must clone via `new URLSearchParams(prev)` (already done in existing `setFilters`). Do not replace wholesale — you'd destroy segment/sort/search params from sibling filters.
- **Polling leak:** `useJobPolling(importJobId, …)` must be cancelled when the SlidePanel closes mid-import. The hook already handles unmount; just ensure `importJobId` persists across panel close only if the user navigated to Jobs. For FIX-224 scope: when SlidePanel closes, keep polling (result still lands in `importResult`), but clear when the component unmounts.

## Tech Debt (from ROUTEMAP)
- No open Tech Debt items currently target FIX-224.
- New items to file (post-FIX-224):
  - **D-### — Server-side `state IN (...)` predicate for SIM list.** Currently multi-select state CSV only honors the first token server-side; FE re-filters client-side for secondary tokens. At ≥1M SIMs the page-buffer approach is wrong (secondary filters drop rows that never came down). Requires `internal/store/sim.go::ListEnriched` to switch `state = $N` → `state = ANY($N::text[])`. 1-file backend change.
  - **D-### — Relative-time in Created column is English (`timeAgo`) while rest of UI is mixed TR/EN.** `formatRelativeTime` (TR) was not chosen because FIX-224's spec shows English "2d ago". Tracked for future i18n pass.

## Mock Retirement
- No mocks for this story (all endpoints already real; backend unchanged).

## Risks & Mitigations
- **R1 — State filter CSV vs backend single-value.** Mitigated by D3 client-side re-filter; tech-debt entry filed. Scale concern flagged explicitly.
- **R2 — Import preview parser disagrees with backend CSV parser.** Go's `encoding/csv` accepts RFC 4180 quoted fields; FE's `split(',')` does not. If a paste-content row contains a quoted field with an embedded comma, the FE preview will mis-parse but the backend will succeed. **Accepted risk** — the schema (ICCID/IMSI/MSISDN/operator_code/apn_name/ip_address) has no free-text fields; commas inside values would already be invalid per format validation. Document in inline code comment where the split happens.
- **R3 — useJobPolling vs SlidePanel lifecycle.** If user closes the panel mid-import, polling continues (hook is at page level) and surfaces as toast on completion (consistent with bulk state-change UX at L151). No leak — hook cleans up on page unmount. No special mitigation.
- **R4 — Pre-existing `useImportSIMs` type lies affecting other callers.** Grep confirmed only `sims/index.tsx` uses this hook. Fix is safe.

## Evidence / Verification Commands
- Build: `cd web && npm run type-check && npm run build`
- No-hex guard: `grep -nE '#[0-9a-fA-F]{3,8}' web/src/pages/sims/index.tsx web/src/pages/sims/compare.tsx web/src/hooks/use-sims.ts`
- State constants alignment: `grep -n 'ordered\|active\|suspended\|terminated\|stolen_lost' internal/store/sim.go | head`

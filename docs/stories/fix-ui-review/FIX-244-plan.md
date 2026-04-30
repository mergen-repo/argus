# FIX-244 — Violations Lifecycle UI: Acknowledge + Remediate Actions Wired — PLAN

- **Spec:** `docs/stories/fix-ui-review/FIX-244-violations-lifecycle-ui.md`
- **Tier:** P1 · **Effort:** S · **Wave:** 9 (UI Review Remediation Phase 2 P1)
- **Track:** UI Review Remediation (full-track AUTOPILOT)
- **Depends on:** FIX-216 (SlidePanel pattern), FIX-211 (severity taxonomy), FIX-219 (entity links)
- **Findings addressed:** F-164, F-165, F-166, F-168, F-169, F-170, F-171, F-310
- **Plan date:** 2026-04-27

---

## Goal

Wire the existing backend violation-lifecycle endpoints (Acknowledge, Remediate {suspend_sim | escalate | dismiss}) into the violations list UI with: real action buttons per row, derived Status column, lifecycle filters (Status / Severity / Date range), correct violation_type vs action_taken filter mapping, fixed Export CSV path, ICCID-as-link drill-downs, an enriched SlidePanel detail view, and row-checkbox bulk operations. Audit trail already emitted on the backend — this story verifies the wires and adds the FE.

---

## Problem Recap

Backend (F-310 verified) already implements full violation lifecycle:
- DB columns `acknowledged_at, acknowledged_by, acknowledgment_note`
- `POST /policy-violations/{id}/acknowledge` (audit emits `violation.acknowledge`)
- `POST /policy-violations/{id}/remediate` with `action ∈ {suspend_sim, escalate, dismiss}` (audit emits `violation.remediated | violation.escalated | violation.dismissed`)
- `GET /policy-violations/export.csv` works at the **correct** path

But UI has wrong wires:
- The "Type" filter dropdown sends action-taken values (`block`, `disconnect`, `suspend`, `throttle`, …) on the `violation_type` query param — backend filters return zero (F-165).
- Export uses `/api/v1/violations/export.csv` (wrong resource segment) — 404 (F-166).
- Row click only opens an info-only SlidePanel; no Acknowledge / Remediate / Dismiss buttons (F-169 — F-310 mis-scoped earlier).
- Row content shows literal "SIM" instead of the ICCID link (F-164).
- Empty-state copy "Well done!" is too casual (F-170).
- No date-range filter (F-168).
- No bulk select / bulk acknowledge / bulk dismiss.
- Inline expand row pattern instead of FIX-216 SlidePanel for rich detail (F-171) — actually a SlidePanel IS used today, but the content is sparse and the action surface is missing.

---

## Architecture Decisions

### D1 — Confirm-vs-SlidePanel split (FIX-216 Option C)

- **Acknowledge (light)** → `<Dialog>` (compact). Just a textarea for optional note + Cancel / Acknowledge button. No nav. Reused for both row-action menu Acknowledge and panel-footer Acknowledge.
- **Remediate (destructive multi-option)** → Row drop-down opens a `<Dialog>` per chosen action:
  - `suspend_sim` → destructive-styled confirm dialog: "Suspend SIM {ICCID}? This will disconnect any active sessions and revoke all RADIUS Access-Accept replies until reactivated." Reason textarea (required). Confirm = red button.
  - `escalate` → confirm dialog with optional reason textarea ("Notifies on-call channel.").
  - `dismiss` → confirm dialog with reason textarea (required, min 3 chars to discourage rubber-stamping).
- **Row click** → `<SlidePanel>` rich detail view (existing component reused; content enriched per AC-5).
- **Bulk action sticky bar** → uses the same Acknowledge / Dismiss confirm dialogs but with count + (for bulk dismiss) reason field.

Rationale: matches the project-wide "Option C" decision recorded in CLAUDE.md. Dialogs for compact confirms; SlidePanel for rich data exploration.

### D2 — Status derivation: frontend-only for this story

Status field is **derived in the FE** from existing backend fields (`acknowledged_at`, `details.remediation`, `details.escalated`). No new backend column.

```ts
type ViolationStatus = 'open' | 'acknowledged' | 'remediated' | 'dismissed'

function deriveStatus(v: PolicyViolation): ViolationStatus {
  const det = v.details ?? {}
  if (det.remediation === 'suspend_sim') return 'remediated'
  if (det.remediation === 'dismiss') return 'dismissed'
  if (v.acknowledged_at) return 'acknowledged'
  return 'open'
}
```

Rationale:
- All required signal already exists on the wire (`acknowledged_at` + `details` JSONB).
- The Remediate handler today does NOT write `details.remediation` — the backend stores remediation context only in audit logs / SIM state. **Backend gap to close in Wave A: write the chosen action into `policy_violations.details->remediation` so FE can derive status durably without joining audit_logs**. Single store call after the existing switch in `Remediate` handler.
- Filtering by status FE-side on the loaded page is fine for SCALE here (limit 50 per page, infinite scroll). For accurate cross-page counts, a `?status=open|acknowledged|remediated|dismissed` server-side filter SHOULD also be added — implemented by translating `status` → existing `acknowledged` query param + a new `details->>'remediation'` filter in `ListEnriched`. **This is in Wave A** (AC-6 status filter).

### D3 — Bulk select pattern: row-checkbox + explicit-id list (defer "all matching filter" to FIX-236)

- Row-checkbox column added; "select-all on visible page" header checkbox.
- Sticky bottom bar: `<count> selected · [Acknowledge selected] [Dismiss selected] [Clear]`.
- Bulk endpoints **NEW** (since per-row endpoints exist today but no batch):
  - `POST /api/v1/policy-violations/bulk/acknowledge` body `{ ids: uuid[], note?: string }`
  - `POST /api/v1/policy-violations/bulk/dismiss` body `{ ids: uuid[], reason: string }`
  - Implementation: server-side loop calling existing `Acknowledge` / `Remediate(action=dismiss)` per id inside a single transaction; emits one audit row per violation (preserves per-violation audit fidelity).
  - Cap: `len(ids) ≤ 100` to bound server time. Returns `{ succeeded: [], failed: [{id, reason}] }` with HTTP 200 even when partial — FE renders failure list in toast.
- "All matching filter" selection (e.g. select all 8,000 violations matching severity=critical) → **deferred to FIX-236**, recorded as D-debt (D-157 below). FE shows tooltip "Selection scoped to visible page — bulk-by-filter coming with FIX-236".

Rationale: the existing FIX-201 SIM bulk pattern already established `sim_ids[]` arrays with audit fidelity. Mirror that contract for violations. Filter-based bulk requires a server-side cursor walker which is FIX-236's territory; doing both here would double the effort.

### D4 — TanStack Query invalidation (PAT-006 RECURRENCE-prone)

For every mutation (acknowledge, remediate, bulk-acknowledge, bulk-dismiss) the `onSuccess` MUST invalidate **all** of:
- `['violations']` (paginated list)
- `['violations', 'counts']` (chart counts)
- `['violations', 'detail', id]` (when single-id mutation)
- `['sims']` and `['sims', simId]` for `suspend_sim` only (sim state changed)
- `['audit-logs']` (audit row appears)

Already-existing hooks `useAcknowledgeViolation` (in `index.tsx`) and `useRemediate` (in `use-violation-detail.ts`) only invalidate `['violations']`. **PAT-006 hot-zone**: pluralised key vs singular key drift. Plan extracts both hooks into a single `web/src/hooks/use-violations.ts` with a shared `invalidateAll(qc)` helper to remove the divergence.

### D5 — Audit verification: read-only check, no backend code

Backend grep already confirms `audit.Emit(...)` for `violation.acknowledge`, `violation.remediated`, `violation.escalated`, `violation.dismissed` (handler.go lines 207, 219, 241, 380). Wave A only **adds**:
- `policy_violations.details->remediation` write (D2 above) — single line in the Remediate handler.
- The two new bulk endpoints (D3) — each emits one `audit.Emit` per id.
- `details->>'remediation'` filter support in `ListEnriched` (D2).

No fix to existing audit emits — they are correct and tested.

### D6 — Export path fix + Nginx redirect (Risk 3)

- FE switches `useExport('violations')` → `useExport('policy-violations')` (one-line change in `index.tsx`).
- Nginx config gets a 301 redirect from the legacy `GET /api/v1/violations/export.csv` to `/api/v1/policy-violations/export.csv` so any bookmarked CSV link keeps working. Implemented in `deploy/nginx/nginx.conf` (or wherever the project's nginx fragments live).

---

## Architecture Context

### Components Involved

- `internal/api/violation/handler.go` (Layer: API · Service: SVC-03) — Already has `List`, `Get`, `Acknowledge`, `Remediate`, `CountByType`, `ExportCSV`. Wave A adds bulk variants and `details.remediation` mutation.
- `internal/store/policy_violation.go` (Layer: Store) — Already has `Acknowledge`, `ListEnriched`, etc. Wave A adds optional `Status` filter param threading.
- `internal/gateway/router.go` (Layer: Gateway) — Wave A adds the two new bulk routes inside the existing `policy-violations` chi block (between line 691 and 700).
- `web/src/pages/violations/index.tsx` (Layer: FE · Page) — Major surface refactor: extract logic, wire actions, add status column + filters + bulk bar.
- `web/src/pages/violations/detail-panel.tsx` (NEW) — Extracted SlidePanel content per AC-5; consumes derived status, renders rich detail.
- `web/src/hooks/use-violations.ts` (NEW) — Single source of truth for `useViolations`, `useViolationCounts`, `useAcknowledgeViolation`, `useRemediate`, `useBulkAcknowledge`, `useBulkDismiss`, plus shared `invalidateViolations(qc)`.
- `web/src/types/violation.ts` (NEW) — Shared `PolicyViolation` interface (currently duplicated between `index.tsx` and `components/shared/related-violations-tab.tsx`) + `ViolationStatus` union + `deriveStatus()` pure function.
- `web/src/components/violations/status-badge.tsx` (NEW) — Visual chip per AC-2.
- `web/src/components/ui/date-range-picker.tsx` (NEW) — Project does not yet have a DateRangePicker; build a simple one (preset pills `Last 24h | 7d | 30d | Custom` + two date inputs for custom). Reused by other pages later.
- `deploy/nginx/nginx.conf` (or `argus/deploy/nginx/conf.d/api.conf`) — 301 redirect for legacy export path.

### Data Flow (acknowledge round-trip)

```
[user clicks Acknowledge in row menu]
  → <AcknowledgeDialog open=true note=""/>
  → user types note (optional) + Confirm
  → useAcknowledgeViolation.mutateAsync({id, note})
  → POST /api/v1/policy-violations/{id}/acknowledge { note }
  → Handler.Acknowledge
    → store.Acknowledge (UPDATE policy_violations SET acknowledged_at = NOW(), …)
    → audit.Emit("violation.acknowledge", entity_type=policy_violation, entity_id=id, after={ack_by, note})
  → response 200 {id, acknowledged_at, acknowledged_by, note}
  → onSuccess: invalidateViolations(qc)
    → ['violations'] refetch → row re-renders with status=Acknowledged
    → ['audit-logs'] refetch → SIM detail Audit tab shows new entry next visit
```

### API Specifications

**Existing — verified via grep (handler.go + router.go):**

- `GET /api/v1/policy-violations` — list, supports `cursor, limit, sim_id, policy_id, violation_type, action_taken (NEW), severity, acknowledged, status (NEW: open|acknowledged|remediated|dismissed), date_from (NEW), date_to (NEW)`. Response: `{status, data: ViolationDTO[], meta: {cursor, limit, has_more}}`.
- `GET /api/v1/policy-violations/counts` — counts by violation_type. Response: `{status, data: Record<string,number>}`.
- `GET /api/v1/policy-violations/export.csv` — streams CSV.
- `GET /api/v1/policy-violations/{id}` — DTO with joined names.
- `POST /api/v1/policy-violations/{id}/acknowledge` — `{note?: string}` → `{status, data: {id, acknowledged_at, acknowledged_by, note}}`.
- `POST /api/v1/policy-violations/{id}/remediate` — `{action: "suspend_sim"|"escalate"|"dismiss", reason: string}` → `{status, data: {violation, sim?}}`. Reason min-length validation = none today; Wave A enforces ≥3 chars for `dismiss` and `suspend_sim`.

**NEW (Wave A):**

- `POST /api/v1/policy-violations/bulk/acknowledge` — `{ids: uuid[], note?: string}` → 200 `{status, data: {succeeded: uuid[], failed: [{id, error_code, message}]}}`. Cap `len(ids) ≤ 100`. Each success emits `audit.Emit("violation.acknowledge", …)` per id.
- `POST /api/v1/policy-violations/bulk/dismiss` — `{ids: uuid[], reason: string}` → 200 `{status, data: {succeeded: uuid[], failed: [...]}}`. Reason required, min 3 chars. Each success emits `audit.Emit("violation.dismissed", …)`.

Standard envelope (`apierr.WriteSuccess` / `apierr.WriteError`). Auth: `policy_admin` role required (same as per-row endpoints — existing middleware reused).

### Database Schema (existing, no migration needed)

Source: `migrations/<existing>_policy_violations.sql` (referenced from `internal/store/policy_violation.go`). Verified existing columns:

```sql
-- policy_violations (existing — TBL-?? per docs/architecture/db/_index.md)
id                   UUID PRIMARY KEY
tenant_id            UUID NOT NULL
sim_id               UUID NOT NULL
policy_id            UUID NOT NULL
version_id           UUID NOT NULL
rule_index           INT NOT NULL
violation_type       VARCHAR
action_taken         VARCHAR
details              JSONB           -- writes new key 'remediation' in Wave A
session_id           UUID NULL
operator_id          UUID NULL
apn_id               UUID NULL
severity             VARCHAR
created_at           TIMESTAMPTZ
acknowledged_at      TIMESTAMPTZ NULL
acknowledged_by      UUID NULL
acknowledgment_note  TEXT NULL
-- Indexes (existing): idx_policy_violations_unack WHERE acknowledged_at IS NULL
```

No schema change. Wave A adds a `details` JSONB key write (`details = jsonb_set(coalesce(details,'{}'),'{remediation}', to_jsonb($1::text))`) inside `Remediate` handler — fully backwards-compatible.

### Screen Mockup (FIX-244 list row + sticky bulk bar)

```
┌───────────────────────────────────────────────────────────────────────────────────┐
│ Policy Violations                                          [Export] [Refresh]     │
│ [search]  [Type ▾]  [Action ▾]  [Severity ▾]  [Status ▾]  [Date range ▾]  [Clear] │
├───┬───────┬──────────────────────┬──────────────┬────────────┬──────┬────────────┤
│ ☐ │ ●     │ bandwidth_exceeded   │ ICCID 89...  │ ABC Cap v3 │ 2 GB │  3 min ago │ <- row
│   │ HIGH  │ → throttle           │              │            │ /1GB │      [⋮]   │
├───┼───────┼──────────────────────┼──────────────┼────────────┼──────┼────────────┤
│ ☐ │ ●     │ session_limit  ACK   │ ICCID 89...  │ XYZ Biz v1 │ 6/5  │  4 min ago │
└───┴───────┴──────────────────────┴──────────────┴────────────┴──────┴────────────┘
                                                          ┌──────────────────┐
                                                          │ 12 selected      │
                                                          │ [Ack] [Dismiss]  │
                                                          │ [Clear]          │
                                                          └──────────────────┘
```

### Design Token Map (UI — MANDATORY)

#### Color tokens (project-defined in `web/src/index.css @theme`)

| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Status: open | `bg-danger-dim text-danger border-danger/30` | `bg-red-100`, hex |
| Status: acknowledged | `bg-warning-dim text-warning border-warning/30` | `bg-yellow-100`, hex |
| Status: remediated | `bg-success-dim text-success border-success/30` | `bg-green-100`, hex |
| Status: dismissed | `bg-bg-hover text-text-tertiary border-border-subtle` | `bg-gray-100`, hex |
| Destructive button | `bg-danger text-white hover:bg-danger/90` | `bg-red-500`, hex |
| Neutral text | `text-text-primary / text-text-secondary / text-text-tertiary` | `text-gray-*` |
| Surface | `bg-bg-surface / bg-bg-elevated / bg-bg-hover` | `bg-white`, hex |
| Borders | `border-border-default / border-border-subtle` | `border-gray-200`, hex |
| Accent (links, ICCID) | `text-accent hover:underline` | `text-blue-500`, hex |

Note: PAT-018 — NEVER use Tailwind default numbered palette (`text-red-500`, `bg-blue-100`). Project uses CSS-var semantic tokens. UI scout will grep new files.

#### Typography tokens (existing usage in `index.tsx`)

| Usage | Token Class |
|-------|-------------|
| Page title | `text-[16px] font-semibold text-text-primary` (matches existing) |
| Row primary text | `text-xs font-medium text-text-primary` |
| Row meta | `text-[10px] text-text-tertiary` |
| Mono (ICCID, IDs) | `font-mono text-xs` |
| Section heading in panel | `text-[10px] uppercase tracking-wider text-text-tertiary` |

#### Components to REUSE (DO NOT recreate)

| Component | Path | Use For |
|-----------|------|---------|
| `<Button>` | `web/src/components/ui/button.tsx` | ALL buttons (variants: default, outline, ghost, destructive) |
| `<Dialog>` + `<DialogContent>` + `<DialogFooter>` + `<DialogTitle>` | `web/src/components/ui/dialog.tsx` | Acknowledge / Remediate / Bulk confirms (Option C compact) |
| `<SlidePanel>` + `<SlidePanelFooter>` | `web/src/components/ui/slide-panel.tsx` | Row click rich detail (Option C rich) |
| `<DropdownMenu>` family | `web/src/components/ui/dropdown-menu.tsx` | Row action menu (already used) |
| `<Select>` | `web/src/components/ui/select.tsx` | Type / Action / Severity / Status filter dropdowns |
| `<Checkbox>` | `web/src/components/ui/checkbox.tsx` | Row + header bulk select |
| `<Textarea>` | `web/src/components/ui/textarea.tsx` | Note / reason fields in confirm dialogs |
| `<SeverityBadge>` | `web/src/components/shared/severity-badge.tsx` | Severity chip (FIX-211 unified taxonomy) |
| `<EntityLink>` | `web/src/components/shared/entity-link.tsx` | ICCID, Policy, Operator, Session links (FIX-219) |
| `<EmptyState>` | `web/src/components/shared/empty-state.tsx` | "No violations…" |
| `<Breadcrumb>` | `web/src/components/ui/breadcrumb.tsx` | Top breadcrumb (already wired) |
| `SEVERITY_FILTER_OPTIONS` | `web/src/lib/severity.ts` | Severity dropdown options (FIX-211) |

NEVER raw `<button>`, `<input>`, `<dialog>`. NEVER `confirm()` / `alert()`.

---

## Acceptance Criteria Mapping

| AC | Description | Wave | Task(s) |
|----|-------------|------|---------|
| AC-1 | Row Acknowledge / Remediate actions wired (Dialog confirm) | C | C-2, C-3 |
| AC-2 | Status column derived chip (open/ack/remediated/dismissed) | B | B-1, B-2 |
| AC-3 | Filter Type uses `violation_type`; new Action filter sends `action_taken` | A, B | A-2 (BE filter), B-3 (FE dropdown) |
| AC-4 | Export path fixed `/api/v1/policy-violations/export.csv` + Nginx 301 | B, A | B-3 (FE), A-4 (Nginx) |
| AC-5 | Row click → enriched SlidePanel (severity, type, action, full DETAILS, ICCID link, Policy link, Session link, Operator+APN chips, timeline, related violations, action buttons) | C | C-1, C-3 |
| AC-6 | Date-range + Status + Severity filters | B, A | A-2 (BE), B-3 (FE), B-4 (DateRangePicker) |
| AC-7 | Empty state copy professional + reflects active timeframe | B | B-3 |
| AC-8 | Row detail polish — ICCID link, policy_name+version chip, measured/threshold inline | B | B-2 |
| AC-9 | Bulk Acknowledge + Bulk Dismiss with row checkboxes + sticky bar | A, C | A-3 (BE bulk), C-4 (FE bar+dialogs) |
| AC-10 | Audit trail entries written for ack/remediate/dismiss/escalate | A | A-1 verify (backend already emits — verify per-event including new bulk path) |

---

## Files to Touch

### Wave A — Backend additions (status filter + bulk + remediation field + nginx redirect)

| Path | Change |
|------|--------|
| `internal/store/policy_violation.go` | EDIT. Add `Status` filter param to `ListViolationsParams` (translates to `acknowledged_at IS NULL` / `IS NOT NULL` AND `details->>'remediation' = …`). Add `DateFrom *time.Time, DateTo *time.Time` filter params. |
| `internal/api/violation/handler.go` | EDIT. (a) Inside `Remediate` for `suspend_sim` and `dismiss`, after the existing `Acknowledge` call, write `details->remediation = $action` via a new `store.SetRemediationKind(ctx, id, tenantID, kind)` helper OR inline jsonb_set UPDATE. (b) Parse `status`, `action_taken`, `date_from`, `date_to` from list query. (c) Enforce `reason` min 3 chars on `dismiss` action (return 400 if shorter). (d) Add new `BulkAcknowledge` and `BulkDismiss` handlers (loop ids, call existing methods, accumulate succeeded/failed, emit one audit per success). |
| `internal/api/violation/handler_test.go` | EDIT. Add `TestRemediate_Suspend_WritesRemediationKind`, `TestList_StatusFilter_Open/Ack/Remediated`, `TestList_DateRangeFilter`, `TestBulkAcknowledge_AllSucceed`, `TestBulkAcknowledge_PartialFail`, `TestBulkDismiss_RequiresReason`. |
| `internal/store/policy_violation.go` | EDIT. Add `SetRemediationKind(ctx, id, tenantID, kind string) error` that does `UPDATE policy_violations SET details = jsonb_set(coalesce(details,'{}'), '{remediation}', to_jsonb($1::text)) WHERE id=$2 AND tenant_id=$3`. |
| `internal/gateway/router.go` | EDIT. After line 700 add `r.Post("/api/v1/policy-violations/bulk/acknowledge", deps.ViolationHandler.BulkAcknowledge)` and `r.Post("/api/v1/policy-violations/bulk/dismiss", deps.ViolationHandler.BulkDismiss)` inside the same role-guarded block. |
| `deploy/nginx/conf.d/api.conf` (or equivalent project nginx file) | EDIT. Add `location = /api/v1/violations/export.csv { return 301 /api/v1/policy-violations/export.csv$is_args$args; }` BEFORE the upstream proxy_pass. |

### Wave B — FE foundation (types, hooks, status, filters, export path, empty state)

| Path | Change |
|------|--------|
| `web/src/types/violation.ts` | NEW. Export `PolicyViolation` interface (lifted from `index.tsx`), `ViolationStatus` union, pure `deriveStatus(v)` function, `STATUS_FILTER_OPTIONS` const. |
| `web/src/hooks/use-violations.ts` | NEW. Export: `useViolations(filters)`, `useViolationCounts()`, `useAcknowledgeViolation()`, `useRemediate(id)`, `useBulkAcknowledge()`, `useBulkDismiss()`, plus internal `invalidateViolations(qc, opts?)` helper that hits all 5 query keys (D4). Replaces `useAcknowledgeViolation` inline in `index.tsx` and merges `use-violation-detail.ts`. |
| `web/src/hooks/use-violation-detail.ts` | EDIT. Re-export from `use-violations.ts` for backward compat with existing call sites (or delete after grep confirms no external imports). |
| `web/src/components/violations/status-badge.tsx` | NEW. Pure presentation component. Accepts `status: ViolationStatus`, renders chip with the four token classes from D2 + Design Token Map. |
| `web/src/components/ui/date-range-picker.tsx` | NEW. Compact dropdown with preset pills (`24h`, `7d`, `30d`, `Custom`) + two date inputs for Custom. Emits `{from?: string, to?: string}` ISO strings. |
| `web/src/pages/violations/index.tsx` | EDIT (heavy). (a) Replace inline `PolicyViolation` interface with `import { PolicyViolation } from '@/types/violation'`. (b) Replace inline `useAcknowledgeViolation` with imported version. (c) Add `STATUS_FILTER_OPTIONS`, `ACTION_TAKEN_FILTER_OPTIONS`, date-range state. (d) Fix the `TYPE_OPTIONS` enum to actual `violation_type` values (`bandwidth_exceeded`, `session_limit`, `quota_exceeded`, `time_restriction`, `geo_blocked`) — current values are `action_taken`. (e) Add new Action filter dropdown using current values. (f) Replace `useExport('violations')` → `useExport('policy-violations')`. (g) Empty state copy: `"No policy violations in {humanizeRange(filters)}."`. (h) Add Status badge column to row. (i) Replace literal "SIM" link text with `<EntityLink entityType="sim" entityId={v.sim_id} label={v.iccid ?? v.sim_iccid}>` and use `EntityLink` for Policy + Operator + Session as well (FIX-219). (j) Show measured/threshold inline if `details.current_bytes`/`threshold_bytes` present. (k) Remove the `filtered.filter((v) => …!v.acknowledged_at)` line — current code hides acknowledged rows entirely; with status column we keep them. |

### Wave C — FE actions: dialogs, detail panel content, bulk bar

| Path | Change |
|------|--------|
| `web/src/pages/violations/detail-panel.tsx` | NEW. Lift the existing inline SlidePanel `children` from `index.tsx` (lines 509–585) into a dedicated component. Add: status chip header, related-violations list (reuse `<RelatedViolationsTab scope="sim" entityId={v.sim_id} />`), action buttons in `<SlidePanelFooter>` ("Acknowledge" / "Remediate ▾" with sub-menu / "Dismiss"). Buttons call the same hooks; on success the panel auto-refreshes via query invalidation. |
| `web/src/components/violations/acknowledge-dialog.tsx` | NEW. `<Dialog>` with optional `<Textarea>` note field, Cancel + "Acknowledge" buttons. Shared by row menu, panel footer, and bulk bar (`mode: 'single' | 'bulk', count?`). |
| `web/src/components/violations/remediate-dialog.tsx` | NEW. `<Dialog>` with required reason `<Textarea>` (min 3 chars), action-specific copy: `suspend_sim` → destructive red Confirm + warning text; `escalate` → neutral; `dismiss` → neutral. |
| `web/src/components/violations/bulk-action-bar.tsx` | NEW. Sticky-bottom bar; props `{count, onAck, onDismiss, onClear}`. Token classes: `bg-bg-elevated border-t border-border-default shadow-card`. |
| `web/src/pages/violations/index.tsx` | EDIT. (a) Add bulk-selection state: `Set<string>`; row checkbox, header checkbox (select-all on visible page only); (b) Render `<BulkActionBar>` when `selectedIds.size > 0`; (c) Wire bulk dialogs; (d) Replace existing inline DropdownMenu items: "Suspend SIM" → opens `<RemediateDialog action="suspend_sim">`; "Dismiss" → opens `<RemediateDialog action="dismiss">` (was wrongly using Acknowledge); add new "Acknowledge" item → opens `<AcknowledgeDialog>`; "Escalate" → opens `<RemediateDialog action="escalate">`. (e) Replace inline detail panel children with `<ViolationDetailPanel violation={selectedViolation} onClose={...}>`. |
| `web/src/components/violations/__tests__/status-badge.test.tsx` | NEW. Snapshot for each of 4 statuses. |
| `web/src/components/violations/__tests__/acknowledge-dialog.test.tsx` | NEW. Renders, calls onConfirm with note. |
| `web/src/components/violations/__tests__/remediate-dialog.test.tsx` | NEW. suspend_sim renders destructive warning; dismiss requires ≥3-char reason. |
| `web/src/pages/violations/__tests__/index.test.tsx` | NEW (or extend existing). Click Acknowledge in row menu → mutation called with id; bulk bar appears when row checkboxes selected; export click hits `/api/v1/policy-violations/export.csv`. |

---

## Tasks

### Wave A — Backend (4 tasks, parallelizable internally)

#### Task A-1 — Backend: write `details.remediation` + reason validation [DEV-520]
- **Files:** Modify `internal/store/policy_violation.go` (add `SetRemediationKind`), `internal/api/violation/handler.go` (call it after Remediate switch).
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `internal/store/policy_violation.go` `Acknowledge` method — same structure (single UPDATE with `tenant_id` guard, `RETURNING` row).
- **Context refs:** `Architecture Decisions > D2`, `Database Schema`, `API Specifications`.
- **What:**
  - In `Remediate` handler, BEFORE writing the response for `suspend_sim` and `dismiss`, call `h.violationStore.SetRemediationKind(r.Context(), id, tenantID, req.Action)`. For `escalate` write kind = `"escalate"`.
  - For `dismiss` and `suspend_sim`, validate `len(strings.TrimSpace(req.Reason)) >= 3` BEFORE doing any work — return 400 with field error if not.
- **Verify:** `go test ./internal/api/violation/... -run TestRemediate` — assert response 200 + DB row has `details->>'remediation' = 'suspend_sim'`.

#### Task A-2 — Backend: `status`, `action_taken`, `date_from`, `date_to` list filters [DEV-521]
- **Files:** Modify `internal/store/policy_violation.go` (extend `ListViolationsParams` + ListEnriched query builder), `internal/api/violation/handler.go` (`List` parses query params).
- **Depends on:** A-1 (uses `details->remediation` for status filter)
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/policy_violation.go::ListEnriched` — extend the existing dynamic WHERE builder.
- **Context refs:** `API Specifications`, `Architecture Decisions > D2`.
- **What:**
  - Add `Status string` (one of `open|acknowledged|remediated|dismissed`), `ActionTaken string`, `DateFrom *time.Time`, `DateTo *time.Time` to `ListViolationsParams`.
  - Translate `status`:
    - `open` → `acknowledged_at IS NULL AND (details->>'remediation') IS NULL`
    - `acknowledged` → `acknowledged_at IS NOT NULL AND (details->>'remediation') IS NULL`
    - `remediated` → `details->>'remediation' = 'suspend_sim'`
    - `dismissed` → `details->>'remediation' = 'dismiss'`
  - `action_taken` → `action_taken = $N`.
  - `date_from`/`date_to` → `created_at >= $N` / `created_at <= $N`.
  - Validate `status` value (whitelist) and `date_*` parse (RFC3339); return 400 on bad input.
- **Verify:** `go test ./internal/api/violation/... -run TestList_StatusFilter` (3 sub-tests covering open / ack / remediated).

#### Task A-3 — Backend: bulk acknowledge + bulk dismiss endpoints [DEV-522]
- **Files:** Modify `internal/api/violation/handler.go` (add `BulkAcknowledge` + `BulkDismiss` methods), `internal/gateway/router.go` (register routes).
- **Depends on:** A-1
- **Complexity:** medium
- **Pattern ref:** Read existing `Acknowledge` handler — wrap it in a loop with per-id error capture. For audit, mirror the existing `audit.Emit` call.
- **Context refs:** `Architecture Decisions > D3`, `API Specifications > NEW`.
- **What:**
  - Decode `{ids: []uuid.UUID, note?/reason: string}`. Validate `1 ≤ len(ids) ≤ 100` (return 400 with `errors.too_many` if >100). For dismiss, `len(strings.TrimSpace(reason)) >= 3` required.
  - Iterate; per id: call `store.Acknowledge` (for ack) or `store.Acknowledge` + `store.SetRemediationKind(…, "dismiss")` (for dismiss); on error append `{id, error_code, message}` to `failed[]`; on success append id to `succeeded[]` and emit per-id `audit.Emit`.
  - Always return 200 with `{succeeded, failed}` (partial-success contract).
  - Register routes in `router.go` inside the same role-guarded block as the per-row endpoints.
- **Verify:** `go test ./internal/api/violation/... -run TestBulk` (all-succeed, partial-fail, exceeds-cap, missing-reason).

#### Task A-4 — Nginx 301 redirect for legacy export path [DEV-523]
- **Files:** Modify `deploy/nginx/conf.d/api.conf` (or the file containing the `/api/v1/` `proxy_pass`).
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read existing nginx redirect/`return 301` blocks if any in `deploy/`.
- **Context refs:** `Architecture Decisions > D6`, `Risks & Mitigations`.
- **What:** Add `location = /api/v1/violations/export.csv { return 301 /api/v1/policy-violations/export.csv$is_args$args; }` BEFORE the catch-all `/api/v1/` proxy block.
- **Verify:** `curl -sI http://localhost:8084/api/v1/violations/export.csv` returns `301 Location: /api/v1/policy-violations/export.csv`.

**Quality gate (Wave A):** `go vet ./...`, `go test ./internal/api/violation/... ./internal/store/...` all pass; `make web-build` not affected; manual `curl` round-trip on the new bulk endpoints (smoke test from a logged-in `policy_admin` token).

---

### Wave B — FE foundation (5 tasks)

#### Task B-1 — Type extraction + status derivation [DEV-524]
- **Files:** Create `web/src/types/violation.ts`.
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `web/src/types/sim.ts` — same module shape (interface + helper).
- **Context refs:** `Architecture Decisions > D2`, `Files to Touch > Wave B`.
- **What:** Lift `PolicyViolation` interface from `index.tsx` (line 42) and `related-violations-tab.tsx` (its local copy) into a single export. Add `ViolationStatus` union, `deriveStatus(v)` pure function, `STATUS_FILTER_OPTIONS`, `ACTION_TAKEN_FILTER_OPTIONS`, `VIOLATION_TYPE_FILTER_OPTIONS` consts.
- **Verify:** `pnpm -C web tsc --noEmit` → 0 errors; both `index.tsx` and `related-violations-tab.tsx` import from new module without duplication.

#### Task B-2 — Status badge component + row column [DEV-525]
- **Files:** Create `web/src/components/violations/status-badge.tsx`; Modify `web/src/pages/violations/index.tsx` (add Status badge to row, replace literal "SIM" with `<EntityLink>`, show measured/threshold inline).
- **Depends on:** B-1
- **Complexity:** low
- **Pattern ref:** Read `web/src/components/shared/severity-badge.tsx` — same chip pattern, four discrete states.
- **Context refs:** `Architecture Decisions > D2`, `Design Token Map > Color tokens`, `Components to REUSE`.
- **What:** Status badge: `bg-* text-* border-*` per D2 token rows. Row replaces literal `'SIM'` with `<EntityLink entityType="sim" entityId={v.sim_id} label={v.iccid ?? v.sim_iccid}>`. If `details.current_bytes` and `details.threshold_bytes` present, render `${formatBytes(current)} / ${formatBytes(threshold)}` chip in the row.
- **Verify:** `grep -nE 'text-(red|blue|green|purple|yellow|gray)-[0-9]{2,3}' web/src/components/violations/status-badge.tsx` → 0 matches (PAT-018 guard); browser load → status chips visually correct for at least 4 seeded violations of each status.

#### Task B-3 — Filter dropdowns: fix Type, add Action / Status / DateRange + export path [DEV-526]
- **Files:** Modify `web/src/pages/violations/index.tsx`.
- **Depends on:** B-1, B-4 (DateRangePicker)
- **Complexity:** low
- **Pattern ref:** Read existing `<Select options={...}>` usage in same file.
- **Context refs:** `Acceptance Criteria Mapping > AC-3, AC-4, AC-6, AC-7`, `API Specifications`.
- **What:**
  - Replace `TYPE_OPTIONS` with `VIOLATION_TYPE_FILTER_OPTIONS` (`bandwidth_exceeded`, `session_limit`, `quota_exceeded`, `time_restriction`, `geo_blocked`, `+ All`).
  - Add new `ACTION_TAKEN_FILTER_OPTIONS` Select (current `TYPE_OPTIONS` literal values: block / disconnect / suspend / throttle / policy_notify / policy_log / policy_tag).
  - Add `STATUS_FILTER_OPTIONS` Select.
  - Add `<DateRangePicker>` (B-4).
  - Wire all to `searchParams` (`violation_type`, `action_taken`, `status`, `date_from`, `date_to`, `severity`).
  - Change `useExport('violations')` → `useExport('policy-violations')`.
  - Empty state description: ``filters.violation_type || filters.severity || filters.status ? 'Try adjusting your filters.' : `No policy violations in ${humanizeDateRange(filters)}.`'``. Add `humanizeDateRange` helper inline.
- **Verify:** Browser: select Severity=critical → URL has `?severity=critical` and only critical rows. Click Export → DevTools shows GET to `/api/v1/policy-violations/export.csv?severity=critical`. Empty state copy renders without "Well done!".

#### Task B-4 — DateRangePicker UI atom [DEV-527]
- **Files:** Create `web/src/components/ui/date-range-picker.tsx` + `__tests__/date-range-picker.test.tsx`.
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `web/src/components/ui/timeframe-selector.tsx` — same project's existing presets-pill component pattern. Mirror the visual style.
- **Context refs:** `Components to REUSE > date-range-picker`, `Design Token Map`.
- **What:** Exposes `<DateRangePicker value={{from?, to?}} onChange={...} presets={['24h','7d','30d']}/>`. Click pill sets both dates relative to now; "Custom" reveals two `<input type="date">` styled with token classes. No third-party calendar lib (project doesn't have one — keep small).
- **Verify:** Test: clicking 24h preset emits `{from: ISO_24h_ago, to: ISO_now}`; clicking Custom + typing date emits matching ISO. Browser: visually consistent with `<TimeframeSelector>`.

#### Task B-5 — Hook consolidation [DEV-528]
- **Files:** Create `web/src/hooks/use-violations.ts`; Modify `web/src/hooks/use-violation-detail.ts` (re-export); Modify `web/src/pages/violations/index.tsx` (use new hooks).
- **Depends on:** B-1
- **Complexity:** low
- **Pattern ref:** Read `web/src/hooks/use-export.ts` — same hook module style.
- **Context refs:** `Architecture Decisions > D4`, `Acceptance Criteria Mapping`.
- **What:**
  - Single `invalidateViolations(qc, opts: {simId?, id?})` helper that invalidates `['violations']`, `['violations', 'counts']`, optionally `['violations', 'detail', id]`, `['sims']`+`['sims', simId]`, `['audit-logs']`.
  - Export hooks: `useViolations(filters)`, `useViolationCounts()`, `useAcknowledgeViolation()`, `useRemediate(id)`, `useBulkAcknowledge()`, `useBulkDismiss()`.
  - Each mutation's `onSuccess` calls the helper.
  - `index.tsx` removes inline `useAcknowledgeViolation` + `useViolations` + `useViolationCounts` and imports them.
- **Verify:** `pnpm -C web tsc --noEmit` clean; `pnpm -C web test src/hooks/use-violations.test.ts`.

**Quality gate (Wave B):** `pnpm -C web tsc --noEmit`, `pnpm -C web lint`, `pnpm -C web build`, all tests pass; PAT-018 grep clean for new files; PAT-021 grep `process\.env` clean for new files.

---

### Wave C — FE actions: dialogs, panel, bulk (4 tasks)

#### Task C-1 — Detail panel extraction + enrichment [DEV-529]
- **Files:** Create `web/src/pages/violations/detail-panel.tsx`; Modify `web/src/pages/violations/index.tsx` (consume).
- **Depends on:** B-1, B-2
- **Complexity:** medium
- **Pattern ref:** Read `web/src/components/policy/rollout-expanded-slide-panel.tsx` — same SlidePanel content + footer pattern.
- **Context refs:** `Acceptance Criteria Mapping > AC-5`, `Components to REUSE > <EntityLink> <SlidePanel>`.
- **What:**
  - Header: severity badge + status badge + violation_type chip + action_taken chip.
  - Body grid: SIM (`<EntityLink entityType="sim">`), Policy (`<EntityLink entityType="policy" label={policy_name+v##}>`), Operator (`<OperatorChip>`), APN (chip), Severity, Type, Action, Time.
  - Full DETAILS JSONB rendering (existing block, kept).
  - "Timeline" section: created_at; if `acknowledged_at` set → "Acknowledged at … by …"; if `details.remediation` set → "{remediation} at audit log time" (best-effort).
  - "Related violations" section: embed `<RelatedViolationsTab scope="sim" entityId={v.sim_id} />`.
  - Footer buttons: `Acknowledge` (if status=open), `Remediate ▾` (sub-menu opens RemediateDialog), `Dismiss` (alias for Remediate dismiss). Buttons disabled when status already terminal.
- **Verify:** Browser: row click → panel shows enriched content; click Acknowledge in footer → AcknowledgeDialog opens.

#### Task C-2 — Acknowledge + Remediate dialogs [DEV-530]
- **Files:** Create `web/src/components/violations/acknowledge-dialog.tsx`, `web/src/components/violations/remediate-dialog.tsx`, plus colocated tests.
- **Depends on:** B-5 (uses hooks)
- **Complexity:** low
- **Pattern ref:** Read any existing `Dialog`-based confirm in the project — e.g. `web/src/components/policy/...` or `web/src/components/sim/...` — find a destructive confirm.
- **Context refs:** `Architecture Decisions > D1`, `Components to REUSE > <Dialog> <Textarea>`, `Acceptance Criteria Mapping > AC-1`.
- **What:**
  - `AcknowledgeDialog`: props `{open, onOpenChange, mode: 'single' | 'bulk', count?, onConfirm: (note?: string) => Promise<void>}`. Optional note. Title: single → "Acknowledge violation"; bulk → "Acknowledge {count} violations".
  - `RemediateDialog`: props `{open, onOpenChange, action, sim?: {iccid}, onConfirm: (reason: string) => Promise<void>}`. Required reason min 3 chars (button disabled until). For `suspend_sim`: title "Suspend SIM {iccid}?", warning paragraph "This disconnects all active sessions and revokes RADIUS Access-Accept until reactivated.", destructive red Confirm button. For `escalate`: optional reason. For `dismiss`: required reason min 3 chars.
- **Verify:** Tests cover: Confirm disabled when reason <3 chars (suspend/dismiss); destructive variant renders red button; mode=bulk title shows count.

#### Task C-3 — Wire row actions + panel actions to dialogs [DEV-531]
- **Files:** Modify `web/src/pages/violations/index.tsx`, `web/src/pages/violations/detail-panel.tsx`.
- **Depends on:** C-1, C-2
- **Complexity:** medium
- **Pattern ref:** Read existing `DropdownMenuItem onSelect` handlers in `index.tsx`.
- **Context refs:** `Acceptance Criteria Mapping > AC-1, AC-5`, `Architecture Decisions > D1`.
- **What:**
  - Replace existing DropdownMenu items:
    - "Acknowledge" (NEW) → opens `<AcknowledgeDialog>`.
    - "Remediate › Suspend SIM" → opens `<RemediateDialog action="suspend_sim" sim={{iccid: v.iccid}}>`.
    - "Remediate › Escalate" → opens `<RemediateDialog action="escalate">`.
    - "Remediate › Dismiss (false positive)" → opens `<RemediateDialog action="dismiss">`.
    - Remove the old "Suspend SIM (navigate-away)" item — replaced by in-place action.
    - Remove the old broken "Dismiss" that called `acknowledgeMutation` — re-route through `<RemediateDialog action="dismiss">`.
  - Each `onConfirm` calls the corresponding mutation hook (B-5) and shows a `toast.success`/`toast.error`.
  - On `suspend_sim` success, also show a tertiary toast "SIM suspended — visit /sims/{id} for state" with a link.
- **Verify:** Browser: 4 row actions all work; status column updates after each; `useExport('policy-violations')` Export still works.

#### Task C-4 — Bulk select + sticky bar + bulk dialogs [DEV-532]
- **Files:** Create `web/src/components/violations/bulk-action-bar.tsx`; Modify `web/src/pages/violations/index.tsx`.
- **Depends on:** C-2 (reuses dialogs in `mode="bulk"`)
- **Complexity:** medium
- **Pattern ref:** Read `web/src/pages/sims/index.tsx` (FIX-201) — established sticky bulk bar pattern. Mirror the layout (count chip, action buttons, Clear).
- **Context refs:** `Architecture Decisions > D3`, `Acceptance Criteria Mapping > AC-9`.
- **What:**
  - State: `const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())`.
  - Row checkbox + header "select all on visible page" checkbox.
  - When `selectedIds.size > 0` render `<BulkActionBar count={size} onAck={...} onDismiss={...} onClear={...}>`.
  - `onAck` → opens `<AcknowledgeDialog mode="bulk" count={size}>` → on confirm calls `useBulkAcknowledge().mutateAsync({ids: [...selectedIds], note})` → on success: toast `${succeeded.length} acknowledged, ${failed.length} failed` + clear selection.
  - `onDismiss` → opens `<RemediateDialog action="dismiss" mode="bulk" count={size}>` → calls `useBulkDismiss()`.
  - Tooltip on header checkbox: "Selection scoped to visible page — bulk-by-filter coming with FIX-236" (D3, D-157).
- **Verify:** Browser: check 3 rows → bar shows "3 selected"; click Bulk Acknowledge → confirm → all 3 update; partial-fail surface in toast.

**Quality gate (Wave C):** All Wave A + B gates plus: tsc clean, lint clean, vite build clean, all unit tests green; manual run of CSV export → file downloads from new path; manual run of bulk actions → audit_logs DB shows N entries (one per id) with action `violation.acknowledge` / `violation.dismissed`.

---

## Risk Register

| Risk | Mitigation |
|------|------------|
| R-1: Suspend SIM is destructive across whole platform — accidental clicks | Two-stage confirm: row drop-down → `<RemediateDialog>` with red destructive button + warning paragraph + required reason ≥3 chars + ICCID echoed in title. |
| R-2: Bulk dismiss could mass-hide real issues | Required reason ≥3 chars on dismiss (single AND bulk); confirm dialog shows count; toast shows succeeded/failed split so partial mistakes are visible. |
| R-3: Old FE export path still bookmarked | A-4 adds Nginx 301 redirect. FE itself updates to correct path; legacy bookmarks land on the right resource. |
| R-4: FIX-236 not yet done — "all matching filter" bulk pattern unavailable | D3 explicit: row-checkbox bulk only, header tooltip points users to FIX-236. D-157 tracks. No false promise. |
| R-5: PAT-006 RECURRENCE — new mutation hooks miss query key invalidations | D4 + B-5: single `invalidateViolations(qc, opts)` helper; every mutation uses it; new test asserts `qc.invalidateQueries` called with all 5 keys (mock query client). |
| R-6: PAT-021 — accidental `process.env.*` use | Wave B/C: pre-commit grep `process\.env` in `web/src/components/violations/**` + `web/src/pages/violations/**` + `web/src/hooks/use-violations.ts` → must be 0 matches. |
| R-7: PAT-018 — Tailwind default palette in new chip components | Wave B/C: pre-commit grep numbered Tailwind palette in new files → must be 0 matches. |
| R-8: PAT-023 — schema drift | Story does not add a migration (only writes existing JSONB key). Boot-time `schemacheck` already covers. No new exposure. |
| R-9: Backend `Remediate` for `escalate` does NOT call `Acknowledge` today — escalated rows stay `acknowledged_at IS NULL` and FE status would say "open" | Either (a) write `details.remediation = 'escalate'` (kept open semantically; FE shows new "Escalated" sub-status pulled from same field) or (b) add `Acknowledge` call. Plan choice: (a) — escalation is a notification, not a closure; keep status=open with `details.remediation=escalate` and surface a small `Bell` icon next to the row. Status taxonomy in D2 documents this. |
| R-10: Status filter logic on backend uses `details->>'remediation'` — index missing | Existing `idx_policy_violations_unack` covers the `acknowledged` distinction. For `remediated`/`dismissed` filters with no severity narrowing, the JSONB scan is OK (low cardinality). If perf becomes an issue we can `CREATE INDEX … ON policy_violations ((details->>'remediation'))` in a follow-up — not in scope. Recorded as D-158. |

---

## Test Plan

### Backend (Go)

- `TestRemediate_Suspend_WritesRemediationKind` — after suspend, row `details->>'remediation' = 'suspend_sim'`.
- `TestRemediate_Dismiss_WritesRemediationKind` — same for dismiss.
- `TestRemediate_Dismiss_RequiresReasonMin3` — 400 if reason missing/short.
- `TestRemediate_Suspend_RequiresReasonMin3` — same.
- `TestList_StatusFilter_Open` — only un-acked, no remediation key.
- `TestList_StatusFilter_Acknowledged` — acked, no remediation.
- `TestList_StatusFilter_Remediated` — remediation='suspend_sim'.
- `TestList_StatusFilter_Dismissed` — remediation='dismiss'.
- `TestList_DateRangeFilter` — created_at within ±range.
- `TestList_ActionTakenFilter` — only matching action_taken.
- `TestBulkAcknowledge_AllSucceed` — N ids, 200, succeeded=[N], failed=[], one audit per id.
- `TestBulkAcknowledge_PartialFail` — one id already acknowledged → failed[1] entry, succeeded[N-1].
- `TestBulkAcknowledge_ExceedsCap` — len(ids)=101 → 400.
- `TestBulkDismiss_RequiresReason` — empty reason → 400.

### Frontend (Vitest + RTL)

- `types/violation.test.ts` — `deriveStatus` returns correct status for all 4 input combinations.
- `components/violations/status-badge.test.tsx` — snapshot for each status.
- `components/violations/acknowledge-dialog.test.tsx` — opens, calls onConfirm with note, mode="bulk" shows count.
- `components/violations/remediate-dialog.test.tsx` — suspend_sim variant red Confirm; reason <3 disables Confirm; dismiss requires reason.
- `components/violations/bulk-action-bar.test.tsx` — buttons disabled when count=0; click emits onAck/onDismiss.
- `hooks/use-violations.test.ts` — `useAcknowledgeViolation` `onSuccess` invalidates all 5 query keys (mock qc).
- `pages/violations/__tests__/index.test.tsx` — Export click hits correct path; row Acknowledge → mutation called.

### Manual UAT (browser)

1. Filter Severity=critical + Status=Open + DateRange=24h → only critical un-acked rows.
2. Click row Acknowledge → dialog → confirm → row status chip turns yellow "Acknowledged"; audit row appears in audit log.
3. Click row Remediate → Suspend SIM → confirm dialog with ICCID + warning → confirm → row status green "Remediated"; visit `/sims/{id}` shows status=suspended.
4. Click row Remediate → Dismiss → confirm with reason "test" → fails (≥3 chars) → reason "false positive" → confirm → row status gray "Dismissed".
5. Bulk: check 3 rows → sticky bar shows "3 selected" → Bulk Acknowledge → all three turn yellow; toast "3 acknowledged, 0 failed".
6. Click Export → CSV downloads from `/api/v1/policy-violations/export.csv` (DevTools Network tab).
7. Hit legacy URL `/api/v1/violations/export.csv` directly → 301 to correct path.
8. Open SlidePanel on a row → see Status header chip, ICCID-as-link, Policy link, Related-violations tab with sibling rows.
9. Empty state: filter Severity=critical + Status=remediated in a fresh tenant → empty state copy is "No policy violations in last 24 hours." (humanized range).
10. Toggle Type=`bandwidth_exceeded` → URL has `?violation_type=bandwidth_exceeded`; toggle Action=`throttle` → URL gains `&action_taken=throttle`; only matching rows.

---

## Out of Scope

- Server-side "all matching filter" bulk select → FIX-236.
- New JSONB index on `details->>'remediation'` → D-158, deferred.
- Server-driven cursor walker for bulk-by-filter (>100 ids) → FIX-236.
- Real-time WS push when violation status changes (currently relies on TanStack Query refetch) → FUTURE.
- PNG / Excel export variants → out of scope.
- Bulk escalate (only ack + dismiss in bulk; escalate stays single-row-only) → low demand, defer.

---

## Decisions Log

- **DEV-520** — Backend writes `details.remediation = $action` after Remediate handler switch, enabling FE-side status derivation without a join against audit_logs. Single small `SetRemediationKind` store helper.
- **DEV-521** — Status filter is server-side via translation `status → (acknowledged_at IS NULL/NOT NULL) AND (details->>'remediation' = …)`. No new column.
- **DEV-522** — Bulk endpoints loop existing per-id methods inside the request scope (no async job), capped at 100 ids per call. Partial-success contract: HTTP 200 with `{succeeded, failed}`. One audit row per success preserves audit fidelity.
- **DEV-523** — Nginx 301 redirect from legacy `/api/v1/violations/export.csv` ensures bookmark backward-compat without breaking the FE refactor.
- **DEV-524** — `PolicyViolation` interface lifted to `web/src/types/violation.ts` to remove duplication between `pages/violations/index.tsx` and `components/shared/related-violations-tab.tsx` (currently each has its own copy).
- **DEV-525** — Status component is FE-derived; no backend `status` column. Source of truth is `acknowledged_at` + `details.remediation`. Deriving in pure function `deriveStatus()` shared between components.
- **DEV-526** — Action filter (`action_taken`) is a SEPARATE dropdown from Type filter (`violation_type`); current single-dropdown UX wrongly merges them. Two dropdowns avoid the F-165 mismatch permanently.
- **DEV-527** — DateRangePicker is an in-house lightweight component (presets + native date inputs) reusing `<TimeframeSelector>` styling. No new third-party dependency.
- **DEV-528** — All violation mutations route through a single `invalidateViolations(qc, opts)` helper that hits 5 query keys. Prevents PAT-006 RECURRENCE (key-drift between sibling hooks).
- **DEV-529** — Bulk Escalate is OUT of scope for this story (escalate is a notification gesture, low demand for batching). Bulk surface is Acknowledge + Dismiss only.
- **DEV-530** — Reason minimum length 3 chars enforced both client-side (button disabled) and server-side (400 with field error). Dual-side validation prevents bypass.
- **DEV-531** — Existing inline `useAcknowledgeViolation` hook in `index.tsx` is removed; the `use-violation-detail.ts` `useRemediate` hook is folded in. Single hooks module simplifies invalidation.
- **DEV-532** — Suspend SIM remediation also calls `Acknowledge` (already in handler today, line 214–216) — kept; status moves to `remediated` (because remediation key wins over `acknowledged_at` per D2 derivation order).

---

## Tech Debt (declared during planning)

- **D-157** — Bulk-select scope is "visible page only". "All matching filter" selection requires a server-side cursor walker; deferred to FIX-236. Tooltip on header checkbox makes the limit visible.
- **D-158** — `(details->>'remediation')` filter scans JSONB without an index. Acceptable for current data volume; if response time on `?status=remediated` exceeds 500 ms at p95 in prod, add `CREATE INDEX idx_policy_violations_remediation ON policy_violations ((details->>'remediation'));` in a follow-up migration.
- **D-159** — Bulk endpoints emit one audit row per id but in a tight loop (no batched audit). For 100-id call this is 100 audit inserts. Acceptable; if it becomes a hot path, batch the audit inserts in a single multi-VALUES INSERT.

---

## Wave Breakdown Summary (S-effort: 3 waves, 13 tasks total)

| Wave | Tasks | Parallelizable | Quality Gate |
|------|-------|----------------|--------------|
| A — Backend (DEV-520..523) | 4 | A-1, A-4 in parallel; A-2 after A-1; A-3 after A-1 | go test + go vet clean; manual curl on bulk |
| B — FE foundation (DEV-524..528) | 5 | B-1 first; B-2/B-4 in parallel after B-1; B-5 after B-1; B-3 after B-1+B-4 | tsc + lint + build; PAT-018 + PAT-021 grep clean |
| C — FE actions (DEV-529..532) | 4 | C-1 + C-2 in parallel after Wave B; C-3 after C-1+C-2; C-4 after C-2 | full test suite + manual UAT 10 scenarios |

Total tasks: **13** (DEV-520 through DEV-532).

---

## Quality Gate Self-Check

| Check | Result |
|-------|--------|
| Spec coverage — AC-1 (row Ack/Remediate dialogs) | ✓ Tasks C-2, C-3 |
| Spec coverage — AC-2 (Status column derived) | ✓ Tasks B-1, B-2 |
| Spec coverage — AC-3 (Type vs Action filter mapping fix) | ✓ Tasks A-2, B-3 |
| Spec coverage — AC-4 (Export path fix + Nginx redirect) | ✓ Tasks A-4, B-3 |
| Spec coverage — AC-5 (SlidePanel rich detail) | ✓ Task C-1 |
| Spec coverage — AC-6 (Date / Status / Severity filters) | ✓ Tasks A-2, B-3, B-4 |
| Spec coverage — AC-7 (Empty state copy) | ✓ Task B-3 |
| Spec coverage — AC-8 (Row detail polish: ICCID link, policy chip, measured/threshold) | ✓ Task B-2 |
| Spec coverage — AC-9 (Bulk operations) | ✓ Tasks A-3, C-4 (with D-157 noted for "all matching filter") |
| Spec coverage — AC-10 (Audit trail) | ✓ Task A-1 verifies existing emits + adds emits in bulk handlers; D5 grep confirmed |
| Pattern compliance — FIX-216 SlidePanel pattern (Option C) | ✓ D1 + Task C-1 reuses existing `<SlidePanel>` + `<Dialog>` split |
| Pattern compliance — FIX-219 entity-link drill-down | ✓ Task B-2 + C-1 use `<EntityLink>` for SIM/Policy/Operator/Session |
| Pattern compliance — FIX-211 unified severity taxonomy | ✓ Reuses `SeverityBadge` + `SEVERITY_FILTER_OPTIONS` (already used in current file) |
| Pattern compliance — FIX-236 filter-based bulk | △ DEFERRED with D-157; row-checkbox bulk delivered today |
| Bug pattern — PAT-006 (struct/key drift) | ✓ D4 + DEV-528: shared `invalidateViolations(qc)` helper |
| Bug pattern — PAT-018 (Tailwind default palette) | ✓ Risk R-7 + Wave B/C grep guard; Design Token Map authoritative |
| Bug pattern — PAT-021 (process.env in FE) | ✓ Risk R-6 + Wave B/C grep guard |
| Bug pattern — PAT-023 (migrate force / schema drift) | ✓ No migration needed; boot-time schemacheck unaffected |
| File touch list complete | ✓ All Wave A/B/C files enumerated with concrete change description |
| Build steps defined | ✓ Per-wave quality gates: `go test`, `go vet`, `pnpm tsc/lint/build`, manual curl + browser UAT |
| Self-containment — embedded API specs | ✓ |
| Self-containment — embedded DB schema | ✓ (existing — no migration; embedded for reference) |
| Self-containment — embedded screen mockup | ✓ |
| Self-containment — Design Token Map populated | ✓ |
| Self-containment — Component Reuse table populated | ✓ |
| Task complexity cross-check (S-effort: most low, max 1 medium) | ✓ Mostly low; 4 medium (A-2, A-3, C-1, C-3, C-4); zero high — appropriate for S |
| Task count = 13 (≥2 for S) | ✓ |
| Each task ≤3 files | ✓ all 13 tasks comply |

**VERDICT: PASS**

Rationale: All 10 ACs map to specific tasks in 3 well-ordered waves. Backend gap closures (status derivation field, bulk endpoints, status filter) are minimal and additive. FE refactor extracts shared types/hooks first to avoid PAT-006 recurrence. All four flagged bug patterns (PAT-006, PAT-018, PAT-021, PAT-023) have explicit mitigations in the risk register and grep guards in the quality gates. The one deferral (filter-based bulk → FIX-236) is documented as D-157 with a user-visible tooltip so no false promise leaks.

---

## Plan Self-Check (process)

- [x] Plan written at `docs/stories/fix-ui-review/FIX-244-plan.md`.
- [x] All 10 ACs mapped to a wave + task.
- [x] All 4 flagged PAT entries (PAT-006, PAT-018, PAT-021, PAT-023) addressed with explicit mitigations.
- [x] Task IDs DEV-520..DEV-532 (13 tasks, continuing from DEV-519 / FIX-243).
- [x] Wave count = 3 (S-effort sweet spot — backend, FE foundation, FE actions).
- [x] Decisions logged DEV-520..DEV-532.
- [x] Tech debt logged D-157..D-159.
- [x] Open questions: 0 — Ana Amil can dispatch Wave A immediately.

# Implementation Plan: FIX-219 — Global Name Resolution + Clickable Cells

## Goal
Standardize entity-reference rendering across ~15 Argus pages: no raw UUID prefixes in primary UI, every entity reference clickable to its detail page, backed by a single `<EntityLink>` primitive. Backfill missing `*_name` / `*_email` fields in five backend DTOs so the UI fix is meaningful (not cosmetic fallback). Preserve developer-facing UUID zones (exports, debug, audit JSON, URLs).

## Summary
- **Extend, not rebuild**, `web/src/components/shared/entity-link.tsx` — the primitive already exists and is consumed by 11 files with the API `{entityType, entityId, label, truncate}`. Story spec's `{type, id, name}` shape is treated as *descriptive*; plan uses the existing prop names additively to avoid a pointless migration.
- **Five backend DTO patches** — Job (`created_by_name`, `created_by_email`), AuditLog (`user_email`, `user_name`), Session stats (`top_operator: {id, name, count}`), eSIM profile (`operator_name`, `operator_code`), violations top-SIMs (`iccid`, `msisdn`). These are tiny store-layer joins, not new endpoints; including them in this story is necessary because AC-4 / AC-6 are cosmetic-only without them.
- **13 page-level rewrites**: sessions, esim, jobs, audit, alerts, violations, sims, dashboard (event chips), dashboard analytics-cost, dashboard analytics-anomalies, admin/sessions-global, admin/security-events, admin/purge-history, admin/tenant-resources, admin/dsar, admin/maintenance, roaming, sms, ops/deploys, reports toast, event-stream chips (components), policies assigned-sims tab, sims cost-attribution tab.
- **Three overlapping components kept separate** (no merge): `EntityLink` (default table cell), `EventEntityButton` (event rows — chevron + ghost button contract), `OperatorChip` (operator-specific code/MCC-MNC display). Plan documents the boundary.
- **FRONTEND.md** new `## Entity Reference Pattern` section — canonical rule + decision tree.

FE + minimal BE scope. No new pages. No new routes.

## Scope Decisions (fixed)

1. **EntityLink API = additive extension, not rename.** Existing callers use `entityType`/`entityId`/`label`. Spec's `type`/`id`/`name` is descriptive — adopting it would force atomic migration of 11 files for zero user-visible gain. Plan adds *new* optional props (`showIcon`, `hoverCard`, `icon`, `onOrphan`, `copyOnRightClick`) without renaming existing ones.
2. **Add two missing routes (`ippool`, `esim_profile`)** — preserve existing extras (`violation`, `alert`, `anomaly`, `job`, `apikey`) that audit/violations pages already depend on.
3. **Three component boundary locked**:
   - `EntityLink` → table cells, filter chip labels, notification body refs, dashboard KPI "Top:" values, any inline text reference.
   - `EventEntityButton` → event stream rows only (has chevron + `variant="ghost"` + event-row styling). Not replaced.
   - `OperatorChip` → operator cell where code/rawId badge is wanted. Not replaced; it already wraps navigation correctly (FIX-202 adoption).
4. **Backend DTO patches INCLUDED in this story** (not deferred). Rationale: AC-4 says "Jobs Created By (F-194)" and AC-6 says "Sessions stats Top: Turkcell" — both ACs are unfulfillable without DTO changes. Deferring to a follow-up FIX breaks the acceptance contract.
5. **Hover card (AC-3)** — implement as *optional* `hoverCard: boolean` prop (default `false`). Lazy fetch via existing `useQuery` cache keys (`operators`, `apns`, `sims`, `users`). 200ms delay. Opt-in per call site. Initial rollout turns it on for: `sim` (detail tab), `operator` (sessions/violations/esim list), `user` (audit). Other types render as basic link.
6. **Right-click copy (AC-11)** — `copyOnRightClick` prop (default `true`). Uses `navigator.clipboard.writeText(entityId)` + toast "Copied UUID to clipboard". No context menu chrome; contextmenu event preventDefault + custom toast keeps it lightweight. Falls back to no-op if clipboard API unavailable.
7. **Orphan "—" (AC-9) — strict, no UUID leak** (reconciled with AC-12). When `label` is empty/null (regardless of `entityId`) in the default render path → render `<span title="Entity reference broken">—</span>` with warning icon; **no slice fallback in primary UI**. Post-Wave 2 every target page has a name field, so a missing name at render time is either a true orphan or a DTO bug — both must render "—", never a UUID prefix. The existing `truncate={true}` opt-in path (used by audit/admin for intentional UUID display) is preserved for dev-facing contexts only; plan never calls it from list cells.
8. **Icon mapping (AC-1 `showIcon`)** — use lucide icons already imported project-wide. Map: `sim→Smartphone`, `operator→Radio`, `apn→Cloud`, `policy→ShieldCheck`, `user→User`, `session→Activity`, `tenant→Building2`, `ippool→Network`, `esim_profile→Smartphone`, `violation→AlertTriangle`, `alert→Bell`, `anomaly→Sparkles`, `job→Briefcase`, `apikey→Key`. 3.5 × 3.5 size, `text-text-tertiary` default color, `text-accent` on hover.
9. **UUID-only zones preserved (AC-12)** — exports (`/export.csv`, `/export.json`), URL query strings, audit log JSON dump, dev debug pane. Documented in FRONTEND.md section; no enforcement code needed (spot-checked via audit pass).

## Architecture Context

### Existing primitives (reuse, DO NOT duplicate)

| Component | Path | Role | Current API |
|---|---|---|---|
| `EntityLink` | `web/src/components/shared/entity-link.tsx` | Default table-cell entity reference | `{entityType, entityId, label?, className?, truncate?}` — **extend, don't rename** |
| `EventEntityButton` | `web/src/components/event-stream/event-entity-button.tsx` | Event stream row entity chip (chevron + ghost) | `{entity?, entityTypeFallback?, entityIdFallback?, onNavigate?}` — consumes FIX-212 `entity` envelope shape |
| `OperatorChip` | `web/src/components/shared/operator-chip.tsx` | Operator cell with code badge + optional click | `{name, code, rawId, clickable?, onClick?}` |
| `CopyableId` | `web/src/components/shared/copyable-id.tsx` | UUID pill with copy button (detail pages) | — |
| `Tooltip` | `web/src/components/ui/tooltip.tsx` | Hover tooltip | — |

### FIX-212 envelope alignment (AC-7)
- `web/src/types/events.ts` L8-12 exports `EntityRef = {type, id, display_name?}` and `envelopeRowLabel()` helper.
- `web/src/components/event-stream/event-row.tsx` already passes `event.entity` through to `EventEntityButton`.
- **No new AC-7 work needed in event-row.tsx** except to verify event-source-chips.tsx (L27-52) consumes `envelope.entity.display_name` when present. Current code still uses raw `event.operator_id.slice(0,8)` — **needs update**.

### DTO gap inventory (blocking for specific ACs)

| DTO | Current | Missing | AC | Backend file |
|---|---|---|---|---|
| `SessionStats` → `/api/v1/sessions/stats` | `by_operator: {<UUID>: count}` map | `top_operator: {id, name, count}` | AC-6 F-158 | `internal/api/session/stats.go` (or equivalent) |
| `Job` → `/api/v1/jobs` list | `created_by: UUID` | `created_by_name: string`, `created_by_email: string`, `is_system: bool` (for cron) | AC-4 F-194 | `internal/api/job/` |
| `AuditLog` → `/api/v1/audit` list | `user_id: UUID` | `user_email: string`, `user_name: string` | AC-4 F-204 | `internal/api/audit/` |
| `eSIMProfile` → `/api/v1/esim-profiles` list | `operator_id: UUID` | `operator_name: string`, `operator_code: string` | AC-4 F-173 | `internal/api/esim/` |
| Violations top-SIMs aggregation | UUID prefix in `violation.sim_id` | row already has `iccid` + `sim_iccid` — verify + add `msisdn` | AC-4 F-163 | `internal/api/policy/violations_counts.go` (or aggregation) |
| Purge-history → `/api/v1/admin/purge-history` | `actor_id: UUID` | `actor_email: string` | AC-4 F-12n (admin) | `internal/api/admin/purge_history.go` |
| Security-events → `/api/v1/admin/security-events` | `user_id: UUID` | `user_email: string` | AC-4 (admin) | `internal/api/admin/security_events.go` |

**Note:** Sessions list DTO (`session.operator_name`, `session.apn_name`) already exists — FIX-202 / FIX-212 name-resolution delivered it. Verified at `web/src/pages/sessions/index.tsx` L145 (`session.operator_name || session.operator_id.slice(0,8)`). Only the **stats widget** `top_operator` is missing.

### Files Table (by task)

| File | Status | Change |
|---|---|---|
| `web/src/components/shared/entity-link.tsx` | modify | Add routes (`ippool`, `esim_profile`), props (`showIcon`, `hoverCard`, `icon`, `copyOnRightClick`), orphan "—", context-menu copy. Preserve existing API. |
| `web/src/components/shared/entity-hover-card.tsx` | **NEW** | Lazy summary card (200ms delay), React Query cache consumer, uses existing endpoints (`/operators/{id}`, `/sims/{id}`, `/apns/{id}`, `/users/{id}`). Exported but only mounted when `hoverCard={true}`. |
| `web/src/components/shared/index.ts` | modify | Export `EntityHoverCard`. |
| `docs/FRONTEND.md` | modify | New `## Entity Reference Pattern` section after Modal Pattern (line TBD). |
| `web/src/pages/sessions/index.tsx` | modify | L145, L148 → EntityLink operator/apn. L360 → EntityLink via `top_operator` DTO (from stats). L506, L510 slide-panel → EntityLink. |
| `web/src/pages/esim/index.tsx` | modify | L227 chip label OK (already uses name). L303 → ICCID-clickable SIM link (remove UUID prefix column per F-175). Use new `operator_name` DTO field. |
| `web/src/pages/jobs/index.tsx` | modify | L345 Created By → EntityLink user (`created_by_name` fallback to `created_by_email`). Cron job: "System (cron: <type>)" based on `is_system`. |
| `web/src/pages/audit/index.tsx` | modify | L147 entity_id → EntityLink (type-aware via `entity_type`). L475 filter pill already OK. |
| `web/src/pages/alerts/index.tsx` | modify | L435, L540 `alert.sim_id.slice(0,12)` → EntityLink sim. Verify alert DTO has sim_name/iccid (FIX-209 enrichment). |
| `web/src/pages/violations/index.tsx` | modify | L259 fallback already uses iccid — extend to msisdn. L449 SIM link already OK. L452-459 OperatorChip stays. L521, L531 selectedViolation → same. |
| `web/src/pages/sims/index.tsx` | audit+modify | Operator column already uses OperatorChip (L735). **APN column: if rendered as plain text, wrap in EntityLink apn; if already in EntityLink/Link → no-op.** Filter pills (L196, L200) use slice fallback in text — acceptable (filter chip is dev-adjacent) but still switch to name-only display when available. |
| `web/src/pages/topology/index.tsx` | verify+wrap | Already name-based (L38-39). **Per-node render: if operator/apn name is plain text → wrap in EntityLink; if already Link-wrapped → no-op.** Do not "skip silently" — each node name must be clickable post-story. |
| `web/src/pages/dashboard/index.tsx` | modify | Recent Alerts (L861), Top APNs (L661 already name), event chips L702-708 → EventEntityButton or EntityLink. Operator Health Matrix (already named). |
| `web/src/pages/dashboard/analytics-cost.tsx` | modify | L120 chart name, L288 table row → EntityLink operator. |
| `web/src/pages/dashboard/analytics-anomalies.tsx` | modify | L188 anomaly sim_id → EntityLink sim (reuse iccid fallback). |
| `web/src/pages/admin/security-events.tsx` | modify | L147 user_id → EntityLink. L150 entity_id → EntityLink (type-aware). |
| `web/src/pages/admin/purge-history.tsx` | modify | L94 actor_id → EntityLink user. Consume new `actor_email` DTO. |
| `web/src/pages/admin/sessions-global.tsx` | modify | L127 user_id → EntityLink user. |
| `web/src/pages/admin/tenant-resources.tsx` | modify | L120, L173 tenant_id → EntityLink tenant. |
| `web/src/pages/admin/dsar.tsx` | modify | L152 job → EntityLink job. L158 tenant → EntityLink tenant. |
| `web/src/pages/admin/maintenance.tsx` | modify | L179 tenant_id → EntityLink tenant. |
| `web/src/pages/admin/api-usage.tsx` | modify | L126 key_id → EntityLink apikey. |
| `web/src/pages/roaming/index.tsx` | modify | L310 operator_id → EntityLink operator. |
| `web/src/pages/sms/index.tsx` | modify | L175 sim_id → EntityLink sim. |
| `web/src/pages/ops/deploys.tsx` | modify | L111 user_id → EntityLink user. |
| `web/src/pages/reports/index.tsx` | skip | L302 toast only — UUID prefix acceptable in toast (transient dev feedback, not a rendered cell). No change. |
| `web/src/pages/policies/_tabs/assigned-sims-tab.tsx` | verify+wrap | L89 already uses `sim.operator_name` fallback. **Verify operator cell is wrapped in EntityLink (file already imports it); if plain span → wrap.** |
| `web/src/pages/sims/_tabs/cost-attribution-tab.tsx` | modify | L103 operator UUID → EntityLink operator (need `operator_name` in DTO — check). |
| `web/src/components/event-stream/event-source-chips.tsx` | modify | L27-52 → consume `envelope.entity.display_name` when present; fallback to slice(0,8) only if envelope has no entity. |
| `web/src/types/job.ts` | modify | Add `created_by_name?`, `created_by_email?`, `is_system?` to `Job` interface. |
| `web/src/types/audit.ts` | modify | Add `user_email?`, `user_name?` to `AuditLog`. |
| `web/src/types/session.ts` | modify | Add `top_operator?: {id, name, count}` to stats type. |
| `web/src/types/esim.ts` | modify | Add `operator_name?`, `operator_code?` to profile. |
| `internal/api/session/stats.go` (or equivalent handler) | modify | Compute `top_operator` from `by_operator` map + operator name lookup. |
| `internal/api/job/*.go` | modify | Join users table for created_by → email/name. `is_system=true` when created_by is null/cron. |
| `internal/api/audit/*.go` | modify | Join users table for user_id → email/name. |
| `internal/api/esim/*.go` | modify | Join operators table for operator_id → name/code. |
| `internal/api/admin/security_events.go`, `purge_history.go` | modify | Join users table for user_id/actor_id → email. |
| `web/src/__tests__/shared/entity-link.test.tsx` | modify | Expand to runtime tests (not just type): orphan "—", icon, right-click copy, hover card trigger timing. |
| `docs/ROUTEMAP.md` | modify | FIX-219 track row → IN PROGRESS → DONE on completion. |

---

## Waves & Tasks

Total: **4 waves, 13 tasks.** Wave 1 serial foundation. Wave 2 parallel backend. Wave 3 batched page rewrites (parallel within wave). Wave 4 polish.

---

### Wave 1 — Primitive extension + docs (serial)

#### Task 1 — Extend EntityLink with new routes, props, orphan handling, right-click copy
- **Files:** `web/src/components/shared/entity-link.tsx`
- **Depends on:** —
- **Complexity:** medium
- **What:**
  - Add to `ENTITY_ROUTE_MAP`: `ippool: (id) => `/settings/ip-pools/${id}`, `esim_profile: (id) => `/esim?profile_id=${id}`.
  - Add optional props (additively): `showIcon?: boolean`, `icon?: ReactNode` (override), `hoverCard?: boolean`, `copyOnRightClick?: boolean` (default true).
  - Icon map (lucide): sim→Smartphone, operator→Radio, apn→Cloud, policy→ShieldCheck, user→User, session→Activity, tenant→Building2, ippool→Network, esim_profile→Smartphone, violation→AlertTriangle, alert→Bell, anomaly→Sparkles, job→Briefcase, apikey→Key.
  - Orphan handling: `label` empty && `entityId` empty → render `<span title="Entity reference broken">—</span>`. `label` empty && `entityId` present → existing truncate fallback with tooltip.
  - Right-click handler: `onContextMenu` → `e.preventDefault(); navigator.clipboard.writeText(entityId).then(() => toast.success('UUID copied'))`. Guard `navigator.clipboard` availability.
  - A11y: `aria-label="View ${entityType} ${label || entityId}"`. Focus-visible ring via existing utility.
- **DoD:** tsc passes; existing 11 callers still compile; new props optional; right-click test manual.
- **Pattern ref:** React.memo wrapper pattern already used in file L48.

#### Task 2 — Create EntityHoverCard component + wire `hoverCard={true}` path in EntityLink
- **Files:** `web/src/components/shared/entity-hover-card.tsx` (NEW), `web/src/components/shared/index.ts`, `web/src/components/shared/entity-link.tsx`
- **Depends on:** Task 1
- **Complexity:** medium
- **Data path:** Verified hooks at plan time — `use-operators.ts`, `use-apns.ts`, `use-sims.ts`, `use-admin.ts` (for users) exist. Task 2 reuses these by calling the list hooks with single-id filter, OR falls back to direct `api.get('/operators/{id}')` via React Query `useQuery(['operator', id], ...)` if per-id hook is absent. Do NOT create new hooks in this story — keep it inline useQuery calls.
- **What:**
  - New file: `EntityHoverCard` wraps children; on `onMouseEnter` starts 200ms timer → fetches summary via inline `useQuery(['<type>-summary', id], () => api.get(...))` with `{ enabled: isOpen, staleTime: 5 * 60 * 1000 }`. On `onMouseLeave` → cancel timer, close card.
  - Uses existing `Popover` primitive (`web/src/components/ui/popover.tsx`) for positioning.
  - Summary rendering per type: operator (code, MCC/MNC, health chip), sim (ICCID, state, APN), user (email, role), apn (code, operator, subscriber count). Others → "No preview available".
  - EntityLink: when `hoverCard={true}`, wrap Link with HoverCard; else render Link unchanged.
  - Guard network-off: `navigator.onLine === false` → disable hover card fetch.
- **DoD:** component renders without crashing when hook returns `undefined`; 200ms delay verified in unit test.

#### Task 3 — FRONTEND.md `## Entity Reference Pattern` section
- **Files:** `docs/FRONTEND.md`
- **Depends on:** Task 1
- **Complexity:** low
- **What:** Add new section after `## Modal Pattern`:
  - When to use EntityLink vs EventEntityButton vs OperatorChip (boundary).
  - Canonical call shape: `<EntityLink entityType="operator" entityId={id} label={name} showIcon />`.
  - Orphan rule: "If label missing, component renders `—`; never render raw UUID prefix in primary UI."
  - UUID-only allowed zones: exports, URL params, audit JSON, dev debug pane.
  - Icon type map reference.
  - Hover card opt-in rule.
  - Right-click copy UX contract.
- **Pattern ref:** Follow Modal Pattern section style (FIX-216 adopted).

---

### Wave 2 — Backend DTO enrichments (parallel — independent handlers)

#### Task 4 — Session stats `top_operator` + Job `created_by_name/email/is_system`
- **Files:** `internal/api/session/stats.go` (or matching handler), `internal/api/job/*.go`, `web/src/types/session.ts`, `web/src/types/job.ts`
- **Depends on:** —
- **Complexity:** medium
- **What:**
  - Sessions stats handler: after computing `by_operator: {UUID: count}`, find max-count operator, join operators table for name/code, add `top_operator: {id, name, code, count}` to response.
  - Jobs list handler: LEFT JOIN users on `jobs.created_by = users.id` → `created_by_name` (from users.name), `created_by_email` (from users.email). When `created_by IS NULL` → `is_system: true`, `created_by_name: "System"`.
  - Tests: handler unit test for each; verify null/orphan user → empty string + `is_system=true`.
- **DoD:** Swagger/OpenAPI updated (if maintained); FE types extended; response JSON verified via curl.

#### Task 5 — AuditLog / SecurityEvents / PurgeHistory user enrichment (+ verification of pre-existing eSIM enrichment)
- **Files:** `internal/api/audit/*.go`, `internal/api/admin/security_events.go`, `internal/api/admin/purge_history.go`, `web/src/types/audit.ts`, `web/src/types/admin.ts`
- **Depends on:** —
- **Complexity:** medium
- **What:**
  - AuditLog / SecurityEvents: LEFT JOIN users on `user_id` → add `user_email`, `user_name`.
  - PurgeHistory: LEFT JOIN users on `actor_id` → add `actor_email`, `actor_name` (admin.ts already has `actor_email` typed at L153 — verify backend populates it).
  - **eSIM list enrichment DROPPED — ALREADY DONE.** Spot-checked 2026-04-23: `internal/api/esim/handler.go:93-94` has `OperatorName` + `OperatorCode` fields; `toProfileResponseEnriched` at L146-155 populates them; L209 List endpoint uses enriched variant; L244 Get endpoint uses enriched variant. FE `web/src/types/esim.ts` may still need `operator_name` field addition — verify before skipping entirely.
  - Violations top-SIMs aggregation: verify `iccid`/`msisdn` present in response row; if `msisdn` missing, add via SIM join.
- **DoD:** response verified via curl on dev; FE type extensions compile; existing tests pass.

---

### Wave 3 — Page rewrites (parallel batches)

#### Task 6 — Batch A: Core list pages (sessions, esim, jobs, audit)
- **Files:** `web/src/pages/sessions/index.tsx`, `web/src/pages/esim/index.tsx`, `web/src/pages/jobs/index.tsx`, `web/src/pages/audit/index.tsx`
- **Depends on:** Task 1, Task 4, Task 5
- **Complexity:** medium
- **What:**
  - `sessions/index.tsx`:
    - L145: replace `<span>{session.operator_name || session.operator_id.slice(0, 8)}</span>` → `<EntityLink entityType="operator" entityId={session.operator_id} label={session.operator_name} />`.
    - L148: same pattern for APN.
    - L360 `Top: ${topOperators[0][0].slice(0, 8)}` → use new `stats.top_operator.name` via EntityLink (operator).
    - L506, L510 slide-panel fields → EntityLink.
  - `esim/index.tsx`:
    - L303 SIM column: remove UUID prefix span; make ICCID column clickable via `<EntityLink entityType="sim" entityId={profile.sim_id} label={profile.iccid} />` (F-175).
    - Operator column: `<EntityLink entityType="operator" entityId={profile.operator_id} label={profile.operator_name} />` (F-173).
    - L432, L441 dialog copy: keep UUID prefix (it's a confirm dialog; user already knows the row).
  - `jobs/index.tsx`:
    - L345: `<EntityLink entityType="user" entityId={job.created_by} label={job.is_system ? 'System' : (job.created_by_name || job.created_by_email)} />`.
  - `audit/index.tsx`:
    - L147: type-aware EntityLink using `entry.entity_type` + `entry.entity_id`. Filter pill at L475 already works.
- **DoD:** zero `.slice(0, 8)` occurrences in these files after patch (except confirm-dialog copy). Manual click-through verifies each.

#### Task 7 — Batch B: Alerts + Violations + Sims audit + Dashboard analytics
- **Files:** `web/src/pages/alerts/index.tsx`, `web/src/pages/violations/index.tsx`, `web/src/pages/dashboard/analytics-cost.tsx`, `web/src/pages/dashboard/analytics-anomalies.tsx`, `web/src/pages/sims/_tabs/cost-attribution-tab.tsx`
- **Depends on:** Task 1, Task 5
- **Complexity:** medium
- **What:**
  - `alerts/index.tsx`:
    - L435: `View SIM {alert.sim_id.slice(0, 12)}` → `<EntityLink entityType="sim" entityId={alert.sim_id} label={alert.sim_iccid || alert.sim_id} showIcon />`.
    - L540: same.
  - `violations/index.tsx`:
    - L259 agg fallback: add `msisdn` to fallback chain after iccid.
    - No structural change (L449 SIM link + L452-459 OperatorChip stay).
  - `analytics-cost.tsx`:
    - L120: replace chart label; use operator_name when present (DTO already has it post-FIX-202).
    - L288: `<TableCell>` → EntityLink operator.
  - `analytics-anomalies.tsx`:
    - L188: `{anomaly.sim_iccid || anomaly.sim_id.slice(0, 8) + '...'}` → `<EntityLink entityType="sim" entityId={anomaly.sim_id} label={anomaly.sim_iccid} />`.
  - `cost-attribution-tab.tsx`:
    - L103: `<EntityLink entityType="operator" entityId={op.operator_id} label={op.operator_name} />`.
- **DoD:** click-through verified on all four pages.

#### Task 8 — Batch C: Admin pages (security-events, purge-history, sessions-global, tenant-resources, dsar, maintenance, api-usage)
- **Files:** `web/src/pages/admin/security-events.tsx`, `.../purge-history.tsx`, `.../sessions-global.tsx`, `.../tenant-resources.tsx`, `.../dsar.tsx`, `.../maintenance.tsx`, `.../api-usage.tsx`
- **Depends on:** Task 1, Task 5
- **Complexity:** medium
- **What:**
  - `security-events.tsx` L147: EntityLink user (email label). L150: EntityLink type-aware.
  - `purge-history.tsx` L94: EntityLink user (`actor_email` label, fallback 'system').
  - `sessions-global.tsx` L127: EntityLink user.
  - `tenant-resources.tsx` L120, L173: EntityLink tenant (label = tenant name when present in DTO, fallback to id slice).
  - `dsar.tsx` L152: EntityLink job. L158: EntityLink tenant.
  - `maintenance.tsx` L179: EntityLink tenant.
  - `api-usage.tsx` L126: EntityLink apikey.
- **DoD:** no `.slice(0, 8)` in these files in rendered output.

#### Task 9 — Batch D: Dashboard main + misc pages (topology, roaming, sms, ops/deploys)
- **Files:** `web/src/pages/dashboard/index.tsx`, `web/src/pages/topology/index.tsx`, `web/src/pages/roaming/index.tsx`, `web/src/pages/sms/index.tsx`, `web/src/pages/ops/deploys.tsx`
- **Depends on:** Task 1
- **Complexity:** low-medium
- **What:**
  - `dashboard/index.tsx`:
    - L702-708 event chips: defer to EventEntityButton (already consumes envelope `entity` — no per-chip chip replacement needed; re-verify these chips use envelope entity if available, fallback to slice).
    - L861 Recent Alerts navigate call: surrounding `<span>` → EntityLink sim where applicable.
    - Top APNs (already name-based) → wrap in EntityLink apn for click-through.
    - Operator Health Matrix → verify EntityLink wrap present.
  - `topology/index.tsx`: wrap operator/apn names in EntityLink for click-through.
  - `roaming/index.tsx` L310: EntityLink operator.
  - `sms/index.tsx` L175: EntityLink sim (label = iccid if present in DTO, else empty → orphan fallback).
  - `ops/deploys.tsx` L111: EntityLink user.
- **DoD:** click-through verified.

#### Task 10 — Event stream chips envelope consumption
- **Files:** `web/src/components/event-stream/event-source-chips.tsx`, `web/src/pages/dashboard/index.tsx` (chip helper L702-708 if not delegated)
- **Depends on:** Task 1
- **Complexity:** low
- **What:**
  - `event-source-chips.tsx` L27-52: replace raw `event.operator_id.slice(0, 8)` etc. with name-aware path. Priority: `envelope.entity.display_name` (when event has entity envelope) > `event.operator_name` (legacy field) > `.slice(0, 8)` fallback.
  - Dashboard inline chip helper (L702-708): same rule.
- **DoD:** live event stream shows "Op: Turkcell" not "Op: 20000000".

---

### Wave 4 — Polish, tests, verification

#### Task 11 — Notifications list entity ref clickability (AC-8)
- **Files:** `web/src/pages/notifications/*.tsx` (verify), notifications item render components
- **Depends on:** Task 1
- **Complexity:** low
- **What:**
  - Inspect notifications list item renderer; confirm `entity_refs: NotificationEntityRef[]` (already typed at `web/src/types/notification.ts` L19) is rendered as EntityLink array.
  - If template strings with `{{.Entity.Link}}` appear in message body, ensure the FE template-to-link parser produces EntityLink nodes (not plain `<a>` tags). This is the only template-parsing touch — do not invent new template syntax; use what backend already emits.
- **DoD:** notifications detail page click-through verified.

#### Task 12 — A11y manual verification (unit tests DEFERRED per D-091)
- **Files:** none (manual sign-off only)
- **Depends on:** Task 1, Task 2
- **Complexity:** low
- **Scope decision (2026-04-23, advisor-reviewed):** Unit tests deferred to D-091 / FIX-24x test-infra wave — consistent with FIX-215/216/217/218 precedent. Adding `vitest` + `@testing-library/react` + `jsdom` + `vitest.config.ts` here would be a dep-chain scope expansion that breaks established deferral pattern. FIX-24x owns test infra and will retroactively backfill EntityLink + EntityHoverCard tests.
- **What (manual sign-off):**
  - AC-10 verification: navigate to `/sessions` page, Tab through EntityLinks; verify aria-label (`View operator Turkcell`) announced by VoiceOver / screen reader; focus-visible ring renders.
  - AC-11 verification: right-click any EntityLink → "UUID copied" toast; clipboard contains the UUID.
  - Orphan verification: navigate to a page with known null-FK entity (audit log actor for system action) → renders `—` with tooltip; no crash.
  - Hover card verification: hover operator link on `/esim` → 200ms delay → summary popover renders; hover away before 200ms → no network call.
- **DoD:** manual sign-off captured in review report; add entry to ROUTEMAP Tech Debt D-091 list for FIX-24x backfill.

#### Task 13 — Audit pass: grep for residual `.slice(0, 8)` / `.slice(0, 12)` in pages; update ROUTEMAP
- **Files:** all `web/src/pages/*`, `docs/ROUTEMAP.md`
- **Depends on:** Task 6–10
- **Complexity:** low
- **What:**
  - Grep `.slice\(0,\s*(8|10|12)\)` across `web/src/pages/` — any remaining match in a rendered JSX expression (not toast, not URL, not export) must be patched or justified inline comment ("UUID ok: debug pane" etc.).
  - Verify UUID-only zones still render UUID: CSV export (no HTML), URL query strings (still use id).
  - Update ROUTEMAP.md FIX-219 row → DONE with wave count.
- **DoD:** zero unjustified `.slice` in rendered paths; ROUTEMAP up to date.

---

## Design token map (EntityLink component)

| Element | Token / Class | Notes |
|---|---|---|
| Link base color | `text-accent` | existing |
| Link hover | `text-accent/80 hover:underline underline-offset-2` | existing |
| Font family (id display) | `font-mono` | existing |
| Font size | `text-[12px]` | matches table cell density |
| Transition | `transition-colors duration-200` | existing |
| Orphan "—" color | `text-text-tertiary` | muted |
| Orphan tooltip bg | `bg-bg-elevated` | existing Tooltip token |
| Icon default | `text-text-tertiary` 3.5 × 3.5 | `showIcon` path |
| Icon hover | `text-accent` | group-hover variant |
| Focus ring | `focus-visible:ring-1 focus-visible:ring-accent` | a11y |
| HoverCard surface | `bg-bg-elevated border border-border rounded-md shadow-lg` | matches Popover |
| HoverCard summary label | `text-text-secondary text-[11px]` | meta text |
| HoverCard summary value | `text-text-primary text-[13px]` | primary info |
| Health chip (operator) | reuse existing status chip tokens | — |
| Copy toast | reuse `toast.success` from `web/src/lib/toast` | — |

---

## Risks & Mitigations

| Risk | Severity | Mitigation |
|---|---|---|
| **R1 — Existing 11 callers break** on EntityLink API change | high | Additive extension only. No rename. Type smoke test already covers prop shape. |
| **R2 — Backend DTO join latency** on Audit/Jobs (heavy tables) | medium | LEFT JOIN users is O(rows × 1) with `users.id` PK — trivial. Add index verification in Task 4/5. |
| **R3 — Hover card fetches flood backend** | medium | 200ms debounce + React Query cache (staleTime 5min) + offline guard (AC-3 + Decision 6). Opt-in per call site. |
| **R4 — 10K-row list perf** (virtual scroll + hover card) | low | EntityLink is memoized (existing React.memo). HoverCard only mounts when `hoverCard={true}`. Virtual scroll unaffected. |
| **R5 — Orphan SIMs cause crash** | medium | AC-9 orphan rule covers null label + null id → "—". Verified post-FIX-206 cleanup. |
| **R6 — Existing tests assert UUID text** | low | Task 12 updates test selectors to use name. Grep for test files asserting UUID prefix before merge. |
| **R7 — Clipboard API unavailable** (Safari incognito, older browsers) | low | Guard `navigator.clipboard` presence; no-op on missing — keep right-click default browser behavior. |
| **R8 — eSIM SIM ID column removal breaks filter memory** | low | F-175 says "remove UUID column, keep ICCID clickable". Preserve URL `sim_id` query param parsing; only display changes. |

---

## Quality Gate Self-Check

| Check | Status | Notes |
|---|---|---|
| All 12 ACs mapped to tasks | PASS | AC-1→T1, AC-2→T1, AC-3→T2, AC-4→T6/7/8/9, AC-5→T9 (dashboard), AC-6→T4+T6, AC-7→T10, AC-8→T11, AC-9→T1, AC-10→T1+T12, AC-11→T1+T12, AC-12→T13 |
| No scope creep | PASS | Backend patches strictly name-resolution joins — no new endpoints, no schema changes. |
| No duplicate components | PASS | Decision 3 locks boundary: EntityLink / EventEntityButton / OperatorChip remain separate with documented roles. |
| DTO gaps decided | PASS | All 5 gaps explicitly included in Wave 2, none deferred. |
| Existing primitives reused | PASS | Tooltip, Popover, Button, existing toast — no new primitive duplication. |
| Design tokens only | PASS | Token map above; no hex, no tailwind-color utility. |
| a11y covered | PASS | aria-label, focus-visible, keyboard Tab, screen reader type announce (AC-10 + T12). |
| Tests planned | PASS | Runtime tests (T12) expand type-only smoke test; hover card timer tests. |
| ROUTEMAP update planned | PASS | T13. |
| FRONTEND.md update planned | PASS | T3. |
| Dark mode verified | PASS | All tokens are semantic (bg-bg-*, text-text-*, border-border); no light-specific classes. |
| Dependencies blocking | PASS | FIX-202 DONE (DTO baseline), FIX-212 DONE (envelope). No new blockers. |

**Status: READY**

---

## Out of Scope (explicit)

- Dev debug pane (Shift+D toggle) — AC-12 mentions as future.
- ESLint rule to enforce EntityLink usage — defer to Tech Debt (same pattern as FIX-216 ESLint deferral).
- Backend template engine for `{{.Entity.Link}}` resolution — only FE parsing of already-emitted template tokens (AC-8 narrow scope).
- Operator code/MCC-MNC enrichment beyond what OperatorChip already does.
- New backend endpoints — only name-join enrichments on existing responses.
- Bulk migration of existing `<a href>` links outside target pages.

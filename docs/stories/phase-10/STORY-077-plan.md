# Implementation Plan: STORY-077 — Enterprise UX Polish & Ergonomics

## Goal

Close the enterprise-UX maturity gap (saved views, undo, inline edit, CSV export everywhere, empty-state CTAs, data freshness, sticky headers, field-level form validation, admin impersonation, announcements, i18n TR/EN, table density, chart export/annotation, optimistic updates with progress, row-click clarity, extended comparison views) and simultaneously retire the four remaining Phase 10 tech-debt items routed to this story (D-006 GeoIP, D-007 APN policy cross-reference tab, D-008 search DTO enrichment, D-009 list-page row data attributes).

## Architecture Context

### Components Involved

- `internal/api/admin` (SVC-01 gateway): new handlers for impersonation, announcements CRUD, active session GeoIP enrichment (D-006)
- `internal/api/user`: saved views CRUD, user preferences (locale, table density, column prefs)
- `internal/api/{sim,apn,operator,policy,session,job,audit,cdr,notification,violation,alert,anomaly,apikey,user}`: CSV export sub-route + undo action registration
- `internal/api/search`: enriched per-type DTO response (D-008)
- `internal/api/system` / new `internal/api/announcements`: announcement CRUD
- `internal/api/policy`: delete rollback flow for undo; APN cross-reference list (D-007)
- `internal/store/policy.go`: new method `ListPoliciesReferencingAPN(ctx, tenantID, apnName)` using `dsl_compiled` GIN trigram search (D-007)
- `internal/store/session_radius.go`: add `Location` field populated via GeoIP lookup (D-006)
- `internal/geoip` (NEW): MaxMind GeoLite2 reader wrapper (lazy-init singleton, no-op when DB absent)
- `internal/undo` (NEW): Redis-backed undo registry (15s TTL, inverse-op JSON payload)
- `internal/export` (NEW): streaming CSV writer helper (shared across resource handlers)
- `internal/middleware`: `ImpersonationContext` middleware (flags read-only when impersonating); audit `impersonated_by` propagation
- `web/src/locales/{en,tr}.json` (NEW): all UI strings
- `web/src/lib/i18n.ts` (NEW): react-i18next init
- `web/src/hooks/*` NEW: `use-saved-views.ts`, `use-undo.ts`, `use-form-validation.ts`, `use-data-freshness.ts`, `use-column-preferences.ts`, `use-announcements.ts`, `use-impersonation.ts`, `use-optimistic.ts`, `use-export.ts`, `use-chart-export.ts`
- `web/src/components/shared/*` NEW: `editable-field.tsx`, `undo-toast.tsx`, `saved-views-menu.tsx`, `empty-state.tsx`, `data-freshness.tsx`, `column-customizer.tsx`, `form-field.tsx`, `impersonation-banner.tsx`, `announcement-banner.tsx`, `progress-toast.tsx`, `unsaved-changes-prompt.tsx`, `first-run-checklist.tsx`, `compare-view.tsx` (policy+operator variants)
- `web/src/stores/ui.ts`: extend with `columnPreferences`, `savedViews` (already has `tableDensity`, `locale`)
- `web/src/pages/admin/announcements.tsx`, `web/src/pages/admin/impersonate-list.tsx` (NEW admin screens)
- `migrations/` — new tables: `user_views`, `announcements`, `announcement_dismissals`, `chart_annotations`; column: `users.locale`, `user_column_preferences` table

### Data Flow

**Saved Views (AC-1)**: User clicks "Save View" on list page → dialog captures name + auto-save toggle → `POST /api/v1/user/views` stores `{page, name, filters_json, columns_json, sort_json, is_default, shared}` → sidebar "My Views" (React hook `useSavedViews(page)` fetches `GET /api/v1/user/views?page=sims`) → click view → filters restored via URLSearchParams + column/sort via store → `PUT /api/v1/user/views/:id/default` sets default (unique per user+page via partial unique index).

**Undo (AC-2)**: Destructive action → backend registers inverse op in Redis (`undo:{tenantID}:{actionID}` = JSON `{action, payload, user_id, issued_at}`, TTL 15s) → response includes `action_id` → frontend shows `<UndoToast>` with 10s countdown → click Undo → `POST /api/v1/undo/:action_id` → handler pops Redis entry, dispatches inverse (restore state) → 200 if succeeded, 410 Gone if TTL expired. Actions covered: bulk suspend/terminate/activate (SIM), delete policy, delete segment, revoke API key. Each action's `RegisterUndo` helper writes to Redis inline within the main handler before responding.

**Inline Edit (AC-3)**: `<EditableField value onSave field>` → hover shows pencil → click switches to `contentEditable` → Enter/blur calls `onSave(newValue)` → optimistic UI swap → PATCH resource → rollback via React Query cache on error → Esc cancels. Wired to: `sim.label`, `sim.notes`, `policy.name`, `operator.display_name`, `apn.description`, `notification_config.name`, `segment.name`.

**CSV Export (AC-4)**: Button in `TableToolbar` (already present — wire `onExport` to new `useExport(resource)` hook) → `GET /api/v1/{resource}/export?format=csv&{filters}` with `Accept: text/csv` → backend streams via `csv.NewWriter(w)` chunked by cursor pages → `Content-Disposition: attachment; filename="{resource}_{filters-slug}_{YYYY-MM-DD}.csv"` → browser download with a progress toast (Content-Length if known, otherwise indeterminate). Resources: sims, apns, operators, policies, sessions, jobs, audit, cdrs, notifications, violations, alerts, anomalies, users, api_keys.

**Empty-State (AC-5)**: Each list page wraps zero-result branch in `<EmptyState icon title description ctaLabel ctaHref>` (per-page copy lookup via i18n key `emptyStates.{resource}`). Dashboard first-run checklist component fetches onboarding completion status (reuse `GET /api/v1/onboarding/status`) and renders 4 steps with links.

**Data Freshness (AC-6)**: `<DataFreshness source="ws"|"poll" lastUpdated refetch autoRefresh setAutoRefresh>` sits in page footer. For WS-fed pages: pulls `connected` from `useEventStore` — shows green "Live" when connected, yellow "Offline" otherwise. For polled pages: `lastUpdated` (Date) + "Xs ago" via `date-fns/formatDistanceStrict` + refresh button + auto-refresh selector (15s/30s/1m/off), persists selection per-page in localStorage; turns yellow after 5m stale.

**Sticky Tables + Columns (AC-7)**: Apply CSS `position: sticky; top: 0; z-index: 2` to `<TableHeader>` globally (one edit to `components/ui/table.tsx` adding a `sticky` prop default-true). Sticky first column via class utility. `<ColumnCustomizer columns onChange>` panel: checkbox list + drag-to-reorder (using `@dnd-kit/core` or native HTML5 DnD — already likely in deps; confirm and fall back to simple up/down arrows). Prefs persisted via new table `user_column_preferences (user_id, page_key, columns_json)` or localStorage (store explicitly decides: server-persist for cross-device).

**Form Validation (AC-8)**: `useFormValidation<T>(schema)` → returns `{ values, errors, touched, handleChange, handleBlur, isValid, isDirty, reset }`. Debounced (300ms) field-level validation via user-supplied rules (`{required, minLength, maxLength, pattern, custom}`). `<FormField label required error>` wrapper renders label with red asterisk + inline error below input. Dirty navigation guard via `useBlocker` (react-router v6) showing `<UnsavedChangesPrompt>` dialog.

**Impersonation (AC-9)**: Super admin → `POST /api/v1/admin/impersonate/:user_id` → issues new JWT with claim `{sub: target_user_id, act: {sub: original_admin_id}, impersonated: true}` and 1h expiry → frontend stores JWT, adds `X-Impersonated-By` header from original user, shows purple `<ImpersonationBanner>` → all mutating middleware (`method != GET`) returns 403 when `ctx.Value(ImpersonatedKey) == true` → audit entries include `impersonated_by` column. Exit: `POST /api/v1/admin/impersonate/exit` → restores original JWT stored server-side for the session.

**Announcements (AC-10)**: Table `announcements` + dismissal table. `GET /announcements/active` filters by `now BETWEEN starts_at AND ends_at AND (target='all' OR target=tenant_id) AND id NOT IN (SELECT announcement_id FROM announcement_dismissals WHERE user_id = :uid)`. `<AnnouncementBanner>` renders below topbar with color per `type`. Dismissible → `POST /announcements/:id/dismiss` writes row; client also mirrors in localStorage for instant hide. Admin CRUD at `/admin/announcements` (super admin + tenant admin for tenant-scoped).

**i18n (AC-11)**: `react-i18next` with backend loader for lazy-split namespaces. Key files `web/src/locales/{en,tr}/{common,forms,errors,emptyStates,announcements,...}.json`. `Intl.DateTimeFormat(locale)` and `Intl.NumberFormat(locale)` wrappers in `lib/format.ts`. Language toggle in topbar + settings page writes `useUIStore.setLocale(x)` and `PATCH /api/v1/user/preferences {locale: 'tr'}` to persist in `users.locale`.

**Density (AC-12)**: Already present in `useUIStore.tableDensity`. Remaining: set CSS variable `--table-row-height` on `<body>` (via a `useEffect` in `app.tsx`) + ensure all table row heights use `var(--table-row-height)` via a single Tailwind plugin rule or CSS variable in `index.css`.

**Chart Export + Annotation (AC-13)**: `useChartExport(chartRef)` → uses `html-to-image` (or `html2canvas`) to PNG. "Compare to previous period" → timeframe selector emits two ranges; chart consumer overlays second series with dashed stroke. Annotations: click handler on chart (recharts `onClick` → computes nearest x) → opens `<AnnotationDialog>` → `POST /api/v1/chart_annotations {chart_key, timestamp, label}` → listed annotations drawn as `<ReferenceLine>` with `<Label>`.

**Optimistic Updates + Progress (AC-14)**: Create/edit mutations wrapped via React Query `onMutate` → cache snapshot → optimistic merge → rollback in `onError`. Bulk/long ops emit WS events `jobs.progress {job_id, percent, eta_sec}` → `<ProgressToast jobId>` subscribes via `useEventStore` and renders progress bar + ETA; final `jobs.completed` replaces with success content.

**Row Click Behavior (AC-15)**: Shared `<DataRow onNavigate onToggleSelect onContextMenu onDoubleClick data-row-index data-href data-row-active>` adopted by every list page. Single-click → onNavigate; checkbox swallowed click; right-click → context menu; dbl-click → inline edit. These data attributes also satisfy D-009.

**Comparison Views (AC-16)**: List pages with `compare` capability (policies, operators) detect 2–3 selected rows → show "Compare" button → `<CompareView entities={ids} kind="policy"|"operator">` renders grid with per-field diff highlighting. Policy: DSL diff using `diff` lib, assignment-count fetched via existing counts endpoint. Operator: capabilities/health/cost/SIM-count per-column.

**D-006 GeoIP**: Add Go module `github.com/oschwald/geoip2-golang`; lazy-load MaxMind GeoLite2-City DB from `GEOIP_DB_PATH` (env). `geoip.Lookup(ip) (*LocationInfo, error)` returning `{country, city, lat, lon}`. Used in `ListActiveSessions` to populate `activeSessionItem.Location *LocationInfo` field. If DB missing or lookup fails → field null (non-fatal). Add Docker Compose volume mount `./geoip:/geoip:ro` with a doc noting GeoLite2 download process.

**D-007 APN Cross-Reference**: `PolicyStore.ListReferencingAPN(ctx, tenantID, apnName string, limit, offset int) ([]Policy, int, error)` performs `WHERE tenant_id=$1 AND dsl_compiled::text ILIKE '%'||$2||'%'` with a GIN trigram index (`CREATE INDEX idx_policy_versions_dsl_trgm ON policy_versions USING gin (dsl_compiled::text gin_trgm_ops)`). Handler: `GET /api/v1/apns/:id/referencing-policies?limit=20&cursor=...`. New tab `PoliciesReferencingTab` in `apns/detail.tsx`.

**D-008 Search Enrichment**: Extend `SearchResult` to per-type DTOs: `SIMResult{id,label,sub,state,operator_name}`, `APNResult{id,label,sub,mcc,operator_name}`, `OperatorResult{id,label,sub,mcc,health_status}`, `PolicyResult{id,label,sub,state}`, `UserResult{id,label,sub,role}`. Response shape becomes `{results: {sims:[...], apns:[...], ...}}` with discriminated types. Sub-joins wrapped in the existing errgroup to keep 500ms budget.

**D-009 Row Data Attributes**: Every list page's row element must emit `data-row-index={n}`, `data-href={detailPath}`, and `data-row-active={n === activeIndex}`. `use-keyboard-nav` hook (STORY-076) consumes these. Simple grep checklist across `pages/{sims,apns,operators,policies,sessions,jobs,audit,cdrs,notifications,violations,alerts,anomalies,users,api_keys,roaming,esim,segments}/index.tsx`.

### API Specifications

- `POST /api/v1/user/views` — create saved view. Body: `{page, name, filters_json, columns_json?, sort_json?, is_default?, shared?}`. 201 → `{status, data: {...view}}`.
- `GET /api/v1/user/views?page=sims` — list views for a page. 200 → `{status, data: [views]}`.
- `PATCH /api/v1/user/views/:id` — update. Body: partial. 200.
- `DELETE /api/v1/user/views/:id` — 204.
- `PUT /api/v1/user/views/:id/default` — mark default (clears siblings). 200.
- `POST /api/v1/undo/:action_id` — execute undo. 200 on success, 410 Gone if expired, 404 if not found. No body.
- `GET /api/v1/{resource}/export?format=csv&{filter params}` — streaming CSV. Content-Type `text/csv; charset=utf-8`. Content-Disposition with filename.
- `POST /api/v1/admin/impersonate/:user_id` — super_admin only. 200 → `{status, data: {jwt, user, tenant}}`. Audit entry.
- `POST /api/v1/admin/impersonate/exit` — 200 → `{status, data: {jwt}}`.
- `GET /api/v1/announcements/active` — returns active, not-dismissed. 200 → `{status, data: [...]}`.
- `POST /api/v1/announcements` (admin). Body: `{title, body, type, target, starts_at, ends_at, dismissible}`. 201.
- `GET /api/v1/announcements?page=1` — admin list. 200.
- `PATCH /api/v1/announcements/:id` — 200.
- `DELETE /api/v1/announcements/:id` — 204.
- `POST /api/v1/announcements/:id/dismiss` — 204.
- `GET /api/v1/apns/:id/referencing-policies?limit=20&cursor=` — D-007. 200 → `{status, data: [policies], meta: {next_cursor}}`.
- `PATCH /api/v1/user/preferences` — Body: `{locale?, columns?, auto_refresh?}`. 200.
- `POST /api/v1/chart_annotations` — Body: `{chart_key, timestamp, label}`. 201.
- `GET /api/v1/chart_annotations?chart_key=cdr-volume&from=&to=` — 200.
- `DELETE /api/v1/chart_annotations/:id` — 204.
- Search response (D-008) becomes: `{status, data: {sims:[{id,label,sub,state,operator_name}], apns:[...], operators:[{id,label,sub,mcc,health_status}], policies:[{id,label,sub,state}], users:[{id,label,sub,role}]}}`.

All responses use the standard envelope `{status, data, meta?, error?}`.

### Database Schema

Source: ARCHITECTURE.md (new tables — no prior migration).

```sql
-- 20260417000001_story_077_ux.up.sql
CREATE TABLE user_views (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  page          TEXT NOT NULL,
  name          TEXT NOT NULL,
  filters_json  JSONB NOT NULL DEFAULT '{}'::jsonb,
  columns_json  JSONB,
  sort_json     JSONB,
  is_default    BOOLEAN NOT NULL DEFAULT FALSE,
  shared        BOOLEAN NOT NULL DEFAULT FALSE,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_user_views_user_page ON user_views(user_id, page);
CREATE INDEX idx_user_views_tenant_shared ON user_views(tenant_id, page) WHERE shared = TRUE;
CREATE UNIQUE INDEX uniq_user_default_view ON user_views(user_id, page) WHERE is_default = TRUE;

CREATE TABLE announcements (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  title         TEXT NOT NULL,
  body          TEXT NOT NULL,
  type          TEXT NOT NULL CHECK (type IN ('info','warning','critical')),
  target        TEXT NOT NULL,        -- 'all' or tenant UUID string
  starts_at     TIMESTAMPTZ NOT NULL,
  ends_at       TIMESTAMPTZ NOT NULL,
  dismissible   BOOLEAN NOT NULL DEFAULT TRUE,
  created_by    UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_announcements_active ON announcements(starts_at, ends_at);

CREATE TABLE announcement_dismissals (
  announcement_id UUID NOT NULL REFERENCES announcements(id) ON DELETE CASCADE,
  user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  dismissed_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (announcement_id, user_id)
);

CREATE TABLE chart_annotations (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  user_id      UUID NOT NULL REFERENCES users(id) ON DELETE SET NULL,
  chart_key    TEXT NOT NULL,
  timestamp    TIMESTAMPTZ NOT NULL,
  label        TEXT NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_chart_annotations_key ON chart_annotations(tenant_id, chart_key, timestamp);

CREATE TABLE user_column_preferences (
  user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  page_key    TEXT NOT NULL,
  columns_json JSONB NOT NULL,
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (user_id, page_key)
);

ALTER TABLE users ADD COLUMN IF NOT EXISTS locale TEXT NOT NULL DEFAULT 'en'
  CHECK (locale IN ('en','tr'));
ALTER TABLE audit_events ADD COLUMN IF NOT EXISTS impersonated_by UUID NULL REFERENCES users(id);
```

```sql
-- 20260417000002_story_077_policy_dsl_trgm.up.sql (D-007)
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX IF NOT EXISTS idx_policy_versions_dsl_trgm
  ON policy_versions USING gin ((dsl_compiled::text) gin_trgm_ops);
```

RLS: extend existing RLS to the new tables (`user_views`, `announcements`, `announcement_dismissals`, `chart_annotations`, `user_column_preferences`) scoping by `tenant_id` / `user_id`.

### Screen Mockups

Layout additions (all overlays/companions to existing screens; no brand-new full pages except `/admin/announcements`):

```
┌─ Topbar ──────────────────────────────────────────────────────┐
│ [Purple] Viewing as alice@tenant.io — Acme Corp    [Exit]     │  (when impersonating)
├───────────────────────────────────────────────────────────────┤
│ [Blue] Maintenance Saturday 2am — details… [×]                │  (announcement)
├───────────────────────────────────────────────────────────────┤
│  Dashboard / SIMs / APNs / …                                  │
│                                                               │
│  [Save View] [Export ▼] [⚙ Columns]   [Density: ▣ Compact]    │
│  ─────────────────────────────────────────────────────────    │
│  ICCID           Label ✎       State       Operator    …      │  (sticky header)
│  89014103…       my-sim-01     Active      Vodafone    …      │
│                                                               │
├───────────────────────────────────────────────────────────────┤
│  Last updated 12s ago  [↻]   Auto: 30s ▼   ● Live              │  (freshness footer)
└───────────────────────────────────────────────────────────────┘
```

```
┌─ Undo Toast (bottom-right) ──────────┐
│ 10 SIMs suspended            [Undo] │
│ ⏱ 8s                                 │
└──────────────────────────────────────┘
```

```
┌─ Empty State ────────────────────────┐
│       🛰   No SIMs yet                │
│  Import your fleet to get started.   │
│  [Import your first SIMs]            │
└──────────────────────────────────────┘
```

```
┌─ Compare (policies) ──────────────────────────────────────┐
│  Field         │ Policy A         │ Policy B              │
│  Name          │ Throttle-BG      │ Throttle-HI           │
│  State         │ Active           │ Draft                 │
│  DSL           │ [diff view]                              │
│  Assignments   │ 1,240            │ 3                     │
└───────────────────────────────────────────────────────────┘
```

### Design Token Map (FRONTEND.md)

#### Color Tokens

| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Primary text | `text-text-primary` | `text-[#E4E4ED]`, `text-white` |
| Secondary text | `text-text-secondary` | `text-gray-500` |
| Tertiary / placeholder | `text-text-tertiary` | `text-gray-400` |
| Accent (CTA, link, live badge) | `text-accent` / `bg-accent` | `text-cyan-400` |
| Success (live, dismiss OK) | `text-success` / `bg-success-dim` | `text-green-500` |
| Warning (stale, announcement) | `text-warning` / `bg-warning-dim` | `text-yellow-500` |
| Danger (undo destructive tag) | `text-danger` / `bg-danger-dim` | `text-red-500` |
| Purple (impersonation banner) | `text-purple` / `bg-purple/10 border-purple` | `bg-violet-500` |
| Info (announcement info) | `text-info` / `bg-info/10` | `bg-blue-500` |
| Surface card | `bg-bg-surface` / `bg-bg-elevated` | `bg-white`, `bg-[#0C0C14]` |
| Border | `border-border` / `border-border-subtle` | `border-gray-200` |

#### Typography Tokens

| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Page title | `text-[15px] font-semibold` (or existing heading tokens) | `text-2xl` |
| Empty-state title | `text-sm font-semibold` | `text-lg` |
| Toast / banner body | `text-xs text-text-secondary` | `text-[14px]` |
| Table data mono | `font-mono text-[12px]` | inline `font-family` |
| Section label uppercase | `text-[10px] uppercase tracking-wider text-text-tertiary` | arbitrary sizes |

#### Spacing & Elevation Tokens

| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Card radius | `rounded-[var(--radius-md)]` | `rounded-lg` |
| Button/badge radius | `rounded-[var(--radius-sm)]` | `rounded-md` |
| Card shadow | existing `shadow-card` utility (via `bg-bg-elevated`) | arbitrary `shadow-lg` |
| Transition | `transition-colors` / `transition-all duration-200` | inline ms values |
| Section padding | `p-4`/`p-6` matching existing cards | `p-[20px]` |

#### Existing Components to REUSE

| Component | Path | Use For |
|-----------|------|---------|
| `<Button>` | `web/src/components/ui/button.tsx` | ALL buttons |
| `<Input>` | `web/src/components/ui/input.tsx` | ALL text inputs |
| `<Textarea>` | `web/src/components/ui/textarea.tsx` | Multi-line |
| `<Select>` | `web/src/components/ui/select.tsx` | Dropdowns |
| `<Checkbox>` | `web/src/components/ui/checkbox.tsx` | Column customizer |
| `<Card>` | `web/src/components/ui/card.tsx` | Compare panels, empty states |
| `<Dialog>` | `web/src/components/ui/dialog.tsx` | Save View, Add Annotation, Announcement editor |
| `<SlidePanel>` | `web/src/components/ui/slide-panel.tsx` | Column customizer |
| `<Tabs>` | `web/src/components/ui/tabs.tsx` | Referencing Policies tab (D-007) |
| `<DropdownMenu>` | `web/src/components/ui/dropdown-menu.tsx` | Saved Views menu, export menu |
| `<Tooltip>` | `web/src/components/ui/tooltip.tsx` | Freshness badge |
| `<Badge>` | `web/src/components/ui/badge.tsx` | Live / Offline indicators |
| `<TableToolbar>` | `web/src/components/ui/table-toolbar.tsx` | Already wired for density + export — extend with saved-views slot |
| `<RowActionsMenu>` | `web/src/components/shared/row-actions-menu.tsx` | Context menu target |
| `<Breadcrumb>` | `web/src/components/ui/breadcrumb.tsx` | Admin screens |
| Toast (sonner) | `import { toast } from 'sonner'` | Undo + progress + success |

**Rule: zero raw `<input>` / `<button>` / `<table>` in any new file — reuse atoms.**

## Prerequisites

- [x] STORY-075 DONE — detail pages (inline-edit targets exist)
- [x] STORY-076 DONE — row actions + `use-keyboard-nav` hook + search skeleton
- [x] D-001, D-002 already resolved (verified: `ip-pool-detail.tsx` uses shadcn `Input`/`Button`; `apns/index.tsx` contains zero raw `<button>`/`<input>` — no action needed, log as already-fixed)
- [x] React Query, Zustand, Sonner, Recharts, shadcn/ui all present
- New runtime deps: `react-i18next`, `i18next`, `i18next-browser-languagedetector`, `html-to-image` (FE); `github.com/oschwald/geoip2-golang` (BE)

## Tech Debt (from ROUTEMAP)

- **D-001** (STORY-056 Gate): Already resolved — `ip-pool-detail.tsx` line 234/242/247 uses shadcn. Verification task only.
- **D-002** (STORY-056 Gate): Already resolved — no raw `<button>` in `apns/index.tsx` or `ip-pool-detail.tsx`. Verification task only.
- **D-006** (STORY-073): GeoIP lookup for SCR-144 — implement in this story (new `internal/geoip` package + sessions handler wiring).
- **D-007** (STORY-075): APN Policies Referencing tab — implement store query + handler + UI tab.
- **D-008** (STORY-076): Search DTO enrichment — implement per-type DTOs.
- **D-009** (STORY-076): Row data attributes on list pages — wire across all list pages.

## Story-Specific Compliance Rules

- **API envelope** (ADR-001): every new endpoint returns `{status, data, meta?, error?}`
- **RLS** (STORY-064): every new table gets RLS policy keyed on `tenant_id` and/or `user_id`
- **Audit** (STORY-007): impersonation actions, announcement CRUD, undo executions all write audit events; `impersonated_by` column populated whenever acting under impersonation
- **Tenant scoping**: all queries gated by `tenant_id` from request context
- **FRONTEND.md**: dark-first, neon accents, font-mono for IDs/timestamps, no hardcoded colors
- **i18n**: NO user-facing string literal left in components after migration; all strings keyed in locale files
- **RBAC**: super_admin-only for impersonation; tenant_admin for their own announcements; user-scoped for saved views/column prefs/chart annotations
- **Read-only impersonation**: middleware MUST block all non-GET/HEAD/OPTIONS requests when `ctx.impersonated == true`
- **Undo TTL** fixed at 15s server-side; client countdown 10s (gap buffers network)

## Bug Pattern Warnings

- Streaming CSV response must flush after each row batch — otherwise Go buffers entire output in memory for 10M SIMs (use `bufio.Writer` or explicit `http.Flusher`).
- React Query optimistic updates require `cancelQueries` in `onMutate` — otherwise inflight refetch overwrites the optimistic value.
- i18n `Intl.DateTimeFormat('tr-TR', {dateStyle:'short'})` yields `DD.MM.YYYY` correctly; DO NOT format with string replace.
- `dsl_compiled::text` ILIKE may return false positives for APN names that are substrings of other identifiers (e.g., "iot" matches "iotx") — wrap the needle with word boundaries `\\m{apn}\\M` via regex or join on whitespace. Document the limitation in the store method.
- Redis undo entry must include tenant_id to prevent cross-tenant undo replay.
- Impersonation JWT must have a distinct `jti` and be logged at issuance; also include `exp` ≤ 1h.
- `html-to-image` fails on SVG `<foreignObject>` within Recharts — may need `toCanvas` fallback.

## Mock Retirement

No mock retirement for this story (all endpoints have real adapters).

## Risks & Mitigations

- **Risk**: i18n migration touching every file is high-blast-radius. **Mitigation**: migrate common + forms + errors + emptyStates namespaces only this story; allow remaining strings to fall through to English default (react-i18next auto-fallback). Track remaining strings in tech-debt ledger as D-010 IF unresolved.
- **Risk**: GeoLite2 DB (~70MB) bloats image. **Mitigation**: mount from host `/opt/geoip` volume; gracefully null-out location when file absent.
- **Risk**: `dsl_compiled` trigram scan may be slow on large policy sets. **Mitigation**: GIN trigram index + hard `LIMIT 50` + explicit EXPLAIN ANALYZE in tests.
- **Risk**: Global sticky table header CSS may break existing tables. **Mitigation**: opt-in via `sticky` prop defaulting to `true` only for the `<Table>` in list pages (not detail).
- **Risk**: CSV export on 10M SIMs times out. **Mitigation**: stream cursor-paginated, no in-memory buffering, `Content-Transfer-Encoding: chunked`.

---

## Tasks

Total: 22 tasks across 5 dependency waves. Wave boundaries preserved for parallelism.

### Wave 1 — DB Migrations + Shared Infra (parallel)

### Task 1: STORY-077 core schema migration
- **Files:** Create `migrations/20260417000001_story_077_ux.up.sql`, `migrations/20260417000001_story_077_ux.down.sql`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** `migrations/20260416000001_admin_compliance.up.sql`
- **Context refs:** Database Schema
- **What:** Create `user_views`, `announcements`, `announcement_dismissals`, `chart_annotations`, `user_column_preferences`. Add `users.locale` and `audit_events.impersonated_by`. Down migration drops everything additive.
- **Verify:** `make db-migrate` succeeds; `psql -c "\d user_views"` shows expected columns.

### Task 2: Policy DSL trigram index migration (D-007)
- **Files:** Create `migrations/20260417000002_story_077_policy_dsl_trgm.up.sql`, `.down.sql`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** `migrations/20260412000008_composite_indexes.up.sql`
- **Context refs:** Database Schema
- **What:** Enable `pg_trgm`; create GIN trigram index on `policy_versions.dsl_compiled::text`. Down: drop index (keep extension).
- **Verify:** `EXPLAIN SELECT ... WHERE dsl_compiled::text ILIKE '%apn-foo%'` uses the new index.

### Task 3: Row-Level Security for new tables
- **Files:** Create `migrations/20260417000003_story_077_rls.up.sql`, `.down.sql`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** `migrations/20260413000002_story_069_rls.up.sql`
- **Context refs:** Database Schema, Story-Specific Compliance Rules
- **What:** Enable RLS on `user_views`, `announcements`, `announcement_dismissals`, `chart_annotations`, `user_column_preferences`; policies scoped by `tenant_id`/`user_id`; `announcements` readable when `target='all'` or `target=current_tenant`.
- **Verify:** Integration test for cross-tenant isolation passes.

### Task 4: Redis undo registry package
- **Files:** Create `internal/undo/registry.go`, `internal/undo/registry_test.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** `internal/cache/*.go`
- **Context refs:** Data Flow (Undo section), API Specifications (POST /undo)
- **What:** `undo.Register(ctx, tenantID, userID, action, payload) (actionID string, err error)` writes `{action, payload, tenant_id, user_id, issued_at}` to key `undo:{actionID}` with 15s TTL. `undo.Consume(ctx, tenantID, actionID) (Entry, error)` atomic GETDEL; returns ErrExpired when TTL gone. Enforces tenant match. Unit test: register/consume happy path; expiry; cross-tenant rejection.
- **Verify:** `go test ./internal/undo/...` passes.

### Task 5: MaxMind GeoIP lookup package (D-006)
- **Files:** Create `internal/geoip/lookup.go`, `internal/geoip/lookup_test.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** `internal/operator/adapter/mock.go` (lazy-init singleton pattern)
- **Context refs:** Data Flow (D-006 section), Risks & Mitigations
- **What:** Add `geoip2-golang` to go.mod. `geoip.New(dbPath string) (*Lookup, error)` opens MaxMind GeoLite2-City; `Lookup(ip string) *LocationInfo` with `{Country, City, Lat, Lon}` — returns nil when disabled/missing/lookup-failed (non-fatal). Thread-safe (reader is safe for concurrent use). Honor `GEOIP_DB_PATH` env; graceful no-op when path empty. Test with fixture DB from geoip2-golang test data.
- **Verify:** `go test ./internal/geoip/...` passes; nil path yields nil location without error.

### Task 6: Streaming CSV export helper
- **Files:** Create `internal/export/csv.go`, `internal/export/csv_test.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** `internal/api/sim/handler.go` (cursor pagination pattern to reuse)
- **Context refs:** Data Flow (Export), API Specifications
- **What:** `export.StreamCSV[T](w http.ResponseWriter, filename string, header []string, rows func(yield func([]string) bool))` — sets Content-Type, Content-Disposition, writes header, streams via `csv.Writer.Write` with periodic flush. Helper `BuildFilename(resource string, filters map[string]string) string` generates `sims_state-active_operator-vodafone_2026-04-11.csv`.
- **Verify:** Unit test: 10k rows streamed without OOM; filename contains filters.

### Wave 2 — Backend Endpoints (parallel, depend on Wave 1)

### Task 7: Saved Views CRUD handler + store
- **Files:** Create `internal/store/user_view.go`, `internal/api/user/views_handler.go`; modify `cmd/argus/main.go` (route wiring)
- **Depends on:** Task 1, Task 3
- **Complexity:** medium
- **Pattern ref:** `internal/store/notification_preference_store.go`, `internal/api/notification/handler.go`
- **Context refs:** API Specifications (saved views), Database Schema, Story-Specific Compliance Rules
- **What:** Store methods: `Create`, `List(userID, page)`, `Update`, `Delete`, `SetDefault`. Handler endpoints per API spec. Validation: `page` ∈ allowlist (sims, apns, …); max 20 views/user/page; default uniqueness enforced by partial index.
- **Verify:** `curl -X POST /api/v1/user/views -d '{"page":"sims","name":"Active VF","filters_json":{"state":"active"}}'` returns 201.

### Task 8: Undo execute endpoint + inverse-op registry
- **Files:** Create `internal/api/undo/handler.go`, `internal/api/undo/handler_test.go`; modify `internal/api/sim/handler.go` (register undo on bulk state change + delete segment), `internal/api/policy/handler.go` (delete policy), `internal/api/apikey/handler.go` (revoke); wire in `cmd/argus/main.go`
- **Depends on:** Task 4
- **Complexity:** high
- **Pattern ref:** `internal/api/sim/handler.go:BulkStateChange`
- **Context refs:** Data Flow (Undo), API Specifications (POST /undo), Bug Pattern Warnings
- **What:** Destructive handlers call `undo.Register` with inverse JSON (e.g., bulk suspend → `{action:"bulk_state_change", previous_states: {sim_id: prev_state, ...}}`). Undo endpoint looks up action type, dispatches typed inverse-executor function map; writes audit event; idempotent.
- **Verify:** E2E: bulk suspend 10 SIMs → returns `action_id` → POST undo → SIMs restored.

### Task 9: Impersonation handlers + middleware
- **Files:** Create `internal/api/admin/impersonate.go`, `internal/api/admin/impersonate_test.go`, `internal/middleware/impersonation.go`; modify `internal/auth/jwt.go` (add `act` claim + `impersonated` flag), `cmd/argus/main.go` (register middleware, route)
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** `internal/api/admin/revoke_sessions_handler.go`
- **Context refs:** Data Flow (Impersonation), API Specifications (admin/impersonate), Story-Specific Compliance Rules, Bug Pattern Warnings
- **What:** Super-admin gate → issue new JWT for target user with `act.sub=original_admin_id`, `impersonated=true`, exp=1h. Middleware: if `impersonated` flag set on request ctx → block all `r.Method != GET && != HEAD && != OPTIONS` with 403 "Impersonation is read-only". All audit entries within impersonation ctx get `impersonated_by = act.sub`. Exit endpoint: re-issues original admin JWT (server holds refresh binding).
- **Verify:** Test: super_admin POST impersonate/:id → 200 + JWT; with impersonation JWT, POST /sims → 403; GET /sims → 200; audit row has `impersonated_by` populated.

### Task 10: Announcements CRUD + dismiss + active feed
- **Files:** Create `internal/store/announcement.go`, `internal/api/announcement/handler.go`, `internal/api/announcement/handler_test.go`; modify route wiring
- **Depends on:** Task 1, Task 3
- **Complexity:** medium
- **Pattern ref:** `internal/api/admin/maintenance_window.go`
- **Context refs:** Data Flow (Announcements), API Specifications, Database Schema
- **What:** Store + handlers per spec. `GET /announcements/active` joins `announcement_dismissals` for the current user; filters by `starts_at ≤ now ≤ ends_at AND (target='all' OR target=current_tenant_id::text)`. Admin CRUD gated by tenant_admin (scope='tenant') or super_admin ('all').
- **Verify:** E2E: create announcement (info, target=all, starts=now, ends=+1h) → GET active returns it → dismiss → GET active excludes it.

### Task 11: CSV export sub-routes on all list resources
- **Files:** Modify `internal/api/sim/handler.go`, `internal/api/apn/handler.go`, `internal/api/operator/handler.go`, `internal/api/policy/handler.go`, `internal/api/session/handler.go`, `internal/api/job/handler.go`, `internal/api/audit/handler.go`, `internal/api/cdr/handler.go`, `internal/api/notification/handler.go`, `internal/api/violation/handler.go`, `internal/api/anomaly/handler.go`, `internal/api/user/handler.go`, `internal/api/apikey/handler.go`, route wiring
- **Depends on:** Task 6
- **Complexity:** medium
- **Pattern ref:** existing list handlers (cursor pagination)
- **Context refs:** API Specifications (export), Data Flow (Export), Bug Pattern Warnings
- **What:** Each list handler gets `ExportCSV(w, r)` method: reuses filter parsing, uses `export.StreamCSV` with resource-specific header + row-mapper. Registers `GET /{resource}/export`. No full in-memory buffering.
- **Verify:** `curl -o sims.csv /api/v1/sims/export?state=active` downloads valid CSV; streams for large result sets (memory stays flat in pprof).

### Task 12: APN Policies Referencing endpoint + store method (D-007)
- **Files:** Modify `internal/store/policy.go` (add `ListReferencingAPN`), `internal/api/apn/handler.go` (add `ListReferencingPolicies`); route wiring
- **Depends on:** Task 2
- **Complexity:** medium
- **Pattern ref:** `internal/store/policy.go` existing methods
- **Context refs:** Data Flow (D-007), Database Schema, Bug Pattern Warnings, API Specifications (apns/:id/referencing-policies)
- **What:** Store: `ListReferencingAPN(ctx, tenantID, apnName, limit, cursor) ([]Policy, nextCursor, error)` — joins `policies` with `policy_versions` via `current_version_id`, filters `dsl_compiled::text ILIKE '%' || $1 || '%'`. Guard against pathological needles (require len ≥ 3). Handler looks up APN name by ID first, then delegates.
- **Verify:** Integration test: policy with `MATCH apn == "iot-apn"`; GET /apns/{iot-apn-id}/referencing-policies returns it; unrelated APNs excluded.

### Task 13: Search handler DTO enrichment (D-008)
- **Files:** Modify `internal/api/search/handler.go`, `internal/api/search/handler_test.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** current `search/handler.go`
- **Context refs:** API Specifications (search), Data Flow (D-008)
- **What:** Replace flat `SearchResult` with per-type structs (SIM: add `state`, `operator_name` via LEFT JOIN operators; APN: `mcc`, `operator_name`; Operator: `mcc`, `health_status`; Policy: `state`; User: `role`). Change response envelope `data` to `{sims:[...], apns:[...], ...}`. Keep errgroup + 500ms budget. Update FE `use-search.ts` + command palette consumer in same task if trivial.
- **Verify:** `curl /api/v1/search?q=vf` returns each SIM hit with state + operator_name.

### Task 14: GeoIP-enriched active sessions (D-006)
- **Files:** Modify `internal/api/admin/sessions_global.go`, `internal/config/config.go` (GEOIP_DB_PATH env), `deploy/docker-compose.yml` (volume mount), `docs/architecture/CONFIG.md` (env docs)
- **Depends on:** Task 5
- **Complexity:** medium
- **Pattern ref:** `internal/api/admin/sessions_global.go` existing shape
- **Context refs:** Data Flow (D-006), Architecture Context (Components)
- **What:** Add `Location *geoip.LocationInfo` field to `activeSessionItem`. Inject `*geoip.Lookup` into `Handler`; during row emission call `Lookup(IPAddress)`. Config: `GEOIP_DB_PATH` default empty → lookup disabled gracefully. Docker compose: mount optional `./geoip:/app/geoip:ro`; document GeoLite2-City.mmdb acquisition.
- **Verify:** With fixture DB, GET /api/v1/admin/sessions/active returns `location: {country:"TR", city:"Istanbul", ...}`; without DB, `location: null`, no errors.

### Task 15: Chart annotations CRUD
- **Files:** Create `internal/store/chart_annotation.go`, `internal/api/analytics/chart_annotation_handler.go`; route wiring
- **Depends on:** Task 1, Task 3
- **Complexity:** low
- **Pattern ref:** `internal/store/anomaly_comment.go`, `internal/api/anomaly/comment.go`
- **Context refs:** API Specifications (chart_annotations), Database Schema
- **What:** Create/List/Delete endpoints per spec. Scoped by tenant+user. List filters by `chart_key` + time range.
- **Verify:** E2E roundtrip.

### Task 16: User preferences endpoint (locale, columns, auto-refresh)
- **Files:** Modify `internal/api/user/handler.go` (add `UpdatePreferences`), `internal/store/user.go`; route wiring
- **Depends on:** Task 1
- **Complexity:** low
- **Pattern ref:** existing user handler
- **Context refs:** API Specifications (user/preferences), Data Flow (i18n, sticky tables)
- **What:** PATCH `/user/preferences` with partial body. Locale → `users.locale` column (CHECK en/tr). Column prefs → upsert into `user_column_preferences`. Auto-refresh → localStorage-only (no backend).
- **Verify:** PATCH sets locale; GET /user/me reflects it.

### Wave 3 — Shared FE Hooks + Components (parallel, depend on Wave 2 endpoints)

### Task 17: Shared FE hooks bundle
- **Files:** Create `web/src/hooks/use-saved-views.ts`, `use-undo.ts`, `use-form-validation.ts`, `use-data-freshness.ts`, `use-column-preferences.ts`, `use-announcements.ts`, `use-impersonation.ts`, `use-export.ts`, `use-chart-export.ts`
- **Depends on:** Tasks 7,8,10,11,15,16,9
- **Complexity:** high
- **Pattern ref:** `web/src/hooks/use-sims.ts` (React Query patterns)
- **Context refs:** Data Flow (each subsection), API Specifications, Bug Pattern Warnings (optimistic updates)
- **What:** React-Query-backed hooks mapping 1:1 to the new endpoints. `useFormValidation<T>` is pure FE (no API). `useDataFreshness({ source, lastUpdated, refetch })` returns `{label, indicator:'live'|'stale'|'offline', setAutoRefresh, autoRefresh}`. `useUndo(action_id)` encapsulates 10s countdown + toast cancellation. `useColumnPreferences(pageKey)` dual-persist (localStorage immediate + debounced server sync). Split into one file per hook; no file >150 lines.
- **Verify:** `pnpm --filter web typecheck` clean; `pnpm --filter web test hooks/*` passes.

### Task 18: Shared FE components bundle
- **Files:** Create `web/src/components/shared/editable-field.tsx`, `undo-toast.tsx`, `saved-views-menu.tsx`, `empty-state.tsx`, `data-freshness.tsx`, `column-customizer.tsx`, `form-field.tsx`, `impersonation-banner.tsx`, `announcement-banner.tsx`, `progress-toast.tsx`, `unsaved-changes-prompt.tsx`, `first-run-checklist.tsx`, `compare-view.tsx`; update `web/src/components/shared/index.ts` barrel
- **Depends on:** Task 17
- **Complexity:** high
- **Pattern ref:** `web/src/components/shared/row-actions-menu.tsx`
- **Context refs:** Screen Mockups, Design Token Map, Components to REUSE
- **What:** One component per AC slice. `<EditableField value onSave schema?>` owns its own local edit state, optimistic swap, rollback, Esc cancel. `<ColumnCustomizer columns onChange>` slide-panel with checkbox list + up/down reorder buttons (no extra DnD dep unless already present). `<CompareView entities kind>` renders side-by-side grid with field-level diff. No hardcoded hex — use the Design Token Map.
- **Note:** Invoke `frontend-design` skill during implementation.
- **Verify:** `grep -rnE "#[0-9a-fA-F]{3,8}" web/src/components/shared/*.tsx` → zero matches for new files.

### Task 19: i18n bootstrap + locale resource files
- **Files:** Create `web/src/lib/i18n.ts`, `web/src/locales/en/common.json`, `tr/common.json`, `en/forms.json`, `tr/forms.json`, `en/errors.json`, `tr/errors.json`, `en/emptyStates.json`, `tr/emptyStates.json`; modify `web/src/main.tsx` (init i18n), `web/src/components/layout/topbar.tsx` (add language toggle), `web/src/lib/format.ts` (locale-aware date/number)
- **Depends on:** Task 16
- **Complexity:** high
- **Pattern ref:** `web/src/lib/format.ts` extend
- **Context refs:** Data Flow (i18n), Bug Pattern Warnings, Design Token Map
- **What:** Install `react-i18next`, `i18next`, `i18next-browser-languagedetector`. Configure: language persistence via `useUIStore.locale` + sync to `PATCH /user/preferences`. Extract common+forms+errors+emptyStates strings — leave remaining literals unchanged (fall back to English). Use `useTranslation()` in shared components. Add TR translations for those four namespaces. Date/number helpers use `Intl` with active locale.
- **Verify:** Toggle language in topbar → `toast("saved")` changes between "Saved" and "Kaydedildi"; dates in `<DataFreshness>` switch between TR/EN formats.

### Task 20: Table header sticky + CSS density variable
- **Files:** Modify `web/src/components/ui/table.tsx` (add `sticky` prop default true for list-page use), `web/src/app.tsx` (apply `--table-row-height` CSS variable based on `useUIStore.tableDensity`), `web/src/index.css` (row height rules)
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** existing `table.tsx`
- **Context refs:** Data Flow (Density, Sticky Tables), Design Token Map
- **What:** Sticky header via `className="sticky top-0 z-[2] bg-bg-surface"`; preserve existing backgrounds. CSS variable applied to `html`: `{compact:'32px',comfortable:'40px',spacious:'48px'}`. Table rows use `style={{height:'var(--table-row-height)'}}` or via Tailwind arbitrary class. Sticky first column opt-in via `<TableCell sticky="left">`.
- **Verify:** Visual scroll SIM list past row 50 → header visible; density cycle changes row height live.

### Wave 4 — Page Integration (parallel, depend on Wave 3)

### Task 21: List-page integration (empty states, saved views, export wiring, freshness, row data-attrs — D-009, row-click behavior)
- **Files:** Modify `web/src/pages/sims/index.tsx`, `apns/index.tsx`, `operators/index.tsx`, `policies/index.tsx`, `sessions/index.tsx`, `jobs/index.tsx`, `audit/index.tsx`, `notifications/index.tsx`, `violations/index.tsx`, `alerts/index.tsx`, `esim/index.tsx`, `roaming/index.tsx`, `admin/api-keys.tsx` (or equivalent), `admin/users.tsx` (or equivalent)
- **Depends on:** Task 17, Task 18, Task 20
- **Complexity:** high
- **Pattern ref:** `web/src/pages/sims/index.tsx` (STORY-076 patterns)
- **Context refs:** Data Flow (Saved Views, Empty State, Export, Data Freshness, Row Click, D-009), Screen Mockups, Components to REUSE
- **What:** For each page: wire `<SavedViewsMenu page="sims"/>`, `<EmptyState>` in zero-result branch, `<DataFreshness>` footer, `<TableToolbar onExport={useExport('sims').export}>`, add `data-row-index / data-href / data-row-active` attributes to each row, enforce row-click-to-navigate while respecting checkbox + context menu + double-click. Each file should change minimally (≤80 lines each). Split into two sub-commits if reviewer prefers (sub-task a: data attrs+row click; sub-task b: saved views+empty+freshness+export).
- **Verify:** (1) Select 2 rows on SIMs list → press J/K → active row moves (D-009 satisfied via `use-keyboard-nav`). (2) Empty tenant SIM list shows `<EmptyState>` CTA. (3) Save view "Active VF" → refresh → appears in sidebar menu → click restores filters.

### Task 22: Inline edit + undo toasts + impersonation banner + announcement banner + optimistic progress + chart export + compare views + form validation (detail-page sweep)
- **Files:** Modify `web/src/pages/sims/detail.tsx`, `policies/detail.tsx`, `operators/detail.tsx`, `apns/detail.tsx` (add Policies Referencing tab — D-007 UI consumer), `notifications/detail.tsx`, `segments/...` (or segment dialog), `web/src/components/layout/dashboard-layout.tsx` (mount banners), `web/src/router.tsx` (admin/announcements + admin/impersonate routes), `web/src/pages/admin/announcements.tsx` (NEW full CRUD), `web/src/pages/admin/impersonate-list.tsx` (NEW — user list with "Impersonate" button), `web/src/pages/policies/compare.tsx` (NEW), `web/src/pages/operators/compare.tsx` (NEW); create `web/src/pages/dashboard.tsx` patch adding `<FirstRunChecklist>`; modify any chart-bearing page to wire `<ChartExportButton>` + annotation handling (dashboards, cdr, cost, anomaly) — `web/src/pages/dashboard.tsx`, `analytics/*`, `compliance/*` as applicable; modify create/edit forms (SIM create/edit, APN create, operator create, policy editor meta, user create, API key create, segment create, notification config, roaming) to use `<FormField>` + `useFormValidation` + `<UnsavedChangesPrompt>`
- **Depends on:** Task 17, Task 18, Task 19, Task 12
- **Complexity:** high
- **Pattern ref:** `web/src/pages/sims/detail.tsx`, existing `apns/detail.tsx` tab structure
- **Context refs:** Data Flow (Inline Edit, Impersonation, Announcements, Optimistic Updates, Chart Export, Compare, Form Validation, D-007), Screen Mockups, Design Token Map
- **What:** Mount `<ImpersonationBanner>` + `<AnnouncementBanner>` in `dashboard-layout.tsx` above the content frame. Wrap field labels in `<EditableField>` per AC-3 list. Admin announcements page (full CRUD list + editor dialog). Admin impersonate-list (search users, POST impersonate, navigate to `/` with new JWT). Dashboard `<FirstRunChecklist>` reads onboarding status. Policy/Operator compare pages: accept `?ids=a,b,c` query; render `<CompareView>`. Charts: add `<ChartExportButton chartRef>` + `<AddAnnotationButton>` + render existing annotations as `<ReferenceLine>`. Every form file converts its inputs to `<FormField>` + uses `useFormValidation` schema + intercepts route change with `<UnsavedChangesPrompt>` when dirty. D-007 tab: `<PoliciesReferencingTab apnId={apn.id}>` using `useQuery(['apn', id, 'referencing-policies'])`.
- **Note:** Break into 2 developer-dispatch groups if the file count > 10 per session — (a) layout mounts + admin screens + compare, (b) inline-edit + form validation + charts + APN tab.
- **Verify:** E2E: the 11 test scenarios in the story pass; super_admin impersonates → purple banner; announcement visible to all; SIM label hover pencil → edit in place; TR toggle changes number format.

---

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| AC-1 Saved views | Task 7 (BE) + Task 17,18,21 (FE) | Task 21 verify; E2E #1 |
| AC-2 Undo | Task 4,8 (BE) + Task 17,18,22 (FE) | Task 8 verify; E2E #2 |
| AC-3 Inline edit | Task 18,22 (FE) + existing PATCH endpoints | Task 22 verify; E2E #3 |
| AC-4 CSV export | Task 6,11 (BE) + Task 17,21 (FE) | Task 11 verify; E2E #4 |
| AC-5 Empty states + checklist | Task 18,21,22 | Task 21 verify; E2E #5 |
| AC-6 Data freshness | Task 17,18,21 | Task 21 verify; E2E #6 |
| AC-7 Sticky headers + columns | Task 16,20,18,21 | Task 20,21 verify; E2E #7 |
| AC-8 Form validation | Task 17,18,22 | Task 22 verify; E2E #8 |
| AC-9 Impersonation | Task 9 (BE) + Task 17,18,22 (FE) | Task 9 verify; E2E #9 |
| AC-10 Announcements | Task 10 (BE) + Task 17,18,22 (FE) | Task 10 verify; E2E #10 |
| AC-11 i18n | Task 16,19 + Task 22 form copy | E2E #11 |
| AC-12 Density | Task 20 (already partial in `useUIStore`) | Task 20 verify |
| AC-13 Chart export + annotation | Task 15 (BE) + Task 17,18,22 (FE) | Task 22 verify |
| AC-14 Progress + optimistic | Task 17,18,22 | Task 22 verify |
| AC-15 Row click | Task 18,21 | Task 21 verify |
| AC-16 Compare views | Task 18,22 | Task 22 verify |
| D-001 Raw input | Already resolved (verify in checkout) | pre-verify by grep — no file change |
| D-002 Raw button | Already resolved | grep verify — no file change |
| D-006 GeoIP | Task 5,14 | Task 14 verify |
| D-007 APN policies ref | Task 2,12,22 | Task 12 verify |
| D-008 Search enrichment | Task 13 | Task 13 verify |
| D-009 Row data-attrs | Task 21 | Task 21 verify (J/K nav works) |

---

## Wave Summary

- **Wave 1 (6 tasks parallel):** Tasks 1,2,3,4,5,6 — DB migrations + Redis undo + GeoIP + CSV helper.
- **Wave 2 (10 tasks parallel):** Tasks 7,8,9,10,11,12,13,14,15,16 — backend endpoints (all independent; some depend on Wave 1).
- **Wave 3 (4 tasks parallel):** Tasks 17,18,19,20 — shared FE hooks, components, i18n, sticky+density.
- **Wave 4 (2 large tasks sequential or split):** Tasks 21,22 — page integration.

Amil may further split Task 21 and Task 22 into 2–3 subtasks each at dispatch time if individual pages would exceed the Developer's comfortable scope.

Total: 22 tasks, 5 complexity-high (Tasks 8, 9, 17, 18, 22), 11 medium, 6 low.

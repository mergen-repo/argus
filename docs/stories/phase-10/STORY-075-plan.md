# Implementation Plan: STORY-075 — Cross-Entity Context & Detail Page Completeness

## Goal

Make every Argus entity detail page feel like Datadog/Linear: six reusable cross-entity components, four existing detail pages enriched with related-data tabs, five brand-new detail pages (session, user, alert, violation, tenant), and audit-log entity_id becoming clickable — closing all 61 missing related-data views found in the Phase 10 cross-entity audit (zero deferral).

## Architecture Context

### Components Involved

Frontend (primary surface — atomic design):
- `web/src/components/shared/` (NEW dir) — six cross-entity molecules/organisms, exported via `index.ts` barrel
  - `entity-link.tsx` (atom) — AC-5
  - `copyable-id.tsx` (atom) — AC-6
  - `related-audit-tab.tsx` (organism) — AC-1
  - `related-notifications-panel.tsx` (molecule) — AC-2
  - `related-alerts-panel.tsx` (molecule) — AC-3
  - `related-violations-tab.tsx` (organism) — AC-4
- `web/src/hooks/use-entity-related.ts` (NEW) — thin wrappers over `useAuditList`, notification list, alert (anomaly) list, violation list, all keyed by `entity_id`/`resource_id`. Reuses existing list endpoints.
- `web/src/pages/sims/detail.tsx` — add 8 sections via new `_tabs/` partial files (avoid bloating to >60K)
- `web/src/pages/apns/detail.tsx` — add 4 sections
- `web/src/pages/operators/detail.tsx` — add 6 sections
- `web/src/pages/policies/editor.tsx` (existing `/policies/:id`) — add 6 sections
- `web/src/pages/sessions/detail.tsx` (NEW) — AC-11
- `web/src/pages/settings/user-detail.tsx` (NEW) — AC-12, routed at `/settings/users/:id`
- `web/src/pages/alerts/detail.tsx` (NEW) — AC-13
- `web/src/pages/violations/detail.tsx` (NEW) — AC-14
- `web/src/pages/system/tenant-detail.tsx` (NEW) — AC-15, routed at `/system/tenants/:id`
- `web/src/pages/audit/index.tsx` — AC-16 (entity_id + actor column become `<EntityLink>`)
- `web/src/router.tsx` — five new routes
- Existing hooks reused: `use-audit`, `use-notifications`, `use-sims`, `use-settings` (users), `use-analytics` (anomalies for alerts), `use-policies`, `use-operators`, `use-apns`, `use-sessions`. Add minimal additions in `use-sessions.ts`, `use-settings.ts` (user detail), plus new `use-tenant-detail.ts` and `use-violation-detail.ts`.

Backend (minimal — fill gaps only):
- `internal/api/session/handler.go` — add `Get(w, r)` method (single-session fetch, DTO enrichment reuses existing `enrichSessionDTO`). Wire `GET /api/v1/sessions/{id}` in `internal/gateway/router.go` (alongside existing `/sessions/{id}/disconnect`).
- `internal/api/session/handler.go` — extend `sessionDTO` to include `sor_decision` and `policy_applied` optional fields (already captured by session engine; surface as JSON map). Add endpoint `GET /api/v1/sims/{id}/policy-rules-preview` via existing SIM or policy handler.
- `internal/api/violation/handler.go` — add `Get(w, r)` method. Wire `GET /api/v1/policy-violations/{id}`. Add `Remediate(w, r)` for inline actions: POST `/api/v1/policy-violations/{id}/remediate` with `action: "suspend_sim"|"escalate"|"dismiss"` body. Each action emits audit log.
- `internal/api/user/handler.go` — add `Get(w, r)` method. Wire `GET /api/v1/users/{id}`. Uses existing `userStore.GetByID`.
- `internal/api/user/handler.go` — add `Activity(w, r)` returning audit log entries where `actor_user_id = {id}` (reuses `auditStore.List` with actor filter). Wire `GET /api/v1/users/{id}/activity`.
- Tenant detail+stats endpoints already exist (`GET /api/v1/tenants/{id}`, `GET /api/v1/tenants/{id}/stats`). For AC-15 we extend `Stats` to include counts for SIM, APN, User, Operator, ActiveSessions, MonthlyCost, StorageBytes, quota_utilization — check current fields first; if missing, extend.
- Audit filter on entity_id + entity_type + actor is already supported (verified `AuditFilters` in `use-audit.ts` includes `entity_id`, `entity_type`, `user_id`).

### Data Flow

#### Shared RelatedAuditTab (AC-1)
```
Detail page mounts → <RelatedAuditTab entityId entityType />
  → calls useAuditList({ entity_id, entity_type, limit: 20 })
  → GET /api/v1/audit?entity_id=X&entity_type=Y&limit=50 (existing endpoint)
  → renders chronological list: action badge, actor <EntityLink>, timestamp, expandable JSON diff
  → "View All (N)" footer link → navigate(`/audit?entity_id=X&entity_type=Y`)
```

#### EntityLink resolution (AC-5)
```
<EntityLink entityType="sim" entityId="abc" label="sim-abc" />
  → internal route map: sim → /sims/:id, policy → /policies/:id, operator → /operators/:id,
    apn → /apns/:id, user → /settings/users/:id, job → /jobs?highlight=:id,
    session → /sessions/:id, tenant → /system/tenants/:id, violation → /violations/:id,
    alert/anomaly → /alerts/:id, apikey → /settings/api-keys?highlight=:id
  → unknown type → render plain <span className="font-mono text-text-secondary">
  → click → React Router <Link to={resolved}> (no page reload)
```

#### Violation remediation (AC-14)
```
User clicks "Suspend SIM" on violation detail
  → <ConfirmDialog> with reason input
  → POST /api/v1/policy-violations/{vid}/remediate { action: "suspend_sim", reason }
  → BE violates: (a) calls simStore state transition to 'suspended' (same code path as /sims/:id/suspend),
    (b) inserts audit log action='violation.remediated' entity_type='violation' entity_id=vid,
    (c) updates violations.acknowledged_at + ack_reason,
    (d) emits NATS event policy.violation.remediated
  → on success: refetch violation, toast "SIM suspended, violation acknowledged"
```

#### Session detail SoR display (AC-11)
```
GET /api/v1/sessions/{id}
  → BE loads session from sessionMgr, enriches operator/apn/sim names,
    adds sor_decision: { chosen_operator_id, scoring: [{op_id, score, reason}] } (from session.Metadata or re-scoring if live),
    adds policy_applied: { policy_id, version, matched_rules: [rule_index_array] }
  → FE renders SoR scoring as ranked list; chosen operator highlighted; others in grey with score delta
```

### API Specifications

All endpoints return standard envelope `{ status, data, meta? }` or error envelope.

**Existing (verified — reuse directly):**
- `GET /api/v1/audit` — `?entity_id`, `?entity_type`, `?user_id`, `?from`, `?to`, `?cursor`, `?limit` — already in `use-audit.ts`.
- `GET /api/v1/notifications?resource_id=X` — already in `use-notifications.ts`.
- `GET /api/v1/analytics/anomalies?sim_id=X` / `?operator_id=X` / `?apn_id=X` — alerts scoping (verify filter param naming during implementation; fallback: client-side filter).
- `GET /api/v1/analytics/anomalies/{id}` — alert detail (already exists).
- `GET /api/v1/policy-violations?sim_id=X&policy_id=X&acknowledged=false` — already exists.
- `POST /api/v1/policy-violations/{id}/acknowledge` — already exists (use for "Dismiss" action).
- `GET /api/v1/sims/{id}/history` (already), `/api/v1/sims/{id}/sessions`, `/api/v1/sims/{id}/usage`.
- `GET /api/v1/tenants/{id}`, `GET /api/v1/tenants/{id}/stats`.
- `GET /api/v1/users` (list), `POST /api/v1/users/{id}/revoke-sessions`, `POST /api/v1/users/{id}/reset-password`.
- `GET /api/v1/auth/sessions` — for User Detail "Active Sessions" tab we need per-user sessions; adapt existing list with user_id filter (verify server filter support; if missing, plan inline small BE addition).

**New endpoints:**
- `GET /api/v1/sessions/{id}` — single session fetch.
  - Response: `{ status, data: { ...sessionDTO, sor_decision?: {...}, policy_applied?: {...}, ip_allocated, quota_usage: { limit_bytes, used_bytes, pct }, audit_entries: [last 10] } }`
  - Errors: 404 `NOT_FOUND` if session terminated and pruned, 403 on tenant mismatch.
  - Status: 200 | 404 | 403.
- `GET /api/v1/policy-violations/{id}` — single violation fetch.
  - Response: `{ status, data: { id, sim_id, sim_iccid, policy_id, policy_name, version_id, rule_index, rule_text, violation_type, severity, action_taken, details, session_id, session, occurred_at, state, acknowledged_at, ack_reason } }`.
- `POST /api/v1/policy-violations/{id}/remediate` — inline remediation.
  - Body: `{ action: "suspend_sim" | "escalate" | "dismiss", reason: string }`.
  - Response: `{ status, data: { violation, sim?: SIM } }` (suspend_sim returns updated SIM).
  - Status: 200 | 400 (bad action) | 404 | 409 (sim already suspended).
- `GET /api/v1/users/{id}` — user detail.
  - Response: `{ status, data: { id, email, name, role, state, created_at, created_by, last_login, locale, totp_enabled, backup_codes_remaining } }`.
- `GET /api/v1/users/{id}/activity?cursor&limit` — audit entries where actor = user (reuse audit store with actor filter).
- `GET /api/v1/users/{id}/sessions` — per-user active browser sessions (if existing `auth/sessions` cannot filter by user, add thin wrapper).
- (Optional if data missing) `GET /api/v1/sims/{id}/policy-rules-preview` — returns current matching rules snippet. If infra cannot cheaply compute, derive client-side from policy DSL + SIM attributes; plan keeps it FE-only fallback.

### Database Schema

No new tables required. All reads use existing tables (verified via migrations up to `20260416000001_admin_compliance`). Remediation endpoint reuses:
- `sims` state transition — **Source: migrations/00000000000000_initial.up.sql via prior stories**.
- `policy_violations` — **Source: migrations/20260413000001_story_069_schema.up.sql** plus `20260413000003_violation_acknowledgment.up.sql` (columns `acknowledged_at timestamptz`, `ack_reason text`).
- `audit_log` — writes go through existing `audit.Auditor` interface.

### Screen Mockups

SCREENS.md has no dedicated ASCII mockups for the five new detail pages (confirmed: 72 lines total, screens-index only). Design follows the existing `sims/detail.tsx` pattern (tabs, cards, overview header). Reference image-equivalent:

```
┌─ Breadcrumb: Home › Sessions › session-abc123 ──────────────────────┐
│  [Pulsing dot] Active · 02h 14m · NB-IoT · APN: iot.argus.io        │
│  SIM <EntityLink sim-xyz> · Operator <EntityLink op-vodafone>       │
│  [Force Disconnect] [Copy Session ID] [Open SIM ↗]                  │
├──────────────────────────────────────────────────────────────────────┤
│  [Overview | SoR Decision | Policy | Audit | Related Sessions | CDRs│
├──────────────────────────────────────────────────────────────────────┤
│  ┌ Overview ─────────────┐  ┌ Quota Usage ─────────────┐           │
│  │ NAS IP:  10.0.0.5     │  │ [====~~~~~~] 42% of 5 GB │           │
│  │ RAT:     NB-IoT       │  │ 2.1 GB used · 2.9 GB left│           │
│  │ Proto:   RADIUS       │  └───────────────────────────┘           │
│  │ IP Alloc: 100.64.3.12 │                                          │
│  └───────────────────────┘                                          │
└──────────────────────────────────────────────────────────────────────┘
```

Analogous layout for Alert, Violation, User, Tenant (replace Overview cards with domain-specific fields, keep tab strip, keep breadcrumb + action bar). Drill-downs: every `<EntityLink>` navigates; action buttons open dialogs.

### Design Token Map (MANDATORY)

#### Color Tokens (from FRONTEND.md — verified against `web/tailwind.config.*`)

| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Page background | `bg-[var(--bg-primary)]` or existing `bg-bg-primary` alias | `bg-black`, `bg-[#06060B]` |
| Card surface | `bg-surface` / `bg-[var(--bg-surface)]` | `bg-[#0C0C14]`, `bg-gray-900` |
| Elevated surface (modals/dropdowns) | `bg-elevated` | `bg-[#12121C]` |
| Hover state | `bg-hover` | `bg-[#1A1A28]` |
| Active selected row | `bg-active` | `bg-[#1E1E2E]` |
| Primary text | `text-text-primary` | `text-white`, `text-[#E4E4ED]` |
| Secondary text | `text-text-secondary` | `text-gray-400`, `text-[#7A7A95]` |
| Tertiary/placeholder | `text-text-tertiary` | `text-[#4A4A65]` |
| Accent (links, focus) | `text-accent` / `bg-accent` | `text-cyan-400`, `text-[#00D4FF]` |
| Accent dim background | `bg-accent/15` or `bg-accent-dim` | `bg-cyan-900/20` |
| Success (active/healthy) | `text-success` / `bg-success/12` | `text-green-400`, `text-[#00FF88]` |
| Warning (degraded/suspended) | `text-warning` / `bg-warning/12` | `text-yellow-500`, `text-[#FFB800]` |
| Danger (critical/terminated) | `text-danger` / `bg-danger/12` | `text-red-500`, `text-[#FF4466]` |
| Purple secondary accent | `text-purple` | `text-violet-500` |
| Default border | `border-border` | `border-gray-800`, `border-[#1E1E30]` |
| Subtle border | `border-border-subtle` | `border-[#16162A]` |

#### Typography Tokens

| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Page title | `text-[15px] font-semibold` (Inter) | `text-2xl`, `text-[24px]` |
| Section label | `text-[11px] uppercase tracking-[0.5px] text-text-secondary font-medium` | arbitrary `text-xs` |
| Body | `text-[13px]` | `text-sm` fixed px |
| Metric value | `text-[28px] font-bold font-mono` | `text-3xl` |
| Mono data (ICCID/IP/UUID) | `text-[12px] font-mono` | `text-xs font-mono` + px overrides |

#### Spacing / Elevation / Radius

| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Card radius | `rounded-[10px]` (or existing `rounded-card` utility) | `rounded-md`, `rounded-lg` |
| Button radius | `rounded-[6px]` | `rounded`, `rounded-md` |
| Modal radius | `rounded-[14px]` | random values |
| Card shadow | existing `shadow-card` utility | none / `shadow-md` |
| Accent glow hover | existing `shadow-glow` utility | arbitrary `shadow-[0_0_20px_...]` |
| Content padding | `p-6` (24px) | `p-[20px]`, `p-4` (inconsistent) |
| Card inner padding | `p-4` (16px) | `p-[18px]` |
| Grid gap | `gap-4` | `gap-[15px]` |

#### Existing Components to REUSE (DO NOT recreate)

| Component | Path | Use For |
|-----------|------|---------|
| `<Button>` | `web/src/components/ui/button.tsx` | ALL buttons — NEVER raw `<button>` |
| `<Input>` | `web/src/components/ui/input.tsx` | ALL text inputs |
| `<Select>` | `web/src/components/ui/select.tsx` | ALL selects |
| `<Card>`, `<CardContent>`, `<CardHeader>`, `<CardTitle>` | `web/src/components/ui/card.tsx` | ALL card containers |
| `<Badge>` | `web/src/components/ui/badge.tsx` | Status/state pills |
| `<Tabs>`, `<TabsList>`, `<TabsTrigger>`, `<TabsContent>` | `web/src/components/ui/tabs.tsx` | All tab groupings |
| `<Table>`, `<TableRow>`, `<TableCell>` | `web/src/components/ui/table.tsx` | All tables (no raw `<table>`) |
| `<Dialog>` | `web/src/components/ui/dialog.tsx` | Confirmation modals (violation remediation) |
| `<SlidePanel>` | `web/src/components/ui/slide-panel.tsx` | Side drawer (alert note entry) |
| `<Breadcrumb>` | `web/src/components/ui/breadcrumb.tsx` | Page heading path |
| `<InfoRow>` | `web/src/components/ui/info-row.tsx` | Key/value pairs in detail overview |
| `<Spinner>` | `web/src/components/ui/spinner.tsx` | Loading state |
| `<Skeleton>` | `web/src/components/ui/skeleton.tsx` | Initial load placeholders |
| `<RATBadge>` | `web/src/components/ui/rat-badge.tsx` | RAT type display in session detail |
| `<AnimatedCounter>` | `web/src/components/ui/animated-counter.tsx` | Tenant dashboard counters |
| `<Sparkline>` | `web/src/components/ui/sparkline.tsx` | Tenant live-traffic strip |
| `<Tooltip>` | `web/src/components/ui/tooltip.tsx` | CopyableId feedback hint |
| `recharts` primitives | already in use in SIM/operator detail | Pie chart for operator SIM state breakdown |

RULE: grep `grep -rn '#[0-9a-fA-F]\{3,6\}' web/src/components/shared web/src/pages/sessions/detail.tsx web/src/pages/violations/detail.tsx web/src/pages/alerts/detail.tsx web/src/pages/settings/user-detail.tsx web/src/pages/system/tenant-detail.tsx` after implementation → MUST return 0.

## Prerequisites

- [x] STORY-057 DONE (API-051/052 live: usage, history, anomaly counts)
- [x] STORY-063 DONE (SLA reports, notification store, webhook, PDF formatters — consumed by Operator / Tenant detail)
- [x] STORY-065 DONE (metrics + SoR scoring telemetry — consumed by Session SoR display)
- [x] STORY-068 DONE (user browser sessions, permissions matrix data — consumed by User detail)
- [x] React Router v6, TanStack Query already in place

## Task Decomposition Rules Applied

XL story (per planner prompt table: most tasks medium/high, multiple high). 14 tasks across 4 waves. Each task touches ≤3 files except router wiring which is single-file. Shared components batched per cohesion (not layer). Existing SIM detail will exceed 50K after enrichment — we extract new tab content into `web/src/pages/sims/_tabs/*.tsx` partial files to keep single-file size reasonable.

## Tasks

### Task 1: Backend — Session GET, Violation GET+Remediate, User GET+Activity endpoints
- **Files:**
  - Modify `internal/api/session/handler.go` (add `Get` method + extend DTO with `sor_decision`, `policy_applied`, `quota_usage`)
  - Modify `internal/api/violation/handler.go` (add `Get` method + `Remediate` method with actions `suspend_sim`, `escalate`, `dismiss`; remediate reuses existing simStore state transition + audit + acknowledge)
  - Modify `internal/api/user/handler.go` (add `Get` method + `Activity` method reading audit log filtered by actor)
  - Modify `internal/gateway/router.go` (wire 5 new routes; RBAC parity with existing sibling routes)
- **Depends on:** —
- **Complexity:** high
- **Pattern ref:** Read `internal/api/tenant/handler.go` lines 190-324 (`Get` + `Stats` methods) and `internal/api/anomaly/handler.go` (`Get` + `UpdateState` pattern). Remediate action follows `internal/api/sim/handler.go` state-change handlers.
- **Context refs:** "API Specifications", "Data Flow", "Database Schema"
- **What:** Five new HTTP routes with tenant scoping, audit emission on remediation, 404 on missing/cross-tenant. Reuse existing stores; no new tables. Extend session DTO to include SoR + policy trace fields (zero-value defaults when session engine did not capture them — forward-compatible).
- **Verify:** `go build ./...`, `go test ./internal/api/session/... ./internal/api/violation/... ./internal/api/user/...`, plus manual `curl -H 'Authorization: Bearer $TOK' /api/v1/sessions/$ID` returning envelope.

### Task 2: Shared atoms — `<EntityLink>` + `<CopyableId>`
- **Files:** Create `web/src/components/shared/entity-link.tsx`, `web/src/components/shared/copyable-id.tsx`, `web/src/components/shared/index.ts` (barrel)
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `web/src/components/ui/badge.tsx` + `web/src/components/ui/rat-badge.tsx` for tiny atom pattern, and `web/src/components/ui/info-row.tsx` for typed props.
- **Context refs:** "Components Involved", "Data Flow > EntityLink resolution", "Design Token Map"
- **What:**
  - `<EntityLink>` props `{ entityType, entityId, label?, className?, truncate? }`. Uses `react-router-dom <Link>`. Internal route map keyed by entityType. Unknown type renders mono `<span>` in `text-text-secondary`. Truncate with middle ellipsis if `truncate` set (8 chars prefix + … + last 6). Hover shows full id via `<Tooltip>`.
  - `<CopyableId>` props `{ value, label?, masked?, mono? }`. Uses `navigator.clipboard.writeText` + 1500ms checkmark state. Icon from `lucide-react` `Copy` / `Check`. Masked mode: first 4 + ••• + last 4, reveal on click (second click to copy). Font `font-mono text-[12px]`.
- **Tokens:** Use ONLY classes from Design Token Map — zero hardcoded hex/px.
- **Components:** Reuse `<Tooltip>`; import `lucide-react` icons (already used elsewhere). No raw `<button>` — use `<Button variant="ghost" size="sm">` or a styled ghost.
- **Note:** Invoke `frontend-design` skill for professional quality.
- **Verify:** `grep -Rn '#[0-9a-fA-F]' web/src/components/shared` returns 0; `pnpm -C web tsc --noEmit` passes.

### Task 3: Shared organism — `<RelatedAuditTab>` + `<RelatedNotificationsPanel>`
- **Files:** Create `web/src/components/shared/related-audit-tab.tsx`, `web/src/components/shared/related-notifications-panel.tsx`; modify `web/src/components/shared/index.ts` (export)
- **Depends on:** Task 2 (uses `<EntityLink>` for actor + entity_id rendering)
- **Complexity:** medium
- **Pattern ref:** Read `web/src/pages/audit/index.tsx` for audit row structure (JSON diff expand, action badge variants, timeAgo). Notifications: read `web/src/pages/notifications/index.tsx` if present; else `web/src/hooks/use-notifications.ts`.
- **Context refs:** "Components Involved", "Data Flow > Shared RelatedAuditTab", "Design Token Map", "API Specifications"
- **What:**
  - `<RelatedAuditTab entityId entityType maxRows?=20 />` calls `useAuditList({ entity_id, entity_type })`. Renders compact table: timestamp (timeAgo, title=full ISO), action badge, actor `<EntityLink entityType="user">`, expand caret for JSON diff panel (before/after side-by-side if present in details). Footer "View all in Audit Log →" `<Link to={`/audit?entity_id=${entityId}&entity_type=${entityType}`}>`. Empty state: "No audit entries for this entity yet." Loading skeletons.
  - `<RelatedNotificationsPanel entityId />` calls `useNotifications({ resource_id: entityId, limit: 5 })`. Card with header "Notifications (count badge)", list of last 5 (channel icon + subject + timeAgo + status badge), "View all →" link to `/notifications?resource_id={entityId}`.
- **Tokens:** Design Token Map only. Use `<Card>`, `<Badge>`, `<Tabs>`, `<Skeleton>`.
- **Components:** Reuse `<Card>`, `<Badge>`, `<Button>`, `<Skeleton>`, `<EntityLink>`.
- **Note:** Invoke `frontend-design` skill.
- **Verify:** `pnpm -C web tsc --noEmit`; visually render against `/sims/:id` in dev.

### Task 4: Shared organism — `<RelatedAlertsPanel>` + `<RelatedViolationsTab>`
- **Files:** Create `web/src/components/shared/related-alerts-panel.tsx`, `web/src/components/shared/related-violations-tab.tsx`; modify `web/src/components/shared/index.ts`
- **Depends on:** Task 2
- **Complexity:** medium
- **Pattern ref:** Read `web/src/pages/alerts/index.tsx` for alert card layout (severity variants, runbook dropdown, ack button) and `web/src/pages/violations/index.tsx` for violation row + action menu.
- **Context refs:** "Components Involved", "API Specifications", "Design Token Map"
- **What:**
  - `<RelatedAlertsPanel entityId entityType />` queries anomalies scoped by entity (`sim_id` / `operator_id` / `apn_id` filter on `/api/v1/analytics/anomalies`). Shows open + last-7-day resolved in two tabs. Each row: severity badge, title, timeAgo, runbook `<Link>`, Ack button → mutation to anomaly `UpdateState`. Empty state handled.
  - `<RelatedViolationsTab entityId />` queries `/api/v1/policy-violations?sim_id=X` or `?policy_id=X` (branch on context — both needed; component accepts `scope: 'sim' | 'policy'`). Inline action menu per row: Suspend SIM, Review Policy, Dismiss (acknowledge existing endpoint), Escalate (remediate endpoint from Task 1). Confirmation dialog for Suspend + Escalate. Toast feedback.
- **Tokens:** Design Token Map only.
- **Components:** Reuse `<Card>`, `<Badge>`, `<Tabs>`, `<DropdownMenu>`, `<Dialog>`, `<Button>`, `<EntityLink>`.
- **Verify:** `pnpm -C web tsc --noEmit`. Grep for hex: 0.

### Task 5: Enrich SIM detail — extract tabs + add 8 new sections
- **Files:**
  - Create `web/src/pages/sims/_tabs/related-data-tab.tsx` (Audit / Notifications / Violations / Anomalies sub-tabs — composes Tasks 2-4 components)
  - Create `web/src/pages/sims/_tabs/policy-assignment-history-tab.tsx`
  - Create `web/src/pages/sims/_tabs/ip-history-tab.tsx`
  - Create `web/src/pages/sims/_tabs/cost-attribution-tab.tsx`
  - Modify `web/src/pages/sims/detail.tsx` (wire new tabs, thin it out by moving heavy inline content)
- **Depends on:** Tasks 2, 3, 4
- **Complexity:** high
- **Pattern ref:** Existing structure in `web/src/pages/sims/detail.tsx` (OverviewTab, HistoryTab functions) and `web/src/pages/sims/esim-tab.tsx` for extracted-file convention.
- **Context refs:** "Components Involved", "Acceptance Criteria Mapping" (AC-7), "Design Token Map"
- **What:** AC-7 in full — tabs added: Related (composite), Policy Assignments History (query `/api/v1/sims/:id/history` filtered to policy events + `/api/v1/sims/:id/policy-assignments` if exists; else reuse audit filter `entity_id=sim&action=sim.policy-assign`), IP Allocation History (query SIM history filtered to IP events or new `/api/v1/sims/:id/ip-history` — prefer existing history filter), Current Policy Rule Preview (client-side render of matching DSL rules by evaluating SIM attributes against `current_policy.dsl`; if too complex, link to policy editor with deep-link). Cost Attribution: use `/api/v1/analytics/cost?sim_id=X&period=30d` if available (extend hook; if missing, show "coming soon" with issue ref is NOT acceptable under zero-deferral — implement server aggregation via existing cdrStore if needed).
- **Tokens:** Design Token Map only.
- **Components:** Reuse `<Card>`, `<Tabs>`, `<Table>`, `<Sparkline>`, all shared components from T2-4.
- **Note:** Invoke `frontend-design` skill.
- **Verify:** `/sims/:id` renders all tabs in dev; no hex; `pnpm tsc --noEmit`.

### Task 6: Enrich APN detail (+4 views)
- **Files:** Modify `web/src/pages/apns/detail.tsx`; optionally create `web/src/pages/apns/_tabs/*.tsx` partials if page exceeds 45K
- **Depends on:** Tasks 2, 3
- **Complexity:** medium
- **Pattern ref:** `web/src/pages/apns/detail.tsx` existing tabs.
- **Context refs:** "Components Involved", "Acceptance Criteria Mapping" (AC-8)
- **What:** AC-8 — Policies Referencing (list policies whose DSL contains this APN — query `/api/v1/policies?references_apn_id=X`; if unsupported, client filter over recent policies and cache), Related Audit (via RelatedAuditTab), CDR Aggregate (reuse `/api/v1/apns/:id/traffic` from `use-apn-traffic.ts`; extend with top-N SIMs via `/api/v1/analytics/usage?apn_id=X&group_by=sim&limit=10` or existing aggregate endpoint), Operators Hosting (list from APN.operator_id + grants).
- **Tokens:** Design Token Map.
- **Components:** Reuse `<Card>`, `<Table>`, `<EntityLink>`, `<RelatedAuditTab>`.
- **Verify:** `/apns/:id` renders; tsc passes.

### Task 7: Enrich Operator detail (+6 views)
- **Files:** Modify `web/src/pages/operators/detail.tsx`; create `web/src/pages/operators/_tabs/sims-tab.tsx`, `web/src/pages/operators/_tabs/sla-summary-tab.tsx`
- **Depends on:** Tasks 2, 3, 4
- **Complexity:** high
- **Pattern ref:** `web/src/pages/operators/detail.tsx`.
- **Context refs:** "Components Involved", "Acceptance Criteria Mapping" (AC-9)
- **What:** AC-9 — Connected SIMs Count + State Breakdown (pie chart via recharts; paginated list via `/api/v1/sims?operator_id=X`), Hosted APNs list (`/api/v1/apns?operator_id=X`), SLA Report Summary (reuse STORY-063 `/api/v1/reports/sla-summary?operator_id=X`), Related Audit, Tenant Grants (`/api/v1/operator-grants?operator_id=X` — includes SoR priority), Active Alerts (via RelatedAlertsPanel), Cost Per Unit Summary.
- **Tokens / Components:** Design Token Map; reuse all shared + recharts `PieChart`.
- **Verify:** `/operators/:id` renders; tsc passes.

### Task 8: Enrich Policy detail (+6 views)
- **Files:** Modify `web/src/pages/policies/editor.tsx`; create `web/src/pages/policies/_tabs/assigned-sims-tab.tsx`, `web/src/pages/policies/_tabs/assignment-history-tab.tsx`
- **Depends on:** Tasks 2, 3, 4
- **Complexity:** high
- **Pattern ref:** `web/src/pages/policies/editor.tsx`.
- **Context refs:** AC-10
- **What:** AC-10 — Assigned SIMs Count + Paginated List (query `/api/v1/sims?policy_version_id=X` grouped by segment), Assignment History Timeline (audit filter `entity_type=policy&action=policy.activate`), Violations By This Policy (via RelatedViolationsTab scope=policy), Related Segments (`/api/v1/sim-segments?policy_version_id=X` or client derivation), Related Audit, Clone button (POST new policy with current DSL body), Export button (download JSON + DSL text as two files via Blob).
- **Verify:** Clone produces new policy row; export files download; tsc passes.

### Task 9: New Session Detail page (`/sessions/:id`)
- **Files:** Create `web/src/pages/sessions/detail.tsx`; modify `web/src/hooks/use-sessions.ts` (add `useSession(id)` + `useDisconnectSession`); modify `web/src/router.tsx` (add route)
- **Depends on:** Task 1 (BE), Tasks 2, 3, 4
- **Complexity:** high
- **Pattern ref:** `web/src/pages/sims/detail.tsx` header/breadcrumb/tabs shape.
- **Context refs:** "Components Involved", "API Specifications", "Screen Mockups", "Acceptance Criteria Mapping" (AC-11)
- **What:** AC-11 in full. Header: pulsing status dot + timer + identity chips (`<EntityLink>` for SIM / operator / APN). Overview card (NAS IP, RAT `<RATBadge>`, proto, allocated IP `<CopyableId>`, started_at, duration live counter via `useEffect` + `setInterval`, bytes in/out). SoR Decision card: ranked operator list from `sor_decision.scoring` — chosen highlighted, reasons bullet list. Policy Applied card: policy name `<EntityLink>`, matched rules as code snippet. Quota Usage bar (progress + percentage, color by pct). Force Disconnect button (active-only; calls `POST /sessions/{id}/disconnect` with reason dialog). Tabs: Related Sessions (same SIM, last 10), CDRs, Anomaly Flags (via RelatedAlertsPanel), Audit. Not-found: 404 state.
- **Tokens / Components:** Design Token Map; `<Breadcrumb>`, `<Card>`, `<Tabs>`, `<Badge>`, `<RATBadge>`, `<Dialog>`, `<Button>`, `<EntityLink>`, `<CopyableId>`, `<RelatedAuditTab>`, `<RelatedAlertsPanel>`.
- **Note:** Invoke `frontend-design` skill.
- **Verify:** `/sessions/:id` end-to-end; grep hex: 0; tsc passes.

### Task 10: New User Detail page (`/settings/users/:id`)
- **Files:** Create `web/src/pages/settings/user-detail.tsx`; modify `web/src/hooks/use-settings.ts` (add `useUser(id)`, `useUserActivity(id)`, `useUserSessions(id)`); modify `web/src/router.tsx`
- **Depends on:** Task 1 (BE), Tasks 2, 3
- **Complexity:** high
- **Pattern ref:** `web/src/pages/sims/detail.tsx` for layout, `web/src/pages/settings/users.tsx` for existing user actions (unlock, reset password, revoke sessions).
- **Context refs:** AC-12, "API Specifications"
- **What:** AC-12 in full. Header identity card + actions (unlock / reset password / force-logout all sessions). Tabs: Activity Timeline (uses new `GET /users/:id/activity` — renders like audit but scoped), API Keys (reuse `useApiKeys` filtered by owner), Active Sessions (`GET /users/:id/sessions` with IP/device/last_active; per-row Revoke button via existing `POST /auth/sessions/:id/revoke`), Permissions Matrix (derive from role — render grid of resources × actions using role-permissions map from `useSettings`; role definitions already seeded by STORY-068), Notifications sent to user (via RelatedNotificationsPanel with filter `recipient_user_id`), Account Events (filter activity by `action IN ('user.login','user.lockout','user.unlock','user.password-reset','user.2fa-setup')`), 2FA State card (totp_enabled, backup_codes_remaining).
- **Tokens / Components:** Design Token Map; all shared components.
- **Note:** Invoke `frontend-design` skill.
- **Verify:** `/settings/users/:id` loads for any tenant user; tsc passes.

### Task 11: New Alert Detail page (`/alerts/:id`)
- **Files:** Create `web/src/pages/alerts/detail.tsx`; modify `web/src/hooks/use-analytics.ts` or create `web/src/hooks/use-alert-detail.ts` (`useAlert(id)`, `useAlertComments(id)`, mutations for ack/resolve/escalate + comment); modify `web/src/router.tsx`
- **Depends on:** Tasks 2, 3 (Task 1 NOT required — anomaly Get/UpdateState exists)
- **Complexity:** high
- **Pattern ref:** `web/src/pages/alerts/index.tsx` for severity/runbook styling; `web/src/pages/alerts/_partials/comment-thread.tsx`, `alert-actions.tsx`.
- **Context refs:** AC-13
- **What:** AC-13 in full. Header: type, severity pill, triggered_at, resource `<EntityLink>`. State transitions timeline (from anomaly status history — existing anomaly schema has state transitions). Action bar: Ack / Resolve / Escalate — each opens `<Dialog>` with note field, calls `PATCH /analytics/anomalies/:id` with new state + note; Escalate additionally calls `POST /analytics/anomalies/:id/escalate` (exists at router line 816). Comments thread reuses existing `_partials/comment-thread.tsx` if generic enough (else extract to shared). Runbook link (from RUNBOOKS table already present in alerts/index). Related SIMs/Operators/Policies Affected (from anomaly.details). Similar Alerts (query anomalies by type + past 30d). Resolution Actions (context-dependent buttons; when type=sim_cloning show Suspend SIM; type=operator_down show Open Operator Detail). All via `<EntityLink>`.
- **Tokens / Components:** Design Token Map; shared.
- **Note:** Invoke `frontend-design` skill.
- **Verify:** `/alerts/:id`; tsc passes.

### Task 12: New Violation Detail page (`/violations/:id`)
- **Files:** Create `web/src/pages/violations/detail.tsx`; create `web/src/hooks/use-violation-detail.ts` (`useViolation(id)`, `useRemediate(id)`); modify `web/src/router.tsx`
- **Depends on:** Task 1 (BE — Get + Remediate), Tasks 2, 3
- **Complexity:** high
- **Pattern ref:** `web/src/pages/alerts/detail.tsx` (Task 11 layout).
- **Context refs:** AC-14, "Data Flow > Violation remediation"
- **What:** AC-14 in full. Header: type, severity, SIM `<EntityLink>`, Policy `<EntityLink>`, rule (rule_index + rule text from policy version), Session `<EntityLink>`, Occurred At. State: open/acknowledged/remediated/dismissed. Remediation Actions bar with confirmation dialogs:
  - Suspend SIM → `POST /policy-violations/:id/remediate {action:"suspend_sim", reason}` → success toast + refetch.
  - Review Policy → `navigate(/policies/:policy_id?version=X&highlight_rule=N)` (deep link).
  - Dismiss → `POST /policy-violations/:id/acknowledge {reason}` (existing endpoint).
  - Escalate → `POST /policy-violations/:id/remediate {action:"escalate", reason}` → creates incident notification.
  Related Violations (same SIM/policy/rule past 30d). Timeline (occurred_at → acknowledged_at → remediated_at with actor).
- **Tokens / Components:** Design Token Map; shared + `<Dialog>`.
- **Note:** Invoke `frontend-design` skill.
- **Verify:** `/violations/:id`; suspend action updates SIM state; tsc passes.

### Task 13: New Tenant Detail page (`/system/tenants/:id`, super_admin)
- **Files:** Create `web/src/pages/system/tenant-detail.tsx`; create `web/src/hooks/use-tenant-detail.ts` (`useTenant(id)`, `useTenantStats(id)`); modify `web/src/router.tsx`
- **Depends on:** Tasks 2, 3, 4
- **Complexity:** high
- **Pattern ref:** `web/src/pages/system/tenants.tsx` (list), `web/src/pages/system/health.tsx`.
- **Context refs:** AC-15
- **What:** AC-15 in full. Header: tenant name, slug, state, created_at. Six dashboard cards with `<AnimatedCounter>`: SIM count, APN count, User count, Operator count, Active Sessions, Monthly Cost, Storage Used. Quota utilization bars (quota limit vs. used from `/tenants/:id/stats`). Live Traffic sparkline (`<Sparkline>` over last 60 min from `/tenants/:id/stats?timeframe=1h`). Recent Audit (RelatedAuditTab with `entity_type=tenant` filter). Active Alerts (RelatedAlertsPanel). SLA Compliance summary (from STORY-063). Connected Operators list (tenant grants) with health status chips. Role gate: only super_admin can access — use existing `ProtectedRoute` with role check (pattern from `system/tenants.tsx`).
- **Tokens / Components:** Design Token Map; `<Card>`, `<AnimatedCounter>`, `<Sparkline>`, all shared.
- **Note:** Invoke `frontend-design` skill.
- **Verify:** `/system/tenants/:id` loads as super_admin; 403 for others; tsc passes.

### Task 14: Audit Log entity linking (AC-16) + Router wiring + regression tests
- **Files:**
  - Modify `web/src/pages/audit/index.tsx` (render `entity_id` column via `<EntityLink entityType={row.entity_type} entityId={row.entity_id} />`, `actor` column via `<EntityLink entityType="user" entityId={row.actor_user_id} label={row.actor_email} />`, JSON diff panel key labels via small map per entity_type)
  - Confirm all five new routes already added in Tasks 9-13 via `router.tsx`; if not, add here
  - Create `web/src/__tests__/shared/entity-link.test.tsx`, `web/src/__tests__/shared/copyable-id.test.tsx`, `web/src/__tests__/shared/related-audit-tab.test.tsx`
  - Add backend tests in `internal/api/violation/handler_test.go` (remediate actions: suspend_sim happy path, escalate, dismiss, unknown action 400, cross-tenant 404) and `internal/api/session/handler_test.go` (Get happy + 404), `internal/api/user/handler_test.go` (Get + Activity)
- **Depends on:** Tasks 1–13
- **Complexity:** high
- **Pattern ref:** Existing test files `internal/api/tenant/handler_test.go`, `internal/api/session/handler_test.go` (5.6K), `web/src/__tests__` (check existing Vitest setup).
- **Context refs:** "Acceptance Criteria Mapping", "API Specifications"
- **What:** AC-16 + comprehensive unit/integration coverage.
- **Verify:** `go test ./internal/api/...`, `pnpm -C web test`, `pnpm -C web tsc --noEmit`, full `make test`. E2E scenarios from story §Test Scenarios (audit click → navigate, copy ICCID → clipboard, suspend SIM from violation → state change + audit row) validated manually in dev.

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| AC-1 RelatedAuditTab | Task 3 | Task 14 unit test |
| AC-2 RelatedNotificationsPanel | Task 3 | Task 14 unit test |
| AC-3 RelatedAlertsPanel | Task 4 | Task 14 + manual E2E on SIM detail |
| AC-4 RelatedViolationsTab | Task 4 | Task 14 + manual E2E on SIM/Policy detail |
| AC-5 EntityLink | Task 2 | Task 14 unit test |
| AC-6 CopyableId | Task 2 | Task 14 unit test + E2E clipboard |
| AC-7 SIM detail +8 views | Task 5 | Task 14 + manual E2E |
| AC-8 APN detail +4 views | Task 6 | Manual E2E |
| AC-9 Operator detail +6 views | Task 7 | Manual E2E |
| AC-10 Policy detail +6 views | Task 8 | Manual E2E |
| AC-11 Session detail page | Tasks 1 + 9 | Task 14 BE test + manual E2E |
| AC-12 User detail page | Tasks 1 + 10 | Task 14 BE test + manual E2E |
| AC-13 Alert detail page | Task 11 | Manual E2E |
| AC-14 Violation detail page | Tasks 1 + 12 | Task 14 BE test (remediate) + manual E2E suspend flow |
| AC-15 Tenant detail page | Task 13 | Manual E2E as super_admin |
| AC-16 Audit log linking | Task 14 | Task 14 + manual E2E |

## Story-Specific Compliance Rules

- **API:** All five new endpoints use standard envelope `{status,data,meta?}` / error envelope. Tenant context enforced via existing `apierr.TenantIDKey` middleware. RBAC: session/violation/user reads allowed to tenant_admin+; violation remediation requires `policies.manage` or `sims.manage` permission (super_admin bypasses).
- **DB:** No new tables. Remediation writes go through existing stores + `audit.Auditor`. Tenant scope enforced by store layer.
- **UI:** Tokens ONLY from Design Token Map. Every clickable entity ID uses `<EntityLink>`; every copyable ID uses `<CopyableId>`. Every new detail page has breadcrumb, loading skeleton, 404 state, and empty states for every sub-tab. Dark theme default per FRONTEND.md. No raw `<table>`/`<button>`/`<input>` in any new file.
- **Business (PRODUCT.md):** Violation remediate → suspend SIM follows existing state transition rules (active → suspended allowed, suspended/terminated/stolen_lost rejected with 409). Escalate creates high-severity notification targeting on-call recipients. All state changes emit audit rows with actor_user_id + reason.
- **ADR:** ADR-002 JWT auth respected via existing middleware; no bypass. ADR-003 bcrypt untouched (no auth code modified).

## Bug Pattern Warnings

- PAT-001 (BR test drift): Not applicable — no business-rule state machine changes beyond reusing existing SIM state transitions already covered by BR tests.
- PAT-002 (duplicated utils): When adding SoR decision display, DO NOT duplicate existing IP/host parsing utilities. Reuse any existing helpers in `internal/net/` if present.
- PAT-003 (EAP MAC): N/A (no auth protocol work).
- **Story-specific pattern risk:** Multiple shared components will render hundreds of `<EntityLink>`s per page — use `React.memo` on `<EntityLink>` and `<CopyableId>` to avoid re-render storms. Audit/Notifications/Violations lists must use stable keys (entry.id). Verify with React DevTools Profiler before sign-off.

## Tech Debt (from ROUTEMAP)

No tech debt items currently target STORY-075. D-001, D-002, D-006 target STORY-077. D-003 targets STORY-062.

## Mock Retirement

No `web/src/mocks/` directory exists in this project. No mock retirement required.

## Risks & Mitigations

- **Risk:** SIM detail page exceeds maintainable size after enrichment (currently 42K, adding 8 sections). **Mitigation:** Task 5 extracts each new tab into `web/src/pages/sims/_tabs/*.tsx` partials (≤400 lines each), keeping `detail.tsx` as a thin composition root.
- **Risk:** `sor_decision` / `policy_applied` telemetry not yet persisted on session records. **Mitigation:** Task 1 extends session DTO with optional zero-value fields; FE renders "decision data unavailable" placeholder if absent. STORY-065 metrics ensure data exists for new sessions; historical sessions tolerate missing fields. This is NOT a deferral — all new sessions have the data; legacy sessions show a labelled empty-state.
- **Risk:** Backend tests for remediate flow could regress existing SIM state machine. **Mitigation:** Task 1 reuses `simStore.ChangeState` (same code path as `/sims/:id/suspend`), so existing state-machine tests still cover the transition. Task 14 adds remediate-specific integration test asserting audit row + state + violation ack all happen atomically (single transaction).
- **Risk:** Permissions matrix on user detail uses role-static mapping — if STORY-068 stored matrix per-tenant, client would need API. **Mitigation:** Task 10 first reads existing `use-settings.ts` for permissions shape; if per-tenant override exists, swap to API call (no additional endpoint needed — STORY-068 already exposes).
- **Risk:** Policies Referencing lookup (AC-8) could require a DSL-text LIKE scan on every APN detail load. **Mitigation:** Implement as server-side search via existing `policyStore.List` with a new optional `references_apn_id` filter that performs a case-insensitive SUBSTRING match on `dsl_text` (add GIN index `CREATE INDEX IF NOT EXISTS idx_policies_dsl_gin ON policies USING gin (dsl_text gin_trgm_ops)` in a new migration **only if** scan perf warrants; first pass: accept seq scan on small policy counts, add index later if audit flags it).

## Waves (Execution Plan)

- **Wave 1 (parallel, no deps):** Task 1, Task 2
- **Wave 2 (parallel, depends on Task 2):** Task 3, Task 4
- **Wave 3 (parallel, depends on Tasks 2-4):** Task 5, Task 6, Task 7, Task 8, Task 11, Task 13
- **Wave 4 (parallel, depends on Task 1 + Tasks 2-4):** Task 9, Task 10, Task 12
- **Wave 5 (final):** Task 14

Wave 3 contains 6 parallel FE tasks — orchestrator may choose to split into two sub-waves (3a: enrichments 5-8; 3b: new pages 11, 13) depending on dispatcher concurrency limits.

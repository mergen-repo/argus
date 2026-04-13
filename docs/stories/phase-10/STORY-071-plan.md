# Implementation Plan: STORY-071 — Roaming Agreement Management

## Goal
Deliver full CRUD lifecycle for roaming agreements — entity table (SLA terms, cost terms, validity window), tenant-scoped REST API, SoR engine consultation (override operator cost with agreement cost), renewal notifications (30 day horizon), and UI (list + detail + operator-detail tab).

## Scope Summary
- Source: PRODUCT.md F-072 Layer 3 (in-scope, zero implementation). STORY file `docs/stories/phase-10/STORY-071-roaming-agreements.md`.
- Dependencies: STORY-063 DONE — notification channels (`internal/notification.Service`) + in-app store.
- Size: M (5 ACs). No phased rollout — zero-deferral mandate.

## Architecture Context

### Components Involved
| Component | Layer | Responsibility | Path |
|-----------|-------|----------------|------|
| `roaming_agreements` table | DB | entity storage with JSONB terms | `migrations/20260414000001_roaming_agreements.{up,down}.sql` |
| `RoamingAgreementStore` | Store | DB access, tenant RLS enforcement | `internal/store/roaming_agreement.go` |
| `internal/api/roaming` | API | CRUD handler (chi routes) | `internal/api/roaming/handler.go` |
| Gateway router | API | mount routes with RBAC | `internal/gateway/router.go` (edit) |
| `internal/operator/sor` engine | Domain | consult active agreement → override cost | `internal/operator/sor/engine.go` (edit) + `internal/operator/sor/types.go` (edit) |
| `RoamingRenewalSweeper` job | Job | cron-driven expiry check + notification dispatch | `internal/job/roaming_renewal.go` |
| Notification dispatch | Notification | email + in-app fan-out | reuse `internal/notification.Service` (`Enqueue` for email; `inApp.CreateNotification`) |
| `cmd/argus/main.go` | Entry | wire store/handler/processor/cron | edit |
| `internal/config/config.go` | Config | `ROAMING_RENEWAL_ALERT_DAYS`, `ROAMING_RENEWAL_CRON` | edit |
| FE list page | UI | SCR-150 | `web/src/pages/roaming/index.tsx` |
| FE detail page | UI | SCR-151 | `web/src/pages/roaming/detail.tsx` |
| FE operator-detail Agreements tab | UI | SCR-041 partial | `web/src/pages/operators/detail.tsx` (edit) |
| FE hook | UI | TanStack query adapter | `web/src/hooks/use-roaming-agreements.ts` |
| FE type | UI | TS type | `web/src/types/roaming.ts` |
| FE router | UI | route registration | `web/src/router.tsx` (edit) |
| FE sidebar | UI | "Roaming" under OPERATIONS | `web/src/components/layout/sidebar.tsx` (edit) |

### Data Flow
Create: UI form → `POST /api/v1/roaming-agreements` → handler validates (FK operator, dates, state enum) → `RoamingAgreementStore.Create` → audit log → 201 JSON envelope.

SoR consultation: `sor.Engine.Evaluate` → `ListGrantsWithOperators(tenantID)` (existing) → NEW: `ListActiveAgreementsByOperators(tenantID, operatorIDs)` lookup → for each candidate, if an active agreement exists for that operator whose `[start_date, end_date]` covers `time.Now()` → override `CandidateOperator.CostPerMB` with `cost_terms.cost_per_mb`; if expired agreement exists with no active replacement, log `warn` and keep operator default (`ReasonDefault` unchanged).

Renewal job: cron `@daily 06:00` fires `JobTypeRoamingRenewal` → `RoamingRenewalSweeper.Process` scans across tenants for agreements where `end_date BETWEEN now AND now + interval 'N days'` AND state='active' → per tenant admin (role=`tenant_admin`), produce `notification.AlertPayload{AlertType: "roaming.agreement.renewal_due", Severity: "warning", EntityType: "roaming_agreement", EntityID: agreement.id, Metadata: {operator, days_to_expiry, end_date}}` → route via `Service.HandleAlert` (email + in-app channels). Dedup via Redis key `argus:roaming:renewal:{agreement_id}:{YYYY-MM}` so repeat ticks within the alert window don't re-notify each day.

### API Specifications
All responses use standard envelope `{status, data, meta?, error?}`. Cursor pagination for list.

- `POST /api/v1/roaming-agreements` (role `operator_manager` or `tenant_admin`)
  - Body: `{operator_id: uuid, partner_operator_name: string, agreement_type: "national"|"international"|"MVNO", sla_terms: {uptime_pct: number, latency_p95_ms: number, max_incidents: int}, cost_terms: {cost_per_mb: number, currency: string (ISO 4217), volume_tiers?: [{threshold_mb: int, cost_per_mb: number}], settlement_period: "monthly"|"quarterly"|"annual"}, start_date: ISO date, end_date: ISO date, auto_renew: bool}`
  - 201 → `{status: "success", data: <agreement>}`; 400 on validation; 409 on overlap (same operator+tenant with overlapping active window); audit entry `roaming_agreement.create`.
- `GET /api/v1/roaming-agreements` (role `operator_manager`, `tenant_admin`, `api_user` for read)
  - Query: `?limit=50&cursor=<b64>&operator_id=<uuid>&state=active&expiring_within_days=30`
  - 200 → `{status: "success", data: [agreement], meta: {next_cursor, count}}`.
- `GET /api/v1/roaming-agreements/{id}` — 200 or 404.
- `PATCH /api/v1/roaming-agreements/{id}` — partial update; recomputes state if `end_date` past; audit `roaming_agreement.update` (before/after diff).
- `DELETE /api/v1/roaming-agreements/{id}` — soft delete (`state="terminated"`, `terminated_at`); audit `roaming_agreement.terminate`.
- `GET /api/v1/operators/{id}/roaming-agreements` — convenience listing for operator-detail tab; same shape as list.

Validation rules:
- `end_date > start_date`
- `agreement_type ∈ {national, international, MVNO}`
- `state ∈ {draft, active, expired, terminated}` (CHECK constraint)
- `cost_terms.cost_per_mb ≥ 0`; currency must be 3-letter uppercase
- `operator_id` must reference existing operator AND operator must be granted to tenant via `operator_grants` (409 `operator_not_granted` otherwise)

Error codes (add to `internal/apierr`): `roaming_agreement_not_found`, `roaming_agreement_overlap`, `roaming_agreement_invalid_dates`, `roaming_agreement_operator_not_granted`.

### Database Schema

Source: ARCHITECTURE.md (NEW table — no prior migration). FK to existing `operators(id)` (see `migrations/20260320000002_core_schema.up.sql` lines 124-136 for operator_grants pattern). Tenant RLS follows the pattern in `migrations/20260412000006_rls_policies.up.sql`.

```sql
-- Source: ARCHITECTURE.md (new table — STORY-071 is first author)
CREATE TABLE IF NOT EXISTS roaming_agreements (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    operator_id UUID NOT NULL REFERENCES operators(id),
    partner_operator_name VARCHAR(200) NOT NULL,
    agreement_type VARCHAR(20) NOT NULL,
    sla_terms JSONB NOT NULL DEFAULT '{}'::jsonb,
    cost_terms JSONB NOT NULL DEFAULT '{}'::jsonb,
    start_date DATE NOT NULL,
    end_date DATE NOT NULL,
    auto_renew BOOLEAN NOT NULL DEFAULT false,
    state VARCHAR(20) NOT NULL DEFAULT 'draft',
    notes TEXT,
    terminated_at TIMESTAMPTZ,
    created_by UUID REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT roaming_agreements_type_chk CHECK (agreement_type IN ('national','international','MVNO')),
    CONSTRAINT roaming_agreements_state_chk CHECK (state IN ('draft','active','expired','terminated')),
    CONSTRAINT roaming_agreements_dates_chk CHECK (end_date > start_date)
);

CREATE INDEX IF NOT EXISTS idx_roaming_agreements_tenant ON roaming_agreements (tenant_id);
CREATE INDEX IF NOT EXISTS idx_roaming_agreements_tenant_op ON roaming_agreements (tenant_id, operator_id);
CREATE INDEX IF NOT EXISTS idx_roaming_agreements_tenant_state ON roaming_agreements (tenant_id, state);
CREATE INDEX IF NOT EXISTS idx_roaming_agreements_expiry ON roaming_agreements (tenant_id, end_date) WHERE state = 'active';
CREATE UNIQUE INDEX IF NOT EXISTS idx_roaming_agreements_active_unique
  ON roaming_agreements (tenant_id, operator_id)
  WHERE state = 'active';  -- prevents two simultaneously-active agreements per operator

-- RLS
ALTER TABLE roaming_agreements ENABLE ROW LEVEL SECURITY;
ALTER TABLE roaming_agreements FORCE ROW LEVEL SECURITY;
CREATE POLICY roaming_agreements_tenant_isolation ON roaming_agreements
  USING (tenant_id = current_setting('app.current_tenant', true)::uuid);
```

Down migration: `DROP POLICY`, `DROP TABLE IF EXISTS roaming_agreements CASCADE;`.

### SoR Engine Integration Spec

Extend `internal/operator/sor`:

1. Add provider interface beside `GrantProvider`:
```go
type RoamingAgreementProvider interface {
    ListActiveByTenant(ctx context.Context, tenantID uuid.UUID, now time.Time) ([]store.RoamingAgreement, error)
    ListRecentlyExpiredByTenant(ctx context.Context, tenantID uuid.UUID, now time.Time, lookback time.Duration) ([]store.RoamingAgreement, error)
}
```
2. `Engine` gains optional `agreementProvider` (nil-safe — if nil, behavior unchanged).
3. In `Evaluate`, after `buildCandidates`, call `agreementProvider.ListActiveByTenant`. Build `map[operatorID]*Agreement`.
4. For each candidate: if active agreement found → override `CostPerMB` with `agreement.CostTerms.CostPerMB`; attach `decision.Metadata["roaming_agreement_id"] = agreement.ID` (extend SoRDecision with `AgreementID *uuid.UUID` field).
5. Also call `ListRecentlyExpiredByTenant(tenantID, now, 7*24h)` — for each recently-expired agreement whose operator is still a candidate and has no active successor → `logger.Warn().Msg("roaming agreement expired — falling back to operator default cost")`. No decision change (AC-3).
6. New `ReasonRoamingAgreement = "roaming_agreement_applied"` — set when cost override occurred AND decision would otherwise have been `ReasonDefault`/`ReasonCostOptimized` (preserve IMSI/RAT reasons).

### Notification Spec (Renewal Alert)

Reuse existing `notification.Service` via `HandleAlert(ctx, AlertPayload)`. Wrapper `internal/notification/roaming_renewal.go`:

- Input: tenant admins list (from `UserStore.ListByRole(tenantID, "tenant_admin")`), agreement, daysToExpiry.
- Payload:
  - `AlertID`: `"roaming-renewal-" + agreement.ID.String() + "-" + endDate.Format("2006-01")`
  - `AlertType`: `"roaming.agreement.renewal_due"`
  - `Severity`: `"warning"` (if ≤7 days → `"critical"`)
  - `Title`: `"Roaming agreement expiring in {days} days"`
  - `Description`: `"Agreement with {partner_operator_name} expires on {end_date}. Review terms and renew if needed."`
  - `EntityType`: `"roaming_agreement"`, `EntityID`: agreement.ID
  - `Metadata`: `{operator_id, partner_operator_name, end_date, days_to_expiry, auto_renew}`

Channels: email (via SMTP sender), in-app (via `inApp.CreateNotification`). SMS NOT used (too noisy for contractual reminders — out of scope). Telegram only if tenant preference opts in (handled by existing `Service.HandleAlert` routing — no new code).

### Cron Job Spec

- New `JobTypeRoamingRenewal = "roaming_renewal_sweep"` in `internal/job/types.go`.
- Processor `RoamingRenewalSweeper` in `internal/job/roaming_renewal.go` — follows pattern from `internal/job/scheduled_report.go` (`Type() string`, `Process(ctx, job *store.Job) error`).
- Dedup: Redis `SETNX argus:roaming:renewal:{agreement_id}:{end_date YYYY-MM} TTL=35d` → only fires once per month per agreement even if job runs daily.
- Config: `ROAMING_RENEWAL_ALERT_DAYS` (default `30`), `ROAMING_RENEWAL_CRON` (default `"0 6 * * *"`).
- Register in `cmd/argus/main.go` alongside `kvkk_purge_daily` et al.

### Screen Specifications

#### SCR-150 — Roaming Agreements List (`/roaming-agreements`)
```
┌────────────────────────────────────────────────────────────────────────┐
│  Roaming Agreements                              [+ New Agreement]     │
├────────────────────────────────────────────────────────────────────────┤
│  [Operator ▾]  [State ▾]  [Expiring in: 30d ▾]  [Search...]           │
├────────────────────────────────────────────────────────────────────────┤
│  Partner           Operator    Type          State     End Date   ⟳   │
│  Vodafone Roam     Turkcell    international [active]  2026-08-12     │
│  MVNO-Go           T2          MVNO          [draft]   2027-01-01     │
│  DT Global         T3          international [expired] 2026-03-01  ⚠  │
│  ...                                                                   │
│                                                        [Load more]     │
└────────────────────────────────────────────────────────────────────────┘
```
- Navigation: sidebar "OPERATIONS" → "Roaming Agreements".
- Drill-down: row click → SCR-151 (`/roaming-agreements/{id}`); operator cell click → operator detail (`/operators/{id}`).
- Empty state: "No agreements yet. Create one to customize SoR cost routing."
- Loading: skeleton rows.
- Error: inline alert with retry.
- Expiring soon badge: amber `<30 days`, red `<7 days`.

#### SCR-151 — Roaming Agreement Detail (`/roaming-agreements/{id}`)
```
┌────────────────────────────────────────────────────────────────────────┐
│  ← Back     Vodafone Roam                     [Edit] [Terminate]      │
│  State: [active]   Type: international   Auto-renew: on               │
├────────────────────────────────────────────────────────────────────────┤
│  Operator: Turkcell  →                                                 │
│  Validity: 2026-01-12 → 2026-08-12   (expiring in 121 days)           │
├──────────────────┬─────────────────────────────────────────────────────┤
│  SLA Terms       │  Cost Terms                                         │
│  Uptime: 99.9%   │  Base: 0.012 USD/MB                                 │
│  Latency p95:80  │  Tiers: >100GB → 0.010 USD/MB                       │
│  Max incidents:3 │  Settlement: monthly                                │
├──────────────────┴─────────────────────────────────────────────────────┤
│  Validity Timeline   [═══════════════▓▓▓▓▓░░░░]                        │
│  Notes / Attachments                                                    │
└────────────────────────────────────────────────────────────────────────┘
```
- Edit → SlidePanel (shadcn `Sheet`) with form + JSONB editors (structured inputs, not raw textarea).
- Terminate → confirm Dialog.
- Validity timeline: horizontal bar; today marker.

#### SCR-041 partial — Operator Detail Agreements tab
- New tab `"Agreements"` in existing `/operators/{id}` page.
- Embedded mini-list (same columns as SCR-150 but scoped to this operator), plus "+ New Agreement (this operator)" button → prefills operator_id.

### Design Token Map

#### Color Tokens (from FRONTEND.md)
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Page background | `bg-bg-primary` | `bg-white`, `bg-gray-50` |
| Card/panel bg | `bg-bg-surface` | `bg-white` |
| Elevated surface (modals, dropdowns) | `bg-bg-elevated` | `bg-gray-100` |
| Hover row | `hover:bg-bg-hover` | `hover:bg-gray-100` |
| Active row | `bg-bg-active` | `bg-blue-50` |
| Primary text | `text-text-primary` | `text-[#E4E4ED]`, `text-gray-900` |
| Secondary text / labels | `text-text-secondary` | `text-[#7A7A95]`, `text-gray-500` |
| Muted / placeholder | `text-text-tertiary` | `text-gray-400` |
| Accent (CTA, link, active tab) | `text-accent` / `bg-accent` | `text-cyan-500`, `#00D4FF` |
| Accent dim (active nav pill) | `bg-accent-dim` | any rgba literal |
| Success (active state badge) | `text-success` / `bg-success-dim` | `text-green-500` |
| Warning (expiring <30d, draft) | `text-warning` / `bg-warning-dim` | `text-yellow-500` |
| Danger (expired, terminated) | `text-danger` / `bg-danger-dim` | `text-red-500` |
| Purple (MVNO badge) | `text-purple` / `bg-purple-dim` | `text-purple-500` |
| Border | `border-border` | `border-gray-200` |
| Subtle divider | `border-border-subtle` | `divide-gray-100` |

#### Typography Tokens
| Usage | Token Class |
|-------|-------------|
| Page title | `text-[22px] font-semibold text-text-primary` (follow `operators/index.tsx` pattern) |
| Section title | `text-[15px] font-semibold text-text-primary` |
| Body | `text-sm text-text-secondary` |
| Caption / label | `text-xs uppercase tracking-wider text-text-tertiary` |

#### Spacing & Elevation
- Card: `rounded-md border border-border bg-bg-surface p-4` (match operators page Card atom).
- Button: ALWAYS use `<Button>` from `@/components/ui/button`.
- Inputs: ALWAYS use `<Input>`, `<Select>`, `<Textarea>` from `@/components/ui/*`.

#### Components to REUSE (zero raw HTML)
| Component | Path | Use For |
|-----------|------|---------|
| `<Button>` | `web/src/components/ui/button.tsx` | all buttons |
| `<Input>` | `web/src/components/ui/input.tsx` | text/number/date fields |
| `<Select>` | `web/src/components/ui/select.tsx` | state, type, operator pickers |
| `<Textarea>` | `web/src/components/ui/textarea.tsx` | notes |
| `<Card>` | `web/src/components/ui/card.tsx` | panels |
| `<Badge>` | `web/src/components/ui/badge.tsx` | state + type badges |
| `<Dialog>` | `web/src/components/ui/dialog.tsx` | terminate confirm |
| `<SlidePanel>` / `<Sheet>` | `web/src/components/ui/slide-panel.tsx` + `sheet.tsx` | edit form |
| `<Skeleton>` | `web/src/components/ui/skeleton.tsx` | loading rows |
| `<Tabs>` | `web/src/components/ui/tabs.tsx` | operator-detail tab |
| `<TableToolbar>` | `web/src/components/ui/table-toolbar.tsx` | filters + search |
| `<Table>` | `web/src/components/ui/table.tsx` | list view |
| `<InfoRow>` | `web/src/components/ui/info-row.tsx` | detail field rows |
| lucide `Handshake`, `AlertTriangle`, `Calendar`, `RefreshCw` | `lucide-react` | icons |

## Prerequisites
- [x] STORY-063 DONE — notification service, email + in-app delivery.
- [x] STORY-068 DONE — RBAC `RequireRole("operator_manager")` middleware, tenant context set.
- [x] `operator_grants` table exists.
- [x] audit.Auditor service wired.

## Story-Specific Compliance Rules
- API: standard envelope; cursor-paginated list; 201 Created for POST; 204 No Content for DELETE soft-delete (or 200 with body — follow APN pattern: 200 + body).
- DB: RLS policy; FK to `operators(id)` and `users(id)`; partial unique index on active state.
- UI: zero hardcoded hex / zero raw `<input>`/`<button>`; use existing ui kit; sidebar entry under OPERATIONS.
- Audit: every mutation → `audit.Auditor.CreateEntry` with before/after JSON for PATCH, full snapshot for POST/DELETE.
- RBAC: write (POST/PATCH/DELETE) → `operator_manager` OR `tenant_admin`; read → `api_user`.
- ADR-001 (Go monolith): new package stays under `internal/`, no new service boundaries.
- PAT-001: tests assert BEHAVIOR (e.g. SoR cost is overridden), not just presence of code paths.
- PAT-002: do NOT duplicate `overlapCheck` logic — put it in store layer, handler calls it.

## Bug Pattern Warnings
- **PAT-001** [STORY-059]: Integration tests must verify behavioral outcomes (agreement cost appears in decision), not just that the code is reachable.
- **PAT-002** [STORY-059]: Overlap detection logic must live in a single location (`RoamingAgreementStore.checkOverlap`); handler must not re-implement it.
- **D-001/D-002** (OPEN, targeted at STORY-077): do NOT introduce new raw `<input>`/`<button>` — use shadcn atoms. We will NOT be in the remediation list.

## Tech Debt (from ROUTEMAP)
- None targeting STORY-071. D-001/D-002/D-003 target other stories — just avoid reintroducing their patterns.

## Mock Retirement
- None. No `web/src/mocks/` directory. Backend-first story.

## Tasks

### Task 1: DB migration — `roaming_agreements` table + RLS
- **Files:** Create `migrations/20260414000001_roaming_agreements.up.sql`, `migrations/20260414000001_roaming_agreements.down.sql`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `migrations/20260320000002_core_schema.up.sql` (operator_grants block, lines 124-136) and `migrations/20260412000006_rls_policies.up.sql` (tenant_isolation policy block, lines 22-61).
- **Context refs:** Database Schema
- **What:** Create table exactly as specified in Database Schema (all columns, constraints, indexes, RLS policy). Down migration drops policy then table CASCADE.
- **Verify:** `make db-migrate` applies cleanly; `make db-rollback` reverts cleanly; `\d roaming_agreements` shows all indexes + constraint names matching spec.

### Task 2: Store — `RoamingAgreementStore` (CRUD + queries)
- **Files:** Create `internal/store/roaming_agreement.go`, `internal/store/roaming_agreement_test.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/apn.go` (full file — CRUD, pagination, cursor encoding) and `internal/store/tenant.go` for scanRow pattern. Also `internal/store/operator.go` for JSONB column handling.
- **Context refs:** Database Schema, API Specifications (validation rules section — overlap check), SoR Engine Integration Spec (provider interface shape)
- **What:** Implement:
  - `type RoamingAgreement struct{...}` mirroring table columns; `SLATerms`, `CostTerms` as `json.RawMessage` + typed helper structs.
  - Methods: `Create`, `GetByID`, `List(ListFilter)`, `Update(UpdateParams)`, `Terminate(id) error`, `ListByOperator(tenantID, opID)`, `ListActiveByTenant(tenantID, now)`, `ListRecentlyExpiredByTenant(tenantID, now, lookback)`, `ListExpiringWithin(days int)` (cross-tenant for cron).
  - `checkOverlap(tenantID, operatorID, start, end, excludeID *uuid.UUID) error` → returns `ErrRoamingAgreementOverlap` when an active window intersects.
  - Errors: `ErrRoamingAgreementNotFound`, `ErrRoamingAgreementOverlap`.
  - Cursor pagination (base64 of `created_at|id`) — match APN store.
  - Table tests with `pgtestdb` (same harness as other `_test.go` files in this dir).
- **Verify:** `go test ./internal/store/ -run RoamingAgreement -v` → green.

### Task 3: API handler — CRUD endpoints + operator-scoped list
- **Files:** Create `internal/api/roaming/handler.go`, `internal/api/roaming/handler_test.go`; Edit `internal/apierr/errors.go` (add 4 error codes)
- **Depends on:** Task 2
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/apn/handler.go` (full CRUD pattern — request/response structs, validation, audit entries, cursor listing) and `internal/api/operator/handler.go` for grant validation.
- **Context refs:** API Specifications, Database Schema, Story-Specific Compliance Rules
- **What:**
  - `Handler` struct with `store *store.RoamingAgreementStore`, `operatorStore *store.OperatorStore`, `grantStore` (for ensure granted), `auditSvc audit.Auditor`, `logger`.
  - Methods: `List`, `Create`, `Get`, `Update`, `Terminate`, `ListForOperator` (handles `GET /api/v1/operators/{id}/roaming-agreements`).
  - Request/response DTOs; validate dates, agreement_type enum, currency 3-letter, cost_per_mb ≥0.
  - Overlap check via store `checkOverlap` — return 409 with code `roaming_agreement_overlap`.
  - Audit every mutation (`roaming_agreement.{create,update,terminate}`).
- **Verify:** `go test ./internal/api/roaming/... -v`; smoke via curl with JWT.

### Task 4: Route wiring + dependency injection
- **Files:** Edit `internal/gateway/router.go`, `cmd/argus/main.go`, `internal/gateway/router_test.go`
- **Depends on:** Task 3
- **Complexity:** low
- **Pattern ref:** Read `internal/gateway/router.go` lines 300-330 (operator + APN route groups — RBAC role groups).
- **Context refs:** API Specifications, Components Involved
- **What:**
  - In `main.go`: construct `store.NewRoamingAgreementStore(pg.Pool)`, `roaming.NewHandler(...)`; pass into `Deps`.
  - In `router.go`: add `Deps.RoamingHandler *roaming.Handler`; register route groups with `JWTAuth` + appropriate `RequireRole` (read group = `api_user`, write group = `operator_manager`); include `/api/v1/operators/{id}/roaming-agreements` under operator-handler block.
  - Smoke route test asserting RBAC 403 for unauthorized roles.
- **Verify:** `go test ./internal/gateway/... -run Router -v`; manual `curl` with valid JWT returns 200/201.

### Task 5: SoR engine — agreement consultation + expired fallback log
- **Files:** Edit `internal/operator/sor/engine.go`, `internal/operator/sor/types.go`, `internal/operator/sor/engine_test.go`; Edit `cmd/argus/main.go` (pass new provider)
- **Depends on:** Task 2
- **Complexity:** high
- **Pattern ref:** Read current `internal/operator/sor/engine.go` Evaluate (lines 54-160) and `engine_test.go` table-driven pattern.
- **Context refs:** SoR Engine Integration Spec, Data Flow
- **What:**
  - Add `RoamingAgreementProvider` interface + optional `agreementProvider` field (constructor remains backward compatible — nil-check at call site).
  - Add `ReasonRoamingAgreement` constant.
  - In `Evaluate`, after `buildCandidates`: if provider is set, fetch active agreements; for each matched candidate override `CostPerMB`; set decision reason to `ReasonRoamingAgreement` when override actually changed ordering; add `AgreementID *uuid.UUID` to `SoRDecision`.
  - For expired agreements (via `ListRecentlyExpiredByTenant`), log `warn` with `agreement_id, operator_id, expired_at` — no state change.
  - Wire in main.go: pass `store.RoamingAgreementStore` (adapter implements interface) into `sor.NewEngine`.
  - Tests: (a) active agreement overrides cost → cheaper agreement-backed operator wins (was not cheapest by default); (b) no agreement → behavior identical to pre-change; (c) expired agreement within 7d lookback → warning logged (capture via zerolog test writer), decision unchanged; (d) multiple active agreements — correct one applied.
- **Verify:** `go test ./internal/operator/sor/... -v`; PAT-001 satisfied (tests assert cost value + agreement_id on decision, not just "no error").

### Task 6: Renewal cron job — sweeper + notification dispatch
- **Files:** Create `internal/job/roaming_renewal.go`, `internal/job/roaming_renewal_test.go`; Edit `internal/job/types.go`; Edit `internal/config/config.go` (2 new env vars); Edit `cmd/argus/main.go` (wire processor + cron entry); Edit `.env.example` (add vars)
- **Depends on:** Task 2, Task 3 (notification hookup)
- **Complexity:** medium
- **Pattern ref:** Read `internal/job/scheduled_report.go` (full file — Sweeper + Processor pattern, `Type()` + `Process()`, enqueue via JobStore). Read `internal/job/kvkk_purge.go` for daily-cron + per-tenant iteration.
- **Context refs:** Notification Spec, Cron Job Spec, Data Flow
- **What:**
  - Add `JobTypeRoamingRenewal = "roaming_renewal_sweep"`.
  - `RoamingRenewalSweeper` struct: deps = `*store.RoamingAgreementStore`, `*store.UserStore`, `notification.Service`, redis client, logger, `alertDays int`.
  - `Process`: query `ListExpiringWithin(ctx, alertDays)` (cross-tenant). Group by tenant. For each: list tenant admins via `UserStore.ListByRole(tenantID, "tenant_admin")`. For each agreement: build `AlertPayload` per spec; Redis `SETNX argus:roaming:renewal:{agreement.id}:{end_date.Format("2006-01")}` with TTL 35d; if acquired → call `notificationSvc.HandleAlert(ctx, payload)`. Skip if lock held (already notified this month).
  - Config: `ROAMING_RENEWAL_ALERT_DAYS int default 30`, `ROAMING_RENEWAL_CRON string default "0 6 * * *"`.
  - `.env.example`: append entries.
  - `main.go`: construct processor, register with jobRunner, `cronScheduler.AddEntry({Name: "roaming_renewal", Schedule: cfg.RoamingRenewalCron, JobType: job.JobTypeRoamingRenewal})`.
  - Tests: (a) agreement expiring in 25d → alert enqueued once; (b) same agreement two consecutive daily runs within same month → second is deduped (Redis lock); (c) expiry > alertDays → no alert; (d) terminated state → no alert.
- **Verify:** `go test ./internal/job/ -run RoamingRenewal -v`.

### Task 7: FE types + data hooks
- **Files:** Create `web/src/types/roaming.ts`, `web/src/hooks/use-roaming-agreements.ts`
- **Depends on:** Task 4 (API stable)
- **Complexity:** low
- **Pattern ref:** Read `web/src/types/operator.ts`, `web/src/hooks/use-operators.ts` — follow TanStack Query + `api` client pattern.
- **Context refs:** API Specifications, Components Involved
- **What:**
  - `RoamingAgreement`, `SLATerms`, `CostTerms`, `AgreementState`, `AgreementType`, `ListRoamingAgreementsParams`, `CreateRoamingAgreementInput`, `UpdateRoamingAgreementInput`.
  - Hooks: `useRoamingAgreementList(params)` (cursor pagination), `useRoamingAgreement(id)`, `useCreateRoamingAgreement`, `useUpdateRoamingAgreement`, `useTerminateRoamingAgreement`, `useOperatorRoamingAgreements(operatorId)`.
- **Verify:** `cd web && npm run type-check` (or `tsc --noEmit`) — zero errors.

### Task 8: FE list page — SCR-150
- **Files:** Create `web/src/pages/roaming/index.tsx`
- **Depends on:** Task 7
- **Complexity:** medium
- **Pattern ref:** Read `web/src/pages/operators/index.tsx` (full — filters + Cards grid + SlidePanel creator). Read `web/src/pages/apns/index.tsx` for table + cursor pagination example.
- **Context refs:** Screen Specifications > SCR-150, Design Token Map
- **What:**
  - Filters bar: Operator `<Select>`, State `<Select>`, Expiring-within `<Select>` (30/60/90), search `<Input>`.
  - Table (use `<Table>` ui component) with columns: Partner, Operator, Type (badge), State (badge w/ color), End Date (+ "expiring in Nd" sub-line), row-click → detail page.
  - "+ New Agreement" button → SlidePanel with create form (all fields, structured SLA/Cost editors — numeric inputs, no JSON textarea).
  - Empty state, skeleton loading, error alert with retry.
  - Invoke `frontend-design` skill during implementation for visual polish.
- **Tokens:** Use ONLY classes from Design Token Map — zero `#hex`, zero `text-gray-*`, zero arbitrary `px/w-[...]` outside layout grids.
- **Components:** Reuse atoms from table above — NEVER raw `<input>`/`<button>`/`<select>`.
- **Verify:** `grep -Pn '#[0-9a-fA-F]{3,6}' web/src/pages/roaming/index.tsx` → zero matches. `grep -En '<(input|button|select|textarea)(\s|>)' web/src/pages/roaming/index.tsx` → zero matches. Manual: page renders, filters work, create flow completes.

### Task 9: FE detail page — SCR-151
- **Files:** Create `web/src/pages/roaming/detail.tsx`
- **Depends on:** Task 7, Task 8
- **Complexity:** medium
- **Pattern ref:** Read `web/src/pages/operators/detail.tsx` (tabs, info rows, edit panel, terminate confirm).
- **Context refs:** Screen Specifications > SCR-151, Design Token Map
- **What:**
  - Header: back link, title, state+type badges, Edit + Terminate buttons (Terminate uses `<Dialog>` confirm).
  - Two-column grid: SLA terms (uptime %, latency p95 ms, max incidents), Cost terms (base rate, tiers table, settlement period, currency).
  - Validity timeline: horizontal bar (CSS gradient or simple div widths), today marker, days-to-expiry badge.
  - Notes panel (read-only from server).
  - Edit opens SlidePanel (Sheet) with full form.
  - Invoke `frontend-design`.
- **Tokens:** same as Task 8.
- **Components:** same as Task 8.
- **Verify:** same grep checks on `detail.tsx`. Type-check green.

### Task 10: FE integration — router + sidebar + operator-detail Agreements tab
- **Files:** Edit `web/src/router.tsx`, `web/src/components/layout/sidebar.tsx`, `web/src/pages/operators/detail.tsx`
- **Depends on:** Task 8, Task 9
- **Complexity:** medium
- **Pattern ref:** Existing route entries in `router.tsx`, existing sidebar group structure, existing `<Tabs>` usage in `operators/detail.tsx`.
- **Context refs:** Components Involved, Screen Specifications > SCR-041 partial, Design Token Map
- **What:**
  - `router.tsx`: lazy-import `RoamingListPage` + `RoamingDetailPage`; register `/roaming-agreements` and `/roaming-agreements/:id` under `DashboardLayout > ProtectedRoute`.
  - `sidebar.tsx`: add `{label: 'Roaming', icon: Handshake, path: '/roaming-agreements'}` to the `OPERATIONS` group (after Reports, before Capacity).
  - `operators/detail.tsx`: add new "Agreements" `<TabsTrigger>` + `<TabsContent>` rendering a mini-list using `useOperatorRoamingAgreements(operator.id)` + "+ New Agreement" button that deep-links to `/roaming-agreements?operator_id={id}&create=1` OR opens SlidePanel inline (prefer inline to keep context).
- **Tokens / Components:** follow design map; reuse `<Tabs>`, `<Badge>`, `<Button>`.
- **Verify:** `npm run build` green; manual nav click lands on list; operator detail shows Agreements tab.

### Task 11: E2E + integration coverage
- **Files:** Create `internal/job/story_071_e2e_test.go` (or append to existing `story_069_e2e_test.go` pattern), Edit `internal/api/roaming/handler_test.go` to add integration paths
- **Depends on:** Task 4, Task 5, Task 6
- **Complexity:** medium
- **Pattern ref:** Read `internal/job/story_069_e2e_test.go` for end-to-end job+notification harness; read `internal/operator/sor/engine_test.go` for SoR assertion style.
- **Context refs:** Test Scenarios (AC-1..AC-5), API Specifications, SoR Engine Integration Spec, Notification Spec
- **What:** Cover the 4 AC test scenarios from story:
  1. Create agreement via handler → `sor.Engine.Evaluate` for a SIM routed to that operator returns `decision.CostPerMB == agreement.cost_per_mb` AND `decision.AgreementID == agreement.id`.
  2. Agreement `end_date` in the past (rewind clock via fake `time.Now()`) → SoR falls back to operator default cost; warning log captured.
  3. Agreement expiring in 25 days → `RoamingRenewalSweeper.Process` triggers `notification.Service.HandleAlert` (spy/mock recorder verifies AlertType, Severity, EntityID).
  4. Frontend E2E is manual per CLAUDE.md (no Playwright CI) — document manual steps in STEP log.
- **Verify:** `go test ./internal/job/... ./internal/operator/sor/... ./internal/api/roaming/... -v -race` → green.

## Acceptance Criteria Mapping

| AC | Implemented In | Verified By |
|----|----------------|-------------|
| AC-1: `roaming_agreements` table with full columns, constraints, RLS, down migration | Task 1 | Task 2 store tests (insert/query), Task 1 verify (migration up+down) |
| AC-2: CRUD API, cursor pagination, filters, RBAC, audit | Task 3, Task 4 | Task 3 handler tests + Task 4 route tests (RBAC 403) + Task 11 integration |
| AC-3: SoR consults active agreements; expired → warn + fallback | Task 5 | Task 5 engine tests (cost override, expired fallback), Task 11 scenario 1+2 |
| AC-4: Cron checks ≤30d expiry; email+in-app notification; `ROAMING_RENEWAL_ALERT_DAYS` configurable | Task 6 | Task 6 tests (alert fires / deduped / skipped), Task 11 scenario 3 |
| AC-5: FE list + detail pages, filters, operator-detail Agreements tab, sidebar Operations entry | Task 7, Task 8, Task 9, Task 10 | Type-check + grep checks + manual E2E walkthrough (Task 11 scenario 4) |

## Wave Breakdown (for Amil orchestration)

| Wave | Tasks | Parallelizable? | Rationale |
|------|-------|-----------------|-----------|
| Wave 1 | T1 | no | schema first |
| Wave 2 | T2 | no | depends on T1 |
| Wave 3 | T3, T5, T6 | YES (parallel) | all depend on T2 (store); T3 touches api, T5 touches sor, T6 touches job — disjoint files |
| Wave 4 | T4 | no | depends on T3 (and ideally T6 for main.go wiring — keep sequential) |
| Wave 5 | T7 | no | API stable (T4 done) |
| Wave 6 | T8, T9 | YES (parallel) | independent FE pages both consume T7 hooks |
| Wave 7 | T10 | no | depends on both FE pages |
| Wave 8 | T11 | no | end-to-end — needs everything |

## Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| SoR cache stale after agreement create/update | M | M | In `RoamingAgreementStore.Create/Update/Terminate`, invoke `sor.Engine.InvalidateTenantCache(tenantID)` via callback (wire through handler). Add to Task 3. |
| Overlap unique index on `(tenant_id, operator_id) WHERE state='active'` blocks legitimate transition (draft → active when one already active) | M | L | Transition rule: PATCH that moves state to `active` must either terminate existing active (409 returned with guidance) or fail with `roaming_agreement_overlap`. Documented in API Specifications. |
| Cron notification spam if admin user has multiple tenant roles | L | M | Redis dedup key is per-agreement-per-month, independent of admin count. `HandleAlert` fans out once per alert. |
| `ListExpiringWithin` cross-tenant query bypasses RLS | H | H | Cron runs in a system context that sets `app.current_tenant = NULL` or uses a superuser connection with RLS off. Pattern already used in `kvkk_purge.go` — follow it. Store method uses direct pool connection with RLS-bypassing session (`SET ROLE argus_cron` or documented equivalent). |
| `SoRDecision.AgreementID` field addition breaks existing JSON consumers | L | L | Field is `omitempty` + `*uuid.UUID` pointer — absent in serialized output when nil. Zero behavioral change for existing clients. |
| FE adds hardcoded colors despite token map | M | M | Verify commands in Task 8/9 grep for hex and raw elements — CI-blocking. Developer must invoke `frontend-design` skill. |

## Pre-Validation Self-Check

- **a. Min substance:** Story Effort M → min 60 lines / 3 tasks. Plan: ~330 lines / 11 tasks. PASS.
- **b. Required sections:** Goal, Architecture Context, Tasks, Acceptance Criteria Mapping — all present. PASS.
- **c. Embedded specs:** API contracts (6 endpoints), DB schema SQL, screen mockups (SCR-150, SCR-151, SCR-041 tab), Design Token Map — all embedded. PASS.
- **d. Task complexity cross-check:** Story M → mix of low + medium expected; Task 5 marked **high** (SoR integration — core business logic, per Story-Specific Compliance Rule and Bug Pattern PAT-001). PASS.
- **e. Context refs validation:** every `Context refs` entry references a heading that exists in this plan (Database Schema, API Specifications, Story-Specific Compliance Rules, Components Involved, Data Flow, SoR Engine Integration Spec, Notification Spec, Cron Job Spec, Screen Specifications, Design Token Map, Test Scenarios). PASS.
- **Architecture compliance:** API → `internal/api/roaming`; store → `internal/store`; domain logic → `internal/operator/sor` (existing bounded context for routing); job → `internal/job`; FE → `web/src/pages/roaming`. No cross-layer imports. PASS.
- **API compliance:** envelope, methods (POST/GET/PATCH/DELETE), validation spec, error codes (4 new). PASS.
- **DB compliance:** up+down migration, indexes for tenant/operator/state/expiry + partial unique on active, RLS policy. Source noted (new table, no prior migration). PASS.
- **UI compliance:** SCR-150/151 mockups embedded; atomic design via existing `web/src/components/ui/*`; drill-down targets (row→detail, operator cell→operator page); empty/loading/error states listed; `frontend-design` invocation noted; Design Token Map populated; Component Reuse table populated; token-enforcement grep in Verify steps. PASS.
- **Task decomposition:** all tasks ≤3 files except T10 (3 edits — acceptable); ordered by dependency; every task has `Depends on`, `Context refs`, `Pattern ref`. PASS.
- **Test compliance:** test task exists (T11) + per-task unit tests specified; paths given. PASS.
- **Self-containment:** API + DB + screens + business rules all inlined. PASS.

**Self-Validation Result: PASS (zero gate failures).**

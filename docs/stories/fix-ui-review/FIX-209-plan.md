# Implementation Plan: FIX-209 — Unified `alerts` Table + Operator/Infra Alert Persistence

## Goal

Create a single `alerts` table that persists every `argus.events.alert.triggered` NATS event (SIM anomaly, operator down / recovery / SLA violation, NATS consumer lag, storage / DB monitor warnings, roaming renewal, policy violation, import job failure, anomaly-batch crash) so the alerts UI, dashboard "Recent Alerts", and SRE audit queries read from one source of truth instead of the partial `anomalies` table. Ships as a single commit that survives `make db-migrate && make db-seed` on a fresh volume, keeps `anomalies` as the SIM-level detection store (unchanged), and preserves every existing publisher — the notification subscriber becomes the single writer into `alerts` (PAT-001 avoidance).

## Problem Context — Current Alert State (Verified)

### What exists today

| Concept | Where | Status |
|---|---|---|
| `anomalies` table | `migrations/20260322000003_anomalies.up.sql` | Persisted; SIM-level only; 4-level state machine `open / acknowledged / resolved / false_positive` (canonical 5-level severity since FIX-211) |
| `anomaly_comments` table | `migrations/20260415000001_anomaly_comments.up.sql` | FK to `anomalies.id ON DELETE CASCADE`; keep as-is, do NOT rename or redirect to alerts |
| NATS subject `argus.events.alert.triggered` | `internal/bus/nats.go:28` `SubjectAlertTriggered` | Fire-and-forget (notification dispatcher + WS relay only consumers today) — NOT persisted |
| `GET /api/v1/analytics/anomalies` | `internal/api/anomaly/handler.go` + `gateway/router.go:627` | FE alerts page calls this path today — anomaly-only |
| Notification subscriber `handleAlert` | `internal/notification/service.go:589` | `json.Unmarshal → AlertPayload{}` then dispatches email/telegram/webhook/SMS/in-app; nothing persisted |
| Dashboard "Recent Alerts" panel | `internal/api/dashboard/handler.go:289-316` | Reads `anomalyStore.ListByTenant` → operator/infra alerts invisible |

### Publisher inventory — 7 sites feed `SubjectAlertTriggered` with 3+ DIFFERENT PAYLOAD SHAPES

This is the #1 blind spot (advisor gap 1). Every row below is the exact construction site the persist subscriber must tolerate.

| # | File:Line | Payload shape | Has title? | Has description? | Has entity_id? | Has severity? | Implicit source | Implicit alert_type | Linkage metadata |
|---|---|---|---|---|---|---|---|---|---|
| 1 | `internal/operator/health.go:481-501` `publishAlert` | Go struct `operator.AlertEvent` (full envelope: AlertID, AlertType, Severity, Title, Description, EntityType, EntityID, Metadata, Timestamp) | yes | yes | yes (`EntityID=opID`) | yes | `operator` | `operator_down`, `operator_recovered`, `sla_violation` | `Metadata.operator_name` |
| 2 | `internal/analytics/anomaly/engine.go:200-214` `publishEvents` | anonymous `map[string]interface{}` with: `alert_id`, `tenant_id`, `alert_type=anomaly_<type>`, `severity`, `title`, `description`, `entity_type="anomaly"`, `entity_id`, `metadata=details`, `timestamp` | yes | yes | yes | yes | `sim` (SIM anomaly source) | `anomaly_sim_cloning`, `anomaly_data_spike`, `anomaly_auth_flood`, `anomaly_nas_flood` | `entity_id = anomaly.id`; include `sim_id` in details |
| 3 | `internal/bus/consumer_lag.go:186-191` `emitAlert` | Go struct `lagAlert` with ONLY: `severity`, `source="nats_consumer_lag"`, `consumer`, `pending` — **NO title, NO description, NO entity_id, NO timestamp** | **no** | **no** | **no** | yes | `infra` | `nats_consumer_lag` | `consumer`, `pending` |
| 4 | `internal/job/storage_monitor.go:170-177` `sendStorageAlert` | anonymous `map[string]interface{}`: `alert_type="storage."+X`, `tenant_id=nil`, `severity`, `title`, `description`, `entity_type="system"` — **NO entity_id, NO timestamp** | yes | yes | no | yes | `infra` | `storage.hypertable_growth`, `storage.low_compression_ratio`, `storage.high_connections`, etc. | none |
| 5 | `internal/job/anomaly_batch_supervisor.go:97-103` | anonymous `map[string]interface{}`: `severity=sev.High`, `source="anomaly_batch_crash"`, `job_id`, `error` — **NO title, NO description, NO tenant_id, NO entity_id, NO timestamp** | no | no | no | yes | `infra` | `anomaly_batch_crash` | `job_id`, `error` |
| 6 | `internal/job/roaming_renewal.go:98-123` | Go struct `notification.AlertPayload` (full envelope incl. `Metadata.operator_id`, `partner_operator_name`, etc.) | yes | yes | yes (`EntityID=agreementID`) | yes | `operator` (operator-scoped) | `roaming.agreement.renewal_due` | full metadata |
| 7 | `internal/policy/enforcer/enforcer.go:240-253` | anonymous `map[string]interface{}`: `id=violation.id`, `tenant_id`, `type="policy_violation"`, `severity`, `state="open"`, `message`, `sim_id`, `entity_type="sim"`, `entity_id=sim.id`, `detected_at` — **NO title, NO description (has `message` instead)** | no (has `message`) | no | yes | yes | `policy` | `policy_violation` | `sim_id`, `policy_violation_id=id` |

**Shape-classes summary (used by the persist subscriber):**
- **A (full `AlertEvent`/`AlertPayload`)**: sites 1, 6 — direct Unmarshal works
- **B (map with title+description, no entity_id)**: site 4 — title/description present; synthesize `entity_id = uuid.Nil`
- **C (map with partial envelope incl. entity_id but `message` instead of title)**: sites 2, 7 — map `message → title`, synthesize description from type+metadata
- **D (lean struct: severity+source+ids only, NO title/description)**: sites 3, 5 — persist layer must SYNTHESIZE title/description from the fields present (advisor gap 1, chosen strategy B "tolerant persist")

### Chosen normalization strategy: **tolerant persist now; full shape unification deferred to FIX-212**

Per advisor recommendation (gap 1, option B + option C). Implemented at `notification.handleAlert` (renamed to `handleAlertPersist`): a single `parseAlertPayload(data []byte) (alertStore.InsertParams, error)` helper with two Unmarshal passes:

1. **Primary pass** — into a tolerant struct `alertEventFlexible` with `json:"alert_id,omitempty"`, `alert_type` / `type` (both accepted; prefer `alert_type`), `severity`, `title` / `message` (both), `description`, `entity_type`, `entity_id`, `sim_id`, `operator_id`, `apn_id`, `source`, `consumer`, `pending`, `metadata` / `details` (both), `timestamp` / `detected_at` (both) — all optional
2. **Field synthesis** when primary is empty:
   - `alert_type` blank → fall back to `source` field; if still blank → `"unknown"`
   - `title` blank → `synthesizeTitle(alertType, consumer, pending, description)` (e.g. `"NATS consumer lag: <consumer> has <pending> pending"`, `"anomaly_batch crashed: job <id>"`)
   - `description` blank → formatted re-dump of remaining fields (`error` for batch crash; `pending` for lag; empty allowed for roaming renewal which has `description`)
   - `source` enum (sim|operator|infra|policy|system) derived from a `publisherSourceMap` (advisor gap 2): `operator_down` → `operator`; `nats_consumer_lag` → `infra`; `storage.*` → `infra`; `anomaly_batch_crash` → `infra`; `anomaly_*` → `sim`; `policy_violation` → `policy`; `roaming.agreement.renewal_due` → `operator`; `sla_violation` → `operator`; fallback → `system`
   - `timestamp` blank → `time.Now().UTC()`

The publisher payload shapes are NOT normalized in this story. FIX-212 (unified event envelope) fixes the shape drift across ALL event subjects. FIX-209's job is to land the persist layer robustly TODAY without blocking on FIX-212.

### Out of Scope (do NOT touch)

- `anomalies` schema — remains canonical SIM-level detection store; `alerts` rows with `source='sim'` link back via `meta.anomaly_id` (AC-7)
- `anomaly_comments` FK to `anomalies.id` — unchanged; alert-level commenting stays on anomalies only for this story (cross-source comments out of scope)
- Publisher payload shape normalization — FIX-212
- Alert deduplication / cooldown state machine — FIX-210 (but plan reserves `dedup_key` column — advisor gap 4)
- `delivery_failed` state — FIX-298 dep, NOT included in state CHECK for this story (advisor gap 9)
- Monthly partitioning of `alerts` — deferred to D-NNN tech debt (advisor gap 8; zero rows pre-release)

## Canonical Alerts Taxonomy (Authoritative)

> **This section is the single source of truth for FIX-209 and the consumer for FIX-210 (dedup) and FIX-213 (UI).**

### `alerts.severity` — CHECK reuses canonical 5-level enum (FIX-211)

```sql
CONSTRAINT chk_alerts_severity
  CHECK (severity IN ('critical','high','medium','low','info'))
```

Constraint name `chk_alerts_severity` is the reserved name from FIX-211 plan §Out of Scope. **PAT-013 does NOT apply** here because `alerts` is a NEW table (no prior constraint to drop). Use the literal constraint name.

### `alerts.state` — 4-value enum (advisor gap 3, explicit choice)

```sql
CONSTRAINT chk_alerts_state
  CHECK (state IN ('open','acknowledged','resolved','suppressed'))
```

**Decision:** FIX-209 `alerts.state` has 4 values: `open | acknowledged | resolved | suppressed`. `anomalies.state` keeps its own 4 values `open | acknowledged | resolved | false_positive` — the two state machines are independent.

- `false_positive` is a SIM-anomaly analyst-noise flag — lives only on `anomalies`
- `suppressed` is reserved for FIX-210 dedup cooldown — lands in this CHECK NOW so FIX-210 does not need a second CHECK migration
- `delivery_failed` (FIX-298) is NOT included — added later when FIX-298 ships

When an `anomalies` row transitions to `false_positive`, the paired `alerts` row (linked via `meta.anomaly_id`) transitions to `resolved`. Mapping documented in Task 3 (engine linkage) and in the FE adapter Task 6 (SIM-scope alert detail shows both fields when linked).

### `alerts.source` — 5-value CHECK

```sql
CONSTRAINT chk_alerts_source
  CHECK (source IN ('sim','operator','infra','policy','system'))
```

Publisher → source mapping table — see `publisherSourceMap` in the persist layer (Task 3). Gate grep `rg -n 'source\s*:\s*"[a-z_]+"' internal/bus internal/operator internal/job internal/analytics/anomaly internal/policy/enforcer` to catch a publisher whose source drifts from the enum (PAT-006 repeat-risk).

### `alerts.type` — OPEN string (length 64), NO CHECK

Free-form string matching the 12 RUNBOOK keys in `web/src/pages/alerts/index.tsx:37-104` (`operator_down`, `operator_recovered`, `sla_violation`, `nats_consumer_lag`, `storage.hypertable_growth`, `storage.low_compression_ratio`, `storage.high_connections`, `anomaly_sim_cloning`, `anomaly_data_spike`, `anomaly_auth_flood`, `anomaly_nas_flood`, `anomaly_batch_crash`, `roaming.agreement.renewal_due`, `policy_violation`, …). No CHECK because new publishers legitimately invent new types; validity is maintained by the FE label map.

### `alerts.dedup_key` — nullable, for FIX-210 (advisor gap 4)

```sql
dedup_key VARCHAR(255),
-- indexed below for FIX-210's cooldown lookup
```

`dedup_key` stays NULL in FIX-209. FIX-210 will set it to a stable hash (`<source>:<type>:<entity_id or consumer>`) and use the partial index below.

### Severity ordering (reuses FIX-211 canonical enum)

```
info (1) < low (2) < medium (3) < high (4) < critical (5)
```

## Architecture Context

### Components Involved

| Component | Layer | File(s) | Role |
|---|---|---|---|
| alerts DB table | DB | `migrations/20260422000001_alerts_table.up.sql` + `.down.sql` (NEW) | Single source of truth for all alert events |
| AlertStore | Go store | `internal/store/alert.go` (NEW) | INSERT on subscribe; List/Get/UpdateState reads; tenant-scoped |
| Alert persist subscriber | Go | `internal/notification/service.go` `handleAlert` → `handleAlertPersist` | INSERT before dispatch; tolerant Unmarshal per §Chosen normalization strategy |
| Alert API handler | Go | `internal/api/alert/handler.go` (NEW) | `GET /api/v1/alerts`, `GET /api/v1/alerts/{id}`, `PATCH /api/v1/alerts/{id}` (state) |
| Gateway routes | Go | `internal/gateway/router.go` | 3 new routes registered |
| Dashboard handler switch | Go | `internal/api/dashboard/handler.go:289-316` | `anomalyStore.ListByTenant` → `alertStore.ListByTenant`; wire `alertStore` dep |
| Anomaly engine linkage | Go | `internal/analytics/anomaly/engine.go:200-214` | Augment the NATS publish map with `meta.anomaly_id` + `sim_id` so persist subscriber links `alerts.sim_id` back to the anomaly |
| Notification wiring | Go | `cmd/argus/main.go` | `notifSvc.SetAlertStore(alertStore)` before `notifSvc.Start(...)` |
| Retention job | Go | `internal/job/alerts_retention.go` (NEW), wiring in `cmd/argus/main.go` | 180d retention purge; `ALERTS_RETENTION_DAYS` env |
| FE alerts page | React | `web/src/pages/alerts/index.tsx`, `web/src/pages/alerts/detail.tsx` | Switch `/analytics/anomalies` → `/alerts`; support `source`, `operator_id`, `apn_id` filters |
| FE dashboard | React | `web/src/pages/dashboard/index.tsx` `RecentAlerts` panel | No data-shape change (server side serves same `alertDTO`); confirm WS `alert.new` still drives refresh |
| Types | TS | `web/src/types/analytics.ts` | Add `Alert` type; keep `Anomaly` for SIM-scope |
| ERROR_CODES doc | Doc | `docs/architecture/ERROR_CODES.md` | Add alerts section with state/source/type reference |
| ADR | Doc (optional) | — | No ADR required; cross-cutting convention |

### Data Flow

```
PUBLISHERS (7 sites, heterogeneous payload shapes — unchanged)
      │
      ▼  NATS publish → subject argus.events.alert.triggered
      │
      ├─► ws.hub (relay to browser as "alert.new")     ← UNCHANGED
      │
      └─► notification.Service.handleAlertPersist      ← ENTRY POINT (modified)
              │
              ├─ 1. parseAlertPayload(data) (tolerant Unmarshal)
              ├─ 2. alertStore.Insert(ctx, params)     ← NEW — first-class write
              │     (on error: log + continue; dispatch still runs)
              ├─ 3. dispatchToChannels(ctx, payload...)  ← UNCHANGED
              └─ 4. audit.Emit(r, ... "alert.created" ...) ← NEW (via store hook or handler helper)

READ PATHS
  GET /api/v1/alerts?severity=&state=&source=&type=&operator_id=&sim_id=&apn_id=&from=&to=&cursor=&limit=
      → alertStore.ListByTenant → rows
  GET /dashboard → alertStore.ListByTenant(Limit=10)  (was anomalyStore)
  GET /api/v1/analytics/anomalies → anomalyStore.ListByTenant  (UNCHANGED — SIM-level noise flag review)

LIFECYCLE
  PATCH /api/v1/alerts/{id}  {state: "acknowledged"|"resolved"}  (suppressed reserved for FIX-210)
      → alertStore.UpdateState → state-machine guarded
      → audit.Emit "alert.update"

ANOMALY LINKAGE (advisor gap 7: single path via NATS, no dual-write)
  Anomaly engine continues to persist anomalies rows AND publish to SubjectAlertTriggered.
  The ONLY modification is engine.go:200-211 publish-map field set — include anomaly_id
  in meta and sim_id at top level so persist subscriber links the alerts row.
  engine.go does NOT call alertStore directly. PAT-001 avoided.
```

### API Specifications

All endpoints use the standard envelope `{ status, data, meta?, error? }`. All require tenant context (same middleware chain as `/analytics/anomalies`).

**`GET /api/v1/alerts`** — List alerts with filters + cursor pagination

Query params (all optional):
- `severity` ∈ canonical 5 values (validated via `severity.Validate` from FIX-211) — 400 `INVALID_SEVERITY` on bad value
- `state` ∈ `open|acknowledged|resolved|suppressed` — 400 `VALIDATION_ERROR` on bad value
- `source` ∈ `sim|operator|infra|policy|system` — 400 `VALIDATION_ERROR` on bad value
- `type` — exact match on `alerts.type` (free-form string; no server-side validation)
- `operator_id`, `sim_id`, `apn_id` — UUID filters, 400 `INVALID_FORMAT` on bad UUID
- `from`, `to` — RFC3339 timestamps on `fired_at`, 400 `INVALID_FORMAT`
- `cursor` — UUID of last row; pagination via `(fired_at DESC, id DESC)` + `id < $cursor`
- `limit` — int 1..100, default 50
- `q` — optional free-text search on `title ILIKE '%q%' OR description ILIKE '%q%'` (limit q to 100 chars)

Success response:
```json
{
  "status": "success",
  "data": [ { /* alertDTO */ } ],
  "meta": { "cursor": "<uuid or ''>", "has_more": true, "limit": 50 }
}
```

**alertDTO** (JSON field ordering fixed):
```
id               string     (uuid)
tenant_id        string     (uuid)
type             string
severity         string     canonical 5
source           string     5-enum
state            string     4-enum
title            string
description      string
meta             object     arbitrary JSON
sim_id           string|null
operator_id      string|null
apn_id           string|null
dedup_key        string|null   (always null in FIX-209; FIX-210 populates)
fired_at         string     RFC3339
acknowledged_at  string|null
acknowledged_by  string|null (uuid)
resolved_at      string|null
```

**`GET /api/v1/alerts/{id}`** — Single alert + tenant-scoped 404 (`ALERT_NOT_FOUND`).

**`PATCH /api/v1/alerts/{id}`** — state transition

Body: `{ "state": "acknowledged" | "resolved", "note"?: string (≤2000 chars) }`

Transitions:
- `open → acknowledged, resolved` (suppressed managed by FIX-210; not exposed on this endpoint)
- `acknowledged → resolved`
- `suppressed` — not transitionable via this endpoint; returns 409 `INVALID_STATE_TRANSITION`
- Any other request → 409 `INVALID_STATE_TRANSITION`

Response: updated alertDTO. Audit log emitted `alert.update`.

**No POST endpoint.** Alert creation is PUBLISHER → NATS → persist subscriber only; never directly via HTTP. State this in the handler Godoc.

### Database Schema

**Source: NEW table — ARCHITECTURE.md design (no prior migration).**

```sql
-- migrations/20260422000001_alerts_table.up.sql

CREATE TABLE IF NOT EXISTS alerts (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id       UUID NOT NULL REFERENCES tenants(id),
  type            VARCHAR(64) NOT NULL,
  severity        TEXT NOT NULL,
  source          VARCHAR(16) NOT NULL,
  state           VARCHAR(20) NOT NULL DEFAULT 'open',
  title           TEXT NOT NULL,
  description     TEXT NOT NULL DEFAULT '',
  meta            JSONB NOT NULL DEFAULT '{}',
  sim_id          UUID,
  operator_id     UUID,
  apn_id          UUID,
  dedup_key       VARCHAR(255),
  fired_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  acknowledged_at TIMESTAMPTZ,
  acknowledged_by UUID REFERENCES users(id),
  resolved_at     TIMESTAMPTZ,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT chk_alerts_severity CHECK (severity IN ('critical','high','medium','low','info')),
  CONSTRAINT chk_alerts_state    CHECK (state    IN ('open','acknowledged','resolved','suppressed')),
  CONSTRAINT chk_alerts_source   CHECK (source   IN ('sim','operator','infra','policy','system'))
);

-- Hot list: tenant + newest first
CREATE INDEX idx_alerts_tenant_fired_at
  ON alerts (tenant_id, fired_at DESC);

-- Open-only filter (most common UI query)
CREATE INDEX idx_alerts_tenant_open
  ON alerts (tenant_id, fired_at DESC)
  WHERE state = 'open';

-- Source filter
CREATE INDEX idx_alerts_tenant_source
  ON alerts (tenant_id, source);

-- Entity-drill filters (partial: non-NULL only)
CREATE INDEX idx_alerts_sim       ON alerts (sim_id)      WHERE sim_id IS NOT NULL;
CREATE INDEX idx_alerts_operator  ON alerts (operator_id) WHERE operator_id IS NOT NULL;
CREATE INDEX idx_alerts_apn       ON alerts (apn_id)      WHERE apn_id IS NOT NULL;

-- FIX-210 dedup lookup (reserve NOW, advisor gap 4)
CREATE INDEX idx_alerts_dedup
  ON alerts (tenant_id, dedup_key)
  WHERE dedup_key IS NOT NULL AND state IN ('open','suppressed');

-- RLS: mirror anomalies policy (tenant isolation via app.current_tenant setting)
ALTER TABLE alerts ENABLE ROW LEVEL SECURITY;
ALTER TABLE alerts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_alerts ON alerts
  USING (tenant_id = current_setting('app.current_tenant', true)::uuid);
```

Down migration drops table + all indices + RLS policy + constraints. Use `DROP TABLE IF EXISTS alerts CASCADE` (removes indices and RLS automatically).

**Seed strategy:** FIX-209 does NOT add fixture rows to `migrations/seed/003_comprehensive_seed.sql`. The table is intentionally empty at fresh-volume startup. Dashboard "Recent Alerts" shows empty state until publishers fire real events (simulator traffic, operator-sim). This is explicit (advisor gap 5) — no silent backfill.

**Partitioning decision (advisor gap 8):** Skip. Zero rows at migration time; retention job (Task 4) caps to 180 days. Re-evaluate as D-NNN when live volume > 1M rows.

### Screen Mockups

FIX-209 reshapes data sources only. Existing alerts-page layout from `web/src/pages/alerts/index.tsx` is preserved. Key change: 3 new filter chips on the filter bar (source, operator-scope, apn-scope), severity filter already canonical from FIX-211.

```
Alerts Page — filter bar:
┌──────────────────────────────────────────────────────────────────────────────────────┐
│  [Search…]  [Severity ▾ All]  [State ▾ All]  [Source ▾ All]  [Type ▾ All Types]  🔄  │
│                                     ↑ NEW (FIX-209)                                   │
├──────────────────────────────────────────────────────────────────────────────────────┤
│ KPI cards: [Critical 0] [High 0] [Medium 0] [Low 0] [Info 0]                         │
│ (after FIX-209, critical+high now include operator/infra alerts, not just SIM)       │
├──────────────────────────────────────────────────────────────────────────────────────┤
│ ◉ <SeverityBadge critical> [open]  Operator acme-op is DOWN                    …     │
│    type=operator_down  source=operator  operator=acme-op  fired 2 min ago            │
│ ◉ <SeverityBadge high>    [open]  SLA violation for operator acme-op           …     │
│ ◉ <SeverityBadge medium>  [ack]   NATS consumer lag: cdr-worker has 520 pending …    │
│ ◉ <SeverityBadge medium>  [open]  Policy violation: throttle on SIM …                │
└──────────────────────────────────────────────────────────────────────────────────────┘
```

Dashboard "Recent Alerts" panel keeps the compact row shape at `internal/api/dashboard/handler.go:298-312` — only the data source flips.

Alert detail page (`/alerts/:id`) already consumes `Anomaly` type; FIX-209 widens the TS type to `Alert` (see Design Token Map + Task 6).

### Design Token Map (Reuse from FIX-211)

FIX-209 does NOT introduce new visual patterns. The alerts page already consumes `<SeverityBadge>` and `SEVERITY_FILTER_OPTIONS` from `web/src/lib/severity.ts` and `web/src/components/shared/severity-badge.tsx` (both shipped in FIX-211). FIX-209 reuses them verbatim.

#### Color Tokens (inherited from FIX-211)

| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Critical alert row border (open state) | `border-danger/40` | `border-[#ff4466]`, `border-red-500` |
| High/Medium alert row border (open state) | `border-warning/30` | `border-[#ffb800]`, `border-yellow-500` |
| Resolved alert row | `opacity-70` | opacity-50 arbitrary |
| Impact badge background (critical/high) | `bg-danger-dim border-danger/20` | raw hex |
| Impact badge background (medium) | `bg-warning-dim border-warning/20` | raw hex |
| State pill open | `bg-danger-dim text-danger` | raw hex |
| State pill ack | `bg-warning-dim text-warning` | raw hex |
| State pill resolved | `bg-success-dim text-success` | raw hex |
| State pill suppressed (NEW — FIX-209) | `bg-bg-elevated text-text-tertiary border border-border` | raw hex |

#### Existing Components to REUSE

| Component | Path | Use For |
|-----------|------|---------|
| `<SeverityBadge>` | `web/src/components/shared/severity-badge.tsx` | ALL severity rendering — NEVER reinvent |
| `SEVERITY_FILTER_OPTIONS` | `web/src/lib/severity.ts` | Severity filter chips |
| `SEVERITY_PILL_CLASSES` | `web/src/lib/severity.ts` | Active-state filter chip |
| `<Badge>` | `web/src/components/ui/badge.tsx` | State pills, generic badges (NOT severity — use SeverityBadge) |
| `<Select>` | `web/src/components/ui/select.tsx` | Source + Type dropdowns |
| `<Button>` | `web/src/components/ui/button.tsx` | ALL buttons — NEVER raw `<button>` |
| `<Input>` | `web/src/components/ui/input.tsx` | Search field — NEVER raw `<input>` |

New source-filter options constant (export from `web/src/lib/alerts.ts` NEW):

```ts
export const ALERT_SOURCE_OPTIONS = [
  { value: '',         label: 'All Sources' },
  { value: 'sim',      label: 'SIM' },
  { value: 'operator', label: 'Operator' },
  { value: 'infra',    label: 'Infra' },
  { value: 'policy',   label: 'Policy' },
  { value: 'system',   label: 'System' },
] as const;
export type AlertSource = 'sim' | 'operator' | 'infra' | 'policy' | 'system';
export const ALERT_STATE_OPTIONS = [
  { value: '',             label: 'All' },
  { value: 'open',         label: 'Active' },
  { value: 'acknowledged', label: 'Acknowledged' },
  { value: 'resolved',     label: 'Resolved' },
  { value: 'suppressed',   label: 'Suppressed' },
] as const;
```

## Prerequisites

- [x] **FIX-211 DONE** — canonical severity enum (`internal/severity/severity.go`, `web/src/lib/severity.ts`, `<SeverityBadge>`, `apierr.CodeInvalidSeverity`) in place; reused for `chk_alerts_severity` + `GET /alerts?severity=` validator.
- [x] **FIX-206 DONE** — FK constraints baseline; seed already clean.
- [x] **FIX-207 DONE** — session/CDR CHECK-constraint migration pattern; followed for alerts CHECKs (plain `CHECK`, no `NOT VALID`).
- [x] **FIX-208 DONE** — aggregates facade; alert counts (if ever exposed on aggregates) go through the facade.
- [ ] **FIX-210** — runs AFTER FIX-209; consumes `dedup_key` + `state='suppressed'` reserved here.
- [ ] **FIX-212** — runs AFTER FIX-209; will normalize publisher payload shapes. FIX-209's tolerant persist stays until then.
- [ ] **FIX-298** — deferred; `state='delivery_failed'` NOT added to CHECK in FIX-209.

## Tasks

### Task 1: Migration — `alerts` table + CHECKs + indices + RLS

- **Files:** Create `migrations/20260422000001_alerts_table.up.sql` + `.down.sql`
- **Depends on:** — (DB-only)
- **Complexity:** medium
- **Pattern ref:** Read `migrations/20260322000003_anomalies.up.sql` for table shape + CHECK placement; read `migrations/20260415000001_anomaly_comments.up.sql` for RLS pattern (`app.current_tenant` setting); read `migrations/20260421000003_severity_taxonomy_unification.up.sql` for the `chk_*_severity` naming reserved by FIX-211.
- **Context refs:** "Database Schema", "Canonical Alerts Taxonomy"
- **What:**
  - Copy the SQL from §Database Schema verbatim. Use constraint names `chk_alerts_severity`, `chk_alerts_state`, `chk_alerts_source` (all three reserved in this plan).
  - Include the 7 indices listed, including the `idx_alerts_dedup` partial reserved for FIX-210.
  - RLS enable + FORCE + tenant_isolation policy matching `anomaly_comments` pattern.
  - Down: `DROP POLICY IF EXISTS tenant_isolation_alerts ON alerts; DROP TABLE IF EXISTS alerts CASCADE;`
- **Verify:**
  - `make db-migrate` applies cleanly on a fresh volume; `psql -c "\d+ alerts"` shows exactly 3 CHECK constraints, 7 indices, RLS enabled
  - `INSERT INTO alerts(tenant_id, type, severity, source, title) VALUES ('<tid>','test','warning','infra','t')` → fails `chk_alerts_severity` (PAT-013 self-check: `warning` rejected)
  - `INSERT INTO alerts(tenant_id, type, severity, source, state, title) VALUES ('<tid>','test','info','infra','delivery_failed','t')` → fails `chk_alerts_state` (FIX-298 deferral self-check)

### Task 2: AlertStore — Go data-access layer

- **Files:** Create `internal/store/alert.go` + `internal/store/alert_test.go`
- **Depends on:** Task 1 (migration must define the schema first)
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/anomaly.go` — follow the EXACT same structure: `AlertStore`, `Alert` model, `ListByTenantParams`, `CreateParams`, `scanAlert`, cursor pagination (`(fired_at DESC, id DESC)` with `id < $cursor`), `CountByTenantAndState`, `ErrAlertNotFound`, `ErrInvalidAlertTransition`. Reuse `validAlertTransitions` map (`open → acknowledged/resolved`, `acknowledged → resolved`) — do NOT expose `suppressed`.
- **Context refs:** "Canonical Alerts Taxonomy", "Database Schema", "API Specifications > alertDTO"
- **What:**
  - Type `Alert` mirrors every column; `Meta json.RawMessage`; `SimID/OperatorID/APNID/AcknowledgedBy *uuid.UUID`; `DedupKey *string`
  - `CreateParams`: `TenantID, Type, Severity, Source, Title, Description, Meta, SimID, OperatorID, APNID, DedupKey, FiredAt` — all persisted as-is (no validation; that lives at the publisher/subscriber boundary). `State` defaults to `'open'`.
  - `Create(ctx, p) (*Alert, error)` — single INSERT RETURNING all columns
  - `GetByID(ctx, tenantID, id)` — tenant-scoped; `ErrAlertNotFound` on ErrNoRows
  - `ListByTenant(ctx, tenantID, ListAlertsParams)` — filters: Type, Severity, Source, State, SimID, OperatorID, APNID, From, To, Q; cursor; limit 1..100 default 50
  - `UpdateState(ctx, tenantID, id, newState string, userID *uuid.UUID)` — guarded by `validAlertTransitions`; sets `acknowledged_at/acknowledged_by/resolved_at` + `updated_at`
  - `CountByTenantAndState(ctx, tenantID, state)` — for dashboard badge counters
  - `DeleteOlderThan(ctx, cutoff time.Time)` — for retention job (Task 4); returns rows-deleted count
- **Verify:** `go build ./internal/store/...` passes; `go test ./internal/store/alert_test.go` passes with the following unit tests:
  - `TestAlertStore_Create_InsertsWithDefaults` — state defaults to `open`, fired_at defaults to NOW
  - `TestAlertStore_ListByTenant_FiltersCombined` — source=operator + severity=critical returns only matching
  - `TestAlertStore_ListByTenant_CursorPagination` — two-page walk returns disjoint sets
  - `TestAlertStore_UpdateState_ValidTransition` — open→ack→resolved (timestamps set)
  - `TestAlertStore_UpdateState_InvalidTransition` — resolved→open returns `ErrInvalidAlertTransition`
  - `TestAlertStore_UpdateState_SuppressedNotExposed` — attempting open→suppressed via this method returns `ErrInvalidAlertTransition` (FIX-210 manages suppressed via a separate method)
  - `TestAlertStore_DeleteOlderThan` — retention purge returns deleted count

### Task 3: Notification subscriber — tolerant persist + anomaly-engine linkage

- **Files:**
  - Modify `internal/notification/service.go` (`handleAlert` → `handleAlertPersist`; add `parseAlertPayload` + `publisherSourceMap` + `synthesizeTitle`; add `SetAlertStore(AlertStoreWriter)` method; add `AlertStoreWriter` interface)
  - Modify `internal/notification/service_test.go` (new tests for persist happy-path + each shape-class A/B/C/D)
  - Modify `internal/analytics/anomaly/engine.go:199-214` (augment publish map with `sim_id`, `meta.anomaly_id`, `source="sim"` so persist subscriber links)
  - Modify `cmd/argus/main.go` — inject `alertStore` via `notifSvc.SetAlertStore(alertStore)` before `notifSvc.Start(...)` (around line 916)
- **Depends on:** Task 2 (AlertStore must exist)
- **Complexity:** **high** — tolerant Unmarshal across 4 shape-classes, PAT-006 grep gate on every publisher site, PAT-011 wiring at `cmd/argus/main.go`
- **Pattern ref:** Read `internal/notification/service.go:589-606` (existing `handleAlert`) for the current structure; read `internal/notification/service.go:317-414` (`Notify`) for the NotifStore persist-before-dispatch pattern — copy the same ordering (`s.alertStore.Create(...)` BEFORE `s.dispatchToChannels(...)`; on persist error, log and continue the dispatch); read `internal/severity/severity.go` for the `publisherSourceMap` validation pattern.
- **Context refs:** "Problem Context > Publisher inventory", "Chosen normalization strategy", "Canonical Alerts Taxonomy", "Data Flow"
- **What:**
  - Define `AlertStoreWriter` interface: `Create(ctx, store.CreateAlertParams) (*store.Alert, error)` — loose coupling per `NotifStore` pattern.
  - Define `alertEventFlexible` private struct (fields in §Chosen normalization strategy: both `alert_type` + `type`, both `title` + `message`, both `metadata` + `details`, both `timestamp` + `detected_at`).
  - Define `publisherSourceMap` private `map[string]string` from `alert_type` prefix → source enum (see table in §Canonical Alerts Taxonomy). Fallback: `system`.
  - `parseAlertPayload(data []byte) (store.CreateAlertParams, error)`:
    1. Unmarshal into `alertEventFlexible`
    2. Resolve `alert_type` from `alert_type` ?? `type` ?? `source` ?? `"unknown"`
    3. Validate `severity` via `severity.Validate` — on invalid, log `Warn` and coerce to `info` (PAT-011 note: invalid severity reaches the table → chk_alerts_severity rejects INSERT; never lose the event — invalid severity becomes `info`)
    4. Resolve `title` from `title` ?? `message` ?? `synthesizeTitle(...)` (never empty)
    5. Resolve `description` from `description` ?? "" (allowed empty)
    6. Resolve `source` enum from `publisherSourceMap[alert_type]` (for known prefixes); fallback `system`. **Gate grep** at end of task: `rg -n 'publisherSourceMap\[' internal/notification/ | wc -l` ≥ 1 + every alert_type in the 7-publisher table covered.
    7. Resolve `sim_id`, `operator_id`, `apn_id` by checking top-level first, then `metadata`/`details` map (type-safe UUID parse; ignore non-UUID strings)
    8. `dedup_key = nil` (FIX-210 populates)
    9. `fired_at = timestamp` ?? `detected_at` ?? `time.Now().UTC()`
    10. `meta = remaining metadata/details` (merge both, prefer metadata)
  - Rename `handleAlert` → `handleAlertPersist`. Body:
    1. `parseAlertPayload(data)` — on parse error, log error + bail out (still attempt dispatch with raw AlertPayload like today, so we never regress dispatch coverage)
    2. `s.alertStore.Create(ctx, params)` — on error, log + continue (dispatch still runs; availability > durability for THIS story)
    3. `s.dispatchToChannels(ctx, payload.Severity, title, description)` — unchanged
  - Anomaly engine `publishEvents` modification at `engine.go:200-211`: the publish map is expanded to include `sim_id: record.SimID.String()` at top level (when non-nil) and `metadata.anomaly_id: record.ID.String()` + `metadata.sim_id: record.SimID.String()` inside the details map. The `alert_type` is already `anomaly_<type>` — stays. This is the linkage that makes AC-7 "ALSO insert alerts row with `source='sim'` and `meta.anomaly_id = X`" work WITHOUT direct double-write (advisor gap 7).
- **Tokens:** N/A (backend)
- **Note:** **PAT-006 gate grep** at end of task (copy verbatim into task description):
  ```
  rg -n '\.Publish\(ctx, bus\.SubjectAlertTriggered,' internal/ --type go
  ```
  Expected: 7 hits. For each hit, verify the payload shape is covered by `alertEventFlexible` field set — fail the task if a new field (not in the flexible struct) would be silently dropped by the Unmarshal.
- **Verify:**
  - `go build ./...` passes; `go test ./internal/notification/... ./internal/analytics/anomaly/...` passes
  - New tests in `internal/notification/service_test.go`:
    - `TestHandleAlertPersist_FullAlertEvent_PersistsAllFields` (shape A — from operator.AlertEvent)
    - `TestHandleAlertPersist_AnomalyMapPayload_LinksAnomalyID` (shape C — from engine.go; asserts `meta.anomaly_id` and `sim_id`)
    - `TestHandleAlertPersist_LagAlert_SynthesizesTitle` (shape D — from lagAlert; asserts title like `"NATS consumer lag: <consumer> has <pending> pending"`)
    - `TestHandleAlertPersist_StorageMonitor_MapsToInfra` (shape B — storage_monitor)
    - `TestHandleAlertPersist_PolicyViolation_MessageBecomesTitle` (shape C — enforcer.go; asserts `message` → title)
    - `TestHandleAlertPersist_AnomalyBatchCrash_NoTitleSynthesizes` (shape D — anomaly_batch_supervisor)
    - `TestHandleAlertPersist_RoamingRenewal_FullEnvelope` (shape A — roaming_renewal)
    - `TestHandleAlertPersist_InvalidSeverity_CoercesToInfo` (PAT-011 — `severity="warning"` from a stale publisher coerces, never loses the event)
    - `TestHandleAlertPersist_PersistFails_DispatchStillRuns` (availability > durability)
    - `TestHandleAlertPersist_DispatchFails_PersistStillCommitted` (the inverse)
  - New test in `internal/analytics/anomaly/engine_test.go`:
    - `TestAnomalyEngine_Publish_IncludesSimIDAndAnomalyIDForLinkage` — asserts the new fields land in the NATS payload map (JSON round-trip)

### Task 4: Alert API handler + routes + 180d retention job

- **Files:**
  - Create `internal/api/alert/handler.go` (`List`, `Get`, `UpdateState`)
  - Create `internal/api/alert/handler_test.go`
  - Modify `internal/gateway/router.go` — register 3 new routes after `/api/v1/analytics/anomalies` block (~line 630):
    - `r.Get("/api/v1/alerts", deps.AlertHandler.List)`
    - `r.Get("/api/v1/alerts/{id}", deps.AlertHandler.Get)`
    - `r.Patch("/api/v1/alerts/{id}", deps.AlertHandler.UpdateState)`
  - Modify `cmd/argus/main.go` — construct `AlertStore` + `AlertHandler`, add to router deps struct
  - Create `internal/job/alerts_retention.go` + `internal/job/alerts_retention_test.go`
  - Modify `cmd/argus/main.go` — register the retention processor with the job scheduler (daily cron; mirror `storage_monitor` wiring)
  - Modify `internal/config/config.go` (or env-loader equiv) — add `ALERTS_RETENTION_DAYS int default 180`
  - Modify `docs/architecture/CONFIG.md` — document `ALERTS_RETENTION_DAYS`
- **Depends on:** Task 2 (AlertStore)
- **Complexity:** **high** (API + retention = 2 surfaces; state machine; 180d purge; cron wiring at main.go; config; docs)
- **Pattern ref:**
  - Handler: Read `internal/api/anomaly/handler.go` — same shape (List with filters, Get by id, UpdateState PATCH, tenant scoping, cursor pagination, `apierr.WriteError`, audit.Emit on state change). Validate severity via `severity.Validate` (FIX-211 canonical helper) — same call shape as `anomaly/handler.go:122`.
  - Retention job: Read `internal/job/storage_monitor.go` — mirror processor shape (`Process(ctx, job)`, `JobType = JobTypeAlertsRetention`, result JSON with `deleted_count`). Read `cmd/argus/main.go` grep `storage_monitor` for the scheduler-registration call shape.
- **Context refs:** "API Specifications", "Canonical Alerts Taxonomy", "Data Flow > LIFECYCLE"
- **What:**
  - **Handler:**
    - Validate every query param per §API Specifications. Use `severity.Validate` (400 `INVALID_SEVERITY`) for severity; hand-rolled maps for `state` and `source` enums (400 `VALIDATION_ERROR` with message listing valid values).
    - UUID filters return 400 `INVALID_FORMAT` on parse error.
    - Cursor is UUID; pagination `(fired_at DESC, id DESC)` with `id < $cursor` tiebreaker.
    - `UpdateState` body: validate `state ∈ {acknowledged, resolved}`; pass `userID` from ctx as `acknowledged_by` when state=acknowledged.
    - `audit.Emit(r, ..., "alert.update", "alert", id.String(), ..., map{"state": newState})` on PATCH.
    - New error code `ALERT_NOT_FOUND` (404) — add to `internal/apierr/apierr.go` + `docs/architecture/ERROR_CODES.md`.
  - **Retention job:**
    - `JobTypeAlertsRetention = "alerts_retention"` constant
    - `AlertsRetentionProcessor` struct with `alertStore *store.AlertStore`, `retentionDays int`, `logger`
    - `Process(ctx, job)`: compute `cutoff := time.Now().UTC().Add(-time.Duration(p.retentionDays) * 24 * time.Hour)`; call `alertStore.DeleteOlderThan(ctx, cutoff)`; write result JSON `{deleted_count: N, cutoff: RFC3339}`; `jobs.Complete(...)` (mirror `storage_monitor` close-out)
    - Cron: daily at 03:00 UTC (same slot as `storage_monitor`? check existing schedule to avoid collision — if collision, pick 03:15 UTC). Wire via `deps.JobScheduler.Register("alerts_retention", "0 3 * * *", processor)` at `cmd/argus/main.go`.
  - **Config + docs:**
    - `ALERTS_RETENTION_DAYS int default 180`; env helper in `internal/config/config.go`
    - Add to `docs/architecture/CONFIG.md` env table with default, meaning, min=30
- **Verify:**
  - `go build ./...` passes; `go test ./internal/api/alert/... ./internal/job/alerts_retention_test.go` passes
  - Handler tests (httptest):
    - `TestList_RejectsInvalidSeverity` — GET `/alerts?severity=warning` → 400 `INVALID_SEVERITY`
    - `TestList_RejectsInvalidState` — GET `/alerts?state=zzz` → 400 `VALIDATION_ERROR`
    - `TestList_RejectsInvalidSource` — GET `/alerts?source=zzz` → 400 `VALIDATION_ERROR`
    - `TestList_CombinedFilters_ReturnsExpectedRows`
    - `TestList_CursorPagination_DisjointPages`
    - `TestGet_NotFound_ReturnsAlertNotFound` — 404 `ALERT_NOT_FOUND`
    - `TestGet_CrossTenant_Returns404NotFound`
    - `TestUpdateState_Open_To_Ack_SetsAckedAtAndBy`
    - `TestUpdateState_Resolved_To_Open_Returns409InvalidStateTransition`
    - `TestUpdateState_SuppressedNotAllowed_Returns409`
    - `TestUpdateState_EmitsAudit` — asserts audit.Emit called with `alert.update`
  - Retention tests:
    - `TestAlertsRetention_DeletesOlderThanCutoff`
    - `TestAlertsRetention_ResultJSONShape` — asserts `{deleted_count: N}` in `job.result_json`
  - Manual smoke: `curl localhost:8080/api/v1/alerts -H "Authorization: Bearer <token>"` returns envelope with empty list on a fresh volume.

### Task 5: Dashboard "Recent Alerts" switchover + WS refresh confirmation

- **Files:**
  - Modify `internal/api/dashboard/handler.go:148, 289-316` — replace `anomalyStore.ListByTenant` with `alertStore.ListByTenant`; keep `alertDTO` response shape but widen the `Type` source to `alerts.type` and `Severity` mapped from `alerts.severity`; include new field `Source` in dto
  - Modify `internal/api/dashboard/handler.go` (struct fields) — inject `*store.AlertStore`
  - Modify `cmd/argus/main.go` — wire `AlertStore` into dashboard handler construction
  - Modify `web/src/pages/dashboard/index.tsx:734` (WS `alert.new` handler) — confirm refetch still targets dashboard query key and the Recent Alerts panel renders new rows from the unified source (no UI component change beyond optional Source chip in the row)
  - Modify `web/src/pages/dashboard/index.tsx` Recent Alerts row render — add small Source chip to the right of severity (if layout allows); keep rest identical
- **Depends on:** Task 2 (AlertStore), Task 4 (for compilability of `cmd/argus/main.go` after handler wiring)
- **Complexity:** medium
- **Pattern ref:** Read the existing `internal/api/dashboard/handler.go:289-316` goroutine — swap the store call, keep the alertDTO assembly. For FE, read the existing `dashboard/index.tsx:734` WS handler (already present) — no change to WS contract.
- **Context refs:** "Data Flow > READ PATHS", "Problem Context > What exists today"
- **What (BE):** Replace anomaly query with `alertStore.ListByTenant(ctx, tenantID, store.ListAlertsParams{Limit: 10})`; alertDTO now includes `source` field. Update the JSON contract doc comment to state alerts source-of-truth.
- **What (FE):** No refetch contract change (TanStack query key `['dashboard']` untouched). Render `source` as a small neutral badge next to severity when present. If a `source='sim'` row carries `meta.anomaly_id` and has an ICCID in meta, link to `/sims/<sim_id>` (reuse existing drill-down).
- **Verify:**
  - `go build ./...` passes; `go test ./internal/api/dashboard/...` passes
  - Integration smoke: fire a fake NATS `alert.triggered` (from a test publisher) → wait 100ms → `GET /dashboard` shows the alert in `recent_alerts`
  - `cd web && pnpm tsc --noEmit` passes; `pnpm build` succeeds

### Task 6: FE alerts page + alert detail — switch data source + Source/State filter chips

- **Files:**
  - Create `web/src/lib/alerts.ts` (`ALERT_SOURCE_OPTIONS`, `AlertSource`, `ALERT_STATE_OPTIONS`, `isAlertSource`)
  - Modify `web/src/types/analytics.ts` — add `Alert` type (mirror `alertDTO` JSON shape); keep `Anomaly` unchanged
  - Modify `web/src/pages/alerts/index.tsx`:
    - Swap `useAlerts` query URL from `/analytics/anomalies` → `/alerts`
    - Accept `Alert[]` instead of `Anomaly[]`
    - Add Source filter `<Select>` using `ALERT_SOURCE_OPTIONS`
    - Update State pill options to use `ALERT_STATE_OPTIONS` (adds `suppressed`)
    - Update RUNBOOKS lookup key — `alert.type` may now include `nats_consumer_lag`, `storage.*`, `anomaly_*` prefixes; add entries for the 7+ new types (`nats_consumer_lag`, `storage.hypertable_growth`, `storage.low_compression_ratio`, `storage.high_connections`, `anomaly_batch_crash`, `roaming.agreement.renewal_due`, `sla_violation`)
    - `impactEstimate` becomes a function of `source` + `severity` (operator-scope critical → 45k; sim-scope critical still 45k; infra critical → smaller synthetic like `{sims: 0, sessions: 0}` which skips the Impact block)
    - KPI card counts now derive from `alerts` (open) — no code change to the derivation; just targets the new data
  - Modify `web/src/pages/alerts/detail.tsx`:
    - Hook `useAlert` switches its URL from `/analytics/anomalies/{id}` → `/alerts/{id}`
    - Type narrows from `Anomaly` → `Alert`
    - If `alert.source === 'sim'` and `alert.meta.anomaly_id` is present, render a "View anomaly detail" link to `/dashboard/analytics?anomaly=<id>` (the existing anomaly-drawer path)
  - Modify `web/src/hooks/use-alert-detail.ts` — URL change `/analytics/anomalies/` → `/alerts/`
  - Modify `web/src/pages/alerts/_partials/alert-actions.tsx` — PATCH URL change `/analytics/anomalies/{id}` → `/alerts/{id}` (body is still `{state: "acknowledged"|"resolved"}`)
  - Modify `web/src/pages/alerts/_partials/comment-thread.tsx` — **keep pointing at `/analytics/anomalies/{id}/comments`** when `alert.source === 'sim'` AND `alert.meta.anomaly_id` is present (comments belong to the anomaly, not the alert — out-of-scope per §Out of Scope). For non-SIM alerts, hide the comment tab (grey out with tooltip "Discussion threads are per-anomaly only — this is not a SIM-level event"). FE-only behaviour change, no new endpoint.
- **Depends on:** Task 4 (BE endpoints must exist for the FE to call)
- **Complexity:** **high** — 6 files, PAT-006-equivalent FE risk (severity/source/state enums drift if inventory is incomplete), hybrid anomaly-vs-alert UX for SIM-scope
- **Pattern ref:** Read `web/src/pages/ops/incidents.tsx:21-29` + `web/src/pages/alerts/index.tsx:1-240` — the alerts page already has the correct shape; changes are mechanical (URL + filter add + type widening). For the hook pattern read `web/src/hooks/use-alert-detail.ts`.
- **Context refs:** "API Specifications > GET /api/v1/alerts", "Design Token Map", "Canonical Alerts Taxonomy", "Problem Context > Publisher inventory"
- **Tokens:** Use ONLY classes from the Design Token Map — zero hardcoded hex. The Source chip reuses `bg-bg-elevated text-text-secondary border border-border` (neutral).
- **Components:** `<SeverityBadge>`, `<Select>`, `<Badge>`, `<Button>`, `<Input>`. NEVER raw HTML elements.
- **Note:** Invoke `frontend-design` skill at the start of this task for a reusability audit.
- **Verify:**
  - `cd web && pnpm tsc --noEmit` passes
  - `cd web && pnpm build` succeeds
  - Grep: `rg -n '/analytics/anomalies' web/src/pages/alerts web/src/hooks/use-alert-detail.ts` → empty (URL rewrite complete)
  - Grep: `rg -n '#[0-9a-fA-F]{3,8}' web/src/pages/alerts web/src/lib/alerts.ts` → zero matches
  - Visual smoke (dev-browser, optional): alerts page shows Source filter dropdown with 5 + All options; severity filter unchanged from FIX-211; selecting Source=operator filters rows; clicking an alert opens detail; if SIM-scope, the "View anomaly detail" link appears.

### Task 7: Docs — ERROR_CODES.md Alerts section + ROUTEMAP entries

- **Files:**
  - Modify `docs/architecture/ERROR_CODES.md` — add new "Alerts" section with `ALERT_NOT_FOUND` (404), `INVALID_STATE_TRANSITION` (409), `VALIDATION_ERROR` (422 — already present; note which fields) and the Alerts Taxonomy reference block (copy the state, source, severity, type tables from §Canonical Alerts Taxonomy)
  - Modify `docs/architecture/CONFIG.md` — add `ALERTS_RETENTION_DAYS` row to env table
  - Modify `docs/architecture/api/_index.md` — add 3 new API rows (API-NNN `GET /alerts`, API-NNN+1 `GET /alerts/{id}`, API-NNN+2 `PATCH /alerts/{id}`)
  - Modify `docs/architecture/db/_index.md` — add new table row `TBL-NN alerts` with columns + CHECK + index summary
  - Modify `docs/ROUTEMAP.md` — Active phase log: mark FIX-209 DONE row (filled at step-log time, not here — but reserve the row)
- **Depends on:** Tasks 1, 2, 3, 4, 5, 6 (so docs reflect shipped behaviour)
- **Complexity:** low
- **Pattern ref:** Read ERROR_CODES.md existing "Validation Errors" section for row shape; read api/_index.md existing rows for format; read db/_index.md `TBL-09 anomalies` for table row shape.
- **Context refs:** "Canonical Alerts Taxonomy", "API Specifications", "Database Schema"
- **What:** Straight doc edits. Include a one-paragraph note in ERROR_CODES.md explaining the 4-state machine + reserved `suppressed` (FIX-210) + reserved `delivery_failed` (FIX-298, not yet in CHECK). Include the publisher-source map table from §Canonical Alerts Taxonomy so readers can trace alert_type back to source.
- **Verify:**
  - `grep -q 'ALERT_NOT_FOUND' docs/architecture/ERROR_CODES.md`
  - `grep -q 'ALERTS_RETENTION_DAYS' docs/architecture/CONFIG.md`
  - `grep -q 'alerts' docs/architecture/db/_index.md` with the new TBL-NN row
  - `grep -q '/alerts' docs/architecture/api/_index.md`

## Acceptance Criteria Mapping

| AC | Implemented in | Verified by |
|---|---|---|
| AC-1: alerts table schema (id, tenant_id, type, severity, source, title, description, state, meta, fired_at, acknowledged_at/_by, resolved_at, sim_id, operator_id, apn_id) + indices | Task 1 | Task 1 `\d+ alerts`; Task 2 store tests; severity/state/source covered by 3 CHECKs (constraint-name grep) |
| AC-2: NATS subscriber `handleAlert` INSERTs first, THEN dispatches; FIX-298 `delivery_failed` deferred | Task 3 | `TestHandleAlertPersist_PersistFails_DispatchStillRuns` + `TestHandleAlertPersist_DispatchFails_PersistStillCommitted` |
| AC-3: all 7 existing publishers remain untouched in shape (advisor gap 1 plan: tolerant persist now, FIX-212 normalizes later) | Task 3 (zero publisher edits — only anomaly engine augmentation for linkage fields) | PAT-006 gate grep in Task 3 Verify; `git diff internal/bus internal/job internal/policy/enforcer internal/operator/health.go` shows zero changes |
| AC-4: `GET /api/v1/alerts` with filters severity/source/state/date/operator_id/sim_id + cursor | Task 4 | Handler tests enumerated in Task 4 Verify |
| AC-5: Dashboard Recent Alerts reads alerts table (not anomalies) | Task 5 | `git diff internal/api/dashboard/handler.go:289-316` shows swap; integration smoke |
| AC-6: FE 5-severity taxonomy + lifecycle actions (ack/resolve); dedup UI deferred to FIX-210 | Task 6 | `pnpm tsc` + `pnpm build`; grep `/analytics/anomalies` empty in alerts pages; state filter now exposes `suppressed` (UI-ready for FIX-210) |
| AC-7: anomalies kept; SIM anomalies ALSO create alerts row with `source='sim'` + `meta.anomaly_id` | Task 3 (engine.go publish map augment → persist subscriber picks up linkage; NO dual-write from engine to store) | `TestAnomalyEngine_Publish_IncludesSimIDAndAnomalyIDForLinkage` + `TestHandleAlertPersist_AnomalyMapPayload_LinksAnomalyID` |
| AC-8: 180d retention via `ALERTS_RETENTION_DAYS` | Task 4 | `TestAlertsRetention_DeletesOlderThanCutoff`; CONFIG.md entry |

## Story-Specific Compliance Rules

- **API:** Standard envelope for all 3 new endpoints; `INVALID_SEVERITY` (400), `VALIDATION_ERROR` (422 for body), `INVALID_FORMAT` (400 for UUID/date), `ALERT_NOT_FOUND` (404), `INVALID_STATE_TRANSITION` (409). Cursor pagination on list. Tenant scoping enforced in store.
- **DB:** Migration is transactional (migrate tool default). Both up + down scripts. CHECK constraints use the reserved names (`chk_alerts_severity`, `chk_alerts_state`, `chk_alerts_source`). RLS enabled with `app.current_tenant` policy matching `anomaly_comments`. Seed does NOT add fixture rows — empty table is explicit.
- **UI:** ONLY `<SeverityBadge>` (from FIX-211) for severity; `ALERT_SOURCE_OPTIONS`/`ALERT_STATE_OPTIONS` from new `web/src/lib/alerts.ts`; zero hardcoded hex; `frontend-design` skill invoked once at Task 6 start.
- **Business:** Alerts table is the ONLY source of truth for operator/infra alert history. Anomalies stays the SIM-level detection store (unchanged). Retention 180d default, per-tenant retention **NOT** supported in this story (D-NNN if requested).
- **ADR:** No ADR changes.
- **Publisher discipline (PAT-006):** FIX-209 does NOT rewrite publisher payloads. FIX-212 does. FIX-209's persist subscriber is tolerant by design. Any future story that adds a new publisher MUST include the `publisherSourceMap` entry — a Gate grep in Task 3 enumerates the 7 current publishers; a new publisher without a map entry falls through to `source='system'`, which is intentional but noisy (log-level Warn in `parseAlertPayload` catches it).

## Bug Pattern Warnings

Consulted `docs/brainstorming/bug-patterns.md`:

- **PAT-006 [FIX-201]** — shared payload field silently omitted. DIRECTLY APPLIES. 7 publisher sites emit 4 payload shapes; the `alertEventFlexible` Unmarshal struct must cover every field in every shape, otherwise a field added in a future publisher silently becomes zero-valued in the persisted row. Task 3 gate grep enumerates the 7 sites and requires a shape-class assertion on each.
- **PAT-011 [FIX-207]** — plan-specified wiring missing at construction sites. DIRECTLY APPLIES. `notifSvc.SetAlertStore(alertStore)` at `cmd/argus/main.go` is the single constructor wiring point. If omitted, `alertStore` is nil inside `handleAlertPersist` → `s.alertStore.Create` NPEs. Task 3 Verify asserts the wiring + a runtime nil guard (`if s.alertStore == nil { log + skip persist }`) as defence-in-depth.
- **PAT-013 [FIX-211]** — ILIKE constraint-drop trap. DOES NOT APPLY (alerts is a NEW table; no prior CHECK to drop). Noted for future reference: if FIX-298 adds `delivery_failed` to `chk_alerts_state`, the migration MUST use `ALTER TABLE alerts DROP CONSTRAINT chk_alerts_state; ALTER TABLE alerts ADD CONSTRAINT chk_alerts_state CHECK (state IN ('open','acknowledged','resolved','suppressed','delivery_failed'));` — NOT an ILIKE-driven dynamic drop.
- **PAT-014 [FIX-211]** — seed-time invariant violations from independent random timestamps. NOT RELEVANT (no seed rows added in FIX-209).
- **PAT-001 [STORY-084]** — double-writer. APPLIES via advisor gap 7. The anomaly engine MUST NOT call `alertStore.Create` directly; the NATS subscriber is the single writer. Plan explicitly documents this; Task 3's engine.go edit is strictly the publish-map augment (linkage fields only).
- **PAT-004 [STORY-090]** — goroutine cardinality. NOT RELEVANT (no fan-out loop introduced).
- **PAT-009 [FIX-204]** — nullable FK in aggregations. POTENTIALLY RELEVANT for future analytics on `alerts` (since `sim_id`, `operator_id`, `apn_id` are all nullable) — FIX-209 itself does NOT aggregate by these, so no action now. Noted for future stories.
- **PAT-012 [FIX-208]** — cross-surface count drift. APPLIES: after FIX-209, dashboard Recent Alerts and `/alerts` page read the same table (AC-5) — the single-source-of-truth invariant the `aggregates` facade enforces is naturally satisfied by having one table. No facade entry needed for alerts in FIX-209 (future stories like FIX-229 may add alert counts there).

## Tech Debt (from ROUTEMAP)

Consulted `docs/ROUTEMAP.md` Tech Debt table: no OPEN items target FIX-209. No tech debt items for this story.

## Mock Retirement

Not applicable — Argus is backend-first; no `src/mocks/` directory.

## Risks & Mitigations

- **Risk 1 — Publisher payload drift (PAT-006 repeat):** 7 heterogeneous shapes today; a future publisher adds a field not in `alertEventFlexible`. **Mitigation:** Gate grep in Task 3 enumerates the 7 sites; `alertEventFlexible` uses `json.RawMessage`-based `meta` merge so unknown fields land in `meta` rather than being silently dropped; invalid severity coerces to `info` so the event is never lost. FIX-212 permanently fixes shape drift.
- **Risk 2 — Alert storm (consumer lag + anomaly batch crashes) floods DB:** Dedup in FIX-210 handles this. FIX-209 reserves `dedup_key` column + partial index NOW so FIX-210 lands additively.
- **Risk 3 — Persist failure masks dispatch failure (or vice versa):** Covered by paired tests `TestHandleAlertPersist_PersistFails_DispatchStillRuns` + `TestHandleAlertPersist_DispatchFails_PersistStillCommitted` — neither failure cancels the other. Logged at `Error` level when one fails.
- **Risk 4 — Dashboard Recent Alerts empty at post-deploy moment (advisor gap 5):** Pre-release, volumes are not live, and no backfill mandated. The empty state already exists in the FE (dashboard/index.tsx render). Accept + log: "Recent Alerts source-of-truth is now the alerts table; expect empty list until simulator/production traffic lands."
- **Risk 5 — Retention job deletes in-flight investigations:** 180d default is conservative. `ALERTS_RETENTION_DAYS` env override. Acknowledged/resolved alerts >180d are deleted regardless of state — if future law-retention or compliance arrives, store a second "archive" bit or dump to Parquet before delete (D-NNN item, post-release).
- **Risk 6 — Cross-state anomaly/alert linkage confusion (advisor gap 3):** anomalies has `false_positive`, alerts has `suppressed`. When a SIM anomaly is marked `false_positive`, does the linked alert move to `resolved`? **Decision:** No automatic transition in FIX-209. Analyst marks both independently if they care. Future story can auto-mirror if UX research demands. Documented in FE detail page help text.
- **Risk 7 — Partitioning not done (advisor gap 8):** At 1M+ rows, table scans slow. **Mitigation:** Covered by indices for common paths (tenant+fired_at, tenant+state=open, tenant+source, entity drills). Partition revisit as D-NNN when row count crosses 1M in any tenant.
- **Risk 8 — `suppressed` state leaks into GET /alerts default (all-states) list, making UI confusing:** FE default filter is "all non-resolved" today. After FIX-210 introduces suppressed rows, users might see unexplained "suppressed" items. **Mitigation:** FE default State filter stays "Active" (=`open`), suppressed rows only visible when user selects "Suppressed" or "All" explicitly. Confirmed in Task 6 state-pills default value.

## Self-Containment Check

- [x] API specs embedded (§API Specifications with exact query params, body shapes, error codes)
- [x] DB schema embedded (§Database Schema — full SQL, source-noted "NEW table")
- [x] Screen mockup embedded (§Screen Mockups)
- [x] Business rules stated inline (§Canonical Alerts Taxonomy, §Chosen normalization strategy)
- [x] Design Token Map populated (§Design Token Map — reuse from FIX-211 + new source-chip neutral tokens)
- [x] Component Reuse table populated (§Existing Components to REUSE)
- [x] Pattern refs present on every create-new-file task
- [x] Context refs on every task point at sections that exist in this plan
- [x] Bug Pattern Warnings enumerated with applicability
- [x] Risks + Mitigations enumerated (8 risks)
- [x] Seed discipline addressed (§Database Schema > Seed strategy)

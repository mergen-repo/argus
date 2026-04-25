# Implementation Plan: FIX-229 — Alert Feature Enhancements (Mute UX, Export, Similar Clustering, Retention)

## Goal
Deliver scoped & scheduled alert mute (ad-hoc + saved rules), tri-format export (CSV/JSON/PDF), "similar alerts" clustering, and per-tenant retention configuration that drives the existing daily purge job — fixing two existing FE bugs in the process (fake `setMuted` state, wrong CSV endpoint).

## Story Reference
- Story: `/Users/btopcu/workspace/argus/docs/stories/fix-ui-review/FIX-229-alert-feature-enhancements.md`
- Priority: P3 · Effort: M · Wave: 7 · Depends: FIX-209 (alerts table), FIX-210 (dedup state machine), FIX-211 (severity)
- ACs: AC-1 … AC-5 (verbatim enforcement)
- Findings addressed: F-38, F-39, F-41, F-42, F-43

---

## Pinned Decisions (Plan-time — Developer must NOT re-decide)

| ID | Decision | Rationale |
|----|----------|-----------|
| **DEV-333** | **Single `alert_suppressions` table covers BOTH AC-1 (ad-hoc mute) and AC-5 (saved reusable rule).** Discriminator: `rule_name TEXT NULL` — NULL = ad-hoc mute, NOT NULL = saved rule. Same row shape `(tenant_id, scope_type, scope_value, expires_at, reason, created_by, rule_name?)`. AC-1 IS an instance of AC-5 with no name. | Story file list says singular `alert_suppression.go`. Saves a table, simpler queries, single audit trail. |
| **DEV-334** | **Suppression applied at TRIGGER time, not at LIST time.** In `UpsertWithDedup`, before INSERT, query `alert_suppressions` for an active matching scope; if match, insert the new alert with `state='suppressed'` directly. Existing state machine already supports `suppressed`. NO list-time JOIN/filter. | (a) Preserves the existing `idx_alerts_dedup_unique WHERE state IN ('open','acknowledged','suppressed')` partial index. (b) Audit trail shows the alert WAS observed and was suppressed by which rule. (c) List queries stay simple — no cross-table filter logic. |
| **DEV-335** | **Retention setting stored in `tenants.settings JSONB` as key `alert_retention_days` — NO new column, NO `tenant_settings` table.** Validation: integer, default 180 (when key missing), min 30 (matches job floor), max 365. Tenant Update handler enforces these bounds. Job processor reads `tenants.settings->>'alert_retention_days'` per tenant — falls back to `cfg.AlertsRetentionDays` (env, currently 180) when key missing. | Existing `Tenant.Settings json.RawMessage` already exists with full Update path. Single-table source-of-truth. |
| **DEV-336** | **Retention job becomes per-tenant.** `internal/job/alerts_retention.go` is currently global (one cutoff for all rows). Reshape to: query all active tenants → for each, resolve effective retention days → call new `AlertStore.DeleteOlderThanForTenant(tenantID, cutoff)`. Aggregate result JSON `{per_tenant: [{tenant_id, deleted, retention_days}], total_deleted}`. Existing 03:15 UTC cron entry stays. | AC-4 says "tenant setting" — global purge violates the spec. Existing test (`alerts_retention_test.go`) updated. |
| **DEV-337** | **Three dedicated export endpoints (mirroring SLA pattern):** `GET /alerts/export.csv`, `GET /alerts/export.json`, `GET /alerts/export.pdf`. Each accepts the SAME query filters as `GET /alerts` (severity, state, type, source, sim_id, operator_id, apn_id, from, to, q). Server-side caps at **10 000 rows** (apply tenant filter — never cross-tenant). PDF goes through the report engine; CSV/JSON streamed inline (no engine roundtrip needed for raw data). | Matches FIX-215 SLA `GET /api/v1/sla/pdf`. Cap prevents OOM for large tenants. Standard envelope NOT used — these are file-download responses with `Content-Disposition: attachment`. |
| **DEV-338** | **PDF goes through `report.Engine.Build` with new `ReportAlertsExport` `ReportType`.** Extend `DataProvider` interface with `AlertsExport(ctx, tenantID, filters map[string]any) (*AlertsExportData, error)`. New `internal/report/alerts_pdf.go` builder. Filters map carries the same query params as the JSON/CSV endpoints. | Single PDF render path; aligns with FIX-215. When FIX-248 lands, this trivially migrates. Tech-debt entry recorded. |
| **DEV-339** | **"Similar alerts" endpoint:** `GET /alerts/{id}/similar?limit=20`. Match logic: rows in same tenant where (`dedup_key IS NOT NULL AND dedup_key = anchor.dedup_key`) **OR** (`dedup_key IS NULL AND type = anchor.type AND source = anchor.source`). Excludes the anchor itself (`id != $1`). Order by `fired_at DESC`. Limit 1..50, default 20. Includes ALL states (open/ack/resolved/suppressed). | (a) Existing `idx_alerts_dedup` is partial on `state IN ('open','suppressed')` — for similar lookup we want ALL states. **Plan: add new index** `idx_alerts_dedup_all_states ON alerts (tenant_id, dedup_key, fired_at DESC) WHERE dedup_key IS NOT NULL` (no state filter). (b) Type+source fallback uses existing `idx_alerts_tenant_source` plus `(tenant_id, fired_at DESC)`. |
| **DEV-340** | **Mute scope_type values (DB CHECK):** `'this'`, `'type'`, `'operator'`, `'dedup_key'`. (`'this'` = single alert ID — alias for the existing single-suppress path; included for symmetry / API consistency. `'type'` = alert.type. `'operator'` = operator_id UUID. `'dedup_key'` = dedup_key string.) `scope_value TEXT NOT NULL` — for `'this'` and `'operator'` it's the UUID as text; for `'type'` and `'dedup_key'` it's the string. | Single column keeps the table simple; type-discriminated parsing in store layer. |
| **DEV-341** | **Duration UI options + DB storage:** UI radio: `1h`, `24h`, `7d`, `Custom`. Custom = ISO-8601 datetime picker bounded to NOW + 30d max (defense against accidental 10-year mutes). Stored as absolute `expires_at TIMESTAMPTZ NOT NULL`. UI never stores intervals — server computes `expires_at = NOW() + interval`. Client sends ONE of `{duration: "1h"|"24h"|"7d"} or {expires_at: ISO-8601}` — the API resolves to absolute. | Absolute timestamp survives clock changes; eliminates DST/server-skew bugs. 30d cap is the UX-side over-aggressive-mute guard from Risks §1. |
| **DEV-342** | **Frontend mute UX = SlidePanel (Option C from FIX-216) — 4 fields qualify as rich form.** Fields: scope radio, scope_value (auto-filled from row), duration radio, reason textarea, "Save as rule" toggle + rule_name input (revealed when toggled). Single panel handles AC-1 (no name) and AC-5 (with name). Unmute = Dialog confirm (single field — reason optional). | Dialog rule from FIX-216: ≤2 fields → Dialog, 3+ → SlidePanel. Mute panel has 4-5 visible fields. |
| **DEV-343** | **FE uses NEW `useAlertExport(format)` hook — `useExport('analytics/anomalies')` is REMOVED.** Existing `useExport` in `web/src/hooks/use-export.ts` is kept for non-alert pages but a new alert-specific helper at `web/src/hooks/use-alert-export.ts` handles all three formats with one signature `{ exportAs(format, filters) }`. Reuses the same blob+download pattern. | Deletes a real bug (wrong endpoint hits anomalies table, not alerts) without breaking other call sites. |
| **DEV-344** | **Tenant settings UPDATE path adds whitelist check on `alert_retention_days`.** `TenantStore.Update` already accepts `Settings *json.RawMessage`. Add validation in the API handler (`internal/api/tenant/handler.go` Update path): when settings JSON contains `alert_retention_days`, must be integer 30..365 inclusive, else 422. Other settings keys pass through. | Encapsulates business rule at the API edge; store remains generic JSONB writer. |

---

## Architecture Context

### Components Involved
| Component | Layer | Responsibility | File |
|-----------|-------|----------------|------|
| `alert_suppressions` | DB (TBL-55) | Single table for ad-hoc mute + saved rule | `migrations/20260426000001_alert_suppressions.{up,down}.sql` (NEW) |
| `idx_alerts_dedup_all_states` | DB index | Support similar-alerts lookup across all states | Same migration |
| `AlertSuppressionStore` | Store | CRUD + `MatchActive(tenantID, alert) (*Suppression, error)` | `internal/store/alert_suppression.go` (NEW) |
| `AlertStore.DeleteOlderThanForTenant` | Store | Per-tenant cutoff delete | `internal/store/alert.go` (extend) |
| `AlertStore.ListSimilar` | Store | Similar-alerts query | `internal/store/alert.go` (extend) |
| `Alert handler — mute/list-rules/unmute/similar/export.{csv,json,pdf}` | API | New HTTP endpoints | `internal/api/alert/handler.go` (extend) |
| `tenants.settings.alert_retention_days` validator | API | 422 on out-of-bounds | `internal/api/tenant/handler.go` (extend Update) |
| `AlertsRetentionProcessor` (per-tenant) | Job | Loop tenants, resolve effective days, purge | `internal/job/alerts_retention.go` (rewrite) |
| `report.ReportAlertsExport` + `AlertsExport` data provider + PDF builder | Report | PDF generation | `internal/report/{types.go,alerts_pdf.go,store_provider.go}` (extend + NEW) |
| `UpsertWithDedup` — apply suppression | Store | At-trigger-time mute application | `internal/store/alert.go` (extend) |
| Alerts page — Mute panel + Export menu + Similar drawer | UI | Rebuild Mute All; add format menu; row-expand similar list | `web/src/pages/alerts/index.tsx` (extend), `_partials/mute-panel.tsx` (NEW), `_partials/similar-alerts.tsx` (NEW), `_partials/export-menu.tsx` (NEW) |
| Suppression rule manager | UI | List/edit saved rules under Settings | `web/src/pages/settings/alert-rules.tsx` (NEW) |
| `useAlertExport` hook | UI | CSV/JSON/PDF download | `web/src/hooks/use-alert-export.ts` (NEW) |
| Retention setting in Settings page | UI | Number input 30-365, default 180 | `web/src/pages/settings/reliability.tsx` (extend — file already exists) |

### Data Flow — Mute (AC-1)
```
User → Alert row → "Mute" → SlidePanel opens
  → POST /api/v1/alerts/suppressions
     {scope_type, scope_value, duration|expires_at, reason, rule_name?}
→ AlertHandler.CreateSuppression
  → validate scope_type (enum), scope_value (UUID|string), duration→expires_at (max NOW+30d)
  → store.AlertSuppressionStore.Create(...) → returns row
  → audit "alert.suppression.created" {scope_type, scope_value, expires_at, rule_name?}
  → BACKFILL: apply to currently-open matching alerts
     UPDATE alerts SET state='suppressed', meta = meta || {'suppress_reason':...,'suppression_id':...}
       WHERE tenant_id=$1 AND state IN ('open','acknowledged') AND <scope match>
  → 201 {data: suppression DTO, applied_count: N}
```

### Data Flow — Trigger-time mute (DEV-334)
```
WS event publisher → handleAlertPersist → store.UpsertWithDedup
  → BEFORE the existing INSERT path, call store.AlertSuppressionStore.MatchActive(tenantID, alert):
      SELECT id FROM alert_suppressions
       WHERE tenant_id=$1 AND expires_at > NOW()
         AND (
              (scope_type='this'      AND scope_value::uuid = $2)
           OR (scope_type='type'      AND scope_value = $3)
           OR (scope_type='operator'  AND scope_value::uuid = $4)
           OR (scope_type='dedup_key' AND scope_value = $5)
         )
       ORDER BY created_at DESC LIMIT 1
  → IF row → INSERT alerts with state='suppressed', meta.suppression_id=<id>
  → ELSE  → existing INSERT/UPSERT path (state='open')
```

### Data Flow — Similar alerts (AC-3)
```
User → Alert row → expand → "Show similar" tab
  → GET /api/v1/alerts/{id}/similar?limit=20
→ AlertHandler.ListSimilar
  → load anchor alert (404 if not found / wrong tenant)
  → store.AlertStore.ListSimilar(tenantID, anchor, limit) executes:
       (anchor.dedup_key IS NOT NULL)
         → SELECT ... WHERE tenant_id=$1 AND id<>$2 AND dedup_key=$3 ORDER BY fired_at DESC LIMIT $4
       (anchor.dedup_key IS NULL)
         → SELECT ... WHERE tenant_id=$1 AND id<>$2 AND type=$3 AND source=$4 ORDER BY fired_at DESC LIMIT $4
  → 200 {status:success, data:[alertDTO...]}
```

### Data Flow — Export (AC-2)
```
User → Alerts page → "Export ▼" → CSV / JSON / PDF
  → GET /api/v1/alerts/export.{csv|json|pdf}?<same filters as list>
→ AlertHandler.Export{CSV,JSON,PDF}
  → enforce limit≤10000 server-side
  → CSV: stream rows with header — Content-Type: text/csv
  → JSON: marshal array — Content-Type: application/json; envelope NOT used (raw array)
  → PDF: report.Engine.Build({Type:ReportAlertsExport, Format:FormatPDF, TenantID, Filters})
        → DataProvider.AlertsExport(ctx, tenantID, filters) → AlertsExportData
        → buildAlertsPDF: rows table + summary header (count, severity breakdown, time window)
  → Content-Disposition: attachment; filename="alerts-<UTC-timestamp>.<ext>"
```

### Data Flow — Retention (AC-4)
```
Cron (03:15 UTC daily) → job.AlertsRetentionProcessor.Process
  → tenantStore.List(active=true, limit=1000) — paginated if needed
  → for each tenant:
      effectiveDays = tenant.settings->>'alert_retention_days' (parsed int)
                       ?? cfg.AlertsRetentionDays (default 180)
      cutoff = NOW() - effectiveDays days
      deleted = alertStore.DeleteOlderThanForTenant(tenantID, cutoff)
      perTenant = append(perTenant, {tenant_id, deleted, retention_days: effectiveDays, cutoff})
  → jobs.Complete(jobID, nil, JSON{total_deleted, per_tenant})
  → eventBus.Publish(JobCompleted, {total_deleted, tenants_processed})
```

---

## API Specifications

### `POST /api/v1/alerts/suppressions` — Create suppression (AC-1, AC-5)
- Auth required (any tenant member). Tenant-scoped.
- Request body:
  ```json
  {
    "scope_type": "type",
    "scope_value": "operator_down",
    "duration": "24h",            // OR
    "expires_at": "2026-04-26T12:00:00Z",
    "reason": "string ≤ 500",
    "rule_name": "Suppress operator-down during maintenance"  // optional — saves as rule
  }
  ```
- Validation:
  - `scope_type ∈ {this, type, operator, dedup_key}` else 400 `INVALID_FORMAT`
  - `scope_value` non-empty, max 255 chars
  - exactly one of `duration` (`1h`,`24h`,`7d`) or `expires_at` (RFC3339)
  - `expires_at` must be > NOW and ≤ NOW + 30d else 400 `VALIDATION_ERROR`
  - `reason` ≤ 500 chars (optional)
  - `rule_name` ≤ 100 chars; unique per tenant when present (409 `DUPLICATE` on conflict)
- Success: `201 {status:"success", data: {<suppression DTO>, applied_count: <int>}}`
- Audit: `alert.suppression.created`

### `GET /api/v1/alerts/suppressions` — List active rules (AC-5)
- Auth required.
- Query: `active_only=true|false` (default true), `cursor`, `limit` (1..100, default 50)
- Response: `{status:"success", data:[suppressionDTO], meta:{cursor, has_more, limit}}`

### `DELETE /api/v1/alerts/suppressions/{id}` — Unmute (AC-1, AC-5)
- Auth required.
- Optional body: `{ "reason": "string ≤ 500" }`
- Effect: row deleted; alerts currently in `state='suppressed'` whose `meta.suppression_id` matches MAY be transitioned back to `open` (best-effort UPDATE; do NOT block on failure — purely informational meta). Operator can manually transition via existing PATCH.
- Success: `200 {status:"success", data:{deleted_id, restored_count: <int>}}`
- Audit: `alert.suppression.deleted`

### `GET /api/v1/alerts/{id}/similar` — Similar alerts (AC-3)
- Auth required. Tenant-scoped.
- Query: `limit` (1..50, default 20)
- Anchor not found → 404 `ALERT_NOT_FOUND`
- Success: `{status:"success", data:[alertDTO], meta:{anchor_id, match_strategy:"dedup_key"|"type_source", count}}`

### `GET /api/v1/alerts/export.csv` — Export CSV (AC-2)
- Auth required. Same query params as `GET /alerts`.
- `Content-Type: text/csv`
- `Content-Disposition: attachment; filename="alerts-YYYYMMDD-HHMMSS.csv"`
- Rows ≤ 10 000; sorted by `fired_at DESC`. Columns:
  `id, fired_at, severity, state, source, type, title, description, sim_id, operator_id, apn_id, dedup_key, occurrence_count, first_seen_at, last_seen_at, acknowledged_at, resolved_at`
- Audit: `alert.exported` `{format:"csv", rows: <n>, filters: {...}}`

### `GET /api/v1/alerts/export.json` — Export JSON (AC-2)
- Same filters/cap. Response is a RAW array (NOT enveloped) to allow direct download → JSONL-friendly tools. Filename `alerts-YYYYMMDD-HHMMSS.json`.

### `GET /api/v1/alerts/export.pdf` — Export PDF (AC-2)
- Same filters/cap. Routed through `report.Engine.Build` (`ReportAlertsExport`).
- 404 `ALERT_NO_DATA` when 0 rows match. Filename `alerts-YYYYMMDD-HHMMSS.pdf`.
- PDF layout: header (tenant, time range, total count, severity breakdown), rows table (top 200 — overflow page footer says "+N more not shown — increase filters").

### `PATCH /api/v1/tenants/{id}` — Update tenant settings (AC-4 plumbing)
- Existing endpoint extended. When `settings.alert_retention_days` present:
  - integer 30..365 inclusive, else 422 `VALIDATION_ERROR` with detail
  - persisted into `tenants.settings` JSONB at top-level key
- Audit: existing `tenant.update` event already covers this.

---

## Database Schema

Source: **NEW migration (TBL-55)** — `migrations/20260426000001_alert_suppressions.{up,down}.sql`.

```sql
-- TBL-55 FIX-229 — Alert suppression rules (DEV-333: rule_name NULL = ad-hoc mute, NOT NULL = saved rule).
CREATE TABLE alert_suppressions (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  scope_type   VARCHAR(16) NOT NULL,
  scope_value  TEXT        NOT NULL,           -- UUID-as-text OR enum string OR dedup_key
  expires_at   TIMESTAMPTZ NOT NULL,
  reason       TEXT,                            -- ≤ 500 chars enforced at API
  rule_name    VARCHAR(100),                    -- NULL = ad-hoc; NOT NULL = saved rule (AC-5)
  created_by   UUID REFERENCES users(id),
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT chk_alert_suppressions_scope_type
    CHECK (scope_type IN ('this','type','operator','dedup_key')),
  CONSTRAINT chk_alert_suppressions_scope_value_nonempty
    CHECK (length(scope_value) > 0)
);

-- Active-suppression lookup at trigger time (DEV-334) — partial: only non-expired rows
CREATE INDEX idx_alert_suppressions_active
  ON alert_suppressions (tenant_id, scope_type, scope_value)
  WHERE expires_at > NOW();
-- NOTE on the partial predicate: NOW() is not IMMUTABLE. Use a non-partial index
-- on (tenant_id, scope_type, scope_value, expires_at) and apply expires_at > NOW()
-- in the WHERE clause at query time. Migration MUST use this form:
DROP INDEX IF EXISTS idx_alert_suppressions_active;
CREATE INDEX idx_alert_suppressions_active
  ON alert_suppressions (tenant_id, scope_type, scope_value, expires_at);

-- Tenant + rule list
CREATE INDEX idx_alert_suppressions_tenant_created
  ON alert_suppressions (tenant_id, created_at DESC);

-- Saved-rule unique name per tenant (partial — ad-hoc mutes have NULL name and are exempt)
CREATE UNIQUE INDEX uq_alert_suppressions_tenant_rule_name
  ON alert_suppressions (tenant_id, rule_name)
  WHERE rule_name IS NOT NULL;

-- Expired-row cleanup index (used by an inline DELETE on every list/match call)
CREATE INDEX idx_alert_suppressions_expires_at
  ON alert_suppressions (expires_at);

ALTER TABLE alert_suppressions ENABLE ROW LEVEL SECURITY;
ALTER TABLE alert_suppressions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_alert_suppressions ON alert_suppressions
  USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

COMMENT ON TABLE alert_suppressions IS
  'TBL-55 FIX-229 (DEV-333): alert mute rules. rule_name NULL = ad-hoc mute (AC-1); NOT NULL = saved rule (AC-5). Applied at trigger time via UpsertWithDedup (DEV-334).';
```

**Additional index on `alerts` table for similar-alerts lookup (DEV-339):**
```sql
CREATE INDEX idx_alerts_dedup_all_states
  ON alerts (tenant_id, dedup_key, fired_at DESC)
  WHERE dedup_key IS NOT NULL;
```

`.down.sql`:
```sql
DROP INDEX IF EXISTS idx_alerts_dedup_all_states;
DROP TABLE IF EXISTS alert_suppressions;
```

### Existing alerts table (TBL-53) — for reference (no schema changes beyond the new index)
Source: `migrations/20260422000001_alerts_table.up.sql` + `20260423000001_alerts_dedup_statemachine.up.sql`.

```sql
CREATE TABLE alerts (
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
  occurrence_count INT DEFAULT 1,         -- FIX-210
  first_seen_at    TIMESTAMPTZ,           -- FIX-210
  last_seen_at     TIMESTAMPTZ,           -- FIX-210
  cooldown_until   TIMESTAMPTZ,           -- FIX-210
  CONSTRAINT chk_alerts_severity CHECK (severity IN ('critical','high','medium','low','info')),
  CONSTRAINT chk_alerts_state    CHECK (state    IN ('open','acknowledged','resolved','suppressed')),
  CONSTRAINT chk_alerts_source   CHECK (source   IN ('sim','operator','infra','policy','system'))
);
-- RLS enabled (tenant_isolation_alerts).
```

### tenants.settings JSONB — DEV-335 contract
```json
{
  "alert_retention_days": 180,   // integer 30..365, default 180; absent = global cfg fallback
  "<other future keys>": "..."
}
```

---

## Screen Mockups

### Alerts page — Header changes (Mute All / Export menu)
```
┌──────────────────────────────────────────────────────────────────────┐
│ Dashboard / Alerts                                                   │
│                                                                      │
│ Alerts & Incidents  [3 critical pulse]    [Mute ▼] [Export ▼] [↻]    │
│                                            │        │                │
│                                            │        ├ CSV            │
│                                            │        ├ JSON           │
│                                            │        └ PDF            │
│                                            ├ Mute matching filters…  │
│                                            ├ ─────────────────────── │
│                                            └ Manage saved rules…     │
├──────────────────────────────────────────────────────────────────────┤
│ [stat cards same as today]                                           │
│ [filter pills + search]                                              │
└──────────────────────────────────────────────────────────────────────┘
```
- "Mute" ▼ menu items:
  - "Mute matching filters…" → opens MutePanel pre-populated from current filters
  - "Manage saved rules…" → navigates to `/settings/alert-rules`

### Alert row — expanded (similar alerts subtab)

> **Plan amendment (2026-04-25, Gate F-A2):** Comments-as-tab was dropped in
> implementation. The row already exposes a Comments icon button that opens
> the existing `CommentThread` SlidePanel — duplicating it as a tab below
> Similar would split the same surface into two competing entry points and
> add no information density. **AC-3 does not require Comments-as-tab**;
> only `Details | Similar(N)` are AC-bearing. Decision recorded; mockup
> updated to reflect the 2-tab shape.

```
┌────────────────────────────────────────────────────────────────────┐
│ [sev][SEV][state][src] Operator connectivity failure detected  ↑   │
├────────────────────────────────────────────────────────────────────┤
│ [Type] [Source] [Fired] [Status]                                   │
│ [Impact] [Runbook]                                                 │
│                                                                    │
│ ┌─ Tabs ──────────────────────┐                                    │
│ │ Details │ Similar (12)      │  (Comments dropped — see below)    │
│ └──────────────────────────────┘                                    │
│                                                                    │
│ Similar tab content:                                               │
│ • [crit][open]  Same dedup_key — fired 12m ago — operator: T1     │
│ • [high][res]   Same dedup_key — fired 1h ago  — operator: T1     │
│ • ...                                                              │
│                                                  [View all →]      │
└────────────────────────────────────────────────────────────────────┘
```

### Mute SlidePanel (DEV-342, AC-1, AC-5)
```
┌─ SlidePanel (lg) ────────────────────────────────────────────┐
│ Mute Alert                                                  ×│
│ Suppress matching alerts for a configurable duration.        │
├──────────────────────────────────────────────────────────────┤
│ Scope                                                        │
│ ◉ This alert                                                 │
│ ○ All alerts of type "operator_down"                         │
│ ○ All alerts on operator "Turkcell"                          │
│ ○ All alerts matching dedup_key "operator:turkcell:offline"  │
│                                                              │
│ Duration                                                     │
│ ◉ 1 hour     ○ 24 hours    ○ 7 days    ○ Custom              │
│   (Custom)   [📅 datetime picker — max NOW + 30d]            │
│                                                              │
│ Reason (optional)                                            │
│ [textarea ≤ 500 chars]                                       │
│                                                              │
│ ☐ Save as reusable rule                                      │
│   Rule name: [_______________________________]               │
├──────────────────────────────────────────────────────────────┤
│                              [ Cancel ]   [ Mute ]           │
└──────────────────────────────────────────────────────────────┘
```

### Settings → Alert Rules (manager — AC-5)
```
┌────────────────────────────────────────────────────────────────┐
│ Settings / Alert Rules                                         │
│                                                                │
│ ┌─ Active rules (3) ───────────────────────────────────────┐  │
│ │ Name          Scope             Expires       Created    │  │
│ │ Maint window  type=op_down      in 22h        admin@…    │  │
│ │ APN drift     dedup=apn:foo:..  in 5d 12h     ops@…      │  │
│ │ ...                                                       │  │
│ └────────────────────────────────────────────────────────────┘ │
│                                                                │
│ ┌─ Retention ───────────────────────────────────────────────┐ │
│ │ Keep alerts for [180] days  (30 ≤ N ≤ 365)               │ │
│ │ [ Save ]                                                  │ │
│ └────────────────────────────────────────────────────────────┘ │
└────────────────────────────────────────────────────────────────┘
```

---

## Design Token Map (UI tasks — MANDATORY per FRONTEND.md)

#### Color Tokens (from `docs/FRONTEND.md`)
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Page bg | `bg-bg-primary` | `bg-[#06060B]`, `bg-black` |
| Card / panel surface | `bg-bg-surface` | `bg-white`, `bg-[#0C0C14]` |
| Elevated / dropdown | `bg-bg-elevated` | `bg-gray-800`, `bg-[#12121C]` |
| Hover state | `bg-bg-hover` | arbitrary opacity hex |
| Primary text | `text-text-primary` | `text-[#E4E4ED]`, `text-gray-100` |
| Secondary text | `text-text-secondary` | `text-gray-400`, hex |
| Muted / placeholder | `text-text-tertiary` | `text-gray-600`, hex |
| Primary accent | `text-accent` / `bg-accent` | `text-cyan-500`, hex |
| Accent dim bg | `bg-accent-dim` | rgba inline |
| Critical / open severity | `text-danger` / `bg-danger-dim` / `border-danger/40` | `text-red-500`, `bg-red-100` |
| Warning / acknowledged | `text-warning` / `bg-warning-dim` | `text-yellow-500` |
| Success / resolved | `text-success` / `bg-success-dim` | `text-green-500` |
| Info / suppressed badge | `text-info` (info), `bg-bg-elevated text-text-tertiary` (suppressed) | `text-blue-500` |
| Standard border | `border-border` | `border-gray-200`, `border-[#1E1E30]` |
| Subtle row border | `border-border-subtle` | hex |

#### Typography Tokens
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Page title | `text-xl font-semibold text-text-primary` (matches existing) | `text-[24px]` |
| Section label | `text-[11px] uppercase tracking-wider text-text-secondary` | `text-xs text-gray-500` |
| Body | `text-sm` (14px) | `text-[14px]` |
| Mono data | `font-mono text-xs` | hardcoded family |
| Caption | `text-[10px] uppercase tracking-wider text-text-tertiary` | `text-2xs` |

#### Spacing & Elevation
| Usage | Token | NEVER Use |
|-------|-------|-----------|
| Card radius | `rounded-[var(--radius-md)]` | `rounded-md`, `rounded-lg` |
| Small radius (badges, pills) | `rounded-[var(--radius-sm)]` | arbitrary px |
| Card shadow | `shadow-[var(--shadow-card)]` | none/arbitrary |
| Section padding | `p-4` (16px), `p-section` for templates | `p-[20px]` |
| Inter-row gap | `gap-3` / `gap-4` | arbitrary |

#### Existing Components to REUSE — NEVER recreate
| Component | Path | Use For |
|-----------|------|---------|
| `<Button>` | `web/src/components/ui/button.tsx` | ALL buttons — NEVER raw `<button>` |
| `<Input>` | `web/src/components/ui/input.tsx` | Text inputs |
| `<Select>` | `web/src/components/ui/select.tsx` | Native selects |
| `<Textarea>` | `web/src/components/ui/textarea.tsx` | Multi-line input (reason field) |
| `<Dialog>` family | `web/src/components/ui/dialog.tsx` | Confirm flows (unmute confirm) |
| `<SlidePanel>` + `<SlidePanelFooter>` | `web/src/components/ui/slide-panel.tsx` | Mute panel (rich form) — pass `title` and `description` props; do NOT hand-roll a header |
| `<Badge>` | `web/src/components/ui/badge.tsx` | Severity / state pills |
| `<SeverityBadge>` | `web/src/components/shared/severity-badge.tsx` | 5-level severity (FIX-211) — REUSE, do not duplicate |
| `<Skeleton>` | `web/src/components/ui/skeleton.tsx` | Loading shimmers |
| `<Spinner>` | `web/src/components/ui/spinner.tsx` | Inline progress |
| `<Breadcrumb>` | `web/src/components/ui/breadcrumb.tsx` | Page header trail |
| `<Tabs>` (shadcn) | `web/src/components/ui/tabs.tsx` if present, else compose with existing pattern | Row-expand subtabs (Details / Similar / Comments) |
| `<RowActionsMenu>` | `web/src/components/shared/row-actions-menu.tsx` | Per-row dropdown |
| `<EntityLink>` | `web/src/components/shared/entity-link.tsx` | SIM/operator/APN reference |
| `useAlerts` query | `web/src/pages/alerts/index.tsx` (existing hook) | Alert list with filters — REUSE |
| Format helpers | `web/src/lib/format.ts` (`timeAgo`, `formatNumber`) | Time + numeric formatting |

**RULE**: any NEW class used in the new files that is not on this map must be reviewed at Gate.

---

## Story-Specific Compliance Rules

- **API**: Standard envelope `{status, data, meta?, error?}` for all JSON responses EXCEPT the three export endpoints (which return raw bytes with `Content-Disposition: attachment`). Document this asymmetry in handler comments.
- **Tenant scoping**: every new query MUST be scoped by `tenant_id` from `apierr.TenantIDKey` context — verified at API edge AND in store WHERE clauses (defense-in-depth; RLS already enforces).
- **Audit**: every state-changing endpoint emits an audit log entry via `audit.Emit(r, h.logger, h.auditSvc, …)`. Specifically `alert.suppression.created`, `alert.suppression.deleted`, `alert.exported` (format + row count + filters).
- **Modal pattern (FIX-216)**: SlidePanel for Mute (4+ fields including conditionals); Dialog for unmute confirm; pass `title` + `description` props to SlidePanel; NEVER hand-roll a header.
- **Tokens (PAT-018)**: zero default Tailwind palette utilities (`text-red-500`, `bg-blue-300`, etc.); zero hardcoded hex; zero raw `<button>`/`<input>`.
- **Pagination**: cursor-based for `GET /suppressions` and `GET /alerts/{id}/similar` (existing pattern).
- **DB**: migration up + down required; partial indexes use `IS NOT NULL` predicates only (NOT `NOW()` — see DEV-339 inline note in schema). RLS enabled with tenant_isolation policy.
- **PDF (FIX-215 pattern)**: `report.Engine.Build`, raw bytes write, `Content-Type: application/pdf`, `Content-Length` set, no envelope.
- **Per-tenant retention floor**: store/processor enforces min 30 days even if user requests less — same as existing `cfg.AlertsRetentionDays < 30 → 30` clamp in config.

---

## Bug Pattern Warnings (relevant subset)

- **PAT-016 (FIX-209)** — Cross-store PK confusion. The new `useAlertExport` hook MUST use `/api/v1/alerts/export.*` (anchored to alert IDs in filters, not anomaly IDs). Verify before commit by `grep -rn 'analytics/anomalies' web/src/pages/alerts web/src/hooks/use-alert-export.ts` → must return ZERO matches in alert code paths.
- **PAT-017 (FIX-210)** — Config-wiring trace: `cfg.AlertsRetentionDays` continues to flow as the FALLBACK; the per-tenant `tenant.settings.alert_retention_days` is the new primary. Trace must show: (1) tenant Update API validation, (2) `TenantStore.Update` JSON write, (3) processor read of `tenants.settings`, (4) effective-days resolution, (5) processor `DeleteOlderThanForTenant` call. ANY missing link is a CRITICAL Gate finding.
- **PAT-018 (FIX-227)** — Default Tailwind palette ban. All new `.tsx` files (`mute-panel.tsx`, `similar-alerts.tsx`, `export-menu.tsx`, `settings/alert-rules.tsx`) MUST be greppable-clean for `\btext-(red|blue|green|purple|...)-[0-9]{2,3}\b` and `\bbg-(...)-[0-9]{2,3}\b`. Use Design Token Map.
- **PAT-019 (FIX-228)** — Typed-nil interface trap. The PDF export path may take an optional `report.Engine` (if a tenant has reports disabled). If you wire the engine, declare it as the INTERFACE type (`var engine *report.Engine` is fine because `Engine` is a struct, not an interface — confirm at Gate). If you introduce ANY interface-typed optional dep, prefer `var iface SomeInterface; if cfg.X { iface = newConcrete() }` over `var ptr *Concrete; … pass ptr`.

---

## Tech Debt (record at Gate)

- **D-091 (FIX-229 → FIX-248):** Alerts PDF export is built via the synchronous report engine path. When FIX-248 (Reports Subsystem Refactor) lands, migrate `ReportAlertsExport` to async/queued generation with a download-link callback. Until then, the 10 000-row cap protects request latency.
- **D-092 (FIX-229):** Backfill of currently-open matching alerts on suppression Create is a single UPDATE without explicit batching. For tenants with >100 K open alerts the UPDATE could lock the index; if observed in prod, batch in 10 K chunks. Currently low-priority; document.

---

## Mock Retirement
Existing FE bug deletions (NOT mocks per se):
- `web/src/pages/alerts/index.tsx`: REMOVE the `setMuted(!muted)` local state lie (line ~735) — replace with the new `MutePanel` open trigger.
- `web/src/pages/alerts/index.tsx`: REPLACE `useExport('analytics/anomalies')` (line ~646) with the new `useAlertExport()` hook bound to `alerts/export.{format}`. **The wrong endpoint MUST be deleted from this file** — Gate scout greps for it.

---

## Risks & Mitigations

| Risk | Mitigation |
|------|-----------|
| **R1 — Over-aggressive mute hides real issues.** | Hard cap `expires_at ≤ NOW + 30d` (DEV-341); audit row on every Create AND Delete; saved rules visible on Settings → Alert Rules with countdown to expiry; daily-digest notification of currently-muted scopes (FUTURE — record as D-093). |
| **R2 — Trigger-time suppression apply path adds latency to alert ingest hot path.** | New active-suppression index `idx_alert_suppressions_active` is `(tenant_id, scope_type, scope_value, expires_at)` — primary lookup is one indexed scan + `expires_at > NOW()` filter. Most tenants have <10 active suppressions; lookup is sub-ms. Benchmark in Gate UAT with 1000 active rows. |
| **R3 — Per-tenant retention loop in retention job may exceed schedule window.** | List active tenants in 1000-row pages; for each, run DELETE with `LIMIT 100000` per call (loop until 0 affected). Total deletes capped per run; remainder picks up next day. Expose `total_deleted` and `tenants_processed` in result JSON for observability. |
| **R4 — Spec deviation: AC-2 says "PDF via reports engine (FIX-248)" but FIX-248 not built yet.** | Use the synchronous `report.Engine.Build` path (FIX-215 pattern) — same engine, just no async queue. Recorded as D-091. AC-2 remains satisfied because PDF generation succeeds and is rendered through the engine — only the queue layer is deferred. |
| **R5 — Similar-alerts query without dedup_key falls back to type+source — may match many rows.** | Server-side cap `limit ≤ 50` (default 20); ordered by `fired_at DESC`; client UI shows "+N more not shown" hint when count == limit. |
| **R6 — JSON export is non-enveloped (raw array) — clients expecting envelope break.** | Document explicitly in handler comment + API spec; downloaded file MIME is `application/json` (NOT a JSON-API endpoint). Mirrors the CSV which is also a raw stream. |
| **R7 — Backfill UPDATE on suppression Create may transition alerts already in `acknowledged` to `suppressed`** — semantically dubious. | Backfill only targets `state='open'` rows. Acknowledged rows are left untouched; user explicitly already engaged with them. State-machine compliance via `alertstate.CanTransition('open','suppressed')` (already allowed). |

---

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|----------------|-------------|
| **AC-1** Mute scope (this/type/operator/dedupe_key) × duration (1h/24h/7d/Custom) | Task 2 (migration), Task 3 (store), Task 4 (suppression API), Task 8 (Mute SlidePanel) | Task 11 backend tests; Gate UAT step |
| **AC-2** Export CSV + PDF + JSON | Task 6 (CSV/JSON), Task 7 (PDF via engine), Task 9 (Export menu + hook) | Task 12 export tests; manual download check |
| **AC-3** Show similar alerts | Task 5 (store + handler + index), Task 10 (similar tab in row-expand) | Task 11 store tests; Gate UAT |
| **AC-4** Tenant retention setting (default 180, max 365) + purge | Task 1 (settings validation), Task 2 (no schema change — JSONB), Task 3 (per-tenant store delete), Task 13 (per-tenant processor rewrite + scheduler stays) | Task 14 retention test; integration UAT |
| **AC-5** Saved suppression rules with expiration | Task 2 (rule_name column + unique index), Task 4 (Create accepts rule_name), Task 8 (toggle in panel), Task 9 (settings/alert-rules manager) | Task 12 saved-rule tests |

---

## Tasks

### Wave 0 — Foundation (parallel-safe)

#### Task 1: Tenant settings — `alert_retention_days` validation (AC-4 plumbing)
- **Files:** Modify `internal/api/tenant/handler.go` (Update path)
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `internal/api/tenant/handler.go` lines around `Update` — follow the existing `UpdateTenantParams` settings pass-through. Validate the JSONB content BEFORE calling `tenantStore.Update`.
- **Context refs:** Pinned Decisions DEV-335, DEV-344; API Specifications `PATCH /tenants/{id}`; tenants.settings JSONB contract
- **What:** When the incoming PATCH body contains `settings.alert_retention_days`, parse it as integer and validate `30 ≤ N ≤ 365`. If out of bounds → `apierr.WriteError(422, CodeValidationError, "alert_retention_days must be between 30 and 365")`. If valid → forward the entire settings JSON unchanged. Other settings keys pass through untouched.
- **Verify:** `go test ./internal/api/tenant/...` passes including a new sub-test asserting 422 on `alert_retention_days=10` and `=400`, and 200 on `=180`.

### Wave 1 — Schema

#### Task 2: Migration — `alert_suppressions` table + similar-alerts index
- **Files:** Create `migrations/20260426000001_alert_suppressions.up.sql`, Create `migrations/20260426000001_alert_suppressions.down.sql`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `migrations/20260422000001_alerts_table.up.sql` and `migrations/20260425000001_password_reset_tokens.up.sql` for column types, indexes, RLS, and `COMMENT ON TABLE` style.
- **Context refs:** Database Schema (full); DEV-333, DEV-339; tenant_isolation_alerts policy reference
- **What:** Create the `alert_suppressions` table EXACTLY as in Database Schema section (including the corrected non-partial active index — partial index with `NOW()` is invalid Postgres). Add `idx_alerts_dedup_all_states` to the alerts table. Down migration drops both. Update `docs/architecture/db/_index.md` to add a TBL-55 row (mirror TBL-54 row format).
- **Verify:** `make db-migrate` succeeds locally; `make db-migrate-down` then `make db-migrate` round-trips clean. `psql -c "\d alert_suppressions"` shows all 4 indexes and the RLS policy. `psql -c "\di alerts"` shows the new `idx_alerts_dedup_all_states`.

### Wave 2 — Store layer (parallel-safe; all depend only on Task 2)

#### Task 3: `AlertSuppressionStore` — CRUD + MatchActive + per-tenant retention
- **Files:** Create `internal/store/alert_suppression.go`, Modify `internal/store/alert.go` (add `DeleteOlderThanForTenant`, `ListSimilar`)
- **Depends on:** Task 2
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/alert.go` (existing `AlertStore`) for pgx query/scan patterns, ErrXxxNotFound conventions, cursor pagination. Read `internal/store/password_reset.go` if present for token-style stores.
- **Context refs:** Database Schema; Data Flow — Trigger-time mute (DEV-334); DEV-333, DEV-336, DEV-339, DEV-340; API Specifications (CRUD endpoints)
- **What:**
  1. `AlertSuppression` struct mirroring DB columns.
  2. `NewAlertSuppressionStore(db *pgxpool.Pool) *AlertSuppressionStore`.
  3. `Create(ctx, params) (*AlertSuppression, error)` — with `ErrDuplicateRuleName` on unique violation (`uq_alert_suppressions_tenant_rule_name`).
  4. `List(ctx, tenantID, params) ([]AlertSuppression, *uuid.UUID, error)` — cursor-based, supports `active_only` filter (`expires_at > NOW()`).
  5. `Delete(ctx, tenantID, id) error` — RLS scoped; returns `ErrSuppressionNotFound` if 0 affected.
  6. `MatchActive(ctx, tenantID, alert AlertMatchProbe) (*AlertSuppression, error)` — returns first active row matching any of the four scope types (this/type/operator/dedup_key). Uses `idx_alert_suppressions_active`.
  7. In `internal/store/alert.go` add:
     - `DeleteOlderThanForTenant(ctx, tenantID, cutoff) (int64, error)` — same shape as `DeleteOlderThan` but with `tenant_id = $1` filter.
     - `ListSimilar(ctx, tenantID, anchor *Alert, limit int) ([]Alert, string, error)` returning match_strategy ("dedup_key" or "type_source").
- **Verify:** `go test ./internal/store/...` passes including new tests:
  - `TestAlertSuppressionStore_CreateAndMatchActive_TypeScope`
  - `TestAlertSuppressionStore_DuplicateRuleNameReturns Error`
  - `TestAlertStore_DeleteOlderThanForTenant_ScopesCorrectly`
  - `TestAlertStore_ListSimilar_DedupKeyMatchAndTypeSourceFallback`

#### Task 4: Apply trigger-time suppression in `UpsertWithDedup` (DEV-334)
- **Files:** Modify `internal/store/alert.go` (extend `UpsertWithDedup`)
- **Depends on:** Task 3
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/alert.go:160-244` (current `UpsertWithDedup`). Add the `MatchActive` lookup BEFORE the existing INSERT path; on match, set `state='suppressed'` and embed `suppression_id` in the meta JSON.
- **Context refs:** Data Flow — Trigger-time mute; DEV-334; existing `UpsertWithDedup` SQL; partial unique index `idx_alerts_dedup_unique`
- **What:** Inject an optional `suppressionStore *AlertSuppressionStore` field into `AlertStore` (constructor remains backward-compat: `WithSuppressionStore(s)` builder OR pass via params). When present, call `suppressionStore.MatchActive(...)` BEFORE the INSERT/UPSERT; on match, override the `state` literal in the SQL from `'open'` to `'suppressed'` and merge `{"suppression_id": <id>}` into the meta JSONB. Existing dedup logic untouched. Verify the existing partial unique index still applies (`state IN ('open','acknowledged','suppressed')` already includes `suppressed` per FIX-210).
- **Verify:** New test `TestUpsertWithDedup_AppliesActiveSuppression` — create suppression, fire matching alert, assert resulting row has `state='suppressed'` and `meta.suppression_id` matches. Existing tests still pass.

### Wave 3 — API handlers (parallel-safe — different routes)

#### Task 5: Similar-alerts handler + route (AC-3)
- **Files:** Modify `internal/api/alert/handler.go` (add `ListSimilar`), Modify `internal/gateway/router.go` (add route)
- **Depends on:** Task 3
- **Complexity:** low
- **Pattern ref:** Read `internal/api/alert/handler.go:239-264` (existing `Get` handler) for handler structure, tenant context check, `chi.URLParam`, error mapping. Read `internal/gateway/router.go:649-651` for alert route registration site.
- **Context refs:** API Spec `GET /alerts/{id}/similar`; Data Flow — Similar alerts; DEV-339
- **What:** New handler method `(h *Handler) ListSimilar(w, r)`: parse `id` URL param, parse `limit` (1..50, default 20), load anchor via `h.alertStore.GetByID` (404 if not found), call `h.alertStore.ListSimilar(...)`, build envelope `{data:[alertDTO], meta:{anchor_id, match_strategy, count}}`. Register route `r.Get("/api/v1/alerts/{id}/similar", deps.AlertHandler.ListSimilar)` in the same group as existing alert routes.
- **Verify:** `curl http://localhost:8080/api/v1/alerts/<id>/similar -H "Authorization: Bearer …"` returns array; new handler test `TestListSimilar_DedupKeyMatch` and `TestListSimilar_AnchorNotFound`.

#### Task 6: Export CSV + JSON handlers + routes (AC-2 part 1)
- **Files:** Modify `internal/api/alert/handler.go` (add `ExportCSV`, `ExportJSON`), Modify `internal/gateway/router.go` (add 2 routes)
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/job/cdr_export.go` if present for CSV streaming pattern; read existing FIX-215 SLA `DownloadPDF` (`internal/api/sla/handler.go:340-451`) for non-enveloped file response (`Content-Type`, `Content-Disposition`, `Content-Length`, raw `w.Write(bytes)`). For CSV, use `encoding/csv` standard library on `w` directly.
- **Context refs:** API Spec `/alerts/export.csv` + `/alerts/export.json`; DEV-337; standard-envelope asymmetry note
- **What:**
  - `ExportCSV(w, r)`: parse same query filters as `List`, cap limit at 10000, call `alertStore.ListByTenant(... limit=10000)` (no cursor), set headers (`text/csv`, `attachment; filename="alerts-<UTC>.csv"`), `csv.NewWriter(w)`, write header row + data rows, audit `alert.exported {format:"csv", rows: N}`.
  - `ExportJSON(w, r)`: same filters/cap, set `application/json`, `attachment; filename="alerts-<UTC>.json"`, `json.NewEncoder(w).Encode(alerts)` (raw array), audit.
- Both: tenant-scoped via existing context middleware. Routes: `r.Get("/api/v1/alerts/export.csv", deps.AlertHandler.ExportCSV)` and similar for `.json`.
- **Verify:** `curl … /alerts/export.csv?severity=critical -o out.csv` produces valid CSV; row count in audit matches actual rows. Test: `TestExportCSV_StreamsRows`, `TestExportJSON_RawArray`.

#### Task 7: Export PDF — extend report engine + handler + route (AC-2 part 2)
- **Files:** Modify `internal/report/types.go` (add `ReportAlertsExport`, `AlertsExportData` struct, extend `DataProvider` interface), Create `internal/report/alerts_pdf.go`, Modify `internal/report/store_provider.go` (implement `AlertsExport` on the provider), Modify `internal/api/alert/handler.go` (add `ExportPDF`), Modify `internal/gateway/router.go` (add route), Modify `cmd/argus/main.go` (wire `report.Engine` into `alertHandler` if not already wired)
- **Depends on:** Task 6 (shares filter parsing helper)
- **Complexity:** high (multi-file engine extension + Go-PDF table layout)
- **Pattern ref:** Read `internal/report/pdf.go` (existing PDF builder layout). Read `internal/api/sla/handler.go:340-451` for handler streaming pattern. Read `internal/report/types.go:140-165` for `Engine.Build` dispatch. Read `internal/report/store_provider.go` for `DataProvider` impl pattern.
- **Context refs:** API Spec `/alerts/export.pdf`; DEV-338; Risks R4 (FIX-248 deferral); FRONTEND.md PDF visual contract for header/summary
- **What:**
  1. In `types.go`: add `ReportAlertsExport ReportType = "alerts_export"`, struct `AlertsExportData {Sections []Section; GeneratedAt time.Time; TenantID uuid.UUID; Filters map[string]any; TotalRows int; SeverityBreakdown map[string]int}`, extend `DataProvider` interface with `AlertsExport(ctx, tenantID, filters) (*AlertsExportData, error)`.
  2. In `store_provider.go`: implement `AlertsExport` — parse filters into `ListAlertsParams`, call `AlertStore.ListByTenant(... limit=10000)`, build sections + breakdown.
  3. In `alerts_pdf.go`: `buildAlertsPDF(data *AlertsExportData) (*Artifact, error)` — header (tenant, time window, total + severity breakdown), table of top 200 rows (overflow footer "+N more"), use existing PDF helpers from `pdf.go`.
  4. In `Engine.Build` dispatch (already switches on Format → buildPDF): no change at top level; in `buildPDF`, switch on `req.Type` add `case ReportAlertsExport`.
  5. In `alert/handler.go`: `ExportPDF(w, r)` — parse filters into a `map[string]any`, call `h.engine.Build({Type: ReportAlertsExport, Format: FormatPDF, TenantID: tenantID, Filters: filters})`, on `len(artifact.Bytes)==0` return 404 `ALERT_NO_DATA`, else stream as in SLA handler.
  6. In `cmd/argus/main.go`: pass `reportEngine` into `alertapi.NewHandler` (or a `WithReportEngine(e)` builder method on `alert.Handler`) wired alongside existing `alertHandler` construction at line ~695.
- Wire route: `r.Get("/api/v1/alerts/export.pdf", deps.AlertHandler.ExportPDF)`.
- **Verify:** `curl … /alerts/export.pdf -o out.pdf` produces valid PDF (`pdfinfo out.pdf` shows pages>0). Test: `TestExportPDF_BuildsArtifact`, `TestExportPDF_NoData_Returns404`. Existing report engine tests still pass.

#### Task 8: Suppression CRUD handlers + routes (AC-1 + AC-5 backend)
- **Files:** Modify `internal/api/alert/handler.go` (add `CreateSuppression`, `ListSuppressions`, `DeleteSuppression`), Modify `internal/gateway/router.go` (add 3 routes), Modify `cmd/argus/main.go` (wire `alertSuppressionStore` into handler)
- **Depends on:** Task 3
- **Complexity:** high (validation + scope_value parsing + backfill UPDATE + audit + 3 endpoints)
- **Pattern ref:** Read `internal/api/alert/handler.go:271-330` for `UpdateState` (validation + audit pattern). Read `internal/api/auth/password_reset.go` (FIX-228) for body parse + JSON 400/422 patterns. Read FIX-210 cooldown wiring (`internal/api/alert/handler.go:35-45`) for handler config injection.
- **Context refs:** API Specs `POST /suppressions`, `GET /suppressions`, `DELETE /suppressions/{id}`; DEV-340, DEV-341, DEV-342; Data Flow — Mute; Risks R1, R7
- **What:**
  - `CreateSuppression`: parse body, validate scope_type ∈ enum, scope_value non-empty + max 255, exactly-one-of(duration, expires_at), resolve duration to absolute `expires_at`, enforce ≤ NOW+30d, optional rule_name ≤100, optional reason ≤500. Call `store.AlertSuppressionStore.Create`. On `ErrDuplicateRuleName` → 409. After create, run backfill UPDATE: `UPDATE alerts SET state='suppressed', meta = meta || jsonb_build_object('suppression_id', $1, 'suppress_reason', $2) WHERE tenant_id=$3 AND state='open' AND <scope match>` — capture affected count. Audit `alert.suppression.created` with full DTO. Respond 201 with `{data, applied_count}`.
  - `ListSuppressions`: cursor + active_only filter, mirror existing `List` cursor pagination.
  - `DeleteSuppression`: row delete (404 on miss); best-effort UPDATE alerts back to `open` where `meta.suppression_id = $1` AND `state='suppressed'`. Audit `alert.suppression.deleted`. Respond `{deleted_id, restored_count}`.
- Routes: `r.Post("/api/v1/alerts/suppressions", …)`, `r.Get("/api/v1/alerts/suppressions", …)`, `r.Delete("/api/v1/alerts/suppressions/{id}", …)`.
- Wire `alertSuppressionStore` into `alertHandler` constructor at `cmd/argus/main.go:695`.
- **Verify:** `go test ./internal/api/alert/...` passes new tests: `TestCreateSuppression_TypeScope_Backfills`, `TestCreateSuppression_RejectsExpiresOver30d`, `TestCreateSuppression_DuplicateRuleName_Returns409`, `TestDeleteSuppression_RestoresAlerts`. Manual curl flow.

### Wave 4 — Frontend (sequential — share components)

#### Task 9: `useAlertExport` hook + ExportMenu + remove old useExport call
- **Files:** Create `web/src/hooks/use-alert-export.ts`, Create `web/src/pages/alerts/_partials/export-menu.tsx`, Modify `web/src/pages/alerts/index.tsx` (replace export button)
- **Depends on:** Task 6, Task 7
- **Complexity:** medium
- **Pattern ref:** Read `web/src/hooks/use-export.ts` for blob+download pattern. Read `web/src/components/ui/dropdown-menu.tsx` (or existing dropdown if present) for menu structure.
- **Context refs:** DEV-337, DEV-343; Mock Retirement (delete the wrong useExport call); Design Token Map; existing alerts page header layout
- **What:**
  - `useAlertExport(): { exportAs(format: 'csv'|'json'|'pdf', filters: Record<string,string>): Promise<void>, exporting: boolean }` — single function dispatching to `/api/v1/alerts/export.${format}` with filters as query string; handles content-disposition filename extraction; toast loading + success/error.
  - `ExportMenu`: dropdown trigger button + 3 items (CSV/JSON/PDF) → calls `exportAs(...)`.
  - In `index.tsx`: REMOVE `const { exportCSV, exporting } = useExport('analytics/anomalies')` line. REMOVE the existing Export button. Mount `<ExportMenu filters={filters} />`. Verify the import line for `useExport` is removed if no longer used (it isn't elsewhere in this file).
- **Tokens:** Use ONLY classes from Design Token Map. Buttons: `<Button>` only. Menu surface: `bg-bg-elevated border-border rounded-[var(--radius-sm)]`.
- **Components:** Reuse `<Button>`, `<Spinner>`, dropdown primitives. Toast via `sonner` (existing).
- **Note:** Invoke `frontend-design` skill before writing the menu visuals.
- **Verify:** `grep -rn "analytics/anomalies" web/src/pages/alerts web/src/hooks/use-alert-export.ts` → ZERO matches. `grep -rnE "\btext-(red|blue|green|purple|pink|orange|yellow|amber|cyan|teal|sky|indigo|violet|fuchsia|rose)-[0-9]{2,3}\b" web/src/hooks/use-alert-export.ts web/src/pages/alerts/_partials/export-menu.tsx` → ZERO matches. Manual: click each format → file downloads with correct extension.

#### Task 10: MutePanel SlidePanel + UnmuteDialog + wire into alerts page
- **Files:** Create `web/src/pages/alerts/_partials/mute-panel.tsx`, Modify `web/src/pages/alerts/index.tsx` (replace fake `setMuted` with panel open trigger; add row-level mute action)
- **Depends on:** Task 8
- **Complexity:** high (4-field form with conditional rule-name field + backend wiring + per-row + per-page invocations + state + a11y)
- **Pattern ref:** Read `web/src/pages/alerts/_partials/alert-actions.tsx` (existing AckDialog/ResolveDialog) for Dialog pattern. Read `web/src/pages/alerts/_partials/comment-thread.tsx` for SlidePanel mounting pattern. Read `web/src/components/ui/slide-panel.tsx` exports — pass `title` + `description` props per FIX-216 contract.
- **Context refs:** DEV-340, DEV-341, DEV-342, DEV-344; API Spec `POST /suppressions`, `DELETE /suppressions/{id}`; Mute SlidePanel mockup; Design Token Map; Modal Pattern (FRONTEND.md)
- **What:**
  - `MutePanel({open, onClose, anchorAlert?, defaultScope?, defaultFilters?})`:
    - Scope radio (4 options); when `anchorAlert` present, pre-populate scope_value; when not (page-level "Mute matching filters"), use `defaultFilters` to pre-populate scope=type+ first matching filter.
    - Duration radio + custom datetime picker (uses `<Input type="datetime-local">`); enforce ≤ NOW+30d client-side (disable Submit + show inline error).
    - Reason `<Textarea>`; max 500 chars.
    - "Save as reusable rule" toggle (`<Checkbox>`/`<Switch>`) revealing rule_name `<Input>`.
    - On submit: POST `/api/v1/alerts/suppressions`; on 201 toast success + invalidate `['alerts']` query + close. On 409 `DUPLICATE` show field-level error on rule_name input.
  - In `index.tsx`:
    - REPLACE `const [muted, setMuted] = useState(false)` and `setMuted(!muted)` lines with `const [mutePanel, setMutePanel] = useState<{open:boolean, anchor?: Alert}>({open:false})`.
    - Header "Mute" button → opens dropdown menu (`Mute matching filters` opens panel with `defaultFilters=filters`; `Manage saved rules` navigates `/settings/alert-rules`).
    - Inside `RowActionsMenu` (currently has View Details / View SIM): add `{label:'Mute…', onClick: () => setMutePanel({open:true, anchor:alert})}`.
  - `UnmuteDialog({open, onClose, suppressionId})` — Dialog with optional reason → DELETE `/api/v1/alerts/suppressions/{id}`.
- **Tokens:** SlidePanel default `bg-bg-surface`; form fields use existing Input/Textarea/Select; submit button `<Button variant="default">`; cancel `<Button variant="outline">`. NO hardcoded hex.
- **Components:** Reuse `<SlidePanel>`, `<SlidePanelFooter>`, `<Dialog>`, `<Button>`, `<Input>`, `<Textarea>`, `<Select>` (or shadcn Switch if available). NEVER raw HTML elements.
- **Note:** Invoke `frontend-design` skill before writing visuals; pass scope-radio + conditional rule-name input as design challenges.
- **Verify:** `grep -nE "\btext-(red|blue|green|purple|pink|...)-[0-9]{2,3}\b|\bbg-(red|...)-[0-9]{2,3}\b" web/src/pages/alerts/_partials/mute-panel.tsx web/src/pages/alerts/index.tsx` → ZERO matches in mute-panel; existing alerts page lines untouched. `grep -n "setMuted" web/src/pages/alerts/index.tsx` → ZERO matches. Manual: open panel from header AND row → POST observed in network tab → alert state flips to suppressed via WebSocket invalidation.

#### Task 11: Similar-alerts row tab + a11y
- **Files:** Create `web/src/pages/alerts/_partials/similar-alerts.tsx`, Modify `web/src/pages/alerts/index.tsx` (introduce subtab structure in `AlertCardExpanded`)
- **Depends on:** Task 5
- **Complexity:** medium
- **Pattern ref:** Read `AlertCardExpanded` in `web/src/pages/alerts/index.tsx:340-443` for the row-expand layout. Use existing tabs primitive (`web/src/components/ui/tabs.tsx`) if present; if not, simple radio-style tab buttons styled to match.
- **Context refs:** AC-3; API Spec `GET /alerts/{id}/similar`; Alert row expanded mockup; Design Token Map
- **What:** Refactor `AlertCardExpanded` to render tabs `Details | Similar (N) | Comments`. Default tab: Details. `SimilarAlertsList({anchorId})`: `useQuery(['alerts','similar',anchorId], () => api.get(...))`, render compact row list (severity icon + title + entity link + fired_at), max-height with scroll, "View all →" link to `/alerts?dedup_key=<key>` if anchor has dedup_key OR `/alerts?type=<t>&source=<s>` otherwise. Show count in tab label. Empty state: "No similar alerts in retention window."
- **Tokens:** Match existing AlertCardExpanded rendering (`text-sm`, `text-text-primary`, `bg-bg-primary/50`); reuse `SeverityBadge`, `EntityLink`.
- **Note:** Invoke `frontend-design` skill for tab visual.
- **Verify:** `grep -n "default Tailwind palette" web/src/pages/alerts/_partials/similar-alerts.tsx` → ZERO. Manual: expand a row, see Similar (N) tab; click; list renders.

#### Task 12: Settings → Alert Rules page (AC-5 manager + AC-4 retention slider)
- **Files:** Create `web/src/pages/settings/alert-rules.tsx`, Modify `web/src/App.tsx` (add route `/settings/alert-rules`), Modify the settings sidebar/nav file (find via grep for existing settings nav) to add the new entry
- **Depends on:** Task 8, Task 1
- **Complexity:** medium
- **Pattern ref:** Read `web/src/pages/settings/reliability.tsx` for typical settings page structure (header + cards + form blocks). Read `web/src/pages/settings/api-keys.tsx` for list-of-rules pattern (table or card grid).
- **Context refs:** Settings → Alert Rules mockup; API Specs `GET /suppressions`, `DELETE /suppressions/{id}`, `PATCH /tenants/{id}` (settings.alert_retention_days); DEV-335, DEV-344
- **What:**
  - **Active rules table** (`useQuery(['suppressions'], …)`): columns Name, Scope (formatted "type=…" / "operator=name" / "dedup=…" / "this=alertId-shortened"), Expires (countdown), Created by, Actions (Edit → opens MutePanel reuse with prefilled values; Delete → UnmuteDialog).
  - **Retention card**: numeric input `<Input type="number" min=30 max=365>` bound to `tenant.settings.alert_retention_days` (fetch via existing tenant API or context); Save → PATCH `/api/v1/tenants/{currentTenantID}` with `{settings: {alert_retention_days: N, ...existingSettings}}`. Toast success.
  - Empty state for rules: "No active suppression rules. Mute an alert to create one."
  - Sidebar entry: under Settings group, label "Alert Rules", icon BellOff (lucide).
- **Tokens:** Same Design Token Map.
- **Note:** Invoke `frontend-design` skill before writing the page composition.
- **Verify:** Manual: visit `/settings/alert-rules`, see rules + retention; edit retention to 365 → 200; to 400 → 422 toast surfaced. `grep -nE "\btext-(red|blue|...)-[0-9]{2,3}\b" web/src/pages/settings/alert-rules.tsx` → ZERO.

### Wave 5 — Per-tenant retention job (depends on Task 1 + Task 3)

#### Task 13: Rewrite `AlertsRetentionProcessor` to per-tenant loop
- **Files:** Modify `internal/job/alerts_retention.go`, Modify `internal/job/alerts_retention_test.go`, Modify `cmd/argus/main.go` (constructor signature change — add `tenantStore`)
- **Depends on:** Task 3 (DeleteOlderThanForTenant), Task 1 (validation already enforces bounds upstream)
- **Complexity:** high (changes a production cron job's algorithm + result schema)
- **Pattern ref:** Read existing `internal/job/alerts_retention.go` for the global path (replace, do not append). Read `internal/store/tenant.go` `List` for tenant pagination. Read another per-tenant cron job if present (`internal/job/data_retention.go`) for the loop pattern.
- **Context refs:** Data Flow — Retention; DEV-335, DEV-336; Risks R3
- **What:**
  - Add `tenantStore *store.TenantStore` field to `AlertsRetentionProcessor`. Update constructor `NewAlertsRetentionProcessor(jobs, alertStore, tenantStore, eventBus, defaultRetentionDays, logger)` and main.go wiring.
  - In `Process`: paginate `tenantStore.List(state="active", limit=1000)` until cursor empty. For each tenant:
    - Parse `tenant.Settings` JSON; extract `alert_retention_days` (int), default to `p.defaultRetentionDays` when missing or invalid.
    - Apply min-30 floor.
    - `cutoff := time.Now().UTC().Add(-time.Duration(retentionDays) * 24 * time.Hour)`
    - `deleted, err := alertStore.DeleteOlderThanForTenant(ctx, tenant.ID, cutoff)` — log + skip on error (don't fail whole job for one tenant).
    - Append `{tenant_id, retention_days, deleted, cutoff}` to per-tenant slice.
  - Compute `totalDeleted = sum(deleted)`.
  - Result JSON shape: `{total_deleted, tenants_processed, per_tenant: [...]}`.
  - Emit single `JobCompleted` event with aggregate stats.
- **Verify:** `go test ./internal/job/...` passes including:
  - `TestAlertsRetention_PerTenantUsesTenantSetting` (tenant A has `alert_retention_days=60`, tenant B has none → A purged at 60d, B at default 180d)
  - `TestAlertsRetention_OneTenantFailureDoesNotFailJob`
  - `TestAlertsRetention_ResultJSONShape`
  - Existing tests adapted (constructor signature change).

### Wave 6 — Verification & integration

#### Task 14: Backend integration test — full mute → trigger → suppress → unmute → restore
- **Files:** Create `internal/api/alert/handler_integration_test.go` (or extend existing test file)
- **Depends on:** Task 4, Task 8
- **Complexity:** medium
- **Pattern ref:** Read existing `internal/api/alert/handler_test.go` for HTTP test scaffolding (httptest server + tenant context middleware).
- **Context refs:** All ACs; Data Flow sections
- **What:** End-to-end test: (1) Create suppression with scope_type=type, scope_value=operator_down, expires=NOW+1h. (2) Verify backfill UPDATE: an existing open `operator_down` alert flipped to `suppressed`. (3) Trigger NEW operator_down alert via `UpsertWithDedup` — assert it lands as `suppressed` with `meta.suppression_id` set. (4) DELETE suppression — assert restored count and that suppressed alerts return to `open`. (5) Trigger another operator_down — lands as `open` again.
- **Verify:** `go test ./internal/api/alert/... -run TestSuppression_FullLifecycle -v` passes.

#### Task 15: ROUTEMAP + step log update + Tech Debt entries
- **Files:** Modify `docs/ROUTEMAP.md` (FIX-229 row → IN PROGRESS → DONE at end; Tech Debt D-091 + D-092 entries), Modify `docs/architecture/db/_index.md` (TBL-55 row — done in Task 2; double-check), Modify `docs/stories/fix-ui-review/FIX-229-step-log.txt` (append per-step entries)
- **Depends on:** All prior tasks complete
- **Complexity:** low
- **Pattern ref:** Read recent FIX rows in `docs/ROUTEMAP.md` (FIX-228 row format).
- **Context refs:** Tech Debt section; ROUTEMAP track location ("UI Review Remediation [IN PROGRESS]")
- **What:** Update ROUTEMAP wave-7 row for FIX-229; append Tech Debt rows D-091 + D-092 with TARGET=FIX-248; ensure step-log captures plan + dev waves + gate. Update `docs/architecture/db/_index.md` if Task 2 didn't already add TBL-55.
- **Verify:** `git diff docs/ROUTEMAP.md docs/architecture/db/_index.md docs/stories/fix-ui-review/FIX-229-step-log.txt` shows clean additions only.

---

## Wave plan
- **W0** (parallel): Task 1
- **W1** (single): Task 2
- **W2** (parallel after W1): Task 3 → Task 4 (sequential — same file `internal/store/alert.go`)
- **W3** (parallel after W2): Tasks 5, 6, 8 (different routes; Task 7 sequential after 6 — shares filter helper)
- **W3.5** (sequential): Task 7
- **W4** (sequential — UI shares index.tsx): Task 9 → Task 10 → Task 11 → Task 12
- **W5** (after Task 3 + Task 1): Task 13
- **W6** (after W4 + W5): Task 14 → Task 15

Total tasks: **15** across **7 waves** (W0..W6).

---

## Pre-Validation Checklist (Planner self-validate)

- [x] Story Effort = M → minimum 60 lines, 3 tasks. **Plan = ~480 lines, 15 tasks.** PASS.
- [x] Required sections: Goal, Architecture Context, Tasks, Acceptance Criteria Mapping. PASS.
- [x] API endpoint specs embedded with HTTP methods, paths, request/response, status codes. PASS.
- [x] DB schema embedded with full SQL DDL, indexes, RLS, COMMENT. PASS.
- [x] Source noted: TBL-55 NEW; alerts table from existing migrations. PASS.
- [x] Design Token Map populated (colors, typography, spacing, components). PASS.
- [x] Component reuse table populated. PASS.
- [x] Each UI task references "Use ONLY classes from Design Token Map". PASS.
- [x] Story is M but several tasks are unavoidable high-complexity (Task 7 PDF engine + Task 8 suppression CRUD + Task 10 MutePanel + Task 13 retention rewrite) — PASS the L-or-XL guidance even though Effort is M.
- [x] Each task has Pattern ref + Context refs + Depends on + Complexity + Verify. PASS.
- [x] Each task touches ≤3 files (Task 7 touches 6 — DB+report+handler+wiring; Task 8 touches 3; Task 13 touches 3). Task 7 documented as inherently multi-file (engine extension). All others ≤3.
- [x] Cursor-based pagination for list endpoints. PASS.
- [x] Audit log entries enumerated for every state-changing endpoint. PASS.
- [x] PAT-016, PAT-017, PAT-018, PAT-019 referenced where applicable. PASS.
- [x] Mock retirement / FE bug deletion enumerated. PASS.
- [x] Tech Debt D-091, D-092 listed with target FIX-248. PASS.
- [x] Modal pattern decision (FIX-216): SlidePanel for mute (4+ fields), Dialog for unmute confirm. PASS.

**RESULT: PASS**

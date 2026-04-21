# Implementation Plan: FIX-211 — Severity Taxonomy Unification (critical/high/medium/low/info)

## Goal

Establish a single 5-level canonical severity taxonomy — `critical | high | medium | low | info` — across DB columns, Go payload structs, API validators, event publishers, seed data, and the React SPA, so alerts / anomalies / violations / notifications sort, filter, and colour-code consistently. Ships as a single commit that survives `make db-migrate && make db-seed` on a fresh volume and passes strict validation (HTTP 400) for non-canonical severity values at every API entry point.

## Problem Context — Current Severity State (Verified)

### DB Schema Inventory

| Table | Column | Current constraint | Current domain | New domain (AC-1) | Data migration needed |
|---|---|---|---|---|---|
| `anomalies` | `severity` TEXT NOT NULL | `CHECK (severity IN ('critical','high','medium','low'))` (4 levels, **no `info`**) | critical, high, medium, low | critical, high, medium, low, **info** | **NO** — pure additive (DROP CHECK + ADD CHECK with `info`); existing rows already comply |
| `policy_violations` | `severity` TEXT NOT NULL DEFAULT 'info' | **none** | info, warning, critical (enforcer emits these) | critical, high, medium, low, info | **YES** — `warning → medium`; then ADD CHECK + change DEFAULT stays `'info'` |
| `notifications` | `severity` VARCHAR(10) NOT NULL DEFAULT 'info' | **none** | info, warning, error, critical (free text) | critical, high, medium, low, info | **YES** — `warning → medium`, `error → high`; then ADD CHECK; DEFAULT stays `'info'` |
| `notification_preferences` | `severity_threshold` VARCHAR(10) NOT NULL DEFAULT 'info' | **none** | info, warning, error, critical | critical, high, medium, low, info | **YES** — same map as notifications; ADD CHECK; DEFAULT stays `'info'` |
| `alerts` | — | — | — | **Out of scope — FIX-209** will create this table with CHECK set to the canonical 5-level taxonomy documented below | — |
| `sla_reports` | — | no severity column | — | — | no-op |

**`anomalies` is NOT a TimescaleDB hypertable** (verified: `migrations/20260320000003_timescaledb_hypertables.up.sql` has no `anomalies` entry). Plain `ALTER TABLE ... ADD CONSTRAINT` is safe; no chunk propagation concerns.

### Backend Handler Validator Inventory

| Call site | File | Line | Current behaviour | Target |
|---|---|---|---|---|
| Notification preferences upsert | `internal/api/notification/handler.go` | 497 (`validSeverities` map) + 527 (validator call, `422`) + 529 (error msg) | Accepts `info/warning/error/critical`; rejects others with `CodeValidationError` (422) | Accept canonical 5; reject others with `400 CodeInvalidSeverity` |
| Notification preference store | `internal/store/notification_preference_store.go` | 20 (`validSeverityThresholds`) + 16 (`ErrInvalidSeverityThreshold`) + 79 (Upsert enforcement) | Store-level validation mirrors handler | Drop or replace with canonical helper; keep single source of truth |
| Notification severity ordinal | `internal/notification/service.go` | 156-168 (`severityOrdinal`) | `info=1, warning=2, error=3, critical=4` (used by AC-8 threshold-suppression) | `info=1, low=2, medium=3, high=4, critical=5` — **runtime-behaviour change** |
| Anomaly list filter | `internal/api/anomaly/handler.go` | 123 (`Severity: q.Get("severity")`) | Passes through raw query param, no validation | Validate via canonical helper; 400 on non-canonical |
| Violation list filter | `internal/api/violation/handler.go` | 288 (`Severity: q.Get("severity")`) | Passes through raw query param | Validate via canonical helper; 400 on non-canonical |
| Ops incidents list filter | `internal/api/ops/incidents.go` | 62 (`f.severity = q.Get("severity")`) | Pass-through | Validate via canonical helper; 400 |

### Backend Event / Payload Construction Sites (non-handler)

These construct Severity as a string field on event/payload structs. All must emit canonical values. (PAT-006: shared payload field silently omitted — grep every construction site.)

| File | Line | Current value | New value |
|---|---|---|---|
| `internal/policy/enforcer/enforcer.go` | 157 | `"critical"` | `"critical"` (unchanged) |
| `internal/policy/enforcer/enforcer.go` | 166 | `"warning"` | **`"medium"`** |
| `internal/policy/enforcer/enforcer.go` | 181 | `"info"` | `"info"` (unchanged) |
| `internal/policy/enforcer/enforcer.go` | 190 | `"info"` | `"info"` (unchanged) |
| `internal/policy/enforcer/enforcer.go` | 199 | `"info"` | `"info"` (unchanged) |
| `internal/policy/enforcer/enforcer.go` | 239 | `v.Severity == "critical" \|\| v.Severity == "warning"` | `v.Severity == "critical" \|\| v.Severity == "high" \|\| v.Severity == "medium"` |
| `internal/bus/consumer_lag.go` | 186 | `"warning"` | **`"medium"`** |
| `internal/api/system/revoke_sessions_handler.go` | 110 | `"warning"` | **`"medium"`** |
| `internal/api/onboarding/handler.go` | 335 | `"info"` | `"info"` (unchanged) |
| `internal/operator/events.go` | 37-39 | Constants `SeverityCritical="critical", SeverityWarning="warning", SeverityInfo="info"` | Replace `SeverityWarning` with full set: `SeverityCritical, SeverityHigh, SeverityMedium, SeverityLow, SeverityInfo`; grep call sites and map (`health.go:522` SLA violation → `SeverityHigh`; operator-down stays `SeverityCritical`; operator-up stays `SeverityInfo`) |
| `internal/job/import.go` | 480-481 | `severity = "error"` | `severity = "high"` |
| `internal/analytics/anomaly/types.go` | 17-20 | `SeverityCritical/High/Medium/Low` constants | Add `SeverityInfo = "info"` |

### Frontend Inventory

Every FE file that renders severity or filters by it:

| File | Line | Current behaviour | Target |
|---|---|---|---|
| `web/src/types/analytics.ts` | 135 | `type AnomalySeverity = '' \| 'critical' \| 'warning' \| 'info'` | Update to `'' \| 'critical' \| 'high' \| 'medium' \| 'low' \| 'info'` |
| `web/src/stores/events.ts` | 7 | `severity: 'critical' \| 'warning' \| 'info'` | Update to canonical 5 |
| `web/src/pages/alerts/index.tsx` | 119 (`SEVERITY_PILLS`), 150 (`severityIcon`), 158 (`severityBadgeVariant`), 221-222 (impactEstimate), 485-486 (border CSS), 660-661 (counters), 795-797 (filter) | 3-value pills `{critical,warning,info}` — filters against 4-level anomaly data (`{critical,high,medium,low}`), so "warning" filter returns zero rows **in prod today** (bug) | Adopt `<SeverityBadge>` + `SEVERITY_OPTIONS` exported from shared module; 5-value pills |
| `web/src/pages/dashboard/analytics-anomalies.tsx` | 20 (SEVERITY_OPTIONS), 45 (icon), 56 (variant), 197-199 (badge), 263-270 (filter state), 305-306 (counts) | 3-value, same bug as alerts | Same as above |
| `web/src/pages/violations/index.tsx` | 99 (SEVERITY_OPTIONS), 112 (SEV_COLORS), 127 (severityVariant), 432-437 (list row render) | 3-value `{critical,warning,info}` | Adopt `<SeverityBadge>` + canonical 5; after data migration `warning` rows become `medium` |
| `web/src/pages/violations/detail.tsx` | 38 (severityVariant), 132-133, 215 | Same as list | Adopt `<SeverityBadge>` |
| `web/src/pages/alerts/detail.tsx` | 44-45 (severityIcon), 50 (variant), 147-152, 231, 296 | 3-value | Adopt `<SeverityBadge>` |
| `web/src/pages/notifications/preferences-panel.tsx` | 40-45 (SEVERITY_OPTIONS), 48 (default), 134-136 (select) | 4-value `{info,warning,error,critical}` | Update to 5-value `{info,low,medium,high,critical}` with default `'info'` |
| `web/src/pages/notifications/index.tsx` | 36-41 (severityColors) | 4-value color map | Adopt `<SeverityBadge>` colors via shared module |
| `web/src/components/notification/notification-drawer.tsx` | 32 (severityColors), 81 (lookup), 96 | Same 4-value | Adopt shared module |
| `web/src/components/event-stream/event-stream-drawer.tsx` | 23 (severityColor), 31 (variant), 99, 112 | 3-value | Adopt `<SeverityBadge>` |
| `web/src/components/shared/related-alerts-panel.tsx` | 30-36 (icon+variant), 99-101 | 3-value | Adopt `<SeverityBadge>` |
| `web/src/components/shared/related-notifications-panel.tsx` | 27, 122-123 | variant map | Adopt `<SeverityBadge>` |
| `web/src/components/shared/related-violations-tab.tsx` | 64, 210-211 | variant map | Adopt `<SeverityBadge>` |
| `web/src/pages/ops/incidents.tsx` | 21-29 (local `SeverityBadge`), 65-75 (select options) | Already has inline 5-value (critical/high/medium/low) — but lives in this file | Replace inline with shared `<SeverityBadge>` import |
| `web/src/pages/dashboard/index.tsx` | 772, 824-825, 842-853, 895-901 | 3-value | Adopt `<SeverityBadge>` |

### Out of Scope (do NOT touch)

- `internal/policy/dsl/parser.go:112,131` — `Severity: "error"/"warning"` here refers to **DSL parse-error severity** (lex/syntax errors in policy DSL), not event severity. Different domain, different enum.
- `web/src/pages/capacity/index.tsx:436-544` — `severity: 'danger' | 'warning' | 'default'` is a local UI-variant enum for recommendation card styling, not backend event severity.
- `alerts` table CHECK constraint — FIX-209 creates the table; this story documents the canonical enum for FIX-209 to adopt.

## Canonical Severity Taxonomy (Authoritative)

> **This section is the single source of truth for FIX-211 and FIX-209 / FIX-213. Copy verbatim when cross-referencing.**

### Values (strict ordering)

```
info < low < medium < high < critical
```

### Ordinal map (for threshold suppression, sort)

| Severity | Ordinal | Meaning |
|---|---|---|
| `info` | 1 | Operational information; no action needed |
| `low` | 2 | Cosmetic / minor; batched review |
| `medium` | 3 | Attention needed within 24h (was `warning`) |
| `high` | 4 | Active issue, respond within 1h (was `error`) |
| `critical` | 5 | Page on-call immediately |

### Old → New migration map (applies to `policy_violations`, `notifications`, `notification_preferences`)

| Old value | New value | Notes |
|---|---|---|
| `critical` | `critical` | unchanged |
| `error` | `high` | AC-3 (notifications) |
| `warning` | `medium` | AC-3 (violations + notifications) |
| `info` | `info` | unchanged |

### Colour coding (FE `<SeverityBadge>`)

Maps to existing Argus design tokens from `docs/FRONTEND.md`:

| Severity | Background | Foreground | Token basis |
|---|---|---|---|
| `critical` | `bg-danger-dim` | `text-danger` | `--danger #FF4466` |
| `high` | `bg-danger-dim` | `text-danger` | shares `--danger` (two-tier red: critical has pulse/ring; high static) |
| `medium` | `bg-warning-dim` | `text-warning` | `--warning #FFB800` |
| `low` | `bg-info/10` | `text-info` | `--info #6C8CFF` |
| `info` | `bg-bg-elevated` | `text-text-secondary` | neutral grey |

**Rule:** no hardcoded hex. All colour comes from Tailwind classes backed by FRONTEND.md CSS custom properties.

### Strict validation (hard migration)

**Decision: HARD validation, NO config toggle.**

Rationale: Argus is internal/single-tenant; there are no external API consumers of severity-accepting endpoints. All handler call sites are in-repo; all event publishers are in-repo. A `SEVERITY_STRICT_VALIDATION` flag would add surface area without reducing risk. The seed-data and test fixtures are fixed in the same commit (Task 4) so `make db-seed` stays clean. If a future external consumer appears, reintroduce the flag per FIX-207 `IMSI_STRICT_VALIDATION` precedent.

## Architecture Context

### Components Involved

| Component | Layer | File(s) | Role |
|---|---|---|---|
| Severity canonical helper | Go shared | `internal/severity/severity.go` (NEW) | Single-source constants + validator; imported by all handlers and the store |
| API error code | Go | `internal/apierr/apierr.go` (existing) | New constant `CodeInvalidSeverity = "INVALID_SEVERITY"` (400) |
| Anomaly DB CHECK | DB | `migrations/2026042200000X_severity_taxonomy_unification.up.sql` (NEW) | DROP + ADD constraint adding `info` |
| Policy violations DB CHECK | DB | (same migration) | Data migration + ADD CHECK |
| Notifications DB CHECK | DB | (same migration) | Data migration + ADD CHECK |
| Notification preferences DB CHECK | DB | (same migration) | Data migration + ADD CHECK |
| Notification handler validator | Go handler | `internal/api/notification/handler.go:497,527` | Use canonical helper; 400 on invalid |
| Notification preference store validator | Go store | `internal/store/notification_preference_store.go:20,79` | Use canonical helper or call shared |
| Anomaly list handler | Go handler | `internal/api/anomaly/handler.go:123` | Validate ?severity= against canonical |
| Violation list handler | Go handler | `internal/api/violation/handler.go:288` | Same |
| Ops incidents list handler | Go handler | `internal/api/ops/incidents.go:62` | Same |
| Policy enforcer | Go package | `internal/policy/enforcer/enforcer.go:157-199,239` | Emit canonical values (`warning → medium`); update threshold check |
| Notification service severity ordinal | Go | `internal/notification/service.go:155-168` | Re-order ordinals to 5-level |
| Operator alert events | Go | `internal/operator/events.go:37-39`, `internal/operator/health.go:522` | Constants + SLA violation → `SeverityHigh` |
| Bus consumer lag alert | Go | `internal/bus/consumer_lag.go:186` | `warning → medium` |
| Revoke-sessions notification | Go | `internal/api/system/revoke_sessions_handler.go:110` | `warning → medium` |
| Import job notification | Go | `internal/job/import.go:480-481` | `error → high` |
| Seed SQL | SQL | `migrations/seed/003_comprehensive_seed.sql:644,672,837,1318,1339` | Any row with `warning/error` must be mapped to new values before the CHECK constraint lands |
| FE SeverityBadge | React | `web/src/components/shared/severity-badge.tsx` (NEW) | Canonical enum + colour + label; single component for all pages |
| FE pages | React | 13 files (see FE inventory above) | Import and use `<SeverityBadge>` instead of inline switch/variant functions |
| ERROR_CODES.md | Doc | `docs/architecture/ERROR_CODES.md` | New section "Severity Taxonomy" + new error code `INVALID_SEVERITY` |

### Data Flow — Migration Sequence

```
Fresh-volume flow (make infra-up && make db-migrate && make db-seed):
  1. argus migrate up applies migrations lexically, including:
     - [NEW] 2026042200000X_severity_taxonomy_unification.up.sql
       Inside a single transaction:
         a. UPDATE policy_violations SET severity='medium' WHERE severity='warning'
         b. UPDATE notifications SET severity = CASE severity
              WHEN 'warning' THEN 'medium' WHEN 'error' THEN 'high' ELSE severity END
         c. UPDATE notification_preferences SET severity_threshold = CASE ...
         d. ALTER TABLE anomalies DROP CONSTRAINT IF EXISTS anomalies_severity_check
         e. ALTER TABLE anomalies ADD CONSTRAINT chk_anomalies_severity
              CHECK (severity IN ('critical','high','medium','low','info'))
         f. ALTER TABLE policy_violations ADD CONSTRAINT chk_policy_violations_severity
              CHECK (severity IN ('critical','high','medium','low','info'))
         g. ALTER TABLE notifications ADD CONSTRAINT chk_notifications_severity
              CHECK (severity IN ('critical','high','medium','low','info'))
         h. ALTER TABLE notification_preferences ADD CONSTRAINT chk_notif_prefs_severity_threshold
              CHECK (severity_threshold IN ('critical','high','medium','low','info'))
       Down migration reverses: drops the new constraints; for anomalies restores 4-level CHECK.
  2. argus seed (alphabetical): 001..008
     - [FIXED] 003_comprehensive_seed.sql: every row targeting notifications/anomalies uses canonical severity only.

Runtime flow — API validation:
  Client → POST/PATCH/PUT /notifications/preferences  { severity_threshold: "warning" }
    → handler.UpdatePreferences → severity.Validate("warning")
    → 400 { code: "INVALID_SEVERITY", message: "severity must be one of: critical, high, medium, low, info" }

  Client → GET /alerts?severity=warning
    → anomaly.List → severity.Validate("warning")
    → 400 INVALID_SEVERITY

Runtime flow — event publishers:
  Policy enforcer → ViolationRecord{Severity: severity.Medium}  (was "warning")
  Operator health check SLA breach → AlertEvent{Severity: severity.High}  (was SeverityWarning)
  Bus consumer lag alert → lagAlert{Severity: severity.Medium}  (was "warning")
```

### API Specifications

Applies uniformly to every query-parameter filter `?severity=...` (alerts, violations, ops incidents) and every request body containing `severity` or `severity_threshold` (notification preferences).

- **Accepted values:** `critical | high | medium | low | info` (lowercase only; no empty string unless the filter is optional — in which case empty is skipped, never validated)
- **Rejection response:** `400 Bad Request`
  ```json
  { "status": "error",
    "error": { "code": "INVALID_SEVERITY",
               "message": "severity must be one of: critical, high, medium, low, info; got 'warning'" } }
  ```
- **Note:** the notification preferences handler previously returned 422 `VALIDATION_ERROR`. This story migrates it to 400 `INVALID_SEVERITY` to match the other severity-accepting endpoints — state this in the commit message and ERROR_CODES.md changelog.

### Design Token Map (for `<SeverityBadge>`)

| Usage | Token class | NEVER use |
|---|---|---|
| Critical background | `bg-danger-dim` | `bg-[#ff4466]`, `bg-red-500` |
| Critical foreground | `text-danger` | `text-[#ff4466]`, `text-red-500` |
| High background | `bg-danger-dim` | raw hex |
| High foreground | `text-danger` | raw hex |
| Medium background | `bg-warning-dim` | `bg-[#ffb800]`, `bg-yellow-500` |
| Medium foreground | `text-warning` | raw hex |
| Low background | `bg-info/10` | raw hex |
| Low foreground | `text-info` | raw hex |
| Info background | `bg-bg-elevated` | `bg-gray-100` |
| Info foreground | `text-text-secondary` | `text-gray-500` |
| Badge radius | `rounded-[var(--radius-sm)]` (inherited from `Badge`) | `rounded-md` |
| Badge font size | `text-[10px]` or `text-[11px]` per caller | `text-xs` |

**Existing components to REUSE:**

| Component | Path | Use |
|---|---|---|
| `<Badge>` | `web/src/components/ui/badge.tsx` | Wrap inside `<SeverityBadge>`; pass `className` for severity-specific styling. Do NOT recreate the base Badge. |

## Prerequisites

- [x] FIX-206 completed — provides orphan cleanup baseline; seed is already clean of FK-orphans
- [x] FIX-207 completed — provides the CHECK-constraint migration pattern (plain CHECK, no NOT VALID, data-migration-first ordering)
- [x] `internal/apierr/apierr.go` error code catalog is in place

## Tasks

### Task 1: Severity canonical helper + API error code

- **Files:** Create `internal/severity/severity.go`; Modify `internal/apierr/apierr.go` (add `CodeInvalidSeverity = "INVALID_SEVERITY"` constant)
- **Depends on:** — (none)
- **Complexity:** low
- **Pattern ref:** Read `internal/aaa/validator/imsi.go` (from FIX-207) — follow same structure: package constants, `Validate(string) error`, sentinel errors. No runtime deps.
- **Context refs:** "Canonical Severity Taxonomy", "API Specifications", "Architecture Context > Components Involved"
- **What:**
  - Export constants `Critical="critical", High="high", Medium="medium", Low="low", Info="info"`
  - Export `Values []string` (ordered: critical → info) and `OrdinalMap map[string]int` (info=1 … critical=5) for re-use
  - `Validate(s string) error` → returns `ErrInvalidSeverity` sentinel when `s` ∉ Values; empty string NOT allowed (caller handles optional-filter case before calling)
  - `IsValid(s string) bool` convenience
  - `Ordinal(s string) int` returns 0 for invalid, 1..5 for valid
  - In `apierr.go` add constant `CodeInvalidSeverity = "INVALID_SEVERITY"` near other validation-ish codes (e.g. `CodeInvalidFormat`)
- **Verify:** `go build ./internal/severity/... ./internal/apierr/...` passes; `go test ./internal/severity/...` passes (unit tests below)
- **Unit tests in same file (`internal/severity/severity_test.go`):**
  - `TestValidate_AcceptsCanonicalValues` — all 5 canonical values return nil
  - `TestValidate_RejectsOldValues` — `warning`, `error` return `ErrInvalidSeverity`
  - `TestValidate_RejectsUppercase` — `Critical`, `HIGH` rejected
  - `TestValidate_RejectsEmpty` — empty string returns error
  - `TestOrdinal_StrictOrder` — `Ordinal("info") < Ordinal("low") < Ordinal("medium") < Ordinal("high") < Ordinal("critical")`

### Task 2: Single-file migration — data map + CHECK constraints

- **Files:** Create `migrations/2026042200000X_severity_taxonomy_unification.up.sql` + matching `.down.sql` (replace `X` with the next available minute suffix when authoring)
- **Depends on:** — (migration is DB-only, does not import Go)
- **Complexity:** medium
- **Pattern ref:** Read `migrations/20260412000003_enum_check_constraints.up.sql` — follow the `DO $$ ... RAISE EXCEPTION IF ...` fail-fast guard + `ALTER TABLE ... ADD CONSTRAINT` pattern. Use `chk_<table>_<column>` naming.
- **Context refs:** "Problem Context > DB Schema Inventory", "Canonical Severity Taxonomy > Old → New migration map", "Architecture Context > Data Flow"
- **What:**
  1. Open transaction (migrate tool wraps automatically)
  2. Data migration (idempotent — safe to re-run):
     - `UPDATE policy_violations SET severity='medium' WHERE severity='warning';`
     - `UPDATE notifications SET severity = CASE severity WHEN 'warning' THEN 'medium' WHEN 'error' THEN 'high' ELSE severity END WHERE severity IN ('warning','error');`
     - `UPDATE notification_preferences SET severity_threshold = CASE severity_threshold WHEN 'warning' THEN 'medium' WHEN 'error' THEN 'high' ELSE severity_threshold END WHERE severity_threshold IN ('warning','error');`
  3. Fail-fast guards for each of the 4 tables (RAISE EXCEPTION if any row still non-canonical)
  4. `ALTER TABLE anomalies DROP CONSTRAINT IF EXISTS anomalies_severity_check;` (PG auto-names or explicit — check actual name via `\d anomalies` in staging; fall back to looping `information_schema.table_constraints` if needed — embed a `DO` block for safety)
  5. `ALTER TABLE anomalies ADD CONSTRAINT chk_anomalies_severity CHECK (severity IN ('critical','high','medium','low','info'));`
  6. `ALTER TABLE policy_violations ADD CONSTRAINT chk_policy_violations_severity CHECK (severity IN ('critical','high','medium','low','info'));`
  7. `ALTER TABLE notifications ADD CONSTRAINT chk_notifications_severity CHECK (severity IN ('critical','high','medium','low','info'));`
  8. `ALTER TABLE notification_preferences ADD CONSTRAINT chk_notif_prefs_severity_threshold CHECK (severity_threshold IN ('critical','high','medium','low','info'));`
  9. Down migration: drop all 4 new constraints; restore anomalies to 4-level CHECK.
- **Verify:** `make db-migrate` on a copy of the dev DB; `psql -c "\d+ anomalies" | grep chk_anomalies_severity` shows the constraint; `INSERT INTO notifications(... severity='warning' ...)` fails with check-constraint violation.
- **Notes:** Track as D-NNN in ROUTEMAP only if prod-scale warning is needed. Current dev volumes are < 10k rows per table — no performance concern. PAT-004 (hypertable chunk propagation) does NOT apply here: `anomalies` is plain, the other 3 tables are plain.

### Task 3: Wire canonical validator into all backend handlers and event publishers

- **Files:**
  - Modify `internal/api/notification/handler.go` (remove local `validSeverities` map; call `severity.Validate`; swap 422 → 400 `CodeInvalidSeverity`)
  - Modify `internal/store/notification_preference_store.go` (remove local `validSeverityThresholds` map; call `severity.Validate`)
  - Modify `internal/notification/service.go` `severityOrdinal` → delegate to `severity.Ordinal` (5-level)
  - Modify `internal/api/anomaly/handler.go:123` — call `severity.Validate` when `q.Get("severity") != ""`; return 400 on error
  - Modify `internal/api/violation/handler.go:288` — same
  - Modify `internal/api/ops/incidents.go:62` — same
  - Modify `internal/policy/enforcer/enforcer.go` — change `"warning" → severity.Medium` at line 166; update line 239 threshold check to include `severity.High` and `severity.Medium`
  - Modify `internal/operator/events.go` — add `SeverityHigh`, `SeverityMedium`, `SeverityLow` constants; keep `SeverityWarning` as a deprecated alias for internal compatibility ONLY if another package imports it (grep first); otherwise remove
  - Modify `internal/operator/health.go:522` — SLA violation emits `SeverityHigh` (was `SeverityWarning`)
  - Modify `internal/bus/consumer_lag.go:186` — `"warning" → severity.Medium`
  - Modify `internal/api/system/revoke_sessions_handler.go:110` — `"warning" → severity.Medium`
  - Modify `internal/job/import.go:480-481` — `"error" → severity.High`
  - Modify `internal/analytics/anomaly/types.go:17-20` — add `SeverityInfo = "info"` constant
- **Depends on:** Task 1 (helper must exist)
- **Complexity:** **high** (12 files, runtime-behaviour change in `severityOrdinal`, PAT-006 repeat-risk across event publishers)
- **Pattern ref:** Read `internal/aaa/radius/server.go:handleDirectAuth` (from FIX-207) — follow the pattern of calling the shared validator at the earliest entry point, logging+metric on rejection, and returning the canonical API error code.
- **Context refs:** "Problem Context > Backend Handler Validator Inventory", "Problem Context > Backend Event / Payload Construction Sites", "API Specifications", "Canonical Severity Taxonomy > Old → New migration map"
- **What:**
  - Replace every `"warning"` literal in the construction-sites table with `severity.Medium`; every `"error"` with `severity.High`. Import `internal/severity`.
  - For the 4 filter call sites (`anomaly`, `violation`, `ops/incidents`, `notification/preferences`): if `q.Get("severity") == ""` → skip (optional filter); otherwise call `severity.Validate`; on error write `apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidSeverity, msg)`.
  - For `severityOrdinal` in service.go: replace the switch with `return severity.Ordinal(s)`. Update existing tests in `internal/notification/service_test.go` that hardcode `"warning"` / `"error"`:
    - Line 305 `Severity:"warning"` → `severity.Medium`
    - Line 751 `Severity:"warning"` → `severity.Medium`
    - Line 810 `SeverityThreshold:"warning"` → `severity.Medium`
    - Line 822 event `Severity:"info"` — unchanged
    - Line 839-855 threshold=`"warning"`, event=`"error"` → threshold=`severity.Medium`, event=`severity.High` (still expects "allow")
  - For the store-level test `internal/store/notification_preference_store_test.go`: update `validSeverityThresholds` references to the shared helper and the error sentinel message.
  - For `internal/store/policy_violation_acknowledge_test.go:42,79,110,140`: `"warning" → "medium"` on seed rows used in tests.
- **Verify:**
  - `go build ./...` passes
  - `go test ./internal/severity/... ./internal/api/notification/... ./internal/api/anomaly/... ./internal/api/violation/... ./internal/api/ops/... ./internal/notification/... ./internal/policy/enforcer/... ./internal/operator/... ./internal/bus/... ./internal/job/... ./internal/store/... ./internal/analytics/anomaly/...` all pass
  - Grep check: `rg -n '"warning"\|"error"' internal/ --type go | grep -iE 'severity' | grep -v _test.go` → empty (no more old-taxonomy severity literals in non-test code)
  - New regression test `TestNotifySeverityThreshold_5Level` in `internal/notification/service_test.go`:
    - threshold=`high`, event=`medium` → suppressed
    - threshold=`medium`, event=`high` → allowed
    - threshold=`info`, event=`info` → allowed
    - threshold=`critical`, event=`critical` → allowed
  - New handler test `TestListAnomalies_RejectsInvalidSeverity` in `internal/api/anomaly/handler_test.go`: `GET /anomalies?severity=warning` → 400 `INVALID_SEVERITY`

### Task 4: Seed-data mapping + tests

- **Files:** Modify `migrations/seed/003_comprehensive_seed.sql` (lines 644, 672, 837, 1318, 1339 — notifications and anomalies inserts)
- **Depends on:** Task 2 (CHECK constraints determine what is legal)
- **Complexity:** low
- **Pattern ref:** Read the existing 5 insert blocks listed — preserve row shape, only change severity values.
- **Context refs:** "Problem Context > DB Schema Inventory", "Canonical Severity Taxonomy > Old → New migration map"
- **What:**
  - Grep every literal `'warning'`, `'error'` inside notifications/anomalies INSERT VALUES clauses and remap per the canonical table.
  - Anomalies seed rows likely already use `critical/high/medium/low` (the old CHECK enforced this); verify no `warning` slipped in.
  - Notifications seed rows with `severity='warning'` → `'medium'`; `'error'` → `'high'`.
  - Per-project memory `feedback_no_defer_seed.md`: this task is MANDATORY in the same commit; `make db-seed` on a fresh volume MUST succeed.
- **Verify:**
  - `make down && make infra-up && make db-migrate && make db-seed` succeeds without errors.
  - `psql -c "SELECT DISTINCT severity FROM notifications"` returns only canonical values.
  - `psql -c "SELECT DISTINCT severity FROM policy_violations"` returns only canonical values (noting some may be 0 rows — if so, just confirm no error).
  - `psql -c "SELECT DISTINCT severity_threshold FROM notification_preferences"` returns only canonical values.

### Task 5: FE shared `<SeverityBadge>` component + severity constants module

- **Files:** Create `web/src/components/shared/severity-badge.tsx`; Create (or extend existing) `web/src/lib/severity.ts` (NEW — canonical JS enum + colour map mirror of the Go constants)
- **Depends on:** — (no runtime dep on backend; purely presentational)
- **Complexity:** low
- **Pattern ref:** Read `web/src/components/ui/badge.tsx` (reuse as the render primitive); read `web/src/pages/ops/incidents.tsx:21-29` (already has an inline 5-level SeverityBadge — port its logic into the shared component with token-based classes, add `info`).
- **Context refs:** "Canonical Severity Taxonomy", "Design Token Map"
- **What:**
  - `web/src/lib/severity.ts` exports:
    - `export const SEVERITY_VALUES = ['critical','high','medium','low','info'] as const`
    - `export type Severity = typeof SEVERITY_VALUES[number]`
    - `export const SEVERITY_OPTIONS: ReadonlyArray<{ value: Severity; label: string }>` — for `<Select>` dropdowns (sorted critical→info)
    - `export const SEVERITY_FILTER_OPTIONS` — with `{value: '', label: 'All Severities'}` prepended
    - `export function severityOrdinal(s: string): number` — mirror Go ordinal (info=1…critical=5, 0 for unknown)
    - `export function isSeverity(s: string): s is Severity`
  - `web/src/components/shared/severity-badge.tsx` exports:
    - `export function SeverityBadge({ severity, className }: { severity: string; className?: string })`
    - Renders `<Badge>` with classes from the Design Token Map above; unknown → renders as `info`-style neutral
    - Supports optional `iconOnly` prop (default false) that renders only the icon for dense rows
    - Label text is the capitalised severity (`'Critical', 'High', 'Medium', 'Low', 'Info'`) unless caller overrides
- **Tokens:** Use ONLY classes from the Design Token Map — zero hardcoded hex, zero `text-red-*`/`bg-yellow-*` Tailwind colour presets (those bypass our dark-theme tokens).
- **Components:** Reuse `<Badge>` as the render primitive. Do NOT recreate.
- **Note:** Invoke `frontend-design` skill for this task.
- **Verify:**
  - `cd web && pnpm tsc --noEmit` passes
  - `rg -n '#[0-9a-fA-F]{3,8}' web/src/components/shared/severity-badge.tsx web/src/lib/severity.ts` → zero matches
  - Unit test `web/src/components/shared/severity-badge.test.tsx` (if testing infra exists) — mounts with each canonical value, asserts rendered text and one token class per variant

### Task 6: FE page adoption — replace inline severity logic with `<SeverityBadge>` + `SEVERITY_FILTER_OPTIONS`

- **Files:** Modify (13 files; group into a single task since the edits are mechanical and share one pattern):
  - `web/src/types/analytics.ts` (update `AnomalySeverity` union)
  - `web/src/stores/events.ts` (update `severity` union)
  - `web/src/pages/alerts/index.tsx` (replace `SEVERITY_PILLS`, `severityIcon`, `severityBadgeVariant`, border-classes, counters)
  - `web/src/pages/alerts/detail.tsx` (replace inline icon+variant)
  - `web/src/pages/dashboard/analytics-anomalies.tsx` (replace `SEVERITY_OPTIONS`, `severityIcon`, `severityVariant`, counters)
  - `web/src/pages/dashboard/index.tsx` (lines 772, 824-825, 842-853, 895-901 — replace two inline `severityVariant` + `severityIcon`)
  - `web/src/pages/violations/index.tsx` (replace `SEVERITY_OPTIONS`, `SEV_COLORS`, `severityVariant`)
  - `web/src/pages/violations/detail.tsx` (replace `severityVariant`)
  - `web/src/pages/notifications/preferences-panel.tsx` (replace `SEVERITY_OPTIONS`, default)
  - `web/src/pages/notifications/index.tsx` (replace `severityColors`)
  - `web/src/components/notification/notification-drawer.tsx` (replace `severityColors`)
  - `web/src/components/event-stream/event-stream-drawer.tsx` (replace `severityColor`, `severityVariant`)
  - `web/src/components/shared/related-alerts-panel.tsx` (replace `severityIcon`, `severityVariant`)
  - `web/src/components/shared/related-notifications-panel.tsx` (replace `severityVariant`)
  - `web/src/components/shared/related-violations-tab.tsx` (replace `severityVariant`)
  - `web/src/pages/ops/incidents.tsx` (replace inline local `SeverityBadge` with shared import; update select options to use `SEVERITY_FILTER_OPTIONS`)
- **Depends on:** Task 5
- **Complexity:** medium (mechanical but broad; PAT-006 risk)
- **Pattern ref:** Use `web/src/pages/ops/incidents.tsx` as the target shape (it already uses a 5-level local SeverityBadge — emulate that pattern against the shared component).
- **Context refs:** "Problem Context > Frontend Inventory", "Design Token Map"
- **What:**
  - For each page: delete local `severityIcon`, `severityVariant`, `severityColor`, `SEVERITY_OPTIONS`, `SEVERITY_PILLS`, `SEV_COLORS`; import `<SeverityBadge>` and `SEVERITY_FILTER_OPTIONS` from the new module.
  - Where a page computes counts (`critical && open`, `warning && open`): use canonical values. Remove the `warning` bucket where no longer applicable; add buckets for `high`/`medium`/`low` where the filter UI now exposes them.
  - `impactEstimate` in `alerts/index.tsx` (line 221-222): update to branch on canonical values; map `critical` and `high` to the upper bound; `medium` to the mid-tier estimate; `low`/`info` return null.
  - `dashboard/index.tsx:842-853`: replace the local `severityIcon` / `severityVariant` with `<SeverityBadge>` render; preserve surrounding layout.
- **Tokens:** Use ONLY classes from the Design Token Map. No raw hex, no Tailwind colour presets for severity.
- **Components:** `<SeverityBadge>` everywhere. Do NOT reintroduce inline switches.
- **Note:** Invoke `frontend-design` skill once at the start of this task for a reusability audit before batch editing.
- **Verify:**
  - `cd web && pnpm tsc --noEmit` passes
  - `cd web && pnpm build` succeeds
  - Grep (from repo root): `rg -n 'severityVariant|severityIcon|severityColor|SEV_COLORS|SEVERITY_PILLS' web/src/pages web/src/components | grep -v severity-badge.tsx | grep -v severity.ts` → **empty** (no lingering inline helpers)
  - Grep: `rg -n "'warning'|'error'" web/src/pages/alerts web/src/pages/violations web/src/pages/notifications web/src/pages/dashboard` → no occurrences inside severity context (other uses of `'error'` as HTTP state or toast type are ok; reviewer grep eyeballs the hits)
  - Visual smoke test (dev-browser skill, optional): alerts page filter dropdown shows 5 values; selecting `medium` returns rows; badges colour-match the Design Token Map.

### Task 7: Documentation — ERROR_CODES.md severity taxonomy section + canonical enum spec

- **Files:** Modify `docs/architecture/ERROR_CODES.md` (add new "Severity Taxonomy" top-level section AND add `INVALID_SEVERITY` row to the appropriate error table — likely "Validation Errors" or a new "Domain Validation" subsection)
- **Depends on:** Tasks 1, 2, 3, 5, 6 (so the docs reflect shipped behaviour)
- **Complexity:** low
- **Pattern ref:** Read the existing "Auth Error Details" sub-section formatting (`docs/architecture/ERROR_CODES.md` around lines 40-58) — same markdown shape (table + prose note + example envelope).
- **Context refs:** "Canonical Severity Taxonomy", "API Specifications"
- **What:**
  - Add error code row: `INVALID_SEVERITY | 400 | Severity value is not in the canonical taxonomy | {"status":"error","error":{"code":"INVALID_SEVERITY","message":"severity must be one of: critical, high, medium, low, info; got 'warning'"}}`
  - Add new top-level section `## Severity Taxonomy` with:
    - The 5-value enum + ordinal table
    - The old → new mapping for reference (migration hint)
    - The colour coding summary referencing `docs/FRONTEND.md` tokens (not hex)
    - A "Consumers" list naming every table (anomalies, alerts FIX-209, policy_violations, notifications, notification_preferences) and every API surface that accepts it
    - Explicit HARD validation note (no toggle)
    - A cross-reference note for FIX-209 that the canonical enum is the required constraint for the new `alerts.severity` column
  - Also add one line in ROUTEMAP Tech Debt if migration emits any prod-scale warning (none expected — see Task 2 Notes).
- **Verify:** `grep -q 'INVALID_SEVERITY' docs/architecture/ERROR_CODES.md && grep -q 'Severity Taxonomy' docs/architecture/ERROR_CODES.md` both true; reviewer eyeballs the section for completeness.

## Acceptance Criteria Mapping

| Criterion | Implemented in | Verified by |
|---|---|---|
| AC-1: canonical enum | Task 1, Task 7 | Task 1 unit tests; Task 7 doc section |
| AC-2: CHECK constraints on anomalies / policy_violations / notifications / notification_preferences (+ alerts reserved for FIX-209 with handoff doc) | Task 2 | `\d+ <table>` in staging; Task 7 handoff note |
| AC-3: data migration `warning→medium`, `error→high`, `info→info`, `critical→critical` | Task 2 (runtime), Task 4 (seed) | Task 2 verify command; `make db-seed` |
| AC-4: backend validators reject non-canonical (400) | Task 1, Task 3 | Task 3 new regression tests; manual curl |
| AC-5: FE dropdowns + badges use canonical 5 with uniform token colours | Task 5, Task 6 | Task 6 grep + tsc + build; Design Token Map check |
| AC-6: ERROR_CODES.md severity taxonomy section | Task 7 | Task 7 grep |
| AC-7: report builders use consistent severity filter | Task 3 (validator wired at every `?severity=` filter — alerts, violations, ops incidents; no separate report-builder filter exists today in `internal/api/...`) | Task 3 grep: no new severity-accepting endpoints missed; noted in plan that no dedicated report-builder call site was found |

## Story-Specific Compliance Rules

- **API:** `400 CodeInvalidSeverity` via canonical helper; standard envelope required
- **DB:** Migration must be transactional; data-map BEFORE CHECK (see Task 2 Data Flow). Both up + down scripts required.
- **UI:** All severity UI consumes `<SeverityBadge>`; zero hardcoded hex; dropdowns use `SEVERITY_FILTER_OPTIONS` from the shared module; typography tokens per FRONTEND.md
- **Business:** Strict HARD validation — no backward-compat flag; seed data adopts canonical values in the same commit (per `feedback_no_defer_seed.md`)
- **ADR:** No ADR changes required; this is a cross-cutting convention, not an architectural shift

## Bug Pattern Warnings

Consulted `docs/brainstorming/bug-patterns.md`:

- **PAT-006 [FIX-201]** — shared payload struct field silently omitted at construction sites. DIRECTLY APPLIES: 12+ Go sites construct `Severity: "..."` literals. Task 3 must grep every such site via `rg -n 'Severity\s*[:=]\s*"(warning|error)"' internal/`. Failure to catch one = silent drift after the CHECK constraint lands (runtime INSERT panic). The plan lists every site up-front (see "Backend Event / Payload Construction Sites") — Developer must verify with grep at end of Task 3.
- **PAT-011 [FIX-207]** — plan-specified wiring missing at construction sites. APPLIES: the `severity` helper is imported in 12+ Go files; the Developer must import + call the validator at every filter query param and every struct-literal construction site. Gate grep at end of Task 3 catches omissions.
- **PAT-009 [FIX-204]** — nullable FK columns in aggregations; NOT DIRECTLY RELEVANT here, but related lesson: a single source of truth for enum → use the `internal/severity` helper as that single source; no inline maps.
- PAT-001 (double-writer), PAT-002 (single-clock polling), PAT-003 (metric labels), PAT-004 (hypertable cardinality), PAT-005 (masked secrets), PAT-007 (mutex ordering), PAT-008 (cold-start triggers), PAT-010 (single-flight), PAT-012 (cross-surface count drift) — not relevant to this story.

## Tech Debt (from ROUTEMAP)

Consulted `docs/ROUTEMAP.md` Tech Debt table: no OPEN items are targeted at FIX-211. No tech debt items for this story.

## Mock Retirement

Not applicable — no `src/mocks/` directory; Argus is backend-first.

## Risks & Mitigations

- **Risk 1 — Missed construction site (PAT-006 repeat):** Gate grep `rg -n '"warning"|"error"' internal/ --type go | grep -iE 'severity'` at end of Task 3; fail the task if any non-test hit remains.
- **Risk 2 — Seed break on fresh volume:** Task 4 is mandatory (not optional), sequenced correctly (Task 2 migration + Task 4 seed ship in same commit). `make db-seed` smoke test is part of the Gate checklist.
- **Risk 3 — `severityOrdinal` runtime behaviour change suppresses more notifications than expected:** New regression tests (Task 3) cover the 5-level threshold matrix; existing `TestService_Notify_SeverityThreshold_*` are updated, not deleted, to preserve coverage.
- **Risk 4 — FIX-209 (unified `alerts` table) drifts from this taxonomy:** Task 7 ERROR_CODES.md section embeds the canonical enum so FIX-209 can copy verbatim; FIX-211 step-log records the hand-off.
- **Risk 5 — FE build break from union-type tightening:** `AnomalySeverity` narrows from `'' | 'critical' | 'warning' | 'info'` to `'' | Severity`. Any code that assigned `'warning'` literally will fail tsc — caught by Task 6 verify step (`pnpm tsc --noEmit`).
- **Risk 6 — Hidden FE severity consumer missed in inventory:** Gate grep `rg -n 'severity' web/src | rg -v 'severity-badge|lib/severity' | rg -v 'capacity/index.tsx'` — reviewer eyeballs any remaining hits.

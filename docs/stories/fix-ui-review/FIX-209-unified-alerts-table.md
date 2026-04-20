# FIX-209: Unified `alerts` Table + Operator/Infra Alert Persistence

## Problem Statement
Argus has TWO parallel alert concepts:
1. `anomalies` table — SIM-level anomalies (auth failures, traffic spikes per SIM) — PERSISTED
2. NATS `alert.triggered` events — operator/infra alerts (operator degraded, pool exhaustion) — FIRE-AND-FORGET, NO TABLE

Consequence:
- Dashboard "Recent Alerts" reads anomalies → only SIM-level
- Alerts page (/alerts) reads same → operator/infra alerts invisible
- If notification dispatch fails, alert is LOST (no audit trail)
- SRE "all alerts last 7d" query — impossible

Verified: DB `\dt | grep alert` → only `anomalies`, `anomaly_comments`.

## User Story
As an SRE, I want a unified alerts table that persists ALL alert events (SIM anomalies + operator + infra + policy enforcement) so I have a single source of truth for alert history, filter, and correlation.

## Architecture Reference
- New table: `alerts`
- Consumers of `SubjectAlertTriggered` NATS events: persist before dispatch
- Dashboard + /alerts page switch data source

## Findings Addressed
F-36, F-40, F-44, F-10 (alerts), F-297 (persist gap), F-298 (delivery retry gap)

## Acceptance Criteria
- [ ] **AC-1:** New `alerts` table migration: `id UUID PK, tenant_id UUID NOT NULL, type VARCHAR(64), severity VARCHAR(20), source VARCHAR(64) (sim|operator|infra|policy|system), title, description, state VARCHAR(20) (open|acknowledged|resolved|suppressed), meta JSONB, fired_at TIMESTAMPTZ, acknowledged_at, acknowledged_by UUID, resolved_at, sim_id UUID NULL, operator_id UUID NULL, apn_id UUID NULL`. Indices on `(tenant_id, fired_at DESC)`, `(tenant_id, state) WHERE state='open'`, `(tenant_id, source)`.
- [ ] **AC-2:** NATS subscriber for `argus.events.alert.triggered` — in notification service's `handleAlert`, INSERT alerts row FIRST, THEN dispatch. Failure → mark `state=delivery_failed` (FIX-298 scope).
- [ ] **AC-3:** All existing alert publishers remain (consumer_lag, storage_monitor, anomaly_batch, policy/enforcer, health worker) — no publisher changes required.
- [ ] **AC-4:** `GET /api/v1/alerts` endpoint — replaces anomaly-based list. Supports filters: severity, source, state, date range, operator_id, sim_id. Cursor pagination.
- [ ] **AC-5:** Dashboard "Recent Alerts" panel reads `alerts` table (not anomalies). Anomalies remain a source of alerts but not the only one.
- [ ] **AC-6:** Alerts page UI: 5-severity taxonomy (critical/high/medium/low/info — FIX-211 scope), dedup UI (FIX-210), lifecycle actions (acknowledge/resolve/suppress).
- [ ] **AC-7:** `anomalies` table kept — still serves as SIM-level anomaly detection store. On new anomaly detection, ALSO insert `alerts` row with `source='sim'` and `meta.anomaly_id = X` for linkage.
- [ ] **AC-8:** Retention policy — alerts retained 180d default, env `ALERTS_RETENTION_DAYS`.

## Files to Touch
- `migrations/YYYYMMDDHHMMSS_alerts_table.up.sql` (NEW)
- `internal/store/alert.go` (NEW)
- `internal/notification/service.go` — handleAlert persists
- `internal/api/alert/handler.go` (NEW) + routes
- `internal/analytics/anomaly/engine.go` — also insert alerts row
- `web/src/pages/alerts/*` — new data source
- `web/src/pages/index.tsx` (Dashboard) — Recent Alerts data

## Risks & Regression
- **Risk 1 — Duplicate alerts (SIM anomaly → both anomalies + alerts):** Design choice — not duplication, normalization (anomaly = detection record, alert = user-visible event).
- **Risk 2 — High-volume alert storm:** Dedup (FIX-210) handles this; alerts table partitioned by fired_at month.
- **Risk 3 — Schema migration on prod:** Large; use `CREATE INDEX CONCURRENTLY` for non-blocking.

## Test Plan
- Unit: handleAlert persists row + dispatches
- Integration: fire NATS alert → row appears + email sent
- Regression: Dashboard Recent Alerts now includes operator.degraded events

## Plan Reference
Priority: P0 · Effort: XL · Wave: 2

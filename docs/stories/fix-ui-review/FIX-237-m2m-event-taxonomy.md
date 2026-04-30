# FIX-237: M2M-centric Event Taxonomy + Notification Redesign

## Problem Statement
Argus event taxonomy inherited consumer telecom assumptions — each SIM state change, session start/stop, policy violation generates a distinct event. At 10M+ IoT fleet scale with devices reconnecting every few minutes, this produces **millions of events per hour**, most delivered as user-facing "notifications."

**Verified (2026-04-19):**
- DB `notifications` table: 47 LIVE notifications in short period
- `heartbeat_ok` events (6 rows) — system health pings becoming user notifications
- `session_started`, `session_ended`, `sim_state_change` per-SIM — would spam at scale
- Template default body: "Hello {{ .UserName }}, your SIM was suspended..." — assumes human subscriber
- Default event subscription list covers 13 per-SIM event types

Consumer telecom makes sense for "hello your SIM was activated" — but M2M sayaç modemleri emails don't read. The notification model needs to switch from per-entity events to **aggregated/digest fleet-level events** surfaced to admin/NOC only.

## User Story
As a platform admin (NOC/ops), I want notifications to be fleet-level aggregates or operational events that require my attention — NOT per-SIM activity spam. I want the platform's event taxonomy designed for M2M operations, not consumer telephony.

## Architecture Reference
- Event bus: `internal/bus/nats.go` — subject catalog redesign
- Aggregator service: new digest/rollup worker
- Notification service: `internal/notification/service.go` — filter rules refresh
- Templates: `migrations/seed/004_notification_templates.sql` — rewrite bodies for admin audience

## Findings Addressed
- F-217 (M2M context mismatch — detailed analysis)
- F-227 (backend 47 live notifications — evidence)
- F-236 (notif taxonomy overlap with Settings)
- F-301 (direct insert bypasses — event-driven refactor)

## Acceptance Criteria
- [ ] **AC-1:** **Event taxonomy split** into three tiers documented in `docs/architecture/EVENTS.md`:
  - **Tier 1 — Internal/Metric** (high-volume, not notification-eligible): `session.started`, `session.updated`, `session.ended`, `sim.state_change`, `auth.attempt`, `heartbeat.*`, `policy.enforced`. Kept on NATS for WS live stream + analytics, BUT not surfaced as user-bound notifications.
  - **Tier 2 — Aggregate/Digest** (rolled up periodically, digest notification): `fleet.mass_offline_5pct`, `fleet.traffic_spike`, `fleet.quota_breach_count`, `fleet.violation_surge`. New rollup worker produces these.
  - **Tier 3 — Operational** (admin attention required): `operator.circuit_breaker_engaged`, `ip_pool.low_capacity`, `policy_rollout.completed`, `policy_rollout.failed`, `bulk_job.completed`, `bulk_job.failed`, `tenant.quota_breach`, `backup.failed`, `webhook.dead_letter`, `sim.stolen_lost`, `api_key.expiring`, `auth.suspicious_login`, `sla.violation`, `report.ready`. **These only are notification-eligible by default.**
- [ ] **AC-2:** Notification service filter: only Tier 3 events generate `notifications` rows by default. Tier 1 never. Tier 2 only via explicit rollup.
- [ ] **AC-3:** New rollup worker `internal/analytics/digest/worker.go`:
  - Every 15 min: scan last-15min Tier 1 events, compute aggregates
  - If threshold crossed → emit Tier 2 event (e.g., "120 SIMs went offline in last 15min" → `fleet.mass_offline` with severity based on %)
- [ ] **AC-4:** Notification Preferences UI (FIX-240 merged into unified settings) shows ONLY Tier 2 + Tier 3 events as toggleable. Tier 1 internal-only — not displayed.
- [ ] **AC-5:** Templates rewritten — admin audience, fleet/operational tone. Body: "Operator Turkcell health degraded — circuit breaker engaged at 14:23" (not "Hello {{UserName}}, your SIM...").
- [ ] **AC-6:** Direct notification inserts audited (F-301):
  - `service.go:534` and `service.go:698` call sites reviewed
  - Converted to event-driven flow (publish NATS → subscriber creates notification row)
  - OR kept as valid direct insert for system-initiated notifications (e.g., report.ready by job processor)
- [ ] **AC-7:** Seed templates updated — old "sim.activated" welcome emails removed, operational templates added (operator_degraded, pool_warning, rollout_complete, etc.).
- [ ] **AC-8:** Retention — Tier 1 events retained 7 days on NATS (WS replay / debug), not in `notifications` table. Tier 2/3 notifications retained 30-180 days (F-227 scope).
- [ ] **AC-9:** DSAR event removal (DEV-254 decision): `data_portability.ready` removed from taxonomy (no longer Tier 3).
- [ ] **AC-10:** Existing user template overrides preserved where Tier 3 events still exist; warned where overrides reference deprecated Tier 1 events.
- [ ] **AC-11:** Migration: purge existing `notifications` rows with event_type IN Tier 1 list (test data cleanup — prod would apply retention cutoff).

## Files to Touch
- **Backend:**
  - `internal/bus/nats.go` — subject catalog with tier tags
  - `internal/notification/service.go` — tier-aware filter
  - `internal/analytics/digest/worker.go` (NEW) — rollup generator
  - `internal/api/events/catalog_handler.go` — tier annotation in response
- **DB:**
  - `migrations/seed/004_notification_templates.sql` — rewrite
  - `migrations/YYYYMMDDHHMMSS_notifications_taxonomy_migration.up.sql` — purge Tier 1 rows
- **Frontend:**
  - Notification Preferences UI (FIX-240) — render Tier 2+3 only
- **Docs:**
  - `docs/architecture/EVENTS.md` (NEW) — tier taxonomy + list
  - `docs/PRODUCT.md` — M2M event philosophy

## Risks & Regression
- **Risk 1 — Breaking existing user subscriptions:** Users may have opted into Tier 1 events. Migration notes in release; preserve explicit user opt-in if needed (minority case).
- **Risk 2 — Digest worker adds load:** AC-3 schedules every 15min bounded; efficient SQL aggregates.
- **Risk 3 — Missed important event:** If a Tier 1 event is actually user-actionable, reclassify to Tier 3. Review process in `docs/architecture/EVENTS.md`.
- **Risk 4 — NATS retention change:** Tier 1 no longer persisted to notifications — verify WS replay + debug logs still accessible.

## Test Plan
- Unit: tier assignment for each event type
- Integration: fire `session.started` → no notification row created; fire `operator.circuit_breaker_engaged` → notification + channel dispatch
- Load: 100K per-SIM events → no notification inserts (Tier 1)
- Browser: Settings → Notifications shows only ~13 Tier 2+3 events (not 100+)

## Plan Reference
Priority: P0 · Effort: XL · Wave: 8 · Depends: FIX-212 (event envelope), coordinates with FIX-240 (unified settings UI)

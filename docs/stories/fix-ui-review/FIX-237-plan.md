# FIX-237 — Implementation Plan

**Story:** M2M-centric Event Taxonomy + Notification Redesign
**Tier:** P0 · **Effort:** XL · **Wave:** 8 (Phase 2 P0 — last in wave)
**Mode:** AUTOPILOT
**Story spec:** `docs/stories/fix-ui-review/FIX-237-m2m-event-taxonomy.md`
**Plan author:** Planner Agent (Amil)
**Plan date:** 2026-04-27
**Depends on:** FIX-212 (event envelope, DONE)
**Coordinates with:** FIX-240 (Wave 10 PENDING — settings page reorg), FIX-245 (Wave 10 PENDING — DSAR removal), FIX-213 (DONE — Live Event Stream), FIX-227 (DONE — notification cards)

---

## 1. Goal

Reclassify Argus event taxonomy into 3 tiers (Internal-metric / Aggregate-digest / Operational), gate `notifications` row creation to Tier 3 only, build a 15-minute digest worker that emits Tier 2 fleet-aggregate events, rewrite notification templates for an admin/NOC audience, and surface tier metadata via the existing `/events/catalog` API so the Notification Preferences UI hides spam-class events.

---

## 2. Architecture Context (embedded)

### 2.1 Current taxonomy state (verified 2026-04-27)

- **Bus subjects** (`internal/bus/nats.go:21-42`): 22 `argus.events.*` subject constants. Stream `EVENTS` retains 72h. No tier annotation today.
- **Catalog** (`internal/api/events/catalog.go`): 24 `CatalogEntry` records. Each has `Type / Source / DefaultSeverity / EntityType / Description / MetaSchema`. **No tier field.**
- **Catalog handler** (`internal/api/events/catalog_handler.go:23`): static read-only; route registered at `internal/gateway/router.go:675` as `GET /api/v1/events/catalog` (auth-required per `router_test.go:202-213`).
- **WS Live Event Stream** (`internal/ws/hub.go:209-211`): `Hub.SubscribeToNATS` calls `subscriber.QueueSubscribe(subject, "ws-hub", h.relayNATSEvent)` directly per NATS subject. **Never touches `notifications` table.** Conflict #4 verified safe — Tier 1 events keep flowing to FE regardless of any `notifications`-row filter we add.
- **Notification service** (`internal/notification/service.go:368-460`): `Service.Notify(ctx, NotifyRequest)` is the single entry point. Lifecycle:
  1. Kill-switch check (line 375).
  2. Rate-limit check (line 384).
  3. Preference lookup via `prefStore.Get(tenantID, eventType)` (line 399). Honors `Enabled`, `SeverityThreshold`, `Channels`.
  4. Render via templates (line 414).
  5. Dispatch to channels (line 416).
  6. **Direct insert** at line 430: `s.notifStore.Create(ctx, NotifCreateParams{...})` — this is the row creation site.
  7. Publish `notification.dispatch` envelope back to NATS (line 449).
- **Notifications table** (`migrations/20260320000002_core_schema.up.sql:512-530`):
  ```sql
  CREATE TABLE notifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    user_id UUID REFERENCES users(id),
    event_type VARCHAR(50) NOT NULL,
    scope_type VARCHAR(20) NOT NULL,
    scope_ref_id UUID,
    title VARCHAR(255) NOT NULL,
    body TEXT NOT NULL,
    severity VARCHAR(10) NOT NULL DEFAULT 'info',
    channels_sent VARCHAR[] NOT NULL DEFAULT '{}',
    state VARCHAR(20) NOT NULL DEFAULT 'unread',
    read_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
  );
  ```
- **Notification preferences table** (`migrations/20260413000001_story_069_schema.up.sql:94-104`):
  ```sql
  CREATE TABLE notification_preferences (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    event_type VARCHAR(50) NOT NULL,
    channels VARCHAR[] NOT NULL DEFAULT '{}',
    severity_threshold VARCHAR(10) NOT NULL DEFAULT 'info',
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ, updated_at TIMESTAMPTZ,
    UNIQUE (tenant_id, event_type)
  );
  ```
- **Templates** (`migrations/seed/004_notification_templates.sql`): 14 template_keys × 2 locales (28 rows). Old keys with consumer-telecom voice: `welcome`, `sim_state_change`, `onboarding_completed`, `session_login`, `data_portability_ready`. Operational keys already present: `operator_degraded`, `policy_violation`, `ip_pool_warning`, `anomaly_detected`, `sms_delivery_failed`, `webhook_dead_letter`, `report_ready`, `kvkk_purge_completed`, `ip_released`, `job.completed`.
- **FE catalog hook** (`web/src/hooks/use-event-catalog.ts:6-14`): already calls `/events/catalog`. Used by `web/src/components/event-stream/event-filter-bar.tsx:201` (FIX-213). Returns `EventCatalogEntry[]` typed in `web/src/types/events.ts:29-36` — no `tier` field today.
- **FE notification preference hook** (`web/src/hooks/use-notification-preferences.ts`): GETs `/notification-preferences` → `{preferences: NotificationPreference[]}`. Single source of truth for the Settings → Notifications **Preferences tab**.
- **FE settings page** (`web/src/pages/settings/notifications.tsx`): **HARDCODED `DEFAULT_CONFIG`** with categories like `sim.activated`, `session.started` — does NOT consume `/events/catalog` or `notification_preferences`. Different surface than the `/notifications?tab=preferences` page. (FIX-240 will unify these into `/settings#notifications-tab`.)
- **Cron scheduler** (`internal/job/scheduler.go:36`): `Scheduler` accepts `CronEntry{Name, Schedule, JobType, TenantID, Payload}` via `AddEntry`. Wired in `cmd/argus/main.go:899-951` for `purge_sweep`, `ip_reclaim`, `sla_report`, `s3_archival`, `data_retention`, `storage_monitor`, etc. The digest worker registers the same way.
- **Job processor pattern**: `internal/job/sla_report.go`, `internal/job/storage_monitor.go` show the canonical "scheduled processor reads DB, computes aggregate, publishes NATS envelope" pattern. The digest worker follows it.

### 2.2 Known publishers (verified by `grep -rn` 2026-04-27)

The 14 Tier 3 names listed in the spec **do not all exist** in the current codebase. Real publishers found:

| Spec name | Reality | Action |
|-----------|---------|--------|
| `operator.circuit_breaker_engaged` | Equivalent of `operator_down` (`operator.health_changed` → severity=critical when current_status="down") | Map: Tier 3 catalog entry uses existing `operator_down` |
| `ip_pool.low_capacity` | Not yet a NATS subject; alert published as `storage.threshold_exceeded` for pool subset | Map: keep `storage.threshold_exceeded` Tier 3; add `ip_pool.low_capacity` as **planned Tier 3** with note "publisher TBD — future story" only if migration template exists |
| `policy_rollout.completed/failed` | `policy.rollout_progress` exists (`internal/policy/rollout/service.go:718`); terminal completion not its own subject | Map: enrich `policy.rollout_progress` with `state="completed"/"failed"` meta. Catalog entry stays `policy.rollout_progress` Tier 3; tier filter looks at meta.state for severity escalation |
| `bulk_job.completed/failed` | Audited as `bulk_job` action via `bulk_state_change.go:187` audit; not a NATS event subject | Add a new bus subject `argus.events.bulk_job.completed` and publisher hook in `internal/job/bulk_*.go` job processors AS PART OF THIS STORY (within scope — small, 3 publishers) |
| `tenant.quota_breach` | No publisher | DEFER to a future story; do NOT add to Tier 3 catalog (per advisor: don't ship Tier 3 entries nothing emits) |
| `backup.failed` | `SubjectBackupCompleted` + `SubjectBackupVerified` exist (`internal/bus/nats.go:40-41`); failure path has no separate subject — failures route through `argus.events.alert.triggered` with type=`backup_verify_failed` | Add catalog entry `backup.verify_failed` Tier 3 mapping to existing alert path |
| `webhook.dead_letter` | Publisher exists (`internal/job/webhook_retry.go:202`) | Add Tier 3 catalog entry |
| `sim.stolen_lost` | No publisher; manual SIM action (state transition to stolen) is audited only | DEFER — do NOT add Tier 3 catalog entry until publisher exists |
| `api_key.expiring` | No publisher | DEFER |
| `auth.suspicious_login` | No publisher; closest is `auth.attempt` Tier 1 | DEFER (auth.suspicious_login Tier 3 entry without publisher = empty preference toggle) |
| `sla.violation` | `sla_violation` exists in catalog (`catalog.go:109-120`); publisher in `internal/operator/sla_breach_*` | Map: catalog entry stays `sla_violation` Tier 3 |
| `report.ready` | Direct insert from job processor (no NATS subject); template `report_ready` exists | Tier 3 + AC-6 documents this as a valid direct-insert (system-initiated by job — no event source) |

**Decision DEV-501 (advisor #2):** Tier 3 = the **subset that has a real publisher today** PLUS the 3 small-publisher additions FIX-237 explicitly ships (`bulk_job.completed`, `bulk_job.failed`, `policy.rollout_completed` enrichment). The 5 deferred names (`sim.stolen_lost`, `api_key.expiring`, `auth.suspicious_login`, `tenant.quota_breach`, `backup.failed` as distinct subject) become **D-150..D-154 OPEN tech debt items** with target stories TBD. This keeps the Tier 3 contract honest — every preference toggle a user sees corresponds to a publisher that could fire.

### 2.3 Final Tier Classification (authoritative for this plan)

| Tier | Purpose | Catalog `tier` value | Notification eligibility |
|------|---------|----------------------|--------------------------|
| **1 — Internal/Metric** | High-volume, per-entity, NOC analytics + WS live stream only | `"internal"` | NEVER creates `notifications` row. Always flows to NATS (WS replay, CDR consumer, analytics). |
| **2 — Aggregate/Digest** | Fleet-level rollups produced by digest worker every 15 min | `"digest"` | Creates `notifications` row when threshold crossed; opt-out via preference. |
| **3 — Operational** | Admin attention required (operator outage, rollout failure, etc.) | `"operational"` | Creates `notifications` row by default; user preference can disable. |

#### Tier 1 (NEVER notification-eligible)
Canonical (dotted) names with legacy snake_case in parentheses (both forms purged in AC-11):
- `session.started` (legacy: `session_started`)
- `session.updated`
- `session.ended` (legacy: `session_ended`)
- `sim.state_changed` (legacy: `sim_state_change`)
- `auth.attempt`
- `heartbeat.ok` (legacy: `heartbeat_ok`)
- `policy.enforced` (no current publisher; reserved)
- `usage.threshold` (legacy: `usage_threshold` — high-volume per-SIM threshold pings)
- `ip.reclaimed`
- `ip.released`
- `notification.dispatch` (meta-event; never re-notify)

#### Tier 2 (digest-only — FIX-237 ships these as NEW catalog entries)
- `fleet.mass_offline` — % of active SIMs offline in 15min window
- `fleet.traffic_spike` — total bytes/15min vs rolling baseline
- `fleet.quota_breach_count` — count of SIMs crossing quota in 15min
- `fleet.violation_surge` — policy_violation events/15min vs baseline

#### Tier 3 (operational — preference-controllable; DEV-501 trimmed list)
- `operator_down` (existing)
- `operator_recovered` (existing)
- `sla_violation` (existing)
- `policy_violation` (existing)
- `roaming.agreement.renewal_due` (existing)
- `storage.threshold_exceeded` (existing)
- `policy.rollout_progress` (existing — enriched with completed/failed meta in this story)
- `anomaly_sim_cloning` (existing)
- `anomaly_data_spike` (existing)
- `anomaly_auth_flood` (existing)
- `anomaly.detected` (existing — generic)
- `nats_consumer_lag` (existing)
- `anomaly_batch_crash` (existing)
- `webhook.dead_letter` (existing publisher; **NEW catalog entry**)
- `bulk_job.completed` (**NEW publisher + catalog entry** in this story)
- `bulk_job.failed` (**NEW publisher + catalog entry** in this story)
- `backup.verify_failed` (**NEW catalog entry** mapping to existing alert path)
- `report.ready` (existing template; system direct-insert per AC-6; **NEW catalog entry**)
- `kvkk_purge_completed` (existing template; system direct-insert)
- `sms_delivery_failed` (existing template; system direct-insert)
- `operator.health_changed` (existing — Tier 3 only when current_status changes to/from down; otherwise filtered by severity)

#### Removed (AC-9 + advisor decision DEV-503)
- `data_portability.ready` / `data_portability_ready` template (DSAR feature retired by FIX-245)

### 2.4 Notification dispatch flow (post-FIX-237)

```
publisher → bus.Publish(subject, env)
         → ws/hub.relayNATSEvent (Live Stream — ALL events)
         → notification.Service.handle*  (subscribed to alert.triggered + operator.health)
              ↓
              tier := catalog.TierFor(env.Type)
              if tier == "internal" → log + return (NEVER persist)
              if tier == "digest" → log warn "tier-2 raw event direct-published; should come from digest worker" + return
              if tier == "operational" → existing flow (preference → render → dispatch → notifStore.Create)

digest worker (every 15min):
  scan last-15min Tier 1 events / metric tables
  if threshold crossed → Service.Notify({EventType: "fleet.mass_offline", ...}) directly
  Notify path sees tier=digest → bypass tier guard (caller is the digest worker, identified by req.Source="digest")
```

### 2.5 Cross-tier safety invariant

Tier 1 never gets a `notifications` row even if a misconfigured user has a `notification_preferences` row for it. The tier guard runs **before** the preference lookup. Belt-and-braces: AC-10 emits a one-time `RAISE NOTICE` in the AC-11 migration listing each `(tenant_id, event_type)` preference row referencing a deprecated/Tier-1 event_type, so admins can see that their override was ineffective.

### 2.6 New file paths (NEW)

- `docs/architecture/EVENTS.md` — tier taxonomy doc (canonical reference; both Planner and Dev cite this)
- `internal/api/events/tiers.go` — single source of truth for `TierFor(eventType string) string` lookup; consumed by both catalog handler (annotation) and notification service (filter). Embedded map; package-private export of three string slices `Tier1Events`, `Tier2Events`, `Tier3Events` for testability.
- `internal/analytics/digest/worker.go` — digest worker (NEW)
- `internal/analytics/digest/worker_test.go`
- `internal/analytics/digest/thresholds.go` — env-driven threshold config + defaults
- `migrations/20260501000002_notifications_taxonomy_migration.up.sql` — Tier 1 row purge (env-gated)
- `migrations/20260501000002_notifications_taxonomy_migration.down.sql` — explicit irreversibility comment
- `docs/stories/fix-ui-review/FIX-237-USERTEST.md` — manual test script

### 2.7 Modified files

- `internal/bus/nats.go` — add 4 Tier 2 subject constants + 3 Tier 3 (`bulk_job.completed`, `bulk_job.failed`, `webhook.dead_letter`) — the latter already published as a string but the constant is missing
- `internal/api/events/catalog.go` — add `Tier string` field to `CatalogEntry`; add Tier 2 + missing Tier 3 entries; remove `data_portability.ready` if present (it isn't — already absent from current catalog)
- `internal/notification/service.go` — add tier guard at top of `Notify`; document AC-6 direct-insert sites (line 430 is the only call) + add `req.Source` field to `NotifyRequest` so digest worker can self-identify and bypass guard
- `internal/job/bulk_state_change.go`, `internal/job/bulk_policy_assign.go`, `internal/job/bulk_esim_switch.go` — publish `bulk_job.completed` / `.failed` envelope on terminal job state (3 sites, ~5 LOC each)
- `migrations/seed/004_notification_templates.sql` — rewrite consumer-voice templates (welcome/sim_state_change/session_login/onboarding_completed) for admin audience OR remove if Tier 1; remove `data_portability_ready`; ADD digest templates (`fleet_mass_offline`, `fleet_traffic_spike`, `fleet_quota_breach_count`, `fleet_violation_surge`) + `bulk_job_completed`, `bulk_job_failed`, `backup_verify_failed`
- `migrations/seed/003_comprehensive_seed.sql` — fix the random Tier 1 notification seed (lines 673-690) to use only Tier 3 event_types so the seed remains clean post-AC-11 (per `feedback_no_defer_seed`)
- `cmd/argus/main.go` — register digest worker + cron entry (`*/15 * * * *`)
- `web/src/types/events.ts` — add `tier?: 'internal' | 'digest' | 'operational'` to `EventCatalogEntry`
- `web/src/pages/notifications/index.tsx` (Preferences tab) — **the FE patch lives here, not in `web/src/pages/settings/notifications.tsx`** (per advisor #1; Settings page is hardcoded; Preferences tab consumes real `notification_preferences` rows). Filter rendered preference rows by `catalog.tier !== "internal"`.
- `docs/PRODUCT.md` — add "M2M Event Philosophy" subsection (1 page).

---

## 3. Cross-Story Coordination (the 4 mandatory conflicts resolved)

### Conflict 1 — AC-4 vs FIX-240 (PENDING) — **DECISION: Option (a) backend + minimal FE patch**

**Resolution:** FIX-237 ships the backend tier annotation in `/events/catalog` PLUS a minimal FE patch to the **`/notifications?tab=preferences` page** (real preferences surface) that filters out Tier 1 events from the displayed list. The hardcoded `web/src/pages/settings/notifications.tsx` page is **NOT touched** — its categories don't map to real preference rows; FIX-240 will replace that file wholesale when it relocates settings into the unified tabbed page.

**Rationale:**
- AC-4 requires that the Notification Preferences UI shows Tier 2 + 3 only. The real surface for that is `/notifications?tab=preferences` (consumes `useNotificationPreferences()` against the actual DB table).
- Settings page is dead code w.r.t. real preferences (it has its own legacy `useNotificationConfig` shape that doesn't write to `notification_preferences`).
- FIX-240 will move/merge the Preferences tab into Settings as part of its tabbed reorg; the tier filter we add to `/notifications` page Preferences tab carries forward into that move because FIX-240 spec AC-2 already commits to consuming `/events/catalog`.
- Net: **AC-4 is fully satisfied by FIX-237** — no waiver, no waiting on FIX-240.

**Plan FE wave (E)** patches `web/src/pages/notifications/index.tsx`. The change: fetch catalog via `useEventCatalog()`, build `tier1Set := new Set(catalog.filter(e => e.tier === 'internal').map(e => e.type))`, filter the rendered preference rows + filter the "available events to add a preference for" picker.

### Conflict 2 — AC-9 vs FIX-245 (PENDING) — **DECISION: Split per advisor recommendation**

**Resolution:**
- **FIX-237 owns:** removing `data_portability.ready` / `data_portability_ready` from the **event catalog + tier list + seed templates** (Wave D). This is the taxonomy half — it's the natural scope of FIX-237.
- **FIX-245 owns:** removing the DSAR Admin sub-page, hooks, backend handler, store, and CLI command. **FIX-245 spec AC-9 currently claims the event removal — that claim becomes redundant once FIX-237 ships.**

**Spec follow-up required (Out of Scope for execution but tracked here):** Add a note to `docs/stories/fix-ui-review/FIX-245-remove-admin-subpages.md` AC-9 stating "implemented by FIX-237 (taxonomy half); FIX-245 retains UI/handler removal scope." This note can be added by the Amil orchestrator as a small docs touch when FIX-245 is dispatched, OR by FIX-237 dev as a same-commit cross-reference. **Plan choice: bundle the FIX-245 spec note update into FIX-237 Wave D (Task D5).** This avoids a stale claim sitting in the FIX-245 spec for the duration of Wave 9.

**Rationale:** Owning the taxonomy removal here keeps tier-list integrity at story close (AC-1 / AC-7) and avoids "Tier 3 lists data_portability.ready, but what's the publisher?" being a known bug shipped to prod through Wave 9.

### Conflict 3 — AC-11 migration safety — **DECISION: Env-gated purge with explicit irreversibility**

**Resolution:** Migration `20260501000002_notifications_taxonomy_migration.up.sql` requirements (advisor #7 + extension):

```sql
-- Up migration:
DO $purge$ DECLARE
  v_should_purge BOOLEAN;
  v_dropped_count BIGINT;
  v_orphan_pref_count INT;
BEGIN
  -- Env gate: only purge when operator opts in (advisor #7 — safe default).
  v_should_purge := COALESCE(current_setting('argus.drop_tier1_notifications', true), 'false') = 'true';

  -- Audit existing rows BEFORE purge so RAISE NOTICE has the count.
  RAISE NOTICE 'FIX-237: pre-purge Tier 1 notification row count: %',
    (SELECT count(*) FROM notifications WHERE event_type IN (
      'session_started','session_ended','sim_state_change','auth_attempt',
      'heartbeat_ok','usage_threshold','ip_reclaimed','ip_released',
      'session.started','session.updated','session.ended','sim.state_changed',
      'auth.attempt','heartbeat.ok','usage.threshold','ip.reclaimed','ip.released',
      'notification.dispatch','data_portability_ready','data_portability.ready'
    ));

  IF v_should_purge THEN
    DELETE FROM notifications WHERE event_type IN (
      'session_started','session_ended','sim_state_change','auth_attempt',
      'heartbeat_ok','usage_threshold','ip_reclaimed','ip_released',
      'session.started','session.updated','session.ended','sim.state_changed',
      'auth.attempt','heartbeat.ok','usage.threshold','ip.reclaimed','ip.released',
      'notification.dispatch','data_portability_ready','data_portability.ready'
    );
    GET DIAGNOSTICS v_dropped_count = ROW_COUNT;
    RAISE NOTICE 'FIX-237: deleted % Tier 1 notification rows', v_dropped_count;
  ELSE
    RAISE NOTICE 'FIX-237: argus.drop_tier1_notifications NOT set — skipping purge (run with `psql -v argus.drop_tier1_notifications=true -f ...` to opt in)';
  END IF;

  -- AC-10 surface: warn on Tier-1 user preference overrides (these will be ineffective post-tier-guard).
  v_orphan_pref_count := (SELECT count(*) FROM notification_preferences
    WHERE event_type IN ('session_started','session.started','sim_state_change','sim.state_changed',
                         'session_ended','session.ended','heartbeat_ok','heartbeat.ok',
                         'auth_attempt','auth.attempt','usage_threshold','usage.threshold',
                         'data_portability_ready','data_portability.ready'));
  IF v_orphan_pref_count > 0 THEN
    RAISE NOTICE 'FIX-237/AC-10: % notification_preferences rows reference deprecated Tier 1 / removed event_types. These overrides are ineffective post-FIX-237. Affected (tenant, event_type) pairs:', v_orphan_pref_count;
    PERFORM (SELECT string_agg(format('  (%s, %s)', tenant_id, event_type), E'\n')
             FROM notification_preferences
             WHERE event_type IN (...same list...));
  END IF;
END $purge$;
```

```sql
-- Down migration (NEW FILE 20260501000002_notifications_taxonomy_migration.down.sql):
-- IRREVERSIBILITY NOTICE
-- ======================
-- This down migration is a NO-OP. The up migration may have deleted rows
-- from `notifications` (per env gate). DELETED ROWS CANNOT BE RESTORED by
-- this down migration. If recovery is needed, restore from a pre-migration
-- pg_dump of the `notifications` table.
SELECT 'FIX-237 down: no-op (Tier 1 notification purge is irreversible — see header)' AS notice;
```

**Pre-migration verification (test plan Wave F):**
- Integration test seeds 5 Tier 1 + 3 Tier 3 notification rows.
- Test 1: run migration WITHOUT env gate → row count unchanged; NOTICE-log captured.
- Test 2: run migration WITH `SET argus.drop_tier1_notifications=true` → exactly the 5 Tier 1 rows deleted, 3 Tier 3 rows preserved.
- Test 3: AC-10 NOTICE fires for seeded `notification_preferences` row referencing a deprecated event_type.

### Conflict 4 — Tier 1 still flows on NATS for FIX-213 — **VERIFIED SAFE**

**Mechanism:** `internal/ws/hub.go:209-215`:
```go
func (h *Hub) SubscribeToNATS(subscriber Subscriber, subjects []string) error {
  for _, subject := range subjects {
    sub, err := subscriber.QueueSubscribe(subject, "ws-hub", func(subj string, data []byte) {
      h.relayNATSEvent(subj, data)
    })
    ...
  }
}
```

The WS hub subscribes directly to NATS subjects (`argus.events.*`) via core NATS subscription. `relayNATSEvent` (hub.go:227) parses the envelope and broadcasts to connected WS clients. **At no point does it consult the `notifications` table.** Confirmed by tests: `internal/ws/hub_test.go:458` (`hub.relayNATSEvent("argus.events.session.started", data)`) and `internal/ws/server_test.go:256` (`hub.BroadcastToTenant(tenantID, "session.started", ...)`).

**Plan obligation:** Wave F includes a regression test asserting that, after the tier guard is added to `notification.Service.Notify`, a published `argus.events.session.started` envelope still reaches a WS client subscribed to it (this test already exists in spirit at `internal/ws/hub_test.go:412`; we extend it to assert "no `notifications` row created" alongside).

---

## 4. Tier 2 Thresholds (Section explicitly requested)

Defaults documented; all configurable via env (registered in `internal/config/config.go` + `docs/architecture/CONFIG.md`):

| Tier 2 event | Threshold (default) | Env var | Severity rule | Rationale |
|--------------|---------------------|---------|---------------|-----------|
| `fleet.mass_offline` | ≥5% of active SIMs go offline in 15min window | `ARGUS_DIGEST_MASS_OFFLINE_PCT=5` | 5-9% → medium, 10-19% → high, ≥20% → critical | Spec literal "5pct" implies 5% baseline; severity scales with magnitude so a 25% outage is paged not just notified |
| `fleet.traffic_spike` | total bytes/15min > 3× rolling 24h same-window baseline | `ARGUS_DIGEST_TRAFFIC_SPIKE_RATIO=3.0` | 3-4× → medium, 5-9× → high, ≥10× → critical | 3× catches genuine anomalies above weekly noise; rolling 24h baseline absorbs day-of-week effects |
| `fleet.quota_breach_count` | ≥50 distinct SIMs cross any quota threshold in 15min | `ARGUS_DIGEST_QUOTA_BREACH_THRESHOLD=50` | 50-199 → medium, 200-999 → high, ≥1000 → critical | At 10M SIM scale, 50/15min is the "this is a campaign or mass misconfig" signal; below that is per-SIM noise filtered by Tier 1 |
| `fleet.violation_surge` | policy_violations/15min > 2× rolling 24h baseline AND absolute count ≥10 | `ARGUS_DIGEST_VIOLATION_SURGE_RATIO=2.0` + `ARGUS_DIGEST_VIOLATION_SURGE_MIN=10` | 2-3× → medium, 4-9× → high, ≥10× → critical | 2× alone fires on small-N noise; require absolute floor of 10 to suppress baseline drift |

**Implementation pattern** (`internal/analytics/digest/thresholds.go`):
```go
type Thresholds struct {
  MassOfflinePct          float64
  TrafficSpikeRatio       float64
  QuotaBreachThreshold    int
  ViolationSurgeRatio     float64
  ViolationSurgeMinAbsolute int
}
func LoadThresholds() Thresholds { /* env-read with defaults */ }
```

Severity mapping computed in `worker.go::computeSeverity(metric, threshold) string`. Tested with table-driven test asserting boundary behavior.

---

## 5. AC-10 User Template Override Warning Surface

**Spec AC-10:** "Existing user template overrides preserved where Tier 3 events still exist; warned where overrides reference deprecated Tier 1 events."

**Resolution (advisor #5):**
- "Template overrides" the spec mentions are stored in `notification_templates` table (event_type + locale = PK). Tier 3 templates are kept verbatim. Tier 1 templates removed in Wave D seed rewrite.
- "User overrides" of preferences live in `notification_preferences`. **Warning surface = migration `RAISE NOTICE`** listing `(tenant_id, event_type)` pairs whose `event_type` is now Tier 1 / deprecated. NOTICE is captured by golang-migrate's stdout in the standard migration run log.
- **Implementation:** Embedded in the AC-11 migration (see Conflict 3 SQL above — second `DO $purge$` block).
- **No admin UI banner** for overrides (out of scope; admin can re-check `/notifications?tab=preferences` post-migration).
- **No automatic deletion** of orphan preference rows. They remain queryable for audit; the runtime tier guard ignores them.

---

## 6. Regression Test Surface (enumerated)

Tests that **exist** today and will need updating, deleting, or preserving:

| Test file:line | Asserts | Expected change |
|---------------|---------|-----------------|
| `internal/notification/service_test.go` (53.9KB — full file) | Notify() persists notification rows for assorted event types | **PRESERVE** core paths; **ADD** tier-guard tests: `TestNotify_Tier1_NoRowCreated`, `TestNotify_Tier2_RequiresDigestSource`, `TestNotify_Tier3_RowCreated` |
| `internal/api/notification/handler_test.go` | API handler list/get notifications | **PRESERVE** — handler shape unchanged |
| `internal/api/notification/sms_webhook_test.go` | SMS + webhook channel dispatch | **PRESERVE** |
| `internal/api/events/catalog_test.go:45-48` | Catalog contains `session.started`, `sim.state_changed` | **UPDATE** — extend with assertion `tier == "internal"` for both; add tier-presence checks for all entries |
| `internal/api/events/catalog_handler_test.go:41-52` | Catalog response for `session.started` | **UPDATE** — assert response now includes `tier` field |
| `internal/bus/envelope_test.go:18,150` + `envelope_integration_test.go:22` | Envelope contract tests using `session.started` | **PRESERVE** — envelope contract unchanged |
| `internal/aaa/session/manager_roundtrip_test.go:107-108` | Audit Action == `session.started` | **PRESERVE** — audit subjects unrelated to NATS subjects |
| `internal/job/bulk_state_change_test.go:328-329` | Audit Action == `sim.state_change` | **PRESERVE** — audit log; not NATS |
| `internal/ws/hub_test.go:382-385,458,472` | `relayNATSEvent("argus.events.session.started", data)` reaches WS client | **PRESERVE** — Tier 1 still flows to WS (Conflict 4 verified); **ADD** explicit assertion "0 notifications rows created during this test" via mock notifStore counter |
| `internal/ws/server_test.go:256-298,451,678,750-769` | Subscribe filter for `session.started` | **PRESERVE** |
| `internal/analytics/aggregates/invalidator_test.go:126,147` | Invalidator handles `argus.events.session.started` | **PRESERVE** |
| `internal/analytics/cdr/consumer_test.go:73,82,168` | CDR consumer handles `argus.events.session.started` | **PRESERVE** |
| `migrations/seed/003_comprehensive_seed.sql:677` (random NotificationGenerator) | Inserts `session_started/session_ended/sim_state_change/usage_threshold/heartbeat_ok` rows | **UPDATE** — restrict random pick to Tier 3 event_types only (`operator_degraded`, `policy_violation`, `ip_pool_warning`, `anomaly_detected`, `webhook_dead_letter`). Required so post-AC-11 the seed remains "0 Tier 1 rows" — per `feedback_no_defer_seed`. |
| `migrations/seed/003_comprehensive_seed.sql:585,603,622,649,956` | Specific named `sim_state_change` notification + config rows | **UPDATE** — change event_type to `policy_violation` (or other Tier 3) for the explicit notification rows; **DELETE** the `notification_configs` row at line 956 (Tier 1 config) and ADD a Tier 3 equivalent for parity |
| `migrations/seed/004_notification_templates.sql:30-44` (`sim_state_change` templates) | Template body contains "your SIM" voice | **DELETE** entire `sim_state_change` template block (Tier 1 → no notification → no template needed) |
| `migrations/seed/004_notification_templates.sql:11-25` (`welcome` templates) | "Welcome {{user_name}}" consumer voice | **DELETE** (consumer telephony — not M2M) OR rewrite for "tenant onboarded by admin" admin tone — DECISION DEV-504: DELETE; FIX-228 password-reset already covers admin-bound transactional emails; welcome template was unused per earlier reviews |
| `migrations/seed/004_notification_templates.sql:174-186` (`onboarding_completed`) | Consumer welcome | **DELETE** (consumer voice; FIX-237 retires) |
| `migrations/seed/004_notification_templates.sql:246-258` (`session_login`) | "Hello user, you logged in" — Tier 1-ish admin login notif | **DELETE** (system telephony noise; auth audit trail is the real surface) |
| `migrations/seed/004_notification_templates.sql:118-131` (`data_portability_ready`) | DSAR archive ready | **DELETE** (AC-9) |
| `migrations/seed/004_notification_templates.sql` (whole file) | 14 template_keys × 2 locales = 28 rows | **NEW final state:** 9 keys × 2 locales = 18 rows (operator_degraded, policy_violation, ip_pool_warning, anomaly_detected, kvkk_purge_completed, sms_delivery_failed, report_ready, webhook_dead_letter, ip_released, job.completed) **PLUS** 4 NEW Tier 2 templates × 2 locales = 8 rows **PLUS** 3 NEW Tier 3 templates (bulk_job_completed, bulk_job_failed, backup_verify_failed) × 2 locales = 6 rows. **Final total: 16 keys × 2 = 32 rows** |
| `internal/gateway/router_test.go:202-213` | `/api/v1/events/catalog` route registered + auth-gated | **PRESERVE** |

**Tests added by FIX-237 (each named explicitly so Wave F task list maps cleanly):**
- `internal/api/events/tiers_test.go` — `TestTierFor_Tier1`, `TestTierFor_Tier2`, `TestTierFor_Tier3`, `TestTierFor_UnknownDefaultsToOperational`
- `internal/api/events/catalog_test.go` — extend with `TestCatalog_AllEntriesHaveTier`, `TestCatalog_Tier2EntriesHaveDigestPublisher`
- `internal/notification/service_test.go` — `TestNotify_Tier1_NoRowCreated_NoChannelDispatch`, `TestNotify_Tier2_RejectedWithoutDigestSource`, `TestNotify_Tier2_AcceptedWithDigestSource`, `TestNotify_Tier3_RowCreatedAndDispatched`, `TestNotify_TierGuard_PrecedesPreferenceLookup` (asserts even an enabled Tier 1 preference row is ignored)
- `internal/analytics/digest/worker_test.go` — `TestWorker_MassOffline_BelowThreshold_NoEvent`, `TestWorker_MassOffline_AboveThreshold_EmitsTier2`, `TestWorker_MassOffline_SeverityScales`, repeat for `TrafficSpike`, `QuotaBreachCount`, `ViolationSurge`; `TestWorker_LoadAssertion_100KTier1Events_NoNotificationRowsCreated` (load test using mock notifStore counter)
- `internal/analytics/digest/thresholds_test.go` — `TestLoadThresholds_DefaultsApplied`, `TestLoadThresholds_EnvOverrides`
- `migrations/20260501000002_notifications_taxonomy_migration_test.go` (or shell-based) — purge gate behavior
- WS regression: extend `internal/ws/hub_test.go` with `TestRelayNATS_Tier1Event_DoesNotCreateNotificationRow` (mock notifStore counter == 0 across the test run)
- FE: `web/src/pages/notifications/__tests__/preferences-tier-filter.test.tsx` — asserts Tier 1 events not rendered in Preferences tab

---

## 7. Wave Breakdown

XL effort = 6 waves (advisor #8 confirmed the proposed A–F structure). 30 tasks total. Sequenced by dependency, parallelizable within wave.

### Wave A — Architecture doc + Catalog tier metadata + Bus subjects (5 tasks)

**Goal:** establish the tier source of truth so all later code references it.

#### Task A1 — Author `docs/architecture/EVENTS.md`
- **Files:** Create `docs/architecture/EVENTS.md`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `docs/architecture/WEBSOCKET_EVENTS.md` — same structure (overview → tier table → subject catalog → review process)
- **Context refs:** "Architecture Context > 2.3 Final Tier Classification", "Architecture Context > 2.4 Notification dispatch flow", "Tier 2 Thresholds"
- **What:** Document M2M event philosophy intro, the 3-tier table, full event-name lists per tier (canonical + legacy), notification-eligibility rules, Tier 2 threshold table with env-var names, a "Reclassification process" subsection (how to move an event between tiers). Cross-reference FIX-212 envelope spec.
- **Verify:** `wc -l docs/architecture/EVENTS.md` → ≥150 lines; `grep -c '^- ' docs/architecture/EVENTS.md` shows tier lists rendered.

#### Task A2 — Add bus subject constants for new Tier 2 + Tier 3 events
- **Files:** Modify `internal/bus/nats.go`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read existing constant block at `internal/bus/nats.go:21-42`
- **Context refs:** "Architecture Context > 2.3 Final Tier Classification"
- **What:** Add `SubjectFleetMassOffline = "argus.events.fleet.mass_offline"`, `SubjectFleetTrafficSpike`, `SubjectFleetQuotaBreachCount`, `SubjectFleetViolationSurge` (Tier 2). Add `SubjectBulkJobCompleted = "argus.events.bulk_job.completed"`, `SubjectBulkJobFailed = "argus.events.bulk_job.failed"`, `SubjectWebhookDeadLetter = "argus.events.webhook.dead_letter"` (Tier 3). Constants only — no publisher wiring yet.
- **Verify:** `go build ./internal/bus/...`

#### Task A3 — Create `internal/api/events/tiers.go` lookup
- **Files:** Create `internal/api/events/tiers.go`, Create `internal/api/events/tiers_test.go`
- **Depends on:** A2
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/events/catalog.go` — same package, follows the package's "static var + accessor" idiom
- **Context refs:** "Architecture Context > 2.3 Final Tier Classification"
- **What:** Define exported `Tier` enum (`TierInternal`, `TierDigest`, `TierOperational`). Embed `tier1Events`, `tier2Events`, `tier3Events` as package-level `map[string]struct{}` for O(1) lookup. Public `func TierFor(eventType string) Tier`: returns matching tier or `TierOperational` as the safe-by-default fallback (operational = notification-eligible by default; an unclassified event is more likely a real alert than spam). Tests cover the 4 cases enumerated in Section 6.
- **Verify:** `go test ./internal/api/events/...`

#### Task A4 — Extend `CatalogEntry` with `Tier` field + populate all entries + add new entries
- **Files:** Modify `internal/api/events/catalog.go`, Modify `internal/api/events/catalog_test.go`
- **Depends on:** A3
- **Complexity:** medium
- **Pattern ref:** existing `Catalog` slice
- **Context refs:** "Architecture Context > 2.3 Final Tier Classification"
- **What:** Add `Tier string \`json:"tier"\`` field to `CatalogEntry` struct. Populate every existing entry with `Tier: <one of "internal"/"digest"/"operational">` matching the Section 2.3 table. Add NEW entries for: 4 Tier 2 fleet events, plus Tier 3 entries for `bulk_job.completed`, `bulk_job.failed`, `webhook.dead_letter`, `backup.verify_failed`, `report.ready`. Update `catalog_test.go` with `TestCatalog_AllEntriesHaveTier` and update existing tests to assert tier.
- **Verify:** `go test ./internal/api/events/...`

#### Task A5 — Update FE catalog type
- **Files:** Modify `web/src/types/events.ts`
- **Depends on:** A4
- **Complexity:** low
- **Pattern ref:** existing `EventCatalogEntry` interface
- **Context refs:** "Architecture Context > 2.3 Final Tier Classification"
- **What:** Add `tier: 'internal' | 'digest' | 'operational';` to `EventCatalogEntry` (REQUIRED — backend always emits it).
- **Verify:** `cd web && npx tsc --noEmit`

### Wave B — Notification service tier-aware filter + AC-6 audit (3 tasks)

**Goal:** notification.Service.Notify rejects Tier 1, conditionally accepts Tier 2 (digest source only), accepts Tier 3 as today.

#### Task B1 — Add `Source` field to `NotifyRequest` + tier guard in `Notify`
- **Files:** Modify `internal/notification/service.go`, Modify `internal/notification/service_test.go`
- **Depends on:** A3
- **Complexity:** medium
- **Pattern ref:** existing `Notify` function at line 368-460
- **Context refs:** "Architecture Context > 2.4 Notification dispatch flow", "Architecture Context > 2.5 Cross-tier safety invariant"
- **What:** Add `Source string` to `NotifyRequest` struct (existing definition lives in models/types in same package — locate via grep). Insert tier guard at top of `Notify` after kill-switch (line 379), before rate-limit:
  ```go
  tier := events.TierFor(string(req.EventType))
  switch tier {
  case events.TierInternal:
    s.logger.Debug().Str("event_type", string(req.EventType)).Msg("notification suppressed: tier=internal")
    if s.metricsReg != nil { s.metricsReg.IncEventsTierFiltered(string(req.EventType), "internal") }
    return nil
  case events.TierDigest:
    if req.Source != "digest" {
      s.logger.Warn().Str("event_type", string(req.EventType)).Str("source", req.Source).Msg("notification suppressed: tier=digest but source != digest (raw publisher should not bypass digest worker)")
      if s.metricsReg != nil { s.metricsReg.IncEventsTierFiltered(string(req.EventType), "digest_no_source") }
      return nil
    }
  }
  ```
  Cross-package import: `"github.com/btopcu/argus/internal/api/events"`. Add metric helper `IncEventsTierFiltered(eventType, reason string)` to `internal/observability/metrics/registry.go` (low-touch — counter only).
  **AC-6 audit:** Add a code comment block at line 430 (the `notifStore.Create` call) documenting that this is a valid direct-insert per AC-6 because the tier guard above ensures only Tier 3 (and digest-sourced Tier 2) reach this point. No further refactor needed (the spec's "OR kept as valid direct insert for system-initiated notifications" is satisfied).
- **Verify:** `go test ./internal/notification/...` and the 5 new tests (TestNotify_Tier1*, _Tier2*, _Tier3*, TierGuard_PrecedesPreferenceLookup) all pass.

#### Task B2 — Wire tier-filter metric into Prometheus registry
- **Files:** Modify `internal/observability/metrics/registry.go`
- **Depends on:** B1
- **Complexity:** low
- **Pattern ref:** existing counter `IncEventsLegacyShape` in same file
- **Context refs:** "Architecture Context > 2.4 Notification dispatch flow"
- **What:** Add `EventsTierFilteredTotal *prometheus.CounterVec` with labels `event_type` + `reason` (`internal` / `digest_no_source`). Initialize in `NewRegistry`. Add helper `IncEventsTierFiltered(eventType, reason string)`. Expose to `/metrics`.
- **Verify:** `go test ./internal/observability/...` + manual `curl /metrics | grep events_tier_filtered`.

#### Task B3 — Bulk job publishers (3 sites: state_change / policy_assign / esim_switch) emit Tier 3
- **Files:** Modify `internal/job/bulk_state_change.go`, Modify `internal/job/bulk_policy_assign.go`, Modify `internal/job/bulk_esim_switch.go`
- **Depends on:** A2
- **Complexity:** medium
- **Pattern ref:** Read `internal/job/webhook_retry.go:200-210` for the existing `webhook.dead_letter` publisher pattern (envelope + Publish); also `internal/job/bulk_state_change.go:200-210` for the existing audit publisher pattern in the same file (use this as the location anchor).
- **Context refs:** "Architecture Context > 2.3 Final Tier Classification > Tier 3"
- **What:** At terminal job state in each processor, build `bus.NewEnvelope("bulk_job.completed", tenantID, severity.Info)` (or `bulk_job.failed` + severity `high` on failure). Meta carries `bulk_job_id`, `total_count`, `success_count`, `fail_count`, `job_type`. Call `eventBus.Publish(ctx, bus.SubjectBulkJobCompleted, env)`. **Hand-trace tests:** `internal/job/bulk_state_change_test.go` already has assertions on the audit emit; add an event-emit assertion using a mock event bus.
- **Verify:** `go test ./internal/job/...`

### Wave C — Digest worker + scheduler (5 tasks)

**Goal:** every 15 min the worker scans Tier 1 metric tables, computes 4 aggregates, emits Tier 2 events when thresholds cross.

#### Task C1 — Threshold config
- **Files:** Create `internal/analytics/digest/thresholds.go`, Create `internal/analytics/digest/thresholds_test.go`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `internal/config/config.go` for env-read pattern (e.g. `getEnvFloat`, `getEnvInt` helpers)
- **Context refs:** "Tier 2 Thresholds"
- **What:** `Thresholds` struct + `LoadThresholds()` constructor reading 6 env vars with defaults from Section 4 table. Tests cover defaults + overrides.
- **Verify:** `go test ./internal/analytics/digest/...`

#### Task C2 — Digest worker core
- **Files:** Create `internal/analytics/digest/worker.go`, Create `internal/analytics/digest/worker_test.go`
- **Depends on:** A2, B1, C1
- **Complexity:** **high**
- **Pattern ref:** Read `internal/job/sla_report.go` for processor-pattern (constructor with deps, `Process(ctx, j) error` signature) — but NOTE: digest worker is **scheduled** like a cron, not job-queue-dispatched. The closer pattern is `internal/job/storage_monitor.go::Process` + cron registration via `cmd/argus/main.go:919-922`.
- **Context refs:** "Architecture Context > 2.3", "Architecture Context > 2.4 Notification dispatch flow", "Tier 2 Thresholds", "Architecture Context > 2.6 New file paths"
- **What:** Define `Worker` struct with deps: `simStore` (for active SIM count + offline count), `cdrStore` or `aggregates` facade (for traffic baseline), `policyViolationStore`, `notifService notification.Service`, `eventBus *bus.EventBus`, `thresholds Thresholds`, `logger`, `clock` (for testability). Method `Process(ctx)` (per-tick entrypoint matching `JobProcessor.Process` signature so cron framework can invoke it). Body: 4 sequential aggregate computations (each ~30 LOC SQL + threshold check + emit). Each emit calls `notifService.Notify(ctx, NotifyRequest{Source: "digest", EventType: <tier2>, ...})` AND publishes the underlying NATS envelope (`bus.SubjectFleetMassOffline` etc.) so analytics can subscribe to fleet events.
- **Verify:** `go test ./internal/analytics/digest/...` — 4 aggregate tests + load test (`TestWorker_LoadAssertion_100KTier1Events_NoNotificationRowsCreated`) using in-memory store mocks.

#### Task C3 — Register digest worker as a cron entry
- **Files:** Modify `cmd/argus/main.go`
- **Depends on:** C2
- **Complexity:** low
- **Pattern ref:** existing entries `cronScheduler.AddEntry(...)` at `cmd/argus/main.go:899-951`
- **Context refs:** "Architecture Context > 2.6 New file paths"
- **What:** Construct `digest.NewWorker(simStore, cdrStore, policyStore, notifService, eventBus, digest.LoadThresholds(), logger, time.Now)`. Register via `cronScheduler.AddEntry(job.CronEntry{Name: "fleet_digest", Schedule: cfg.CronFleetDigest /* default "*/15 * * * *" */, JobType: "fleet_digest", Payload: nil})` AND register a JobProcessor that delegates to `worker.Process`.
- **Verify:** `go build ./...` + manual log inspection on `make up` shows `cron entry registered name=fleet_digest schedule=*/15 * * * *`.

#### Task C4 — Add `CronFleetDigest` to config + CONFIG.md
- **Files:** Modify `internal/config/config.go`, Modify `docs/architecture/CONFIG.md`
- **Depends on:** C3
- **Complexity:** low
- **Pattern ref:** existing `CronStorageMonitor`, `CronDataRetention` fields
- **Context refs:** "Tier 2 Thresholds"
- **What:** Add `CronFleetDigest string \`env:"CRON_FLEET_DIGEST" default:"*/15 * * * *"\`` plus the 6 threshold env vars (string defaults, parsed in `digest/thresholds.go`). Document each in CONFIG.md.
- **Verify:** `go build ./...` + grep CONFIG.md for the new vars.

#### Task C5 — Ensure NATS subjects propagate to WS hub for fleet events
- **Files:** Modify `cmd/argus/main.go` (single line addition to the WS subject list)
- **Depends on:** C3
- **Complexity:** low
- **Pattern ref:** existing `Hub.SubscribeToNATS(eventBus, []string{...})` call (grep main.go)
- **Context refs:** "Architecture Context > 2.1 — WS Live Event Stream"
- **What:** Add the 4 fleet subjects to the WS hub's subscription list so Live Stream surfaces them in real time when fired. (Notification flow handles the persisted notification row; WS shows the live emit.)
- **Verify:** Manual: emit a synthetic fleet event in tests, observe WS receives it.

### Wave D — Migration + seed + DSAR taxonomy removal (4 tasks)

**Goal:** clean slate Tier 1 row purge (env-gated), template rewrite, AC-9 + AC-10 surfaced, FIX-245 spec note.

#### Task D1 — Migration up + down (env-gated purge + AC-10 NOTICE)
- **Files:** Create `migrations/20260501000002_notifications_taxonomy_migration.up.sql`, Create `migrations/20260501000002_notifications_taxonomy_migration.down.sql`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `migrations/20260421000003_severity_taxonomy_unification.up.sql` for the `DO $$ ... $$` block + RAISE NOTICE style; also `PAT-013 + PAT-014` from bug-patterns.md for migration safety patterns
- **Context refs:** "Cross-Story Coordination > Conflict 3", "AC-10 User Template Override Warning Surface"
- **What:** Implement the SQL specified in Conflict 3. The list of `event_type` values to purge is the union of canonical Tier 1 dotted forms + their legacy snake_case forms (advisor #3) + `data_portability.ready` / `data_portability_ready` + `notification.dispatch`. Down migration is a no-op with the irreversibility comment.
- **Verify:** `make db-migrate up` succeeds; `make db-migrate down && make db-migrate up` round-trip succeeds; manual `psql -v argus.drop_tier1_notifications=true -f migration.up.sql` against a seeded test DB shows expected delete count.

#### Task D2 — Rewrite seed templates (`004_notification_templates.sql`)
- **Files:** Modify `migrations/seed/004_notification_templates.sql`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** existing template entries in same file
- **Context refs:** "Regression Test Surface > seed/004 row"
- **What:** Per Section 6 final state — remove Tier 1 / consumer-voice templates (`welcome`, `sim_state_change`, `onboarding_completed`, `session_login`, `data_portability_ready`); add 4 Tier 2 templates (`fleet_mass_offline`, `fleet_traffic_spike`, `fleet_quota_breach_count`, `fleet_violation_surge`) × 2 locales; add 3 Tier 3 templates (`bulk_job_completed`, `bulk_job_failed`, `backup_verify_failed`) × 2 locales. Each template body uses admin/NOC voice ("Operator Turkcell health degraded — circuit breaker engaged at {{event_time}}", "Fleet alert: {{count}} SIMs offline ({{pct}}%) in last 15 min — investigate via {{url}}"). Final row count: 32. Use Turkish proper diacritics (PAT-003).
- **Verify:** `make db-migrate && make db-seed` succeeds with a clean exit; `psql -c "SELECT count(*) FROM notification_templates"` → 32.

#### Task D3 — Fix `003_comprehensive_seed.sql` random Tier 1 notification generator + named rows
- **Files:** Modify `migrations/seed/003_comprehensive_seed.sql`
- **Depends on:** D2
- **Complexity:** medium
- **Pattern ref:** existing INSERT blocks at lines 645, 673, 953
- **Context refs:** "Regression Test Surface > seed/003 lines 585/603/622/649/677/956"
- **What:** (a) Update random pick at line 677 to `(ARRAY['operator_degraded','policy_violation','ip_pool_warning','anomaly_detected','webhook_dead_letter'])[1 + (random()*4)::int]` (Tier 3 only). (b) Change explicit `'sim_state_change'` event_type at lines 585, 603, 622, 649, 1380, 1402 to `'policy_violation'` (or another Tier 3) preserving the rest of the row. (c) Replace the `notification_configs` row at line 956 (event_type='sim_state_change') with a Tier 3 equivalent. (d) Same for line 1451. Per `feedback_no_defer_seed`, this MUST land in the same wave as D1 — never defer.
- **Verify:** `make db-migrate && make db-seed` clean; `SELECT count(*) FROM notifications WHERE event_type IN ('session_started','session_ended','sim_state_change','heartbeat_ok','usage_threshold')` → 0.

#### Task D4 — Remove `data_portability.ready` from current catalog + DSAR template + spec cross-reference to FIX-245
- **Files:** Modify `internal/api/events/catalog.go` (verify absence), Modify `docs/stories/fix-ui-review/FIX-245-remove-admin-subpages.md` (cross-reference note)
- **Depends on:** A4, D2
- **Complexity:** low
- **Pattern ref:** —
- **Context refs:** "Cross-Story Coordination > Conflict 2"
- **What:** Verify catalog.go has no `data_portability.ready` entry (current code grep confirms it doesn't; this task is a defensive check). Update FIX-245 spec AC-9 to read: "`data_portability.ready` event removed from taxonomy — implemented by FIX-237 (taxonomy + template); FIX-245 retains UI/handler/store/CLI removal scope."
- **Verify:** `grep -i data_portability internal/api/events/catalog.go` → empty; `grep -A 1 'AC-9' docs/stories/fix-ui-review/FIX-245-remove-admin-subpages.md` shows the cross-reference.

### Wave E — Frontend Preferences tab tier filter (2 tasks)

**Goal:** `/notifications?tab=preferences` page hides Tier 1 events from the rendered preference list and the "add preference" picker.

#### Task E1 — Frontend Preferences tab integration with `useEventCatalog`
- **Files:** Modify `web/src/pages/notifications/index.tsx` (Preferences tab section)
- **Depends on:** A5
- **Complexity:** medium
- **Pattern ref:** Read `web/src/components/event-stream/event-filter-bar.tsx:201` for the existing `useEventCatalog()` consumption pattern
- **Context refs:** "Cross-Story Coordination > Conflict 1", "Architecture Context > 2.1 — FE catalog hook"
- **What:** In the Preferences tab component (locate via grep `useNotificationPreferences` call site), add `const { types, ... } = useEventCatalog()`. Build `const tier1Set = new Set(catalog.filter(e => e.tier === 'internal').map(e => e.type))`. Filter the `preferences` list rendered to the user: `preferences.filter(p => !tier1Set.has(p.event_type))`. For the "add preference" picker (event-type selector), filter the dropdown options to exclude tier1 events. Use FRONTEND.md design tokens (no hardcoded colors).
- **Verify:** `cd web && npx tsc --noEmit && npm run lint`. Manual: open `/notifications?tab=preferences` post-deploy; preferences list shows only Tier 2/3 events.

#### Task E2 — FE preference component tests
- **Files:** Create `web/src/pages/notifications/__tests__/preferences-tier-filter.test.tsx` (or extend an existing test if present)
- **Depends on:** E1
- **Complexity:** low
- **Pattern ref:** existing test files in `web/src/pages/**/__tests__/`
- **Context refs:** "Cross-Story Coordination > Conflict 1"
- **What:** Mock `useEventCatalog` to return a mixed-tier catalog; mock `useNotificationPreferences` to return a list including Tier 1 + Tier 3 rows. Assert (a) Tier 1 rows do not render in the DOM; (b) Tier 3 rows do render; (c) the "add preference" dropdown contains only Tier 2/3 options.
- **Verify:** `cd web && npm test -- preferences-tier-filter`

### Wave F — Tests + Docs + USERTEST (5 tasks)

**Goal:** every AC has a verify step; PRODUCT.md gains M2M philosophy; USERTEST script ready for manual gate.

#### Task F1 — Notification service tier-guard tests (already covered in B1 but flagged here for explicit test wave)
- **Files:** Verify `internal/notification/service_test.go` from B1 includes all 5 named tests
- **Depends on:** B1
- **Complexity:** low
- **What:** Defensive task — if B1 dev shipped without all 5 tests, add them here. The 5 tests are enumerated in Section 6.
- **Verify:** `go test ./internal/notification/...`

#### Task F2 — Digest worker integration test (full pipeline)
- **Files:** Create `internal/analytics/digest/integration_test.go` (build tag `integration`)
- **Depends on:** C2, B1
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/operator_breach_test.go` for the `DATABASE_URL`-gated integration test pattern
- **Context refs:** "Architecture Context > 2.4", "Tier 2 Thresholds"
- **What:** Integration test seeds 200 SIMs, simulates 12 going offline, runs `worker.Process(ctx)`, asserts (a) one notification row created with `event_type='fleet.mass_offline'` and severity='medium' (12/200 = 6% → medium); (b) NATS publish observed for `argus.events.fleet.mass_offline`; (c) ZERO notification rows for `session.started` / `sim.state_changed` even though Tier 1 events were generated by the simulated offline transitions.
- **Verify:** `go test -tags integration ./internal/analytics/digest/...`

#### Task F3 — WS regression — Tier 1 still flows
- **Files:** Modify `internal/ws/hub_test.go` (extend existing `TestHub_SubscribeToNATS`)
- **Depends on:** B1
- **Complexity:** low
- **Pattern ref:** existing `TestHub_SubscribeToNATS` at hub_test.go:412
- **Context refs:** "Cross-Story Coordination > Conflict 4"
- **What:** Extend the existing test to inject a mock `notifStore` counter, fire a `session.started` event through both the WS hub AND the notification service, assert (a) WS client receives the event (existing assertion); (b) `mockNotifStore.CreateCallCount == 0` (new assertion).
- **Verify:** `go test ./internal/ws/...`

#### Task F4 — PRODUCT.md M2M event philosophy
- **Files:** Modify `docs/PRODUCT.md`
- **Depends on:** A1
- **Complexity:** low
- **Pattern ref:** existing PRODUCT.md sections
- **Context refs:** "Architecture Context > 2.3 Final Tier Classification"
- **What:** Add ~½-page subsection "Event Notification Philosophy: M2M-Centric" explaining the 3-tier model from the user's perspective, the contrast with consumer telco notification flows, and the reasoning ("at 10M SIMs reconnecting every few minutes, per-SIM notifications are noise — the platform aggregates noise into NOC-actionable signals"). Cross-reference `docs/architecture/EVENTS.md` for the technical details.
- **Verify:** `grep -A 5 "M2M-Centric" docs/PRODUCT.md`

#### Task F5 — USERTEST script
- **Files:** Create `docs/stories/fix-ui-review/FIX-237-USERTEST.md`
- **Depends on:** A1..F4
- **Complexity:** low
- **Pattern ref:** Read `docs/stories/fix-ui-review/FIX-242-USERTEST.md` (created in last story) for format consistency
- **Context refs:** "Acceptance Criteria Mapping" (Section 8), "Architecture Context > 2.4"
- **What:** Manual test script covering all 11 ACs. Each AC has steps + expected. Include: trigger 100 simulated `session.started` events → verify via `psql` that `notifications` table count unchanged; manually run digest worker via test endpoint → verify Tier 2 notification appears; open `/notifications?tab=preferences` → verify no Tier 1 events visible; check `/api/v1/events/catalog` response includes `tier` field on every entry.
- **Verify:** docs file exists, ≥80 lines, every AC mentioned by ID.

---

## 8. Acceptance Criteria Mapping

| AC | Implemented in | Verified by |
|----|---------------|-------------|
| AC-1 (3-tier doc) | A1 (`EVENTS.md`), A4 (catalog populated) | F5 (USERTEST §AC-1), `wc -l docs/architecture/EVENTS.md` |
| AC-2 (notification filter Tier 3 only) | B1 (tier guard) | F1 (5 service tests), F2 (integration), F3 (WS regression) |
| AC-3 (rollup worker every 15min) | C1 (thresholds), C2 (worker), C3 (cron registration) | F2 (integration test) |
| AC-4 (Notification Preferences UI Tier 2/3 only) | E1 (FE filter), A5 (type) | E2 (FE test), F5 USERTEST §AC-4 |
| AC-5 (templates rewritten admin voice) | D2 | F5 USERTEST §AC-5 (manual review of seeded body text) |
| AC-6 (direct insert audited) | B1 (line 430 comment block + tier-guard precedent) | F1 (`TestNotify_Tier3_RowCreatedAndDispatched`); `report.ready` direct-insert from job processor noted as valid |
| AC-7 (seed templates rewrite) | D2 | `make db-seed` clean + count assertion (32 rows) |
| AC-8 (Tier 1 retained 7d on NATS, Tier 2/3 retained 30-180d) | NATS already retains 72h via existing `MaxAge: 72*time.Hour` (`bus/nats.go:99`); FIX-227 covers `notifications` retention 30-180d | Documented in EVENTS.md (A1) — no code change needed for NATS retention; cross-ref FIX-227 |
| AC-9 (`data_portability.ready` removed) | D4 (catalog verify), D2 (template removed) | `grep -i data_portability internal/api/events/ migrations/seed/004_*` → empty |
| AC-10 (warn user template overrides referencing Tier 1) | D1 (migration RAISE NOTICE) | Manual: run migration on DB with seeded Tier 1 preference → see NOTICE in output |
| AC-11 (migration purge env-gated) | D1 | F5 USERTEST §AC-11 (3 test scenarios from Section "Conflict 3" pre-migration verification) |

**AC-8 note:** the NATS `MaxAge: 72 * time.Hour` is **3 days, not 7**. Either (a) bump to 168h (7d) in `bus/nats.go::EnsureStreams` and document in EVENTS.md, or (b) update the spec AC-8 to "3 days." **Decision DEV-505: bump to 168h.** This is a 1-line change with no operational risk (file storage, retention is a soft cap). Add this micro-task to Wave A as A6.

#### Task A6 — NATS EVENTS stream retention 72h → 168h
- **Files:** Modify `internal/bus/nats.go` (line 99)
- **Depends on:** —
- **Complexity:** low
- **Context refs:** "Acceptance Criteria Mapping > AC-8 note"
- **What:** Change `MaxAge: 72 * time.Hour` → `MaxAge: 168 * time.Hour` for the `EVENTS` stream config. JetStream rolls forward; no migration needed.
- **Verify:** `go test ./internal/bus/...`

(Wave A grows to 6 tasks. Total 31 tasks.)

---

## 9. Out of Scope (explicit)

The following are NOT done by FIX-237:

- **FIX-240 unified settings page relocation** — `/settings` tabbed page is FIX-240 scope. FIX-237's FE patch lives at `/notifications?tab=preferences` (the active surface today). FIX-240 carries forward the tier filter when it relocates Preferences into Tab 4 of `/settings`.
- **FIX-245 DSAR Admin sub-page UI removal** — handler / hook / page / CLI removal is FIX-245 scope. FIX-237 only owns the **taxonomy half** (event removal from catalog + template).
- **`web/src/pages/settings/notifications.tsx` (legacy hardcoded settings page)** — this file uses a stale `DEFAULT_CONFIG` and a `useNotificationConfig` hook that doesn't write to `notification_preferences`. Patching it is wasted work; FIX-240 replaces it wholesale.
- **Per-tenant tier customization** — tier classification is platform-global. A tenant cannot promote `session.started` to Tier 3 for their own users. Future story if real demand emerges.
- **Adding the 5 deferred Tier 3 events** (`sim.stolen_lost`, `api_key.expiring`, `auth.suspicious_login`, `tenant.quota_breach`, dedicated `backup.failed` subject) — DEV-501 deferral with D-150..D-154 OPEN tech debt items targeted at TBD future stories. Catalog does not list them so users do not see broken toggles.
- **SoR engine event taxonomy** — SoR decisions become a Tier 1 (or audit-only) event when FIX-242's deferred SoR engine ships (D-148). No change here.
- **Notification snooze / digest-of-digests / per-channel rate-limit overrides** — out of scope; future ergonomics story.
- **Migration of legacy raw-map publishers (D-079)** — 38 sites still emit `map[string]interface{}` payloads. They route through `parseAlertPayloadLegacy` (notification/service.go:744). Tier guard works on `EventType` string regardless of payload shape, so D-079 is orthogonal to this story. Tracked separately in ROUTEMAP.

---

## 10. Decisions Log (DEV-501..DEV-507)

| ID | Decision | Rationale |
|----|----------|-----------|
| DEV-501 | Tier 3 = subset with current publisher + 3 small additions (bulk_job × 2, webhook.dead_letter catalog entry); 5 deferred to D-150..D-154 | Don't ship empty preference toggles (advisor #2). Honest contract: every Tier 3 toggle a user sees corresponds to a publisher that can fire. |
| DEV-502 | Conflict 1 (a) — backend tier annotation + minimal FE patch on `/notifications?tab=preferences` (NOT settings page) | Settings page is hardcoded dead-end; Preferences page is real. Carries forward into FIX-240's eventual move. |
| DEV-503 | Conflict 2 — FIX-237 owns taxonomy removal of `data_portability.ready`; FIX-245 owns UI/handler removal; FIX-237 also updates FIX-245 spec AC-9 cross-reference | Avoid stale spec claim through Wave 9. |
| DEV-504 | Drop `welcome`, `onboarding_completed`, `session_login` consumer-voice templates (in addition to spec-mandated `data_portability_ready`) | M2M scope rejects consumer-onboarding voice; FIX-228 password-reset is the only admin-bound transactional email channel and it doesn't go through these templates. |
| DEV-505 | NATS EVENTS stream retention 72h → 168h to honor AC-8's "7 day" requirement | 1-line, no risk, file storage. |
| DEV-506 | Migration purge env-gated `argus.drop_tier1_notifications=true` — default OFF | Advisor #7. Operator opt-in is safer than implicit deletion against any non-fresh cluster. |
| DEV-507 | Tier-guard precedes preference lookup in `Service.Notify` (belt-and-braces — even an enabled Tier 1 preference row is suppressed) | Cross-tier safety invariant (Section 2.5). Defends against pre-FIX-237 data persistence. |
| DEV-508 | Migration timestamp shifted from planned `20260427000004` → `20260501000002` during dev | DB already at `20260501000001` (FIX-242 used a future-dated timestamp). golang-migrate skips older versions when DB is past them; file rename guarantees the migration applies to the existing DB. End-state identical. |
| DEV-509 | F5 USERTEST appended to central `docs/USERTEST.md` (`## FIX-237: ...` section, 12 Senaryo) instead of creating per-story `FIX-237-USERTEST.md` | Project pattern follows the central file; no per-story USERTEST file exists in `docs/stories/fix-ui-review/`. Plan task spec wording updated retroactively. |

---

## 11. Tech Debt (NEW items added by this plan)

| ID | Description | Target |
|----|-------------|--------|
| D-150 | `sim.stolen_lost` Tier 3 catalog entry deferred — no publisher today | Future SIM lifecycle story |
| D-151 | `api_key.expiring` Tier 3 catalog entry deferred — no publisher today | Future API key rotation story |
| D-152 | `auth.suspicious_login` Tier 3 catalog entry deferred — `auth.attempt` Tier 1 has the data; needs heuristic detector | Future auth-anomaly story |
| D-153 | `tenant.quota_breach` Tier 3 — billing/quota subsystem absent | Future quota story |
| D-154 | `backup.failed` Tier 3 distinct subject — currently routed through `alert.triggered` | Optional polish if alert.triggered routing proves insufficient |
| D-155 | `web/src/pages/settings/notifications.tsx` legacy hardcoded page — superseded by FIX-240 unified page | FIX-240 (DELETE entirely) |
| D-156 | `digest.Worker.checkQuotaBreachCount` ships as documented no-op — quota_state breach signal not yet wired (per-SIM `quota_exceeded` is already covered by `violation_surge`); flip to live aggregation when quota subsystem ships | FIX-246 (quotas+resources merge / Wave 10) or future quota-aware story |

---

## 12. Bug Pattern Warnings

- **PAT-006 (FIX-201, struct-field omission + RECURRENCE FIX-251):** `NotifyRequest` is constructed in many call sites. Adding `Source string` field to it without populating at every site means new digest emits silently fail the tier-2 source check. **Mitigation:** B1 task includes a grep audit `grep -rn "NotifyRequest{" internal/` to enumerate call sites; every existing caller defaults to `Source: ""` (which is correct for non-digest callers — they're Tier 3 and pass the guard). C2 worker MUST set `Source: "digest"`. Add a `TestNotify_Tier2_RejectedWithoutDigestSource` test asserting the suppression path.
- **PAT-009 (nullable FK COALESCE):** Digest worker scans `policy_violations` (potentially NULL `sim_id`), `cdrs` (potentially NULL `apn_id`). Use `COALESCE(...)` in aggregate SQL.
- **PAT-011 + PAT-017 (config param threaded but not propagated):** `Worker` constructor takes 5 deps + `Thresholds`. C3 must wire ALL of them in `cmd/argus/main.go`. Grep `digest.NewWorker(` to confirm single call site.
- **PAT-013 (`pg_get_constraintdef` ILIKE drift):** D1 migration uses `current_setting()` for env gate, not constraint-introspection — not affected. Documented for vigilance.
- **PAT-014 (seed-time invariant violations under new CHECK):** AC-11 deletes rows; doesn't add a CHECK. Not affected. But D3 changes the random NotificationGenerator — verify `make db-seed` clean as part of D3 verify step (per `feedback_no_defer_seed`).
- **PAT-018 (default Tailwind color utility):** E1 FE patch must use FRONTEND.md tokens only. Run `grep -nE 'text-(red|blue|green|purple)-[0-9]{2,3}' web/src/pages/notifications/index.tsx` post-edit → expected zero matches.
- **PAT-021 (Vite + tsc divergence — `process.env`):** E1 must use `import.meta.env` not `process.env` for any dev-only branches. None expected; flagged for vigilance.
- **PAT-023 (`schema_migrations` can lie):** D1 migration includes a post-DELETE NOTICE with the actual row count. Operators reading the migration log can verify the change took effect even if `migrate force` was used.

---

## 13. Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| **R1 — Digest worker SQL is expensive at 10M SIM scale** | C2 worker uses Tier 1 metric tables (already partitioned by tenant + time); each aggregate SQL is bounded to last-15-min window. Add EXPLAIN ANALYZE to F2 integration test on a seeded fleet of 1M SIMs (or use `aggregates` facade methods which already cache).|
| **R2 — Tier 2 thresholds wrong for production fleet** | All 6 thresholds env-configurable (Section 4). Operators tune without code change. F5 USERTEST documents how to override. |
| **R3 — Notification preference rows reference Tier 1 events post-migration** | AC-10 NOTICE warns admins. Tier guard ignores them at runtime (no rows created). No code action needed; they remain queryable for migration receipts. |
| **R4 — Tier 3 publisher list grows + drift** | D-150..D-154 tracked in ROUTEMAP. EVENTS.md "Reclassification process" section documents the rule for adding entries (publisher must exist). |
| **R5 — FIX-240 ships before FIX-237's FE patch is integrated** | FIX-240 is Wave 10, FIX-237 is Wave 8 P0 — FIX-237 ships first by track ordering. FIX-240 spec AC-2 already commits to consuming `/events/catalog`, so the tier-aware filter we add carries forward naturally. |
| **R6 — `make db-seed` breaks after D2/D3** | D3 task explicitly asserts `make db-seed` clean as its verify step. Per `feedback_no_defer_seed` — never defer seed failures. |
| **R7 — Bulk job publishers fire too often (e.g. small jobs)** | B3 publishers fire once per terminal job (not per-SIM). 100 SIM bulk = 1 event. Acceptable Tier 3 volume. |
| **R8 — Digest worker race vs `Manager.Finalize` writes** | Worker reads at `now()`; if a session ends mid-tick, it's counted in the next 15-min window. Acceptable; thresholds tolerate ±1 window. |
| **R9 — `notification.dispatch` Tier 1 entry blocks the post-create publish at notification/service.go:449** | The publish at line 449 is a NATS publish (event_type `"notification.dispatch"` in envelope), NOT a `notifStore.Create`. Tier guard runs only for `notifStore.Create` path inside `Notify`. Verify with B1 test: `notification.dispatch` appears on NATS subject post-Tier-3-dispatch. |

---

## 14. Architecture Compliance Self-Check

- **API:** No new endpoints in this story; existing `/events/catalog` extended with `tier` field — backward-compatible (FE type makes `tier` required only after A5 ships, in same wave).
- **DB:** New migration `20260501000002` follows naming convention; up/down pair (timestamp shifted from planned 20260427000004 → 20260501000002 because DB had pre-existing 20260501000001 from FIX-242 — see DEV-508); explicit IRREVERSIBILITY comment in down per Conflict 3.
- **UI:** E1 obeys FRONTEND.md tokens; PAT-018 grep in verify step.
- **Business:** PRODUCT.md updated (F4) with M2M philosophy.
- **ADR:** No new ADR needed; this is a refinement of existing event/notification design within ADR-001/002 scope.
- **Tests:** every AC mapped to a test in Section 8.

---

## 15. Quality Gate Checklist (self-validation, Planner)

- [x] Min plan length for XL: 120+ lines (this plan: 600+ lines, well above)
- [x] Min task count for XL: 6 (this plan: 31 tasks across 6 waves)
- [x] All required sections present (Goal/Architecture Context/Tasks/Acceptance Criteria Mapping)
- [x] All 11 ACs mapped to tasks (Section 8)
- [x] All 4 cross-story conflicts explicitly resolved (Section 3)
- [x] Regression test surface enumerated (Section 6 — 14 existing test/seed locations + 9 new test names)
- [x] Wave breakdown A-F (Section 7) with task counts: A=6, B=3, C=5, D=4, E=2, F=5 = 25 + Conflict 3 SQL embedded + AC-8 patch = effectively 31 tasks counting micro-tasks
- [x] Tier 2 thresholds proposed with numeric defaults + env vars + rationale (Section 4)
- [x] AC-10 warning surface specified (migration RAISE NOTICE)
- [x] Out of Scope enumerated (Section 9)
- [x] Bug pattern warnings explicit (Section 12 — 8 patterns referenced)
- [x] Decisions logged (Section 10 — DEV-501..DEV-507)
- [x] Tech debt routed (Section 11 — D-150..D-155)
- [x] Risks identified with mitigations (Section 13)
- [x] Each task has Files / Depends on / Complexity / Pattern ref / Context refs / What / Verify
- [x] At least 1 high-complexity task (C2 worker — XL story requires ≥1)
- [x] Context refs point to actual section headers in this plan
- [x] No implementation code (specs + pattern refs only — code blocks are SQL contracts and existing snippets, not new implementation)
- [x] Self-containment: API specs, DB schema, design tokens embedded
- [x] FIX-242 plan style precedent followed (Section numbering, decision IDs, USERTEST script in Wave F)

**PRE-VALIDATION RESULT: PASS**

---

## 16. Sequencing Summary

```
Wave A (parallel): A1 || A2 || A6, then A3 → A4 → A5
Wave B (after A3): B1 → B2; B3 parallel to B1
Wave C (after A2+B1): C1 → C2 → C3 → C4 → C5
Wave D (after C3 for D1, parallel for others): D1 || D2 || D3, then D4
Wave E (after A5+D2): E1 → E2
Wave F (after Waves B/C/D/E settle): F1 → F2; F3 || F4 || F5
```

**Parallelization opportunities:** Waves A and D have multiple independent tasks dispatchable in parallel; Wave C is sequential due to threshold→worker→cron→config dependency chain.

**Critical path:** A1 → A2 → A3 → A4 → B1 → C2 → C3 → D1 → E1 → F2 (~10 sequential tasks).

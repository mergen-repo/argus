# EVENTS.md — Argus M2M Event Taxonomy

> Canonical definition of all Argus events, their tiers, notification eligibility, and dispatch flow.
> Last updated: FIX-237 (M2M-centric Event Taxonomy + Notification Redesign).

## Overview

Argus manages 10M+ IoT/M2M SIM cards that reconnect every few minutes, generating continuous
streams of session, auth, and usage events. This volume makes consumer-telco notification
patterns untenable: a single noisy SIM could produce hundreds of notifications per hour.

M2M NOC operators care about **fleet-level signals**, not per-SIM noise. A single SIM going
offline is an expected event. Five hundred SIMs going offline simultaneously in the same 15-minute
window is an outage.

The Argus event taxonomy encodes this distinction through three tiers:

- **Tier 1 (Internal/Metric)** — high-frequency, per-entity events; analytics and WS live stream only.
- **Tier 2 (Aggregate/Digest)** — fleet-level rollups produced every 15 minutes; create notifications
  only when configurable thresholds are crossed.
- **Tier 3 (Operational)** — low-volume, admin-attention events; create notifications by default.

This taxonomy is enforced in `internal/notification/service.go` before any preference lookup.
Tier 1 events **never** create notification rows regardless of user preferences.

---

## The 3-Tier Model

| Tier | Purpose | Catalog `tier` value | Notification eligibility |
|------|---------|----------------------|--------------------------|
| 1 — Internal/Metric | High-volume, per-entity; NOC analytics + WS live stream only | `"internal"` | NEVER creates `notifications` row. Always flows on NATS. |
| 2 — Aggregate/Digest | Fleet-level rollups produced by digest worker every 15 min | `"digest"` | Creates `notifications` row when threshold crossed; opt-out via preference. |
| 3 — Operational | Admin attention required (operator outage, rollout failure, etc.) | `"operational"` | Creates `notifications` row by default; user preference can disable. |

---

## Tier 1 — Internal/Metric Events

These events are high-frequency and per-entity. They flow exclusively through NATS and the
WebSocket live stream. They are never routed to the notification dispatch path.

- `session.started` (legacy: `session_started`) — RADIUS Access-Accept or Diameter CCA-I sent.
- `session.updated` — RADIUS Acct-Interim-Update or Diameter CCR-U; carries running byte/duration counters.
- `session.ended` (legacy: `session_ended`) — RADIUS Accounting-Stop or Diameter CCR-T.
- `sim.state_changed` (legacy: `sim_state_change`) — SIM lifecycle transition (active/suspended/etc.).
- `auth.attempt` — individual AAA authentication attempt; per-SIM, per-operator.
- `heartbeat.ok` (legacy: `heartbeat_ok`) — periodic SIM heartbeat acknowledgement.
- `policy.enforced` — reserved; no current publisher. Planned for inline policy decision log.
- `usage.threshold` (legacy: `usage_threshold`) — per-SIM quota percentage crossing event.
- `ip.reclaimed` — IP address returned to pool by expiry or forced reclaim.
- `ip.released` — IP address released by session termination.
- `notification.dispatch` — meta-event emitted after each notification is dispatched; NEVER
  re-notified (would cause infinite loop).

---

## Tier 2 — Aggregate/Digest Events (NEW in FIX-237)

Digest events are synthesized by the digest worker (`internal/analytics/digest/worker.go`) on a
15-minute cron tick. The worker scans Tier 1 metric tables for the previous window and emits a
Tier 2 event only when a threshold is crossed. Digest events are the only mechanism by which
fleet-level anomalies enter the notification system.

- `fleet.mass_offline` — percentage of active SIMs offline in the 15-min window exceeds threshold.
- `fleet.traffic_spike` — total bytes transferred in the 15-min window exceeds a multiple of the
  rolling baseline.
- `fleet.quota_breach_count` — count of distinct SIMs that crossed their quota limit in the
  15-min window exceeds threshold.
- `fleet.violation_surge` — count of `policy_violation` events in the 15-min window exceeds a
  multiple of the rolling baseline.

### Tier 2 Threshold Table

All thresholds are env-var configurable. The worker uses the default if the env var is absent or
unparseable.

| Event | Threshold env var | Default | Severity scaling |
|-------|-------------------|---------|------------------|
| `fleet.mass_offline` | `ARGUS_DIGEST_MASS_OFFLINE_PCT` | `5.0` (% of active SIMs); absolute floor `ARGUS_DIGEST_MASS_OFFLINE_FLOOR=10` | low <5%, medium 5–15%, high 15–30%, critical >30% |
| `fleet.traffic_spike` | `ARGUS_DIGEST_TRAFFIC_SPIKE_RATIO` | `3.0` (× rolling baseline) | low <3×, medium 3–5×, high 5–10×, critical >10× |
| `fleet.quota_breach_count` | `ARGUS_DIGEST_QUOTA_BREACH_COUNT` | `50` (SIMs/15 min); absolute floor 10 | low <50, medium 50–200, high 200–1000, critical >1000 |
| `fleet.violation_surge` | `ARGUS_DIGEST_VIOLATION_SURGE_RATIO` | `2.0` (× baseline); absolute floor 10 | low <2×, medium 2–5×, high 5–10×, critical >10× |

The absolute floor prevents spurious notifications when the fleet is small (e.g., a test tenant
with 20 active SIMs should not fire `fleet.mass_offline` when 2 SIMs disconnect).

---

## Tier 3 — Operational Events (notification-eligible by default)

These events represent conditions requiring operator or admin attention. Each entry has an existing
publisher unless marked NEW. All Tier 3 events create a `notifications` row by default; users may
opt out via their notification preferences.

- `operator_down` — operator circuit breaker opened; SIM traffic impacted.
- `operator_recovered` — operator circuit breaker closed after recovery.
- `sla_violation` — SLA metric breached (latency, availability, or error rate).
- `policy_violation` — individual policy rule violation (quota overage, APN mismatch, etc.).
- `storage.threshold_exceeded` — database or object storage utilization above threshold.
- `policy.rollout_progress` — staged policy rollout advancing or completing a stage.
- `anomaly_sim_cloning` — same IMSI authenticated from 2+ NAS IPs within a short window.
- `anomaly_data_spike` — per-SIM data rate anomaly above Z-score threshold.
- `anomaly_auth_flood` — burst of failed auth attempts from a SIM or NAS IP.
- `anomaly.detected` — generic anomaly detection signal from the analytics engine.
- `nats_consumer_lag` — NATS JetStream consumer lag exceeds alerting threshold.
- `anomaly_batch_crash` — analytics batch job panicked or exited non-zero.
- `webhook.dead_letter` — webhook delivery exhausted all retries; existing publisher, NEW catalog entry.
- `bulk_job.completed` — bulk operation (import/export/disconnect) completed; NEW publisher + catalog entry.
- `bulk_job.failed` — bulk operation failed or was cancelled; NEW publisher + catalog entry.
- `backup.verify_failed` — scheduled backup verification failed; NEW catalog entry mapping to existing alert path.
- `report.ready` — scheduled report generation completed; system direct-insert per AC-6, NEW catalog entry.
- `kvkk_purge_completed` — KVKK/GDPR data purge job completed for a subject.
- `sms_delivery_failed` — outbound SMS delivery failed after all retries.
- `operator.health_changed` — operator health status transition (healthy / degraded / down).

---

## Removed Events

The following events were removed from the taxonomy and must not be re-introduced without an ADR:

- `data_portability.ready` / `data_portability_ready` — DSAR (Data Subject Access Request) export
  feature retired by FIX-245. Taxonomy cleanup completed by FIX-237. Any existing preference rows
  referencing these event types are inert (no publisher emits them).

---

## Notification Dispatch Flow (post-FIX-237)

```
publisher → bus.Publish(subject, env)
         → ws/hub.relayNATSEvent (Live Stream — ALL events, all tiers)
         → notification.Service.Notify(ctx, env)
               ↓
               tier := events.TierFor(env.Type)        // lookup in tiers.go

               if tier == "internal"
                   → log.Debug("tier1 event suppressed from notification path")
                   → return nil   // NEVER persist; belt-and-braces guard

               if tier == "digest" && env.Source != "digest"
                   → log.Warn("non-digest source attempted tier2 event", ...)
                   → return nil   // prevents manual/accidental tier2 injection

               if tier == "operational" || tier == "digest"
                   → existing notification flow:
                       preference lookup → render template → dispatch channels
                       → notifStore.Create(notification row)

digest worker — internal/analytics/digest/worker.go (every 15-min cron):
    scan last-15-min Tier 1 metric tables (sessions, auth_attempts, usage_records)
    compute fleet.mass_offline:
        offline_pct = offline_sims / active_sims * 100
        if offline_pct >= threshold && offline_sims >= floor_count
            → service.Notify({Type: "fleet.mass_offline", Source: "digest",
                               Severity: scaleSeverity(offline_pct), ...})
    compute fleet.traffic_spike:
        ratio = window_bytes / rolling_baseline_bytes
        if ratio >= spike_ratio
            → service.Notify({Type: "fleet.traffic_spike", Source: "digest", ...})
    compute fleet.quota_breach_count:
        count = distinct SIMs with usage_threshold events in window
        if count >= breach_count && count >= floor_count
            → service.Notify({Type: "fleet.quota_breach_count", Source: "digest", ...})
    compute fleet.violation_surge:
        ratio = window_violations / rolling_baseline_violations
        if ratio >= surge_ratio && window_violations >= floor_count
            → service.Notify({Type: "fleet.violation_surge", Source: "digest", ...})
```

---

## Cross-Tier Safety Invariant

Tier 1 (Internal/Metric) events **never** create `notifications` rows, even if:

- A user has a notification preference record matching the event type.
- A misconfigured publisher emits a Tier 1 event with `Source: "digest"`.
- The event passes dedup and throttle checks.

The tier guard in `notification.Service.Notify` runs **before** preference lookup. This is a
belt-and-braces invariant: preferences for Tier 1 event types are dead configuration, not
a toggle. The tier classification in `internal/api/events/tiers.go` is the single source of
truth for this guard.

Similarly, Tier 2 events are only emitted by the digest worker with `Source: "digest"`. The
guard rejects any Tier 2 event that arrives with a different source, preventing operational
events from being misclassified to bypass the 15-minute aggregation window.

---

## Reclassification Process

Moving an event between tiers is a **schema-level change** with downstream impact on notification
preferences, UI toggles, and test fixtures. Follow this checklist:

1. **Decision record**: Add a DEV-NNN entry in the relevant plan or `docs/decisions.md` explaining
   the rationale (e.g., "policy_violation demoted to Tier 1 because per-SIM volume is too high at
   scale > 500k SIMs").
2. **Update `internal/api/events/tiers.go`**: Change the tier value in the lookup map.
3. **Update `internal/api/events/catalog.go`**: Update the `tier` field on the catalog entry.
4. **Update this document**: Bullet the event under its new tier; remove it from the old tier.
5. **Add a regression test**: In the notification service test suite, assert that the reclassified
   event type follows the new routing behaviour (e.g., no notification row for Tier 1).
6. **Confirm publisher coverage**: Every Tier 3 entry MUST have an existing publisher. Do not add
   preference toggles for events that have no publisher — empty toggles create misleading UI.

---

## Cross-References

- `docs/architecture/WEBSOCKET_EVENTS.md` — FIX-212 envelope spec (`bus.Envelope` wire format,
  per-subject `meta` schemas, name resolution strategy, WS connection protocol).
- `internal/api/events/tiers.go` — Tier lookup source of truth. One `map[string]string` keyed
  by canonical event type returning `"internal"`, `"digest"`, or `"operational"`.
- `internal/notification/service.go` — Tier guard implementation; preference lookup; render and
  dispatch pipeline.
- `internal/analytics/digest/worker.go` — Digest emitter; 15-min cron; threshold evaluation and
  severity scaling for all four Tier 2 events.

# Implementation Plan: FIX-212 — Unified Event Envelope + Name Resolution + Missing Publishers

**Story:** `docs/stories/fix-ui-review/FIX-212-unified-event-envelope.md`
**Track / Wave:** UI Review Remediation, Wave 3 (P1 Event System + Dashboard Metrics)
**Effort:** XL
**Depends on:** FIX-209 (DONE — tolerant persist), FIX-210 (DONE — `dedup_key` + `alertstate` pkg), FIX-211 (DONE — canonical severity), FIX-202 (DONE — operator name resolution precedent), FIX-206 (DONE — FK constraints)
**Mode:** AUTOPILOT (full UI Review Remediation track)

---

## Decisions (read first — resolve story-vs-dispatch conflicts before tasks)

These lock down ambiguities in the story AC text and the pre-dispatch note. Every downstream task references these.

| # | Conflict / Open question | Decision | Rationale |
|---|---|---|---|
| D1 | JSON field naming — camelCase vs snake_case | **snake_case** throughout (matches every existing `bus.Subject*` payload and `ws.EventEnvelope{type,id,timestamp,data}`). Go struct fields keep PascalCase; `json:"…"` tags are snake_case. | Zero migration cost for FE (already parses snake_case across every subject); changing convention on the same commit as envelope migration risks invisible regressions. |
| D2 | Name resolution — publisher-side (eager) vs subscriber-side (lazy) | **Hybrid — publisher-side, but gated by hot-path class.** Session publishers (`radius/server.go`, `diameter/gx.go`, `diameter/gy.go`, `sba/ausf.go`, `sba/udm.go`) embed **ICCID only** (already in scope at the publish boundary via `session.sim_id → store.SIM.ICCID` — already cached in-memory by the AAA pipeline). Operator name + APN name for session subjects come from a **Redis-backed resolver (`internal/events/resolver.go`, NEW)** with 10-minute TTL. All NON-session publishers (alert, operator health, policy, sim, anomaly) do full triple resolution synchronously — they fire O(1-10)/sec, not O(10k)/sec. | AC-6 requires publisher-side resolution; a naive synchronous resolve on the AAA hot path (10k+ publishes/sec at fleet scale) would wreck p99 session-start latency. Hybrid preserves AC-6 (FE sees names, not UUIDs) without breaking the AAA SLO. Cache invalidation: operator/APN rename events already exist (FIX-202) and can invalidate by key. |
| D3 | Versioning — envelope version field + grace window for AC-8 | **`"event_version": 1` mandatory field. Consumers (ws/hub, notification subscriber) branch on presence:** missing/≠1 → treat as legacy shape, pass through with `argus_events_legacy_shape_total{subject}` metric increment + WARN log (sampled 1/100). Legacy shim lives for 1 release; removal tracked as new D-NNN opened by this story. | AC-8 mandates backward shim. Per-subject metric lets us verify every publisher migrated before removing the shim in the follow-up story. Sampling prevents log-flood if a deferred publisher (see D6) fires frequently. |
| D4 | `dedup_key` — who computes it? | **Publisher may OPTIONALLY override, default compute stays in `parseAlertPayload` via `alertstate.DedupKey`.** Envelope has `dedup_key` as `*string` (pointer; nil = "subscriber computes via canonical formula"; non-nil = "publisher authored, use as-is"). No publisher in FIX-212 sets it — the field is reserved for future stories where a publisher knows a stronger canonical key (e.g. `operator_health` already has `op:<id>|<state>`). | Preserves FIX-210's PAT-006 defense: *"Only parseAlertPayload computes the key (single compute point)"*. Moving compute to every publisher would reintroduce the exact bug FIX-210 closed. Optional override gives future flexibility without changing today's single-writer invariant. |
| D5 | D-075 closure — can `systemTenantID` sentinel actually be removed? | **Yes, with ONE documented exception: `bus/consumer_lag.go` (NATS consumer lag is infra-global, not tenant-scoped).** For consumer_lag, the publisher itself writes the sentinel `systemTenantID` into the envelope — the value lives at the publisher, not the subscriber fallback. `parseAlertPayload`'s `systemTenantID` variable is DELETED; the tolerant `alertEventFlexible` struct is REPLACED by strict `bus.Envelope` Unmarshal. Story AC-1 thus closes D-075 as "sentinel now publisher-authored where infra-global; removed from subscriber fallback." | The sentinel was always an infra-scope workaround; placing it at the single infra-global publisher (consumer_lag) makes scope explicit and allows the subscriber to enforce `tenant_id` mandatory. Alternative (per-tenant fanout of consumer_lag) is deferred D-NNN — not worth a tenant-loop for an infra metric. |
| D6 | Scope — which subjects migrate in FIX-212 | **IN-SCOPE (14 subjects, 8 subject-clusters):** `SubjectAlertTriggered`, `SubjectSessionStarted`, `SubjectSessionUpdated`, `SubjectSessionEnded`, `SubjectSIMUpdated` (NEW publisher per AC-3), `SubjectPolicyChanged`, `SubjectOperatorHealthChanged`, `SubjectAnomalyDetected`, `SubjectNotification`, `SubjectPolicyRolloutProgress`, `SubjectIPReclaimed`, `SubjectIPReleased`, `SubjectSLAReportGenerated`, `SubjectAuthAttempt`. **DEFERRED to D-077 tech debt (not in-scope):** `SubjectJobQueue`, `SubjectJobCompleted`, `SubjectJobProgress`, `SubjectCacheInvalidate`, `SubjectBackupCompleted`, `SubjectBackupVerified`, `SubjectAuditCreate`. | XL ≠ "all 40+ publish sites." Story AC-2 lists UI + notification-consumed subjects ("etc."). Job/cache/backup/audit subjects are internal plumbing (not UI-surfaced) and don't feed the Event Stream or notifications. Gate on deferred set: no new envelope-migration there; keeps diff reviewable. |
| D7 | Event catalog endpoint scope (AC-5) | **`GET /api/v1/events/catalog` is API-only in FIX-212; zero FE consumer in this story.** Registered on the router, audited, but NO dashboard/settings page calls it until FIX-240 (Notification Preferences). PAT-015 mitigation: catalog endpoint has an integration test verifying JSON shape; FE `web/src/types/events.ts` is populated from the catalog shape but the catalog URL is not fetched. | FIX-240 is downstream; shipping a dead FE mount today creates PAT-015 repeat. Catalog endpoint is the canonical reference source (referenced by FIX-234 Settings Notifications and FIX-240 Notification Preferences stories). |
| D8 | `EntityRef` cardinality — 1 entity or N | **Exactly 1** `Entity *EntityRef` at the envelope level (per AC-1). Subjects with multiple entities (e.g. operator switch has old_op + new_op) put the **primary** in `Entity` and additional entities in `meta` with `entity_*` keyed fields. | AC-1 specifies singular `Entity *EntityRef`. A `[]EntityRef` would complicate FE Event Stream rendering (need to pick one as the row anchor anyway). Primary-entity convention documented in `bus/envelope.go`. |
| D9 | Missing publishers inventory (beyond `sim.updated`) | **ONLY `sim.updated` is added as NEW publisher in FIX-212** (per AC-3, unblocks policy matcher F-119). The story text mentions F-301 "publisher coverage matrix" generically; audit-log replacement of direct `inApp.CreateNotification` calls for `heartbeat_ok`/`user_login` is AC-4 option (b) in this plan: **keep as internal (not surfaced as user-facing notifications), mark with inline `// internal: not a user-notification` comment.** No new publishers beyond `sim.updated`. | Option (b) in AC-4 avoids adding 2 more publishers to this already-XL story; FIX-217 (notification cleanup) handles internal-vs-user notification separation. Scope discipline. |

---

## Problem Context — Publisher Inventory & Payload Drift (Verified)

### Current state — what FIX-209 tolerates and what FIX-212 normalizes

Post-FIX-210, `internal/notification/service.go` uses `alertEventFlexible` + `parseAlertPayload` + `publisherSourceMap` + `synthesizeTitle` to tolerate heterogeneous shapes on `argus.events.alert.triggered` only. Every OTHER subject has its own shape, parsed by its own consumer — no central tolerance, no shared struct. This plan makes every in-scope subject emit a `bus.Envelope` and every consumer parse the same type.

### Publisher Inventory — 14 IN-SCOPE subjects across 22 call sites

| Subject | Publisher file:line | Current payload shape | `tenant_id`? | Entity embedded? | Name embedded? | After FIX-212 envelope |
|---|---|---|---|---|---|---|
| `alert.triggered` | `operator/health.go:520-529` | `operator.AlertEvent` struct | no (D-075) | `EntityID=opID` (UUID) | `meta.operator_name` | `Envelope{type:"operator_down", tenant_id, entity:{type:"operator", id, display_name}, severity, title, description, meta, event_version:1}` |
| `alert.triggered` | `analytics/anomaly/engine.go:200-214` | `map[string]interface{}` | yes | `entity_type="anomaly"`, `entity_id` | no | same → `entity:{type:"sim", id=sim_id, display_name="ICCID …"}`; `meta.anomaly_id` linkage |
| `alert.triggered` | `bus/consumer_lag.go:186-191` | `lagAlert` struct (severity, source, consumer, pending) | no | no | no | same, `tenant_id=system` (D5); `entity:{type:"consumer", id=consumer, display_name=consumer}`; title synthesized |
| `alert.triggered` | `job/storage_monitor.go:170-177` | `map[string]interface{}`, `tenant_id=nil` | nil (explicit) | no (`entity_type="system"`) | no | same; `tenant_id=system` per-publisher (D5); `entity:{type:"system", id="storage", display_name="Storage"}` |
| `alert.triggered` | `job/anomaly_batch_supervisor.go:97-103` | `map[string]interface{}` | no | no | no | same; `entity:{type:"job", id=job_id}`; title synthesized |
| `alert.triggered` | `job/roaming_renewal.go:98-123` | `notification.AlertPayload` + `json.RawMessage` | yes | `EntityID=agreementID` | `metadata.partner_operator_name` | same → `entity:{type:"agreement", id, display_name=partner_operator_name}` |
| `alert.triggered` | `policy/enforcer/enforcer.go:240-253` | `map[string]interface{}` (uses `message` not `title`) | yes | `entity_type="sim"`, `entity_id=sim.id` | no | same → `entity:{type:"sim", id, display_name=ICCID}`; `title ← message`; `meta.policy_violation_id`, `meta.policy_id` |
| `session.started` | `radius/server.go:892`, `diameter/gx.go:174`, `diameter/gy.go:150`, `sba/ausf.go:227` | `map[string]interface{}` (diverges per protocol) | yes | `sim_id` at top-level | no (**ICCID NOT embedded in any**) | `Envelope{type:"session.started", tenant_id, entity:{type:"sim", id, display_name="ICCID <iccid>"}, severity="info", title="Session started", meta:{operator_id, apn_id, framed_ip, rat_type, …}, event_version:1}` — ICCID resolved at publisher (already in SIM context); operator/APN name via resolver (D2) |
| `session.updated` | `diameter/gx.go:297`, `diameter/gy.go:211`, `sba/udm.go:111`, `radius/server.go:931` | `map[string]interface{}` | varies | `sim_id` | no | same envelope shape as session.started |
| `session.ended` | `aaa/session/sweep.go:230`, `api/session/handler.go:575`, `radius/server.go:801,965,1019`, `diameter/gx.go:357`, `diameter/gy.go:279`, `sba/ausf.go:183`, `job/bulk_disconnect.go:115` | `map[string]interface{}` (8 sites, 8 shapes) | varies | `sim_id`, `session_id` | no | same; `meta.termination_cause`, `meta.bytes_in/out`; session UUID in `meta.session_id` |
| `sim.updated` | **NO PUBLISHER TODAY (F-119)** | — | — | — | — | **NEW publisher** at `api/sim/handler.go` for Activate/Suspend/Resume/Terminate/ReportLost/Patch + `bulk_state_change.go` outcome path. `Envelope{type:"sim.state_changed", tenant_id, entity:{type:"sim", id, display_name=ICCID}, severity="info", title, meta:{old_state, new_state, operator_id, apn_id, policy_version_id}, event_version:1}` |
| `policy.changed` | (no current publisher — policy CRUD in `api/policy/handler.go` does NOT emit) | — | — | — | — | AC-2 "etc." — **add publisher** in policy CRUD (Create/Update/Delete) emitting `Envelope{type:"policy.updated", tenant_id, entity:{type:"policy", id, display_name=name}, severity="info", meta:{version, change_summary}}` |
| `operator.health` | `operator/health.go:453` | `OperatorHealthEvent` struct (has `OperatorName` field already) | no (D-075) | no | yes (`operator_name`) | `Envelope{type:"operator.health_changed", tenant_id, entity:{type:"operator", id, display_name}, severity, title="Operator <name> <previous>→<current>", meta:{previous_status, current_status, circuit_state, latency_ms}, event_version:1}` |
| `anomaly.detected` | `analytics/anomaly/engine.go:194`, `batch.go:165` | `map[string]interface{}` | yes | `sim_id`, `anomaly_id` | no | `Envelope{type:"anomaly.detected", tenant_id, entity:{type:"sim", id=sim_id, display_name=ICCID}, severity, title, meta:{anomaly_type, score, details}, event_version:1}` |
| `notification.dispatch` | `notification/service.go:439`, `policy/enforcer/enforcer.go:333`, `job/sms_gateway.go:144`, `job/webhook_retry.go:201`, `job/data_portability.go:221`, `job/scheduled_report.go:189` | `map[string]interface{}` (6 sites, 6 shapes) | varies | varies | varies | `Envelope{type:"notification.<kind>", tenant_id, entity=nil (notification is entity-less), severity, title, meta:{channels_sent, event_type, …}}` |
| `policy.rollout_progress` | `policy/rollout/service.go:475` | `rollout.event` struct | yes | `policy_id`, `rollout_id` | no | `Envelope{type:"policy.rollout_progress", tenant_id, entity:{type:"policy", id=policy_id, display_name}, severity="info", meta:{rollout_id, completed_count, total_count, …}}` |
| `ip.reclaimed` | `job/ip_reclaim.go:123` | `map[string]any` | yes | `ip, operator_id` | no | `Envelope{type:"ip.reclaimed", tenant_id, entity:{type:"ip", id=ip, display_name=ip}, severity="info", meta:{operator_id, pool_id, reclaim_reason}}` |
| `ip.released` | `job/ip_grace_release.go:94` | `map[string]any` | yes | `ip, sim_id` | no | `Envelope{type:"ip.released", tenant_id, entity:{type:"sim", id=sim_id, display_name=ICCID}, severity="info", meta:{ip, reason}}` |
| `sla.report.generated` | `job/sla_report.go:164` | `map[string]any` | yes | `report_id, operator_id` | no | `Envelope{type:"sla.report.generated", tenant_id, entity:{type:"operator", id=operator_id, display_name}, severity="info", meta:{report_id, period_start, period_end, …}}` |
| `auth.attempt` | (subject defined, publisher TBD — check if exists) | — | — | — | — | if publisher exists: migrate; if zero publishers exist: deferred to D-NNN (out of scope for FIX-212 per D6 scope discipline). |

**Publisher site count (in-scope):** 22 publish call sites across 14 subjects × ~20 files modified. Deferred (D6): ~30+ publish sites on 7 subjects left unchanged.

### What the CONSUMERS look like today (must adapt)

| Consumer | File:Line | Current parse | After FIX-212 |
|---|---|---|---|
| Notification subscriber (alerts) | `internal/notification/service.go:684` `parseAlertPayload` | Tolerant `alertEventFlexible` Unmarshal + field synthesis + `systemTenantID` fallback | **Strict** `json.Unmarshal(data, &bus.Envelope{})` + `envelope.Validate()` (severity, source, type non-empty); legacy-shape path for `event_version != 1` via backward shim calling the OLD `parseAlertPayload`. `systemTenantID` variable + `alertEventFlexible` struct DELETED. |
| Notification subscriber (notifications) | `internal/notification/service.go` notification.dispatch handler | ad-hoc parse | `bus.Envelope` strict parse |
| WS hub relay | `internal/ws/hub.go:208` `relayNATSEvent` | `json.Unmarshal(data, &payload map[string]interface{})` then `extractTenantID(payload)` | Unmarshal into `bus.Envelope`; `envelope.TenantID` authoritative; `payload` bag passes through as `envelope` to the browser (wrapped in `ws.EventEnvelope.Data`). Legacy-shape fallback (version missing) still works via map path with WARN metric. |
| Policy matcher | `internal/policy/matcher.go:40` `handleSIMUpdated` | NONE today (no publisher exists) | `bus.Envelope` strict parse — after Task 4 ships the publisher, this consumer works end-to-end (F-119 close) |
| Event Stream FE | `web/src/pages/events/` (if exists) or dashboard live feed | parses WS `ws.EventEnvelope.Data` as `Record<string, unknown>` | FE `types/events.ts` adds `BusEnvelope` type matching Go struct; FE extracts `entity.display_name` for row label; falls back to `entity.id` if absent (legacy shape) |

### Out of Scope (do NOT touch)

- `SubjectJobQueue`, `SubjectJobCompleted`, `SubjectJobProgress` — job-subject envelope migration deferred to D-077 (not UI-surfaced)
- `SubjectCacheInvalidate`, `SubjectBackupCompleted`, `SubjectBackupVerified`, `SubjectAuditCreate` — internal plumbing, deferred D-077
- **Legacy raw-map job publishers (deferred to D-079)** — these 38 call sites stay on `map[string]interface{}` payloads and drain via `argus_events_legacy_shape_total`. Explicit list: `internal/job/{s3_archival,webhook_retry,sms_gateway,backup,runner,bulk_state_change (non-sim.updated path),backup_verify,import,timeout,scheduled_report,sla_report (job-progress path only — the sla.report.generated publish IS migrated),bulk_esim_switch,data_portability,ip_grace_release (job-progress path only),bulk_policy_assign,storage_monitor (job-progress only — the alert publish IS migrated),bulk_disconnect (job-progress path only),alerts_retention,anomaly_aggregate}.go`, plus `internal/api/{cdr,esim,session}/handler.go` job-progress publishes. Any `.Publish(..., map[string]interface{}{...})` remaining after FIX-212 is permitted iff it is one of these sites. Migrating them is scope creep for FIX-212 (XL already).
- Notification Preferences (FIX-240) FE consumer of catalog — FIX-240 scope
- FE Event Stream UI polish — FIX-213 scope
- Per-tenant fanout of `bus/consumer_lag` — keep system-tenant sentinel at publisher (D5)
- Auth attempt subject publishers (if any) — survey in Task 5 and defer to D-NNN if not trivially migratable
- `handleAlert` notification dispatch ordering — unchanged (FIX-209 `handleAlertPersist` ordering preserved)
- Severity enum, `alerts` table schema, dedup state machine — FIX-211/209/210 territory, not re-touched

---

## Canonical Event Envelope (Authoritative — the single source of truth this story delivers)

### Go struct (`internal/bus/envelope.go` — NEW)

```go
package bus

import (
    "fmt"
    "time"

    "github.com/google/uuid"
)

// CurrentEventVersion is the envelope schema version shipped with FIX-212.
// Consumers that see a different value (or missing field) invoke their legacy-shape shim.
const CurrentEventVersion = 1

// Envelope is the canonical schema for every argus NATS event in the FIX-212
// in-scope subject set (see FIX-212 plan §Scope). Publishers construct via
// NewEnvelope(...) and MUST set Type, TenantID, Severity. Consumers strict-parse
// and call Validate(); on validation failure, the consumer's legacy-shape shim
// handles the event and increments argus_events_legacy_shape_total{subject}.
type Envelope struct {
    EventVersion int                    `json:"event_version"`
    ID           string                 `json:"id"`
    Type         string                 `json:"type"`
    Timestamp    time.Time              `json:"timestamp"`
    TenantID     string                 `json:"tenant_id"`
    Severity     string                 `json:"severity"`
    Source       string                 `json:"source"`
    Title        string                 `json:"title"`
    Message      string                 `json:"message,omitempty"`
    Entity       *EntityRef             `json:"entity,omitempty"`
    DedupKey     *string                `json:"dedup_key,omitempty"`
    Meta         map[string]interface{} `json:"meta,omitempty"`
}

type EntityRef struct {
    Type        string `json:"type"`
    ID          string `json:"id"`
    DisplayName string `json:"display_name,omitempty"`
}

// Validate enforces mandatory fields. Returns nil on valid envelope.
// Caller increments argus_events_invalid_total{subject,reason} on error.
func (e *Envelope) Validate() error { /* body in Task 1 */ }

// NewEnvelope constructs a new envelope with EventVersion=CurrentEventVersion,
// fresh UUID, and Timestamp=now.UTC(). Callers set remaining fields.
func NewEnvelope(evtType, tenantID, severity string) *Envelope { /* body in Task 1 */ }
```

### JSON shape (wire format — snake_case per D1)

```json
{
  "event_version": 1,
  "id": "f47ac10b-58cc-4372-a567-0e02b2c3d479",
  "type": "session.started",
  "timestamp": "2026-04-21T14:23:45.123Z",
  "tenant_id": "00000000-0000-0000-0000-000000000001",
  "severity": "info",
  "source": "aaa",
  "title": "Session started",
  "message": "RADIUS session established on operator turkcell",
  "entity": {
    "type": "sim",
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "display_name": "ICCID 8990011234567890123"
  },
  "meta": {
    "operator_id": "…",
    "operator_name": "turkcell",
    "apn_id": "…",
    "apn_name": "iot.argus",
    "framed_ip": "10.20.30.40",
    "rat_type": "LTE",
    "nas_ip": "192.0.2.1"
  }
}
```

### Mandatory vs optional fields

| Field | Required | Validate rule | Fallback on parse |
|---|---|---|---|
| `event_version` | YES | `== 1` (CurrentEventVersion). If missing or `!= 1` → legacy shim. | — |
| `id` | YES | Non-empty string (UUID recommended but not enforced). | — |
| `type` | YES | Non-empty; recommended format `<domain>.<action>` (e.g. `session.started`). No CHECK. | — |
| `timestamp` | YES | Parseable RFC3339; not zero. | `time.Now().UTC()` in NewEnvelope |
| `tenant_id` | YES | Parseable UUID. **D-075 closure: no fallback in subscriber.** | — (strict) |
| `severity` | YES | `severity.Validate(sev) == nil` (canonical 5 from FIX-211). | Invalid → Validate() error → legacy shim |
| `source` | YES | Non-empty; recommended one of `sim`, `operator`, `infra`, `policy`, `system`, `aaa`, `analytics`, `notification`, `job` (no CHECK). | — |
| `title` | YES | Non-empty. | — |
| `message` | no | Long-form description. | empty OK |
| `entity` | no | If present, Type+ID non-empty. | nil OK (e.g. notification.dispatch) |
| `dedup_key` | no | If set, `len(*DedupKey) ≤ 255`. See D4. | nil OK |
| `meta` | no | Map. | `{}` OK |

### Source enum (ADVISORY not CHECK — allows future growth)

```
sim | operator | infra | policy | system | aaa | analytics | notification | job
```

Alert-scope alerts MUST use one of `sim|operator|infra|policy|system` (same as `chk_alerts_source` CHECK in FIX-209). Other subjects use their publishing domain (`aaa`, `analytics`, `job`, `notification`). The envelope itself does NOT CHECK `source` — that enforcement lives in `parseAlertPayload` after FIX-212 (called through `envelope.Validate()` for `alert.triggered` specifically).

---

## Name Resolution Strategy (D2 — hybrid hot-path-safe)

### Resolver package (`internal/events/resolver.go` — NEW)

```go
package events

// Resolver embeds display_name into envelopes at publish time. Backed by a
// Redis LRU cache with 10-minute TTL per entity kind. Cache misses hit the
// store synchronously (acceptable for non-AAA publishers at O(1-10)/sec).
// For AAA publishers (session.*), callers MUST NOT invoke ResolveOperator/
// ResolveAPN — they use pre-resolved names passed from the AAA pipeline's
// in-memory operator/apn lookup tables. Only ResolveICCID is safe on the
// hot path (SIM is already loaded by the AAA request context).
type Resolver interface {
    ResolveICCID(ctx context.Context, simID uuid.UUID) (string, error)        // used by AAA hot path via SIM context
    ResolveOperator(ctx context.Context, operatorID uuid.UUID) (string, error) // DB/Redis — NOT hot-path
    ResolveAPN(ctx context.Context, apnID uuid.UUID) (string, error)           // DB/Redis — NOT hot-path
}
```

### Wiring per publisher cluster

| Publisher cluster | Hot path? | Resolver usage |
|---|---|---|
| **Session (`radius/*`, `diameter/gx.go`, `diameter/gy.go`, `sba/*`, `session/sweep.go`, `api/session/handler.go`, `job/bulk_disconnect.go`)** | YES (10k+/sec at scale) | ICCID embedded directly from SIM context already loaded by the AAA request. operator/apn names embedded as empty string if not pre-resolved at the publisher (resolver subscriber path handles enrichment later for FE — but this is deferred to D-NNN; FIX-212 accepts empty display_name for operator/apn inside session meta). |
| **Alert (`operator/health.go`, `analytics/anomaly/*`, `bus/consumer_lag.go`, `job/storage_monitor.go`, `job/anomaly_batch_supervisor.go`, `job/roaming_renewal.go`, `policy/enforcer/enforcer.go`)** | no (O(1-10)/sec) | Synchronous `resolver.Resolve*` calls; cached in Redis (10m TTL); DB fallback. PAT-011 note: every alert-publisher constructor gains `WithResolver(resolver)` option in `cmd/argus/main.go`. |
| **Operator health** | already has `OperatorName` in event today | Pass-through via existing `OperatorName` field; no resolver call needed. |
| **SIM (`api/sim/handler.go`, `job/bulk_state_change.go`)** | no | Resolver for ICCID (SIM UUID → ICCID) |
| **Policy (`api/policy/handler.go`, `policy/rollout/service.go`)** | no | Resolver for policy name (add `ResolvePolicy` — task 7 optional; fallback to empty display_name if not implemented in FIX-212 scope) |
| **Anomaly** | no | Resolver for SIM ICCID |
| **IP (`job/ip_reclaim.go`, `job/ip_grace_release.go`)** | no | SIM ICCID if sim_id present; otherwise entity=`{type:"ip", id:ip, display_name:ip}` |

### Cache invalidation

Resolver subscribes to existing FIX-202 operator-name cache invalidation channel (`argus.cache.invalidate` for `operator:<id>` and `apn:<id>` keys). On invalidation → `DEL` the resolver's Redis key. No new invalidation subjects needed.

### Resolver Redis key schema

```
argus:resolve:operator:<uuid> → string (operator.name) — TTL 600s
argus:resolve:apn:<uuid>      → string (apn.name)      — TTL 600s
argus:resolve:sim:<uuid>      → string (sim.iccid)     — TTL 600s
```

### Fallback behavior

If resolver returns error (Redis down, DB error, not found), publisher emits `display_name=""` and continues. Alert is persisted with empty display_name — FE falls back to `entity.id` rendering. `argus_events_resolver_miss_total{kind,reason}` metric fires.

---

## Architecture Context

### Components Involved

| Component | Layer | File(s) | Role |
|---|---|---|---|
| Envelope struct + validator | Go shared | `internal/bus/envelope.go` (NEW), `internal/bus/envelope_test.go` (NEW) | Authoritative envelope type; `Validate()`, `NewEnvelope()` constructor, unit tests. |
| Name resolver | Go shared | `internal/events/resolver.go` (NEW), `internal/events/resolver_test.go` (NEW) | Redis+DB-backed LRU resolver for ICCID/operator-name/apn-name. |
| Event catalog handler | Go API | `internal/api/events/catalog_handler.go` (NEW), `internal/api/events/catalog_handler_test.go` (NEW), `internal/gateway/router.go` (modify: route registration) | `GET /api/v1/events/catalog` returns static catalog (event type → default severity + meta schema). |
| Alert publishers migrate | Go | `internal/operator/health.go`, `internal/analytics/anomaly/engine.go`, `internal/analytics/anomaly/batch.go`, `internal/bus/consumer_lag.go`, `internal/job/storage_monitor.go`, `internal/job/anomaly_batch_supervisor.go`, `internal/job/roaming_renewal.go`, `internal/policy/enforcer/enforcer.go` | Each publishes `bus.Envelope` instead of ad-hoc map/struct. |
| Session publishers migrate | Go | `internal/aaa/radius/server.go`, `internal/aaa/diameter/gx.go`, `internal/aaa/diameter/gy.go`, `internal/aaa/sba/ausf.go`, `internal/aaa/sba/udm.go`, `internal/aaa/session/sweep.go`, `internal/api/session/handler.go`, `internal/job/bulk_disconnect.go` | ICCID embedded via SIM context; operator/apn names empty (session-hot-path SLO per D2). |
| SIM publisher (NEW) | Go | `internal/api/sim/handler.go` (add publishSimUpdated helper + call sites in Activate/Suspend/Resume/Terminate/ReportLost/Patch), `internal/job/bulk_state_change.go` (outcome publisher) | Closes F-119 (policy matcher unblock). |
| Other publishers migrate | Go | `internal/api/policy/handler.go` (add publisher for policy.updated), `internal/policy/rollout/service.go`, `internal/job/ip_reclaim.go`, `internal/job/ip_grace_release.go`, `internal/job/sla_report.go`, `internal/notification/service.go` (notification.dispatch publisher), `internal/job/sms_gateway.go`, `internal/job/webhook_retry.go`, `internal/job/data_portability.go`, `internal/job/scheduled_report.go`, `internal/policy/enforcer/enforcer.go` (notification path) | Same envelope construction. |
| Alert subscriber (strict) | Go | `internal/notification/service.go` — `parseAlertPayload` REPLACED; `alertEventFlexible` + `systemTenantID` DELETED; legacy shim added | Strict `bus.Envelope` parse + `envelope.Validate()`; legacy-shape path on version mismatch calls old parser + WARN + metric. |
| WS hub | Go | `internal/ws/hub.go` `relayNATSEvent` | Parse envelope first; fallback to old `map[string]interface{}` on unmarshal fail. |
| FE types | TS | `web/src/types/events.ts` (NEW) | `BusEnvelope`, `EntityRef`, `EventVersion` types matching Go struct. |
| Docs | Markdown | `docs/architecture/WEBSOCKET_EVENTS.md` (modify) | Embed envelope schema + per-subject example payloads. |
| Wiring | Go | `cmd/argus/main.go` | Resolver construction + injection into every publisher constructor; catalog handler wiring. |
| Tech Debt close | Markdown | `docs/ROUTEMAP.md` | Mark D-075 RESOLVED; open D-077 for deferred subjects per D6. |

### Data Flow

```
PUBLISHERS (in-scope subjects — 22 sites, now envelope-based)
      │
      ▼  bus.EventBus.Publish(ctx, subject, *bus.Envelope)
      │  (EventBus serializes Envelope to JSON per existing Publish signature)
      │
      ├─► ws.Hub.relayNATSEvent(subject, data):
      │      json.Unmarshal(data, &bus.Envelope)  — strict
      │      if err OR envelope_version != 1 → fallback path:
      │          json.Unmarshal(data, &map[string]interface{})
      │          metric: argus_events_legacy_shape_total{subject}
      │      extract tenant_id from envelope.TenantID (authoritative)
      │      BroadcastToTenant(tenantID, eventType, envelope-as-map)
      │
      └─► notification.Service.handleAlertPersist (subject=alert.triggered):
             json.Unmarshal(data, &bus.Envelope)  — strict
             if err OR envelope_version != 1 → legacy shim:
                 OLD parseAlertPayload (tolerant) — logged, metric fires
             else:
                 envelope.Validate()
                 params := alertParamsFromEnvelope(envelope) [NEW]
                 alertStore.UpsertWithDedup(params) — FIX-210 path unchanged
                 dispatch — unchanged

NAME RESOLUTION (publisher-side, D2 hybrid)
  Session publishers (hot-path):
       sim := ctx.SIM (already loaded); entity.display_name = sim.ICCID
       meta.operator_name = ""  // resolver subscriber enrichment deferred D-NNN
  Alert publishers (cold-path):
       resolver.ResolveOperator(ctx, opID) → Redis GET → DB fallback → cache SET
       envelope.Entity.DisplayName = resolved name

D-075 CLOSURE (consumer-side):
  Before FIX-212: parseAlertPayload falls back to systemTenantID on missing tenant_id
  After FIX-212:  parseAlertPayload is REPLACED; envelope.Validate() returns error on
                  missing tenant_id → legacy shim handles (logs + metric); publishers
                  at bus/consumer_lag.go SET tenant_id to infra sentinel THEMSELVES.
                  systemTenantID variable DELETED from notification/service.go.
```

### API Specifications

**`GET /api/v1/events/catalog`** — event type catalog (D7: API-only in FIX-212)

- Auth: standard auth middleware; tenant-scoped read-only
- Request: no body, no query params
- Response 200:
  ```json
  {
    "status": "success",
    "data": {
      "events": [
        {
          "type": "session.started",
          "source": "aaa",
          "default_severity": "info",
          "entity_type": "sim",
          "description": "A new AAA session was established for a SIM.",
          "meta_schema": { "operator_id": "uuid", "apn_id": "uuid", "framed_ip": "string", "rat_type": "string" }
        },
        { "type": "operator_down", "source": "operator", "default_severity": "critical", "entity_type": "operator", "description": "An operator went DOWN (circuit breaker opened).", "meta_schema": { "previous_status": "string", "circuit_state": "string" } },
        … (full 14-subject catalog, ~30 event types total)
      ]
    }
  }
  ```
- Error responses: 401 `AUTH_REQUIRED`, 500 `INTERNAL_ERROR`
- Implementation: static table constant in `internal/api/events/catalog.go`; handler just serializes. No DB query.

### Screen Mockups

FIX-212 is backend-heavy; the only visible FE change is that Event Stream rows (if dashboard has a live feed) now show `entity.display_name` instead of UUIDs. No new screens. No changes to SCREENS.md.

### Design Token Map

N/A — FIX-212 is backend-first. The optional FE `web/src/types/events.ts` type file does not introduce UI. FIX-213 (downstream story) polishes the Event Stream with tokens.

---

## Prerequisites

- [x] **FIX-202 DONE** — operator name resolution precedent (Redis cache + invalidation). Resolver in FIX-212 reuses this subscription.
- [x] **FIX-206 DONE** — FK constraints; resolver DB lookups guaranteed referentially intact.
- [x] **FIX-209 DONE** — `alerts` table, `handleAlertPersist`, tolerant persist. This story REPLACES the tolerant parser with strict envelope parse.
- [x] **FIX-210 DONE** — `dedup_key` + `alertstate` package + `UpsertWithDedup`. Envelope preserves dedup contract (D4).
- [x] **FIX-211 DONE** — canonical severity; envelope.Validate calls `severity.Validate`.
- [ ] **FIX-213** — follows FIX-212; consumes envelope-shaped payloads in Event Stream UI.
- [ ] **FIX-240** — follows FIX-212; consumes `/events/catalog`.
- [ ] **D-077 (NEW, opened by this story)** — remaining job/cache/backup/audit subjects envelope migration; separate story, post-release.

---

## Tasks

### Task 1: Envelope foundation — `bus.Envelope`, `EntityRef`, `Validate`, catalog constants

- **Files:**
  - Create `internal/bus/envelope.go`
  - Create `internal/bus/envelope_test.go`
  - Create `internal/api/events/catalog.go` (static catalog constants)
- **Depends on:** — (foundation)
- **Complexity:** **high**
- **Pattern ref:** Read `internal/severity/severity.go` for the single-validator-package pattern (exported `Validate()`, constants, typed fallback); read `internal/ws/hub.go:14-19` for the existing `EventEnvelope` naming convention (NOT reused — envelope types differ in scope); read `internal/operator/events.go` for the existing `AlertEvent` struct style.
- **Context refs:** "Canonical Event Envelope", "Decisions > D1,D3,D4,D8"
- **What:**
  - Define `CurrentEventVersion = 1` const.
  - Define `Envelope` struct with snake_case JSON tags (D1), fields per §Canonical.
  - Define `EntityRef{Type,ID,DisplayName}` struct.
  - Implement `Validate()` enforcing: `EventVersion == 1` (else `ErrLegacyShape`), `ID != ""`, `Type != ""`, `!Timestamp.IsZero()`, `TenantID` parseable UUID (via `uuid.Parse`), `severity.Validate(Severity) == nil`, `Source != ""`, `Title != ""`, `Entity != nil → Type+ID non-empty`, `DedupKey != nil → len ≤ 255`. Return typed errors `ErrLegacyShape`, `ErrInvalidSeverity`, `ErrInvalidTenant`, `ErrMissingField`.
  - Implement `NewEnvelope(evtType, tenantID, severity string) *Envelope`: allocates UUID, sets Timestamp=now.UTC(), EventVersion=1, Meta=`{}`. Returns pointer.
  - Implement `SetEntity(type, id string, displayName string) *Envelope` chainable builder method.
  - Implement `WithMeta(k string, v interface{}) *Envelope` chainable builder.
  - In `internal/api/events/catalog.go`: define `type CatalogEntry struct { Type, Source, DefaultSeverity, EntityType, Description string; MetaSchema map[string]string }` and a global `var Catalog = []CatalogEntry{…}` enumerating every in-scope event type (~30 entries — pull from §Publisher Inventory above).
- **Verify:**
  - `go build ./internal/bus/... ./internal/api/events/...` passes
  - `go test ./internal/bus/envelope_test.go` passes with tests:
    - `TestEnvelope_NewEnvelope_SetsVersionIDTimestamp`
    - `TestEnvelope_Validate_RejectsMissingFields` (table-driven: each mandatory field nil/empty)
    - `TestEnvelope_Validate_RejectsInvalidSeverity`
    - `TestEnvelope_Validate_RejectsInvalidTenantUUID`
    - `TestEnvelope_Validate_AcceptsLegacyVersionAsErrLegacyShape` (validates that `event_version=0` returns `ErrLegacyShape`, not `ErrMissingField`)
    - `TestEnvelope_RoundTrip_JSONMarshalUnmarshal` (snake_case tags preserved; entity nil-safe; meta preserved)
    - `TestEnvelope_SetEntity_Chainable`
  - `go test ./internal/api/events/...` asserts catalog has ≥14 subjects, every entry has non-empty `default_severity` validated by `severity.Validate`.

### Task 2: Name resolver (`internal/events/resolver.go`) + Redis-backed LRU

- **Files:**
  - Create `internal/events/resolver.go`
  - Create `internal/events/resolver_test.go`
- **Depends on:** — (independent; parallelizable with Task 1)
- **Complexity:** **high**
- **Pattern ref:** Read `internal/operator/cache.go` (if exists) or `internal/cache/` for the Redis TTL-key pattern used in FIX-202; read `internal/store/operator.go` `GetByID` for the DB fallback pattern. If FIX-202 operator-name-cache lives in a different path, follow its pattern verbatim — do NOT invent a new cache layer.
- **Context refs:** "Name Resolution Strategy > Resolver package", "Decisions > D2"
- **What:**
  - Define `Resolver` interface per §Name Resolution Strategy.
  - Define `redisResolver` struct with: `redis *redis.Client`, `simStore SimLookup` (tiny interface: `GetByID(ctx, tenantID, simID) (*store.SIM, error)`), `operatorStore`, `apnStore` (analogous).
  - `ResolveICCID(ctx, simID)`: Redis GET `argus:resolve:sim:<id>` → if miss, `simStore.GetByID(nil tenantID — cross-tenant; use system context)` → Redis SET with 600s TTL → return.
  - `ResolveOperator(ctx, opID)`: analogous; key `argus:resolve:operator:<id>`.
  - `ResolveAPN(ctx, apnID)`: analogous; key `argus:resolve:apn:<id>`.
  - Subscribe to `bus.SubjectCacheInvalidate` for keys `operator:<uuid>` and `apn:<uuid>` — DEL the corresponding resolver key. Wire in a `(r *redisResolver) Start(ctx, eventBus)` method called from `cmd/argus/main.go`.
  - Metrics: `argus_events_resolver_hit_total{kind}`, `argus_events_resolver_miss_total{kind,reason}`.
  - Constructor `NewRedisResolver(redis, simStore, opStore, apnStore, logger) *redisResolver`.
- **Verify:**
  - `go build ./internal/events/...` passes
  - `go test ./internal/events/resolver_test.go` passes with:
    - `TestResolver_ResolveOperator_CacheHit` (pre-seeded Redis; no DB call)
    - `TestResolver_ResolveOperator_CacheMiss_HitsDB` (mock DB returns name; cache is SET after)
    - `TestResolver_ResolveOperator_DBError_ReturnsEmpty` (graceful fallback; miss metric fires)
    - `TestResolver_InvalidateOnCacheMessage_ClearsKey`
    - `TestResolver_ResolveICCID_Happy`

### Task 3: Alert publishers migrate to `bus.Envelope` + strict subscriber (closes D-075)

- **Files:**
  - Modify `internal/operator/health.go` — `publishAlert` constructs `*bus.Envelope`; existing `AlertEvent` struct kept for internal types but not the wire format (rename to `internalAlertEvent` or inline into publisher)
  - Modify `internal/analytics/anomaly/engine.go:200-214` and `internal/analytics/anomaly/batch.go:165,199` — map → envelope
  - Modify `internal/bus/consumer_lag.go:186-199` — `lagAlert` struct → envelope; **tenant_id set to `systemTenantID` at publisher** (D5 closure path); entity `{type:"consumer", id:consumer, display_name:consumer}`
  - Modify `internal/job/storage_monitor.go:170-177` — envelope; tenant_id set to `systemTenantID` at publisher
  - Modify `internal/job/anomaly_batch_supervisor.go:97-103` — envelope; tenant_id set to `systemTenantID` at publisher
  - Modify `internal/job/roaming_renewal.go:98-123` — `notification.AlertPayload` → envelope
  - Modify `internal/policy/enforcer/enforcer.go:240-253` — map → envelope; `message → title`
  - Modify `internal/notification/service.go` — DELETE `alertEventFlexible` struct (lines ~653-680), DELETE `systemTenantID` variable (line ~33-39), DELETE `parseAlertPayload` tolerant impl, REPLACE with strict envelope path + legacy shim
  - Modify `internal/notification/service_test.go` — update tests for strict parse + legacy shim
  - Modify `cmd/argus/main.go` — inject `resolver` into every alert publisher's constructor (PAT-011 gate)
- **Depends on:** Task 1 (envelope), Task 2 (resolver)
- **Complexity:** **high**
- **Pattern ref:** Read `internal/operator/health.go:509-533` existing `publishAlert` for constructor style (callsite pattern matters more than the internal struct); read `internal/notification/service.go:877-xxx` `handleAlertPersist` for the persist-before-dispatch ordering — preserve it verbatim.
- **Context refs:** "Problem Context > Publisher Inventory", "Canonical Event Envelope", "Decisions > D3,D5", "Name Resolution Strategy"
- **What (per publisher):**
  - `operator/health.go::publishAlert` — allocate `env := bus.NewEnvelope(alertType, tenantID, severity)`; `env.Source = "operator"`; `env.Title = title`; `env.Message = description`; `env.SetEntity("operator", opID.String(), opName)`; `env.Meta["previous_status"] = previousStatus`; (tenantID: see note below — operator-scoped events need tenantID from operator metadata; for now, operator.health.checker holds tenantID from operator DB row — thread through, or use `systemTenantID` if operator has no tenant)
  - `bus/consumer_lag.go::emitAlert` — `env := bus.NewEnvelope("nats_consumer_lag", systemTenantID.String(), "high"); env.Source="infra"; env.Title=fmt.Sprintf("NATS consumer lag: %s has %d pending", consumer, pending); env.SetEntity("consumer", consumer, consumer); env.Meta["consumer"]=consumer; env.Meta["pending"]=pending`
  - `notification/service.go::handleAlertPersist` REWRITE:
    1. `var env bus.Envelope; if err := json.Unmarshal(data, &env); err != nil || env.EventVersion != bus.CurrentEventVersion { return s.handleAlertLegacy(data) }` (legacy shim: old tolerant parseAlertPayload logic preserved for 1 release per D3, renamed `parseAlertPayloadLegacy`, emits `argus_events_legacy_shape_total{subject=alert.triggered}`)
    2. `if err := env.Validate(); err != nil { s.logger.Warn()...; metric.Inc("invalid", err); return }` 
    3. `params := alertParamsFromEnvelope(&env)` (NEW helper — maps Envelope → `store.CreateAlertParams`; the `dedupKey = alertstate.DedupKey(...)` call happens HERE unless `env.DedupKey != nil` per D4)
    4. `s.alertStore.UpsertWithDedup(ctx, params, cooldownMinutes)` — FIX-210 path unchanged
    5. Dispatch as today
  - DELETE `systemTenantID` variable (line ~33-39). DELETE `alertEventFlexible` struct (lines ~653-680). DELETE tolerant `parseAlertPayload` — its body moves verbatim into `parseAlertPayloadLegacy` (unexported) as the fallback in handleAlertPersist for legacy shapes. This PRESERVES availability during the rollout (D3 grace window).
  - `cmd/argus/main.go`: each alert-publisher constructor (`health.NewHealthChecker`, `anomaly.NewEngine`, `roaming.NewRenewalJob`, `policy.NewEnforcer`, `jobs storage/anomaly_batch_supervisor`, `bus.NewConsumerLagPublisher`) gains `WithResolver(resolver)` option. PAT-011/PAT-017: every construction site enumerated + `grep` gated.
- **Verify:**
  - `go build ./...` passes
  - `go test ./internal/notification/... ./internal/operator/... ./internal/analytics/anomaly/... ./internal/bus/... ./internal/job/... ./internal/policy/enforcer/...` passes
  - **PAT-006 gate grep** (must match all 7 alert publishers + produce 0 maps):
    ```
    rg -n '\.Publish\(ctx, bus\.SubjectAlertTriggered' internal/ --type go
    ```
    → 7 hits. For each hit, grep `grep -A 3` expects `bus.NewEnvelope(` or a `*bus.Envelope` variable NOT a `map[string]interface{}` literal.
  - **D-075 closure verification:** `grep -n 'systemTenantID' internal/notification/` → **ZERO hits**.
  - **Legacy shim verification:** legacy `parseAlertPayloadLegacy` exists; `argus_events_legacy_shape_total` counter accepts tests.
  - New tests (add to `service_test.go`):
    - `TestHandleAlertPersist_StrictEnvelope_PersistsAllFields`
    - `TestHandleAlertPersist_LegacyShape_RoutesToShim_IncrementsMetric`
    - `TestHandleAlertPersist_MissingTenantID_FailsValidation_IncrementsMetric`
    - `TestHandleAlertPersist_InvalidSeverity_FailsValidation`
    - `TestHandleAlertPersist_DedupKeyFromEnvelope_RespectsPublisherOverride` (D4)
    - `TestConsumerLagPublisher_SetsSystemTenantID` (D5 closure at publisher side)
    - `TestStorageMonitor_SetsSystemTenantID` (D5)

### Task 4: Session publishers migrate + `sim.updated` NEW publisher

- **Files:**
  - Modify `internal/aaa/radius/server.go` (4 publish sites: 801, 892, 931, 965, 1019 — 5 sites) — envelope-based; ICCID embedded from SIM context
  - Modify `internal/aaa/diameter/gx.go` (3 sites: 174, 297, 357) — envelope; ICCID embedded
  - Modify `internal/aaa/diameter/gy.go` (3 sites: 150, 211, 279) — envelope; ICCID embedded
  - Modify `internal/aaa/sba/ausf.go` (2 sites: 183, 227) — envelope
  - Modify `internal/aaa/sba/udm.go` (1 site: 111) — envelope
  - Modify `internal/aaa/session/sweep.go` (1 site: 230) — envelope
  - Modify `internal/api/session/handler.go` (1 site: 575; also 550 job-queue stays legacy-map per D6 scope) — envelope
  - Modify `internal/job/bulk_disconnect.go` (1 site: 115) — envelope
  - Modify `internal/api/sim/handler.go` — ADD `publishSimUpdated(ctx, sim, oldState, newState)` helper + call sites in `Activate`, `Suspend`, `Resume`, `Terminate`, `ReportLost`, `Patch` (6 sites)
  - Modify `internal/job/bulk_state_change.go` — outcome publisher for sim.updated (1 site per-SIM, batched or per-SIM based on existing pattern)
  - Modify `internal/policy/matcher.go` — verify `handleSIMUpdated` parses envelope (strict) and logs + metric on legacy shape
  - Modify `cmd/argus/main.go` — wire SIM handler to receive `eventBus` + resolver; wire `publishSimUpdated` metric
- **Depends on:** Task 1 (envelope), Task 2 (resolver)
- **Complexity:** **high** (hot-path AAA publishers, 17 sites total, PAT-006 scope)
- **Pattern ref:** Read `internal/aaa/diameter/gx.go:174-190` for the current session.started publish shape; read `internal/api/sim/handler.go::Activate` (around line 857) for the state-change handler pattern; read `internal/analytics/aggregates/invalidator.go` for the `sim.updated` subscriber (confirm subscriber expects envelope after migration).
- **Context refs:** "Publisher Inventory", "Name Resolution Strategy > Wiring per publisher cluster", "Decisions > D2"
- **What:**
  - Session publishers: `env := bus.NewEnvelope(sessionEventType, tenantID, "info")`; `env.Source="aaa"`; `env.Title=...`; `env.SetEntity("sim", simID.String(), iccid)` (ICCID from SIM in-memory context — PAT-011 note: if `simCtx.ICCID` is not already on the struct in the AAA pipeline, wire it; if it is, pass through); `env.Meta["operator_id"]=operatorID; env.Meta["apn_id"]=apnID; env.Meta["framed_ip"]=framedIP; env.Meta["rat_type"]=ratType; env.Meta["nas_ip"]=nasIP;` **no** resolver call for operator/apn names (D2 hot-path); `operator_name`/`apn_name` meta keys set to "" or omitted.
  - `sim.updated` publisher: `publishSimUpdated(ctx, sim *store.SIM, oldState, newState string, extraMeta map[string]interface{})` in `api/sim/handler.go`:
    - `env := bus.NewEnvelope("sim.state_changed", sim.TenantID.String(), "info")`
    - `env.Source = "sim"`
    - `env.Title = fmt.Sprintf("SIM %s → %s", oldState, newState)`
    - `env.SetEntity("sim", sim.ID.String(), sim.ICCID)`  (ICCID in the loaded struct — no resolver call)
    - `env.Meta["old_state"] = oldState; env.Meta["new_state"] = newState; env.Meta["operator_id"] = sim.OperatorID; env.Meta["apn_id"] = sim.APNID; env.Meta["policy_version_id"] = sim.PolicyVersionID` (nullable — only set when non-nil)
    - merge `extraMeta`
    - `h.eventBus.Publish(ctx, bus.SubjectSIMUpdated, env)` — error logged, not returned (non-fatal to HTTP response — availability > durability precedent from FIX-209)
  - Call sites: `Activate` → `publishSimUpdated(ctx, sim, "inactive|suspended|terminated", "active", …)`; `Suspend`, `Resume`, `Terminate`, `ReportLost` — each call before the HTTP response is written (post-DB-commit); `Patch` — call only when state/APN/operator/policy actually changed (compute diff).
  - `bulk_state_change.go` — emit one envelope per successful SIM state change in the outcome loop (not per-batch, per-SIM — F-119 requires policy matcher to react to every SIM).
  - `policy/matcher.go::handleSIMUpdated` — strict envelope parse: `var env bus.Envelope; json.Unmarshal(data, &env); if env.EventVersion != 1 { m.logger.Warn("legacy sim.updated shape"); metric.Inc; return }`. Use `env.Entity.ID` as sim_id; `env.Meta["operator_id"]`, `env.Meta["apn_id"]` for matcher inputs.
- **Verify:**
  - `go build ./...` passes
  - `go test ./internal/aaa/... ./internal/api/sim/... ./internal/job/bulk_state_change_test.go ./internal/policy/matcher_test.go` passes
  - **PAT-006 gate grep for session subjects:**
    ```
    rg -n '\.Publish\(ctx, bus\.SubjectSession' internal/ --type go | grep -v _test.go
    rg -n '\.Publish\(r.Context\(\), bus\.SubjectSession' internal/ --type go | grep -v _test.go
    ```
    → ~12-14 hits; every hit's context must contain `bus.NewEnvelope` or `*bus.Envelope`, NO `map[string]interface{}`.
  - **sim.updated publisher gate (closes F-119):**
    ```
    rg -n '\.Publish\(.*bus\.SubjectSIMUpdated' internal/ --type go
    ```
    → ≥7 hits (6 handlers + 1 bulk). BEFORE FIX-212: 0 hits.
  - New tests:
    - `TestSessionStarted_PublishesEnvelope_WithICCID` (radius, gx, gy, ausf — one per protocol)
    - `TestPublishSimUpdated_Activate_EmitsEnvelope`
    - `TestPublishSimUpdated_Terminate_EmitsEnvelope`
    - `TestPublishSimUpdated_Patch_OnlyEmitsOnActualStateChange`
    - `TestBulkStateChange_EmitsPerSimEnvelope`
    - `TestPolicyMatcher_ConsumesEnvelope_HappyPath`
    - `TestPolicyMatcher_LegacyShape_LogsAndSkips`

### Task 5: Other publishers migrate (policy.changed, operator.health, anomaly, IP, SLA, notification.dispatch)

- **Files:**
  - Modify `internal/operator/health.go` — `publishHealthEvent` (line 453) envelope migration (`OperatorHealthEvent` struct → envelope wrapping the existing fields in `Meta`)
  - Create/modify `internal/api/policy/handler.go` — add `publishPolicyChanged(ctx, policy, changeType)` + call sites in `Create/Update/Delete/Archive` (add publisher per AC-2 "etc.")
  - Modify `internal/policy/rollout/service.go:475` — `rollout_progress` envelope
  - Modify `internal/analytics/anomaly/engine.go:194`, `batch.go:165` — `anomaly.detected` envelope
  - Modify `internal/job/ip_reclaim.go:123` — envelope
  - Modify `internal/job/ip_grace_release.go:94` — envelope
  - Modify `internal/job/sla_report.go:164` — envelope
  - Modify `internal/notification/service.go:439` — `notification.new` envelope; also `job/sms_gateway.go:144`, `job/webhook_retry.go:201`, `job/data_portability.go:221`, `job/scheduled_report.go:189`, `policy/enforcer/enforcer.go:333` (notification.dispatch path)
- **Depends on:** Task 1 (envelope)
- **Complexity:** **medium** (repetitive migrations, no hot-path risk; resolver calls cold-path acceptable)
- **Pattern ref:** Mirror Task 3 pattern — read the existing publish-map at each site, convert to `bus.NewEnvelope(type, tenantID, severity).SetEntity(…).WithMeta(…)`. For policy CRUD publisher (NEW in `api/policy/handler.go`): read `internal/api/sim/handler.go::publishSimUpdated` (established in Task 4) for the exact pattern.
- **Context refs:** "Publisher Inventory", "Canonical Event Envelope"
- **What:**
  - `operator/health.go:453` healthchange — envelope `type="operator.health_changed"`, `source="operator"`, `entity:{type:"operator", id, display_name=OperatorName}`, `meta:{previous_status, current_status, circuit_state, latency_ms}`.
  - `policy.changed` NEW publisher in `api/policy/handler.go`: emitted on Create/Update/Delete/Archive. Entity `{type:"policy", id, display_name=policy.Name}`. Severity `"info"`. Meta `{change_type, version}`. PAT-011 note: `api/policy/handler.Handler` constructor MUST gain `eventBus` dependency — audit constructor in `cmd/argus/main.go`.
  - `anomaly.detected`: entity `{type:"sim", id=sim_id, display_name=ICCID via resolver}`.
  - IP subjects: entity varies (see §Publisher Inventory).
  - SLA report: entity operator.
  - Notification.dispatch: entity nil; meta carries event_type, channels_sent.
- **Verify:**
  - `go build ./...` passes
  - `go test ./internal/operator/... ./internal/api/policy/... ./internal/policy/rollout/... ./internal/analytics/anomaly/... ./internal/job/...` passes
  - **PAT-006 gate grep** per subject:
    ```
    rg -n '\.Publish\(.*bus\.Subject(Policy|OperatorHealth|Anomaly|IP|SLA|Notification)' internal/ --type go | grep -v _test.go
    ```
    Each match line has `bus.NewEnvelope(` or `*bus.Envelope` in surrounding ±3 lines.
  - Unit test coverage: one happy-path test per subject per publisher file.

### Task 6: Consumer refactor — WS hub envelope-aware + notification strict parse + legacy shim metric

- **Files:**
  - Modify `internal/ws/hub.go:208` `relayNATSEvent` — dual-path parse
  - Modify `internal/ws/hub_test.go` — new tests
  - Modify `internal/notification/service.go` — legacy shim wiring (if not already complete in Task 3; verify here)
  - Modify `internal/bus/envelope.go` — add `argus_events_legacy_shape_total` + `argus_events_invalid_total` Prometheus counters
  - Modify `internal/metrics/` (or wherever metrics live) to register new counters
- **Depends on:** Task 1, Task 3
- **Complexity:** **high** (touches the cross-cutting message relay + metrics)
- **Pattern ref:** Read `internal/ws/hub.go:208-222` existing relayNATSEvent; read `internal/metrics/*.go` for the existing counter-registration pattern (likely `prometheus.NewCounterVec`).
- **Context refs:** "Data Flow", "Canonical Event Envelope", "Decisions > D3"
- **What:**
  - WS hub `relayNATSEvent`: `var env bus.Envelope; if err := json.Unmarshal(data, &env); err != nil || env.EventVersion != 1 { return h.relayLegacyEvent(subject, data) }`. On success: tenantID from `env.TenantID`; broadcast the raw data byte-slice to connections matching the subject's WS type (preserving behavior).
  - `relayLegacyEvent` (NEW): existing map-based path, but with `argus_events_legacy_shape_total{subject}.Inc()` metric emit.
  - New metrics: `argus_events_legacy_shape_total{subject}`, `argus_events_invalid_total{subject,reason}`.
- **Verify:**
  - `go build ./...` passes
  - `go test ./internal/ws/... ./internal/metrics/...` passes
  - New tests:
    - `TestHub_RelayNATSEvent_EnvelopeShape_ExtractsTenantID`
    - `TestHub_RelayNATSEvent_LegacyShape_FallsThroughWithMetric`
    - `TestHub_RelayNATSEvent_MissingTenantID_BroadcastsAll` (legacy contract preserved)

### Task 7: Event catalog endpoint + FE types

- **Files:**
  - Create `internal/api/events/catalog_handler.go`
  - Create `internal/api/events/catalog_handler_test.go`
  - Modify `internal/gateway/router.go` — register `r.Get("/api/v1/events/catalog", deps.EventsCatalogHandler)`
  - Modify `cmd/argus/main.go` — construct catalog handler + add to router deps
  - Create `web/src/types/events.ts` — `BusEnvelope`, `EntityRef`, `EventCatalogEntry` types
- **Depends on:** Task 1
- **Complexity:** **medium**
- **Pattern ref:** Read `internal/api/alert/handler.go` for the handler structure (from FIX-209); read `web/src/types/analytics.ts` for the TS type file style.
- **Context refs:** "API Specifications > GET /api/v1/events/catalog", "Canonical Event Envelope", "Decisions > D7"
- **What:**
  - Handler: single method `List(w, r)` returning `writeSuccess(w, http.StatusOK, map[string]interface{}{"events": events.Catalog}, nil)`. No DB. No query params. Returns in O(1).
  - Router: register GET route inside auth-required block (mirror other `/api/v1/*` patterns).
  - `web/src/types/events.ts`:
    ```ts
    export interface BusEnvelope<M = Record<string, unknown>> {
      event_version: number;
      id: string;
      type: string;
      timestamp: string;
      tenant_id: string;
      severity: 'critical' | 'high' | 'medium' | 'low' | 'info';
      source: string;
      title: string;
      message?: string;
      entity?: EntityRef;
      dedup_key?: string;
      meta?: M;
    }
    export interface EntityRef { type: string; id: string; display_name?: string; }
    export interface EventCatalogEntry { type: string; source: string; default_severity: string; entity_type: string; description: string; meta_schema: Record<string, string>; }
    ```
  - **PAT-015 gate:** NO FE page mounts or imports `BusEnvelope` in this story (D7). Catalog handler is API-only.
- **Verify:**
  - `go build ./... && go test ./internal/api/events/... ./internal/gateway/...` passes
  - `npm --prefix web run lint && npm --prefix web run type-check` passes
  - `curl localhost:8080/api/v1/events/catalog` returns 401 without auth, 200 with auth; JSON shape matches §API Specifications.
  - Tests:
    - `TestEventsCatalogHandler_List_ReturnsCatalog`
    - `TestEventsCatalogHandler_List_RequiresAuth`

### Task 8: Docs + ROUTEMAP tech debt management

- **Files:**
  - Modify `docs/architecture/WEBSOCKET_EVENTS.md` — add envelope schema section + per-subject example payloads
  - Modify `docs/ROUTEMAP.md` — mark D-075 RESOLVED (FIX-212); add D-077 (deferred subjects); add D-078 (legacy shape shim removal; follow-up story)
  - Modify `docs/stories/fix-ui-review/FIX-212-unified-event-envelope.md` — if AC text conflicts with plan decisions (D-class), amend with a `## Amendments` section referencing this plan
- **Depends on:** Tasks 1-7
- **Complexity:** low
- **Pattern ref:** Read `docs/architecture/WEBSOCKET_EVENTS.md` existing structure; read `docs/ROUTEMAP.md` `## Tech Debt` table for row format.
- **Context refs:** "Decisions > D5,D6,D3", "Problem Context"
- **What:**
  - `WEBSOCKET_EVENTS.md`: new `## Event Envelope (FIX-212)` section with the Go struct + JSON sample + mandatory/optional table. Per-subject subsection enumerates `type`, `source`, entity kind, default severity, meta schema.
  - `ROUTEMAP.md`:
    - D-075: status → `RESOLVED 2026-04-21 (FIX-212)`.
    - Add D-077: "Remaining NATS subjects deferred from FIX-212 envelope migration: SubjectJob*, SubjectCacheInvalidate, SubjectBackup*, SubjectAuditCreate. Internal plumbing subjects not UI-surfaced. Envelope migration desirable for consistency but not blocking. Target: post-release consolidation story." Status OPEN.
    - Add D-078: "Legacy event-shape shim in `ws/hub.go::relayLegacyEvent` and `notification/service.go::parseAlertPayloadLegacy` — 1-release grace window per FIX-212 D3. Remove when `argus_events_legacy_shape_total{subject=*}` stays at 0 across a full release cycle." Target: next release.
- **Verify:**
  - `grep -n 'D-075' docs/ROUTEMAP.md` → shows RESOLVED
  - `grep -n 'D-077\|D-078' docs/ROUTEMAP.md` → shows both new OPEN entries
  - `grep -n 'Event Envelope' docs/architecture/WEBSOCKET_EVENTS.md` → shows new section

---

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|---|---|---|
| AC-1 (unified envelope struct) | Task 1 | Task 1 tests (TestEnvelope_*), Task 8 (docs) |
| AC-2 (publishers migrated) | Tasks 3, 4, 5 | PAT-006 gate greps per task, per-subject unit tests |
| AC-3 (sim.updated publisher added) | Task 4 | `TestPublishSimUpdated_*` tests, `rg` gate shows ≥7 hits |
| AC-4 (direct notification inserts handled) | Tasks 3, 5 — kept as internal with inline comment per D9 | Code-review gate (comment presence) |
| AC-5 (catalog endpoint) | Task 7 | `TestEventsCatalogHandler_*`, curl verification |
| AC-6 (entity.display_name filled by publisher) | Tasks 3, 4, 5 (resolver wiring in all non-hot-path publishers; ICCID for session hot-path per D2) | Unit tests assert display_name non-empty for each publisher |
| AC-7 (WEBSOCKET_EVENTS.md updated) | Task 8 | grep verification |
| AC-8 (backward compat shim, 1 release) | Tasks 3, 6 — `parseAlertPayloadLegacy` + `relayLegacyEvent` + `argus_events_legacy_shape_total` metric | Tests `TestHandleAlertPersist_LegacyShape_*`, `TestHub_*LegacyShape*`; D-078 opened in Task 8 for removal |
| D-075 closure (tenant_id subscriber fallback removed) | Task 3 | `grep -n 'systemTenantID' internal/notification/` → ZERO hits; D5 decision enforced at consumer_lag + storage_monitor publishers |

---

## Story-Specific Compliance Rules

- **API:** `GET /api/v1/events/catalog` uses standard envelope `{status, data, meta?, error?}` (D7)
- **DB:** No migration. FIX-212 is shape-only at the wire layer.
- **Architecture:**
  - Envelope type lives in `internal/bus/` (shared; zero cross-layer import — `bus` is already the lowest shared layer)
  - Resolver in `internal/events/` (new package); depends only on `internal/store` + `internal/cache`/`internal/redis`
  - Every publisher file constructs via `bus.NewEnvelope(...)`; no raw `map[string]interface{}` literals left for in-scope subjects
- **FE:** `web/src/types/events.ts` defines the TS mirror of Go `Envelope`. No visual changes in this story (FIX-213 polishes).
- **Business:** SIM state-change publishing closes F-119 (policy matcher unblock). Alert publisher tenant_id is now mandatory (D-075 closure).
- **ADR:** No ADR required (existing pub/sub architecture unchanged; shape refinement only).

---

## Bug Pattern Warnings

- **PAT-006 [EXISTENTIAL]** — shared payload struct field silently omitted. FIX-212 migrates 22 publish sites to a shared `bus.Envelope` struct. Every publisher call site must set `Type`, `TenantID`, `Severity`, `Source`, `Title` explicitly; Go's zero-value defaults will silently compile with any field missing. **Gate grep per task 3/4/5**: every `.Publish(...Subject*...)` line must be followed within 3 lines by `bus.NewEnvelope(` OR carry a `*bus.Envelope` variable. Zero `map[string]interface{}` literals allowed in in-scope publishers.
- **PAT-011 [CRITICAL]** — plan-specified option/dependency missing at main.go construction site. Resolver dependency is threaded into every alert-publisher constructor (`health.NewHealthChecker(..., WithResolver(r))`, `anomaly.NewEngine(..., WithResolver(r))`, etc.). `cmd/argus/main.go` is the sole wiring point; a missed constructor → that publisher's `entity.display_name` stays empty → FE shows UUID → silent AC-6 regression. Gate: `rg -n 'NewHealthChecker|NewEngine|NewRenewalJob|NewConsumerLagPublisher|NewEnforcer|NewStorageMonitor|NewAnomalyBatchSupervisor' cmd/argus/main.go` → every line includes `WithResolver(` argument.
- **PAT-015 [MEDIUM]** — declared but unmounted FE component. `web/src/types/events.ts` is created but NOT consumed by any page in FIX-212 (D7 — FIX-240 scope). Mitigation: file is type-only (no React component to mount); TS import check runs in next story. Gate: `grep -r 'from.*types/events' web/src` → 0 hits OK in this story; MUST be >0 in FIX-240.
- **PAT-016 [LOW]** — cross-store PK confusion. Envelope carries `entity.id` which may be alerts.id, anomalies.id, sims.id, operators.id depending on event type. Consumers MUST branch on `entity.type` before using `entity.id`. Gate: WS hub relay + notification subscriber tests include at least one wrong-type assertion.
- **PAT-017 [CRITICAL]** — config parameter threaded to store but not to REST handler. N/A in FIX-212 direct — no new config knobs. But: resolver TTL should be configurable; if added, must thread through both resolver construction AND any /events/catalog-serving path. Current plan: TTL hardcoded at 600s in resolver; no config knob to trace. Low risk.
- **PAT-013 [N/A]** — no CHECK-constraint migrations in this story.

---

## Tech Debt (from ROUTEMAP)

- **D-075 (from FIX-209 Gate F-A1): systemTenantID fallback in parseAlertPayload → CLOSE in this story.** Closure mechanism: publishers at `bus/consumer_lag.go`, `job/storage_monitor.go`, `job/anomaly_batch_supervisor.go` set `tenant_id = systemTenantID` themselves in their envelope; subscriber-side sentinel variable + fallback branch DELETED from `notification/service.go`. Gate: `grep -n 'systemTenantID' internal/notification/` → 0 hits post-Task-3.
- **D-077 (NEW, opened in Task 8):** deferred subject set per D6. Status OPEN, target post-release.
- **D-078 (NEW, opened in Task 8):** legacy-shape shim removal per D3. Status OPEN, target next release after metric confirms zero legacy events.

---

## Mock Retirement

No mocks directory in this project. No retirement.

---

## Risks & Mitigations

1. **Risk — AAA hot-path latency regression.** Session publishers fire thousands/sec at fleet scale. Even in-memory ICCID lookup adds nanoseconds; DB/Redis resolver call for operator/APN name would add milliseconds — unacceptable.
   **Mitigation:** D2 hybrid — session publishers EMBED ICCID ONLY (from SIM context already loaded), operator_name/apn_name left empty (display_name = ""). FE falls back to `entity.id` rendering. If future story needs operator_name on session events, deferred to a resolver-subscriber enrichment path (separate D-NNN). Benchmarks: add a micro-benchmark in Task 4 `BenchmarkPublishSessionStarted_EnvelopeOverhead` — must show ≤100ns overhead over current map-literal path.

2. **Risk — 22 publisher migrations in one story = high surface area for PAT-006 regression.** Zero-value-compile risk is existential at this scale.
   **Mitigation:** each task (3, 4, 5) has a mandatory `rg` gate grep that enumerates every in-scope publish-site; reviewer must paste output into the gate. Unit test per publisher per subject — 22 tests minimum. No task advances without green gate.

3. **Risk — Legacy shape shim becomes a permanent tenant.** If any deferred publisher (D6) keeps emitting legacy shapes, `argus_events_legacy_shape_total` never hits zero → D-078 never closes → shim stays forever → technical debt accumulation.
   **Mitigation:** metric emit is per-subject; deferred subjects (job.*, cache.*, backup.*, audit.*) don't go through the alert-triggered or ws-hub envelope path at all (their consumers are internal plumbing, not the envelope-aware paths). Shim only fires for IN-SCOPE subjects publishing pre-FIX-212 payloads — which should be ZERO after the commit lands. Task 8 documents the metric observability; next-release story gates removal on 1 full week of zero emission across the 14 in-scope subjects.

4. **Risk — Resolver cache staleness on rename.** Operator renamed → Redis still has old name for up to 10 min → alerts fired in that window carry stale display_name.
   **Mitigation:** FIX-202 already publishes `cache.invalidate` for operator/apn rename events; resolver subscribes and DELs the key (Task 2). Worst case: 10m TTL expiry catches any missed invalidation.

5. **Risk — FE breakage during rollout.** If a deployed FE version expects the new envelope but some in-flight NATS event is still old-shape, the Event Stream UI silently drops rows.
   **Mitigation:** FE `BusEnvelope` type has ALL fields optional except `type`; FE parse path has `entity?.display_name ?? entity?.id ?? "unknown"` fallback chain. New envelope is a strict superset of the old map-based payload (old fields still accessible via `meta` after migration), so FE sees continuity. Task 7 TS types reflect this.

6. **Risk — Alert publisher tenantID mandate breaks infra-global alerts (consumer lag, storage).** D-075 closure removes subscriber fallback; publishers must set SOMETHING.
   **Mitigation:** D5 decision — publishers at `bus/consumer_lag.go`, `job/storage_monitor.go`, `job/anomaly_batch_supervisor.go` set `tenant_id = systemTenantID` (demo tenant UUID) themselves. This preserves UI visibility under the demo tenant's RLS scope (unchanged from FIX-209 behavior — just moves the sentinel from subscriber to publisher, which is architecturally correct).

7. **Risk — `sim.updated` publisher fires storms on bulk state change.** 10k SIMs activated in one bulk operation → 10k envelope publishes → 10k resolver calls → Redis/DB hammered.
   **Mitigation:** (a) bulk_state_change emits per-SIM but through NATS which is decoupled from HTTP; (b) resolver uses ICCID from the already-loaded SIM (Task 4 passes SIM pointer, not UUID) → zero DB/Redis calls on hot path; (c) if throughput is still an issue, defer to D-NNN: bulk publisher batches into a single `sim.bulk_state_changed` envelope instead of per-SIM. Out of scope for FIX-212; Task 4 ships per-SIM and flag if Gate benchmarks show regression.

8. **Risk — Wave 3 complexity (XL + most complex architectural shift so far).** Gate failure likelihood is highest of any FIX story to date.
   **Mitigation:** 8-task decomposition with explicit parallelization: Tasks 1 and 2 run in parallel (independent); Tasks 3, 4, 5 depend on Tasks 1+2 but are independent of each other (can run in 3 dispatches in parallel); Task 6 depends on Tasks 1, 3; Task 7 depends on Task 1; Task 8 is final cleanup. Wave plan: Wave A (1, 2) → Wave B (3, 4, 5) → Wave C (6, 7) → Wave D (8). High-complexity tasks (3, 4, 6) land in Wave B/C.

---

## Summary

FIX-212 delivers the authoritative `bus.Envelope` struct, migrates 22 publish sites across 14 in-scope NATS subjects, adds the missing `sim.updated` publisher (closes F-119), embeds entity names at the publisher (hot-path-safe per D2), closes D-075 via publisher-side sentinel + subscriber-side strict validation, and ships the `GET /api/v1/events/catalog` endpoint. The tolerant `alertEventFlexible` persist path is replaced by strict envelope + legacy shim (1-release grace per D3, tracked as D-078). 8 tasks, ~20-25 files modified, ~8-10 files created. All in-scope alert publishers now carry `tenant_id` and `entity.display_name` without subscriber-side fallback. Gate greps enumerate every publish site to prevent PAT-006 regression; resolver wiring at every constructor site gates PAT-011.

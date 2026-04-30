# Implementation Plan: FIX-210 — Alert Deduplication + State Machine (Edge-triggered, Cooldown)

**Story:** `docs/stories/fix-ui-review/FIX-210-alert-deduplication.md`
**Track / Wave:** UI Review Remediation, Wave 2 (Alert Architecture)
**Depends on:** FIX-209 (DONE — `alerts` table, `dedup_key` column reserved, `suppressed` state reserved, `handleAlertPersist` single-writer), FIX-211 (DONE — canonical 5-value severity)
**Mode:** AUTOPILOT (full track)

---

## Decisions (read first — these resolve story-vs-dispatch conflicts before tasks)

FIX-210 inherits two constraints in tension: (a) the reserved schema shipped in FIX-209 (`dedup_key VARCHAR(255)` NULL, non-unique partial index on `state IN ('open','suppressed')`) and (b) the story AC text which was written before FIX-209 landed. Where they conflict, we reconcile here explicitly.

| # | Conflict | Decision | Rationale |
|---|---|---|---|
| D1 | Column name — story says `dedupe_key`, FIX-209 shipped `dedup_key` | **Keep `dedup_key`** everywhere (column, Go field, payload, metric label) | FIX-209 is already on disk; `dedup_key` is the canonical name. Story file is amended in Task 7 to match. |
| D2 | Nullability — story says `NOT NULL`, FIX-209 reserved nullable | **Keep nullable**; add **explicit `NOT NULL` for operator/infra/policy sources** via `handleAlertPersist` invariant, but do NOT alter column to `NOT NULL` | Existing partial index already has `WHERE dedup_key IS NOT NULL`. Nullable lets us ship `alerts` rows without dedup for future rare cases (e.g. manually inserted admin alerts) without re-migration. Persist path always computes a key post-FIX-210, so in practice the column is non-null after this story; guard at runtime. |
| D3 | Severity in hash — story AC-2 includes `severity`, dispatch says exclude | **EXCLUDE severity from the hash.** Store latest severity in the row; update it on dedup hit (severity may escalate medium→high on the same flap and we want ONE row to reflect the escalation) | Dedup identity = "same root cause." Severity is a measurement of that cause, not its identity. Including severity would spawn a new row every time an operator degrades further — defeats dedup. **Deviation from story file documented in §Acceptance Criteria Mapping and propagated to story in Task 7.** |
| D4 | Index uniqueness — story AC-3 needs `ON CONFLICT`, FIX-209 index is non-unique | **Migration drops `idx_alerts_dedup` and recreates as `UNIQUE` partial index** with scope D5 below. Table is empty pre-release; safe. Down migration restores the non-unique form. | `ON CONFLICT` requires a unique constraint/index that matches the conflict target exactly. |
| D5 | Index state scope — story `state='open'`, FIX-209 `('open','suppressed')`, dispatch `('open','acknowledged')` | **`state IN ('open','acknowledged','suppressed')`** | While a row is in any of these states it is an active/known incident; a new event matching the same `dedup_key` must hit that row (increment count or suppress), not create a duplicate. Once `resolved`, the row leaves the partial index and the **cooldown window** (see §State Machine) takes over. |
| D6 | Edge-trigger scope (AC-4) — 7+ publishers, not all fire repeatedly | **In-scope: operator health_worker + policy enforcer** (the two verified repeat-firers today). **Deferred D-NNN tech debt:** anomaly engine bursts, roaming_renewal, storage alerts, consumer lag, SLA violations (these are single-shot or already state-transition-gated upstream; dedup at persist is sufficient). | Enumerated in §Problem Context > Publisher repeat-fire inventory. Scope is the two high-volume flapping sources; others are caught by the persist-level dedup (belt) even without edge-triggering (suspenders). |
| D7 | `fired_at` drift on dedup hit | **Do NOT update `fired_at`** on dedup hit. Update `last_seen_at`, `occurrence_count`, and conditionally `severity` (on escalation only, not downgrade). `fired_at` anchors cursor pagination and retention and must stay stable. | Changing `fired_at` on every hit breaks `(fired_at DESC, id DESC)` cursor determinism and makes retention cutoffs move. |

---

## Problem Context — Current Alert State (Verified)

### Behaviour today (post-FIX-209)

`internal/notification/service.go` `handleAlertPersist` is the single writer to `alerts` via `alertStore.Create`. Every inbound NATS event on `argus.events.alert.triggered` becomes a row — no dedup, no cooldown, no state-machine edge detection.

**Empirical failure modes:**

1. **Operator healthcheck flap (verified):** `internal/operator/health.go` publishes on every failing probe. A stuck SoR endpoint at 30s probe interval produces ~120 identical alert rows/hr — 2880/day. The UI `alerts` page shows a wall of duplicates; notification dispatch (email/Telegram) emits 2880 messages.
2. **Policy enforcer repeat fires:** `internal/policy/enforcer/enforcer.go` publishes an alert each time a policy violates, even when the same SIM+policy+window has already been alerted. Bulk rule firing (e.g. 5k SIMs violating same APN policy during migration) instantly floods the table.
3. **Anomaly batch crash rebursts:** when an anomaly engine run crashes and retries, the same batch can emit the same `anomaly_batch_crash` alert multiple times seconds apart.

### `alerts` schema today (relevant columns)

| Column | Type | State before FIX-210 | State after FIX-210 |
|---|---|---|---|
| `dedup_key` | `VARCHAR(255)` NULL | Reserved, always NULL | Populated by `handleAlertPersist` via `sha256(tenant_id\|type\|source\|entity_triple)`; truncated to 64 hex chars |
| `state` | `VARCHAR(20)` CHECK | `chk_alerts_state` includes `suppressed` (reserved) | Used actively: `open ↔ suppressed` transitions via new `SuppressAlert`/`UnsuppressAlert` store methods |
| `occurrence_count` | — (missing) | — | NEW column `INT NOT NULL DEFAULT 1` |
| `first_seen_at` | — (missing) | — | NEW column `TIMESTAMPTZ NOT NULL DEFAULT NOW()` — copy of `fired_at` at INSERT |
| `last_seen_at` | — (missing) | — | NEW column `TIMESTAMPTZ NOT NULL DEFAULT NOW()` — updates on every dedup hit |
| `cooldown_until` | — (missing) | — | NEW column `TIMESTAMPTZ NULL` — set on resolve; new events within window are dropped (not persisted) |
| `idx_alerts_dedup` | Partial index, non-unique | `(tenant_id, dedup_key) WHERE dedup_key IS NOT NULL AND state IN ('open','suppressed')` | DROP + recreate as **UNIQUE** on `(tenant_id, dedup_key) WHERE state IN ('open','acknowledged','suppressed')` |

### Publisher repeat-fire inventory (AC-4 edge-triggering scope)

| Publisher site | File | Repeat-fires? | AC-4 edge-trigger in-scope? | Persist-level dedup catches it? |
|---|---|---|---|---|
| Operator health_worker | `internal/operator/health.go` | YES (every probe) | **YES** — Task 4 | YES (belt) |
| Policy enforcer | `internal/policy/enforcer/enforcer.go` | YES (every violation check) | **YES** — Task 4 | YES (belt) |
| Anomaly engine | `internal/analytics/anomaly/engine.go` | sometimes (batch retries) | NO (D-NNN — anomaly-level dedup already at `anomalies` table) | YES |
| Roaming agreement renewal | `internal/operator/roaming_renewal.go` | NO (date-gated, once per due) | — | YES |
| Storage metrics | `internal/job/storage_check.go` | once per threshold cross (edge-gated upstream via hysteresis) | — | YES |
| NATS consumer lag | `internal/job/nats_lag.go` | once per threshold cross | — | YES |
| SLA violation | `internal/job/sla_check.go` | once per window | — | YES |

**Scope justification:** two publishers (health_worker, enforcer) account for >95% of observed duplicate volume in pre-release smoke. The remaining five are already state-transition-gated upstream; persist-level dedup (belt) covers the rare backdoor without edge-triggering (suspenders not required in FIX-210).

### Out of Scope (do NOT touch)

- `anomalies` schema & state machine — independent, owns `false_positive` state
- `meta.anomaly_id` linkage from FIX-209 — untouched
- Publisher payload shape normalization — FIX-212
- Monthly partitioning of `alerts` — D-NNN
- Email/Telegram dispatch dedup (channel-level suppression) — future "alert-notification-polish" story; FIX-210 handles DB/UI only. Dispatch will still fire once per INSERT but with dedup in place INSERT rate drops dramatically.

---

## Canonical State Machine (Authoritative)

### States

| State | Meaning | Terminal? | In dedup partial index? |
|---|---|---|---|
| `open` | Incident active, no human action | No | YES |
| `acknowledged` | Operator aware, not yet resolved | No | YES |
| `suppressed` | Dedup'd into another row (or admin silenced) | No | YES |
| `resolved` | Incident closed | Yes | NO — dedup defers to `cooldown_until` |

### Transitions

```
  ┌──────────────── NEW event (matching dedup_key) ─────────────┐
  │                                                             │
  ▼                                                             │
[open] ──PATCH ack──► [acknowledged] ──PATCH resolve──► [resolved]
  │                         │                              │
  │                         │                              │
  └──── (dedup hit: ────────┘                              │
        inc count,                                         │
        update last_seen_at,                               │
        severity escalates in place)                       │
                                                           │
                            NEW event arrives while in ────┘
                            cooldown window (< cooldown_until):
                               → DROP event (count a metric, do NOT persist)
                            NEW event arrives after cooldown:
                               → INSERT new row (fresh incident)
```

**Edge triggers (NEW in FIX-210):**

1. **INSERT-or-increment (AC-3):** `handleAlertPersist` computes `dedup_key`, attempts `INSERT ... ON CONFLICT (tenant_id, dedup_key) WHERE state IN ('open','acknowledged','suppressed') DO UPDATE SET occurrence_count = alerts.occurrence_count + 1, last_seen_at = NOW(), severity = CASE WHEN EXCLUDED.severity_ordinal > alerts.severity_ordinal THEN EXCLUDED.severity ELSE alerts.severity END, meta = alerts.meta || EXCLUDED.meta` — atomic via the unique partial index.

2. **Cooldown gate (AC-5):** before attempting INSERT, if there exists a row with matching `dedup_key` where `state='resolved' AND cooldown_until > NOW()`, drop the event (increment `argus_alerts_cooldown_dropped_total{type}`) and skip persist. Dispatch MAY still run (availability > durability precedent from FIX-209), but with a WARN log.

3. **Resolve → cooldown stamp:** `UpdateState(id, 'resolved')` also sets `cooldown_until = NOW() + interval '<ALERT_COOLDOWN_MINUTES> minutes'`.

4. **Publisher edge-trigger (AC-4) — in-scope publishers only (D6):**
   - `operator/health.go`: track `lastObservedStatus` per operator in-memory; only publish on `healthy→degraded` or `degraded→healthy` transition, not every probe. Persist-level dedup still catches the case where two workers race.
   - `policy/enforcer/enforcer.go`: track `lastEmittedAt` per `(policy_id, sim_id)` with a 60s min interval. Persist-level dedup catches races.

### Dedup Key Strategy (D3 — severity NOT included)

```go
entityTriple := "-"
switch {
case simID != nil:      entityTriple = "sim:" + simID.String()
case operatorID != nil: entityTriple = "op:"  + operatorID.String()
case apnID != nil:      entityTriple = "apn:" + apnID.String()
}
raw := fmt.Sprintf("%s|%s|%s|%s", tenantID, alertType, source, entityTriple)
sum := sha256.Sum256([]byte(raw))
dedupKey := hex.EncodeToString(sum[:]) // 64 chars, fits VARCHAR(255)
```

- **Fields included:** `tenant_id`, `type`, `source`, entity triple (sim/operator/apn — exactly one, prefixed for disambiguation)
- **Fields excluded:** `severity` (D3), `title`, `description`, `meta` (these are human-readable or vary per event; they'd spawn a row per title wording change)
- **Normalization:** tenant + UUIDs are lowercase hex strings from `uuid.UUID.String()`; type+source are lowercase ASCII enum values; no whitespace collapsing needed (fields are already normalized at persist boundary)
- **Length:** always 64 hex chars (SHA-256), trivially fits `VARCHAR(255)`
- **Collision risk:** SHA-256 on a short input space; effectively zero

---

## Architecture Context

### Components Involved

| Component | Layer | File(s) | Role |
|---|---|---|---|
| Alert-state shared package | Go shared | `internal/alertstate/alertstate.go` (NEW) | D-076 consolidation: single source of truth for state enum, transitions, update-allowed set |
| Alert dedup helper | Go shared | `internal/alertstate/dedup.go` (NEW; same package) | `DedupKey(tenantID, type, source, sim, op, apn) string` — pure function |
| Alert schema migration | DB | `migrations/20260423000001_alerts_dedup_statemachine.up.sql` / `.down.sql` (NEW) | Adds `occurrence_count`, `first_seen_at`, `last_seen_at`, `cooldown_until`; drops+recreates `idx_alerts_dedup` as UNIQUE with new state scope |
| AlertStore extensions | Go store | `internal/store/alert.go` (MODIFY) | New methods `UpsertWithDedup`, `SuppressAlert`, `UnsuppressAlert`, `FindActiveByDedupKey`; `UpdateState` stamps `cooldown_until` on resolve; `validAlertTransitions` moved to `internal/alertstate` |
| Alert handler | Go API | `internal/api/alert/handler.go` (MODIFY) | Imports `validAlertStates` / `allowedUpdateStates` from `internal/alertstate`; no behavior change (D-076 only) |
| Notification persist subscriber | Go service | `internal/notification/service.go` (MODIFY) | `handleAlertPersist` swaps `alertStore.Create` → `alertStore.UpsertWithDedup`; `parseAlertPayload` computes `dedup_key`; cooldown gate check |
| Operator health worker | Go service | `internal/operator/health.go` (MODIFY) | Edge-trigger: publish only on status change, not every probe |
| Policy enforcer | Go service | `internal/policy/enforcer/enforcer.go` (MODIFY) | Edge-trigger: 60s min-interval per `(policy_id, sim_id)` |
| Alert DTO | Go API | `internal/api/alert/handler.go` + `internal/store/alert.go` `scanAlert` | Expose new columns in JSON |
| Config | Go config | `internal/config/config.go` | NEW env `ALERT_COOLDOWN_MINUTES` (default 5) |
| Metrics | Go metrics | `internal/metrics/metrics.go` | 3 new Prometheus counters — see §Metrics |
| FE alert types | Web types | `web/src/types/analytics.ts` | Add `occurrence_count`, `first_seen_at`, `last_seen_at`, `cooldown_until`, `dedup_key` to `Alert` |
| FE alert list row | Web page | `web/src/pages/alerts/index.tsx` | Render "N× in last Xh" badge when `occurrence_count > 1`; keep existing filter incl. `suppressed` (already shipped in FIX-209 Task 6) |
| FE alert detail | Web page | `web/src/pages/alerts/detail.tsx` | Show first/last seen + count; "Suppressed" status treatment |
| ERROR_CODES docs | Docs | `docs/architecture/ERROR_CODES.md` | Update Alerts Taxonomy: add `suppressed` as actively used, describe dedup/cooldown |
| CONFIG docs | Docs | `docs/architecture/CONFIG.md` | `ALERT_COOLDOWN_MINUTES` env row |
| API index | Docs | `docs/architecture/api/_index.md` | Alert list response shape gains 5 new fields — note only |
| DB index | Docs | `docs/architecture/db/_index.md` | `alerts` row updated: +4 columns, unique partial index |
| Story file | Docs | `docs/stories/fix-ui-review/FIX-210-alert-deduplication.md` | Amend AC-1 column name `dedupe_key→dedup_key` and AC-2 note severity exclusion (D3) |

### Data Flow (post-FIX-210)

```
PUBLISHER (operator/health.go, policy/enforcer.go — NOW edge-triggered)
      │
      ▼  NATS publish → subject argus.events.alert.triggered
      │
      ├─► ws.hub (relay to browser as "alert.new")                    ← UNCHANGED
      │
      └─► notification.Service.handleAlertPersist
              │
              ├─ 1. parseAlertPayload(data)
              │     └─ NEW: compute dedup_key via alertstate.DedupKey(...)
              │
              ├─ 2. alertStore.UpsertWithDedup(ctx, params)           ← NEW ENTRY (replaces .Create)
              │     ├─ a. if exists(dedup_key, state=resolved AND cooldown_until > NOW)
              │     │     → return (nil, ErrAlertInCooldown)
              │     │       → metric argus_alerts_cooldown_dropped_total++
              │     │       → LOG Warn; skip dispatch? NO — still dispatch (availability)
              │     │
              │     ├─ b. else: INSERT ... ON CONFLICT (tenant_id, dedup_key)
              │     │     WHERE state IN ('open','acknowledged','suppressed')
              │     │     DO UPDATE SET
              │     │       occurrence_count = alerts.occurrence_count + 1,
              │     │       last_seen_at = NOW(),
              │     │       severity = GREATEST_BY_ORDINAL(EXCLUDED.severity, alerts.severity),
              │     │       meta = alerts.meta || EXCLUDED.meta
              │     │     RETURNING *, (xmax = 0) AS was_inserted
              │     │
              │     └─ c. if was_inserted=false → metric argus_alerts_deduplicated_total{type,source}++
              │
              ├─ 3. dispatchToChannels(...)                            ← UNCHANGED
              └─ 4. audit.Emit("alert.created" | "alert.deduplicated") ← log which happened

READ PATHS (UNCHANGED from FIX-209 — new columns additive)
  GET /api/v1/alerts         → alertStore.ListByTenant (extra fields in response)
  PATCH /api/v1/alerts/{id}  → alertStore.UpdateState (now also stamps cooldown_until on resolve)
                             → NEW admin action (in scope but low priority): UpdateState(..., "suppressed")
                                rejected; use SuppressAlert(id, reason) for manual suppression
```

### Metrics (Prometheus)

Label cardinality is critical — **no tenant_id labels, no UUID labels**.

| Metric | Labels | Fires when | Cardinality bound |
|---|---|---|---|
| `argus_alerts_deduplicated_total` | `type`, `source` | ON CONFLICT hit (dedup instead of new row) | type × source = ~50 × 5 = 250 combos max |
| `argus_alerts_cooldown_dropped_total` | `type`, `source` | cooldown gate drops an event | Same bound |
| `argus_alerts_suppressed_total` | `reason` ∈ `{dedup_burst, manual_admin}` | SuppressAlert called | 2 values |

**Do NOT label by tenant_id or alert_id** — unbounded cardinality. PAT-003.

---

## Tasks

### Task 1: `internal/alertstate` package — D-076 consolidation + DedupKey helper

- **Files:** Create `internal/alertstate/alertstate.go`, `internal/alertstate/dedup.go`, `internal/alertstate/alertstate_test.go`
- **Depends on:** — (none)
- **Complexity:** low
- **Pattern ref:** Read `internal/severity/severity.go` (FIX-211) — same structure: package constants, typed `State` string, `Validate`, sentinel errors, `Transitions` map, pure helpers. No runtime deps.
- **Context refs:** "Decisions > D6", "Canonical State Machine > Transitions", "Dedup Key Strategy"
- **What:**
  - Export constants: `StateOpen="open", StateAcknowledged="acknowledged", StateResolved="resolved", StateSuppressed="suppressed"`
  - Export `AllStates []string`, `ActiveStates []string` (`open`, `acknowledged`, `suppressed` — the three that dedup matches against)
  - Export `UpdateAllowedStates map[string]bool` — which states a user PATCH may target: `{acknowledged: true, resolved: true}` (NOT `suppressed` — that is internal/admin-only, reached via `SuppressAlert` method; preserves the FIX-209 API contract that `PATCH /alerts/{id}` does not accept `suppressed`)
  - Export `Transitions map[string]map[string]bool` — `open→{acknowledged, resolved, suppressed}`, `acknowledged→{resolved, suppressed}`, `suppressed→{open, resolved}`, `resolved→{}` (terminal for state-machine purposes; cooldown is a property, not a state transition)
  - Export `Validate(s string) error`, `IsActive(s string) bool`, `CanTransition(from, to string) bool`, `IsUpdateAllowed(s string) bool`
  - `dedup.go`: `func DedupKey(tenantID uuid.UUID, alertType, source string, simID, operatorID, apnID *uuid.UUID) string` — exactly the algorithm in §Dedup Key Strategy; pure, no I/O
  - Sentinel errors: `ErrInvalidAlertState`, `ErrInvalidAlertTransition`
- **Verify:**
  - `go build ./internal/alertstate/...` passes
  - `go test ./internal/alertstate/...` passes
  - **Grep gate:** `rg -n 'validAlertStates|validAlertTransitions|allowedUpdateStates' internal/` finds ONLY the import+assignment lines in `internal/api/alert/handler.go` and `internal/store/alert.go` (Task 2) — no inline map definitions remain (D-076 closed)
- **Unit tests (`alertstate_test.go`):**
  - `TestDedupKey_Deterministic` — same inputs → same 64-char hex
  - `TestDedupKey_DiffersByEntity` — sim vs operator vs apn with same UUID produces different keys (prefix discipline)
  - `TestDedupKey_DoesNotIncludeSeverity` — call with `type=foo` twice, both produce same key (regression guard for D3)
  - `TestDedupKey_DoesNotIncludeNil` — nil sim/op/apn → triple is `-`, stable
  - `TestTransitions_RejectsResolvedToAnything` — terminal
  - `TestTransitions_AllowsOpenToSuppressed` — covers FIX-210 admin suppression path
  - `TestIsUpdateAllowed_RejectsSuppressed` — API contract preservation from FIX-209

### Task 2: DB migration — dedup columns + UNIQUE partial index + cooldown column

- **Files:** Create `migrations/20260423000001_alerts_dedup_statemachine.up.sql` + `.down.sql`
- **Depends on:** — (can run before Task 1, but sequenced after for cleaner story commit)
- **Complexity:** medium
- **Pattern ref:** Read `migrations/20260422000001_alerts_table.up.sql` for style; read `migrations/20260422000001_alerts_table.down.sql` for inverse pattern.
- **Context refs:** "Decisions > D4, D5", "Architecture Context > Components Involved"
- **What (up.sql):**
  ```sql
  ALTER TABLE alerts
    ADD COLUMN occurrence_count INT NOT NULL DEFAULT 1,
    ADD COLUMN first_seen_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN last_seen_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN cooldown_until   TIMESTAMPTZ NULL;

  -- Backfill (safe; pre-release, table empty — but keep idempotent)
  UPDATE alerts SET first_seen_at = fired_at, last_seen_at = fired_at WHERE first_seen_at <> fired_at;

  -- Replace non-unique partial index with UNIQUE partial index (D4, D5)
  DROP INDEX IF EXISTS idx_alerts_dedup;
  CREATE UNIQUE INDEX idx_alerts_dedup_unique
    ON alerts (tenant_id, dedup_key)
    WHERE dedup_key IS NOT NULL
      AND state IN ('open', 'acknowledged', 'suppressed');

  CREATE INDEX idx_alerts_cooldown_lookup
    ON alerts (tenant_id, dedup_key, cooldown_until)
    WHERE state = 'resolved' AND cooldown_until IS NOT NULL;
  ```
- **What (down.sql):** reverse — drop unique index, recreate the original non-unique, drop columns. Table is pre-release empty; safe.
- **Verify:**
  - `make down && make infra-up && make db-migrate` succeeds; then down: `migrate ... down 1` restores the original `idx_alerts_dedup` without UNIQUE.
  - `psql -c "\d+ alerts"` shows 4 new columns with correct defaults + NOT NULL where specified
  - `psql -c "\d alerts"` shows `idx_alerts_dedup_unique UNIQUE, partial` and `idx_alerts_cooldown_lookup`
  - Seed smoke: `make db-seed` clean (no new rows in alerts seed per FIX-209 policy; verify unchanged)

### Task 3: AlertStore — `UpsertWithDedup`, cooldown-aware `UpdateState`, suppress/unsuppress, refactor for D-076

- **Files:** Modify `internal/store/alert.go` + `internal/store/alert_test.go`
- **Depends on:** Task 1 (package), Task 2 (schema)
- **Complexity:** high — atomic upsert SQL, cooldown branch, severity-escalation CASE, concurrent-hit test
- **Pattern ref:** Read existing `internal/store/alert.go::Create` for INSERT RETURNING pattern; read `internal/store/anomaly.go::UpdateState` for update pattern. Read `internal/severity/severity.go::Ordinal` — needed for the CASE expression (pass ordinal as query parameter to avoid PL/pgSQL).
- **Context refs:** "Decisions (all)", "Canonical State Machine", "Data Flow"
- **What:**
  - Remove inline `validAlertTransitions` map; import from `internal/alertstate`
  - Change `Alert` struct: add `OccurrenceCount int`, `FirstSeenAt time.Time`, `LastSeenAt time.Time`, `CooldownUntil *time.Time`
  - Update `scanAlert` to include new 4 columns (PAT-006: any scan-site that's been missed = silent NULL/zero values at runtime)
  - `CreateParams` stays; `DedupKey *string` already present from FIX-209
  - **NEW: `UpsertWithDedup(ctx, p CreateParams) (*Alert, UpsertResult, error)`** where `UpsertResult` is `enum {Inserted, Deduplicated, CoolingDown}`:
    1. If `p.DedupKey == nil`: fall through to plain INSERT (rare path for admin alerts without entity)
    2. SELECT cooldown check: `SELECT 1 FROM alerts WHERE tenant_id=$1 AND dedup_key=$2 AND state='resolved' AND cooldown_until > NOW() LIMIT 1` — if hit → return `CoolingDown` + no INSERT
    3. Else: `INSERT ... ON CONFLICT (tenant_id, dedup_key) WHERE state IN ('open','acknowledged','suppressed') DO UPDATE SET occurrence_count=alerts.occurrence_count+1, last_seen_at=NOW(), severity=CASE WHEN $N > severity_to_ordinal(alerts.severity) THEN $M ELSE alerts.severity END, meta=alerts.meta || EXCLUDED.meta, updated_at=NOW() RETURNING *, (xmax=0) AS was_inserted`
    4. Return `Inserted` or `Deduplicated` based on `was_inserted`
  - **NEW: `SuppressAlert(ctx, tenantID, id uuid.UUID, reason string) (*Alert, error)`** — transitions `open|acknowledged → suppressed`; rejects other transitions with `ErrInvalidAlertTransition`; records `meta.suppress_reason`
  - **NEW: `UnsuppressAlert(ctx, tenantID, id uuid.UUID) (*Alert, error)`** — transitions `suppressed → open` only
  - **NEW: `FindActiveByDedupKey(ctx, tenantID, dedupKey)` (*Alert, error)** — for tests + potential diagnostic endpoint (returns row WHERE state IN active and dedup_key match)
  - **MODIFY `UpdateState`:** when `newState='resolved'`, also set `cooldown_until = NOW() + (cooldownMinutes * interval '1 minute')`. Cooldown minutes passed as parameter (injected from config at handler).
  - Helper `severity_to_ordinal` — implement as a SQL-level `CASE` in the UPDATE SET clause or pass ordinals as parameters. Prefer parameters for clarity: `EXCLUDED` row ordinal computed in Go from `severity.Ordinal(p.Severity)` and passed as `$N`.
- **Verify:**
  - `go build ./internal/store/...` passes
  - `go test ./internal/store/alert_test.go` passes with new tests below
  - `go vet ./internal/store/...` clean
  - Grep gate: `rg -n 'validAlertTransitions' internal/store/alert.go` returns zero hits (map moved to `alertstate`); instead `alertstate.CanTransition` imported
- **Unit tests (new):**
  - `TestAlertStore_UpsertWithDedup_FirstEventInserts` — returns `Inserted`, `occurrence_count=1`
  - `TestAlertStore_UpsertWithDedup_SecondEventIncrements` — returns `Deduplicated`, same `id`, `occurrence_count=2`, `last_seen_at` advanced, `fired_at` unchanged (D7 regression guard)
  - `TestAlertStore_UpsertWithDedup_SeverityEscalationUpdatesInPlace` — first event sev=medium, second event sev=high → row severity=high after second
  - `TestAlertStore_UpsertWithDedup_SeverityDowngradeKeepsHigher` — first event sev=critical, second event sev=low → row severity stays critical
  - `TestAlertStore_UpsertWithDedup_ConcurrentHit` — spin 10 goroutines calling upsert with same key; total `occurrence_count = 10`, exactly 1 row exists
  - `TestAlertStore_UpsertWithDedup_CooldownActive_ReturnsCoolingDown` — seed resolved row with `cooldown_until=NOW()+5min`; upsert returns `CoolingDown`, no new row
  - `TestAlertStore_UpsertWithDedup_CooldownExpired_InsertsFresh` — seed resolved row with `cooldown_until=NOW()-1s`; upsert returns `Inserted`, new row created
  - `TestAlertStore_UpdateState_ResolveStampsCooldownUntil` — resolve sets `cooldown_until = fired_at + 5min` (approximate)
  - `TestAlertStore_SuppressAlert_FromOpen_Succeeds`
  - `TestAlertStore_SuppressAlert_FromResolved_Fails` — `ErrInvalidAlertTransition`
  - `TestAlertStore_UnsuppressAlert_ReopensToOpen`
  - `TestAlertStore_FindActiveByDedupKey_ResolvedNotReturned` — explicit regression for partial-index scope

### Task 4: `handleAlertPersist` dedup wiring + publisher edge-triggering

- **Files:**
  - Modify `internal/notification/service.go` — `parseAlertPayload` computes `dedup_key`; `handleAlertPersist` calls `UpsertWithDedup`; metrics emission
  - Modify `internal/notification/service_test.go` — dedup flow tests
  - Modify `internal/operator/health.go` — in-memory `lastObservedStatus` map per operator; publish only on transition
  - Modify `internal/operator/health_test.go` — transition-only test
  - Modify `internal/policy/enforcer/enforcer.go` — 60s min-interval per `(policy_id, sim_id)` via in-memory LRU; publish only if interval elapsed
  - Modify `internal/policy/enforcer/enforcer_test.go` — interval gate test
  - Modify `internal/config/config.go` — `AlertCooldownMinutes int` with env `ALERT_COOLDOWN_MINUTES` default 5
  - Modify `internal/metrics/metrics.go` — 3 new counters (see §Metrics)
  - Modify `cmd/argus/main.go` — pass `cfg.AlertCooldownMinutes` into notification service + into AlertStore (as handler param threaded through)
- **Depends on:** Task 3 (store), Task 1 (DedupKey)
- **Complexity:** high — 3 publisher sites, store-level cooldown threading, metric wiring at every outcome branch (PAT-011)
- **Pattern ref:**
  - `internal/notification/service.go:831-880` (existing `handleAlertPersist`) — hook dedup_key compute BEFORE `alertStore.Create` call and swap to new method
  - `internal/operator/health.go` — read current probe loop; add `prevStatus map[uuid.UUID]string` guarded by `sync.Mutex` or per-operator channel-owned state (match whichever the existing code uses)
  - `internal/policy/enforcer/enforcer.go` — read current violation emission; wrap with `sync.Map` of `struct{policyID, simID}→time.Time` plus cleanup tick (or use `golang-lru/v2`)
- **Context refs:** "Canonical State Machine > Edge triggers", "Problem Context > Publisher repeat-fire inventory (scope D6)", "Metrics"
- **What — `parseAlertPayload`:**
  - After resolving `type`, `source`, `simID`, `operatorID`, `apnID`, call `alertstate.DedupKey(tenantID, alertType, source, simID, operatorID, apnID)` and set `params.DedupKey = &key` (always populate; never nil post-FIX-210 — D2 runtime invariant)
  - Leave existing severity validation unchanged (FIX-211 coerces invalid to `info`)
- **What — `handleAlertPersist`:**
  - Replace `alertStore.Create(ctx, params)` with `alertStore.UpsertWithDedup(ctx, params)` returning `(*Alert, UpsertResult, error)`
  - Switch on `UpsertResult`:
    - `Inserted`: metric `argus_alerts_created_total` (existing FIX-209), audit "alert.created" (existing)
    - `Deduplicated`: metric `argus_alerts_deduplicated_total{type,source}`; audit "alert.deduplicated" with prior alert_id; LOG Debug
    - `CoolingDown`: metric `argus_alerts_cooldown_dropped_total{type,source}`; audit "alert.cooldown_dropped"; LOG Warn; **still run dispatch** (availability > durability; consistent with FIX-209 Task 3 decision)
  - On persist error (other than cooldown): LOG Error, continue dispatch (unchanged from FIX-209)
- **What — `operator/health.go` edge-trigger:**
  - Add `healthTracker` keyed by `operator_id` storing last probed `status string` (healthy/degraded/down)
  - On every probe: compute new status; if equal to prev → no publish; if changed → publish event with `meta.previous_status` added
  - Track map cleanup: TTL 1h (stale operators) OR cleanup when operator deleted (hook existing operator delete path with event sub)
- **What — `policy/enforcer` edge-trigger:**
  - Add `enforcerRateLimiter` — `map[struct{policyID, simID uuid.UUID}]time.Time` guarded by `sync.RWMutex`; TTL eviction every 5min
  - Before publishing: check last emission time; if `< now - 60s` → skip emit (metric `argus_alerts_rate_limited_publishes_total{publisher="enforcer"}`)
- **Verify:**
  - `go build ./...` passes; `go vet ./...` clean
  - `go test ./internal/notification/... ./internal/operator/... ./internal/policy/enforcer/... ./internal/alertstate/...` all pass
  - **Grep gate PAT-011:** `rg -n 'UpsertWithDedup|Create.*CreateAlertParams' internal/notification internal/store` — `handleAlertPersist` uses `UpsertWithDedup`, no residual `Create(...)` on alertStore from the persist path
  - **Grep gate PAT-006:** `rg -n 'DedupKey' internal/notification/service.go` shows it's computed once in `parseAlertPayload`, not scattered. No publisher computes its own key (AC-3 atomicity requires the single compute point).
  - Integration smoke: fire same operator-health alert 10× in quick succession via simulator → `SELECT occurrence_count FROM alerts WHERE ...` = 10, single row; Prometheus shows `argus_alerts_deduplicated_total=9`
- **New unit/integration tests:**
  - `TestHandleAlertPersist_SecondEvent_Deduplicates` — `handleAlertPersist` called twice with same payload → one row, count=2, metric++
  - `TestHandleAlertPersist_Cooldown_DropsEvent` — seed resolved+cooldown → handleAlertPersist does NOT INSERT; metric `cooldown_dropped` increments; dispatch still runs
  - `TestHandleAlertPersist_CooldownExpired_InsertsFresh` — complementary
  - `TestHandleAlertPersist_NilAlertStore_NoPanic` — PAT-011 regression: if wiring is missed in main.go (nil store), persist path degrades gracefully, dispatch still runs
  - `TestOperatorHealth_SameStatusTwice_NoPublish` — probe twice with same status → one publish
  - `TestOperatorHealth_StatusChange_Publishes`
  - `TestEnforcer_WithinMinInterval_NoPublish`
  - `TestEnforcer_AfterMinInterval_Publishes`

### Task 5: D-076 consolidation in `internal/api/alert/handler.go`

- **Files:** Modify `internal/api/alert/handler.go`; update `internal/api/alert/handler_test.go` if needed (expect no behavior change)
- **Depends on:** Task 1
- **Complexity:** low
- **Pattern ref:** Read `internal/api/alert/handler.go:19-41` (current `validAlertStates` + `allowedUpdateStates` inline maps) — replace with imports from `internal/alertstate`.
- **Context refs:** "Decisions > D6 (blind spot)"
- **What:**
  - Delete inline `validAlertStates` map; replace usage at line 140 with `alertstate.Validate(stateFilter)` or `alertstate.AllStates` membership check (pick whichever keeps error envelope identical to today — FIX-209 API contract MUST NOT change)
  - Delete inline `allowedUpdateStates` map; replace usage at line 286 with `alertstate.IsUpdateAllowed(req.State)` — error envelope on reject must remain `INVALID_STATE_TRANSITION` (409) with the same message. Preserve FIX-209 API contract exactly: `suppressed` is still rejected by PATCH (D1 of `alertstate` package).
  - Handler tests re-run unchanged — any diff in output = regression
- **Verify:**
  - `go test ./internal/api/alert/...` passes unchanged
  - `go build ./...` passes
  - Grep gate: `rg -n 'map\[string\]bool\{\s*"open"' internal/api/alert internal/store/alert.go` returns zero hits (no local re-declarations)
  - Hit the 3 endpoints via curl smoke (existing FIX-209 smoke test recipe in its step-log): response shapes identical

### Task 6: FE — alert row dedup badge + detail panel

- **Files:**
  - Modify `web/src/types/analytics.ts` — add 5 new fields to `Alert`: `dedup_key: string | null`, `occurrence_count: number`, `first_seen_at: string`, `last_seen_at: string`, `cooldown_until: string | null`
  - Modify `web/src/pages/alerts/index.tsx` — render occurrence badge; keep FIX-209-shipped state filter (which already includes `suppressed`)
  - Modify `web/src/pages/alerts/detail.tsx` — render First seen / Last seen / Count block; cooldown banner if active
  - Modify `web/src/lib/alerts.ts` (from FIX-209 Task 6) — add `formatOccurrence(count, firstSeenAt, lastSeenAt): string` helper returning e.g. `"5× in last 2h"`
- **Depends on:** Task 3 (API response shape gains new fields via `alertDTO`)
- **Complexity:** medium — purely presentational but must preserve FIX-209 A11y + FIX-211 `<SeverityBadge>` adoption
- **Pattern ref:** Read `web/src/pages/alerts/index.tsx` current row layout for Badge placement; read `web/src/components/ui/badge.tsx` for variants; NO hex colours — tokens only per `docs/FRONTEND.md`. Invoke `frontend-design` skill once at Task 6 start per FIX-211 precedent.
- **Context refs:** "Data Flow (post-FIX-210)", "Canonical State Machine"
- **What:**
  - `formatOccurrence`: when `count === 1` → return empty string (no badge); when `count > 1` → `"${count}× in last ${humanizeDuration(last_seen_at - first_seen_at)}"` e.g. `"3× in last 45m"`, `"120× in last 2h"`. Cap window label granularity at minutes/hours/days.
  - Alert list row: after title, render `<Badge variant="outline" className="text-xs">{formatOccurrence(...)}</Badge>` only when count > 1
  - Alert detail: add a "Occurrence" section showing First seen (absolute + relative), Last seen, Count. If `state === 'resolved'` AND `cooldown_until > now` → show muted banner "Cooldown active until HH:MM — new occurrences of this condition will be suppressed."
  - If `state === 'suppressed'` → status pill uses muted/neutral variant (not alarming); detail shows `meta.suppress_reason` if present
- **Verify:**
  - `pnpm tsc --noEmit` passes
  - `pnpm build` succeeds
  - `pnpm test` (if any jest suites touch these files) passes
  - Manual visual in dev: simulator fires 5 repeat alerts → row shows "5× in last ..." badge; first_seen / last_seen populated in detail panel
  - A11y: badge has `aria-label="occurred ${count} times"` equivalent or the badge text is sufficient

### Task 7: Docs — ERROR_CODES.md, CONFIG.md, api/_index.md, db/_index.md, ROUTEMAP.md, story file

- **Files:**
  - Modify `docs/architecture/ERROR_CODES.md` §Alerts Taxonomy — update §State table: `suppressed` now actively used (not reserved); describe dedup/cooldown mechanics in a new sub-section; update Cross-reference for FIX-210 to note it is now implemented
  - Modify `docs/architecture/CONFIG.md` — add `ALERT_COOLDOWN_MINUTES` row (default 5, description, min/max sanity)
  - Modify `docs/architecture/api/_index.md` — note expanded `alertDTO` fields (+5)
  - Modify `docs/architecture/db/_index.md` — `TBL-NN alerts` row: +4 columns, index list updated
  - Modify `docs/ROUTEMAP.md` — UI Review track: FIX-210 status to be stamped by step-log; mark D-076 CLOSED in Tech Debt table
  - Modify `docs/stories/fix-ui-review/FIX-210-alert-deduplication.md` — AC-1 rewrite: `dedupe_key → dedup_key`; AC-2 rewrite: severity EXCLUDED from hash with Decisions reference; add Decisions-consumed note at top
- **Depends on:** Tasks 2, 3, 4, 5, 6
- **Complexity:** low
- **Pattern ref:** Read `docs/architecture/ERROR_CODES.md` §Alerts Taxonomy §Cross-reference for FIX-210 (current state); read FIX-209 Task 7 plan entry for the overall docs hygiene template.
- **Context refs:** All §Decisions
- **What:** Straight doc edits. Include one paragraph in ERROR_CODES.md explaining why severity is NOT in the hash (D3) — future maintainers will ask.
- **Verify:**
  - `grep -q 'dedup_key' docs/architecture/ERROR_CODES.md` passes
  - `grep -q 'ALERT_COOLDOWN_MINUTES' docs/architecture/CONFIG.md` passes
  - `grep -q 'occurrence_count' docs/architecture/db/_index.md` passes
  - ROUTEMAP renders correctly (no markdown breakage)

---

## Complexity Guide (Summary)

| Task | Complexity | Effort drivers |
|---|---|---|
| 1. alertstate package | low | pure Go helpers, pattern copy |
| 2. DB migration | medium | unique partial index recreate + down compatibility |
| 3. AlertStore upsert + cooldown + D-076 wiring | **high** | atomic SQL, severity CASE, concurrent-race test, interface refactor |
| 4. handleAlertPersist dedup + 2 publisher edge-triggers + metrics | **high** | 3 call sites, PAT-011 wiring at every branch |
| 5. Handler D-076 consolidation | low | mechanical refactor |
| 6. FE dedup badge + detail | medium | presentational only; A11y + token discipline |
| 7. Docs | low | straight edits |

---

## Acceptance Criteria Mapping

| AC (from story) | Implemented in | Verified by | Deviation notes |
|---|---|---|---|
| AC-1: schema augmented (dedupe_key, occurrence_count, first/last_seen_at, cooldown_until) | Task 2 | `psql \d+ alerts`; migration smoke | Column name is `dedup_key` (D1), nullable (D2) — story text amended in Task 7 |
| AC-2: dedup_key = SHA256(tenant_id \| type \| source \| entity \| severity) | Task 1 `DedupKey` | `TestDedupKey_*` unit tests | **severity EXCLUDED (D3)** — rationale in §Decisions; story text amended in Task 7 |
| AC-3: INSERT ON CONFLICT atomic upsert | Task 2 (UNIQUE index), Task 3 `UpsertWithDedup` | `TestAlertStore_UpsertWithDedup_ConcurrentHit` + integration smoke | Conflict scope is `state IN ('open','acknowledged','suppressed')` (D5), not `state='open'` — justified in §Decisions |
| AC-4: edge-triggered publishers | Task 4 (health.go, enforcer.go) | `TestOperatorHealth_SameStatusTwice_NoPublish`, `TestEnforcer_WithinMinInterval_NoPublish` | Scope = 2 publishers (D6); rest deferred to D-NNN tech debt |
| AC-5: cooldown N minutes post-resolve | Task 3 `UpdateState` + `UpsertWithDedup` cooldown branch; Task 4 config | `TestAlertStore_UpsertWithDedup_Cooldown*`; `TestHandleAlertPersist_Cooldown_DropsEvent` | — |
| AC-6: UI shows occurrence_count | Task 6 | Manual + `pnpm build` | — |
| AC-7: Prometheus `argus_alerts_deduplicated_total{type}` | Task 4 metrics | `curl :8080/metrics \| grep argus_alerts_deduplicated_total`; integration smoke | Labels are `{type, source}` not just `{type}` — extra dimension for ops visibility, still bounded (§Metrics) |

### Tech Debt closed by this story

- **D-076 (FIX-209 Gate F-A9):** three alert-state enum definitions consolidated into `internal/alertstate` package. Verified via grep gate in Task 1, Task 3, Task 5.

---

## Story-Specific Compliance Rules

- **API:** `alertDTO` response shape gains 5 additive fields — no breaking change; no version bump. Error codes unchanged (`ALERT_NOT_FOUND`, `INVALID_STATE_TRANSITION`). `PATCH /alerts/{id}` contract preserved — `suppressed` still rejected via handler; admin suppression goes through `SuppressAlert` store method (not exposed on REST in this story).
- **DB:** Migration is transactional. Up + down scripts. Table is pre-release-empty → ALTER safe. RLS policy from FIX-209 unchanged.
- **UI:** ONLY `<SeverityBadge>` (FIX-211), `<Badge>` primitive with tokens. No hex. `frontend-design` skill invoked once at Task 6 start.
- **Business:** Alerts table remains the ONLY source of truth for non-SIM alert history. Dedup is transparent — operator sees "5× alert" not 5 alerts, but every occurrence is logged via `last_seen_at` + `occurrence_count`. Cooldown is a protection, not data loss — the Warn log + metric make it visible.
- **ADR:** No ADR changes.
- **Publisher discipline (PAT-006):** FIX-210 does NOT rewrite publisher payloads; it adds edge-triggering guards at two specific publishers. FIX-212 owns the full envelope unification. FIX-210's persist-level dedup is the safety net covering the other 5 publishers without touching their code.

---

## Bug Pattern Warnings

Consulted `docs/brainstorming/bug-patterns.md`:

- **PAT-006 [FIX-201] — shared payload field silently omitted:** DIRECTLY APPLIES. The `Alert` struct and `CreateParams` gain 4 new columns. Every scan-site, every SELECT, every test fixture must include them. Task 3 grep gate: `rg -n 'SELECT.*FROM alerts' internal/` — every hit must include the 4 new columns or use `*`. Task 6 grep: `rg -n 'Alert\s*=|Alert\{' web/src` — TypeScript strictness catches missing fields at compile.
- **PAT-011 [FIX-207] — plan-specified wiring missing at construction sites:** DIRECTLY APPLIES. `UpsertWithDedup` replaces `Create` at the persist path; if a future code path adds a new direct `alertStore.Create` call site (bypassing `handleAlertPersist`), it skips dedup entirely. Task 4 grep gate: `rg -n 'alertStore\.Create\(' internal/` must match ONLY the admin/rare-path usage OR be empty. Same for cooldown config threading: `cfg.AlertCooldownMinutes` must be read at ONE place and passed via parameter — Task 4 verify step checks `rg -n 'AlertCooldownMinutes' internal/` shows the config read + the handler receive site only.
- **PAT-015 [UI component re-invention]:** APPLIES to Task 6. The dedup count badge MUST use the existing `<Badge>` primitive with `variant="outline"`, not a bespoke component. The cooldown banner uses existing `<Alert>` (shadcn) component with muted variant. Grep gate Task 6: `rg -n 'className.*border.*rounded' web/src/pages/alerts/*.tsx` does not reveal new inline pill CSS.
- **PAT-016 [cross-store ID confusion]:** APPLIES but NARROW. `meta.anomaly_id` (FIX-209 linkage) is preserved on the dedup-merged row — BUT PostgreSQL `jsonb || jsonb` is **right-hand wins** on key collision (FIX-210 Gate F-A2 prose correction). So a naive `alerts.meta || EXCLUDED.meta` would overwrite the original `anomaly_id` with the incoming one. The actual implementation therefore explicitly pre-strips conflicting keys on the incoming side (or builds `EXCLUDED.meta || alerts.meta` if older-wins semantics are required). Task 4 verify asserts whichever semantics are implemented by the store survive the round-trip. Alternative: document that for sim-source alerts, dedup is at SIM level not anomaly level — one "SIM X policy violated" alert row dedups across 10 distinct anomaly detections of the same root cause. That is the intended behaviour.
- **PAT-003 [metric cardinality explosion]:** APPLIES to Task 4. Metrics labelled ONLY by `type` + `source` (bounded enum × enum). NO tenant_id, NO alert_id, NO UUIDs. Grep gate: `rg -n 'argus_alerts_.*\.WithLabelValues' internal/` — verify no UUID string passed.
- PAT-001 (double-writer): NOT APPLICABLE — `UpsertWithDedup` is still single-writer-per-store; it's one statement, not two
- PAT-002 (single-clock polling), PAT-004 (hypertable cardinality), PAT-005 (masked secrets), PAT-007–PAT-010, PAT-012: not relevant

---

## Risks & Mitigations

- **Risk 1 — Concurrent persist race on same `dedup_key`:** two workers INSERT within the same millisecond. **Mitigation:** `INSERT ... ON CONFLICT` is atomic at the DB level (the UNIQUE partial index guarantees exclusivity). Explicit regression test `TestAlertStore_UpsertWithDedup_ConcurrentHit` spins 10 goroutines; asserts exactly 1 row, count=10. No TOCTOU because cooldown check + INSERT are in the same transaction (either SERIALIZABLE via savepoint or ordered logic where cooldown SELECT-then-INSERT is outside transaction — acceptable because a new-event-within-cooldown is itself best-effort; racing with resolve is vanishingly rare).
- **Risk 2 — `fired_at` drift on dedup hit breaks pagination + retention:** (D7 blind spot). **Mitigation:** UPDATE clause explicitly does NOT touch `fired_at`. Regression test `TestAlertStore_UpsertWithDedup_SecondEventIncrements` asserts `fired_at` unchanged after second event.
- **Risk 3 — Metric cardinality explosion:** PAT-003. **Mitigation:** labels bounded to `type × source = ~250 combos max`. No UUID labels. Documented at §Metrics.
- **Risk 4 — D-076 refactor silently changes FIX-209 handler contract:** tests around PATCH rejection of `suppressed` or unknown filter values. **Mitigation:** Task 5 explicit contract-preservation rule + existing FIX-209 handler tests re-run unchanged (Task 5 verify step). If any FIX-209 test fails → D-076 consolidation broke something; fix at Task 5 before proceeding.
- **Risk 5 — Publisher edge-triggering loses a real incident:** e.g. two distinct down events that happen within 60s get collapsed. **Mitigation:** persist-level dedup at DB is the authoritative source; publisher edge-triggering is an optimization, not the dedup mechanism. If edge-trigger incorrectly skips a publish, the DB already knows about the active alert (row exists in state=open) so operator sees it. `last_seen_at` advances on publisher edge-trigger still emitting on status-flip (which does emit). Net effect: fewer wasted publishes, no missed incidents.
- **Risk 6 — Cooldown storms hide a genuinely recurring issue:** a flaky operator that resolves every 5min and re-fires instantly would be dedup'd for 5min then re-fire → looks like status is healthy when it's actually flapping. **Mitigation:** cooldown drop emits Warn log + metric (`argus_alerts_cooldown_dropped_total`). Ops runbook (step-log appendix) instructs: if this metric is non-trivial for a given operator, investigate flapping. Pre-release this is visibility, not automation.
- **Risk 7 — `meta` merge collision (PAT-016):** PostgreSQL `jsonb || jsonb` is **right-hand wins** on key collision (F-A2 prose correction — earlier draft incorrectly stated left-hand wins). So `alerts.meta || EXCLUDED.meta` would clobber the original `anomaly_id` with the incoming one. **Mitigation:** Task 4 explicit behaviour: on dedup UPDATE, append incoming `anomaly_id` (if different from existing) into `meta.also_triggered_by_anomaly_ids` array rather than rely on `||` ordering. Alternative accepted: for sim-source alerts, this is "collapse 10 anomaly detections into 1 alert row" — intentional and desirable.
- **Risk 8 — Concurrent state transition on resolve while new event arrives:** resolve commits at T; new event arrives at T+1ms; UPDATE's `WHERE` clause sees resolved row; ON CONFLICT partial index scope excludes resolved → fresh INSERT, cooldown check BYPASSES (cooldown_until not yet committed-visible to fresh txn). **Mitigation:** UPDATE of `UpdateState` to `resolved` commits `cooldown_until` in the SAME transaction → post-commit visible. `UpsertWithDedup` cooldown SELECT runs with `READ COMMITTED` (default) so sees committed data. Race narrows to sub-millisecond. If the race triggers, worst case is one extra INSERT that should've been cooldown'd — acceptable. Tracked as D-NNN if seen in prod.
- **Risk 9 — Migration breaks fresh seed:** per `feedback_no_defer_seed.md`. **Mitigation:** Task 2 explicit verify step `make down && make infra-up && make db-migrate && make db-seed` clean. Table empty pre-release; no data migration needed.

---

## Pre-Merge Gate Checklist (Planner Embedded Quality Gate)

Developer must check EVERY box before Amil gate:

- [ ] `go build ./...` passes
- [ ] `go vet ./...` clean
- [ ] `go test ./...` all packages PASS (0 failures) — count matches or exceeds pre-FIX-210 baseline (95 packages)
- [ ] New tests from Task 1, 3, 4 all pass
- [ ] `pnpm tsc --noEmit` in `web/` passes
- [ ] `pnpm build` in `web/` succeeds
- [ ] `make down && make infra-up && make db-migrate && make db-seed` succeeds on fresh volume
- [ ] `make down && make infra-up && migrate ... up; migrate ... down 1; migrate ... up` round-trips cleanly
- [ ] **Grep gate D-076 closed:** `rg -n 'validAlertStates|validAlertTransitions|allowedUpdateStates' internal/ | grep -v alertstate` returns zero hits (only alertstate package + imports)
- [ ] **Grep gate PAT-011:** `rg -n 'alertStore\.Create\(' internal/notification/` returns zero hits (persist path uses Upsert exclusively)
- [ ] **Grep gate PAT-003:** `rg -n 'argus_alerts_.*WithLabelValues.*uuid\|tenant' internal/metrics internal/` returns zero hits
- [ ] **Grep gate D3 regression:** `rg -n 'severity' internal/alertstate/dedup.go` returns zero hits (dedup algorithm does not reference severity)
- [ ] Integration smoke: run simulator emitting 10 identical operator-health alerts → `psql -c "SELECT occurrence_count FROM alerts WHERE dedup_key IS NOT NULL ORDER BY fired_at DESC LIMIT 1"` returns 10; exactly 1 row
- [ ] Integration smoke: resolve that row → `UPDATE state → resolved, cooldown_until set`; fire 11th event within 5min → metric `argus_alerts_cooldown_dropped_total` increments; no new row inserted
- [ ] Integration smoke: wait (or force expire) cooldown → fire 12th event → new row inserted (fresh incident)
- [ ] UI smoke: visit `/alerts` → dedup'd row shows `"10× in last 2m"` badge; detail page shows First/Last seen + cooldown banner where applicable
- [ ] ROUTEMAP updated: D-076 marked CLOSED in Tech Debt; FIX-210 slot reserved in UI Review Remediation track
- [ ] Story file (`FIX-210-alert-deduplication.md`) amended: AC-1 column name + AC-2 severity note + reference to plan's §Decisions
- [ ] Docs (ERROR_CODES, CONFIG, api, db indexes) updated per Task 7
- [ ] No `emoji` added to any file per `.claude/CLAUDE.md`

---

## FIX Mode Summary

- **Minimal surface:** 8 Go files modified (store, handler, service, health, enforcer, config, metrics, main) + 4 new Go files (alertstate package + 1 migration pair); 3 web files modified.
- **No new dependencies.** No ADR changes. No API breaking changes. No FE component re-design.
- **Clears D-076.** Does not introduce new tech debt; any dedup-related refinements (admin REST endpoint for `SuppressAlert`, channel-level dispatch dedup, partitioning, anomaly-engine-specific dedup) are tracked as D-NNN in ROUTEMAP if/when encountered during gate.
- **Blocked downstream stories unblocked after this lands:** FIX-213 (alert UX polish), FIX-215 (runbooks), FIX-229 (dashboard alert widgets).

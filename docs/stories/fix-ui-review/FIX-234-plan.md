# Implementation Plan: FIX-234 — CoA Status Enum Extension + Idle SIM Handling + UI Counters

> Spec: `docs/stories/fix-ui-review/FIX-234-coa-status-enum-extension.md`
> Effort: S · Priority: P2 · Wave 6 (UI Review Remediation)
> Dependencies: FIX-209 (alerts table — DONE), FIX-212 (`session.started` event — DONE), FIX-232 (`RolloutActivePanel` — DONE), FIX-233 (`coa_status` DTO field — DONE)

---

## Goal

Replace the permanent-`pending` failure mode for CoA delivery against idle SIMs with an explicit lifecycle (`pending → queued → acked|failed|no_session|skipped`), automatically re-fire CoA when an idle SIM next authenticates, surface the breakdown in the rollout panel + SIM detail, and trigger a `coa_delivery_failed` alert when failures persist.

## Architecture Reference

- **DB column:** `policy_assignments.coa_status` — defined in `migrations/20260320000002_core_schema.up.sql:352` as `VARCHAR(20) DEFAULT 'pending'`. **There is NO Postgres ENUM type and NO CHECK constraint today.** AC-1 phrasing "extend the existing enum" is interpreted as: add a `CHECK` constraint to *enforce* the canonical set during this story.
- **CoA dispatcher contract:** `internal/policy/rollout/service.go:32` `CoARequest` / `CoAResult` / `CoADispatcher` interface; `sendCoAForSIM` at line 520.
- **CoA dispatch invocation:** Today only the rollout stage executor calls `sendCoAForSIM` (lines 313, 462). FIX-234 adds a second caller: a `session.started` subscriber re-firing CoA for idle SIMs that already have a pending assignment.
- **Active sessions lookup:** `internal/aaa/session/session.go:590` `Manager.GetSessionsForSIM(ctx, simID) → []*Session`. The rollout service consumes this through the local `sessionProvider` interface (rollout/service.go:47).
- **Status update path:** `internal/store/policy.go:1209` `PolicyStore.UpdateAssignmentCoAStatus(ctx, simID, status)` — single hop write that also touches `coa_sent_at = NOW()`. Currently called only from `sendCoAForSIM`.
- **`coa_sent_at` column:** Already present (migration line 354, `TIMESTAMPTZ` nullable). AC-4 dedup reuses it; **no schema change needed for dedup**.
- **Event subscriber pattern:** `internal/policy/matcher.go:36` shows the canonical `EventBus.QueueSubscribeCtx(SubjectSessionStarted, "<group>", handler)` shape. FIX-234 adds a sibling subscriber group `"rollout-coa-resend"` to keep concerns separate (the matcher mutates assignments; FIX-234 only reads them and dispatches CoA).
- **Alert creation:** `internal/store/alert.go:135` `AlertStore.Create(ctx, CreateAlertParams{...})` — used by `internal/notification/service.go` envelope path. Background-job pattern in `internal/job/stuck_rollout_reaper.go` is the template for AC-7.
- **Prometheus registry:** `internal/observability/metrics/metrics.go:138+` — `prometheus.NewGaugeVec` is the established factory. Existing similar gauge: `OperatorHealth` at line 238.
- **Rollout panel UI:** `web/src/components/policy/rollout-active-panel.tsx:23` — `RolloutCoaCounts` type currently has only `acked` + `failed`. **Important: no caller currently populates `coaCounts`** — `web/src/components/policy/rollout-tab.tsx:462` passes only `rollout`. AC-5 is a vertical slice (store → API → hook → panel).
- **SIM detail UI:** `web/src/pages/sims/detail.tsx:155-175` — "Policy & Session" Card. Today it shows policy name + version, eSIM profile, timeouts. **Zero `coa_status` references.** AC-6 adds a new `InfoRow` here despite FIX-233 already plumbing the DTO field into the SIM list (`internal/api/sim/handler.go:173,298`, `internal/store/sim.go:1390`).

### Components Involved

| Component | Layer | Path | Responsibility |
|---|---|---|---|
| `policy_assignments` table | DB | `migrations/` | `coa_status` column + new CHECK constraint |
| `PolicyStore` | data access | `internal/store/policy.go` | `UpdateAssignmentCoAStatus` extension + new `GetCoAStatusCountsByRollout` |
| `rollout.Service.sendCoAForSIM` | service | `internal/policy/rollout/service.go` | Status propagation: empty sessions → `no_session`; sessionProvider/dispatcher nil → `no_session` |
| `coaSessionResender` | service (NEW) | `internal/policy/rollout/coa_session_resender.go` | `session.started` subscriber that fires CoA for assignments with `coa_status='no_session'` and not recently sent |
| `coaFailureAlerter` | job (NEW) | `internal/job/coa_failure_alerter.go` | Periodic sweep — emit `coa_delivery_failed` alert when `coa_status='failed' AND coa_sent_at < NOW() - 5min` |
| `metrics.CoAStatusByState` | observability | `internal/observability/metrics/metrics.go` | `argus_coa_status_by_state{state}` GaugeVec, refreshed by alerter sweep |
| Active rollout API | gateway | `internal/api/policy/handler.go` | Extend `GetRollout`/active-rollout response payload with `coa_counts` |
| `RolloutActivePanel` | UI organism | `web/src/components/policy/rollout-active-panel.tsx` | Render 6-state breakdown |
| `rollout-tab.tsx` | UI tab | `web/src/components/policy/rollout-tab.tsx` | Pass `coaCounts` down from `useRollout` payload |
| SIM detail Policy card | UI page | `web/src/pages/sims/detail.tsx` | New `InfoRow` showing `coa_status` + last-attempt timestamp + failure reason |
| PROTOCOLS doc | docs | `docs/architecture/PROTOCOLS.md` | CoA section status-lifecycle ASCII diagram |

### Data Flow

#### A. Stage CoA dispatch (existing — extended in this story)

```
Operator clicks Advance Stage in UI
  → POST /policy-rollouts/{id}/advance
    → rollout.Service.executeStage
      → store.AssignSIMsToVersion (writes pending rows)
      → for each simID: sendCoAForSIM(ctx, simID)         [extended]
        → sessionProvider.GetSessionsForSIM(simID)
          if sessions == nil/empty:
            policyStore.UpdateAssignmentCoAStatus(simID, 'no_session')   # NEW
            return
          for each session:
            coaDispatcher.SendCoA(...)
            → success → 'acked' / failure → 'failed' / mid-flight → 'queued'
            → policyStore.UpdateAssignmentCoAStatus(simID, status)
      → publishProgress
```

#### B. Idle SIM re-fire (NEW — AC-4)

```
RADIUS Access-Accept fires
  → server.go:879 publishes bus.NewSessionEnvelope("session.started", …)
  → NATS topic argus.events.session.started
    ┌── existing subscriber: policy.Matcher (re-evaluates assignment)
    └── NEW subscriber: coaSessionResender (group "rollout-coa-resend")
         → extract tenantID, simID from envelope
         → policyStore.GetAssignmentBySIM(simID)
           if assignment.coa_status == 'no_session'
              AND (assignment.coa_sent_at IS NULL OR coa_sent_at < NOW() - 60s):
              → rollout.Service.sendCoAForSIM(ctx, simID)
                → status transitions to 'queued' → 'acked'/'failed'
```

Dedup window: **60 seconds** of `coa_sent_at` (covers concurrent multi-session bursts on the same SIM without spamming CoA).

#### C. Failure alerter (NEW — AC-7)

```
JobScheduler fires "coa_failure_alerter" every 60s
  → store.PolicyStore.ListStuckCoAFailures(NOW() - 5min)
    → returns rows where coa_status='failed' AND coa_sent_at < NOW() - 5min AND no open alert dedup_key matches
  → for each: alertStore.UpsertWithDedup(CreateAlertParams{
       Type: "coa_delivery_failed", Severity: "high",
       Source: "rollout", DedupKey: ptr("coa_failed:" + sim_id), … })
  → also refreshes metrics.CoAStatusByState gauge per state
```

### API Specifications

#### Extension to existing rollout endpoints

`GET /policy-rollouts/{id}` and `GET /policy-rollouts/active` — response envelope `data` shape gains a `coa_counts` block:

```jsonc
{
  "status": "success",
  "data": {
    "id": "...",
    "policy_version_id": "...",
    "state": "in_progress",
    "stages": [...],
    "total_sims": 100,
    "migrated_sims": 80,
    "coa_counts": {                         // NEW (FIX-234 AC-5)
      "pending": 0,
      "queued": 2,
      "acked": 73,
      "failed": 1,
      "no_session": 4,
      "skipped": 0
    }
  }
}
```

`coa_counts` is OPTIONAL in the schema (omit if rollout has no assignments yet). All six fields are non-negative integers. Sum must equal the count of `policy_assignments` rows in the rollout — store query enforces.

No new endpoints. No request shape changes. No auth changes.

### Database Schema

```sql
-- Source: migrations/20260320000002_core_schema.up.sql:344-358 (ACTUAL)
CREATE TABLE IF NOT EXISTS policy_assignments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sim_id UUID NOT NULL REFERENCES sims(id),
    policy_version_id UUID NOT NULL REFERENCES policy_versions(id),
    rollout_id UUID REFERENCES policy_rollouts(id),
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    coa_sent_at TIMESTAMPTZ,
    coa_status VARCHAR(20) DEFAULT 'pending'
    -- stage_pct INTEGER NULL    (added by FIX-233 / 20260429000001)
);
CREATE INDEX IF NOT EXISTS idx_policy_assignments_coa
  ON policy_assignments (coa_status) WHERE coa_status != 'acked';
```

#### FIX-234 migration `20260430000001_coa_status_enum_extension.{up,down}.sql`

UP (forward):
1. **Reclassify pre-existing rows** — convert `pending` rows that have no active session to `no_session`. Single SQL using a NOT EXISTS subquery against `sessions` table (or the AAA equivalent — Dev verifies the actual table during W1):
   ```sql
   UPDATE policy_assignments pa
      SET coa_status = 'no_session'
    WHERE pa.coa_status = 'pending'
      AND NOT EXISTS (
        SELECT 1 FROM sessions s
         WHERE s.sim_id = pa.sim_id AND s.state IN ('active','accounting'));
   ```
   *Dev MUST run* `psql -c "\d sessions"` *first to confirm column names; substitute as needed. Comment the SQL with the verification step.*
2. **Add CHECK constraint enforcing the 6-value enum:**
   ```sql
   ALTER TABLE policy_assignments
     ADD CONSTRAINT chk_coa_status
     CHECK (coa_status IN ('pending','queued','acked','failed','no_session','skipped'));
   ```
3. **Reuse `idx_policy_assignments_coa`** — already covers WHERE `coa_status != 'acked'`. Sufficient for alerter sweep.
4. **Add covering index** for FIX-234 alerter sweep:
   ```sql
   CREATE INDEX IF NOT EXISTS idx_policy_assignments_coa_failed_age
     ON policy_assignments (coa_sent_at)
     WHERE coa_status = 'failed';
   ```

DOWN (reverse):
1. `DROP INDEX IF EXISTS idx_policy_assignments_coa_failed_age;`
2. `ALTER TABLE policy_assignments DROP CONSTRAINT IF EXISTS chk_coa_status;`
3. **Do NOT roll back row reclassification** — `no_session` is information-bearing; reverting it to `pending` would lose state. Comment this in the down file.

#### Seed update

- `migrations/seed/003_comprehensive_seed.sql:561,1434` — both `INSERT INTO policy_assignments (... coa_status)` lines must use only the canonical 6 values. Audit and fix any row that uses an out-of-set string. **Per project memory `feedback_no_defer_seed.md`: do NOT defer — `make db-seed` must remain clean.**

### Status Lifecycle State Machine (canonical — embed in PROTOCOLS.md)

```
                ┌─────────┐
        insert  │         │
       ───────► │ pending │
                │         │
                └────┬────┘
        sendCoA      │ sendCoAForSIM dispatched
        triggered    ▼
                ┌─────────┐
                │ queued  │  (in-flight, dispatcher accepted but not yet acked)
                └─┬─────┬─┘
        ack       │     │  failure / non-ack response
                  ▼     ▼
              ┌─────┐ ┌────────┐
              │acked│ │ failed │ ──── sweep > 5min ──► alert(coa_delivery_failed)
              └─────┘ └────────┘
                          │ retry succeeds
                          ▼
                       acked

   Edge transitions from pending:
     pending ──(no active sessions)──► no_session
                  │ session.started for SIM (dedup 60s)
                  ▼
              (sendCoAForSIM)
                  ▼
                queued → acked / failed

     pending ──(policy rule: low-priority change)──► skipped  (terminal)
```

### Screen References

#### `RolloutActivePanel` "CoA Acks" tile — extension

Current layout (lines 277-294 of rollout-active-panel.tsx) shows 2 numbers: `acked · failed`. After FIX-234:

```
┌────────────────────────────────────────────┐
│  COA STATUS                                │
│  73 acked · 2 queued · 4 no-session        │
│  · 1 failed · 0 skipped                    │
│  [────────────────████─────] (color stack) │
└────────────────────────────────────────────┘
```

Order matches AC-2: `pending | queued | acked | failed | no_session | skipped`. Display order optimized for operator scanning (success first, failure last). `pending` count omitted when 0 (transient state — not interesting unless something stuck).

#### SIM detail Policy & Session card — extension at line 159 of detail.tsx

```
┌─ Policy & Session ───────────────────────┐
│  Policy            Premium IoT (v3)       │
│  CoA Status        ✓ acked · 2m ago       │  ← NEW (FIX-234 AC-6)
│  eSIM Profile      …                      │
│  Max Concurrent…   1                      │
│  Idle Timeout      30m                    │
│  Hard Timeout      24h                    │
└──────────────────────────────────────────┘
```

States (text-token mapping, see Design Token Map):
- `pending` → `text-text-tertiary` "pending"
- `queued`  → `text-warning` "queued"
- `acked`   → `text-success` "acked · {timeAgo}"
- `failed`  → `text-danger` "failed · {reason}" (tooltip on hover)
- `no_session` → `text-text-secondary` "no active session"
- `skipped` → `text-text-tertiary` "skipped"

### Design Token Map (UI tasks)

#### Color Tokens (existing, used as-is)
| Usage | Token Class | Never |
|---|---|---|
| acked text | `text-success` | `text-green-600`, `text-[#10b981]` |
| failed text | `text-danger`  | `text-red-600`, `text-[#ef4444]` |
| queued text | `text-warning` | `text-amber-500` |
| pending/no_session/skipped (muted) | `text-text-tertiary` / `text-text-secondary` | `text-gray-400` |
| Card border | `border-border-subtle` | `border-gray-200` |
| Card bg | `bg-bg-surface` | `bg-white` |
| Stack-bar segments | `bg-success`, `bg-warning`, `bg-danger`, `bg-text-tertiary` | hex literals |

#### Typography
| Usage | Token Class |
|---|---|
| Tile label | `text-[10px] font-medium text-text-tertiary uppercase tracking-wider` (matches existing tile in panel — preserve) |
| Tile value | `font-mono text-xs text-text-primary` |

#### Existing Components to REUSE
| Component | Path | Use For |
|---|---|---|
| `<Badge>` | `web/src/components/ui/badge.tsx` | optional small status pills next to `coa_status` |
| `<Card>`, `<CardContent>`, `<CardHeader>` | `web/src/components/ui/card.tsx` | already used in detail.tsx |
| `<InfoRow>` | local helper inside `web/src/pages/sims/detail.tsx` | adding the new CoA Status row |
| `timeAgo` | `web/src/lib/format.ts` | already used in panel |
| `<Tooltip>` (if exists) | `web/src/components/ui/` | failure reason tooltip — Dev verifies, otherwise plain `title=` attr |

**RULE: Zero hex literals, zero `text-gray-N`, zero `bg-white`. Run `grep -nE '#[0-9a-fA-F]{3,6}|gray-|text-\\[#' web/src/components/policy/rollout-active-panel.tsx web/src/pages/sims/detail.tsx` after changes — must show zero new matches.**

---

## Acceptance Criteria Mapping

| AC | Description | Implemented in Task | Verified by |
|----|------|------|------|
| AC-1 | DB migration adding `queued`, `no_session`, `skipped` (via CHECK constraint enforcement) | T1 | T1 verify + T7 unit test |
| AC-2 | Final canonical enum is `{pending, queued, acked, failed, no_session, skipped}` | T1 + T7 | DB CHECK constraint + Go const set |
| AC-3 | `sendCoAForSIM` writes `no_session` for empty session list / nil providers; `queued` for in-flight; `acked` / `failed` per dispatch result | T2 | T7 unit test (4 scenarios) |
| AC-4 | `session.started` subscriber re-fires CoA for SIMs with `coa_status='no_session'`; dedup via 60s `coa_sent_at` window | T3 | T7 integration test |
| AC-5 | `RolloutActivePanel` shows 6-state breakdown with color-coded bar; backend → API → hook → panel pipeline wired | T4 + T5 | T8 web tsc + manual rollout panel screenshot |
| AC-6 | SIM detail Policy & Session card shows `coa_status` + last attempt + failure reason tooltip | T6 | T8 web tsc + manual nav |
| AC-7 | Background job `coa_failure_alerter`: `failed > 5min` → `alert(type=coa_delivery_failed, severity=high)` with dedup | T3b | T7 unit test (job sweep) |
| AC-8 | Prometheus gauge `argus_coa_status_by_state{state}` registered + refreshed by alerter sweep | T3b | `curl /metrics | grep argus_coa_status_by_state` |
| AC-9 | `docs/architecture/PROTOCOLS.md` CoA section gains the lifecycle diagram | T9 | grep + diff |

---

## Wave Plan & Tasks

> 9 tasks across **3 waves**. Wave boundaries respect dependencies. Within a wave, tasks are independent and parallelizable.

### Wave 1 — DB foundation + service status propagation

#### Task 1: Migration + CHECK constraint + row reclassification + seed audit
- **Files (3):** create `migrations/20260430000001_coa_status_enum_extension.up.sql`, create `migrations/20260430000001_coa_status_enum_extension.down.sql`, modify `migrations/seed/003_comprehensive_seed.sql` if any out-of-set values found.
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `migrations/20260427000002_reconcile_policy_assignments.up.sql` for the in-migration row-fix-up pattern, and `migrations/20260429000001_policy_assignments_stage_pct.up.sql` for the column-extension pattern.
- **Context refs:** "Database Schema", "Risks & Mitigations" (Risk 1).
- **What:**
  - Verify the actual `sessions` table column names (`\d sessions`) before writing the reclassification UPDATE. Document the verified state filter in a SQL comment.
  - UP step 1: `UPDATE … SET coa_status='no_session' WHERE coa_status='pending' AND NOT EXISTS (active sessions)`.
  - UP step 2: `ALTER TABLE policy_assignments ADD CONSTRAINT chk_coa_status CHECK (...)`.
  - UP step 3: `CREATE INDEX … idx_policy_assignments_coa_failed_age`.
  - DOWN: drop index + drop constraint. Comment that reclassification is intentionally NOT reversed.
  - Audit `migrations/seed/003_comprehensive_seed.sql` for any `coa_status` value outside `{pending,queued,acked,failed,no_session,skipped}` and fix in place. **Per project memory `feedback_no_defer_seed.md`: `make db-seed` must remain clean.**
- **Verify:** `make db-migrate && make db-seed` — both clean. Then `psql -c "SELECT DISTINCT coa_status FROM policy_assignments"` — must return only canonical values.

#### Task 2: `sendCoAForSIM` status propagation + Go const set
- **Files (2):** modify `internal/policy/rollout/service.go`, create `internal/policy/rollout/coa_status.go` (small const-set file).
- **Depends on:** T1
- **Complexity:** medium
- **Pattern ref:** Read existing `internal/policy/rollout/service.go:520-559` for the function. Read `internal/audit/types.go` (or similar) for the const-grouping idiom used in the project.
- **Context refs:** "Architecture Reference", "Components Involved", "Status Lifecycle State Machine".
- **What:**
  - New file `coa_status.go` exposing `const CoAStatusPending = "pending"` … `const CoAStatusSkipped = "skipped"` and a `var CoAStatusAll = []string{...}` for use in tests / metric label seeding. Order MUST match the AC-2 list.
  - Modify `sendCoAForSIM` (line 520):
    - At top, if `s.sessionProvider == nil || s.coaDispatcher == nil` — currently silently returns. Change to: write `CoAStatusNoSession` via `policyStore.UpdateAssignmentCoAStatus` and return.
    - After `GetSessionsForSIM`: if `sessions` is empty → write `CoAStatusNoSession` and return.
    - Before the dispatch loop, write `CoAStatusQueued` to mark in-flight.
    - Inside the loop, replace the local `status` string with `CoAStatusAcked` / `CoAStatusFailed` from the const set. The "sent" / "ack" intermediate variants in the current code are dead — remove.
- **Verify:** `go build ./... && go vet ./... && go test ./internal/policy/rollout/...`. Existing tests in `service_test.go` already cover `NoSessions` + `WithSessions` — extend rather than break.

### Wave 2 — Idle SIM re-fire subscriber + alerter + metrics + API extension

#### Task 3: `coaSessionResender` subscriber on `session.started`
- **Files (2):** create `internal/policy/rollout/coa_session_resender.go`, create `internal/policy/rollout/coa_session_resender_test.go`.
- **Depends on:** T2
- **Complexity:** high (concurrency-sensitive; race-condition mitigation per Risk 2)
- **Pattern ref:** Read `internal/policy/matcher.go` (entire file — 100 lines). Mirror its `Register` + `extractTenantAndSIM` + `evaluate` shape. Read `internal/aaa/session/counter.go` for the queue-group naming convention.
- **Context refs:** "Architecture Reference", "Data Flow > B. Idle SIM re-fire (NEW — AC-4)", "Risks & Mitigations" (Risk 2).
- **What:**
  - `type Resender struct { policyStore, rolloutSvc, logger ... }`.
  - `Register(eb *bus.EventBus) error` calls `eb.QueueSubscribeCtx(bus.SubjectSessionStarted, "rollout-coa-resend", r.handle)`.
  - `handle(ctx, _, data)`: extract tenantID + simID via the **same** envelope/legacy fallback as `matcher.go::extractTenantAndSIM` (copy-paste — that function is already a documented pattern; D-078 grace).
  - Read assignment via a new store method `GetAssignmentBySIMForResend(ctx, simID)` that returns `coa_status` + `coa_sent_at`. (Add the method in the same task.)
  - Skip unless `coa_status == 'no_session'` AND (`coa_sent_at IS NULL` OR `coa_sent_at < NOW() - 60s`).
  - Wire to `rolloutSvc.SendCoAForSIM(ctx, simID)` — the rollout service exposes the unexported helper through a thin public wrapper added in this task: `func (s *Service) ResendCoA(ctx, simID)`.
  - Tests cover: (a) handler skips when status != no_session, (b) handler skips when within 60s window, (c) handler dispatches when stale window + no_session, (d) handler tolerates malformed envelope (logs and returns).
- **Verify:** `go test ./internal/policy/rollout/...` — new test count > 0 added. `grep -n 'rollout-coa-resend' internal/` confirms queue group.

#### Task 3b: Failure alerter background job + Prometheus gauge
- **Files (3):** create `internal/job/coa_failure_alerter.go`, create `internal/job/coa_failure_alerter_test.go`, modify `internal/observability/metrics/metrics.go` (add `CoAStatusByState *prometheus.GaugeVec`).
- **Depends on:** T1
- **Complexity:** high (multi-system: store query + alert dedup + metric registration + scheduler wiring)
- **Pattern ref:** Read `internal/job/stuck_rollout_reaper.go` end-to-end — same job shape (sweep → log → publish job result). Read `internal/observability/metrics/metrics.go:238-244` (`OperatorHealth`) for the GaugeVec registration pattern. Read `internal/notification/service.go:880` for `CreateAlertParams` construction.
- **Context refs:** "Architecture Reference", "Data Flow > C. Failure alerter (NEW — AC-7)", "Components Involved".
- **What:**
  - Add new job type const `JobTypeCoAFailureAlerter = "coa_failure_alerter"` in `internal/job/types.go`. Add to the registered list at line 71.
  - `coa_failure_alerter.go`: process function takes `policyStore`, `alertStore`, `metricsRegistry`, `logger`. Sweep:
    1. Query `coa_status='failed' AND coa_sent_at < NOW() - 5min`. New store method `ListStuckCoAFailures(ctx, age) → []StuckCoAFailure{TenantID, SimID, FailedAt}`.
    2. For each row: `alertStore.UpsertWithDedup(CreateAlertParams{Type:"coa_delivery_failed", Severity:"high", Source:"rollout", DedupKey: ptr("coa_failed:" + simID), TenantID, SimID, Title:"CoA delivery failed", Description: "..."}, severityOrdinalHigh)`.
    3. Refresh the gauge: per state, `CoAStatusByState.WithLabelValues(state).Set(count)` after a `SELECT coa_status, COUNT(*) … GROUP BY coa_status` query (new store method `CoAStatusCounts(ctx) → map[string]int64` — tenant-aggregated; tenant scope is project-wide for system metrics — confirm convention with existing `OperatorHealth` gauge).
  - Sweep cadence: every 60 seconds.
  - Dedup window matches existing alert TTL (FIX-209 alert dedup_key default).
  - Test covers: (a) failed >5min creates alert, (b) failed <5min creates nothing, (c) repeat sweep within dedup window does not create duplicate alert, (d) gauge counts equal DB counts.
- **Verify:** `go test ./internal/job/...`, `go test ./internal/observability/...`. After dispatch in dev: `curl http://localhost:8080/metrics | grep argus_coa_status_by_state` — must show 6 label rows.

#### Task 4: Store method + active-rollout API extension for `coa_counts`
- **Files (2):** modify `internal/store/policy.go` (add `GetCoAStatusCountsByRollout`), modify `internal/api/policy/handler.go` (extend rollout response payload).
- **Depends on:** T1
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/policy.go:1219-1240` (`GetAssignmentsByRollout`) for the rollout-scoped query shape. Read existing rollout handler response builder in `internal/api/policy/handler.go` (search for `RolloutResponse` or `getRolloutHandler`).
- **Context refs:** "API Specifications", "Database Schema", "Components Involved".
- **What:**
  - Store method:
    ```go
    func (s *PolicyStore) GetCoAStatusCountsByRollout(ctx, rolloutID) (map[string]int, error)
    ```
    SQL: `SELECT coa_status, COUNT(*) FROM policy_assignments WHERE rollout_id=$1 GROUP BY coa_status`. Return map seeded with all 6 states at 0 (so JSON always carries the full set).
  - API handler: in the response builder for both `GET /policy-rollouts/{id}` and `GET /policy-rollouts/active`, populate a new `CoaCounts map[string]int` field on the response struct. JSON tag: `"coa_counts,omitempty"` — only omit when the rollout has zero assignments (not for in-progress with all-zero non-acked counts).
- **Verify:** `go test ./internal/store/... ./internal/api/policy/...`. Manual: `curl /policy-rollouts/{active_id}` returns the new block.

### Wave 3 — UI integration + docs

#### Task 5: `RolloutActivePanel` 6-state breakdown
- **Files (2):** modify `web/src/components/policy/rollout-active-panel.tsx`, modify `web/src/components/policy/__tests__/rollout-active-panel.test.tsx`.
- **Depends on:** T4
- **Complexity:** medium
- **Pattern ref:** Existing `RolloutCoaCounts` interface and CoA tile (lines 23-25, 277-294 of the file). Existing test at `__tests__/rollout-active-panel.test.tsx` provides the type-smoke pattern to extend.
- **Context refs:** "Screen References > RolloutActivePanel", "Design Token Map", "API Specifications".
- **What:**
  - Extend `RolloutCoaCounts`: `{ pending: number; queued: number; acked: number; failed: number; no_session: number; skipped: number }`.
  - Replace the 2-number string render with a structured 6-segment list. Color tokens per Design Token Map. Optional inline stacked progress bar — segments computed as `segment.count / total * 100`.
  - Hide segments with count 0 EXCEPT acked/failed (always show — they are the high-signal states).
  - Update existing test to pass full 6-key fixture; assert nothing renders raw hex.
- **Tokens:** Use ONLY classes from Design Token Map — zero hardcoded hex/px.
- **Components:** Reuse atoms/molecules already imported in the file.
- **Note:** Invoke `frontend-design` skill if visual polish exceeds straight refactor.
- **Verify:** `cd web && pnpm tsc --noEmit && pnpm build`. `grep -nE '#[0-9a-fA-F]{3,6}' web/src/components/policy/rollout-active-panel.tsx` → zero matches.

#### Task 6: SIM detail Policy & Session card — CoA Status row
- **Files (1):** modify `web/src/pages/sims/detail.tsx`.
- **Depends on:** —  (consumes existing `sim.coa_status` DTO field plumbed by FIX-233)
- **Complexity:** low
- **Pattern ref:** Existing InfoRow at lines 159-170 of the same file.
- **Context refs:** "Screen References > SIM detail", "Design Token Map".
- **What:**
  - Add a new `<InfoRow label="CoA Status" value={…} />` BELOW the existing "Policy" row inside the "Policy & Session" Card.
  - Renderer maps the 6 states to text + color token per Design Token Map. For `failed`, append " · " + failure reason (from a new optional `coa_failure_reason` DTO field — if backend doesn't carry yet, fall back to a `title=` tooltip on the row text reading "See policy events"; mark as Open Question if extension is non-trivial).
  - For `acked`/`failed`/`no_session`, if `sim.coa_sent_at` is exposed in the DTO, append " · {timeAgo}" — verify field presence first; if absent, plan accepts it and Dev adds DTO field in this task (one-line additions in `internal/api/sim/handler.go` + `internal/store/sim.go`).
- **Tokens:** zero hex, no `text-gray-N`, no raw `<input>`/`<button>`.
- **Verify:** `cd web && pnpm tsc --noEmit && pnpm build`. Manual: visit `/sims/{id}` for a SIM with each of the 6 states (use seed data after migration).

#### Task 7: Backend test pass — enum, status propagation, subscriber, alerter
- **Files (1-2):** modify `internal/policy/rollout/service_test.go` (add new scenario tests), additions made in T2/T3/T3b are already inline.
- **Depends on:** T2, T3, T3b, T4
- **Complexity:** medium
- **Pattern ref:** Existing `service_test.go::TestSendCoAForSIM_NoSessions` and `_WithSessions` for the table-driven harness.
- **Context refs:** "Acceptance Criteria Mapping", "Status Lifecycle State Machine", "Risks & Mitigations".
- **What:**
  - 4 scenarios for AC-3: nil providers → no_session; empty sessions → no_session; dispatch ok → acked; dispatch fail → failed; mid-flight (mock that delays) → queued at intermediate write.
  - 1 scenario for AC-2 (CHECK constraint): `psql` integration — try to update a row to `'invalid_state'` and assert the constraint rejects.
  - 1 scenario for AC-7 dedup: alerter sweep called twice within 5 min for the same SIM creates 1 alert row, not 2.
  - Confirm all existing tests in the rollout package still pass (`TestSendCoAForSIM_NilProviders` may need an update to assert the new no_session write).
- **Verify:** `go test ./internal/policy/rollout/... ./internal/job/... -count=1`. All green. Test count ≥ pre-T7 + 6.

#### Task 8: Web build + lint smoke + integration sanity
- **Files (0):** no file changes; runs only.
- **Depends on:** T5, T6
- **Complexity:** low
- **Context refs:** —
- **What:**
  - Run `cd web && pnpm tsc --noEmit && pnpm build`. Zero TS errors.
  - Run `grep -nE '#[0-9a-fA-F]{3,6}|text-gray-' web/src/components/policy/rollout-active-panel.tsx web/src/pages/sims/detail.tsx` — zero new matches (PAT-018 guard).
  - `make up`, navigate to `/policies/<id>/rollout` for an in-progress rollout, screenshot the panel, verify breakdown.
  - Navigate to `/sims/<id>` for a SIM in each of the 6 states (seed produces ≥1 of each), verify CoA Status InfoRow renders correctly.
- **Verify:** Screenshots attached to step log. Web build passes.

#### Task 9: Update `docs/architecture/PROTOCOLS.md` CoA section
- **Files (1):** modify `docs/architecture/PROTOCOLS.md` (insert at line 92 — under the existing CoA/DM heading).
- **Depends on:** —  (can run in parallel with anything in W3)
- **Complexity:** low
- **Pattern ref:** Existing PROTOCOLS.md formatting (ASCII diagrams already in use at lines 101-106 for RADIUS flow).
- **Context refs:** "Status Lifecycle State Machine", "Acceptance Criteria Mapping".
- **What:**
  - Insert a new subsection "### CoA Status Lifecycle (FIX-234)" after line 106 of PROTOCOLS.md.
  - Embed the ASCII state-machine from this plan's "Status Lifecycle State Machine" section verbatim.
  - One paragraph above the diagram: definitions of each of the 6 states + when each is written.
  - One paragraph below: dedup rule (60s window) + alerter rule (failed > 5min → high alert) + metric reference (`argus_coa_status_by_state{state}`).
- **Verify:** `grep -n 'CoA Status Lifecycle' docs/architecture/PROTOCOLS.md` returns exactly one match.

---

## Risks & Mitigations

### Risk 1 — Migration row reclassification correctness
**Risk:** UPDATE step in T1 mis-classifies rows because the actual `sessions` table column or state filter doesn't match the comment-in-spec assumption.
**Mitigation:**
- T1 is required to run `\d sessions` first and document the verified state filter inline in the migration SQL.
- Reclassification is **idempotent** (UPDATE with WHERE clause) and **non-destructive** for non-pending rows.
- DOWN migration intentionally does NOT reverse the row update — `no_session` is information-bearing; no rollback regret.
- Rollback path on prod: if reclassification misfires, an operator can manually `UPDATE policy_assignments SET coa_status='pending' WHERE coa_status='no_session' AND <criteria>`. Document in the migration's UP comment.

### Risk 2 — `session.started` CoA fire race / duplicate dispatch
**Risk:** Multiple concurrent sessions for the same SIM, or a single session re-published due to NATS at-least-once, fire duplicate CoAs.
**Mitigation:**
- Dedup via existing `policy_assignments.coa_sent_at` column with a **60-second** window (chosen here; see Open Question OQ-1 for tunability).
- Subscriber checks `coa_sent_at IS NULL OR coa_sent_at < NOW() - 60s` before dispatching.
- The status update inside `sendCoAForSIM → UpdateAssignmentCoAStatus` already sets `coa_sent_at = NOW()` (existing behavior at `internal/store/policy.go:1209`).
- Queue group `"rollout-coa-resend"` ensures single-consumer semantics across replicas.

### Risk 3 — Prometheus metric cardinality
**Risk:** Gauge labels explode if combined with `tenant_id` or `sim_id`.
**Mitigation:**
- Gauge `argus_coa_status_by_state{state}` carries ONLY `state` as a label — bounded set of 6 values. Aggregated across tenants by design (matches `OperatorHealth` precedent).
- No per-tenant or per-sim gauge.

### Risk 4 — FIX-232 panel regression
**Risk:** Reshaping `RolloutCoaCounts` from `{acked, failed}` → 6-key object breaks the existing FIX-232 tsc-smoke test and the hidden assumption that callers pass undefined.
**Mitigation:**
- T5 explicitly updates `__tests__/rollout-active-panel.test.tsx` fixtures to the 6-key shape.
- The optional `?` on `coaCounts` in the `Props` is preserved — existing caller (`rollout-tab.tsx`) that passes nothing remains valid until T4 wires the data flow.

### Risk 5 — Seed pollution
**Risk:** Seed file uses out-of-set values (e.g. `'sent'` from the dead-code path in `sendCoAForSIM`) and breaks the new CHECK constraint.
**Mitigation:**
- T1 audit explicitly reads both seed insert sites (lines 561, 1434) and fixes any out-of-set value.
- Per project memory `feedback_no_defer_seed.md`: this is **not** deferred. `make db-seed` must be clean before T1 closes.

### Risk 6 — Migration on existing rows reclassifying too aggressively
**Risk:** A row that *should* still be `pending` (just inserted seconds before migration runs) gets demoted to `no_session`.
**Mitigation:**
- The reclassification predicate also requires `assigned_at < NOW() - 1 minute` to avoid race with concurrent rollout activity. Add this clause in T1.

---

## Test Plan

### Unit (Go)
- `service_test.go` — 5 scenarios for status propagation (AC-3): nil providers, empty sessions, single-session ack, single-session fail, multi-session mixed.
- `service_test.go` — verifies `sendCoAForSIM` never writes `pending` (transient-only invariant).
- `coa_session_resender_test.go` — 4 scenarios for AC-4: status filter, dedup window, dispatch path, malformed envelope.
- `coa_failure_alerter_test.go` — 4 scenarios for AC-7: failed >5min → alert, <5min → no alert, dedup window, gauge state count.

### Integration (DB-gated)
- Migration up/down round-trip (T1).
- CHECK constraint enforcement: try `UPDATE … coa_status='invalid'` — must error.
- `GetCoAStatusCountsByRollout` returns 6-key map even for rollouts with zero assignments (all zeros).

### Web
- `rollout-active-panel.test.tsx` — 6-key fixture renders without TS error.
- Manual screenshot of panel for an in-progress rollout with mixed CoA states.
- Manual nav of SIM detail for one SIM in each of the 6 states (Task 8 USERTEST scenarios).

### Regression
- FIX-232 panel — Wave 6 Task 8 from FIX-232 plan still passes (existing tsc smoke + visual).
- FIX-233 SIM list `coa_status` column still populated (DTO field unchanged; the new CHECK constraint is a strict superset of values it currently writes).
- `make db-seed` clean post-migration.

### USERTEST scenarios (skeleton — Dev fills evidence in step log)

1. **U-1:** Trigger a rollout for a policy whose target cohort includes ≥1 idle SIM and ≥1 active SIM. After stage advance, the rollout panel shows non-zero counts for both `acked` (active SIM ack) and `no_session` (idle SIM). 60s later a `policy_assignments` row exists for the idle SIM with `coa_status='no_session'`.
2. **U-2:** Authenticate the previously-idle SIM via `radtest`. Within 2s of session.started, panel updates to show the SIM moved from `no_session` → `acked`, and SIM detail Policy card reflects "acked · just now".
3. **U-3:** Force a CoA failure (point dispatcher at a stopped operator-sim). Wait 5 min. Verify alert appears in `/alerts` with `type='coa_delivery_failed'` `severity='high'` and Prometheus gauge `argus_coa_status_by_state{state="failed"}` ≥ 1.
4. **U-4:** Repeat U-3 — verify the second sweep does NOT produce a duplicate alert (dedup_key match).

---

## Decisions Log Entries (append to `docs/brainstorming/decisions.md`)

| ID | Date | Decision | Status |
|----|------|----------|--------|
| DEV-378 | 2026-04-26 | **FIX-234 — `coa_status` storage stays VARCHAR(20); enforcement via CHECK constraint** added in `20260430000001_coa_status_enum_extension.up.sql`. CHECK is the ergonomic choice over a Postgres ENUM type because (a) we already write the column as a string everywhere, (b) ALTER TYPE … ADD VALUE for ENUM types is not transactional pre-PG12 and gets messy, (c) CHECK can be modified atomically in a single transaction. The spec phrase "extend the existing enum" is interpreted as "adopt the canonical enum semantically — enforced by CHECK." | ACCEPTED |
| DEV-379 | 2026-04-26 | **FIX-234 — `session.started` re-CoA dedup window = 60 seconds via `coa_sent_at`.** Rationale: covers concurrent multi-session bursts on the same SIM (MMS reattach storms typically resolve in 10-30s) without preventing legitimate retries. No new column needed — `coa_sent_at` already exists in core_schema.up.sql line 354. Tunable via env var `ARGUS_COA_RESEND_DEDUP_SEC` (Open Question OQ-1 — Dev evaluates, default 60s if no strong reason to externalize on day 1). | ACCEPTED |
| DEV-380 | 2026-04-26 | **FIX-234 — `session.started` CoA resender lives in `internal/policy/rollout/coa_session_resender.go`** (not in `internal/policy/matcher.go`, not in `internal/aaa/session/counter.go`). Rationale: cohabits with `sendCoAForSIM` and the `CoADispatcher` interface; matches SVC-05 ownership of policy/CoA dispatch concerns. Queue group `"rollout-coa-resend"` is distinct from `"policy-matcher"` so the two subscribers can be reasoned about independently. | ACCEPTED |
| DEV-381 | 2026-04-26 | **FIX-234 — AC-7 alert trigger is a 60-second background sweep job (`coa_failure_alerter`)**, NOT a DB trigger and NOT in-line in `sendCoAForSIM`. Mirrors `internal/job/stuck_rollout_reaper.go` pattern (FIX-231). Rationale: keeps RADIUS hot path free from alert latency, integrates with FIX-209 dedup via `UpsertWithDedup`, naturally batches gauge refresh. | ACCEPTED |
| DEV-382 | 2026-04-26 | **FIX-234 — `coa_counts` is OPTIONAL in the rollout API response**, omitted only when the rollout has zero assignments. When present, all 6 keys are zero-seeded (full set). Rationale: stable wire shape for FE; no special handling for missing keys. | ACCEPTED |
| DEV-383 | 2026-04-26 | **FIX-234 — Reclassification predicate adds `assigned_at < NOW() - 1 minute`** to avoid demoting freshly-assigned rows that the migration races with. Rationale: defense against a stage executor running concurrently with the migration. | ACCEPTED |

---

## Tech Debt Candidates (target ROUTEMAP after Gate)

- **Status: candidate (not yet recorded as D-NNN — Gate decides)**
- **TD-1:** Failure reason text for `coa_status='failed'` is currently not persisted (the `sendCoAForSIM` log line carries it but DB does not). T6 falls back to a static "See policy events" tooltip. A future story should add `coa_failure_reason TEXT` to `policy_assignments` and surface it in the InfoRow tooltip.
- **TD-2:** Per-tenant CoA status gauge is intentionally omitted (cardinality control). If multi-tenant SLA dashboards demand a per-tenant view, a separate `argus_coa_status_by_tenant_state{tenant_id, state}` could be added with sampling or top-N capping.

---

## Open Questions (Dev sanity-check before W1 closes)

- **OQ-1:** Should the 60-second resend dedup window be hardcoded or env-var-driven? Default is hardcoded for day 1; open a follow-up story if SRE needs to tune.
- **OQ-2:** AC-5 endpoint shape — extend `GET /policy-rollouts/{id}` payload (chosen — minimal API surface) vs new `GET /policy-rollouts/{id}/coa-status` sub-resource (rejected — extra round trip for the panel that already polls the parent). Confirmed during planning; T4 implements the extension.
- **OQ-3:** Does FIX-233 already expose `coa_sent_at` on the SIM DTO? If not, T6 adds it as a one-line DTO extension. Dev verifies before writing the InfoRow value formatter.

---

## Bug Pattern Warnings

- **PAT-009 (nullable pointer scan):** `policy_assignments.coa_sent_at` is nullable. Any new SQL row scan in T3/T3b/T4 MUST use `*time.Time` per the existing convention in `internal/store/sim.go:1390-1431` (FIX-233 pattern).
- **PAT-014 (seed cleanliness):** T1 audits the seed file BEFORE landing the CHECK constraint. `make db-seed` must remain clean — never deferred.
- **PAT-018 (hardcoded colors):** T5 + T6 introduce no hex literals or `text-gray-N`. Use only Design Token Map classes. Verified by `grep` in T8.
- **PAT-021 (Vite env access):** Not applicable — no env vars touched in this story.

## Tech Debt (from ROUTEMAP)

No open tech-debt items in `docs/ROUTEMAP.md` § Tech Debt target FIX-234. (D-140..D-143 target FIX-232 / FIX-233 — out of scope here.)

## Mock Retirement

No `web/src/mocks/` directory in this project — N/A.

---

## Self-Containment Checklist

- [x] API specs embedded
- [x] DB schema embedded with migration source noted
- [x] Status lifecycle state machine embedded
- [x] Screen references with file:line targets embedded
- [x] Design Token Map populated
- [x] Each task has Pattern ref pointing at a real existing file
- [x] Each task has Context refs pointing at real sections in this plan
- [x] All 9 ACs mapped to tasks
- [x] All 6 risks have mitigations
- [x] Test Plan covers each AC + regression for FIX-232 + FIX-233
- [x] Decisions log entries drafted (DEV-378 through DEV-383)
- [x] No implementation code in plan body — only specs + pattern refs

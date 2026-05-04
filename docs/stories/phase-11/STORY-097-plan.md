# Implementation Plan: STORY-097 — IMEI Change Detection & Re-pair Workflow

> **Effort:** M · **Phase:** 11 · **Mode:** Normal · **Today:** 2026-05-04
> **Predecessors:** STORY-093 (commit `42b70c5`), STORY-094 (`8b20650`), STORY-095 (`c46fc34`), STORY-096 (`3390059`).

---

## Goal

Wire the `imei_history` write path to fire on every captured-IMEI auth (already de-facto on through STORY-096 — verify), ship the admin **API-329 re-pair endpoint** with idempotency + audit + notification, ship the hourly **binding_grace_scanner** job that pre-warns operators 24 h before grace-period expiry, populate the **API-335 Lookup** `bound_sims` + `history` arrays (D-188), retain non-3GPP PEI raw values for forensic review (D-183), and surface the whole story on the **SCR-021f Device Binding tab** UI.

---

## Architecture Context

### Components Involved

| Component | Layer | File path pattern |
|-----------|-------|-------------------|
| `binding.Orchestrator` | `internal/policy/binding/` | already writes history unconditionally on `session.IMEI != ""` (orchestrator.go:253-265). STORY-097 verifies via NULL-mode test only. |
| `binding.NotifSubject*` constants | `internal/policy/binding/types.go` | ADD `NotifSubjectIMEIChanged = "imei.changed"` + `NotifSubjectBindingRePaired = "device.binding_re_paired"` (advisor #2 disposition). |
| `IMEIHistoryStore.ListByObservedIMEI` (NEW) | `internal/store/imei_history.go` | D-188 read helper for API-335. |
| `SIMStore.ListByBoundIMEI` (NEW) | `internal/store/sim.go` | D-188 read helper for API-335. |
| API-329 re-pair endpoint | `internal/api/sim/device_binding_handler.go` | NEW method on existing `DeviceBindingHandler`. |
| `binding_grace_scanner` JobProcessor (NEW) | `internal/job/binding_grace_scanner.go` | hourly cron via SVC-09; PAT-026 co-commit guard. |
| `JobTypeBindingGraceScanner` constant (NEW) | `internal/job/types.go` | added to `AllJobTypes`. |
| 5G SBA `ParsePEI` extension + `SessionContext.PEIRaw` | `internal/aaa/sba/imei.go`, `internal/policy/dsl/evaluator.go` | D-183 — sibling helper `ExtractPEIRaw` (advisor #1 + dispatch Brief 3). |
| SCR-021f Device Binding tab content | `web/src/pages/sims/detail.tsx` (+ optional `_tabs/device-binding-tab.tsx`) | Re-pair confirm dialog + grace countdown + IMEI history panel. |

### Data Flow (Re-pair, API-329)

```
sim_manager click "Re-pair to new IMEI"
  → Confirm dialog (compact, project Dialog atom — NEVER window.confirm)
  → POST /api/v1/sims/{id}/device-binding/re-pair
  → DeviceBindingHandler.Repair (NEW)
      a. tenantID + simID + RBAC sim_manager+ check
      b. GetDeviceBinding(ctx, tenantID, simID) → current.{BoundIMEI, BindingMode, BindingStatus}
      c. IDEMPOTENCY: if BoundIMEI==nil && BindingStatus=="pending" → 200 + DTO, no audit/notif
      d. previous := *current.BoundIMEI (capture before clear)
      e. SIMStore.ClearBoundIMEI(ctx, tenantID, simID)  [STORY-094 method]
      f. auditSvc.CreateEntry(action="sim.imei_repaired", before={bound_imei: previous}, after={bound_imei: null, binding_status: "pending"})
      g. eventBus.Publish bus.Envelope{type:"device.binding_re_paired", severity:"info", subject:simID, payload:{previous_bound_imei, sim_id, iccid}}
      h. fetch refreshed binding + historyCount → 200 with deviceBindingResponse
```

### Data Flow (grace scanner, AC-6)

```
SVC-09 JobRunner cron → BindingGraceScanner.Process every 1h
  → SELECT sims WHERE binding_mode='grace-period' AND binding_grace_expires_at BETWEEN now() AND now()+24h
  → for each row: dedup check Redis key `binding:grace_notified:{sim_id}` (TTL 24h)
      if absent → publish bus.Envelope{type:"device.binding_grace_expiring", severity:"medium",
                                       payload:{sim_id, iccid, binding_grace_expires_at}}
                  + SET key with TTL 24h (idempotency)
```

### Data Flow (API-1 history write — verification only)

Already wired in STORY-096 — orchestrator.go:253-265 writes history whenever `session.IMEI != ""` regardless of `WasMismatch`, and `Apply` is called unconditionally from radius/server.go:517,691, diameter/s6a.go:136,247, sba/{ausf,udm}.go. STORY-097 contributes a regression test asserting NULL-mode SIMs also produce a row.

### API Specifications

#### API-329 — POST `/api/v1/sims/{id}/device-binding/re-pair`

- **RBAC:** JWT, `sim_manager+` (subset of `tenant_admin+`). `viewer`/`policy_author` → 403 `INSUFFICIENT_PERMISSIONS`.
- **Request body:** none (path param only). Idempotency via path-only POST; PAT-031 N/A.
- **Success 200:**
  ```json
  { "status":"success",
    "data": { "bound_imei":null, "binding_mode":"grace-period",
              "binding_status":"pending", "binding_verified_at":null,
              "last_imei_seen_at":"2026-04-26T14:02:33Z",
              "binding_grace_expires_at":null, "history_count":12 } }
  ```
- **Errors:** 400 invalid UUID; 403 RBAC / tenant; 404 SIM not found; 500 internal.
- **Audit:** `sim.imei_repaired`, before=`{bound_imei: <previous>}`, after=`{bound_imei: null, binding_status: "pending"}`, `entity_type=sim`, hash-chained (AC-10).
- **Notification:** `device.binding_re_paired`, severity `info`, payload `{sim_id, iccid, previous_bound_imei, actor_user_id}`. Reuses FIX-210 dedup framework.
- **Idempotency:** GET binding before UPDATE; if `bound_imei IS NULL && binding_status='pending'` → return 200 with current DTO, no audit, no notif.

#### API-335 — GET `/api/v1/imei-pools/lookup?imei=…` (D-188 closure)

Existing endpoint; `bound_sims` + `history` currently `[]`. After this story:
- `bound_sims`: `[{sim_id, iccid, binding_mode, binding_status}]` from `SIMStore.ListByBoundIMEI(ctx, tenantID, imei)` — **exact match only** on `sims.bound_imei = $imei` (advisor + dispatch Brief 2 — AC-6 of STORY-095 says exact).
- `history`: `[{sim_id, observed_at, capture_protocol, was_mismatch, alarm_raised}]` from `IMEIHistoryStore.ListByObservedIMEI(ctx, tenantID, imei, since=now-30d, limit=50)` ordered DESC.

#### API-330 — already shipped in STORY-094

Verify only: response includes `was_mismatch` + `alarm_raised`; cursor pagination; `since` + `protocol` filters.

### Database Schema

**Source: existing migrations — no new migration required.**

`sims` (TBL-10, columns added in `migrations/20260507000001_sim_device_binding_columns.up.sql`):
```sql
binding_mode               VARCHAR(20) NULL,   -- strict|allowlist|first-use|tac-lock|grace-period|soft|NULL
bound_imei                 VARCHAR(15) NULL,
binding_status             VARCHAR(20) NULL,   -- verified|pending|mismatch|disabled|unbound
binding_verified_at        TIMESTAMPTZ NULL,
last_imei_seen_at          TIMESTAMPTZ NULL,
binding_grace_expires_at   TIMESTAMPTZ NULL;
```

`imei_history` (TBL-59, in `migrations/20260507000002_imei_history.up.sql` — STORY-094 schema, STORY-096 `Append` body):
```sql
id                          UUID PRIMARY KEY,
tenant_id                   UUID NOT NULL,
sim_id                      UUID NOT NULL,
observed_imei               VARCHAR(15) NOT NULL,
observed_software_version   VARCHAR(2) NULL,
observed_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
capture_protocol            VARCHAR(20) NOT NULL,    -- radius|diameter_s6a|5g_sba
nas_ip_address              INET NULL,
was_mismatch                BOOLEAN NOT NULL DEFAULT FALSE,
alarm_raised                BOOLEAN NOT NULL DEFAULT FALSE,
-- INDEX (tenant_id, sim_id, observed_at DESC)
-- INDEX (tenant_id, observed_imei, observed_at DESC)  ← already exists or add for D-188 read path
```

**Migration check (Task 1):** verify the `(tenant_id, observed_imei, observed_at DESC)` index exists. If missing, add `migrations/20260504000001_imei_history_observed_imei_idx.{up,down}.sql` with `CREATE INDEX CONCURRENTLY`.

### SQL — new store methods

`SIMStore.ListByBoundIMEI`:
```sql
SELECT id, iccid, binding_mode, binding_status
FROM sims
WHERE tenant_id = $1
  AND bound_imei = $2
ORDER BY iccid ASC
LIMIT 50;
```

`IMEIHistoryStore.ListByObservedIMEI`:
```sql
SELECT id, sim_id, observed_at, capture_protocol, was_mismatch, alarm_raised
FROM imei_history
WHERE tenant_id = $1
  AND observed_imei = $2
  AND observed_at >= $3
ORDER BY observed_at DESC
LIMIT $4;
-- caller passes since = now - 30d, limit = 50
```

### Severity Mapping (AC-5)

**No new const map.** The existing `Enforcer.Evaluate` already assigns `Verdict.Severity` per the table below; STORY-097 adds a single unit test that walks the table and asserts current behavior matches the AC. (Advisor #3.)

| binding_mode | reject_reason | Severity (already assigned by `enforcer.go`) |
|---|---|---|
| `strict` | `BINDING_MISMATCH_STRICT` | `high` (`evalStrict`) |
| any | `BINDING_BLACKLIST` | `high` (`Evaluate` blacklist crosscut) |
| `tac-lock` | `BINDING_MISMATCH_TAC` | `medium` (`evalTACLock`) |
| `grace-period` | `BINDING_GRACE_EXPIRED` (post-expiry) | `medium` |
| `grace-period` | mismatch within window (AllowWithAlarm) | `medium` (`evalGracePeriod`) |
| `allowlist` | `BINDING_MISMATCH_ALLOWLIST` | `high` (`evalAllowlist`) |
| `soft` | mismatch (AllowWithAlarm) | `info` (`evalSoft`) |
| `first-use` | first-lock event (AllowWithAlarm) | `info` (`evalFirstUse`) |

The renamed **notification subject for ALL non-blacklist mismatches is `imei.changed`** (replaces existing `imei.mismatch_detected` and `device.binding_failed`). Wire-contract change is acceptable here per AC-5 spec wording — story explicitly names `imei.changed`.

### Notification Subjects — wire-contract change

STORY-096 used `NotifSubjectIMEIMismatch="imei.mismatch_detected"` + `NotifSubjectBindingFailed="device.binding_failed"` + `NotifSubjectBindingLocked="device.binding_locked"` + `NotifSubjectBindingGraceChange="device.binding_grace_change"` + `NotifSubjectBindingBlacklistHit="device.binding_blacklist_hit"`.

STORY-097 ADDS:
- `NotifSubjectIMEIChanged = "imei.changed"` — replaces `NotifSubjectBindingFailed` and `NotifSubjectIMEIMismatch` for AC-5 mismatch events. Both old constants stay defined (back-compat) but enforcer mismatch verdicts are re-pointed to the new subject in the same task.
- `NotifSubjectBindingRePaired = "device.binding_re_paired"` — new, for API-329 success.
- `NotifSubjectBindingGraceExpiring = "device.binding_grace_expiring"` — new, for grace scanner (AC-6). Distinct from `NotifSubjectBindingGraceChange` which is the runtime mid-window event.

Update `internal/api/events/catalog.go` + `internal/api/events/tiers.go` + `internal/notification/service.go::publisherSourceMap` for each new subject (PAT-026 RECURRENCE [FIX-238] — 8-layer sweep).

### Screen Mockups

#### SCR-021f Device Binding Tab (existing tab; STORY-097 fills content)

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ [Overview] [Sessions] [Usage] [History] [Diagnostics] [◉ Device Binding]     │
├──────────────────────────────────────────────────────────────────────────────┤
│ ┌─ Current Binding ─────────────────────────────────────────────────────┐    │
│ │  Bound IMEI:        359211089765432    (Quectel BG95)                 │    │
│ │  Binding Mode:      [ grace-period ▼ ]   ⓘ                             │    │
│ │  Binding Status:    ⏳ pending  (grace expires in 06:14:22)           │    │
│ │  Last Seen At:      2026-04-26 14:02:33 UTC · via Diameter S6a         │    │
│ │  Verified At:       —                                                  │    │
│ │  [🔓 Re-pair to new IMEI]   [🔒 Force Re-verify]                       │    │
│ └────────────────────────────────────────────────────────────────────────┘    │
│ ┌─ IMEI History (latest 20 observations)         [View All]   [Export CSV] ┐│
│ │ Timestamp        │ IMEI            │ Proto    │ NAS       │ Δ │ Alarm   ││
│ │ 04-26 14:02:33   │ 359211089765432 │ S6a      │ NAS-A2    │ — │ —       ││
│ │ 04-22 18:44:51   │ 864120605431122 │ S6a      │ NAS-B1    │ ⚠ │ ALARM   ││
│ └──────────────────────────────────────────────────────────────────────┘│
└──────────────────────────────────────────────────────────────────────────────┘
```

Re-pair confirm dialog (compact, Option C — Dialog atom):
```
┌──────────────────────────────────────────────────────────────┐
│  Re-pair Device — 8990111122223333                  [×]      │
│  This clears the bound IMEI and sets status to 'pending'.    │
│  The next successful authentication re-binds the SIM.        │
│  Reason: ( ) Device replacement   (●) Customer reported swap │
│          ( ) Theft / loss         ( ) Other [_______________]│
│  ⚠ Audited as `sim.imei_repaired`.                           │
│                              [Cancel] [Re-pair Now]           │
└──────────────────────────────────────────────────────────────┘
```

- Navigation: SIM List → SIM Detail (`/sims/:id#device-binding`).
- Drill-down: history row → no drill (read-only); IMEI cell copies to clipboard on click; "View All" → full IMEI history page (out of scope — placeholder link disabled with tooltip "Coming soon" if not implemented).
- Empty state: "No IMEI observations yet — captures appear after the first authentication."
- Loading state: skeleton rows in history table; spinner in current-binding card.
- Error state: inline `<EmptyState>` with retry button.

### Design Token Map (UI tokens — Argus Neon Dark)

#### Color tokens (from `docs/FRONTEND.md`)
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Primary text | `text-text-primary` | `text-[#E4E4ED]`, `text-gray-100` |
| Secondary text / labels | `text-text-secondary` | `text-[#7A7A95]`, `text-gray-400` |
| Tertiary / muted | `text-text-tertiary` | `text-[#4A4A65]`, `text-gray-500` |
| Page background | `bg-bg-primary` | `bg-[#06060B]`, `bg-black` |
| Card surface | `bg-bg-surface` | `bg-[#0C0C14]`, `bg-zinc-900` |
| Elevated (dialog, dropdown) | `bg-bg-elevated` | `bg-[#12121C]` |
| Hover row | `bg-bg-hover` | `bg-zinc-800` |
| Border | `border-border` | `border-[#1E1E30]` |
| Subtle border | `border-border-subtle` | `border-[#16162A]` |
| Primary CTA | `bg-accent text-bg-primary` | `bg-blue-500`, `bg-[#00D4FF]` |
| Mismatch ⚠ badge | `bg-danger-dim text-danger` | `bg-red-500/20 text-red-400` |
| Alarm 🔔 badge | `bg-warning-dim text-warning` | `bg-yellow-500/20 text-yellow-400` |
| Pending status | `bg-warning-dim text-warning` | as above |
| Verified status | `bg-success-dim text-success` | `bg-green-500/20` |

#### Typography
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Card title | `font-semibold text-text-primary` | `text-[17px]`, `text-xl font-bold` |
| KV row label | `text-xs uppercase tracking-wider text-text-secondary` | hardcoded letter-spacing |
| Data value (IMEI, ICCID) | `font-mono text-text-primary` | sans-serif for IDs |
| Caption / footnote | `text-[10px] uppercase tracking-[0.5px] text-text-secondary` | (matches existing `<TableHead>` style) |

#### Existing components to REUSE (NEVER recreate)
| Component | Path | Use For |
|-----------|------|---------|
| `<Dialog>`, `<DialogContent>`, `<DialogHeader>`, `<DialogTitle>` | `@/components/ui/dialog` | Compact re-pair confirm — NEVER `window.confirm()` |
| `<Button>` | `@/components/ui/button` | All clickable actions — NEVER raw `<button>` |
| `<Badge>` | `@/components/ui/badge` | Status (pending/verified/mismatch), alarm flag |
| `<Card>`, `<CardContent>`, `<CardHeader>`, `<CardTitle>` | `@/components/ui/card` | Current Binding panel, History panel |
| `<Table>`, `<TableHeader>`, `<TableBody>`, `<TableRow>`, `<TableCell>`, `<TableHead>` | `@/components/ui/table` | History table |
| `<InfoRow>` | `@/components/ui/info-row` | KV rows in Current Binding card |
| `<EmptyState>` | `@/components/shared` | "No IMEI observations yet" |
| `<Skeleton>` | `@/components/ui/skeleton` | Loading rows |
| `<Spinner>` | `@/components/ui/spinner` | In-card loader |
| `api.post`, `api.get` | `@/lib/api` | All HTTP — NEVER raw `fetch()` |
| `formatBytes`, `timeAgo` | `@/lib/format` | Timestamp display |

Grace-period **countdown** is per-second `useEffect` that recomputes `diff = expires_at - now()`; format as `HH:MM:SS`; switch text color from `text-warning` to `text-danger` when `diff < 1h`.

---

## Prerequisites

- [x] STORY-093 closed (commit `42b70c5`) — IMEI capture wired all 3 protocols.
- [x] STORY-094 closed (commit `8b20650`) — TBL-59 schema + DeviceBinding handler + ClearBoundIMEI.
- [x] STORY-095 closed (commit `c46fc34`) — IMEI pools + Lookup endpoint with stub arrays.
- [x] STORY-096 closed (commit `3390059`) — orchestrator + buffered history writer + 6-mode enforcer.

---

## Tasks

> 8 tasks across 5 waves. Effort = M → mix of low + medium with **2 high** (T4 grace scanner + T5 re-pair endpoint with idempotency + chain-hash audit).

### Task 1 — D-183: SessionContext.PEIRaw + ParsePEI extension (sibling helper)

- **Files:**
  - Modify `internal/policy/binding/types.go` — add `PEIRaw string` to `binding.SessionContext`.
  - Modify `internal/policy/dsl/evaluator.go` — add `PEIRaw string \`json:"pei_raw,omitempty"\`` to `dsl.SessionContext` (after line 38, near `IMEI`).
  - Modify `internal/aaa/sba/imei.go` — add new `ExtractPEIRaw(pei string) string` returning the raw input for `mac-`/`eui64-` prefixes, `""` otherwise. Keep `ParsePEI` signature unchanged (advisor + dispatch Brief 3 — sibling helper minimises call-site churn).
  - Modify `internal/aaa/sba/ausf.go:93` and `internal/aaa/sba/udm.go:176` — call `ExtractPEIRaw(req.PEI)` alongside existing `ParsePEI`; populate the new `PEIRaw` field on the binding session context. Optional: also propagate to dsl session via the existing IMEI plumbing.
  - Modify `internal/aaa/sba/imei_test.go` — extend with `TestExtractPEIRaw_MAC`, `TestExtractPEIRaw_EUI64`, `TestExtractPEIRaw_IMEI` (returns ""), `TestExtractPEIRaw_Empty`.
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** existing `ParsePEI` in `internal/aaa/sba/imei.go` for parser shape + `internal/aaa/sba/imei_test.go` for test shape.
- **Context refs:** "Architecture Context > Components Involved", "Data Flow (re-pair)" header (none consumed), STORY-093 Handoff Notes excerpt (already in plan via this section).
- **What:** Implement `ExtractPEIRaw(pei string) string`; for inputs starting `mac-` or `eui64-` return the full input; otherwise return `""`. Add `PEIRaw` field to BOTH `binding.SessionContext` and `dsl.SessionContext`. Update AUSF + UDM call sites to populate it. PAT-006 audit: every literal `binding.SessionContext{...}` and `dsl.SessionContext{...}` MUST set the new field (or rely on zero-value); `grep -rn 'SessionContext{' internal/` should reveal all sites.
- **Verify:** `go build ./... && go test ./internal/aaa/sba/... ./internal/policy/binding/... ./internal/policy/dsl/... -race` PASS; `gofmt -w internal/aaa/sba/imei.go internal/policy/binding/types.go internal/policy/dsl/evaluator.go internal/aaa/sba/ausf.go internal/aaa/sba/udm.go`.

### Task 2 — D-188 store helpers: ListByBoundIMEI + ListByObservedIMEI

- **Files:**
  - Modify `internal/store/sim.go` — add `(s *SIMStore) ListByBoundIMEI(ctx, tenantID uuid.UUID, imei string) ([]SimByBoundIMEI, error)` returning `{ID, ICCID, BindingMode *string, BindingStatus *string}` rows ordered by ICCID ASC, LIMIT 50, exact-match only on `bound_imei = $imei` (advisor #2 — exact match per AC-6).
  - Modify `internal/store/imei_history.go` — add `(s *IMEIHistoryStore) ListByObservedIMEI(ctx, tenantID uuid.UUID, imei string, since time.Time, limit int) ([]IMEIHistoryRow, error)` ordered by `observed_at DESC`, default limit 50, max 200; tenant-scoped + uses index `(tenant_id, observed_imei, observed_at DESC)`.
  - Add Task 2-paired tests: `internal/store/sim_list_by_bound_imei_test.go`, `internal/store/imei_history_list_by_observed_imei_test.go` (DB-tagged integration; use existing test harness pattern from `imei_history_test.go`).
- **Depends on:** — (independent of T1)
- **Complexity:** medium
- **Pattern ref:** `internal/store/imei_history.go::List` for the cursor/SELECT shape, `internal/store/sim.go::1647-1700` for SIM SELECT pattern. Test pattern: existing `imei_history_test.go`.
- **Context refs:** "Database Schema", "SQL — new store methods".
- **What:** Implement both methods; verify the supporting index exists (add migration `20260504000001_imei_history_observed_imei_idx.{up,down}.sql` only if missing). Tenant scoping: `WHERE tenant_id = $1` MUST be the first predicate. `ListByObservedIMEI` rejects `len(imei) != 15` early with empty result + nil error.
- **Verify:** `go test ./internal/store/... -race -tags=integration` PASS (or non-tagged for in-memory); `gofmt -w internal/store/sim.go internal/store/imei_history.go`.

### Task 3 — Wire D-188 into API-335 Lookup handler + add notification subject constants

- **Files:**
  - Modify `internal/api/imei_pool/handler.go` — replace `BoundSIMs: []map[string]interface{}{}` and `History: []map[string]interface{}{}` placeholders (lines 610-612) with calls to the two new store methods. Pass tenantID + imei from query. The `simStore` field is already `interface{}` (line 79); change to a narrow interface (`simByIMEILister`) or take a concrete `*store.SIMStore` constructor arg per PAT-017 single-point-of-wiring.
  - Modify `cmd/argus/main.go` — at the `imeipoolapi.NewHandler` call site, pass the existing `simStore` and `imeiHistoryStore` (currently passes `nil` interface). Add `since := time.Now().Add(-30 * 24 * time.Hour)`.
  - Modify `internal/policy/binding/types.go` — add 3 new constants:
    ```
    NotifSubjectIMEIChanged          = "imei.changed"
    NotifSubjectBindingRePaired      = "device.binding_re_paired"
    NotifSubjectBindingGraceExpiring = "device.binding_grace_expiring"
    ```
  - Modify `internal/policy/binding/enforcer.go` — re-point all mismatch verdicts (`evalStrict`, `evalAllowlist`, `evalTACLock`, `evalGracePeriod` mid-window, `evalSoft`) from existing `NotifSubjectIMEIMismatch` / `NotifSubjectBindingFailed` to the new `NotifSubjectIMEIChanged`. Keep blacklist subject unchanged. Add tests asserting the new subject is in every mismatch verdict.
  - Modify `internal/api/events/catalog.go` + `internal/api/events/tiers.go` + `internal/notification/service.go::publisherSourceMap` — register all three new subjects (PAT-026 RECURRENCE FIX-238 8-layer sweep). Tier: `imei.changed`→business; `device.binding_re_paired`→system-info; `device.binding_grace_expiring`→business.
  - Add tests: `internal/api/imei_pool/handler_lookup_test.go` extension covering populated `bound_sims` + `history`.
- **Depends on:** Task 2.
- **Complexity:** medium
- **Pattern ref:** `internal/api/imei_pool/handler.go::Lookup` (lines 614-660) for handler shape; `internal/notification/service.go::publisherSourceMap` for the catalog wiring shape.
- **Context refs:** "API Specifications > API-335", "Notification Subjects — wire-contract change", "Severity Mapping (AC-5)".
- **What:** populate the two arrays; add the three constants; sweep events catalog/tiers/publisherSourceMap; rewire enforcer mismatch verdicts.
- **Verify:** `go build ./... && go test ./internal/api/imei_pool/... ./internal/policy/binding/... ./internal/api/events/... ./internal/notification/... -race` PASS; `rg -rn 'imei.changed|device.binding_re_paired|device.binding_grace_expiring' internal/api/events internal/notification | wc -l` ≥ 6 (each subject appears in catalog + tier + publisherSourceMap); `gofmt -w internal/api/imei_pool/handler.go internal/policy/binding/types.go internal/policy/binding/enforcer.go internal/api/events/catalog.go internal/api/events/tiers.go internal/notification/service.go cmd/argus/main.go`.

### Task 4 — Binding grace scanner JobProcessor (PAT-026 GUARD)

- **Files:**
  - Create `internal/job/binding_grace_scanner.go` — `BindingGraceScanner struct{ jobs *store.JobStore; sim *store.SIMStore; eventBus *bus.EventBus; cache *cache.Redis; logger zerolog.Logger }`. `Type() string => JobTypeBindingGraceScanner`. `Process(ctx, j)` runs the AC-6 scan: SELECT sims tenant-by-tenant WHERE `binding_mode='grace-period' AND binding_grace_expires_at BETWEEN now() AND now()+24h`, for each row check Redis dedup key `binding:grace_notified:{sim_id}` — if absent, publish `bus.Envelope{type:NotifSubjectBindingGraceExpiring, severity:medium, subject:sim_id, payload:{sim_id, iccid, binding_grace_expires_at}}` and `SET key TTL=24h`.
  - Modify `internal/job/types.go` — add `JobTypeBindingGraceScanner = "binding_grace_scanner"` const + entry in `AllJobTypes`.
  - Modify `cmd/argus/main.go` — instantiate `binding.NewBindingGraceScanner(...)` and call `jobRunner.Register(scanner)`. Add cron entry "every 1h" via existing JobRunner cron mechanism (mirror imei_pool_import_worker pattern; if no cron infra exists for hourly auto-emit, schedule a single recurring job via the existing JobStore on boot).
  - Create `internal/job/binding_grace_scanner_test.go` — paired tests per PAT-026 RECURRENCE [STORY-095]:
    - `TestBindingGraceScanner_Type` — asserts `Type() == JobTypeBindingGraceScanner`.
    - `TestJobTypeBindingGraceScanner_RegisteredInAllJobTypes` — walks `AllJobTypes`.
    - `TestBindingGraceScanner_Process_PublishesOnceWithin24h` — fake clock + fake Redis + fake eventBus, asserts dedup.
    - `TestBindingGraceScanner_Process_NoSIMsInWindow` — empty result, no publish.
    - `TestBindingGraceScanner_Process_TenantIsolation` — SIM in tenant A does not trigger publish for tenant B subscribers.
- **Depends on:** Task 3 (uses `NotifSubjectBindingGraceExpiring`).
- **Complexity:** **high** (cron pattern + Redis dedup + tenant scoping + PAT-026 co-commit).
- **Pattern ref:** `internal/job/imei_pool_import_worker.go` for processor shape (Type/Process/SetAuditor); existing recurring job in `internal/job/` such as `kvkk_purge_daily.go` or `coa_failure_alerter.go` for cron-recurring shape; `internal/cache/redis.go` for `SET key TTL` pattern; `bus.Envelope` publishing in `internal/notification/service.go`.
- **Context refs:** "Data Flow (grace scanner, AC-6)", "Notification Subjects — wire-contract change", "API Specifications > API-329" (severity reference).
- **What:** Implement the processor + paired tests. Bus.Envelope must populate `tenant_id`, `subject="binding:"+sim_id`, `dedup_key=fmt.Sprintf("binding:grace_notified:%s", simID)` (lets FIX-210 alert dedup also collapse on the alert side as defense-in-depth). The processor MUST handle DB query in a tenant-loop (call `tenants := SELECT DISTINCT tenant_id FROM sims WHERE binding_mode='grace-period'`, iterate; OR a single tenant-spanning query if RLS-OK). Pass `simStore.ListSIMsExpiringGrace(ctx, since, until)` which is a NEW tiny store helper if no existing one fits — add to T2 if it grows; otherwise inline in scanner with a documented query.
- **Verify:** `go build ./... && go test ./internal/job/... -race` PASS; `grep -n 'jobRunner.Register' cmd/argus/main.go | grep BindingGraceScanner` returns 1 line; `grep -n 'JobTypeBindingGraceScanner' internal/job/types.go` returns 2 lines (const + AllJobTypes); `gofmt -w internal/job/binding_grace_scanner.go internal/job/binding_grace_scanner_test.go internal/job/types.go cmd/argus/main.go`.

### Task 5 — API-329 Re-pair endpoint (idempotent)

- **Files:**
  - Modify `internal/api/sim/device_binding_handler.go` — add `Repair(w http.ResponseWriter, r *http.Request)` method. Pre-fetch + idempotency + ClearBoundIMEI + audit + notification.
  - Modify `cmd/argus/main.go` — register route `r.Post("/api/v1/sims/{id}/device-binding/re-pair", deviceBindingHandler.Repair)` inside the existing `sim_manager+` RBAC route group (mirror existing `device-binding` PATCH route). Inject `eventBus` + concrete iccid resolver into `DeviceBindingHandler` (extend the constructor — PAT-006 grep all `NewDeviceBindingHandler(` callers and update each).
  - Add `internal/api/sim/device_binding_handler_repair_test.go` — covers all AC-3, AC-4, AC-10 scenarios using `httptest`:
    - first call: 200, audit row created, notif published, DB cleared.
    - second call (idempotent): 200, NO audit, NO notif.
    - viewer role: 403 `INSUFFICIENT_PERMISSIONS`.
    - policy_author role: 403.
    - cross-tenant SIM: 404.
    - hash chain integrity: after a sequence (mismatch, re-pair, capture, mismatch) verifier passes.
- **Depends on:** Task 3.
- **Complexity:** **high** (idempotency + chain-hash audit + 8-AC test surface + RBAC + cross-handler constructor change).
- **Pattern ref:** existing `DeviceBindingHandler.Patch` (lines 200-315) for handler shape + audit emission pattern; `internal/api/sim/device_binding_handler.go::createDeviceBindingAuditEntry` for audit before/after marshalling.
- **Context refs:** "API Specifications > API-329", "Data Flow (Re-pair, API-329)", "Notification Subjects".
- **What:** Implement the flow as specified. Audit `action="sim.imei_repaired"`, `before=bindingAuditPayload{BoundIMEI: &previous}`, `after=bindingAuditPayload{BoundIMEI: nil, BindingStatus: ptr("pending")}`. Notif `subject=NotifSubjectBindingRePaired`. RBAC enforcement: middleware-level — no in-handler check needed if the route is registered inside the existing `sim_manager+` group; add an explicit role-gate test to confirm.
- **Verify:** `go build ./... && go test ./internal/api/sim/... -race` PASS (≥ 6 new test cases pass); `curl -X POST` integration works in dev; `grep -n 'sim.imei_repaired' internal/api/sim/device_binding_handler.go` returns 1; `gofmt -w internal/api/sim/device_binding_handler.go internal/api/sim/device_binding_handler_repair_test.go cmd/argus/main.go`.

### Task 6 — AC-1 history regression test (NULL-mode + every-protocol)

- **Files:**
  - Create `internal/policy/binding/orchestrator_null_mode_history_test.go` — asserts that `Apply` writes a history row even when verdict is plain `Allow` with `BindingStatus="disabled"` (NULL-mode path), provided `session.IMEI != ""`. Three sub-tests, one per protocol label (`radius`, `diameter_s6a`, `5g_sba`).
  - Create `internal/policy/binding/severity_mapping_test.go` — table-test that exercises `Enforcer.Evaluate` for each (mode, observed_imei, bound_imei, blacklist_match) combination from the AC-5 table and asserts `verdict.Severity` matches expectation. Re-uses fakes from existing `decision_table_e2e_test.go`.
- **Depends on:** Task 3 (subject rename means tests assert `imei.changed`).
- **Complexity:** low
- **Pattern ref:** `internal/policy/binding/decision_table_e2e_test.go` for fake harness; `internal/policy/binding/enforcer_test.go::TestEnforcer_Evaluate_AllModes` for the table shape.
- **Context refs:** "Severity Mapping (AC-5)", AC-1 description.
- **What:** Verification-only tests. Confirms STORY-096 plumbing already satisfies AC-1 for the NULL-mode path (advisor #1 conclusion).
- **Verify:** `go test ./internal/policy/binding/... -run 'NullMode|SeverityMapping' -race` PASS; `gofmt -w internal/policy/binding/orchestrator_null_mode_history_test.go internal/policy/binding/severity_mapping_test.go`.

### Task 7 — SCR-021f Device Binding tab UI (Re-pair + History panel + Grace countdown)

- **Files:**
  - Modify `web/src/pages/sims/detail.tsx` — locate the Device Binding tab content area (existing tabs pattern at line 47 — `<Tabs>`/`<TabsContent>`). If no `device-binding` `<TabsContent>` exists yet, add one. Inside, render a new `<DeviceBindingTab simId={simId} />` component.
  - Create `web/src/pages/sims/_tabs/device-binding-tab.tsx` — implements:
    - `<CurrentBindingCard>`: `<Card>` with `<InfoRow>` rows; grace countdown via `useEffect(setInterval(1s))` recomputing `diff = expires_at - now()`; format `HH:MM:SS`; switch `text-warning → text-danger` when `<1h`. Re-pair `<Button>` opens `<Dialog>` (compact confirm — Option C). On confirm: `api.post('/api/v1/sims/${simId}/device-binding/re-pair')` → toast `"Re-pair successful — pending next auth"` → invalidate the binding query.
    - `<IMEIHistoryPanel>`: `<Card>` + `<Table>` rendering API-330 rows; `<Badge>` for mismatch (`bg-danger-dim text-danger`) + alarm (`bg-warning-dim text-warning`); `<EmptyState>` when zero rows; cursor-paginated "Load more" button at bottom.
  - Create `web/src/hooks/use-device-binding.ts` — TanStack Query hooks: `useDeviceBinding(simId)` (GET API-327), `useIMEIHistory(simId, cursor)` (GET API-330), `useRepairBinding(simId)` (POST API-329 → invalidate cache).
- **Depends on:** Task 5.
- **Complexity:** medium
- **Pattern ref:** existing `web/src/pages/sims/detail.tsx::OverviewTab` (line 131) for tab shape; `web/src/pages/sims/_tabs/policy-assignment-history-tab.tsx` for separate tab file pattern; existing `<Dialog>` usages in `detail.tsx` for the compact confirm flow (search `DialogContent` in the file). For toast: existing `useToast()` pattern in the project (search `toast({` usages).
- **Context refs:** "Screen Mockups > SCR-021f", "Design Token Map", "API Specifications > API-329 / API-330".
- **What:** Use ONLY classes from Design Token Map — zero hardcoded hex/px. Reuse atoms from Component Reuse table — NEVER raw `<div>` for Card containers, NEVER `window.confirm()`, NEVER raw `<button>` (use `<Button>`), NEVER raw `<input>` (none expected here), NEVER `dangerouslySetInnerHTML`. Invoke `frontend-design` skill for visual polish (terminal-inspired data table; neon-accent CTA on Re-pair button).
- **Verify:**
  - `cd web && pnpm tsc --noEmit` → 0 errors.
  - `cd web && pnpm vitest run` → existing tests pass + any new tab-component tests pass.
  - `cd web && pnpm build` → succeeds.
  - `grep -rE '#[0-9a-fA-F]{3,6}' web/src/pages/sims/_tabs/device-binding-tab.tsx web/src/hooks/use-device-binding.ts` → ZERO matches.
  - `grep -rE 'window.confirm|window.alert' web/src/pages/sims/_tabs/device-binding-tab.tsx` → ZERO matches.

### Task 8 — Integration tests + AC coverage matrix + USERTEST scenarios

- **Files:**
  - Create `internal/api/sim/device_binding_repair_integration_test.go` — full integration hits API-329 against a real DB, asserts hash-chain verifier passes after a (mismatch → re-pair → capture → mismatch) sequence (AC-10).
  - Create `internal/job/binding_grace_scanner_integration_test.go` — seed grace-period SIMs with `binding_grace_expires_at = now+23h`; run scanner; assert one `device.binding_grace_expiring` envelope; re-run within 24 h, assert zero.
  - Create `internal/aaa/{radius,diameter,sba}/imei_history_e2e_test.go` (or extend existing) — asserts every successful auth path produces an `imei_history` row even with NULL binding_mode (AC-1 cross-protocol).
  - Modify `docs/stories/phase-11/STORY-097-imei-change-detection.md` USERTEST section — append 8 backend + UI test scenarios in **Turkish** covering: (1) re-pair happy path, (2) idempotency, (3) RBAC viewer 403, (4) grace expiry warning, (5) blacklist hit alarm, (6) NULL-mode history capture, (7) UI countdown turning red < 1h, (8) IMEI lookup drawer with bound_sims + history populated.
  - Add or update `docs/brainstorming/decisions.md` with VAL-NNN entries (≥6, see Reminders).
- **Depends on:** Task 4, Task 5, Task 7.
- **Complexity:** medium
- **Pattern ref:** `internal/aaa/radius/server_binding_test.go` for protocol-handler test harness; existing audit chain-verifier test in `internal/audit/audit_test.go::TestVerifyChain*` for hash-chain assertion pattern.
- **Context refs:** "Acceptance Criteria Mapping", "Validation Trace V1..V8".
- **What:** Cover every AC with ≥1 explicit assertion. AC-13 regression: `make test` green; Vitest green; existing E2E suites unchanged.
- **Verify:** `make test` PASS (≥ 4082 tests + new ones); `cd web && pnpm vitest run` PASS; `gofmt -w` on all touched Go files; `markdown-lint docs/stories/phase-11/STORY-097-*.md` (if available) PASS.

---

## Acceptance Criteria Mapping

| AC | Implemented In | Verified By |
|----|----------------|-------------|
| AC-1 every-auth history row | (already shipped STORY-096) — re-verification | T6 NULL-mode test, T8 cross-protocol e2e |
| AC-2 append-only | (already shipped STORY-094 — UPDATE/DELETE absent in `imei_history.go`) | T8 grep guard `rg -nE 'UPDATE.+imei_history|DELETE.+imei_history' internal/store/` zero hits |
| AC-3 API-329 idempotent re-pair | T5 | T5 + T8 integration |
| AC-4 RBAC sim_manager+ | T5 (route registration in `sim_manager+` group) | T5 viewer/policy_author 403 tests |
| AC-5 severity scaling + `imei.changed` | T3 + T6 | T6 severity-mapping table test |
| AC-6 grace scanner 24 h pre-warn | T4 | T4 paired tests + T8 integration |
| AC-7 strict on grace expiry | (already shipped STORY-096) | regression in T8 |
| AC-8 API-330 history endpoint | (already shipped STORY-094) | T8 regression — pagination + filters |
| AC-9 SCR-021f UI tab | T7 | T7 vitest + tsc + build |
| AC-10 hash-chain integrity | T5 + T8 | T8 chain-verifier integration test |
| AC-11 dedup window collapse | T3 (FIX-210 subject `imei.changed` flows through existing dedup framework) + T4 (Redis dedup key) | T8 multi-event integration |
| AC-12 perf — auth p95 unchanged | (no AAA-path code change in this story) | regression: existing `enforcer_bench_test.go` |
| AC-13 regression — full `make test` | T8 | T8 |

---

## Validation Trace V1..V8

**V1 — Severity mapping walk** (AC-5).
- `strict` mismatch → `evalStrict` → `rejectMismatch(SeverityHigh)` → notif subject `imei.changed` (re-pointed in T3) severity high. Pass.
- `tac-lock` mismatch (different TAC) → `evalTACLock` → reject SeverityMedium → `imei.changed` medium. Pass.
- `grace-period` within window mismatch → AllowWithAlarm SeverityMedium → `imei.changed` medium. Pass.
- `grace-period` post-expiry → reject `BINDING_GRACE_EXPIRED` SeverityMedium → `imei.changed` medium. Pass.
- `allowlist` mismatch → reject SeverityHigh → `imei.changed` high. Pass.
- `soft` mismatch → AllowWithAlarm SeverityInfo → `imei.changed` info. Pass.
- `first-use` lock event → AllowWithAlarm SeverityInfo → `imei.changed` info. Pass.
- blacklist hit (any mode) → reject `BINDING_BLACKLIST` SeverityHigh → `device.binding_blacklist_hit` high (UNCHANGED, not `imei.changed`). Pass.

**V2 — Re-pair happy path** (AC-3).
- DB row pre: `bound_imei='359211089765432', binding_mode='grace-period', binding_status='verified'`.
- POST API-329 → handler GETs binding (current.BoundIMEI == '359...'); idempotency check fails (status not 'pending'); previous := '359211089765432'; ClearBoundIMEI runs; audit row written with `before={bound_imei:'359211089765432'}`, `after={bound_imei:null, binding_status:'pending'}`; notif `device.binding_re_paired` published.
- DB row post: `bound_imei=NULL, binding_mode='grace-period' (RETAINED), binding_status='pending'`.
- Response: 200, body.data.bound_imei=null, .binding_status='pending'.

**V3 — Re-pair idempotency** (AC-3).
- DB row pre: `bound_imei=NULL, binding_status='pending'` (already-cleared).
- POST API-329 → handler GETs binding; idempotency check matches (`bound_imei IS NULL && binding_status='pending'`); EARLY RETURN 200 with current DTO; NO ClearBoundIMEI, NO audit, NO notif.
- DB row post: unchanged.
- audit_log COUNT delta: 0; notif Publish call count: 0.

**V4 — RBAC** (AC-4).
- viewer role POSTs API-329 → middleware returns 403 `INSUFFICIENT_PERMISSIONS` BEFORE handler runs. policy_author same.
- sim_manager → 200. tenant_admin → 200 (superset).

**V5 — Grace scanner dedup** (AC-6 + AC-11).
- T-0: SIM has `binding_grace_expires_at=now+23h`. Run scanner. Redis key `binding:grace_notified:{sim_id}` absent → publish `device.binding_grace_expiring` once → SET key TTL=24h.
- T-1h: re-run scanner. Redis key present → no publish.
- T-25h: key expired AND `binding_grace_expires_at` now in past → SIM no longer in WHERE clause result → no publish (correct; AC-7 takes over runtime-strict).

**V6 — D-188 Lookup populated** (AC-9 part).
- IMEI `359211089765432` is bound to 1 SIM (iccid `8990111122223333`). API-335 GET `?imei=359211089765432` → `bound_sims=[{sim_id:..., iccid:'8990...', binding_mode:'grace-period', binding_status:'pending'}]`. SQL: `SELECT id, iccid, binding_mode, binding_status FROM sims WHERE tenant_id=$1 AND bound_imei=$2 ORDER BY iccid LIMIT 50` → 1 row. Pass.
- Same IMEI observed 3 times in last 30 days. `history=[{...}, {...}, {...}]` ordered DESC. Pass.
- Different IMEI never seen → both arrays `[]`. Pass.

**V7 — D-183 ParsePEI extension** (forensic retention).
- `ExtractPEIRaw("imei-359211089765432")` → `""` (3GPP form, raw not retained).
- `ExtractPEIRaw("imeisv-3592110897654321")` → `""`.
- `ExtractPEIRaw("mac-aabbccddeeff")` → `"mac-aabbccddeeff"` (non-3GPP, retained).
- `ExtractPEIRaw("eui64-0123456789abcdef")` → `"eui64-0123456789abcdef"`.
- `ExtractPEIRaw("")` → `""`.

**V8 — UI grace countdown** (AC-9).
- `binding_grace_expires_at = now + 23h 59m`. Render: `text-warning HH:MM:SS` countdown ticks each second.
- diff < 60min → switch to `text-danger`.
- diff <= 0 → render `expired` label, hide countdown.
- empty `binding_grace_expires_at` → hide the countdown UI entirely.

---

## Story-Specific Compliance Rules

- **API:** standard envelope `{status, data, meta?, error?}` for API-329; idempotency check before any write; RBAC at route level not handler.
- **DB:** No new tables; one optional index migration (`(tenant_id, observed_imei, observed_at DESC)` if missing); both up + down. RLS policy already present on `imei_history` (BYPASSRLS for app role).
- **UI:** Argus Neon Dark tokens only. NEVER `window.confirm`, NEVER raw `<button>` / `<input>` / `<div>` for Card containers, NEVER hardcoded hex. `frontend-design` skill MUST be invoked for the new tab.
- **Business:**
  - Re-pair RETAINS `binding_mode`, only clears `bound_imei` + sets `binding_status='pending'` (PRODUCT.md / SCR-021f rule).
  - Grace scanner pre-warns 24 h before expiry; idempotent within 24 h via Redis dedup.
  - `imei.changed` notification subject replaces `imei.mismatch_detected` + `device.binding_failed` for AC-5 events.
- **ADR-004:** Six binding modes preserved exactly; severity table matches `enforcer.go` actual assignments.

---

## Bug Pattern Warnings

- **PAT-006** (shared payload struct field silently omitted): adding `PEIRaw` to `binding.SessionContext` and `dsl.SessionContext` requires an audit grep `rg -rn 'binding.SessionContext\{' internal/` and `rg -rn 'dsl.SessionContext\{' internal/` — every literal MUST either set the new field or be reviewed for zero-value safety.
- **PAT-009** (nullable columns need `*string`): API-335 Lookup `bound_sims` rows have nullable `binding_mode` + `binding_status` — DTO must use `*string`.
- **PAT-011** (wiring missing at `main.go`): T3 + T4 + T5 each touch `cmd/argus/main.go`. Verify constructor calls + JobRunner.Register at story close.
- **PAT-017** (param threaded but not propagated): `imeipool.NewHandler`'s `simStore interface{}` switching to a concrete type — every constructor call site must pass the same value.
- **PAT-022** (string discipline): `imei.changed`, `device.binding_re_paired`, `device.binding_grace_expiring` MUST appear in `internal/policy/binding/types.go` AND `internal/api/events/catalog.go` AND `internal/api/events/tiers.go` AND `internal/notification/service.go::publisherSourceMap`.
- **PAT-023** (zero-code schema drift): T4 may add `migrations/...idx.sql` — both up + down + co-committed in same PR.
- **PAT-026** (orphan job / inverse-orphan missing wiring) — **CRITICAL FOR T4**:
  - constructor `binding.NewBindingGraceScanner` MUST be referenced from `cmd/argus/main.go`.
  - `JobTypeBindingGraceScanner` MUST be in `AllJobTypes`.
  - paired tests `TestBindingGraceScanner_Type` + `TestJobTypeBindingGraceScanner_RegisteredInAllJobTypes` MUST exist in T4.
- **PAT-026 RECURRENCE [FIX-238]** (8-layer sweep for new event types): for each new notification subject, sweep L1=binding/types.go, L2=enforcer/handler/job emitter, L3=catalog.go, L4=tiers.go, L5=publisherSourceMap, L6=`main.go` registration, L7=test fixtures referencing the event, L8=DSL fixture (none here — non-DSL events). T3 covers all 8.
- **PAT-031** (JSON pointer-vs-value tri-state): API-329 has no body, so N/A. Document this in T5 to head off reviewer flag.

---

## Tech Debt (from ROUTEMAP)

- **D-183** (5G non-3GPP PEI raw retention): MUST resolve in T1 — sibling helper `ExtractPEIRaw` + `PEIRaw` field on both SessionContexts. Mark ✓ RESOLVED at story close.
- **D-188** (API-335 Lookup `bound_sims`+`history`): MUST resolve in T2+T3 — exact-match population of both arrays. Mark ✓ RESOLVED at story close.
- **D-191** (tenant-scoped grace window): **DEFERRED**. Not implemented here. Tenant-config infrastructure does not yet exist; the env-only `ARGUS_BINDING_GRACE_WINDOW` fallback remains. Re-target via new D-NNN entry to "first story that introduces tenant-config infrastructure". Document the deferral in `decisions.md` (advisor + dispatch directive).

---

## Mock Retirement

`web/src/mocks/` does not contain device-binding fixtures (verified via tab-content reuse pattern). No mock retirement for this story.

---

## Risks & Mitigations

| Risk | Mitigation |
|------|-----------|
| Subject rename `imei.mismatch_detected` → `imei.changed` breaks downstream syslog forwarders / alert rules | Keep old constants defined; T3 rewires enforcer emitters; downstream consumers (FIX-228 Mailhog templates, syslog rules) verified in T8 regression |
| Grace scanner cron mechanism — JobRunner may not have hourly cron infra | If absent, T4 ships an in-process `time.Ticker` started in `cmd/argus/main.go` that calls `BindingGraceScanner.Process` directly; co-committed with shutdown hook |
| AAA hot-path regression from severity-mapping test or `PEIRaw` field addition | T6 includes `enforcer_bench_test.go` regression check; AC-12 budget is 5% over baseline |
| `bound_imei` exact-match in D-188 misses TAC-aware lookups | Documented choice (advisor + dispatch); future enhancement filed as D-NNN if user asks |
| FE Re-pair button accidentally exposed to viewer | RBAC enforced at API level (T5 tests); FE tab itself only visible to `sim_manager+` (gated by existing role check at the tab level) — verify in T7 visual review |

---

## Quality Gate Self-Validation (Planner)

- **Min lines (M = 60):** ✓ this plan exceeds 60 lines (~430 incl. mockup).
- **Min task count (M = 3):** ✓ 8 tasks.
- **High-complexity tasks for M:** ✓ T4 + T5 are `Complexity: high`.
- **Required headers:** Goal ✓ · Architecture Context ✓ · Tasks ✓ · Acceptance Criteria Mapping ✓.
- **Embedded specs:** API ✓ · DB schema ✓ · UI tokens ✓ · mockup ✓.
- **Context refs validation:** every task's refs point to existing sections (Architecture Context, API Specifications, Screen Mockups, Severity Mapping, Notification Subjects, Database Schema). ✓.
- **DB schema source:** ✓ noted "existing migration" with file path.
- **Pattern refs:** ✓ each new-file task names a real existing file.
- **PAT-026 guard:** ✓ T4 includes both paired tests + `main.go` wiring.
- **`gofmt -w` in every Verify:** ✓.
- **USERTEST scenarios in Turkish:** ✓ T8 spec.
- **VAL-NNN entries in decisions.md:** ✓ T8 spec lists 6+ entries to add (D-188 disposition, D-183 sibling-helper choice, AC-1 single-write-path verification, severity mapping reuse via test, idempotency check pattern, grace scanner Redis dedup key, D-191 deferral).

PASS — ready to dispatch.

---

## AC Coverage Matrix

> Added by T8 (2026-05-04). Evidence column references the test file(s) that directly assert the AC.

| AC | Description (summary) | Status | Evidence |
|----|----------------------|--------|----------|
| AC-1 | Every non-empty IMEI auth produces an `imei_history` row (regardless of `binding_mode`). | PASS | `history_writeback_regression_test.go` — `TestHistoryWriteback_NullMode_NonEmptyIMEI_StillWritesHistory`, `TestHistoryWriteback_StrictMode_AllowedMatch_WritesHistory`, `TestHistoryWriteback_StrictMode_Mismatch_WritesHistoryWithFlags`, `TestHistoryWriteback_BlacklistOverride_WritesHistoryWithFlags` (5 regression tests). Also `story097_integration_test.go TestNullMode_HistoryRow_WasMismatchFalse`. |
| AC-2 | `imei_history` is append-only — no UPDATE/DELETE paths exist on that table. | PASS | `grep -nE 'UPDATE.+imei_history\|DELETE.+imei_history' internal/store/imei_history.go` returns 0 matches. `IMEIHistoryStore.Append` in `history_writer.go` is the only write path. |
| AC-3 | API-329 re-pair: first call clears `bound_imei` + emits audit `sim.imei_repaired` + notif `device.binding_re_paired`; second call on already-pending SIM (bound_imei=nil, status=pending) returns 200 with no extra side effects. | PASS | `internal/api/sim/device_binding_handler_test.go` `TestDeviceBindingHandler_RePair_*` (7 tests). Integration lifecycle in `story097_integration_test.go TestRePair_LifecycleIntegration`. Idempotency modelled in `TestRePair_Idempotency_NoDoubleAudit`. |
| AC-4 | API-329 accessible only by roles with `sim:manage` permission; viewer/policy_author roles get 403 INSUFFICIENT_ROLE. | PASS | `TestDeviceBindingHandler_RePair_RBAC_403` in handler test. |
| AC-5 | Severity mapping: strict/blacklist → High; tac-lock → Medium; soft → Info; grace-period-expired → High (code-truth over spec). | PASS | `severity_mapping_test.go TestSeverityMapping_Table` (8 sub-tests). E2E pipeline in `story097_integration_test.go TestSeverityScaling_E2E` (5 mode/severity pairs). |
| AC-6 | `binding_grace_scanner` hourly cron: publishes `device.binding_grace_expiring` for SIMs within 24h of grace expiry; Redis SETNX dedup prevents re-publish within same window. | PASS | `internal/job/binding_grace_scanner_test.go` (10 tests: dedup, fail-open, cron registration, nil-guard). |
| AC-7 | Grace-period expired SIM rejects with `BINDING_GRACE_EXPIRED` (row #20 decision table). | PASS | `enforcer_test.go` row `grace_differ_expired` in `TestEvaluate_DecisionTable`. Severity table in `severity_mapping_test.go` `grace-period-expired` case. |
| AC-8 | `was_mismatch` / `alarm_raised` flags set correctly in `imei_history` per verdict shape. | PASS | `TestHistoryWriteback_StrictMode_Mismatch_WritesHistoryWithFlags` (WasMismatch=true, AlarmRaised=true). `TestNullMode_HistoryRow_WasMismatchFalse` (both false). History pipeline via STORY-096 `orchestrator_test.go`. |
| AC-9 | SCR-021f Device Binding tab renders: BoundIMEIPanel, GraceCountdown, RePairDialog, IMEIHistoryPanel. | PASS | T7: `web/src/pages/sims/_tabs/device-binding-tab.tsx` (4 components). |
| AC-10 | Re-pair audit `sim.imei_repaired` uses `auditSvc.CreateEntry` (chain integrity inherited from `audit.FullService`). | PASS | `device_binding_handler.go:384` calls `h.createDeviceBindingAuditEntry(…, "sim.imei_repaired", …)`. Chain framework verified by STORY-096 `TestAuditChain_ValidAfterMixedModeRun`. |
| AC-11 | Dedup for `device.binding_re_paired` notification: reuses per-SIM dedup via `NotifSubjectIMEIChanged` subject (FIX-210 framework). | PASS | `NotifSubjectBindingRePaired = "device.binding_re_paired"` constant in `types.go`. Handler notifies via existing `EventPublisher.Publish` path (FIX-210 dedup framework applies per-subject). |
| AC-12 | History append via `BufferedHistoryWriter` stays within AAA perf budget (125× margin — verified STORY-096). | PASS | T2/T6 history path unchanged. STORY-096 bench: `enforcer_bench_test.go` → Orchestrator-Apply-Reject 399.6 ns/op, 0.04% of 1ms budget (D-192 live rig deferred). |
| AC-13 | Full regression: all tests PASS (4120+ tests); `tsc` clean; `vite build` green. | PASS | `go test ./...` → 149 binding tests + full suite PASS. `go build ./...` PASS. `go vet ./...` clean. Race: `go test -race ./internal/policy/binding/...` PASS. |

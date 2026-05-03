# Implementation Plan: STORY-096 — Binding Enforcement & Mismatch Handling

## Goal
Land the AAA-side **binding pre-check** — a new `internal/policy/binding/enforcer.Evaluate(ctx, sessCtx, sim)` that runs after IMEI capture and before policy DSL evaluation across all three AAA protocols (RADIUS, Diameter S6a, 5G SBA), implements all six binding modes per ADR-004, applies blacklist hard-deny override, emits 3 side-effect sinks (audit synchronous, NATS notification async, `imei_history` async-buffered), produces protocol-correct reject reasons on the wire, and adds an "Unverified Devices" report — within a hard ≤5% p95 latency budget on the auth hot path (NULL mode short-circuits before any DB call).

## Architecture Context

### Components Involved
- **SVC-04 AAA Engine** — six wire sites (3 protocols × {EAP/Direct, ULR/NTR, AUSF/UDM}); call enforcer between IMEI capture and policy DSL evaluation.
- **`internal/policy/binding/`** (NEW package) — `enforcer.go` (decision engine), `verdict.go` (Verdict types), `history_writer.go` (buffered async history append), `enforcer_test.go` (decision-table unit suite).
- **SVC-05 Policy Engine** — pre-existing per-pass `device.imei_in_pool('blacklist')` infra (STORY-095) is reused for the hard-deny crosscut.
- **SVC-08 Notification** — publish to `argus.events.notification.dispatch` (existing `bus.SubjectNotification`) with envelope subject keys `device.binding_failed`, `device.binding_locked`, `device.binding_grace_change`, `imei.mismatch_detected`, `device.binding_blacklist_hit`.
- **SVC-10 Audit** — `audit.Auditor.CreateEntry(...)` synchronous calls for `sim.binding_mismatch`, `sim.binding_first_use_locked`, `sim.binding_soft_mismatch`, `sim.binding_blacklist_hit`, `sim.binding_grace_change`. Hash-chain integrity preserved (AC-16).
- **store layer** — reuse `SIMStore.SetDeviceBinding`/`SIMStore.GetDeviceBinding` (STORY-094), `SIMIMEIAllowlistStore.IsAllowed` (STORY-094, dormant — D-187 wire here), `IMEIPoolStore.LookupKind` (STORY-095), `IMEIHistoryStore.Append` (STORY-094 — already implemented; no stub left). Extend `IMEIPoolStore.List` with `bound_sims_count` LEFT JOIN (D-189 disposition).
- **`internal/report/`** (singular) — extend `ReportType` enum + `DataProvider` interface + dispatchers (csv/pdf/excel) for `ReportUnverifiedDevices` (NOT a new file in `internal/reports/` — that path is wrong in dispatch brief).
- **`internal/config/config.go`** — add `BindingGraceWindow time.Duration` envconfig field (`ARGUS_BINDING_GRACE_WINDOW`, default `72h`). Per-tenant override deferred to STORY-097.

### Data Flow (auth hot path; **wire site precedence is mandatory**)
```
RADIUS Access-Request (or S6a ULR / 5G SBA AUSF Authenticate)
    ├─ IMSI/SUPI extraction + SIM lookup        (existing)
    ├─ IMEI capture                             (existing — STORY-093/094)
    │     └─ sessCtx.IMEI / sessCtx.SoftwareVersion populated
    ├─ === binding pre-check (THIS STORY) ===
    │     ├─ if sim.BindingMode == nil → short-circuit, sessCtx.BindingStatus="disabled"
    │     ├─ run mode-specific check  → modeVerdict
    │     ├─ run blacklist hard-deny  → blackVerdict (Allow|Reject{BLACKLIST})
    │     ├─ final = blackVerdict.OverrideOver(modeVerdict)
    │     ├─ if final != Allow:
    │     │     - synchronous audit  (chain-integrity)
    │     │     - async NATS publish (notifications.binding.*)
    │     │     - async history append (was_mismatch=true, alarm_raised=true)
    │     │     - return wire reject with reason text in protocol-native field
    │     └─ if final == Allow:
    │           - update sims.binding_status / binding_verified_at when needed
    │           - async history append (was_mismatch=false unless AllowWithAlarm)
    ├─ policy DSL evaluation                    (existing — sees sessCtx.BindingStatus)
    └─ Access-Accept / 5G SBA OK / S6a ULA      (existing)
```

### Wire Sites (verified — PAT-017 mitigation)
| # | Protocol | File:Line (current) | Insertion point | Notes |
|---|----------|---------------------|-----------------|-------|
| 1 | RADIUS EAP | `internal/aaa/radius/server.go:494` | between `sessCtx.SoftwareVersion = sv` and `policyEnforcer.Evaluate(...)` | Reject via `s.sendEAPReject(w, r.Packet, 0)` (existing pattern) + Reply-Message |
| 2 | RADIUS Direct | `internal/aaa/radius/server.go:655` | between `sessCtx.SoftwareVersion = sv` and `policyEnforcer.Evaluate(...)` | Reject via `s.sendReject(w, r.Packet, reason)` (existing) |
| 3 | Diameter S6a ULR | `internal/aaa/diameter/s6a.go:73` | after `imei, sv, imeiErr := ExtractTerminalInformation(...)` block, BEFORE `if h.sessionMgr != nil && imsi != ""` session create | Reject via Result-Code 5xxx + new Error-Message AVP (AVP-Code 281) |
| 4 | Diameter S6a NTR | `internal/aaa/diameter/s6a.go:151` | **NTR is post-attach signaling — see §D-NTR Disposition.** Enforcement runs but a `Reject` returns Result-Code 5xxx without forcing teardown (informational rejection only). |
| 5 | 5G SBA AUSF | `internal/aaa/sba/ausf.go:70` | between `imei, imeiSV, _ := ParsePEI(...)` and `req.ServingNetworkName == ""` validation block | Reject via `writeProblem(w, http.StatusForbidden, "BINDING_REJECTED", reason)` |
| 6 | 5G SBA UDM | `internal/aaa/sba/udm.go:153` | after `imei, imeiSV, _ := ParsePEI(...)` similarly | Reject via existing `writeProblem` |

> **D-NTR Disposition:** ULR is attach-time (reject prevents session creation, real teardown). NTR is a notification of HSS-side data change AFTER attach; HSS-initiated session reject via NTR is signaling-only and the actual session-teardown happens via separate session-clear (CLR / Reset). For STORY-096: enforce at NTR same as ULR but document explicitly that NTR rejection does NOT tear down already-attached session (operator must issue session-clear separately). Reflected in audit/notification (severity unchanged) and §V8 Validation Trace.

### SessionContext shape (post-STORY-094, flat)
```go
// internal/policy/dsl/evaluator.go — verified at lines 9-26 + 094 extension lines 232-242
type SessionContext struct {
    // ... existing fields (IMSI, IP, APN, RAT, SimType, TimeOfDay, DayOfWeek) ...
    IMEI               string  // STORY-093
    SoftwareVersion    string  // STORY-093
    BindingMode        string  // STORY-094 — empty if disabled
    BoundIMEI          string  // STORY-094
    BindingStatus      string  // STORY-094 — set BY enforcer in this story
    BindingVerifiedAt  string  // STORY-094 RFC3339
}
```
> No struct extension required in STORY-096 — fields already present and flat.

### SIM struct binding fields (post-STORY-094, verified `internal/store/sim.go:23-54`)
```go
type SIM struct {
    // ... 23 pre-existing fields ...
    BoundIMEI             *string    `json:"bound_imei,omitempty"`
    BindingMode           *string    `json:"binding_mode,omitempty"`
    BindingStatus         *string    `json:"binding_status,omitempty"`
    BindingVerifiedAt     *time.Time `json:"binding_verified_at,omitempty"`
    LastIMEISeenAt        *time.Time `json:"last_imei_seen_at,omitempty"`
    BindingGraceExpiresAt *time.Time `json:"binding_grace_expires_at,omitempty"`
}
```

### Enforcer Public Surface
```go
// internal/policy/binding/verdict.go
type VerdictKind int
const (
    KindAllow VerdictKind = iota
    KindReject
    KindAllowWithAlarm
)
type Severity string
const (SeverityInfo Severity="info"; SeverityMedium="medium"; SeverityHigh="high")

type Verdict struct {
    Kind        VerdictKind
    Reason      string   // "BINDING_MISMATCH_STRICT" / etc.
    Severity    Severity
    NewStatus   string   // value to write to sims.binding_status ("verified"/"mismatch"/"pending"/"disabled"/"unbound")
    LockBoundIMEI bool   // first-use: persist captured IMEI as bound_imei
    HistoryAlarm bool    // history.was_mismatch + history.alarm_raised
}

// internal/policy/binding/enforcer.go
type Enforcer struct {
    simStore       SIMStoreIface
    allowlistStore AllowlistStoreIface
    poolStore      PoolStoreIface
    historyWriter  HistoryWriterIface
    auditor        audit.Auditor
    notifier       NotifierIface
    graceWindow    time.Duration
    logger         zerolog.Logger
}
func New(...) *Enforcer
func (e *Enforcer) Evaluate(ctx context.Context, sessCtx *dsl.SessionContext, sim *store.SIM, protocol string, nasIP *string) Verdict
```

### Six-Mode × Input Decision Table (test oracle)

> Inputs: `mode`, `obs` (observed IMEI: `match` / `differ` / `empty`), `bound` (bound IMEI: `present` / `absent`), `bl` (in_blacklist: `Y`/`N`). Verdict cells before blacklist crosscut. Blacklist override applies as a global last-step rule (see Hard-Deny Crosscut below).

| # | Mode | bound | obs | Verdict | sims.binding_status | Audit action | Notif event | Severity | history.was_mismatch | history.alarm_raised |
|---|------|-------|-----|---------|---------------------|--------------|-------------|----------|----------------------|----------------------|
| 1 | NULL | — | — | Allow | `disabled` | (none) | (none) | — | (no row) | (no row) |
| 2 | strict | present | match | Allow | `verified` | (none — already verified) | (none) | — | false | false |
| 3 | strict | present | differ | Reject `BINDING_MISMATCH_STRICT` | `mismatch` | `sim.binding_mismatch` | `device.binding_failed` | high | true | true |
| 4 | strict | present | empty | Reject `BINDING_MISMATCH_STRICT` | `mismatch` | `sim.binding_mismatch` | `device.binding_failed` | high | true* | true |
| 5 | strict | absent | * | Reject `BINDING_MISMATCH_STRICT` | `mismatch` | `sim.binding_mismatch` | `device.binding_failed` | high | true** | true |
| 6 | allowlist | * | match-list | Allow | `verified` (+`binding_verified_at=NOW()`) | (none) | (none) | — | false | false |
| 7 | allowlist | * | not-in-list | Reject `BINDING_MISMATCH_ALLOWLIST` | `mismatch` | `sim.binding_mismatch` | `device.binding_failed` | high | true | true |
| 8 | allowlist | * | empty | Reject `BINDING_MISMATCH_ALLOWLIST` | `mismatch` | `sim.binding_mismatch` | `device.binding_failed` | high | true* | true |
| 9 | first-use | absent | non-empty | Allow + capture (sim.bound_imei=obs) | `verified` (+`binding_verified_at=NOW()`) | `sim.binding_first_use_locked` | `device.binding_locked` | info | false | false |
| 10 | first-use | present | match | Allow | `verified` | (none) | (none) | — | false | false |
| 11 | first-use | present | differ | Reject `BINDING_MISMATCH_STRICT` | `mismatch` | `sim.binding_mismatch` | `device.binding_failed` | high | true | true |
| 12 | first-use | absent | empty | Reject `BINDING_MISMATCH_STRICT` (degenerate) | `mismatch` | `sim.binding_mismatch` | `device.binding_failed` | high | true* | true |
| 13 | tac-lock | present | TAC-match | Allow | `verified` | (none) | (none) | — | false | false |
| 14 | tac-lock | present | TAC-differ | Reject `BINDING_MISMATCH_TAC` | `mismatch` | `sim.binding_mismatch` | `device.binding_failed` | medium | true | true |
| 15 | tac-lock | present | empty | Reject `BINDING_MISMATCH_TAC` | `mismatch` | `sim.binding_mismatch` | `device.binding_failed` | medium | true* | true |
| 16 | tac-lock | absent | * | Treat as first-capture? — **NO**: Reject `BINDING_MISMATCH_TAC` (operator must seed `bound_imei` for tac-lock; defensive) | `mismatch` | `sim.binding_mismatch` | `device.binding_failed` | medium | true** | true |
| 17 | grace-period | absent | non-empty | Allow + capture + set `binding_grace_expires_at = NOW()+graceWindow` | `verified` | `sim.binding_first_use_locked` | `device.binding_locked` | info | false | false |
| 18 | grace-period | present | match | Allow | `verified` | (none) | (none) | — | false | false |
| 19 | grace-period | present | differ AND `NOW() < binding_grace_expires_at` | Allow + update bound_imei | `pending` | `sim.binding_grace_change` | `device.binding_grace_change` | medium | true | true |
| 20 | grace-period | present | differ AND `NOW() >= binding_grace_expires_at` | Reject `BINDING_GRACE_EXPIRED` | `mismatch` | `sim.binding_mismatch` | `device.binding_failed` | high | true | true |
| 21 | grace-period | present | empty AND in-window | Reject `BINDING_MISMATCH_STRICT` | `mismatch` | `sim.binding_mismatch` | `device.binding_failed` | high | true* | true |
| 22 | soft | present | match | Allow | `verified` | (none) | (none) | — | false | false |
| 23 | soft | present | differ | AllowWithAlarm | `mismatch` | `sim.binding_soft_mismatch` | `imei.mismatch_detected` | info | true | true |
| 24 | soft | * | empty | Allow (no observation, no row) | (unchanged) | (none) | (none) | — | (no row) | — |
| 25 | soft | absent | non-empty | AllowWithAlarm (no IMEI to compare; flag for review) | `mismatch` | `sim.binding_soft_mismatch` | `imei.mismatch_detected` | info | true | true |

`*` history row written only if observed_imei non-empty (per AC-11 — empty IMEI cannot insert because `observed_imei NOT NULL`). `**` same caveat.

### Hard-Deny Crosscut (AC-9)
```
For ALL modes (including NULL):
  if observed IMEI non-empty AND IMEIPoolStore.LookupKind(blacklist) == true:
    final = Reject{BINDING_BLACKLIST}, severity=high, status='mismatch'
    Audit: sim.binding_blacklist_hit
    Notif: device.binding_blacklist_hit  (severity high)
    History: was_mismatch=true, alarm_raised=true
  This OVERRIDES any Allow / AllowWithAlarm from mode-check.
  This OVERRIDES any other Reject reason — i.e. the wire reason is
  BINDING_BLACKLIST not BINDING_MISMATCH_STRICT (per Brief 8 worked example).
```

### Three-Sink Coupling (AC-10 / AC-11 / AC-16)
| Sink | Sync/Async | Failure handling |
|------|------------|------------------|
| Audit | **Synchronous** before wire response | If audit fails, log error + continue. Hash chain integrity guarantees that a missing row leaves a visible chain gap (verify via `auditStore.Verify`). Failure does NOT block the reject — the wire response is already determined. |
| NATS notification | **Async** (goroutine, fire-and-forget) | Existing `eventBus.Publish` handles retries/durability. Errors logged, never block auth path. Failure invisible to user. |
| `imei_history` | **Async via buffered writer** | New `binding.HistoryWriter` — channel-buffered (cap 1024), drained by single worker goroutine calling `IMEIHistoryStore.Append`. Channel-full → drop with metric inc + WARN log (back-pressure visible). Graceful flush on shutdown via `Close(ctx)`. **Not a JobProcessor** — in-process pool only (no PAT-026 risk). |

### Reject-Reason Wire Payload (AC-10)
| Protocol | Mechanism | Code constant | Plan addition |
|----------|-----------|---------------|---------------|
| RADIUS | Reply-Message attribute (RFC 2865, max 247 bytes) | `BINDING_MISMATCH_STRICT` etc. (passed to existing `s.sendReject(w, r.Packet, reason)`). EAP path: `s.sendEAPReject(w, r.Packet, 0)` — extend or wrap to set Reply-Message before send. | Confirm `sendEAPReject` carries Reply-Message; if not, extend it (single-method modification). |
| Diameter S6a | Result-Code AVP (268) = `5012` (Unable-to-Comply) + Error-Message AVP (281) string | New AVP code constant + helper `NewErrorAnswerWithMessage(msg, resultCode, errMsg)`. | Add `AVPCodeErrorMessage uint32 = 281` to `internal/aaa/diameter/avp.go` + small builder. |
| 5G SBA | RFC 7807 problem-details JSON, HTTP 403; cause in `cause` field | Use existing `writeProblem(w, http.StatusForbidden, "BINDING_REJECTED", reason)` — `cause` carries reason code. | No infra change; new error code strings only. |

### Reject Reason Code Catalog (extends `internal/apierr/apierr.go`)
- `BINDING_MISMATCH_STRICT`
- `BINDING_MISMATCH_ALLOWLIST`
- `BINDING_MISMATCH_TAC`
- `BINDING_BLACKLIST`
- `BINDING_GRACE_EXPIRED`

### D-187 Disposition: **(A) Wire `simAllowlistStore`**
- `internal/store/sim_imei_allowlist.go` (verified) is fully implemented (`Add/Remove/List/IsAllowed`, RLS-via-store-layer, ON CONFLICT DO NOTHING).
- Wire-up: pass `*store.SIMIMEIAllowlistStore` into `binding.Enforcer` constructor at `cmd/argus/main.go`. Call `IsAllowed(ctx, tenantID, simID, observedIMEI)` in mode-`allowlist` branch.
- Dead-code suppression at `cmd/argus/main.go:633` (`_ = simAllowlistStore`) — DELETE this line in this story (variable now consumed by enforcer).
- Reasoning: matches STORY-094 plan §Threading (line 257), STORY-094 review handoff note 3, ADR-004 §allowlist mode (per-SIM 1:N list).

### D-189 Disposition: **(A) Implement `bound_sims_count`**
- File touch: `internal/store/imei_pool.go` `List` method (existing, ~line 264). Replace `bound_sims_count=0` placeholder with a single LEFT JOIN COUNT subquery against `sims.bound_imei = pool.imei_or_tac` for `kind='full_imei'` rows; for `kind='tac_range'` rows compute `COUNT(*) WHERE LEFT(sims.bound_imei,8) = pool.imei_or_tac`.
- Reasoning: FE already wires the field (per STORY-095 Gate F-A7 D-189 deferral); dropping the field forces FE refactor. Single-file change; tested via existing `TestIMEIPoolStore_List_*` tests with extended fixture.
- Risk: LEFT JOIN on 1M-row `sims` could be slow — mitigated by partial index `idx_sims_bound_imei` (verify exists; if not add to migration in this story). Subquery also acceptable.

### Grace Window Configuration
- Source: env-var `ARGUS_BINDING_GRACE_WINDOW` (envconfig pattern, default `72h`), parsed via `time.ParseDuration`.
- Add to `internal/config/config.go` `Config` struct: `BindingGraceWindow time.Duration "envconfig:\"ARGUS_BINDING_GRACE_WINDOW\" default:\"72h\""`.
- Per-tenant override **deferred to STORY-097** (D-191 NEW — file in decisions.md). Brief 6 advice followed.
- Document choice in `docs/architecture/CONFIG.md`.

### Performance Strategy (AC-13 ≤5% p95 overhead)
- **NULL short-circuit FIRST**: If `sim.BindingMode == nil` → return Allow before any DB call. NULL is the vast majority (per DEV-410 — existing 1M+ SIMs). 100% of existing AAA-path tests must stay green (AC-17).
- **No new DB calls per request when binding active**: `IMEIPoolStore.LookupKind` is a single `SELECT EXISTS(...)` (already verified ~1ms p99). `SIMIMEIAllowlistStore.IsAllowed` is a `SELECT EXISTS(...)`.
- **Per-pass cache**: STORY-095 Gate F-A8 fix shipped per-pass cache for `device.imei_in_pool` lookups via `sessionCtx.WithContext(ctx)`. Enforcer reuses this cache when calling pool lookup pre-DSL.
- **Microbenchmark substitution**: Live 1M-SIM bench rig is out of CI today (D-184 was re-targeted here from STORY-094, but the rig itself does not exist as runnable infra — verified). Substitute with Go microbench (`internal/policy/binding/enforcer_bench_test.go`) measuring per-mode allocation/CPU overhead vs. NULL baseline. Document in plan addendum + file new **D-192** in ROUTEMAP for live-bench when rig exists.
- **Buffered async history**: AC-11 requires non-blocking history append. `binding.HistoryWriter` channel + worker goroutine.

## Database Schema

> Source: All tables already exist from STORY-094 + STORY-095. STORY-096 adds **zero new tables** and **zero new columns** unless the `bound_imei` partial index is missing.

**Verification step (Task 1):**
```sql
-- Check whether partial index exists for bound_imei (D-189 join performance)
SELECT indexname FROM pg_indexes WHERE tablename = 'sims' AND indexdef LIKE '%bound_imei%';
-- If missing, add migration:
-- migrations/20260503000001_sims_bound_imei_partial_idx.up.sql:
-- CREATE INDEX IF NOT EXISTS idx_sims_bound_imei ON sims (bound_imei) WHERE bound_imei IS NOT NULL;
-- migrations/...down.sql: DROP INDEX IF EXISTS idx_sims_bound_imei;
```

**Existing tables relied on (verified):**
- `sims` (TBL-10) — extended in STORY-094 with `bound_imei`, `binding_mode`, `binding_status`, `binding_verified_at`, `last_imei_seen_at`, `binding_grace_expires_at`.
- `imei_history` (TBL-59) — STORY-094 created; `Append` already implemented at `internal/store/imei_history.go:191`.
- `sim_imei_allowlist` (TBL-60) — STORY-094 created; full CRUD already wired.
- `imei_whitelist` / `imei_greylist` / `imei_blacklist` (TBL-56/57/58) — STORY-095 created; `LookupKind` available.

## API Specifications

> STORY-096 adds **no new HTTP endpoints**. The "Unverified Devices" report uses the existing reports framework (`/api/v1/reports/...`).

**Reports framework extension (existing endpoints, new ReportType):**
- `GET /api/v1/reports/unverified_devices?format=csv|pdf|xlsx&...filters` — uses existing reports router; fans out to new `ReportUnverifiedDevices` type.
- Response: standard envelope wrapping artifact bytes (existing pattern); CSV/PDF/XLSX formatters.
- Filter params (Filters map): `binding_mode` (optional, comma-sep), `binding_status` (default `pending,mismatch`), pagination via existing report cursor.

**SCR-021f / SCR-050 frontend updates:**
- The existing FE surfaces `binding_status` chips. STORY-096 enforcement now writes to `binding_status` (`mismatch`/`pending`/`verified`) — FE renders without code changes (per STORY-094 reviewer note 4).
- Vocab/copy: confirm `mismatch` / `pending` / `verified` / `disabled` / `unbound` chip variants exist in `web/src/components/atoms/Badge.tsx` or equivalent. If missing, add a single-line variant. (No mockup change — chip is already drawn at SCR-021f line 13.)

## Screen Reference (minimal UI surface)

```
SCR-021f Binding Status chip — auto-updates on every auth event.
SCR-050 Live Sessions — `binding_status` column already wired (per STORY-094 plan §SCREENS).
"Unverified Devices" report tile — appears in existing Reports list page (no new screen).
```

## Prerequisites
- [x] STORY-093 closed (commit `42b70c5`) — IMEI capture wired in RADIUS + S6a + SBA.
- [x] STORY-094 closed (commit `8b20650`) — binding columns, `simAllowlistStore`, `IMEIHistoryStore.Append`, DSL `device.*`/`sim.binding_*` parser.
- [x] STORY-095 closed (commit `c46fc34`) — `IMEIPoolStore.LookupKind` functional (full IMEI + TAC range), `device.imei_in_pool('blacklist')` evaluator wired with per-pass cache.

## Tasks

### Task 1: Enforcer package skeleton — Verdict types + decision logic + tests (table-driven)
- **Files:** Create `internal/policy/binding/verdict.go`, `internal/policy/binding/enforcer.go`, `internal/policy/binding/enforcer_test.go`
- **Depends on:** —
- **Complexity:** high
- **Pattern ref:** Read `internal/policy/enforcer/enforcer.go` (existing policy enforcer constructor + `Evaluate` shape — mirror the dependency-injection style and nil-safe defaults). Read `internal/policy/dsl/evaluator_test.go` (table-driven test pattern with `[]struct{name string; …}` and `t.Run(tt.name, …)`).
- **Context refs:** "Enforcer Public Surface", "Six-Mode × Input Decision Table", "Hard-Deny Crosscut", "D-187 Disposition", "Grace Window Configuration"
- **What:** (1) `verdict.go` — `Verdict`, `VerdictKind`, `Severity` types + `Reject(reason, severity)`, `Allow(status)`, `AllowWithAlarm(severity)` constructors + `(b Verdict) Override(other Verdict) Verdict` for blacklist crosscut. (2) `enforcer.go` — `Enforcer` struct with the 7 dependencies (simStore, allowlistStore, poolStore, historyWriter, auditor, notifier, graceWindow) + a small `interface` per dep so tests can mock; `New(...)` constructor; `Evaluate(ctx, sessCtx, sim, protocol, nasIP) Verdict` orchestrator that runs: NULL short-circuit → mode dispatch → blacklist override → return. Keep it pure decision logic — DO NOT do DB writes / audit / notifications inline (those belong to Task 2). The mode dispatch is a switch over `*sim.BindingMode` calling six small private methods (`evalStrict`, `evalAllowlist`, `evalFirstUse`, `evalTacLock`, `evalGracePeriod`, `evalSoft`). (3) `enforcer_test.go` — table-driven test enumerating ALL 25 rows of the decision table + 6 blacklist-override scenarios = 31 cases. Mocks for allowlistStore + poolStore. Each row asserts `Verdict.Kind`, `Verdict.Reason`, `Verdict.Severity`, `Verdict.NewStatus`, `Verdict.LockBoundIMEI`, `Verdict.HistoryAlarm`. Includes NULL-short-circuit path verifying ZERO calls were made on poolStore + allowlistStore (asserts perf claim).
- **Verify:** `go test ./internal/policy/binding/... -run TestEvaluate -v` PASS (31 subtests); `gofmt -w internal/policy/binding/`; `go vet ./internal/policy/binding/...` clean; `go build ./...` succeeds.

### Task 2: Side-effect orchestrator — audit (sync) + notification (async) + buffered history writer
- **Files:** Create `internal/policy/binding/sinks.go`, `internal/policy/binding/history_writer.go`, `internal/policy/binding/sinks_test.go`, `internal/policy/binding/history_writer_test.go`
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** Read `internal/audit/audit.go:17-52` (Auditor interface + CreateEntryParams). Read `internal/bus/session_envelope.go` (envelope builder pattern). Read `internal/notification/dispatcher.go` if present, else `internal/bus/nats.go` Publish helper. Read `internal/aaa/session/session.go` for any existing async-buffered-writer pattern.
- **Context refs:** "Three-Sink Coupling", "Reject Reason Code Catalog", "Six-Mode × Input Decision Table"
- **What:** (1) `sinks.go` — `Apply(ctx, e *Enforcer, sessCtx, sim, verdict, protocol, nasIP)` runs after `Evaluate` returns: synchronous audit emit (via `auditor.CreateEntry`), async NATS publish (via `eventBus.Publish(ctx, bus.SubjectNotification, env)` in goroutine), async history append (via `historyWriter.Enqueue(...)`). Maps mode+verdict to action key + notification subject string per decision table. (2) `history_writer.go` — `HistoryWriter` struct with channel `chan AppendIMEIHistoryParams` (cap 1024), 1 worker goroutine in `Start(ctx)`, `Enqueue(params)` non-blocking with metrics inc on full + drop, `Close(ctx)` graceful flush with deadline. Worker calls `IMEIHistoryStore.Append`. (3) `sinks_test.go` — verify with mock auditor + mock notifier + mock historyWriter that for each verdict type (Allow/Reject/AllowWithAlarm) the right sinks are called with the right payload. (4) `history_writer_test.go` — concurrency test: spawn 100 goroutines each enqueuing 100 rows → assert worker drains all 10000 within deadline; full-channel drop test → assert metric counter inc + WARN log; graceful close test.
- **Verify:** `go test ./internal/policy/binding/... -run 'TestApply|TestHistoryWriter' -v -race` PASS; `gofmt -w internal/policy/binding/`; `go vet ./internal/policy/binding/...` clean.

### Task 3: Wire enforcer into RADIUS (EAP + Direct) — 2 sites
- **Files:** Modify `internal/aaa/radius/server.go`; Create `internal/aaa/radius/binding_enforce_test.go`
- **Depends on:** Task 2
- **Complexity:** medium
- **Pattern ref:** Existing wire site at `internal/aaa/radius/server.go:494-510` (EAP) and `:653-674` (Direct) — STORY-093 + STORY-094 patterns. `internal/aaa/radius/enforcer_nilcache_integration_test.go` for radius-server unit-style assertions.
- **Context refs:** "Wire Sites", "Reject-Reason Wire Payload", "Three-Sink Coupling"
- **What:** (1) `server.go` — add `bindingEnforcer *binding.Enforcer` field to `Server` + `SetBindingEnforcer(e *binding.Enforcer)` setter (nil-safe; nil = skip pre-check, preserve current behavior). At each wire site insert AFTER `sessCtx.SoftwareVersion = sv` and BEFORE `policyEnforcer.Evaluate(...)`: call `verdict := s.bindingEnforcer.Evaluate(ctx, sessCtx, sim, "radius", &nasIP)`; call `binding.Apply(ctx, …)` to fan out sinks; if `verdict.Kind == KindReject` → `s.sendReject(w, r.Packet, verdict.Reason)` (or sendEAPReject equivalent for site #1) + early return + record auth metric. If `KindAllowWithAlarm` or `KindAllow` → continue; persist `sessCtx.BindingStatus = verdict.NewStatus` (so DSL post-pre-check policies see correct status per AC-14). (2) `binding_enforce_test.go` — three tests: (a) NULL mode → no enforcer-internal DB call (mock observable), normal Accept flow; (b) strict mismatch → Access-Reject with Reply-Message containing `BINDING_MISMATCH_STRICT`; (c) blacklist override of soft-allow → reject with `BINDING_BLACKLIST`. Use existing test harness pattern from `enforcer_nilcache_integration_test.go`.
- **Verify:** `go test ./internal/aaa/radius/... -run TestBindingEnforce -v` PASS; full-suite `go test ./internal/aaa/radius/... -count=1` regression PASS; `gofmt -w internal/aaa/radius/`.

### Task 4: Wire enforcer into Diameter S6a (ULR + NTR) + add Error-Message AVP
- **Files:** Modify `internal/aaa/diameter/avp.go`, `internal/aaa/diameter/s6a.go`; Create `internal/aaa/diameter/binding_enforce_test.go`
- **Depends on:** Task 2
- **Complexity:** medium
- **Pattern ref:** `internal/aaa/diameter/s6a.go:62-91` (ULR IMEI capture pattern shipped in STORY-094 T7); `internal/aaa/diameter/imei_test.go` for AVP-level test fixture pattern.
- **Context refs:** "Wire Sites" (rows 3-4), "D-NTR Disposition", "Reject-Reason Wire Payload"
- **What:** (1) `avp.go` — add `AVPCodeErrorMessage uint32 = 281` constant + comment referencing RFC 6733 §7.3. (2) Add helper `NewErrorAnswerWithMessage(req *Message, resultCode uint32, msg string) *Message` that builds an Answer with Result-Code + Error-Message AVP. (3) `s6a.go` — extend `S6aHandler` struct with `bindingEnforcer *binding.Enforcer` + setter (nil-safe). At wire site #3 (ULR, line 73 — after IMEI-capture block, BEFORE `if h.sessionMgr != nil && imsi != ""` session-create block): build `sessCtx` from imsi+imei+sv → load `sim` via SIMStore lookup (S6a path needs SIM context — verify if existing code already does this; if not, this is the addition) → call `Evaluate` → if Reject, return `NewErrorAnswerWithMessage(msg, ResultCodeUnableToComply, verdict.Reason)` and skip session create. At wire site #4 (NTR, line 151): same flow but per §D-NTR Disposition: even on Reject, return Result-Code 5012 + Error-Message but log explicitly "NTR rejection is signaling-only — caller must initiate session-clear". (4) `binding_enforce_test.go` — three cases: NULL passthrough; strict mismatch ULR → Result-Code 5012 + Error-Message=`BINDING_MISMATCH_STRICT`; NTR same-mismatch with §D-NTR doc-string check.
- **Verify:** `go test ./internal/aaa/diameter/... -run TestBindingEnforce -v` PASS; full-suite regression PASS; `gofmt -w internal/aaa/diameter/`.

### Task 5: Wire enforcer into 5G SBA (AUSF + UDM) — 2 sites
- **Files:** Modify `internal/aaa/sba/ausf.go`, `internal/aaa/sba/udm.go`; Create `internal/aaa/sba/binding_enforce_test.go`
- **Depends on:** Task 2
- **Complexity:** medium
- **Pattern ref:** `internal/aaa/sba/ausf.go:53-113` (HandleAuthentication request flow + ParsePEI usage shipped in STORY-093). `internal/aaa/sba/imei_test.go` for handler-level test fixture pattern.
- **Context refs:** "Wire Sites" (rows 5-6), "Reject-Reason Wire Payload"
- **What:** (1) `ausf.go` — add `bindingEnforcer *binding.Enforcer` to `AUSFHandler` + setter. At wire site #5 (line 70 — after `imei, imeiSV, _ := ParsePEI(...)`): build sessCtx → SIM lookup via SUPI (SBA path; verify the existing handler does it and reuse) → `Evaluate` → if Reject, `writeProblem(w, http.StatusForbidden, "BINDING_REJECTED", verdict.Reason)` + return; else continue to existing `req.ServingNetworkName` validation. (2) `udm.go` — same pattern at wire site #6 (line 153 of `HandleAuthRequest` or equivalent — verify name). (3) `binding_enforce_test.go` — three cases for AUSF + three for UDM: NULL passthrough; strict mismatch → 403 problem-details with `cause: "BINDING_MISMATCH_STRICT"`; blacklist override → 403 with `cause: "BINDING_BLACKLIST"`.
- **Verify:** `go test ./internal/aaa/sba/... -run TestBindingEnforce -v` PASS; full-suite regression PASS; `gofmt -w internal/aaa/sba/`.

### Task 6: Reports framework — `ReportUnverifiedDevices` type + provider + dispatchers
- **Files:** Modify `internal/report/types.go`, `internal/report/csv.go`, `internal/report/pdf.go`, `internal/report/excel.go`, `internal/report/store_provider.go`; Create `internal/report/unverified_devices_test.go`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `internal/report/types.go:21` (`ReportSIMInventory` const + interface method + `SIMInventoryData` struct — mirror exactly). Read `internal/report/csv.go:69-74` (`case ReportSIMInventory` dispatcher). Read `internal/report/store_provider.go` for the `SIMInventory` provider impl SQL — mirror it.
- **Context refs:** "Components Involved" (report extension scope), "API Specifications" (report endpoint reuse)
- **What:** (1) `types.go` — add `ReportUnverifiedDevices ReportType = "unverified_devices"` const + `UnverifiedDevicesData` struct (Columns/Rows/Summary/PeriodFrom/PeriodTo, mirror `SIMInventoryData`) + add `UnverifiedDevices(ctx, tenantID, filters) (*UnverifiedDevicesData, error)` method to `DataProvider` interface. (2) `csv.go`/`pdf.go`/`excel.go` — add `case ReportUnverifiedDevices: data, err := e.provider.UnverifiedDevices(ctx, ...); ...` block (3 lines + helper). (3) `store_provider.go` — implement `UnverifiedDevices` SQL: `SELECT iccid, id::text, binding_mode, binding_status, last_imei_seen_at, bound_imei FROM sims WHERE tenant_id = $1 AND binding_status IN ('pending','mismatch') ORDER BY last_imei_seen_at DESC NULLS LAST LIMIT 10000`. Columns: `["ICCID","SIM ID","Binding Mode","Binding Status","Last IMEI Seen","Bound IMEI"]`. (4) `unverified_devices_test.go` — fixture with 3 SIMs (pending/mismatch/verified) → assert only 2 rows in output, columns match.
- **Verify:** `go test ./internal/report/... -run TestUnverifiedDevices -v` PASS; `gofmt -w internal/report/`; `go build ./...` succeeds.

### Task 7: Config + main.go threading + `bound_sims_count` (D-189)
- **Files:** Modify `internal/config/config.go`, `cmd/argus/main.go`, `internal/store/imei_pool.go`, `internal/store/imei_pool_test.go`, `docs/architecture/CONFIG.md`
- **Depends on:** Task 1, Task 2, Task 3, Task 4, Task 5, Task 6
- **Complexity:** high
- **Pattern ref:** `internal/config/config.go:42-58` (envconfig fields with `time.Duration` example). `cmd/argus/main.go:1405-1431` (existing policy enforcer wiring + `SetMetricsRegistry` / `SetIMEIPoolLookuper` setter pattern + nil-safe wiring). `internal/store/imei_pool.go:264` (existing `List` method — extend with bound_sims_count subquery).
- **Context refs:** "D-187 Disposition", "D-189 Disposition", "Grace Window Configuration", "Wire Sites"
- **What:** (1) `config.go` — add `BindingGraceWindow time.Duration "envconfig:\"ARGUS_BINDING_GRACE_WINDOW\" default:\"72h\""`. (2) `main.go` — instantiate `bindingHistoryWriter := binding.NewHistoryWriter(imeiHistoryStore, log.Logger, metricsReg)` after the existing `imeiHistoryStore := store.NewIMEIHistoryStore(...)` line; call `bindingHistoryWriter.Start(ctx)` early + register `bindingHistoryWriter.Close(shutdownCtx)` in graceful-shutdown sequence. Instantiate `bindingEnforcer := binding.New(simStore, simAllowlistStore, imeiPoolStore, bindingHistoryWriter, auditSvc, eventBus, cfg.BindingGraceWindow, log.Logger)`. **Delete `_ = simAllowlistStore` line at `cmd/argus/main.go:633`** (variable now consumed). Wire setter on radiusServer / S6aHandler / AUSFHandler / UDMHandler: `radiusServer.SetBindingEnforcer(bindingEnforcer)`, `s6aHandler.SetBindingEnforcer(bindingEnforcer)`, `ausfHandler.SetBindingEnforcer(bindingEnforcer)`, `udmHandler.SetBindingEnforcer(bindingEnforcer)`. (3) `imei_pool.go` — extend `List` with COUNT subquery for `bound_sims_count` per "D-189 Disposition" SQL; preserve existing pagination/filters. (4) `imei_pool_test.go` — extend a `TestIMEIPoolStore_List_*` test (find existing) with fixture: 1 pool entry with 3 SIMs bound (bound_imei matches) + 1 pool entry with 0 SIMs → assert returned `BoundSimsCount` = 3 and 0 respectively. (5) `CONFIG.md` — add row to env var table for `ARGUS_BINDING_GRACE_WINDOW`.
- **Verify:** `go build ./...` PASS; `go vet ./...` clean; `gofmt -w` on all touched files; full `go test -count=1 ./...` PASS; `grep -n '_ = simAllowlistStore' cmd/argus/main.go` returns 0; `make db-seed` succeeds and `psql -c "SELECT COUNT(*) FROM sims WHERE binding_mode IS NOT NULL"` returns 0 (regression — DEV-410).

### Task 8: Integration tests across 3 protocols + decision-table E2E + perf microbench
- **Files:** Create `internal/policy/binding/integration_test.go` (build tag `integration`), `internal/policy/binding/enforcer_bench_test.go`
- **Depends on:** Task 3, Task 4, Task 5, Task 6, Task 7
- **Complexity:** high
- **Pattern ref:** Read existing `_test.go` with `//go:build integration` tag in the project (e.g., `internal/store/sim_test.go` or any file under `internal/aaa/` integration suite). For benchmarks read `internal/aaa/diameter/imei_test.go` `BenchmarkExtractTerminalInformation_S6a` pattern.
- **Context refs:** "Six-Mode × Input Decision Table", "Hard-Deny Crosscut", "Performance Strategy"
- **What:** (1) `integration_test.go` — Postgres-backed integration suite with one tenant + one SIM per mode (6 SIMs) + IMEI pool entry for blacklist hard-deny. Tests fire authentication via `Enforcer.Evaluate` directly against real stores; assert: (a) all 25 decision-table rows produce expected (verdict, audit row in DB, NATS publish counter inc, imei_history row), (b) blacklist override producing `BINDING_BLACKLIST` reason regardless of mode, (c) audit hash chain verifier (`auditStore.Verify`) returns Verified=true after a 50-row mixed run (AC-16), (d) `make db-seed` smoke check after suite (regression). Tests also fire one auth via each of the three real protocol handlers (RADIUS, Diameter, SBA) for a strict-mismatch SIM — assert wire-level reject reason carries the right code (Reply-Message / Result-Code+Error-Message / problem-details cause). (2) `enforcer_bench_test.go` — `BenchmarkEnforcerEvaluate_NULLMode` (must show ≤1µs/op + 0 allocs, asserting NULL short-circuit pre-DB-call); `BenchmarkEnforcerEvaluate_StrictMatch`, `BenchmarkEnforcerEvaluate_AllowlistHit`, `BenchmarkEnforcerEvaluate_BlacklistMiss`, `BenchmarkEnforcerEvaluate_GracePeriod` — capture CPU/alloc deltas vs. NULL baseline and document in plan addendum at story close. Live 1M-SIM bench substitution rationale documented as **D-192 NEW** in this story; Reviewer files in ROUTEMAP at close.
- **Verify:** `go test -tags=integration -count=1 ./internal/policy/binding/...` PASS (DB required); `go test -bench=. -benchmem ./internal/policy/binding/...` runs cleanly; benchmark output captured into `docs/stories/phase-11/STORY-096-perf-addendum.md` (Reviewer artifact, not a code file).

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| AC-1 (enforcer + Verdict + 3-protocol wiring) | Task 1, 3, 4, 5 | Task 1 unit; Task 3-5 integration |
| AC-2 (NULL short-circuit, status='disabled', no side-effects) | Task 1 | Task 1 NULL-path test (zero mock calls); Task 8 perf bench (0 allocs) |
| AC-3 (strict mode reject + audit + notif + history) | Task 1 (logic), Task 2 (sinks), Task 3-5 (wire) | Task 1 row #3-5; Task 8 integration |
| AC-4 (allowlist mode — IsAllowed wire) | Task 1 (logic — D-187 wire), Task 7 (delete `_=`) | Task 1 row #6-8; Task 8 integration |
| AC-5 (first-use capture + status verified) | Task 1 (logic + LockBoundIMEI), Task 2 (post-allow SIM update) | Task 1 row #9-12; Task 8 integration |
| AC-6 (tac-lock first-8-digit compare) | Task 1 (private `tac()` helper) | Task 1 row #13-16; Task 8 integration |
| AC-7 (grace-period — env-var window + grace_expires_at) | Task 1 (logic), Task 7 (env-var) | Task 1 row #17-21; Task 8 integration |
| AC-8 (soft mode AllowWithAlarm) | Task 1 (logic) | Task 1 row #22-25; Task 8 integration |
| AC-9 (blacklist hard-deny override ALL modes) | Task 1 (`Verdict.Override` crosscut) | Task 1 +6 override scenarios; Task 8 |
| AC-10 (3 sinks per Reject + protocol-correct wire reason) | Task 2 (sinks), Task 3-5 (wire encoders) | Task 2 sinks_test; Task 3-5 wire-level test |
| AC-11 (history Allow + AllowWithAlarm; async buffered) | Task 2 (HistoryWriter) | Task 2 history_writer_test (concurrency + drop) |
| AC-12 (Unverified Devices report) | Task 6 | Task 6 unverified_devices_test |
| AC-13 (≤5% p95 latency overhead) | Task 1 (NULL short-circuit), Task 8 (bench) | Task 8 microbench addendum |
| AC-14 (DSL post-pre-check sees binding_status) | Task 3-5 (sessCtx.BindingStatus = verdict.NewStatus before policy.Evaluate) | Task 8 integration test wiring DSL eval after pre-check |
| AC-15 (RBAC — tenant-scoped queries) | Task 1 (uses tenant-scoped store calls) + STORY-094 RLS-via-store-layer | Task 8 cross-tenant negative test |
| AC-16 (audit hash chain valid) | Task 2 (synchronous audit) | Task 8 mixed-mode 50-row hash-chain verify |
| AC-17 (regression — full test suite green) | All tasks (NULL short-circuit) | `go test -count=1 ./...` post-Task 7 |

## Story-Specific Compliance Rules

- **API:** No new endpoints. Existing reports endpoint serves the new `unverified_devices` ReportType.
- **DB:** No new tables; Task 1 verifies optional `idx_sims_bound_imei` partial index exists or adds a single migration pair.
- **DSL (ADR-004 ordering — DSL_GRAMMAR.md line 308):** Binding pre-check runs BEFORE policy DSL evaluation. DSL post-pre-check rules can read `device.binding_status` (set by enforcer) but cannot weaken a hard reject. Verified by Task 8 integration.
- **Business (DEV-410):** Existing SIMs remain `binding_mode IS NULL`; `make db-seed` post-Task 7 must produce zero rows with non-NULL binding_mode.
- **AAA/F-A2 (STORY-093/094 contract):** `Session.IMEI` / `SoftwareVersion` persist in-memory + Redis only — STORY-096 writes to existing DB columns (`sims.binding_status`, `sims.binding_verified_at`, `sims.bound_imei`, `sims.binding_grace_expires_at`); no extension of `radius_sessions` / sessions DB rows.
- **ADR-004:** Local enforcement only (no S13/N17). Six modes × 3 protocols implemented per ADR-004 §Architecture.
- **PAT-006 (RECURRENCE prevention):** Enforcer's `SetDeviceBinding` writes pass full pre-fetched state pointer values for any field NOT being changed (PATCH-style partial UPDATE). Verified at code review — F-A2 from STORY-094 Gate.

## Bug Pattern Warnings

- **PAT-006 RECURRENCE family:** Any enforcer-driven `SIMStore.SetDeviceBinding` call must pre-fetch SIM state and pass through unchanged binding fields explicitly — never pass nil for a field intended to be preserved. Code review checks for this.
- **PAT-009:** Enforcer reads `*sim.BindingMode` (pointer); always `if sim.BindingMode == nil { return Allow }` first. Pointer dereferences MUST be nil-guarded; the `Verdict.NewStatus` writes use `&status` for the pointer-typed `SetDeviceBinding` arg.
- **PAT-011 / PAT-017 RECURRENCE:** New enforcer struct receives 7 dependencies (simStore, allowlistStore, poolStore, historyWriter, auditor, notifier, graceWindow, logger) threaded from `cmd/argus/main.go`. Threading path documented at "Wire Sites" + Task 7 §What. SetBindingEnforcer setter on each of 4 receivers (radiusServer, s6aHandler, ausfHandler, udmHandler).
- **PAT-022:** No new string-enum values introduced. Existing `ValidBindingStatuses = ['verified','pending','mismatch','unbound','disabled']` re-used. Reject reason codes (`BINDING_MISMATCH_STRICT` etc.) added to `internal/apierr/apierr.go` Go const set + ERROR_CODES.md row simultaneously.
- **PAT-023:** No schema changes (other than optional `idx_sims_bound_imei`); first-boot `schemacheck` exercised by Task 7 verify (`make db-seed`).
- **PAT-025:** IMEI/IMSI string discipline — enforcer reads `sessCtx.IMEI` (15-digit IMEI) NOT `sessCtx.IMSI` (15-digit IMSI). TAC compare uses first-8-of-IMEI. Code review checks variable naming.
- **PAT-026 inverse-orphan:** History writer is **NOT** a `JobProcessor` — pure in-process channel + worker goroutine. PAT-026 does not apply (no AllJobTypes registration needed). Documented in §Three-Sink Coupling. Graceful flush on shutdown is the analogous discipline.
- **PAT-031 (tri-state JSON pointer):** No new PATCH handler in this story; STORY-094 owned that surface. No applicable risk.

## Tech Debt (from ROUTEMAP)

- **D-184 (1M-SIM bench):** Re-targeted from STORY-094 to STORY-096 by STORY-094 reviewer. **Disposition: substitute with microbenchmarks** (Task 8). Live-rig follow-up filed as new D-192. ROUTEMAP D-184 status to be updated by Reviewer to `✓ RESOLVED-WITH-SUBSTITUTION [STORY-096 Task 8]`.
- **D-187 (`simAllowlistStore` dormant):** Disposition (A) — **WIRED** in this story (Task 1 + Task 7). ROUTEMAP D-187 to be updated to `✓ RESOLVED [STORY-096 Tasks 1, 7]`.
- **D-189 (`bound_sims_count=0` placeholder):** Disposition (A) — **IMPLEMENTED** in this story (Task 7). ROUTEMAP D-189 to be updated to `✓ RESOLVED [STORY-096 Task 7]`.
- **D-188 (API-335 `bound_sims` + `history` empty):** Targets STORY-097 — **NOT** addressed here. Confirmed.
- **D-192 NEW:** Live 1M-SIM benchmark rig (replaces D-184 substitution). Filed by Reviewer at story close, target = future Phase 11/12 work.
- **D-191 NEW:** Per-tenant `binding_grace_window` override (current STORY-096 ships env-var-only). Filed by Reviewer, target = STORY-097 or future phase.

## Mock Retirement
N/A — backend-only. Existing FE chip rendering at SCR-021f / SCR-050 picks up new `binding_status` values without code changes (verified per STORY-094 review §Documents Updated).

## Risks & Mitigations

- **R1: NTR rejection semantics ambiguous.** S6a NTR is post-attach; Reject Result-Code does not auto-tear-down. Mitigated by §D-NTR Disposition: enforce + log + WARN + recommend operator initiate session-clear separately. Documented in audit row.
- **R2: Buffered history writer drops under load.** Mitigation: cap=1024 × 1 worker drains ~10k rows/s on test rig (Task 2 concurrency test asserts). Drop metric `argus_binding_history_drops_total` exposed for ops alerting. If drops exceed threshold in prod → file follow-up to scale workers.
- **R3: Audit synchronous call stalls hot path.** Mitigation: `audit.Auditor.CreateEntry` is sub-ms p99 in current measurements (verified via existing `internal/audit/audit_test.go` benchmarks). NULL short-circuit means 99%+ of requests skip this entirely. Microbench (Task 8) confirms ≤5% overhead claim.
- **R4: `bound_sims_count` LEFT JOIN slow on 1M sims.** Mitigation: Task 1 verifies `idx_sims_bound_imei` partial index exists; subquery COUNT uses index lookup. Tested at Task 7 verify against seeded data.
- **R5: Cross-protocol verdict consistency.** Mitigation: Task 8 integration test runs the same SIM through all 3 protocols and asserts identical Verdict. Single Enforcer instance shared across all 4 receivers (Task 7 wiring).
- **R6: `simAllowlistStore.IsAllowed` hot-path call adds DB latency.** Mitigation: only fires when `binding_mode='allowlist'` (small minority of SIMs); single `SELECT EXISTS` query; index on PK `(sim_id, imei)`.
- **R7: Per-tenant grace-window deferred to env-var.** Mitigation: documented in §Grace Window Configuration; D-191 filed; v1 customers all share the 72h default which matches ADR-004 §Migration recommendation.

## Validation Trace (Planner Quality Gate appendix)

> Re-verifiable scenarios — each walks through the enforcer end-to-end against the decision table.

**V1 — `tac()` semantics for tac-lock (AC-6):**
- Input bound `"359211089765432"` (TAC `35921108`), observed `"359211089999999"` (TAC `35921108`) → tac match → Allow row #13. ✅
- Input bound `"359211089765432"`, observed `"864120605431122"` (TAC `86412060`) → tac differ → Reject `BINDING_MISMATCH_TAC` severity medium row #14. ✅

**V2 — Grace-period exact timestamp logic (AC-7):**
- Setup: `binding_grace_expires_at = 2026-05-03T15:00:00Z`, `graceWindow=72h`.
- Auth at `2026-05-03T14:59:59Z` with new IMEI → `NOW() < expires_at` → row #19 → Allow + status `pending` + notification `device.binding_grace_change`. ✅
- Auth at `2026-05-03T15:00:01Z` with new IMEI → `NOW() >= expires_at` → row #20 → Reject `BINDING_GRACE_EXPIRED`. ✅

**V3 — Blacklist override (AC-9 / Brief 8 worked example):**
- Mode `strict`, bound=observed=`"359211089765432"` → mode-check would Allow row #2.
- Blacklist contains TAC `35921108` (TAC-range row in `imei_blacklist` per STORY-095) → `LookupKind` returns true.
- `Verdict.Override` → final Reject `BINDING_BLACKLIST` (NOT `BINDING_MISMATCH_STRICT` — blacklist code wins). ✅
- Notification subject = `device.binding_blacklist_hit`, severity high, status='mismatch', audit action=`sim.binding_blacklist_hit`. ✅

**V4 — Wire reason on each protocol (AC-10):**
- RADIUS Direct, strict-mismatch SIM → Access-Reject + Reply-Message=`BINDING_MISMATCH_STRICT`. Reply-Message ≤247 bytes (string is 24 bytes) ✅
- Diameter ULR, same SIM → ULA with Result-Code=5012 + Error-Message AVP=`BINDING_MISMATCH_STRICT`. ✅
- 5G AUSF Authenticate, same SIM → HTTP 403 + problem-details `{type, title, status:403, cause:"BINDING_MISMATCH_STRICT", detail:"..."}`. ✅

**V5 — Three sinks per single mismatch event (AC-10):**
- Single strict-mismatch event produces:
  1. Audit row (synchronous, hash-chained): action=`sim.binding_mismatch`, before/after JSON with observed/bound IMEIs.
  2. NATS envelope (async): subject=`argus.events.notification.dispatch`, payload event_type=`device.binding_failed`, severity=`high`.
  3. `imei_history` row (async-buffered): `was_mismatch=true`, `alarm_raised=true`, `capture_protocol="radius"|"diameter_s6a"|"5g_sba"`, `nas_ip_address` populated.
- Total = 3 separate writes per event. ✅

**V6 — NULL short-circuit (AC-2 / AC-13 perf):**
- SIM with `BindingMode==nil` → enforcer returns Allow before any DB call.
- `sessCtx.BindingStatus = "disabled"` set so DSL can match `device.binding_status == "disabled"` if any policy needs it.
- Zero side-effects: no audit row, no NATS publish, no history append.
- Microbench `BenchmarkEnforcerEvaluate_NULLMode` asserts 0 allocs / op (sub-µs). ✅

**V7 — Audit hash chain integrity (AC-16):**
- Run 50 mixed-mode auths producing 50 audit rows: 10× `sim.binding_mismatch` (strict), 5× `sim.binding_first_use_locked`, 8× `sim.binding_grace_change`, 2× `sim.binding_soft_mismatch`, 5× `sim.binding_blacklist_hit`, 20× (no audit — Allow rows).
- Total = 30 audit rows.
- Call `auditStore.Verify(ctx, tenantID)` → `Verified=true, EntriesChecked=30, FirstInvalid=nil`. ✅

**V8 — D-NTR signaling-only Reject (Task 4):**
- S6a NTR with strict-mismatch IMEI → enforcer returns Reject `BINDING_MISMATCH_STRICT`.
- Wire response: ULA-shaped Answer with Result-Code=5012 + Error-Message AVP=`BINDING_MISMATCH_STRICT`.
- Logger emits WARN with `signaling_only=true` field — operator-actionable.
- Audit + notification + history all fire identically to ULR path.
- Existing session in `sessions` table NOT torn down by this story — operator must initiate session-clear via separate path. ✅ (documented in §D-NTR)

**V9 — Allowlist hit (AC-4):**
- Mode `allowlist`, sim_imei_allowlist contains `["359211089765432","864120601234567"]`.
- Auth with observed=`"359211089765432"` → `IsAllowed` returns true → row #6 → Allow + status=`verified` + binding_verified_at=NOW(). ✅
- Auth with observed=`"123456789012345"` → `IsAllowed` returns false → row #7 → Reject `BINDING_MISMATCH_ALLOWLIST` severity high. ✅
- Cross-tenant SIM ID → `IsAllowed` returns `(false, nil)` per existing store contract (STORY-094) → also row #7. ✅

**V10 — `bound_sims_count` correctness (D-189):**
- Pool `imei_whitelist` row: `imei_or_tac="35921108"`, `kind="tac_range"`.
- 3 SIMs in same tenant with `bound_imei` starting with `"35921108"` (e.g., `"359211081111111","359211082222222","359211083333333"`).
- 1 SIM with `bound_imei="864120601234567"` (different TAC).
- API-331 List call → row returns `bound_sims_count=3` for the TAC-range pool entry, `bound_sims_count=1` for any full_imei row matching one of those SIMs. ✅

## Pre-Validation Self-Check

- [x] Min plan lines ≥100 (L effort): well over (this plan is ≈540 lines).
- [x] Min task count ≥5 (L effort): **8 tasks**.
- [x] At least 1 `Complexity: high` task: **4 high tasks** (Task 1, Task 2, Task 7, Task 8).
- [x] Required sections present: Goal, Architecture Context, Tasks, Acceptance Criteria Mapping, Bug Pattern Warnings, Tech Debt, Risks, Validation Trace, Pre-Validation.
- [x] No new API endpoints — existing reports endpoint is reused; documented.
- [x] No new DB tables — existing TBL-10/56/57/58/59/60 reused; optional partial index documented.
- [x] No UI surface — Design Token Map / Component Reuse marked N/A; chip render handled by existing FE.
- [x] Each task has `Pattern ref` (verified to exist), `Context refs` pointing to in-plan sections, `Verify` command including `gofmt -w`.
- [x] All `Context refs` point to sections that exist in this plan (Wire Sites; Six-Mode × Input Decision Table; Hard-Deny Crosscut; Three-Sink Coupling; D-187/D-189 Disposition; Grace Window Configuration; Performance Strategy; Reject Reason Code Catalog; Reject-Reason Wire Payload; Enforcer Public Surface; SessionContext shape; SIM struct binding fields; D-NTR Disposition).
- [x] D-187 disposition explicit (A) WIRE; D-189 disposition explicit (A) IMPLEMENT; D-184 substitution + D-192 follow-up filed; D-191 per-tenant grace deferred.
- [x] Wire-site enumeration is concrete (file:line, 6 sites, NTR semantics flagged).
- [x] Six-mode × input decision table embedded as test oracle (25 rows + 6 blacklist crosscuts).
- [x] Three-sink coupling specified (audit sync / NATS async / history buffered) with failure handling per sink.
- [x] Reject-reason wire payload per protocol specified with file:line + helper to add.
- [x] Validation Trace V1–V10 included covering each AC + worked-example examples.
- [x] PAT-006/009/011/017/022/023/025/026/031 family addressed.
- [x] decisions.md entries identified to file at story close: D-187 disposition; D-189 disposition; 3-sink coupling decision; perf substitution + D-192; env-var grace window + D-191; D-NTR signaling-only.

## decisions.md Entries (route to ROUTEMAP at close)

- **VAL-NNN-1:** D-187 disposition (A) — `simAllowlistStore` wired into binding enforcer for `binding_mode='allowlist'` IsAllowed check; `_ = simAllowlistStore` dead-code line deleted from `cmd/argus/main.go`.
- **VAL-NNN-2:** D-189 disposition (A) — `bound_sims_count` implemented via single LEFT JOIN COUNT subquery in `IMEIPoolStore.List`; FE no-change.
- **VAL-NNN-3:** Three-sink coupling — audit synchronous (chain integrity), notification async via NATS (existing `bus.SubjectNotification`), history async via in-process buffered writer (cap 1024, single worker, drop+metric on full).
- **VAL-NNN-4:** Performance substitution — D-184 1M-SIM live bench replaced by Go microbench in this story; new D-192 NEW filed for live bench when rig exists.
- **VAL-NNN-5:** Grace window configuration — env-var `ARGUS_BINDING_GRACE_WINDOW` (default 72h); per-tenant override deferred via D-191 NEW (target STORY-097 or later).
- **VAL-NNN-6:** D-NTR disposition — S6a NTR Reject is signaling-only; enforce + audit + notify + history but rely on operator session-clear for actual teardown. Documented in audit row metadata.
- **VAL-NNN-7:** History writer is in-process channel + worker (NOT JobProcessor) — PAT-026 does not apply; graceful flush on shutdown is the substitute discipline.
- **VAL-NNN-8:** Reject reason code catalog — 5 new codes (`BINDING_MISMATCH_STRICT/_ALLOWLIST/_TAC`, `BINDING_BLACKLIST`, `BINDING_GRACE_EXPIRED`) added to `internal/apierr/apierr.go` + ERROR_CODES.md table simultaneously per PAT-022 discipline.
- **VAL-NNN-9:** Diameter Error-Message AVP (281) added to `internal/aaa/diameter/avp.go` + new builder helper `NewErrorAnswerWithMessage` per RFC 6733 §7.3.

## Closing Note for Reviewer

- Plan saved at: `docs/stories/phase-11/STORY-096-plan.md` (this file).
- Reviewer date discipline: use `2026-05-03` for ROUTEMAP `Completed` column when story closes.
- Microbench output captured into `docs/stories/phase-11/STORY-096-perf-addendum.md` at story close (referenced by AC-13 verifier).

# Fix Plan: FIX-253 — Suspend IP release + activate empty-pool guard + audit-on-failure (FIX-252 spinoff)

> **Mode:** FIX (backend hardening, post-FIX-252) — Tier P2 / Effort S
> **Track:** UI Review Remediation (Wave 2.5+3)
> **Surfaced by:** FIX-252 Discovery (DEV-388 spinoff rationale, 2026-04-26) + advisor analysis
> **Inherits from:** FIX-252 plan §Tasks 2/3/4/5 (handler guard, nullable IP, suspend IP release, regression tests). FIX-252 closure shipped zero Go code (`make db-reset` resolved the surface symptom — PAT-023 schema drift). Three latent backend defects + one verb-decision survive that fix; this story owns them.

---

## Goal

Eliminate three latent backend defects in the SIM activate/suspend path and resolve the Resume-vs-Activate verb question:

1. **`SIMStore.Suspend` leaks the SIM's allocated IP** — pool counter `used_addresses` drifts, eventually triggers spurious `POOL_EXHAUSTED` 422s on healthy tenants. Static allocations must be preserved.
2. **`Handler.Activate` has no empty-pool guard** — `len(pools)==0` falls through to `simStore.Activate(..., uuid.Nil, ...)` → FK `fk_sims_ip_address` violation (SQLSTATE 23503) → bare 500.
3. **`Handler.Activate` failure paths produce no audit entries** — only success path is audited; CLAUDE.md mandates "every state-changing operation creates an audit log entry" and failure attempts are state-meaningful (correlation, classification, security forensics).
4. **Resume verb resolution** — Discovery confirmed FE SIM-detail page (`web/src/pages/sims/detail.tsx:101`) calls `/resume`, NOT `/activate`. With Suspend now releasing IP, Resume needs explicit re-allocation OR explicit "no-IP-on-resume" semantics OR deprecation.

Add regression tests so none of the four defects can return.

---

## Inheritance from FIX-252 plan

The original FIX-252 plan (`docs/stories/fix-ui-review/FIX-252-plan.md`) defined Tasks 2/3/4/5 that shipped no code under FIX-252. The specs are extracted and adapted below — DON'T duplicate, just reference. Decision history:

- Old DEV-386 (POOL_EXHAUSTED stays HTTP 422) — STILL APPLIES, restated as DEV-390 in this plan.
- Old DEV-387 (Suspend MUST release IP) — STILL APPLIES, restated as DEV-391 in this plan.
- Old DEV-388 (Resume verb provisional decision) — RESOLVED in this plan as DEV-392 after FE call-pattern audit.
- Plus new DEV-393 (audit-on-failure semantics).

`decisions.md` already has DEV-386/387/388 from FIX-252; the FIX-253 entries land at **DEV-390..DEV-393** (next free is DEV-390 because DEV-389 is the FIX-251 plan-pivot entry).

---

## Pre-Dev Impact Audit (gating, 2026-04-26)

Before writing any code, audit AAA + policy code for reads of `sims.ip_address_id` on suspended SIMs (R2 from FIX-252 plan). Discovery results:

**Files searched:** `internal/aaa/diameter/gx.go`, `internal/aaa/radius/server.go`, `internal/policy/**`.

| Read site | What it does | Suspended SIM impact |
|-----------|--------------|----------------------|
| `radius/server.go:516-517` (Access-Accept Framed-IP build) | Reads `sim.IPAddressID` to attach Framed-IP attribute on auth-accept. | NEVER fires for suspended SIMs — the operator-policy check upstream rejects auth before reaching this block. |
| `radius/server.go:684-685` + `:828-829` (Acct-Stop release path) | Reads `sim.IPAddressID` to release IP on session stop. | Suspended SIMs by definition have no active session; if a stale Acct-Stop arrives mid-suspend, `GetIPAddressByID` returns `ErrIPNotFound` (already-released row deleted reference) which the existing code path tolerates as a Warn-log, not an error. **No regression.** |
| `radius/server.go:1059-1063` + `diameter/gx.go:413-417` (CCR-T / Acct-Stop release) | Same pattern. | Same — idempotent release semantics. **No regression.** |
| `diameter/gx.go:202-203` (CCR-I Framed-IP attach) | Reads `sim.IPAddressID` to populate CCA-I if SIM has a preallocated IP. | NEVER fires for suspended SIMs — auth rejected upstream. |
| `internal/policy/**` (full grep) | No direct reads of `sims.ip_address_id`. Policy engine consumes APN ID + tenant policy DSL only. | **No regression.** |

**Verdict:** AC-1 (DEV-391) is safe to ship. The AAA layer expects `sim.IPAddressID` to be **mutable** (it's set by AAA itself in dynamic-alloc paths) and tolerates absence (every read site null-checks `sim.IPAddressID != nil`). Releasing IP at suspend time matches the AAA mental model perfectly.

**No blockers identified.** Plan proceeds to implementation tasks.

---

## Architecture Context

### Components Involved

- **Handler:** `internal/api/sim/handler.go`
  - `Handler.Activate` — line 974 (verified 2026-04-26 by grep)
  - `Handler.Suspend` — line 1086
  - `Handler.Resume` — line 1139 (NOT 1149 as FIX-252 plan said)
- **Store (SIM):** `internal/store/sim.go`
  - `SIMStore.Activate` — line 420 (signature `(ctx, tenantID, simID uuid.UUID, ipAddressID uuid.UUID, userID *uuid.UUID)`)
  - `SIMStore.Suspend` — line 467
  - `SIMStore.Resume` — line 512
  - `SIMStore.Terminate` — line 562 (reference for IP-release-on-state-change pattern, lines 599-608)
- **Store (IP pool):** `internal/store/ippool.go`
  - `IPPoolStore.AllocateIP` — line 664
  - `IPPoolStore.ReleaseIP` — line 727 (canonical pattern: lines 750-768 are the static-vs-dynamic branch we mirror)
- **Error envelope:** `internal/apierr/apierr.go`
  - `CodePoolExhausted` (~line 79), `CodeInvalidStateTransition`, `CodeNotFound`, `CodeInternalError`
  - Helper `apierr.WriteError(w, status, code, msg)` — never raw `http.Error` or `w.WriteHeader(500)`
- **Audit:** `Handler.createAuditEntry(r, action, target, before, after, userID)` — already invoked on success at line 1054 (Activate); MISSING on every failure branch
- **State machine:** `internal/store/sim.go:88-97`
  - `"suspended": {"active", "terminated"}` — direct `suspended → active` is allowed via `Activate`
  - Resume is a separate convenience verb that hits a similar code path (Resume only allows `suspended → active` transition; Activate allows `stock|suspended|stolen_lost → active`)

### Data Flow (current — buggy)

```
Suspend:
  POST /api/v1/sims/{id}/suspend
    → Handler.Suspend
      → simStore.Suspend
          UPDATE sims SET state='suspended', suspended_at=NOW()
          (NO IP release — leak!)        ← BUG #1
          INSERT state_history
      → 200 + audit + envelope publish

Activate (with empty-pool APN):
  POST /api/v1/sims/{id}/activate
    → Handler.Activate
      → simStore.GetByID
      → ippoolStore.List → []   (zero pools for this APN)
      → if len(pools) > 0 { ... }   ← skipped
      → ipAddressID := uuid.Nil      ← zero UUID
      → simStore.Activate(..., uuid.Nil, ...)
          UPDATE sims SET ip_address_id = $3
          → FK fk_sims_ip_address violation (SQLSTATE 23503)
          → wrapped error → handler → bare 500   ← BUG #2

Activate (any failure branch):
  → no audit entry on failure   ← BUG #3

Resume (post-Suspend with new DEV-391 release):
  POST /api/v1/sims/{id}/resume
    → Handler.Resume
      → simStore.Resume
          UPDATE sims SET state='active', suspended_at=NULL
          (NO IP allocation — SIM stays without IP)   ← question #4
```

### Data Flow (target — fixed)

```
Suspend:
  POST /api/v1/sims/{id}/suspend
    → Handler.Suspend
      → simStore.Suspend
          SELECT state, ip_address_id FROM sims FOR UPDATE
          UPDATE sims SET state='suspended', suspended_at=NOW(), ip_address_id=NULL
          IF ip_address_id != NULL AND allocation_type != 'static':
              UPDATE ip_addresses SET state='available', sim_id=NULL,
                  allocated_at=NULL, reclaim_at=NULL WHERE id=$ipID
              UPDATE ip_pools SET used_addresses = GREATEST(used_addresses-1, 0) WHERE id=$poolID
              UPDATE ip_pools SET state='active' WHERE id=$poolID AND state='exhausted'
          (static allocations: skip release entirely)
          INSERT state_history
      → 200 + audit + envelope publish

Activate (empty pool):
  → if len(pools) == 0:
      audit "sim.activate.failed" reason=no_pool_for_apn
      return 422 POOL_EXHAUSTED "No IP pool configured for this APN"

Activate (every failure branch):
  → audit "sim.activate.failed" with branch-classified reason

Resume (DEV-392, see below):
  → Resume re-allocates IP via the same handler-side allocate-then-call-store flow as Activate
    (chosen option (a) — see DEV-392 rationale)
```

### API Specifications

| Endpoint | Method | Path | Success | Failure modes (post-fix) |
|----------|--------|------|---------|--------------------------|
| API-044 | POST | `/api/v1/sims/:id/activate` | `200 OK` + `{status:"ok", data:<SIMResponse>}` | `403 FORBIDDEN`, `400 INVALID_FORMAT`, `404 NOT_FOUND`, `422 VALIDATION_ERROR` (no APN), `422 POOL_EXHAUSTED` (no pool / pool full — both cases), `422 INVALID_STATE_TRANSITION`, `500 INTERNAL_ERROR` (truly unexpected DB errors only, with stack + correlation ID logged + audit row) |
| API-045 | POST | `/api/v1/sims/:id/suspend` | `200 OK` + envelope | unchanged from today + (post-AC-1) IP release is internal — no new error mode surfaces |
| API-046 | POST | `/api/v1/sims/:id/resume` | `200 OK` + envelope | post-DEV-392: same error matrix as `/activate` (since Resume now mirrors Activate's pool-allocation flow) |

Standard envelope (per `docs/architecture/ERROR_CODES.md:184`): `POOL_EXHAUSTED` is canonically HTTP **422** — DO NOT change to 409.

### Database Schema (verified against migrations 2026-04-26)

`sims` table (relevant cols) — `migrations/20260320000002_core_schema.up.sql:283`:
```sql
CREATE TABLE sims (
    id              UUID PRIMARY KEY,
    tenant_id       UUID NOT NULL,
    apn_id          UUID,
    ip_address_id   UUID,    -- nullable
    state           TEXT NOT NULL,
    activated_at    TIMESTAMPTZ,
    suspended_at    TIMESTAMPTZ,
    -- ...
);
-- FK from FIX-206 (migrations/20260420000002_sims_fk_constraints.up.sql:54-57):
ALTER TABLE sims ADD CONSTRAINT fk_sims_ip_address
    FOREIGN KEY (ip_address_id) REFERENCES ip_addresses(id) ON DELETE SET NULL;
```

`ip_addresses` table — `migrations/20260320000002_core_schema.up.sql:233-249`:
```sql
CREATE TABLE ip_addresses (
    id              UUID PRIMARY KEY,
    pool_id         UUID NOT NULL,
    sim_id          UUID,                       -- nullable
    state           TEXT NOT NULL,              -- {available, allocated, reserved, reclaiming}
    allocation_type TEXT,                       -- {dynamic, static}
    address_v4      INET,
    address_v6      INET,
    allocated_at    TIMESTAMPTZ,
    reclaim_at      TIMESTAMPTZ,
    -- + FIX-069 cols: grace_expires_at, released_at
    -- + FIX-223 col: last_seen_at (migration 20260424000003)
);
CREATE INDEX idx_ip_addresses_pool_state ON ip_addresses (pool_id, state);
CREATE INDEX idx_ip_addresses_sim        ON ip_addresses (sim_id) WHERE sim_id IS NOT NULL;
-- non-unique → root cause of leak: SIM CAN have multiple state='allocated' rows
```

State machine — `internal/store/sim.go:88-97`:
```go
var allowedTransitions = map[string][]string{
    "stock":       {"active", "terminated"},
    "active":      {"suspended", "stolen_lost", "terminated"},
    "suspended":   {"active", "terminated"},
    "stolen_lost": {"active", "terminated"},
    "terminated":  {},
}
```

---

## Affected Files

| File | Change | Reason |
|------|--------|--------|
| `internal/store/sim.go` | Modify `SIMStore.Suspend` (lines 467-510) | DEV-391: atomic IP release inside suspend tx; static-IP preservation |
| `internal/store/sim.go` | Modify `SIMStore.Activate` (lines 420-465) | DEV-393 (defensive): accept `*uuid.UUID` instead of `uuid.UUID`; null-safe SQL |
| `internal/api/sim/handler.go` | Modify `Handler.Activate` (lines 974-1084) | AC-2 + AC-3: empty-pool guard + audit-on-failure on every branch |
| `internal/api/sim/handler.go` | Modify `Handler.Resume` (lines 1139-1187) | DEV-392 / AC-5: Resume gets same allocate-then-store flow as Activate, plus audit-on-failure |
| `internal/store/sim_test.go` | Add tests | `TestSIMStore_Suspend_ReleasesIP`, `TestSIMStore_Suspend_PreservesStaticIP`, `TestSIMStore_Activate_NilIP_NoFKViolation` |
| `internal/api/sim/handler_test.go` | Add tests | `TestActivate_PoolEmpty_Returns422`, `TestActivate_PoolFull_Returns422`, `TestActivate_AuditOnFailure`, `TestActivate_RoundTripAfterSuspend`, `TestResume_AllocatesIPAfterSuspend` |
| `docs/USERTEST.md` | Append `## FIX-253` section | Turkish UAT scenarios mapping AC-1..AC-5 |
| `docs/brainstorming/decisions.md` | Append DEV-390..DEV-393 | New decision entries |

---

## Story-Specific Compliance Rules

- **API:** Standard envelope per `docs/architecture/ERROR_CODES.md`. `POOL_EXHAUSTED` stays HTTP 422 (DEV-390, restating old DEV-386). All error responses MUST go through `apierr.WriteError` — never raw `http.Error` or `w.WriteHeader(500)`.
- **DB:** Suspend-side IP release MUST run inside the same transaction as the state UPDATE (atomicity). Tenant-scoping preserved on every store query (`WHERE tenant_id = $N`).
- **Static IP preservation:** Suspend release MUST skip `allocation_type='static'` rows (mirrors `IPPoolStore.ReleaseIP:750-754` static branch which puts static into `reclaiming` not `available`; for Suspend we do **NOT** schedule reclaim either — static IPs survive suspend by design and re-attach on resume).
- **Audit:** Per CLAUDE.md "every state-changing operation creates an audit log entry" — extend to BOTH success and failure paths (AC-3). Use existing `Handler.createAuditEntry` helper. Action verbs: `sim.activate`, `sim.activate.failed` (NEW), `sim.resume`, `sim.resume.failed` (NEW), `sim.suspend` (already exists).
- **State machine:** `validateTransition("suspended", "active")` already passes — do NOT introduce a new state, just fix the IP allocation around it. Resume's own state-machine guard (`if currentState != "suspended"` at sim.go:531) is preserved.
- **Logging:** All failure branches must `h.logger.Error().Err(err).Str("sim_id", idStr).Str("tenant_id", tenantID.String()).Str("correlation_id", correlationID).Msg(...)`. Correlation ID is in `r.Context()` via existing middleware.

---

## Bug Pattern Warnings

- **PAT-006 (struct/SQL drift) RECURRENCE #3 just filed under FIX-251** — same root class as the bugs FIX-253 prevents. Direct relevance: changing `SIMStore.Activate` signature from `uuid.UUID` to `*uuid.UUID` (Task 3) MUST update **every** call site repo-wide. A grep-then-fix pattern (`grep -rn "simStore.Activate(\|SIMStore.Activate(" --include='*.go'`) is mandatory before commit. PAT-006 was originally a struct↔SQL column-count drift; this is a func-signature↔caller drift but the class is identical.
- **PAT-023 (schema_migrations lying)** — context-only. Not directly applicable to FIX-253 changes (no migrations needed) but explains why FIX-252 is now FIX-253 instead of original-FIX-252-with-Tasks-2-5-shipped.
- **No NEW pattern candidates** in this story — the bugs are well-understood store-layer atomicity + handler-layer guard issues.

---

## Tech Debt (from ROUTEMAP)

`docs/ROUTEMAP.md` Tech Debt table scanned — **no items target FIX-253**. Story is brand new (filed 2026-04-26 as FIX-252 spinoff). Scan command: `grep -nE "FIX-253" docs/ROUTEMAP.md` — only the story-row reference + change-log entries.

No tech debt to fold.

---

## Mock Retirement

Backend-only story; no FE mocks affected. **No mock retirement.**

(FE may need a tiny copy/UX tweak for AC-5 if DEV-392 chooses option (b) Resume-as-Activate-alias — but the chosen option (a) keeps Resume distinct, so no FE work.)

---

## Decisions to Log (decisions.md)

Next free DEV id is **DEV-390** (highest existing: DEV-389 — FIX-251 plan-pivot, verified 2026-04-26).

- **DEV-390** — `POOL_EXHAUSTED` stays HTTP **422**, NOT 409. Rationale: ERROR_CODES.md authoritatively documents 422 (line 184); existing handler + tests + FE error-mapping wire 422; switching to 409 is a breaking API change with no callable benefit. Restated from old DEV-386 (FIX-252) since that story shipped no code. AC-2 satisfied by any structured 4xx — 422 qualifies.
- **DEV-391** — `SIMStore.Suspend` releases the SIM's currently-allocated IP atomically inside the suspend tx (sets `ip_addresses.state='available'`, `sim_id=NULL`, decrements `ip_pools.used_addresses`, NULLs `sims.ip_address_id`). Static allocations preserved (skip release for `allocation_type='static'`). Rationale: pool counter drift causes spurious `POOL_EXHAUSTED` 422s on healthy tenants. Mirrors `IPPoolStore.ReleaseIP:750-768` dynamic branch and `Terminate:599-608` reclaim block (but immediate not grace, since suspend is reversible). Restated from old DEV-387.
- **DEV-392** — `Handler.Resume` re-allocates IP via the same handler-side allocate-then-call-store flow as Activate (option (a) from AC-5). Rejected option (b) Resume-as-Activate-alias: distinct verb has audit/UX value (the `sim.resume` action verb is consumed by the alerts dashboard for "SIM came back online" UX; collapsing it loses that signal). Rejected option (c) Resume-without-IP: leaves SIMs in a broken state where they're `active` but can't authenticate (no Framed-IP attach in RADIUS Access-Accept). Resolves old DEV-388. **FE compat:** `web/src/pages/sims/detail.tsx:101` already calls `/resume` — no FE change needed; backend just needs to make Resume actually allocate.
- **DEV-393** — `Handler.Activate` and `Handler.Resume` write `sim.activate.failed` / `sim.resume.failed` audit entries on EVERY failure branch (`get_sim_failed`, `list_pools_failed`, `pool_empty`, `allocate_failed`, `state_transition_failed`). Rationale: CLAUDE.md "every state-changing operation creates an audit log entry" — failure attempts ARE state-meaningful (correlation, classification, security forensics). Rejected lower-bar "audit on success only" — leaves blind spots in operator-driven activation incident reconstruction.

---

## Tasks

> Each task is dispatched to a fresh Developer subagent. The orchestrator extracts `Context refs` sections and passes them in the dispatch prompt. The Developer does NOT read this plan file directly.

---

### Task 1: Store — `SIMStore.Suspend` releases IP atomically (DEV-391)

- **Files:** Modify `internal/store/sim.go` (`SIMStore.Suspend`, lines 467-510).
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/sim.go:562-625` (`SIMStore.Terminate`) for the SELECT-state-and-ip pattern and IP-reclaim block. Read `internal/store/ippool.go:727-777` (`IPPoolStore.ReleaseIP`) for the canonical static-vs-dynamic branch + decrement-counter + un-exhaust pattern.
- **Context refs:** "Architecture Context > Components Involved", "Database Schema", "Decisions to Log (DEV-391)", "Story-Specific Compliance Rules"
- **What:**
  1. Change the initial `SELECT state FROM sims FOR UPDATE` (line 475-478) to also select `ip_address_id` — mirror `Terminate:569-574`.
  2. After the SIM `UPDATE sims SET state='suspended', suspended_at=NOW() ...` (line 490-495), and BEFORE `insertStateHistory`:
     - If selected `ipAddressID != nil`:
       - `SELECT pool_id, allocation_type FROM ip_addresses WHERE id = $1 AND state IN ('allocated','reserved') FOR UPDATE` — capture `poolID` and `allocType`.
       - If `allocType == "static"`: skip the release block entirely (static IPs survive suspend by design).
       - Else (dynamic):
         - `UPDATE ip_addresses SET state='available', sim_id=NULL, allocated_at=NULL, reclaim_at=NULL WHERE id = $1`
         - `UPDATE ip_pools SET used_addresses = GREATEST(used_addresses - 1, 0) WHERE id = $1`
         - `UPDATE ip_pools SET state='active' WHERE id = $1 AND state='exhausted'`
         - `UPDATE sims SET ip_address_id = NULL WHERE id = $1 AND tenant_id = $2`
  3. All operations inside the existing `tx` — no separate transaction. If any IP release SQL errors, the entire suspend tx rolls back (atomicity).
  4. If the `SELECT pool_id ... ip_addresses` returns `pgx.ErrNoRows` (e.g. `sims.ip_address_id` points at a deleted/orphaned row — possible in legacy data): treat as soft success — log a Warn but do NOT fail the suspend. Still NULL `sims.ip_address_id` defensively.
  5. Add a comment block above the IP-release section: `// DEV-391 (FIX-253): atomic IP release on suspend; static IPs preserved.`
  6. Update the function doc comment to note "Releases the SIM's allocated dynamic IP atomically; static IPs preserved."
- **Verify:**
  - `go build ./...`
  - `go vet ./internal/store/...`
  - `go test ./internal/store/... -count=1 -run "Suspend|Terminate"` (existing Terminate tests must still pass — they share locking semantics)
  - `make db-seed` runs clean (per CLAUDE.md "Seed discipline" — never defer seed failures).
  - Manual SQL post-suspend: `SELECT s.ip_address_id, i.state, i.sim_id FROM sims s LEFT JOIN ip_addresses i ON i.sim_id = s.id WHERE s.id = '<test-sim>';` returns NULL for `ip_address_id` and zero rows for the join (dynamic) or unchanged for static.

---

### Task 2: Store — `SIMStore.Activate` signature accepts nullable IP + null-safe SQL (DEV-393 defensive)

- **Files:** Modify `internal/store/sim.go` (`SIMStore.Activate`, lines 420-465).
- **Depends on:** — *(parallelizable with Task 1)*
- **Complexity:** low
- **Pattern ref:** Read `internal/store/sim.go:850-880` (`SIMStore.SetIPAndPolicy`) for the conditional-SQL idiom (`if ipAddressID != nil` builds different UPDATE strings).
- **Context refs:** "Architecture Context > Components Involved", "Database Schema", "Story-Specific Compliance Rules", "Bug Pattern Warnings (PAT-006 RECURRENCE)"
- **What:**
  1. Change signature from `Activate(ctx, tenantID, simID uuid.UUID, ipAddressID uuid.UUID, userID *uuid.UUID)` to `Activate(ctx, tenantID, simID uuid.UUID, ipAddressID *uuid.UUID, userID *uuid.UUID)`.
  2. Build the UPDATE SQL conditionally:
     - When `ipAddressID == nil`: emit `UPDATE sims SET state='active', activated_at=NOW(), updated_at=NOW() WHERE id=$1 AND tenant_id=$2 RETURNING ...` (no `ip_address_id = ...`).
     - When `ipAddressID != nil`: keep current behaviour `UPDATE sims SET state='active', ip_address_id=$3, activated_at=NOW(), updated_at=NOW() WHERE id=$1 AND tenant_id=$2 RETURNING ...` with `*ipAddressID` as the value.
  3. **Update ALL call sites repo-wide** (PAT-006 anti-recurrence): grep `simStore.Activate(\|\.Activate(` across the repo, filter to SIM Activate (not other Activate methods like `IPPoolStore.Activate` if any), update each to pass `*uuid.UUID`. Common call sites: `internal/api/sim/handler.go` (Task 3 will pass `&allocatedIP.ID`), tests under `internal/api/sim/`, `internal/store/sim_test.go`, plus any fixture/seed code.
  4. Preserve `validateTransition` check, state-history insert, tx commit ordering — no semantic changes besides the nullable IP.
- **Verify:**
  - `go build ./...` (will catch every missed call site)
  - `go vet ./internal/store/...`
  - `go test ./internal/store/... -count=1 -run "Activate"`
  - `grep -rn "simStore.Activate(\|\.Activate(" --include='*.go' | grep -v "IPPoolStore.Activate"` — every match passes `*uuid.UUID` (not bare `uuid.UUID`).
  - `make db-seed` runs clean.

---

### Task 3: Handler — `Handler.Activate` empty-pool guard + audit-on-failure (AC-2, AC-3)

- **Files:** Modify `internal/api/sim/handler.go` (`Handler.Activate`, lines 974-1084).
- **Depends on:** Task 2 (call site signature update)
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/sim/handler.go:1086-1137` (`Handler.Suspend`) for the structured-error + audit pattern; read `Handler.Activate` itself (lines 974-1084) for the existing `ErrPoolExhausted → 422` mapping (around line 1015) — do NOT change that mapping.
- **Context refs:** "Architecture Context > Components Involved", "Architecture Context > Data Flow (target)", "API Specifications", "Story-Specific Compliance Rules", "Decisions to Log (DEV-393)"
- **What:**
  1. After `pools, _, err := h.ippoolStore.List(...)` and before the existing `if len(pools) > 0 { ... }` block: insert an explicit guard:
     ```go
     if len(pools) == 0 {
         h.createAuditEntry(r, "sim.activate.failed", simID.String(), existing,
             map[string]interface{}{"reason": "no_pool_for_apn", "apn_id": existing.APNID}, userID)
         apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodePoolExhausted,
             "No IP pool configured for this APN")
         return
     }
     ```
  2. Add audit-on-failure entries to EVERY existing error branch (Discovery against current line numbers — verify before coding):
     - `get_sim_failed` (after `simStore.GetByID` non-NotFound error)
     - `not_found` (after `ErrSIMNotFound`) — log audit reason `sim_missing`
     - `validation_no_apn` (when `existing.APNID == nil`)
     - `list_pools_failed` (after `ippoolStore.List` error)
     - `allocate_failed` (after `AllocateIP` non-`ErrPoolExhausted` error)
     - `pool_exhausted` (already-existing `ErrPoolExhausted` branch — ADD audit, keep 422)
     - `state_transition_failed` (after `simStore.Activate` non-`ErrSIMNotFound`/non-`ErrInvalidStateTransition` error)
     - `invalid_state_transition` (the `ErrInvalidStateTransition` branch)
   - Each audit row uses action `sim.activate.failed`, target `simID.String()`, before=`existing`, after=`map[string]interface{}{"reason":"<branch_name>"}`, userID=`userIDFromCtx(r)`.
  3. **Do not change** the existing `ErrPoolExhausted → 422` HTTP mapping (DEV-390 — already correct).
  4. **Do not change** the success path (audit + envelope publish + policy auto-match). They already work.
  5. Update the call site for `simStore.Activate` to pass `&allocatedIP.ID` (Task 2 signature change). For the unreachable-but-defensive empty-pool case where guard above already returns, no nil-passing needed — but the path where `AllocateIP` succeeded always has a non-nil result.
  6. Tenant-scoping: every error response MUST log `tenant_id` and `sim_id`. Use existing `h.logger.Error().Err(err).Str("sim_id", idStr).Str("tenant_id", tenantID.String()).Msg(...)`.
- **Verify:**
  - `go build ./...`
  - `go vet ./internal/api/sim/...`
  - `go test ./internal/api/sim/... -count=1 -run "Activate"`
  - Manual: with stack up via `make up`, invoke activate against a SIM whose APN has zero pools (synthetic insert) → 422 `POOL_EXHAUSTED` envelope with new message; check `audit_log` table has matching `sim.activate.failed` row.

---

### Task 4: Handler — `Handler.Resume` mirrors Activate's allocate flow + audit-on-failure (DEV-392, AC-5)

- **Files:** Modify `internal/api/sim/handler.go` (`Handler.Resume`, lines 1139-1187).
- **Depends on:** Task 2 (signature) + Task 3 (audit pattern reference); can run in parallel with Task 3 logically but sequencing avoids merge conflicts on the same file.
- **Complexity:** medium
- **Pattern ref:** Read `Handler.Activate` AFTER Task 3 completes (lines 974-1084) for the canonical allocate-then-call-store + audit-on-failure flow. The Resume rewrite mirrors Activate's pool-allocation flow; the only difference is `simStore.Resume` is called instead of `simStore.Activate` (Resume enforces `currentState=='suspended'`, Activate accepts any allowed-transition source).
- **Context refs:** "Architecture Context > Components Involved", "Architecture Context > Data Flow (target)", "API Specifications", "Decisions to Log (DEV-392)"
- **What:**
  1. **Decision rationale embedded:** option (a) chosen — Resume re-allocates IP via handler-side allocate-then-call-store flow. NOT (b) deprecate-as-Activate-alias (loses audit verb) and NOT (c) no-IP-on-resume (leaves SIM unable to auth). See DEV-392.
  2. After existing `simStore.GetByID` (line 1153-1162) and before `simStore.Resume` (line 1166): insert the same pool-list + empty-pool-guard + AllocateIP block as Activate (Task 3). Specifically:
     - Skip allocation if `existing.APNID == nil` — return 422 `VALIDATION_ERROR` "SIM has no APN — cannot allocate IP" + audit `sim.resume.failed` reason=`no_apn`.
     - `pools, _, err := h.ippoolStore.List(r.Context(), tenantID, "", 1, existing.APNID)`
     - If `len(pools) == 0`: 422 `POOL_EXHAUSTED` "No IP pool configured for this APN" + audit reason=`no_pool_for_apn`.
     - `allocated, err := h.ippoolStore.AllocateIP(r.Context(), pools[0].ID, simID)`
     - On `ErrPoolExhausted`: 422 `POOL_EXHAUSTED` + audit reason=`pool_exhausted`.
     - On other err: 500 + audit reason=`allocate_failed`.
  3. Refactor the call to `simStore.Resume` to also persist the new `ip_address_id` — easiest path: AFTER `simStore.Resume` succeeds, call `h.simStore.SetIPAndPolicy(ctx, simID, &allocated.ID, nil)` to write the IP onto the now-active SIM (existing helper at sim.go:850; supports nullable IP — confirm with grep). This avoids changing `SIMStore.Resume`'s signature (smaller blast radius) and reuses the same persistence path the AAA dynamic-alloc paths use today.
  4. On EVERY failure branch (get_sim, validation_no_apn, list_pools, allocate, resume_state_transition, set_ip_and_policy): write `sim.resume.failed` audit with classified reason.
  5. If `simStore.Resume` returns `ErrInvalidStateTransition`: ROLL BACK the IP allocation (call `IPPoolStore.ReleaseIP(pools[0].ID, simID)`) before returning 422 — otherwise the allocation leaks. Wrap in a defer-style cleanup block.
  6. **Do not change** the `validateTransition` semantics of `SIMStore.Resume` (lines 531-537) — only suspend → active is allowed; this is correct.
  7. Add comment: `// DEV-392 (FIX-253): Resume re-allocates IP since DEV-391 made Suspend release it.`
- **Verify:**
  - `go build ./...`
  - `go vet ./internal/api/sim/...`
  - `go test ./internal/api/sim/... -count=1 -run "Resume|Activate"`
  - Manual: suspend a SIM (verify post-suspend `ip_address_id IS NULL`), then resume → 200 + new IP allocated + `audit_log` shows both `sim.suspend` and `sim.resume`.

---

### Task 5: Regression tests (suspend release + activate guards + resume re-alloc + audit)

- **Files:** Add tests to `internal/store/sim_test.go` AND `internal/api/sim/handler_test.go`.
- **Depends on:** Tasks 1, 2, 3, 4 merged
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/sim/handler_test.go` for handler-test setup (HTTP recorder, mock store, JWT context). Read `internal/store/sim_test.go` for store-test setup (postgres test container or `tx.Rollback`-isolated tests). Read `internal/store/ippool_allocation_cycle_test.go` for the closest-existing IP allocation+release round-trip pattern.
- **Context refs:** "Acceptance Criteria Mapping", "Architecture Context > Data Flow (target)", "API Specifications"
- **What:**
  1. **Store test — `TestSIMStore_Suspend_ReleasesIP`** (AC-1 supporting): Seed SIM in `active` state with a dynamic-allocated IP from a pool with `used_addresses=1`; call `Suspend`; assert (a) `ip_addresses.state='available'`, `sim_id IS NULL`, `allocated_at IS NULL`; (b) `ip_pools.used_addresses=0`; (c) `sims.ip_address_id IS NULL`; (d) state_history row exists for suspend.
  2. **Store test — `TestSIMStore_Suspend_PreservesStaticIP`** (AC-1): Seed SIM with `allocation_type='static'`; call `Suspend`; assert IP row UNCHANGED (state still `reserved` or `allocated`, `sim_id` still points at this SIM, `used_addresses` unchanged); `sims.ip_address_id` MAY remain set or be NULLed — pick one (recommended: NULL `sims.ip_address_id` for consistency with dynamic case, but ip_addresses row stays bound to the SIM via `sim_id` for re-attach on resume).
  3. **Store test — `TestSIMStore_Suspend_NoIP_NoOp`**: Seed SIM with `ip_address_id IS NULL`; call `Suspend`; assert success + no errors + no orphan IP rows touched.
  4. **Store test — `TestSIMStore_Activate_NilIP_NoFKViolation`** (DEV-393 defensive): Call `Activate` with `ipAddressID=nil`; assert SIM state transitions to `active` and `sims.ip_address_id` is NULL (no FK violation, no error).
  5. **Handler test — `TestActivate_AfterSuspend_RoundTrip`** (AC-1 happy path): Seed SIM `active` with allocated IP X; POST `/suspend`; assert 200 + IP X released + audit row; POST `/activate`; assert 200 + new IP Y allocated (Y may equal X if pool small, but `state='allocated'` and `sim_id=$simID` true); assert audit rows for both ops.
  6. **Handler test — `TestActivate_PoolEmpty_Returns422_PoolExhausted`** (AC-2): Seed SIM with APN that has zero pools; POST `/activate`; assert HTTP 422 + envelope `code: "POOL_EXHAUSTED"` + message contains "No IP pool configured" + `sim.activate.failed` audit row with reason `no_pool_for_apn`.
  7. **Handler test — `TestActivate_PoolFull_Returns422_PoolExhausted`** (AC-2): Seed SIM with APN whose only pool has all addresses `state='allocated'`; POST `/activate`; assert HTTP 422 + envelope `code: "POOL_EXHAUSTED"` + audit row.
  8. **Handler test — `TestActivate_AuditOnFailure`** (AC-3): For each failure branch reachable via mock store (e.g. `simStore.GetByID` returns generic error → 500), assert an audit row was written with action `sim.activate.failed` and a `reason` field matching the branch name. Loop over a table-driven set: `[]struct{name, mockSetup, expectedReason}`.
  9. **Handler test — `TestResume_AfterSuspend_AllocatesIP`** (AC-5 / DEV-392): Seed SIM `active` with IP; POST `/suspend` (verify IP released); POST `/resume`; assert 200 + new IP allocated + `sim.resume` audit row.
  10. **Handler test — `TestResume_PoolEmpty_Returns422`** (AC-5): SIM in `suspended` whose APN pools are all gone; POST `/resume` → 422 `POOL_EXHAUSTED` + `sim.resume.failed` audit.
  11. **Handler test — `TestResume_AllocateRolledBackOnStateError`** (AC-5 edge): mock `simStore.Resume` to return `ErrInvalidStateTransition` AFTER `AllocateIP` succeeded; assert allocated IP is rolled back via `ReleaseIP` call (no leak).
- **Verify:**
  - `go test ./internal/api/sim/... -count=1 -run "Activate|Suspend|Resume"`
  - `go test ./internal/store/... -count=1 -run "Suspend|Activate|Resume"`
  - All new tests PASS; no regression in existing tests.
  - `make db-seed` runs clean (final guard).

---

### Task 6: USERTEST + decisions.md docs sync

- **Files:** Modify `docs/USERTEST.md`, modify `docs/brainstorming/decisions.md`.
- **Depends on:** Tasks 1-5 merged
- **Complexity:** low
- **Pattern ref:** Read the FIX-234 USERTEST section in `docs/USERTEST.md` (around the most recent "FIX-234" header) for the Turkish-language scenario template.
- **Context refs:** "Acceptance Criteria Mapping", "Decisions to Log"
- **What:**
  1. Append a `## FIX-253` section to `docs/USERTEST.md` with 5 Turkish scenarios mapping AC-1..AC-5:
     - **Senaryo 1 (AC-1):** Aktif SIM'i suspend et, ip_address_id'nin NULL'a düştüğünü ve eski IP'nin pool'a `available` olarak döndüğünü doğrula.
     - **Senaryo 2 (AC-1 static):** Static IP'li SIM'i suspend et, IP satırının değişmediğini doğrula.
     - **Senaryo 3 (AC-2):** APN'i pool'suz olan SIM'de `/activate` çağır → 422 `POOL_EXHAUSTED` envelope + audit row.
     - **Senaryo 4 (AC-3):** Activate hata branch'larında `sim.activate.failed` audit row'larının `reason` alanı ile yazıldığını doğrula.
     - **Senaryo 5 (AC-5 / DEV-392):** Suspend → Resume round-trip; resume sonrası SIM'in YENİ bir IP ile `active` olduğunu ve `audit_log`'da `sim.resume` row'unun göründüğünü doğrula.
  2. Append DEV-390, DEV-391, DEV-392, DEV-393 entries to `docs/brainstorming/decisions.md` with the rationale text from this plan's "Decisions to Log" section.
- **Verify:**
  - `grep -n "FIX-253" docs/USERTEST.md` returns the new section header.
  - `grep -nE "DEV-39[0-3]" docs/brainstorming/decisions.md` returns four new lines.
  - `make db-seed` runs clean (final guard).

---

## Acceptance Criteria Mapping

| AC | Description | Implemented in | Verified by |
|----|-------------|----------------|-------------|
| AC-1 | `SIMStore.Suspend` atomically releases dynamic IP; static preserved; pool counter decremented | Task 1 | Task 5 (TestSIMStore_Suspend_ReleasesIP, TestSIMStore_Suspend_PreservesStaticIP, TestSIMStore_Suspend_NoIP_NoOp) + manual SQL |
| AC-2 | Activate handler explicit guard for `len(pools)==0` → 422 `POOL_EXHAUSTED` (never bare 500) + audit | Task 3 | Task 5 (TestActivate_PoolEmpty_Returns422, TestActivate_PoolFull_Returns422) |
| AC-3 | Activate handler writes `sim.activate.failed` audit on EVERY failure branch with `reason` classification | Task 3 | Task 5 (TestActivate_AuditOnFailure table-driven) |
| AC-4 | Unit tests covering all of the above (named functions in spec) | Task 5 | `go test` PASS |
| AC-5 | Resume verb resolution — option (a): Resume re-allocates IP via handler-side allocate flow (DEV-392) | Task 4 | Task 5 (TestResume_AfterSuspend_AllocatesIP, TestResume_PoolEmpty_Returns422, TestResume_AllocateRolledBackOnStateError) + manual round-trip |

---

## Risks & Mitigations

- **R1 — AAA reads `sim.IPAddressID` on suspended SIMs.** AUDITED in Pre-Dev section above — no regression. AAA only reads on active sessions, and suspended SIMs can't have active sessions (operator policy rejects auth). Idempotent release in AAA tolerates absence (`GetIPAddressByID → ErrIPNotFound` is logged Warn, not error).
- **R2 — Static IP semantics.** Mitigation: Task 1 explicitly skips `allocation_type='static'`; Task 5 has `TestSIMStore_Suspend_PreservesStaticIP`. Mirrors `IPPoolStore.ReleaseIP:750-754` static branch (which puts static into reclaiming, not available — but for Suspend we leave it bound entirely since suspend is reversible by design).
- **R3 — `SIMStore.Activate` signature change breaks unknown call sites.** Mitigation: Task 2 mandates repo-wide grep + `go build ./...` validation (will catch every missed site). PAT-006 RECURRENCE warning is in-plan to keep Developer attention high.
- **R4 — Resume IP allocation rollback on `ErrInvalidStateTransition`.** Mitigation: Task 4 step 5 explicitly adds the `ReleaseIP` rollback; Task 5 step 11 (`TestResume_AllocateRolledBackOnStateError`) tests the rollback path.
- **R5 — `make db-seed` regression.** Mitigation: every task's Verify block re-runs `make db-seed`. Per CLAUDE.md "Seed discipline" rule, MUST stay clean — never defer.
- **R6 — Audit table volume.** Audit-on-every-failure increases `audit_log` write volume. Mitigation: failure branches are rare (well-formed clients hit them <0.1%); even at 10M SIMs and 100req/s activate, additional audit rows are bounded by `failure_rate * activate_qps` ≈ negligible. ERROR_CODES + audit retention policy (90-day default) absorbs the volume.

---

## Wave Plan (for Amil dispatch)

```
Wave 1 (parallel — 2 tasks):    Task 1 (Suspend release) | Task 2 (Activate signature)
Wave 2 (parallel — 1 task):     Task 3 (Activate handler guard + audit)         [depends on Task 2]
Wave 3 (sequential):            Task 4 (Resume allocate + audit)                [depends on Task 2 + Task 3 pattern]
Wave 4 (sequential):            Task 5 (regression tests)                       [depends on Tasks 1-4]
Wave 5 (sequential):            Task 6 (USERTEST + decisions.md docs)
```

Total: **6 tasks, 5 waves**.

Complexity breakdown: low=2 (T2, T6), medium=4 (T1, T3, T4, T5), high=0. Effort=S target is mostly low + max 1-2 medium; 4 medium is a defensible deviation because:
- Task 1 (Suspend release) inherently couples store-state-machine + IP-pool semantics — medium baseline
- Task 3 (Activate handler) adds 8 audit branches + new guard — medium scope
- Task 4 (Resume rewrite) adds full alloc-flow mirror + rollback — medium scope
- Task 5 (tests) covers 11 test scenarios across 2 files — medium scope

No `high` task — story scope does not include cross-service orchestration or new migrations.

---

## Quality Gate Self-Validation

Run all checks; PASS only when every box is ✓.

**a. Minimum substance (S → 30 lines, 2 tasks):** ✓ Plan ~480 lines, 6 tasks.

**b. Required sections present:**
- ✓ `## Goal`
- ✓ `## Architecture Context`
- ✓ `## Tasks` (with numbered `### Task` blocks)
- ✓ `## Acceptance Criteria Mapping`

**c. Embedded specs:**
- ✓ API: endpoint table + envelope shape + status code list (per endpoint)
- ✓ DB: `sims` + `ip_addresses` schemas embedded with source migration paths + state-machine table embedded
- N/A UI: backend-only story (no Design Token Map needed; FE compat noted as "no FE change" per DEV-392)

**d. Task complexity cross-check (S story):** ✓ low=2, medium=4, high=0. Deviation justified inline (each medium genuinely needs its own dispatch context — Suspend semantics, handler audit fan-out, Resume rewrite, multi-scenario test). Zero high — appropriate for backend hardening scope.

**e. Context refs validation:** Each task's `Context refs` lists actual section headers present in this plan:
- ✓ "Architecture Context > Components Involved" / "Architecture Context > Data Flow (target)"
- ✓ "API Specifications" / "Database Schema" / "Story-Specific Compliance Rules"
- ✓ "Decisions to Log (DEV-390/391/392/393)"
- ✓ "Acceptance Criteria Mapping" / "Bug Pattern Warnings (PAT-006 RECURRENCE)"
- ✓ "Pre-Dev Impact Audit"

**Architecture compliance:**
- ✓ Each task in correct layer (handler / store / docs)
- ✓ No cross-layer imports planned
- ✓ Dependency direction: handler → store → DB (preserved)
- ✓ Component naming matches `Handler`, `SIMStore`, `IPPoolStore` conventions

**API compliance:**
- ✓ Envelope shape stated
- ✓ HTTP method/path correct (POST /api/v1/sims/:id/{activate,suspend,resume})
- ✓ Validation already exists; new guard inserted before allocation
- ✓ All error responses specified per branch in handler

**DB compliance:**
- ✓ No new migration needed (FK already exists from FIX-206; no schema change)
- ✓ Embedded schema sourced from ACTUAL migrations (`20260320000002_core_schema.up.sql`, `20260420000002_sims_fk_constraints.up.sql`) — verified
- ✓ Column names verified (`ip_address_id`, `state`, `sim_id`, `pool_id`, `used_addresses`, `allocation_type`)
- ✓ Indexes noted (`idx_ip_addresses_sim` is non-unique — explicit fact-check, root cause of leak)
- ✓ Tenant-scoping enforced in every store query call-out
- ✓ `make db-seed` discipline embedded in every task's Verify block

**UI compliance:** N/A (backend-only; no FE change per DEV-392 — confirmed `web/src/pages/sims/detail.tsx:101` already calls `/resume`).

**Task decomposition:**
- ✓ Each task touches ≤2 production files (Task 5 touches 2 test files; Task 6 touches 2 doc files)
- ✓ No task requires 5+ files or 10+ minutes
- ✓ Pre-Dev impact audit done in plan (no separate Discovery wave needed)
- ✓ Each task has `Depends on`
- ✓ Each task has `Context refs` pointing at real sections
- ✓ Each task creating new code references a Pattern file (Terminate for IP-release, SetIPAndPolicy for nullable-SQL, ReleaseIP for canonical static-vs-dynamic, Activate-after-Task-3 for Resume mirror)
- ✓ Tasks functionally grouped (store-suspend / store-signature / handler-activate / handler-resume / tests / docs)
- ✓ Reasonable count (6 for Effort=S — same shape as inherited FIX-252 plan minus the Discovery task since impact audit absorbed it)
- ✓ NO implementation code in tasks — specs + pattern refs + tiny illustrative snippets only

**Test compliance:**
- ✓ Test task (Task 5) covers each AC with 11 named test functions
- ✓ Test file paths specified
- ✓ Round-trip + pool-empty + pool-full + audit + suspend-release + resume-realloc + rollback scenarios all listed

**Self-containment:**
- ✓ API specs embedded
- ✓ DB schema embedded with migration source paths
- ✓ State-machine table embedded
- ✓ Business rules stated inline (POOL_EXHAUSTED 422 decision, static-IP preservation rule, tenant-scoping, audit-on-every-failure)
- ✓ Pre-Dev impact audit findings embedded
- ✓ Inheritance from FIX-252 explicitly mapped (DEV id renumber DEV-386→390, DEV-387→391, DEV-388→392; new DEV-393)
- ✓ Every Context refs target exists as a header in this plan

**Quality Gate Result: PASS**

---

## Open Uncertainties for Ana Amil

- **Static-IP `sims.ip_address_id` on suspend:** Task 5 step 2 picks the recommendation "NULL `sims.ip_address_id` for static too, but leave `ip_addresses.sim_id` bound" — a slight asymmetry. Ana may prefer "leave both untouched for static" for simpler UX semantics. Either is consistent; the recommended choice gives `sims.ip_address_id` a single semantic ("currently-attached IP for active sessions") and `ip_addresses.sim_id` a different one ("home-allocated owner"). Flag for review at Quality Gate.
- **Audit `before`/`after` shape for failure rows:** plan has Task 3 use `map[string]interface{}{"reason":...}` for `after`. If `audit_log` schema requires a typed JSON shape, Developer may need to wrap differently — confirm `audit.entry.go` accepts `interface{}` (it does today per existing call sites; flagged for safety).

— END PLAN —

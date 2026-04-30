# Fix Plan: FIX-252 — `POST /api/v1/sims/{id}/activate` returns 500 (IP-pool path)

> **Mode:** FIX (backend bug, pre-release) — Tier P2 / Effort S
> **Track:** UI Review Remediation (Wave 2.5+3)
> **Surfaced by:** FIX-249 Gate UI Scout (F-U2) — stuck SIM `fffa41ad-abfe-4cd6-9ec6-6bd81409e2c1`

---

## Goal

Eliminate the bare HTTP 500 returned by `POST /api/v1/sims/:id/activate` for the suspend → activate round-trip, replacing every reachable failure mode with a structured envelope error, fixing the underlying suspend-side IP leak, and adding regression coverage so the bug cannot return.

---

## Bug Description

A SIM left in `suspended` state by the FIX-249 UI Scout cannot be reactivated through the public API:

```
POST /api/v1/sims/fffa41ad-abfe-4cd6-9ec6-6bd81409e2c1/activate
→ HTTP 500 INTERNAL_ERROR (bare envelope, no actionable code)
```

The suspend half of the round-trip works; the activate half does not. AC catalogue (from story spec):

- **AC-1** Round-trip suspend → activate succeeds for a clean SIM.
- **AC-2** Pool-cannot-allocate path returns structured 4xx (NOT bare 500).
- **AC-3** Failure path logs stack + correlation ID and writes audit entry on BOTH success and failure.
- **AC-4** Unit test: activate-after-suspend round-trip.
- **AC-5** SIM `fffa41ad-…` reactivatable (or restorable via `make db-seed`).

---

## Root Cause Hypotheses (to validate in Discovery task)

The activate handler at `internal/api/sim/handler.go:974-1084` already structures `ErrPoolExhausted` correctly (line 1015 → 422 `POOL_EXHAUSTED`). So the 500 must come from one of:

| # | Hypothesis | Where it falls into the bare-500 branch |
|---|------------|-----------------------------------------|
| **H1** | **APN has zero IP pools.** `pools` is empty → `len(pools) > 0` skipped → `ipAddressID = uuid.Nil` → `simStore.Activate` runs `UPDATE sims SET ip_address_id = $3` with all-zeros UUID → FK `fk_sims_ip_address` (migration `20260420000002_sims_fk_constraints.up.sql:54-57`) raises **SQLSTATE 23503** → wrapped as `"store: update sim activate: …"` → handler line 1049 → 500. | **Most likely** per advisor analysis. |
| H2 | Suspend-side IP leak. `ip_addresses.sim_id` index `idx_ip_addresses_sim` is non-unique (core_schema.up.sql:246) so re-allocation does NOT collide on uniqueness — but the previous `ip_addresses` row stays `state='allocated'` with `sim_id` pointing at this SIM. Pool counter `used_addresses` becomes wrong; eventually `state='exhausted'` triggers spurious `POOL_EXHAUSTED`. | Indirect cause; produces 422, not 500. |
| H3 | `AllocateIP` returns a non-`ErrPoolExhausted` DB error (e.g. tx commit failure) → handler line 1020 → 500. | Possible but less probable. |
| H4 | `simStore.Activate` returns a non-`ErrSIMNotFound` / non-`ErrInvalidStateTransition` error (e.g. state-history insert fails) → handler line 1049 → 500. | Possible. |

The Discovery task must capture the actual `pgx` error / SQLSTATE from container logs before fix tasks proceed.

---

## Discovery Findings — 2026-04-26 (Task 1)

- **Confirmed root cause:** H3 variant — `AllocateIP` fails with SQLSTATE 42703 (`undefined_column`) because `ipAddressColumns` in `internal/store/ippool.go:217-219` includes `last_seen_at` but that column was never added to the `ip_addresses` DB table (DB has 11 columns; `last_seen_at` is absent). The pool exists (979 available IPs) so H1 is definitively ruled out.
- **Stack reproduction:** HTTP 500 on `POST /sims/fffa41ad-abfe-4cd6-9ec6-6bd81409e2c1/activate` — `{"status":"error","error":{"code":"INTERNAL_ERROR","message":"An unexpected error occurred"}}`
- **DB state:** sims row → state=`suspended`, apn_id=`00000000-0000-0000-0000-000000000302`, ip_address_id=`92853e9f-5a7f-4b72-bf77-933435d91764`; pool count for that APN = 1 (XYZ-M2M-Pool, 979 available / 43 reserved of 1024 total)
- **Container log excerpt** (essential lines only):
  ```
  ERR allocate ip for activate error="store: find available ip: ERROR: column \"last_seen_at\" does not exist (SQLSTATE 42703)" component=sim_handler service=argus sim_id=fffa41ad-abfe-4cd6-9ec6-6bd81409e2c1
  WRN gx: get ip_address for release failed error="store: get ip address by id: ERROR: column \"last_seen_at\" does not exist (SQLSTATE 42703)" component=diameter_server
  WRN radius: get ip_address for release failed error="store: get ip address by id: ERROR: column \"last_seen_at\" does not exist (SQLSTATE 42703)" component=radius_server
  ```
- **Code drift check:** handler line 1049 is still the bare-500 site — yes, confirmed (handler.go:1049-1050 matches plan exactly). The error arrives at line 1020 (`allocate ip for activate` branch → H3 → line 1021 → 500).
- **Root cause detail:** `ipAddressColumns` var (`internal/store/ippool.go:217-219`) = `id, pool_id, address_v4::text, address_v6::text, allocation_type, sim_id, state, allocated_at, reclaim_at, last_seen_at`. The `last_seen_at` column exists in the `IPAddress` struct (line 151) and in `ipAddressColumnsJoined` but was NEVER added to the `ip_addresses` table via migration. This breaks every `AllocateIP`, `ReserveIP`, `ReleaseIP`, and `GetIPByID` call that uses `ipAddressColumns`. This is a systemic bug affecting AAA stack too (RADIUS/Diameter release paths fail with same SQLSTATE).
- **DEV-388 (Resume usage):** FE calls `/resume` (not `/activate`) on the SIM detail page Resume button — `web/src/pages/sims/detail.tsx:101` → `{ action: 'resume', label: 'Resume', ... }`, wired to `use-sims.ts:218` `/sims/${simId}/${action}`. SIM LIST page uses `/activate` for stock→active; detail page uses `/resume` for suspended→active. **Both verbs hit `AllocateIP` via their respective handlers and both fail with SQLSTATE 42703.**
- **Wave 2 fix path:** PIVOT required — primary fix is adding migration to add `last_seen_at` column to `ip_addresses` table OR removing it from `ipAddressColumns` (if column is not actually needed). Tasks 2-4 as planned (handler guard + nullable IP + suspend release) are still valid and needed, but Task 3 (`SIMStore.Activate` nullable sig) now also requires fixing `AllocateIP` / `ipAddressColumns` as prerequisite. Recommend adding this as Task 3a or prepending to Task 3.
- **New uncertainties for Ana Amil:**
  1. Is `last_seen_at` intentionally designed but its migration was never written (omission), OR was it added to the struct/constant as dead code and should be removed? Check migration history and `IPAddress` struct usage to decide whether to ADD the migration or REMOVE from `ipAddressColumns`.
  2. `ReleaseIP` and `GetIPByID` also use `ipAddressColumns` — this same SQLSTATE 42703 breaks the RADIUS/Diameter IP-release path for ALL SIMs (visible in logs). Fix scope may expand beyond FIX-252 boundary.
  3. DEV-388 confirmed: `/resume` calls the Resume handler (not Activate). Resume handler must also be checked for `AllocateIP` usage — if it calls it, same fix applies there too.

---

## Architecture Context

### Components Involved

- **Handler:** `internal/api/sim/handler.go` — `Handler.Activate` (line 974), `Handler.Suspend` (line 1086), `Handler.Resume` (line ~1149).
- **Store (SIM):** `internal/store/sim.go` — `SIMStore.Activate` (line 420), `SIMStore.Suspend` (line 467), `SIMStore.Resume` (line 512), `SIMStore.Terminate` (line 562 — reference for "release IP on state-change" pattern).
- **Store (IP pool):** `internal/store/ippool.go` — `IPPoolStore.AllocateIP` (line 664), `IPPoolStore.ReleaseIP` (line 727).
- **Error envelope:** `internal/apierr/apierr.go` — `CodePoolExhausted` (line 79), `CodeInvalidStateTransition`, `CodeNotFound`, `CodeInternalError`. Helper `apierr.WriteError(w, status, code, msg)`.
- **Audit:** `Handler.createAuditEntry(r, action, target, before, after, userID)` — already invoked at line 1054 on success path; missing on failure paths.
- **State machine:** `internal/store/sim.go:92-93` — `"suspended": {"active", "terminated"}` allows direct `suspend → active` via `Activate` (Resume is a separate convenience verb that hits a similar code path with extra `suspended_at = NULL`).

### Data Flow (current — buggy)

```
POST /api/v1/sims/{id}/activate
  → Handler.Activate
    → simStore.GetByID            (existing SIM fetched, ok)
    → assert existing.APNID != nil (story SIM has APN, passes)
    → ippoolStore.List(tenantID, "", 1, existing.APNID)   ← may return EMPTY slice
    → if len(pools) > 0:
         AllocateIP(pools[0].ID, simID)  ← skipped when pools empty
    → ipAddressID := uuid.Nil           ← ZERO UUID when pools empty
    → simStore.Activate(tenantID, simID, ipAddressID, userID)
         UPDATE sims SET ip_address_id = $3   ← FK violation when $3 = uuid.Nil
    → bare 500 INTERNAL_ERROR              ← BUG
```

### Data Flow (target — fixed)

```
POST /api/v1/sims/{id}/activate
  → Handler.Activate
    → simStore.GetByID
    → assert existing.APNID != nil
    → ippoolStore.List → pools
    → if len(pools) == 0:
         audit "sim.activate.failed" with reason
         return 422 POOL_EXHAUSTED "No IP pool configured for this APN"   ← NEW
    → AllocateIP(pools[0].ID, simID)
         on ErrPoolExhausted → audit failure → 422 POOL_EXHAUSTED         ← already exists
         on other error      → audit failure → 500 INTERNAL_ERROR (logged)  ← audit ADDED
    → simStore.Activate(tenantID, simID, &allocatedIP.ID, userID)
         pass NULLABLE pointer (or guarantee non-nil) — never uuid.Nil    ← fix
    → audit "sim.activate" success                                         ← already exists
    → publish sim.updated envelope                                         ← already exists
```

### API Specifications

| Endpoint | Method | Path | Success | Failure modes (post-fix) |
|----------|--------|------|---------|--------------------------|
| API-044 | POST | `/api/v1/sims/:id/activate` | `200 OK` + standard envelope `{status:"ok", data:<SIMResponse>}` | `403 FORBIDDEN` (no tenant), `400 INVALID_FORMAT` (bad UUID), `404 NOT_FOUND` (SIM gone), `422 VALIDATION_ERROR` (no APN), `422 POOL_EXHAUSTED` (no pool / pool full), `422 INVALID_STATE_TRANSITION` (e.g. terminated SIM), `500 INTERNAL_ERROR` (only on truly unexpected DB errors, with stack + correlation ID logged) |

Standard envelope shape (per `docs/architecture/ERROR_CODES.md`):
```json
{ "status": "error", "error": { "code": "POOL_EXHAUSTED", "message": "...", "details": [] } }
```

`POOL_EXHAUSTED` is canonically **HTTP 422** (ERROR_CODES.md:184). Story spec hints at 409 — that is a non-binding suggestion. **Decision DEV-386 below** keeps 422 for backward compatibility with FE clients, error catalog, and existing handler.

### Database Schema (verified against migrations)

`sims` table (relevant columns) — source `migrations/20260320000002_core_schema.up.sql:283` + FK migration `20260420000002_sims_fk_constraints.up.sql:54-57`:

```sql
CREATE TABLE sims (
    id              UUID PRIMARY KEY,
    tenant_id       UUID NOT NULL,
    apn_id          UUID,
    ip_address_id   UUID,    -- nullable; FK to ip_addresses(id)
    state           TEXT NOT NULL,    -- {active, suspended, terminated, stolen_lost, ...}
    activated_at    TIMESTAMPTZ,
    suspended_at    TIMESTAMPTZ,
    terminated_at   TIMESTAMPTZ,
    purge_at        TIMESTAMPTZ,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- ...
);
-- FK from FIX-206 (2026-04-20):
ALTER TABLE sims ADD CONSTRAINT fk_sims_ip_address
    FOREIGN KEY (ip_address_id) REFERENCES ip_addresses(id) ON DELETE SET NULL;
```

`ip_addresses` table — source `migrations/20260320000002_core_schema.up.sql:233-249`:

```sql
CREATE TABLE ip_addresses (
    id            UUID PRIMARY KEY,
    pool_id       UUID NOT NULL,
    sim_id        UUID,                       -- nullable
    state         TEXT NOT NULL,              -- {available, allocated, reserved, reclaiming}
    allocation_type TEXT,                     -- {dynamic, static}
    address_v4    INET,
    address_v6    INET,
    allocated_at  TIMESTAMPTZ,
    reclaim_at    TIMESTAMPTZ,
    -- + FIX-069 columns: grace_expires_at, released_at
);
CREATE INDEX  idx_ip_addresses_pool_state ON ip_addresses (pool_id, state);
CREATE INDEX  idx_ip_addresses_sim        ON ip_addresses (sim_id) WHERE sim_id IS NOT NULL;  -- NON-unique
CREATE UNIQUE INDEX idx_ip_addresses_v4   ON ip_addresses (pool_id, address_v4) WHERE address_v4 IS NOT NULL;
CREATE UNIQUE INDEX idx_ip_addresses_v6   ON ip_addresses (pool_id, address_v6) WHERE address_v6 IS NOT NULL;
```

Note: `idx_ip_addresses_sim` is plain (non-unique). A SIM CAN have multiple `state='allocated'` rows pointing at it — this is the ip-leak symptom for H2.

State-machine table — source `internal/store/sim.go:88-97`:

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
| `internal/api/sim/handler.go` | Modify `Handler.Activate` (lines 974-1084) | Guard empty-pool path → 422; add audit entries on failure branches; pass `*uuid.UUID` for nullable IP. |
| `internal/store/sim.go` | Modify `SIMStore.Activate` (lines 420-465) | Accept `*uuid.UUID` instead of `uuid.UUID` for `ipAddressID`; build SQL conditionally to avoid sending `uuid.Nil`. |
| `internal/store/sim.go` | Modify `SIMStore.Suspend` (lines 467-510) | Release IP allocation atomically inside the suspend tx (mirror `Terminate` pattern at lines 599-608, but immediate release not grace reclaim). |
| `internal/store/sim.go` | Modify `SIMStore.Resume` (lines 512-560) | Resume currently does NOT re-allocate an IP — confirm this is intentional and either (a) document or (b) re-allocate. Decision DEV-388. |
| `internal/api/sim/handler_test.go` | Add tests | Round-trip suspend → activate (AC-4), pool-empty → 422 (AC-2), audit-on-failure (AC-3). |
| `internal/store/sim_test.go` | Add test | Suspend releases IP at store layer. |
| `docs/architecture/ERROR_CODES.md` | Annotate `POOL_EXHAUSTED` row | Note the new "no pool configured" case folds under same code (line 184). |

---

## Story-Specific Compliance Rules

- **API:** Standard envelope per `docs/architecture/ERROR_CODES.md`. `POOL_EXHAUSTED` stays HTTP 422 (DEV-386). All error responses MUST go through `apierr.WriteError` — never raw `http.Error` or `w.WriteHeader(500)`.
- **DB:** Suspend-side IP release MUST run inside the same transaction as the state UPDATE (atomicity). Tenant-scoping preserved on every store query (`WHERE tenant_id = $2`).
- **Audit:** Per CLAUDE.md "every state-changing operation creates an audit log entry" — extend to BOTH success and failure paths (AC-3). Use existing `Handler.createAuditEntry` helper. Action verbs: `sim.activate`, `sim.activate.failed` (new), `sim.suspend`, `sim.suspend.failed` (only if suspend can fail post-fix).
- **State machine:** `validateTransition("suspended", "active")` already passes — do NOT introduce a new state, just fix the IP allocation around it.
- **Logging:** All failure branches must `h.logger.Error().Err(err).Str("sim_id", idStr).Str("correlation_id", correlationID).Msg(...)`. Correlation ID is in `r.Context()` via existing middleware.

---

## Bug Pattern Warnings

Read `docs/brainstorming/bug-patterns.md` (PAT-001..PAT-022) — none directly target IP allocation, SIM state machine, or store-handler error contracts. Closest neighbors (PAT-006, PAT-017) are not specific enough to apply.

**No matching bug patterns** for FIX-252.

(Discovery findings may seed a NEW pattern — see Step 7. If the root cause is H1 — uuid.Nil FK violation — the new pattern is "Handler passes `uuid.Nil` to a store method whose SQL has FK on the column". Only file PAT after the fix is verified.)

---

## Tech Debt (from ROUTEMAP)

`docs/ROUTEMAP.md` Tech Debt table scanned — **no items target FIX-252**.

(D-141..D-146 from the FIX-234 batch are unrelated: CoA lifecycle, dashboard counters.)

---

## Mock Retirement

Backend-only story; no FE mocks affected. **No mock retirement.**

---

## Decisions to Log (decisions.md)

Next free DEV id is **DEV-386** (highest existing: DEV-385).

- **DEV-386** — Keep `POOL_EXHAUSTED` at HTTP **422**, NOT 409 (story suggestion). Rationale: ERROR_CODES.md authoritatively documents 422; existing handler + tests + FE error-mapping already wire 422; switching to 409 is a breaking API change with no callable benefit. AC-2 is satisfied by any structured 4xx — 422 qualifies.
- **DEV-387** — `SIMStore.Suspend` MUST release the SIM's currently-allocated IP atomically inside the suspend transaction (set `ip_addresses.state='available'`, decrement `ip_pools.used_addresses`, NULL `sims.ip_address_id`). Rationale: Without this, post-suspend allocations leak the previous IP — pool counter drifts, `state='exhausted'` triggers spuriously, and audit history is misleading. Mirrors `Terminate` pattern (sim.go:599-608) but immediate not grace.
- **DEV-388** *(provisional, decide post-discovery)* — `SIMStore.Resume` (a separate verb from `Activate`) currently does NOT re-allocate an IP. After Suspend now releases, Resume on a previously-suspended SIM must either (a) reuse the same handler-side allocate-then-call-store flow, or (b) be deprecated in favor of `/activate`. Will be determined after Discovery task confirms whether FE calls `/resume` or `/activate` on suspended SIMs and whether a Resume-without-IP SIM is ever expected in the AAA path.

---

## Tasks

> Each task is dispatched to a fresh Developer subagent. The orchestrator extracts `Context refs` sections and passes them in the dispatch prompt. The Developer does NOT read this plan file directly.

---

### Task 1: Discovery — reproduce 500 and classify root cause

- **Files:** No code changes. Append findings inline to this plan (new "## Discovery Findings" section near the top) AND update `docs/stories/fix-ui-review/FIX-234-step-log.txt` with FIX-252 line.
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** N/A (investigation task)
- **Context refs:** "Bug Description", "Root Cause Hypotheses", "Affected Files"
- **What:**
  1. Bring stack up: `make up` (or `make infra-up && go run ./cmd/argus/`).
  2. Confirm stuck SIM exists in DB: `psql -c "SELECT id, state, apn_id, ip_address_id FROM sims WHERE id='fffa41ad-abfe-4cd6-9ec6-6bd81409e2c1';"` — record state, APN id, IP id.
  3. Confirm pool count for that APN: `psql -c "SELECT COUNT(*) FROM ip_pools WHERE apn_id='<apn_id>' AND tenant_id='<tenant_id>';"` and `SELECT * FROM ip_addresses WHERE pool_id IN (SELECT id FROM ip_pools WHERE apn_id='<apn_id>') GROUP BY state, pool_id;`.
  4. Reproduce: obtain JWT via `/auth/login` (admin@argus.io / admin), then `curl -X POST http://localhost:8084/api/v1/sims/fffa41ad-abfe-4cd6-9ec6-6bd81409e2c1/activate -H 'Authorization: Bearer …' -i`.
  5. Capture full container log around the 500: `docker logs argus 2>&1 | grep -A 20 'activate sim\|allocate ip for activate' | tail -50`.
  6. Identify the wrapped error message AND the underlying pgx SQLSTATE if present (look for `23503` = FK violation, `23505` = unique violation, `23502` = NOT NULL).
  7. Classify against H1/H2/H3/H4 above. If a 5th cause emerges, document it.
  8. Confirm DEV-388 question: grep FE for `'/activate'` vs `'/resume'` calls on suspended SIMs (hint: `web/src/hooks/use-sims.ts:215` exposes both — confirm which one the SIM list "Resume" button actually calls).
- **Verify:** A new "## Discovery Findings" section is added to this plan file with: confirmed root cause hypothesis, captured stack trace excerpt, captured SQLSTATE if any, and a one-line "Fix path = Tasks 2 + 3 + 4 + 5" or pivot note if H1 is wrong. **No code changed in this task.**

---

### Task 2: Handler — guard empty-pool path + audit on failure

- **Files:** Modify `internal/api/sim/handler.go` (`Handler.Activate`, lines 974-1084).
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/sim/handler.go:1086-1145` (`Handler.Suspend`) for the structured-error + audit pattern; read `Handler.Activate` itself for the existing `ErrPoolExhausted → 422` pattern.
- **Context refs:** "Architecture Context > Components Involved", "Architecture Context > Data Flow (target)", "API Specifications", "Story-Specific Compliance Rules"
- **What:**
  1. After `pools, _, err := h.ippoolStore.List(...)` and before the `if len(pools) > 0` block: insert an explicit guard `if len(pools) == 0 { ... }` that:
     - Calls `h.createAuditEntry(r, "sim.activate.failed", simID.String(), existing, map[string]interface{}{"reason": "no_pool_for_apn", "apn_id": existing.APNID}, userID)`.
     - Calls `apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodePoolExhausted, "No IP pool configured for this APN")`.
     - Returns.
  2. Add audit-on-failure entries to the existing error branches at lines ~995, ~1006, ~1020, ~1049 — same `sim.activate.failed` action, with `reason` keyed by branch (`"get_sim_failed"`, `"list_pools_failed"`, `"allocate_failed"`, `"state_transition_failed"`, etc.).
  3. **Do not change** the existing `ErrPoolExhausted → 422` mapping (already correct per DEV-386).
  4. **Do not change** the success path (audit + envelope publish + policy auto-match). They already work.
  5. Update the call site for `simStore.Activate` to match the new signature from Task 3 (pass `&allocatedIP.ID` instead of `ipAddressID uuid.UUID`). Keep `nil` semantics for the "no allocation occurred" case — but with the new guard, this case is unreachable from this handler.
  6. Tenant-scoping: every error response MUST log `tenant_id` and `sim_id` for grep-ability.
- **Verify:**
  - `go build ./...`
  - `go vet ./internal/api/sim/...`
  - `go test ./internal/api/sim/... -count=1 -run Activate`
  - Manual: `curl -X POST .../activate` for a SIM whose APN has zero pools returns `422 POOL_EXHAUSTED` with the new message.

---

### Task 3: Store — `SIMStore.Activate` signature accepts nullable IP + null-safe SQL

- **Files:** Modify `internal/store/sim.go` (`SIMStore.Activate`, lines 420-465).
- **Depends on:** —  *(parallelizable with Task 2 — Task 2 modifies the call site to match)*
- **Complexity:** low
- **Pattern ref:** Read `internal/store/sim.go:850-877` (`SIMStore.SetIPAndPolicy`) for the `if ipAddressID != nil` SQL-building idiom already used in this file.
- **Context refs:** "Architecture Context > Components Involved", "Database Schema", "Story-Specific Compliance Rules"
- **What:**
  1. Change signature from `Activate(ctx, tenantID, simID uuid.UUID, ipAddressID uuid.UUID, userID *uuid.UUID)` to `Activate(ctx, tenantID, simID uuid.UUID, ipAddressID *uuid.UUID, userID *uuid.UUID)`.
  2. Build the UPDATE SQL conditionally: when `ipAddressID == nil` emit `UPDATE sims SET state='active', activated_at=NOW(), updated_at=NOW() WHERE …` (no `ip_address_id = …`). When non-nil, keep current behaviour with `ip_address_id = $3`.
  3. Update all call sites (handler is Task 2; also grep `simStore.Activate(` repo-wide for tests / fixtures and update them).
  4. Preserve `validateTransition` check, state-history insert, tx commit ordering — no semantic changes besides the nullable IP.
- **Verify:**
  - `go build ./...`
  - `go vet ./internal/store/...`
  - `go test ./internal/store/... -count=1 -run SIMActivate`
  - `grep -rn "simStore.Activate(\|SIMStore.Activate(" /Users/btopcu/workspace/argus --include='*.go'` returns only call sites updated to pass `*uuid.UUID`.

---

### Task 4: Store — `SIMStore.Suspend` releases IP atomically (DEV-387)

- **Files:** Modify `internal/store/sim.go` (`SIMStore.Suspend`, lines 467-510).
- **Depends on:** —  *(parallelizable with Tasks 2 + 3)*
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/sim.go:599-608` (`SIMStore.Terminate` IP-reclaim block) and `internal/store/ippool.go:727-777` (`IPPoolStore.ReleaseIP` for the canonical decrement-counter + un-exhaust pattern). Mirror the structure but with **immediate** release (state → `available`) not grace reclaim (state → `reclaiming`).
- **Context refs:** "Architecture Context > Components Involved", "Database Schema", "Decisions to Log (DEV-387)"
- **What:**
  1. After the existing `SELECT state FROM sims … FOR UPDATE`, also select `ip_address_id` (mirror `Terminate` line 569-574).
  2. After the SIM `UPDATE sims SET state='suspended' …` and BEFORE `tx.Commit`:
     - If `ipAddressID != nil`, lookup the matching `ip_addresses` row (`SELECT pool_id FROM ip_addresses WHERE id = $1 AND state IN ('allocated','reserved')`).
     - If found: `UPDATE ip_addresses SET state='available', sim_id=NULL, allocated_at=NULL WHERE id = $1` AND `UPDATE ip_pools SET used_addresses = GREATEST(used_addresses - 1, 0) WHERE id = $2` AND `UPDATE ip_pools SET state='active' WHERE id = $2 AND state='exhausted'` (mirror `IPPoolStore.ReleaseIP` lines 756-765).
     - Also `UPDATE sims SET ip_address_id = NULL` so re-activation starts clean.
  3. **Static allocations (`allocation_type='static'`) MUST be preserved** — do NOT release them on suspend. Skip the IP-release block entirely when `allocation_type = 'static'` (mirror Terminate's distinction at sim.go:602 — but for suspend we genuinely skip rather than schedule reclaim, since static IPs survive suspend by design).
  4. All operations inside the existing `tx` — no separate transaction.
  5. Add comments referencing DEV-387.
- **Verify:**
  - `go build ./...`
  - `go vet ./internal/store/...`
  - `go test ./internal/store/... -count=1 -run "Suspend|Activate"`
  - Manual SQL post-suspend: `SELECT s.ip_address_id, i.state, i.sim_id FROM sims s LEFT JOIN ip_addresses i ON i.sim_id = s.id WHERE s.id = '<test-sim>';` returns NULL for `ip_address_id` and zero rows for the join.

---

### Task 5: Regression tests (suspend → activate round-trip + pool-empty + suspend release)

- **Files:** Add tests to `internal/api/sim/handler_test.go` AND `internal/store/sim_test.go`.
- **Depends on:** Tasks 2, 3, 4
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/sim/handler_test.go` for handler-test setup (HTTP recorder, mock store, JWT context). Read `internal/store/sim_test.go` for store-test setup (postgres test container or `tx.Rollback`-isolated tests). Read `internal/store/ippool_allocation_cycle_test.go` for the closest-existing IP allocation+release round-trip pattern.
- **Context refs:** "Acceptance Criteria Mapping", "Architecture Context > Data Flow (target)", "API Specifications"
- **What:**
  1. **Handler test — `TestActivate_AfterSuspend_RoundTrip`** (AC-1, AC-4): Seed a SIM in `active` state with allocated IP; POST `/suspend`; assert 200 + suspend audit; POST `/activate`; assert 200 + new IP allocated + activate audit + `sim.updated` envelope published.
  2. **Handler test — `TestActivate_PoolEmpty_Returns422_PoolExhausted`** (AC-2): Seed a SIM with APN that has zero pools; POST `/activate`; assert HTTP 422 + envelope `code: "POOL_EXHAUSTED"` + `sim.activate.failed` audit entry.
  3. **Handler test — `TestActivate_PoolFull_Returns422_PoolExhausted`** (AC-2): Seed a SIM with APN whose only pool has all addresses `state='allocated'`; POST `/activate`; assert HTTP 422 + envelope `code: "POOL_EXHAUSTED"` + audit entry.
  4. **Handler test — `TestActivate_AuditOnFailure`** (AC-3): Trigger any 4xx/500 path; assert an audit row was written with action `sim.activate.failed` and a `reason` field.
  5. **Store test — `TestSIMStore_Suspend_ReleasesIP`** (AC-1 supporting): Seed SIM with allocated dynamic IP; call `Suspend`; assert `ip_addresses.state='available'`, `sim_id IS NULL`, `ip_pools.used_addresses` decremented, `sims.ip_address_id IS NULL`.
  6. **Store test — `TestSIMStore_Suspend_PreservesStaticIP`**: Seed SIM with `allocation_type='static'`; call `Suspend`; assert IP row untouched.
- **Verify:**
  - `go test ./internal/api/sim/... -count=1 -run "Activate|Suspend"`
  - `go test ./internal/store/... -count=1 -run "Suspend|Activate"`
  - All new tests PASS; no regression in existing tests.

---

### Task 6: Manual restore + USERTEST + decisions.md

- **Files:** Modify `docs/USERTEST.md`, modify `docs/brainstorming/decisions.md`.
- **Depends on:** Tasks 2-5 merged
- **Complexity:** low
- **Pattern ref:** Read the FIX-234 USERTEST section in `docs/USERTEST.md` (around the most recent "FIX-234" header) for the Turkish-language scenario template.
- **Context refs:** "Acceptance Criteria Mapping", "Decisions to Log"
- **What:**
  1. Restore stuck SIM either via `psql -c "UPDATE sims SET state='active', ip_address_id=NULL WHERE id='fffa41ad-abfe-4cd6-9ec6-6bd81409e2c1';"` OR full `make db-seed`. Confirm via curl `/sims/{id}/activate` returns 200 (AC-5).
  2. Add a `## FIX-252` section to `docs/USERTEST.md` with 4 Turkish scenarios mapping AC-1..AC-5 (round-trip suspend → activate, no-pool 422, pool-full 422, audit log presence on success+failure, manual restore confirmation).
  3. Append DEV-386, DEV-387, DEV-388 (resolved or marked open) entries to `docs/brainstorming/decisions.md` with the rationale text from this plan's "Decisions to Log" section.
- **Verify:**
  - `grep -n "FIX-252" docs/USERTEST.md` returns the new section.
  - `grep -nE "DEV-38[678]" docs/brainstorming/decisions.md` returns three new lines.
  - Stuck-SIM curl returns HTTP 200.

---

## Acceptance Criteria Mapping

| AC | Description | Implemented in | Verified by |
|----|-------------|----------------|-------------|
| AC-1 | Round-trip suspend → activate succeeds | Tasks 2, 3, 4 | Task 5 (TestActivate_AfterSuspend_RoundTrip), Task 6 manual |
| AC-2 | Pool-cannot-allocate → structured 4xx (422) never bare 500 | Task 2 (handler guard) | Task 5 (TestActivate_PoolEmpty / TestActivate_PoolFull) |
| AC-3 | Stack + correlation ID logged; audit on success AND failure | Task 2 (failure audit + logging) | Task 5 (TestActivate_AuditOnFailure) |
| AC-4 | Unit test: activate-after-suspend round-trip | Task 5 | `go test` PASS |
| AC-5 | SIM `fffa41ad-…` reactivatable | Task 6 (manual restore) | curl returns 200 |

---

## Risks & Mitigations

- **R1 — Activate is hot path** for operator-driven activations. **Mitigation:** Tasks 2+3 are additive (new branch + nullable signature); the success path SQL is unchanged. Task 5 regression tests cover the existing happy path explicitly. Run `go test ./internal/api/sim/... ./internal/store/... ./internal/aaa/... -count=1` after Task 4 to catch any AAA-side coupling.
- **R2 — Suspend now releases IP** — eSIM provisioning and policy assignment may hold expectations about `sims.ip_address_id` persistence across suspend. **Mitigation:** Audit `internal/aaa/*` and `internal/policy/*` for reads of `sims.ip_address_id` on suspended SIMs in Task 4 (grep `state.*suspended.*ip_address_id` and reverse). Static allocations are explicitly preserved.
- **R3 — Discovery confirms a 5th root cause** outside H1-H4. **Mitigation:** Task 1 produces a "Discovery Findings" section that may pivot Tasks 2-4. Plan accepts this — Task 1 is gating, others depend on it conceptually even when listed parallel-eligible.
- **R4 — `Resume` semantics drift after `Suspend` releases.** **Mitigation:** DEV-388 marker in Task 1 + Task 6. Decide before merging Task 4 whether `Resume` should re-allocate or be deprecated; if deprecated, FE update is a separate FIX-NNN out of scope here.
- **R5 — `make db-seed` regression** if Task 4 introduces a transient `ip_addresses.state` invariant violation. **Mitigation:** Run `make db-migrate && make db-seed` after Task 4; per CLAUDE.md "Seed discipline" rule, MUST stay clean.

---

## Wave Plan (for Amil dispatch)

```
Wave 1 (sequential gate):       Task 1 (Discovery — must complete first)
Wave 2 (parallel — 3 tasks):    Task 2  | Task 3  | Task 4
Wave 3 (sequential):            Task 5 (depends on Tasks 2+3+4 merged)
Wave 4 (sequential):            Task 6 (manual + docs — final)
```

Total: **6 tasks, 4 waves** (Wave 1 = gate, Wave 2 = 3-way parallel fix, Wave 3 = test, Wave 4 = docs/manual).

Complexity breakdown: low=4 (T1, T3, T6, …) — wait, recount:
- T1 low / T2 medium / T3 low / T4 medium / T5 medium / T6 low → **low=3, medium=3, high=0**.

(Effort=S → mostly low + max 1 medium per protocol. We have 3 medium because Task 4 inherently couples store-state-machine + IP-pool semantics, Task 2 adds 5 new code branches with audit, and Task 5 covers 6 test scenarios. This is a defensible deviation; advisor explicitly recommended marking Suspend release as medium. No `high` task — story scope does not include cross-service orchestration.)

---

## Quality Gate Self-Validation

Run all checks; PASS only when every box is ✓.

**a. Minimum substance (S → 30 lines, 2 tasks):** ✓ Plan ~340 lines, 6 tasks.

**b. Required sections present:**
- ✓ `## Goal`
- ✓ `## Architecture Context`
- ✓ `## Tasks` (with numbered `### Task` blocks)
- ✓ `## Acceptance Criteria Mapping`

**c. Embedded specs:**
- ✓ API: endpoint table + envelope shape + status code list
- ✓ DB: `sims` + `ip_addresses` schemas embedded with source migration paths
- N/A UI: backend-only story, no Design Token Map needed

**d. Task complexity cross-check (S story):** ✓ Mostly low + 3 mediums (justified inline). Zero `high` — appropriate for backend bug-fix scope.

**e. Context refs validation:** Each task's `Context refs` lists actual section headers present in this plan:
- ✓ "Bug Description" / "Root Cause Hypotheses" / "Affected Files"
- ✓ "Architecture Context > Components Involved" / "Architecture Context > Data Flow"
- ✓ "API Specifications" / "Database Schema" / "Story-Specific Compliance Rules"
- ✓ "Acceptance Criteria Mapping" / "Decisions to Log"

**Architecture compliance:**
- ✓ Each task in correct layer (handler / store / docs)
- ✓ No cross-layer imports planned
- ✓ Dependency direction: handler → store → DB (preserved)
- ✓ Component naming matches `Handler`, `SIMStore`, `IPPoolStore` conventions

**API compliance:**
- ✓ Envelope shape stated
- ✓ HTTP method/path correct (POST /api/v1/sims/:id/activate)
- ✓ Validation already exists; new guard inserted before allocation
- ✓ All error responses specified per branch in handler

**DB compliance:**
- ✓ No new migration needed (FK already exists from FIX-206)
- ✓ Embedded schema sourced from ACTUAL migrations (`20260320000002_core_schema.up.sql`, `20260420000002_sims_fk_constraints.up.sql`) — verified
- ✓ Column names verified (`ip_address_id`, `state`, `sim_id`, `pool_id`, `used_addresses`, etc.)
- ✓ Indexes noted (`idx_ip_addresses_sim` is non-unique — explicit fact-check)
- ✓ Tenant-scoping enforced in every store query call-out

**UI compliance:** N/A (backend-only).

**Task decomposition:**
- ✓ Each task touches ≤3 files
- ✓ No task requires 5+ files or 10+ minutes
- ✓ Discovery first (gating); fix tasks parallel; tests after; docs last
- ✓ Each task has `Depends on`
- ✓ Each task has `Context refs` pointing at real sections
- ✓ Each task creating new code references a Pattern file (closest existing — `Suspend` for handler, `SetIPAndPolicy` for store, `Terminate` for IP-release, `ippool_allocation_cycle_test.go` for tests)
- ✓ Tasks functionally grouped (handler-fix / store-signature / store-suspend-release / tests / docs)
- ✓ Reasonable count (6 for Effort=S — a touch over the ideal 4-5 because Discovery + Audit + Suspend-release each genuinely need their own dispatch context)
- ✓ NO implementation code in tasks — specs + pattern refs only

**Test compliance:**
- ✓ Test task (Task 5) covers each AC with named test functions
- ✓ Test file paths specified
- ✓ Round-trip + pool-empty + pool-full + audit + suspend-release scenarios all listed

**Self-containment:**
- ✓ API specs embedded
- ✓ DB schema embedded with migration source paths
- ✓ State-machine table embedded
- ✓ Business rules stated inline (POOL_EXHAUSTED 422 decision, static-IP preservation rule, tenant-scoping)
- ✓ Every Context refs target exists as a header in this plan

**Quality Gate Result: PASS**

---

## Open Uncertainties for Ana Amil

- **Task 1 may pivot the fix** — if Discovery confirms H2/H3/H4 (not H1), the handler guard in Task 2 still helps but the primary fix shifts. Tasks 2-4 are written to cover all 4 hypotheses defensively, so no re-plan is strictly required, but the dispatch should respect Wave 1 as a hard gate.
- **DEV-388 (Resume re-allocation)** is provisional — confirm during Discovery whether to keep or drop that decision. If kept and `Resume` needs IP re-allocation, an additional small task (or scope-creep into Task 2) may be needed; flag for Ana.

— END PLAN —

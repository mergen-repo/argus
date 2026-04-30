# Implementation Plan: STORY-092 — Dynamic IP Allocation pipeline + SEED FIX

> Plan drafted 2026-04-18 by the Amil Planner agent after code-state
> validation against the current tree (post-STORY-086 DONE, post-STORY-087
> DONE, post-STORY-088 DONE, post-D-038 enforcer nil-guard 2026-04-17) and
> advisor pre-draft consultation.

## Goal

Make IP allocation actually happen on the AAA hot paths so SIMs receive
routable addresses during authentication, sessions surface them in the
UI, `ip_pools.used_addresses` reflects reality, and the policy enforcer
nil-cache path (D-038) is exercised by at least one integration test.
Concretely: (1) seed 003's 120+ SIMs gain reserved IPs from their APN
pool (matching the seed-006 pattern already in place for seed-005's 16
SIMs), (2) RADIUS Access-Accept dynamically allocates and persists an
IP when `sim.IPAddressID IS NULL`, (3) RADIUS Accounting-Stop releases
the dynamic allocation symmetrically, (4) Diameter Gx CCA-I installs a
Framed-IP-Address AVP (code 8) and CCR-T releases, (5) 5G SBA gains a
minimal mock `Nsmf_PDUSession` endpoint (Create/Release) that allocates
and releases UE IPs end-to-end via the same store-layer pipeline
(matching the existing Argus "mock-the-operator-SoR" stance already
shipped by STORY-082..STORY-085's AUSF/UDM mocks), (6) a Wave-1
integration test boots an Enforcer with `policyCache=nil` (matching
`cmd/argus/main.go:1055-1062` today) and runs RADIUS Access-Request
through to Access-Accept with a non-empty Framed-IP-Address.

## Architecture Context

### Current deployed reality (verified 2026-04-18)

#### RADIUS auth path (`internal/aaa/radius/server.go`)

- `sendEAPAccept()` at line 343 — builds Access-Accept for EAP auth.
  Between lines 360-416 it looks up `sim` via IMSI, and at line 363 it
  reads `sim.IPAddressID`. If non-nil, fetches the `ip_addresses` row
  via `ipPoolStore.GetIPAddressByID()` at :364 and sets
  `rfc2865.FramedIPAddress_Set(accept, ip.To4())` at :370. If
  `sim.IPAddressID == nil` — **no Framed-IP is ever attached**. Policy
  evaluation happens AFTER the IP block (line 384-414).
- `handleDirectAuth()` at line 458 — same pattern. Policy evaluation
  happens at lines 514-554 BEFORE the Access-Accept build. After
  policy Allow, line 556 builds `accept`, line 558 reads
  `sim.IPAddressID`, line 559-564 attaches Framed-IP **only if
  pre-assigned**, line 582 writes the packet. No `AllocateIP` call.
- `handleAcctStart()` at line 637 — reads `framedIP` from the incoming
  RADIUS packet at lines 683-686 via `rfc2865.FramedIPAddress_Lookup`.
  If the NAS omits the attribute (simulator-style), `session.framed_ip`
  is saved empty. No fallback to `sim.IPAddressID`.
- `handleAcctStop()` at line 832 — reads `bytesIn/bytesOut`, calls
  `sessionMgr.TerminateWithCounters`, publishes `session.ended`. **No
  `ReleaseIP` call.** Even dynamic allocations leak forever.

Server struct at `:42-67` already has `ipPoolStore *store.IPPoolStore`
and `simCache *SIMCache`. Enforcer is attached via
`SetPolicyEnforcer(*enforcer.Enforcer)` from `main.go:1068`.

#### Diameter Gx (`internal/aaa/diameter/gx.go`)

- `handleInitial()` at line 69 — on CCR-I, builds CCA-I at lines 146-162.
  Attaches Session-ID, Result-Code, CC-Request-Type, CC-Request-Number,
  Auth-Application-ID, and a `Charging-Rule-Install` AVP via
  `BuildChargingRuleInstall()`. **Zero Framed-IP-Address handling.**
- `handleTermination()` at line 211 — calls `sessionMgr.Terminate`,
  publishes `session.ended`, returns CCA-T. **No `ReleaseIP` call.**
- `GxHandler` struct at `:15-21` has `simResolver SIMResolver` (which is
  the same `*radius.SIMCache` at `main.go:981`) but NO `ipPoolStore`.
  Wiring needs extension. `ServerDeps` at `server.go:26-31` likewise
  lacks `IPPoolStore`.

Gy (`internal/aaa/diameter/gy.go`) is credit-control/charging — no IP
responsibility per RFC 4006 / 3GPP TS 32.299. **Not in scope.**

Diameter AVP codes in `avp.go:17-69`: `AVPCodeFramedIPAddress = 8` is
**not yet defined**. Must be added. Helper `NewAVPAddress(code, flags,
vendorID, ip [4]byte)` exists at `avp.go:288` and is used for
`AVPCodeHostIPAddress` — reuse verbatim.

#### 5G SBA (`internal/aaa/sba/server.go`)

Routes mounted at `:75-111`:
- `/nausf-auth/v1/ue-authentications[*]` — 5G-AKA + EAP-AKA' auth
- `/nudm-ueau/v1/*` — security-information, auth-events
- `/nudm-uecm/v1/*` — registrations
- `/nnrf-nfm/v1/*` — NRF registration + heartbeat
- **NEW (this story, D3-B)**: `/nsmf-pdusession/v1/sm-contexts` — minimal
  mock PDU Session Create / Release endpoint (see §Routes below).

**Zero PDU session handling today.** Per 3GPP TS 23.501 §5.6 (User Plane
function selection) and TS 23.502 §4.3.2 (PDU Session Establishment
procedure), in a real 5G Core the SMF owns UE IP allocation during
`Nsmf_PDUSession_CreateSMContext`; AUSF (authentication) and UDM
(subscriber data) do not allocate IPs. `grep -rn
"IPAddressID\|AllocateIP\|Framed-IP" internal/aaa/sba/` currently returns
0 matches, confirming this.

**Why Argus ships its own minimal Nsmf mock** (D3-B rationale): Argus is
a simulator / test platform, not a production 5GC. STORY-082..STORY-085
already expose AUSF/UDM mocks that simulate operator-side behaviour so
that Argus's own simulator harness can drive end-to-end auth flows
without a real HSS/UDM. Extending the mock surface to include a minimal
`Nsmf_PDUSession` endpoint is consistent with that stance — the Nsmf
handler added here does NOT implement real SMF logic (no UPF selection,
no QoS flow establishment, no PCF integration); it only reuses the
existing store-layer `AllocateIP`/`ReleaseIP` pipeline so the 5G path
produces visible UE IPs end-to-end.

**Cross-reference to STORY-089 (Operator SoR Simulator)**: STORY-089 is
the logical long-term home for operator-side PDU session state (it will
own richer operator-simulation scope beyond just Nsmf). STORY-089 is
currently blocked behind STORY-090 (adapter refactor). In the meantime,
the runtime symptom — `/sessions` column "IP" shows `-` for 5G sessions
because AUSF/UDM return no IP and there is no Nsmf at all — is a
visible defect that STORY-092 should close end-to-end. The Nsmf mock
added here is deliberately minimal (Create + Release only, no
modification) so that STORY-089 can later absorb or replace it without
breaking surface contracts.

#### Seed state (verified 2026-04-18)

- `migrations/seed/003_comprehensive_seed.sql:289-301` — INSERTs 9
  ip_pools with `used_addresses=VARIABLE_INT` (e.g., 456, 218, 120…)
  directly into `ip_pools`. **No matching ip_addresses rows are ever
  created.** Line 1165-1170 adds 4 more pools for the demo tenant.
- Seed 003 then creates 120+ SIM rows (lines 308-1100+) without
  `ip_address_id` set. `grep -n "ip_address_id" 003_comprehensive_seed.sql`
  returns 0 matches — SIMs never point to an IP.
- `migrations/seed/005_multi_operator_seed.sql:142-149` creates 4 more
  pools (XYZ-IoT, XYZ-M2M, ABC-IoT, ABC-M2M) with `used_addresses=0`.
- `migrations/seed/006_reserve_sim_ips.sql` is the ALREADY-EXISTING
  precedent that materialises ip_addresses rows for 6 pools (seed 005
  + 2 added inline) and reserves one per SIM via a CTE (lines 119-161),
  then recalculates `used_addresses` deterministically (lines 168-176).
  BUT: seed 006's `candidates` CTE at lines 121-128 only matches SIMs
  whose `ip_address_id IS NULL` AND `apn_id IS NOT NULL` AND
  `state='active'` — it correctly runs against all such SIMs in the
  DB, **including seed 003's**, BUT it only materialises ip_addresses
  for the 6 pools it knows about (the seed-005 pools + 2 XYZ/ABC
  private pools). Seed 003's 13 pools remain empty → seed 003's SIMs
  find no available IP in their APN's pool → candidates CTE sees no
  match → they stay `ip_address_id=NULL`.

The fix (see Decision Point D1 below) is to extend materialisation to
cover seed 003's pools as well, without duplicating seed 006's
reservation logic.

#### IP allocation store (`internal/store/ippool.go`)

- `AllocateIP(ctx, poolID, simID)` at `:554` — transactional. Locks
  pool row, picks lowest-numbered `state='available'` row via
  `FOR UPDATE SKIP LOCKED`, flips it to `state='allocated'` with
  `sim_id`, and updates `ip_pools.used_addresses += 1` in the same tx
  (line 604-605). If no available row: sets pool to `exhausted` and
  returns `ErrPoolExhausted`. Already thread-safe and already maintains
  `used_addresses` app-side.
- `ReleaseIP(ctx, poolID, simID)` at `:617` — for `allocation_type='dynamic'`,
  flips row back to `state='available'`, nulls sim_id/allocated_at,
  decrements `used_addresses` (line 651-652), clears pool's `exhausted`
  state. For `allocation_type='static'`, moves to `state='reclaiming'`
  with `reclaim_at=NOW()+grace_days` — grace release is handled by the
  existing `internal/job/ip_grace_release.go` background worker.
- Current callers of `AllocateIP`: `internal/job/import.go:346`,
  `internal/api/sim/handler.go:876`, `internal/api/ippool/handler.go`
  (plus new hot-path callers added in this story).

#### Enforcer (D-038)

`internal/policy/enforcer/enforcer.go` already has nil-guards at
lines 82-107 (2026-04-17 fix): `if e.policyCache != nil { compiled, ok
= e.policyCache.Get(versionID) }` and fall-through to DB via
`policyStore.GetVersionByID` when cache miss or cache nil.
`cmd/argus/main.go:1051-1062` still instantiates the Enforcer with
literal `nil` for policyCache (TODO D-038). Unit tests in
`enforcer_test.go` cover the nil path **at unit level only** — no
integration test exists that boots the whole RADIUS Access-Request
pipeline against a nil-cache enforcer and a DB-seeded policy.

#### Session DTO flow to UI (verified)

`internal/aaa/session/session.go:45` — `FramedIP string
'json:"framed_ip"'`. Persisted at :128 (`nilIfEmpty(sess.FramedIP)`),
round-tripped at `:664-665`. UI consumes both `ip_address` and
`framed_ip` (`web/src/pages/sessions/index.tsx:142, 249, 502`,
`detail.tsx:230`) so no frontend DTO work is needed — the column
already exists and will populate the moment session.FramedIP is
non-empty.

### Data flow (post-fix, RADIUS happy path)

```
Simulator → RADIUS Access-Request (IMSI=2860100001)
  ↓ server.go:458 handleDirectAuth
  sim ← simCache.GetByIMSI(ctx, imsi)
  policy ← enforcer.Evaluate(ctx, sim, sessCtx)  [D-038 nil-cache path]
  if !policy.Allow → Access-Reject, NO allocation (advisor flag #6)
  ↓
  if sim.IPAddressID == nil AND sim.APNID != nil:
    pools ← ippoolStore.List(ctx, sim.TenantID, "", 1, sim.APNID)
    allocated ← ippoolStore.AllocateIP(ctx, pools[0].ID, sim.ID)
    simStore.SetIPAndPolicy(ctx, sim.ID, &allocated.ID, nil)  [persist]
    simCache.InvalidateIMSI(ctx, imsi)                         [cache coherence]
    sim.IPAddressID = &allocated.ID                            [local in-memory]
  ↓
  if sim.IPAddressID != nil:
    ipAddr ← ippoolStore.GetIPAddressByID(ctx, *sim.IPAddressID)
    rfc2865.FramedIPAddress_Set(accept, parseV4Address(*ipAddr.AddressV4))
  ↓
  w.Write(accept)  → Access-Accept with Framed-IP

Simulator → RADIUS Accounting-Request (Start)
  ↓ handleAcctStart
  framedIP ← rfc2865.FramedIPAddress_Lookup(packet)  [may be empty — simulator]
  if framedIP == "" AND sim.IPAddressID != nil:      [advisor flag #2, bug B]
    ipAddr ← ippoolStore.GetIPAddressByID(ctx, *sim.IPAddressID)
    framedIP ← parseV4Address(*ipAddr.AddressV4)
  session.FramedIP = framedIP  → persists to radius_sessions.framed_ip
  → /sessions UI column "IP" populated

Simulator → RADIUS Accounting-Request (Stop)
  ↓ handleAcctStop
  sessionMgr.TerminateWithCounters(...)
  if sim.IPAddressID != nil AND ip.allocation_type == 'dynamic':
    poolID ← ippoolStore.GetIPAddressByID(*sim.IPAddressID).PoolID
    ippoolStore.ReleaseIP(ctx, poolID, sim.ID)
    simStore.SetIPAndPolicy(ctx, sim.ID, nil_uuid_marker, nil)
    simCache.InvalidateIMSI(ctx, imsi)
  → ip_addresses.state back to 'available', used_addresses -= 1
```

Static (seed-reserved) allocations are NOT released on Accounting-Stop
— `allocation_type='static'` SIMs keep their reserved IP for the life
of the SIM (current `ReleaseIP` semantics preserve this via the
`reclaim_at` grace path).

### Wiring gaps to close

- Diameter `GxHandler` struct at `gx.go:15-21` — add
  `ipPoolStore *store.IPPoolStore` field; constructor `NewGxHandler` at
  `:23` already takes explicit deps — extend signature.
- Diameter `ServerDeps` at `server.go:26-31` — add `IPPoolStore`.
- `cmd/argus/main.go:987-996` — pass the shared `ippoolStore` into
  `aaadiameter.ServerDeps`.

RADIUS server already has `ipPoolStore`; no wiring changes needed.
SBA server (`internal/aaa/sba/server.go`) now DOES need wiring per
D3-B: add `IPPoolStore *store.IPPoolStore` and `SIMStore
*store.SIMStore` to the SBA `ServerDeps` (or equivalent constructor
input), thread them through to the new Nsmf handler, and extend
`cmd/argus/main.go`'s SBA server construction site to pass the shared
`ippoolStore` and `simStore` instances.

## Decision Points

> **LOCKED 2026-04-18 per user selection.** All three decisions
> resolved: **D1 → D1-A**, **D2 → D2-A**, **D3 → D3-B**. The planner
> recommendations and trade-off analysis for each decision are preserved
> below for audit / future-reviewer context; the `[LOCKED …]` header on
> each decision marks the outcome.

### D1. Seed fix placement (advisor flag #1) — **LOCKED 2026-04-18: D1-A**

The dispatch text says "Fix seed 003". The existing pattern lives in
seed 006. Two options:

- **D1-A (RECOMMENDED by planner — single source of truth)**: Extend
  `migrations/seed/006_reserve_sim_ips.sql` to:
  1. Iterate ALL `ip_pools` rows not yet materialised (`total_addresses
     > 0 AND NOT EXISTS (SELECT 1 FROM ip_addresses WHERE pool_id=p.id)`).
  2. For each, materialise the first N usable addresses (N =
     `LEAST(total_addresses, 254)` — the current 50-per-pool cap stays for
     the 6 already-covered pools; seed 003's 13 pools get the same 50).
  3. Keep the existing CTE reservation logic untouched — it already
     scans ALL SIMs cross-tenant.
  4. Keep the deterministic `used_addresses` recount at the end.

  Pros: one file owns all reservation logic; idempotent re-runs; seed
  003 untouched (130K-line file); no duplication.
  Cons: seed 006's filename ("reserve_sim_ips") no longer fully
  describes its scope — rename to `006_materialise_and_reserve_ips.sql`
  OR add a header comment clarifying scope.

- **D1-B (matches dispatch literal wording)**: Add a
  `BEGIN/COMMIT`-wrapped block at the bottom of
  `migrations/seed/003_comprehensive_seed.sql` that materialises
  ip_addresses rows for the 13 pools declared earlier in the file AND
  runs a reservation CTE for seed-003 SIMs only. Seed 006 continues to
  handle seed-005 SIMs independently.

  Pros: matches dispatch text verbatim; each seed is self-contained.
  Cons: duplicates the reservation CTE logic; re-running seed 003 is a
  60-second-plus operation on the 138KB file; any future pool schema
  change requires two touch points.

**Planner recommendation**: D1-A. Reason: seed 006 was purpose-built
for exactly this pattern (see file header at `006:4-12`) and the user's
observed symptom ("every IP column shows -") maps directly to seed 006
having the wrong scope. Extending it fixes the root cause cleanly. But
this is a user decision.

### D2. `ip_pools.used_addresses` counter mechanism (advisor flag #4, reframed) — **LOCKED 2026-04-18: D2-A**

> **Note on dispatch framing**: the dispatch presented this as A (DB
> trigger) vs. B (app-level) and implied neither is currently chosen.
> That framing is inaccurate — the code is ALREADY option B (app-level
> in AllocateIP/ReleaseIP). The real decision is "keep app-only" vs.
> "add trigger as a safety belt on top of app-level." Relabeled below
> as D2-A (keep) / D2-B (trigger belt) to match current code state.

Reframed from the dispatch text: current code at `ippool.go:604-605`
and `:651-652` ALREADY maintains `used_addresses` app-level inside
`AllocateIP`/`ReleaseIP`/`FinalizeReclaim`/`ReleaseGraceIP`
transactions. The real choice is:

- **D2-A (RECOMMENDED — no change)**: Leave app-level maintenance as-is.
  Seed 006's final `UPDATE ip_pools SET used_addresses = sub.used_count
  FROM (...)` recount (lines 168-176) remains the authoritative recount
  for seed-time drift. Additionally, add a maintenance SQL helper in
  this story:
  `UPDATE ip_pools SET used_addresses = (SELECT COUNT(*) FROM ip_addresses
  WHERE pool_id=ip_pools.id AND state IN ('allocated','reserved'))`.
  Expose it via a new store method `IPPoolStore.RecountUsedAddresses(ctx
  context.Context, tenantID uuid.UUID) error` and call it at the end
  of seed 006 (already done inline). Leave for future ops-CLI wiring.

  Pros: zero hot-path regression risk; matches every other STORY-061
  convention; fast.
  Cons: a future caller that INSERTs/UPDATEs `ip_addresses` outside
  AllocateIP/ReleaseIP can desync the counter. No callers do this
  today (grep-verified).

- **D2-B (trigger-based safety belt)**: Add a PostgreSQL trigger on
  `ip_addresses` INSERT/UPDATE OF state/DELETE that recomputes the
  parent pool's `used_addresses`. Requires a new migration
  `20260419000001_ip_pools_used_addresses_trigger.{up,down}.sql` plus
  REMOVAL of the app-level `UPDATE ip_pools SET used_addresses …`
  lines in `AllocateIP`/`ReleaseIP`/`FinalizeReclaim`/`ReleaseGraceIP`
  (to avoid double-counting).

  Pros: correct under any future caller; a single SQL invariant.
  Cons: adds a hot-path trigger on every allocation (measurable
  latency); removes 4 app-level updates that need careful refactoring;
  larger blast radius if the trigger is buggy.

**Planner recommendation**: D2-A (keep current mechanism + add the
recount helper as a low-risk safety valve). But this is a user decision
— if the user wants the trigger belt as a belt-and-suspenders move,
say so and we add D2-B as a separate Wave 3 task.

### D3. SBA scope (advisor flag #5) — **LOCKED 2026-04-18: D3-B**

Argus SBA currently exposes AUSF/UDM only. In a real 5G Core, UE IP
allocation is SMF responsibility per 3GPP TS 23.501 §5.6 (User Plane
function selection) and TS 23.502 §4.3.2 (PDU Session Establishment).
AUSF handles EAP/5G-AKA auth; UDM holds subscriber data. Neither
returns a UE IP.

However, Argus is a simulator / test platform, and STORY-082..STORY-085
already expose AUSF/UDM mocks that simulate operator-side behaviour.
Extending the mock surface to include a minimal `Nsmf_PDUSession`
endpoint is consistent with the existing "mock-the-operator-SoR"
stance: the endpoint implements only IP allocation on Create and IP
release on Delete, NOT real SMF behaviour (no UPF selection, no QoS
flow establishment, no PCF integration, no session modification).

- **D3-A (originally recommended — DOC-ONLY, NOT SELECTED)**: SBA IP
  binding out of scope for STORY-092. Document the 3GPP responsibility
  split in `docs/architecture/PROTOCOLS.md` and add Tech Debt entry
  D-039 for future consideration.

  Pros: matches real-5GC spec; avoids mock endpoints; zero SBA code
  change.
  Cons: 5G auth path shipped in Argus remains unable to prove
  end-to-end IP-bearing sessions — `/sessions` "IP" column stays `-`
  for every 5G session.

- **D3-B (SELECTED — mock Nsmf_PDUSession endpoint)**: Add a minimal
  mock Nsmf_PDUSession handler to `internal/aaa/sba/`
  (`POST /nsmf-pdusession/v1/sm-contexts` for Create,
  `DELETE /nsmf-pdusession/v1/sm-contexts/{smContextRef}` for Release).
  The Create handler resolves the SIM's APN pool (reusing the Wave-1
  RADIUS allocation pattern), calls `AllocateIP`, and returns a minimal
  `PduSessionCreatedData`-shaped response with `ueIpv4Address`, `supi`,
  `dnn`, and `sNssai`. The Release handler calls `ReleaseIP`. The
  STORY-084 SBA simulator client (`internal/simulator/sba/client.go` +
  `picker.go`) is extended with a Nsmf client method so the simulator's
  end-to-end flow runs AUSF auth → UDM auth-status → Nsmf
  CreateSMContext → (session lifetime) → Nsmf ReleaseSMContext. New
  Prometheus metric `simulator_sba_pdu_sessions_total{result}`.

  Pros: closes the visible runtime defect; exercises the SBA path
  end-to-end; consistent with the AUSF/UDM mock stance already shipped;
  no new protocol families introduced.
  Cons: adds ~2 new SBA handlers and simulator client methods; mock
  Nsmf surface must be strictly scope-limited to avoid drift into
  real-SMF semantics (see Risk 7).

**Outcome**: **D3-B LOCKED**. STORY-089 (Operator SoR Simulator) is the
long-term home for a richer operator-side PDU Session simulator; it is
currently blocked behind STORY-090, so shipping a minimal mock in
STORY-092 is the path that closes the runtime defect without further
blocking.

## Acceptance Criteria

Each AC is a testable condition; test tasks cover them individually.

### AC-1: RADIUS Access-Accept includes Framed-IP for previously-unassigned SIM

Preconditions: seed 003 + seed 006 applied per D1 decision; pool has ≥1
available address.

Test: RADIUS Access-Request with IMSI of a seed-003 SIM whose
`ip_address_id` was NULL before. Expected:
- Response code = Access-Accept.
- Attribute `Framed-IP-Address` (type 8) is present and parses as a valid
  IPv4 within the SIM's APN's pool CIDR.
- Post-request DB state: `sims.ip_address_id` is no longer NULL for
  that SIM. `ip_addresses.state` for that row is `'allocated'`.
  `ip_pools.used_addresses` is one higher than before.
- SIM cache: next `GetByIMSI` returns a sim with the updated
  `IPAddressID` (verify via `simCache.GetByIMSI` after first auth;
  cached value should reflect the new ID after `InvalidateIMSI` clears
  the entry).

### AC-2: RADIUS Accounting-Start session carries Framed-IP even when packet omits it (advisor flag #2)

Test: simulator-style Accounting-Request (Start) with no
Framed-IP-Address attribute in the packet, matching the
`internal/simulator/radius/` default. Expected:
`radius_sessions.framed_ip` for the new session is populated with the
SIM's current IP (from `sim.IPAddressID → ip_addresses.address_v4`).

### AC-3: RADIUS Accounting-Stop releases dynamic allocation (and ONLY dynamic)

Test 1 (dynamic): SIM received its IP via `AllocateIP` during the same
session. After Accounting-Stop: `ip_addresses.state='available'`,
`sim_id` is NULL, `allocated_at` NULL, `ip_pools.used_addresses` -1
vs. pre-stop. `sims.ip_address_id` is NULL.

Test 2 (static/seed-reserved): SIM whose `ip_addresses.allocation_type
='static'` (pre-reserved by seed 006). After Accounting-Stop: row goes
to `state='reclaiming'` with `reclaim_at = NOW() + grace_period_days`.
`sims.ip_address_id` is still set (reclaim is async via the
ip_grace_release job). `ip_pools.used_addresses` UNCHANGED.

### AC-4: Diameter Gx CCA-I includes Framed-IP-Address AVP (code 8)

Test: CCR-I from a simulated Gx client with a valid IMSI. Expected
CCA-I contains an AVP with code=8, flags=`AVPFlagMandatory`, vendorID=0
(RFC standard — not 3GPP-specific), data=4 bytes of IPv4. AVP encoded
per `NewAVPAddress` helper at `avp.go:288`.

Implementation note: set the AVP ONLY if the SIM has a non-nil
`IPAddressID` (either pre-assigned or dynamically allocated in the
same handler). If the pool is exhausted and AllocateIP returns
`ErrPoolExhausted`, the CCA-I STILL returns `ResultCodeSuccess` but
without the Framed-IP-Address AVP (matching RFC 6733 optional-AVP
semantics). Log a warning.

### AC-5: Diameter Gx CCR-T releases dynamic allocation

Test: CCR-T for a session whose sim_id has a dynamic allocation.
Expected: `ReleaseIP` is called; AC-3 Test 1 assertions hold. CCR-T
response is `ResultCodeSuccess`.

### AC-6: 5G SBA Access/Auth flow allocates and releases a UE IP end-to-end (D3-B)

Preconditions: seed fix (AC-7) applied so the target SIM's APN pool has
available addresses.

Test: A simulated 5G SBA flow runs AUSF `ue-authentications` (initiate
+ confirm) → UDM `authentication-status` → `Nsmf_PDUSession` Create
against a Argus-hosted mock SBA. Expected:

- AUSF + UDM stages complete with their existing result codes (no
  regression).
- `POST /nsmf-pdusession/v1/sm-contexts` returns `201 Created` with a
  `PduSessionCreatedData`-shaped body containing `ueIpv4Address` set to
  a routable IPv4 drawn from the SIM's APN pool, plus `supi`, `dnn`,
  and `sNssai` echoed from the request.
- Post-Create DB state: `ip_addresses.state='allocated'` for the
  returned address; `ip_addresses.sim_id` matches the SUPI's resolved
  SIM; `ip_pools.used_addresses` +1.
- `DELETE /nsmf-pdusession/v1/sm-contexts/{smContextRef}` (PDU Session
  Release) returns `204 No Content`. Post-Delete DB state (dynamic
  allocation): `ip_addresses.state='available'`, `sim_id` NULL,
  `ip_pools.used_addresses` -1 (back to pre-Create).
- Prometheus metric `simulator_sba_pdu_sessions_total{result="ok"}`
  increments by 1 for each successful Create.

Out-of-scope for this AC: session modification (`PATCH`), N2/N4 UPF
interaction, QoS flow descriptors, PCF integration. See Risk 7.

### AC-7: Seed bootstrap (per D1 decision)

Post `make db-seed`:
- `SELECT COUNT(*) FROM sims WHERE state='active' AND apn_id IS NOT NULL
  AND ip_address_id IS NULL` returns 0 (every active SIM with an APN
  has an IP).
- `SELECT COUNT(*) FROM ip_addresses` returns ≥ N*K where K is the
  per-pool cap (50) and N is the number of active pools.
- For every pool: `ip_pools.used_addresses = (SELECT COUNT(*) FROM
  ip_addresses WHERE pool_id = p.id AND state IN ('allocated','reserved'))`.
  Zero drift.

### AC-8: `used_addresses` counter is consistent after N allocations + N releases (advisor flag #4)

Test: pick a pool, record initial `used_addresses`. Run N=100 cycles
of AllocateIP → ReleaseIP against 100 disposable synthetic sim_ids.
Final `used_addresses` equals initial `used_addresses`. Zero race
detected (the `FOR UPDATE` locks in AllocateIP/ReleaseIP already
serialize; this test simply proves the app-level counter math).

### AC-9: Nil-cache Enforcer integration regression (advisor hard flag #1, D-038)

Test (Go integration test under `internal/aaa/radius/`):
1. Construct `enforcer.New(nil, policyStore, violationStore, nil, nil,
   logger)` — matching `cmd/argus/main.go:1055-1062` literal nil.
2. Seed a test DB with one tenant, one operator, one APN with pool,
   one active SIM with `policy_version_id` pointing at an active
   policy version whose compiled rules emit `Allow=true`.
3. Call RADIUS Access-Request end-to-end via
   `aaaradius.Server.handleDirectAuth` (either via the actual radius
   packet server or a direct method call with a fake
   `radius.ResponseWriter`).
4. Assert: response code = Access-Accept. Framed-IP-Address present.
   Enforcer successfully evaluated via DB fall-through (not cache).
   `ip_pools.used_addresses` incremented by 1.

This closes D-038 at integration level (the existing unit tests in
`enforcer_test.go` already cover the nil-guard in isolation).

### AC-10: Baseline test suite remains green

`go test ./...` result ≥ 3000 PASS, 0 FAIL, no new regressions.
`go vet ./...` exit 0 (should remain clean post-STORY-088).

## Architecture references

### RADIUS (RFC 2865)

§5.8 Framed-IP-Address. Type = 8. Length = 6. Value = 4 octets of
IPv4 address. Already handled verbatim by
`layeh.com/radius/rfc2865.FramedIPAddress_Set`. No new deps.

### Diameter Gx (3GPP TS 29.212)

§5.3.1 Framed-IP-Address AVP (code 8, inherited from RFC 7155 NASREQ
§4.4.10.5.1). Type = Address. Length = 4 bytes for IPv4. Flags
`M=1, V=0` (vendor 0 — RFC standard), `P=0`. Add `AVPCodeFramedIPAddress
= 8` to `internal/aaa/diameter/avp.go` constants. Use existing
`NewAVPAddress(code, flags, 0, [4]byte{...})` helper.

### 3GPP TS 23.501 §5.6 + TS 23.502 §4.3.2 (SBA / SMF IP allocation)

SMF is the Session Management Function. During PDU Session
Establishment (§4.3.2.2), the SMF optionally selects a UPF and obtains
the UE IP address from its IP address pool (via IP Pool Allocation in
DHCP/static/RADIUS/Diameter mode). AUSF (Access and Mobility Function
**auth**) and UDM (Unified Data Management) are NOT involved in IP
assignment in a real 5GC. Argus diverges from this in simulator mode
by exposing a minimal Nsmf mock (per D3-B); see Routes below for the
exact URL shapes.

### 3GPP TS 29.502 (Nsmf_PDUSession service — mock subset)

The new mock endpoints follow the existing SBA versioned-prefix pattern
(`/nausf-auth/v1`, `/nudm-ueau/v1`). Per TS 29.502:

- **`POST /nsmf-pdusession/v1/sm-contexts`** — PDU Session Create
  (a.k.a. CreateSMContext). Argus mock accepts a request body with
  (minimal subset) `supi`, `dnn`, `sNssai{sst,sd}`, `pduSessionId`,
  `servingNetwork`, `anType`, `ratType`; responds `201 Created` with
  `Location: /nsmf-pdusession/v1/sm-contexts/{smContextRef}` and a
  `PduSessionCreatedData`-shaped JSON body containing minimally
  `ueIpv4Address`, `dnn`, `sNssai`, and `supi`. All other fields are
  OMITTED (the simulator does not read them; adding them now invites
  scope creep).
- **`DELETE /nsmf-pdusession/v1/sm-contexts/{smContextRef}`** — PDU
  Session Release (a.k.a. ReleaseSMContext). Argus mock looks up the
  smContextRef (UUID generated on Create, stored in-memory), calls
  `ReleaseIP` for dynamic allocations, and responds `204 No Content`.

Error shape: 3GPP-native `ProblemDetails` (RFC 7807) with `type`,
`title`, `status`, `detail`. Do NOT wrap in the Argus `{status, data,
error}` envelope — SBA responses are protocol-native per the existing
AUSF/UDM precedent.

### STORY-086 / STORY-087 precedents

`migrations/seed/006_reserve_sim_ips.sql` (STORY-082 follow-up) is the
live pattern for materialising ip_addresses from a pool CIDR via
`generate_series` + `WHERE NOT EXISTS` idempotency + CTE reservation.
D1-A extends this pattern; D1-B introduces a parallel pattern in a
different file. STORY-086's `check_sim_exists` trigger (installed at
`20260417000004_sms_outbound_recover.up.sql:49-74`) is the precedent
for FK-to-partitioned-parent integrity — unrelated here but cited for
authors who may wonder why ip_addresses.sim_id has no FK on sims (it
follows the same pattern — trigger, not constraint).

## Tasks

Dependency-ordered. Each task touches ≤3 files. Wave breakdown reflects
advisor flag #7: Wave 1 = seed fix + nil-cache enforcer integration
test + RADIUS happy path; Wave 2 = RADIUS release + Acct-Start
fallback + Diameter; Wave 3 = SBA docs + UI/telemetry verification.

### Wave 1 — foundation

#### Task 0: Seed fix per D1 decision

- **What**: Per D1 decision (A or B), update seed 006 (or seed 003) to
  materialise ip_addresses rows for ALL ip_pools with total_addresses > 0
  and no existing ip_addresses rows, then reserve one per active SIM
  matching the existing CTE pattern at `006:119-161`. Update file
  header comment to reflect scope extension. Add a `SELECT ... INTO
  TEMP TABLE` verification count at the end so a failed reservation is
  loud (fail-fast `RAISE EXCEPTION` if count mismatch).
- **Files**:
  - MOD `migrations/seed/006_reserve_sim_ips.sql` (D1-A)
    OR MOD `migrations/seed/003_comprehensive_seed.sql` (D1-B)
- **Pattern ref**: Read the existing CTE block at
  `migrations/seed/006_reserve_sim_ips.sql:119-161` — extend, don't
  duplicate.
- **Context refs**: Current deployed reality > Seed state; Decision
  Points > D1.
- **Verify**: `make db-seed` re-run is idempotent (second run reports 0
  row changes for ip_addresses INSERT and 0 UPDATE for sims.ip_address_id).
  `SELECT COUNT(*) FROM sims WHERE state='active' AND apn_id IS NOT NULL
  AND ip_address_id IS NULL` returns 0.
- **Complexity**: medium.
- **Depends on**: —

#### Task 1: Add `IPPoolStore.RecountUsedAddresses` + unit test (D2-A supporting helper)

- **What**: Add a new method to `internal/store/ippool.go` that
  deterministically recomputes `ip_pools.used_addresses` for a single
  tenant (or ALL pools if tenantID is uuid.Nil). Implementation:
  `UPDATE ip_pools p SET used_addresses = sub.cnt FROM (SELECT pool_id,
  COUNT(*) AS cnt FROM ip_addresses WHERE state IN ('allocated',
  'reserved') GROUP BY pool_id) sub WHERE p.id = sub.pool_id AND ($1 =
  uuid_nil OR p.tenant_id = $1)`. Add 3 unit tests: (a) recount fixes
  app-level drift; (b) handles empty pool (used_addresses → 0); (c)
  tenant scoping works.
- **Files**:
  - MOD `internal/store/ippool.go`
  - MOD `internal/store/ippool_test.go`
- **Pattern ref**: Read existing `IPPoolStore.TenantPoolUsage` at
  `ippool.go:45-59` for tenant-scoped query style; existing
  `ippool_test.go` for test harness.
- **Context refs**: Decision Points > D2.
- **Verify**: `go test ./internal/store -run TestIPPoolStore_RecountUsedAddresses`.
- **Complexity**: low.
- **Depends on**: — (new method, additive)

#### Task 2: RADIUS Access-Accept dynamic allocation (both EAP + direct paths)

- **What**: In `sendEAPAccept` (lines 343-447) and `handleDirectAuth`
  (lines 458-593) of `internal/aaa/radius/server.go`, AFTER the
  `!policyResult.Allow` early-return branch and BEFORE the
  `rfc2865.FramedIPAddress_Set(...)` call:
  1. If `sim.IPAddressID != nil` — skip allocation (existing behavior).
  2. Else if `sim.APNID == nil` — skip (can't pick a pool).
  3. Else: `pools, _, err := s.ipPoolStore.List(ctx, sim.TenantID, "",
     1, sim.APNID)`. If len==0: log debug, skip (no pool for this APN —
     not an error).
  4. Else: `allocated, err := s.ipPoolStore.AllocateIP(ctx, pools[0].ID,
     sim.ID)`. On `ErrPoolExhausted`: log warning, skip (send
     Access-Accept without Framed-IP; RFC allows it).
  5. On success: `s.simStore.SetIPAndPolicy(ctx, sim.ID, &allocated.ID,
     nil)` to persist. Then `s.simCache.InvalidateIMSI(ctx, imsi)` for
     cache coherence. Locally update `sim.IPAddressID = &allocated.ID`
     so the existing `GetIPAddressByID` lookup below finds it.
  6. Existing Framed-IP attach block continues to run unchanged.
  Requires extending `Server` struct to hold `*store.SIMStore` — add
  `simStore *store.SIMStore` field and plumb via `NewServer` signature
  or a new `SetSIMStore(s *store.SIMStore)` setter (prefer the setter,
  matching `SetPolicyEnforcer` at line 1068 of main.go precedent).
- **Files**:
  - MOD `internal/aaa/radius/server.go`
  - MOD `cmd/argus/main.go` (new `radiusServer.SetSIMStore(simStore)` call)
- **Pattern ref**: Read `SetPolicyEnforcer`/`SetMetricsRecorder` in
  `server.go` for setter pattern; read existing `AllocateIP` caller in
  `internal/api/sim/handler.go:874-886` for err handling shape.
- **Context refs**: Architecture Context > Data flow; Architecture
  Context > Current deployed reality > RADIUS auth path.
- **Verify**: new Go test `TestRADIUSAccessAccept_DynamicAllocation` in
  `internal/aaa/radius/server_test.go` covering the happy path; also
  covers AC-1.
- **Complexity**: high (touches hot path, both EAP and direct auth
  flows, careful ordering vs. policy, cache invalidation, struct
  extension).
- **Depends on**: Task 0 (needs pools to have available addresses).

#### Task 3: Nil-cache Enforcer integration test (AC-9, D-038 closure)

- **What**: New Go integration test under `internal/aaa/radius/` (or
  `internal/policy/enforcer/` — developer judgment) that:
  1. Skips if `DATABASE_URL` env var unset (existing idiom — see
     `internal/store/sms_outbound_test.go:144`).
  2. Creates a disposable test database, runs `argus migrate up`, seeds
     a minimal fixture (1 tenant + 1 operator + 1 operator partition +
     1 APN + 1 pool with 5 ip_addresses + 1 SIM with
     `policy_version_id` pointing at an active version whose
     compiled_rules JSON is `{"rules":[{"when":{},"then":{"action":"allow"}}]}` —
     i.e., unconditionally Allow).
  3. Instantiates `enforcer.New(nil, policyStore, violationStore, nil,
     nil, logger)` — matching main.go literal nil.
  4. Constructs a real `*aaaradius.Server` with ipPoolStore, simStore,
     simCache (in-memory redis mock or nil-redis), attaches the
     enforcer via `SetPolicyEnforcer`.
  5. Runs RADIUS Access-Request (either via an in-proc
     `radius.PacketServer` dialed over loopback, or a direct method
     call to `handleDirectAuth` with a stub ResponseWriter — prefer the
     latter for test speed).
  6. Asserts: response code = Access-Accept; Framed-IP-Address attached;
     `ip_addresses.state='allocated'` for the SIM's new IP;
     `ip_pools.used_addresses=1` (from 0); enforcer Evaluate returned
     Allow=true via the DB fall-through path (captured via log
     assertion or by observing `policyStore.GetVersionByID` was called —
     not via cache).
- **Files**:
  - NEW `internal/aaa/radius/enforcer_nilcache_integration_test.go`
- **Pattern ref**: Read `internal/store/migration_freshvol_test.go`
  (STORY-087, shipped 2026-04-17) for disposable-DB pattern, and
  `internal/aaa/radius/server_test.go` for radius test stubs.
- **Context refs**: Architecture Context > Enforcer (D-038);
  Acceptance Criteria > AC-9.
- **Verify**: `go test ./internal/aaa/radius -run
  TestEnforcerNilCacheIntegration_STORY092`. Passes in both nil-cache
  and (manually toggled) non-nil-cache modes.
- **Complexity**: high (integration test with disposable DB, real
  enforcer, real radius server).
- **Depends on**: Task 2 (tests the full allocation happy path).

### Wave 2 — release + Diameter

#### Task 4: RADIUS Accounting-Stop release + Acct-Start framed_ip fallback

- **What**:
  1. **Acct-Start fallback (advisor flag #2, bug B)**: In
     `handleAcctStart` at `server.go:637-746`, after the existing
     `framedIP, _ := rfc2865.FramedIPAddress_Lookup(...)` block at
     :683-686, add: if `framedIP == "" && sim.IPAddressID != nil`,
     fetch `ip_addresses` row, parse the v4 address, assign to local
     `framedIP` so the `session.FramedIP = framedIP` at :704 is
     populated.
  2. **Acct-Stop release**: In `handleAcctStop` at `:832-875`, BEFORE
     the final `Info().Msg("session stopped")` log:
     - Fetch `sim` via `simCache.GetByIMSI(ctx, imsi)`. If err, log
       warning and skip release (session termination already happened).
     - If `sim.IPAddressID == nil` — skip.
     - Fetch `ip_addresses` row via `GetIPAddressByID`. If
       `allocation_type != 'dynamic'` — skip (static allocations are
       preserved; reclaim handled elsewhere).
     - Call `ipPoolStore.ReleaseIP(ctx, ipAddr.PoolID, sim.ID)`. Log
       warn on error, continue.
     - Call `simStore.SetIPAndPolicy(ctx, sim.ID,
       <nil_uuid_pointer_that_clears>, nil)` — note:
       `SetIPAndPolicy` current signature nullifies ONLY when the
       pointer is non-nil for the field; clearing requires a new
       store method OR adding a `ClearIP(simID)` method. **Add
       `SIMStore.ClearIPAddress(ctx, simID)`** as a new, minimal store
       method (UPDATE sims SET ip_address_id = NULL WHERE id = $1).
     - `simCache.InvalidateIMSI(ctx, imsi)`.
- **Files**:
  - MOD `internal/aaa/radius/server.go`
  - MOD `internal/store/sim.go` (add `ClearIPAddress`)
  - MOD `internal/aaa/radius/server_test.go` (tests for AC-2 + AC-3)
- **Pattern ref**: Read `ReleaseIP` at `ippool.go:617-667` for call
  shape; `SetIPAndPolicy` at `sim.go:791-817` for new method pattern.
- **Context refs**: Architecture Context > Data flow; AC-2, AC-3.
- **Verify**: `go test ./internal/aaa/radius -run
  TestRADIUSAccountingStop_ReleasesDynamic`; `TestRADIUSAccountingStart_FallbackFramedIP`.
- **Complexity**: medium.
- **Depends on**: Task 2 (dynamic allocation must exist to release).

#### Task 5a: Diameter Framed-IP AVP + dependency wiring

- **What**:
  1. Add `AVPCodeFramedIPAddress uint32 = 8` to
     `internal/aaa/diameter/avp.go` constants block near line 20
     (after `AVPCodeHostIPAddress`). No new helper — the existing
     `NewAVPAddress(code, flags, vendorID, ip [4]byte)` at `avp.go:288`
     already supports this AVP.
  2. Extend `ServerDeps` in `internal/aaa/diameter/server.go:26-31`
     with `IPPoolStore *store.IPPoolStore` and `SIMStore
     *store.SIMStore` fields. Plumb into the handler constructor call
     site within the package (update the `NewGxHandler` invocation in
     `server.go` to pass the new deps).
  3. Update `cmd/argus/main.go:987-996` to pass `ippoolStore` and
     `simStore` into `aaadiameter.ServerDeps{...}`.
- **Files**:
  - MOD `internal/aaa/diameter/avp.go`
  - MOD `internal/aaa/diameter/server.go`
  - MOD `cmd/argus/main.go`
- **Pattern ref**: Existing `NewAVPAddress` usage at
  `internal/aaa/diameter/diameter_test.go:811` for call shape; existing
  `ServerDeps` plumbing in `cmd/argus/main.go:987-996` for wiring style.
- **Context refs**: Architecture Context > Diameter Gx; Architecture
  Context > Wiring gaps.
- **Verify**: `go build ./...` green; `internal/aaa/diameter` compiles
  with the new struct field even without any logic consuming it yet.
- **Complexity**: low (pure wiring, no behaviour change).
- **Depends on**: —

#### Task 5b: Diameter Gx CCA-I Framed-IP + CCR-T release logic

- **What**:
  1. Extend `GxHandler` struct at `gx.go:15-21` with `ipPoolStore
     *store.IPPoolStore` and `simStore *store.SIMStore`. Update
     `NewGxHandler` signature to accept them.
  2. Add a package-private helper `parseV4AddressForAVP(s string)
     [4]byte` in `gx.go` (copy the strip-CIDR logic from
     `internal/aaa/radius/server.go:79-84` — do NOT shared-package-ify
     in this story, keep the copy localised per YAGNI).
  3. In `handleInitial` at `gx.go:69-163`, AFTER the
     `simResolver.GetByIMSI` block at :87-110 and BEFORE the CCA-I
     build at :146, add the allocation pattern from Task 2 (pool list
     → AllocateIP → SetIPAndPolicy → InvalidateIMSI on the shared
     radius simCache, for consistency across protocols). Respect
     advisor flag #6 — allocation happens AFTER the SIM is confirmed
     active/allowed.
  4. In the CCA-I build block at `:146-162`, after
     `BuildChargingRuleInstall(...)` at :154, add:
     ```
     if sim.IPAddressID != nil {
         ipAddr, err := h.ipPoolStore.GetIPAddressByID(ctx, *sim.IPAddressID)
         if err == nil && ipAddr.AddressV4 != nil {
             ip4 := parseV4AddressForAVP(*ipAddr.AddressV4)
             cca.AddAVP(NewAVPAddress(AVPCodeFramedIPAddress, AVPFlagMandatory, 0, ip4))
         }
     }
     ```
     vendorID=0 per RFC 7155 NASREQ §4.4.10.5.1. Flags: `M=1, V=0, P=0`.
  5. In `handleTermination` at `:211-265`, after
     `sessionMgr.Terminate(...)` at :233, add the release logic from
     Task 4 (ReleaseIP if `allocation_type='dynamic'`, ClearIPAddress,
     cache invalidate).
  6. Add `TestGxCCAInitial_FramedIPAddress` (asserts AVP code=8,
     vendor=0, M flag set, payload = 4-byte IPv4 matching the
     pool-allocated address) and `TestGxCCRTermination_ReleasesDynamic`
     (asserts `ip_addresses.state='available'` post-CCR-T for dynamic,
     stays `state='reclaiming'` for static).
- **Files**:
  - MOD `internal/aaa/diameter/gx.go`
  - MOD `internal/aaa/diameter/diameter_test.go`
- **Pattern ref**: Task 2 implementation for the RADIUS allocation
  block — lift structure and adapt for Diameter types; Task 4 for the
  release block.
- **Context refs**: Architecture Context > Diameter Gx; AC-4, AC-5;
  Architecture references > Diameter Gx; Risks > Risk 6 (vendorID=0).
- **Verify**: `go test ./internal/aaa/diameter -run
  TestGxCCAInitial_FramedIPAddress`; `TestGxCCRTermination_ReleasesDynamic`.
- **Complexity**: high (hot-path logic across two handlers; AVP
  encoding correctness; cross-protocol consistency with RADIUS cache
  invalidation; AVP vendorID subtlety).
- **Depends on**: Task 5a, Task 2, Task 4.

#### Task 6: AllocateIP/ReleaseIP roundtrip property test (AC-8)

- **What**: Add a Go integration test that runs N=100 cycles of
  AllocateIP→ReleaseIP against a single pool on disposable test DB.
  Asserts `ip_pools.used_addresses` returns to initial value after the
  cycles complete, regardless of concurrency (spawn 10 goroutines doing
  10 cycles each; use `SELECT …FOR UPDATE SKIP LOCKED` contention to
  stress the serialization).
- **Files**:
  - NEW `internal/store/ippool_allocation_cycle_test.go` (or extend
    `internal/store/ippool_test.go` — developer judgment)
- **Pattern ref**: `migration_freshvol_test.go` for disposable-DB
  harness; existing `ippool_test.go` for basic AllocateIP unit tests.
- **Context refs**: AC-8; Decision Points > D2.
- **Verify**: `go test ./internal/store -run
  TestIPPoolStore_AllocateReleaseCycle`.
- **Complexity**: medium.
- **Depends on**: — (independent of other Wave-1/2 tasks).

### Wave 3 — SBA Nsmf mock + simulator integration + UI/telemetry verification

#### Task 7a: Mock Nsmf_PDUSession handler (Create + Release)

- **What**: Add a new file `internal/aaa/sba/nsmf.go` defining minimal
  mock handlers for PDU Session Create and Release per TS 29.502 shape
  (see Architecture references > TS 29.502). Specifically:
  1. **`CreateSMContext(w http.ResponseWriter, r *http.Request)`**:
     - Decode JSON body into a local struct with minimal fields:
       `supi`, `dnn`, `sNssai{sst,sd}`, `pduSessionId`, `servingNetwork`,
       `anType`, `ratType`.
     - Resolve SIM from SUPI (strip `imsi-` prefix per TS 23.003, pass
       remainder to `simResolver.GetByIMSI`). If not found, return
       ProblemDetails 404 `USER_NOT_FOUND`.
     - Advisor flag #6 (mirror RADIUS/Gx): allocation happens only
       after SIM is confirmed active. If `sim.State != active`, return
       ProblemDetails 403 `USER_UNKNOWN`. No enforcer call here —
       enforcer gating is at the AUSF/UDM stage; if those passed, Nsmf
       proceeds. Document the gap in the file header (future story may
       revisit).
     - Resolve APN pool via `ipPoolStore.List(ctx, sim.TenantID, "", 1,
       sim.APNID)`. If no pool or pool exhausted, ProblemDetails 503
       `INSUFFICIENT_RESOURCES` — DO NOT allocate.
     - Else: `allocated := ipPoolStore.AllocateIP(ctx, pools[0].ID,
       sim.ID)`; persist via `simStore.SetIPAndPolicy(ctx, sim.ID,
       &allocated.ID, nil)`; cache-invalidate via
       `simCache.InvalidateIMSI`.
     - Generate `smContextRef := uuid.New().String()`; store
       `{smContextRef → (poolID, simID, allocatedID)}` in an in-memory
       `sync.Map` on the handler struct (no DB persistence — mock
       scope; explicit comment).
     - Respond `201 Created` with `Location:
       /nsmf-pdusession/v1/sm-contexts/{smContextRef}` and JSON body
       `{"dnn": …, "sNssai": {…}, "supi": …, "ueIpv4Address": "…"}`.
  2. **`ReleaseSMContext(w http.ResponseWriter, r *http.Request)`**:
     - Parse `smContextRef` from URL path.
     - Look up in the in-memory map. If missing, 404 ProblemDetails.
     - If `allocation_type='dynamic'`: `ipPoolStore.ReleaseIP(ctx,
       poolID, simID)`; `simStore.ClearIPAddress(ctx, simID)`;
       `simCache.InvalidateIMSI(ctx, imsi)`; delete from in-memory
       map.
     - If `allocation_type='static'`: do NOT release (mirror RADIUS
       AC-3 Test 2 semantics). Remove from in-memory map.
     - Respond `204 No Content`.
  3. Register routes in `internal/aaa/sba/server.go` in the existing
     route-mount block at `:75-111` alongside AUSF/UDM/NRF — pattern:
     `r.Post("/nsmf-pdusession/v1/sm-contexts", …)`;
     `r.Delete("/nsmf-pdusession/v1/sm-contexts/{smContextRef}", …)`.
  4. Extend SBA `ServerDeps` (or equivalent) with `IPPoolStore`,
     `SIMStore`, and the radius `SIMCache` for cross-protocol cache
     coherence (same cache instance threaded into RADIUS per STORY-089
     precedent). Plumb via `cmd/argus/main.go`.
- **Files**:
  - NEW `internal/aaa/sba/nsmf.go`
  - MOD `internal/aaa/sba/server.go` (route mount + ServerDeps extension)
  - MOD `cmd/argus/main.go` (pass IPPoolStore + SIMStore + SIMCache
    into SBA server construction)
- **Pattern ref**: Read existing AUSF/UDM handlers in
  `internal/aaa/sba/ausf.go` and `internal/aaa/sba/udm.go` for
  handler/request-decode/ProblemDetails style. Lift the
  allocation/release block from Task 2/Task 4 (RADIUS) verbatim
  structurally — minor adaptation for SBA context.
- **Context refs**: Decision Points > D3 (D3-B); AC-6; Architecture
  Context > 5G SBA; Risks > Risk 7.
- **Verify**: new Go test `TestSBANsmfCreateSMContext_AllocatesIP` in
  `internal/aaa/sba/nsmf_test.go` covering success + pool-exhausted +
  user-not-found paths.
- **Complexity**: high (new handler file + in-memory state map + cross-
  protocol cache wiring + ProblemDetails error shape).
- **Depends on**: Task 0 (pools have available addresses), Task 4
  (needs `SIMStore.ClearIPAddress` added there).

#### Task 7b: Extend SBA simulator client with Nsmf Create/Release

- **What**: Extend `internal/simulator/sba/client.go` and
  `internal/simulator/sba/picker.go` to call the new Nsmf endpoint
  after the existing AUSF + UDM flow:
  1. Inspect `client.go` / `picker.go` / `doc.go` to find the current
     endpoint table and method naming convention (planner observed the
     file layout at `internal/simulator/sba/{ausf,udm,client,picker}.go`
     plus `integration_test.go`; confirm shape before adding).
  2. Add `Client.CreatePDUSession(ctx, supi, dnn, sNssai) (smContextRef
     string, ueIpv4 string, err error)` and
     `Client.ReleasePDUSession(ctx, smContextRef) error`. Match the
     existing error sentinels (`ErrAuthFailed`, `ErrServerError`,
     etc.) — add a new `ErrPDUSessionFailed` only if none fit.
  3. Extend picker if there is a per-operator endpoint list it governs;
     add `NsmfPath = "/nsmf-pdusession/v1"`.
  4. Wire into the simulator's session lifecycle in
     `internal/simulator/engine/engine.go` (or the equivalent flow
     driver): after AUSF `ue-authentications` success and UDM
     `authentication-status` success, call `CreatePDUSession`. On
     session end (simulator-defined TTL), call `ReleasePDUSession`. If
     the engine currently abandons a session without calling a
     "release" hook for AUSF/UDM, add the release call symmetrically
     (do not regress existing behaviour — gate behind a feature check
     if necessary).
  5. Add Prometheus counter
     `simulator_sba_pdu_sessions_total{operator,result}` to
     `internal/simulator/metrics/metrics.go` with labels
     `result∈{ok,pool_exhausted,user_not_found,transport_error,timeout}`.
     Wire increments inside `CreatePDUSession` / `ReleasePDUSession`
     and in the engine's session lifecycle.
- **Files**:
  - MOD `internal/simulator/sba/client.go`
  - MOD `internal/simulator/sba/picker.go` (if endpoint table present)
  - MOD `internal/simulator/engine/engine.go`
  - MOD `internal/simulator/metrics/metrics.go`
- **Pattern ref**: Read `internal/simulator/sba/client.go:Authenticate*`
  (whichever method implements AUSF ue-authentications) for HTTP
  request-response style and error-classification precedent.
- **Context refs**: Decision Points > D3 (D3-B); AC-6.
- **Verify**: `go build ./...` green. `go test
  ./internal/simulator/sba -run TestClient_CreatePDUSession` +
  `TestClient_ReleasePDUSession` passes against a httptest server.
- **Complexity**: medium (new client methods + engine wiring +
  metrics).
- **Depends on**: Task 7a (needs the server endpoint to test against).

#### Task 7c: SBA integration test — full AUSF+UDM+Nsmf flow

- **What**: Add an integration test
  `internal/aaa/sba/nsmf_integration_test.go` that:
  1. Skips if DATABASE_URL unset (matching the Task 3 / STORY-087
     disposable-DB idiom).
  2. Spins up a disposable DB with minimal fixture (1 tenant, 1 APN, 1
     pool with 5 ip_addresses, 1 active SIM with `policy_version_id`
     pointing at an allow-all policy).
  3. Stands up a real `aaasba.Server` (AUSF + UDM + Nsmf all mounted)
     with the real IPPoolStore, SIMStore, and a nil SIMCache (or
     mock).
  4. Drives the sequence: AUSF `ue-authentications` (initiate +
     confirm) → UDM `authentication-status` → Nsmf
     CreateSMContext → assertions → Nsmf ReleaseSMContext.
  5. Asserts: CreateSMContext returned 201, body has non-empty
     `ueIpv4Address`; the returned IP appears in `ip_addresses` with
     `state='allocated'` and `sim_id` set; `ip_pools.used_addresses`
     is 1. Post-Release: `ip_addresses.state='available'`, sim_id
     NULL, `used_addresses` back to 0.
- **Files**:
  - NEW `internal/aaa/sba/nsmf_integration_test.go`
- **Pattern ref**: `internal/store/migration_freshvol_test.go` for
  disposable-DB harness; existing
  `internal/aaa/sba/integration_test.go` (STORY-084 + STORY-085) for
  full-SBA-flow test style and fixture setup.
- **Context refs**: Decision Points > D3 (D3-B); AC-6; Tasks 7a, 7b.
- **Verify**: `go test ./internal/aaa/sba -run
  TestSBAFullFlow_NsmfAllocates`.
- **Complexity**: high (integration test, disposable DB, three SBA
  stages).
- **Depends on**: Task 7a, Task 7b.

#### Task 8: UI/telemetry verification

- **What**: Smoke-verify the frontend surfaces the allocated IP without
  any frontend change (the DTO fields `framed_ip` and `ip_address` are
  already consumed at `web/src/pages/sessions/index.tsx:142, 249, 502`
  and `detail.tsx:230`).
  1. Post `make up` + `make db-seed`, navigate to `/sessions` — every
     active session row shows a populated IP column (not `-`).
  2. Navigate to `/ippools` — a seed-003 pool's
     `used_addresses/total_addresses` reflects the recount (non-zero,
     matching the seed-time reservation count).
  3. Run the simulator (`make sim-up` if available) for 60 seconds —
     `ip_pools.used_addresses` fluctuates as simulated sessions
     allocate/release; Prometheus metric (if any) tracks the same.
  4. Capture 4 screenshots: /sessions list, /sessions detail, /ippools
     list, /ippools detail. Save under
     `docs/stories/test-infra/STORY-092-evidence/` for gate evidence.
- **Files**:
  - NEW `docs/stories/test-infra/STORY-092-evidence/*.png` (4 PNGs)
- **Pattern ref**: STORY-087 gate evidence layout.
- **Context refs**: AC-1 UI-visible verification.
- **Verify**: developer runs the steps manually; screenshots attached.
- **Complexity**: low (manual verification).
- **Depends on**: Tasks 0, 2, 4, 5a/5b, 7a/7b/7c complete.

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| AC-1 (RADIUS Accept Framed-IP) | Task 2 | Task 3 (integration), `TestRADIUSAccessAccept_DynamicAllocation` |
| AC-2 (Acct-Start fallback) | Task 4 (part 1) | `TestRADIUSAccountingStart_FallbackFramedIP` |
| AC-3 (Acct-Stop release, dynamic-only) | Task 4 (part 2) | `TestRADIUSAccountingStop_ReleasesDynamic` + static-preserve variant |
| AC-4 (Diameter Gx CCA-I Framed-IP) | Tasks 5a + 5b | `TestGxCCAInitial_FramedIPAddress` |
| AC-5 (Diameter Gx CCR-T release) | Task 5b | `TestGxCCRTermination_ReleasesDynamic` |
| AC-6 (5G SBA Nsmf end-to-end) | Tasks 7a + 7b + 7c | `TestSBANsmfCreateSMContext_AllocatesIP`, `TestSBAFullFlow_NsmfAllocates` |
| AC-7 (Seed materialisation) | Task 0 | Post-seed counts query |
| AC-8 (counter consistency) | Tasks 2, 4, 6 | Task 6 cycle test |
| AC-9 (nil-cache enforcer integration) | Task 3 | `TestEnforcerNilCacheIntegration_STORY092` |
| AC-10 (baseline green) | all | `go test ./...` in gate |

## Test Strategy

- **Unit tests**: Tasks 1, 2, 4, 5a/5b, 7a, 7b each add or extend a
  `_test.go` file covering the new behaviour against mocked stores /
  httptest servers where possible. Target: **+17 unit test cases**
  total across `internal/aaa/radius`, `internal/aaa/diameter`,
  `internal/aaa/sba`, `internal/simulator/sba`, `internal/store`
  (bumped from the pre-D3-B estimate of +12 to reflect the three new
  Wave-3 subtasks).
- **Integration tests** (disposable-DB, DATABASE_URL-skipped):
  - Task 3 — nil-cache enforcer full-pipeline test (CORE for D-038).
  - Task 6 — allocation/release cycle stress test.
  - Task 7c — SBA full-flow AUSF+UDM+Nsmf integration test (new under
    D3-B).
- **Smoke / manual** (Task 8): `make up` + `make db-seed` + UI check
  + simulator run (with the Nsmf-extended simulator flow from Task
  7b). Screenshots captured.
- **Regression guard**: baseline `go test ./...` must remain ≥ 3000
  PASS; no new package SKIPs beyond the DATABASE_URL-gated set.

## Rollback Plan

- Task 0: seed changes are idempotent — re-running the PREVIOUS version
  of seed 006 restores the prior state (seed-005 SIMs reserved, seed-003
  SIMs not). No migration involved. For a full undo:
  `UPDATE sims SET ip_address_id = NULL WHERE tenant_id IN (…)` and
  `UPDATE ip_addresses SET state='available', sim_id=NULL, allocated_at=
  NULL WHERE sim_id IN (…)`.
- Tasks 2, 3, 4, 5a, 5b: pure Go code changes, revertible via `git
  revert`. No DB migration touched.
- Task 6: new test, no production impact.
- Tasks 7a / 7b / 7c (D3-B): all pure new code — new SBA handler file,
  new simulator client methods, new integration test. **Rollback is a
  pure `git revert`: no migration, no schema change, no data
  movement.** The in-memory `smContextRef → allocation` map is
  discarded on process restart; the underlying
  `ip_addresses`/`sims.ip_address_id` state is reclaimed by the
  existing ip_grace_release background job per normal dynamic-release
  semantics.
- Task 8: manual verification only; no rollback needed.
- D2-A (LOCKED) uses the pre-existing app-level counter mechanism — no
  migration touched by this story. (D2-B was NOT selected; trigger
  migration rollback paragraph removed.)

## Wave plan

### Wave 1 (parallel-safe): Tasks 0, 1, and sequentially Task 2 → Task 3

- Task 0 runs first (seed fix) — unblocks all integration tests that
  need data.
- Task 1 independently (new store method + unit test).
- Task 2 after Task 0 (needs available IPs in pools to AllocateIP
  against).
- Task 3 after Task 2 (integration test covers the full Task 2 path).

### Wave 2: Task 4 → Task 5b, Tasks 5a + 6 in parallel

- Task 4 after Wave 1 (release depends on allocation).
- Task 5a (Diameter wiring) can run in parallel with Task 4 (no shared
  files; pure plumbing).
- Task 5b after BOTH Task 4 and Task 5a (lifts the RADIUS
  implementation pattern; needs the wired deps).
- Task 6 runs in parallel with Tasks 4 / 5a / 5b (independent — hits
  `internal/store` only).

### Wave 3: Tasks 7a → 7b → 7c, then Task 8

Wave 3 scope grew materially under D3-B: instead of a doc-only task,
it is now SBA Nsmf mock + simulator integration + integration test +
UI/telemetry verify. Sequencing:

- **Task 7a** (mock handler, new file + wiring) runs first — it
  depends on Task 4's `SIMStore.ClearIPAddress` helper, so it must
  wait for Wave 2 to complete.
- **Task 7b** (simulator client extension) runs after Task 7a — the
  simulator needs the server endpoint to exist before tests will
  drive it.
- **Task 7c** (SBA full-flow integration test) runs after Task 7b —
  it asserts the wiring from both 7a and 7b.
- **Task 8** (UI / telemetry smoke + screenshots) runs last, after
  7a/7b/7c complete.

**Effort delta**: Wave 3 shifted from ~1 low-complexity task
(doc-only) to 3 tasks with mixed complexity (high + medium + high)
plus Task 8. This bumps the overall story estimate from **M-L** to
**L**. See §Quality Gate > Effort confirmation for the revised
assessment.

## Story-Specific Compliance Rules

- **API**: Two new HTTP endpoints added under SBA (D3-B):
  `POST /nsmf-pdusession/v1/sm-contexts` and
  `DELETE /nsmf-pdusession/v1/sm-contexts/{smContextRef}`. **Standard
  envelope does NOT apply** — SBA endpoints follow 3GPP-native shapes:
  `PduSessionCreatedData` on success, `ProblemDetails` (RFC 7807) on
  error, matching the existing AUSF/UDM precedent at
  `internal/aaa/sba/ausf.go` / `udm.go`. No changes to the existing
  HTTP API envelope. Keep response bodies minimal — only the fields
  the Argus simulator actually reads (`ueIpv4Address`, `supi`, `dnn`,
  `sNssai`).
- **DB**: new store methods added (`IPPoolStore.RecountUsedAddresses`,
  `SIMStore.ClearIPAddress`). NO new migrations unless D2-B chosen —
  in which case one new migration pair for the trigger.
- **UI**: no new components. Existing `framed_ip`/`ip_address` fields
  auto-populate. Frontend-design skill NOT invoked (no new UI).
- **Business**:
  - Dynamic allocation MUST happen AFTER policy Allow check — never
    allocate for a rejected auth (advisor flag #6).
  - Static (`allocation_type='static'`) allocations MUST NOT be
    released on Accounting-Stop — they persist through grace period
    (advisor flag #5).
  - Every AllocateIP MUST be followed by SetIPAndPolicy + simCache
    InvalidateIMSI in the SAME logical operation — otherwise re-auth
    for the same SIM allocates a fresh IP and leaks the first
    (advisor flag #3).
- **ADR**: none directly relevant. ADR-002 (golang-migrate) applies
  if D2-B chosen.

## Bug Pattern Warnings

- **PAT-004 (FK-to-partitioned-parent)** — NOT applicable here;
  `ip_addresses` uses `sim_id UUID` with no FK, correctly matching the
  partitioned-sims pattern. Developer should NOT try to add an FK.
- **PAT-002 (composite-timer pattern for externally-mutable
  deadlines)** — NOT applicable (no timer work here).
- **PAT-001 (SIM hot-path cache coherence)** — heightened relevance:
  advisor flag #3 explicitly requires `simCache.InvalidateIMSI` after
  every `SetIPAndPolicy` write. Developer MUST follow this verbatim.

## Tech Debt (from ROUTEMAP)

- **D-029** (STORY-079 Gate F-A4 — no CI guard against seed drift) —
  NOT blocked by this story. Seed changes here are idempotent;
  post-STORY-092 the seed-smoke test would catch regressions, but that
  is D-029's own story.
- **D-037** (TimescaleDB 2.26.2 columnstore/RLS DDL incompatibility) —
  pre-existing, unrelated. If encountered during integration test DB
  bootstrap (Task 3), developer should skip the test with a clear
  message and reference D-037; do NOT attempt a fix in this story.
- **D-038** (Enforcer nil-cache) — **CLOSED by this story at
  integration-test level** (Task 3). The unit-level closure remains
  from the 2026-04-17 commit. ROUTEMAP row flipped to `✓ RESOLVED` in
  the review step post-gate.
- **D-039** — NOT created by this story. Under the originally-planned
  D3-A path, STORY-092 would have deferred SBA UE IP binding and
  opened D-039 for a future story. D3-B (selected 2026-04-18) ships
  the mock Nsmf endpoint inside this story, so the deferral is no
  longer needed. STORY-089 (Operator SoR Simulator) remains the
  long-term home for a richer operator-side PDU Session simulator,
  but that is captured in STORY-089's own scope — not as tech debt
  against STORY-092.

## Mock Retirement

No mocks retired. Frontend DTO plumbing for `framed_ip`/`ip_address`
already in place from earlier stories; this story only adds backend
behaviour.

## Dependencies

- **STORY-086 (DONE, 2026-04-17)** — `check_sim_exists` trigger +
  schemacheck manifest. Both stay intact.
- **STORY-087 (DONE, 2026-04-17)** — pre-069 shim. Tasks 3 and 6 reuse
  STORY-087's disposable-DB pattern at
  `internal/store/migration_freshvol_test.go`.
- **D-038 enforcer nil-guard fix (committed 2026-04-17)** — Task 3 is
  the integration-level regression test.
- **STORY-088 (DONE, 2026-04-17)** — `go vet ./...` clean. Task 9 (AC-
  10 regression check) preserves this.

## Blocking

- **STORY-090** (multi-protocol adapter refactor) cannot start until
  STORY-092 DONE. STORY-090 touches `operator.adapter_config` shape
  and UI; if allocation fails intermittently during STORY-090's
  development, the signal-to-noise falls.

## Out of Scope

- Mini Phase Gate spec extension (`docs/reports/test-infra-tech-debt-
  gate-spec.md`) — per dispatch advisor hard flag #4, this is the
  STORY-089 post-processing step. NOT touched here.
- IPv6 dynamic allocation — current `AllocateIP` already picks v4 OR v6
  rows via `NULLS LAST` ordering. Scope of this story: the v4 AC path.
  v6 verification lives in a future story.
- Redis-level counter cache — `used_addresses` stays app+DB only.
- CoA/DM triggered by policy re-eval re-binding a different IP — out
  of scope; existing CoA path does not touch Framed-IP.
- **5G Nsmf_PDUSession mock — STRICTLY LIMITED** (per D3-B LOCKED):
  this story ships ONLY `CreateSMContext` (POST) and
  `ReleaseSMContext` (DELETE). Explicitly **out of scope for the mock
  handler**: session modification (PATCH), QoS flow descriptors,
  N2/N4 UPF interaction, PCF (Nsmf_PDUSession notifications to PCF),
  charging data reporting, IPv6 prefix delegation, roaming (VPLMN
  Nsmf), handover procedures. See Risk 7.
- Retroactive reservation for historical sessions in
  `radius_sessions_archive` — the UI only reads live `radius_sessions`;
  historical rows stay empty where they were.
- CI wiring for the new integration tests in GHA — D-029 will handle.

## Risks & Mitigations

### Risk 1: Hot-path latency regression

- **Description**: Adding `AllocateIP` + `SetIPAndPolicy` +
  `InvalidateIMSI` to the RADIUS Access-Accept path on every
  unassigned-SIM auth could regress auth latency under load.
- **Likelihood**: Low under seed-time conditions (seed reserves IPs
  eagerly; dynamic allocations only happen for SIMs without pre-
  assignment, which will be rare post-seed-006-extension).
- **Mitigation**: Post-deploy, benchmark auth latency via existing
  `argus_auth_latency_*` Prometheus metrics in a dedicated gate step.
  If p99 regresses > 20ms, defer Task 2's dynamic allocation to a
  background job (new tech debt D-040) and ship Task 0 + Task 4 (release)
  only — static reservations cover the UI symptom.

### Risk 2: Cache invalidation race

- **Description**: After `SetIPAndPolicy` writes the new
  ip_address_id, `InvalidateIMSI` is called, but if a concurrent
  Access-Request for the SAME IMSI hits the radius server between the
  write and the invalidation, it reads stale cache and allocates a
  second IP.
- **Likelihood**: Low (simulator is single-threaded per IMSI; realistic
  dual-auth windows are < 5ms).
- **Mitigation**: Task 2 acquires the sim row via a DB-level read
  after `SetIPAndPolicy` returns; document the race in the code
  comment and rely on `FOR UPDATE SKIP LOCKED` in AllocateIP to ensure
  at most one IP per SIM at the DB level. If empirical duplicate
  allocations surface in Phase Gate, add a row-level lock on
  `sims.id` before AllocateIP (expansion of Task 2, acceptable scope
  creep).

### Risk 3: Diameter Gx AVP vendorID interop

- **Description**: Some Diameter clients expect
  `AVPCodeFramedIPAddress=8` with 3GPP vendor flag (0x80|0x40 + vendor
  10415). RFC 7155 (NASREQ) defines it as vendor=0, flags=M=1.
- **Likelihood**: Unknown — Argus's own `cmd/simulator` Gx client is
  the only current consumer.
- **Mitigation**: Use RFC 7155 shape (vendor=0, M=1, P=0). Document
  in the code comment. If a future operator adapter rejects this,
  gate Task 5's test against the simulator first and expose a feature
  flag in a later story.

### Risk 4: Seed 006 extension breaks existing seed-005 reservation

- **Description**: If Task 0 (D1-A) incorrectly generalises the CTE,
  seed-005's 16 SIMs could end up double-reserving or lose their
  existing reservations.
- **Likelihood**: Low — the existing CTE at `006:119-161` is already
  scoped to `s.ip_address_id IS NULL` which is idempotent.
- **Mitigation**: Developer adds a pre-extension assertion test:
  before the CTE runs, snapshot the count of
  `sims WHERE ip_address_id IS NOT NULL` grouped by tenant; post-CTE,
  assert the count is ≥ snapshot. Fail-loud via `RAISE EXCEPTION` if
  seed-005 reservations decreased.

### Risk 5: D-037 blocks Task 3 integration test in CI

- **Description**: Task 3 spins up a disposable DB via `argus migrate
  up`; D-037 documents a TimescaleDB-RLS DDL failure at
  `20260412000006_*`. If the CI image doesn't match the exact
  TimescaleDB version of the dev environment, the test skips.
- **Likelihood**: Medium.
- **Mitigation**: Per STORY-087 gate precedent (Risk 6 in STORY-087-plan.md),
  test uses `t.Skip("DATABASE_URL not set or migration env incompatible")`
  with a clear log message referencing D-037. Task 3 passes in the
  local gate run; CI hardening lives under D-029.

### Risk 6: Developer wires Diameter Framed-IP AVP with 3GPP vendor

- **Description**: A natural-looking but wrong implementation would
  set vendor=VendorID3GPP=10415 on the AVP, matching the existing 3GPP
  AVPs in the file. This violates RFC 7155.
- **Likelihood**: Medium — the surrounding AVPs in `avp.go` are 3GPP.
- **Mitigation**: Task 5 spec explicitly states vendorID=0 with
  citation. Unit test asserts `avp.VendorID == 0` and `avp.Flags ==
  AVPFlagMandatory`.

### Risk 7: SBA Nsmf mock scope creep (NEW — D3-B)

- **Description**: Once a mock Nsmf handler exists, a well-meaning
  developer may extend it to support PDU session modification (PATCH),
  QoS flow descriptors, PCF notifications, IPv6 prefix delegation, N2
  SM information, or real UPF selection — none of which STORY-092
  needs. Each addition increases the Argus SBA surface area, blurs
  the boundary with STORY-089 (Operator SoR Simulator), and makes
  STORY-089's eventual absorption of this mock harder.
- **Likelihood**: Medium — the 3GPP TS 29.502 surface is large, and
  a "fill in the rest of the spec" instinct is natural.
- **Mitigation**:
  1. Task 7a spec **strictly limits** the mock to IP allocation on
     Create and release on Delete — explicitly out-of-scope:
     `PATCH /sm-contexts/{smContextRef}`, QoS flows, PCF integration,
     session modification, N2/N4 UPF interaction, IPv6 prefix
     delegation, handover procedures.
  2. The §Out of Scope block in this plan names each forbidden
     extension explicitly so that future reviewers see the fence.
  3. Risk-7 line in this section is referenced from Task 7a and
     AC-6.
  4. If 5G scope genuinely needs to grow beyond Create/Release, open
     a follow-up story (expected owner: STORY-089) rather than
     extending STORY-092's implementation in-flight.

## Quality Gate (plan self-validation)

### Substance

- Goal stated (6-point summary).
- Root cause traced to exact lines (RADIUS `server.go:363, 550`; seed
  `003:289-301, 1165-1170`; seed `006:119-161`).
- Every advisor hard flag surfaced and placed into task spec.
- 3 explicit decision points (D1, D2, D3) with recommendations,
  trade-offs, and required user input.

### Required sections

- Goal.
- Architecture Context (current reality, data flow, wiring gaps).
- Decision Points (D1, D2, D3).
- Acceptance Criteria (AC-1..AC-10).
- Architecture references (RFC 2865, TS 29.212, TS 23.501/23.502).
- Tasks (0, 1, 2, 3, 4, 5a, 5b, 6, 7a, 7b, 7c, 8 — 12 numbered,
  wave-grouped).
- Acceptance Criteria Mapping.
- Test Strategy.
- Rollback plan.
- Wave plan.
- Story-Specific Compliance Rules.
- Bug Pattern Warnings.
- Tech Debt.
- Mock Retirement.
- Dependencies.
- Blocking.
- Out of Scope.
- Risks & Mitigations (7 risks — Risk 7 added under D3-B).
- Quality Gate self-validation (this section).

### Embedded specs

- RADIUS packet flow written out in pseudocode with exact attribute
  names and line numbers.
- Diameter Gx AVP code, flags, vendor written out.
- Seed extension semantics described at the CTE level.
- Nil-cache enforcer test steps enumerated (1-6).

### Effort confirmation

- Original estimate: **M-L**.
- Task count: **12** (Task 0, 1, 2, 3, 4, 5a, 5b, 6, 7a, 7b, 7c, 8)
  — bumped from 10 under D3-B expansion (Wave 3 grew from 1 doc-only
  task to 3 tasks: mock handler + simulator extension + integration
  test).
- High-complexity tasks: **5** (Task 2 RADIUS hot path, Task 3
  nil-cache integration test, Task 5b Diameter hot path, Task 7a SBA
  Nsmf mock handler, Task 7c SBA full-flow integration test) —
  exceeds the "L = at least 1 high" bar.
- **Revised estimate after D3-B selection: L** (bumped from M-L). The
  Wave-3 expansion is material: a new SBA handler file, simulator
  client extension, and a third integration test. Signalling
  explicitly so Amil downstream estimates reflect the shift.
- Risk count: **7** (was 6; added Risk 7 — SBA Nsmf scope creep).

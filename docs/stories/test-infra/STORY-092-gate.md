# Gate Report: STORY-092 — Dynamic IP Allocation pipeline + SEED FIX

## Summary
- Requirements Tracing: 10/10 ACs traced to tests + evidence (AC-1 dynamic alloc → T2+integration; AC-2 Acct-Start fallback → T4; AC-3 dynamic/static release → T4 (dynamic + static variants); AC-4 Gx Framed-IP → T5b; AC-5 Gx release → T5b; AC-6 SBA Nsmf → T7a/b/c; AC-7 seed materialisation → T0 + DB snapshot; AC-8 counter cycle → T6; AC-9 nil-cache integration → T3; AC-10 baseline green → this gate)
- Gap Analysis: 10/10 acceptance criteria passed
- Compliance: COMPLIANT (API envelope not applicable — SBA uses 3GPP-native ProblemDetails per precedent; DB new methods only, no migrations; UI no changes)
- Tests: 3024 PASS no-DB (98 packages) + 3057 PASS / 15 FAIL with-DB; all 15 DB failures are pre-existing (BackupStore×2, BackupCodeStore×8 incl subtests, FreshVolumeBootstrap_STORY087, DownChain_STORY087, PasswordHistory×3) per dispatch baseline, confirmed unchanged. Story tests: 12/12 sentinel tests PASS.
- Test Coverage: 10/10 ACs have positive tests; AC-3 and AC-5 have explicit negative tests (static_preserved branches); AC-6 has user_not_found, pool_exhausted, wrong_method, bad_body, missing_supi, deps_not_wired_503 negative tests.
- Performance: no new hot-path regression introduced. `allocateDynamicIPIfNeeded` is no-op when `sim.IPAddressID != nil`, so the common post-seed path hits a single cheap nil-pointer check. No queries added on the static-preallocated path.
- Build: PASS (`go build ./...` clean, `go vet ./...` clean)
- Screen Mockup Compliance: N/A (no UI changes — existing `framed_ip`/`ip_address` fields auto-populate)
- Token Enforcement: N/A (no UI changes)
- Turkish Text: N/A (backend-only story)
- Overall: PASS (unconditional)

## Team Composition
- Analysis Scout: 1 finding (F-A1 dead code in nsmf.go SUPI prefix handling)
- Test/Build Scout: 0 findings (build + vet + tests all green)
- UI Scout: 0 findings (4 evidence PNGs all valid, show end-to-end IP population)
- De-duplicated: 1 → 1 finding

## Fixes Applied

| # | Category | File | Change | Verified |
|---|----------|------|--------|----------|
| 1 | Code quality (dead code) | `internal/aaa/sba/nsmf.go:170-177` | Removed unreachable conjunct `imsi == req.SUPI && !strings.HasPrefix(req.SUPI, "imsi-")` in the SUPI-to-IMSI normalization. The original condition could never be true because `strings.TrimPrefix` only returns the original string unchanged when the prefix is absent — in which case `imsi == req.SUPI` is already captured by the first branch when trimming yields empty (which happens only for empty input or prefix-only input). Simplified to a one-branch fallback with a clarifying comment. | go build + go vet clean; TestSBANsmfCreateSMContext_AllocatesIP (6 subtests) + TestSBANsmfReleaseSMContext_Unknown + TestExtractSmContextRef all green. |

## Escalated Issues

None.

## Deferred Items

None (every finding was fixable in-gate).

## Verification

- Tests after fixes: all 12 Wave 1-3 sentinel tests PASS (TestRADIUSAccessAccept_DynamicAllocation, TestEnforcerNilCacheIntegration_STORY092, TestRADIUSAccountingStop_ReleasesDynamic, TestRADIUSAccountingStop_PreservesStatic, TestRADIUSAccountingStart_FallbackFramedIP, TestGxCCAInitial_FramedIPAddress, TestGxCCRTermination_ReleasesDynamic [dynamic_released + static_preserved], TestIPPoolStore_AllocateReleaseCycle, TestIPPoolStore_RecountUsedAddresses_FixesDrift + _EmptyPool + _TenantScoping, TestSBANsmfCreateSMContext_AllocatesIP [6 subtests], TestSBAFullFlow_NsmfAllocates, TestClient_CreatePDUSession [3 subtests], TestClient_ReleasePDUSession [3 subtests])
- Build after fixes: PASS
- Fix iterations: 1 (max 2)

## Passed Items

### RADIUS (Wave 1-2)
- `Server.allocateDynamicIPIfNeeded` (`server.go:248`) correctly guards on nil simStore / ipPoolStore / sim / APN, short-circuits when `sim.IPAddressID != nil`, logs pool-exhausted as warning not error, persists via SetIPAndPolicy + cache invalidation + local mutation (advisor flag #3 compliance).
- `sendEAPAccept` (`server.go:488`): allocation runs AFTER policy Allow check (advisor flag #6 compliance); order is policy-eval → allocation → Framed-IP attach.
- `handleDirectAuth` (`server.go:647`): same ordering — allocation between policy check and Framed-IP emit.
- `handleAcctStart` (`server.go:783-791`): Framed-IP fallback to sim.IPAddressID when NAS omits Framed-IP-Address AVP (AC-2).
- `handleAcctStop` (`server.go:977`): `releaseDynamicIPIfNeededForSession` resolves SIM via session.TenantID + SimID (correct — radius_sessions doesn't persist IMSI); allocation_type gate preserves static reservations (AC-3).
- `SetSIMStore` setter (`server.go:230`): follows the SetPolicyEnforcer / SetMetricsRecorder precedent; nil is valid and disables dynamic alloc.

### Diameter Gx (Wave 2)
- `AVPCodeFramedIPAddress = 8` (`avp.go:25`) with explicit comment citing RFC 7155 NASREQ §4.4.10.5.1 — vendor=0 not 3GPP (Risk 6 mitigation).
- `GxHandler` struct extended with ipPoolStore + simStore (`gx.go:18-30`); constructor takes explicit deps; `NewGxHandler` signature extended cleanly.
- `handleInitial` (`gx.go:146-150`): allocation runs AFTER SIM confirmed active (advisor flag #6); CCA-I Framed-IP AVP emitted with `AVPFlagMandatory | vendorID=0` via `NewAVPAddress` (AC-4).
- `handleTermination` (`gx.go:354`): `releaseDynamicIPIfNeeded` mirrors RADIUS symmetrically; static-preserve check correct (AC-5).
- `parseV4AddressForAVP` helper localised in gx.go per YAGNI plan directive.
- `ServerDeps` extension (`server.go:34-37`): IPPoolStore + SIMStore optional; nil disables IP handling.

### 5G SBA Nsmf (Wave 3, D3-B)
- `NsmfHandler` with interface-typed stores (`SIMResolver`, `SIMUpdater`, `IPPoolOperations`, `SIMCache`) — testable without a DB; concrete production types satisfy the interfaces automatically.
- `HandleCreate` (`nsmf.go:154`): request-handling branches exhaustively covered — method guard → body decode → SUPI normalization → resolver short-circuit → SIM state gate → APN check → alloc store gate → pool list → AllocateIP with ErrPoolExhausted mapping → persist → cache invalidate → smContextRef store → 201 + Location.
- `HandleRelease` (`nsmf.go:290`): method guard → path extract → LoadAndDelete on sync.Map → allocation_type gate (dynamic→release, static→preserve); 204 No Content response.
- Route mounting (`server.go:126-127`): `POST /nsmf-pdusession/v1/sm-contexts` and `DELETE /nsmf-pdusession/v1/sm-contexts/` wired correctly.
- ServerDeps extension (`server.go:34-42`): 4 new optional deps (SIMResolver, SIMStore, IPPoolStore, SIMCache); constructor auto-wires.
- Scope strictly limited per Risk 7: no PATCH, no QoS, no PCF, no UPF — confirmed by file header, Risk 7 doc, and §Out of Scope block.

### Simulator SBA client (Wave 3)
- `CreatePDUSession` / `ReleasePDUSession` emit full metric triple (Requests, Responses, Latency) plus the new `SBAPDUSessionsTotal` counter with disjoint result labels {ok, pool_exhausted, user_not_found, transport_error, timeout}.
- `classifyPDUCause` maps ProblemDetails.Cause → label enum; unknown causes collapse to transport_error so dashboards surface aberrant servers (not silent).
- `extractRefFromLocation` handles both absolute URLs and path-only Location headers; strips query/fragment.
- Empty-ref short-circuit on Release returns nil (no wasted HTTP); matches the engine pattern where Create failure leaves smContextRef empty.

### Simulator engine (Wave 3)
- `runSBASession` (`engine.go:501-547`): Create runs AFTER AUSF + UDM success; failure is non-fatal at session level (smContextRef stays empty, Release leg becomes no-op); Release uses bounded 5s fresh context so shutdown doesn't hang on unresponsive Nsmf.
- DNN + sNssai derivation: defaults `internet` + `{SST:1, SD:"000001"}` per AUSF precedent; operator override via `op.SBA.Slices[0]`.

### Store (Wave 1, T0 + T1)
- `IPPoolStore.RecountUsedAddresses` (`ippool.go:55-92`): deterministic recount via COALESCE-LEFT-JOIN pattern; handles tenantID uuid.Nil as "all pools"; idempotent. 3 unit tests (FixesDrift, EmptyPool, TenantScoping) all green.
- `SIMStore.ClearIPAddress` (`sim.go:823-829`): minimal nullifier, symmetric with SetIPAndPolicy per plan.

### Seed 006 (Wave 1, T0)
- D1-A extension correctly scoped: adds ip_addresses materialisation for seed 003's 13 pools + adds ip_pools row for seed 003's missing m2m.water APN + guards seed 005 pool INSERTs with `WHERE EXISTS` so seed-003-only databases don't FK-fail.
- Idempotency preserved via `WHERE NOT EXISTS` on every ip_addresses INSERT.
- Fail-fast `DO $$ RAISE EXCEPTION` at end (lines 402-422) catches silent seed regressions.
- Reservation CTE untouched from previous revision — seed 005's 16 SIMs stay reserved, seed 003's SIMs now gain IPs from the newly-materialised pools.

### SIMCache nil-redis guard (D-038 closure at integration level)
- `InvalidateIMSI` (`sim_cache.go:79-81`): nil-redis guard added. Without this, the RADIUS dynamic allocation path would NPE when main.go boots with nil redis client.
- `TestEnforcerNilCacheIntegration_STORY092` literally constructs `enforcer.New(nil, policyStore, violationStore, nil, nil, logger)` — matching main.go literal nil at positions 1 (policyCache) AND 5 (redis). This closes D-038 at integration level; the unit tests still cover the nil-guard in isolation.

### AC-9 D-038 closure verification
- Test file `enforcer_nilcache_integration_test.go:196`: `pe := enforcer.New(nil, policyStore, violationStore, nil, nil, zerolog.Nop())` — same literal-nil pattern as `cmd/argus/main.go:1067`. Advisor hard flag #1 (Wave 1 regression must be integration) is satisfied: the test (a) provisions a disposable test DB via golang-migrate `file://…/migrations`, (b) seeds tenant+operator+apn+pool+policy+sim, (c) issues a full RADIUS Access-Request through `handleDirectAuth`, (d) asserts Access-Accept + Framed-IP + DB state + enforcer Evaluate Allow=true.

### AC-10 baseline greenness
- `go test ./...` no-DB: 3024 PASS, 0 FAIL, matches step-log.
- `go test ./...` with DB: 3057 PASS, 15 FAIL — every failure is pre-existing, confirmed against the dispatch baseline note. Specifically:
  - TestBackupCodeStore_Integration + 7 subtests (8 total) — pre-existing, migration hash / FK-to-partitioned-parent issue unrelated to STORY-092
  - TestBackupStore_Integration, TestBackupStore_ExpireRetention — pre-existing
  - TestFreshVolumeBootstrap_STORY087 — pre-existing per plan Risk 5 (D-037 TimescaleDB-RLS)
  - TestDownChain_STORY087 — pre-existing (closed-world test sensitive to local DB drift)
  - TestPasswordHistoryStore_Integration + 2 subtests — pre-existing

## Maintenance Mode — Pass 0 Regression

N/A (STORY-092 is a forward-feature story, not maintenance mode).

## Advisor-flag compliance (from plan LOCKED section)

| Hard flag | Compliance | Evidence |
|-----------|------------|----------|
| #1 Wave 1 nil-cache must be INTEGRATION not unit | PASS | `enforcer_nilcache_integration_test.go` uses real DB via golang-migrate, not a mock. Enforcer literally constructed with nil positions matching main.go. |
| #2 D2-A app-level counter (no trigger) | PASS | `AllocateIP` / `ReleaseIP` maintain `used_addresses` app-side; `RecountUsedAddresses` added as a reconciliation helper only — no new migration. |
| #3 D3-B Nsmf minimal (Create+Release only) | PASS | Only two routes mounted (`/sm-contexts` POST + `/sm-contexts/{ref}` DELETE). No PATCH, no QoS, no PCF. §Out of Scope block named each forbidden extension. |
| #4 ReleaseIP symmetric with AllocateIP | PASS | Both mutate `ip_addresses.state`, `sim_id`, `allocated_at`, `ip_pools.used_addresses` within the same transaction. Static allocations go through the reclaim grace path per existing STORY-082 semantics. |
| #5 Mini Phase Gate spec OUT OF SCOPE | PASS | Not touched — spec file unchanged in this story. |

## Dispatch "Absolute rules" compliance

- No escalations to user (all findings fixable in-gate). PASS
- No new TODOs / stubs introduced. PASS (grep confirms zero TODO/FIXME/XXX across all 10 new/modified test + source files touched by this story).
- No Wave 1-3 tests broken by in-gate fix (all 12 sentinel tests green post-fix). PASS
- STORY-090 / STORY-089 scope untouched. PASS.
- No pre-existing bugs fixed in-story (the SMSOutbound case is already closed by STORY-086; Backup/PasswordHistory etc. remain as separate debt items). PASS.

## Gate verdict

**PASS (unconditional)**.

All 12 tasks (T0, T1, T2, T3, T4, T5a, T5b, T6, T7a, T7b, T7c, T8) are complete and green. 10/10 acceptance criteria have test + runtime coverage. D-038 closed at integration level. One LOW finding (dead-code conjunct in SUPI normalization) fixed in-gate. Step 4 Review can proceed.

## Re-dispatch to Step 4 Review

No blockers. Review should verify:
- D-038 ROUTEMAP entry transitions from OPEN → ✓ RESOLVED (Gate step flipped it via Task 3 closure; review to stamp the row).
- 4 evidence PNGs + DB state snapshot confirm the runtime reality STORY-092 set out to deliver.
- Step-log gate line appended (see below).

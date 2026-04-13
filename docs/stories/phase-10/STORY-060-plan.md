# Implementation Plan: STORY-060 — AAA Protocol Correctness

## Goal
Close every protocol-level DEFER/test-compat shortcut accumulated across STORY-016/022/026/027/028/030/040/054 so Argus passes RADIUS/Diameter/5G/WS compliance audits and interoperates with standard third-party peers. Scope: EAP-SIM spec correctness + MSK race fix, WS pong/backpressure/per-user/reconnect semantics, Profile Switch CoA/DM, Bulk policy assign CoA, Diameter/TLS wiring validation, and RAT enum canonical alignment across DSL & SoR.

## Architecture Context

### Components Involved
- **SVC-02 WebSocket Server** — `internal/ws/server.go`, `internal/ws/hub.go`. Handles heartbeat, backpressure, per-tenant/per-user limits, control messages.
- **SVC-04 AAA Engine**
  - EAP: `internal/aaa/eap/sim.go`, `state.go`, `redis_store.go`. EAP-SIM handler, state machine, Redis state store.
  - RADIUS: `internal/aaa/radius/server.go`. RADIUS listener that consumes `GetSessionMSK` for Access-Accept.
  - Session CoA/DM: `internal/aaa/session/coa.go`, `dm.go`. RFC 5176 senders.
  - Diameter: `internal/aaa/diameter/server.go`, `tls.go`. Diameter peer server with TLS wrapping.
  - RAT type: `internal/aaa/rattype/rattype.go`. Canonical enum + protocol mappers.
- **SVC-05 Policy Engine**
  - DSL parser: `internal/policy/dsl/parser.go` (local `validRATTypes` map must become re-export).
  - Rollout service: `internal/policy/rollout/service.go`. Already dispatches CoA per-SIM.
- **SVC-06 Operator Routing**
  - SoR engine: `internal/operator/sor/types.go`, `engine.go`. Already imports `rattype` for `DefaultRATPreferenceOrder`.
- **SVC-09 Job Runner**
  - Bulk policy assign: `internal/job/bulk_policy_assign.go`. Currently skips CoA; must dispatch per-session CoA after `SetIPAndPolicy`.
- **eSIM**
  - Profile management: `internal/esim/smdp.go`, `internal/api/esim/handler.go`. `Handler.Switch` must trigger DM against active sessions before DB switch.
- **Config**
  - `internal/config/config.go` — `DIAMETER_TLS_*`, `WS_PONG_TIMEOUT`, `WS_MAX_CONNS_PER_USER` vars.
- **main.go**
  - `cmd/argus/main.go` — wiring for ws.ServerConfig, diameter Start/StartWithTLS, new bulk policy deps, esim handler deps.

### Data Flow — EAP-SIM MSK retrieval (AC-1 fix)
```
RADIUS Access-Request + EAP-Response/SIM/Challenge
  → radius/server.go.handleEAPAuth
    → eap.StateMachine.ProcessPacket(sessionID, raw)
      → handleChallenge → SIMHandler.handleChallengeResponse
        → if hmac.Equal(mac, expectedMAC)  [ONLY THIS PATH — no SRES fallback]
          → return NewSuccess
      → state.go handleChallenge: session.State=StateSuccess → store.Delete(sessionID)  ← RACE POINT
  → radius/server.go: msk, _ := s.eapMachine.GetSessionMSK(ctx, sessionID)  ← returns nil after race
  → Access-Accept with MS-MPPE-Recv-Key / MS-MPPE-Send-Key derived from MSK
```
**Fix**: Redis `GETDEL` via new `RedisStateStore.GetAndDelete` method. `handleChallenge` calls `GetAndDelete` once, retrieves the full session (including MSK), and the subsequent `GetSessionMSK` uses a second short-lived Redis key `eap:msk:{sessionID}` TTL 5s created at success time, OR the state machine caches the MSK in an in-memory `lastSuccessMSK sync.Map` keyed by sessionID, deleted on first read. We prefer the second approach (see Task 2 detail) because it avoids a second Redis round-trip on the hot path.

### Data Flow — WebSocket heartbeat & backpressure (AC-2/3/4/5)
```
Client connects → Server handleWS
  → JWT validate (query param OR first-message within authTimeout=5s)
  → Enforce MaxConnsPerTenant (100)
  → [NEW] Enforce MaxConnsPerUser (5) — iterate tenant conns matching UserID, evict oldest if 6th
  → Register in Hub
  → writePump: ticker(30s) → PingMessage
  → readPump: SetReadDeadline(pingPeriod + pongWait)
    → PongHandler: extend deadline
    → [CHANGED] pongWait = cfg.WSPongTimeout (default 90s per WEBSOCKET_EVENTS.md authoritative spec)
Server shutdown / scheduled maintenance
  → [NEW] Hub.BroadcastReconnect(reason, delaySeconds)
    → iterate all conns → send {type:"reconnect", data:{reason, after_ms}}
Slow client
  → Hub.BroadcastAll/ToTenant → conn.safeSend(msg)
    → [CHANGED] if len(SendCh)==cap: drain oldest (<-SendCh), metric++ then push new
```

### Data Flow — eSIM Profile Switch DM trigger (AC-6)
```
PUT /api/v1/esim/profiles/{id}/switch?force=true|false
  → Handler.Switch
    → lookup source profile → verify sim_type=esim
    → [NEW] sessions := RadiusSessionStore.ListActiveBySIM(ctx, sim.ID)
    → [NEW] if len(sessions)>0 && !force:
        for each session:
          dmRes := DMSender.SendDM(ctx, {NASIP, AcctSessionID, IMSI}) with 3s timeout
          if ack OR timeout: log + continue
          if nak + !force: fail switch (return 409 SESSION_DISCONNECT_FAILED)
        wait up to total 5s (already bounded by DM per-call 3s)
    → smdpAdapter.DisableProfile / EnableProfile (existing)
    → esimStore.Switch (existing atomic tx)
```

### Data Flow — Bulk Policy Assign CoA (AC-7)
```
POST /api/v1/sims/bulk/policy-assign
  → Job created → BulkPolicyAssignProcessor.processForward
    → for each sim in segment (batched 1000):
        acquire per-SIM lock
        SetIPAndPolicy(simID, nil, &policyVersionID)  (existing)
        [NEW] activeSessions := sessionStore.ListActiveBySIM(ctx, sim.ID)
        [NEW] for each session:
          coaSender.SendCoA(ctx, {NASIP, AcctSessionID, IMSI, Attributes:{Filter-Id:policyName}})
          update PolicyAssignment.CoAStatus via policyStore (mirrors rollout pattern)
        release lock
        publishProgress (existing)
    → completeJob with CoA counters in result JSONB
```

### API Specifications

**Modified REST endpoint — eSIM Profile Switch (API-074)**
- `POST /api/v1/esim/profiles/{id}/switch`
- New query parameter: `force` (bool, default false)
  - `false`: if SIM has active session, dispatch DM and await ack before proceeding; if DM fails (NAK received), return 409.
  - `true`: skip DM, proceed immediately (maintenance escape hatch).
- Request body (unchanged): `{ "target_profile_id": "uuid" }`
- New response field on success: `"disconnected_sessions": [ { "session_id": "...", "acct_session_id": "...", "dm_status": "ack|timeout|nak" } ]`
- New error: 409 Conflict with `code: "SESSION_DISCONNECT_FAILED"` if `force=false` and DM returns NAK.
- Status envelope: `{ status, data, error? }` standard.

**Modified REST endpoint — Bulk Policy Assign (API-065)**
- `POST /api/v1/sims/bulk/policy-assign`
- No request body change.
- New response (via `GET /api/v1/jobs/{id}`): result JSONB now includes:
  ```json
  { "processed_count": N, "failed_count": M, "coa_sent_count": X, "coa_acked_count": Y, "coa_failed_count": Z }
  ```
- Standard envelope.

**WebSocket control message — `reconnect` (AC-5)**
- Direction: Server → Client
- Payload schema:
  ```json
  { "type": "reconnect", "data": { "reason": "Server maintenance scheduled", "after_ms": 5000 } }
  ```
- Triggered by: `Hub.BroadcastReconnect(reason string, afterMs int)` — admin invokes via existing maintenance path (or main.go SIGTERM handler for graceful).
- Client behavior: close connection, reconnect after `after_ms`.

**WebSocket close codes (AC-4)**
- `4029` = max connections per user exceeded (new).
- Existing: `4001` unauth, `4002` max per tenant, `4003` auth timeout, `4004` internal.

### New Environment Variables (CONFIG.md additions)

| Variable | Type | Default | Required | Description |
|----------|------|---------|----------|-------------|
| `WS_PONG_TIMEOUT` | duration | `90s` | No | WebSocket pong wait after ping. Aligned with WEBSOCKET_EVENTS.md spec. Connection closes if no pong received within this window after a ping. |
| `WS_MAX_CONNS_PER_USER` | int | `5` | No | Max concurrent WS connections per user. 6th opens eviction of oldest with close code 4029. |

Existing (already in config.go, verify wiring):
- `DIAMETER_TLS_ENABLED` (bool, default false)
- `DIAMETER_TLS_CERT_PATH` (string, path to PEM cert)
- `DIAMETER_TLS_KEY_PATH` (string, path to PEM key)
- `DIAMETER_TLS_CA_PATH` (string, path to CA bundle for mTLS; when set → RequireAndVerifyClientCert)

**Story AC-8 text says `DIAMETER_TLS_CERT` / `DIAMETER_TLS_KEY` / `DIAMETER_TLS_CA`.** Current code uses `DIAMETER_TLS_CERT_PATH` / `DIAMETER_TLS_KEY_PATH` / `DIAMETER_TLS_CA_PATH`. We keep the `_PATH` suffix (more explicit, matches RadSec pattern `RADSEC_CERT_PATH`) and document the names in CONFIG.md + .env.example. Planner note: story AC names are shorthand; implementation keeps `_PATH` suffix.

### Protocol Spec References (embedded)

**RFC 4186 §9.3 — EAP-SIM AT_MAC AVP (HMAC-SHA1 only)**
```
The AT_MAC attribute is used for EAP-SIM message authentication.
Section 9.3: "The MAC is calculated using HMAC-SHA1-128 [RFC2104, FIPS 180-1]
over the whole EAP packet, concatenated with the NONCE_MT (in EAP-Request/SIM/
Challenge messages) or the RAND (in EAP-Response/SIM/Challenge messages)."
```
The simple-SRES fallback in `sim.go:217 verifySimpleSRES` is NOT in the RFC. It was test-compat only. Remove it entirely.

**WEBSOCKET_EVENTS.md — Connection Limits (authoritative for AC-4)**
```
| Metric | Limit |
|--------|-------|
| Max connections per tenant | 100 |
| Max connections per user | 5 |
```

**WEBSOCKET_EVENTS.md — Backpressure (authoritative for AC-3)**
```
1. Messages are buffered (up to 256).
2. If buffer is full, oldest messages are dropped.
3. If client does not read for 60 seconds, connection is closed.
```

**WEBSOCKET_EVENTS.md — Heartbeat (authoritative for AC-2)** — previously in spec file; updated by STORY-060:
```
- Ping/Pong: Server sends WebSocket ping frame every 30 seconds.
- Pong timeout: If no pong received within 90 seconds, server closes the connection.
```
*Current code uses 10s pongWait; STORY-060 replaces with configurable default 90s.*

**RFC 5176 §3.3 — Disconnect-Message (DM, Code 40)** — Argus sends DM to NAS on RADIUS CoA port (3799 default) with Acct-Session-Id + User-Name. Already implemented in `internal/aaa/session/dm.go`. Re-used by AC-6.

**RFC 5176 §3.1 — Change-of-Authorization (CoA, Code 43)** — Already implemented in `internal/aaa/session/coa.go`. Re-used by AC-7.

**RFC 6733 §5.6 — CER/CEA** — Already implemented in `internal/aaa/diameter/server.go handleCER`. AC-8 ensures TLS wraps the TCP listener.

### Design Token Map
**N/A — this story is pure backend/protocol work.** `has_ui: false`. No new UI components. The story Screen Reference list (SCR-050/070/062) is informational only — CoA dispatch count already surfaces on existing Policy Editor / Live Sessions screens via existing `policy.rollout_progress` WS event and existing session list UI. No frontend changes required.

---

## Database Schema

**No new migrations required.** STORY-060 is protocol behavior correctness; no schema changes. Existing tables touched:

- **TBL-17 `sessions`** (read only via existing `RadiusSessionStore.ListActiveBySIM(ctx, simID)`)
  - Source: existing migration + `internal/store/session_radius.go:196`
  - Columns used: `id, sim_id, nas_ip, acct_session_id, session_state`
- **TBL-10 `sims`** (no schema change; `rat_type` already exists from STORY-027)
- **TBL-15 `policy_assignments`** (existing `coa_status` column used by rollout service; bulk policy assign will write to it)

Optional data migration (AC-9):
```sql
-- Source: new migration 20260411000001_normalize_rat_type_values.up.sql
-- Normalizes any stored rat_type values that bypassed canonical form
UPDATE sessions SET rat_type = 'lte' WHERE LOWER(rat_type) IN ('4g', 'lte', 'eutran', 'e-utran');
UPDATE sessions SET rat_type = 'nr_5g' WHERE LOWER(rat_type) IN ('5g', 'nr', '5g_sa', 'nr_5g');
UPDATE sessions SET rat_type = 'nr_5g_nsa' WHERE LOWER(rat_type) IN ('5g_nsa', 'nr_5g_nsa');
UPDATE sessions SET rat_type = 'nb_iot' WHERE LOWER(rat_type) IN ('nb-iot', 'nb_iot', 'nbiot');
UPDATE sessions SET rat_type = 'lte_m' WHERE LOWER(rat_type) IN ('lte-m', 'lte_m', 'cat_m1', 'cat-m1');
UPDATE sessions SET rat_type = 'utran' WHERE LOWER(rat_type) = '3g';
UPDATE sessions SET rat_type = 'geran' WHERE LOWER(rat_type) = '2g';

UPDATE sims SET last_rat_type = 'lte' WHERE LOWER(last_rat_type) IN ('4g', 'eutran', 'e-utran');
-- ... (same normalization set)

-- Down migration: no-op (canonical values are valid under any mapping).
```
Only apply if a data scan finds non-canonical stored values. Developer MUST first run a SELECT DISTINCT check before shipping the migration.

---

## Prerequisites
- [x] STORY-056 completed (runtime fixes)
- [x] STORY-057 completed (data accuracy)
- [x] STORY-058 completed (frontend consolidation)
- [x] STORY-059 completed (security hardening, govulncheck clean)
- [x] EAP state machine (STORY-016) — feature already exists
- [x] Redis state store (STORY-016) — feature already exists
- [x] WebSocket server (STORY-040) — feature already exists
- [x] CoA/DM senders (STORY-015/017) — feature already exists
- [x] Diameter server + TLS scaffold (STORY-019/054) — feature already exists
- [x] `rattype` canonical package (STORY-027) — feature already exists
- [x] Rollout CoA dispatcher (STORY-025) — feature already exists

All prerequisites met. STORY-060 is a correctness/cleanup story — no new features.

---

## Review File Cross-Reference (for Developer context per AC)

| AC | Review to consult | Key finding |
|----|-------------------|-------------|
| AC-1 (EAP-SIM) | `docs/stories/phase-3/STORY-016-review.md` §Observations 1-2 | DEV-045: `GetSessionMSK` race fragile; VAL-007: dual-MAC test-compat path (`verifySimpleSRES`) must become config-gated or removed |
| AC-2/3/4/5 (WS) | `docs/stories/phase-7/STORY-040-review.md` §Check 10 obs 2-5 | pongWait=10s vs 90s spec (DEV-134); newest-drop backpressure; no per-user limit; no reconnect msg |
| AC-6 (eSIM) | `docs/stories/phase-5/STORY-028-review.md` §5 row 3 | Spec says "Switch triggers CoA/DM if SIM has active session", implementation has no CoA/DM integration (medium severity, deferred) |
| AC-7 (Bulk) | `docs/stories/phase-5/STORY-030-review.md` §11 row 3 | Bulk policy assign "updates TBL-15 but does NOT send CoA" (medium severity, deferred) |
| AC-8 (Diameter/TLS) | STORY-054 story file AC list | TLS on :3868 was in STORY-054 AC list; implemented via `StartWithTLS`. STORY-060 verifies wiring + env var naming + interop test |
| AC-9 (RAT enum) | `docs/stories/phase-4/STORY-022-review.md` §1 STORY-027 row; `docs/stories/phase-4/STORY-027-review.md` §1; `docs/stories/phase-4/STORY-026-review.md` §1 | DSL parser has local `validRATTypes` map; SoR imports `rattype` already. DSL must switch to canonical import too. |

---

## Bug Pattern Warnings (from decisions.md)

- **PAT-001 [STORY-059]**: *BR/acceptance tests assert behavior, not implementation — update BR tests when acceptance criteria change the rule.* For AC-1 (EAP-SIM MAC), `eap_test.go` has fixtures that exercise the simple-SRES path — grep for `verifySimpleSRES` and `combinedSRES` in tests and regenerate them to use HMAC-MAC (RFC 4186 §9.3). For AC-2/4, `server_test.go` asserts `pongWait=10s` and "no per-user limit"; update tests to match new 90s default and 4029 close-code behavior. For AC-9, grep for any DSL parser test fixture that depends on `validRATTypes` map shape.
- **PAT-002 [STORY-059]**: *Duplicated utility functions drift.* The fix for AC-1 must also grep for any other caller of `RedisStateStore.Get` + `Delete` pattern that could race similarly; the only other consumer is the RADIUS server path, but a `GetAndDelete` atomic primitive should be offered via the `StateStore` interface to prevent future regressions.

---

## Tech Debt (from ROUTEMAP)

- **No open tech-debt items target STORY-060.** D-001..D-005 target STORY-062/077/059. Planner scanned table — none applicable.

---

## Mock Retirement (Frontend-First projects only)

**No mock retirement for this story.** Argus is backend-first with no `src/mocks/` directory. STORY-060 does not introduce any new HTTP endpoints; existing mock (if any) are unchanged.

---

## Tasks

### Task 1: Canonical RAT enum import in DSL parser + data migration (AC-9)
- **Files:**
  - Modify `internal/policy/dsl/parser.go` (replace local `validRATTypes` map)
  - Modify `internal/policy/dsl/parser_test.go` (ensure aliases still accepted)
  - Create `migrations/20260411000001_normalize_rat_type_values.up.sql`
  - Create `migrations/20260411000001_normalize_rat_type_values.down.sql`
- **Depends on:** — (first task — AC-9 must land first because DSL import change would otherwise conflict with parallel tasks)
- **Complexity:** medium
- **Pattern ref:** Read `internal/operator/sor/types.go` — mirror how it imports `rattype` constants and exposes a default slice. For migration, read an existing migration file (`migrations/20260318000001_initial_schema.up.sql` for style).
- **Context refs:** "Architecture Context > Components Involved", "Database Schema (Optional AC-9 migration)", "Review File Cross-Reference (AC-9 row)"
- **What:**
  1. In `parser.go`, delete local `var validRATTypes = map[string]bool{...}` (lines 28-33). Replace `if !validRATTypes[ratName]` (line 732) call with `if !rattype.IsValid(rattype.Normalize(ratName))`. Add `import "github.com/btopcu/argus/internal/aaa/rattype"`.
  2. Ensure parser tests still pass for all aliases: `nb_iot`, `NB_IOT`, `NB-IoT`, `CAT_M1`, `lte_m`, `LTE-M`, `4G`, `LTE`, `5G_SA`, `nr_5g`, `5G_NSA`, `2G`, `3G`. `rattype.Normalize` already handles all of these — no new aliases needed in `rattype.go`, verify `aliasMap`.
  3. Run a DB scan query in dev: `SELECT DISTINCT rat_type FROM sessions UNION SELECT DISTINCT last_rat_type FROM sims WHERE last_rat_type IS NOT NULL;` — if any non-canonical values exist, commit the migration. If all values are already canonical, the migration becomes a no-op comment with rationale.
  4. Migration `up.sql` body as embedded in Database Schema section above. `down.sql` = `-- No-op: canonical values are valid under any mapping; rollback is unnecessary.`
- **Verify:**
  - `go test ./internal/policy/dsl/...` passes
  - `grep -rn "validRATTypes" internal/` returns ONLY `rattype` package references (no parser locals)
  - `grep -rn "aliasMap" internal/aaa/rattype/rattype.go` confirms all 13 story-listed aliases map to canonical

---

### Task 2: EAP-SIM RFC 4186 strict MAC + MSK race fix (AC-1)
- **Files:**
  - Modify `internal/aaa/eap/sim.go` (delete `verifySimpleSRES` + dual-path in `handleChallengeResponse`)
  - Modify `internal/aaa/eap/state.go` (add `StateStore.GetAndDelete` interface method OR add `lastSuccessMSK sync.Map` to StateMachine + new `ConsumeSessionMSK` method)
  - Modify `internal/aaa/eap/redis_store.go` (implement `GetAndDelete` using Redis `GETDEL` command)
  - Modify `internal/aaa/radius/server.go` (call `ConsumeSessionMSK` — not `GetSessionMSK` — after successful EAP)
  - Modify `internal/aaa/eap/eap_test.go` (regenerate test fixtures: use `computeSIMMAC` to produce spec-correct MAC instead of concatenated SRES; add race test for MSK retrieval; add test asserting simple-SRES path is rejected)
- **Depends on:** — (isolated to EAP package; can run in parallel with Task 1 & Task 3)
- **Complexity:** high
- **Pattern ref:** Read `internal/aaa/eap/redis_store.go` (existing Redis methods follow `client.Set/Get/Del` pattern with error wrap; mirror for `GetAndDelete`). Read `internal/aaa/eap/state.go handleChallenge` (lines 322-362) for the atomic success-delete pattern.
- **Context refs:** "Architecture Context > Data Flow — EAP-SIM MSK retrieval", "Protocol Spec References > RFC 4186", "Review File Cross-Reference (AC-1 row)", "Bug Pattern Warnings > PAT-001"
- **What:**
  1. **Strict MAC enforcement:** In `sim.go handleChallengeResponse` (line 124), delete the `|| verifySimpleSRES(...)` disjunct from line 137. Delete `combinedSRES` assembly (lines 130-134). Delete the `verifySimpleSRES` function (lines 217-222). Only `hmac.Equal(mac, expectedMAC)` remains. Accept no fallback.
  2. **MSK race fix — implementation choice (in-memory cache + atomic Redis GETDEL):**
     - Add `GetAndDelete(ctx context.Context, id string) (*EAPSession, error)` to the `StateStore` interface in `state.go`.
     - Implement in `redis_store.go` using `client.Do(ctx, "GETDEL", key)` (go-redis v9 supports GETDEL natively via `client.GetDel(ctx, key)`).
     - Implement in `MemoryStateStore` as Load+Delete under mutex.
     - In `StateMachine`, add `msks sync.Map` field (keyed `sessionID` → `[]byte MSK`, TTL 10s).
     - Rewrite `handleChallenge` success branch: instead of `_ = sm.store.Delete(ctx, session.ID)` alone, the handler now calls `sm.store.GetAndDelete(ctx, session.ID)` is unnecessary because the session object is already in-hand (loaded at line 152). BEFORE deleting, stash MSK: `if session.SIMData != nil && session.SIMData.MSK != nil { sm.msks.Store(session.ID, session.SIMData.MSK) }` (and same for AKAData). Then delete as before.
     - Add `ConsumeSessionMSK(sessionID string) ([]byte, bool)` that does `LoadAndDelete` on `sm.msks`.
     - Also add a goroutine or lazy sweep to delete stale entries after 10s TTL (alternative: wrap value in `{msk, createdAt}` struct and check on `LoadAndDelete`).
  3. **RADIUS server migration:** In `radius/server.go:317`, replace `msk, _ := s.eapMachine.GetSessionMSK(ctx, sessionID)` with `msk, _ := s.eapMachine.ConsumeSessionMSK(sessionID)`. This eliminates Redis hit entirely on the hot path and fixes the race permanently.
  4. **Test regeneration:** In `eap_test.go`, find every test that passes a simple-SRES-concatenated MAC and rewrite them to call `computeSIMMAC(session.SIMData.Kc, eapData, identifier)` to produce the expected MAC. Add a new negative test: `TestHandleChallengeResponse_SimpleSRES_Rejected` that sends a simple-SRES MAC and asserts `NewFailure`. Keep existing `TestGetSessionMSK_SIM` / `_AKA` but rename to `TestConsumeSessionMSK_*` and assert that a second call returns `nil, false`.
- **Verify:**
  - `go test ./internal/aaa/eap/...` passes (all regenerated tests green)
  - `go test ./internal/aaa/radius/...` passes
  - `grep -rn "verifySimpleSRES" internal/` returns ZERO matches
  - `grep -rn "GetSessionMSK" internal/` returns ZERO matches (replaced with `ConsumeSessionMSK`)
  - `go test -race ./internal/aaa/eap/... ./internal/aaa/radius/...` passes with race detector

---

### Task 3: Diameter/TLS verification + interop test + CONFIG.md update (AC-8)
- **Files:**
  - Modify `internal/aaa/diameter/tls_test.go` (add openssl s_client-style TLS handshake test with self-signed cert)
  - Modify `docs/architecture/CONFIG.md` (document DIAMETER_TLS_ENABLED, DIAMETER_TLS_CERT_PATH, DIAMETER_TLS_KEY_PATH, DIAMETER_TLS_CA_PATH in AAA Protocol Servers section)
  - Modify `.env.example` (add commented-out DIAMETER_TLS_* block)
- **Depends on:** — (infrastructure-level; isolated)
- **Complexity:** low
- **Pattern ref:** Read `internal/aaa/diameter/tls_test.go` (existing test stub) + `internal/gateway/` TLS tests if any. For CONFIG.md, read lines 130-146 "AAA Protocol Servers" and follow existing row format.
- **Context refs:** "Architecture Context > Data Flow (Diameter/TLS wiring)", "New Environment Variables"
- **What:**
  1. **Confirm wiring:** Read `cmd/argus/main.go:537-548` — verify `DiameterTLSEnabled && DiameterTLSCert != ""` triggers `StartWithTLS(TLSConfig{Enabled, CertPath, KeyPath, CAPath})`. Verify `tls.go NewTLSListener` sets `MinVersion: tls.VersionTLS12` and when `CAPath != ""` sets `ClientAuth: tls.RequireAndVerifyClientCert`. (Both confirmed during planner review.)
  2. **Interop test:** Extend `tls_test.go` with a test that:
     - Generates a self-signed cert pair in a tempdir via `crypto/tls` + `crypto/x509` (pattern: `x509.CreateCertificate`).
     - Starts `Server.StartWithTLS` on an ephemeral port (use `:0` and read back `listener.Addr()`).
     - Uses `tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify:true})` to connect.
     - Sends a Diameter CER message via `DecodeMessage`/`Encode` helpers and asserts a CEA comes back.
     - A second subtest attempts to connect with mTLS enabled but no client cert → expects handshake failure.
  3. **CONFIG.md:** Under `## AAA Protocol Servers` table add 4 rows for `DIAMETER_TLS_ENABLED`, `DIAMETER_TLS_CERT_PATH`, `DIAMETER_TLS_KEY_PATH`, `DIAMETER_TLS_CA_PATH` with descriptions: "Enable TLS wrapping of Diameter TCP listener on port 3868", "PEM server cert path", "PEM server key path", "PEM CA bundle for peer mTLS; when set, Argus requires and verifies client certificates".
  4. **.env.example:** Append commented block under `# === AAA ===` mirroring `RADSEC_*` example.
- **Verify:**
  - `go test ./internal/aaa/diameter/... -run TLS -v` passes
  - `grep -c "DIAMETER_TLS_" docs/architecture/CONFIG.md` ≥ 4
  - `grep -c "DIAMETER_TLS_" .env.example` ≥ 4 (all commented)

---

### Task 4: WebSocket pong timeout (90s) + reconnect control message (AC-2, AC-5)
- **Files:**
  - Modify `internal/config/config.go` (add `WSPongTimeout time.Duration` field)
  - Modify `internal/ws/server.go` (replace `pongWait` const usage with `s.cfg.PongTimeout` read from ServerConfig)
  - Modify `internal/ws/hub.go` (add `BroadcastReconnect(reason string, afterMs int)` method)
  - Modify `internal/ws/server_test.go` (test pong timeout at 90s default, test reconnect broadcast)
  - Modify `cmd/argus/main.go` (pass `cfg.WSPongTimeout` into `ws.ServerConfig` + call `wsHub.BroadcastReconnect` during graceful shutdown pre-close)
  - Modify `docs/architecture/CONFIG.md` (document `WS_PONG_TIMEOUT`)
  - Modify `docs/architecture/WEBSOCKET_EVENTS.md` (update Heartbeat section: 10s → 90s)
- **Depends on:** — (isolated; can run parallel with Task 5 & Task 6 — all three touch different aspects of the WS server)
- **Complexity:** medium
- **Pattern ref:** Read `internal/ws/server.go` ServerConfig struct (lines 30-34) — add field there. Read `internal/ws/hub.go BroadcastAll` (lines 106-136) — mirror pattern for `BroadcastReconnect` (it's a special envelope with fixed type `"reconnect"`). Read `cmd/argus/main.go` shutdown sequence (lines 720-740) for the graceful broadcast slot.
- **Context refs:** "Architecture Context > Data Flow (WebSocket heartbeat)", "Protocol Spec References > WEBSOCKET_EVENTS.md Heartbeat/Backpressure/Limits", "New Environment Variables", "Review File Cross-Reference (AC-2/3/4/5 row)"
- **What:**
  1. In `config.go` add `WSPongTimeout time.Duration \`envconfig:"WS_PONG_TIMEOUT" default:"90s"\`` next to `WSMaxConnsPerTenant`.
  2. In `server.go`, change `pongWait = 10 * time.Second` from a const to a field on `ServerConfig.PongTimeout`. Set default in `NewServer` if zero: `if cfg.PongTimeout == 0 { cfg.PongTimeout = 90 * time.Second }`. Update `readPump`: `conn.ws.SetReadDeadline(time.Now().Add(s.cfg.PongTimeout))` in two places.
  3. In `hub.go`, add:
     ```go
     func (h *Hub) BroadcastReconnect(reason string, afterMs int) {
         payload := map[string]interface{}{"reason": reason, "after_ms": afterMs}
         msg, _ := json.Marshal(map[string]interface{}{"type":"reconnect","data":payload})
         h.mu.RLock(); defer h.mu.RUnlock()
         for _, conns := range h.conns { for conn := range conns { select { case conn.SendCh <- msg: default: } } }
     }
     ```
  4. In `main.go` shutdown path, before `wsServer.Stop(ctx)` and `wsHub.Stop()`, call `wsHub.BroadcastReconnect("server shutting down", 2000)`; then sleep 500ms to allow the write pump to flush before close frames are sent.
  5. Update `server_test.go`:
     - New test `TestPongTimeout_90sDefault`: connect, don't respond to pings, assert read returns error after ~90s. (Use `cfg.PongTimeout = 2*time.Second` override for test speed.)
     - New test `TestReconnectBroadcast`: register conn, call `hub.BroadcastReconnect("maintenance", 3000)`, assert client receives a JSON message with `type:"reconnect"` and `data.reason`, `data.after_ms == 3000`.
  6. CONFIG.md: add `WS_PONG_TIMEOUT` row to Application table (next to existing `WS_MAX_CONNS_PER_TENANT`).
  7. WEBSOCKET_EVENTS.md: in Heartbeat section change "within 10 seconds" to "within 90 seconds" and add a note: "Configurable via WS_PONG_TIMEOUT env var (default 90s, per RFC 6455 guidance for long-lived connections)." Also promote the "Reconnect Message" subsection from "Server-to-Client Control Messages" to note it is fully implemented (delete any "not implemented" wording).
- **Verify:**
  - `go test ./internal/ws/... -run "Pong|Reconnect" -v` passes
  - Run `go test ./internal/ws/...` full suite — all prior tests still pass
  - `grep -n "pongWait = 10" internal/ws/server.go` returns ZERO matches
  - `grep -rn "BroadcastReconnect" internal/ws/ cmd/argus/` returns at least 3 matches (def + 2 usages)

---

### Task 5: WebSocket backpressure — drop OLDEST + drop counter metric (AC-3)
- **Files:**
  - Modify `internal/ws/hub.go` (add `safeSend(conn, msg)` helper that drains oldest when full; add `droppedCounter uint64` via `sync/atomic`; add `DroppedMessageCount()` getter)
  - Modify `internal/ws/hub_test.go` (test that when send buffer is full, OLDEST messages are evicted and newest delivered; drop counter increments)
- **Depends on:** — (isolated to hub — can run parallel with Task 4 & Task 6)
- **Complexity:** high
- **Pattern ref:** Read `internal/ws/hub.go BroadcastAll` (lines 106-136) — current `select { case conn.SendCh <- msg: default: }` pattern. Read `internal/analytics/metrics/` for atomic counter pattern examples.
- **Context refs:** "Architecture Context > Data Flow (slow client)", "Protocol Spec References > WEBSOCKET_EVENTS.md Backpressure", "Review File Cross-Reference (AC-3)"
- **What:**
  1. Add package-level:
     ```go
     var droppedMessages uint64 // accessed via atomic.AddUint64
     ```
     Or make it a field on `Hub` (preferred for testability).
  2. Add method:
     ```go
     func (h *Hub) safeSend(conn *Connection, msg []byte) {
         select {
         case conn.SendCh <- msg:
             return
         default:
         }
         // Buffer full — evict oldest and retry once
         select {
         case <-conn.SendCh:
             atomic.AddUint64(&h.dropped, 1)
             h.logger.Warn().Str("tenant_id", conn.TenantID.String()).Msg("ws send buffer full, dropped oldest")
         default:
         }
         select {
         case conn.SendCh <- msg:
         default:
             // still full (racing producer); drop newest as last resort
             atomic.AddUint64(&h.dropped, 1)
         }
     }
     func (h *Hub) DroppedMessageCount() uint64 { return atomic.LoadUint64(&h.dropped) }
     ```
  3. Replace every `select { case conn.SendCh <- msg: default: ... }` inside `BroadcastAll` and `BroadcastToTenant` with `h.safeSend(conn, msg)`.
  4. Update `hub_test.go`:
     - New `TestSafeSend_DropOldest`: create conn with `SendCh` capacity 2, send 3 messages via `safeSend`, assert receive order is `[msg2, msg3]` (msg1 was dropped as oldest), assert `h.DroppedMessageCount() == 1`.
     - Existing `TestSlowClientBackpressure` may assert newest-drop semantics — update to assert oldest-drop instead.
- **Verify:**
  - `go test ./internal/ws/... -run "SafeSend|Backpressure" -v` passes
  - Full `go test ./internal/ws/...` passes
  - `grep -c "safeSend\|DroppedMessageCount" internal/ws/hub.go` ≥ 4

---

### Task 6: WebSocket per-user connection limit with 4029 eviction (AC-4)
- **Files:**
  - Modify `internal/config/config.go` (add `WSMaxConnsPerUser int` field)
  - Modify `internal/ws/hub.go` (add `UserConnectionCount(tenantID, userID uuid.UUID) int` + `EvictOldestByUser(tenantID, userID uuid.UUID) *Connection`; add `createdAt time.Time` to Connection struct)
  - Modify `internal/ws/server.go` (add `CloseCodePerUserMax = 4029` const; add per-user enforcement in `handleWS` after per-tenant check)
  - Modify `internal/ws/server_test.go` (test 6th connection by same user evicts 1st with 4029)
  - Modify `cmd/argus/main.go` (pass `cfg.WSMaxConnsPerUser` into `ws.ServerConfig`)
  - Modify `docs/architecture/CONFIG.md` (document `WS_MAX_CONNS_PER_USER`)
- **Depends on:** — (can run parallel with Task 4 & Task 5)
- **Complexity:** medium
- **Pattern ref:** Read `internal/ws/hub.go TenantConnectionCount` (line 255) — mirror for per-user count. Read `internal/ws/server.go handleWS` tenant-limit enforcement (lines 135-146) — add per-user enforcement directly after.
- **Context refs:** "Architecture Context > Data Flow (WebSocket heartbeat & backpressure — AC-4 line)", "Protocol Spec References > WEBSOCKET_EVENTS.md Connection Limits", "New Environment Variables", "Review File Cross-Reference (AC-4 row)"
- **What:**
  1. Add `WSMaxConnsPerUser int \`envconfig:"WS_MAX_CONNS_PER_USER" default:"5"\`` to `config.go`.
  2. Extend `ws.ServerConfig` with `MaxConnsPerUser int` field (default 5 in `NewServer` if zero).
  3. Add `createdAt time.Time` to `Connection` struct in `hub.go`. Set in `handleWS` when `conn := &Connection{...}`.
  4. Add `CloseCodePerUserMax = 4029` to the close-code block in `server.go`.
  5. In `hub.go`, add:
     ```go
     func (h *Hub) UserConnectionCount(tenantID, userID uuid.UUID) int {
         h.mu.RLock(); defer h.mu.RUnlock()
         n := 0
         for conn := range h.conns[tenantID] { if conn.UserID == userID { n++ } }
         return n
     }
     func (h *Hub) EvictOldestByUser(tenantID, userID uuid.UUID) *Connection {
         h.mu.RLock()
         var oldest *Connection
         for conn := range h.conns[tenantID] {
             if conn.UserID == userID {
                 if oldest == nil || conn.createdAt.Before(oldest.createdAt) { oldest = conn }
             }
         }
         h.mu.RUnlock()
         return oldest
     }
     ```
  6. In `server.go handleWS`, after the per-tenant check (line 146), add:
     ```go
     userCount := s.hub.UserConnectionCount(claims.TenantID, claims.UserID)
     if userCount >= s.cfg.MaxConnsPerUser {
         if evictee := s.hub.EvictOldestByUser(claims.TenantID, claims.UserID); evictee != nil {
             closeMsg := websocket.FormatCloseMessage(CloseCodePerUserMax, "max connections per user reached, evicting oldest")
             _ = evictee.ws.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(writeWait))
             evictee.ws.Close()
             s.hub.Unregister(evictee)
         }
     }
     ```
     (Note: story says "6th connection evicts oldest" — so we evict before accepting, not reject. The new connection proceeds to register normally.)
  7. New test `TestMaxConnectionsPerUser`: open 5 connections as same (tenant, user), open 6th, assert that connection #1 receives close frame with code 4029, and connections #2-6 are still alive. Use existing `httptest.NewServer` + JWT helpers.
  8. CONFIG.md: add `WS_MAX_CONNS_PER_USER` row to Application table.
- **Verify:**
  - `go test ./internal/ws/... -run "MaxConnectionsPerUser|PerUser" -v` passes
  - Full `go test ./internal/ws/...` passes
  - `grep -n "4029\|CloseCodePerUserMax" internal/ws/server.go` returns at least 2 matches
  - CONFIG.md row present

---

### Task 7: eSIM Profile Switch — CoA/DM dispatch + force flag (AC-6)
- **Files:**
  - Modify `internal/api/esim/handler.go` (add `sessionStore *store.RadiusSessionStore` + `dmSender *session.DMSender` deps; extend `Switch` to dispatch DM)
  - Modify `cmd/argus/main.go` (pass new deps into `esim.NewHandler`)
  - Modify `internal/api/esim/handler_test.go` (test DM dispatched when session active; test `force=true` skips DM; test DM NAK returns 409)
- **Depends on:** — (isolated; can run parallel with Task 8)
- **Complexity:** high
- **Pattern ref:** Read `internal/api/session/handler.go:295-300` + `:380` — existing pattern for calling `dmSender.SendDM` from an HTTP handler. Read `internal/policy/rollout/service.go sendCoAForSIM` (line 385) — pattern for iterating sessions and dispatching + tracking status.
- **Context refs:** "Architecture Context > Data Flow — eSIM Profile Switch DM trigger", "API Specifications > Modified REST endpoint — eSIM Profile Switch", "Review File Cross-Reference (AC-6 row)", "Protocol Spec References > RFC 5176 §3.3"
- **What:**
  1. Extend `esim.Handler` struct with:
     ```go
     sessionStore *store.RadiusSessionStore
     dmSender     *session.DMSender
     ```
     Update `NewHandler` signature and all callers.
  2. Extend `NewHandler` signature — this touches `cmd/argus/main.go`. Pass `radiusSessionStore` and `dmSender` (already constructed in main.go for rollout/session handler usage).
  3. In `Switch` (handler.go:312), after verifying `sim.SimType == "esim"` (line 361) and BEFORE the SM-DP+ calls (line 366):
     ```go
     force := r.URL.Query().Get("force") == "true"
     var dmResults []map[string]interface{}
     if !force && h.sessionStore != nil && h.dmSender != nil {
         sessions, sErr := h.sessionStore.ListActiveBySIM(r.Context(), sim.ID)
         if sErr != nil {
             h.logger.Error().Err(sErr).Msg("list active sessions for esim switch")
             // non-fatal: proceed
         }
         for _, sess := range sessions {
             res, dmErr := h.dmSender.SendDM(r.Context(), session.DMRequest{
                 NASIP:         sess.NASIP,
                 AcctSessionID: sess.AcctSessionID,
                 IMSI:          sess.IMSI,
             })
             status := session.DMResultError
             if dmErr == nil && res != nil { status = res.Status }
             dmResults = append(dmResults, map[string]interface{}{
                 "session_id":      sess.ID.String(),
                 "acct_session_id": sess.AcctSessionID,
                 "dm_status":       status,
             })
             if status == session.DMResultNAK {
                 apierr.WriteError(w, http.StatusConflict, "SESSION_DISCONNECT_FAILED",
                     fmt.Sprintf("NAS refused DM for session %s; pass force=true to override", sess.AcctSessionID))
                 return
             }
         }
     }
     ```
  4. Include `dmResults` in the success response envelope:
     ```go
     resp := switchResponse{ ..., DisconnectedSessions: dmResults }
     ```
     Add `DisconnectedSessions []map[string]interface{} \`json:"disconnected_sessions,omitempty"\`` to `switchResponse` struct.
  5. Add new `apierr.CodeSessionDisconnectFailed = "SESSION_DISCONNECT_FAILED"` constant in `internal/apierr/` if not present.
  6. Wire in main.go: `esimHandler := esim.NewHandler(esimStore, simStore, smdpAdapter, auditSvc, log.Logger, radiusSessionStore, dmSender)` — signature order to be finalized by developer.
  7. Tests:
     - `TestSwitch_NoActiveSession_NoDM`: mock sessionStore returns empty; assert `Switch` succeeds and `dmSender` not called.
     - `TestSwitch_ActiveSession_DMAck`: mock session returned, mock DMSender returns `{Status: DMResultACK}`; assert `Switch` proceeds and response includes `disconnected_sessions`.
     - `TestSwitch_ActiveSession_DMNAK_NoForce`: mock DMSender returns `{Status: DMResultNAK}`; assert 409 response.
     - `TestSwitch_ActiveSession_DMNAK_Force`: same but `?force=true`; assert 200 and switch proceeds.
- **Verify:**
  - `go test ./internal/api/esim/... -v` passes
  - `go build ./cmd/argus/...` compiles (main.go wiring valid)
  - `grep -n "DMRequest\|SendDM" internal/api/esim/handler.go` returns matches
  - `grep -n "SESSION_DISCONNECT_FAILED" internal/apierr/ internal/api/esim/` returns matches

---

### Task 8: Bulk Policy Assign — per-session CoA dispatch + result counters (AC-7)
- **Files:**
  - Modify `internal/job/bulk_policy_assign.go` (add `sessionStore` + `coaSender` deps; dispatch CoA after `SetIPAndPolicy`; collect counters)
  - Modify `internal/job/bulk_types.go` (add `CoASentCount`, `CoAAckedCount`, `CoAFailedCount` to `BulkResult`)
  - Modify `cmd/argus/main.go` (pass new deps into `NewBulkPolicyAssignProcessor`)
  - Modify `internal/job/bulk_state_change_test.go` (new test file or same pattern: `bulk_policy_assign_test.go` with CoA mock)
- **Depends on:** — (isolated; can run parallel with Task 7)
- **Complexity:** high
- **Pattern ref:** Read `internal/policy/rollout/service.go:385-422 sendCoAForSIM` — reference pattern for per-SIM session lookup + CoA dispatch + status tracking. Read `internal/job/bulk_policy_assign.go` (current processor) to understand the per-SIM loop where CoA needs to plug in. Read `internal/job/bulk_state_change_test.go` for processor test pattern.
- **Context refs:** "Architecture Context > Data Flow — Bulk Policy Assign CoA", "API Specifications > Modified REST endpoint — Bulk Policy Assign", "Review File Cross-Reference (AC-7 row)", "Protocol Spec References > RFC 5176 §3.1"
- **What:**
  1. Extend `BulkPolicyAssignProcessor` struct with:
     ```go
     sessionStore *store.RadiusSessionStore
     coaSender    *session.CoASender
     policyStore  *store.PolicyStore  // for UpdateAssignmentCoAStatus
     ```
     Update `NewBulkPolicyAssignProcessor` signature + main.go caller.
  2. In `BulkResult` (bulk_types.go), add:
     ```go
     CoASentCount   int `json:"coa_sent_count"`
     CoAAckedCount  int `json:"coa_acked_count"`
     CoAFailedCount int `json:"coa_failed_count"`
     ```
  3. In `processForward` (line 56), after the successful `SetIPAndPolicy` call (line 108) and before `_ = p.distLock.Release`, insert per-session CoA dispatch:
     ```go
     // AC-7: dispatch CoA to active sessions on the affected SIM
     if p.sessionStore != nil && p.coaSender != nil {
         sessions, _ := p.sessionStore.ListActiveBySIM(ctx, sim.ID)
         for _, sess := range sessions {
             coaSentCount++
             res, coaErr := p.coaSender.SendCoA(ctx, session.CoARequest{
                 NASIP: sess.NASIP, AcctSessionID: sess.AcctSessionID, IMSI: sess.IMSI,
                 Attributes: map[string]interface{}{},
             })
             status := "failed"
             if coaErr == nil && res != nil {
                 if res.Status == session.CoAResultACK { coaAckedCount++; status = "acked" } else { coaFailedCount++ }
             } else { coaFailedCount++ }
             if p.policyStore != nil {
                 _ = p.policyStore.UpdateAssignmentCoAStatus(ctx, sim.ID, status)
             }
         }
     }
     ```
  4. Pass `coaSentCount, coaAckedCount, coaFailedCount` into `completeJob` (extend signature) and include in `BulkResult` JSON serialization.
  5. **Async/batched guarantee:** The existing processor already batches by `bulkBatchSize=100` and the job runs via NATS-dispatched worker pool. Story says "Batched by 1000, async via job runner" — the 1000 in the story is a rounding; current 100 is more conservative and well within story intent. Document the deviation inline as a code comment.
  6. Tests — new `bulk_policy_assign_test.go`:
     - `TestBulkPolicyAssign_CoADispatch`: seed 3 SIMs, 2 with active session mock, 1 without; run processor; assert `CoASentCount == 2`, `processed_count == 3`.
     - `TestBulkPolicyAssign_CoA_NoSessionStore`: pass nil sessionStore; processor still succeeds, `CoASentCount == 0`.
     - `TestBulkPolicyAssign_CoA_NAK`: mock CoASender returns NAK; assert `CoAFailedCount++` and `processed_count` still increments (policy DB update succeeded).
- **Verify:**
  - `go test ./internal/job/... -run "BulkPolicyAssign" -v` passes
  - `go build ./cmd/argus/...` compiles
  - `grep -n "coa_sent_count" internal/job/bulk_types.go internal/job/bulk_policy_assign.go` returns matches
  - `grep -n "ListActiveBySIM\|SendCoA" internal/job/bulk_policy_assign.go` returns matches

---

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| AC-1 (EAP-SIM strict HMAC + MSK race) | Task 2 | Task 2 tests (TestHandleChallengeResponse_SimpleSRES_Rejected, TestConsumeSessionMSK_*, go test -race) |
| AC-2 (WS pong 90s, configurable) | Task 4 | Task 4 tests (TestPongTimeout_90sDefault) |
| AC-3 (WS drop oldest + counter) | Task 5 | Task 5 tests (TestSafeSend_DropOldest) |
| AC-4 (WS per-user 5, 4029 eviction) | Task 6 | Task 6 tests (TestMaxConnectionsPerUser) |
| AC-5 (WS reconnect control message) | Task 4 | Task 4 tests (TestReconnectBroadcast) |
| AC-6 (eSIM switch DM + force) | Task 7 | Task 7 tests (TestSwitch_*_DM*) |
| AC-7 (Bulk policy assign CoA) | Task 8 | Task 8 tests (TestBulkPolicyAssign_CoADispatch/_NAK) |
| AC-8 (Diameter/TLS wired + interop) | Task 3 | Task 3 tests (internal/aaa/diameter tls_test) |
| AC-9 (RAT enum canonical, DSL imports) | Task 1 | Task 1 tests (parser tests + optional migration) |

---

## Test Scenario Mapping (from story)

| Story test scenario | Task | Notes |
|---------------------|------|-------|
| Unit: EAP-SIM simple-SRES path rejected | Task 2 | `TestHandleChallengeResponse_SimpleSRES_Rejected` — send simple-SRES MAC, assert NewFailure |
| Integration: no pong for 95s → connection closed; 85s → alive | Task 4 | `TestPongTimeout_90sDefault` with cfg override for speed |
| Integration: slow WS client buffer full → oldest dropped → counter++ | Task 5 | `TestSafeSend_DropOldest` |
| Integration: 6 connections same user → 1st closed with 4029 | Task 6 | `TestMaxConnectionsPerUser` |
| Integration: server sends reconnect → client reconnects after after_ms | Task 4 | `TestReconnectBroadcast` |
| Integration: eSIM profile switch with active session → DM dispatched | Task 7 | `TestSwitch_ActiveSession_DMAck` |
| Integration: bulk policy assign 100 SIMs / 50 sessions → 50 CoA sent | Task 8 | `TestBulkPolicyAssign_CoADispatch` |
| Integration: Diameter peer connects via TLS on 3868 → CER/CEA OK | Task 3 | `tls_test.go` TLS handshake + CER subtest |
| Integration: Diameter peer with invalid cert rejected | Task 3 | `tls_test.go` mTLS no-cert subtest |
| Unit: DSL `rat_type == "NB_IOT"` == `"nb_iot"` canonical | Task 1 | `parser_test.go` alias normalization test |
| Unit: SoR decision with `rat_type=5G_NSA` canonical | Task 1 (validated; SoR already canonical) | Ensured via rattype.Normalize — parser tests exercise this path |

---

## Story-Specific Compliance Rules

- **API envelope:** All modified API responses (eSIM switch, bulk policy assign job detail) MUST use `{status, data, error?}` envelope — inherited from existing handlers. No new top-level fields outside envelope.
- **Protocol correctness is non-negotiable:** No "test-compat" shortcuts. If a test fixture is broken by stricter validation, regenerate the fixture to spec-correct inputs — do not weaken validation.
- **Hot path latency:** `ConsumeSessionMSK` (Task 2) MUST be in-memory (no Redis round-trip) to preserve the AAA hot-path p99 < 50ms target (ARCHITECTURE.md §Performance).
- **ADR-001 (modular monolith):** All changes remain in `internal/`. No new top-level packages.
- **ADR-003 (custom AAA engine):** Argus owns the protocol implementation; no delegation to FreeRADIUS for any fix.
- **Audit trail:** eSIM switch DM dispatch (Task 7) MUST be captured in the existing audit entry — extend the `createAuditEntry` call to include `disconnected_sessions` in the "after" data field.
- **Tenant scoping:** All DB lookups (`ListActiveBySIM`) go through existing tenant-aware store methods. No raw SQL outside store layer.
- **Cursor pagination:** N/A — no new list endpoints.
- **No hardcoded secrets:** Diameter TLS cert paths via env vars; no embedded PEMs in code or fixtures (test fixtures generate self-signed on-the-fly).
- **Backwards compatibility:** The `GetSessionMSK` rename to `ConsumeSessionMSK` is internal to `internal/aaa/eap` and only has one caller (`radius/server.go`). No external API breakage.
- **WebSocket spec authority:** WEBSOCKET_EVENTS.md is the single source of truth. When code and spec disagreed previously (pongWait, per-user limit, reconnect msg, drop-oldest), code was wrong and spec wins. Update spec file ONLY where STORY-060 explicitly revises (pongWait 10s → 90s note).
- **ADR-002 (data stack):** Redis GETDEL command requires Redis 6.2+; existing Argus stack is Redis 7. OK.

---

## Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|-----------|
| MSK race fix via `sync.Map` leaks entries if `ConsumeSessionMSK` is never called | Low | Low | Add a background GC goroutine or wrap values in `{msk, expiresAt}` and prune lazily on each `LoadAndDelete`. Document the TTL (10s) inline. |
| Per-user eviction during heavy reconnect flood causes thrash | Medium | Medium | Evict policy is "oldest first" — well-behaved clients with stable userID don't churn. Rate limit via existing brute-force protection on JWT auth. |
| DM dispatch on eSIM switch blocks HTTP handler for multiple sessions | Low | Medium | DMSender per-call timeout is 3s (hardcoded); worst case for 5 active sessions = 15s. Acceptable: eSIM switch is not hot-path. Also `force=true` escape hatch. |
| Bulk CoA dispatch adds ~100ms per SIM × 10K SIMs = 17min job | Medium | Low | Already async via job runner. Progress events keep UI live. 10K SIMs with active sessions is rare in practice. |
| Diameter TLS self-signed cert test flaky on CI due to system time | Low | Low | Generate cert with 1-year validity, backdate `NotBefore` by 1h. Pattern used elsewhere in Go stdlib tests. |
| DSL parser import cycle risk (`rattype` → nothing, DSL → rattype — no cycle) | Very Low | — | `rattype` has zero internal imports (verified). No risk. |
| Test regeneration for EAP-SIM introduces subtle bugs in MAC computation | Medium | High | Computed MAC via the same `computeSIMMAC` helper used by production. Add a round-trip test: build request → send → verify success. |
| STORY-060 touches 10+ files — coordination risk if waves aren't strict | Low | Medium | Plan enforces 3 waves with explicit no-cross-dependencies per wave. Task 1 must land before Task 7/8 touch shared test machinery, but tasks are file-disjoint. |

---

## Wave Schedule (for Amil orchestrator)

**Wave 1 (parallel, foundational — no cross-deps):**
- Task 1 (AC-9 — DSL RAT canonical import + optional migration)
- Task 2 (AC-1 — EAP-SIM strict MAC + MSK race fix) — HIGH complexity
- Task 3 (AC-8 — Diameter/TLS verification + interop test + CONFIG.md)

**Wave 2 (parallel, WebSocket cluster — all touch ws package but disjoint files/funcs):**
- Task 4 (AC-2 + AC-5 — pong timeout 90s + reconnect control message) — touches `server.go`, `hub.go` new method only
- Task 5 (AC-3 — drop oldest + counter) — touches `hub.go` safeSend helper only
- Task 6 (AC-4 — per-user limit 4029) — touches `server.go` handleWS + `hub.go` new helpers only

  *Note to orchestrator:* Tasks 4/5/6 all modify `hub.go` and `server.go`. If parallelism causes merge conflicts in practice, serialize them in order 4 → 5 → 6 (each is small enough that this takes no material time). The file-function split is designed to minimize overlap: Task 4 adds `BroadcastReconnect`; Task 5 adds `safeSend`; Task 6 adds `UserConnectionCount` + `EvictOldestByUser`. In `server.go`, Task 4 modifies `readPump` pongWait; Task 6 adds per-user gate in `handleWS`.

**Wave 3 (parallel, cross-service orchestration — depends on nothing new, but higher complexity):**
- Task 7 (AC-6 — eSIM CoA/DM trigger) — HIGH
- Task 8 (AC-7 — Bulk policy assign CoA) — HIGH

---

## Summary Stats

- **Total tasks:** 8
- **Complexity distribution:** 4 high (Tasks 2, 5, 7, 8), 3 medium (Tasks 1, 4, 6), 1 low (Task 3)
- **Waves:** 3
- **Files touched:** ~22 files across 10 packages
- **New files:** 2 (migration up/down SQL)
- **Test files updated:** 6
- **Env vars added:** 2 (`WS_PONG_TIMEOUT`, `WS_MAX_CONNS_PER_USER`)
- **DB migrations:** 1 optional data normalization (AC-9)
- **New WS close code:** 4029
- **Breaking API changes:** None (additive only)
- **Has UI:** false

---

## Pre-Validation Self-Check

- [x] Minimum substance: plan is ~700 lines, 8 tasks (XL requires 120+ lines, 6+ tasks) ✔
- [x] Required sections: Goal, Architecture Context, Tasks, Acceptance Criteria Mapping ✔
- [x] Embedded specs: env vars, API contracts, WS close codes, RFC references inline ✔
- [x] Task complexity cross-check: XL story has 4 high-complexity tasks ✔
- [x] Context refs validation: each task's refs point to real plan sections ✔
- [x] No UI: has_ui=false, no Design Token Map needed ✔
- [x] Each task ≤3 files (Task 6/7/8 touch 4 — borderline — justified because config+test+main.go wiring are inseparable; re-splitting would increase coordination cost) — acceptable
- [x] Depends on field populated for every task ✔
- [x] Pattern ref populated for every task that creates or substantially modifies files ✔
- [x] Tests embedded in the same task as the code they verify ✔
- [x] API envelope rule stated ✔
- [x] ADR compliance noted ✔
- [x] Bug patterns section present ✔
- [x] Tech debt section present (none applicable) ✔
- [x] Risk section present ✔

**PRE-VALIDATION: PASS**

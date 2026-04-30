# Implementation Plan: STORY-083 — Simulator Diameter Client (Gx/Gy)

> Plan revised 2026-04-17 after code-state validation. Material drift from
> 2026-04-14 plan: Argus uses a **native** Diameter implementation
> (`internal/aaa/diameter`), not `fiorix/go-diameter`. The simulator MUST
> reuse Argus's own AVP/Message codecs (same package, imported as a library
> from the simulator side) — this guarantees byte-for-byte compatibility
> with the Argus server (codes, flags, vendor IDs all match) and removes
> the dictionary-XML risk that was the #1 risk in the prior plan.

## Goal

Extend the simulator (STORY-082) with a Diameter client that exercises
Argus's Gx (Policy Control, Application-ID 16777238) and Gy (Online
Charging, Application-ID 4) interfaces. For each session belonging to a
Diameter-enabled operator, the simulator issues:

```
RADIUS Access-Accept ──► Gx CCR-I  (install policy)
                  └────► Gy CCR-I  (open credit-control session)
RADIUS Acct-Interim ──► Gy CCR-U  (report usage, ask for next quota)
RADIUS Acct-Stop    ──► Gx CCR-T  +  Gy CCR-T
```

Selection is config-driven per operator. Default = RADIUS-only (no
behavior change vs STORY-082); operators opt into Diameter via
`operators[].diameter.enabled: true`. Peer loss falls back to RADIUS-only
for that operator's SIMs and surfaces in metrics — Diameter never blocks
RADIUS.

## Architecture Context

### Components Involved

**Verified disk layout BEFORE this story (2026-04-17):**

```
cmd/simulator/
  main.go                       # entry, env guard, orchestrator
  Dockerfile
  config.example.yaml
internal/simulator/
  config/{config.go, config_test.go}
  discovery/db.go               # PG read-only SIM/operator/APN fetch
  scenario/{scenario.go, scenario_test.go}
  radius/{client.go, client_test.go}
  engine/engine.go
  metrics/metrics.go
deploy/simulator/{Dockerfile, config.example.yaml}
deploy/docker-compose.simulator.yml
```

**NEW under `internal/simulator/diameter/`:**

```
diameter/
  client.go         # one TCP connection per (operator, peer); CER/DWR
                    # lifecycle, hop-by-hop / end-to-end ID rolling, send+
                    # match-reply API, auto-reconnect with backoff.
  client_test.go
  ccr.go            # Gx & Gy CCR builders. Uses argus's AVP helpers.
  ccr_test.go       # Encoded-bytes golden tests for CCR-I, CCR-U, CCR-T.
  peer.go           # Peer struct (state machine: Closed → Connecting →
                    # WaitCEA → Open → Closing); watchdog (DWR every
                    # WatchdogInterval); reconnect on transport error.
  peer_test.go
  doc.go            # Package overview.
```

**MODIFIED:**

```
internal/simulator/config/config.go      # add DiameterConfig (per-op),
                                         # add operator-level DiameterEnabled,
                                         # add DiameterRate, validate.
internal/simulator/config/config_test.go # cover new fields incl. defaults.
internal/simulator/engine/engine.go      # bracket session lifecycle with
                                         # Diameter calls when enabled.
internal/simulator/metrics/metrics.go    # add 4 Diameter metric vectors.
cmd/simulator/main.go                    # construct per-operator Diameter
                                         # clients, pass into engine, drain
                                         # on shutdown.
deploy/simulator/config.example.yaml     # demonstrate one operator opt-in,
                                         # document defaults.
docs/architecture/simulator.md           # add Diameter section (created
                                         # if missing).
```

### Reuse strategy — CRITICAL

The simulator's diameter package **imports** the Argus native diameter
package and reuses its primitives:

```go
import argusdiameter "github.com/btopcu/argus/internal/aaa/diameter"
```

We use the following exported symbols (already public in
`internal/aaa/diameter/`):

| Symbol | Source file | Purpose |
|--------|-------------|---------|
| `AVP`, `NewAVPUint32`, `NewAVPUint64`, `NewAVPString`, `NewAVPGrouped`, `NewAVPAddress` | `avp.go` | Encode AVPs |
| `BuildSubscriptionID(imsi, msisdn string) []*AVP` | `avp.go` | IMSI/MSISDN sub-id |
| `Message`, `Encode`, `DecodeMessage`, `NewRequest`, `MsgFlagRequest` | `message.go` | Wire format |
| `CommandCER`, `CommandCEA`, `CommandDWR`, `CommandDWA`, `CommandCCR`, `CommandCCA`, `CommandDPR`, `CommandDPA` | `message.go` | Cmd codes |
| `ApplicationIDGx`, `ApplicationIDGy`, `ApplicationIDDiameterBase` | `avp.go` | App IDs |
| `AVPCodeSessionID`, `AVPCodeOriginHost`, `AVPCodeOriginRealm`, `AVPCodeDestinationRealm`, `AVPCodeAuthApplicationID`, `AVPCodeCCRequestType`, `AVPCodeCCRequestNumber`, `AVPCodeUsedServiceUnit`, `AVPCodeRequestedServiceUnit`, `AVPCodeCCInputOctets`, `AVPCodeCCOutputOctets`, `AVPCodeCCTotalOctets`, `AVPCodeCCTime`, `AVPCodeRATType3GPP`, `AVPCodeIPCANType`, `AVPCodeFramedIPAddress` (use 8 = stock Diameter NAS) | `avp.go` | AVP codes |
| `CCRequestTypeInitial`, `CCRequestTypeUpdate`, `CCRequestTypeTermination` | `avp.go` | CCR types |
| `ResultCodeSuccess` | `avp.go` | Compare CCA result |
| `VendorID3GPP` | `avp.go` | Vendor flag value |
| `ReadMessageLength`, `DiameterHeaderLen` | `message.go` | Wire framing |
| `AVPFlagMandatory`, `AVPFlagVendor` | `avp.go` | Flags |

**No third-party Diameter library is needed.** `fiorix/go-diameter` is
explicitly NOT added to go.mod.

### Data Flow (per session, when Diameter enabled for the operator)

```
engine.runSession (modified)
  │
  ├── radius.Auth ──────────── Argus :1812 (existing)
  │      └─ Access-Accept (sc.FramedIP captured)
  │
  ├── diameter.OpenSession(sc) ◄── NEW
  │      ├── Gx CCR-I  ──────► Argus :3868 ──► Gx CCA-I (Result=2001 expected)
  │      └── if Gy app enabled:
  │             Gy CCR-I  ──► Gy CCA-I (Granted-Service-Unit returned)
  │      └─ on error: log, mark session "diameter_failed", abort engine session
  │             (no Acct-Start sent — symmetric with existing radius Auth-fail
  │             handling)
  │
  ├── radius.AcctStart ──────── Argus :1813 (existing)
  │
  ├── interim loop:
  │      ├── radius.AcctInterim (existing)
  │      └── if Gy enabled:
  │             diameter.UpdateGy(sc)  ◄── NEW
  │              CCR-U (Used-Service-Unit = bytesIn/Out delta since last)
  │
  ├── radius.AcctStop ───────── Argus :1813 (existing)
  │
  └── diameter.CloseSession(sc) ◄── NEW
         ├── Gx CCR-T (final)
         └── Gy CCR-T (final Used-Service-Unit)
```

The Diameter peer (one TCP connection per operator) lives independent of
session goroutines: it is opened at simulator startup, watchdog'd with DWR
every `WatchdogInterval`, reconnected on transport failure (exponential
backoff, capped at 30s). Sessions that need to send a CCR while peer is
not Open get an immediate error and skip Diameter (RADIUS still happens).

### Argus Diameter Server — Source-of-Truth (read for compatibility)

| File | Use during planning |
|------|--------------------|
| `internal/aaa/diameter/server.go` | CER/CEA shape, port, peer lifecycle |
| `internal/aaa/diameter/avp.go`    | AVP codes, vendor IDs, helpers |
| `internal/aaa/diameter/message.go`| Wire format, command codes |
| `internal/aaa/diameter/gx.go`     | Expected Gx CCR fields & error paths |
| `internal/aaa/diameter/gy.go`     | Expected Gy CCR fields & error paths |
| `internal/aaa/diameter/diameter_test.go` | Reference for golden-byte tests |

Key facts pulled out:
- Port: `DIAMETER_PORT` env, default `3868` (`internal/config/config.go:59`)
- Origin-Host/Realm of server: `DIAMETER_ORIGIN_HOST` / `DIAMETER_ORIGIN_REALM` env
- Server CEA advertises BOTH Gx and Gy app IDs unconditionally (`server.go:382-383`)
- Server requires Origin-Host + Origin-Realm AVPs on every request (`server.go:367-374`)
- Gx CCR-I requires: Session-ID, Subscription-Id (IMSI), CC-Request-Type, CC-Request-Number — see `gx.go:34-47, 70-73`
- Gy CCR-U: Session-Id + Used-Service-Unit (octets pulled by `ExtractUsedServiceUnit`, `avp.go:345-371`)
- Application-ID Gx = 16777238, Gy = 4 (`avp.go:102-104`)

### Config schema — additive

Source: STORY-082 `internal/simulator/config/config.go` (current truth).
Add the following struct fields. **No removals.**

```go
// Top-level — default set sane so existing config files keep working.
type Config struct {
    Argus     ArgusConfig
    Operators []OperatorConfig
    Scenarios []ScenarioConfig
    Rate      RateConfig
    Metrics   MetricsConfig
    Log       LogConfig
    Diameter  DiameterDefaults `yaml:"diameter"` // NEW — global defaults
}

// NEW.
type DiameterDefaults struct {
    Host                string        `yaml:"host"`                  // default "argus-app"
    Port                int           `yaml:"port"`                  // default 3868
    OriginRealm         string        `yaml:"origin_realm"`          // default "sim.argus.test"
    DestinationRealm    string        `yaml:"destination_realm"`     // default "argus.local" — must match server's DIAMETER_ORIGIN_REALM
    WatchdogInterval    time.Duration `yaml:"watchdog_interval"`     // default 30s
    ConnectTimeout      time.Duration `yaml:"connect_timeout"`       // default 5s
    RequestTimeout      time.Duration `yaml:"request_timeout"`       // default 5s
    ReconnectBackoffMin time.Duration `yaml:"reconnect_backoff_min"` // default 1s
    ReconnectBackoffMax time.Duration `yaml:"reconnect_backoff_max"` // default 30s
}

// EXTEND OperatorConfig — additive.
type OperatorConfig struct {
    Code          string                 `yaml:"code"`
    NASIdentifier string                 `yaml:"nas_identifier"`
    NASIP         string                 `yaml:"nas_ip"`
    Diameter      *OperatorDiameterConfig `yaml:"diameter,omitempty"` // NEW; nil = disabled
}

// NEW.
type OperatorDiameterConfig struct {
    Enabled      bool     `yaml:"enabled"`           // master switch
    OriginHost   string   `yaml:"origin_host"`       // default "sim-{operator-code}.{origin_realm}"
    Applications []string `yaml:"applications"`      // subset of {"gx","gy"}; default ["gx","gy"]
}
```

Validation rules (added to `Config.Validate`):
- If any operator has `Diameter.Enabled: true`, `Diameter.DestinationRealm` must be non-empty.
- `Diameter.Port` defaults to 3868; `Diameter.WatchdogInterval` defaults to 30s.
- For each enabled operator, default `Applications` to `["gx","gy"]` and `OriginHost` to `sim-<code>.<origin-realm>` (kebab-case the operator code).
- Unknown application names are an error (only `gx` and `gy` accepted).

### Engine integration

- New field on `Engine`: `dm map[string]*diameter.Client` keyed by operator code (only for enabled operators).
- `Engine.New` accepts an extra `dmClients map[string]*diameter.Client`.
- `runSession`: after Access-Accept, if `e.dm[op.Code] != nil`:
  1. Call `dm.OpenSession(sessionCtx, sc, sample)` — sends Gx CCR-I, then (if Gy enabled) Gy CCR-I.
  2. On error → metric `simulator_diameter_session_aborted_total{operator,reason}` increment, return BEFORE Acct-Start.
- After each Acct-Interim, if Gy is enabled, call `dm.UpdateGy(sessionCtx, sc, deltaIn, deltaOut)`. Failure is non-fatal (logged + metric).
- At session end (after Acct-Stop), call `dm.CloseSession(stopCtx, sc)` regardless of cause; failures non-fatal but counted.
- One Diameter session-id per simulator session: reuse `sc.AcctSessionID` so Argus's session manager correlates RADIUS + Diameter (server uses `AcctSessionID` as the diameter `Session-Id` too — see `gx.go:81`).

### Metrics — additions

```go
// All carry operator + app (gx|gy) labels.
DiameterRequestsTotal *prometheus.CounterVec   // labels: operator, app, type (ccr_i|ccr_u|ccr_t|cer|dwr)
DiameterResponsesTotal *prometheus.CounterVec  // labels: operator, app, result (success|error_<code>|timeout|peer_down)
DiameterLatencySeconds *prometheus.HistogramVec // labels: operator, app, type — buckets like RADIUS
DiameterPeerState *prometheus.GaugeVec         // labels: operator — 0=closed,1=connecting,2=wait_cea,3=open
DiameterSessionAbortedTotal *prometheus.CounterVec // labels: operator, reason — reasons: ccr_i_failed|peer_down|timeout|reject
```

### Wire-format details (all derived from `internal/aaa/diameter/`)

**CER** (cmd 257, app-id 0):
- Origin-Host (operator's `origin_host`)
- Origin-Realm (config `origin_realm`)
- Host-IP-Address (use 0.0.0.0 placeholder, mirrors server.go:379)
- Vendor-Id (use simulator-specific value, e.g. `99999` to match server)
- Product-Name (`"argus-simulator"`)
- Auth-Application-Id Gx (16777238) — only if op has gx enabled
- Auth-Application-Id Gy (4) — only if op has gy enabled
- Supported-Vendor-Id (10415 = 3GPP) — only if any 3GPP AVPs are sent
- Firmware-Revision (1)

Expect CEA with `Result-Code = 2001` (success). On any other code → log error and let peer enter Closed; reconnect path will retry.

**Gx CCR-I** (cmd 272, app-id 16777238, request flag set):
- Session-Id = `sc.AcctSessionID` (UUID string from session_context)
- Origin-Host, Origin-Realm
- Destination-Realm (from config)
- Auth-Application-Id (16777238)
- CC-Request-Type = `CCRequestTypeInitial` (1)
- CC-Request-Number = 0
- Subscription-Id grouped (use `argusdiameter.BuildSubscriptionID(imsi, msisdn)`)
- Framed-IP-Address (`AVPCodeFramedIPAddress` = code 8) — from `sc.FramedIP`
- IP-CAN-Type vendor 3GPP = `1` (3GPP-GPRS) — vendor-mandatory
- RAT-Type vendor 3GPP = `1004` (EUTRAN) — vendor-mandatory

**Gy CCR-I** (cmd 272, app-id 4):
- Same base AVPs as Gx CCR-I
- Auth-Application-Id (4)
- Requested-Service-Unit grouped: `CC-Total-Octets = 100*1024*1024` (matches server default `DefaultGrantedOctets`)

**Gy CCR-U** (cmd 272, app-id 4, type Update):
- Session-Id, Origin-Host/Realm, Destination-Realm, Auth-App-Id (4)
- CC-Request-Type = `CCRequestTypeUpdate` (2)
- CC-Request-Number = N (monotonically increasing per session)
- Subscription-Id (IMSI)
- Used-Service-Unit grouped: CC-Input-Octets = `delta_in`, CC-Output-Octets = `delta_out`, CC-Time = seconds since last update
- Requested-Service-Unit (request next chunk)

**CCR-T** (both apps, cmd 272):
- Same as CCR-U but `CC-Request-Type = CCRequestTypeTermination` (3) and final Used-Service-Unit deltas.

### Safety Envelope (additive to STORY-082)

- **Per-operator opt-in** (`diameter.enabled: false` by default in example config) — no risk to RADIUS-only consumers.
- **Peer lifecycle isolation** — peer goroutine independent from session goroutines; transport failures never block RADIUS.
- **Bounded TCP usage** — one connection per operator (~3 in current seed), no per-session connections.
- **Strict shutdown order** — `Engine.Run` returns only after all sessions drained; `main.go` then closes Diameter clients (sends DPR, then closes TCP).
- **Single-binary build** — `cmd/simulator/Dockerfile` unchanged (no build args, no new system deps).
- **Boot guard preserved** — `SIMULATOR_ENABLED` env still required.

## Tasks

### Wave 1 — Foundation (config + scaffolding)

> Independent except where noted. Wave 1 must complete before Wave 2.

#### Task 1: Extend simulator config schema for Diameter
- **Files:** Modify `internal/simulator/config/config.go`, Modify `internal/simulator/config/config_test.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/simulator/config/config.go` (current schema + Validate) and `internal/simulator/config/config_test.go` (table-driven validation tests)
- **Context refs:** "Architecture Context > Config schema — additive"
- **What:**
  - Add `DiameterDefaults` struct + `Diameter` field on `Config`.
  - Add `OperatorDiameterConfig` + `Diameter *OperatorDiameterConfig` on `OperatorConfig`.
  - Defaults applied in `Validate`: port 3868, watchdog 30s, connect/request timeouts 5s, backoff 1s/30s, applications `["gx","gy"]`, `OriginHost = sim-<kebab(code)>.<origin_realm>`.
  - Reject unknown application names.
  - If any op has Diameter enabled → require `Diameter.DestinationRealm` non-empty (so blank-default doesn't silently break).
  - Tests: defaults applied, unknown app rejected, missing realm rejected, RADIUS-only config still validates.
- **Verify:** `cd /Users/btopcu/workspace/argus && go test ./internal/simulator/config/...`

#### Task 2: Diameter peer state machine + reconnect
- **Files:** Create `internal/simulator/diameter/peer.go`, Create `internal/simulator/diameter/peer_test.go`, Create `internal/simulator/diameter/doc.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/aaa/diameter/server.go` (`Peer` struct + `PeerState` enum) for state names; read `internal/simulator/radius/client.go` for the Argus-style options + retry pattern.
- **Context refs:** "Architecture Context > Reuse strategy — CRITICAL", "Architecture Context > Wire-format details" (only CER/CEA/DWR sections), "Architecture Context > Safety Envelope"
- **What:**
  - `Peer` struct: holds operator code, origin-host, origin-realm, dest-realm, host:port, app-ids enabled, watchdog interval, backoff config, and a `PeerState` (Closed | Connecting | WaitCEA | Open | Closing).
  - `(p *Peer) Run(ctx)` goroutine: connect → send CER → wait CEA → on success transition to Open and start watchdog → on transport error or DWR timeout transition to Closed and back-off + reconnect.
  - `(p *Peer) Send(ctx, msg) (*Message, error)`: only succeeds when Open; otherwise returns `ErrPeerNotOpen`. Uses hop-by-hop ID to correlate replies; pending-request map cleaned on context done or timeout.
  - Watchdog: send DWR every `WatchdogInterval`, expect DWA — failure transitions Closed.
  - `(p *Peer) Close()` sends DPR, waits DPA (1s deadline), closes conn.
  - Tests: state transitions (use a fake net.Listener in-process to drive CER/CEA/DWR/DWA/DPR/DPA exchange), reconnect after EOF, `Send` rejects when not Open.
- **Verify:** `go test ./internal/simulator/diameter/... -run TestPeer`

#### Task 3: Diameter metrics
- **Files:** Modify `internal/simulator/metrics/metrics.go`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `internal/simulator/metrics/metrics.go` — add new vectors next to existing ones, register in `MustRegister`.
- **Context refs:** "Architecture Context > Metrics — additions"
- **What:** Add `DiameterRequestsTotal`, `DiameterResponsesTotal`, `DiameterLatencySeconds`, `DiameterPeerState`, `DiameterSessionAbortedTotal` with the exact label sets specified. Wire into `MustRegister`.
- **Verify:** `go build ./...` and confirm `/metrics` endpoint exposes new families when simulator is started locally with at least one operator's Diameter enabled.

### Wave 2 — Integration (CCR builders + client + engine wiring)

> Wave 2 starts after Wave 1 tasks 1+2 are merged.

#### Task 4: CCR builders (Gx + Gy)
- **Files:** Create `internal/simulator/diameter/ccr.go`, Create `internal/simulator/diameter/ccr_test.go`
- **Depends on:** Task 2 (uses `peer.go` types? — no, builders are pure; but lives in same package, so order this AFTER Task 2 to avoid import cycles)
- **Complexity:** medium
- **Pattern ref:** Read `internal/aaa/diameter/avp.go` (helpers `BuildSubscriptionID`, `NewAVPGrouped`, `NewAVPAddress`) and `internal/aaa/diameter/gx.go` (server-side AVP order) to mirror what server expects.
- **Context refs:** "Architecture Context > Reuse strategy — CRITICAL", "Architecture Context > Wire-format details"
- **What:**
  - `BuildGxCCRI(sc *radius.SessionContext, originHost, originRealm, destRealm string, hopID, endID uint32) *argusdiameter.Message`
  - `BuildGxCCRT(sc, ..., reqNum uint32) *argusdiameter.Message`
  - `BuildGyCCRI(sc, ..., requestedOctets uint64) *argusdiameter.Message`
  - `BuildGyCCRU(sc, ..., reqNum uint32, deltaIn, deltaOut uint64, deltaSec uint32) *argusdiameter.Message`
  - `BuildGyCCRT(sc, ..., reqNum uint32, finalIn, finalOut uint64, finalSec uint32) *argusdiameter.Message`
  - All builders use ONLY `argusdiameter.NewAVP*` helpers. Framed-IP-Address uses `AVPCodeFramedIPAddress` = 8 (constant must be added if not exported — verify in `internal/aaa/diameter/avp.go`; if missing, define as a local const referencing IETF RFC 6733).
  - Tests: golden-byte tests for each builder with a deterministic SessionContext; assert decoded message has expected AVP codes & values via `argusdiameter.DecodeMessage`.
- **Verify:** `go test ./internal/simulator/diameter/... -run TestBuildGx -run TestBuildGy`

#### Task 5: Diameter Client (high-level façade)
- **Files:** Create `internal/simulator/diameter/client.go`, Create `internal/simulator/diameter/client_test.go`
- **Depends on:** Task 2, Task 4
- **Complexity:** high
- **Pattern ref:** Read `internal/simulator/radius/client.go` (struct + per-action method shape) for layout, and `internal/aaa/diameter/server.go` lines 333-455 to understand reply matching (Hop-by-Hop ID).
- **Context refs:** "Architecture Context > Reuse strategy — CRITICAL", "Architecture Context > Data Flow", "Architecture Context > Engine integration", "Architecture Context > Wire-format details"
- **What:**
  - `Client` struct: holds one `*Peer` (started in `New`), the operator's per-config (origin-host, dest-realm, app set), atomic counters for hop-by-hop / end-to-end / CC-Request-Number-per-session, metrics handles.
  - `New(cfg config.OperatorConfig, defaults config.DiameterDefaults, logger zerolog.Logger) *Client` — wires up Peer, returns Client; does NOT yet connect (caller invokes `Start(ctx)`).
  - `Start(ctx)` spawns peer goroutine; returns `<-chan struct{}` closed when peer reaches Open for the first time (or returns an error after `ConnectTimeout`).
  - `OpenSession(ctx, sc) error` — Gx CCR-I, then (if Gy enabled) Gy CCR-I; tracks `Session-Id → ccrNum` in a per-session counter map.
  - `UpdateGy(ctx, sc, deltaIn, deltaOut, deltaSec uint32) error` — Gy CCR-U with monotonic ccrNum.
  - `CloseSession(ctx, sc) error` — Gx CCR-T + Gy CCR-T; tear-down session counter entry.
  - `Stop(ctx)` — DPR + close peer, wait drained.
  - All Send paths: increment `DiameterRequestsTotal`, observe `DiameterLatencySeconds`, classify response into `DiameterResponsesTotal{result}`. Result-Code `2001` → `success`; other → `error_<code>`; transport timeout → `timeout`; peer-not-open → `peer_down` (and `DiameterSessionAbortedTotal{reason=peer_down}`).
  - Tests: in-process mock listener that accepts CER/answers CEA/answers CCR (use `argusdiameter.DecodeMessage` to inspect inbound). Verify `OpenSession` sends correct CCR-I, `UpdateGy` increments ccrNum, `CloseSession` sends CCR-T, peer-down path returns sentinel error.
- **Verify:** `go test ./internal/simulator/diameter/... -run TestClient`

#### Task 6: Engine integration (conditional Diameter bracketing)
- **Files:** Modify `internal/simulator/engine/engine.go`, Modify `cmd/simulator/main.go`
- **Depends on:** Task 1, Task 5
- **Complexity:** high
- **Pattern ref:** Read existing `engine.go` `runSession` to see how to insert pre/post hooks WITHOUT disturbing the existing label vocabulary or shutdown order.
- **Context refs:** "Architecture Context > Data Flow", "Architecture Context > Engine integration"
- **What:**
  - Engine: add `dm map[string]*diameter.Client` (operator code → client). Add to `Engine.New(cfg, picker, radiusClient, dmClients, logger)`.
  - In `runSession`:
    - After `Access-Accept`: `if dm := e.dm[sim.OperatorCode]; dm != nil { if err := dm.OpenSession(sessionCtx, sc); err != nil { /* metric inc + return */ } }`
    - Inside interim loop, after `AcctInterim` success: `if dm enabled and Gy in apps { dm.UpdateGy(...) }` — non-fatal on error.
    - In the deferred stop block (label `stop:`), call `dm.CloseSession(stopCtx, sc)` after the final RADIUS Acct-Stop.
  - `cmd/simulator/main.go`:
    - For each operator with `Diameter.Enabled`, build `*diameter.Client` and call `Start(ctx)`. Pass map into `engine.New`.
    - On shutdown (after `eng.Run` returns and metrics shutdown queued), iterate over clients and call `Stop(shutdownCtx)`.
  - No new imports of third-party libs.
- **Verify:**
  - `go build ./cmd/simulator`
  - `go test ./internal/simulator/engine/...` (must not regress)

### Wave 3 — Tests, integration, deploy, docs

> Wave 3 starts after Wave 2 merged.

#### Task 7: End-to-end integration test (simulator ↔ Argus diameter server)
- **Files:** Create `internal/simulator/diameter/integration_test.go` (build-tag `integration`)
- **Depends on:** Task 5, Task 6
- **Complexity:** medium
- **Pattern ref:** Read `internal/aaa/diameter/diameter_test.go` for how the package boots an in-process Argus diameter server (`NewServer` + `Start`).
- **Context refs:** "Architecture Context > Reuse strategy — CRITICAL", "Architecture Context > Argus Diameter Server"
- **What:**
  - Spin up `argusdiameter.NewServer` on a free port with stub `SIMResolver` returning a known SIM by IMSI.
  - Build a simulator `diameter.Client` pointed at that port, call `Start`, then `OpenSession(ctx, sc)`, `UpdateGy(...)`, `CloseSession(...)`.
  - Assert: server sees Gx CCR-I (via `SessionStateMap` count == 1 after open), Gy CCR-U updates session counters, CCR-T removes the session.
  - Run with `go test -tags=integration ./internal/simulator/diameter/...` — skipped in default `go test ./...` so we don't slow CI.
- **Verify:** `go test -tags=integration -run TestSimulator_AgainstArgusDiameter ./internal/simulator/diameter/...`

#### Task 8: Example config + compose smoke
- **Files:** Modify `deploy/simulator/config.example.yaml`, (optional) Modify `Makefile` if a `sim-diameter-up` helper is desired
- **Depends on:** Task 1
- **Complexity:** low
- **Pattern ref:** Existing `deploy/simulator/config.example.yaml` — keep comment style.
- **Context refs:** "Architecture Context > Config schema — additive"
- **What:**
  - Add a top-level `diameter:` block with all defaults explicitly listed (so operators see the knobs).
  - Add `diameter:` opt-in to the `turkcell` operator entry only (others remain RADIUS-only). Show `applications: [gx, gy]`.
  - Document in comments that destination-realm must equal Argus `DIAMETER_ORIGIN_REALM`.
- **Verify:**
  - YAML parses (`yq eval . deploy/simulator/config.example.yaml > /dev/null`).
  - `make sim-build` succeeds.

#### Task 9: Architecture doc — Diameter section
- **Files:** Create or modify `docs/architecture/simulator.md`
- **Depends on:** Task 5, Task 6
- **Complexity:** low
- **Pattern ref:** If `docs/architecture/simulator.md` exists, follow its structure; otherwise mirror the heading style of `docs/architecture/PROTOCOLS.md`.
- **Context refs:** "Architecture Context > Components Involved", "Architecture Context > Data Flow", "Architecture Context > Reuse strategy — CRITICAL"
- **What:**
  - Add a "Diameter Client" subsection covering: opt-in semantics, per-operator config, Gx + Gy CCR ordering, peer lifecycle, metrics, failure modes (peer down → RADIUS only), why we reused `internal/aaa/diameter` instead of pulling `fiorix/go-diameter`.
- **Verify:** `markdownlint docs/architecture/simulator.md` (if linter present), otherwise visual.

## Acceptance Criteria

(8 ACs preserved from 2026-04-14 plan; lightly clarified to match the
package layout and metric names finalized above.)

- **AC-1** Simulator establishes a Diameter peer (CER → CEA → Open) within `ConnectTimeout + 5s` of startup for each operator with `diameter.enabled: true`. Verified by `simulator_diameter_peer_state{operator=...} == 3` (Open) within the deadline.
- **AC-2** Every session belonging to a Diameter-enabled operator emits exactly one Gx CCR-I before Accounting-Start and one Gx CCR-T after Accounting-Stop. Verified by `simulator_diameter_requests_total{operator,app="gx",type="ccr_i"} == sessionsStarted` and `{type="ccr_t"} == sessionsCompleted` after a 2-min run.
- **AC-3** Gy-enabled operators additionally emit one Gy CCR-U per Accounting-Interim and a final Gy CCR-T. Verified by `simulator_diameter_requests_total{app="gy",type="ccr_u"} ≥ interimsCompleted` after a 2-min run.
- **AC-4** Argus's `GET /api/v1/cdrs?protocol=diameter` (or equivalent — verify exact path is unchanged in current API) returns a non-empty page after a 2-minute sim-up window for at least one Diameter-enabled operator.
- **AC-5** Disabling Diameter for an operator (`diameter.enabled: false` or omitted) falls back to RADIUS-only lifecycle — `simulator_diameter_requests_total{operator=<that op>}` remains 0; STORY-082 ACs unaffected.
- **AC-6** After killing argus-app for ≥ 30s and restarting, every previously-Open peer reaches Open again within `ReconnectBackoffMax + WatchdogInterval` of argus-app readiness. Verified by `simulator_diameter_peer_state` returning to 3.
- **AC-7** `/metrics` endpoint exposes `simulator_diameter_requests_total`, `_responses_total`, `_latency_seconds`, `_peer_state`, `_session_aborted_total` with non-zero values during a 2-min run with at least one Diameter-enabled operator.
- **AC-8** No regression in STORY-082 ACs: `go test ./internal/simulator/...` passes, RADIUS-only config still produces identical output (zero diameter metrics, identical RADIUS metric counts).

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| AC-1 | Task 2 (peer), Task 5 (Start) | Task 5 unit test, Task 7 integration |
| AC-2 | Task 4 (Gx builders), Task 5 (OpenSession/CloseSession), Task 6 (engine bracket) | Task 5 unit, Task 7 integration |
| AC-3 | Task 4 (Gy CCR-U), Task 5 (UpdateGy), Task 6 (interim hook) | Task 5 unit, Task 7 integration |
| AC-4 | Task 6 (lifecycle wiring) | Task 7 integration + manual sim-up smoke |
| AC-5 | Task 1 (default-off), Task 6 (nil check) | Task 1 config tests + Task 6 engine-test (no nil deref) |
| AC-6 | Task 2 (reconnect+backoff) | Task 2 unit (transport-fail path), Task 7 integration (kill+restart) |
| AC-7 | Task 3 (metrics) | Task 7 integration scrape |
| AC-8 | Task 1 (additive), Task 6 (nil-guarded) | `go test ./internal/simulator/...` |

## Story-Specific Compliance Rules

- **No new third-party dependencies.** `fiorix/go-diameter` MUST NOT be added to `go.mod`. Reuse `internal/aaa/diameter`.
- **API:** Standard envelope rule does not apply — no HTTP endpoints introduced.
- **DB:** No migrations.
- **Naming:** Go camelCase, files snake_case under `internal/simulator/diameter/`. Match STORY-082's package layout exactly.
- **Boot guard:** `SIMULATOR_ENABLED` must remain mandatory (no change).
- **Per-operator opt-in default = false**; explicit per-operator enable required.
- **Tenant context:** the simulator authenticates by IMSI; tenant binding happens server-side via `SIMResolver`. No tenant headers needed in Diameter CCRs (server's `BuildSubscriptionID` carries IMSI).
- **STORY-086 (boot schema guard):** unrelated; no schema changes by this story.

## Bug Pattern Warnings

- **Hop-by-Hop / End-to-End ID collision:** Hop-by-Hop must be unique per outstanding request per peer; End-to-End globally-unique-ish for de-dup. Use atomic counters seeded from `time.Now().UnixNano()` (mirrors `server.go:139-140`). Tests must verify two consecutive requests have different Hop-by-Hop IDs.
- **AVP padding:** Diameter AVPs are 4-byte aligned; rely on `argusdiameter.AVP.Encode` (does padding). Don't hand-roll bytes — would break decode on the server.
- **Watchdog races:** DWR/DWA bookkeeping must be done under the peer's mutex. Pattern: send DWR, set `dwrInFlight=true`, on DWA clear; if next tick fires with `dwrInFlight==true`, treat as failure.
- **Session-Id reuse for Gx vs Gy:** Argus server stores Gx and Gy in the same `SessionStateMap` keyed by Session-Id. Reusing `sc.AcctSessionID` for both apps is intended; do NOT mint two ids.
- **Premature close on context cancel mid-Open:** When ctx cancels during a CCR send, ensure pending-request entry is cleaned to avoid leaking goroutines waiting forever for an answer.

## Tech Debt (from ROUTEMAP)

No tech debt items target STORY-083 specifically as of 2026-04-17.

## Mock Retirement

Not applicable — simulator is itself a mock-traffic generator; no UI mocks involved.

## Risks & Mitigations

- **Argus Diameter server rejects unexpected Origin-Host:** Server is permissive (accepts any peer that completes CER/CEA), but if the deployment hardens this in the future, we expose `OriginHost` per operator so it's tunable. Mitigation: document and surface in metrics (response result label).
- **Hop-by-Hop ID exhaustion under high churn:** uint32 wraparound; mitigation: `atomic.Uint32.Add(1)` is safe across wrap, but pending-request map must clean on response/timeout to bound memory. Test covers a 10k-msg loop.
- **Default destination-realm vs server's actual realm:** If misconfigured, all CCRs error and sessions abort. Mitigation: Validate rejects empty `DestinationRealm` when any op opts in; example config comments say "must equal Argus `DIAMETER_ORIGIN_REALM`".
- **Peer dead → silent session aborts spike:** Mitigation: dedicated `simulator_diameter_session_aborted_total{reason}` counter and peer-state gauge so dashboards alert on `peer_state == 0` or aborted-total derivative.
- **Build cycle risk:** `internal/simulator/diameter` imports `internal/aaa/diameter` (different layer). Both are `internal/`; no cycle since aaa/diameter does NOT import simulator. Verify with `go build ./...`.

## Dependencies

- **STORY-082** complete (RADIUS-only simulator merged) — confirmed at `cmd/simulator/main.go` and `internal/simulator/engine/engine.go`.
- **STORY-019** Argus Diameter server — confirmed live at `internal/aaa/diameter/server.go` (Gx + Gy handlers present).
- **No new go.mod dependency required** (uses `internal/aaa/diameter` and stdlib `net`).

## Out of Scope

- S6a (HSS/UDM) interface — different 3GPP application, not implemented in Argus.
- Rf (offline charging) — Argus does CDR generation in-process, not over Diameter Rf.
- TLS for Diameter from the simulator — Argus supports it via `DIAMETER_TLS_*`, but the dev/test simulator runs in-cluster on the docker network; TLS adds friction with no test value. Tracked as future enhancement if the simulator ever runs outside the compose network.
- Reactive behavior (back off on `ResultCode != 2001`, retry strategies) — that's STORY-085's scope.
- Argus 5G SBA — STORY-084.

---

## Quality Gate (plan self-validation)

Run before dispatching Wave 1. Plan FAILS if any check is FALSE.

### Substance
- [x] Story Effort = M-L → Plan ≥ 100 lines, ≥ 5 tasks. Actual: 9 tasks across 3 waves, plan ~430 lines.
- [x] At least 1 task marked `Complexity: high` (Tasks 5 and 6 both high).

### Required Sections
- [x] `## Goal`
- [x] `## Architecture Context`
- [x] `## Tasks` (numbered Task blocks)
- [x] `## Acceptance Criteria` and `## Acceptance Criteria Mapping`
- [x] `## Risks & Mitigations`
- [x] `## Dependencies`
- [x] `## Out of Scope`
- [x] `## Quality Gate` (this section)

### Embedded Specs (self-contained)
- [x] Wire-format details for CER, Gx CCR-I, Gy CCR-I, Gy CCR-U, CCR-T listed inline (not "see RFC").
- [x] Config schema embedded with Go struct snippets (NOT just "see config.go").
- [x] Source-of-truth files cross-referenced with line numbers where helpful (e.g. `server.go:382-383`, `avp.go:102-104`).
- [x] Reuse strategy explicit: which Argus diameter symbols are imported and from where (table in "Reuse strategy" section).

### Code-State Validation
- [x] Disk verified: `cmd/simulator/main.go`, `internal/simulator/{config,discovery,engine,metrics,radius,scenario}/` exist (2026-04-17 ls). Plan does NOT propose duplicating directories.
- [x] Argus Diameter server symbols verified exported (AVP helpers, Message, command codes, application IDs all public in `internal/aaa/diameter/`).
- [x] No third-party Diameter library introduced (drift from 2026-04-14 plan called out at top).
- [x] `go.mod` checked: `fiorix/go-diameter` NOT present. Plan does not add it.
- [x] STORY-082 simulator config struct read; new fields are additive (no rename, no removal).
- [x] STORY-086 schema guard impact: none; no schema changes by this story.

### Task Decomposition
- [x] All 9 tasks touch ≤3 files (largest is Task 6: `engine.go` + `main.go` = 2 files).
- [x] Each task has `Depends on`, `Complexity`, `Pattern ref`, `Context refs`, `What`, `Verify` fields.
- [x] DB-first ordering N/A (no DB changes); foundation (config, peer, metrics) before integration (CCR, Client) before engine wiring.
- [x] Context refs all reference sections that exist in this plan.
- [x] Wave structure explicit: Wave 1 tasks 1–3, Wave 2 tasks 4–6, Wave 3 tasks 7–9.

### Test Coverage
- [x] Each AC mapped to a task that implements it AND a task that verifies it (table above).
- [x] Unit tests planned in same task as the code (Tasks 1, 2, 4, 5).
- [x] Integration test gated behind `-tags=integration` (Task 7).

### Drift Notes
- [x] 2026-04-14 plan called for `fiorix/go-diameter`. **CHANGED** — reuse `internal/aaa/diameter` (rationale documented at top + Reuse strategy section).
- [x] 2026-04-14 plan referenced `simulator_diameter_*` metrics generically. **CLARIFIED** — exact metric names + labels enumerated.
- [x] 2026-04-14 plan's "3GPP AVP dictionary gaps" risk is **RETIRED** (no third-party lib, AVP codes already defined in `argusdiameter`).
- [x] 2026-04-14 plan implied per-operator opt-in but didn't show config struct. **EMBEDDED** — full schema with Go struct snippet.

### Result
**PLAN GATE: PASS** — all checks satisfied; plan is dispatch-ready for Wave 1.

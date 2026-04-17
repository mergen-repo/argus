# Implementation Plan: STORY-085 — Simulator Reactive Behavior (Approach B)

> Plan revised 2026-04-17 after code-state validation against the current
> tree (post-STORY-082 / 083 / 084 / 086). Material drift from the
> 2026-04-14 plan:
>
> 1. **Reactive scope is RADIUS-only.** STORY-084 shipped a per-session
>    RADIUS-vs-SBA fork (`engine.go:138-154`); 5G-SBA sessions are HTTP
>    req/resp and have no CoA/DM equivalent on the Argus side. SBA and
>    Diameter branches are explicitly untouched.
> 2. **CoA/DM routing unblocked by hostname-based NAS-IP.** Argus dials
>    `nasIP:3799` (`internal/aaa/session/dm.go:57`,
>    `internal/aaa/session/coa.go:70`). Current `nas_ip: 10.99.0.{1,2,3}`
>    values are fake and unreachable from `argus-app` → simulator.
>    Resolution: simulator advertises `nas_ip: argus-simulator` (the
>    compose container name, DNS-resolvable on `argus-net`), simulator's
>    CoA/DM listener binds `0.0.0.0:3799`, compose exposes no host port
>    (listener is reachable only on the compose network).
> 3. **Bandwidth-cap reaction RETIRED to Out of Scope.**
>    `internal/aaa/radius/server.go:571-580` installs bandwidth at raw
>    `radius.Type(11)` (Filter-Id per RFC 2865 — already written at
>    server.go:569, so this collides) and raw `radius.Type(12)`
>    (Framed-MTU per RFC 2865 — semantically wrong). A simulator that
>    reacted to these values would be reacting to a pre-existing Argus
>    bug. Documented as new tech-debt D-034.
> 4. **Session-abort reasons use single-writer pattern (PAT-001).**
>    STORY-084 established engine-only writers for
>    `*SessionAbortedTotal`; STORY-085 extends the same rule to new
>    reactive counters. Client layers (CoA listener, reject-backoff)
>    return wrapped sentinel errors; engine classifies once.
> 5. **Retry-storm cap REDESIGNED.** Old plan's "per-SIM rolling
>    counter ≥ 5 rejects / hour → suspend-by-simulator" implied persistent
>    state. Simulator is stateless per session; per-SIM suspension can
>    run in-memory on the engine with a `sync.Map` of
>    `operator+imsi → nextRetryAt` + a 60-minute sliding bucket. No DB
>    writes; state lost on simulator restart (acceptable — 1-hour
>    cooldown max).
> 6. **CoA shared secret IS a YAML field** (with env override), not
>    env-only. Rationale: the Argus `coaSender` uses a static
>    `session.NewCoASender(secret, …)` wired at boot from the Argus
>    config (not per-operator); there's only one secret to plumb.
>    Env override `ARGUS_SIM_COA_SECRET` mirrors
>    `ARGUS_SIM_RADIUS_SECRET` precedent (`config.go:152-154`).
> 7. **CoA/DM listener recognises `radius.Code(40)` (Disconnect-Request)
>    + `radius.Code(43)` (CoA-Request).** STORY-085 MUST replicate the
>    Argus-side codepoints (see `dm.go:16` + `coa.go:16`) rather than the
>    FreeRADIUS convention, because those are the codes Argus uses and
>    the simulator is validating against Argus — not against a generic
>    NAS.

## Goal

Upgrade the simulator from "dumb client" (STORY-082 approach A) to
"reactive SIM/modem emulator" for the **RADIUS session path only**
(approach B, RADIUS-scoped). The simulator:

- Respects `Session-Timeout` from Access-Accept: terminates the session
  at `min(scenario_duration, Session-Timeout − early_termination_margin)`.
- Applies exponential back-off on `Access-Reject` instead of instant
  retry, with an in-memory per-SIM cool-down against retry storms.
- Binds a UDP listener on `:3799` (RFC 3576 / 5176) per simulator
  process and reacts to Argus-initiated `Disconnect-Request` (session
  terminate) and `CoA-Request` (session-parameter update) by driving the
  matching session to Terminating and (for CoA) adjusting live session
  deadlines.
- Formalises the implicit state machine inside `runSession` as an
  explicit enum with single-writer transitions so metrics and tests
  have a well-defined state to key on.

End-to-end, this makes three Argus features observable from the
simulator: (1) `POST /api/v1/sessions/{id}/disconnect` (STORY-017)
triggers a simulator-side Accounting-Stop within seconds;
(2) `Session-Timeout` tightening via policy is respected;
(3) sustained Access-Reject (e.g. kill-switch enabled, policy denies)
produces measurable back-off rather than a retry storm.

Default = **reactive disabled**. With `reactive.enabled: false` the
STORY-082 RADIUS lifecycle is byte-identical to pre-STORY-085. STORY-083
(Diameter) and STORY-084 (SBA) code paths are untouched.

## Architecture Context

### Verified disk layout BEFORE this story (2026-04-17)

```
cmd/simulator/
  main.go                                # STORY-082+083+084 wiring; reactive listener wiring is additive
  Dockerfile
internal/simulator/
  config/
    config.go                            # has DiameterDefaults + SBADefaults; Reactive is NEW
    config_test.go
  discovery/db.go
  scenario/{scenario.go, scenario_test.go}
  radius/
    client.go                            # Auth / AcctStart / AcctInterim / AcctStop
    client_test.go
  diameter/                              # STORY-083 — untouched
  sba/                                   # STORY-084 — untouched
  engine/engine.go                       # post-STORY-084: runSession forks RADIUS|SBA
  metrics/metrics.go                     # 15 vectors registered; STORY-085 adds 3
deploy/simulator/
  config.example.yaml                    # demonstrates STORY-082/083/084; extends with reactive block
  Dockerfile
deploy/docker-compose.simulator.yml      # exposes 9099; STORY-085 does NOT expose 3799 on host
docs/architecture/simulator.md           # STORY-082/083/084 sections; STORY-085 appends Reactive section
```

### Argus-side reference points (verified 2026-04-17)

| What Argus does | File:line | Relevance to STORY-085 |
|---|---|---|
| Installs `Session-Timeout` in Access-Accept (direct auth) | `internal/aaa/radius/server.go:567` | Simulator must parse it and shorten scenario duration |
| Installs `Session-Timeout` in Access-Accept (EAP) | `internal/aaa/radius/server.go:418` | Same; EAP isn't exercised by sim today but parser works regardless |
| Sends `Disconnect-Request` (`radius.Code(40)`) on `POST /api/v1/sessions/{id}/disconnect` | `internal/aaa/session/dm.go:16,52-74`, `internal/api/session/handler.go:372-385` | Simulator listener must respond with `radius.Code(41)` (DM-ACK) |
| Sends `CoA-Request` (`radius.Code(43)`) via `session.CoASender` | `internal/aaa/session/coa.go:16,54-87` | Simulator listener must respond with `radius.Code(44)` (CoA-ACK) on known Acct-Session-Id, else NAK (`45`) |
| DM/CoA destination = `req.NASIP:3799` | `dm.go:57`, `coa.go:70` | Simulator's `nas_ip` MUST resolve from `argus-app` |
| Session record's NASIP | `internal/aaa/session/session.go:43,658-659` | The value Argus dials is what the simulator sent as `NAS-IP-Address` AVP |
| Active-session terminate | `internal/aaa/session/session.go` (`Terminate`) | Argus terminates locally after DM is ACKed or NAKed (non-blocking on NAK) |
| Bandwidth-cap install (BROKEN) | `internal/aaa/radius/server.go:571-580` | RFC-incorrect; **STORY-085 OUT OF SCOPE**, tracked as D-034 |

### Components Involved

**NEW under `internal/simulator/reactive/`:**

```
reactive/
  doc.go                  # Package overview: state machine + CoA/DM listener
  state.go                # SessionState enum + atomic Session struct
  state_test.go           # transition table tests
  backoff.go              # exponential backoff + rolling-window retry limiter
  backoff_test.go         # curve (30/60/120/240/480/600), per-SIM cooldown math
  listener.go             # UDP server on :3799 for Disconnect-Request + CoA-Request
  listener_test.go        # packet decode, secret validation, ACK/NAK write
  registry.go             # AcctSessionID → *Session in-memory map (sync.Map-backed)
  registry_test.go        # register/lookup/delete + goroutine-safe assertions
```

**MODIFIED:**

```
internal/simulator/config/config.go             # + ReactiveDefaults struct, Validate
internal/simulator/config/config_test.go        # + defaults, validation table rows
internal/simulator/engine/engine.go             # + reactive hooks in runSession (RADIUS branch only)
internal/simulator/radius/client.go             # + SessionTimeout + ReplyMessage attribute parse
internal/simulator/radius/client_test.go        # + assert parse of new AVPs
internal/simulator/metrics/metrics.go           # + 3 reactive vectors
cmd/simulator/main.go                           # + reactive listener lifecycle + registry injection
deploy/simulator/config.example.yaml            # + reactive: block demonstrating defaults + 1 opt-in
deploy/docker-compose.simulator.yml             # NO host port exposure; comment explaining :3799 internal-only
docs/architecture/simulator.md                  # + "Reactive Behavior (STORY-085)" section
```

### Reuse strategy — CRITICAL

STORY-085 **consumes** `layeh.com/radius` (already in `go.mod` at
`v0.0.0-20231213012653-1006025d24f8`) for decode + encode of DM/CoA
packets — the same library both Argus and the simulator's RADIUS client
already use. No new third-party dependency.

Symbols consumed from `layeh.com/radius`:

| Symbol | Purpose |
|---|---|
| `radius.Code(40)`, `radius.Code(41)` | Disconnect-Request / DM-ACK |
| `radius.Code(43)`, `radius.Code(44)`, `radius.Code(45)` | CoA-Request / CoA-ACK / CoA-NAK |
| `radius.Parse(bytes, secret)` | Verifies Message-Authenticator + parses DM/CoA |
| `radius.New(code, secret)` | Builds ACK/NAK response |
| `rfc2866.AcctSessionID_LookupString` | Extract Acct-Session-Id from incoming CoA/DM |
| `rfc2865.SessionTimeout_Lookup` (in client.go) | Parse Access-Accept's Session-Timeout AVP |
| `rfc2865.ReplyMessage_LookupString` (in client.go) | Parse Access-Accept / Reject's Reply-Message AVP |

Symbols consumed from the existing simulator tree:

| Symbol | File | Purpose |
|---|---|---|
| `simradius.SessionContext` | `radius/client.go:45-56` | Already carries AcctSessionID, NASIP, SIM |
| `simradius.Client.Auth` | `radius/client.go:72-103` | Returns `*radius.Packet`; now caller parses Session-Timeout + Reply-Message |
| `metrics.*` counters | `metrics/metrics.go` | Observer for new reactive vectors |

### Config schema — additive

Source: `internal/simulator/config/config.go` (current truth, post-STORY-084).

```go
type Config struct {
    Argus     ArgusConfig
    Operators []OperatorConfig
    Scenarios []ScenarioConfig
    Rate      RateConfig
    Metrics   MetricsConfig
    Log       LogConfig
    Diameter  DiameterDefaults `yaml:"diameter"`
    SBA       SBADefaults      `yaml:"sba"`
    Reactive  ReactiveDefaults `yaml:"reactive"` // NEW — defaults-off master block
}

// NEW — simulator reactive-behaviour knobs. All fields have safe defaults;
// `Enabled: false` makes the simulator byte-identical to STORY-082 output.
type ReactiveDefaults struct {
    Enabled                    bool          `yaml:"enabled"`                       // master switch — default false
    SessionTimeoutRespect      bool          `yaml:"session_timeout_respect"`       // default true when Enabled
    EarlyTerminationMargin     time.Duration `yaml:"early_termination_margin"`      // default 5s
    RejectBackoffBase          time.Duration `yaml:"reject_backoff_base"`           // default 30s
    RejectBackoffMax           time.Duration `yaml:"reject_backoff_max"`            // default 600s (10 min)
    RejectMaxRetriesPerHour    int           `yaml:"reject_max_retries_per_hour"`   // default 5 (then 1-hour cooldown)
    CoAListener                CoAListenerConfig `yaml:"coa_listener"`
}

// CoAListenerConfig — simulator-side UDP server for RFC 3576 / 5176 packets.
type CoAListenerConfig struct {
    Enabled      bool   `yaml:"enabled"`       // default false; requires ReactiveDefaults.Enabled: true
    ListenAddr   string `yaml:"listen_addr"`   // default "0.0.0.0:3799"
    SharedSecret string `yaml:"shared_secret"` // MUST match Argus RADIUS_SECRET (or override via env ARGUS_SIM_COA_SECRET)
}
```

**Env overrides (applyEnvOverrides addition):**

```go
if v := os.Getenv("ARGUS_SIM_COA_SECRET"); v != "" {
    c.Reactive.CoAListener.SharedSecret = v
}
```

**Defaults + validation (in new `validateReactive()` called after
`validateSBA()`):**

```go
func (c *Config) validateReactive() error {
    r := &c.Reactive
    if !r.Enabled {
        return nil // disabled → no validation, no defaults applied
    }
    // ---- Reactive enabled: apply defaults ----
    if r.EarlyTerminationMargin == 0 { r.EarlyTerminationMargin = 5 * time.Second }
    if r.RejectBackoffBase == 0      { r.RejectBackoffBase      = 30 * time.Second }
    if r.RejectBackoffMax == 0       { r.RejectBackoffMax       = 600 * time.Second }
    if r.RejectMaxRetriesPerHour == 0 { r.RejectMaxRetriesPerHour = 5 }
    if !r.SessionTimeoutRespect {
        // explicit-false permitted; zero-value honours opt-in default of true
    }

    if r.CoAListener.Enabled {
        if r.CoAListener.ListenAddr == "" { r.CoAListener.ListenAddr = "0.0.0.0:3799" }
        if r.CoAListener.SharedSecret == "" {
            // Fall back to main RADIUS shared secret — same wire-level semantic
            r.CoAListener.SharedSecret = c.Argus.RadiusSharedSecret
        }
        if r.CoAListener.SharedSecret == "" {
            return fmt.Errorf("reactive.coa_listener.shared_secret required (or ARGUS_SIM_COA_SECRET env, or inherit from argus.radius_shared_secret)")
        }
        if r.RejectBackoffBase > r.RejectBackoffMax {
            return fmt.Errorf("reactive.reject_backoff_base (%s) > reject_backoff_max (%s)", r.RejectBackoffBase, r.RejectBackoffMax)
        }
    }
    return nil
}
```

### State machine

The STORY-082/083/084 `runSession` is an implicit state machine; STORY-085
makes it explicit. The states below are the ONLY valid values of
`Session.State` (atomic uint32 for lock-free reads from the listener).

```
                 ┌───────────┐
                 │   Idle    │   ← scheduler picks scenario
                 └──────┬────┘
                        │ runSession entry
                        ▼
                 ┌───────────────┐
                 │ Authenticating│ ── Auth sent, awaiting response
                 └──┬──────────┬─┘
                    │ Accept   │ Reject
                    ▼          ▼
         ┌────────────────┐  ┌─────────────────┐
         │ Authenticated  │  │   BackingOff    │ ── backoff timer running
         └──────┬─────────┘  └────────┬────────┘
                │ AcctStart              │ expired; check retry budget
                ▼                        ▼
         ┌────────────────┐     (back to Idle OR
         │    Active      │      Suspended if ≥ max retries/hr)
         └────┬───────────┘
              │
              │ one of:
              │  • Acct-Interim tick (no state change)
              │  • Session-Timeout (server) OR scenario deadline (local) expires
              │  • CoA-Request received  (stay Active; may tighten deadline)
              │  • Disconnect-Request received
              │  • ctx.Done() (shutdown)
              ▼
         ┌────────────────┐
         │  Terminating   │ ── single-writer AcctStop sent
         └──────┬─────────┘
                ▼
            (back to Idle)

 Side state (per-operator+IMSI, in RejectTracker):
 ┌─────────────┐
 │  Suspended  │ ← ≥ N rejects in sliding 1-hour window; Idle scheduler
 │             │    skips this SIM until Now() ≥ nextRetryAt.
 └─────────────┘
```

**Transition rules (enforced in `reactive/state.go`):**

| From | Event | To | Side-effect |
|---|---|---|---|
| Idle | scheduler tick | Authenticating | — |
| Authenticating | Access-Accept | Authenticated | Parse Session-Timeout, Reply-Message |
| Authenticating | Access-Reject | BackingOff | RejectTracker.Record(op,imsi); compute `nextRetryAt = base × 2^attempt` capped by max |
| Authenticating | timeout / transport err | Idle | (no backoff — transport errors are not Argus's rejection) |
| Authenticated | AcctStart Accept | Active | Register in Registry (keyed by AcctSessionID) |
| Authenticated | AcctStart error | Idle | (abort — no AcctStop sent) |
| Active | Interim tick | Active | — |
| Active | deadline / ctx | Terminating | AcctStop(User-Request) |
| Active | Disconnect-Request | Terminating | AcctStop(Admin-Reset), ACK DM |
| Active | CoA-Request (Session-Timeout) | Active | Update deadline; ACK CoA |
| BackingOff | timer expired AND retries < limit | Idle | — (scheduler picks next tick) |
| BackingOff | timer expired AND retries ≥ limit | Suspended | Hold until 1-hour window rolls off |
| Suspended | sliding-window rolls off | Idle | — |
| any | ctx.Done() | Terminating | AcctStop(AdminReboot) when Active; drop when pre-Active |

**Concurrency:** one goroutine per session owns writes to `Session.State`.
The CoA/DM listener signals the session via its `cancelFn` (captured
at registration) — the listener does NOT mutate state directly. This
keeps the single-writer rule and matches the Diameter/SBA error
classification pattern (PAT-001).

### CoA / Disconnect handling

**Wire layer** (`reactive/listener.go`):

```go
// Listener binds ListenAddr, reads UDP packets, parses with the shared
// secret, and dispatches by Code to handleCoA / handleDM.
type Listener struct {
    addr     string
    secret   []byte
    registry *Registry          // AcctSessionID → *Session
    logger   zerolog.Logger
    conn     *net.UDPConn
    wg       sync.WaitGroup
}

func (l *Listener) Start(ctx context.Context) error { ... }
func (l *Listener) Stop(ctx context.Context) error  { ... }
```

**Packet flow:**

1. Read UDP frame (4096-byte buffer; oversized frames rejected).
2. `radius.Parse(frame, l.secret)` — fails closed (no response emitted)
   when secret mismatches, Message-Authenticator is invalid, or the
   packet is malformed. Increment `SimulatorReactiveCoABadPackets` (see
   metrics below).
3. Branch on `pkt.Code`:
   - `radius.Code(40)` Disconnect-Request → `handleDM(pkt, src)`
   - `radius.Code(43)` CoA-Request → `handleCoA(pkt, src)`
   - anything else → log at debug + drop
4. Lookup session by `rfc2866.AcctSessionID_LookupString(pkt)`. If
   not found → respond with NAK (`radius.Code(41)` for DM, `radius.Code(45)`
   for CoA); increment `SimulatorReactiveUnknownSession`.
5. For DM: trigger `session.cancelFn()` → session goroutine transitions
   Active → Terminating; listener writes `radius.Code(41)` DM-ACK.
6. For CoA: inspect attributes (Session-Timeout is the only one
   honoured in this story), atomically update `Session.Deadline`, then
   write `radius.Code(44)` CoA-ACK. On attribute decode error → CoA-NAK
   with `Error-Cause` AVP populated.

**Port binding:** `0.0.0.0:3799` inside the container. Compose does NOT
expose this port on the host — the listener is reachable only from
`argus-app` over `argus-net`. This intentionally avoids making a
CoA-speaking endpoint public.

**NAS-IP resolution (critical fix):**

Currently `nas_ip: 10.99.0.1` is written into the `NAS-IP-Address` AVP
and persisted into `sessions.nas_ip`. Argus dials that string at CoA/DM
time — which won't route to the simulator.

**Resolution (config change documented in Task 7):** operators set
`nas_ip: argus-simulator` (the compose container name). Go's
`net.DialTimeout("udp", "argus-simulator:3799", ...)` resolves via
compose's embedded DNS on the shared `argus-net` network. The
`NAS-IP-Address` AVP still technically requires a 4-byte IPv4 value;
the simulator's `radius.setCommonNAS` uses `net.ParseIP(sc.NASIP)`
(`radius/client.go:156`) which returns nil for the hostname → attribute
is omitted from the packet. That is consistent with RFC 2865 which
makes `NAS-IP-Address` OR `NAS-Identifier` mandatory (not both); the
simulator still sends `NAS-Identifier`. Argus persists the hostname in
`sessions.nas_ip` (the column is `text`), and dials it at CoA time.

**Verification that this works:**

- `net.DialTimeout("udp", "argus-simulator:3799", 3s)` — Go's stdlib
  resolves A records via `/etc/resolv.conf` which compose populates
  with its embedded resolver.
- `rfc2865.NASIPAddress_Set(pkt, nil)` — the `nil` branch in
  `radius/client.go:156` is already a no-op when `sc.NASIP` doesn't
  parse as an IP. Confirmed by inspection.
- Argus's `dm.go:57` uses `fmt.Sprintf("%s:%d", req.NASIP, d.port)`
  which accepts a hostname unchanged.

### Reject backoff

**Per-session:** local exponential back-off timer (30 → 60 → 120 → 240
→ 480 → 600s cap, doubling each attempt for the same `(operator, imsi)`
key). The session goroutine simply sleeps `nextRetryAt - Now()` before
retrying.

**Per-operator+IMSI (global):** `RejectTracker` in `reactive/backoff.go`:

```go
type RejectTracker struct {
    mu       sync.Mutex
    attempts map[string]*attemptRecord // key = operatorCode+":"+imsi
}
type attemptRecord struct {
    count          int         // rolling count in the last hour
    firstInBucket  time.Time   // timestamp of first count in current bucket
    nextRetryAt    time.Time   // set when suspending
}
```

- `Record(op, imsi)` bumps count; resets if `Now() - firstInBucket > 1h`.
- `Allowed(op, imsi) (bool, time.Duration)` returns false + remaining
  cooldown when `count ≥ RejectMaxRetriesPerHour`.
- Bucket rolls every hour; no DB writes; state lost on restart (a
  conscious trade-off — cooldown max is 1 hour).

### Session-Timeout respect

After `Access-Accept`, parse `Session-Timeout` AVP
(`rfc2865.SessionTimeout_Lookup`). If present and
`ReactiveDefaults.SessionTimeoutRespect == true`:

```go
serverDeadline := sc.StartedAt.Add(time.Duration(sessionTimeout) * time.Second)
scenarioDeadline := sc.StartedAt.Add(sample.SessionDuration)
effectiveDeadline := minTime(scenarioDeadline, serverDeadline.Add(-cfg.Reactive.EarlyTerminationMargin))
```

Used directly in the existing interim loop at `engine.go:226` in place
of `scenarioDeadline`. This is the ONLY change to the existing interim
tick logic.

### Reply-Message parsing

Parse `rfc2865.ReplyMessage_LookupString` on both Access-Accept and
Access-Reject. Log at debug level. No metric (free-form text would blow
label cardinality). This is purely observational — debugging aid.

### Metrics — additions

Following PAT-001 (single-writer), all new counters are written exactly
once at the outermost layer that classifies the event.

```go
// Writer: reject-backoff path in engine.runSession after classifying
// Access-Reject.
// Labels: operator, outcome (backoff_set|retry_immediate|suspended)
SimulatorReactiveRejectBackoffsTotal *prometheus.CounterVec

// Writer: reactive.Listener after successfully decoding and dispatching.
// Labels: operator, kind (coa|dm), result (ack|nak|unknown_session|bad_secret)
SimulatorReactiveIncomingTotal *prometheus.CounterVec

// Writer: reactive.Listener / state machine transition into Terminating.
// Labels: operator, cause (session_timeout|disconnect|coa_deadline|scenario_end|shutdown|reject_suspend)
SimulatorReactiveTerminationsTotal *prometheus.CounterVec
```

Three new vectors, taking total simulator metric count from 15 → 18.
All labels bounded (`kind` ∈ {coa, dm}, `result` ∈ 4 values, `cause` ∈
6 values, `outcome` ∈ 3 values).

**Why not a state-histogram or current-state gauge?** Gauges over
per-session state require atomic increment/decrement on every
transition (4 states × 3 operators × N ops) and are a footgun for the
single-writer rule. Counters with cause/outcome labels deliver the same
observability without racy gauge bookkeeping.

### Safety Envelope

- **`reactive.enabled: false`** (default). Reactive goroutines never
  start; `runSession` takes the identical code path as STORY-082. Safety
  invariant: `go test ./internal/simulator/engine/...` passes
  unchanged.
- **CoA listener only binds when `coa_listener.enabled: true`** —
  zero open ports otherwise.
- **CoA secret defaults to `argus.radius_shared_secret`** — one less
  secret to keep in sync in dev. Override via
  `reactive.coa_listener.shared_secret` YAML field or
  `ARGUS_SIM_COA_SECRET` env var.
- **Compose does NOT publish `:3799` on host** — listener reachable
  only on `argus-net`.
- **Single-writer rule (PAT-001)** preserved for the three new vectors.
- **In-memory state** — no persistent writes (no new DB tables, no
  Redis). Simulator restart re-seeds from empty.
- **SBA + Diameter branches unaffected** — `runSBASession` and
  Diameter bracket are untouched.
- **SIMULATOR_ENABLED env guard** preserved.

### Acceptance Criteria Mapping — Argus side

Every AC here maps to a concrete Argus endpoint or code path:

- AC-1 (Session-Timeout) ↔ `internal/aaa/radius/server.go:567`
- AC-2 (Reject backoff curve) ↔ no Argus change; simulator-internal.
- AC-3 (DM terminate round-trip) ↔ `POST /api/v1/sessions/{id}/disconnect`
  → `internal/api/session/handler.go:372-385` → `session.DMSender.SendDM`
  → `dm.go:52-74` → simulator listener → session terminates → Argus
  `sessionMgr.Terminate`.
- AC-4 (CoA Session-Timeout update) ↔ `session.CoASender.SendCoA` with
  `Attributes["Session-Timeout"]` → simulator listener → session deadline
  shortened → AcctStop emitted at new deadline.
- AC-5 (Retry-storm cap) ↔ simulator-internal; in-memory RejectTracker.
- AC-6 (Reactive disabled → byte-identical) ↔ simulator-internal; go
  test ./... regression.

## Tasks

### Wave 1 — Foundation (config + state types + metrics)

> Three independent tasks; can run in parallel. Wave 1 must complete
> before Wave 2.

#### Task 1: Extend simulator config schema for reactive behavior
- **Files:** Modify `internal/simulator/config/config.go`, Modify `internal/simulator/config/config_test.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/simulator/config/config.go` — follow the `validateSBA()` / `validateDiameter()` structure: new struct next to `SBADefaults`, new `validateReactive()` called from `Validate()` after `validateSBA()`. Env-override follows `ARGUS_SIM_RADIUS_SECRET` pattern at `config.go:152-154`.
- **Context refs:** "Architecture Context > Config schema — additive", "Architecture Context > Safety Envelope"
- **What:**
  - Add `ReactiveDefaults` + `CoAListenerConfig` structs.
  - Add `Reactive ReactiveDefaults \`yaml:"reactive"\`` to `Config`.
  - Add `ARGUS_SIM_COA_SECRET` env override to `applyEnvOverrides`.
  - Implement `validateReactive()`: zero-value skip when `!Enabled`; otherwise apply all defaults (5s margin, 30/600s backoff, 5 retries/hr, listener addr `0.0.0.0:3799`, inherit `argus.radius_shared_secret` for CoA secret when unset).
  - Call `validateReactive()` from `Validate()` after `validateSBA()`.
  - Tests (table-driven): defaults-off → no change; enabled → all defaults applied; `reject_backoff_base > reject_backoff_max` → error; CoA secret missing AND `argus.radius_shared_secret` missing → error; env override beats YAML; operator configs remain valid with or without reactive block.
- **Verify:** `cd /Users/btopcu/workspace/argus && go test ./internal/simulator/config/...`

#### Task 2: Reactive state types + session registry
- **Files:** Create `internal/simulator/reactive/doc.go`, Create `internal/simulator/reactive/state.go`, Create `internal/simulator/reactive/state_test.go`, Create `internal/simulator/reactive/registry.go`, Create `internal/simulator/reactive/registry_test.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/simulator/diameter/peer.go` — follow its atomic state-enum + `sync.Mutex` pattern for transition correctness. Read `internal/aaa/session/session.go:43-135` for Session-struct layout idioms.
- **Context refs:** "Architecture Context > State machine", "Architecture Context > Components Involved"
- **What:**
  - `doc.go`: package comment with state diagram (ASCII copy of plan §State machine), single-writer invariant note, PAT-001 cite.
  - `state.go`:
    - `type SessionState uint32` with constants `StateIdle`, `StateAuthenticating`, `StateAuthenticated`, `StateActive`, `StateBackingOff`, `StateTerminating`, `StateSuspended`, plus `String()` method.
    - `type Session struct { ID, OperatorCode, AcctSessionID string; State atomic.Uint32; Deadline atomic.Int64 (unix-nanos); CancelFn context.CancelFunc }` — single goroutine writes State via CAS transition helper.
    - `Transition(from, to SessionState) bool` — CAS; returns false if another transition raced (caller logs at warn).
    - `UpdateDeadline(t time.Time)` — atomic store.
  - `state_test.go`: table-driven valid-transition test; concurrent CAS race assertions (100-goroutine fanout).
  - `registry.go`:
    - `type Registry struct { m sync.Map /* AcctSessionID → *Session */ }` with `Register`, `Lookup`, `Delete`.
    - `Len() int` for tests / debug.
  - `registry_test.go`: insertion, concurrent insert/delete, lookup-miss returns `nil`.
- **Verify:** `go test ./internal/simulator/reactive/... -run "TestState|TestRegistry"`.

#### Task 3: Reactive metrics
- **Files:** Modify `internal/simulator/metrics/metrics.go`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `internal/simulator/metrics/metrics.go` — add new vectors next to `SBAServiceErrorsTotal`; register in `MustRegister`. Match naming convention `simulator_reactive_*`.
- **Context refs:** "Architecture Context > Metrics — additions"
- **What:** Add three CounterVec registrations with label sets as specified in §Metrics. Register all three in `MustRegister`. No histograms; no gauges — all-counter design intentional (see §Metrics rationale note).
- **Verify:** `go build ./...`; `go test ./internal/simulator/metrics/... -run TestRegister` (add one if absent — one-liner using `prometheus.NewRegistry()`).

### Wave 2 — Core reactive logic

> Starts after Wave 1 Tasks 1 + 2 merged. Tasks 4 + 5 + 6 sequential
> (each depends on the previous); Task 7 independent.

#### Task 4: Reject back-off tracker
- **Files:** Create `internal/simulator/reactive/backoff.go`, Create `internal/simulator/reactive/backoff_test.go`
- **Depends on:** Task 2
- **Complexity:** medium
- **Pattern ref:** Read `internal/gateway/bruteforce.go` — sliding-window counter pattern with in-memory map. Read `internal/simulator/diameter/peer.go` (reconnect-backoff math) for exponential-backoff idiom.
- **Context refs:** "Architecture Context > Reject backoff", "Architecture Context > State machine" (BackingOff/Suspended states)
- **What:**
  - `type RejectTracker struct { mu sync.Mutex; attempts map[string]*attemptRecord; baseBackoff, maxBackoff time.Duration; maxPerHour int }`.
  - `NewRejectTracker(cfg config.ReactiveDefaults) *RejectTracker`.
  - `NextBackoff(op, imsi string) (wait time.Duration, suspended bool)`: records attempt, returns `(base × 2^(n-1), false)` while `n < maxPerHour`, else `(remaining_window, true)`.
  - `Reset(op, imsi)` called on Access-Accept (clear state).
  - Tests: curve 30/60/120/240/480/600 (at 5 retries); capping at max; sliding-window reset after 1h; `suspended=true` on 6th attempt within window.
- **Verify:** `go test ./internal/simulator/reactive/... -run TestBackoff`.

#### Task 5: CoA/DM listener
- **Files:** Create `internal/simulator/reactive/listener.go`, Create `internal/simulator/reactive/listener_test.go`
- **Depends on:** Task 2, Task 3
- **Complexity:** high
- **Pattern ref:** Read `internal/aaa/session/coa.go:89-129` (packet encode + secret-based response) and `internal/aaa/radius/server.go` (specifically `handleDirectAuth` / worker-pool pattern) for `radius.PacketServer` idiom. Read `internal/simulator/diameter/peer.go` for start/stop lifecycle pattern (`Start(ctx) <-chan struct{}` / `Stop(ctx) error`).
- **Context refs:** "Architecture Context > CoA / Disconnect handling", "Architecture Context > Metrics — additions", "Architecture Context > State machine" (transitions Active → Terminating, Active with updated Deadline)
- **What:**
  - `type Listener struct { addr string; secret []byte; registry *Registry; logger; conn *net.UDPConn; wg sync.WaitGroup; ready chan struct{} }`.
  - `Start(ctx)` binds UDP, spawns reader goroutine, closes `ready` on successful bind. Ctx cancellation triggers `conn.Close()`; reader loop exits on net.ErrClosed.
  - `handleDM(pkt, src)`: look up session by AcctSessionID → call `session.CancelFn()` → write DM-ACK (`radius.Code(41)`). Metric: `SimulatorReactiveIncomingTotal{kind="dm", result="ack"}`. On missing session: NAK + `result="unknown_session"`. On secret mismatch: `result="bad_secret"` + no response.
  - `handleCoA(pkt, src)`: look up session; read `rfc2865.SessionTimeout_Lookup(pkt)`; `session.UpdateDeadline(sc.StartedAt + newTimeout)`; write CoA-ACK (`radius.Code(44)`). No session → NAK with `Error-Cause` = `503 Session-Context-Not-Found`.
  - `Stop(ctx)`: close conn, wait on WaitGroup with ctx deadline.
  - `listener_test.go` (using `httptest`-style in-process client):
    - Valid DM → ACK written; session's `CancelFn` called.
    - Valid CoA with Session-Timeout=30 → deadline updated; CoA-ACK written.
    - Unknown Acct-Session-Id → NAK.
    - Wrong secret → silent drop; metric increments with `result="bad_secret"`.
    - Malformed UDP frame (e.g. 6 bytes) → dropped; listener keeps running.
    - Concurrent 100 packets → all responded.
- **Verify:** `go test ./internal/simulator/reactive/... -run TestListener`.

#### Task 6: Engine integration (RADIUS branch only)
- **Files:** Modify `internal/simulator/engine/engine.go`, Modify `internal/simulator/radius/client.go`, Modify `internal/simulator/radius/client_test.go`
- **Depends on:** Task 1, Task 4, Task 5
- **Complexity:** high
- **Pattern ref:** Read current `engine.go:138-297` (`runSession`) — reactive hooks are additive, zero changes to the non-reactive code path. Read the STORY-083 Diameter bracket pattern (`engine.go:181-192`, `engine.go:261-268`, `engine.go:291-296`) for how to thread an optional subsystem through the lifecycle without breaking the default path. Read `simradius.Client.Auth` at `radius/client.go:72-103` — extend to parse Session-Timeout + Reply-Message after Access-Accept.
- **Context refs:** "Architecture Context > State machine", "Architecture Context > Session-Timeout respect", "Architecture Context > Reject backoff", "Architecture Context > Components Involved"
- **What:**
  - **`radius/client.go`:** In `Auth()`, after the existing `rfc2865.FramedIPAddress_Get` block, also read `sessionTimeout` and `replyMessage`. Store on `SessionContext` as new fields `ServerSessionTimeout time.Duration` (zero-valued if absent) and `ReplyMessage string`. Parser reads both for Access-Accept AND Access-Reject (some NAS implementations include Reply-Message on reject).
  - **`client_test.go`:** Add table row for an Access-Accept carrying `Session-Timeout=120` + `Reply-Message=test` — assert both fields extracted into the SessionContext.
  - **`engine.go`:**
    - Extend `Engine` struct: `reactive *reactive.Subsystem` (holds `RejectTracker`, `Registry`, and `Cfg config.ReactiveDefaults`). Nil when disabled.
    - Extend `New(...)` signature: accept `reactiveSub *reactive.Subsystem` (pass nil when disabled). Backward-compatible — callers can pass nil.
    - In `runSession`, BEFORE the existing Authenticate call:
      - If `e.reactive != nil`: check `e.reactive.Rejects.Allowed(op, imsi)`. If `!allowed` → log + return (Suspended session skips entire cycle until cooldown).
    - AFTER the Access-Reject branch (`engine.go:171-173`):
      - If `e.reactive != nil`: call `NextBackoff(op, imsi)` → `time.Sleep(wait)` (guarded by `ctx.Done()`). Metric `SimulatorReactiveRejectBackoffsTotal{outcome=backoff_set|suspended}`.
      - Else (reactive nil): current fast-retry behaviour is preserved.
    - AFTER Access-Accept (`engine.go:174` onward):
      - If `e.reactive != nil` AND `cfg.SessionTimeoutRespect` AND `sc.ServerSessionTimeout > 0`:
        compute `effectiveDeadline = min(scenarioDeadline, sc.StartedAt + ServerSessionTimeout - EarlyTerminationMargin)` and use it in place of `deadline` at `engine.go:226`.
      - If `e.reactive != nil`: create `reactive.Session` + register in `e.reactive.Registry` by AcctSessionID with `CancelFn = cancel`. Defer `Registry.Delete(AcctSessionID)`.
      - In the interim loop, check `session.Deadline.Load()` at each tick (not just scenarioDeadline) so CoA-triggered deadline updates take effect within one interim interval.
    - On termination, emit `SimulatorReactiveTerminationsTotal{cause}` with cause ∈ {session_timeout, disconnect, scenario_end, shutdown, coa_deadline, reject_suspend} — **engine is single writer** (PAT-001). Listener sets a flag on `Session` indicating the cancel came from a DM; engine reads it to choose `cause="disconnect"` vs. `cause="scenario_end"`.
  - **Non-regression:** engine_test.go (if present) passes; byte-identical RADIUS path when `reactive = nil`. Diameter + SBA paths untouched.
- **Verify:** `go build ./cmd/simulator`; `go test ./internal/simulator/{engine,radius,reactive}/...`; `go vet ./...` clean.

#### Task 7: Wire reactive lifecycle in main.go
- **Files:** Modify `cmd/simulator/main.go`
- **Depends on:** Task 1
- **Complexity:** low
- **Pattern ref:** Read `cmd/simulator/main.go:87-124` — Diameter + SBA clients are built per-operator and passed to `engine.New`. Reactive is process-wide (one listener, one tracker, one registry), so it lives between SBA setup (line 124) and engine construction (line 134).
- **Context refs:** "Architecture Context > Components Involved", "Architecture Context > Safety Envelope"
- **What:**
  - After SBA client loop: construct `reactive.Subsystem` iff `cfg.Reactive.Enabled`. Nil otherwise.
  - If `cfg.Reactive.CoAListener.Enabled`: `listener := reactive.NewListener(...)`; `listener.Start(ctx)`; wait on `listener.Ready()` with 2s bounded timeout; on failure log warning and set `reactive.Subsystem.Listener = nil` (session-timeout + backoff still work without the listener).
  - Pass `reactive` subsystem into `engine.New(...)`.
  - On shutdown (after engine drain, before Diameter/SBA teardown): `_ = listener.Stop(shutdownCtx)` — closes UDP socket and waits for reader goroutine.
  - Log startup line: `zerolog .Info().Bool("reactive", cfg.Reactive.Enabled).Bool("coa_listener", cfg.Reactive.CoAListener.Enabled).Msg("reactive subsystem ready")` — visible in compose logs to confirm state.
- **Verify:** `go build ./cmd/simulator`; start locally with reactive disabled (default) — startup logs `"reactive=false"`; flip to true with listener enabled — logs `"reactive=true coa_listener=true"`; metrics endpoint exposes the three new vectors (at 0 baseline).

### Wave 3 — Config example + doc + integration test

> Starts after Wave 2 (Tasks 4, 5, 6, 7) merged. Tasks 8 + 9 can run in
> parallel after Task 7 lands.

#### Task 8: Example config + compose comment
- **Files:** Modify `deploy/simulator/config.example.yaml`, Modify `deploy/docker-compose.simulator.yml`
- **Depends on:** Task 1, Task 7
- **Complexity:** low
- **Pattern ref:** Existing `deploy/simulator/config.example.yaml` SBA block (lines 32-46) — same comment style (one-line explanation per field). Existing `deploy/docker-compose.simulator.yml` ports block (lines 28-29).
- **Context refs:** "Architecture Context > Config schema — additive", "Architecture Context > CoA / Disconnect handling" (hostname-based NAS-IP)
- **What:**
  - Append top-level `reactive:` block demonstrating all 7+3 default fields explicit (enabled=false, session_timeout_respect=true, early_termination_margin=5s, reject_backoff_base=30s, reject_backoff_max=600s, reject_max_retries_per_hour=5, coa_listener.{enabled=false, listen_addr, shared_secret=""}). Include comment: "Uncomment `enabled: true` to activate reactive behavior. Set coa_listener.enabled: true to accept Argus-initiated Disconnect/CoA."
  - Change `nas_ip:` for all three operators from `10.99.0.{1,2,3}` to the container name: `nas_ip: argus-simulator` (same value for all three — simulator is one container). Add comment: "Resolves via compose DNS; required for Argus CoA/DM to reach the sim listener."
  - Compose: add a comment above the `ports:` block stating "Port 3799 (CoA/DM) is intentionally NOT published to the host — listener is reachable only on argus-net from the argus-app container."
- **Verify:** YAML parses (`yq eval . deploy/simulator/config.example.yaml > /dev/null`); `make sim-build` succeeds.

#### Task 9: Integration test + architecture doc
- **Files:** Create `internal/simulator/reactive/integration_test.go` (build-tag `integration`), Modify `docs/architecture/simulator.md`
- **Depends on:** Task 6, Task 7
- **Complexity:** medium
- **Pattern ref:** Read `internal/simulator/diameter/integration_test.go` (STORY-083) for build-tag + in-process server pattern. Read `docs/architecture/simulator.md` STORY-084 section for doc structure.
- **Context refs:** "Architecture Context > Session-Timeout respect", "Architecture Context > CoA / Disconnect handling", "Architecture Context > State machine"
- **What:**
  - `integration_test.go`:
    - Spin up a minimal `radius.PacketServer` on ephemeral port that responds Access-Accept with `Session-Timeout=30` + `Reply-Message="ok"`, Accounting-Response on acct ports.
    - Spin up simulator engine with reactive enabled + listener on ephemeral `:3799`.
    - Test `TestReactive_SessionTimeout`: verify AcctStop is sent at t ≈ 25s (scenario duration 60s, timeout 30s, margin 5s → min = 25s).
    - Test `TestReactive_DisconnectMessage`: after session is Active, client (test harness) dials the simulator listener + sends Disconnect-Request → assert DM-ACK written AND session transitions to Terminating within 1s AND AcctStop packet arrives at the test RADIUS server.
    - Test `TestReactive_RejectBackoff`: server returns Access-Reject 5 times; verify sleeps of 30/60/120/240/480s (mocked time via injectable clock), and 6th attempt flips to Suspended (no auth packet sent).
    - Test `TestReactive_DisabledByDefault`: with `reactive.enabled=false`, simulator behaviour is byte-identical to STORY-082 (compare packet sequence).
    - Build-tag `integration`; skipped by default CI; docker compose smoke run verifies in Wave 3 gate step.
  - `docs/architecture/simulator.md`:
    - Append "Reactive Behavior (STORY-085)" section below "5G SBA Client".
    - Sub-sections: (1) opt-in semantics (reactive.enabled + coa_listener.enabled); (2) state machine diagram (ASCII); (3) Session-Timeout respect math; (4) reject backoff curve + per-SIM cooldown; (5) CoA/DM listener — wire-level, port binding rationale, NAS-IP hostname fix; (6) metrics table (3 vectors, exact label values); (7) safety envelope summary; (8) scope boundaries — RADIUS only, SBA/Diameter untouched, bandwidth caps out of scope.
    - One callout block: **"Why hostname-based NAS-IP?"** — explains Argus dials `nasIP:3799`, compose DNS resolves the container name, no IPAM config.
    - One callout block: **"Why bandwidth-cap reaction is out of scope"** — cite `internal/aaa/radius/server.go:571-580` bug (D-034), explain that the simulator cannot cleanly read bandwidth back until the server install is RFC-correct.
- **Verify:** `go test -tags=integration -run "TestReactive" ./internal/simulator/reactive/...`; visual inspection of doc section; `grep -n "STORY-085" docs/architecture/simulator.md` shows the new section.

## Acceptance Criteria

- **AC-1 Session-Timeout respect:** With `reactive.enabled: true` and
  `session_timeout_respect: true`, an Access-Accept carrying
  `Session-Timeout=30` results in Accounting-Stop at `t ≈ 25s` (margin
  = 5s), regardless of scenario's native duration. Verified by
  integration test + metric `SimulatorReactiveTerminationsTotal{cause="session_timeout"} >= 1`.
- **AC-2 Exponential backoff curve:** Five consecutive Access-Reject
  responses for one `(operator, imsi)` produce sleeps of 30/60/120/240/480s
  (± 100ms jitter tolerated). Verified by unit test `TestBackoff_Curve`.
- **AC-3 Disconnect round-trip:** Argus `POST /api/v1/sessions/{id}/disconnect`
  triggers a simulator-side Accounting-Stop within 3 seconds of the DM
  response. Verified by integration test + metric
  `SimulatorReactiveIncomingTotal{kind="dm",result="ack"} >= 1` AND
  `SimulatorReactiveTerminationsTotal{cause="disconnect"} >= 1`.
- **AC-4 CoA Session-Timeout update:** `session.CoASender.SendCoA`
  with `Attributes["Session-Timeout"]=5` against an Active session
  causes that session's AcctStop to be emitted within 10s (5s new
  timeout + 5s interim-interval observation), instead of the
  original scenario duration. Verified by integration test.
- **AC-5 Retry-storm cap:** After 5 Access-Reject in a rolling
  1-hour window for `(operator, imsi)`, the simulator skips the 6th
  auth attempt entirely (marks SIM suspended in-memory). Verified by
  unit test + metric `SimulatorReactiveRejectBackoffsTotal{outcome="suspended"} >= 1`.
- **AC-6 Reactive disabled → byte-identical:** `reactive.enabled: false`
  (default) mode produces the exact RADIUS packet sequence as STORY-082.
  Verified by packet-capture comparison in integration test + `go
  test ./internal/simulator/...` passes on PR head with zero regression.
- **AC-7 CoA listener only binds when enabled:** Simulator does NOT
  bind UDP :3799 when `coa_listener.enabled: false` (or
  `reactive.enabled: false`). Verified by lsof inspection in compose
  smoke; documented runbook step.
- **AC-8 CoA/DM bad-secret silent drop:** Packets with wrong
  Message-Authenticator are dropped without response; metric
  `SimulatorReactiveIncomingTotal{result="bad_secret"}` increments.
  Verified by unit test `TestListener_BadSecret`.
- **AC-9 Compose reachability:** With hostname-based `nas_ip: argus-simulator`,
  `docker compose exec argus-app nslookup argus-simulator` resolves
  inside the compose network, and a test `POST
  /api/v1/sessions/{id}/disconnect` succeeds (DM-ACK observed in Argus
  logs). Verified by compose smoke runbook.

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| AC-1 | Task 6 (SessionTimeout parse + deadline min) | Task 6 `client_test.go`, Task 9 integration `TestReactive_SessionTimeout` |
| AC-2 | Task 4 (RejectTracker) | Task 4 `TestBackoff_Curve` |
| AC-3 | Task 5 (listener handleDM), Task 6 (cause classification) | Task 5 `TestListener_DM`, Task 9 integration `TestReactive_DisconnectMessage` |
| AC-4 | Task 5 (listener handleCoA), Task 6 (UpdateDeadline read at interim tick) | Task 5 `TestListener_CoA`, Task 9 integration `TestReactive_CoASessionTimeout` |
| AC-5 | Task 4 (sliding-window), Task 6 (Allowed check before auth) | Task 4 `TestBackoff_Suspend`, Task 9 integration `TestReactive_RetryStorm` |
| AC-6 | Task 6 (nil-reactive path preserved) | Task 6 build/test, Task 9 integration `TestReactive_DisabledByDefault` |
| AC-7 | Task 7 (main.go conditional Start) | Compose smoke runbook in doc |
| AC-8 | Task 5 (secret validation) | Task 5 `TestListener_BadSecret` |
| AC-9 | Task 8 (nas_ip rename) | Compose smoke runbook |

## Story-Specific Compliance Rules

- **No new third-party dependencies.** `layeh.com/radius` already in
  `go.mod` at `v0.0.0-20231213012653-1006025d24f8`; covers both
  encode/decode of Disconnect/CoA packets and the RFC 2865 AVP lookups.
- **Single-writer metric pattern (PAT-001) preserved.** Engine is the
  sole writer for `SimulatorReactiveRejectBackoffsTotal` and
  `SimulatorReactiveTerminationsTotal`; listener is the sole writer
  for `SimulatorReactiveIncomingTotal`. Inner layers (state-machine
  transitions, RejectTracker) return typed results, never touch
  counters.
- **Reactive subsystem opt-in.** Default `reactive.enabled: false` must
  produce byte-identical output to pre-STORY-085 binary. Task 6 diff
  MUST be reviewable as "additive only" — existing code paths changed
  structurally only where reactive is explicitly wired.
- **CoA listener port NOT published on host.** Compose
  `ports:` block must NOT include `:3799` — AC-7 depends on it and
  STORY-082 safety envelope disallows exposing test-only services.
- **Hostname-based NAS-IP.** config.example.yaml MUST use
  `argus-simulator` for all operators (compose DNS resolves it).
  Real-world deployments would set a container-network-addressable
  hostname.
- **No DB changes.** Simulator remains read-only via
  `internal/simulator/discovery/`; no migration files produced.
- **SIMULATOR_ENABLED env guard** preserved (`cmd/simulator/main.go:33-37`).
- **ADR-002 (JWT auth), ADR-003 (bcrypt):** N/A — reactive is
  wire-level, no user-identity concerns.
- **ADR compliance:** reactive listener does not introduce a new HTTP
  endpoint on argus-app; it exposes a UDP port on the simulator
  (internal-only). No platform-side ADR touched.

## Bug Pattern Warnings

Drawn from `docs/brainstorming/bug-patterns.md` (1 pattern as of
2026-04-17) + STORY-084 gate report lessons.

- **PAT-001 — Double-writer on abort/session-end metric vectors.** Apply
  to all three new reactive counters. Listener returns typed result
  values (ack/nak/unknown_session/bad_secret); engine classifies
  termination cause. Tests must assert total-over-all-reasons for one
  logical event equals exactly 1.
- **Goroutine leak on listener Stop:** Mirror STORY-083 Diameter peer
  shutdown sequence (`Stop` closes conn, `WaitGroup.Wait` with
  deadline). Regression guard: `go test -race ./internal/simulator/reactive/...`.
- **UDP bind failure at startup:** If `:3799` is already bound (e.g.
  operator runs two sim instances), bind returns `EADDRINUSE`; Task 7
  logs warning and nils the listener rather than panic'ing. Engine
  continues without reactive CoA — session-timeout + reject-backoff
  still function.
- **Metric label cardinality:** `cause` label enum is fixed at 6
  values; `outcome` at 3; `kind` at 2; `result` at 4. No user-derived
  values leak into labels (AcctSessionID / IMSI / operator all stay on
  logs, not metrics, except `operator` which is already bounded to
  ≤ 10 operators in practice).
- **`rfc2865.SessionTimeout_Lookup` edge case:** When the AVP is
  absent, the function returns `0, ErrNotFound`. Task 6 MUST check for
  `errors.Is(err, radius.ErrNoAttribute)` and treat as "no server
  deadline" (fall through to scenario deadline) — NOT as a config
  error.
- **Reply-Message encoding:** Some NASes send RFC-6158-style UTF-8;
  others send raw bytes. Task 6 uses `rfc2865.ReplyMessage_LookupString`
  which does the conversion safely; log at debug only (avoids crash
  on malformed bytes).

## Tech Debt (from ROUTEMAP)

Scanned `docs/ROUTEMAP.md` §Tech Debt (lines 362-398 visible). No OPEN
item targets STORY-085. Phase 10 residuals D-032 and D-033 are
scheduled for STORY-087 and STORY-088 respectively — not this story.

**New tech debt emitted by STORY-085:**

- **D-034 (NEW):** `internal/aaa/radius/server.go:571-580` installs
  bandwidth caps at raw `radius.Type(11)` (RFC 2865 Filter-Id — already
  written at line 569, collision) and `radius.Type(12)` (RFC 2865
  Framed-MTU, semantically wrong). Policy engine's
  `policyResult.BandwidthDown/Up` values are lost on the wire. Fix:
  use a proper VSA (Mikrotik-Rate-Limit vendor-specific attribute, or
  a 3GPP QoS bundle when on Diameter). Simulator-side bandwidth-cap
  reaction was cut from STORY-085 scope because of this bug —
  revisit after D-034 resolves. Target story: TBD (mention in Mini
  Phase Gate and ROUTEMAP update step of STORY-085 close).

## Mock Retirement

Not applicable — simulator generates mock traffic; no UI mocks
involved. STORY-085 does not touch frontend.

## Risks & Mitigations

- **Argus-side DM/CoA secret mismatch.** If the simulator's
  `reactive.coa_listener.shared_secret` differs from Argus's
  `RADIUS_SECRET` (the secret `session.CoASender` is constructed
  with), all incoming packets fail Message-Authenticator verification
  and silently drop. Mitigation: default behaviour inherits
  `argus.radius_shared_secret` from the same YAML block; doc in
  Task 8 calls this out; metric `result="bad_secret"` is the canary.
- **Hostname-resolution failure on some compose setups.** If the
  simulator's `nas_ip: argus-simulator` is put through a custom DNS
  resolver that doesn't know compose service names (e.g. host-network
  mode), the hostname won't resolve. Mitigation: doc states "requires
  default compose bridge network (argus-net)"; failure manifests as
  Argus-side DM errors ("dial NAS argus-simulator: lookup:
  no such host") which are already logged at error level.
- **State-machine race between scheduler cancellation and DM.** If
  `ctx.Done()` fires at the same instant as an incoming DM, both
  attempt to drive Active → Terminating. Mitigation: CAS transition
  helper guarantees one wins; loser logs at warn and returns. PAT-001
  preserved because termination-cause label is recorded by the engine,
  not the listener.
- **Listener port already bound (`EADDRINUSE`).** Two simulator
  instances on the same host, or a leftover process after ungraceful
  shutdown. Mitigation: Task 7 catches bind error and logs warning;
  engine continues without CoA (session-timeout + reject-backoff still
  work). Docker compose `restart: "no"` (see compose file line 20)
  already means sim doesn't auto-restart.
- **Aggressive per-SIM suspend hiding real issues.** If all 3
  operators × 200 SIMs hit the suspension wall during a genuine Argus
  outage, all reactive sim traffic stops. Mitigation: 1-hour bucket
  is per-IMSI, not global; `SimulatorReactiveRejectBackoffsTotal{outcome="suspended"}`
  gauge makes mass-suspension visible on dashboards.
- **Session-Timeout mid-interim timing boundary.** A server timeout
  of 30s, early-termination margin of 5s, interim interval of 60s
  means the interim loop never ticks (25s deadline < 60s tick). The
  engine's existing `select { case <-ticker.C: ...}` fires every 60s,
  so a sub-60s deadline requires a second timer. Mitigation: engine's
  interim loop uses a composite `time.After(effectiveDeadline -
  now)` AND `ticker.C` — whichever fires first wins. Simpler
  alternative (chosen in Task 6): reduce the `select` block's
  `time.Now().After(deadline)` check so that after deadline, any
  `<-ticker.C` OR immediate loop iteration both route to `goto stop`.
  One-liner check added before the `goto stop` label. Tested in Task 9.

## Dependencies

- **STORY-082** complete (simulator base binary + config + RADIUS
  engine lifecycle).
- **STORY-083** complete (per-operator opt-in pattern, metric naming
  convention, engine-nil-subsystem pattern).
- **STORY-084** complete (per-session protocol fork, single-writer
  PAT-001 precedent, config validation chain
  `validateDiameter → validateSBA → (new) validateReactive`).
- **STORY-086** complete (unrelated to simulator; boot schema guard —
  must not break simulator DB discovery read). Verified by
  `docs/ROUTEMAP.md` marking STORY-086 DONE at 2026-04-17.
- **No new `go.mod` dependency required.** `layeh.com/radius` already
  covers DM/CoA codec.

## Out of Scope

- **Reactive behavior for Diameter (Gx ASR / Gy ASA).** If Argus sends
  Abort-Session-Request over Gx, the simulator would need a peer-side
  ASR handler in `internal/simulator/diameter/`. Deferred — reactive
  is RADIUS-only for this story.
- **Reactive behavior for 5G SBA (UE-Context-Termination).**
  Argus's SBA layer has no push-to-UE equivalent currently; deferred
  until Argus adds a notification channel.
- **Bandwidth-cap reaction.** Cut entirely. See §Drift Notes #3 +
  §Tech Debt D-034. When D-034 is fixed and Argus installs a
  parseable bandwidth VSA, a follow-up story can re-introduce the
  simulator-side clamp in the interim byte generator.
- **Actual IP-layer packet shaping.** Simulator remains at the RADIUS
  protocol layer — no real packets, no netem-style throttling.
- **Per-operator CoA shared secret.** Argus currently uses one global
  RADIUS secret (`RADIUS_SECRET` env); multi-tenant secret splitting
  is tracked in a future story.
- **Fault injection / CoA-spoofing tests.** Out of scope; security
  testing is a separate track.
- **State-machine persistence across simulator restarts.** Reject
  history + active-session registry are in-memory only. A restart
  during a rejection storm resets the counter — acceptable because
  max cooldown is 1 hour.
- **Accounting-Start reject handling.** Argus's accounting path
  always replies with Accounting-Response (it's a sink, not an
  auth); there's no reject to react to. STORY-085 does not touch
  the accounting flow.

## Out-of-band Setup

- When Argus RADIUS_SECRET changes in environment, simulator
  `reactive.coa_listener.shared_secret` must be updated (or the
  inherit-from-`argus.radius_shared_secret` default re-applied).
- First run on hostname-based NAS-IP should be verified by:
  ```
  docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.simulator.yml up -d
  docker compose exec argus-app nslookup argus-simulator  # expect A record
  curl -X POST -H "Authorization: Bearer $TOKEN" \
      http://localhost:8084/api/v1/sessions/$SID/disconnect
  # Then grep argus-app logs for "DM sent", grep simulator logs for "DM ACK"
  ```

---

## Quality Gate (plan self-validation)

Run before dispatching Wave 1. Plan FAILS if any check is FALSE.

### Substance
- [x] Story Effort = L-XL (ROUTEMAP line 224). Plan meets XL minimums: 9 tasks across 3 waves; plan ≥ 120 lines. Actual: 9 tasks, ~560 lines.
- [x] At least 2 tasks marked `Complexity: high` (Tasks 5 and 6).

### Required Sections
- [x] `## Goal`
- [x] `## Architecture Context` (with Verified disk layout, Argus-side reference points, Components Involved, Reuse strategy, Config schema, State machine, CoA handling, Reject backoff, Session-Timeout respect, Reply-Message, Metrics, Safety Envelope, AC Mapping — Argus side)
- [x] `## Tasks` (numbered, wave-grouped)
- [x] `## Acceptance Criteria` and `## Acceptance Criteria Mapping`
- [x] `## Story-Specific Compliance Rules`
- [x] `## Bug Pattern Warnings`
- [x] `## Tech Debt (from ROUTEMAP)` (incl. new D-034 emitted by this story)
- [x] `## Mock Retirement`
- [x] `## Risks & Mitigations`
- [x] `## Dependencies`
- [x] `## Out of Scope`
- [x] `## Quality Gate` (this section)

### Embedded Specs (self-contained)
- [x] Config schema embedded with Go struct + YAML examples + validation code sketch.
- [x] State machine diagram + explicit transition table embedded.
- [x] CoA/DM wire-level flow embedded (packet codes, secret validation, ACK/NAK choice).
- [x] Metrics label sets embedded (3 vectors × 4 labels each).
- [x] Argus-side reference points enumerated with `file:line` citations.

### Code-State Validation
- [x] Disk verified 2026-04-17:
  - `cmd/simulator/main.go` (present, post-STORY-084 wiring at lines 109-124).
  - `internal/simulator/{config,discovery,engine,metrics,radius,scenario,diameter,sba}/` all present.
  - `internal/aaa/session/{coa.go,dm.go}` present — Argus-side CoA/DM sender. Codepoints `radius.Code(40)`, `(41)`, `(43)`, `(44)` verified at `dm.go:16` and `coa.go:16`.
  - `internal/aaa/radius/server.go` — Session-Timeout install verified at line 418 (EAP) and 567 (direct auth). FilterID collision at line 574 verified; documented as D-034.
  - `internal/aaa/session/session.go:43` — `Session.NASIP string` is the persisted column Argus dials.
- [x] `go.mod` checked: `layeh.com/radius v0.0.0-20231213012653-1006025d24f8` direct dep; no new module needed.
- [x] STORY-082 RADIUS lifecycle shape preserved (engine.go fork-point is BEFORE auth, reactive hooks are additive to the RADIUS branch only).
- [x] STORY-083 Diameter bracket unchanged.
- [x] STORY-084 SBA fork unchanged (`engine.go:138-154` remains byte-identical).
- [x] PAT-001 single-writer pattern honoured; engine is sole writer for session-level counters.
- [x] No compose host-port binding for `:3799` — AC-7 enforceable.

### Task Decomposition
- [x] All 9 tasks touch ≤3 files (Task 6 is the largest at 3: engine.go, client.go, client_test.go).
- [x] Each task has `Depends on`, `Complexity`, `Pattern ref`, `Context refs`, `What`, `Verify`.
- [x] DB-first ordering N/A (no DB changes). Wave 1 (foundations) before Wave 2 (core logic) before Wave 3 (config/test/doc).
- [x] Wave 1 tasks (1, 2, 3) independent → parallel dispatch possible.
- [x] Wave 2 tasks (4, 5, 6) serial — each depends on prior foundational artifact. Task 7 is independent of 4-6 and can run in parallel with them after Task 1 lands.
- [x] Wave 3 tasks (8, 9) parallel after Wave 2 completes.
- [x] Context refs all reference sections that exist in this plan (verified).

### Test Coverage
- [x] Each AC mapped to a task that implements it AND a task that verifies it (table above).
- [x] Unit tests planned in same task as code (Tasks 1, 2, 4, 5).
- [x] Integration test gated behind `-tags=integration` (Task 9).

### Drift Notes (relative to 2026-04-14 plan)
1. **Reactive scope narrowed to RADIUS-only.** SBA has no CoA
   equivalent; Diameter ASR is future.
2. **CoA/DM routing unblocked via hostname-based NAS-IP** (new §CoA
   handling subsection). Old plan assumed Argus could reach
   `10.99.0.1` — verified false; hostname resolution via compose DNS
   is the minimal fix.
3. **Bandwidth-cap reaction RETIRED.** Argus's install path is
   RFC-broken (`server.go:571-580`); simulator cannot cleanly sense
   the value. Tracked as D-034, retired to Out of Scope.
4. **Single-writer PAT-001 applied to new counters.** Old plan
   implied listener would emit abort-reason metrics directly;
   revised to engine-only writes.
5. **Per-SIM retry suspension is in-memory, not persistent.**
   Old plan implied "SIM marked suspended" as a DB state change; no
   such facility exists in the simulator.
6. **CoA secret inherits from `argus.radius_shared_secret`** by
   default + optional `ARGUS_SIM_COA_SECRET` env override. Old plan
   mandated env-only; YAML is documented-secure since this is a
   dev-tool config file.
7. **SBA + Diameter paths explicitly untouched.** No reactive hooks
   in runSBASession or Diameter bracket. This keeps the additive /
   non-regression claim clean.
8. **AC count increased from 9 (old plan) to 9 (revised)** — same
   numerical count; AC-1/AC-6/AC-9 reshaped; AC-7/AC-8 new (bind
   gate, bad-secret drop); old plan's AC-9 (policy rollout
   end-to-end) deferred — that's an E2E test, not a story AC.

### Reuse Strategy Validation
- [x] `layeh.com/radius` symbols enumerated (table in §Reuse strategy).
- [x] No third-party lib added; `go.mod` unchanged by STORY-085.
- [x] STORY-083/084 package layouts followed as templates for
  `internal/simulator/reactive/`.
- [x] Argus `internal/aaa/session/{coa.go,dm.go}` is a reference, not
  an import. The simulator is a wire-level peer; it re-derives
  codepoints rather than consuming the Argus package (keeps the
  simulator independent of Argus internal packages, per STORY-082's
  "simulator does not import argus-app packages except types" rule —
  DM/CoA codes are RFC constants anyway).

### Result

**PLAN GATE: PASS** — all substance, required-sections, embedded-spec,
code-state, task-decomposition, test-coverage, drift-note, and
reuse-strategy checks satisfied. Plan is dispatch-ready for Wave 1.

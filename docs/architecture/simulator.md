# Simulator Architecture Reference — Argus

> The Argus simulator (`cmd/simulator/`) generates realistic AAA traffic against a
> running Argus stack for load testing, regression, and integration verification.
> Implementation packages: `internal/simulator/{config,discovery,engine,metrics,radius,scenario,diameter,sba,reactive}/`
> Introduced in STORY-082 (RADIUS), extended in STORY-083 (Diameter), STORY-084 (5G SBA), STORY-085 (Reactive Behavior).

---

## RADIUS Client (STORY-082)

The simulator's RADIUS client (`internal/simulator/radius/client.go`) sends
Auth / Accounting-Start / Accounting-Interim / Accounting-Stop sequences that
mirror real NAS traffic. Each operator has its own `NAS-Identifier` and
`NAS-IP-Address`; sessions are driven by a weighted-random scenario picker.
Rate limiting (`rate.max_radius_requests_per_second`) prevents start-up bursts.

---

## Diameter Client (STORY-083)

### Overview — opt-in semantics per operator

Diameter support is disabled by default. An operator opts in by adding a
`diameter:` block with `enabled: true` to its entry in `config.example.yaml`.
Operators without that block remain RADIUS-only and are unaffected by Diameter
lifecycle events. Diameter never blocks RADIUS: peer loss causes the session to
skip Diameter and continue with RADIUS Accounting unchanged.

### Config

```yaml
# Global defaults (top-level `diameter:` block)
diameter:
  host: argus-app
  port: 3868
  origin_realm: sim.argus.test
  # destination_realm must equal Argus DIAMETER_ORIGIN_REALM (default argus.local)
  destination_realm: argus.local
  watchdog_interval: 30s
  connect_timeout: 5s
  request_timeout: 5s
  reconnect_backoff_min: 1s
  reconnect_backoff_max: 30s

operators:
  - code: turkcell
    nas_identifier: sim-turkcell
    nas_ip: 10.99.0.1
    diameter:
      enabled: true
      # origin_host defaults to "sim-<operator-code>.<origin_realm>" when omitted
      applications: [gx, gy]   # gx = Gx (App-ID 16777238); gy = Gy (App-ID 4)
  - code: vodafone
    nas_identifier: sim-vodafone
    nas_ip: 10.99.0.2
    # no diameter block → RADIUS-only
```

**Config fields reference (`DiameterDefaults`)**

| Field | Default | Description |
|-------|---------|-------------|
| `host` | `argus-app` | Argus Diameter server hostname (inside compose network) |
| `port` | `3868` | TCP port; matches `DIAMETER_PORT` default |
| `origin_realm` | `sim.argus.test` | Realm declared in CER and every CCR from the simulator |
| `destination_realm` | `argus.local` | Target realm; must equal server's `DIAMETER_ORIGIN_REALM` |
| `watchdog_interval` | `30s` | DWR → DWA period (RFC 6733 §5.5) |
| `connect_timeout` | `5s` | TCP dial + CER/CEA handshake deadline |
| `request_timeout` | `5s` | Per-CCR round-trip deadline |
| `reconnect_backoff_min` | `1s` | Initial reconnect backoff after peer loss |
| `reconnect_backoff_max` | `30s` | Backoff ceiling (exponential, capped here) |

**Per-operator fields (`OperatorDiameterConfig`)**

| Field | Default | Description |
|-------|---------|-------------|
| `enabled` | `false` | Master switch; must be `true` to enable any Diameter traffic |
| `origin_host` | `sim-<code>.<origin_realm>` | Origin-Host AVP sent by the simulator for this operator |
| `applications` | `[gx, gy]` | Subset of supported apps; `gx` = Policy Control, `gy` = Online Charging |

### Data Flow

For each session belonging to a Diameter-enabled operator the engine issues the
following sequence. RADIUS messages are unchanged from STORY-082; Diameter
messages are added in brackets.

```
RADIUS Auth (Access-Request → Access-Accept)   :1812
  │
  ├─ [Gx CCR-I → CCA-I]   :3868  (install policy; Application-ID 16777238)
  └─ [Gy CCR-I → CCA-I]   :3868  (open credit-control session; App-ID 4)
       only when "gy" in operator's applications list

RADIUS Acct-Start          :1813

  interim loop (every interim_interval_seconds):
    RADIUS Acct-Interim    :1813
    [Gy CCR-U → CCA-U]    :3868  (report delta usage, request next quota)
       only when "gy" in applications list; failure is non-fatal

RADIUS Acct-Stop           :1813

  [Gx CCR-T → CCA-T]      :3868  (terminate policy session)
  [Gy CCR-T → CCA-T]      :3868  (final used-service-unit report)
```

`Session-Id` for both Gx and Gy is set to `sc.AcctSessionID` (the RADIUS
`Acct-Session-Id` UUID). Argus's Gx and Gy handlers share the same
`SessionStateMap` keyed by `Session-Id`, so RADIUS and Diameter sessions are
correlated server-side automatically.

### Peer Lifecycle

One TCP connection is maintained per Diameter-enabled operator, independent of
session goroutines. State transitions:

```
Closed ──connect──► Connecting ──send CER──► WaitCEA ──CEA Result=2001──► Open
  ▲                                                                          │
  └─────────── transport error / DWA timeout / DPR received ────────────────┘
              (exponential backoff: reconnect_backoff_min → max)
```

**Watchdog (RFC 6733 §5.5)**

- Every `watchdog_interval`, the peer sends a DWR.
- If DWA is not received before the next tick, the peer transitions to Closed
  and the reconnect loop starts.
- DWR/DWA bookkeeping is mutex-protected (`dwrInFlight` flag) to prevent races.

**Graceful shutdown**

When `Stop(ctx)` is called (after `engine.Run` returns), the peer sends DPR,
waits up to 1 s for DPA, then closes the TCP connection.

### Metrics

Five new Prometheus vectors are registered in `internal/simulator/metrics/`:

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `simulator_diameter_requests_total` | Counter | `operator`, `app`, `type` | CCRs and handshake messages sent (`type`: `ccr_i`, `ccr_u`, `ccr_t`, `cer`, `dwr`) |
| `simulator_diameter_responses_total` | Counter | `operator`, `app`, `result` | CCAs received (`result`: `success`, `error_<code>`, `timeout`, `peer_down`) |
| `simulator_diameter_latency_seconds` | Histogram | `operator`, `app`, `type` | Round-trip latency per message type |
| `simulator_diameter_peer_state` | Gauge | `operator` | Current peer state (`0`=Closed, `1`=Connecting, `2`=WaitCEA, `3`=Open) |
| `simulator_diameter_session_aborted_total` | Counter | `operator`, `reason` | Sessions dropped due to Diameter failure (`reason`: `ccr_i_failed`, `peer_down`, `timeout`, `reject`) |

### Failure Modes

| Failure | Effect on RADIUS | Effect on Diameter | Metric |
|---------|------------------|--------------------|--------|
| Peer down at CCR-I time | Unaffected | Session skips Diameter; Acct-Start is NOT sent (symmetric with RADIUS auth-fail) | `simulator_diameter_session_aborted_total{reason=peer_down}` |
| CCR-I returns non-2001 | Unaffected | Same as peer-down; session aborted before Acct-Start | `simulator_diameter_session_aborted_total{reason=reject}` |
| CCR-U failure (interim) | Unaffected | Logged; counted; session continues (non-fatal) | `simulator_diameter_responses_total{result=error_*}` |
| CCR-T failure | Unaffected | Logged; counted; session still considered complete | `simulator_diameter_responses_total{result=error_*}` |
| All peers down | Unaffected | All new sessions are RADIUS-only until peer recovers | `simulator_diameter_peer_state{operator=*} == 0` |

Diameter failures NEVER block or fail RADIUS traffic for any operator.

### Manual smoke runbook — Argus HTTP CDR assertion (plan AC-4)

The Go test suite covers the CCR wire format (unit) and end-to-end session
lifecycle against an in-process `argusdiameter.NewServer` (`integration` build
tag). Proving that Diameter CDRs surface on Argus's HTTP gateway
(`GET /api/v1/cdrs?protocol=diameter`) requires a running Argus stack with a
seeded tenant + SIM database and therefore lives outside the automated test
layer. Run the following manual smoke after deploying:

```bash
# 1. Bring up the full stack (Argus + simulator).
make up                                    # argus-app + pg + redis + nats
make sim-up                                # simulator with diameter enabled for turkcell

# 2. Wait 2 minutes so at least a few Diameter-bracketed sessions complete.
sleep 120

# 3. Query Argus for Diameter-protocol CDRs.
curl -sSf -H "X-Tenant-ID: $TENANT_ID" -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8084/api/v1/cdrs?protocol=diameter&limit=10" | jq '.data | length'
#   → expect a non-zero integer

# 4. Tear down.
make down
```

Automating this smoke would require provisioning a tenant + SIM row + auth
token from within the test binary — deferred as a future enhancement. The
primary evidence for AC-4 remains the integration test
(`TestSimulator_AgainstArgusDiameter`) which verifies `ActiveSessionCount`
transitions on the Argus Diameter server directly; the HTTP-side assertion
is covered manually via the runbook above.

### Tech debt / future enhancements

- **Busy-poll → state-change hook** (F-A4): `Client.Start` currently polls
  `Peer.State()` every 5 ms to detect the first transition to `Open`. This is
  correct but wasteful. A future refactor can route `PeerConfig.OnStateChange`
  callbacks to a subscription channel and block on that instead.
- **Gx Update-phase support** (F-A5 follow-up): `CloseSession` hardcodes
  `reqNum=1` for Gx CCR-T because Gx has no Update phase in Argus. If Gx
  Update is ever added, replace with a per-session counter analogous to Gy.

### Reuse Note

The simulator's `internal/simulator/diameter/` package imports Argus's own
Diameter implementation directly:

```go
import argusdiameter "github.com/btopcu/argus/internal/aaa/diameter"
```

Reused symbols include AVP constructors (`NewAVPUint32`, `NewAVPUint64`,
`NewAVPGrouped`, `NewAVPAddress`, `BuildSubscriptionID`), the wire-format codec
(`Message`, `Encode`, `DecodeMessage`), command codes, application IDs, and AVP
code constants. No external Diameter library (`fiorix/go-diameter` or similar)
is used. This guarantees byte-for-byte compatibility with the Argus server:
AVP codes, vendor flags, and padding are identical to what the server expects,
eliminating dictionary mismatch as a risk category.

---

## 5G SBA Client (STORY-084)

### Overview — opt-in per operator, alternative protocol fork

5G SBA support is disabled by default. An operator opts in by adding an `sba:`
block with `enabled: true` to its entry in `config.example.yaml`. Operators
without that block remain RADIUS-only and are unaffected by SBA lifecycle events.

SBA is an **alternative protocol fork**, not an addition on top of Diameter. For
a given session, the engine selects the protocol path using the `Rate`-based
picker (`internal/simulator/sba/picker.go`): sessions whose AcctSessionID hash
falls below the configured rate follow the SBA path; all others follow RADIUS
(with optional Diameter). SBA never stacks on top of Diameter for the same
session.

### Config

```yaml
# Global defaults (top-level `sba:` block)
sba:
  host: argus-app
  port: 8443
  tls_enabled: false
  tls_skip_verify: false
  serving_network_name: "5G:mnc001.mcc286.3gppnetwork.org"
  request_timeout: 5s
  amf_instance_id: sim-amf-01
  dereg_callback_uri: "http://sim-amf.local/dereg"
  include_optional_calls: false

operators:
  - code: turkcell
    nas_identifier: sim-turkcell
    nas_ip: 10.99.0.1
    sba:
      enabled: true
      rate: 0.2          # fraction of sessions routed via SBA (0.0–1.0)
      auth_method: 5G_AKA
  - code: vodafone
    nas_identifier: sim-vodafone
    nas_ip: 10.99.0.2
    # no sba block → RADIUS-only
```

**Config fields reference (`SBADefaults`)**

| Field | Default | Description |
|-------|---------|-------------|
| `host` | `argus-app` | Argus SBA server hostname (inside compose network) |
| `port` | `8443` | TCP port; matches `SBA_PORT` default |
| `tls_enabled` | `false` | Enable HTTPS + HTTP/2 ALPN (dev compose uses cleartext) |
| `tls_skip_verify` | `false` | Skip TLS certificate verification (dev only) |
| `serving_network_name` | `5G:mnc001.mcc286.3gppnetwork.org` | 3GPP serving network name sent in authentication requests |
| `request_timeout` | `5s` | Per-HTTP-request deadline |
| `amf_instance_id` | `sim-amf-01` | AMF instance UUID sent in UDM registration body |
| `dereg_callback_uri` | `http://sim-amf.invalid/dereg` | Callback URI embedded in AMF registration (informational) |
| `include_optional_calls` | `false` | When `true`, a per-session 20% Bernoulli roll prepends `GET /nudm-ueau/v1/{supi}/security-information` before Authenticate and appends `POST /nudm-ueau/v1/{supi}/auth-events` after Confirm. Optional-call failures are logged and discarded |
| `prod_guard` | `true` | When `true` AND `tls_skip_verify: true`, Validate rejects the config under `ARGUS_SIM_ENV=prod`. Set to `false` only for exceptional hand-crafted prod-like fixtures |

**Per-operator fields (`OperatorSBAConfig`)**

| Field | Default | Description |
|-------|---------|-------------|
| `enabled` | `false` | Master switch; must be `true` to route any sessions via SBA |
| `rate` | `0.0` | Fraction of sessions sent via SBA (0.0 = none; 1.0 = all). Determined deterministically per AcctSessionID |
| `auth_method` | `5G_AKA` | Authentication method; only `5G_AKA` is implemented in STORY-084 (EAP_AKA_PRIME is reserved for a future story) |
| `slices` | `[{sst: 1, sd: "000001"}]` | Optional S-NSSAI set advertised in RequestedNSSAI on authentication |

### Data Flow

For each session belonging to an SBA-enabled operator whose AcctSessionID hash
passes the rate check, the engine calls `runSBASession(ctx, sim, op, sample, sbaC, log)`:

```
[optional, when include_optional_calls && Bernoulli(0.2)]
GET  /nudm-ueau/v1/<supi>/security-information      :8443  (security-info)
   → 200 OK  (response body unused; pure traffic exercise)

POST /nausf-auth/v1/ue-authentications              :8443  (authenticate)
   → 201 Created, body: AuthenticationResponse + Links["5g-aka"].Href

PUT  <authCtxHref>/5g-aka-confirmation              :8443  (confirm)
   → 200 OK, body: ConfirmationResponse{SUPI, Kseaf, AuthResult="SUCCESS"}

PUT  /nudm-uecm/v1/<supi>/registrations/amf-3gpp-access  :8443  (register)
   → 201 Created (first registration) or 200/204 (idempotent re-registration)

   <session held for sample.SessionDuration or until context cancellation>

[optional, when include_optional_calls && Bernoulli(0.2)]
POST /nudm-ueau/v1/<supi>/auth-events               :8443  (auth-events)
   → 201 Created  (session-end authentication log)
```

Authentication failure at step 2 or 3 causes an immediate abort (metric
`simulator_sba_session_aborted_total{reason=auth_failed|confirm_failed|...}`
emitted once by the engine). Registration failure at step 4 aborts with
`reason=register_failed`. The context hold ends when the sample duration
elapses or the engine cancels the session context. Optional calls are
best-effort — failures are logged and discarded.

Deregister is **not** part of the minimum flow. Argus's current
`HandleRegistration` only accepts `PUT` and returns 405 for `DELETE`; until a
future story implements server-side AMF deregistration, the simulator does not
emit `DELETE /nudm-uecm/...` requests.

### Crypto

Authentication uses 5G-AKA. The simulator derives the expected `xresStar`
locally and sends it in the confirmation PUT body so that Argus's AUSF can
verify `sha256(xresStar)[:16] == hxresStar` from its own derivation.

Both the server (`internal/aaa/sba/ausf.go`) and the simulator
(`internal/simulator/sba/ausf.go`) compute authentication vectors from the same
deterministic pseudo-random function keyed on `(SUPI, servingNetworkName)`:

```
seed     = SHA-256("5g-av:" + supi + ":" + servingNetwork)
rand     = PRF(seed, index=0, length=16)
autn     = PRF(seed, index=1, length=16)
xresStar = PRF(seed, index=2, length=16)
kausf    = PRF(seed, index=3, length=32)
```

The canary test `TestCrypto_Canary` in `ausf_test.go` fires the real Argus AUSF
handler against the simulator's locally-computed `xresStar` and asserts that
the SHA-256 hash matches `hxresStar` returned by the server. If
`internal/aaa/sba/ausf.go` ever changes its key derivation, the canary fails
immediately, preventing silent drift.

### Metrics

Five new Prometheus vectors are registered in `internal/simulator/metrics/`:

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `simulator_sba_requests_total` | Counter | `operator`, `service`, `endpoint` | HTTP requests sent (`service`: `ausf`/`udm`; `endpoint`: `authenticate`, `confirm`, `register`, `security-info`, `auth-events`) |
| `simulator_sba_responses_total` | Counter | `operator`, `service`, `endpoint`, `result` | HTTP responses received (`result`: `success`, `error_4xx`, `error_5xx`, `timeout`, `transport`) |
| `simulator_sba_latency_seconds` | Histogram | `operator`, `service`, `endpoint` | Round-trip latency per endpoint |
| `simulator_sba_session_aborted_total` | Counter | `operator`, `reason` | Sessions aborted before completion (`reason`: `auth_failed`, `confirm_failed`, `register_failed`, `transport`, `timeout`) |
| `simulator_sba_service_errors_total` | Counter | `operator`, `service`, `cause` | Non-2xx responses broken down by ProblemDetails.Cause (`MANDATORY_IE_INCORRECT`, `AUTH_REJECTED`, `SNSSAI_NOT_ALLOWED`, `AUTH_CONTEXT_NOT_FOUND`, `METHOD_NOT_ALLOWED`, `RESOURCE_NOT_FOUND`, `unknown` when body isn't `application/problem+json`) |

Note: unlike the Diameter client, there is no peer-state gauge. HTTP is
stateless and connectionless from the simulator's perspective — no long-lived TCP
session is maintained between requests; the transport pool reconnects
transparently.

**Label cardinality:** `cause` is bounded by the server's own enum (not
attacker-controlled). Adding a new cause on the server side expands cardinality
by exactly one; this is documented in STORY-084 §Risks.

### Failure Modes

| Failure | Session Effect | Metric |
|---------|---------------|--------|
| Auth POST returns non-201 | Session aborted before register | `simulator_sba_responses_total{result=error_4xx|error_5xx}` + `simulator_sba_service_errors_total{cause=<ProblemDetails.Cause>}` + `simulator_sba_session_aborted_total{reason=auth_failed}` |
| Confirm PUT returns non-200 | Session aborted | Same as above with `endpoint=confirm` and `reason=confirm_failed` |
| Confirm returns 200 but `AuthResult` ≠ `SUCCESS` | Session aborted | `simulator_sba_responses_total{endpoint=confirm,result=success}` + `simulator_sba_session_aborted_total{reason=confirm_failed}` (HTTP succeeded; session-layer reject) |
| Register PUT returns non-2xx | Session aborted with metric | `simulator_sba_session_aborted_total{reason=register_failed}` |
| Transport error (DNS, TCP refused) | Session aborted | `simulator_sba_responses_total{result=transport}` or `result=timeout` |
| Optional security-info or auth-events failure | Logged; error discarded; session continues | (non-fatal — no session-abort metric) |

### Scope exclusion: Deregister not implemented

The current Argus SBA server's `HandleRegistration` only accepts `PUT` and
returns `405 Method Not Allowed` for `DELETE`. Server-side AMF deregistration
is out of scope for STORY-084. The simulator's minimum flow is POST authenticate
→ PUT confirm → PUT register; no `DELETE` traffic is emitted. A future story
can add a deregister surface when Argus's UDM exposes it.

### Reuse Note

The simulator's `internal/simulator/sba/` package imports Argus's own SBA
types directly:

```go
import argussba "github.com/btopcu/argus/internal/aaa/sba"
```

Reused symbols include `AuthenticationRequest`, `AuthenticationResponse`,
`ConfirmationRequest`, `ConfirmationResponse`, `Amf3GppAccessRegistration`,
`SNSSAI`, `GUAMI`, `PlmnID`, `ProblemDetails`, and `AuthLink`. No external 5G
SBA library is used. This guarantees that the simulator sends exactly the JSON
field names and shapes that Argus's handlers expect, eliminating schema mismatch
as a risk category.

### Manual smoke runbook — SBA session verification (plan AC-4)

```bash
# 1. Bring up the full stack and simulator with SBA enabled for at least one operator.
make up
make sim-up   # operator config must have sba: enabled: true, rate: >0

# 2. Wait 2 minutes so a few SBA sessions complete.
sleep 120

# 3. Query the simulator metrics endpoint for SBA counter values.
curl -sSf http://localhost:9099/metrics | grep simulator_sba_requests_total
# → expect non-zero counts for ausf/authenticate, ausf/confirm, udm/register

# 4. Tear down.
make down
```

---

## Reactive Behavior (STORY-085)

### Scope

Approach-B upgrade to the simulator: from "dumb client" (STORY-082 approach A) to
"realistic SIM/modem emulator". When enabled, the simulator:

- Interprets Access-Accept attributes: honors `Session-Timeout` and surfaces
  `Reply-Message` in session context.
- Backs off exponentially on Access-Reject (30s → 600s cap, 5 retries per
  1-hour sliding window, then Suspended).
- Responds to Argus-initiated RADIUS Disconnect-Message (DM, code 40) and
  Change-of-Authorization (CoA, code 43) per RFC 5176, on UDP port 3799.
- Preserves byte-identical behavior when `reactive.enabled: false` (default)
  — STORY-082/083/084 flows are unaffected.

### State machine

```
  Idle ──Auth─► Authenticating ──Accept─► Authenticated ──Start─► Active ─┐
   ▲                  │                                                    │
   │                  Reject                                               │
   │                  ▼                                                    │
   └──cooldown── BackingOff ──max-retries─► Suspended                      │
                                                                           │
   ┌────────── Terminating ◄── DM / deadline / scenario-end ────────────────┘
```

Engine is the single writer of `simulator_reactive_terminations_total`
(PAT-001). Listener sets `Session.DisconnectCause` when it cancels; engine
classifies on teardown.

### Configuration

Top-level `reactive:` block in `config.example.yaml`. All fields optional
with sensible defaults; block is opt-in via `reactive.enabled: true`.

| Field | Default | Purpose |
|---|---|---|
| `enabled` | `false` | Master switch |
| `session_timeout_respect` | `true` | Honor Access-Accept Session-Timeout |
| `early_termination_margin` | `5s` | End session N seconds before Session-Timeout |
| `reject_backoff_base` | `30s` | Base of exponential backoff |
| `reject_backoff_max` | `600s` | Cap |
| `reject_max_retries_per_hour` | `5` | Sliding-window cap; after this → Suspended |
| `coa_listener.enabled` | `false` | Bind UDP :3799 listener |
| `coa_listener.listen_addr` | `0.0.0.0:3799` | Listener bind address |
| `coa_listener.shared_secret` | `""` | Inherits `argus.radius_shared_secret` when empty |

### Metrics

| Vector | Labels | Meaning |
|---|---|---|
| `simulator_reactive_terminations_total` | `operator, cause` | Session ended; cause ∈ {`session_timeout`, `disconnect`, `coa_deadline`, `reject_suspend`, `scenario_end`, `shutdown`} |
| `simulator_reactive_reject_backoffs_total` | `operator, outcome` | Access-Reject triggered backoff; outcome ∈ {`backoff_set`, `suspended`} |
| `simulator_reactive_incoming_total` | `operator, kind, result` | Inbound packet on CoA listener. `kind` ∈ {`dm`, `coa`, `unknown`} (`unknown` = packet code neither 40 nor 43 OR parse failed before code extraction). `result` ∈ {`ack`, `unknown_session`, `bad_secret`, `malformed`, `unsupported`} — `ack` for a session match (both DM and CoA); `unknown_session` is the NAK case with Error-Cause 503; `bad_secret` = Message-Authenticator failed; `malformed` = packet shorter than RADIUS header or `radius.Parse` rejected it; `unsupported` = valid RADIUS packet with a code we do not handle. `operator` is `unknown` when the session cannot be located. |

All counter-only; no histograms/gauges. Sessions emit at most one
termination event.

### CoA/DM routing

Argus sends Disconnect-Message and CoA-Request to the NAS-IP-Address it
recorded from the original Access-Request. STORY-085 resolved the
"NAS-IP unreachability" blocker by switching all three operators in
`deploy/simulator/config.example.yaml` to `nas_ip: argus-simulator` (the
compose container name, DNS-resolvable from the `argus-app` container on
`argus-net`). Because `net.ParseIP` returns `nil` for a hostname, the
`NAS-IP-Address` AVP is silently omitted from the Access-Request (per
RFC 2865 §5.4 `NAS-Identifier` is an acceptable substitute); the
`NAS-Identifier` AVP (e.g. `sim-turkcell`) still carries operator
identity, and Argus persists the hostname string into `sessions.nas_ip`.
At CoA/DM time Argus dials `fmt.Sprintf("%s:%d", req.NASIP, 3799)` —
Go's stdlib resolves the A record via compose's embedded DNS and the
packet reaches the simulator's `0.0.0.0:3799` listener.

**Deployment note**: This works in default compose networking
(`argus-net` bridge). Host-network or alternative DNS resolvers that do
not know compose service names must substitute a container-addressable
hostname or IP.

For local E2E testing, the listener also accepts packets crafted
in-process via `reactive.NewListener` + direct UDP write — see
`internal/simulator/reactive/integration_test.go` (build-tag
`integration`) and `listener_test.go`.

### Out of scope (future stories)

- Bandwidth-cap reaction (requires Argus to install rate-limit attributes
  via standard-compliant RADIUS VSA — see Tech Debt D-035; the pre-existing
  `internal/aaa/radius/server.go:571-580` broken install is tracked there).
- Diameter Abort-Session-Request (ASR) handling (no Argus push mechanism
  today).
- 5G SBA UE-context-termination from AMF (no Argus push mechanism today).
- Persistent suspension state across simulator restarts (in-memory only).

### Testing

```bash
# Unit tests (all build configs)
go test ./internal/simulator/reactive/...

# Integration tests (package-level end-to-end)
go test -tags=integration ./internal/simulator/reactive/...

# Enable reactive in a compose run (default=off)
# Edit deploy/simulator/config.example.yaml:
#   reactive.enabled: true
#   reactive.coa_listener.enabled: true
make sim-up
docker compose logs argus-simulator | grep "reactive subsystem ready"
```

---

## Engine (session orchestration)

`internal/simulator/engine/engine.go` owns the session lifecycle loop. It holds
a RADIUS client and, optionally, a `map[string]*diameter.Client` keyed by
operator code. Nil entries for an operator mean RADIUS-only. The engine's
`New(cfg, picker, radiusClient, dmClients, logger)` constructor accepts the
pre-built client map; `main.go` builds and starts Diameter clients before
constructing the engine.

---

## Metrics endpoint

All simulator metrics are exposed on `:9099/metrics` (configurable via
`metrics.listen`). Both RADIUS and Diameter vectors share the `simulator_`
prefix for easy dashboard grouping.

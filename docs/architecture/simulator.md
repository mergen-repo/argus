# Simulator Architecture Reference — Argus

> The Argus simulator (`cmd/simulator/`) generates realistic AAA traffic against a
> running Argus stack for load testing, regression, and integration verification.
> Implementation packages: `internal/simulator/{config,discovery,engine,metrics,radius,scenario,diameter}/`
> Introduced in STORY-082 (RADIUS), extended in STORY-083 (Diameter).

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

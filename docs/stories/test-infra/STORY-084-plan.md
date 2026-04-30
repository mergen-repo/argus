# Implementation Plan: STORY-084 — Simulator 5G SBA Client (AUSF/UDM)

> Plan revised 2026-04-17 after code-state validation. Material drift from
> 2026-04-14 plan:
>
> 1. **Reuse `internal/aaa/sba` types** (same pattern STORY-083 used for
>    Diameter) — no third-party 3GPP lib needed. The server's request /
>    response structs are public Go types and must be the source of truth.
> 2. **Confirmation is `PUT`, not `POST`** — see `sba/server.go:78-83` +
>    `sba/ausf.go:138-141`. Old plan said POST; that would 405.
> 3. **`AuthResult` value is `"SUCCESS"`, not `"AUTHENTICATION_SUCCESSFUL"`**
>    — see `sba/ausf.go:244`.
> 4. **RAND / AUTN / HxresStar / Kseaf are base64-encoded in JSON**, not hex
>    — see `sba/types.go:39-43` + `sba/ausf.go:114-117, 246`.
> 5. **`GET /nudm-sdm/v1/{supi}/am-data` does NOT exist** on the server. The
>    UECM path is `PUT /nudm-uecm/v1/{supi}/registrations/amf-3gpp-access`
>    (`sba/server.go:101-107`). The UDM-UEAU paths are `GET /nudm-ueau/v1/
>    {supiOrSuci}/security-information` and `POST /nudm-ueau/v1/{supiOrSuci}/
>    auth-events` (`sba/server.go:89-99`). The simulator targets the three
>    real UDM paths, not the non-existent SDM/am-data endpoint.
> 6. **Server transport is HTTP/1.1 cleartext on :8443 by default**: TLS
>    activates only when `TLS_CERT_PATH` + `TLS_KEY_PATH` are set (see
>    `sba/server.go:142-170`); dev compose leaves them empty. Plan therefore
>    treats HTTP/1.1 as the default, HTTP/2 as a conditional opt-in when TLS
>    is enabled on the server side. `http2.Transport` is available via
>    `golang.org/x/net/http2` (already indirect in `go.mod`) — no new module.
> 7. **No `fraction_of_sessions` — use `sba_rate: 0..1`** to match
>    STORY-083's per-operator opt-in style. Selection is per-session
>    (Bernoulli trial on session entry), identical in shape to Diameter's
>    opt-in.

## Goal

Extend the simulator (STORY-082 RADIUS-only, STORY-083 +Diameter) with an
HTTP/1.1 (or HTTP/2 when TLS is enabled) client that exercises Argus's 5G
Service-Based Architecture endpoints (AUSF for 5G-AKA authentication, UDM for
security-info + AMF registration). For operators that opt in
(`operators[].sba.enabled: true`), a configurable fraction of sessions
(`sba.rate`) skip the RADIUS path entirely and authenticate via 5G-SBA,
producing the traffic patterns Argus's `:8443` SBA proxy sees in an actual
5G core.

Default = 5G-SBA disabled (no behaviour change vs STORY-082/083). The 5G
session is **alternative** to RADIUS (not additive) for sessions selected
into the 5G bucket — this reflects the real 5G SA topology where a UE
attaches via SBA directly, not through RADIUS.

## Architecture Context

### Verified disk layout BEFORE this story (2026-04-17)

```
cmd/simulator/
  main.go                       # entry, env guard, orchestrator (wires
                                # Diameter clients into engine already)
  Dockerfile
internal/simulator/
  config/{config.go, config_test.go}       # STORY-083 added DiameterDefaults
  discovery/db.go
  scenario/{scenario.go, scenario_test.go}
  radius/{client.go, client_test.go}
  diameter/                     # STORY-083 package — reuse pattern template
    client.go, client_test.go
    ccr.go, ccr_test.go
    peer.go, peer_test.go
    integration_test.go, doc.go
  engine/engine.go              # STORY-083 added dm map + OpenSession/UpdateGy/CloseSession
  metrics/metrics.go            # STORY-083 added 5 diameter vectors
deploy/simulator/config.example.yaml
docs/architecture/simulator.md  # STORY-083 added Diameter section + runbook
```

### Components Involved

**NEW under `internal/simulator/sba/`:**

```
sba/
  client.go            # High-level façade: Authenticate, Register.
                       # Holds one *http.Client per operator with HTTP/1.1
                       # transport by default; HTTP/2 transport when
                       # TLSEnabled + skip-verify honoured.
  client_test.go       # Mock server asserts method/path/headers/body shape.
  ausf.go              # Two calls: PostAuthentication + PutConfirmation.
                       # Returns auth-ctx ID and Kseaf on success.
  ausf_test.go         # Golden-body and status-code tests via httptest.
  udm.go               # Two calls: GetSecurityInfo + PutRegistration.
  udm_test.go          # httptest assertion for path + method.
  picker.go            # Per-session protocol selector: RADIUS vs SBA vs
                       # Diameter-via-RADIUS. Returns "radius", "sba", or
                       # "radius+diameter" for a given operator.
  picker_test.go       # Fraction bucket math; determinism with fixed seed.
  integration_test.go  # build-tag "integration" — round-trip against an
                       # in-process aaasba.Server.
  doc.go               # Package overview.
```

**MODIFIED:**

```
internal/simulator/config/config.go            # add SBADefaults (global) +
                                               # OperatorSBAConfig (per-op).
internal/simulator/config/config_test.go       # cover new fields + defaults.
internal/simulator/engine/engine.go            # per-session protocol fork:
                                               # RADIUS vs SBA. SBA path
                                               # drives Authenticate →
                                               # Register. Diameter stays
                                               # conditional on RADIUS path.
internal/simulator/metrics/metrics.go          # add 5 SBA metric vectors.
cmd/simulator/main.go                          # construct per-operator SBA
                                               # clients, pass into engine.
deploy/simulator/config.example.yaml           # demonstrate one operator
                                               # opt-in, document defaults.
docs/architecture/simulator.md                 # add 5G SBA section below
                                               # STORY-083's Diameter section.
```

### Reuse strategy — CRITICAL

The simulator's sba package **imports** `internal/aaa/sba` and reuses its
public types for request / response payloads. This guarantees JSON shape
compatibility with the Argus server and eliminates the "drift between
client JSON and server JSON" risk the old plan flagged.

```go
import argussba "github.com/btopcu/argus/internal/aaa/sba"
```

Symbols consumed (all already public in `internal/aaa/sba/types.go`):

| Symbol | File | Purpose |
|--------|------|---------|
| `AuthType`, `AuthType5GAKA`, `AuthTypeEAPAKA` | types.go | Auth method enum |
| `AuthenticationRequest` (fields `SUPIOrSUCI`, `ServingNetworkName`, `RequestedNSSAI`) | types.go | Authenticate body |
| `AuthenticationResponse` (`AuthType`, `AuthData5G` ptr, `Links` map, `SUPI`) | types.go | Authenticate reply |
| `AKA5GAuthData` (`RAND`, `AUTN`, `HxresStar` — base64 strings) | types.go | Challenge data |
| `AuthLink` (`Href`) | types.go | 5g-aka confirmation URL |
| `ConfirmationRequest` (`ResStar`) | types.go | Confirm body |
| `ConfirmationResponse` (`AuthResult`, `SUPI`, `Kseaf`) | types.go | Confirm reply |
| `Amf3GppAccessRegistration` (`AmfInstanceID`, `DeregCallbackURI`, `GUAMI`, `RATType`, `InitialRegInd`) | types.go | AMF-3GPP registration PUT body |
| `GUAMI`, `PlmnID` | types.go | Guami struct for registration body |
| `SNSSAI` (`SST`, `SD`) | types.go | NSSAI entries (optional in authenticate body) |
| `ProblemDetails` (`Status`, `Cause`, `Detail`) | types.go | RFC 7807-style error body (decode for result classification) |

**Server routes to exercise (verified in `sba/server.go:75-107`):**

| Method | Path | Handler | Purpose |
|--------|------|---------|---------|
| POST | `/nausf-auth/v1/ue-authentications` | `AUSFHandler.HandleAuthentication` | Start 5G-AKA; returns auth-ctx link |
| PUT | `/nausf-auth/v1/ue-authentications/{authCtxId}/5g-aka-confirmation` | `AUSFHandler.HandleConfirmation` | Complete 5G-AKA; returns Kseaf |
| GET | `/nudm-ueau/v1/{supiOrSuci}/security-information` | `UDMHandler.HandleSecurityInfo` | UDM auth-vector fetch (optional) |
| POST | `/nudm-ueau/v1/{supiOrSuci}/auth-events` | `UDMHandler.HandleAuthEvents` | Log auth event (optional) |
| PUT | `/nudm-uecm/v1/{supi}/registrations/amf-3gpp-access` | `UDMHandler.HandleRegistration` | AMF registration |
| GET | `/health` | inline | Health probe |

The simulator implements a **minimum flow** covering the two REQUIRED
surface: `POST authenticate → PUT confirm → PUT register`. Optional calls
(`GET security-information`, `POST auth-events`) are **emitted
probabilistically** when `sba.include_optional_calls: true` to broaden
coverage of the proxy.

**Note:** The 2026-04-14 plan's mention of `GET /nudm-sdm/v1/{supi}/am-data`
is retired — no such route exists on the server. Plan targets the actual
registration path instead.

### Crypto — how the simulator makes resStar match server's HxresStar

The server's 5G-AKA flow computes `HxresStar = sha256(xresStar)[:16]`
(ausf.go:351-354) and stores it against a per-context `auth_ctx_id`. On
confirmation, the server checks `sha256(decode(resStar))[:16] == HxresStar`
— **so the simulator wins confirmation by sending `resStar = xresStar`**.

The simulator derives `xresStar` locally by replicating the server's
deterministic `generate5GAV` function (ausf.go:340-349). The algorithm is
pure-Go, stdlib-only, reproducible from `(supi, servingNetworkName)`. The
simulator package **copies the three helper functions** (`generate5GAV`,
`derivePseudoRandom`, `sha256Sum`) into `internal/simulator/sba/crypto.go`
with a comment citing `internal/aaa/sba/ausf.go` as source-of-truth. This
is a conscious duplication (not an import) because the server functions
are unexported; exporting them from the server package solely for the
simulator would clutter the server API.

Golden test in `ausf_test.go` verifies: for fixed `(supi, sn)`, simulator's
local xresStar equals server's. If the server's helpers ever change, this
test fails loudly — deliberate canary.

### Transport — HTTP/1.1 default, HTTP/2 conditional on TLS

Per `sba/server.go:142-170`:
- If `TLSCertPath != "" && TLSKeyPath != ""`: server runs HTTPS with
  `NextProtos = []string{"h2", "http/1.1"}` → HTTP/2 via ALPN is available.
- Else: server runs plain `httpServer.Serve(ln)` → HTTP/1.1 cleartext
  only (Go's net/http does NOT speak h2c without explicit
  `h2c.NewHandler` wiring, which the SBA server does NOT install).

**Dev/compose default is TLS-OFF → simulator's default is HTTP/1.1 cleartext.**

Client configuration:
- `tls_enabled: false` (default): `http.Transport` with plaintext
  connection-pool, keep-alive on. HTTP/1.1.
- `tls_enabled: true`, `tls_skip_verify: true` (dev/stage): `http.Transport`
  with `TLSClientConfig: &tls.Config{InsecureSkipVerify: true, NextProtos:
  []string{"h2", "http/1.1"}}`; additionally configure `http2.Transport` via
  `http2.ConfigureTransport(t)` for automatic h2 negotiation.
- `tls_enabled: true`, `tls_skip_verify: false` (prod-like): same but
  `InsecureSkipVerify: false` — requires valid cert on server.

`golang.org/x/net/http2` is already in `go.mod` as an indirect dep
(`golang.org/x/net v0.52.0` line 109). Promoting it to a direct import
requires no go.mod edit — `go mod tidy` after first use rewrites the
`// indirect` comment, which is auto-handled.

### Config schema — additive

Source: `internal/simulator/config/config.go` (current truth, post-STORY-083).

```go
type Config struct {
    Argus     ArgusConfig
    Operators []OperatorConfig
    Scenarios []ScenarioConfig
    Rate      RateConfig
    Metrics   MetricsConfig
    Log       LogConfig
    Diameter  DiameterDefaults `yaml:"diameter"`
    SBA       SBADefaults      `yaml:"sba"` // NEW — global defaults
}

// NEW — global SBA client defaults (applied to every operator that opts in).
type SBADefaults struct {
    Host                 string        `yaml:"host"`                   // default "argus-app"
    Port                 int           `yaml:"port"`                   // default 8443
    TLSEnabled           bool          `yaml:"tls_enabled"`            // default false (matches dev)
    TLSSkipVerify        bool          `yaml:"tls_skip_verify"`        // default false; required for self-signed dev TLS
    ServingNetworkName   string        `yaml:"serving_network_name"`   // default "5G:mnc001.mcc286.3gppnetwork.org"
    RequestTimeout       time.Duration `yaml:"request_timeout"`        // default 5s
    AMFInstanceID        string        `yaml:"amf_instance_id"`        // default "sim-amf-01"
    DeregCallbackURI     string        `yaml:"dereg_callback_uri"`     // default "http://sim-amf.invalid/dereg"
    IncludeOptionalCalls bool          `yaml:"include_optional_calls"` // default false — if true, random 20% of flows add GET security-information
    ProdGuard            bool          `yaml:"prod_guard"`             // default true; rejects TLSSkipVerify when ARGUS_SIM_ENV=prod
}

// EXTEND OperatorConfig — additive.
type OperatorConfig struct {
    Code          string                  `yaml:"code"`
    NASIdentifier string                  `yaml:"nas_identifier"`
    NASIP         string                  `yaml:"nas_ip"`
    Diameter      *OperatorDiameterConfig `yaml:"diameter,omitempty"`
    SBA           *OperatorSBAConfig      `yaml:"sba,omitempty"` // NEW; nil = disabled
}

// NEW.
type OperatorSBAConfig struct {
    Enabled    bool     `yaml:"enabled"`     // master switch
    Rate       float64  `yaml:"rate"`        // 0.0..1.0 fraction of this operator's
                                             // sessions that use 5G-SBA instead of RADIUS
    AuthMethod string   `yaml:"auth_method"` // "5G_AKA" (default) | "EAP_AKA_PRIME" (future)
    Slices     []SliceConfig `yaml:"slices,omitempty"` // optional S-NSSAI set to advertise
}

type SliceConfig struct {
    SST int    `yaml:"sst"`
    SD  string `yaml:"sd,omitempty"`
}
```

Validation rules (additive to existing `Validate`):

- Applied in a new `(c *Config) validateSBA()` called from `Validate` after
  `validateDiameter`.
- Apply defaults: port 8443, serving-network-name constant, timeout 5s,
  auth-method `"5G_AKA"`, amf-instance-id `"sim-amf-01"`.
- `Rate` clamped to `[0.0, 1.0]`; if out-of-range → error.
- If any operator has `SBA.Enabled: true`:
  - Each operator's `auth_method` MUST be `"5G_AKA"` (EAP_AKA_PRIME reserved,
    rejected for this story).
  - If `TLSSkipVerify: true` and env `ARGUS_SIM_ENV=prod` AND
    `SBADefaults.ProdGuard: true` → error. Matches STORY-082/083
    defence-in-depth philosophy: production env refuses skip-verify.
- `Slices` default = `[{SST:1, SD:"000001"}]` when empty AND operator opts in.

### Engine integration — per-session protocol picker

STORY-083 added a post-RADIUS Diameter bracket. STORY-084's model is
**different**: when the session rolls into the "5g" bucket, **RADIUS
doesn't run at all** — the session's authentication is the SBA call. When
the session rolls into the "radius" bucket, the existing STORY-082/083
flow runs unchanged (with Diameter conditionally bracketing it).

```
engine.runSession (rewritten top-level)
  │
  ├── picker.ProtocolFor(operator) → "radius" | "sba"
  │      (Bernoulli trial keyed on operator's sba.rate; seeded per-session)
  │
  ├── if "sba":
  │      ├── sba.OpenSession(sessionCtx, sc)         ◄── NEW
  │      │      POST /nausf-auth/v1/ue-authentications
  │      │      PUT  /nausf-auth/v1/ue-authentications/{ctx}/5g-aka-confirmation
  │      │      (optionally GET /nudm-ueau/v1/{supi}/security-information
  │      │       when include_optional_calls && Rand() < 0.2)
  │      ├── sba.RegisterAMF(sessionCtx, sc)         ◄── NEW
  │      │      PUT /nudm-uecm/v1/{supi}/registrations/amf-3gpp-access
  │      ├── — no accounting layer for 5G in this story (CHF over HTTP/2
  │      │     is out-of-scope; future) —
  │      ├── — no Acct-Interim loop for 5G; session duration enforced
  │      │     by a plain sleep so ActiveSessions gauge still reflects
  │      │     live session count —
  │      └── idle until deadline or ctx.Done()
  │
  └── if "radius":
         ... existing STORY-082 RADIUS flow unchanged ...
         ... existing STORY-083 Diameter bracket when operator opts in ...
```

**Engine struct additions:**

```go
type Engine struct {
    // ... existing fields ...
    sba map[string]*sba.Client // operator code → SBA client; nil = SBA disabled for that op
    sbaRate map[string]float64 // operator code → sba fraction (for picker)
}

func New(..., sbaClients map[string]*sba.Client, sbaRates map[string]float64, ...) *Engine
```

**New helper** `chooseProtocol(operatorCode string, rand *rand.Rand) string`
— returns `"sba"` with probability `sbaRate[operatorCode]` if `sbaClient
!= nil`, else `"radius"`. Seeded independently per session to avoid the
picker reusing the scenario picker's seed.

### Metrics — additions

Five counters/histograms matching STORY-083's 5-vector shape:

```go
// labels: operator, service (ausf|udm), endpoint (authenticate|confirm|register|security-info|auth-events)
SBARequestsTotal *prometheus.CounterVec

// labels: operator, service, endpoint, result (success|error_<status>|timeout|transport)
SBAResponsesTotal *prometheus.CounterVec

// labels: operator, service, endpoint
SBALatencySeconds *prometheus.HistogramVec  // buckets like RADIUS/Diameter

// labels: operator, reason (auth_failed|confirm_failed|register_failed|transport|timeout)
SBASessionAbortedTotal *prometheus.CounterVec

// labels: operator, service, cause (per-server ProblemDetails.Cause value, e.g. MANDATORY_IE_INCORRECT, AUTH_REJECTED)
SBAServiceErrorsTotal *prometheus.CounterVec  // fifth vector per brief requirement
```

Register in `metrics.MustRegister`. All names carry `simulator_sba_*`
prefix, matching STORY-083's `simulator_diameter_*` convention.

### Wire-format details — request / response shapes

Verified against `internal/aaa/sba/types.go` + handler source.

**1. POST `/nausf-auth/v1/ue-authentications`** (AUSF authenticate start)

Request (`AuthenticationRequest`):
```json
{
  "supiOrSuci": "imsi-286010123456789",
  "servingNetworkName": "5G:mnc001.mcc286.3gppnetwork.org",
  "requestedNssai": [{"sst": 1, "sd": "000001"}]
}
```
Headers: `Content-Type: application/json`.

Response 201 Created (`AuthenticationResponse`):
```json
{
  "authType": "5G_AKA",
  "5gAuthData": {
    "rand":      "<base64>",
    "autn":      "<base64>",
    "hxresStar": "<base64>"
  },
  "_links": { "5g-aka": {"href": "/nausf-auth/v1/ue-authentications/<uuid>/5g-aka-confirmation"} }
}
```
Header `Location: /nausf-auth/v1/ue-authentications/<uuid>`.

Simulator extracts `Links["5g-aka"].Href` → use directly as PUT path.

**2. PUT `<link-href>`** (5G-AKA confirmation)

Request (`ConfirmationRequest`):
```json
{ "resStar": "<base64-of-xresStar>" }
```

`xresStar` is derived locally by re-running `generate5GAV(supi,
servingNetworkName)` — see "Crypto" section above.

Response 200 OK (`ConfirmationResponse`):
```json
{ "authResult": "SUCCESS", "supi": "imsi-286010123456789", "kseaf": "<base64>" }
```

Simulator asserts `AuthResult == "SUCCESS"`; on mismatch, increment
`SBASessionAbortedTotal{reason="confirm_failed"}` and return.

**3. PUT `/nudm-uecm/v1/{supi}/registrations/amf-3gpp-access`** (AMF registration)

Request (`Amf3GppAccessRegistration`):
```json
{
  "amfInstanceId":          "sim-amf-01",
  "deregCallbackUri":       "http://sim-amf.invalid/dereg",
  "guami": {"plmnId": {"mcc": "286", "mnc": "01"}, "amfId": "abc123"},
  "ratType":                "NR",
  "initialRegistrationInd": true
}
```

Response 201 Created — echoes the body (handler returns the decoded input
as a confirmation). Simulator ignores the body; status-code check suffices.

**4. (optional) GET `/nudm-ueau/v1/{supiOrSuci}/security-information?servingNetworkName=...`**

Used only when `include_optional_calls: true` and a 20% in-session
Bernoulli roll triggers. Response is `SecurityInfoResponse`. Simulator
does NOT decode fields — this is pure traffic exercise.

**5. (optional) POST `/nudm-ueau/v1/{supiOrSuci}/auth-events`**

Request (`AuthEvent`):
```json
{
  "nfInstanceId":       "sim-amf-01",
  "success":            true,
  "timeStamp":          "2026-04-17T09:30:00Z",
  "authType":           "5G_AKA",
  "servingNetworkName": "5G:mnc001.mcc286.3gppnetwork.org"
}
```
Emitted only when `include_optional_calls && confirmation succeeded`.

### Error handling & metric classification

Per `simple classification → disjoint partitions` rule (STORY-083 F-A3
pattern):

| Scenario | Metric |
|---|---|
| HTTP 201/200 (expected) | `SBAResponsesTotal{result="success"}` |
| HTTP 4xx with `application/problem+json` body | `SBAResponsesTotal{result="error_4xx"}` + `SBAServiceErrorsTotal{cause=<ProblemDetails.Cause>}` |
| HTTP 5xx | `SBAResponsesTotal{result="error_5xx"}` |
| `net.Error` with `Timeout() == true` | `SBAResponsesTotal{result="timeout"}` |
| Any other transport error (conn reset, DNS, TLS) | `SBAResponsesTotal{result="transport"}` |

Session-abort reasons are emitted by the **engine** (single writer, per
STORY-083 F-A3 lesson):

```go
if err := sbaClient.OpenSession(ctx, sc); err != nil {
    reason := "auth_failed"
    switch {
    case errors.Is(err, sba.ErrConfirmFailed):    reason = "confirm_failed"
    case errors.Is(err, sba.ErrTimeout):           reason = "timeout"
    case errors.Is(err, sba.ErrTransport):         reason = "transport"
    }
    metrics.SBASessionAbortedTotal.WithLabelValues(op.Code, reason).Inc()
    return
}
```

Client returns wrapped sentinel errors; classification happens exactly
once in the engine. Enum buckets are disjoint.

### Safety Envelope (additive to STORY-082/083)

- **Per-operator opt-in** (`sba.enabled: false` default) — zero risk to
  RADIUS-only or Diameter-enabled consumers.
- **`ARGUS_SIM_ENV=prod` refuses `tls_skip_verify: true`** when
  `prod_guard: true` (default) — defence-in-depth against accidental prod
  activation of a test-only TLS shortcut.
- **Per-operator HTTP client** — one `*http.Client` per operator with
  connection pool ≤ 10 idle conns. No per-session client churn.
- **Bounded request timeout** — `5s` default; every HTTP call wraps
  `context.WithTimeout(ctx, cfg.RequestTimeout)`.
- **Strict shutdown order** — same pattern STORY-083 uses: `engine.Run`
  returns → SBA clients' `CloseIdleConnections()` is called → metrics HTTP
  server shuts down.
- **`SIMULATOR_ENABLED` env guard** preserved.
- **No third-party dependency added** — `golang.org/x/net/http2` is
  already a transitive dependency; promoting it to a direct import does
  not add to the module graph.

## Prerequisites

- [x] STORY-082 complete — simulator binary + config + engine + metrics.
- [x] STORY-083 complete — per-operator opt-in pattern, metric vocab,
  engine wiring approach, `golang.org/x/net` already indirect.
- [x] STORY-020 complete — `internal/aaa/sba` exposes all request/response
  types and handlers (verified `server.go:52-127`, `types.go` exports).

## Tasks

### Wave 1 — Foundation (config + HTTP/2 scaffold + metrics)

> All three tasks are independent; can run in parallel. Wave 1 must
> complete before Wave 2.

#### Task 1: Extend simulator config schema for SBA
- **Files:** Modify `internal/simulator/config/config.go`, Modify `internal/simulator/config/config_test.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/simulator/config/config.go` — follow STORY-083's `validateDiameter()` pattern for `validateSBA()`. New struct fields live next to `DiameterDefaults` and `OperatorDiameterConfig`.
- **Context refs:** "Architecture Context > Config schema — additive", "Architecture Context > Safety Envelope"
- **What:**
  - Add `SBADefaults` struct + `SBA` field on `Config`.
  - Add `OperatorSBAConfig` + `SBA *OperatorSBAConfig` on `OperatorConfig`.
  - Add `SliceConfig` helper struct.
  - Defaults in `validateSBA()`: port 8443, serving-network-name default,
    timeout 5s, amf-instance-id `"sim-amf-01"`, dereg-callback default,
    prod-guard true, auth-method `"5G_AKA"`, slices `[{SST:1, SD:"000001"}]`.
  - Validation: `Rate` in `[0,1]`; non-5G_AKA auth methods rejected in this
    story; `prod_guard && ARGUS_SIM_ENV=prod && tls_skip_verify` → error.
  - Tests (table-driven, matching STORY-083 style): defaults applied,
    rate=1.2 rejected, unknown auth-method rejected, prod-guard triggers
    when env set, SBA-disabled config still validates, RADIUS-only and
    Diameter-only configs still validate.
- **Verify:** `cd /Users/btopcu/workspace/argus && go test ./internal/simulator/config/...`

#### Task 2: SBA HTTP client scaffold + protocol picker
- **Files:** Create `internal/simulator/sba/client.go`, Create `internal/simulator/sba/doc.go`, Create `internal/simulator/sba/picker.go`, Create `internal/simulator/sba/picker_test.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/simulator/diameter/client.go` (STORY-083) for overall struct layout + `Start(ctx) <-chan struct{}` and `Stop(ctx)` signatures; read `internal/simulator/radius/client.go` for per-operator per-endpoint helper pattern.
- **Context refs:** "Architecture Context > Components Involved", "Architecture Context > Transport — HTTP/1.1 default, HTTP/2 conditional on TLS", "Architecture Context > Safety Envelope"
- **What:**
  - `Client` struct: holds `baseURL string`, `httpClient *http.Client`,
    `operatorCode string`, `servingNetworkName string`, `amfInstanceID
    string`, `deregCallbackURI string`, `slices []argussba.SNSSAI`,
    `logger zerolog.Logger`, `rnd *rand.Rand`.
  - Constructor `New(op config.OperatorConfig, defaults config.SBADefaults,
    logger zerolog.Logger) *Client`:
    - Build `*http.Transport` with 10-conn idle pool, `ForceAttemptHTTP2:
      true` if `TLSEnabled`.
    - If `TLSEnabled`: set `TLSClientConfig` with `InsecureSkipVerify` +
      `NextProtos = ["h2", "http/1.1"]`. Call `http2.ConfigureTransport(t)`.
    - Wrap in `&http.Client{Transport: t, Timeout: defaults.RequestTimeout}`.
    - Compute `baseURL` from `host:port` + scheme (`http` or `https`).
  - No `Start(ctx)` equivalent needed — HTTP is on-demand; just a
    `Ping(ctx) error` that GETs `/health` to warm up the first connection
    and return a classification-ready error (surfaces DNS/TLS issues at
    startup).
  - `Stop(ctx) error` — calls `httpClient.Transport.(*http.Transport)
    .CloseIdleConnections()`.
  - Sentinel errors: `ErrConfirmFailed`, `ErrTimeout`, `ErrTransport`,
    `ErrServerError`, `ErrAuthFailed`.
  - `picker.go`: `ProtocolSelector` struct holds per-operator rate map +
    per-session RNG. `SelectProtocol(opCode string) string` returns
    `"sba"` or `"radius"` per Bernoulli trial. `picker_test.go`: 10 000
    trials at rate=0.2 → observed fraction in `[0.17, 0.23]` at 99%
    confidence.
- **Verify:** `go build ./internal/simulator/sba/...`; `go test ./internal/simulator/sba/... -run TestPicker`.

#### Task 3: SBA metrics
- **Files:** Modify `internal/simulator/metrics/metrics.go`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `internal/simulator/metrics/metrics.go` — add new vectors next to existing Diameter vectors; register in `MustRegister`.
- **Context refs:** "Architecture Context > Metrics — additions"
- **What:** Add `SBARequestsTotal`, `SBAResponsesTotal`, `SBALatencySeconds`, `SBASessionAbortedTotal`, `SBAServiceErrorsTotal` with the exact label sets. Buckets match RADIUS/Diameter (`ExponentialBuckets(0.001, 2, 12)`). Register all 5 in `MustRegister`.
- **Verify:** `go build ./...`; start simulator locally with one SBA-enabled operator → confirm `/metrics` exposes new families.

### Wave 2 — Protocol handlers + client façade

> Starts after Wave 1 tasks 1 + 2 merged.

#### Task 4: AUSF handler — authenticate + confirm
- **Files:** Create `internal/simulator/sba/ausf.go`, Create `internal/simulator/sba/ausf_test.go`, Create `internal/simulator/sba/crypto.go`, Create `internal/simulator/sba/crypto_test.go`
- **Depends on:** Task 2
- **Complexity:** high
- **Pattern ref:** Read `internal/aaa/sba/ausf.go` (`HandleAuthentication` at line 47 + `generate5GAV` at line 340) for request/response shape; read `internal/aaa/sba/server_test.go:32-70` for the exact body the server accepts.
- **Context refs:** "Architecture Context > Reuse strategy — CRITICAL", "Architecture Context > Crypto — how the simulator makes resStar match server's HxresStar", "Architecture Context > Wire-format details" (POST authenticate + PUT confirm sections), "Architecture Context > Error handling & metric classification"
- **What:**
  - `crypto.go`: copy `generate5GAV`, `derivePseudoRandom`, `sha256Sum`
    from `internal/aaa/sba/ausf.go:340-375`. Top-of-file comment cites
    source path + line numbers + states "duplicated because server
    helpers are unexported; golden canary test catches drift".
  - `crypto_test.go`: golden test asserts local `generate5GAV("imsi-286010123456789",
    "5G:mnc001.mcc286.3gppnetwork.org")` produces byte-for-byte the same
    xresStar the server would. Achieved by running the simulator's
    function and comparing to a known-fixed expected slice (hex-encoded
    in the test).
  - `ausf.go`:
    - `Authenticate(ctx context.Context, sc *radius.SessionContext) (authCtxHref string, kseaf []byte, err error)`:
      - Marshal `argussba.AuthenticationRequest{SUPIOrSUCI:
        "imsi-"+sc.SIM.IMSI, ServingNetworkName: c.servingNetworkName,
        RequestedNSSAI: c.slices}` → POST to `baseURL+"/nausf-auth/v1/ue-authentications"`.
      - On 201: decode `argussba.AuthenticationResponse`; extract
        `Links["5g-aka"].Href`; emit `SBARequestsTotal{endpoint="authenticate"}`
        + `SBAResponsesTotal{result="success"}` + latency.
      - On 4xx with problem+json: emit `SBAServiceErrorsTotal{cause}` and
        return `fmt.Errorf("%w: %s", ErrAuthFailed, cause)`.
    - `Confirm(ctx, href string, supiOrSuci, servingNetwork string) (kseaf []byte, err error)`:
      - Compute `xresStar := generate5GAV(supi, servingNetwork)` → third
        return value.
      - Build `argussba.ConfirmationRequest{ResStar:
        base64.StdEncoding.EncodeToString(xresStar)}`.
      - PUT to `baseURL+href`.
      - Decode `argussba.ConfirmationResponse`; assert
        `AuthResult == "SUCCESS"`; decode Kseaf from base64.
  - `ausf_test.go` (three cases):
    - Happy path with `httptest.NewServer` reusing the real
      `aaasba.NewAUSFHandler(nil, nil, zerolog.Nop())` on a test mux.
      Assert POST body decodes correctly, `Authenticate` returns the
      link href, then simulator can `Confirm` → SUCCESS + non-empty
      kseaf.
    - Failure path: server returns 401 AUTH_REJECTED → `Confirm` returns
      error wrapping `ErrConfirmFailed` AND the cause surfaces in
      `SBAServiceErrorsTotal{cause="AUTH_REJECTED"}`.
    - Timeout path: server hangs; client's context deadline fires →
      error is `errors.Is(err, ErrTimeout)`.
- **Verify:** `go test ./internal/simulator/sba/... -run "TestAUSF|TestCrypto"`.

#### Task 5: UDM handler — AMF registration + optional probes + client façade
- **Files:** Create `internal/simulator/sba/udm.go`, Create `internal/simulator/sba/udm_test.go`, Modify `internal/simulator/sba/client.go` (expose `OpenSession` / `RegisterAMF` façade methods)
- **Depends on:** Task 2, Task 4
- **Complexity:** high
- **Pattern ref:** Read `internal/aaa/sba/udm.go` (`HandleRegistration` line 131 + `HandleSecurityInfo` line 31 + `HandleAuthEvents` line 77) for body shapes; read `internal/simulator/diameter/client.go` `OpenSession`/`CloseSession` shape for the façade convention.
- **Context refs:** "Architecture Context > Reuse strategy — CRITICAL", "Architecture Context > Wire-format details" (sections 3, 4, 5), "Architecture Context > Engine integration — per-session protocol picker"
- **What:**
  - `udm.go`:
    - `RegisterAMF(ctx, supi string) error`: PUT to
      `baseURL+"/nudm-uecm/v1/"+supi+"/registrations/amf-3gpp-access"`
      with body `argussba.Amf3GppAccessRegistration{
        AmfInstanceID: c.amfInstanceID,
        DeregCallbackURI: c.deregCallbackURI,
        GUAMI: argussba.GUAMI{PlmnID: argussba.PlmnID{MCC:"286", MNC:"01"}, AmfID:"abc123"},
        RATType: "NR",
        InitialRegInd: true,
      }`. Expect 201; emit metrics; classify errors.
    - `GetSecurityInformation(ctx, supiOrSuci, servingNetwork string) error`:
      GET `baseURL+"/nudm-ueau/v1/"+url.PathEscape(supiOrSuci)+"/security-information?servingNetworkName="+url.QueryEscape(servingNetwork)`.
      Only called when `IncludeOptionalCalls` + in-session coin flip.
    - `RecordAuthEvent(ctx, supiOrSuci string, success bool) error`: POST
      `argussba.AuthEvent`. Same gating.
  - `client.go` additions (façade methods):
    - `OpenSession(ctx, sc) error`: call `Authenticate` → `Confirm`
      (optionally `GetSecurityInformation` first when flag + coin) →
      return wrapped error with appropriate sentinel. Store `Kseaf`
      ephemerally on the SessionContext for future CHF use (not in
      scope, but stage the hook).
    - `RegisterAMF(ctx, sc) error`: call `udm.RegisterAMF` with
      `"imsi-"+sc.SIM.IMSI`.
    - Optional: `RecordAuthEvent(ctx, sc, success)` — caller-driven,
      engine calls it at session-end when enabled.
  - `udm_test.go`:
    - httptest server reusing real `aaasba.NewUDMHandler(nil, nil, zerolog.Nop())`.
    - AMF registration: PUT body decodes as expected struct, status 201,
      `SBARequestsTotal{endpoint="register"}` incremented.
    - Security-information GET: path + query string correct;
      `SBARequestsTotal{endpoint="security-info"}` incremented.
    - Registration rejected (simulate 400 via mux): error surfaces the
      `MANDATORY_IE_INCORRECT` cause.
- **Verify:** `go test ./internal/simulator/sba/... -run "TestUDM|TestClient"`.

### Wave 3 — Engine wiring + example config + integration test + doc

> Starts after Wave 2 (Tasks 4 + 5) merged.

#### Task 6: Engine integration — per-session RADIUS vs SBA fork
- **Files:** Modify `internal/simulator/engine/engine.go`, Modify `cmd/simulator/main.go`
- **Depends on:** Task 1, Task 5
- **Complexity:** high
- **Pattern ref:** Read current `engine.go` (post-STORY-083) `runSession` — keep the existing RADIUS+Diameter path intact; ADD a top-level fork that dispatches to a new `runSBASession` when the picker chose "sba". Read `cmd/simulator/main.go:86-106` for the Diameter-clients construction pattern; mirror for SBA clients.
- **Context refs:** "Architecture Context > Engine integration — per-session protocol picker", "Architecture Context > Components Involved", "Architecture Context > Error handling & metric classification"
- **What:**
  - `engine.go`:
    - Extend `Engine`: add `sba map[string]*sba.Client`, `picker
      *sba.ProtocolSelector`.
    - Extend `New`: accept `sbaClients map[string]*sba.Client`, `sbaRates
      map[string]float64`; build `ProtocolSelector` from rates.
    - In `runSession`, BEFORE the current auth step:
      ```go
      if sbaC := e.sba[sim.OperatorCode]; sbaC != nil {
          if e.sbaPicker.SelectProtocol(sim.OperatorCode) == "sba" {
              e.runSBASession(ctx, sim, op, sample, sbaC, log)
              return
          }
      }
      ```
    - New method `runSBASession`:
      - `sc := simradius.NewSessionContext(sim, op.NASIP, op.NASIdentifier)`
        (reuse for IMSI/acct-session-id generation, even though RADIUS
        isn't called).
      - `sbaC.OpenSession(ctx, sc)` → Authenticate + Confirm. Errors →
        `SBASessionAbortedTotal{reason}` per classification table, return.
      - `sbaC.RegisterAMF(ctx, sc)`. Errors → abort with reason
        `"register_failed"`.
      - `ActiveSessions.Inc()` + defer `.Dec()` (same as RADIUS path).
      - `time.Sleep(sample.SessionDuration)` guarded by `<-ctx.Done()`.
      - No Acct-Interim equivalent in this story (CHF is future).
      - Optionally `sbaC.RecordAuthEvent(ctx, sc, true)` at end when
        `IncludeOptionalCalls`.
    - Keep the existing RADIUS path + Diameter bracket untouched when
      picker returns `"radius"`.
  - `cmd/simulator/main.go`:
    - For each operator with `SBA != nil && SBA.Enabled`, build
      `*sba.Client` and `Ping(ctx)` (non-fatal — log warning on failure;
      simulator continues, first real call will surface permanent
      issues). Collect into `sbaClients` map and `sbaRates` map.
    - Pass both maps to `engine.New(cfg, picker, client, dmClients,
      sbaClients, sbaRates, logger)`.
    - On shutdown, iterate SBA clients: `_ = c.Stop(shutdownCtx)`.
  - **Non-regression:** RADIUS-only and Diameter-enabled configs continue
    to produce byte-identical RADIUS/Diameter traffic (no code path
    changes in those branches — the fork is additive).
- **Verify:**
  - `go build ./cmd/simulator`
  - `go test ./internal/simulator/engine/...` (must not regress; if
    engine package lacks direct tests, at minimum confirm build passes
    and `go vet ./...` is clean).
  - Manual: start sim with one SBA-enabled op at rate=1.0 → observe all
    sessions hit SBA endpoints in Argus logs; flip rate=0.0 → all hit
    RADIUS.

#### Task 7: Example config + compose smoke
- **Files:** Modify `deploy/simulator/config.example.yaml`
- **Depends on:** Task 1
- **Complexity:** low
- **Pattern ref:** Existing `deploy/simulator/config.example.yaml` — keep
  the comment style used for the `diameter:` block (each defaulted field
  shown with its default + brief one-line explanation).
- **Context refs:** "Architecture Context > Config schema — additive"
- **What:**
  - Append a top-level `sba:` block with all 9 default fields explicit
    (host, port, tls_enabled=false, tls_skip_verify=false, serving-
    network-name, request_timeout, amf_instance_id, dereg_callback_uri,
    include_optional_calls=false, prod_guard=true).
  - Add `sba:` opt-in to the `turkcell` operator block alongside the
    existing `diameter:` block — demonstrates multiple protocols
    simultaneously. Set `rate: 0.2` (20% of turkcell sessions use 5G).
  - Keep `vodafone` + `turk_telekom` RADIUS-only.
  - Document in comments: `rate: 1.0` = SBA-only, `rate: 0.0` = SBA
    effectively disabled (client still built but never called),
    `enabled: false` = SBA not built at all.
- **Verify:**
  - YAML parses: `yq eval . deploy/simulator/config.example.yaml > /dev/null`.
  - `make sim-build` succeeds.
  - `cd /Users/btopcu/workspace/argus && ARGUS_SIM_CONFIG=deploy/simulator/config.example.yaml go run ./cmd/simulator` (under `SIMULATOR_ENABLED=1`) — startup reaches "SIMs discovered" line.

#### Task 8: End-to-end integration test + architecture doc
- **Files:** Create `internal/simulator/sba/integration_test.go` (build-tag `integration`); Modify `docs/architecture/simulator.md`
- **Depends on:** Task 5, Task 6
- **Complexity:** medium
- **Pattern ref:** Read `internal/simulator/diameter/integration_test.go` (STORY-083) for the build-tag + in-process server pattern; read `docs/architecture/simulator.md` STORY-083's "Diameter Client" section for doc structure.
- **Context refs:** "Architecture Context > Reuse strategy — CRITICAL", "Architecture Context > Wire-format details", "Architecture Context > Engine integration — per-session protocol picker"
- **What:**
  - `integration_test.go`:
    - Spin up `aaasba.NewServer` on an ephemeral port (port=0 binding
      pattern), wire with in-memory `SessionMgr` stub. Server runs
      cleartext HTTP/1.1 (no TLS cert → matches dev compose).
    - Build simulator `sba.Client` pointed at that port with
      `TLSEnabled: false`.
    - Run:
      - `OpenSession(ctx, sc)` → verify 201 then 200 received end-to-end.
      - `RegisterAMF(ctx, sc)` → verify 201.
      - Assert `SBARequestsTotal{endpoint="authenticate"} == 1`,
        `{endpoint="confirm"} == 1`, `{endpoint="register"} == 1`.
      - Assert `SBAResponsesTotal{result="success"}` equals 3.
    - Negative case: set `serving_network_name` to empty string →
      Authenticate fails with `MANDATORY_IE_INCORRECT` cause → assert
      `SBAServiceErrorsTotal{cause="MANDATORY_IE_INCORRECT"} > 0`.
    - Run with `go test -tags=integration ./internal/simulator/sba/...`
      — skipped by default CI.
  - `docs/architecture/simulator.md`:
    - Add "5G SBA Client (STORY-084)" section below "Diameter Client".
    - Sub-sections: opt-in semantics (enabled + rate), per-session fork
      (sba vs radius — alternative, not additive), supported endpoints
      list with one-line purpose each, metrics table, failure modes (auth
      failed → session abort, server down → SBA skipped for this
      session, but session does NOT fall back to RADIUS — it's dropped;
      per safety envelope), TLS knobs, prod-guard rationale,
      out-of-scope (CHF/N32/full Milenage).
    - One-paragraph "Why reuse `internal/aaa/sba` types?" callout
      mirroring STORY-083's equivalent for Diameter.
- **Verify:** `go test -tags=integration -run TestSimulator_AgainstArgusSBA ./internal/simulator/sba/...`; visual inspection of doc.

## Acceptance Criteria

(Eight ACs; re-verified to map to the actual code deliverables listed above.
AC-5 preserved but re-scoped now that the old plan's CHF/GET am-data paths
are retired; AC-6 TLS/prod-guard logic unchanged.)

- **AC-1** When `operators[].sba.enabled: true` and `sba.rate: 0.2`,
  approximately 20% of that operator's sessions use 5G-SBA instead of
  RADIUS. Verified by `simulator_sba_requests_total{operator=X,service="ausf",
  endpoint="authenticate"}` ≈ 0.2 × total scenario-starts for operator X
  over a 5-min window (±5% at 1000+ sessions).
- **AC-2** Every 5G-selected session completes the 3-call minimum flow
  (POST authenticate → PUT confirm → PUT register) end-to-end against
  argus-app's `:8443` SBA proxy. Verified by `simulator_sba_responses_total
  {result="success",endpoint="authenticate"}` == `{endpoint="confirm"}` ==
  `{endpoint="register"}` over a 2-min run with rate=1.0 and no transport
  errors.
- **AC-3** Argus's SBA proxy logs the three expected request paths for
  each SBA session (`/nausf-auth/v1/ue-authentications`,
  `.../5g-aka-confirmation`, `/nudm-uecm/v1/.../registrations/amf-3gpp-
  access`). Verified by manual docker log inspection, runbook in
  `docs/architecture/simulator.md`. Captured during Wave 3 smoke.
- **AC-4** `/metrics` endpoint exposes all five `simulator_sba_*` vectors
  (`_requests_total`, `_responses_total`, `_latency_seconds`,
  `_session_aborted_total`, `_service_errors_total`) during a 2-min run
  with at least one SBA-enabled operator. Latency histogram p95 < 500ms
  in-cluster (dev).
- **AC-5** `sba.enabled: false` (or omitted) disables all SBA calls for
  that operator — `simulator_sba_requests_total{operator=X}` stays at 0
  and RADIUS-only lifecycle is byte-identical to STORY-082. STORY-082 and
  STORY-083 ACs remain unaffected.
- **AC-6** TLS `skip_verify: true` only activates when `prod_guard: true`
  AND `ARGUS_SIM_ENV!=prod`, OR when `prod_guard: false`. Simulator
  refuses to start with `prod_guard: true` + `ARGUS_SIM_ENV=prod` +
  `tls_skip_verify: true` (config validation error). Verified by config
  unit test + Dockerfile smoke with explicit env set.
- **AC-7** After killing argus-app's SBA process for ≥ 30s and
  restarting, new 5G sessions resume successfully within 1s of argus-app
  readiness (HTTP is connectionless per-request; no warmup needed). No
  "stuck state" akin to Diameter's peer-not-open. Verified by manual
  compose-restart smoke.
- **AC-8** No regression: `go test ./...` passes (new baseline = 2900 +
  simulator/sba tests); RADIUS-only and Diameter-enabled configs produce
  identical output to pre-STORY-084 binary.

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| AC-1 | Task 2 (picker), Task 6 (engine fork) | Task 2 `TestPicker`, Task 6 manual smoke, Task 8 integration |
| AC-2 | Task 4 (AUSF), Task 5 (UDM register), Task 6 (engine bracket) | Task 4 `TestAUSF_Happy`, Task 5 `TestUDM_Register`, Task 8 integration |
| AC-3 | Task 6 (engine wires full flow) | Task 8 integration + manual runbook |
| AC-4 | Task 3 (metrics) | Task 4+5 unit tests observe counters, Task 8 integration scrape |
| AC-5 | Task 1 (default-off), Task 6 (nil check) | Task 1 config tests, Task 6 build-passes-without-regression |
| AC-6 | Task 1 (prod_guard logic) | Task 1 config tests (with env injection) |
| AC-7 | Task 2 (per-request HTTP, no persistent state) | Task 8 integration (kill-restart sub-test) |
| AC-8 | Task 1 (additive), Task 6 (nil-guarded fork) | `go test ./...` after Task 6 |

## Story-Specific Compliance Rules

- **No new third-party dependencies.** `golang.org/x/net/http2` is already
  transitively in `go.mod`; promoting to direct is `go mod tidy` territory.
  No other new modules.
- **Reuse `internal/aaa/sba` types** — must import, not redefine.
- **Crypto duplication allowed but quarantined** — `crypto.go` is the
  ONLY file that duplicates server behaviour; it carries a "source:
  `internal/aaa/sba/ausf.go` lines X-Y" comment and a canary test.
- **API:** No HTTP endpoints introduced on the Argus side. Standard
  envelope N/A.
- **DB:** No migrations. No DB access from the simulator beyond the
  existing read-only discovery query.
- **Boot guard:** `SIMULATOR_ENABLED` env still mandatory.
- **Per-operator opt-in default = false**; explicit per-operator enable
  required.
- **STORY-086 (boot schema guard):** unrelated; no schema changes.
- **ADR-002 (JWT auth):** N/A — SBA uses its own NF-to-NF auth model
  (out of scope for simulator; bearer token not required by the proxy's
  current handler implementation).

## Bug Pattern Warnings

Drawn from STORY-083 gate's "New pattern worth adding" section plus HTTP
client pitfalls specific to this story.

- **Single-writer metric classification (STORY-083 F-A3):** Engine is the
  one writer for `SBASessionAbortedTotal`; client returns wrapped sentinel
  errors, engine classifies. Tests must assert the TOTAL across reason
  buckets for one logical abort equals exactly 1.
- **`CloseIdleConnections` at shutdown:** Without this, the binary can
  hang on Ctrl-C while the TLS transport refuses to exit. STORY-083's
  Diameter shutdown drains; SBA's shutdown must do the same.
- **Path escaping for SUPI in UDM URL:** IMSI-based SUPIs are safe ASCII,
  but if the SUCI branch is ever exercised it may carry dashes or
  percent-reserved chars — use `url.PathEscape` on the SUPI segment to
  avoid server 404s on edge cases.
- **`http.Response.Body` always closed:** every code path must defer
  `resp.Body.Close()` OR read-and-close via `io.Copy(io.Discard, body)`
  before returning — leaks connections from the pool otherwise and
  defeats keep-alive.
- **`context.WithTimeout` vs `Client.Timeout`:** Use context, not
  `Client.Timeout`, for per-request deadlines — `Client.Timeout` is a
  blunt instrument (covers dial+TLS+write+read+decode), and wrapping in
  `context.WithTimeout` lets callers correlate cancellations with their
  session lifecycle.
- **Crypto drift canary:** if `internal/aaa/sba/ausf.go`'s
  `generate5GAV` ever changes, the simulator's duplicated copy stops
  matching. The canary test in `crypto_test.go` is the early warning —
  do NOT suppress it by regenerating the expected slice; fix the drift
  by re-copying the server's function and updating the "source: line
  X-Y" comment.

## Tech Debt (from ROUTEMAP)

No tech debt items target STORY-084 specifically as of 2026-04-17.

## Mock Retirement

Not applicable — simulator generates mock traffic; no UI mocks involved.

## Risks & Mitigations

- **Server's `generate5GAV` changes unnoticed:** simulator's `Confirm`
  stops passing because local `xresStar` no longer matches server's.
  Mitigation: `crypto_test.go` canary (see Bug Pattern); expected bytes
  hardcoded; if server crypto changes, simulator CI breaks before any
  metric drift is observed.
- **HTTP/2 auto-negotiation footgun:** if `http2.ConfigureTransport(t)`
  is called before `TLSClientConfig` is set, h2 negotiation silently
  falls back to HTTP/1.1 (NextProtos never set). Mitigation: order is
  documented in Task 2 `What`; `client_test.go` verifies negotiated
  protocol via `resp.Proto` for a TLS-enabled client variant.
- **Server's AMF registration rejects non-conformant GUAMI:** The server
  accepts any struct that decodes cleanly, but future tightening could
  require valid MCC/MNC. Mitigation: populate plausible MCC=286 (Turkey)
  / MNC=01 (Turkcell) defaults. Track as future story.
- **Prod-guard bypass:** a `tls_skip_verify: true` + `prod_guard: false`
  config flipped to prod by mistake. Mitigation: config tests cover the
  prod+skip-verify+prod_guard=true failure path; runbook in
  `simulator.md` notes "prod_guard: false is for exceptional hand-crafted
  scenarios only; default leave it `true`".
- **Fork vs additive confusion in engine:** Developers might assume SBA
  is bracketed *around* RADIUS (as Diameter is). Mitigation: plan
  explicitly calls out "alternative, not additive"; code comment in
  `runSession` at the fork reads "5G-SBA REPLACES the RADIUS+Diameter
  flow — UE attaches over SBA directly in a 5G SA deployment".
- **Metric cardinality from `cause` label:** `ProblemDetails.Cause` is
  server-controlled free-form string. Mitigation: `SBAServiceErrorsTotal`
  documents the expected enum (MANDATORY_IE_INCORRECT, AUTH_REJECTED,
  SNSSAI_NOT_ALLOWED, AUTH_CONTEXT_NOT_FOUND, METHOD_NOT_ALLOWED,
  RESOURCE_NOT_FOUND). If the server introduces new causes, label
  cardinality grows but remains bounded by the server's own enumeration
  (not attacker-controlled).

## Dependencies

- **STORY-082** complete (simulator base binary + config + engine).
- **STORY-083** complete (per-operator opt-in pattern, metric vocab,
  engine integration shape, `golang.org/x/net` transitive).
- **STORY-020** complete — `internal/aaa/sba` server ready with AUSF +
  UDM handlers (verified on disk 2026-04-17).
- **No new `go.mod` dependency required** — `golang.org/x/net/http2`
  promoted from indirect to direct is NOT a new module.

## Out of Scope

- CHF (Converged Charging Function) — future story when Argus gains CHF
  HTTP/2 proxy support.
- N32 (inter-PLMN) interfaces.
- Full Milenage implementation — stub SHA-256-based `generate5GAV`
  continues to suffice because the simulator and server share the same
  pseudo-RNG algorithm.
- NRF service discovery — simulator hardcodes AUSF/UDM URLs; a real 5G
  stack queries NRF. STORY-020 added NRF registration on the server side
  but the simulator does not register or discover.
- EAP-AKA' (prime) authentication — handler exists server-side
  (`eap_proxy.go`) but config rejects non-`5G_AKA` method for this story.
- Per-operator SBA host override — single `sba.host` global default for
  now; future if operators are routed to distinct AUSF/UDM endpoints.
- SUCI concealment — simulator sends SUPI directly (`imsi-...` prefix);
  SUCI is a privacy envelope and the server's resolver transparently
  handles `suci-...` prefixes already but simulator doesn't need it.

---

## Quality Gate (plan self-validation)

Run before dispatching Wave 1. Plan FAILS if any check is FALSE.

### Substance
- [x] Story Effort = L → Plan ≥ 100 lines, ≥ 5 tasks. Actual: 8 tasks
  across 3 waves; plan ~540 lines.
- [x] At least 1 task marked `Complexity: high` (Tasks 4, 5, 6 all high).

### Required Sections
- [x] `## Goal`
- [x] `## Architecture Context`
- [x] `## Tasks` (numbered Task blocks, wave-grouped)
- [x] `## Acceptance Criteria` and `## Acceptance Criteria Mapping`
- [x] `## Risks & Mitigations`
- [x] `## Dependencies`
- [x] `## Out of Scope`
- [x] `## Quality Gate` (this section)

### Embedded Specs (self-contained)
- [x] Wire-format details for all five endpoints inline (method, path,
  request JSON, response JSON, headers).
- [x] Config schema embedded with Go struct snippets.
- [x] Source-of-truth files cross-referenced with line numbers
  (`server.go:75-107`, `ausf.go:340-349`, `types.go:39-43`).
- [x] Reuse strategy explicit: which `internal/aaa/sba` symbols are
  imported (table in "Reuse strategy" section).
- [x] Crypto duplication rationale documented + canary test specified.

### Code-State Validation
- [x] Disk verified 2026-04-17: `cmd/simulator/main.go`,
  `internal/simulator/{config,discovery,engine,metrics,radius,scenario,diameter}/`
  all present. New package under `internal/simulator/sba/` is the only
  new top-level dir.
- [x] Server-side SBA package verified: `internal/aaa/sba/{server.go,
  ausf.go,udm.go,types.go,eap_proxy.go,nrf.go}` all present. All
  required types (`AuthenticationRequest`, `AuthenticationResponse`,
  `AKA5GAuthData`, `AuthLink`, `ConfirmationRequest`,
  `ConfirmationResponse`, `Amf3GppAccessRegistration`, `GUAMI`,
  `PlmnID`, `ProblemDetails`, `SNSSAI`, `AuthType*`) are exported
  (`types.go`).
- [x] `go.mod` checked: `golang.org/x/net v0.52.0 // indirect` present
  at line 109. Plan does not add a new top-level module.
- [x] STORY-082 config + engine + metrics shape preserved (no rename,
  no removal).
- [x] STORY-083 package layout used as template; no overlap — new dir
  `internal/simulator/sba/` does not collide with
  `internal/simulator/diameter/`.
- [x] Server's actual endpoint paths verified (`server.go:75-107`) —
  four retired-from-old-plan route corrections called out in the plan
  header drift notes.
- [x] Server's actual response values verified
  (`AuthResult="SUCCESS"`, base64 not hex, PUT not POST for
  confirmation). Plan header drift notes call each one out.

### Task Decomposition
- [x] All 8 tasks touch ≤3 files except Task 8 which touches two
  (integration_test.go + simulator.md — same functional unit, test +
  doc).
- [x] Each task has `Depends on`, `Complexity`, `Pattern ref`,
  `Context refs`, `What`, `Verify` fields.
- [x] DB-first ordering N/A (no DB changes). Wave 1 (config + scaffold
  + metrics) before Wave 2 (handlers + client façade) before Wave 3
  (engine wiring + config + test + doc).
- [x] Context refs all reference sections that exist in this plan.
- [x] Wave structure explicit: Wave 1 tasks 1–3, Wave 2 tasks 4–5,
  Wave 3 tasks 6–8.
- [x] Wave 1 tasks (1,2,3) independent → parallel dispatch possible.
- [x] Wave 2 tasks (4 → 5) serial (Task 5 extends client.go written in
  Task 2 and imports crypto from Task 4).
- [x] Wave 3 tasks (6 → 7,8) Task 6 is the integration pivot; Tasks 7
  and 8 can run in parallel after 6.

### Test Coverage
- [x] Each AC mapped to a task that implements it AND a task that
  verifies it (table above).
- [x] Unit tests planned in same task as code (Tasks 1, 2, 4, 5).
- [x] Integration test gated behind `-tags=integration` (Task 8).
- [x] Crypto canary explicitly specified (Task 4 `crypto_test.go`).

### Drift Notes (relative to 2026-04-14 plan)
- [x] Old plan's POST for confirmation → **CHANGED** to PUT (with cite).
- [x] Old plan's `AUTHENTICATION_SUCCESSFUL` → **CHANGED** to `SUCCESS`.
- [x] Old plan's hex-encoded RAND/AUTN → **CHANGED** to base64.
- [x] Old plan's `GET /nudm-sdm/v1/{supi}/am-data` → **RETIRED**; not a
  real route. Replaced with PUT AMF registration + optional GET
  security-information.
- [x] Old plan's `fraction_of_sessions` → **RENAMED** to `rate` for
  consistency with STORY-083's knob style.
- [x] Old plan's Milenage dependency risk → **RETIRED**; simulator
  reuses server's deterministic pseudo-RNG with a canary test.
- [x] Old plan's `http.Client` with ALPN `h2` baseline → **CLARIFIED**
  as conditional on server TLS being on (dev default is HTTP/1.1
  cleartext).
- [x] 2026-04-14 plan had 8 ACs; this plan retains 8 ACs, preserving
  numbering + intent, re-scoping AC-5 and AC-7 now that no CHF/SDM
  surface is in play.

### Reuse Strategy Validation
- [x] Table lists every `internal/aaa/sba` symbol the simulator imports.
- [x] Crypto helpers duplicated (not imported) — rationale documented,
  canary test specified.
- [x] No third-party 3GPP lib added; no new go.mod direct dependency.

### Result

**PLAN GATE: PASS** — all substance, required-sections, embedded-spec,
code-state, task-decomposition, test-coverage, drift-note, and reuse-
strategy checks satisfied. Plan is dispatch-ready for Wave 1.

# Protocol Implementation Reference — Argus

> Argus implements multiple AAA protocols as a Go modular monolith.
> Implementation packages: `internal/aaa/radius/`, `internal/aaa/diameter/`, `internal/aaa/sba/`, `internal/aaa/eap/`
> Performance target: p99 < 50ms for auth/acct hot path.

---

## RADIUS (RFC 2865/2866)

### Overview

- **Library**: `layeh/radius` (Go native implementation)
- **Auth port**: UDP 1812 (env: `RADIUS_AUTH_PORT`)
- **Accounting port**: UDP 1813 (env: `RADIUS_ACCT_PORT`)
- **Shared secret**: env `RADIUS_SECRET` (per-operator secrets stored in TBL-05 `operators.settings`)
- **Packet size**: Max 4096 bytes (RFC default)

### Key Attributes

| Attribute | Type | ID | Usage in Argus |
|-----------|------|-----|----------------|
| User-Name | string | 1 | Set to IMSI. Used as primary subscriber identifier for auth lookup. |
| User-Password | string | 2 | Used in PAP authentication. MD5-XOR with shared secret per RFC. |
| NAS-IP-Address | IPv4 | 4 | Source NAS (operator equipment). Used for operator routing and anomaly detection (SIM cloning = same IMSI from different NAS). |
| NAS-Identifier | string | 32 | NAS hostname. Logged for audit and diagnostics. |
| Framed-IP-Address | IPv4 | 8 | IP assigned to SIM. Set in Access-Accept from IP pool allocation. |
| Framed-IPv6-Prefix | IPv6 prefix | 97 | IPv6 prefix assigned to SIM (dual-stack). |
| Session-Timeout | integer | 27 | Hard session timeout in seconds (from policy `session_timeout`). Set in Access-Accept. |
| Idle-Timeout | integer | 28 | Idle timeout in seconds (from policy `idle_timeout`). Set in Access-Accept. |
| Acct-Session-Id | string | 44 | Unique session identifier. Used to correlate Start/Interim/Stop. Stored in TBL-17 `sessions.acct_session_id`. |
| Acct-Status-Type | integer | 40 | 1=Start, 2=Stop, 3=Interim-Update. Drives session lifecycle. |
| Acct-Input-Octets | integer | 42 | Bytes received by NAS (download from device perspective). |
| Acct-Output-Octets | integer | 43 | Bytes sent by NAS (upload from device perspective). |
| Acct-Input-Gigawords | integer | 52 | High 32 bits of Acct-Input-Octets (for >4GB sessions). |
| Acct-Output-Gigawords | integer | 53 | High 32 bits of Acct-Output-Octets. |
| Acct-Session-Time | integer | 46 | Session duration in seconds. |
| Acct-Terminate-Cause | integer | 49 | Reason for session termination. |
| Filter-Id | string | 11 | Policy name/identifier. Set in Access-Accept to communicate policy to NAS. |

### 3GPP Vendor-Specific Attributes (Vendor ID 10415)

| Attribute | VSA Type | Usage |
|-----------|----------|-------|
| 3GPP-IMSI | 1 | IMSI (redundant with User-Name, but some NAS send here). |
| 3GPP-MSISDN | 3 | MSISDN of the subscriber. Stored in session. |
| 3GPP-RAT-Type | 21 | Radio Access Type. Values: 1=UTRAN, 2=GERAN, 3=WLAN, 6=EUTRAN(LTE), 7=NB-IoT(mapped), 8=eLTE, 9=NR. Critical for policy evaluation and cost calculation. |
| 3GPP-User-Location-Info | 22 | Cell/TAI location. Logged for analytics. |
| 3GPP-SGSN-Address | 6 | SGSN/MME address. Used for operator identification. |
| 3GPP-Charging-Id | 2 | Carrier charging correlation ID. |

### Dictionary Loading

```go
import "layeh.com/radius/vendors/threeGPP"

// Dictionary is loaded at startup. Custom attributes registered:
parser := radius.NewParser()
parser.RegisterVendor(10415, threeGPP.Dictionary)
```

Vendor dictionaries loaded from `internal/aaa/radius/dictionary/` at startup. Format follows FreeRADIUS dictionary syntax for compatibility.

### Packet Processing Flow

```
UDP packet received on :1812 (goroutine from pool)
    │
    ├─ Decode RADIUS packet (layeh/radius)
    ├─ Validate authenticator (shared secret)
    ├─ Extract IMSI from User-Name or 3GPP-IMSI
    ├─ Redis: lookup SIM by IMSI → get SIM config, APN, policy
    │   (cache miss → PostgreSQL lookup → cache populate)
    ├─ Check SIM state (must be 'active')
    ├─ EAP handling if EAP-Message attribute present
    │   └─ Delegate to internal/aaa/eap/ (EAP-SIM, EAP-AKA, EAP-AKA')
    ├─ Evaluate policy (compiled rules from Redis cache)
    ├─ Route to operator adapter (IMSI prefix table)
    ├─ Operator adapter: forward/process
    ├─ Build Access-Accept/Access-Reject response
    │   ├─ Set Framed-IP-Address (from pool)
    │   ├─ Set Session-Timeout, Idle-Timeout (from policy)
    │   ├─ Set Filter-Id (policy name)
    │   └─ Set vendor-specific QoS attributes
    ├─ Redis: create/update session record
    ├─ NATS: publish auth event (async)
    └─ Send UDP response
```

### CoA/DM (Change of Authorization / Disconnect-Message)

Argus sends CoA (RFC 5176) to active NAS to modify or terminate sessions:

- **CoA (Code 43)**: Sent when policy changes mid-session (rollout, throttle action). Contains updated Session-Timeout, bandwidth attributes.
- **DM (Code 40)**: Sent to disconnect a session (SIM suspend/terminate, admin force-disconnect).

```
Argus → NAS (UDP port 3799 by convention, configurable per operator)
    ├─ Acct-Session-Id (identifies the session)
    ├─ User-Name (IMSI)
    ├─ Updated attributes (CoA) or no extra attrs (DM)
    └─ Authenticator (signed with shared secret)
```

### CoA Status Lifecycle (FIX-234)

The `policy_assignments.coa_status` column tracks each SIM's CoA delivery state across 6 canonical values. The CHECK constraint `chk_coa_status` (migration `20260430000001`) enforces this set; the canonical Go const set lives in `internal/policy/rollout/coa_status.go`.

**State definitions:**
- `pending` — Just-inserted assignment row; transient, set by `AssignSIMsToVersion` writer.
- `queued` — `sendCoAForSIM` has identified active sessions and is dispatching CoA.
- `acked` — CoA delivered + ack received from RADIUS/Diameter.
- `failed` — Dispatch attempted but failed after retries.
- `no_session` — SIM has no active session at the time of policy assignment; nothing to push to.
- `skipped` — Policy rule indicated CoA-skip (e.g., low-priority change with no protocol delta).

**Lifecycle state machine:**

```
                  ┌──────────┐
       insert →   │ pending  │
                  └────┬─────┘
                       │  sendCoAForSIM
                       ▼
              ┌────────────────────┐
              │  has active session?│
              └────┬─────────┬─────┘
                   │ no      │ yes
                   ▼         ▼
            ┌──────────┐  ┌────────┐
            │no_session│  │ queued │
            └────┬─────┘  └────┬───┘
                 │             │ dispatch
       session.started        ▼
            (re-fire)    ┌─────────────┐
                 │       │ acked|failed│
                 └──────►└─────────────┘
```

**Re-fire on `session.started`:** When a SIM with `coa_status='no_session'` starts a new session, the `coaSessionResender` subscriber (queue group `rollout-coa-resend`) re-invokes `sendCoAForSIM` to push the pending policy. A 60-second dedup window (via `coa_sent_at`) prevents thrashing on rapid session start/stop cycles.

**Failure alerter:** The `coa_failure_alerter` background job (cron `* * * * *`, every minute) sweeps for rows with `coa_status='failed' AND coa_sent_at < NOW() - 5min` and creates a high-severity alert via `AlertStore.UpsertWithDedup` (dedup key `coa_failed:<sim_id>`).

**Metric:** `argus_coa_status_by_state{state}` Prometheus gauge (registered in `metrics.CoAStatusByState`) — refreshed on each alerter sweep with the current per-state counts.

**Skipped state:** Currently set explicitly by callers that determine a CoA push is unnecessary (e.g., a metadata-only policy update). Not currently auto-set by `sendCoAForSIM` — see future hardening for skip heuristics.

---

## Diameter (RFC 6733)

### Overview

- **Port**: TCP 3868 (env: `DIAMETER_PORT`)
- **Transport**: TCP with optional TLS (Diameter/TLS)
- **Origin-Host**: env `DIAMETER_ORIGIN_HOST` (e.g., `argus.example.com`)
- **Origin-Realm**: env `DIAMETER_ORIGIN_REALM` (e.g., `example.com`)
- **Vendor-Id**: 10415 (3GPP) for all 3GPP AVPs

### Connection Management

| Message | Direction | Purpose |
|---------|-----------|---------|
| CER (Capabilities-Exchange-Request) | Argus → Peer | Initiate connection, declare supported applications |
| CEA (Capabilities-Exchange-Answer) | Peer → Argus | Confirm capabilities |
| DWR (Device-Watchdog-Request) | Bidirectional | Keepalive (every 30s) |
| DWA (Device-Watchdog-Answer) | Bidirectional | Keepalive response |
| DPR (Disconnect-Peer-Request) | Either | Graceful disconnect |
| DPA (Disconnect-Peer-Answer) | Either | Confirm disconnect |

### Gx Interface (Policy and Charging Control)

Application-Id: 16777238 (3GPP Gx)

**CCR/CCA (Credit-Control-Request/Answer)**:

| AVP | Code | Type | Usage |
|-----|------|------|-------|
| Session-Id | 263 | UTF8String | Unique session identifier |
| CC-Request-Type | 416 | Enumerated | 1=INITIAL_REQUEST, 2=UPDATE_REQUEST, 3=TERMINATION_REQUEST |
| CC-Request-Number | 415 | Unsigned32 | Sequence number per session |
| Subscription-Id | 443 | Grouped | Contains IMSI (type 1) and MSISDN (type 0) |
| IP-CAN-Type | 1027 | Enumerated | 0=3GPP-GPRS, 5=3GPP-EPS, 6=Non-3GPP-EPS, 7=3GPP-5GS |
| RAT-Type (3GPP) | 1032 | Enumerated | 1000=UTRAN, 1004=EUTRAN, 1005=NB-IoT(mapped), 1009=NR |
| 3GPP-User-Location-Info | 22 (vendor 10415) | OctetString | ULI (TAI + ECGI) |
| Charging-Rule-Install | 1001 | Grouped | Install QoS/charging rules |
| Charging-Rule-Remove | 1002 | Grouped | Remove rules |
| Charging-Rule-Definition | 1003 | Grouped | Rule with QoS and filter |
| QoS-Information | 1016 | Grouped | Contains bandwidth limits |
| Max-Requested-Bandwidth-UL | 516 | Unsigned32 | Upload bandwidth (bits/sec) |
| Max-Requested-Bandwidth-DL | 515 | Unsigned32 | Download bandwidth (bits/sec) |
| QoS-Class-Identifier | 1028 | Enumerated | QCI value (1-9 for LTE, 5QI for 5G) |
| Bearer-Identifier | 1020 | OctetString | EPS bearer ID |
| Event-Trigger | 1006 | Enumerated | Triggers for re-auth (e.g., RAT change, QoS change) |

**Gx Message Flow**:
```
UE attaches → PGW sends CCR-I to Argus
    Argus: lookup IMSI → evaluate policy → determine QoS
    Argus: if sim.ip_address_id == nil && sim.apn_id != nil → AllocateIP (STORY-092)
    Argus → CCA-I with:
        ├─ Charging-Rule-Install (QoS-Information, filter)
        └─ Framed-IP-Address (AVP 8, RFC 7155 §4.4.10.5.1, vendor=0, M flag)

Policy changes → Argus sends RAR (Re-Auth-Request) to PGW
    PGW → RAA (Re-Auth-Answer) confirming rule installation

UE detaches → PGW sends CCR-T to Argus
    Argus: close session, generate CDR
    Argus: if allocation_type == 'dynamic' → ReleaseIP (STORY-092; static preserved)
    Argus → CCA-T confirming
```

`AVPCodeFramedIPAddress = 8` is defined in `internal/aaa/diameter/avp.go` with explicit vendor=0 (not 3GPP vendor 10415) per RFC 7155 NASREQ binding. Encoded via the shared `NewAVPAddress` helper.

### Gy Interface (Online Charging)

Application-Id: 4 (Diameter Credit-Control)

| AVP | Code | Type | Usage |
|-----|------|------|-------|
| CC-Request-Type | 416 | Enumerated | 1=INITIAL, 2=UPDATE, 3=TERMINATION, 4=EVENT |
| CC-Request-Number | 415 | Unsigned32 | Sequence number |
| Requested-Service-Unit | 437 | Grouped | Units requested (time, volume) |
| Used-Service-Unit | 446 | Grouped | Units consumed since last report |
| Granted-Service-Unit | 431 | Grouped | Units granted by Argus |
| CC-Total-Octets | 421 | Unsigned64 | Total bytes (in Granted/Used-Service-Unit) |
| CC-Input-Octets | 412 | Unsigned64 | Download bytes |
| CC-Output-Octets | 414 | Unsigned64 | Upload bytes |
| CC-Time | 420 | Unsigned32 | Granted/used time in seconds |
| Validity-Time | 448 | Unsigned32 | Time before quota must be re-requested |
| Final-Unit-Indication | 430 | Grouped | What to do when quota runs out |
| Final-Unit-Action | 449 | Enumerated | 0=TERMINATE, 1=REDIRECT, 2=RESTRICT_ACCESS |
| Rating-Group | 432 | Unsigned32 | Grouping for rating/billing |
| Service-Identifier | 439 | Unsigned32 | Service being charged |
| Subscription-Id | 443 | Grouped | IMSI and MSISDN |

**Gy Message Flow**:
```
Session starts → OCS (Argus) receives CCR-I
    Argus: check balance/quota → grant initial units
    Argus → CCA-I with Granted-Service-Unit

Quota running low → PGW sends CCR-U with Used-Service-Unit
    Argus: deduct used, check remaining, grant new units
    Argus → CCA-U with Granted-Service-Unit (or Final-Unit-Indication)

Quota exhausted → Argus sends Final-Unit-Indication in CCA-U
    Action per policy: TERMINATE (disconnect) or RESTRICT_ACCESS (throttle)

Session ends → PGW sends CCR-T with final Used-Service-Unit
    Argus: final deduction, generate CDR
    Argus → CCA-T confirming
```

### Diameter Error Handling

| Result-Code | Meaning | Argus Behavior |
|-------------|---------|----------------|
| 2001 | DIAMETER_SUCCESS | Normal |
| 3002 | DIAMETER_UNABLE_TO_DELIVER | Peer down, trigger circuit breaker |
| 4001 | DIAMETER_AUTHENTICATION_REJECTED | Log failed auth, increment counter |
| 5001 | DIAMETER_AVP_UNSUPPORTED | Log warning, continue without AVP |
| 5012 | DIAMETER_UNABLE_TO_COMPLY | Generic error, retry with backoff |

---

## RadSec (RFC 6614 — RADIUS over TLS)

### Overview

- **Port**: TCP 2083 (standard RadSec port)
- **Transport**: TLS 1.2+ wrapping standard RADIUS packets
- **Purpose**: Encrypted RADIUS transport for operators requiring confidentiality beyond shared-secret

### Certificate Requirements

| Certificate | Purpose | Format | Config |
|-------------|---------|--------|--------|
| Server cert | Argus RadSec server identity | PEM (X.509) | env `RADSEC_CERT_PATH` |
| Server key | Private key for server cert | PEM (PKCS#8) | env `RADSEC_KEY_PATH` |
| CA cert | Verify peer (NAS/operator) certificates | PEM bundle | Stored in operator settings (TBL-05) |
| Client cert | Mutual TLS — Argus authenticating to NAS | PEM (X.509) | Per-operator config in TBL-05 |

### TLS Configuration

```go
tlsConfig := &tls.Config{
    MinVersion:   tls.VersionTLS12,
    Certificates: []tls.Certificate{serverCert},
    ClientAuth:   tls.RequireAndVerifyClientCert,
    ClientCAs:    operatorCAPool,
    CipherSuites: []uint16{
        tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
        tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
    },
}
```

### Protocol Differences from UDP RADIUS

- **No shared secret for packet authentication**: TLS provides integrity and confidentiality. The RADIUS Authenticator field is set to all zeros (per RFC 6614).
- **TCP framing**: Each RADIUS packet is prefixed by its length in the TLS stream. No retransmission needed (TCP handles it).
- **Connection persistence**: Single TLS connection carries many RADIUS exchanges. Connection loss triggers reconnect with exponential backoff.
- **Same RADIUS attributes**: All RADIUS attributes work identically. The payload is a standard RADIUS packet.

### Operator Configuration

Each operator can be configured to use RadSec instead of UDP RADIUS:

```json
// operators.settings JSONB
{
  "radius": {
    "transport": "radsec",      // "udp" | "radsec"
    "host": "radius.turkcell.com.tr",
    "port": 2083,
    "ca_cert": "...",           // PEM-encoded CA cert
    "client_cert": "...",       // PEM-encoded client cert (for mTLS)
    "client_key_ref": "vault://...", // reference to secure key storage
    "tls_server_name": "radius.turkcell.com.tr"
  }
}
```

---

## 5G SBA (Service-Based Architecture)

### Overview

- **Port**: HTTPS 8443 (env: `SBA_PORT`)
- **Transport**: HTTP/2 with TLS
- **Purpose**: Proxy for 5G SA core network integration (AUSF, UDM)
- **Implementation**: `internal/aaa/sba/`

### AUSF Interface (Authentication Server Function)

**Endpoint**: `/nausf-auth/v1/ue-authentications`

**Authentication Initiation (POST)**:
```
POST /nausf-auth/v1/ue-authentications
Content-Type: application/json

{
  "supiOrSuci": "imsi-286010123456789",
  "servingNetworkName": "5G:mnc001.mcc286.3gppnetwork.org",
  "resynchronizationInfo": null
}
```

**Response (201 Created)**:
```json
{
  "authType": "5G_AKA",
  "5gAuthData": {
    "rand": "base64...",
    "autn": "base64...",
    "hxresStar": "base64..."
  },
  "_links": {
    "5g-aka": {
      "href": "/nausf-auth/v1/ue-authentications/{authCtxId}/5g-aka-confirmation"
    }
  }
}
```

**Authentication Confirmation (PUT)**:
```
PUT /nausf-auth/v1/ue-authentications/{authCtxId}/5g-aka-confirmation
Content-Type: application/json

{
  "resStar": "base64..."
}
```

### UDM Interface (Unified Data Management)

**UECM Registration (PUT)**:
```
PUT /nudm-uecm/v1/{supi}/registrations/amf-3gpp-access
Content-Type: application/json

{
  "amfInstanceId": "uuid",
  "deregCallbackUri": "https://amf.example.com/callback",
  "guami": {
    "plmnId": { "mcc": "286", "mnc": "01" },
    "amfId": "cafe00"
  },
  "ratType": "NR",
  "initialRegistrationInd": true
}
```

**SDM Subscription Data (GET)**:
```
GET /nudm-sdm/v1/{supi}/nssai
GET /nudm-sdm/v1/{supi}/sm-data?single-nssai={"sst":1,"sd":"000001"}
```

### Nsmf (Session Management Function) — mock (STORY-092)

Minimal `Nsmf_PDUSession` mock mounted at `internal/aaa/sba/nsmf.go` — Create + Release only. In a real 5G Core the SMF owns UE IP allocation during `Nsmf_PDUSession_CreateSMContext` (per 3GPP TS 23.502 §4.3.2); AUSF and UDM do not allocate IPs. Argus is a management platform / test harness, not a production 5GC, so it ships this minimal mock alongside the existing AUSF/UDM mocks so simulator harnesses drive a full end-to-end IP allocation pipeline.

**Create SM Context (POST)** — `/nsmf-pdusession/v1/sm-contexts` (API-304):

```
POST /nsmf-pdusession/v1/sm-contexts
Content-Type: application/json

{
  "supi": "imsi-286010123456789",
  "dnn": "internet",
  "sNssai": { "sst": 1, "sd": "000001" }
}
```

Response: `201 Created` with `Location: /nsmf-pdusession/v1/sm-contexts/{smContextRef}`, body includes allocated `ueIpv4Address`. 3GPP-native ProblemDetails on failure (USER_NOT_FOUND, SERVING_NETWORK_NOT_AUTHORIZED, SYSTEM_FAILURE, DNN_NOT_SUPPORTED).

**Release SM Context (DELETE)** — `/nsmf-pdusession/v1/sm-contexts/{smContextRef}` (API-305):

Releases dynamic IP (static preserved via `allocation_type` gate — matches RADIUS/Diameter symmetric release). Returns `204 No Content`. Unknown `smContextRef` returns 204 (idempotent).

**Scope discipline**: no PATCH, no QoS update, no PCF, no UPF selection — strictly Create + Release. STORY-089 will absorb this mock into the new `cmd/operator-sim` container (tracked on ROUTEMAP D-039 for holistic SBA section re-sweep).

### Network Slice (NSSAI) in Authentication

5G authentication requests include S-NSSAI (Single Network Slice Selection Assistance Information):

```json
{
  "supiOrSuci": "imsi-286010123456789",
  "servingNetworkName": "5G:mnc001.mcc286.3gppnetwork.org",
  "requestedNssai": [
    { "sst": 1, "sd": "000001" },
    { "sst": 3, "sd": "000002" }
  ]
}
```

| SST | Description | Argus Mapping |
|-----|-------------|---------------|
| 1 | eMBB (Enhanced Mobile Broadband) | Standard IoT policy |
| 2 | URLLC (Ultra-Reliable Low Latency) | Low-latency policy (if available) |
| 3 | MIoT (Massive IoT) | NB-IoT/LTE-M optimized policy |

Argus uses the slice information to select the appropriate policy during evaluation. The `slice_sst` and `slice_sd` are stored in the session record and available as policy conditions.

### SBA Error Handling

All SBA endpoints return 3GPP-standard error responses:

```json
{
  "status": 403,
  "cause": "SERVING_NETWORK_NOT_AUTHORIZED",
  "detail": "The serving network is not authorized for this subscriber"
}
```

| HTTP Status | 3GPP Cause | Argus Behavior |
|-------------|------------|----------------|
| 400 | MANDATORY_IE_INCORRECT | Log, reject auth |
| 403 | SERVING_NETWORK_NOT_AUTHORIZED | Log, reject auth, alert |
| 404 | USER_NOT_FOUND | SIM not in system or not active |
| 500 | SYSTEM_FAILURE | Retry with backoff, circuit breaker |
| 504 | NF_CONGESTION | Back-pressure, reduce request rate |

### TLS Configuration for SBA

```
TLS_CERT_PATH=/certs/argus-sba.pem
TLS_KEY_PATH=/certs/argus-sba-key.pem
```

5G SBA requires mutual TLS (mTLS) with NF-specific certificates. Each operator's 5G core has its own CA trust chain configured in operator settings.

---

## Protocol Bridge (Diameter ↔ RADIUS)

Argus can act as a protocol bridge when an operator speaks one protocol but the upstream speaks another:

```
NAS (RADIUS) → Argus → Operator core (Diameter Gx/Gy)
NAS (Diameter) → Argus → Operator core (RADIUS)
```

### Attribute Mapping

| RADIUS Attribute | Diameter AVP | Notes |
|-----------------|--------------|-------|
| User-Name | Subscription-Id (IMSI) | IMSI extracted from User-Name |
| Framed-IP-Address | Framed-IP-Address (AVP 8) | Direct mapping |
| Session-Timeout | Session-Timeout (AVP 27) | Direct mapping |
| Acct-Session-Id | Session-Id (AVP 263) | Argus generates consistent ID |
| Acct-Input-Octets + Gigawords | CC-Input-Octets (AVP 412) | Combined into 64-bit |
| Acct-Output-Octets + Gigawords | CC-Output-Octets (AVP 414) | Combined into 64-bit |
| 3GPP-RAT-Type (VSA) | RAT-Type (AVP 1032) | Value mapping required |
| Filter-Id | Charging-Rule-Name | Policy name mapping |

### RAT-Type Value Mapping

| Description | RADIUS 3GPP-RAT-Type | Diameter RAT-Type | Argus Internal |
|-------------|---------------------|-------------------|----------------|
| UTRAN (3G) | 1 | 1000 | `utran` |
| GERAN (2G) | 2 | 1001 | `geran` |
| E-UTRAN (LTE) | 6 | 1004 | `lte` |
| NB-IoT | 6 (with ext) | 1005 (mapped) | `nb_iot` |
| LTE-M | 6 (with ext) | 1004 (with ext) | `lte_m` |
| NR (5G) | 9 (mapped) | 1009 | `nr_5g` |

Note: NB-IoT and LTE-M are variants of E-UTRAN. Standard RADIUS/Diameter may report them as E-UTRAN. Argus uses extended attributes or operator-specific VSAs to distinguish them. The operator adapter is responsible for mapping.

## IMEI Capture (Cross-Protocol) — Phase 11

> Reference: ADR-004 (IMEI Binding Architecture). The Argus AAA engine reads the device identity (IMEI / IMEI-SV / PEI) presented at authentication on every supported protocol and feeds it into a normalized `device.*` SessionContext consumed by the policy engine. Capture is **read-only and null-safe**: missing IMEI never blocks authentication, only weakens enforcement strength for binding-mode-enabled SIMs.

### RADIUS — 3GPP-IMEISV VSA

- **Vendor-Id**: 10415 (3GPP)
- **Vendor-Type**: 20 (`3GPP-IMEISV`)
- **Reference**: 3GPP TS 29.061 §16.4.7
- **Wire format**: ASCII string `"<15-digit IMEI>,<2-digit Software-Version>"` (the comma is literal). Older IEs may carry the bare 16-digit IMEISV without comma — parsers MUST handle both shapes.
- **Parser contract** (`internal/protocol/radius`):
  - Split on `,`. If split yields 2 parts: `imei = part[0]`, `software_version = part[1]`.
  - If no `,` and length is 16: `imei = first15`, `software_version = last1+pad` (legacy IMEISV).
  - Validate IMEI is exactly 15 numeric digits; validate Software-Version is 2 numeric digits. Fail-soft: malformed → leave SessionContext fields nil + emit `argus_imei_capture_parse_errors_total{protocol="radius"}` counter.
- Captured on Access-Request (auth) AND Accounting-Start (mid-session change detection).

### Diameter S6a — Terminal-Information AVP

- **AVP code**: 350, grouped, M-bit set
- **Reference**: 3GPP TS 29.272 §7.3.3 (Terminal-Information), §5.2.2.1.1 (AIR), §5.2.2.1.3 (ULR)
- **Sub-AVPs**:
  | Sub-AVP | Code | Type | Notes |
  |---------|------|------|-------|
  | IMEI | 1402 | UTF8String | 15 digits |
  | Software-Version | 1403 | UTF8String | 2 digits |
  | IMEI-SV | 1404 | UTF8String | 16 digits (alt to IMEI+SV pair) |
- **Captured during**: AIR (Authentication-Information-Request) and ULR (Update-Location-Request) command exchanges (S6a interface).
- **Parser contract** (`internal/protocol/diameter`): unpack grouped AVP 350; prefer `IMEI` + `Software-Version` pair if both present; fall back to splitting `IMEI-SV` (1404) when only it is supplied. Same fail-soft + counter behaviour as RADIUS.

### 5G SBA — PEI (Permanent Equipment Identifier)

- **Source**: `Nudm_UEAuthentication` request body and `Namf_Communication` UE-context fields populated upstream by the AMF.
- **Reference**: 3GPP TS 23.003 §6.2A (PEI format), TS 29.503 (Nudm), TS 29.518 (Namf)
- **Wire format**: PEI is a tagged URI:
  - `imei-<15 digits>` — 4G-style identity
  - `imeisv-<16 digits>` — IMEISV (15-digit IMEI + 2-digit SV concatenated, last 1 digit padding per spec)
  - `mac-<12 hex>` / `eui64-<16 hex>` — non-3GPP access; Argus stores raw value but does NOT participate in IMEI binding logic.
- **Parser contract** (`internal/aaa/sba`): strip prefix; for `imeisv-` split into 15-digit IMEI + 2-digit SV; for `imei-` keep 15-digit IMEI and leave SV nil; for non-3GPP forms, pass through to `device.peri_raw` for forensic logging only.

### SessionContext Population

After protocol-specific parsing the AAA engine writes the captured device identity onto `SessionContext` **before** policy DSL evaluation. STORY-093 ships the *flat* shape (lower touch, no struct nesting); STORY-094 will migrate this surface to a nested `SessionContext.Device { ... }` struct as the binding pre-check + change-detection workflow lands and additional fields (TAC, IMEISV, PEIRaw, CaptureProtocol, BindingStatus) are populated alongside IMEI / SoftwareVersion.

**STORY-093 (current — flat fields on `SessionContext`):**

```go
SessionContext.IMEI            string  // normalized 15 digits, or empty
SessionContext.SoftwareVersion string  // 2 digits, or empty
```

**STORY-094+ (forward — nested `Device` struct):**

```
SessionContext.Device {
  IMEI               string  // normalized 15 digits, or empty
  TAC                string  // first 8 digits of IMEI, or empty
  SoftwareVersion    string  // 2 digits, or empty
  IMEISV             string  // 16-digit concatenated form, or empty
  PEIRaw             string  // raw PEI string for 5G SBA only
  CaptureProtocol    enum    // "radius" | "diameter_s6a" | "5g_sba"
  BindingStatus      enum    // populated by binding pre-check (see ADR-004): "verified", "pending", "mismatch", "unbound", "disabled"
}
```

`BindingStatus = "disabled"` when `sims.binding_mode IS NULL` (default for migrated rows) and the binding pre-check is skipped.

### Out of Scope (v1)

EIR (Equipment Identity Register) integration via Diameter S13 (4G/EPC) or 5G N17 is **OUT OF SCOPE for v1** per ADR-004. No EIR client, no S13 stub, no N17 SBA mock, no AVP scaffolding for ME-Identity-Check is implemented or stubbed. Local enforcement via the IMEI pool tables (`imei_whitelist` / `imei_greylist` / `imei_blacklist`) and per-SIM `binding_mode` is the policy decision point; integration with operator EIRs is a future-track item should a customer require it.

## eSIM M2M (SGP.02) Provisioning

> Implemented in FIX-235. Runtime packages: `internal/smsr/`, `internal/job/` (OTA dispatcher + reaper + stock alerter + bulk-switch processor), `internal/store/` (`esim_ota_commands`, `esim_profile_stock`), `internal/api/esim/`.

### SGP.02 vs SGP.22 — Consumer Pull vs M2M Push

| Dimension | SGP.22 (Consumer) | SGP.02 (M2M) |
|-----------|-------------------|--------------|
| Architecture | SM-DP+ pull (LPAd on device initiates) | SM-SR push (platform sends OTA command) |
| Profile discovery | QR code / activation code | Platform-controlled EID registry |
| Consent model | End-user confirmation on device | Operator / platform-driven, no UI on device |
| Transport | HTTPS to SM-DP+ | OTA SMS / CAT-TP or HTTPS to SM-SR |
| Target device | Consumer smartphone | IoT/M2M eUICC (no display) |
| Argus role | Not implemented | Platform → SM-SR → eUICC |

Argus implements the **M2M push model** (SGP.02). The platform acts as the EUM/operator platform: it issues OTA commands to a Subscription Manager – Secure Routing (SM-SR) which delivers them to the eUICC over air (CAT-TP / SMS-PP or HTTPS depending on the operator's SM-SR capabilities).

### SM-SR Push Architecture

```
Argus Platform
│
│  POST /smsr/push  (internal/smsr.Client.Push)
│  body: { command_id, eid, command_type, target_iccid, operator_id }
│
▼
SM-SR (operator-managed or third-party)
│
│  OTA SMS-PP / CAT-TP / HTTPS Bearer
│
▼
eUICC (M2M eSIM in IoT device)
│
│  HTTPS callback → POST /api/v1/esim/ota/callback
│  header: X-SMSR-Signature: <hmac-sha256>
│  body: { command_id, eid, status: "acked|failed", error_code? }
│
▼
Argus Callback Handler (internal/api/esim)
```

The SM-SR client interface (`internal/smsr.Client`) is injected at boot. A mock implementation (`internal/smsr.MockClient`) provides deterministic test doubles with configurable fail-rate and latency.

### State Machine

Each OTA command progresses through the following states. All transitions are enforced at the store layer (`internal/store.EsimOTACommandStore`) — invalid transitions return `ErrEsimOTAInvalidTransition`.

```
         ┌──────────────────────────────────────┐
         │                                      │
  INSERT  ▼         dispatcher              callback
         queued ──────────────► sent ──────────────► acked (terminal)
                                 │                 └──────────────► failed (terminal)
                                 │
                         timeout reaper
                                 │
                                 ├─ retries < max ─► queued (re-enqueue)
                                 └─ retries ≥ max ─► failed (terminal)
```

| State | Description |
|-------|-------------|
| `queued` | Command inserted; ready for dispatcher to pick up. |
| `sent` | Push accepted by SM-SR; awaiting eUICC callback. |
| `acked` | eUICC confirmed profile switch. Terminal — no further transitions. |
| `failed` | Permanent failure (max retries exceeded or SM-SR rejected). Terminal. |

### Retry Policy

Exponential backoff with jitter, capped at 5 attempts:

| Attempt | Delay before retry |
|---------|-------------------|
| 1 | 30 s |
| 2 | 60 s |
| 3 | 120 s |
| 4 | 240 s |
| 5 | 480 s |

After attempt 5 the command transitions to `failed` (terminal). The timeout reaper (`ESimOTATimeoutReaperProcessor`) runs on a schedule (default 60 s tick) and re-queues commands stuck in `sent` past their `next_retry_at` deadline.

### Rate Limiting

Token-bucket rate limiting is applied per operator to avoid overloading SM-SR endpoints:

- **Algorithm**: `golang.org/x/time/rate` token bucket, one limiter per `operator_id` (lazy-initialised).
- **Default**: `SMSR_RATE_LIMIT_RPS` env var (default `100`). Per-operator overrides are not yet implemented (D-163).
- **Behaviour when limited**: the dispatcher parks the batch and returns without error; the job scheduler retries on the next tick.

### Stock Allocation

eSIM profile stock (`esim_profile_stock`) tracks available profiles per `(tenant_id, operator_id)` pair. Allocation is atomic:

```sql
UPDATE esim_profile_stock
   SET available = available - 1
 WHERE tenant_id = $1
   AND operator_id = $2
   AND available > 0
 RETURNING *
```

If no row is updated, `ErrStockExhausted` is returned and the SIM is recorded as `STOCK_EXHAUSTED` in the bulk-switch error report. The dispatcher never allocates stock — allocation is the responsibility of the bulk-switch processor before inserting into `esim_ota_commands`.

### HMAC Callback Verification

The SM-SR callback endpoint (`POST /api/v1/esim/ota/callback`) verifies the `X-SMSR-Signature` header before processing any payload:

- **Algorithm**: HMAC-SHA-256 over the raw request body prefixed with the Unix timestamp: `<unix_ts>.<body_bytes>`
- **Header format**: `X-SMSR-Signature: t=<unix_ts>,v1=<hex_digest>`
- **Secret**: `SMSR_CALLBACK_SECRET` env var (required; boot-fatal if absent when `SMSR_ENABLED=true`)
- **Replay window**: 300 seconds — requests with `|now - t| > 300` are rejected with `403 Forbidden`
- **Failure response**: `401 Unauthorized` on signature mismatch; `403 Forbidden` on replay

### Bulk Switch Flow

The `BulkEsimSwitchProcessor` handles the `bulk.esim_switch` job type:

```
1. Deserialise payload (target_operator_id, sim_ids or segment filter)
2. Acquire distributed lock (Redis SETNX, 5-minute TTL)
3. For each eSIM in the batch:
   a. GetEnabledProfileForSIM → current profile
   b. List disabled profiles for target operator (at most 1)
   c. Allocate stock (atomic UPDATE; skip SIM on ErrStockExhausted)
   d. BatchInsert single ota_commands row (command_type="switch")
   e. Emit bulk.ota_enqueue audit log entry
4. Release distributed lock
5. Publish job progress via WebSocket (bus subject: esim.bulk.progress)
6. Write job result summary (processed_count, failed_count, error_details)
```

### Audit Log Entries

| Event action | Trigger | Key fields |
|-------------|---------|-----------|
| `ota.dispatch` | Dispatcher sends push to SM-SR | `eid`, `command_id`, `operator_id` |
| `ota.callback_acked` | Callback received with `status=acked` | `eid`, `command_id`, `profile_id` |
| `ota.callback_failed` | Callback received with `status=failed` | `eid`, `command_id`, `error_code` |
| `bulk.ota_enqueue` | Bulk-switch processor inserts ota_command | `job_id`, `sim_id`, `target_operator_id` |

### NATS Bus Subjects

| Subject | Published when | Payload |
|---------|---------------|---------|
| `esim.command.issued` | Dispatcher successfully sends push | `{ command_id, eid, operator_id, tenant_id }` |
| `esim.command.acked` | Callback acked | `{ command_id, eid, tenant_id }` |
| `esim.command.failed` | Callback failed or max retries exceeded | `{ command_id, eid, error_code, tenant_id }` |

All events use the canonical `bus.Envelope` wire format (see `docs/architecture/WEBSOCKET_EVENTS.md`).

### Integration & Load Test Scenarios

| Scenario | Status | Notes |
|----------|--------|-------|
| 100 SIMs → 100 ota_commands, all stock allocated | Implemented (`internal/job/esim_bulkswitch_integration_test.go`, runs without -short) | In-process fakes; no external deps |
| Stock exhaustion mid-batch — partial OTA insert | Implemented (unit test `TestBulkEsimSwitch_StockExhausted_SkipsOTAInsert`) | — |
| Dispatcher consumes queued → sent → acked end-to-end | Covered by dispatcher unit tests + timeout-reaper unit tests | — |
| 10K SIM bulk switch — load test | **Deferred D-168** | Requires testcontainers PostgreSQL harness for real DB throughput. Run: `go test ./internal/job/... -run Load -v -race` once harness is available |

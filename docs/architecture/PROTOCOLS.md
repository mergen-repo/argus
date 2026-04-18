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

# Implementation Plan: STORY-084 — Simulator 5G SBA Client (AUSF/UDM)

## Goal

Extend the simulator with HTTP/2 clients for Argus's 5G Service-Based Architecture endpoints (AUSF for authentication, UDM for subscriber data). For operators with `5g.enabled: true`, a fraction of sessions use 5G-SBA authentication instead of RADIUS, producing the traffic patterns Argus's `:8443` SBA proxy sees in an actual 5G core.

## Architecture Context

### Components Involved

**New under `internal/simulator/`:**
```
sba/
  client.go            # HTTP/2 client with TLS, JSON bodies, 3GPP-compliant paths
  client_test.go
  ausf.go              # Nausf_UEAuthentication_Authenticate
  udm.go               # Nudm_UEContextManagement_Registration + Nudm_SDM_Get
  types.go             # AuthenticationInfo, AuthenticationResult, SubscriptionData structs
```

**Config addition:**
```yaml
operators:
  - code: turkcell
    5g:
      enabled: true
      host: argus-app
      port: 8443
      tls_skip_verify: true       # dev only
      auth_method: 5G_AKA          # 5G_AKA | EAP_AKA_PRIME
      fraction_of_sessions: 0.2    # 20% of turkcell sessions go through 5G
```

**Engine change** — for 5G-enabled operators, scenario picker adds a third dimension: protocol = random(radius=0.8, 5g=0.2) per session. 5G sessions skip RADIUS entirely and execute:
1. **POST /nausf-auth/v1/ue-authentications** (AUSF) → get authentication vector
2. **POST /nausf-auth/v1/ue-authentications/{authCtxId}/5g-aka-confirmation** → confirm
3. **PUT /nudm-uecm/v1/{supi}/registrations/amf-3gpp-access** (UDM) → register
4. **GET /nudm-sdm/v1/{supi}/am-data** → fetch subscription
5. Simulated data phase — no accounting protocol here; 5G uses CHF over HTTP/2 (future)
6. **DELETE /nudm-uecm/v1/{supi}/registrations/amf-3gpp-access** → deregister

### Minimal Request Shapes

**AUSF Authenticate (POST /nausf-auth/v1/ue-authentications):**
```json
{
  "supiOrSuci": "imsi-28601000000001",
  "servingNetworkName": "5G:mnc001.mcc286.3gppnetwork.org"
}
```
Response `201 Created`:
```json
{
  "authType": "5G_AKA",
  "5gAuthData": {
    "rand": "<hex>",
    "autn": "<hex>",
    "hxresStar": "<hex>"
  },
  "_links": {"5g-aka": {"href": "/nausf-auth/v1/ue-authentications/<ctx>/5g-aka-confirmation"}}
}
```

**5G-AKA Confirmation (POST /.../5g-aka-confirmation):**
```json
{ "resStar": "<computed>" }
```
Response `200 OK`:
```json
{ "authResult": "AUTHENTICATION_SUCCESSFUL", "supi": "imsi-28601000000001", "kseaf": "<hex>" }
```

### Crypto Stubs

5G-AKA is Milenage-based (f1..f5 functions). The simulator does NOT implement real Milenage — it sends a stub `resStar = sha256(rand || k)` where `k` is a per-SIM constant derived from IMSI. Argus's 5G SBA proxy (STORY-020) is likewise a proxy/mock, so crypto validation is expected-to-pass for any non-empty `resStar`. Document this as a **test-only** behavior; real 5G integration needs a proper Milenage library. Acceptable for dev simulator.

### Safety Envelope

- **5G opt-in per operator**; `fraction_of_sessions` caps exposure; defaults off.
- **TLS skip-verify** is allowed **only** when `ARGUS_SIM_ENV=dev` or when `tls_skip_verify: true` is explicitly set in config and `SIMULATOR_ENABLED=true`. Exits otherwise.
- **HTTP/2 connection pooling** — one `http.Client` per operator with `Transport: &http2.Transport{}`.

## Tasks

1. Add `golang.org/x/net/http2` dependency (probably transitively present).
2. Implement AUSF + UDM client calls with correct URL paths, headers (`Content-Type: application/json`, `3gpp-Sbi-Correlation-Info`), and JSON bodies.
3. Scenario picker extension: 5G vs RADIUS selection per session.
4. Integrate SBA flow into engine orchestrator; ensure cancellation & cleanup on SIGTERM.
5. Unit tests with `httptest` server asserting request paths, bodies, header presence.
6. Integration test: simulator 5G session end-to-end against live argus-app SBA proxy.
7. Metrics: `simulator_sba_requests_total{operator, service, endpoint}`, `simulator_sba_latency_seconds`.
8. Docs: extend `docs/architecture/simulator.md` with 5G section.

## Acceptance Criteria

- **AC-1** When `5g.enabled: true`, approximately 20% of the operator's sessions (±5% over 5-min window) use 5G-SBA instead of RADIUS.
- **AC-2** Full 5G flow (Authenticate → Confirmation → Registration → SDM Get → Deregistration) completes end-to-end against argus-app for each 5G session.
- **AC-3** Argus's SBA proxy logs the four expected request paths for each session.
- **AC-4** Metrics endpoint exposes `simulator_sba_*` counters; latency histogram p95 < 500ms in dev.
- **AC-5** `5g.enabled: false` disables all SBA calls for that operator; RADIUS-only flow unchanged.
- **AC-6** TLS skip-verify only activates when explicitly enabled in config; simulator refuses to skip TLS verification when `ARGUS_SIM_ENV=prod` even if config says so.

## Risks

- **HTTP/2 prior-knowledge vs ALPN**: argus-app SBA proxy may require ALPN h2. **Mitigation**: set `TLSClientConfig.NextProtos = []string{"h2"}` on the http2.Transport.
- **Service discovery (NRF)**: 3GPP stack normally uses an NRF for service discovery; our simulator hardcodes AUSF/UDM URLs. This is acceptable for a test simulator but documented as a known simplification.
- **Milenage stub may start failing if Argus tightens 5G auth**: any future change to argus-app's 5G crypto validation could break the simulator. **Mitigation**: track `internal/aaa/sba/` for crypto validation changes; update simulator when needed.

## Dependencies

- **STORY-082** simulator base must be in place.
- **STORY-083** is NOT a prerequisite — 5G can land before or after Diameter.
- Argus's 5G SBA proxy (STORY-020).

## Out of Scope

- NRF service discovery (simulator hardcodes URLs)
- CHF (Converged Charging Function) over HTTP/2 — future when Argus gains CHF support
- N32 (inter-PLMN) interfaces
- Full Milenage implementation (stub SHA-256 is used)

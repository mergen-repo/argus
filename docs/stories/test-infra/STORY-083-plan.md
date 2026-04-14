# Implementation Plan: STORY-083 — Simulator Diameter Client (Gx/Gy)

## Goal

Extend the simulator (STORY-082) with a Diameter client that exercises Argus's Gx (Policy Control) and Gy (Online Charging) interfaces. The simulator issues CCR-I → CCR-U × N → CCR-T for each policy-controlled session, paired with the RADIUS session lifecycle. Selecting which operators/SIMs use Diameter vs plain RADIUS is config-driven; the default keeps RADIUS-only to match STORY-082's baseline, and we opt specific operators into Diameter via a config flag.

## Architecture Context

### Components Involved

**New under `internal/simulator/`:**
```
diameter/
  client.go            # TCP connection + CER/DWR/DPR lifecycle
  client_test.go
  cer.go               # Capabilities-Exchange-Request/Answer
  ccr.go               # Credit-Control-Request (Gx + Gy variants)
  dictionary.go        # AVP codes: Subscription-Id, Framed-IP, Used-Service-Unit, etc.
  peer.go              # peer state machine (Closed → Wait-I-CEA → Open)
```

**Config addition** (`deploy/simulator/config.yaml`):
```yaml
operators:
  - code: turkcell
    radius_secret: ...
    diameter:
      enabled: true
      host: argus-app
      port: 3868
      origin_host: sim-turkcell.test
      origin_realm: sim.test
      dest_realm: argus.local
      applications: [gx, gy]          # which apps to open CER with
  - code: vodafone
    diameter:
      enabled: true
      applications: [gx]              # Gy disabled for vodafone
  - code: turk_telekom
    diameter:
      enabled: false                   # RADIUS-only
```

**Engine orchestrator change** — for SIMs whose operator has `diameter.enabled: true`, the scenario also emits:
- After RADIUS Access-Accept: **Gx CCR-I** (policy install request) → expect CCA-I with installed policy rules
- Alongside each RADIUS Accounting-Interim: **Gy CCR-U** (usage report, update quota)
- On session end: **Gx CCR-T** + **Gy CCR-T**

Diameter is synchronous with session state — a Gx failure terminates the session (no Accounting-Start, no session recorded).

**Library**: `github.com/fiorix/go-diameter/v4` — mature, supports RFC 6733 + 3GPP apps.

### Minimal AVP set

**Gx CCR-I:**
- Session-Id (RFC 6733)
- Origin-Host, Origin-Realm (operator config)
- Destination-Realm
- Auth-Application-Id: 16777238 (3GPP Gx)
- CC-Request-Type: INITIAL_REQUEST (1)
- CC-Request-Number: 0
- Subscription-Id: { Subscription-Id-Type: END_USER_IMSI, Subscription-Id-Data: <imsi> }
- Framed-IP-Address (from RADIUS Accept)
- IP-CAN-Type: 3GPP-GPRS (1)
- RAT-Type: EUTRAN (1004)

**Gy CCR-U:**
- same base AVPs
- CC-Request-Type: UPDATE_REQUEST (2)
- CC-Request-Number: N (incremented per update)
- Used-Service-Unit: { CC-Input-Octets, CC-Output-Octets, CC-Time }
- Requested-Service-Unit: { CC-Total-Octets: <next-quota-chunk> }

**CCR-T** (both apps): CC-Request-Type: TERMINATION_REQUEST (3), final Used-Service-Unit.

### Safety Envelope (additive to STORY-082)

- **Diameter opt-in per operator** — defaults off; no risk of affecting existing RADIUS-only test runs.
- **Peer state machine** — on CER failure or DWR timeout (30s), mark peer down; fall back to RADIUS-only for that operator's SIMs and log warning. Do not block Access-Request.
- **TCP connection pooling** — one TCP conn per (operator, simulator_instance); reused for all SIMs of that operator. Prevents connection exhaustion.

## Tasks

1. Add `go-diameter/v4` dependency to simulator module.
2. Implement CER/CEA exchange with Argus's Diameter server (already exists, STORY-019).
3. Implement Gx CCR-I/CCR-T and Gy CCR-U/CCR-T builders with proper AVP encoding.
4. Peer state machine with reconnect + exponential backoff.
5. Integrate with existing engine — conditional Diameter calls bracketed around RADIUS lifecycle per config flag.
6. Unit tests: CER encoding, CCR-I golden bytes, peer state transitions.
7. Integration test: simulator against live argus-app, assert Diameter metrics non-zero.
8. Metrics extension: `simulator_diameter_requests_total{operator, app, type}`, `simulator_diameter_latency_seconds`.
9. Docs: update `docs/architecture/simulator.md` with Diameter section.

## Acceptance Criteria

- **AC-1** Simulator establishes a Diameter peer session (CER → CEA → Open state) within 10s of startup for each operator with `diameter.enabled: true`.
- **AC-2** Every session belonging to a Diameter-enabled operator emits CCR-I before Accounting-Start and CCR-T after Accounting-Stop.
- **AC-3** Gy-enabled operators additionally emit CCR-U alongside each Accounting-Interim.
- **AC-4** Argus's `GET /api/v1/cdrs?protocol=diameter` returns non-empty after a 2-minute sim-up window for Diameter-enabled operators.
- **AC-5** Disabling Diameter for an operator (`diameter.enabled: false`) falls back to RADIUS-only lifecycle — no CCR packets emitted.
- **AC-6** Peer loss (kill argus-app, restart) triggers CER re-exchange within 30s of argus-app availability.
- **AC-7** Metrics endpoint exposes `simulator_diameter_*` counters and histogram.
- **AC-8** No regression in STORY-082 ACs — RADIUS-only mode still works identically.

## Risks

- **3GPP AVP dictionary gaps**: go-diameter ships a base dictionary; some VSA codes (3GPP-specific) need custom dictionary XML. **Mitigation**: ship `deploy/simulator/3gpp-avps.xml` with AVPs used by Argus's Diameter server (cross-reference `internal/aaa/diameter/` code).
- **TCP connection storms**: one conn per operator × multiple operators = small count; not a risk.
- **Synchronization between RADIUS and Diameter lifecycles**: if Gx CCR-I fails mid-flight after RADIUS accept, session is orphaned. **Mitigation**: document this and add "orphan session" count to metrics; engine logs warning.

## Dependencies

- **STORY-082** must be in production (RADIUS-only simulator working).
- Argus's Diameter server (STORY-019) — already exists.

## Out of Scope

- S6a (HSS/UDM) interface — different 3GPP app, not used by Argus
- Rf (offline charging) — Argus does offline via CDR processing, not Diameter

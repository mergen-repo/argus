# Implementation Plan: STORY-085 — Simulator Reactive Behavior (Approach B)

## Goal

Upgrade the simulator from "dumb client" (STORY-082 approach A) to "realistic SIM/modem emulator" (approach B). The simulator interprets Access-Accept attributes, reacts to Access-Reject, respects Session-Timeout, attempts reconnection on disconnect-message, and adjusts its data-rate when policy-installed bandwidth caps are present. This makes end-to-end testing of Argus's policy enforcement visible from both sides (action on Argus, reaction on simulator), not just from the audit log.

This is the user's originally-stated "B" option, deferred from STORY-082 to keep the initial delivery minimal. Landing this transforms the simulator from a traffic generator into a real conformance-testing tool: you can author a policy ("cap iot.xyz.local at 1 Mbps after 10 MB in 1 minute") and actually see simulated traffic throttle in response.

## Architecture Context

### Components Involved

**Extensions under `internal/simulator/`:**
```
radius/
  attribute_parser.go       # parse Access-Accept → typed attribute struct
  disconnect_listener.go    # UDP server on simulator side for RADIUS Disconnect-Message (CoA)
  coa_listener.go           # UDP server for RADIUS Change-of-Authorization
scenario/
  policy_aware.go           # adjust scenario parameters based on installed policy attributes
engine/
  session_state.go          # per-session state machine (Auth → Authenticated → Active → Throttled → Terminated)
  reject_handler.go         # backoff + retry on Access-Reject
  coa_applicator.go         # apply CoA changes mid-session
```

**Config additions:**
```yaml
reactive:
  enabled: true                       # master switch
  access_reject:
    backoff_base_seconds: 30
    backoff_max_seconds: 600
    max_retries_per_hour: 5           # cap retry storm
  disconnect_message:
    reconnect_after_seconds: [10, 60] # random within window
    reconnect_max_attempts: 3
  session_timeout:
    respect: true                     # respect Session-Timeout AVP
    early_termination_margin: 5       # send Accounting-Stop N seconds before timeout
  coa_listener:
    enabled: true
    listen_port: 3799                 # RFC 5176 standard port
    shared_secret: sim-coa-shared-secret-32-chars
  bandwidth_cap:
    honor_framed_bandwidth: true      # adjust bytes_per_interim when Framed-Bandwidth AVP set
    honor_mt_xdsl: false              # operator-specific; disabled by default
```

### Session State Machine

```
        ┌──────────┐
        │   Idle   │ ← scheduler picks scenario
        └─────┬────┘
              │ Access-Request
              ▼
        ┌──────────────┐
   ┌────│ Authenticating │
   │    └──────┬────────┘
   │           │ Access-Accept        │ Access-Reject
   │           ▼                      ▼
   │    ┌──────────────┐       ┌─────────────────┐
   │    │  Authenticated│       │   Backing-Off   │
   │    └──────┬────────┘       └────────┬────────┘
   │           │ Accounting-Start         │ timer expired, retries < max
   │           ▼                          ▼
   │    ┌──────────────┐           (back to Idle)
   │    │    Active    │ ◄──────┐
   │    └──────┬───┬───┘        │
   │           │   │            │ CoA: bandwidth update
   │           │   └────────────┘
   │           │ Acct-Interim periodic
   │           │
   │           │ Session-Timeout expiring
   │           │   OR Disconnect-Message received
   │           │   OR scenario duration done
   │           ▼
   │    ┌──────────────┐
   │    │ Terminating  │
   │    └──────┬────────┘
   │           │ Accounting-Stop
   │           ▼
   └────▶ (back to Idle)
```

### Reactive Behaviors

**1. Access-Reject handling** — today the simulator logs and retries immediately on next scheduler tick. Reactive mode:
- Exponential backoff: 30s → 60s → 120s → 240s → 480s → 600s cap
- Per-SIM rolling counter: ≥ 5 rejects in 1 hour → simulator marks the SIM "suspended-by-simulator", logs at warn level, checks every 15 minutes if SIM's DB state changed.
- `Reply-Message` AVP (if present) logged at debug level.

**2. Session-Timeout respect** — Access-Accept carrying Session-Timeout=N means: send Accounting-Stop at or before `now + N - early_termination_margin`. Today's simulator uses its own scenario timer only; reactive mode takes the **minimum** of (scenario duration, Session-Timeout - margin).

**3. Disconnect-Message (CoA disconnect, RFC 3576)** — Argus can push a disconnect to terminate a session (e.g., force-logout from admin UI, STORY-017). Simulator opens UDP :3799 listening socket per-operator, validates shared secret + message authenticator, and on matching Acct-Session-Id:
- Sends Accounting-Stop with `Acct-Terminate-Cause: Admin-Reset`
- Waits `reconnect_after_seconds` random window
- Initiates new Access-Request (retry count tracked; give up after `reconnect_max_attempts`)

**4. Change-of-Authorization (CoA, RFC 5176)** — Argus can push mid-session attribute updates (common for policy changes mid-session). Simulator on CoA-Request:
- Validates secret + Acct-Session-Id
- Applies known AVPs: Framed-Bandwidth (throttle byte rate), Filter-Id (log only), Session-Timeout (reset deadline)
- Responds with CoA-ACK or CoA-NAK depending on whether attributes are supported

**5. Bandwidth cap reaction** — if Access-Accept or CoA carries `Framed-Bandwidth` (kbps):
- Compute `max_bytes_per_interim = (kbps * 1000 / 8) * interim_interval_seconds`
- Scenario's native `bytes_per_interim_*` is clamped to this cap
- Visibly produces a "flat line" on dashboard when throttled

**6. Policy rollout observation** — when Argus rolls out a new policy version (STORY-025), either a CoA is pushed or next Access-Request picks up the new rules. Simulator's cumulative session log should show before/after usage patterns.

### Test Scenarios Enabled By This Story

- Policy rollout: create "cap at 1 Mbps" → rollout to 10% → observe 10% of simulator SIMs throttle on dashboard → rollback → observe recovery
- Force disconnect: UI button → simulator session ends → reconnects after 30s
- Kill-switch: enable `radius_auth` kill-switch → all new Access-Requests 403 → simulator backs off; disable → reconnect
- Bandwidth SLA testing: traffic graphs show expected curves under varying policy

### Safety Envelope (additive)

- **`reactive.enabled: false`** by default — must be explicitly enabled per environment
- **CoA listener only binds when enabled** — no open port otherwise
- **CoA shared secret is env-loaded** — not in config file — to avoid leaking in git
- **Backoff prevents retry storms** — per-SIM cap protects argus-app during an outage

## Tasks

1. Add Access-Accept attribute parser (decode Session-Timeout, Framed-Bandwidth, Framed-IP, Class, Reply-Message, Filter-Id).
2. Implement per-session state machine with context cancellation and clean transitions.
3. Access-Reject exponential backoff with rolling-window retry cap.
4. Session-Timeout respect with `min(scenario, server)` deadline logic.
5. Disconnect-Message listener: UDP :3799 per-operator, auth validation, session lookup by Acct-Session-Id.
6. CoA-Request listener: same port, CoA-ACK/NAK responses, apply attributes.
7. Bandwidth cap enforcement in scenario byte generator.
8. Metrics:
   - `simulator_reactive_access_reject_backoffs_total{operator}`
   - `simulator_reactive_disconnect_received_total{operator}`
   - `simulator_reactive_coa_received_total{operator, result}` (result ∈ ack/nak)
   - `simulator_reactive_throttled_sessions{operator}` gauge
9. Unit tests: state machine transitions, backoff curve, CoA attribute application, bandwidth clamp math.
10. Integration test: run simulator, force-disconnect via admin API, assert simulator reconnects.
11. Docs: `docs/architecture/simulator.md` Reactive Behavior section + flow diagrams.

## Acceptance Criteria

- **AC-1** With `reactive.enabled: true`, Access-Reject triggers exponential backoff as configured; simulator does not retry immediately.
- **AC-2** Session-Timeout=60 in Access-Accept results in Accounting-Stop at t ≈ 55s (early_termination_margin=5), not at the scenario's native duration.
- **AC-3** Disconnect-Message from Argus (via `POST /api/v1/sessions/{id}/disconnect`) terminates the matching simulator session within 5 seconds and triggers a reconnect within the configured window.
- **AC-4** CoA-Request with valid Framed-Bandwidth=512 kbps clamps the target session's next interim byte counter to ≤ 3.84 MB (512000/8 × 60s).
- **AC-5** Access-Reject loop > 5/hour marks SIM suspended-by-simulator in logs; resumes after the configured cooling window.
- **AC-6** `reactive.enabled: false` mode matches STORY-082 behavior exactly — no regressions.
- **AC-7** Metrics endpoint exposes all 4 reactive metrics above.
- **AC-8** CoA listener shared secret comes from env var `ARGUS_SIM_COA_SECRET`; config file values are rejected at startup.
- **AC-9** Policy rollout end-to-end test: create a bandwidth-cap policy → rollout to 50% of SIMs → dashboard shows 50% of simulator sessions throttled within 3 minutes.

## Risks

- **CoA packet spoofing**: an attacker who knows the shared secret can force-disconnect sessions. **Mitigation**: this is a dev tool; CoA listener is inside the Docker compose network not exposed externally. Documented constraint.
- **State machine deadlocks**: concurrent CoA + scenario timer could race on session state. **Mitigation**: per-session mutex; state transitions through a single goroutine per session (channel-driven); comprehensive test matrix.
- **Bandwidth cap drift**: clamping bytes-per-interim doesn't actually shape packet rate — it just reduces counters. A pedantic observer might notice this isn't "real" throttling. **Mitigation**: document as acceptable simulation; actual packet shaping is out of scope for a RADIUS-level simulator.
- **Over-aggressive retry storm during Argus outage**: the backoff ceiling prevents this per-SIM, but 16 SIMs each hitting max backoff still produces occasional requests. **Mitigation**: the 600s cap is intentional; circuit-breaker-level protection would be over-engineering for 16-SIM scale.

## Dependencies

- **STORY-082** must be in production.
- **STORY-083** and **STORY-084** are independent — reactive behavior extends RADIUS path; Diameter/5G equivalents are future follow-ups if needed.

## Out of Scope

- **Reactive behavior for Diameter**: if Argus sends Abort-Session-Request (ASR) on Gx, the simulator would need a similar listener. Deferred.
- **Reactive behavior for 5G SBA**: UE-Context-Termination equivalents. Deferred.
- **Actual IP-layer packet shaping** — simulator does not push real IP packets; it only sends RADIUS messages with counter claims.
- **Fault injection** — deliberate malformed packets, CoA spoofing tests, etc. Separate security-testing story if ever needed.

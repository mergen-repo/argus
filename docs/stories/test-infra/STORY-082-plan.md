# Implementation Plan: STORY-082 — Operator Simulator (RADIUS, A-Minimal)

## Goal

Introduce a standalone, independently-deployable Go microservice (`cmd/simulator`) that synthesizes realistic RADIUS client traffic against the running Argus instance. For every seeded SIM (STORY-080), the simulator executes a full RFC 2865/2866 session lifecycle — Access-Request → Access-Accept → Accounting-Start → Accounting-Interim × N → Accounting-Stop — using the operator's shared secret, the SIM's IMSI, and a realistic byte counter progression.

The simulator is the "SIM/BRAS emulator": it produces the traffic that an on-prem AAA system would normally see from real subscriber equipment, letting us validate Argus end-to-end (authentication, session management, accounting, live event stream, policy enforcement visible in audit logs, analytics fill-in) without hardware.

This story ships **RADIUS only** — no Diameter (STORY-083) and no 5G SBA (STORY-084). The simulator is a "dumb client" (approach A): it reads Access-Accept attributes (Framed-IP-Address, Session-Timeout) but does not adapt its behavior based on them beyond ending the session when Session-Timeout elapses. Reactive behavior (throttling, reject handling, reconnect logic) is STORY-085.

This story also closes the `live-stream verification` question folded in from the dropped STORY-081: once the simulator runs, `/ws/v1/events` emits real `session.started`, `session.updated`, `session.ended`, `sim.updated` events; the dashboard sparkline and Event Stream drawer populate automatically. An AC verifies this end-to-end.

## Architecture Context

### Components Involved

**New Go source tree:**
```
cmd/simulator/
  main.go                     # entry, flag/env parsing, orchestration, graceful shutdown
internal/simulator/
  config/
    config.go                 # env/yaml loading, Validate()
    config_test.go
  discovery/
    db.go                     # read-only PG fetch of SIMs/operators/APNs, refreshes every N minutes
    db_test.go                # uses testcontainers if available, otherwise integration tag
  scenario/
    scenario.go               # scenario picker, session-duration + byte-rate generator
    scenario_test.go
  radius/
    client.go                 # layeh.com/radius wrapper: Access-Request, Accounting-Request builders
    client_test.go            # table-driven encoding tests against known packet bytes
    attributes.go             # operator-specific attribute injection
  engine/
    engine.go                 # goroutine-per-SIM orchestrator, rate limiter, shutdown
    engine_test.go            # mock RADIUS client, assert lifecycle ordering
  metrics/
    metrics.go                # Prometheus counters/histograms on :9099
```

**New deployment artifacts:**
```
deploy/
  docker-compose.simulator.yml   # overlay file: simulator service definition
  simulator/
    Dockerfile                   # multi-stage: golang:1.25-alpine → alpine:3.19
    config.example.yaml          # sample config, checked in
```

**Makefile additions** (non-breaking — ana `make up` simulator'u başlatmaz):
- `make sim-build` — builds simulator Docker image
- `make sim-up` — starts simulator (requires argus-app already up)
- `make sim-down` — stops simulator only
- `make sim-logs` — follows simulator logs
- `make sim-ps` — shows simulator container status

**No changes** to `cmd/argus/main.go`, existing RADIUS server, or any production code path. Simulator is purely additive.

### Data Flow

```
simulator startup
    ↓
[1] load config (env + yaml)
    ↓
[2] DB discovery: SELECT sims + operators + apns (read-only user)
    ↓
[3] for each active SIM → spawn goroutine with jitter
        ↓
    pick scenario (normal_browsing 70% / heavy_user 20% / idle 10%)
        ↓
    RADIUS Access-Request
      - User-Name: <imsi>
      - User-Password: <pap-encrypted placeholder>
      - NAS-IP-Address: <simulator-assigned per operator>
      - Called-Station-Id: <APN name>
      - Calling-Station-Id: <msisdn>
      - Framed-Protocol: PPP
      - Service-Type: Framed-User
      - Message-Authenticator: computed
      - VSA: 3GPP-IMSI = <imsi>
      - Shared secret: per-operator from config
        ↓
    Argus → 200 Access-Accept { Framed-IP-Address, Session-Timeout, Class }
        ↓
    RADIUS Accounting-Start (Acct-Status-Type=1)
      - Acct-Session-Id: <uuid-v4>
      - Framed-IP-Address: <from Accept>
      - remaining attributes
        ↓
    loop every 60s (±jitter):
      RADIUS Accounting-Interim (Acct-Status-Type=3)
        - Acct-Input-Octets, Acct-Output-Octets (cumulative)
        - Acct-Session-Time (cumulative seconds)
        - Acct-Input-Packets, Acct-Output-Packets
      until session_duration reached or Session-Timeout exceeded
        ↓
    RADIUS Accounting-Stop (Acct-Status-Type=2)
      - final counters, Acct-Terminate-Cause: User-Request
        ↓
    idle (5-60s) → repeat from scenario pick
```

Concurrency: one goroutine per SIM × 16 SIMs = 16 goroutines. Global rate limiter (token bucket, default 5 req/s) throttles RADIUS traffic to protect argus-app from unintended DoS during startup bursts.

### Config Structure

`deploy/simulator/config.yaml` (bind-mounted into container):
```yaml
argus:
  radius_host: argus-app
  radius_auth_port: 1812
  radius_accounting_port: 1813
  # Direct DB discovery — read-only user, same network
  db_url: postgres://argus_sim:sim_ro_pass@argus-postgres:5432/argus?sslmode=disable
  db_refresh_interval: 5m

operators:
  - code: turkcell
    radius_secret: sim-turkcell-secret-32-chars-long
    nas_identifier: sim-turkcell
    nas_ip: 10.99.0.1
  - code: vodafone
    radius_secret: sim-vodafone-secret-32-chars-long
    nas_identifier: sim-vodafone
    nas_ip: 10.99.0.2
  - code: turk_telekom
    radius_secret: sim-tt-secret-32-chars-long
    nas_identifier: sim-tt
    nas_ip: 10.99.0.3

scenarios:
  - name: normal_browsing
    weight: 0.7
    session_duration_seconds: [600, 1800]     # 10-30 min
    interim_interval_seconds: 60
    bytes_per_interim_in: [1_000_000, 10_000_000]     # 1-10 MB per interim
    bytes_per_interim_out: [500_000, 5_000_000]
  - name: heavy_user
    weight: 0.2
    session_duration_seconds: [3600, 7200]    # 60-120 min
    interim_interval_seconds: 60
    bytes_per_interim_in: [50_000_000, 200_000_000]
    bytes_per_interim_out: [25_000_000, 100_000_000]
  - name: idle
    weight: 0.1
    session_duration_seconds: [60, 300]       # 1-5 min
    interim_interval_seconds: 60
    bytes_per_interim_in: [100_000, 1_000_000]
    bytes_per_interim_out: [50_000, 500_000]

rate:
  max_radius_requests_per_second: 5
  initial_jitter_seconds: [0, 30]             # spread initial burst across 0-30s

metrics:
  listen: :9099

log:
  level: info                                  # debug/info/warn/error
  format: console                              # console | json
```

Env overrides (standard 12-factor):
- `ARGUS_SIM_CONFIG=/etc/simulator/config.yaml`
- `ARGUS_SIM_DB_URL=<override>`
- `ARGUS_SIM_LOG_LEVEL=debug`
- `SIMULATOR_ENABLED=true` — **hard requirement**; simulator binary exits with error if not set. Guards against accidental production activation.

### Discovery Model (Direct DB Read)

Simulator uses a dedicated read-only PG user `argus_sim` with SELECT-only grants on `tenants`, `operators`, `apns`, `sims`, `operator_grants`. Seed migration will add this role:

```sql
-- Part of 005_multi_operator_seed.sql (STORY-080)
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'argus_sim') THEN
    CREATE ROLE argus_sim LOGIN PASSWORD 'sim_ro_pass';
  END IF;
END$$;
GRANT CONNECT ON DATABASE argus TO argus_sim;
GRANT USAGE ON SCHEMA public TO argus_sim;
GRANT SELECT ON tenants, operators, apns, sims, operator_grants, ip_pools TO argus_sim;
```

Discovery query (simplified):
```sql
SELECT s.id, s.imsi, s.msisdn, s.iccid, s.operator_id, s.apn_id, s.tenant_id,
       o.code AS operator_code, o.mcc, o.mnc,
       a.name AS apn_name
FROM sims s
JOIN operators o ON o.id = s.operator_id
LEFT JOIN apns a ON a.id = s.apn_id
WHERE s.state = 'active'
  AND o.code != 'mock'         -- skip the upstream mock operator
ORDER BY s.operator_id, s.imsi;
```

Simulator refreshes this every 5 minutes (configurable). New SIMs appear automatically; goroutines spawn for them on next refresh.

### Security & Safety Envelope

1. **SIMULATOR_ENABLED env flag** — binary exits non-zero at startup if absent. No build-time define; pure runtime guard.
2. **Rate limiter** — global token bucket, default 5 req/s, configurable. Prevents first-start bursts from overwhelming argus-app.
3. **Graceful shutdown** — SIGTERM triggers in-flight session close (Accounting-Stop for every active session before exit). 30-second deadline.
4. **DB read-only role** — `argus_sim` cannot INSERT/UPDATE/DELETE by schema-level grant, enforced at PG layer.
5. **RADIUS secret sharing** — operators table carries `adapter_config` with secret; simulator config duplicates these for its client-side signing. **Important**: these are NOT production secrets — they exist only in the test seed and simulator config. Document this in config.yaml header.
6. **No policy-enforcement bypass** — simulator sends legitimate RADIUS traffic; Argus processes it through the normal pipeline. Policies, kill-switches, and rate limits on the Argus side all apply.

### Metrics Surface

Prometheus endpoint at `:9099/metrics`:
- `simulator_radius_requests_total{operator, type}` — counter (type ∈ auth/acct_start/acct_interim/acct_stop)
- `simulator_radius_responses_total{operator, type, result}` — counter (result ∈ accept/reject/timeout/error)
- `simulator_radius_latency_seconds{operator, type}` — histogram
- `simulator_active_sessions{operator}` — gauge
- `simulator_scenario_starts_total{operator, scenario}` — counter

## Tasks

1. **Add new Go module tree** — `cmd/simulator/main.go` + `internal/simulator/` packages (config, discovery, scenario, radius, engine, metrics). No cross-dependency with existing `internal/*` code — clean separation.

2. **Add dependency `layeh.com/radius`** to `go.mod`. This library is mature, stdlib-only externals, covers RFC 2865/2866 + common VSAs.

3. **Config loader** — yaml + env overrides. `Validate()` checks required fields, secret lengths, port ranges.

4. **DB discovery** — simple sqlx/pgx query with 5-minute periodic refresh. Cache SIM list in memory; goroutine orchestrator diffs old vs new on refresh and spawns/cancels goroutines accordingly.

5. **Scenario engine** — weighted-random picker with deterministic RNG (seedable via config for reproducible runs). Generates session duration and interim byte counts.

6. **RADIUS client**:
   - Access-Request builder with PAP (RFC 2865 §5.2 encoding), Message-Authenticator attribute, 3GPP VSAs
   - Accounting-Request builder (Start/Interim/Stop) with Acct-Session-Id, cumulative counters
   - UDP socket with 3-second timeout + 2 retries before giving up on a packet

7. **Engine orchestrator** — goroutine-per-SIM, context cancellation, global rate limiter (`golang.org/x/time/rate`), in-flight session registry for graceful shutdown.

8. **Metrics HTTP server** — `promhttp.Handler()` on :9099. Exposed for `make sim-ps` health check curl.

9. **Dockerfile** — multi-stage, reuses `golang:1.25-alpine@sha256:...` from deploy/Dockerfile for parity. Final image < 30 MB.

10. **docker-compose.simulator.yml overlay** — references `argus-app` network (external), `argus-postgres` network; healthcheck via `wget -q http://localhost:9099/-/health`.

11. **Makefile targets** — 5 new targets, all prefixed `sim-`. Use `-f deploy/docker-compose.yml -f deploy/docker-compose.simulator.yml` for compose invocations so existing `argus-app` stays up.

12. **Unit tests**:
    - Config validation (required fields, weight sums, port ranges)
    - RADIUS Access-Request packet encoding (golden bytes for known input)
    - Accounting attribute encoding (Acct-Session-Id uniqueness, counter cumulative correctness)
    - Scenario weighted picker statistical distribution (χ² test at 10k samples)
    - Engine lifecycle (mock client → assert Start → Interim → Stop ordering)

13. **Integration test** (`//go:build integration` tag) — spins up `testcontainers` argus-app + postgres, runs simulator for 60s, asserts > 0 rows in `sessions` table and `session.ended` event received on NATS.

14. **Live stream verification AC** (rolled from STORY-081) — with simulator running, `/ws/v1/events` stream shows at least 1 event per second; `/dashboard` sparkline sparkles.

15. **Docs**:
    - New `docs/architecture/simulator.md` — architecture diagram, scenarios, config reference, troubleshooting
    - `docs/architecture/api/_index.md` — no changes (simulator exposes no Argus-facing API)
    - `docs/ARCHITECTURE.md` — add simulator to deployment topology section
    - `docs/GLOSSARY.md` — add "Simulator" entry
    - `docs/architecture/CONFIG.md` — add simulator env vars section
    - `docs/USERTEST.md` — add scenarios for simulator smoke test (make sim-up → wait 60s → check dashboard)

## Acceptance Criteria

- **AC-1** `make sim-build` produces a Docker image tagged `argus-simulator:latest`, < 30 MB final size.
- **AC-2** `make sim-up` starts the simulator container; it connects to `argus-app` and `argus-postgres` over the compose network; exits non-zero within 5s if `SIMULATOR_ENABLED` is not set.
- **AC-3** Within 30 seconds of `sim-up`, the simulator has discovered all 16 seeded SIMs and spawned one goroutine per SIM.
- **AC-4** For each SIM, the simulator sends a valid Access-Request with the correct operator's shared secret; Argus returns Access-Accept (assuming policy allows) and issues a Framed-IP.
- **AC-5** Each session emits Accounting-Start immediately after auth, Accounting-Interim at 60-second intervals (±10% jitter), and Accounting-Stop at session end.
- **AC-6** `/api/v1/sessions?state=active` returns ≥ 10 active sessions within 2 minutes of `sim-up`.
- **AC-7** `/ws/v1/events` streams at least 1 event per second with event types including `session.started`, `session.updated`, `session.ended`. (Folded from STORY-081.)
- **AC-8** Dashboard (`/`) sparkline shows non-flat activity within 2 minutes of `sim-up`.
- **AC-9** `make sim-down` stops the simulator container; all active sessions receive Accounting-Stop within the 30-second graceful-shutdown deadline.
- **AC-10** Prometheus metrics endpoint `:9099/metrics` exposes all 5 simulator metrics listed above, populated with non-zero values after sim-up.
- **AC-11** `make sim-up` followed by `make down` (stop argus-app) does NOT leave the simulator in a zombie state; simulator's retry loop backs off gracefully and logs connection errors.
- **AC-12** Running `make up` (normal production start) does NOT start the simulator. Simulator is **strictly opt-in** via `make sim-up`.
- **AC-13** All new Go code passes `go vet`, `go test -race`, and has ≥ 70% line coverage on non-integration packages.
- **AC-14** No changes to existing `cmd/argus/main.go`, RADIUS server, or any existing internal package beyond adding read-only PG role in 005 seed.

## Risks

- **RADIUS secret mismatch**: if `operators.adapter_config.radius_secret` on the Argus side differs from `operators[*].radius_secret` on simulator side, all Access-Requests fail with Authenticator validation error. **Mitigation**: STORY-080 seed and simulator config **share the same secrets** via documented constants; the seed SQL header lists them as "test-only secrets" and simulator config references them verbatim.
- **Partition not found** for a new SIM operator: simulator will receive `pgx.ErrNoRows`-family errors when Argus tries to UPDATE a SIM (e.g. for IP allocation). **Mitigation**: STORY-080 AC-5 mandates partitions exist for all 3 real operators. Simulator AC-3 depends on STORY-080 AC-1/5 passing.
- **RADIUS packet flood on startup**: all 16 goroutines hitting argus-app at t=0 could trigger rate limits or timeouts. **Mitigation**: `initial_jitter_seconds: [0, 30]` config spreads starts; global 5 req/s token bucket caps steady-state.
- **Session-Timeout ignored means sessions pile up**: simulator only ends sessions when its own scenario timer fires, NOT when Argus sends Session-Timeout in Access-Accept. In A-minimal mode this is intentional — but it can drift. **Mitigation**: engine also respects a hard `MAX_SESSION_DURATION = 4h` safety cap independent of scenario.
- **DB read-only role not created**: seed 005 creates `argus_sim` role; if a fresh DB has only 001-004 applied, simulator fails to connect. **Mitigation**: `make sim-up` fails fast with clear error message linking to 005 seed. `make sim-up` preflight also `psql`-pings with the role.
- **Dockerfile base image pinning drift**: the `golang:1.25-alpine@sha256:...` digest must match current `deploy/Dockerfile`. **Mitigation**: reuse a Makefile variable `GO_BUILDER_IMAGE` across both Dockerfiles (future refactor; for now documented constant).
- **Timing dependency with policy matcher** (cd41969): policy matcher subscribes to `session.started` and auto-assigns policies. Simulator sessions will trigger it; first Accounting-Interim might race with a `SetIPAndPolicy` write. Argus's existing locking handles this. **Mitigation**: verify via AC-6 that sessions don't end in `failed` state.

## Dependencies

- **STORY-080** must land first (3 operators, 16 SIMs, partitions, `argus_sim` DB role).

## Out of Scope

- Diameter client (Gx/Gy) — STORY-083
- 5G SBA client — STORY-084
- Reactive client behavior (interpret Access-Accept attributes, react to Session-Timeout, reject-then-reconnect logic, attribute-based throttling) — STORY-085
- Replay mode (record real traffic and replay) — future
- Multi-instance simulator for > 1000 SIM scale — future (current design is single-instance)
- Chaos/failure injection (deliberate malformed packets, flood attacks) — future security-testing track

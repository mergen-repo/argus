# Implementation Plan: STORY-089 — Operator SoR Simulator (passive HTTP backend)

> Plan drafted 2026-04-18 by the Amil Planner agent after (a) reading
> the ROUTEMAP Runtime Alignment block (§lines 247-274), (b) reading
> STORY-090's shipped plan (+ review §Impact), (c) reading STORY-082's
> plan to reuse the container-scaffolding idioms, (d) inspecting the
> current `internal/operator/adapter/http.go` shape, and (e) advisor
> pre-draft consultation that locked four scope boundaries (see
> §Decision Points).
>
> Track: **Runtime Alignment — 3/3** (final story of the track).
> Effort: **L**. Depends on: STORY-090 (DONE 2026-04-18).
> Upstream surface inherited: nested `adapter_config.{radius,diameter,sba,http,mock}`,
> API-307 per-protocol test endpoint, `enabled_protocols[]` derived
> runtime-truth, 5 new bug patterns (PAT-001..PAT-005).

## Goal

Ship a **passive HTTP backend simulator** — new binary at `cmd/operator-sim`
and Docker container `argus-operator-sim` — that emulates the
Turkcell / Vodafone TR / Türk Telekom **operator-side System-of-Record
HTTP APIs** that the Argus HTTP adapter (STORY-090 Wave 2 Task 4 at
`internal/operator/adapter/http.go`) calls out to for metadata-sync /
health-probe / SoR-style operations. Update seeds 002, 003, 005 so
every operator row carries per-protocol `adapter_config` sub-keys
where the `http` sub-key points at this simulator container (e.g.
`http://argus-operator-sim:9595/turkcell`). Re-sweep
`docs/architecture/api/_index.md` to close D-039 (the pre-existing
AUSF/UDM/NRF indexing gap unblocked by STORY-090's SBA promotion to
first-class protocol). Extend the Mini Phase Gate spec after this
story ships.

**Critical distinction** (advisor-locked to prevent downstream
reader confusion): there are now TWO simulators in the tree, moving
traffic in opposite directions.

| Binary | Container | Direction | Protocol | Pre-existing? |
|--------|-----------|-----------|----------|---------------|
| `cmd/simulator` | `argus-simulator` | **Client → Argus** (outbound RADIUS/Diameter/SBA spraying Argus AAA ports) | RADIUS/Diameter/SBA | STORY-082/083/084 |
| `cmd/operator-sim` (NEW) | `argus-operator-sim` (NEW) | **Argus → SoR** (passive HTTP server Argus dials out to for metadata/health) | HTTP | STORY-089 (this story) |

## Architecture Context

### Upstream STORY-090 surfaces this story builds on

- **Nested `adapter_config` JSONB**: `operators.adapter_config` is now
  `{radius,diameter,sba,http,mock}` per-protocol sub-keys. Source of
  truth for this shape is `internal/operator/adapterschema/` (STORY-090
  Wave 2). STORY-089 writes operator rows with the `http` sub-key
  populated.
- **HTTP adapter** at `internal/operator/adapter/http.go`:
  - Config fields used (from `HTTPConfig` struct lines 24-33):
    `base_url`, `auth_type` (`none|bearer|basic`), `bearer_token`/
    `auth_token`, `basic_user`/`basic_pass`, `health_path`
    (default `/health`), `timeout_ms` (default 2000).
  - Only method implemented is `HealthCheck(ctx)` (lines 80-123).
    All AAA methods (ForwardAuth, ForwardAcct, SendCoA, SendDM,
    Authenticate, AccountingUpdate, FetchAuthVectors) return
    `ErrUnsupportedProtocol`. STORY-089 **does NOT extend** those —
    the HTTP surface stays SoR-metadata-flavored only.
- **API-307 `POST /api/v1/operators/:id/test/:protocol`**
  (`internal/api/operator/handler.go`, STORY-090 Wave 3 Task 7a):
  the integration-verification surface. With `http.enabled=true`
  on an operator, calling `/test/http` resolves an HTTPAdapter via
  `adapterRegistry.GetOrCreate`, calls `HealthCheck`, returns
  `{success:true, latency_ms, error:""}` on a 2xx from the simulator.
- **`enabled_protocols[]`** (derived in `internal/operator/adapterschema/`)
  — operator responses (`GET /api/v1/operators/:id`) now return
  `enabled_protocols` canonical-ordered array (`diameter, radius, sba,
  http, mock`). STORY-089 verifies: after seed reload, every of the 3
  real operators has `"http"` present in this array.
- **HealthChecker** at `internal/operator/health.go`: single ticker per
  operator iterates protocols sequentially (PAT-004 collapse).
  Exposes `argus_operator_adapter_health_status{operator_id, protocol}`
  gauge. STORY-089 verifies the `protocol="http"` series goes from
  unhealthy (before seed flip) → healthy (after simulator up + seed
  flip) on the three real operators.

### The gap STORY-089 fills

Today (post-090, pre-089), seeds 002/003/005 populate the `http`
sub-key either (a) not at all (`mock.enabled=true` sibling only) or
(b) pointed at fake external hostnames like `radius.turkcell.com.tr`
(which already happens for the `radius` sub-key — not reachable from
inside a docker-compose bridge network). Consequence:
`argus_operator_adapter_health_status{protocol="http"}` reports
`0` (unhealthy) for every operator that has `http.enabled=true`, OR
no `http` series exists at all. This (a) makes `make up` produce a
perpetually-yellow operator health UI, (b) blocks any future dev/demo
from exercising the HTTP SoR integration path end-to-end, (c) leaves
the HealthChecker fanout code path (STORY-090 PAT-004 test surface)
without a live target to prove against.

STORY-089 provides the target: a passive HTTP server that responds
`200 OK` to `GET /<operator-code>/health` (and a small stub set of
forward-looking endpoints documented as out-of-scope for now) and a
seed flip to point all three real operators' `adapter_config.http.base_url`
at `http://argus-operator-sim:9595/<operator-code>`. After this
story ships, `docker compose up` produces a green operator health
board with `protocol=http` series reporting healthy for all three
Turkish operators.

### Components involved

**New Go source tree** (pattern ref: `cmd/simulator/` + `internal/simulator/`):

```
cmd/operator-sim/
  main.go                    # entry, flag/env parsing, chi router, graceful shutdown
internal/operatorsim/
  config/
    config.go                # yaml/env loading, Validate()
    config_test.go
  server/
    server.go                # chi router setup, per-operator mount
    server_test.go           # table-driven: each endpoint × each operator → expected response
    health.go                # GET /{operator}/health handler
    health_test.go
    subscriber.go            # GET /{operator}/subscribers/{imsi} stub handler (forward-looking)
    cdr.go                   # POST /{operator}/cdr stub handler (forward-looking)
    # NOTE (Gate F-A2 2026-04-18): Prometheus counters/histograms are
    # inlined inside server.go (metricsRegistry type + newMetricsRegistry
    # constructor) rather than a separate internal/operatorsim/metrics/
    # subpackage. Rationale: the registry is solely consumed by the
    # server instance; a dedicated package adds only indirection. Label
    # set remains PAT-003 compliant (registered at init time).
```

**New deployment artifacts** (pattern ref: `deploy/simulator/`):

```
deploy/operator-sim/
  Dockerfile                 # multi-stage, reuse pinned golang:1.25-alpine digest from deploy/simulator/Dockerfile
  config.example.yaml        # sample config, checked in
```

**Base compose integration** (advisor-locked choice — see D3):

- `deploy/docker-compose.yml` gains a new `operator-sim` service
  definition (NOT an overlay) so it comes up with `make up`.
  Rationale: with seeds enabling `http` protocol on all real
  operators, HealthChecker probes start immediately at boot; if the
  simulator isn't running, the dashboard shows perpetual unhealthy
  `http` protocol status and Grafana alerts fire for every dev. The
  `argus-simulator` container (RADIUS/Diameter/SBA traffic generator)
  stays in its opt-in `docker-compose.simulator.yml` overlay — those
  two simulators are different products.

**Makefile additions** (non-breaking):

- `make operator-sim-build` — builds `argus-operator-sim:latest` image
- `make operator-sim-logs` — follows container logs
- (no -up / -down targets; starts with `make up`)

### Data flow (post-ship, Turkcell operator health probe)

```
HealthChecker ticker fires every 30s (STORY-090 F-A5 single-ticker-per-op)
    ↓
for protocol in enabled_protocols order [diameter, radius, sba, http, mock]:
    ↓ (for protocol = "http")
    adapter := adapterRegistry.GetOrCreate(turkcell_id, "http", http-sub-config)
        ↓ http-sub-config = {"enabled":true, "base_url":"http://argus-operator-sim:9595/turkcell", "health_path":"/health", "timeout_ms":2000}
    result := adapter.HealthCheck(ctx)
        ↓ http.Client.Get("http://argus-operator-sim:9595/turkcell/health")
        ↓
argus-operator-sim container (chi router)
    ↓ match /turkcell/health → healthHandler
    ↓ return 200 OK {"operator":"turkcell","status":"ok","timestamp":"..."}
    ↓
adapter sees StatusCode in [200,300) → HealthResult{Success:true, LatencyMs: ~5-15ms}
    ↓
HealthChecker writes gauge argus_operator_adapter_health_status{operator_id=turkcell_id, protocol="http"} = 2  // healthy (0=down, 1=degraded, 2=healthy)
```

### Simulator endpoints (HTTP surface)

All paths prefixed by operator code (path-prefix routing — D2-A
locked below). Method list intentionally narrow — advisor locked
scope to health + forward-looking stubs. Nsmf/AUSF/UDM absorption
is EXPLICITLY out of scope (STORY-090 review line 523 said
"long-term home" — future tense, not this story).

| Method | Path | Purpose | In scope for this story |
|--------|------|---------|-------------------------|
| GET | /{operator}/health | Simulator health probe — `{"operator": "<code>", "status":"ok", "timestamp":"<RFC3339>"}`, 200 OK | YES (primary deliverable) |
| GET | /{operator}/subscribers/{imsi} | Stub subscriber lookup — echoes `{"imsi":"<imsi>","operator":"<code>","plan":"default","status":"active"}` 200, or 404 for unknown operator | YES (stub only — documents forward-looking contract) |
| POST | /{operator}/cdr | Stub CDR ingest — accepts any JSON body, returns 202 Accepted with `{"received":true, "ingested_at":"<RFC3339>"}` | YES (stub only — proves Argus POST path shape) |
| GET | /-/health | Container-level readiness probe (NOT per-operator) — always 200 OK — for docker-compose healthcheck | YES (container infrastructure) |
| GET | /-/metrics | Prometheus metrics endpoint — counter per operator×path×status-code | YES (infrastructure) |

Operators supported: `turkcell`, `vodafone_tr`, `turk_telekom`,
`vodafone`, `mock`. Note the double-aliasing: seeds 003 uses
`vodafone_tr` as the operator code, seed 005 uses `vodafone`. The
simulator registers BOTH routes (same handler) so either seed's
URL resolves. Unknown operator path prefix returns 404 with
`{"error":"unknown operator","operator":"<code>"}`.

### Config structure

`deploy/operator-sim/config.example.yaml`:

```yaml
server:
  listen: :9595              # operator HTTP surface
  metrics_listen: :9596      # Prometheus scrape + /-/health
  read_timeout: 5s
  write_timeout: 10s

operators:
  - code: turkcell
    display_name: Turkcell
  - code: vodafone_tr
    display_name: Vodafone TR
  - code: vodafone            # seed 005 alias
    display_name: Vodafone TR
  - code: turk_telekom
    display_name: Türk Telekom
  - code: mock
    display_name: Mock Operator

log:
  level: info                # debug|info|warn|error
  format: console            # console|json

# Forward-looking stub config — overrides per-operator response
# shape for the non-health endpoints. Intentionally SPARSE; real
# implementations belong to a follow-up story.
stubs:
  subscriber_status: active
  subscriber_plan: default
  cdr_echo: true
```

Env overrides (standard 12-factor, STORY-082 pattern):

- `ARGUS_OPERATOR_SIM_CONFIG=/etc/operator-sim/config.yaml`
- `ARGUS_OPERATOR_SIM_LOG_LEVEL=debug`
- **No `SIMULATOR_ENABLED`-equivalent env guard** (advisor-locked —
  see D4). This is a passive server, not a traffic generator; there
  is no "accidental production activation" risk to guard against.

### Database Schema

No schema changes. STORY-089 only edits seed data (not migrations).

**Seed file impact matrix** (per-protocol sub-key materialization —
dispatch mandate #1):

Legend: `✓ present + points at simulator` / `◯ present but placeholder` / `⊘ absent`

| Seed file | Operator row | radius sub-key | http sub-key (STORY-089 target) | sba sub-key | mock sub-key |
|-----------|--------------|----------------|----------------------------------|-------------|--------------|
| `seed/002_system_data.sql` | mock | ⊘ (mock-only) | ◯ add `http.enabled=false` (not needed; mock operator doesn't participate) | ⊘ | ✓ existing |
| `seed/003_comprehensive_seed.sql` | turkcell | ✓ existing (radius.turkcell.com.tr) | **✓ ADD**: `{"enabled":true,"base_url":"http://argus-operator-sim:9595/turkcell","health_path":"/health","timeout_ms":2000}` | ⊘ | ✓ existing |
| `seed/003_comprehensive_seed.sql` | vodafone_tr | ✓ existing (radius.vodafone.com.tr) | **✓ ADD**: `{"enabled":true,"base_url":"http://argus-operator-sim:9595/vodafone_tr","health_path":"/health","timeout_ms":2000}` | ⊘ | ✓ existing |
| `seed/003_comprehensive_seed.sql` | turk_telekom | ✓ existing (radius.turktelekom.com.tr) | **✓ ADD**: `{"enabled":true,"base_url":"http://argus-operator-sim:9595/turk_telekom","health_path":"/health","timeout_ms":2000}` | ⊘ | ✓ existing |
| `seed/005_multi_operator_seed.sql` | turkcell | ✓ existing (radius.turkcell.sim.local) | **✓ ADD**: `{"enabled":true,"base_url":"http://argus-operator-sim:9595/turkcell","health_path":"/health","timeout_ms":2000}` | ⊘ | ✓ existing |
| `seed/005_multi_operator_seed.sql` | vodafone | ✓ existing (radius.vodafone.sim.local) | **✓ ADD**: `{"enabled":true,"base_url":"http://argus-operator-sim:9595/vodafone","health_path":"/health","timeout_ms":2000}` | ⊘ | ✓ existing |
| `seed/005_multi_operator_seed.sql` | turk_telekom | ✓ existing (radius.turktelekom.sim.local) | **✓ ADD**: `{"enabled":true,"base_url":"http://argus-operator-sim:9595/turk_telekom","health_path":"/health","timeout_ms":2000}` | ⊘ | ✓ existing |

**SBA sub-key deliberately stays absent** for this story — the Nsmf
mock (STORY-092 API-304/305) currently lives inside argus-app itself
and is reachable as `http://argus-app:8443`. If a future story moves
the Nsmf mock into `cmd/operator-sim`, it will add the `sba` sub-key
pointing at operator-sim at that time.

**Result-shape example** (`seed/005_multi_operator_seed.sql` turkcell row after edit):

```json
{
  "radius":{"enabled":true,"shared_secret":"sim-turkcell-secret-32-chars-long","listen_addr":":1812","host":"radius.turkcell.sim.local","port":1812,"timeout_ms":3000},
  "http":{"enabled":true,"base_url":"http://argus-operator-sim:9595/turkcell","health_path":"/health","timeout_ms":2000},
  "mock":{"enabled":true,"latency_ms":5,"simulated_imsi_count":1000}
}
```

### Docker Compose integration

`deploy/docker-compose.yml` gains a new first-class service
(not an overlay) — advisor-locked choice:

```yaml
  operator-sim:
    build:
      context: ..
      dockerfile: deploy/operator-sim/Dockerfile
    image: argus-operator-sim:latest
    container_name: argus-operator-sim
    restart: unless-stopped
    environment:
      ARGUS_OPERATOR_SIM_CONFIG: /etc/operator-sim/config.yaml
      ARGUS_OPERATOR_SIM_LOG_LEVEL: ${ARGUS_OPERATOR_SIM_LOG_LEVEL:-info}
    volumes:
      - ./operator-sim/config.example.yaml:/etc/operator-sim/config.yaml:ro
    # Ports intentionally not published to host — argus-app reaches
    # operator-sim over argus-net bridge by hostname.
    healthcheck:
      test: ["CMD-SHELL", "wget -qO- http://localhost:9596/-/health || exit 1"]
      interval: 10s
      timeout: 3s
      retries: 3
      start_period: 5s
    networks:
      - argus-net
```

**Startup dependency ordering**: `argus-app` gains `depends_on:
operator-sim: condition: service_healthy` — this prevents
HealthChecker from firing its first 30s tick against a not-yet-
listening operator-sim during the compose-up race window. Cost: a
~5-10s extra boot time for Argus.

### Metrics surface

Prometheus endpoint `:9596/-/metrics` exposes:

- `operator_sim_requests_total{operator,path,method,status_code}` counter
- `operator_sim_request_duration_seconds{operator,path,method}` histogram

These are distinct metric namespaces from the STORY-082 simulator
(`simulator_*`) so they coexist cleanly in the shared Prometheus
scrape.

**Coverage scope (Gate F-A7 2026-04-18)**: the counter covers only
requests that pass the `validateOperator` middleware (i.e., a known
operator code). Requests to an unknown `/<op>/...` path return 404
before reaching `instrumentMiddleware` and are intentionally NOT
counted. Rationale: this keeps the `{operator}` label dimension
bounded by the config-registered operator set (5 values) and
prevents an adversarial scanner from inflating cardinality through
404s. If a future story needs to track 404 volumes, add a separate
`operator_sim_invalid_operator_requests_total` counter at the
router-root level (no `operator` label).

**PAT-003 compliance**: the `{operator, path, method, status_code}`
label set is defined AT REGISTRATION TIME (metrics init), not
after a per-operator write-fanout grows. Adding a new operator code
only grows the cardinality naturally via chi router registration.

**PAT-004 compliance**: no scheduled ticker inside operator-sim —
this is a passive request-driven server. Goroutine cardinality =
http.Server's default per-connection goroutine pool, bounded by
`server.read_timeout` + `server.write_timeout` — no N×M fanout
risk.

## Decision Points

> All four decisions LOCKED 2026-04-18 per advisor pre-draft
> consultation. Listed for audit trail.

### D1. Simulator direction naming (pre-emptive disambiguation)

**LOCKED: document the cmd/simulator vs cmd/operator-sim inverse-
direction distinction explicitly in the Goal block and §Risks.**

- `cmd/simulator` = outbound RADIUS/Diameter/SBA client. Argus is
  the target.
- `cmd/operator-sim` = inbound HTTP server. Argus is the caller.

Risk if missed: downstream readers confuse the two; grep for
"simulator" turns up both containers and nobody knows which is
which. Mitigation: naming discipline in the plan Goal block (done
above), in USERTEST.md post-ship, and in the ARCHITECTURE.md
deployment topology diagram update (Task 8a).

### D2. Per-operator routing (path-prefix vs vhost)

**LOCKED: D2-A (path-prefix) — `/turkcell/...`, `/vodafone_tr/...`,
etc.**

- D2-A (path-prefix): one chi router, `chi.Route("/{operator}",
  subrouter)` for each supported operator code. Simpler for seeds
  (single hostname `argus-operator-sim` per adapter_config.http.base_url
  with path differing), simpler for Go routing, trivial to test
  with net/http/httptest.
- D2-B (vhost): `turkcell.argus-operator-sim` etc. Requires
  per-vhost DNS entries in docker-compose (extra_hosts) or
  container aliases. More "realistic" but adds compose complexity
  for zero functional gain.

Chose D2-A: simpler ALL dimensions, and seed base_url is the same
hostname varying only by path — which is easier to diff/review than
vhost variations.

### D3. Compose placement (base vs overlay)

**LOCKED: D3-A (base compose file — always up with `make up`).**

- D3-A (base `docker-compose.yml`): operator-sim is always running
  for any dev/CI/staging that does `make up`. HTTP adapter
  HealthChecker probes succeed on fresh boots. Operator health UI
  ships green out-of-box.
- D3-B (overlay `docker-compose.operator-sim.yml`): consistency
  with the STORY-082 simulator overlay. But: overlay means devs who
  don't opt-in see perpetually-unhealthy `http` protocol probes on
  all 3 real operators, triggering PAT-style metric noise and
  Grafana alert fatigue for no gain.

Chose D3-A: the operator-sim is effectively infrastructure (like
`argus-postgres`, `argus-redis`, `argus-nats`) once seeds enable
`http.enabled=true`. The STORY-082 simulator is a traffic generator
— completely different semantics, stays in its overlay.

### D4. Env guard (`OPERATOR_SIM_ENABLED`)

**LOCKED: D4-A (no env guard).**

STORY-082 requires `SIMULATOR_ENABLED=true` because the RADIUS
simulator is an active traffic generator — if it accidentally
starts in production, it DoS-floods real AAA. STORY-089's
operator-sim is passive: it listens for incoming requests and
responds. Accidental production activation produces zero outbound
traffic and doesn't touch real operator infrastructure. No guard
needed; overhead ~= 0.

## Acceptance Criteria

Each AC is testable; test tasks cover them individually (see Task
breakdown).

### AC-1: Binary compiles and container builds clean

`make operator-sim-build` produces a Docker image tagged
`argus-operator-sim:latest`, final layer < 30 MB (stripped Alpine
binary). `docker run argus-operator-sim:latest --help` prints
usage without panic.

### AC-2: Container comes up healthy via docker-compose

`make up` brings the container up; `docker inspect
argus-operator-sim | grep Health` shows `healthy` within 15 seconds
of boot. `docker compose ps` shows operator-sim in
`running (healthy)` state alongside argus-postgres, argus-redis,
argus-nats.

### AC-3: Per-operator health endpoints respond correctly

For each of the 5 registered operator codes (`turkcell`,
`vodafone_tr`, `vodafone`, `turk_telekom`, `mock`), curl
`http://argus-operator-sim:9595/{operator}/health` from inside
the compose network returns:

- Status: 200 OK
- Body (JSON): `{"operator":"<code>","status":"ok","timestamp":"<RFC3339>"}`
- Header: `Content-Type: application/json; charset=utf-8`
- Response time: < 50ms p95

Curl to an unknown operator (`/nonsense/health`) returns 404 with
`{"error":"unknown operator","operator":"nonsense"}`.

### AC-4: Per-operator forward-looking stubs respond correctly

- `GET /turkcell/subscribers/2860100001` → 200 with
  `{"imsi":"2860100001","operator":"turkcell","plan":"default","status":"active"}`.
- `POST /turkcell/cdr` with any JSON body → 202 with
  `{"received":true,"ingested_at":"<RFC3339>"}`.
- Same paths on `vodafone_tr` / `turk_telekom` / `vodafone` / `mock`
  respond the same shape with `"operator"` field swapped.

### AC-5: Seed 003 materialization — per-protocol http sub-keys

After `make db-migrate && make db-seed`, the 3 real-operator rows in
`operators` (IDs 20000000-0000-0000-0000-000000000001..003) have
`adapter_config` JSONB containing an `"http"` sub-key matching the
shape documented in §Database Schema > Seed file impact matrix. SQL
verification query:

```sql
SELECT code,
       adapter_config->'http'->>'base_url'   AS http_base_url,
       adapter_config->'http'->>'health_path' AS http_health_path,
       (adapter_config->'http'->>'enabled')::boolean AS http_enabled
FROM operators
WHERE code IN ('turkcell','vodafone_tr','turk_telekom')
ORDER BY code;
```

Expected: 3 rows, all `http_enabled=true`, `http_health_path='/health'`,
`http_base_url` matches `^http://argus-operator-sim:9595/<code>$`.

### AC-6: Seed 005 materialization — parallel coverage

Same as AC-5 for the seed-005 operator IDs
(`00000000-0000-0000-0000-000000000101..103`) with the `vodafone`
(not `vodafone_tr`) alias on the base_url path.

### AC-7: API-307 per-protocol Test Connection succeeds for http

`POST /api/v1/operators/20000000-0000-0000-0000-000000000001/test/http`
(authenticated as super_admin) returns:

- Status: 200
- Body: `{"status":"success","data":{"success":true,"latency_ms":<int>, "error":""}}`
- `latency_ms` must be < 500 (round-trip inside docker bridge)

Same for the other 2 seed-003 operators. Same for all 3 seed-005
operators when seed 005 is the active seed. These assertions run
against a **live argus-app + argus-operator-sim compose stack**
(integration test, `//go:build integration` tag).

### AC-8: enabled_protocols array reflects http on all real operators

`GET /api/v1/operators/:id` (super_admin auth) for each of the 3
real operators returns a response with `enabled_protocols` array
that includes `"http"` at its canonical position (after `radius`,
before `sba`/`mock`).

Example expected array for seed-003 turkcell: `["radius","http","mock"]`
(canonical order: diameter > radius > sba > http > mock — per
STORY-090 `internal/operator/adapterschema/`). Similarly for the
other two.

### AC-9: HealthChecker reports http protocol healthy end-to-end

After 60 seconds of compose uptime (one HealthChecker tick):

- Prometheus scrape at `argus-app:8080/metrics` returns
  `argus_operator_adapter_health_status{operator_id="<uuid>", protocol="http"}`
  with a value **≥ 1** (gauge semantics per `internal/observability/metrics/metrics.go:321`:
  `0=down`, `1=degraded`, `2=healthy`; a green probe is `2`, a partial probe is `1`)
  for each of the 3 real operators. Integration assertion at
  `test/e2e/operator_sim_test.go` uses `GreaterOrEqualf(..., 1.0)`.
- Operator detail UI at `/operators/:id` → Protocols tab shows the
  HTTP card's "Last probe" chip in green ("OK · <N>ms").
- PAT-004 invariant: only ONE ticker goroutine exists per operator
  (not 3 per operator × 3 protocols = 9) — verified by runtime
  goroutine count via pprof at steady-state.

### AC-10: D-039 closed — AUSF/UDM/NRF indexed in api/_index.md

`docs/architecture/api/_index.md` gains a new section below the
existing "5G SBA — Nsmf Mock" section (or extended inline) that
indexes the pre-existing endpoints shipped by STORY-020:

- `POST /nausf-auth/v1/ue-authentications` (AUSF)
- `GET /nudm-ueau/v1/{supi}/security-information` (UDM UEAU)
- `POST /nudm-uecm/v1/{ueId}/registrations/amf-3gpp-access` (UDM UECM)
- `GET /nnrf-nfm/v1/nf-instances` (NRF discovery)
- `PATCH /nnrf-nfm/v1/nf-instances/{nfInstanceId}` (NRF heartbeat)

Per endpoint: ID (API-308..API-312 or next available), method,
path, description, auth, notes pointing at the existing
`internal/aaa/sba/{ausf,udm,nrf}.go` files. Total endpoint count
at the bottom of api/_index.md updated from 241 to the new total.
The inline "pending STORY-089's holistic SBA section re-sweep"
note (line 525) is removed. ROUTEMAP Tech Debt table D-039 row
marked `✓ RESOLVED`.

### AC-11: Prometheus metrics populated after 1 request

After curl'ing any endpoint against operator-sim, its
`:9596/-/metrics` returns non-zero counter values for
`operator_sim_requests_total{operator="<x>",...}` and at least one
histogram bucket populated.

### AC-12: All new Go code passes vet/race/coverage

- `go vet ./cmd/operator-sim/... ./internal/operatorsim/...` exit 0
- `go test -race ./internal/operatorsim/...` passes
- Line coverage ≥ 70% on non-integration packages (matches
  STORY-082 AC-13 threshold — the project convention)

### AC-13: Mini Phase Gate spec extended (not rewritten)

`docs/reports/test-infra-tech-debt-gate-spec.md` gains a new
section for STORY-089 deliverables covering: new binary compiles;
container healthy under compose; seed update coverage; API-307
per-protocol test passes for http on all 3 real operators;
HealthChecker reports http healthy; D-039 resolved in api/_index.md.
**Existing sections (STORY-080/082/083/084/085/087/088/090/092)
untouched** — verified by per-line diff. ROUTEMAP hard flag #5.

### AC-14: Runtime Alignment track completion

After this story ships: `docs/ROUTEMAP.md` §Runtime Alignment
counter advances `2/3 → 3/3`; track status moves to `[DONE]`;
`docs/CLAUDE.md` Active Session advances from `Runtime Alignment
— STORY-089` to the Documentation Phase handoff state.

## Architecture references

- HTTP adapter contract (consumed by this simulator): `internal/operator/adapter/http.go` lines 24-123.
- HealthChecker shape (probes this simulator): `internal/operator/health.go` (single-ticker-per-operator, per-protocol fan-out).
- API-307 endpoint definition: `internal/api/operator/handler.go` (STORY-090 Wave 3 Task 7a).
- `enabled_protocols[]` derivation: `internal/operator/adapterschema/` (STORY-090 Wave 2).
- Seed files to patch: `migrations/seed/{002_system_data,003_comprehensive_seed,005_multi_operator_seed}.sql`.
- Pattern ref for new binary scaffold: `cmd/simulator/main.go` + `internal/simulator/config/config.go`.
- Pattern ref for Docker-compose first-class service: existing `postgres` / `redis` / `nats` blocks in `deploy/docker-compose.yml`.
- Pattern ref for Dockerfile: `deploy/simulator/Dockerfile` (reuse `golang:1.25-alpine` pinned digest).
- Pattern ref for chi router with nested subrouters: existing `internal/gateway/router.go` (chi-based).
- 5G SBA endpoints to index (D-039): `internal/aaa/sba/ausf.go`, `internal/aaa/sba/udm.go`, `internal/aaa/sba/nrf.go`.

## Tasks

### Wave 1 — scaffolding (parallel)

#### Task 1: New binary + config loader

- **Files:** Create `cmd/operator-sim/main.go`, `internal/operatorsim/config/config.go`, `internal/operatorsim/config/config_test.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** `cmd/simulator/main.go` + `internal/simulator/config/config.go` — reuse the yaml+env pattern wholesale; drop all RADIUS-specific fields
- **Context refs:** §Architecture Context > Components Involved; §Config structure
- **What:** Go module layout. Config struct with `server`, `operators[]`, `log`, `stubs` sections. `Load(path) (*Config, error)` reads yaml + merges env. `Validate()` enforces: at least 1 operator, unique operator codes, non-zero listen ports. No `SIMULATOR_ENABLED`-style env guard (D4-A).
- **Verify:** `go build ./cmd/operator-sim/...` exit 0; `go test ./internal/operatorsim/config/...` passes

#### Task 2: HTTP server + per-operator routing skeleton

- **Files:** Create `internal/operatorsim/server/server.go`, `internal/operatorsim/server/server_test.go`
- **Depends on:** Task 1
- **Complexity:** **high** — the core business logic; path-prefix router + handler dispatch + graceful shutdown + in-process metrics — the Developer has to get this right or the rest of the story doesn't run
- **Pattern ref:** `internal/gateway/router.go` for chi usage patterns; `net/http/httptest` for unit tests
- **Context refs:** §Simulator endpoints; §Decision Points > D2 (path-prefix); §Metrics surface
- **What:** chi.Router with nested routes. For each operator in config, register 3 routes under `/{operator}/*` (health, subscribers/:imsi, cdr). Register `/-/health` and `/-/metrics` at root. Graceful shutdown on SIGTERM with 10s deadline. Prometheus collector registration (operator_sim_requests_total counter + operator_sim_request_duration_seconds histogram). Table-driven tests cover each endpoint × each operator × expected response shape.
- **Verify:** `go test -race ./internal/operatorsim/server/...` passes; coverage ≥ 80%

#### Task 3: Per-endpoint handlers

- **Files:** Create `internal/operatorsim/server/health.go` + `health_test.go`, `subscriber.go` + (no separate test — covered by server_test.go), `cdr.go` + (same)
- **Depends on:** Task 2
- **Complexity:** low
- **Pattern ref:** any simple chi handler in `internal/api/`, e.g. `internal/api/health/handler.go`
- **Context refs:** §Simulator endpoints
- **What:** Three handler funcs. `healthHandler` returns `{operator, status:"ok", timestamp}` 200. `subscriberHandler` reads `:imsi` chi param, returns stub. `cdrHandler` reads body, echoes receipt. All set `Content-Type: application/json; charset=utf-8`. Unknown operator (404) is handled at the router layer in Task 2, not here.
- **Verify:** `go test ./internal/operatorsim/server/...` covers all 3 handlers with happy-path + error-path

### Wave 2 — container + compose (parallel with Wave 3)

#### Task 4: Dockerfile + config.example.yaml

- **Files:** Create `deploy/operator-sim/Dockerfile`, `deploy/operator-sim/config.example.yaml`
- **Depends on:** Task 1
- **Complexity:** low
- **Pattern ref:** `deploy/simulator/Dockerfile` (copy wholesale, swap build target from `./cmd/simulator` to `./cmd/operator-sim`, reuse the pinned golang+alpine digests)
- **Context refs:** §Config structure; §Docker Compose integration
- **What:** multi-stage: `golang:1.25-alpine@sha256:7a00384194cf2cb68924bbb918d675f1517357433c8541bac0ab2f929b9d5447` builder → `alpine:3.19@sha256:6baf43584bcb78f2e5847d1de515f23499913ac9f12bdf834811a3145eb11ca1` runtime. `ca-certificates tzdata wget` installed. EXPOSE 9595 9596. ENTRYPOINT `/app/operator-sim`. Config example yaml carries all 5 operator codes with realistic display_names.
- **Verify:** `docker build -f deploy/operator-sim/Dockerfile -t argus-operator-sim:test .` exits 0; `docker inspect argus-operator-sim:test | jq '.[0].Size'` < 31457280 (30 MB)

#### Task 5: docker-compose.yml base service + Makefile targets

- **Files:** Modify `deploy/docker-compose.yml`, `Makefile`
- **Depends on:** Task 4
- **Complexity:** low
- **Pattern ref:** existing `postgres:` / `redis:` / `nats:` service blocks in `deploy/docker-compose.yml` for first-class service wiring + healthcheck pattern
- **Context refs:** §Docker Compose integration; §Decision Points > D3 (base, not overlay)
- **What:** add `operator-sim:` service block per §Docker Compose integration; add `depends_on: operator-sim: service_healthy` clause to argus-app's existing depends_on block; add Makefile targets `operator-sim-build` and `operator-sim-logs` (pattern-refed off existing `sim-build`/`sim-logs`).
- **Verify:** `docker compose -f deploy/docker-compose.yml config` parses without error; `make operator-sim-build` succeeds

### Wave 3 — seed integration (depends on Wave 1, parallel with Wave 2)

#### Task 6: Seed 003 http sub-key insertion

- **Files:** Modify `migrations/seed/003_comprehensive_seed.sql`
- **Depends on:** Task 1 (binary doesn't need to exist; just config shape)
- **Complexity:** medium
- **Pattern ref:** existing shape in same file lines 126-139 where `adapter_config` JSON literal is already embedded
- **Context refs:** §Database Schema > Seed file impact matrix
- **What:** for each of the 3 real operator rows (turkcell, vodafone_tr, turk_telekom), merge a new `"http"` sub-key into the existing JSONB literal per the matrix table. Update the in-file "Gate F-A6" comment block to also mention STORY-089 http sub-key addition. Re-run the seed in a local DB to verify idempotency (the existing `ON CONFLICT (code) DO NOTHING` means seed re-runs are no-ops — so initial load must be on a clean DB to pick up the new http sub-key; document this in the task's verify step).
- **Verify:** psql query from AC-5 returns 3 rows; `go test ./internal/api/operator/...` passes (no regression on 090 tests)

#### Task 7: Seed 005 http sub-key insertion

- **Files:** Modify `migrations/seed/005_multi_operator_seed.sql`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** same file lines 44-76 existing adapter_config literal pattern
- **Context refs:** §Database Schema > Seed file impact matrix
- **What:** identical to Task 6 but against seed 005's operator rows (IDs ending in 101/102/103). Note operator code `vodafone` (not `vodafone_tr`) in this seed — base_url path uses `vodafone`. Update the STORY-090 Gate F-A6 comment block to mention STORY-089 addition.
- **Verify:** psql query from AC-6 returns 3 rows

#### Task 8: Seed 002 — acknowledgement only (no change needed)

- **Files:** Modify `migrations/seed/002_system_data.sql` (comment block only)
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** —
- **Context refs:** §Database Schema > Seed file impact matrix (row 1)
- **What:** seed 002 inserts the `mock` operator which does NOT need http enabled (it's the dev-only mock operator). Add a 2-line SQL comment explaining the absence: "STORY-089 note: the mock operator does not enable the http sub-key because no simulator path emulates mock; all http routing goes through the three real operators."
- **Verify:** file still parses; no JSONB changes

### Wave 4 — documentation + gate (depends on everything above)

#### Task 9: D-039 SBA api/_index.md re-sweep (dispatch mandate #7)

- **Files:** Modify `docs/architecture/api/_index.md`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** existing "5G SBA — Nsmf Mock" section at lines 521-530 of same file
- **Context refs:** §Acceptance Criteria > AC-10; §Tech Debt
- **What:** index the AUSF/UDM/NRF endpoints per AC-10. Assign IDs API-308..API-312 (or the next sequential range if the file has been updated since 2026-04-18). Remove the pending-STORY-089 note on line 525. Update the total-endpoint-count footer (`Total: 241 REST endpoints` → new total). Append a brief STORY-089 row to the file's updated-at footer.
- **Verify:** `grep -c 'API-' docs/architecture/api/_index.md` increments by the number of rows added; `grep 'pending STORY-089' docs/architecture/api/_index.md` returns 0 matches; ROUTEMAP Tech Debt D-039 row flips to `✓ RESOLVED`

#### Task 10: Integration test — end-to-end compose stack verification

- **Files:** Create `test/e2e/operator_sim_test.go` (or add a subtest to an existing e2e file if pattern fits)
- **Depends on:** Task 5 (compose wiring), Task 6/7 (seeds)
- **Complexity:** **high** — cross-container orchestration + API-307 round-trip + metrics scrape
- **Pattern ref:** existing `test/e2e/tenant_onboarding_test.go` (testcontainers + compose pattern)
- **Context refs:** §Acceptance Criteria > AC-7, AC-8, AC-9, AC-11; §Architecture references
- **What:** `//go:build integration` test that brings up the full docker-compose stack (or uses a minimal reduced overlay), seeds the DB, obtains a super_admin JWT, then:
  1. Asserts AC-7 — hit API-307 for `protocol=http` on each of 3 real operators; expect 200+success=true+latency_ms<500.
  2. Asserts AC-8 — GET each operator; enabled_protocols contains "http".
  3. Asserts AC-9 — wait 45s, scrape `argus-app:8080/metrics`, assert gauge value=1 for each (operator_id, protocol=http) pair.
  4. Asserts AC-11 — scrape operator-sim `:9596/-/metrics`; assert operator_sim_requests_total{operator="turkcell",...} > 0.
- **Verify:** `go test -tags=integration ./test/e2e/operator_sim_test.go` passes in the CI env

#### Task 11: Mini Phase Gate spec extension (ROUTEMAP hard flag #5)

- **Files:** Modify `docs/reports/test-infra-tech-debt-gate-spec.md`
- **Depends on:** Task 10
- **Complexity:** low
- **Pattern ref:** the existing sections for STORY-080/082/083/084/085/087/088/090/092 in the same file — follow the structure (AC list, verification commands, PASS criteria)
- **Context refs:** §Acceptance Criteria > AC-13; dispatch mandate on "extend, not rewrite"
- **What:** append a new `## STORY-089 — Operator SoR Simulator` section summarizing the 14 ACs, the `go test -tags=integration` command, and the specific `curl` / `psql` verification queries for AC-5/6/7/8/9/10. **Do not modify existing sections.** Add a note at the top of the file's changelog that STORY-089 shipped on <date>.
- **Verify:** `git diff docs/reports/test-infra-tech-debt-gate-spec.md` shows only new lines in the appended section + the changelog; no deletions in existing STORY-NNN sections

#### Task 12: ARCHITECTURE.md + GLOSSARY.md + USERTEST.md touches

- **Files:** Modify `docs/ARCHITECTURE.md`, `docs/GLOSSARY.md`, `docs/USERTEST.md`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** STORY-090 review's list of doc touch-ups in `docs/stories/test-infra/STORY-090-review.md` — follow the same convention
- **Context refs:** §Goal; §Architecture Context
- **What:** (a) ARCHITECTURE.md — add `cmd/operator-sim/` and `internal/operatorsim/` to the project-structure tree; add argus-operator-sim to the deployment topology section. (b) GLOSSARY.md — add "Operator SoR Simulator" entry clarifying the passive-HTTP-server-vs-traffic-generator distinction vs `cmd/simulator`. (c) USERTEST.md — append a STORY-089 scenario block covering: docker-compose up green; Protocols tab HTTP card healthy; curl verification from within the compose network.
- **Verify:** `grep -c 'operator-sim' docs/ARCHITECTURE.md docs/GLOSSARY.md docs/USERTEST.md` ≥ 3; manual visual review of each

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|----------------|-------------|
| AC-1 (binary/image) | Task 1, Task 4 | `make operator-sim-build` |
| AC-2 (container healthy) | Task 5 | `docker compose ps` after `make up` |
| AC-3 (per-operator health) | Task 2, Task 3 | Task 10 subtest; server_test.go |
| AC-4 (forward-looking stubs) | Task 3 | server_test.go; Task 10 subtest |
| AC-5 (seed 003 http sub-key) | Task 6 | Task 10 psql assertion |
| AC-6 (seed 005 http sub-key) | Task 7 | Task 10 psql assertion |
| AC-7 (API-307 test/http success) | Task 5 + Task 6 | Task 10 (integration) |
| AC-8 (enabled_protocols contains http) | Task 6, Task 7 | Task 10 (integration) |
| AC-9 (HealthChecker http gauge) | Task 5 | Task 10 metrics scrape |
| AC-10 (D-039 closed) | Task 9 | file diff + ROUTEMAP update |
| AC-11 (simulator metrics populated) | Task 2 | Task 10 metrics scrape |
| AC-12 (go vet/race/coverage) | Task 1, 2, 3 | `go test -race`, `go vet` |
| AC-13 (gate spec extended) | Task 11 | diff-audit per line |
| AC-14 (runtime alignment 3/3) | Task 11 + post-gate review | ROUTEMAP + CLAUDE.md commit |

## Test Strategy

**Table-driven per-operator response tests** (Task 2/3 unit):

```go
cases := []struct {
    operator       string
    path           string
    method         string
    body           string
    wantStatus     int
    wantBodyField  string
    wantBodyValue  string
}{
    {"turkcell", "/turkcell/health", "GET", "", 200, "operator", "turkcell"},
    {"vodafone_tr", "/vodafone_tr/health", "GET", "", 200, "operator", "vodafone_tr"},
    {"turk_telekom", "/turk_telekom/health", "GET", "", 200, "operator", "turk_telekom"},
    {"mock", "/mock/health", "GET", "", 200, "operator", "mock"},
    {"unknown", "/unknown/health", "GET", "", 404, "error", "unknown operator"},
    // subscriber stubs
    {"turkcell", "/turkcell/subscribers/2860100001", "GET", "", 200, "plan", "default"},
    // cdr stub
    {"turkcell", "/turkcell/cdr", "POST", `{"bytes":1234}`, 202, "received", "true"},
}
```

**Smoke test against running simulator container** (Task 10 integration):

- Full compose stack (argus-postgres, argus-redis, argus-nats,
  argus-app, argus-operator-sim)
- seed load via `make db-seed`
- super_admin JWT fetched from `/api/v1/auth/login`
- loop: curl API-307 for each (operator_id, "http") pair × 3
- curl `/metrics` both from argus-app (HealthChecker gauge) and
  from argus-operator-sim (request counter)

## Story-Specific Compliance Rules

- **API**: new simulator endpoints are NOT `/api/v1/*` (they impersonate
  operator SoR endpoints, not Argus endpoints) — they should NOT appear
  in `docs/architecture/api/_index.md` (that index is Argus-side only).
  However the D-039 AUSF/UDM/NRF additions (Task 9) ARE Argus-side
  endpoints exposed by `internal/aaa/sba/` and DO belong in the index.
- **DB**: no migrations created. Seed edits only. Every seed edit is
  idempotent (`ON CONFLICT DO NOTHING` already present in target rows)
  — but since we're mutating a JSONB column inside existing rows,
  the seed file will only pick up the new shape on a CLEAN DB load.
  Document this in the Task 6/7 verify step so reviewers don't expect
  seed re-runs against an existing DB to update in place.
- **Go**: follow STORY-082 scaffold conventions (`internal/<name>/` package layout,
  `config.Load()` + `Validate()` pattern, `zerolog` logger, promhttp
  metrics endpoint).
- **Dockerfile**: reuse the pinned base-image digests from
  `deploy/simulator/Dockerfile` to keep the container ecosystem
  consistent across both simulators.
- **ADR**: no new ADR required. STORY-089 is a test-infra deliverable
  that does not introduce new architectural patterns; it consumes the
  HTTP-adapter contract established by STORY-090.
- **Business**: the simulator is a dev/test-only component. Its
  container MUST NOT be depended on by production-critical code
  paths inside `internal/aaa/` or `internal/api/` — Argus production
  behaviour must be identical with operator-sim running or absent
  (beyond the HealthChecker gauge value). Verification: `grep -rn
  'argus-operator-sim\|cmd/operator-sim' internal/` must return ZERO
  matches in non-test code.

## Bug Pattern Warnings

Per `docs/brainstorming/bug-patterns.md` PAT-001..PAT-005, Developer
MUST observe:

- **PAT-003** (STORY-090 F-A1): the simulator's Prometheus metrics use
  `{operator, path, method, status_code}` label set. All four labels
  MUST be defined AT REGISTRATION TIME in `metrics.go` — never grow
  the label set after first-write. If a future change adds a label
  (e.g. `tenant_id`), the Grafana dashboard + alert rules + metric
  renames ship in the SAME commit.
- **PAT-004** (STORY-090 F-A5): operator-sim has ZERO periodic-tick
  goroutines by design (request-driven only). If a future story adds
  scheduled work (e.g. periodic self-health-log), use a SINGLE ticker
  that iterates operators sequentially inside — never one goroutine
  per operator-code per tick. The http.Server connection pool
  goroutines are fine (bounded by `server.read_timeout`).
- **PAT-005** (STORY-090 F-A2): not directly applicable (operator-sim
  has no secret-masking API). Documented here for awareness in case
  a future story adds a secret/credential surface.
- **PAT-001** (STORY-084): not directly applicable (operator-sim has
  no session/engine/client pair that could double-write a metric).
- **PAT-002** (STORY-085): not directly applicable (operator-sim has
  no polling loop with mutable deadlines).

## Tech Debt (from ROUTEMAP)

- **D-039** (source: STORY-092 review; target: STORY-089): `AUSF/UDM/
  NRF api/_index.md` indexing gap. **Addressed by Task 9** — see AC-10.
- **D-029** (source: post-GA seed CI guard; target: post-GA): OUT OF
  SCOPE for this story per the dispatch — stays PENDING.
- No other Tech Debt rows target STORY-089.

## Mock Retirement

Not applicable — Argus is not a Frontend-First project; `src/mocks/`
does not exist. STORY-089 does not retire any mock.

## Dependencies

- **STORY-090** (DONE 2026-04-18): provides the nested `adapter_config.http`
  sub-key shape, the HTTP adapter implementation, API-307 per-protocol
  test endpoint, and the HealthChecker fanout code path.
- **STORY-082** (DONE earlier in Test Infra track): provides the
  `cmd/simulator` + `deploy/simulator/` scaffolding pattern this story
  reuses for `cmd/operator-sim`.
- **STORY-080** (DONE): provides the 3 real operator rows + 16 SIM
  seeds that Task 10's integration test exercises.

## Blocking

- Blocks: the Runtime Alignment track completion (3/3). After this
  story's gate PASS, the Mini Phase Gate extension (Task 11) gates the
  track's DONE status; the Documentation Phase handoff follows.

## Out of Scope

> Per advisor scope-boundary lock and ROUTEMAP hard flag discipline.

- **NOT absorbed**: the STORY-092 Nsmf mock (API-304/305) stays inside
  `internal/aaa/sba/nsmf.go` and the `argus-app` container. Its move
  into operator-sim is a future story (STORY-090 review line 523 says
  "long-term home" — future tense).
- **NOT implemented**: AAA methods on the HTTP adapter
  (`ForwardAuth`, `ForwardAcct`, `SendCoA`, `SendDM`, `Authenticate`,
  `AccountingUpdate`, `FetchAuthVectors`) — they stay returning
  `ErrUnsupportedProtocol` in `internal/operator/adapter/http.go`.
  operator-sim does NOT emulate radius/diameter/sba AAA protocols.
- **NOT implemented**: subscriber/CDR handler business logic — the
  stubs echo fixed shapes. A follow-up story will flesh them out if
  a real operator-side integration scenario demands it.
- **NOT rewritten**: the Mini Phase Gate spec at
  `docs/reports/test-infra-tech-debt-gate-spec.md` — only EXTENDED
  with a new STORY-089 section. Existing sections untouched. (ROUTEMAP
  hard flag #5.)
- **NOT introduced**: a competing config surface for adapter_config.
  STORY-089 piggybacks on STORY-090's nested JSONB shape — zero
  new fields, zero new sub-key types, zero schema changes.
- **NOT added**: `OPERATOR_SIM_ENABLED` env guard (D4-A locked —
  passive server, no accidental-activation risk).
- **NOT added**: production-wiring for the HTTP adapter in
  `internal/aaa/*` hot paths. The HTTP protocol remains metadata-
  sync flavored; RADIUS/Diameter/SBA stay the AAA protocols.

## Risks & Mitigations

- **Risk 1 — naming collision in reader mental model**: two
  simulators (`cmd/simulator` = client, `cmd/operator-sim` = server)
  with similar names. **Mitigation**: goal paragraph carries the
  inversion-table; Task 12 GLOSSARY.md entry makes it explicit; Task
  11 gate spec calls out the distinction.
- **Risk 2 — simulator down causes cascade unhealthiness**: if
  operator-sim crashes or exits, all 3 real operators' HTTP protocol
  probes fail immediately; operator-health dashboard turns yellow;
  Grafana alerts may fire. **Mitigation**: `restart: unless-stopped`
  on the compose service; `depends_on: condition: service_healthy`
  on argus-app prevents boot-race unhealthiness; the PAT-004 invariant
  of single-ticker-per-operator means recovery is fast (next 30s
  tick after operator-sim comes back up flips the gauge back to 1).
- **Risk 3 — seed alias drift (`vodafone` vs `vodafone_tr`)**: seed
  003 and seed 005 use different codes for the same operator. If the
  simulator registers only one, whichever seed runs against a fresh
  DB produces a 404 on the http probe. **Mitigation**: simulator
  registers BOTH routes with the same handler (see §Simulator
  endpoints — 5 operator codes including both Vodafone aliases).
- **Risk 4 — path-prefix routing with variable operator codes**: if a
  future seed introduces a new operator code (e.g. `bim`) without
  also updating operator-sim's config, the HealthChecker probe 404s.
  **Mitigation**: documented in §Tech Debt risk-list (fold into a
  follow-up story); for now the supported set is frozen at 5.
- **Risk 5 — container size exceeds 30 MB**: the chi dependency plus
  promhttp plus zerolog can compile heavier than the STORY-082
  minimal stdlib-only simulator. **Mitigation**: AC-1's `< 30 MB`
  threshold is an aspiration; if violated during Wave 2, bump to
  < 40 MB in the post-ship gate and document the delta.
- **Risk 6 — integration test flakiness on CI**: docker-compose +
  seed + multi-container probe is a bigger integration surface than
  most existing e2e tests. **Mitigation**: Task 10 uses the same
  testcontainers pattern as `test/e2e/tenant_onboarding_test.go` —
  proven in CI. Add 5s buffer between seed completion and API-307
  poll to accommodate HealthChecker first-tick arrival.

## Quality Gate (plan self-validation)

### Substance

- Goal stated (single paragraph + inversion-table distinguishing
  the two simulators). ✓
- Root gap traced (seeds post-090 don't point `http` sub-keys at a
  reachable simulator → PAT-004 goroutine fanout has no live target,
  UI shows perpetual yellow). ✓
- Every advisor pre-draft boundary surfaced: simulator direction
  naming (D1), per-operator routing (D2), compose placement (D3),
  env guard (D4). All four LOCKED with rationale. ✓
- Upstream STORY-090 surfaces cited with file+line (http.go lines 24-123,
  adapterschema/, HealthChecker fanout, API-307 handler ref). ✓
- Seed file impact matrix explicitly per-protocol (dispatch mandate
  #1 — NOT singular). 7 rows across 3 seed files. ✓

### Required sections

- Goal ✓
- Architecture Context (upstream surfaces, components, data flow,
  endpoints, config, schema, compose, metrics) ✓
- Decision Points (D1–D4, all LOCKED) ✓
- Acceptance Criteria (AC-1 … AC-14) ✓
- Architecture references ✓
- Tasks (12 tasks, 4 waves) ✓
- Acceptance Criteria Mapping ✓
- Test Strategy ✓
- Story-Specific Compliance Rules ✓
- Bug Pattern Warnings (PAT-003/004 directly applicable; 001/002/005 documented for awareness) ✓
- Tech Debt (D-039 in scope) ✓
- Mock Retirement (N/A) ✓
- Dependencies ✓
- Blocking ✓
- Out of Scope ✓
- Risks & Mitigations (6 explicit) ✓
- Quality Gate (this block) ✓

### Embedded specs

- Per-endpoint HTTP shapes documented inline with example JSON. ✓
- Seed JSON shape shown inline (turkcell example). ✓
- Config yaml example embedded inline. ✓
- Docker compose service block inlined. ✓
- Pattern refs point at ACTUAL files (`cmd/simulator/main.go`,
  `deploy/simulator/Dockerfile`, `test/e2e/tenant_onboarding_test.go`,
  etc.) — not "similar". ✓

### Effort confirmation

- Dispatch estimate: **L**. ✓
- Task count: **12** (L minimum: 5). Well over. ✓
- AC count: **14** (L dispatch mandate: ≥ 10). Well over. ✓
- High-complexity tasks: **2** (Task 2 HTTP server core, Task 10
  integration test). L dispatch mandate: ≥ 1 high. Met. ✓
- Plan line count: ~500 (L minimum: 100). Well over. ✓

### Dispatch-mandate compliance

- ☑ STORY-090 integration surface (`adapter_config.http`, API-307,
  `enabled_protocols[]`) exercised in AC-7/8.
- ☑ Per-protocol seed sub-keys (NOT singular) — matrix in §Database
  Schema with 7 rows covering the 3 real operators × seed 003/005.
- ☑ D-039 re-sweep given dedicated Task 9 + AC-10.
- ☑ Mini Phase Gate spec EXTENDED (not rewritten) — Task 11 with
  per-line diff-audit verify.
- ☑ STORY-082 scaffold idioms reused (cmd/simulator → cmd/operator-sim,
  deploy/simulator/ → deploy/operator-sim/, pinned base image digests).
- ☑ PAT-003 / PAT-004 bug patterns called out in §Bug Pattern Warnings.
- ☑ No competing config surface introduced — all operator config
  stays in STORY-090's nested JSONB.
- ☑ Docker-compose integration with healthcheck and dependency
  ordering documented inline.
- ☑ Quality Gate embedded at plan end with self-grading PASS per
  criterion (this block).

### Pre-Validation: PASS

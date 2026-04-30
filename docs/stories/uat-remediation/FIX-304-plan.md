# FIX-304 Plan: 5G SBA :8443 Listener Not Bound Inside Container

> S effort | 4 tasks | Pre-release bugfix | UAT F-4 (CRITICAL)

## Symptom + Reproduction

UAT-019 Step 1 (`docs/reports/uat-acceptance-2026-04-30.md` finding F-4):

> Inside `argus-app` container, `wget --no-check-certificate https://localhost:8443/-/health` → "Connection refused" on both `::1` and `127.0.0.1`. Docker maps :8443 but app is NOT listening on it. Other listeners (8080, 8081, 3868, 1812 UDP) all work. UAT-019 (5G SBA AUSF/UDM) entirely impossible.

Reproduced 2026-04-30:

```
$ docker exec argus-app sh -c 'netstat -tlnp 2>/dev/null'
tcp  :::3868  LISTEN  1/rosetta   # Diameter
tcp  :::8081  LISTEN  1/rosetta   # WebSocket
tcp  :::8080  LISTEN  1/rosetta   # HTTP API
tcp  :::6060  LISTEN  1/rosetta   # pprof
udp  :::1812  -                   # RADIUS auth
udp  :::1813  -                   # RADIUS acct
# 8443 ABSENT — no SBA listener bound.
```

## Investigation Findings

1. **SBA code path exists and is wired.** `cmd/argus/main.go:1259-1302` instantiates `aaasba.NewServer(...)` and calls `sbaServer.Start()` which in turn calls `net.Listen("tcp", ":8443")` (`internal/aaa/sba/server.go:148-186`). Implementation is complete (AUSF, UDM, NRF, NSMF, EAP-proxy, /health, TLS+mTLS optional).
2. **The entire SBA block is gated behind `cfg.SBAEnabled`.** `main.go:1260`: `if cfg.SBAEnabled { ... sbaServer.Start() ... }`. When false, **no SBA goroutine, no listener, no log line**.
3. **Default value is `false`.** `internal/config/config.go:65`:
   ```go
   SBAEnabled bool `envconfig:"SBA_ENABLED" default:"false"`
   ```
   Confirmed by `.env.example:SBA_ENABLED=false` and `.env` (the file actually loaded into the container) **does not set `SBA_ENABLED` at all** — falls back to default `false`.
4. **docker-compose publishes the port but never enables the listener.** `deploy/docker-compose.yml` argus service maps `"8443:8443"` and pulls env from `../.env`; no `SBA_ENABLED=true` override. Result: kernel forwards :8443 into the container, but nothing inside the container is bound, so the connection is refused.
5. **No log evidence of SBA failure.** `docker logs argus-app | grep -iE 'sba|8443|tls|listener'` returns zero lines — confirms the init block was skipped, NOT that it crashed. (If it had tried and failed, `main.go:1296` is `log.Fatal` — the container would have exited.)
6. **TLS is optional.** `internal/aaa/sba/server.go:163-185` — when `TLSCertPath` and `TLSKeyPath` are both empty (the current default), the server starts plain HTTP/2 ("development mode" log line). No cert needed for dev. UAT used `wget --no-check-certificate https://...` but the dev listener serves plain HTTP — UAT will need to use `http://` (or we enable TLS). See AC-2 below.

## Root Cause

Hypothesis **(a) — Disabled by config**. SBA_ENABLED defaults to `false`; docker-compose `.env` does not override it; main.go's SBA init block is therefore skipped at boot. Docker-compose still publishes :8443, producing the misleading "port mapped but app not listening" symptom. This is **not a code bug** in the SBA implementation — it is a configuration/defaults defect. The SBA server has been fully implemented (STORY-092 Wave 3 D3-B per main.go comments) and is regression-tested (`internal/aaa/sba/server_test.go`, 16 KB), but never actually runs in the dev/UAT environment because the toggle is off.

## Fix Approach

**Decision: enable SBA by default in dev/UAT** (NOT just a doc/SKIP). The platform's value proposition includes 5G SBA AAA; the listener must be live in the dev environment so UAT-019 and integration tests can exercise it. Production deployments that disable SBA (e.g., 4G-only operators) can still set `SBA_ENABLED=false` explicitly.

Three coordinated changes:

### Task 1: Flip the default in code and example env
**Files:** `internal/config/config.go:65`, `.env.example`
- Change `default:"false"` → `default:"true"` on the `SBAEnabled` field.
- Update `.env.example` `SBA_ENABLED=false` → `SBA_ENABLED=true` and add an inline comment: `# set to false to disable 5G SBA listener (4G-only deployments)`.
- Rationale: aligns the documented dev default with the as-shipped intent of the SBA story (STORY-092). Operators who actively want SBA off must set it explicitly.

### Task 2: Set SBA_ENABLED=true in docker-compose env (defense in depth)
**Files:** `deploy/docker-compose.yml`
- In the `argus:` service `environment:` block (or via a top-level `SBA_ENABLED: "true"` line), explicitly set `SBA_ENABLED=true`.
- Belt-and-suspenders: even if `.env` is regenerated from a stale template, the compose file guarantees the listener is up in the standard local stack.

### Task 3: Reproduction test — assert listener bound at boot
**Files:** `cmd/argus/main_sba_listener_test.go` (new) OR extend `internal/aaa/sba/server_test.go`
- Integration-style test: with `SBA_ENABLED=true` + ephemeral port, call `sbaServer.Start()`, then `net.Dial("tcp", addr)` must succeed within 2 s.
- Negative variant: with `SBA_ENABLED=false`, main.go path leaves `sbaServer == nil`, no port bound. Encode this as a gating-logic unit test on the config branch in main.go (extract gating into a small helper if needed to keep the test pure) OR document as a startup-log assertion.
- The existing `internal/aaa/sba/server_test.go` already covers Start/Stop semantics; add a "starts on configured port" test if not present.

### Task 4: Docs — UAT step + CONFIG reference
**Files:** `docs/architecture/CONFIG.md`, `docs/UAT.md` UAT-019
- `CONFIG.md`: document `SBA_ENABLED` default flip + that the dev listener is plain HTTP/2 unless `TLS_CERT_PATH`+`TLS_KEY_PATH` are set.
- `UAT.md` UAT-019: change the reproduction command from `wget --no-check-certificate https://localhost:8443/-/health` to `wget http://localhost:8443/health` (note: SBA's healthz is at `/health` per `server.go:130`, not `/-/health`). Document that mTLS+TLS variants are opt-in.

## Acceptance Criteria

- **AC-1:** After `make down && make up`, `docker exec argus-app sh -c 'ss -tlnp'` shows port `8443` in the LISTEN list (alongside 8080/8081/3868).
- **AC-2:** `docker exec argus-app wget -qO- http://localhost:8443/health` returns HTTP 200 with body `{"status":"healthy","service":"argus-sba"}`. (Note: dev listener is plain HTTP/2 — `https://` only works when `TLS_CERT_PATH`+`TLS_KEY_PATH` are provided. UAT command in UAT.md updated accordingly under Task 4.)
- **AC-3:** UAT-019 5G SBA AUSF/UDM happy path runs end-to-end:
  - `POST http://localhost:8443/nausf-auth/v1/ue-authentications` returns 201 with auth-vector
  - `POST .../5g-aka-confirmation` returns 200
  - DB row created in `radius_sessions` with `protocol_type='5g_sba'` and `auth_method='5g_aka'` (note: F-4 also flagged miscategorization — out of scope for FIX-304, route to a follow-up if still wrong after the listener is up)
- **AC-4:** Default boot of the standard local stack (`make up` with no env overrides) brings SBA up. `docker logs argus-app 2>&1 | grep -i sba` shows `"SBA server started" port=8443`.
- **AC-5:** Reproduction Go test (Task 3) is green in CI: asserts `sbaServer.Start()` binds the configured port and refuses Start when called twice; negative-config branch leaves listener absent.

## Files Changed

- `internal/config/config.go` (1 line: default flip on SBAEnabled)
- `.env.example` (1 line + comment)
- `deploy/docker-compose.yml` (1 env line under argus service)
- `internal/aaa/sba/server_test.go` OR new `cmd/argus/main_sba_listener_test.go` (~30 LoC test)
- `docs/architecture/CONFIG.md` (paragraph)
- `docs/UAT.md` UAT-019 (corrected reproduction command + healthz path)

## Risks

- **TLS cert handling.** The dev listener runs **plain HTTP/2** because `TLS_CERT_PATH`/`TLS_KEY_PATH` are empty by default. Any UAT step or external client expecting HTTPS will fail. Mitigation: AC-2 + Task 4 explicitly document this; F-4's `wget --no-check-certificate https://` was incorrect for the dev profile and is corrected in UAT.md. Production deployments should set the TLS paths and (optionally) `SBA_ENABLE_MTLS=true`; that path is already implemented and unchanged.
- **NRF registration warnings on boot.** `main.go:1299` calls `sbaServer.NRFRegistration().Register()`; when `SBA_NRF_URL` is empty (the dev default), this returns an error that is logged at WARN. Acceptable — does not prevent the listener from binding. `internal/aaa/sba/nrf.go` no-ops cleanly when URL is empty per existing tests.
- **Port conflict on host.** If a developer already has something on host :8443, `make up` will fail. Mitigation: this is a pre-existing concern across all mapped ports; no change.
- **Production default change.** Operators who run a custom env file inheriting the old default `false` are unaffected (their env override wins). Operators who relied on the old code default will now boot SBA on. Document in the Wave-1 release notes / CHANGELOG and call it out in the Phase Gate handoff.
- **CI test flakiness.** Net-listener tests can race with port reuse; use `:0` ephemeral port and read the bound port back from `httpServer.Addr` or `Listener.Addr()`.

## Regression Risk

**Low.** The SBA server, handlers, NRF, NSMF, AUSF, UDM, EAP-proxy code paths are already implemented and tested (`internal/aaa/sba/*_test.go`, ~50 KB of tests). This story only **enables** them by flipping a config default; it does not modify behaviour. The blast radius for environments that explicitly set `SBA_ENABLED=false` is zero. The blast radius for environments that did not set it is "SBA now binds :8443 and registers with NRF if configured" — exactly what the story shipped to do.

## Effort

**S** — single config default flip + docker-compose env line + test + 2 doc tweaks.

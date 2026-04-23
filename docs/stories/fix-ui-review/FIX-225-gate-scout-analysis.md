# Gate Scout — Analysis (FIX-225)

Story: **FIX-225 — Docker Restart Policy + Infra Stability**
Scope: compose hardening + DEPLOYMENT.md + decisions.md (no app code).

## Checks Performed

| # | Check | Result |
|---|-------|--------|
| A-1 | `docker compose -f deploy/docker-compose.yml config --quiet` exit 0 | PASS (exit=0) |
| A-2 | NATS image `nats:2.10-alpine` ships busybox `wget` | VERIFIED — alpine variant ships busybox wget; pattern matches existing `operator-sim` wget probe at line 150 |
| A-3 | NATS `http_port: 8222` enabled in `infra/nats/nats.conf` | VERIFIED — L15 `http_port: 8222` with comment `/healthz, /varz, /jsz used by probes` |
| A-4 | `start_period: 10s` sufficient for NATS boot | ACCEPTABLE — NATS typically ready <3s; 10s buffer; `retries: 5 × interval 5s` gives 25s of retry runway after start-period, total 35s before hard fail |
| A-5 | All 4 argus `depends_on` entries use `service_healthy` | PASS — merged compose shows postgres, redis, nats, operator-sim all `condition: service_healthy` |
| A-6 | `grep service_started deploy/docker-compose.yml` | PASS — 0 matches |
| A-7 | DEPLOYMENT.md restart matrix matches actual compose file | PASS — 7 services × policy/probe/start_period/depends_on verified row-by-row against `deploy/docker-compose.yml` |
| A-8 | DEPLOYMENT.md healthcheck matrix matches compose | PASS — nats `wget /healthz :8222`, argus `wget :8080/health/ready`, nginx `wget localhost/health`, postgres `pg_isready`, redis `redis-cli ping`, operator-sim `wget :9596/-/health`, pgbouncer `pg_isready -p 6432` — all match |
| A-9 | Recovery runbook commands correct | PASS — `docker logs argus-<service>`, `docker inspect … --format='{{.State.Health.Status}}'`, `docker compose ps`, `make down && make up`, `curl http://localhost:8084/health` — all reflect current compose/Makefile state |
| A-10 | CLAUDE.md cross-link path correct, file exists | PASS — L113 `docs/architecture/DEPLOYMENT.md — Container restart policy + recovery runbook`; file present |
| A-11 | decisions.md DEV-312/313/314 appended with rationale | PASS — L566/567/568, all dated 2026-04-23, all reference FIX-225, status ACCEPTED |
| A-12 | No TODO/FIXME in DEPLOYMENT.md | PASS — 0 matches |
| A-13 | NATS comment in compose no longer claims "distroless" | PASS — L125 now reads `nats:2.10-alpine includes busybox wget; /healthz exposed on :8222 via http_port in nats.conf` |
| A-14 | ADR/decisions trail for AC-5 deferral present | PASS — DEV-314 text explicitly cites Compose v3 limitation + external-watchdog requirement; DEPLOYMENT.md Limitations #1 references DEV-314 back |

## Findings

<SCOUT-ANALYSIS-FINDINGS>
No findings. All 14 analysis checks PASS.
</SCOUT-ANALYSIS-FINDINGS>

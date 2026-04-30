# Deployment — Container Restart Policy & Recovery Runbook

> Single source of truth for Argus container restart, health-check, and recovery behavior.
> **Keep this doc in sync with `deploy/docker-compose.yml` whenever the compose file changes.**

## Purpose

This document describes how Argus containers auto-recover from crashes and host reboots, which health probes are in use, the dependency start order, and the manual runbook for common failure scenarios.

---

## Service Restart & Health Matrix

| Service | Restart policy | Health probe | Start period | Depends on (condition) |
|---------|---------------|--------------|-------------|------------------------|
| `argus-postgres` | `unless-stopped` | `pg_isready` | 60s | — |
| `argus-redis` | `unless-stopped` | `redis-cli ping` | — | — |
| `argus-nats` | `unless-stopped` | `wget /healthz :8222` | 10s | — |
| `argus-operator-sim` | `unless-stopped` | `wget :9596/-/health` | 5s | — |
| `argus-pgbouncer` | `unless-stopped` | `pg_isready -p 6432` | — | postgres: service_healthy |
| `argus-app` | `unless-stopped` | `wget :8080/health/ready` | 60s | postgres, redis, nats, operator-sim: **service_healthy** |
| `argus-nginx` | `unless-stopped` | `wget http://localhost/health` | 10s | argus: service_healthy |

> **Nginx healthcheck note:** The nginx probe hits `/health` which nginx proxies to argus's `/api/health`. A "nginx unhealthy" state therefore reflects argus unavailability, not an nginx-side failure. This is expected because argus is already an ordered dependency (`service_healthy`) for nginx.

---

## Restart Policy Rationale

- **`unless-stopped` everywhere** — restarts on non-zero exit AND auto-starts on Docker daemon start (host reboot). The container does NOT restart if manually stopped with `docker stop`.
- **`on-failure:N` rejected** — rate-limits restarts but does NOT auto-start after host reboot. Losing reboot-auto-up behavior is unacceptable for production deployments.
- **`always` rejected** — identical to `unless-stopped` except it also restarts manually-stopped containers on daemon start, which fights `docker stop` during maintenance.

---

## Dependency Ordering

All hard dependencies of `argus-app` use `condition: service_healthy`. This ensures argus never races its dependencies on cold boot or after a dependency restart.

```
postgres ─┐
redis    ─┤──service_healthy──▶ argus-app ──service_healthy──▶ nginx
nats     ─┤
operator-sim ─┘
```

- `condition: service_started` (process spawned) is insufficient — it provides no guarantee the port is ready.
- `condition: service_healthy` waits for the dependency's health probe to pass before the dependent container starts.

---

## Recovery Runbook

### Host reboot
Docker daemon auto-starts on boot; all containers come back via `unless-stopped`.
Wait ~60s for `argus-app` to pass `/health/ready` (postgres + NATS start-periods).
No manual action is required under normal conditions.

### Single container crash
Docker restarts the container automatically. If it keeps failing:
```bash
docker logs argus-<service> --tail 100
docker inspect argus-<service> --format='{{.State.Health.Status}}'
```

### Argus app restart loop
```bash
docker inspect argus-app --format='{{.RestartCount}}'
```
If RestartCount > 5 within a short window, stop the container, investigate logs, then restart:
```bash
docker stop argus-app
docker logs argus-app --tail 200
docker start argus-app
```
> Automatic crash-loop detection is not yet implemented — see **Limitations** below (DEV-314).

### Dependency unhealthy
```bash
docker compose ps
```
If postgres, redis, or nats shows `unhealthy`, argus will not start (`service_healthy` gate). Fix the dependency first; argus will auto-start once the gate passes.

### Full stack restart
```bash
make down && make up
```
Allow ~90s for all services to reach `healthy`. Verify with:
```bash
docker compose ps
docker inspect argus-nats --format='{{.State.Health.Status}}'
docker inspect argus-app  --format='{{.State.Health.Status}}'
```

### End-to-end health check
```bash
curl -s http://localhost:8084/health
```
A 200 OK confirms the nginx → argus chain is healthy.

---

## Limitations

1. **No automatic crash-loop detection (AC-5 deferred — DEV-314).** Docker Compose v3 provides only `restart: on-failure:N`, which loses host-reboot auto-start when N is exhausted. Proper crash-loop detection requires an external watchdog (Prometheus Alertmanager or a `docker events` sidecar). Tracked as a future ops story.

2. **`unless-stopped` loops forever on persistent failure.** If a container exits non-zero repeatedly (e.g., misconfiguration), Docker will restart it indefinitely without alerting. Use `docker inspect` + external monitoring to catch this.

3. **No external alerting hooks in Compose.** Crash events are not forwarded to Telegram, email, or webhook by the Compose layer. Argus's internal notification service (STORY-038) covers application-level alerts, not container-level restarts.

---

## References

- `deploy/docker-compose.yml` — service definitions, healthcheck blocks, depends_on conditions
- `docs/architecture/CONFIG.md` — environment variable reference
- `docs/brainstorming/decisions.md` — DEV-312 (service_healthy rule), DEV-313 (compose-level nginx healthcheck), DEV-314 (AC-5 crash-loop deferral)

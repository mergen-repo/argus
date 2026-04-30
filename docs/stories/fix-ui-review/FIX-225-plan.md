# Implementation Plan: FIX-225 — Docker Restart Policy + Infra Stability

## Goal
Lock in auto-recovery for all Argus containers (host reboot + crash) and document the restart/recovery contract. Discovery shows most of the policy surface is already in place; this story closes the last two gaps (missing deployment doc, NATS healthcheck posture) and explicitly defers AC-5 crash-loop detection with decision evidence.

## Scope Discipline
- **In scope:** `deploy/docker-compose.yml` (minor hardening), new `docs/architecture/DEPLOYMENT.md`, one ADR-lite entry in `docs/brainstorming/decisions.md` (DEV-312..DEV-314).
- **Out of scope (deferred):**
  - **AC-5 crash-loop detection** — Docker Compose v3 has no native rate-limited restart. `restart: on-failure:3` would lose host-reboot behavior. Proper detection requires an external watchdog (Prometheus + Alertmanager, or a `docker events` sidecar). Deferring to a future ops story. Decision logged as DEV-314.
  - Nginx Dockerfile `HEALTHCHECK` clause (story file mentions it, but compose-level healthcheck already covers the requirement without a rebuild — simpler + no image drift). Decision logged as DEV-313.
- **No backend code changes.**

## Findings Addressed
| Finding | Current state | Action |
|---------|---------------|--------|
| F-02 (`argus-nginx` in Created, not running) | Compose already has `restart: unless-stopped` + healthcheck + `depends_on: argus: service_healthy`. | Verify compose syntax; document in DEPLOYMENT.md. |
| F-07 (`argus-app` recent restart, instability concern) | Compose already has `restart: unless-stopped` + healthcheck (wget `/health/ready`). NATS dependency is `service_started` (distroless image, no shell for healthcheck). | Replace NATS `service_started` with a TCP-port check healthcheck using a sidecar-less approach OR document the trade-off. |

## Discovery Summary

### Current `deploy/docker-compose.yml` posture (2026-04-23)
| Service | restart | healthcheck | depends_on condition |
|---------|---------|-------------|----------------------|
| nginx | `unless-stopped` | wget `http://localhost/health` | argus: service_healthy |
| argus | `unless-stopped` | wget `:8080/health/ready` | postgres: healthy, redis: healthy, nats: **started**, operator-sim: healthy |
| postgres | `unless-stopped` | `pg_isready` | — |
| redis | `unless-stopped` | `redis-cli ping` | — |
| nats | `unless-stopped` | **none** (distroless) | — |
| operator-sim | `unless-stopped` | wget `:9596/-/health` | — |
| pgbouncer | `unless-stopped` | `pg_isready -p 6432` | postgres: healthy |

**Gap 1:** NATS has no healthcheck → argus cannot wait for `service_healthy` on NATS; uses `service_started` which only guarantees process spawn, not port readiness.
**Gap 2:** `docs/architecture/DEPLOYMENT.md` does not exist.
**Gap 3:** `docs/brainstorming/decisions.md` has no record of the AC-5 deferral rationale (required for audit trail per AUTOPILOT quality policy).

### NATS healthcheck feasibility
- NATS official image `nats:2.10-alpine` — **alpine**, not distroless. Comment in compose says "nats:latest is distroless" but we pin to the alpine variant. `wget` IS available in `nats:2.10-alpine`. Verify at planning time via `docker run --rm nats:2.10-alpine wget --version` (optional).
- NATS exposes `/healthz` on `:8222` (monitoring port). **Verified during discovery:** `infra/nats/nats.conf` contains `http_port: 8222` with comment "/healthz, /varz, /jsz used by probes" — monitor endpoint is enabled and no NATS config change is required for this story.
- Add: `healthcheck: test: ["CMD", "wget", "--quiet", "--tries=1", "--spider", "http://localhost:8222/healthz"]`.
- If wget is missing on alpine image (unlikely), fall back to `nats:2.10-alpine`'s busybox `nc`: `test: ["CMD-SHELL", "echo | nc -w 1 localhost 4222 || exit 1"]`. Either works.
- With healthcheck in place, change argus `depends_on: nats: condition: service_healthy`.

### Nginx healthcheck coupling note
The current nginx compose healthcheck probes `/health` which proxies to argus's `/api/health`. This means nginx healthcheck transitively depends on argus. Acceptable because argus is already an ordered dependency (`depends_on: argus: service_healthy`). Documented for operators so they don't misread "nginx unhealthy" as nginx-side failure.

## Architecture Context

### Components Involved
- **deploy/docker-compose.yml** — Compose v3 service graph, healthchecks, restart policy, dependency conditions.
- **docs/architecture/DEPLOYMENT.md** — NEW — canonical deployment + recovery doc.
- **docs/brainstorming/decisions.md** — append DEV-312 (healthcheck-based dependency ordering), DEV-313 (compose-level nginx healthcheck vs Dockerfile), DEV-314 (AC-5 deferral).

### Restart Policy Semantics (embedded — used by DEPLOYMENT.md)
- `restart: unless-stopped` — restart on non-zero exit AND start on Docker daemon start (host reboot). **Does NOT** rate-limit restarts. Will loop indefinitely on persistent failure.
- `restart: on-failure:N` — rate-limited but does NOT auto-start after host reboot. **Rejected for Argus** because ops needs reboot-auto-up behavior.
- `restart: always` — identical to `unless-stopped` except a manually-stopped container is still restarted on daemon start. Rejected because it fights `docker stop`.
- **Decision:** `unless-stopped` for ALL services. Crash-loop detection handled externally (DEV-314).

### Dependency Ordering Semantics
- `condition: service_started` — satisfied when container starts (process visible). Does NOT wait for port/ready.
- `condition: service_healthy` — waits for healthcheck to report `healthy`. Requires the dependency to define `healthcheck`.
- **Rule:** every hard dependency of `argus` MUST be `service_healthy` (or we lose startup sequencing guarantees).

## Prerequisites
- [x] FIX-206 (data integrity — unrelated track, no blocker here)
- [x] No blocking stories; FIX-225 is wave-6 isolated infra work.

## Tasks

### Task 1 — NATS healthcheck + argus dependency upgrade
- **Files:** Modify `deploy/docker-compose.yml`
- **Depends on:** — (first task)
- **Complexity:** low
- **Pattern ref:** Follow existing `redis` healthcheck block in same file (lines 105-109) for structure. For the NATS test command itself, mirror the operator-sim wget pattern at lines 145 (`wget -qO- http://localhost:9596/-/health || exit 1`).
- **Context refs:** "Discovery Summary > NATS healthcheck feasibility", "Architecture Context > Dependency Ordering Semantics"
- **What:**
  1. Add `healthcheck:` block to the `nats` service (around line 124, after `command:`, before `networks:`). Use:
     ```yaml
     healthcheck:
       test: ["CMD-SHELL", "wget -qO- http://localhost:8222/healthz || exit 1"]
       interval: 5s
       timeout: 3s
       retries: 5
       start_period: 10s
     ```
  2. Remove/replace the comment "nats:latest is distroless..." — rewrite it to note that `nats:2.10-alpine` has wget and we probe `/healthz`.
  3. In the `argus.depends_on.nats` block (around line 50-51), change `condition: service_started` → `condition: service_healthy`.
- **Verify:**
  - `docker compose -f deploy/docker-compose.yml config --quiet` → exit 0
  - Search: `grep -n "service_started" deploy/docker-compose.yml` → zero results (or only for non-argus deps if any exist — there are none).
  - If Docker available: `make down && make up` → all services reach `healthy`. `docker inspect argus-nats --format='{{.State.Health.Status}}'` → `healthy`.

### Task 2 — Create `docs/architecture/DEPLOYMENT.md` + cross-link in CLAUDE.md
- **Files:** Create `docs/architecture/DEPLOYMENT.md`, Modify `CLAUDE.md`
- **Depends on:** Task 1 (so doc reflects final compose state)
- **Complexity:** low
- **Pattern ref:** Follow the structure of `docs/architecture/MIDDLEWARE.md` (same `docs/architecture/` sibling, short reference doc format: Purpose → Table → Runbook → References).
- **Context refs:** "Discovery Summary > Current compose posture", "Architecture Context > Restart Policy Semantics", "Architecture Context > Dependency Ordering Semantics", "Nginx healthcheck coupling note"
- **What:** Write a short (≤200 line) reference doc with these sections:
  1. **Purpose** — one paragraph: this doc is the single source of truth for Argus container restart + recovery behavior.
  2. **Service Restart Matrix** — table (Service · Restart policy · Healthcheck · Depends on · Start-period). Copy from the Discovery Summary table, adding `start_period` column.
  3. **Restart Policy Rationale** — 3 bullets explaining why `unless-stopped` everywhere (copy from "Restart Policy Semantics" above).
  4. **Dependency Ordering** — explain `service_healthy` rule, with a text-only graph: `postgres+redis+nats+operator-sim → argus → nginx`.
  5. **Recovery Runbook** — 4 short playbooks:
     - Host reboot: "Docker daemon auto-starts; all containers come back via `unless-stopped`. Wait ~60s for argus to pass `/health/ready`. No action needed."
     - Single container crash: "Docker restarts automatically. If still failing, `docker logs argus-<svc> --tail 100`."
     - Argus app restart loop (pending AC-5 automation): "Run `docker inspect argus-app --format='{{.RestartCount}}'`. If >5 within 5 min, `docker stop argus-app`, investigate logs, then `docker start argus-app`. Crash-loop auto-detection tracked under future ops story (DEV-314)."
     - Dependency unhealthy: "Check `docker compose ps`. If postgres/redis/nats unhealthy, argus will not start (service_healthy gate). Fix the dependency, argus will auto-start."
  6. **Limitations** — explicit list: (a) no automatic crash-loop detection, (b) `unless-stopped` will loop forever on persistent failure, (c) no external alerting hooks (tracked as future work).
  7. **References** — links to `docs/architecture/CONFIG.md`, `deploy/docker-compose.yml`, decisions DEV-312..DEV-314.
  8. **CLAUDE.md cross-link (MANDATORY):** In `CLAUDE.md`, under the "## Architecture Docs" bullet list (contains `MIDDLEWARE.md`, `ERROR_CODES.md`, `DSL_GRAMMAR.md`, `PROTOCOLS.md`, `ALGORITHMS.md`, `WEBSOCKET_EVENTS.md`, `TESTING.md`, `CONFIG.md`), add one new bullet in the natural position:
     `- \`docs/architecture/DEPLOYMENT.md\` — Container restart policy + recovery runbook`
- **Verify:**
  - File exists: `ls docs/architecture/DEPLOYMENT.md`
  - Required sections present: `grep -E "^##" docs/architecture/DEPLOYMENT.md | wc -l` ≥ 7
  - No TODO/FIXME markers: `grep -E "TODO|FIXME" docs/architecture/DEPLOYMENT.md` → empty
  - CLAUDE.md cross-linked: `grep "DEPLOYMENT.md" CLAUDE.md` → 1 match.

### Task 3 — Decisions log (DEV-312, DEV-313, DEV-314)
- **Files:** Modify `docs/brainstorming/decisions.md`
- **Depends on:** Task 1, Task 2
- **Complexity:** low
- **Pattern ref:** Follow the existing DEV table format used throughout `docs/brainstorming/decisions.md` (column schema `| ID | Date | Context | Decision | Rationale |` — check rows around lines 127-133 for exact shape). Append new rows at the END of the same table (or the next decisions section, whichever is canonical in the file).
- **Context refs:** "Scope Discipline", "Architecture Context > Restart Policy Semantics", "Architecture Context > Dependency Ordering Semantics"
- **What:** Append three rows:
  - **DEV-312** (2026-04-23): "FIX-225 — All compose services use `depends_on: service_healthy` (not `service_started`) for argus's hard deps. NATS now has a `/healthz` wget healthcheck so it can satisfy the condition. Rationale: `service_started` doesn't wait for port readiness; argus would race NATS on cold boot."
  - **DEV-313** (2026-04-23): "FIX-225 — Nginx healthcheck implemented at **compose level** (not in `deploy/nginx/Dockerfile`). Rationale: no image rebuild needed, healthcheck config lives next to restart policy, and we don't ship a custom nginx image. Story file mentioned Dockerfile HEALTHCHECK — superseded."
  - **DEV-314** (2026-04-23): "FIX-225 — AC-5 (argus app crash-loop detection, stop restart after ≥3 restarts in 1 min) DEFERRED. Reason: Compose v3 offers only `restart: on-failure:N` which loses host-reboot auto-start; proper detection needs external watchdog (Prometheus Alertmanager or `docker events` sidecar). Tracked as future ops story. Trade-off documented in `docs/architecture/DEPLOYMENT.md > Limitations`."
- **Verify:**
  - `grep -c "^| DEV-31[234]" docs/brainstorming/decisions.md` → 3
  - Each row has Date=2026-04-23 and mentions FIX-225.

## Acceptance Criteria Mapping
| Criterion | Implemented In | Verified By |
|-----------|----------------|-------------|
| AC-1 (all services have `restart: unless-stopped`) | **Already DONE** pre-FIX-225; verified in Discovery Summary table. | Task 2 (doc codifies the matrix); Task 1 `compose config` runs clean. |
| AC-2 (argus→deps use `service_healthy`) | Task 1 (NATS is the last holdout at `service_started`; upgrade requires adding NATS healthcheck first). | `grep service_started deploy/docker-compose.yml` → empty. |
| AC-3 (nginx healthcheck HTTP probe) | **Already DONE** (compose-level wget `/health`). See DEV-313 for why we did NOT add a Dockerfile HEALTHCHECK. | Task 2 doc matrix lists nginx healthcheck; Task 1 compose config exits clean. |
| AC-4 (DEPLOYMENT.md restart policy + recovery) | Task 2. | Task 2 verify step (section count + path exists). |
| AC-5 (crash loop detection) | **DEFERRED** — DEV-314 in Task 3. | Task 3 verify step confirms DEV-314 row; Task 2 doc Limitations section references it. |

## Story-Specific Compliance Rules
- **Infra:** No `restart:` policy other than `unless-stopped` unless a decisions.md entry justifies it.
- **Docs:** All new `docs/architecture/*.md` files MUST be cross-linked from `CLAUDE.md` "Architecture Docs" list (consistency requirement — every existing architecture doc is listed there). Task 2 handles this.
- **Audit:** Every non-obvious infra decision has a DEV-### entry.
- **Backward compat:** `make up` / `make down` / `make infra-up` must continue to work unchanged.

## Bug Pattern Warnings
(No `docs/brainstorming/bug-patterns.md` file exists as of 2026-04-23; no matching patterns.)

## Tech Debt (from ROUTEMAP)
No open tech debt items target FIX-225 per `docs/ROUTEMAP.md`.

## Mock Retirement
Not applicable — infra-only story, no API surface changes.

## Risks & Mitigations
- **R1:** Adding NATS healthcheck could fail on `nats:2.10-alpine` if wget is unexpectedly absent. *Mitigation:* fall back to `CMD-SHELL` with `nc -w 1 localhost 4222`; verify in Task 1 before declaring done.
- **R2:** NATS `/healthz` slow-start could cause argus start_period to exhaust and mark argus unhealthy. *Mitigation:* argus already has `start_period: 60s`; NATS `/healthz` typically ready <5s. If observed, bump NATS `start_period` to 15s.
- **R3:** New DEPLOYMENT.md drifts from compose file over time. *Mitigation:* Task 2 notes "regenerate matrix when `deploy/docker-compose.yml` changes" in the doc's header comment.
- **R4:** AC-5 deferral may be challenged in Gate. *Mitigation:* DEV-314 explains the compose-limitation; DEPLOYMENT.md Limitations section cites it; Gate reviewer can read the rationale trail.

## Test Plan (manual — no automated tests)
- **T1:** `docker compose -f deploy/docker-compose.yml config --quiet` — YAML valid after Task 1.
- **T2:** `make down && make up && sleep 90 && docker compose ps` — all 7 services report `healthy` (nginx, argus, postgres, redis, nats, operator-sim, pgbouncer).
- **T3:** Kill argus PID: `docker kill -s KILL argus-app`; within 10s `docker ps` shows argus restarting; within 90s shows `healthy`.
- **T4:** Simulate reboot: `docker compose down && docker compose up -d` (without `--force-recreate`) — all services come up; dependency ordering holds (argus waits for all 4 deps).
- **T5:** `curl -s http://localhost:8084/health` → 200 OK (end-to-end nginx→argus chain).

Unit tests intentionally omitted (infra config; covered by Docker's own healthcheck/restart machinery).

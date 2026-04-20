# FIX-225: Docker Restart Policy + Infra Stability

## Problem Statement
`argus-nginx` container was observed in "Created" state (not running) — missing `restart: unless-stopped` policy. Manual `docker start` required after host reboot. `argus-app` recently restarted (19s uptime at review time) — possible instability.

## User Story
As an ops engineer, I want all Argus containers to auto-restart on host reboot or app crash, so zero manual intervention for common failure modes.

## Findings Addressed
F-02, F-07

## Acceptance Criteria
- [ ] **AC-1:** `deploy/docker-compose.yml` — all services have `restart: unless-stopped`.
- [ ] **AC-2:** Healthcheck dependencies — `depends_on: condition: service_healthy` for argus app → postgres/redis/nats.
- [ ] **AC-3:** Nginx container healthcheck added (HTTP probe to `/health`).
- [ ] **AC-4:** `docs/architecture/DEPLOYMENT.md` — restart policy + recovery procedures documented.
- [ ] **AC-5:** Argus app crash loop detection — if ≥3 restarts within 1min, stop restarting (prevents cascade in bad state).

## Files to Touch
- `deploy/docker-compose.yml`
- `deploy/nginx/Dockerfile` — add HEALTHCHECK
- `docs/architecture/DEPLOYMENT.md`

## Risks & Regression
- **Risk 1 — Restart loop hides bugs:** AC-5 crash detection surfaces persistent failures.

## Test Plan
- Reboot docker host → all services back up
- Kill argus-app PID → restarts within 5s

## Plan Reference
Priority: P2 · Effort: S · Wave: 6

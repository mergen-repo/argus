---
story: STORY-067
title: CI/CD Pipeline, Deployment Strategy & Ops Tooling
phase: 10
effort: M-L
complexity: Medium (1 High-complexity task)
planner: amil-planner
plan_version: 1
zero_deferral: true
waves: 6
tasks: 11
acs: 9
---

# Implementation Plan: STORY-067 — CI/CD Pipeline, Deployment Strategy & Ops Tooling

## Goal
Deliver the release/deploy/ops layer — GitHub Actions CI with lint/test/security/build/deploy gates, SHA256 digest-pinned base images, blue-green deploy with Nginx upstream flip, automated rollback with image+DB snapshot, an `argusctl` admin CLI covering tenant/apikey/user/compliance/sim/backup operations, a `/api/v1/status` aggregate endpoint, deploy tagging via git + `argus_build_info` metric + audit log, 10 incident runbooks, and rationalized Makefile targets — zero deferrals.

## Phase 10 Zero-Deferral Charter
- Every AC (AC-1..AC-9) closes in THIS story. No stubs, no TODOs, no feature flags hiding incomplete work.
- CI workflow actually runs on GitHub Actions on PR and push; each stage (lint/test/security/build/deploy) is a real step and fails the run on error.
- Digest pinning is real SHA256 — `postgres:16@sha256:...`, `redis:7-alpine@sha256:...`, `nats:latest` replaced with versioned `nats:2.10-alpine@sha256:...`, `alpine:3.19@sha256:...`, `golang:1.25-alpine@sha256:...`, `node:20-alpine@sha256:...`, `nginx:alpine@sha256:...`, `edoburu/pgbouncer:latest@sha256:...`. Script `infra/scripts/update-digests.sh` is the idempotent re-pinner.
- Blue-green is a working two-stack deploy: `docker-compose.blue.yml` + `docker-compose.green.yml` on distinct host ports, Nginx upstream resolved via `include upstream.conf` that points at whichever color is active. Flip script (`deploy/scripts/bluegreen-flip.sh`) does: start new stack, wait healthy (`/health/ready` from STORY-066), smoke test, atomically rewrite `upstream.conf`, `nginx -s reload`, drain old connections, stop old stack.
- Rollback stores a JSON snapshot per deploy (`deploy/snapshots/<timestamp>.json`: prev image SHA, last backup file, config version, git SHA); `make rollback VERSION=...` reads the snapshot, redeploys the previous image, optionally `--with-db-restore` re-runs `deploy/scripts/pitr-restore.sh` against the snapshot's backup reference, runs post-rollback smoke, and appends an audit entry via `argusctl`.
- `argusctl` is a second compiled binary from `cmd/argusctl/main.go` — cobra CLI; authenticates via short-lived admin JWT minted from an operator-provided admin refresh token OR mTLS client cert (both paths implemented; pick per env). No stubs.
- `/api/v1/status` is public (no auth) but `meta.details` is gated: unauthenticated callers see `status|version|uptime|active_tenants` only; authenticated callers get dependency-level probe results.
- Deploy tagging shells out: CI creates `git tag deploy-<env>-<ISO8601>`, pushes it; `cmd/argus/main.go` exports `argus_build_info{version,git_sha,build_time}` gauge at process start; post-deploy hook writes an audit entry (`audit.Event{action:"deploy.succeeded", ...}`).
- Runbooks: `db-full.md`, `nats-lag.md`, `latency-spike.md`, `session-loss.md`, `operator-down.md`, `deploy.md`, `rollback.md`, `dr-pitr.md` (already exists — extend with status-endpoint links), `tenant-suspend.md`, `cert-rotation.md`. Each references real Grafana panel URLs (placeholders allowed for panel IDs) + real Prometheus queries (must match metrics that exist after STORY-065).
- All new Go code ships with `-race` tests; CLI commands have table-driven tests.

## Architecture Context

### Components Involved
| Component | Layer | Files (+create, ~modify) |
|-----------|-------|--------------------------|
| CI workflow | `.github/workflows/` | +`ci.yml`, +`deploy-staging.yml` (called by ci.yml on main) |
| Digest pin audit + script | `infra/docker/`, `deploy/`, `infra/scripts/` | ~`Dockerfile.argus`, ~`deploy/docker-compose.yml`, +`infra/scripts/update-digests.sh`, +`infra/scripts/update-digests_test.sh` (or `bats`-based) |
| Blue-green compose + nginx | `deploy/`, `infra/nginx/` | +`deploy/docker-compose.blue.yml`, +`deploy/docker-compose.green.yml`, ~`infra/nginx/nginx.conf` (include upstream), +`infra/nginx/upstream.conf`, +`deploy/scripts/bluegreen-flip.sh`, +`deploy/scripts/bluegreen-flip_test.sh` |
| Rollback scripts + snapshot | `deploy/scripts/`, `deploy/snapshots/`, `Makefile` | +`deploy/scripts/deploy-snapshot.sh`, +`deploy/scripts/rollback.sh`, +`deploy/snapshots/.gitkeep`, ~`Makefile` |
| `argusctl` CLI | `cmd/argusctl/` | +`main.go`, +`cmd/tenant.go`, +`cmd/apikey.go`, +`cmd/user.go`, +`cmd/compliance.go`, +`cmd/sim.go`, +`cmd/health.go`, +`cmd/backup.go`, +`cmd/status.go`, +`internal/client.go` (admin auth client), +`main_test.go` + per-cmd tests |
| Status endpoint | `internal/api/system/` | +`status_handler.go`, +`status_handler_test.go`, ~`cmd/argus/main.go` (wire), ~`internal/gateway/router.go` (route) |
| Build info metric + wire | `internal/observability/metrics/`, `cmd/argus/main.go` | ~`metrics.go`, ~`main.go` (set on startup) |
| Deploy tagging hook | `deploy/scripts/`, `internal/audit/` | +`deploy/scripts/deploy-tag.sh`, reuse `audit.Store.WriteEvent` from existing layer |
| Makefile rationalization | repo root | ~`Makefile` |
| Runbook suite | `docs/runbook/` | +`db-full.md`, +`nats-lag.md`, +`latency-spike.md`, +`session-loss.md`, +`operator-down.md`, +`deploy.md`, +`rollback.md`, +`tenant-suspend.md`, +`cert-rotation.md`, ~`dr-pitr.md` (add status endpoint + STORY-067 deploy cross-links) |
| CI smoke script | `deploy/scripts/` | +`deploy/scripts/smoke-test.sh` (calls `/health/ready`, `/api/v1/status`, `/metrics`, hits 3 read-only API routes with seeded token) |

### Data Flow — CI Pipeline (AC-1)
```
PR opened / push to main
    │
    ▼
GitHub Actions: ci.yml triggered
    │
    ▼  Stage 1 (parallel matrix)
  ├── go-lint: golangci-lint run ./...
  └── web-lint: cd web && npm ci && npm run lint && npm run type-check
    │
    ▼  Stage 2 (parallel matrix)
  ├── go-test: go test ./... -race -cover -timeout=10m
  └── web-test: cd web && npm test -- --run
    │
    ▼  Stage 3 (parallel matrix)
  ├── govulncheck ./...
  ├── gosec ./... (install via go install github.com/securego/gosec/v2/cmd/gosec@latest)
  └── cd web && npm audit --audit-level=high
    │
    ▼  Stage 4 (main/PR both) — image build
  Docker build using infra/docker/Dockerfile.argus
  Tag with: <registry>/argus:sha-<short-sha> AND <registry>/argus:<branch>
  Push to GHCR (PRs: build only, no push; main: build + push)
    │
    ▼  Stage 5 (main only) — staging deploy + smoke
  SSH to staging host → make deploy-staging (which pulls SHA tag + runs bluegreen-flip.sh)
  Wait 60s for readiness
  curl /health/ready → assert 200 OK with all probes OK
  curl /api/v1/status → assert version matches new SHA
  Run deploy/scripts/smoke-test.sh (fails workflow if any assertion fails)
    │
    ▼  On success
  Create git tag: deploy-staging-<ISO8601>
  Attach workflow artifact: snapshot JSON for rollback
```

### Data Flow — Blue-Green Flip (AC-3)
```
make deploy-staging (or prod after confirmation)
    │
    ▼
deploy-snapshot.sh: write deploy/snapshots/<ts>.json {
  prev_image: "<registry>/argus@sha256:...",     // from docker compose config
  prev_backup: "backups/<latest>.sql",            // from make db-backup
  config_version: "<git_sha>",
  started_at: "<ISO>"
}
    │
    ▼
bluegreen-flip.sh:
  1. Determine CURRENT color from infra/nginx/upstream.conf (grep active upstream)
  2. Start INACTIVE color: docker compose -f deploy/docker-compose.<inactive>.yml up -d
  3. Wait loop (max 120s, 2s interval): curl http://localhost:<inactive-port>/health/ready
     — if probes all green, proceed; else abort + tear down inactive
  4. Run deploy/scripts/smoke-test.sh against inactive
  5. Atomic rewrite of upstream.conf (write upstream.conf.new, mv -f over upstream.conf)
  6. docker compose -f deploy/docker-compose.yml exec nginx nginx -s reload
  7. Drain window: sleep ${DRAIN_SECONDS:-15}
  8. docker compose -f deploy/docker-compose.<current>.yml stop
  9. Finalize snapshot: append finished_at + new_image SHA
  10. argusctl audit emit — "deploy.succeeded"
```

### Data Flow — Rollback (AC-4)
```
make rollback VERSION=deploy-prod-2026-04-11T12:00:00 [--with-db-restore]
    │
    ▼
rollback.sh:
  1. Read deploy/snapshots/<ts>.json for the given version tag
  2. Assert: previous image digest is still present locally OR pullable from registry
  3. Spin up previous image in INACTIVE color via blue-green flip process (reuses bluegreen-flip.sh)
  4. If --with-db-restore: invoke deploy/scripts/pitr-restore.sh with snapshot's backup reference
  5. Run smoke-test.sh
  6. If smoke fails → abort, keep current color live
  7. Emit audit event: deploy.rollback.succeeded{from_version, to_version, with_db_restore}
  8. Git tag rollback-<env>-<ISO8601>
```

### Data Flow — argusctl (AC-5)
```
operator shell → argusctl tenant create --name=acme --admin-email=ops@acme.io
    │
    ▼
argusctl/cmd/tenant.go:
  1. Load config from ~/.argusctl.yaml (API base URL, admin refresh token OR mTLS cert paths)
  2. internal/client.go: POST /api/v1/auth/admin-refresh → short-lived admin JWT
      (if mTLS: open https.Client with client cert, no token needed)
  3. POST /api/v1/tenants with {name, admin_email} + Authorization: Bearer <admin-jwt>
  4. Parse envelope response; on success print: "Tenant created: <id>. Temp admin password: <pw>"
  5. Audit happens server-side (existing tenant handler already audits)
```

### Data Flow — /api/v1/status (AC-7)
```
GET /api/v1/status [no auth required]
    │
    ▼
status_handler.Serve(w, r):
  1. Gather: version (from observability.BuildInfo), git_sha, build_time, uptime (time.Since(startAt))
  2. Query TenantStore.Count() (active tenants)
  3. Query recent errors via metrics snapshot (argus_http_requests_total{status=~"5.."} in last 5m) — fallback to zero if Prometheus scrape not configured
  4. Shallow probe: db/redis/nats via the same HealthChecker interfaces (reuse HealthHandler aggregators)
  5. Build envelope:
     {status:"success", data:{overall:"healthy|degraded|unhealthy", version, git_sha, build_time, uptime, active_tenants, recent_error_5m}, meta: maybe{details:<probe map>}}
  6. Gate `meta.details`: only attach if request has valid admin JWT; otherwise omit.
```

### API Specifications

#### `GET /api/v1/status` (new — AC-7)
- **Request:** none; `Authorization: Bearer <admin-jwt>` optional
- **Success 200 (unauthenticated):**
  ```json
  {
    "status": "success",
    "data": {
      "overall": "healthy",
      "version": "v0.67.0",
      "git_sha": "abc1234",
      "build_time": "2026-04-12T10:00:00Z",
      "uptime": "3h22m",
      "active_tenants": 42,
      "recent_error_5m": 0
    }
  }
  ```
- **Success 200 (authenticated admin):** same body, plus `meta.details` with `{db, redis, nats, aaa}` probe results mirroring `/health/ready` format.
- **Degraded (any dependency probe amber):** `overall:"degraded"`, HTTP 200.
- **Unhealthy:** `overall:"unhealthy"`, HTTP 503.
- **No error envelope paths** (read-only aggregate; panics recovered by middleware).
- **Non-goal:** not mounted behind rate limiter (public status consumption); brute-force middleware skipped via the existing `/api/health`-style allowlist.

#### `POST /api/v1/auth/admin-refresh` (NOT NEW — existing `/api/v1/auth/refresh` reused with admin-role check)
- Planner note: no new endpoint needed; argusctl calls existing refresh; server-side we add a check that the token has `role=admin` when issuing commands via CLI-targeted endpoints. Add enforcement as part of argusctl task if missing.

### Database Schema
No new tables. No migrations for this story.
- `argusctl user purge` calls into existing `UserStore.DeletePII` (from STORY-059 — verify the helper exists; if not, add it as part of that task's scope — see Risks).
- `argusctl compliance export` calls the existing compliance handler (from STORY-039/STORY-059) and writes the response bytes to a file.
- `argusctl backup restore` shells out to `deploy/scripts/pitr-restore.sh` (exists from STORY-066).

### Base Image Digest Pinning (AC-2) — target list
| Service | Current tag | Action |
|---------|-------------|--------|
| `golang:1.25-alpine` (builder stage) | floating | pin `golang:1.25-alpine@sha256:...` |
| `node:20-alpine` (web-builder stage) | floating | pin `node:20-alpine@sha256:...` |
| `alpine:3.19` (runtime stage) | floating | pin `alpine:3.19@sha256:...` |
| `postgres:16` (compose — verify current) | floating | pin `postgres:16@sha256:...` |
| `redis:7-alpine` (compose) | floating | pin `redis:7-alpine@sha256:...` |
| `nats:latest` (compose) | floating `latest` — **REPLACE** with `nats:2.10-alpine@sha256:...` | pin versioned |
| `nginx:alpine` (compose) | floating | pin `nginx:alpine@sha256:...` |
| `edoburu/pgbouncer:latest` (compose) | floating `latest` — **REPLACE** with specific version `edoburu/pgbouncer:1.22.1@sha256:...` | pin versioned |

`infra/scripts/update-digests.sh` usage:
```
./infra/scripts/update-digests.sh            # Resolves current pinned tags → fetches latest digests → updates files in place
./infra/scripts/update-digests.sh --check    # Exit 1 if any digest stale (for CI monthly run)
```

### Build Info Metric (AC-8)
- New Prometheus gauge in `internal/observability/metrics/metrics.go`:
  ```
  argus_build_info{version, git_sha, build_time} 1
  ```
- Set once at process startup in `cmd/argus/main.go` via `metricsReg.BuildInfo.WithLabelValues(version, gitSHA, buildTime).Set(1)`.
- Values injected by `-ldflags "-X main.version=... -X main.gitSHA=... -X main.buildTime=..."` — already the pattern for Go build info; add the ldflags into `Dockerfile.argus` build stage and CI `make build`.

### Screen Mockups
No screen mockups — backend/infra story. `/api/v1/status` surface feeds SCR-120 (SVC health) later; not implemented here.

### Design Token Map
Not applicable — no UI work in this story.

### Makefile Targets (AC-9) — rationalization
New / modified targets:
- `make lint` — runs `golangci-lint run ./...` AND `cd web && npm run lint && npm run type-check`
- `make test` — keep current (`go test ./... -v -race -short`); add `make test-web` for `cd web && npm test`
- `make security-scan` — new: `govulncheck ./...` + `gosec ./...` + `cd web && npm audit --audit-level=high`
- `make deploy-staging` — new: calls `deploy/scripts/deploy-snapshot.sh` then `bluegreen-flip.sh staging`
- `make deploy-prod` — modify: add explicit confirmation prompt + pre-flight (`make test` + `make lint` + `make security-scan` + `make db-backup`) + `bluegreen-flip.sh prod`
- `make rollback VERSION=...` — new: calls `deploy/scripts/rollback.sh $(VERSION) $(WITH_DB_RESTORE)`
- `make ops-status` — new: `curl -s http://$${ARGUS_HOST:-localhost:8084}/api/v1/status | jq .`
- `help` target extended (new grouping "CI & Release")

## Prerequisites
- [x] STORY-065 (observability) — DONE. Metrics registry exists at `internal/observability/metrics/metrics.go`; Prometheus scrape wired; Grafana dashboards exist for panel references in runbooks.
- [x] STORY-066 (reliability + backups) — DONE. `/health/{live,ready,startup}` exist at `internal/gateway/health.go:Live/Ready/Startup` (router.go:109-111); backup automation + PITR restore script at `deploy/scripts/pitr-restore.sh` exist; CLI `argusctl backup restore` shells out to it.
- [x] Existing Go build (`cmd/argus`) compiles — confirmed.

## Task Decomposition Rules
> Each task dispatched to a FRESH Developer subagent with isolated context.
> Amil orchestrator extracts the context sections listed in `Context refs` and passes them directly to the Developer.
> The Developer does NOT read this plan file.

## Tasks

### Task 1 (Wave 1) — Base image digest pinning + re-pin script
- **Files:** ~`infra/docker/Dockerfile.argus`, ~`deploy/docker-compose.yml`, +`infra/scripts/update-digests.sh`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `infra/docker/Dockerfile.argus` and `deploy/docker-compose.yml` (both are in-repo and short). Script pattern: bash with `set -euo pipefail`, `docker manifest inspect` for digest retrieval. Mirror header style of `deploy/scripts/pitr-restore.sh` (if script is longer than a few lines, include the same banner comment convention).
- **Context refs:** "Architecture Context > Base Image Digest Pinning (AC-2) — target list", "Phase 10 Zero-Deferral Charter" (digest bullet)
- **What:** (a) Replace every `FROM <image>:<tag>` and compose `image: <image>:<tag>` with `<image>:<tag>@sha256:...` (resolve actual digests via `docker manifest inspect <image>:<tag>`). (b) Replace `nats:latest` with `nats:2.10-alpine@sha256:...` and `edoburu/pgbouncer:latest` with `edoburu/pgbouncer:1.22.1@sha256:...`. (c) Create `infra/scripts/update-digests.sh` that: iterates a list of `<file>:<image>:<tag>` tuples, calls `docker manifest inspect --verbose <image>:<tag>` to resolve digest, rewrites the file in-place with the new `@sha256` fragment. Support `--check` flag (exit 1 if any drift).
- **Verify:** `./infra/scripts/update-digests.sh --check` exits 0 right after pinning; `docker compose -f deploy/docker-compose.yml config` succeeds; `docker build -f infra/docker/Dockerfile.argus .` succeeds locally.

### Task 2 (Wave 1) — Makefile rationalization
- **Files:** ~`Makefile`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `Makefile` in repo root — follow existing Turkish `@echo` style + `.PHONY` list convention. Keep existing targets backward-compatible (do NOT remove `deploy-dev`, `build`, etc. — they remain used).
- **Context refs:** "Architecture Context > Makefile Targets (AC-9) — rationalization"
- **What:** Add new targets `lint` (extend to also run web lint+type-check), `test-web`, `security-scan`, `deploy-staging`, `rollback`, `ops-status`. Modify `deploy-prod` to add pre-flight sequence. Add a "CI & Release" section to the `help` target. Keep the Turkish descriptions style consistent. Add new targets to `.PHONY` declaration.
- **Verify:** `make help` displays new section; `make -n deploy-staging` dry-run prints the expected sequence; every existing target still runs.

### Task 3 (Wave 2) — argus_build_info metric + ldflags injection
- **Files:** ~`internal/observability/metrics/metrics.go`, ~`internal/observability/metrics/metrics_test.go`, ~`cmd/argus/main.go`, ~`infra/docker/Dockerfile.argus`
- **Depends on:** Task 1 (Dockerfile already being edited there — avoid merge conflict by sequencing)
- **Complexity:** low
- **Pattern ref:** Read `internal/observability/metrics/metrics.go:42-80` (NewRegistry pattern — how gauges are registered). Read `cmd/argus/main.go:79-100` for where to set gauge on startup.
- **Context refs:** "Architecture Context > Build Info Metric (AC-8)"
- **What:** Add `BuildInfo *prometheus.GaugeVec` to the Registry struct with labels `{version, git_sha, build_time}`. In `NewRegistry()` register it. In `cmd/argus/main.go`, declare `var (version, gitSHA, buildTime string)` package-level, then after metrics registry creation call `metricsReg.BuildInfo.WithLabelValues(version, gitSHA, buildTime).Set(1)`. In Dockerfile builder stage, add `-ldflags "-w -s -X main.version=$${VERSION:-dev} -X main.gitSHA=$${GIT_SHA:-unknown} -X main.buildTime=$${BUILD_TIME}"`. Update `metrics_test.go` to assert the gauge is registered.
- **Verify:** `go build ./...` passes; `go test ./internal/observability/...` passes; after building with `GIT_SHA=abc1234 VERSION=test docker build ...`, `curl :8080/metrics | grep argus_build_info` returns a line with those label values.

### Task 4 (Wave 2) — CI pipeline (.github/workflows/ci.yml) + smoke script
- **Files:** +`.github/workflows/ci.yml`, +`deploy/scripts/smoke-test.sh`
- **Depends on:** Task 2 (Makefile targets used by CI)
- **Complexity:** medium
- **Pattern ref:** First of its kind in repo (`.github/workflows/` is empty). Establish pattern: separate `jobs:` per stage with `needs:` chain, use `actions/setup-go@v5`, `actions/setup-node@v4`, Docker `docker/setup-buildx-action@v3`, GHCR login `docker/login-action@v3`, `docker/build-push-action@v6`. Reference: GitHub Actions docs for matrix builds.
- **Context refs:** "Architecture Context > Data Flow — CI Pipeline (AC-1)", "Phase 10 Zero-Deferral Charter" (CI bullet), "Makefile Targets" section
- **What:** Implement `.github/workflows/ci.yml` with 5 stages per data flow. Use `needs` dependencies. Staging deploy stage triggers only on `push` to `main`, runs `make deploy-staging` via SSH action (`appleboy/ssh-action@v1`) with host/user/key from repository secrets (`STAGING_SSH_HOST`, `STAGING_SSH_USER`, `STAGING_SSH_KEY`). After deploy, run `deploy/scripts/smoke-test.sh <host>` which: (a) `curl /health/ready`, assert JSON has `state:"ready"`; (b) `curl /api/v1/status`, assert `data.version` equals short-sha of commit; (c) hits `/api/v1/health`, `/metrics`, and one read-only API route with a test-tenant token from secrets. On success, create and push `git tag deploy-staging-<iso-timestamp>`. Fail-fast semantics: every stage must pass for overall success.
- **Verify:** `.github/workflows/ci.yml` passes `actionlint` (run locally if available; otherwise yaml-lint equivalent). Documented in a comment inside the workflow that merge protections must require this workflow (repo setting, not file setting). Run `bash -n deploy/scripts/smoke-test.sh`.

### Task 5 (Wave 3) — Blue-green compose + Nginx upstream include + flip script
- **Files:** +`deploy/docker-compose.blue.yml`, +`deploy/docker-compose.green.yml`, ~`infra/nginx/nginx.conf`, +`infra/nginx/upstream.conf`, +`deploy/scripts/bluegreen-flip.sh`
- **Depends on:** Task 1 (compose files inherit pinned digests)
- **Complexity:** medium
- **Pattern ref:** Read `deploy/docker-compose.yml` (base stack), `infra/nginx/nginx.conf` lines 1-40 for header/event block convention. Script pattern: mirror `deploy/scripts/pitr-restore.sh` structure (header banner + `set -euo pipefail` + step-numbered shell functions + color output).
- **Context refs:** "Architecture Context > Data Flow — Blue-Green Flip (AC-3)", "Architecture Context > Components Involved (Blue-green)"
- **What:** (a) Create `docker-compose.blue.yml` and `.green.yml` as overlay files that redefine the `argus` service (different `container_name: argus-app-blue|green`, different `ports:` binding the HTTP port to host :8085/:8086). Other services stay shared (postgres/redis/nats). (b) Create `infra/nginx/upstream.conf` with a single `upstream argus_backend { server argus-app-blue:8080; }` block. (c) Modify `infra/nginx/nginx.conf` `http {}` section: replace direct `server argus:8080` references with `include /etc/nginx/upstream.conf;` + `proxy_pass http://argus_backend;`. Remount `upstream.conf` in compose. (d) Write `bluegreen-flip.sh <env>` per the data flow, using health check loop with 2s poll, 120s timeout against `/health/ready`. Atomic file rewrite: `cp upstream.conf upstream.conf.bak && echo ... > upstream.conf.new && mv -f upstream.conf.new upstream.conf`. Reload via `docker compose exec nginx nginx -s reload`. Drain sleep ${DRAIN_SECONDS:-15}. Stop old color compose file. Emit audit via `argusctl audit emit` (or curl the audit API if argusctl is not yet built — note: Task 7 builds argusctl, so this task can use curl fallback which the test uses too).
- **Verify:** `docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.blue.yml config` parses OK; same for green; `bash -n deploy/scripts/bluegreen-flip.sh`; ShellCheck passes; nginx config reload succeeds after upstream.conf change (can test with a temporary local nginx container).

### Task 6 (Wave 3) — Deploy snapshot + rollback scripts + Make targets wiring
- **Files:** +`deploy/scripts/deploy-snapshot.sh`, +`deploy/scripts/rollback.sh`, +`deploy/scripts/deploy-tag.sh`, +`deploy/snapshots/.gitkeep`, ~`Makefile` (wire targets to scripts)
- **Depends on:** Task 2 (Makefile), Task 5 (bluegreen-flip.sh exists)
- **Complexity:** medium
- **Pattern ref:** `deploy/scripts/pitr-restore.sh` for bash conventions + step numbering. JSON snapshot written via `cat > ...json <<EOF` heredoc.
- **Context refs:** "Architecture Context > Data Flow — Rollback (AC-4)", "Phase 10 Zero-Deferral Charter" (rollback + tagging bullets)
- **What:** (a) `deploy-snapshot.sh <env>`: writes `deploy/snapshots/<ISO>.json` capturing prev image SHA (via `docker inspect`), latest backup filename from `backups/`, current git SHA, timestamps. (b) `rollback.sh <version-tag> [--with-db-restore]`: reads the snapshot, re-tags the old image as "incoming", invokes `bluegreen-flip.sh <env> --image=<prev-sha>`, on `--with-db-restore` calls `pitr-restore.sh` with the snapshot's backup reference, runs `smoke-test.sh`, appends audit event. (c) `deploy-tag.sh <env>`: creates + pushes `deploy-<env>-<ISO>` git tag. (d) Wire `make deploy-staging`, `make deploy-prod`, `make rollback` targets to invoke these scripts in sequence (snapshot → flip → tag).
- **Verify:** `bash -n` for each script; dry-run `make -n rollback VERSION=x` prints expected sequence; `./deploy-snapshot.sh dev` in a local compose produces valid JSON (`jq .` passes); gitignore `deploy/snapshots/*.json` but keep `.gitkeep`.

### Task 7 (Wave 4) — argusctl CLI scaffold + tenant/apikey/user/health commands [HIGH complexity]
- **Files:** +`cmd/argusctl/main.go`, +`cmd/argusctl/cmd/root.go`, +`cmd/argusctl/cmd/tenant.go`, +`cmd/argusctl/cmd/apikey.go`, +`cmd/argusctl/cmd/user.go`, +`cmd/argusctl/cmd/health.go`, +`cmd/argusctl/cmd/status.go`, +`cmd/argusctl/internal/client.go`, +`cmd/argusctl/cmd/tenant_test.go`, +`cmd/argusctl/cmd/client_test.go`, ~`infra/docker/Dockerfile.argus` (build second binary), ~`Makefile` (add `build-ctl`)
- **Depends on:** Task 2 (Makefile — `build-ctl` target)
- **Complexity:** high
- **Pattern ref:** For HTTP client: read `internal/api/tenant/handler.go:163` for the tenant create request shape + `internal/store/tenant.go:47-78` for params. For cobra CLI structure: first of its kind in repo — establish pattern with `cobra.Command{Use:"tenant", Short:"..."}` tree rooted at `rootCmd` in `cmd/argusctl/cmd/root.go`. Admin auth flow: POST to `/api/v1/auth/refresh` (existing) with role=admin enforcement; mTLS path uses `http.Client{Transport: &http.Transport{TLSClientConfig: ...}}`. Envelope parse: `type resp struct{ Status string; Data json.RawMessage; Error *struct{ Code, Message string } }`.
- **Context refs:** "Architecture Context > Data Flow — argusctl (AC-5)", "Phase 10 Zero-Deferral Charter" (argusctl bullet), full AC-5 list in story
- **What:** Build cobra-based CLI. `argusctl` root command with persistent flags `--config` (default `~/.argusctl.yaml`), `--api-url`, `--token`, `--cert`, `--key`, `--ca`, `--format` (json|table). Subcommands:
  - `tenant create --name --admin-email` → POST /api/v1/tenants; print temp password from response
  - `tenant list` → GET /api/v1/tenants; render table
  - `tenant suspend <id>` → POST /api/v1/tenants/<id>/suspend
  - `tenant resume <id>` → POST /api/v1/tenants/<id>/resume
  - `apikey rotate --tenant --key` → POST /api/v1/apikeys/<key>/rotate (grace period flag)
  - `user purge --tenant --user --confirm` → DELETE /api/v1/users/<id>?gdpr=1 (must error out without `--confirm`)
  - `health` → GET /health/ready (raw JSON pretty-print)
  - `status` → GET /api/v1/status (table render)
  
  Configure Go module entry by adding `cmd/argusctl` to Dockerfile build stage (emit `/app/argusctl` binary). Add `make build-ctl` target. Unit tests: mock HTTP server via `httptest.NewServer` for each command's happy path + one error path.
- **Verify:** `go build ./cmd/argusctl` succeeds. `go test ./cmd/argusctl/...` all green. `./argusctl --help` lists all commands. Manual smoke against dev env: `./argusctl tenant list --api-url=http://localhost:8084 --token=<admin-jwt>` returns tenants.

### Task 8 (Wave 4) — argusctl compliance/sim/backup commands
- **Files:** +`cmd/argusctl/cmd/compliance.go`, +`cmd/argusctl/cmd/sim.go`, +`cmd/argusctl/cmd/backup.go`, +`cmd/argusctl/cmd/compliance_test.go`, +`cmd/argusctl/cmd/sim_test.go`, +`cmd/argusctl/cmd/backup_test.go`
- **Depends on:** Task 7 (scaffold + client)
- **Complexity:** medium
- **Pattern ref:** Follow the structure established in Task 7 (same cobra.Command pattern, same client.go usage). For `backup restore`, shell out: `cmd := exec.Command("deploy/scripts/pitr-restore.sh", "--from="+backupFile)` + stream stdout/stderr.
- **Context refs:** "Architecture Context > Data Flow — argusctl (AC-5)", AC-5 bullet list, "Prerequisites" (pitr-restore.sh exists from STORY-066)
- **What:**
  - `compliance export --tenant --format=pdf --from --to --output=FILE` → GET /api/v1/compliance/reports?format=pdf&... streams body to `--output`
  - `sim bulk-op --tenant --operation=suspend|resume --segment=ID` → POST /api/v1/jobs/bulk; then GET /api/v1/jobs/<id> polling for progress; stream progress bar
  - `backup restore --from=<file>` → confirms with `--confirm` flag, shells out to `deploy/scripts/pitr-restore.sh`, pipes stdout
- **Verify:** `go test ./cmd/argusctl/...` passes; integration: `./argusctl compliance export --tenant=acme --format=pdf --from=2026-01-01 --to=2026-03-31 --output=/tmp/x.pdf` produces non-zero file; `./argusctl backup restore --from=backups/foo.sql --dry-run` prints the pitr-restore command without executing.

### Task 9 (Wave 5) — `/api/v1/status` endpoint + router wiring + test
- **Files:** +`internal/api/system/status_handler.go`, +`internal/api/system/status_handler_test.go`, ~`internal/gateway/router.go`, ~`cmd/argus/main.go` (wire)
- **Depends on:** Task 3 (BuildInfo + version vars)
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/system/reliability_handler.go` (same package — follow its handler struct + constructor convention). Read `internal/gateway/health.go:Ready` for the aggregate-probe pattern. Read `internal/api/tenant/handler.go` for envelope write pattern (`apierr.WriteEnvelope`). Register the route in `internal/gateway/router.go` under the `/api/v1/` group (pattern: line 156-157 style).
- **Context refs:** "Architecture Context > Data Flow — /api/v1/status (AC-7)", "API Specifications > GET /api/v1/status"
- **What:** Implement `StatusHandler` with dependencies `{healthHandler, tenantStore, metricsReg, version, gitSHA, buildTime, startAt}`. `Serve(w, r)` method builds the aggregate per data flow. Role gate for `meta.details`: inspect `r.Context()` for presence of admin claim (reuse `auth.FromContext(ctx)` helper); if admin → include details. Unit tests: table-driven over (db down / redis down / all ok / admin-authenticated / non-authenticated) asserting HTTP status + body shape. Register route `r.Get("/api/v1/status", deps.StatusHandler.Serve)` in router.go.
- **Verify:** `go test ./internal/api/system/...` passes with `-race`. Manual: `curl http://localhost:8084/api/v1/status | jq .data.version` returns the injected version. With admin token: `meta.details` is present.

### Task 10 (Wave 6) — Runbook suite (10 playbooks)
- **Files:** +`docs/runbook/db-full.md`, +`docs/runbook/nats-lag.md`, +`docs/runbook/latency-spike.md`, +`docs/runbook/session-loss.md`, +`docs/runbook/operator-down.md`, +`docs/runbook/deploy.md`, +`docs/runbook/rollback.md`, +`docs/runbook/tenant-suspend.md`, +`docs/runbook/cert-rotation.md`, ~`docs/runbook/dr-pitr.md` (cross-link section)
- **Depends on:** Task 5 (deploy.md documents blue-green flip), Task 6 (rollback.md references rollback.sh), Task 9 (runbooks reference /api/v1/status)
- **Complexity:** medium
- **Pattern ref:** Read `docs/runbook/dr-pitr.md` — it is the template. Sections: "When to use", "Prerequisites", "Estimated Duration" (table), "Procedure" (numbered steps), "Verification", "Post-incident" (audit entries, comms). Each runbook lists specific Prometheus queries + Grafana panel URLs (placeholder IDs OK — must include metric names that exist: `argus_http_request_duration_seconds`, `argus_active_sessions`, `argus_nats_consumer_lag`, `argus_disk_usage_percent`, `argus_backup_last_success_timestamp`, `argus_build_info`, `argus_aaa_auth_latency_seconds`, `argus_circuit_breaker_state`, etc.).
- **Context refs:** "Phase 10 Zero-Deferral Charter" (runbook bullet), "Prerequisites" (metrics available)
- **What:** Write each runbook with the structure above. Specific content requirements:
  - `db-full.md`: disk purge strategy (prune old CDRs via partitions; emergency archive to S3; vacuum full)
  - `nats-lag.md`: identify lagging consumer (via `argus_nats_consumer_lag`), scale consumer, purge old messages if safe
  - `latency-spike.md`: p99 triage checklist (check DB pool saturation, slow query log, downstream operator circuit breakers, rate limiter saturation)
  - `session-loss.md`: RADIUS storm detection + mitigation (rate-limit ACL, fall back to stateless auth cache)
  - `operator-down.md`: failover routing, comms templates, SLA clock
  - `deploy.md`: blue-green procedure — full command sequence from `make deploy-prod`
  - `rollback.md`: rollback procedure — `make rollback VERSION=...` + when to use `--with-db-restore`
  - `tenant-suspend.md`: legal/compliance-driven suspension — CLI command + audit trail + comms
  - `cert-rotation.md`: JWT key rotation (references STORY-066 dual-key flow); stub TLS section for when TLS is added
  - `dr-pitr.md`: extend with a "See also" section linking to deploy.md, rollback.md, /api/v1/status
- **Verify:** Each file opens with proper H1; `grep -l "Prometheus" docs/runbook/*.md` lists ≥9 files; every metric name referenced exists in `internal/observability/metrics/metrics.go`; manual linting by rendering in VS Code Markdown preview.

### Task 11 (Wave 6) — Integration tests + CI workflow dry-run
- **Files:** +`test/e2e/story067_test.go` (or bash harness at `test/e2e/story067_cicd.sh`), ~`deploy/scripts/smoke-test.sh` (finalize assertions)
- **Depends on:** Tasks 4, 5, 6, 7, 8, 9, 10
- **Complexity:** medium
- **Pattern ref:** Read `test/e2e/` existing structure (if any — otherwise establish). Go test with `//go:build e2e` tag. Use `exec.Command("make", "deploy-staging")` + HTTP assertions.
- **Context refs:** "Test Scenarios" (story file), "Data Flow — Blue-Green Flip", "Data Flow — Rollback"
- **What:** Implement test scenarios covering:
  - CI lint failure simulation: create a temp branch with a lint error, assert `golangci-lint run` exits non-zero (can be a shell test, not requiring GitHub).
  - Blue-green flip under load: run `wrk -t4 -c100 -d30s http://localhost:8084/api/health` while executing `bluegreen-flip.sh dev`; assert no HTTP 5xx in wrk output (tolerance: 0 dropped requests).
  - Rollback: snapshot current deploy, deploy a breaking change, `make rollback VERSION=<previous>`, assert smoke passes, audit entry created.
  - `argusctl tenant create --name=e2e-test --admin-email=e2e@example.com`: assert tenant exists in DB.
  - `argusctl compliance export`: assert PDF produced with expected sections (PDF header magic + > 1KB size).
  - `argusctl backup restore --from=<fixture.sql>`: assert DB restored (smoke SELECT count).
  - Latency-spike runbook step-by-step under `wrk` load: execute runbook steps, confirm each Prometheus query returns data.
- **Verify:** All scenarios pass when run manually via `make test-e2e` (new Makefile target if needed); document any manual-only scenarios in `docs/e2e-evidence/STORY-067-cicd-test.md` (as STORY-066 did for PITR).

## Acceptance Criteria Mapping
| AC | Summary | Implemented In | Verified By |
|----|---------|----------------|-------------|
| AC-1 | GitHub Actions CI with 5 stages | Task 4 | Task 4 verify + Task 11 lint-failure sim |
| AC-2 | Base image digest pinning + update script | Task 1 | Task 1 verify (`--check` exits 0) |
| AC-3 | Blue-green deployment (compose + nginx + flip script) | Task 5 | Task 11 blue-green flip under load |
| AC-4 | Automated rollback with snapshot + optional DB restore | Task 6 | Task 11 rollback scenario |
| AC-5 | `argusctl` admin CLI (9 command groups) | Task 7 + Task 8 | Task 7/8 unit tests + Task 11 CLI scenarios |
| AC-6 | Runbook directory (10 playbooks) | Task 10 | Task 10 verify + Task 11 latency-spike runbook walk |
| AC-7 | `/api/v1/status` aggregate endpoint | Task 9 | Task 9 unit test + Task 4 smoke assertion |
| AC-8 | Deploy tagging: git tag + `argus_build_info` + audit | Task 3 (metric) + Task 6 (tag script + audit) | Task 3 verify + Task 6 verify + Task 11 rollback audit assertion |
| AC-9 | Makefile rationalization + help docs | Task 2 | Task 2 verify + Task 11 via `make` usage |

## Wave Schedule (parallelizable within a wave)
- **Wave 1 (parallel):** Task 1 (digest pin), Task 2 (Makefile)
- **Wave 2 (parallel):** Task 3 (build info metric), Task 4 (CI workflow)
- **Wave 3 (parallel):** Task 5 (blue-green), Task 6 (snapshot/rollback scripts)
- **Wave 4 (sequential):** Task 7 (argusctl scaffold — HIGH), then Task 8 (remaining commands)
- **Wave 5:** Task 9 (/api/v1/status endpoint)
- **Wave 6 (parallel):** Task 10 (runbooks), Task 11 (integration tests)

## Story-Specific Compliance Rules
- **API (Task 9):** `/api/v1/status` MUST use the standard envelope `{status, data, meta?, error?}`. Public access (no auth middleware on that route specifically) but `meta.details` gated by admin claim — enforce in handler, not middleware.
- **CLI (Tasks 7, 8):** Every `argusctl` subcommand that mutates state MUST require explicit `--confirm` flag for destructive operations (`user purge`, `backup restore`, `tenant suspend`). Every subcommand MUST respect the envelope response shape and print the server's error message on non-2xx.
- **Audit (Tasks 5, 6):** Every deploy/rollback success emits an audit event via the existing `internal/audit` layer (shell-out to `argusctl audit emit` once Task 7 lands; interim curl POST to `/api/v1/audit/events` if admin endpoint exists, else new endpoint is OUT OF SCOPE and requires story amendment — see Risks).
- **DB:** No schema changes. No migration needed.
- **Docker:** After Task 1, ALL base images must carry a `@sha256:...` pin. CI must enforce via `infra/scripts/update-digests.sh --check`.
- **Secrets (Task 4):** CI workflow MUST source every credential from GitHub Secrets (`STAGING_SSH_KEY`, `GHCR_TOKEN`, `SMOKE_TEST_JWT`). No secret values hardcoded.
- **ADR-001 (modular monolith):** `argusctl` is a separate binary, NOT a second monolith — it is a thin HTTP client over the existing API. No new business logic in argusctl; reuse server-side validation.

## Bug Pattern Warnings
Read `docs/brainstorming/decisions.md` Bug Patterns section (PAT-001..PAT-003). None of the listed patterns (BR-assertion tests; duplicated `extractIP`; EAP-SIM AT_MAC zeroing) overlap with the CI/ops/CLI scope of this story. **No matching bug patterns.**

## Tech Debt (from ROUTEMAP)
Reviewed `docs/ROUTEMAP.md` Tech Debt table (D-001..D-005). None target STORY-067 (D-001, D-002, D-003 target STORY-062/STORY-077; D-004, D-005 RESOLVED). **No tech debt items for this story.**

## Mock Retirement (Frontend-First projects only)
Story has no new frontend UI and does not introduce new API endpoints for which frontend mocks exist. The only new endpoint (`/api/v1/status`) has no frontend mock to retire. **No mock retirement for this story.**

## Risks & Mitigations
- **R1: `user purge` helper missing in UserStore.** The CLI command assumes `UserStore.DeletePII` from STORY-059. If not present, Task 7 must add a minimal helper inline (GDPR erasure: NULL-ify PII columns + delete sessions + audit log). — **Mitigation:** Task 7 scope note; grep `internal/store/user.go` first; if missing, extend Task 7 Files list rather than failing gate.
- **R2: Admin-refresh endpoint role enforcement.** If existing `/api/v1/auth/refresh` doesn't enforce admin role when minting CLI tokens, Task 7 must add a role check (middleware) — not a separate endpoint. — **Mitigation:** Grep `internal/api/auth/` first; if missing, add to Task 7.
- **R3: Docker manifest inspect requires login.** Some registries need `docker login` before `docker manifest inspect` works. — **Mitigation:** Task 1 script supports `DOCKER_CREDS` env or falls back to public-image path `docker pull <tag> && docker inspect --format '{{.Id}}'`.
- **R4: CI workflow requires repo secrets not yet configured.** — **Mitigation:** Task 4 PR notes which secrets must be configured in GitHub settings (`STAGING_SSH_HOST`, `STAGING_SSH_USER`, `STAGING_SSH_KEY`, `GHCR_TOKEN`, `SMOKE_TEST_JWT`); Gate will verify the workflow file exists and is syntactically valid but CANNOT run it end-to-end without those secrets. Story acceptance for the staging-deploy stage is documented as "workflow runs dry OK; actual staging deploy exercised in next environment setup".
- **R5: Nginx reload racing with in-flight connections.** Drain window of 15s may be insufficient for long-lived WebSocket connections. — **Mitigation:** `bluegreen-flip.sh` accepts `DRAIN_SECONDS` env; default 15, recommend 60s for WS-heavy loads. Documented in `deploy.md`.
- **R6: `argus_build_info` ldflags injection.** If Dockerfile build fails to pass ldflags correctly, metric shows empty labels. — **Mitigation:** Task 3 verify step asserts non-empty labels; Task 4 smoke-test.sh asserts `data.version != ""` on `/api/v1/status`.
- **R7: Blue-green compose share database — partial deploy can see DB migrated by new color before old color stopped.** If migrations are incompatible (columns dropped), old color may error. — **Mitigation:** Runbook `deploy.md` documents forward-compatible migration policy (add-only); rollback.md documents `--with-db-restore` for breaking migrations.

## Pre-Validation & Quality Gate Self-Check

### Minimum substance (story effort = M-L)
- [x] Plan lines ≥ 100 (well over 300).
- [x] Task count ≥ 5 (11 tasks).

### Required sections
- [x] `## Goal`
- [x] `## Architecture Context`
- [x] `## Tasks` with numbered `### Task` blocks
- [x] `## Acceptance Criteria Mapping`

### Embedded specs
- [x] API endpoint (`GET /api/v1/status`) fully specified with request/response shape + status codes.
- [x] No DB schema changes — explicitly noted "No new tables. No migrations."
- [x] No UI — explicitly noted "No screen mockups — backend/infra story."

### Task complexity cross-check (Story effort M-L → at least 1 high)
- [x] Task 7 (argusctl scaffold + core commands) marked **high** complexity.

### Context refs validation
- [x] All Context refs point to sections that exist in this plan (`Architecture Context > ...`, `API Specifications > ...`, `Phase 10 Zero-Deferral Charter`, `Prerequisites`).

### Architecture Compliance
- [x] Files per task in correct layers (`.github/workflows` = CI; `infra/docker`, `deploy/` = infra; `cmd/argusctl` = new binary; `internal/api/system` = API handler; `docs/runbook` = docs).
- [x] No cross-layer imports planned (argusctl only consumes HTTP; server-side code unchanged in client direction).
- [x] Component names match existing conventions (handler suffix, `cmd/<binary>/main.go` entry).

### API Compliance
- [x] `/api/v1/status` uses standard envelope.
- [x] Proper HTTP method (GET — read-only).
- [x] No input validation needed (no body).
- [x] Error responses: handler recovers panics via middleware; 503 for unhealthy.

### Database Compliance
- [x] No DB changes — section explicit.

### UI Compliance
- [x] No UI — no design tokens or component table needed.

### Task Decomposition
- [x] Each task touches ≤ 3 files for most; Task 7 is larger (CLI scaffold) but bounded to one new binary directory; split rationale justified (Task 7 = core, Task 8 = extensions).
- [x] DB/foundation tasks first (digest pin, Makefile, metric), then CI + deploy layer, then CLI, then status endpoint, then docs/tests.
- [x] Each task has `Depends on` field.
- [x] Each task has `Context refs` field.
- [x] Each task creating new files has `Pattern ref` field.
- [x] Independent tasks within each wave can be parallelized (Wave 1, 2, 3, 6).
- [x] Total task count 11 — reasonable for M-L story.
- [x] No implementation code in tasks — specs + pattern refs only.

### Test Compliance
- [x] Task 11 covers every story test scenario; per-task verify steps provide unit-level checks.
- [x] Test file paths specified (`test/e2e/story067_*`, `*_test.go` alongside each new file).

### Self-Containment
- [x] API spec embedded.
- [x] No DB schema to embed — noted source.
- [x] No screens — explicit.
- [x] Business rules stated inline (audit on deploy, confirm-gate on destructive CLI, ADR-001 compliance for CLI).
- [x] Every task's `Context refs` resolves to a real section in this plan.

**Result: PASS** — all checks green.

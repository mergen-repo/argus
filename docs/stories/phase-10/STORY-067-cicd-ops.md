# STORY-067: CI/CD Pipeline, Deployment Strategy & Ops Tooling

## User Story
As a release manager and on-call engineer, I want a real CI/CD pipeline with lint/test/build/deploy gates, digest-pinned images, a blue-green deploy mechanism, an automated rollback path, an admin CLI for operational tasks, and a runbook directory covering every incident class, so that shipping code is predictable and on-call has tools instead of guesswork.

## Description
Current state: no CI/CD (`.github/workflows/` empty), base images use floating tags (`postgres:16`, `redis:7-alpine`), `make deploy-prod` is `build && up` with no gates or rollback, no admin CLI (all operational tasks require API calls), no runbook. This is unsuitable for an enterprise product. This story builds the release, deploy, and ops tooling layer.

## Architecture Reference
- Packages: Makefile, deploy/, infra/, cmd/argusctl (new), .github/workflows (new)
- Source: Phase 10 production ops audit (6-agent scan 2026-04-11), STORY-065 dashboards/alerts feed into runbooks

## Screen Reference
- Not directly tied to screens — backend/infra story.
- `/api/v1/status` overview endpoint added (SCR-120 linked).

## Acceptance Criteria
- [ ] AC-1: **GitHub Actions CI pipeline** at `.github/workflows/ci.yml`. Triggers on PR + push to main.
  - Stage 1: Lint — `golangci-lint run`, `cd web && npm run lint && npm run type-check`
  - Stage 2: Test — `go test ./... -race -cover`, `cd web && npm test`
  - Stage 3: Security — `govulncheck ./...`, `npm audit --audit-level=high`, `gosec ./...`
  - Stage 4: Build — Docker build with digest-pinned base images, push to registry with SHA tag
  - Stage 5: (Main only) Deploy to staging, run smoke tests, tag release candidate
  - Fail-fast on any stage; required for merge to main
- [ ] AC-2: **Base image digest pinning.** All `FROM` statements in `infra/docker/Dockerfile.argus`, `deploy/docker-compose.yml` services use `image@sha256:...` digests. Script `infra/scripts/update-digests.sh` fetches latest digests for pinned tags and updates files. Run monthly or on security advisory.
- [ ] AC-3: **Blue-green deployment strategy.**
  - `deploy/docker-compose.blue.yml` + `.green.yml` stacks on different ports
  - Nginx upstream switches between blue/green via `infra/nginx/upstream.conf` include
  - Deploy procedure: bring up new stack alongside current, smoke test, flip Nginx, drain old, tear down
  - Documented in `docs/runbook/deploy.md`
- [ ] AC-4: **Automated rollback.**
  - Each deploy snapshots: previous image SHA, DB backup checkpoint, config version
  - `make rollback VERSION=<prev>` restores image + optionally DB (`--with-db-restore` flag)
  - Rollback verified with post-rollback smoke test
  - Rollback history logged to audit
- [ ] AC-5: **Admin CLI tool** at `cmd/argusctl/main.go`, built as `argusctl` binary.
  - `argusctl tenant create --name=foo --admin-email=x@y` — creates tenant + admin user, emits temp password
  - `argusctl tenant list`, `argusctl tenant suspend <id>`, `argusctl tenant resume <id>`
  - `argusctl apikey rotate --tenant=X --key=Y` — generates new key, invalidates old after grace period
  - `argusctl user purge --tenant=X --user=Y --confirm` — GDPR right-to-erasure + audit log
  - `argusctl compliance export --tenant=X --format=pdf --from=... --to=...` — pulls compliance report, writes to file
  - `argusctl sim bulk-op --tenant=X --operation=suspend --segment=Y` — triggers bulk job, tails progress
  - `argusctl health` — prints full health state (replaces raw /health curl)
  - `argusctl backup restore --from=<backup-file>` — guided restore
  - All commands authenticate via short-lived admin JWT or mTLS
- [ ] AC-6: **Runbook directory** at `docs/runbook/` with playbooks:
  - `db-full.md` — disk space full, purge strategy, emergency archive
  - `nats-lag.md` — NATS consumers backing up, diagnosis + remediation
  - `latency-spike.md` — p99 API latency high, triage checklist with Grafana dashboard links
  - `session-loss.md` — mass session disconnect, RADIUS storm recovery
  - `operator-down.md` — upstream operator unreachable, failover + comms
  - `deploy.md` — blue-green deploy procedure
  - `rollback.md` — rollback procedure
  - `dr-pitr.md` — disaster recovery + point-in-time restore
  - `tenant-suspend.md` — legal/compliance-driven tenant suspension
  - `cert-rotation.md` — JWT key rotation (once TLS added, also TLS cert rotation)
  - Each references specific Grafana dashboard panels and Prometheus queries
- [ ] AC-7: **Status endpoint** `GET /api/v1/status` — overall health aggregated from all dependencies, recent error count, current deploy version (SHA), uptime, active tenant count. Public (no auth) for status page consumption; sensitive details gated.
- [ ] AC-8: **Deploy tagging.** Every successful deploy sets:
  - Git tag `deploy-<env>-<timestamp>`
  - Prometheus metric `argus_build_info{version,git_sha,build_time}`
  - Audit entry with deployer, git SHA, changelog snippet
- [ ] AC-9: **Makefile targets rationalized:**
  - `make lint` — runs all lint stages locally
  - `make test` — runs all tests locally
  - `make security-scan` — runs govulncheck + npm audit + gosec
  - `make deploy-staging` — staging deploy (used by CI)
  - `make deploy-prod` — prod deploy with confirmation prompt + pre-flight checks
  - `make rollback VERSION=...` — rollback
  - `make ops-status` — prints status from /api/v1/status
  - All targets help-documented (`make help`)

## Dependencies
- Blocked by: STORY-065 (metrics must exist for alerts/dashboards), STORY-066 (backup/rollback needs health probes)
- Blocks: Phase 10 Gate, Documentation Phase (runbooks feed into ops docs)

## Test Scenarios
- [ ] CI: PR with lint error → CI fails, PR blocked.
- [ ] CI: PR with failing test → CI fails with clear log.
- [ ] CI: Merge to main → deploys to staging, runs smoke, tags RC.
- [ ] Integration: Blue-green flip during low traffic → 0 dropped requests verified via load test (wrk 100 req/s during switch).
- [ ] Integration: `make rollback VERSION=v1.2.3` → previous image running, smoke test green.
- [ ] CLI: `argusctl tenant create --name=acme --admin-email=ops@acme.io` → tenant exists, admin user created, temp password printed.
- [ ] CLI: `argusctl compliance export --tenant=acme --format=pdf --from=2026-01-01 --to=2026-03-31` → PDF file written, contains expected sections.
- [ ] CLI: `argusctl backup restore --from=backups/2026-04-10.sql` → DB restored, smoke test passes.
- [ ] Ops: Run latency-spike runbook step-by-step during simulated load test → runbook is accurate and sufficient.

## Effort Estimate
- Size: M-L
- Complexity: Medium (mostly glue, runbook writing is time-consuming)

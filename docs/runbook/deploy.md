# Standard Blue-Green Deploy Procedure

## When to use

- Deploying a new Argus release to staging or production
- Scheduled maintenance release with zero-downtime requirement
- Hotfix deployment requiring immediate rollout with rollback capability
- Any production deployment that must keep existing sessions alive during cutover

## Prerequisites

- `docker`, `docker compose` installed on operator machine
- `make` available in project root
- AWS credentials with ECR push access
- Argus git repository cloned and at the correct release commit/tag
- `deploy/bluegreen-flip.sh` script present and executable
- `deploy/deploy-snapshot.sh` script present and executable
- Staging environment available and passing smoke tests before production deploy
- Access to `http://localhost:8084/health/ready` and Grafana dashboard

## Estimated Duration

| Step | Expected time |
|------|---------------|
| Step 1 — Pre-deploy checks | 5–10 min |
| Step 2 — Deploy to staging | 10–15 min |
| Step 3 — Staging smoke tests | 5–10 min |
| Step 4 — Take pre-prod snapshot | 2–3 min |
| Step 5 — Deploy to production | 5–10 min |
| Step 6 — Blue-green flip | 2–3 min |
| Step 7 — Post-deploy verification | 5–10 min |
| **Total** | **~35–60 min** |

---

## Procedure

### 1. Pre-deploy checks

```bash
# Confirm you are on the correct release commit or tag
git log --oneline -5
# Expected: HEAD is at the intended release commit

# Confirm the release tag
git describe --tags
# Expected: e.g., v1.5.0

# Check that the current production stack is healthy before starting
curl -sf http://localhost:8084/health/ready | jq
# Expected: {"status":"ok", ...}

# Check there are no ongoing alerts
curl -s 'http://localhost:9090/api/v1/alerts' | jq '.data.alerts[] | select(.state == "firing") | {alertname: .labels.alertname, state: .state}'
# Expected: no firing alerts (or only known/acknowledged ones)

# Confirm database migrations are compatible (check for additive-only changes)
ls -la migrations/*.up.sql | sort | tail -5
# Expected: the latest migration files that will run with this release

# Notify the team before starting
# Post to incident channel: "Starting deploy of <version> to <staging|production> at $(date -u)"
```

### 2. Deploy to staging

```bash
# Build and deploy to staging environment
make deploy-staging
# This runs: build → push to ECR → deploy-staging docker-compose → migrate → health check
# Expected: "Staging deploy complete. Health: OK" — takes ~10–15 minutes

# Alternatively, use explicit make targets:
make web-build
make docker-build VERSION=$(git describe --tags)
make docker-push VERSION=$(git describe --tags)
# Expected: image pushed to registry with the version tag

# Run staging deployment
DEPLOY_ENV=staging ARGUS_VERSION=$(git describe --tags) \
  docker compose -f deploy/docker-compose.staging.yml up -d
# Expected: all containers start successfully

# Run database migrations on staging
make db-migrate DEPLOY_ENV=staging
# Expected: migrations applied, "N migrations applied" message

# Wait for staging argus to be healthy
docker compose -f deploy/docker-compose.staging.yml exec argus \
  sh -c 'for i in $(seq 30); do curl -sf http://localhost:8080/health/ready && break || sleep 2; done'
# Expected: healthy response within 60 seconds
```

### 3. Staging smoke tests

```bash
# Run the smoke test suite against staging
SMOKE_TARGET=http://staging:8084 bash deploy/smoke-test.sh
# Expected: all checks pass (exit 0)

# Manual spot-check on staging:
# - Login with admin credentials
# - List SIMs for a test tenant
# - Verify metrics endpoint is serving
curl -sf http://staging:8084/health/ready | jq
curl -sf http://staging:8084/api/v1/status | jq

# If staging smoke tests fail, STOP here. Fix the issue and redeploy to staging.
# Do NOT proceed to production if staging smoke tests fail.
```

### 4. Take a pre-production deploy snapshot

Before flipping production, take a deploy snapshot for rollback capability. This captures the current production image tags and database state.

```bash
# Take a deploy snapshot (captures current image tags + DB snapshot reference)
bash deploy/deploy-snapshot.sh --env=production --label="pre-$(git describe --tags)"
# Expected: snapshot saved to deploy/snapshots/production-pre-<version>-<timestamp>.json
# Expected output: "Snapshot created: deploy/snapshots/production-pre-v1.5.0-20260412140000.json"

ls -la deploy/snapshots/
# Expected: new snapshot file present

# Also take a database backup via the existing backup job
argusctl job run backup-full
# Expected: backup job queued; confirm completion in argus_backup_last_success_timestamp_seconds
```

### 5. Deploy to production

```bash
# Pull and start the new container image in the "blue" slot
# (bluegreen-flip.sh handles the actual cutover — this step only starts the new version)
ARGUS_VERSION=$(git describe --tags) \
  docker compose -f deploy/docker-compose.yml \
  up -d --no-deps --no-recreate argus-blue
# Expected: argus-blue container starts with the new image

# Run database migrations (always forward-only — never run down migrations on production)
make db-migrate
# Expected: migrations applied cleanly; if any migration fails, STOP and rollback via rollback.md

# Wait for the new container to pass its health check
docker compose -f deploy/docker-compose.yml exec argus-blue \
  sh -c 'for i in $(seq 60); do curl -sf http://localhost:8080/health/ready && break || sleep 2; done'
# Expected: healthy response within 120 seconds

# Smoke test the blue slot directly (before cutting over nginx)
curl -sf http://localhost:8082/health/ready | jq
# Expected: {"status":"ok", ...}
```

### 6. Blue-green flip

Flip nginx to route production traffic to the new "blue" slot.

```bash
# Execute the blue-green flip script
bash deploy/bluegreen-flip.sh --to=blue --env=production
# This script:
#   1. Updates the nginx upstream to point to argus-blue
#   2. Reloads nginx (zero-downtime via SIGUSR2)
#   3. Runs smoke tests against the live endpoint
#   4. If smoke tests fail: automatically rolls back nginx to previous slot
# Expected: "Flip complete: production now serving from blue slot"

# Verify the flip was applied
curl -sf http://localhost:8084/health/ready | jq
# Expected: {"status":"ok", "version": "<new_version>", ...}

# Check the version header to confirm new code is serving
curl -sI http://localhost:8084/api/v1/status | grep -i 'x-argus-version'
# Expected: X-Argus-Version: <new_version>
```

### 7. Post-deploy verification

```bash
# Run the full smoke test suite against production
bash deploy/smoke-test.sh
# Expected: all checks pass (exit 0)

# Check Prometheus for elevated error rates post-deploy (watch for 5 minutes)
curl -s 'http://localhost:9090/api/v1/query?query=rate(argus_http_requests_total%7Bstatus%3D~"5.."%7D%5B2m%5D)' | \
  jq '[.data.result[] | {route: .metric.route, error_rps: .value[1]}] | sort_by(.error_rps | tonumber) | reverse | .[0:5]'
# Expected: all values near 0 (no elevated 5xx rate)

# Check p99 latency is normal
curl -s 'http://localhost:9090/api/v1/query?query=histogram_quantile(0.99%2C+rate(argus_http_request_duration_seconds_bucket%5B5m%5D))' | \
  jq '[.data.result[] | {route: .metric.route, p99_ms: (.value[1] | tonumber * 1000 | round)}] | sort_by(.p99_ms) | reverse | .[0:5]'
# Expected: all routes < 500ms

# Verify active sessions were maintained through the flip
curl -s 'http://localhost:9090/api/v1/query?query=argus_active_sessions' | \
  jq '[.data.result[] | {tenant: .metric.tenant_id, sessions: .value[1]}]'
# Expected: session counts near pre-deploy baseline (no session loss during flip)

# Check Grafana overview dashboard
# Open: <grafana>/d/argus-overview
# Verify: no spikes in error rate, latency, or session drops at the flip timestamp

# Create audit log entry
argusctl audit log \
  --action=deploy \
  --resource=system \
  --note="Deploy $(git describe --tags) to production. Blue-green flip at $(date -u +%Y-%m-%dT%H:%M:%SZ). Smoke tests: pass."
# Expected: Audit log entry created
```

**If post-deploy verification shows elevated errors or latency:** immediately follow `rollback.md`.

---

## Verification

- `curl http://localhost:8084/health/ready` returns 200 with new version
- `curl http://localhost:8084/api/v1/status` returns 200
- `rate(argus_http_requests_total{status=~"5.."}[2m])` < 0.01 across all routes
- `histogram_quantile(0.99, rate(argus_http_request_duration_seconds_bucket[5m]))` < 0.5 for all routes
- `argus_active_sessions` at pre-deploy baseline (within 5%)
- All smoke tests passing

---

## Post-incident / Rollback

If the deploy introduces regressions, immediately follow `rollback.md` using the snapshot taken in Step 4.

---

## Post-deploy

- Announce deploy success in the team channel: `"Deploy <version> complete at <time>. All checks passing."`
- Tag the production deploy in your tracking system
- Archive the pre-deploy snapshot file to the deploy history log
- Monitor Grafana dashboard for 30 minutes post-deploy before closing the change window

## Related Runbooks

- [rollback.md](rollback.md) — Rollback procedure if this deploy causes issues
- [dr-pitr.md](dr-pitr.md) — Point-in-time recovery if a migration caused data corruption

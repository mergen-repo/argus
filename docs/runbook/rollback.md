# Rollback Procedure

## When to use

- Post-deploy verification failed (elevated errors, latency spike, session loss)
- A regression is detected within the deploy change window (typically < 2 hours post-deploy)
- A critical bug is found in production that requires reverting to the previous version
- A database migration needs to be reversed (see DB Rollback path below)

## Prerequisites

- `docker`, `docker compose` on operator machine
- `make` available in project root
- `deploy/rollback.sh` script present and executable
- `deploy/bluegreen-flip.sh` script present and executable
- Deploy snapshots available: `ls deploy/snapshots/*.json`
- Git tag or image tag of the previous stable version known
- If DB rollback needed: access to `dr-pitr.md` runbook and S3 WAL archive

## Estimated Duration

| Step | Expected time |
|------|---------------|
| Step 1 — Decide rollback scope | 2–3 min |
| Step 2 — Application rollback via script | 5–10 min |
| Step 3 — Verify rollback | 5 min |
| Step 4 (if needed) — DB rollback (PITR) | 1–2 hours |
| **Total (app only)** | **~15–20 min** |
| **Total (app + DB)** | **~2–2.5 hours** |

---

## Procedure

### 1. Decide rollback scope

```bash
# Identify the previous stable version from git tags
git tag --sort=-version:refname | head -10
# Expected: list of version tags; choose the last known-good version

# Identify the snapshot file from the failed deploy (taken in deploy.md Step 4)
ls -la deploy/snapshots/ | sort -k6,7 | tail -5
# Expected: snapshot files with timestamps; use the most recent pre-<version> snapshot

# Review the snapshot to confirm the previous image tag
cat deploy/snapshots/production-pre-<version>-<timestamp>.json | jq .
# Expected: JSON with previous_image_tag, previous_db_migration, deploy_timestamp

# Determine if DB rollback is needed:
# - If the failed deploy DID NOT include new migrations → app-only rollback (Steps 2–3)
# - If the failed deploy DID include new migrations → app rollback + DB PITR (Steps 2–4)
# Check which migrations ran:
argusctl migration status
# Expected: lists applied migrations; compare to what the previous version expected
```

### 2. Application rollback via rollback script

```bash
# Rollback using make target with explicit version tag
VERSION="<previous_stable_version>"
make rollback VERSION=${VERSION}
# This invokes deploy/rollback.sh internally
# Expected: "Rollback to ${VERSION} initiated..."

# Alternatively, invoke rollback.sh directly
bash deploy/rollback.sh --version=${VERSION} --env=production
# Expected script actions:
#   1. Pulls the previous image from registry
#   2. Starts argus-green slot with the previous image
#   3. Waits for health check to pass
#   4. Flips nginx back to green slot via bluegreen-flip.sh
#   5. Runs smoke tests
#   6. Stops and removes the failed blue slot
# Expected output: "Rollback complete: production now serving <VERSION>"

# If rollback.sh is not available, perform manually:
# Step 2a: Start previous version in green slot
ARGUS_VERSION=${VERSION} \
  docker compose -f deploy/docker-compose.yml \
  up -d --no-deps --no-recreate argus-green

# Step 2b: Wait for green slot health
docker compose -f deploy/docker-compose.yml exec argus-green \
  sh -c 'for i in $(seq 60); do curl -sf http://localhost:8080/health/ready && break || sleep 2; done'

# Step 2c: Flip nginx back to green
bash deploy/bluegreen-flip.sh --to=green --env=production
# Expected: "Flip complete: production now serving from green slot"

# Step 2d: Stop the failed blue slot
docker compose -f deploy/docker-compose.yml stop argus-blue
```

### 3. Verify rollback

```bash
# Confirm the running version is the rollback target
curl -sf http://localhost:8084/health/ready | jq
# Expected: {"status":"ok", ...}

curl -sI http://localhost:8084/api/v1/status | grep -i 'x-argus-version'
# Expected: X-Argus-Version: <VERSION> (the rolled-back version)

# Run smoke tests
bash deploy/smoke-test.sh
# Expected: all checks pass (exit 0)

# Check error rate has normalized
curl -s 'http://localhost:9090/api/v1/query?query=rate(argus_http_requests_total%7Bstatus%3D~"5.."%7D%5B2m%5D)' | \
  jq '[.data.result[] | {route: .metric.route, error_rps: .value[1]}]'
# Expected: all values near 0

# Check p99 latency is back to normal
curl -s 'http://localhost:9090/api/v1/query?query=histogram_quantile(0.99%2C+rate(argus_http_request_duration_seconds_bucket%5B5m%5D))' | \
  jq '[.data.result[] | {route: .metric.route, p99_ms: (.value[1] | tonumber * 1000 | round)}] | sort_by(.p99_ms) | reverse | .[0:5]'
# Expected: all routes below 500ms

# Confirm active sessions are stable
curl -s 'http://localhost:9090/api/v1/query?query=argus_active_sessions' | \
  jq '[.data.result[] | {tenant: .metric.tenant_id, sessions: .value[1]}]'
# Expected: session counts stable

# Audit log entry
argusctl audit log \
  --action=rollback \
  --resource=system \
  --note="Rolled back to ${VERSION} from <failed_version>. Reason: <brief_reason>. Smoke tests: pass."
# Expected: Audit log entry created
```

### 4. Database rollback (PITR) — only if migrations must be reversed

**This is a major operation requiring full disaster recovery. Estimated time: 1–2 hours.**

If the failed deploy ran database migrations that need to be reversed, application rollback alone is not sufficient. The database must be restored to a point-in-time before the migration ran.

```bash
# 1. Identify the timestamp when the migration ran (from argus logs)
docker compose -f deploy/docker-compose.yml logs argus | grep -i 'migration' | grep -i 'applied'
# Expected: log line with timestamp when the migration was applied
# Note this timestamp — you will recover to just before it

# 2. Confirm the pre-deploy backup is available
aws s3 ls s3://argus-backup/daily/ --human-readable | sort | tail -5
# Expected: a backup taken before the deploy (from deploy.md Step 4)

# 3. Identify the snapshot file for PITR reference
cat deploy/snapshots/production-pre-<version>-<timestamp>.json | jq .previous_db_migration
# Expected: the last migration version that was applied before this deploy
```

Now follow the full PITR procedure: **see [dr-pitr.md](dr-pitr.md)** — use the timestamp identified above as the `recovery_target_time`.

Key difference from a normal PITR: after DB recovery, you also need the application rolled back (done in Step 2 above). Ensure both are at the same version before bringing services back up.

```bash
# After completing PITR, bring services back up in the correct order
docker compose -f deploy/docker-compose.yml up -d postgres
# Wait for postgres to be healthy, then:
docker compose -f deploy/docker-compose.yml up -d argus nginx

# Verify the database schema matches the rolled-back application version
argusctl migration status
# Expected: last applied migration matches <VERSION>'s expected schema version

# Full health check
curl -sf http://localhost:8084/health/ready | jq
# Expected: {"status":"ok", ...}
```

---

## Verification

- `curl http://localhost:8084/health/ready` returns 200
- `curl http://localhost:8084/api/v1/status` returns 200 with correct version
- All smoke tests pass: `bash deploy/smoke-test.sh`
- `rate(argus_http_requests_total{status=~"5.."}[2m])` near 0
- `argus_active_sessions` stable at pre-incident baseline
- Database schema at correct version: `argusctl migration status`

---

## Post-incident

- Audit log entry: `argusctl audit log --action=rollback_complete --resource=system --note="Rolled back to <version>. DB rollback: <yes|no>. Total downtime: <N>min."`
- Open a bug report on the failed version with: reproduction steps, metrics evidence, migration state at failure
- Block the failed version tag in the release pipeline until the bug is fixed
- Schedule a post-mortem within 48 hours
- **Comms template (incident channel):**
  > `[RESOLVED] Production rolled back to <version>. <version_failed> caused <issue>. Rollback completed in <N> minutes. All smoke tests passing. <DB rollback was/was not required>. Post-mortem scheduled for <date>.`
- **Stakeholder email:**
  > Subject: [Argus] Production rollback completed — <version_failed> reverted
  > Body: Deployment of <version_failed> at <time> caused <issue description>. The system was rolled back to <version> at <time>. Service impact: <N> minutes of <elevated errors | degraded performance | brief outage>. Root cause investigation in progress. Post-mortem: <date>.

## Related Runbooks

- [deploy.md](deploy.md) — Standard deploy procedure (produces the snapshot used here)
- [dr-pitr.md](dr-pitr.md) — Point-in-time recovery for database rollback path

# Emergency Procedures — Argus Kill Switches

> **Scope**: FIX-245. Kill switches are environment-variable-backed circuit breakers for immediate, no-deploy mitigation of operational incidents.
> **Architecture reference**: `docs/architecture/CONFIG.md` — Kill Switches section.
> **Implementation**: `internal/killswitch/service.go`

---

## 1. When to Use Kill Switches

| Scenario | Recommended switch |
|----------|--------------------|
| DDoS / volumetric RADIUS flood saturating AAA workers | `KILLSWITCH_RADIUS_AUTH` |
| Runaway session-create loop exhausting DB connections | `KILLSWITCH_SESSION_CREATE` |
| Bulk import job storm overwhelming job queue | `KILLSWITCH_BULK_OPERATIONS` |
| Email/Telegram notification storm (spam, misconfigured alert rule) | `KILLSWITCH_EXTERNAL_NOTIFICATIONS` |
| Emergency read-only lockdown (billing protection, audit hold, pre-maintenance) | `KILLSWITCH_READ_ONLY_MODE` |
| All of the above simultaneously (full incident freeze) | All five switches |

Kill switches are **not** a substitute for fixing the root cause. They buy time for diagnosis and remediation without a code deployment.

---

## 2. How to Toggle

### Docker Compose (standard dev/staging/production)

```bash
# 1. Add the variable to deploy/.env
echo "KILLSWITCH_RADIUS_AUTH=on" >> deploy/.env

# 2. Restart only the argus service (no downtime on other services)
docker compose restart argus

# 3. Verify: tail logs for confirmation
docker compose logs -f argus | grep -i "killswitch"
```

### Systemd (bare-metal / VM production)

```bash
# 1. Edit the unit's EnvironmentFile or override
systemctl edit argus
# Add under [Service]:
#   Environment="KILLSWITCH_RADIUS_AUTH=on"

# 2. Reload and restart
systemctl daemon-reload
systemctl restart argus

# 3. Verify
journalctl -u argus -f | grep -i "killswitch"
```

### No-restart path (30s propagation)

The `EnvReader` TTL cache is 30 seconds. If the process is started with the env var already set, no restart is needed — toggle propagates within one cache window. To exploit this in Docker without restart:

```bash
# Docker exec approach (does NOT persist across container restart)
docker exec argus env KILLSWITCH_RADIUS_AUTH=on /proc/1/exe  # NOT recommended for production
# Preferred: always use the .env file + restart pattern above
```

---

## 3. Per-Switch Effect and Verification

### `KILLSWITCH_RADIUS_AUTH`

- **Effect**: All incoming RADIUS `Access-Request` packets are answered with `Access-Reject`. Accounting packets (Start/Stop/Interim) are unaffected.
- **Verification**:
  ```bash
  # Check RADIUS reject counter spike
  curl -s http://localhost:9090/metrics | grep 'argus_aaa_radius_auth_total{result="reject"}'

  # Or send a test Access-Request and verify reject response
  radtest testuser testpass localhost 1812 testing123
  # Expected: Access-Reject
  ```

### `KILLSWITCH_SESSION_CREATE`

- **Effect**: `RADIUS Accounting-Start` and 5G SBA UE-Registration calls that would create new sessions are dropped with a log warning. Existing sessions continue. Session termination (Stop) is unaffected.
- **Verification**:
  ```bash
  # No new rows in sessions table
  docker exec argus-db psql -U argus argus_dev -c "SELECT count(*) FROM sessions WHERE created_at > now() - interval '1 minute';"
  # Expected: 0 new sessions while switch is active
  ```

### `KILLSWITCH_BULK_OPERATIONS`

- **Effect**: All bulk API endpoints return `HTTP 503` with `{"error": {"code": "KILLSWITCH_ACTIVE", "message": "Bulk operations are temporarily disabled."}}`. Affected endpoints: `POST /api/v1/sims/bulk-*`, `POST /api/v1/esim-profiles/bulk-switch`, all import job creation endpoints.
- **Verification**:
  ```bash
  curl -s -o /dev/null -w "%{http_code}" \
    -X POST http://localhost:8084/api/v1/sims/bulk-suspend \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"filter":{"state":"active"}}'
  # Expected: 503
  ```

### `KILLSWITCH_EXTERNAL_NOTIFICATIONS`

- **Effect**: The notification dispatch worker skips all outbound delivery (SMTP, Telegram, webhook HTTP calls). Notification records are still written to the DB — they accumulate with `status=suppressed`. In-app WebSocket notifications are unaffected.
- **Verification**:
  ```bash
  # Check Mailhog — no new emails should appear
  curl -s http://localhost:8025/api/v2/messages | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['total'])"
  # Expected: count should not increase while switch is active

  # Check notification status in DB
  docker exec argus-db psql -U argus argus_dev -c \
    "SELECT status, count(*) FROM notification_logs WHERE created_at > now() - interval '5 minutes' GROUP BY status;"
  # Expected: suppressed count rising, dispatched count static
  ```

### `KILLSWITCH_READ_ONLY_MODE`

- **Effect**: All state-changing HTTP methods (`POST`, `PUT`, `PATCH`, `DELETE`) return `HTTP 503` with `{"error": {"code": "READ_ONLY_MODE", "message": "System is in read-only mode."}}`. Read operations (`GET`, WebSocket subscriptions) pass through normally.
- **Verification**:
  ```bash
  # Write attempt should fail
  curl -s -o /dev/null -w "%{http_code}" \
    -X PATCH http://localhost:8084/api/v1/sims/some-id \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"state":"suspended"}'
  # Expected: 503

  # Read should succeed
  curl -s -o /dev/null -w "%{http_code}" \
    http://localhost:8084/api/v1/sims \
    -H "Authorization: Bearer $TOKEN"
  # Expected: 200
  ```

---

## 4. Rollback

```bash
# 1. Remove the variable from deploy/.env
# Edit deploy/.env and delete the KILLSWITCH_* line(s)
grep -v 'KILLSWITCH_' deploy/.env > deploy/.env.tmp && mv deploy/.env.tmp deploy/.env

# 2. Restart the service
docker compose restart argus

# 3. Verify normal operation
curl -s http://localhost:9090/metrics | grep 'argus_aaa_radius_auth_total{result="accept"}'
# Expected: accept counter incrementing again
```

For systemd:

```bash
systemctl edit argus
# Remove the Environment="KILLSWITCH_*" lines
systemctl daemon-reload
systemctl restart argus
```

---

## 5. Escalation Checklist

When activating a kill switch in production, follow this sequence:

- [ ] Identify incident scope — which switch(es) address the problem
- [ ] Activate kill switch(es) as above
- [ ] Verify effect with the smoke commands in Section 3
- [ ] Notify on-call team (Slack `#incidents` or PagerDuty)
- [ ] Create incident ticket — record activation time, switch(es) activated, observed symptoms
- [ ] Diagnose root cause (do NOT deactivate until root cause understood)
- [ ] Apply fix or configuration change
- [ ] Deactivate kill switch(es) per Section 4 — Rollback
- [ ] Confirm normal operation restored
- [ ] Write post-mortem within 24h

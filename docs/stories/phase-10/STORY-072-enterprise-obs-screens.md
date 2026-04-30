# STORY-072: Enterprise Observability Screens

## User Story
As an SRE and NOC operator monitoring a 10M+ SIM platform, I want dedicated screens for performance, error drill-down, real-time AAA traffic, message bus health, database health, job queue observability, backup status, deploy history, incident timeline, and alert acknowledgment, so that I can diagnose, triage, and resolve production issues without leaving the Argus portal.

## Description
Enterprise screen audit found 10+ critical observability screens completely missing. Alerts page exists but lacks ack/resolve/escalate UX. No performance dashboard, no error drill-down, no real-time traffic monitor, no NATS/DB/Redis health detail, no backup status, no deploy history. This story creates the full ops screen layer, consuming metrics from STORY-065 (OpenTelemetry + Prometheus).

## Architecture Reference
- Services: SVC-01 (Gateway — metrics), SVC-04 (AAA — traffic), SVC-09 (Job Runner)
- Packages: web/src/pages/ops/* (new directory), internal/api/ops/* (new endpoints for ops data)
- Source: Phase 10 enterprise screen audit (2026-04-11)
- Depends on: STORY-065 (metrics must be Prometheus-native), STORY-066 (backup status API), STORY-067 (deploy history API)

## Screen Reference
- SCR-130: Performance Dashboard (new)
- SCR-131: Error Rate Drill-down (new)
- SCR-132: Real-time AAA Traffic Monitor (new)
- SCR-133: NATS Message Bus Health (new)
- SCR-134: Database Health Detail (expand SCR-120)
- SCR-135: Redis Cache Health (new)
- SCR-136: Job Queue Observability (expand SCR-080)
- SCR-137: Backup & Restore Status (new)
- SCR-138: Deploy History / Change Log (new)
- SCR-139: Incident Timeline (new)
- SCR-074+: Alert Acknowledgment UX (expand existing /alerts)

## Acceptance Criteria
- [ ] AC-1: **Performance Dashboard (SCR-130):** Slow queries (top 10 by duration, p95/p99 breakdown), API latency by endpoint (heatmap or bar chart, percentiles), hot endpoints (top 10 by request count), goroutine count, memory usage, GC stats. Data from Prometheus queries. Auto-refresh 15s.
- [ ] AC-2: **Error Rate Drill-down (SCR-131):** Error rate per tenant × endpoint × status code × time window. Drill from global → tenant → endpoint → individual errors. Time-series chart + filterable table. Link from error to audit log entry. Data from Prometheus `argus_http_requests_total{status=~"5.."}`.
- [ ] AC-3: **Real-time AAA Traffic Monitor (SCR-132):** Live RADIUS/Diameter/5G SBA req/s gauge + time series. Per-protocol breakdown. Auth success/fail ratio. Current active sessions gauge. p99 auth latency. WebSocket-fed for real-time feel. "Traffic spike" visual indicator.
- [ ] AC-4: **NATS Bus Health (SCR-133):** Per-subject pending message count. Consumer lag per stream. Subscriber count. Slow consumer warnings. Dead-letter queue depth. Historical chart (last 1h). Alert link if threshold exceeded.
- [ ] AC-5: **Database Health Detail (SCR-134):** Connection pool utilization (idle/in_use/waiting). Slow query log (last 50, sortable by duration). Lock contention. Replication lag (if replica configured). Table sizes. Partition status (existing + next auto-create date). Continuous aggregate refresh status.
- [ ] AC-6: **Redis Cache Health (SCR-135):** Ops/sec, hit rate, miss rate, eviction rate. Memory usage vs maxmemory. Key count by namespace. Connected clients. Latency percentiles. Historical charts.
- [ ] AC-7: **Job Queue Observability (expand SCR-080):** Queue depth gauge. Active workers. Success/failure rate time series. Retry count histogram. Average duration by job type. Stuck jobs (running > 2× expected duration). Dead-letter jobs.
- [ ] AC-8: **Backup & Restore Status (SCR-137):** Last successful backup timestamp + size. Backup schedule. Retention policy (days kept). Last verification result (PASS/FAIL). Restore history (if any). WAL archiving status. S3 upload status. Prometheus metric `argus_backup_last_success_seconds` visualized.
- [ ] AC-9: **Deploy History / Change Log (SCR-138):** Each deploy: git SHA, version tag, deployer, timestamp, changelog snippet, status (success/rollback). Diff link to git. Filter by date/deployer. Current running version highlighted.
- [ ] AC-10: **Incident Timeline (SCR-139):** Chronological view of all incidents: alert trigger, escalation, ack, resolution. Link to affected entities. Duration and MTTR metrics. Post-mortem link field. Filter by severity/entity/resolved status.
- [ ] AC-11: **Alert Acknowledgment UX (expand /alerts):** Each alert row gets: Ack button (with note), Resolve button (with resolution summary), Escalate button (creates notification to on-call), Comment thread. State machine: open → acknowledged → resolved. History of state transitions. Runbook link (clickable, opens docs page or external URL).
- [ ] AC-12: **WebSocket connection status indicator** in header: green dot (connected), yellow pulse (reconnecting), red (offline). Tooltip with detail. Click to force reconnect.
- [ ] AC-13: Sidebar "Operations" section added with links to all new screens. Command palette entries added.

## Dependencies
- Blocked by: STORY-065 (metrics), STORY-066 (backup API), STORY-067 (deploy history API)
- Blocks: Phase 10 Gate

## Test Scenarios
- [ ] E2E: Open Performance Dashboard → slow queries table populated (requires at least 1 query > 100ms), latency chart renders.
- [ ] E2E: Simulate 500 error → Error Drill-down shows spike in chart + filterable to affected endpoint.
- [ ] E2E: Open Real-time Traffic → gauge shows live RADIUS req/s (seed traffic via radclient or mock).
- [ ] E2E: Kill Redis → NATS Health shows consumer lag spike, DB Health shows connection wait spike.
- [ ] E2E: Run backup cron → Backup Status shows "Last backup: just now, 45MB, PASS."
- [ ] E2E: Deploy new version → Deploy History shows entry with git SHA and "Running" badge.
- [ ] E2E: Alert fires → click Ack → add note → Resolve → Incident Timeline shows full lifecycle.

## Effort Estimate
- Size: XL
- Complexity: High (13 ACs, 10+ new screens, backend metrics endpoints required)

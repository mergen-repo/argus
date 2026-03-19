# STORY-036: Anomaly Detection

## User Story
As a security analyst, I want automated detection of SIM cloning, data usage spikes, and authentication floods, so that I can respond to security threats and abnormal behavior quickly.

## Description
Rule-based anomaly detection engine: detect SIM cloning (same IMSI authenticating from two different NAS IPs within time window), data usage spikes (SIM exceeding N× average daily usage), and auth floods (excessive auth requests from same IMSI or NAS). Anomalies generate alerts via notification service. ML-based detection deferred to FUTURE.md.

## Architecture Reference
- Services: SVC-07 (Analytics Engine — internal/analytics/anomaly)
- API Endpoints: API-113
- Database Tables: TBL-17 (sessions), TBL-18 (cdrs), TBL-10 (sims)
- Source: docs/architecture/api/_index.md (Analytics section)

## Screen Reference
- SCR-013: Analytics Anomalies — anomaly table with severity, type, affected SIM, expandable details

## Acceptance Criteria
- [ ] SIM cloning detection: same IMSI authenticated from 2+ different NAS IPs within 5 minutes
- [ ] Data spike detection: SIM daily usage exceeds 3× its 30-day average (configurable multiplier)
- [ ] Auth flood detection: >100 auth requests from same IMSI within 1 minute (configurable)
- [ ] NAS flood detection: >1000 auth requests from same NAS IP within 1 minute
- [ ] Anomaly record: type, severity (critical/high/medium/low), sim_id, details (JSONB), detected_at, resolved_at
- [ ] GET /api/v1/analytics/anomalies lists anomalies with filters (type, severity, state, date range)
- [ ] Anomaly states: open → acknowledged → resolved / false_positive
- [ ] Critical anomalies (SIM cloning) trigger: auto-suspend SIM (configurable), alert.new event
- [ ] Alert.new → notification service: in-app + email + Telegram for security events
- [ ] Anomaly detection runs on:
  - Real-time: auth events checked against Redis sliding window (flood, cloning)
  - Batch: hourly job checks CDR aggregates (data spikes)
- [ ] False positive marking: analyst can mark anomaly as false_positive to tune thresholds
- [ ] Configurable thresholds per tenant (stored in tenant config)

## API Contract

| Ref | Method | Path | Request | Response (data) | Auth | Status Codes |
|-----|--------|------|---------|-----------------|------|-------------|
| API-113 | GET | /api/v1/analytics/anomalies | `?cursor&limit&type&severity&state&from&to` | `[{id,type,severity,sim_id,sim_iccid,details,detected_at,state}]` | JWT(analyst+) | 200 |

## Dependencies
- Blocked by: STORY-015 (RADIUS events for real-time detection), STORY-032 (CDR data for batch detection), STORY-038 (notification service for alerts)
- Blocks: STORY-048 (frontend anomaly page)

## Test Scenarios
- [ ] Same IMSI from 2 NAS IPs within 5min → SIM_CLONING anomaly created (critical)
- [ ] SIM cloning with auto-suspend enabled → SIM auto-suspended
- [ ] SIM daily usage 4× average → DATA_SPIKE anomaly created (high)
- [ ] 150 auth requests from same IMSI in 1 min → AUTH_FLOOD anomaly (high)
- [ ] Anomaly list filtered by severity=critical → only critical anomalies
- [ ] Acknowledge anomaly → state changes to acknowledged
- [ ] Mark false positive → state=false_positive, threshold feedback recorded
- [ ] Critical anomaly → alert.new event published, notification sent
- [ ] Normal auth pattern (single NAS, normal usage) → no anomaly created

## Effort Estimate
- Size: L
- Complexity: High

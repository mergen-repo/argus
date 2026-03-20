# STORY-032: CDR Processing & Rating Engine

## User Story
As an analyst, I want call detail records (CDRs) generated from RADIUS/Diameter accounting events with cost calculation, so that I can track usage and carrier costs per SIM.

## Description
CDR processing pipeline: accounting events from RADIUS (Acct-Start/Interim/Stop) and Diameter (CCR) flow into TBL-18 (cdrs) via SVC-07. Rating engine calculates cost per CDR based on operator rates, RAT type, time-of-day, and volume tiers. Carrier cost tracking aggregates total spend per operator. TimescaleDB hypertable for time-series queries.

## Architecture Reference
- Services: SVC-07 (Analytics Engine — internal/analytics/cdr)
- Database Tables: TBL-18 (cdrs — TimescaleDB hypertable), TBL-17 (sessions), TBL-05 (operators — rate config)
- API Endpoints: API-114, API-115
- Source: docs/architecture/services/_index.md (SVC-07), docs/architecture/db/_index.md (TBL-18)

## Screen Reference
- SCR-012: Analytics Cost — cost breakdown, carrier cost tracking
- SCR-021c: SIM Detail Usage tab — per-SIM CDR list

## Acceptance Criteria
- [ ] RADIUS Accounting-Start → CDR record created in TBL-18 (type='start')
- [ ] RADIUS Accounting-Interim → CDR record with delta bytes_in/bytes_out
- [ ] RADIUS Accounting-Stop → CDR record with final totals, duration, terminate_cause
- [ ] Diameter CCR events → equivalent CDR records in TBL-18
- [ ] Rating engine: calculate cost_amount per CDR based on:
  - Operator rate card (cost_per_mb from TBL-05 config)
  - RAT type multiplier (e.g., 5G = 1.5x base rate)
  - Time-of-day tariff (peak/off-peak)
  - Volume tier (first 1GB at rate X, next 10GB at rate Y)
- [ ] CDR fields: session_id, sim_id, operator_id, apn_id, rat_type, bytes_in, bytes_out, duration_sec, cost_amount, cost_currency, rated_at
- [ ] TBL-18 is TimescaleDB hypertable partitioned by timestamp
- [ ] GET /api/v1/cdrs lists CDRs with time-range filter, pagination
- [ ] POST /api/v1/cdrs/export exports CDRs to CSV for date range
- [ ] Carrier cost aggregation: total cost per operator per day/month
- [ ] CDR processing is async via NATS (accounting events → SVC-07 consumer)
- [ ] CDR deduplication: same session_id + timestamp → idempotent insert

## API Contract

| Ref | Method | Path | Request | Response (data) | Auth | Status Codes |
|-----|--------|------|---------|-----------------|------|-------------|
| API-114 | GET | /api/v1/cdrs | `?cursor&limit&sim_id&operator_id&from&to&min_cost` | `[{id,session_id,sim_iccid,operator,bytes_in,bytes_out,duration,cost,rat_type,timestamp}]` | JWT(analyst+) | 200 |
| API-115 | POST | /api/v1/cdrs/export | `{from,to,operator_id?,format:"csv"}` | `{job_id,download_url}` | JWT(analyst+) | 202 |

## Dependencies
- Blocked by: STORY-015 (RADIUS accounting events), STORY-019 (Diameter accounting), STORY-009 (operator rate config)
- Blocks: STORY-034 (usage analytics), STORY-035 (cost analytics)

> **Note (post-STORY-019):** STORY-019 Diameter Gx/Gy handlers publish session events to the same NATS topics as RADIUS: `argus.events.session.started` (on CCR-I), `argus.events.session.updated` (on CCR-U), `argus.events.session.ended` (on CCR-T). The CDR consumer should subscribe to these NATS topics and create CDR records regardless of whether the source is RADIUS or Diameter. Event payloads include session_id, sim_id, operator_id, bytes_in/bytes_out, and protocol_type. The Gy (credit-control) events additionally carry Granted-Service-Unit and Used-Service-Unit data useful for cost calculation.

## Test Scenarios
- [ ] RADIUS Accounting-Start → CDR created with type=start, no cost yet
- [ ] RADIUS Accounting-Interim → CDR with delta usage, cost calculated
- [ ] RADIUS Accounting-Stop → final CDR with total usage, final cost rated
- [ ] Rating: 500MB at $0.01/MB = $5.00 cost_amount
- [ ] Rating with RAT multiplier: 5G session at 1.5x → $7.50
- [ ] CDR list filtered by time range → correct results
- [ ] CDR export → CSV file with all fields, download URL returned
- [ ] Duplicate accounting event → no duplicate CDR (idempotent)
- [ ] Carrier cost aggregation → correct total per operator per month

## Effort Estimate
- Size: L
- Complexity: High

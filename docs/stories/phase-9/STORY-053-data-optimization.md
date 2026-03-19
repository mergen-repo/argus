# STORY-053: Data Volume Optimization

## User Story
As a platform operator, I want data volume optimization with TimescaleDB compression, partition management, and archival to S3, so that the platform handles years of CDR data without performance degradation.

## Description
Data lifecycle management for high-volume tables (TBL-17 sessions, TBL-18 cdrs, TBL-19 audit_logs, TBL-23 operator_health_logs). TimescaleDB compression policies for older data. Partition management (automatic chunk creation/dropping). Archival to S3 for data beyond retention period. Read replica for analytics queries. PgBouncer connection pooling for efficient database access.

## Architecture Reference
- Services: SVC-07 (Analytics — query optimization), SVC-09 (Job Runner — archival jobs)
- Database Tables: TBL-17 (sessions), TBL-18 (cdrs), TBL-19 (audit_logs), TBL-23 (operator_health_logs)
- Source: docs/architecture/db/_index.md

## Screen Reference
- SCR-120: System Health — database stats, storage usage, compression ratio

## Acceptance Criteria
- [ ] TimescaleDB compression policy: compress chunks older than 7 days for TBL-18 (cdrs)
- [ ] TimescaleDB compression policy: compress chunks older than 30 days for TBL-17 (sessions)
- [ ] Compression ratio target: > 10:1 for CDR data
- [ ] Continuous aggregates: hourly, daily, monthly rollups for TBL-18 (used by analytics)
- [ ] Continuous aggregate refresh: real-time for recent data, scheduled for historical
- [ ] Partition management: automatic chunk creation, drop chunks beyond retention
- [ ] CDR retention: configurable per tenant (default 365 days), auto-drop older chunks
- [ ] S3 archival: export compressed chunks to S3 before dropping (optional, configurable)
- [ ] S3 archival job: scheduled, runs during off-peak hours
- [ ] Read replica: configure analytics queries to use read replica (connection string per query type)
- [ ] PgBouncer: connection pooling between Go app and PostgreSQL
- [ ] PgBouncer config: pool_mode=transaction, max_client_conn=200, default_pool_size=20
- [ ] Query optimization: all analytics queries use continuous aggregates, not raw tables
- [ ] Storage monitoring: track table sizes, compression ratios, chunk counts
- [ ] Alert on storage: notify when disk usage exceeds 80% threshold

## Dependencies
- Blocked by: STORY-002 (DB schema), STORY-032 (CDR processing), STORY-034 (analytics queries)
- Blocks: None (optimization story)

## Test Scenarios
- [ ] Insert 1M CDRs → compress 7-day-old chunks → verify compression ratio > 10:1
- [ ] Query 30-day analytics on compressed data → same results as uncompressed, faster response
- [ ] Continuous aggregate: hourly rollup matches raw CDR sum
- [ ] Drop chunks older than retention → data removed, aggregate preserved
- [ ] S3 archival: chunk exported → downloadable from S3
- [ ] PgBouncer: 200 concurrent queries → no connection errors
- [ ] Read replica query: analytics query routed to replica, not primary
- [ ] Storage alert: disk at 82% → notification sent

## Effort Estimate
- Size: L
- Complexity: High

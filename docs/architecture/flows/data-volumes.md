# Data Volume Analysis — Argus

> Capacity planning for 10M+ SIM deployment across 3-4 operators.

## Per-SIM Data Metrics

| Metric | Typical IoT | High-Usage IoT | Unit |
|--------|------------|----------------|------|
| Daily data usage | 1-10 MB | 100 MB - 1 GB | per SIM |
| Monthly data usage | 30-300 MB | 3-30 GB | per SIM |
| Sessions per day | 1-5 | 10-50 | per SIM |
| CDR records per day | 3-15 (start+interim+stop) | 50-150 | per SIM |
| Auth requests per day | 2-10 | 20-100 | per SIM |
| Avg session duration | 4-24 hours | 1-4 hours | per session |

## System-Wide Projections (10M SIMs)

| Metric | Low Estimate | High Estimate | Storage Impact |
|--------|-------------|---------------|----------------|
| **CDR records/day** | 30M | 150M | 3-15 GB/day (compressed) |
| **CDR records/month** | 900M | 4.5B | 90-450 GB/month |
| **Auth requests/sec (peak)** | 1,000 | 15,000 | Redis: ~50MB session state |
| **Active sessions (concurrent)** | 500K | 5M | Redis: ~500MB-5GB |
| **Audit log entries/day** | 100K | 1M | 50-500 MB/day |
| **SIM table size** | 10M rows | 10M rows | ~8 GB (with indexes) |
| **Session table (hot, 30d)** | 150M | 1.5B | 15-150 GB |
| **Notification events/day** | 10K | 100K | Negligible |

## Storage Retention Strategy

| Data Type | Hot (PG/TimescaleDB) | Warm (Compressed) | Cold (S3) | Total Retention |
|-----------|---------------------|-------------------|-----------|----------------|
| SIM records | Forever | — | — | Forever |
| Active sessions | 30 days | — | — | 30 days hot |
| CDRs | 7 days uncompressed | 90 days compressed | 3 years archived | 3 years |
| Audit logs | 90 days | 1 year compressed | 7 years archived | 7 years (compliance) |
| Operator health | 7 days | 90 days compressed | 1 year | 1 year |
| Analytics aggregates | Forever (continuous aggs) | — | — | Forever |
| Job history | 30 days | 1 year compressed | — | 1 year |
| Notifications | 90 days | — | — | 90 days |

## Per-APN Data Volume

| Metric | Calculation | Example (100K SIMs on APN) |
|--------|------------|---------------------------|
| Daily traffic | SIMs × avg_daily_usage | 100K × 10MB = 1 TB/day |
| Monthly traffic | SIMs × avg_monthly_usage | 100K × 300MB = 30 TB/month |
| Active sessions | SIMs × concurrent_ratio | 100K × 50% = 50K concurrent |
| CDRs/day | SIMs × avg_cdrs_per_day | 100K × 10 = 1M CDRs/day |
| IP pool size | SIMs + 10% headroom | 110K IPs needed |

## Per-Operator Data Volume

| Metric | Calculation | Example (Turkcell, 4.5M SIMs) |
|--------|------------|-------------------------------|
| Auth requests/sec (peak) | SIMs × peak_auth_rate | 4.5M × 0.001 = 4,500/sec |
| RADIUS packets/sec | auth + acct (3x) | ~18,000/sec |
| Diameter messages/sec | Gx + Gy | ~9,000/sec |
| Health check rate | 1 per 30s | 2/min per operator |
| SLA data points/day | 2880 (every 30s) | 2880 records/day |
| Monthly carrier cost data | SIMs × CDRs × rate | Millions of CDR cost records |

## Anomaly Detection Volume

| Metric | Expected | Storage |
|--------|----------|---------|
| Anomaly events/day | 10-100 (after filtering) | Negligible |
| Raw signals analyzed/sec | 10K-100K (auth patterns, traffic spikes) | In-memory only |
| False positive rate target | < 5% | — |
| Historical pattern data | 30-day sliding window of CDR aggregates | ~10 GB |

## Policy Engine Volume

| Metric | Value | Impact |
|--------|-------|--------|
| Policy evaluations/sec | = Auth requests/sec (10K+) | In-memory cache, <0.1ms |
| Staged rollout: CoA messages | Up to 10M (full fleet rollout) | ~30 min at 5K CoA/sec |
| Policy versions stored | ~100 per policy | Negligible (text + JSON) |
| Dry-run simulation | Scans up to 10M SIM records | Must use read replica, <30s |

## Dashboard Data Aggregation

| Dashboard Widget | Data Source | Update Frequency | Query Complexity |
|-----------------|-------------|-------------------|-----------------|
| Active SIM count | Redis counter (NATS-updated) | Real-time | O(1) |
| Active session count | Redis counter | Real-time (1s) | O(1) |
| Auth/s rate | Redis sliding window | Real-time (1s) | O(1) |
| Alert count | PG query (indexed) | 5s poll or WebSocket | O(log n) |
| SIM distribution pie | TimescaleDB continuous agg | 1h refresh | Pre-computed |
| Operator health | Redis (last check) | 30s | O(1) per operator |
| Top APNs by traffic | TimescaleDB continuous agg | 1h refresh | Pre-computed |
| Cost metrics | TimescaleDB daily agg | Daily refresh | Pre-computed |
| Usage time-series | TimescaleDB hourly agg | 1h refresh | Pre-computed |

## Redis Memory Budget

| Key Pattern | Count | Size Each | Total |
|------------|-------|-----------|-------|
| `session:{sim_id}` | 5M (concurrent) | ~500 bytes | 2.5 GB |
| `sim:{imsi}` (auth cache) | 10M | ~200 bytes | 2 GB |
| `policy:{version_id}` (compiled) | ~500 | ~10 KB | 5 MB |
| `ratelimit:{key}:{window}` | ~10K | ~100 bytes | 1 MB |
| `tenant:{id}` (config) | ~100 | ~1 KB | 100 KB |
| `operator:{id}:health` | ~5 | ~200 bytes | 1 KB |
| **Total Redis memory** | | | **~5 GB peak** |

## Database Disk Budget (1 year)

| Table | Rows (1yr) | Size (uncompressed) | Size (compressed) |
|-------|-----------|--------------------|--------------------|
| sims (TBL-10) | 10M | 8 GB | — (always hot) |
| sessions (TBL-17) | 1.8B | 180 GB | 30 GB (after 30d compress) |
| cdrs (TBL-18) | 10B+ | 1 TB | 150 GB (after 7d compress) |
| audit_logs (TBL-19) | 50M | 25 GB | 5 GB (after 90d compress) |
| sim_state_history (TBL-11) | 100M | 10 GB | 2 GB |
| operator_health (TBL-23) | 10M | 1 GB | 200 MB |
| **Total disk (1 year)** | | | **~200 GB compressed** |

## Recommendations

1. **PostgreSQL**: Minimum 256 GB SSD, recommend 500 GB for growth
2. **Redis**: Minimum 8 GB RAM, recommend 16 GB
3. **NATS**: 10 GB disk for JetStream persistence
4. **S3/MinIO**: Plan for 1 TB/year cold storage (CDR + audit archives)
5. **TimescaleDB compression**: Enable after 7 days for CDRs, 30 days for sessions
6. **Read replica**: Required for analytics queries (don't impact OLTP)
7. **Connection pooling**: PgBouncer with 100 connections to PG, Go pool of 25 idle

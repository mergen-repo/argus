# Database: AAA & Analytics Domain

## TBL-17: sessions

TimescaleDB hypertable — partitioned by started_at.

| Column | Type | Constraints | Description |
|--------|------|------------|-------------|
| id | UUID | PK, DEFAULT gen_random_uuid() | Session identifier |
| sim_id | UUID | NOT NULL | SIM reference |
| tenant_id | UUID | NOT NULL | Tenant (denormalized for query performance) |
| operator_id | UUID | NOT NULL | Operator handling this session |
| apn_id | UUID | | APN used |
| nas_ip | INET | | NAS (Network Access Server) IP |
| framed_ip | INET | | IP assigned to device |
| calling_station_id | VARCHAR(50) | | Device identifier (IMSI/MSISDN) |
| called_station_id | VARCHAR(100) | | APN identifier |
| rat_type | VARCHAR(10) | | nb_iot, lte_m, lte, nr_5g |
| session_state | VARCHAR(20) | NOT NULL, DEFAULT 'active' | active, terminated, force_disconnected |
| auth_method | VARCHAR(20) | | eap_sim, eap_aka, eap_aka_prime, pap, chap |
| policy_version_id | UUID | | Policy in effect |
| acct_session_id | VARCHAR(100) | | RADIUS Acct-Session-Id |
| started_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Session start |
| ended_at | TIMESTAMPTZ | | Session end |
| terminate_cause | VARCHAR(50) | | Reason for termination |
| bytes_in | BIGINT | NOT NULL, DEFAULT 0 | Download bytes |
| bytes_out | BIGINT | NOT NULL, DEFAULT 0 | Upload bytes |
| packets_in | BIGINT | NOT NULL, DEFAULT 0 | Download packets |
| packets_out | BIGINT | NOT NULL, DEFAULT 0 | Upload packets |
| last_interim_at | TIMESTAMPTZ | | Last interim accounting update |
| sor_decision | JSONB | | SoR engine decision record (operator, reason, fallbacks) |

Indexes:
- `idx_sessions_sim_active` on (sim_id) WHERE session_state = 'active'
- `idx_sessions_tenant_active` on (tenant_id) WHERE session_state = 'active'
- `idx_sessions_tenant_operator` on (tenant_id, operator_id, started_at DESC)
- `idx_sessions_acct_session` on (acct_session_id) WHERE acct_session_id IS NOT NULL

TimescaleDB:
```sql
SELECT create_hypertable('sessions', 'started_at');
```
- Compression after 30 days
- Retention: 365 days (archival to S3 before drop)

---

## TBL-18: cdrs

TimescaleDB hypertable — partitioned by timestamp. High-volume table (every accounting update).

| Column | Type | Constraints | Description |
|--------|------|------------|-------------|
| id | BIGSERIAL | PK | CDR entry ID |
| session_id | UUID | NOT NULL | Parent session |
| sim_id | UUID | NOT NULL | SIM reference (denormalized) |
| tenant_id | UUID | NOT NULL | Tenant (denormalized) |
| operator_id | UUID | NOT NULL | Operator (denormalized) |
| apn_id | UUID | | APN (denormalized) |
| rat_type | VARCHAR(10) | | RAT type |
| record_type | VARCHAR(20) | NOT NULL | start, interim, stop |
| bytes_in | BIGINT | NOT NULL, DEFAULT 0 | Period download bytes |
| bytes_out | BIGINT | NOT NULL, DEFAULT 0 | Period upload bytes |
| duration_sec | INTEGER | NOT NULL, DEFAULT 0 | Period duration |
| usage_cost | DECIMAL(12,4) | | Calculated cost for this period |
| carrier_cost | DECIMAL(12,4) | | Cost charged by operator |
| rate_per_mb | DECIMAL(8,4) | | Applied rate |
| rat_multiplier | DECIMAL(4,2) | DEFAULT 1.0 | RAT-type cost multiplier |
| timestamp | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | CDR timestamp |

Indexes:
- `idx_cdrs_session` on (session_id, timestamp)
- `idx_cdrs_tenant_time` on (tenant_id, timestamp DESC)
- `idx_cdrs_tenant_operator_time` on (tenant_id, operator_id, timestamp DESC)
- `idx_cdrs_sim_time` on (sim_id, timestamp DESC)
- `idx_cdrs_dedup` UNIQUE on (session_id, timestamp, record_type) -- deduplication index for idempotent inserts (STORY-032)

TimescaleDB:
```sql
SELECT create_hypertable('cdrs', 'timestamp');
```
- Compression after 7 days
- Continuous aggregate: hourly/daily usage per tenant/operator/apn
- Retention: 90 days hot, then S3 archive

### Continuous Aggregates

```sql
-- Hourly usage per tenant/operator
CREATE MATERIALIZED VIEW cdrs_hourly
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 hour', timestamp) AS bucket,
    tenant_id,
    operator_id,
    apn_id,
    rat_type,
    COUNT(*) AS record_count,
    SUM(bytes_in) AS total_bytes_in,
    SUM(bytes_out) AS total_bytes_out,
    SUM(usage_cost) AS total_usage_cost,
    SUM(carrier_cost) AS total_carrier_cost
FROM cdrs
GROUP BY bucket, tenant_id, operator_id, apn_id, rat_type;

-- Daily usage per tenant
CREATE MATERIALIZED VIEW cdrs_daily
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 day', timestamp) AS bucket,
    tenant_id,
    operator_id,
    COUNT(DISTINCT sim_id) AS active_sims,
    SUM(bytes_in + bytes_out) AS total_bytes,
    SUM(usage_cost) AS total_cost,
    SUM(carrier_cost) AS total_carrier_cost
FROM cdrs
GROUP BY bucket, tenant_id, operator_id;

-- Monthly usage per tenant/operator (STORY-034)
CREATE MATERIALIZED VIEW cdrs_monthly
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 month', timestamp) AS bucket,
    tenant_id,
    operator_id,
    apn_id,
    rat_type,
    COUNT(*) AS record_count,
    COUNT(DISTINCT sim_id) AS unique_sims,
    SUM(bytes_in + bytes_out) AS total_bytes,
    SUM(bytes_in) AS total_bytes_in,
    SUM(bytes_out) AS total_bytes_out,
    SUM(usage_cost) AS total_usage_cost,
    SUM(carrier_cost) AS total_carrier_cost
FROM cdrs
GROUP BY bucket, tenant_id, operator_id, apn_id, rat_type;
-- Refresh: every 6h, 3-month offset, real-time aggregation enabled
```

All three continuous aggregates have `materialized_only = false` (real-time aggregation) enabled, ensuring queries return current data by combining materialized results with recent un-aggregated rows.

---

## TBL-28: anomalies

Anomaly detection records for fraud, usage spikes, and connectivity issues flagged by the analytics engine.

### Columns

| Column | Type | Nullable | Default | Description |
|--------|------|----------|---------|-------------|
| id | UUID | NOT NULL | gen_random_uuid() | Anomaly identifier |
| tenant_id | UUID | NOT NULL | — | Tenant (FK → tenants.id) |
| sim_id | UUID | NULL | — | Affected SIM (nullable; null for tenant-wide anomalies) |
| type | TEXT | NOT NULL | — | Anomaly type: `sim_cloning`, `data_spike`, `auth_flood`, `nas_flood` |
| severity | TEXT | NOT NULL | — | `critical`, `high`, `medium`, `low` |
| state | TEXT | NOT NULL | `open` | Lifecycle state: `open`, `acknowledged`, `resolved`, `false_positive` |
| details | JSONB | NOT NULL | `{}` | Structured anomaly payload (thresholds, observed values, evidence) |
| source | TEXT | NULL | — | Detection source (e.g., rule engine, ML model identifier) |
| detected_at | TIMESTAMPTZ | NOT NULL | now() | When the anomaly was detected |
| acknowledged_at | TIMESTAMPTZ | NULL | — | When the anomaly was acknowledged |
| resolved_at | TIMESTAMPTZ | NULL | — | When the anomaly was resolved or dismissed |
| created_at | TIMESTAMPTZ | NOT NULL | now() | Record creation time |
| updated_at | TIMESTAMPTZ | NOT NULL | now() | Last modification time |

CHECK constraints: `type IN ('sim_cloning', 'data_spike', 'auth_flood', 'nas_flood')`, `severity IN ('critical', 'high', 'medium', 'low')`, `state IN ('open', 'acknowledged', 'resolved', 'false_positive')`

### Indexes

- `idx_anomalies_tenant_id` on (tenant_id)
- `idx_anomalies_sim_id` on (sim_id) WHERE sim_id IS NOT NULL
- `idx_anomalies_type` on (type)
- `idx_anomalies_severity` on (severity)
- `idx_anomalies_state` on (state)
- `idx_anomalies_detected_at` on (detected_at DESC)
- `idx_anomalies_tenant_state` on (tenant_id, state)

### Partitioning

None.

### Related

- TBL-01 tenants (tenant_id FK)
- TBL-10 sims (sim_id, nullable — anomaly may apply to a specific SIM or the entire tenant)
- SVC-07 analytics — anomaly detection engine writes records here
- TBL-21 notifications — anomaly events trigger `anomaly_detected` notifications

-- Usage Analytics Continuous Aggregates (STORY-034)
-- Extends existing hourly/daily aggregates with monthly view
-- and adds real-time aggregate support for recent data.

-- Monthly usage per tenant/operator
CREATE MATERIALIZED VIEW IF NOT EXISTS cdrs_monthly
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
GROUP BY bucket, tenant_id, operator_id, apn_id, rat_type
WITH NO DATA;

-- Refresh monthly aggregates every 6 hours
SELECT add_continuous_aggregate_policy('cdrs_monthly',
    start_offset => INTERVAL '3 months',
    end_offset => INTERVAL '1 day',
    schedule_interval => INTERVAL '6 hours',
    if_not_exists => true
);

-- Enable real-time aggregation on hourly view for recent data
ALTER MATERIALIZED VIEW cdrs_hourly SET (timescaledb.materialized_only = false);

-- Enable real-time aggregation on daily view for recent data
ALTER MATERIALIZED VIEW cdrs_daily SET (timescaledb.materialized_only = false);

-- Enable real-time aggregation on monthly view for recent data
ALTER MATERIALIZED VIEW cdrs_monthly SET (timescaledb.materialized_only = false);

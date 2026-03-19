-- CDR Continuous Aggregates

-- Hourly usage per tenant/operator/apn/rat_type
CREATE MATERIALIZED VIEW IF NOT EXISTS cdrs_hourly
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
GROUP BY bucket, tenant_id, operator_id, apn_id, rat_type
WITH NO DATA;

-- Daily usage per tenant/operator
CREATE MATERIALIZED VIEW IF NOT EXISTS cdrs_daily
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
GROUP BY bucket, tenant_id, operator_id
WITH NO DATA;

-- Refresh policies: refresh hourly aggregates every 30 minutes
SELECT add_continuous_aggregate_policy('cdrs_hourly',
    start_offset => INTERVAL '3 hours',
    end_offset => INTERVAL '1 hour',
    schedule_interval => INTERVAL '30 minutes',
    if_not_exists => true
);

-- Refresh daily aggregates every hour
SELECT add_continuous_aggregate_policy('cdrs_daily',
    start_offset => INTERVAL '3 days',
    end_offset => INTERVAL '1 day',
    schedule_interval => INTERVAL '1 hour',
    if_not_exists => true
);

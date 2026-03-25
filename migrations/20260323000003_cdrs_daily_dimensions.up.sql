-- Add apn_id and rat_type dimensions to cdrs_daily continuous aggregate
-- Required for group_by=apn and group_by=rat_type on 30d+ queries
-- Without this, analytics would fall back to raw cdrs table (10M+ rows = slow)

DROP MATERIALIZED VIEW IF EXISTS cdrs_daily CASCADE;

CREATE MATERIALIZED VIEW cdrs_daily
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 day', timestamp) AS bucket,
    tenant_id,
    operator_id,
    apn_id,
    rat_type,
    COUNT(DISTINCT sim_id) AS active_sims,
    SUM(bytes_in + bytes_out) AS total_bytes,
    SUM(usage_cost) AS total_cost,
    SUM(carrier_cost) AS total_carrier_cost
FROM cdrs
GROUP BY bucket, tenant_id, operator_id, apn_id, rat_type
WITH NO DATA;

SELECT add_continuous_aggregate_policy('cdrs_daily',
    start_offset => INTERVAL '90 days',
    end_offset => INTERVAL '1 day',
    schedule_interval => INTERVAL '1 day');

-- Revert to original cdrs_daily without apn_id/rat_type dimensions

DROP MATERIALIZED VIEW IF EXISTS cdrs_daily CASCADE;

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
GROUP BY bucket, tenant_id, operator_id
WITH NO DATA;

SELECT add_continuous_aggregate_policy('cdrs_daily',
    start_offset => INTERVAL '90 days',
    end_offset => INTERVAL '1 day',
    schedule_interval => INTERVAL '1 day');

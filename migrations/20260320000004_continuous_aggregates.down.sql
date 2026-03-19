-- Reverse continuous aggregates
-- Remove refresh policies first, then drop the views

SELECT remove_continuous_aggregate_policy('cdrs_daily', if_not_exists => true);
SELECT remove_continuous_aggregate_policy('cdrs_hourly', if_not_exists => true);

DROP MATERIALIZED VIEW IF EXISTS cdrs_daily CASCADE;
DROP MATERIALIZED VIEW IF EXISTS cdrs_hourly CASCADE;

-- Reverse usage analytics aggregates (STORY-034)

SELECT remove_continuous_aggregate_policy('cdrs_monthly', if_not_exists => true);
DROP MATERIALIZED VIEW IF EXISTS cdrs_monthly CASCADE;

-- Restore materialized_only defaults
ALTER MATERIALIZED VIEW cdrs_hourly SET (timescaledb.materialized_only = true);
ALTER MATERIALIZED VIEW cdrs_daily SET (timescaledb.materialized_only = true);

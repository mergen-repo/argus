-- STORY-053: Rollback data optimization

SELECT remove_retention_policy('sessions', if_exists => true);
SELECT remove_retention_policy('cdrs', if_exists => true);

DROP TABLE IF EXISTS tenant_retention_config;
DROP TABLE IF EXISTS s3_archival_log;

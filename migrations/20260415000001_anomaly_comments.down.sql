-- STORY-072 Task 1: Anomaly comment thread (rollback)

DROP POLICY IF EXISTS tenant_isolation_anomaly_comments ON anomaly_comments;
DROP TABLE IF EXISTS anomaly_comments;

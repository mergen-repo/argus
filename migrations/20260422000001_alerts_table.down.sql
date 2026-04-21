-- Down migration for 20260422000001_alerts_table

DROP POLICY IF EXISTS tenant_isolation_alerts ON alerts;
DROP TABLE IF EXISTS alerts CASCADE;

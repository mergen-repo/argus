DROP POLICY IF EXISTS imei_blacklist_tenant_isolation ON imei_blacklist;
DROP POLICY IF EXISTS imei_greylist_tenant_isolation ON imei_greylist;
DROP POLICY IF EXISTS imei_whitelist_tenant_isolation ON imei_whitelist;
DROP TABLE IF EXISTS imei_blacklist;
DROP TABLE IF EXISTS imei_greylist;
DROP TABLE IF EXISTS imei_whitelist;

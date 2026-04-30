-- FIX-245 T1: Drop kill_switches and maintenance_windows tables.
-- These tables backed Admin > Compliance sub-pages which are removed in FIX-245.
-- Kill switches are replaced by environment-variable feature flags.
-- CASCADE is safe: no other table has FK references to these tables.

DROP TABLE IF EXISTS kill_switches CASCADE;
DROP TABLE IF EXISTS maintenance_windows CASCADE;

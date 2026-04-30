-- STORY-086 AC-2: revert sms_outbound recover migration.
-- CASCADE removes indexes and the RLS policy.
DROP TABLE IF EXISTS sms_outbound CASCADE;

-- STORY-087 D-032 down: idempotent. Safe after 20260413000001.down
-- has already dropped sms_outbound (DROP ... IF EXISTS is a no-op
-- when the table is absent). CASCADE removes the policy and any
-- indexes if they exist, matching STORY-086's down semantics.
DROP TABLE IF EXISTS sms_outbound CASCADE;

-- FIX-245 T2: Remove data_portability_ready notification template.
-- This Tier 1 consumer-voice template is deprecated (FIX-237 taxonomy reclassification).
-- No CHECK constraint on notification_templates.event_type — plain VARCHAR(50).
-- Idempotent: DELETE is a no-op if row doesn't exist.

DELETE FROM notification_templates
 WHERE event_type = 'data_portability_ready';

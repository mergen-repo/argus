-- STORY-077 Task 1 rollback

ALTER TABLE audit_events DROP COLUMN IF EXISTS impersonated_by;
ALTER TABLE users DROP COLUMN IF EXISTS locale;

DROP TABLE IF EXISTS user_column_preferences;
DROP TABLE IF EXISTS chart_annotations;
DROP TABLE IF EXISTS announcement_dismissals;
DROP TABLE IF EXISTS announcements;
DROP TABLE IF EXISTS user_views;

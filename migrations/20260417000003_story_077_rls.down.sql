-- STORY-077 Task 3 rollback

DROP POLICY IF EXISTS user_column_preferences_isolation ON user_column_preferences;
ALTER TABLE user_column_preferences DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS chart_annotations_tenant_isolation ON chart_annotations;
ALTER TABLE chart_annotations DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS announcement_dismissals_isolation ON announcement_dismissals;
ALTER TABLE announcement_dismissals DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS announcements_visibility ON announcements;
ALTER TABLE announcements DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS user_views_isolation ON user_views;
ALTER TABLE user_views DISABLE ROW LEVEL SECURITY;

-- STORY-077 Task 3: RLS for new UX tables

-- user_views: scoped by user_id (private) or tenant_id+shared (shared views)
ALTER TABLE user_views ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_views FORCE ROW LEVEL SECURITY;
CREATE POLICY user_views_isolation ON user_views
    USING (
        user_id::text = current_setting('app.current_user', true)
        OR (shared = TRUE AND tenant_id = current_setting('app.current_tenant', true)::uuid)
    );

-- announcements: target='all' OR target matches current tenant
ALTER TABLE announcements ENABLE ROW LEVEL SECURITY;
ALTER TABLE announcements FORCE ROW LEVEL SECURITY;
CREATE POLICY announcements_visibility ON announcements
    USING (
        target = 'all'
        OR target = current_setting('app.current_tenant', true)
    );

-- announcement_dismissals: scoped by user
ALTER TABLE announcement_dismissals ENABLE ROW LEVEL SECURITY;
ALTER TABLE announcement_dismissals FORCE ROW LEVEL SECURITY;
CREATE POLICY announcement_dismissals_isolation ON announcement_dismissals
    USING (user_id::text = current_setting('app.current_user', true));

-- chart_annotations: scoped by tenant
ALTER TABLE chart_annotations ENABLE ROW LEVEL SECURITY;
ALTER TABLE chart_annotations FORCE ROW LEVEL SECURITY;
CREATE POLICY chart_annotations_tenant_isolation ON chart_annotations
    USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- user_column_preferences: scoped by user
ALTER TABLE user_column_preferences ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_column_preferences FORCE ROW LEVEL SECURITY;
CREATE POLICY user_column_preferences_isolation ON user_column_preferences
    USING (user_id::text = current_setting('app.current_user', true));

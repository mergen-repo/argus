-- STORY-077 Task 1: Enterprise UX tables
-- Creates user_views, announcements, announcement_dismissals,
-- chart_annotations, user_column_preferences.
-- Adds users.locale and audit_logs.impersonated_by columns.

CREATE TABLE IF NOT EXISTS user_views (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    page         TEXT NOT NULL,
    name         TEXT NOT NULL,
    filters_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    columns_json JSONB,
    sort_json    JSONB,
    is_default   BOOLEAN NOT NULL DEFAULT FALSE,
    shared       BOOLEAN NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_user_views_user_page ON user_views(user_id, page);
CREATE INDEX IF NOT EXISTS idx_user_views_tenant_shared ON user_views(tenant_id, page) WHERE shared = TRUE;
CREATE UNIQUE INDEX IF NOT EXISTS uniq_user_default_view ON user_views(user_id, page) WHERE is_default = TRUE;

CREATE TABLE IF NOT EXISTS announcements (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title       TEXT NOT NULL,
    body        TEXT NOT NULL,
    type        TEXT NOT NULL CHECK (type IN ('info','warning','critical')),
    target      TEXT NOT NULL,
    starts_at   TIMESTAMPTZ NOT NULL,
    ends_at     TIMESTAMPTZ NOT NULL,
    dismissible BOOLEAN NOT NULL DEFAULT TRUE,
    created_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_announcements_active ON announcements(starts_at, ends_at);

CREATE TABLE IF NOT EXISTS announcement_dismissals (
    announcement_id UUID NOT NULL REFERENCES announcements(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    dismissed_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (announcement_id, user_id)
);

CREATE TABLE IF NOT EXISTS chart_annotations (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id    UUID REFERENCES users(id) ON DELETE SET NULL,
    chart_key  TEXT NOT NULL,
    timestamp  TIMESTAMPTZ NOT NULL,
    label      TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_chart_annotations_key ON chart_annotations(tenant_id, chart_key, timestamp);

CREATE TABLE IF NOT EXISTS user_column_preferences (
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    page_key     TEXT NOT NULL,
    columns_json JSONB NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, page_key)
);

ALTER TABLE users ADD COLUMN IF NOT EXISTS locale TEXT NOT NULL DEFAULT 'en'
    CHECK (locale IN ('en','tr'));

ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS impersonated_by UUID NULL REFERENCES users(id);

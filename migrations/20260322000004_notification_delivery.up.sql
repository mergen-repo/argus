-- STORY-038: Notification delivery tracking
-- Adds delivery tracking columns to notifications table

ALTER TABLE notifications
    ADD COLUMN IF NOT EXISTS sent_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS delivered_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS failed_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS retry_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS delivery_meta JSONB NOT NULL DEFAULT '{}';

CREATE INDEX IF NOT EXISTS idx_notifications_delivery_pending
    ON notifications (created_at) WHERE state = 'pending' AND retry_count < 5;

CREATE UNIQUE INDEX IF NOT EXISTS idx_notif_configs_tenant_user_event_scope
    ON notification_configs (tenant_id, user_id, event_type, scope_type)
    WHERE user_id IS NOT NULL;

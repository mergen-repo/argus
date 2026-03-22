DROP INDEX IF EXISTS idx_notif_configs_tenant_user_event_scope;
DROP INDEX IF EXISTS idx_notifications_delivery_pending;

ALTER TABLE notifications
    DROP COLUMN IF EXISTS delivery_meta,
    DROP COLUMN IF EXISTS retry_count,
    DROP COLUMN IF EXISTS failed_at,
    DROP COLUMN IF EXISTS delivered_at,
    DROP COLUMN IF EXISTS sent_at;

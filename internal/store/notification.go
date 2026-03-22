package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotificationNotFound = errors.New("store: notification not found")
)

type NotificationRow struct {
	ID           uuid.UUID  `json:"id"`
	TenantID     uuid.UUID  `json:"tenant_id"`
	UserID       *uuid.UUID `json:"user_id,omitempty"`
	EventType    string     `json:"event_type"`
	ScopeType    string     `json:"scope_type"`
	ScopeRefID   *uuid.UUID `json:"scope_ref_id,omitempty"`
	Title        string     `json:"title"`
	Body         string     `json:"body"`
	Severity     string     `json:"severity"`
	ChannelsSent []string   `json:"channels_sent"`
	State        string     `json:"state"`
	ReadAt       *time.Time `json:"read_at,omitempty"`
	SentAt       *time.Time `json:"sent_at,omitempty"`
	DeliveredAt  *time.Time `json:"delivered_at,omitempty"`
	FailedAt     *time.Time `json:"failed_at,omitempty"`
	RetryCount   int        `json:"retry_count"`
	DeliveryMeta json.RawMessage `json:"delivery_meta"`
	CreatedAt    time.Time  `json:"created_at"`
}

type CreateNotificationParams struct {
	TenantID     uuid.UUID
	UserID       *uuid.UUID
	EventType    string
	ScopeType    string
	ScopeRefID   *uuid.UUID
	Title        string
	Body         string
	Severity     string
	ChannelsSent []string
}

type ListNotificationParams struct {
	Cursor    string
	Limit     int
	UnreadOnly bool
}

type NotificationConfigRow struct {
	ID             uuid.UUID       `json:"id"`
	TenantID       uuid.UUID       `json:"tenant_id"`
	UserID         *uuid.UUID      `json:"user_id,omitempty"`
	EventType      string          `json:"event_type"`
	ScopeType      string          `json:"scope_type"`
	ScopeRefID     *uuid.UUID      `json:"scope_ref_id,omitempty"`
	Channels       json.RawMessage `json:"channels"`
	ThresholdType  *string         `json:"threshold_type,omitempty"`
	ThresholdValue *float64        `json:"threshold_value,omitempty"`
	Enabled        bool            `json:"enabled"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type UpsertNotificationConfigParams struct {
	TenantID       uuid.UUID
	UserID         *uuid.UUID
	EventType      string
	ScopeType      string
	ScopeRefID     *uuid.UUID
	Channels       json.RawMessage
	ThresholdType  *string
	ThresholdValue *float64
	Enabled        bool
}

type NotificationStore struct {
	db *pgxpool.Pool
}

func NewNotificationStore(db *pgxpool.Pool) *NotificationStore {
	return &NotificationStore{db: db}
}

func (s *NotificationStore) Create(ctx context.Context, p CreateNotificationParams) (*NotificationRow, error) {
	channelsSent := p.ChannelsSent
	if channelsSent == nil {
		channelsSent = []string{}
	}

	row := s.db.QueryRow(ctx, `
		INSERT INTO notifications (tenant_id, user_id, event_type, scope_type, scope_ref_id, title, body, severity, channels_sent, state)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'unread')
		RETURNING id, tenant_id, user_id, event_type, scope_type, scope_ref_id, title, body, severity,
			channels_sent, state, read_at, sent_at, delivered_at, failed_at, retry_count, delivery_meta, created_at`,
		p.TenantID, p.UserID, p.EventType, p.ScopeType, p.ScopeRefID,
		p.Title, p.Body, p.Severity, channelsSent,
	)

	return scanNotification(row)
}

func (s *NotificationStore) GetByID(ctx context.Context, tenantID, id uuid.UUID) (*NotificationRow, error) {
	row := s.db.QueryRow(ctx, `
		SELECT id, tenant_id, user_id, event_type, scope_type, scope_ref_id, title, body, severity,
			channels_sent, state, read_at, sent_at, delivered_at, failed_at, retry_count, delivery_meta, created_at
		FROM notifications
		WHERE id = $1 AND tenant_id = $2`, id, tenantID)

	n, err := scanNotification(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotificationNotFound
	}
	return n, err
}

func (s *NotificationStore) ListByUser(ctx context.Context, tenantID, userID uuid.UUID, p ListNotificationParams) ([]NotificationRow, string, error) {
	limit := p.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{tenantID, userID, limit + 1}
	conditions := []string{"tenant_id = $1", "(user_id = $2 OR user_id IS NULL)"}

	if p.UnreadOnly {
		conditions = append(conditions, "state = 'unread'")
	}

	argIdx := 4
	if p.Cursor != "" {
		cursorID, err := uuid.Parse(p.Cursor)
		if err == nil {
			conditions = append(conditions, fmt.Sprintf("id < $%d", argIdx))
			args = append(args, cursorID)
			argIdx++
		}
	}

	where := strings.Join(conditions, " AND ")

	query := fmt.Sprintf(`
		SELECT id, tenant_id, user_id, event_type, scope_type, scope_ref_id, title, body, severity,
			channels_sent, state, read_at, sent_at, delivered_at, failed_at, retry_count, delivery_meta, created_at
		FROM notifications
		WHERE %s
		ORDER BY CASE WHEN state = 'unread' THEN 0 ELSE 1 END, created_at DESC, id DESC
		LIMIT $3`, where)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list notifications: %w", err)
	}
	defer rows.Close()

	var results []NotificationRow
	for rows.Next() {
		n, err := scanNotificationRows(rows)
		if err != nil {
			return nil, "", fmt.Errorf("store: scan notification: %w", err)
		}
		results = append(results, *n)
	}

	var nextCursor string
	if len(results) > limit {
		nextCursor = results[limit-1].ID.String()
		results = results[:limit]
	}

	return results, nextCursor, nil
}

func (s *NotificationStore) MarkRead(ctx context.Context, tenantID, id uuid.UUID) (*NotificationRow, error) {
	row := s.db.QueryRow(ctx, `
		UPDATE notifications
		SET state = 'read', read_at = NOW()
		WHERE id = $1 AND tenant_id = $2
		RETURNING id, tenant_id, user_id, event_type, scope_type, scope_ref_id, title, body, severity,
			channels_sent, state, read_at, sent_at, delivered_at, failed_at, retry_count, delivery_meta, created_at`,
		id, tenantID)

	n, err := scanNotification(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotificationNotFound
	}
	return n, err
}

func (s *NotificationStore) MarkAllRead(ctx context.Context, tenantID, userID uuid.UUID) (int64, error) {
	tag, err := s.db.Exec(ctx, `
		UPDATE notifications
		SET state = 'read', read_at = NOW()
		WHERE tenant_id = $1 AND (user_id = $2 OR user_id IS NULL) AND state = 'unread'`,
		tenantID, userID)
	if err != nil {
		return 0, fmt.Errorf("store: mark all read: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (s *NotificationStore) UnreadCount(ctx context.Context, tenantID, userID uuid.UUID) (int64, error) {
	var count int64
	err := s.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM notifications
		WHERE tenant_id = $1 AND (user_id = $2 OR user_id IS NULL) AND state = 'unread'`,
		tenantID, userID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: unread count: %w", err)
	}
	return count, nil
}

func (s *NotificationStore) UpdateDelivery(ctx context.Context, id uuid.UUID, sentAt, deliveredAt, failedAt *time.Time, retryCount int, channelsSent []string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE notifications
		SET sent_at = $2, delivered_at = $3, failed_at = $4, retry_count = $5, channels_sent = $6
		WHERE id = $1`,
		id, sentAt, deliveredAt, failedAt, retryCount, channelsSent)
	if err != nil {
		return fmt.Errorf("store: update delivery: %w", err)
	}
	return nil
}

type NotificationConfigStore struct {
	db *pgxpool.Pool
}

func NewNotificationConfigStore(db *pgxpool.Pool) *NotificationConfigStore {
	return &NotificationConfigStore{db: db}
}

func (s *NotificationConfigStore) ListByUser(ctx context.Context, tenantID, userID uuid.UUID) ([]NotificationConfigRow, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, tenant_id, user_id, event_type, scope_type, scope_ref_id,
			channels, threshold_type, threshold_value, enabled, created_at, updated_at
		FROM notification_configs
		WHERE tenant_id = $1 AND user_id = $2
		ORDER BY event_type, scope_type`, tenantID, userID)
	if err != nil {
		return nil, fmt.Errorf("store: list notification configs: %w", err)
	}
	defer rows.Close()

	var results []NotificationConfigRow
	for rows.Next() {
		var c NotificationConfigRow
		err := rows.Scan(
			&c.ID, &c.TenantID, &c.UserID, &c.EventType, &c.ScopeType, &c.ScopeRefID,
			&c.Channels, &c.ThresholdType, &c.ThresholdValue, &c.Enabled, &c.CreatedAt, &c.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("store: scan config: %w", err)
		}
		results = append(results, c)
	}
	return results, nil
}

func (s *NotificationConfigStore) Upsert(ctx context.Context, p UpsertNotificationConfigParams) (*NotificationConfigRow, error) {
	row := s.db.QueryRow(ctx, `
		INSERT INTO notification_configs (tenant_id, user_id, event_type, scope_type, scope_ref_id, channels, threshold_type, threshold_value, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (tenant_id, user_id, event_type, scope_type) WHERE user_id IS NOT NULL
		DO UPDATE SET channels = $6, threshold_type = $7, threshold_value = $8, enabled = $9, updated_at = NOW()
		RETURNING id, tenant_id, user_id, event_type, scope_type, scope_ref_id,
			channels, threshold_type, threshold_value, enabled, created_at, updated_at`,
		p.TenantID, p.UserID, p.EventType, p.ScopeType, p.ScopeRefID,
		p.Channels, p.ThresholdType, p.ThresholdValue, p.Enabled,
	)

	var c NotificationConfigRow
	err := row.Scan(
		&c.ID, &c.TenantID, &c.UserID, &c.EventType, &c.ScopeType, &c.ScopeRefID,
		&c.Channels, &c.ThresholdType, &c.ThresholdValue, &c.Enabled, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("store: upsert config: %w", err)
	}
	return &c, nil
}

func (s *NotificationConfigStore) GetEnabledForEvent(ctx context.Context, tenantID uuid.UUID, eventType, scopeType string) ([]NotificationConfigRow, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, tenant_id, user_id, event_type, scope_type, scope_ref_id,
			channels, threshold_type, threshold_value, enabled, created_at, updated_at
		FROM notification_configs
		WHERE tenant_id = $1 AND event_type = $2 AND scope_type = $3 AND enabled = true
		ORDER BY user_id`, tenantID, eventType, scopeType)
	if err != nil {
		return nil, fmt.Errorf("store: get enabled configs: %w", err)
	}
	defer rows.Close()

	var results []NotificationConfigRow
	for rows.Next() {
		var c NotificationConfigRow
		err := rows.Scan(
			&c.ID, &c.TenantID, &c.UserID, &c.EventType, &c.ScopeType, &c.ScopeRefID,
			&c.Channels, &c.ThresholdType, &c.ThresholdValue, &c.Enabled, &c.CreatedAt, &c.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("store: scan config: %w", err)
		}
		results = append(results, c)
	}
	return results, nil
}

func (s *NotificationConfigStore) DeleteByUser(ctx context.Context, tenantID, userID uuid.UUID) error {
	_, err := s.db.Exec(ctx, `
		DELETE FROM notification_configs
		WHERE tenant_id = $1 AND user_id = $2`, tenantID, userID)
	if err != nil {
		return fmt.Errorf("store: delete configs: %w", err)
	}
	return nil
}

func scanNotification(row pgx.Row) (*NotificationRow, error) {
	var n NotificationRow
	err := row.Scan(
		&n.ID, &n.TenantID, &n.UserID, &n.EventType, &n.ScopeType, &n.ScopeRefID,
		&n.Title, &n.Body, &n.Severity,
		&n.ChannelsSent, &n.State, &n.ReadAt,
		&n.SentAt, &n.DeliveredAt, &n.FailedAt, &n.RetryCount, &n.DeliveryMeta,
		&n.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func scanNotificationRows(rows pgx.Rows) (*NotificationRow, error) {
	var n NotificationRow
	err := rows.Scan(
		&n.ID, &n.TenantID, &n.UserID, &n.EventType, &n.ScopeType, &n.ScopeRefID,
		&n.Title, &n.Body, &n.Severity,
		&n.ChannelsSent, &n.State, &n.ReadAt,
		&n.SentAt, &n.DeliveredAt, &n.FailedAt, &n.RetryCount, &n.DeliveryMeta,
		&n.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

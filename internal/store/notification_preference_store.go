package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/btopcu/argus/internal/severity"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrPreferenceNotFound = errors.New("store: notification preference not found")
)

type NotificationPreference struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	EventType         string
	Channels          []string
	SeverityThreshold string
	Enabled           bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type NotificationPreferenceStore struct {
	db *pgxpool.Pool
}

func NewNotificationPreferenceStore(db *pgxpool.Pool) *NotificationPreferenceStore {
	return &NotificationPreferenceStore{db: db}
}

// GetMatrix returns all preference rows for the tenant. Returns an empty slice
// when no rows exist — the Dispatch layer (Task 15) decides default behavior
// for missing rows.
func (s *NotificationPreferenceStore) GetMatrix(ctx context.Context, tenantID uuid.UUID) ([]*NotificationPreference, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, tenant_id, event_type, channels, severity_threshold, enabled, created_at, updated_at
		FROM notification_preferences
		WHERE tenant_id = $1
		ORDER BY event_type`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("store: get preference matrix: %w", err)
	}
	defer rows.Close()

	var results []*NotificationPreference
	for rows.Next() {
		p, err := scanPreferenceRows(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan preference: %w", err)
		}
		results = append(results, p)
	}
	if results == nil {
		results = []*NotificationPreference{}
	}
	return results, nil
}

// Upsert replaces the full preference matrix for a tenant atomically.
// It deletes all existing preferences for the tenant, then inserts the new list.
// Empty prefs slice is valid — clears all preferences.
func (s *NotificationPreferenceStore) Upsert(ctx context.Context, tenantID uuid.UUID, prefs []NotificationPreference) error {
	for i, p := range prefs {
		if !severity.IsValid(p.SeverityThreshold) {
			return fmt.Errorf("%w (row %d, value %q)", severity.ErrInvalidSeverity, i, p.SeverityThreshold)
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		DELETE FROM notification_preferences WHERE tenant_id = $1`, tenantID); err != nil {
		return fmt.Errorf("store: delete preferences: %w", err)
	}

	for _, p := range prefs {
		channels := p.Channels
		if channels == nil {
			channels = []string{}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO notification_preferences
				(tenant_id, event_type, channels, severity_threshold, enabled)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (tenant_id, event_type)
			DO UPDATE SET channels = $3, severity_threshold = $4, enabled = $5, updated_at = NOW()`,
			tenantID, p.EventType, channels, p.SeverityThreshold, p.Enabled,
		); err != nil {
			return fmt.Errorf("store: upsert preference row (%s): %w", p.EventType, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit preferences upsert: %w", err)
	}
	return nil
}

// Get returns a single preference by (tenant, event_type).
// Returns nil, nil when not found — no sentinel, callers treat nil as "use defaults".
func (s *NotificationPreferenceStore) Get(ctx context.Context, tenantID uuid.UUID, eventType string) (*NotificationPreference, error) {
	row := s.db.QueryRow(ctx, `
		SELECT id, tenant_id, event_type, channels, severity_threshold, enabled, created_at, updated_at
		FROM notification_preferences
		WHERE tenant_id = $1 AND event_type = $2`, tenantID, eventType)

	p, err := scanPreference(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: get preference: %w", err)
	}
	return p, nil
}

func scanPreference(row pgx.Row) (*NotificationPreference, error) {
	var p NotificationPreference
	err := row.Scan(
		&p.ID, &p.TenantID, &p.EventType, &p.Channels,
		&p.SeverityThreshold, &p.Enabled, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func scanPreferenceRows(rows pgx.Rows) (*NotificationPreference, error) {
	var p NotificationPreference
	err := rows.Scan(
		&p.ID, &p.TenantID, &p.EventType, &p.Channels,
		&p.SeverityThreshold, &p.Enabled, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

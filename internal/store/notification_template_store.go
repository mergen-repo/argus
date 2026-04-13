package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrTemplateNotFound = errors.New("store: notification template not found")
)

type NotificationTemplate struct {
	EventType string
	Locale    string
	Subject   string
	BodyText  string
	BodyHTML  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type NotificationTemplateStore struct {
	db *pgxpool.Pool
}

func NewNotificationTemplateStore(db *pgxpool.Pool) *NotificationTemplateStore {
	return &NotificationTemplateStore{db: db}
}

// Get looks up a template by (event_type, locale). If the requested locale row
// is missing, it falls back to 'en' (AC-8). Returns ErrTemplateNotFound when
// both the requested locale and 'en' are absent.
func (s *NotificationTemplateStore) Get(ctx context.Context, eventType, locale string) (*NotificationTemplate, error) {
	row := s.db.QueryRow(ctx, `
		SELECT event_type, locale, subject, body_text, body_html, created_at, updated_at
		FROM notification_templates
		WHERE event_type = $1 AND locale IN ($2, 'en')
		ORDER BY CASE WHEN locale = $2 THEN 0 ELSE 1 END
		LIMIT 1`, eventType, locale)

	t, err := scanTemplate(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTemplateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get template: %w", err)
	}
	return t, nil
}

// List returns all templates optionally filtered by event_type and/or locale.
// Empty string arguments are treated as "no filter".
func (s *NotificationTemplateStore) List(ctx context.Context, eventType, locale string) ([]*NotificationTemplate, error) {
	query := `
		SELECT event_type, locale, subject, body_text, body_html, created_at, updated_at
		FROM notification_templates
		WHERE 1=1`
	args := []interface{}{}
	argIdx := 1

	if eventType != "" {
		query += fmt.Sprintf(" AND event_type = $%d", argIdx)
		args = append(args, eventType)
		argIdx++
	}
	if locale != "" {
		query += fmt.Sprintf(" AND locale = $%d", argIdx)
		args = append(args, locale)
		argIdx++
	}
	query += " ORDER BY event_type, locale"

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list templates: %w", err)
	}
	defer rows.Close()

	var results []*NotificationTemplate
	for rows.Next() {
		t, err := scanTemplateRows(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan template: %w", err)
		}
		results = append(results, t)
	}
	if results == nil {
		results = []*NotificationTemplate{}
	}
	return results, nil
}

// Upsert inserts or overwrites a template row identified by (event_type, locale).
func (s *NotificationTemplateStore) Upsert(ctx context.Context, t *NotificationTemplate) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO notification_templates (event_type, locale, subject, body_text, body_html)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (event_type, locale)
		DO UPDATE SET subject = $3, body_text = $4, body_html = $5, updated_at = NOW()`,
		t.EventType, t.Locale, t.Subject, t.BodyText, t.BodyHTML,
	)
	if err != nil {
		return fmt.Errorf("store: upsert template: %w", err)
	}
	return nil
}

func scanTemplate(row pgx.Row) (*NotificationTemplate, error) {
	var t NotificationTemplate
	err := row.Scan(
		&t.EventType, &t.Locale, &t.Subject, &t.BodyText, &t.BodyHTML,
		&t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func scanTemplateRows(rows pgx.Rows) (*NotificationTemplate, error) {
	var t NotificationTemplate
	err := rows.Scan(
		&t.EventType, &t.Locale, &t.Subject, &t.BodyText, &t.BodyHTML,
		&t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

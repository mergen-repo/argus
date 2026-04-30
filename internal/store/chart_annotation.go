package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrChartAnnotationNotFound = errors.New("store: chart annotation not found")

type ChartAnnotation struct {
	ID        uuid.UUID  `json:"id"`
	TenantID  uuid.UUID  `json:"tenant_id"`
	UserID    *uuid.UUID `json:"user_id"`
	ChartKey  string     `json:"chart_key"`
	Timestamp time.Time  `json:"timestamp"`
	Label     string     `json:"label"`
	CreatedAt time.Time  `json:"created_at"`
}

type ChartAnnotationStore struct {
	db *pgxpool.Pool
}

func NewChartAnnotationStore(db *pgxpool.Pool) *ChartAnnotationStore {
	return &ChartAnnotationStore{db: db}
}

func (s *ChartAnnotationStore) Create(ctx context.Context, tenantID, userID uuid.UUID, chartKey string, ts time.Time, label string) (*ChartAnnotation, error) {
	var a ChartAnnotation
	err := s.db.QueryRow(ctx, `
		INSERT INTO chart_annotations (tenant_id, user_id, chart_key, timestamp, label)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, tenant_id, user_id, chart_key, timestamp, label, created_at`,
		tenantID, userID, chartKey, ts, label,
	).Scan(&a.ID, &a.TenantID, &a.UserID, &a.ChartKey, &a.Timestamp, &a.Label, &a.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("store: create chart annotation: %w", err)
	}
	return &a, nil
}

func (s *ChartAnnotationStore) List(ctx context.Context, tenantID uuid.UUID, chartKey string, from, to time.Time) ([]ChartAnnotation, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, tenant_id, user_id, chart_key, timestamp, label, created_at
		FROM chart_annotations
		WHERE tenant_id = $1 AND chart_key = $2 AND timestamp BETWEEN $3 AND $4
		ORDER BY timestamp ASC`,
		tenantID, chartKey, from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list chart annotations: %w", err)
	}
	defer rows.Close()

	var list []ChartAnnotation
	for rows.Next() {
		var a ChartAnnotation
		if err := rows.Scan(&a.ID, &a.TenantID, &a.UserID, &a.ChartKey, &a.Timestamp, &a.Label, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan chart annotation: %w", err)
		}
		list = append(list, a)
	}
	if list == nil {
		list = []ChartAnnotation{}
	}
	return list, nil
}

func (s *ChartAnnotationStore) Delete(ctx context.Context, tenantID, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM chart_annotations WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	if err != nil {
		return fmt.Errorf("store: delete chart annotation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrChartAnnotationNotFound
	}
	return nil
}

func (s *ChartAnnotationStore) getByID(ctx context.Context, id uuid.UUID) (*ChartAnnotation, error) {
	var a ChartAnnotation
	err := s.db.QueryRow(ctx, `
		SELECT id, tenant_id, user_id, chart_key, timestamp, label, created_at
		FROM chart_annotations WHERE id = $1`, id,
	).Scan(&a.ID, &a.TenantID, &a.UserID, &a.ChartKey, &a.Timestamp, &a.Label, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrChartAnnotationNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get chart annotation: %w", err)
	}
	return &a, nil
}

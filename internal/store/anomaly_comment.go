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

var ErrAnomalyCommentNotFound = errors.New("store: anomaly comment not found")

type AnomalyComment struct {
	ID        uuid.UUID `json:"id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	AnomalyID uuid.UUID `json:"anomaly_id"`
	UserID    uuid.UUID `json:"user_id"`
	UserEmail string    `json:"user_email"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

type AnomalyCommentStore struct {
	db *pgxpool.Pool
}

func NewAnomalyCommentStore(db *pgxpool.Pool) *AnomalyCommentStore {
	return &AnomalyCommentStore{db: db}
}

func (s *AnomalyCommentStore) Create(ctx context.Context, tenantID, anomalyID, userID uuid.UUID, body string) (*AnomalyComment, error) {
	row := s.db.QueryRow(ctx, `
		INSERT INTO anomaly_comments (tenant_id, anomaly_id, user_id, body)
		VALUES ($1, $2, $3, $4)
		RETURNING id, tenant_id, anomaly_id, user_id, body, created_at
	`, tenantID, anomalyID, userID, body)

	var c AnomalyComment
	err := row.Scan(&c.ID, &c.TenantID, &c.AnomalyID, &c.UserID, &c.Body, &c.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("store: create anomaly comment: %w", err)
	}

	email, _ := s.getUserEmail(ctx, userID)
	c.UserEmail = email

	return &c, nil
}

func (s *AnomalyCommentStore) ListByAnomaly(ctx context.Context, tenantID, anomalyID uuid.UUID) ([]AnomalyComment, error) {
	rows, err := s.db.Query(ctx, `
		SELECT ac.id, ac.tenant_id, ac.anomaly_id, ac.user_id,
		       COALESCE(u.email, ''), ac.body, ac.created_at
		FROM anomaly_comments ac
		LEFT JOIN users u ON u.id = ac.user_id
		WHERE ac.tenant_id = $1 AND ac.anomaly_id = $2
		ORDER BY ac.created_at ASC
	`, tenantID, anomalyID)
	if err != nil {
		return nil, fmt.Errorf("store: list anomaly comments: %w", err)
	}
	defer rows.Close()

	var results []AnomalyComment
	for rows.Next() {
		var c AnomalyComment
		if err := rows.Scan(&c.ID, &c.TenantID, &c.AnomalyID, &c.UserID, &c.UserEmail, &c.Body, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan anomaly comment: %w", err)
		}
		results = append(results, c)
	}
	return results, nil
}

func (s *AnomalyCommentStore) getUserEmail(ctx context.Context, userID uuid.UUID) (string, error) {
	var email string
	err := s.db.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, userID).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return email, err
}

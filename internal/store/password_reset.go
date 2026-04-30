package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PasswordResetToken struct {
	ID           uuid.UUID
	UserID       uuid.UUID
	TokenHash    [32]byte
	EmailRateKey string
	ExpiresAt    time.Time
	CreatedAt    time.Time
}

type PasswordResetStore struct {
	db *pgxpool.Pool
}

func NewPasswordResetStore(db *pgxpool.Pool) *PasswordResetStore {
	return &PasswordResetStore{db: db}
}

func (s *PasswordResetStore) Create(ctx context.Context, userID uuid.UUID, tokenHash [32]byte, emailRateKey string, expiresAt time.Time) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO password_reset_tokens (user_id, token_hash, email_rate_key, expires_at)
		 VALUES ($1, $2, $3, $4)`,
		userID, tokenHash[:], emailRateKey, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("store: create password reset token: %w", err)
	}
	return nil
}

func (s *PasswordResetStore) FindByHash(ctx context.Context, tokenHash [32]byte) (*PasswordResetToken, error) {
	row := s.db.QueryRow(ctx,
		`SELECT id, user_id, token_hash, email_rate_key, expires_at, created_at
		 FROM password_reset_tokens
		 WHERE token_hash = $1 AND expires_at > NOW()`,
		tokenHash[:],
	)

	var t PasswordResetToken
	var hashBytes []byte
	err := row.Scan(&t.ID, &t.UserID, &hashBytes, &t.EmailRateKey, &t.ExpiresAt, &t.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: find password reset token: %w", err)
	}
	copy(t.TokenHash[:], hashBytes)
	return &t, nil
}

func (s *PasswordResetStore) DeleteByHash(ctx context.Context, tokenHash [32]byte) error {
	_, err := s.db.Exec(ctx,
		`DELETE FROM password_reset_tokens WHERE token_hash = $1`,
		tokenHash[:],
	)
	if err != nil {
		return fmt.Errorf("store: delete password reset token by hash: %w", err)
	}
	return nil
}

func (s *PasswordResetStore) DeleteAllForUser(ctx context.Context, userID uuid.UUID) error {
	_, err := s.db.Exec(ctx,
		`DELETE FROM password_reset_tokens WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("store: delete all password reset tokens for user: %w", err)
	}
	return nil
}

func (s *PasswordResetStore) CountRecentForEmail(ctx context.Context, emailRateKey string, window time.Duration) (int, error) {
	cutoff := time.Now().UTC().Add(-window)
	var count int
	err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM password_reset_tokens
		 WHERE email_rate_key = $1 AND created_at > $2`,
		emailRateKey, cutoff,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: count recent password reset tokens: %w", err)
	}
	return count, nil
}

func (s *PasswordResetStore) PurgeExpired(ctx context.Context) error {
	_, err := s.db.Exec(ctx,
		`DELETE FROM password_reset_tokens WHERE expires_at < NOW()`,
	)
	if err != nil {
		return fmt.Errorf("store: purge expired password reset tokens: %w", err)
	}
	return nil
}

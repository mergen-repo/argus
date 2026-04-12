package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PasswordHistoryStore struct {
	pool *pgxpool.Pool
}

func NewPasswordHistoryStore(pool *pgxpool.Pool) *PasswordHistoryStore {
	return &PasswordHistoryStore{pool: pool}
}

func (s *PasswordHistoryStore) Insert(ctx context.Context, userID uuid.UUID, hash string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO password_history (user_id, password_hash, created_at) VALUES ($1, $2, NOW())`,
		userID, hash)
	if err != nil {
		return fmt.Errorf("store: insert password history: %w", err)
	}
	return nil
}

func (s *PasswordHistoryStore) GetLastN(ctx context.Context, userID uuid.UUID, n int) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT password_hash FROM password_history WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2`,
		userID, n)
	if err != nil {
		return nil, fmt.Errorf("store: get last n password hashes: %w", err)
	}
	defer rows.Close()

	var hashes []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, fmt.Errorf("store: scan password hash: %w", err)
		}
		hashes = append(hashes, h)
	}
	return hashes, nil
}

func (s *PasswordHistoryStore) Trim(ctx context.Context, userID uuid.UUID, keep int) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM password_history
		 WHERE user_id = $1
		   AND id NOT IN (
		       SELECT id FROM password_history
		       WHERE user_id = $1
		       ORDER BY created_at DESC
		       LIMIT $2
		   )`,
		userID, keep)
	if err != nil {
		return fmt.Errorf("store: trim password history: %w", err)
	}
	return nil
}

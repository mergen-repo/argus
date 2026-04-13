package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UserColumnPref struct {
	UserID      uuid.UUID       `json:"user_id"`
	PageKey     string          `json:"page_key"`
	ColumnsJSON json.RawMessage `json:"columns_json"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type UserColumnPrefStore struct {
	db *pgxpool.Pool
}

func NewUserColumnPrefStore(db *pgxpool.Pool) *UserColumnPrefStore {
	return &UserColumnPrefStore{db: db}
}

func (s *UserColumnPrefStore) Upsert(ctx context.Context, userID uuid.UUID, pageKey string, columns json.RawMessage) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO user_column_preferences (user_id, page_key, columns_json, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (user_id, page_key)
		DO UPDATE SET columns_json = EXCLUDED.columns_json, updated_at = NOW()`,
		userID, pageKey, columns,
	)
	if err != nil {
		return fmt.Errorf("store: upsert column prefs: %w", err)
	}
	return nil
}

func (s *UserColumnPrefStore) Get(ctx context.Context, userID uuid.UUID, pageKey string) (*UserColumnPref, error) {
	var p UserColumnPref
	err := s.db.QueryRow(ctx,
		`SELECT user_id, page_key, columns_json, updated_at FROM user_column_preferences WHERE user_id = $1 AND page_key = $2`,
		userID, pageKey,
	).Scan(&p.UserID, &p.PageKey, &p.ColumnsJSON, &p.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("store: get column prefs: %w", err)
	}
	return &p, nil
}

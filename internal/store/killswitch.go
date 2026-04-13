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

var ErrKillSwitchNotFound = errors.New("store: kill switch not found")

type KillSwitch struct {
	Key         string     `json:"key"`
	Label       string     `json:"label"`
	Description string     `json:"description"`
	Enabled     bool       `json:"enabled"`
	Reason      *string    `json:"reason"`
	ToggledBy   *uuid.UUID `json:"toggled_by"`
	ToggledAt   *time.Time `json:"toggled_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

type KillSwitchStore struct {
	db *pgxpool.Pool
}

func NewKillSwitchStore(db *pgxpool.Pool) *KillSwitchStore {
	return &KillSwitchStore{db: db}
}

func (s *KillSwitchStore) List(ctx context.Context) ([]KillSwitch, error) {
	rows, err := s.db.Query(ctx, `
		SELECT key, label, description, enabled, reason, toggled_by, toggled_at, created_at
		FROM kill_switches
		ORDER BY key
	`)
	if err != nil {
		return nil, fmt.Errorf("store: list kill switches: %w", err)
	}
	defer rows.Close()

	var results []KillSwitch
	for rows.Next() {
		var ks KillSwitch
		if err := rows.Scan(&ks.Key, &ks.Label, &ks.Description, &ks.Enabled,
			&ks.Reason, &ks.ToggledBy, &ks.ToggledAt, &ks.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan kill switch: %w", err)
		}
		results = append(results, ks)
	}
	if results == nil {
		results = []KillSwitch{}
	}
	return results, nil
}

func (s *KillSwitchStore) GetByKey(ctx context.Context, key string) (*KillSwitch, error) {
	var ks KillSwitch
	err := s.db.QueryRow(ctx, `
		SELECT key, label, description, enabled, reason, toggled_by, toggled_at, created_at
		FROM kill_switches
		WHERE key = $1
	`, key).Scan(&ks.Key, &ks.Label, &ks.Description, &ks.Enabled,
		&ks.Reason, &ks.ToggledBy, &ks.ToggledAt, &ks.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrKillSwitchNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get kill switch: %w", err)
	}
	return &ks, nil
}

func (s *KillSwitchStore) Toggle(ctx context.Context, key string, enabled bool, reason *string, actorUserID *uuid.UUID) (*KillSwitch, error) {
	now := time.Now().UTC()
	var ks KillSwitch
	err := s.db.QueryRow(ctx, `
		UPDATE kill_switches
		SET enabled = $1, reason = $2, toggled_by = $3, toggled_at = $4
		WHERE key = $5
		RETURNING key, label, description, enabled, reason, toggled_by, toggled_at, created_at
	`, enabled, reason, actorUserID, now, key).
		Scan(&ks.Key, &ks.Label, &ks.Description, &ks.Enabled,
			&ks.Reason, &ks.ToggledBy, &ks.ToggledAt, &ks.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrKillSwitchNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: toggle kill switch: %w", err)
	}
	return &ks, nil
}

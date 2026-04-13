package undo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	keyPrefix = "undo:"
	ttl       = 15 * time.Second
)

var (
	ErrExpired      = errors.New("undo: action expired or not found")
	ErrTenantDenied = errors.New("undo: tenant mismatch")
)

type Entry struct {
	Action    string          `json:"action"`
	Payload   json.RawMessage `json:"payload"`
	TenantID  uuid.UUID       `json:"tenant_id"`
	UserID    uuid.UUID       `json:"user_id"`
	IssuedAt  time.Time       `json:"issued_at"`
}

type Registry struct {
	rdb *redis.Client
}

func NewRegistry(rdb *redis.Client) *Registry {
	return &Registry{rdb: rdb}
}

func (r *Registry) Register(ctx context.Context, tenantID, userID uuid.UUID, action string, payload interface{}) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("undo: marshal payload: %w", err)
	}

	entry := Entry{
		Action:   action,
		Payload:  raw,
		TenantID: tenantID,
		UserID:   userID,
		IssuedAt: time.Now().UTC(),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return "", fmt.Errorf("undo: marshal entry: %w", err)
	}

	actionID := uuid.New().String()
	key := keyPrefix + actionID

	if err := r.rdb.Set(ctx, key, data, ttl).Err(); err != nil {
		return "", fmt.Errorf("undo: redis set: %w", err)
	}

	return actionID, nil
}

func (r *Registry) Consume(ctx context.Context, tenantID uuid.UUID, actionID string) (*Entry, error) {
	key := keyPrefix + actionID

	data, err := r.rdb.GetDel(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrExpired
		}
		return nil, fmt.Errorf("undo: redis getdel: %w", err)
	}

	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("undo: unmarshal entry: %w", err)
	}

	if entry.TenantID != tenantID {
		return nil, ErrTenantDenied
	}

	return &entry, nil
}

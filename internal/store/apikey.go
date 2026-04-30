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

var ErrAPIKeyNotFound = errors.New("store: api key not found")

type APIKey struct {
	ID                 uuid.UUID
	TenantID           uuid.UUID
	Name               string
	KeyPrefix          string
	KeyHash            string
	Scopes             []string
	AllowedIPs         []string
	RateLimitPerMinute int
	RateLimitPerHour   int
	ExpiresAt          *time.Time
	RevokedAt          *time.Time
	LastUsedAt         *time.Time
	UsageCount         int64
	CreatedAt          time.Time
	CreatedBy          *uuid.UUID
	PreviousKeyHash    *string
	KeyRotatedAt       *time.Time
}

type CreateAPIKeyParams struct {
	Name               string
	KeyPrefix          string
	KeyHash            string
	Scopes             []string
	AllowedIPs         []string
	RateLimitPerMinute int
	RateLimitPerHour   int
	ExpiresAt          *time.Time
	CreatedBy          *uuid.UUID
}

type UpdateAPIKeyParams struct {
	Name               *string
	Scopes             *[]string
	AllowedIPs         *[]string
	RateLimitPerMinute *int
	RateLimitPerHour   *int
}

type APIKeyStore struct {
	db *pgxpool.Pool
}

func NewAPIKeyStore(db *pgxpool.Pool) *APIKeyStore {
	return &APIKeyStore{db: db}
}

func (s *APIKeyStore) Create(ctx context.Context, p CreateAPIKeyParams) (*APIKey, error) {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	scopesJSON, err := json.Marshal(p.Scopes)
	if err != nil {
		return nil, fmt.Errorf("store: marshal scopes: %w", err)
	}

	allowedIPs := p.AllowedIPs
	if allowedIPs == nil {
		allowedIPs = []string{}
	}

	var k APIKey
	var scopesRaw json.RawMessage
	err = s.db.QueryRow(ctx, `
		INSERT INTO api_keys (tenant_id, name, key_prefix, key_hash, scopes, allowed_ips, rate_limit_per_minute, rate_limit_per_hour, expires_at, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, tenant_id, name, key_prefix, key_hash, scopes, allowed_ips, rate_limit_per_minute, rate_limit_per_hour,
			expires_at, revoked_at, last_used_at, usage_count, created_at, created_by
	`, tenantID, p.Name, p.KeyPrefix, p.KeyHash, scopesJSON, allowedIPs, p.RateLimitPerMinute, p.RateLimitPerHour, p.ExpiresAt, p.CreatedBy).
		Scan(&k.ID, &k.TenantID, &k.Name, &k.KeyPrefix, &k.KeyHash, &scopesRaw,
			&k.AllowedIPs, &k.RateLimitPerMinute, &k.RateLimitPerHour, &k.ExpiresAt, &k.RevokedAt,
			&k.LastUsedAt, &k.UsageCount, &k.CreatedAt, &k.CreatedBy)
	if err != nil {
		return nil, fmt.Errorf("store: create api key: %w", err)
	}

	if err := json.Unmarshal(scopesRaw, &k.Scopes); err != nil {
		return nil, fmt.Errorf("store: unmarshal scopes: %w", err)
	}
	if k.AllowedIPs == nil {
		k.AllowedIPs = []string{}
	}

	return &k, nil
}

func (s *APIKeyStore) GetByID(ctx context.Context, id uuid.UUID) (*APIKey, error) {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.scanAPIKey(s.db.QueryRow(ctx, `
		SELECT id, tenant_id, name, key_prefix, key_hash, scopes, allowed_ips, rate_limit_per_minute, rate_limit_per_hour,
			expires_at, revoked_at, last_used_at, usage_count, created_at, created_by, previous_key_hash, key_rotated_at
		FROM api_keys
		WHERE id = $1 AND tenant_id = $2
	`, id, tenantID))
}

func (s *APIKeyStore) GetByPrefix(ctx context.Context, prefix string) (*APIKey, error) {
	return s.scanAPIKey(s.db.QueryRow(ctx, `
		SELECT id, tenant_id, name, key_prefix, key_hash, scopes, allowed_ips, rate_limit_per_minute, rate_limit_per_hour,
			expires_at, revoked_at, last_used_at, usage_count, created_at, created_by, previous_key_hash, key_rotated_at
		FROM api_keys
		WHERE key_prefix = $1
	`, prefix))
}

func (s *APIKeyStore) scanAPIKey(row pgx.Row) (*APIKey, error) {
	var k APIKey
	var scopesRaw json.RawMessage
	err := row.Scan(&k.ID, &k.TenantID, &k.Name, &k.KeyPrefix, &k.KeyHash, &scopesRaw,
		&k.AllowedIPs, &k.RateLimitPerMinute, &k.RateLimitPerHour, &k.ExpiresAt, &k.RevokedAt,
		&k.LastUsedAt, &k.UsageCount, &k.CreatedAt, &k.CreatedBy, &k.PreviousKeyHash, &k.KeyRotatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: scan api key: %w", err)
	}

	if err := json.Unmarshal(scopesRaw, &k.Scopes); err != nil {
		return nil, fmt.Errorf("store: unmarshal scopes: %w", err)
	}
	if k.AllowedIPs == nil {
		k.AllowedIPs = []string{}
	}

	return &k, nil
}

func (s *APIKeyStore) ListByTenant(ctx context.Context, cursor string, limit int) ([]APIKey, string, error) {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return nil, "", err
	}

	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{tenantID}
	conditions := []string{"tenant_id = $1"}
	argIdx := 2

	if cursor != "" {
		cursorID, parseErr := uuid.Parse(cursor)
		if parseErr == nil {
			conditions = append(conditions, fmt.Sprintf("id < $%d", argIdx))
			args = append(args, cursorID)
			argIdx++
		}
	}

	where := "WHERE " + strings.Join(conditions, " AND ")

	args = append(args, limit+1)
	limitPlaceholder := fmt.Sprintf("$%d", argIdx)

	query := fmt.Sprintf(`
		SELECT id, tenant_id, name, key_prefix, key_hash, scopes, allowed_ips, rate_limit_per_minute, rate_limit_per_hour,
			expires_at, revoked_at, last_used_at, usage_count, created_at, created_by, previous_key_hash, key_rotated_at
		FROM api_keys
		%s
		ORDER BY created_at DESC, id DESC
		LIMIT %s
	`, where, limitPlaceholder)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list api keys: %w", err)
	}
	defer rows.Close()

	var results []APIKey
	for rows.Next() {
		var k APIKey
		var scopesRaw json.RawMessage
		if err := rows.Scan(&k.ID, &k.TenantID, &k.Name, &k.KeyPrefix, &k.KeyHash, &scopesRaw,
			&k.AllowedIPs, &k.RateLimitPerMinute, &k.RateLimitPerHour, &k.ExpiresAt, &k.RevokedAt,
			&k.LastUsedAt, &k.UsageCount, &k.CreatedAt, &k.CreatedBy, &k.PreviousKeyHash, &k.KeyRotatedAt); err != nil {
			return nil, "", fmt.Errorf("store: scan api key: %w", err)
		}
		if err := json.Unmarshal(scopesRaw, &k.Scopes); err != nil {
			return nil, "", fmt.Errorf("store: unmarshal scopes: %w", err)
		}
		if k.AllowedIPs == nil {
			k.AllowedIPs = []string{}
		}
		results = append(results, k)
	}

	nextCursor := ""
	if len(results) > limit {
		nextCursor = results[limit-1].ID.String()
		results = results[:limit]
	}

	return results, nextCursor, nil
}

func (s *APIKeyStore) Update(ctx context.Context, id uuid.UUID, p UpdateAPIKeyParams) (*APIKey, error) {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	sets := []string{}
	args := []interface{}{id, tenantID}
	argIdx := 3

	if p.Name != nil {
		sets = append(sets, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *p.Name)
		argIdx++
	}
	if p.Scopes != nil {
		scopesJSON, mErr := json.Marshal(*p.Scopes)
		if mErr != nil {
			return nil, fmt.Errorf("store: marshal scopes: %w", mErr)
		}
		sets = append(sets, fmt.Sprintf("scopes = $%d", argIdx))
		args = append(args, scopesJSON)
		argIdx++
	}
	if p.AllowedIPs != nil {
		ips := *p.AllowedIPs
		if ips == nil {
			ips = []string{}
		}
		sets = append(sets, fmt.Sprintf("allowed_ips = $%d", argIdx))
		args = append(args, ips)
		argIdx++
	}
	if p.RateLimitPerMinute != nil {
		sets = append(sets, fmt.Sprintf("rate_limit_per_minute = $%d", argIdx))
		args = append(args, *p.RateLimitPerMinute)
		argIdx++
	}
	if p.RateLimitPerHour != nil {
		sets = append(sets, fmt.Sprintf("rate_limit_per_hour = $%d", argIdx))
		args = append(args, *p.RateLimitPerHour)
		argIdx++
	}

	if len(sets) == 0 {
		return s.GetByID(ctx, id)
	}

	query := fmt.Sprintf(`
		UPDATE api_keys SET %s
		WHERE id = $1 AND tenant_id = $2 AND revoked_at IS NULL
		RETURNING id, tenant_id, name, key_prefix, key_hash, scopes, allowed_ips, rate_limit_per_minute, rate_limit_per_hour,
			expires_at, revoked_at, last_used_at, usage_count, created_at, created_by, previous_key_hash, key_rotated_at
	`, strings.Join(sets, ", "))

	return s.scanAPIKey(s.db.QueryRow(ctx, query, args...))
}

func (s *APIKeyStore) Revoke(ctx context.Context, id uuid.UUID) error {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return err
	}

	tag, err := s.db.Exec(ctx,
		`UPDATE api_keys SET revoked_at = NOW() WHERE id = $1 AND tenant_id = $2 AND revoked_at IS NULL`, id, tenantID)
	if err != nil {
		return fmt.Errorf("store: revoke api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAPIKeyNotFound
	}
	return nil
}

func (s *APIKeyStore) Rotate(ctx context.Context, id uuid.UUID, newPrefix, newHash string) (*APIKey, error) {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.scanAPIKey(s.db.QueryRow(ctx, `
		UPDATE api_keys SET
			previous_key_hash = key_hash,
			key_hash = $3,
			key_prefix = $4,
			key_rotated_at = NOW()
		WHERE id = $1 AND tenant_id = $2 AND revoked_at IS NULL
		RETURNING id, tenant_id, name, key_prefix, key_hash, scopes, allowed_ips, rate_limit_per_minute, rate_limit_per_hour,
			expires_at, revoked_at, last_used_at, usage_count, created_at, created_by, previous_key_hash, key_rotated_at
	`, id, tenantID, newHash, newPrefix))
}

func (s *APIKeyStore) RevokeAllByUser(ctx context.Context, userID uuid.UUID) (int64, error) {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return 0, err
	}

	tag, err := s.db.Exec(ctx,
		`UPDATE api_keys SET revoked_at = NOW() WHERE created_by = $1 AND tenant_id = $2 AND revoked_at IS NULL`,
		userID, tenantID)
	if err != nil {
		return 0, fmt.Errorf("store: revoke all api keys by user: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (s *APIKeyStore) UpdateUsage(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx,
		`UPDATE api_keys SET usage_count = usage_count + 1, last_used_at = NOW() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("store: update api key usage: %w", err)
	}
	return nil
}

func (s *APIKeyStore) CountByTenant(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var count int
	err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM api_keys WHERE tenant_id = $1 AND revoked_at IS NULL`, tenantID).
		Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: count api keys by tenant: %w", err)
	}
	return count, nil
}

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

var (
	ErrTenantNotFound = errors.New("store: tenant not found")
	ErrDomainExists   = errors.New("store: domain already exists")
)

type Tenant struct {
	ID                 uuid.UUID       `json:"id"`
	Name               string          `json:"name"`
	Domain             *string         `json:"domain"`
	ContactEmail       string          `json:"contact_email"`
	ContactPhone       *string         `json:"contact_phone"`
	MaxSims            int             `json:"max_sims"`
	MaxApns            int             `json:"max_apns"`
	MaxUsers           int             `json:"max_users"`
	PurgeRetentionDays int             `json:"purge_retention_days"`
	Settings           json.RawMessage `json:"settings"`
	State              string          `json:"state"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
	CreatedBy          *uuid.UUID      `json:"created_by,omitempty"`
	UpdatedBy          *uuid.UUID      `json:"updated_by,omitempty"`
}

type TenantStats struct {
	SimCount       int `json:"sim_count"`
	UserCount      int `json:"user_count"`
	APNCount       int `json:"apn_count"`
	ActiveSessions int `json:"active_sessions"`
	StorageBytes   int `json:"storage_bytes"`
}

type CreateTenantParams struct {
	Name         string
	Domain       *string
	ContactEmail string
	ContactPhone *string
	MaxSims      *int
	MaxApns      *int
	MaxUsers     *int
	CreatedBy    *uuid.UUID
}

type UpdateTenantParams struct {
	Name         *string
	ContactEmail *string
	ContactPhone *string
	MaxSims      *int
	MaxApns      *int
	MaxUsers     *int
	State        *string
	Settings     *json.RawMessage
	UpdatedBy    *uuid.UUID
}

type TenantStore struct {
	db *pgxpool.Pool
}

func NewTenantStore(db *pgxpool.Pool) *TenantStore {
	return &TenantStore{db: db}
}

func (s *TenantStore) Create(ctx context.Context, p CreateTenantParams) (*Tenant, error) {
	maxSims := 100000
	if p.MaxSims != nil {
		maxSims = *p.MaxSims
	}
	maxApns := 100
	if p.MaxApns != nil {
		maxApns = *p.MaxApns
	}
	maxUsers := 50
	if p.MaxUsers != nil {
		maxUsers = *p.MaxUsers
	}

	var t Tenant
	err := s.db.QueryRow(ctx, `
		INSERT INTO tenants (name, domain, contact_email, contact_phone, max_sims, max_apns, max_users, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, name, domain, contact_email, contact_phone, max_sims, max_apns, max_users,
			purge_retention_days, settings, state, created_at, updated_at, created_by, updated_by
	`, p.Name, p.Domain, p.ContactEmail, p.ContactPhone, maxSims, maxApns, maxUsers, p.CreatedBy).
		Scan(&t.ID, &t.Name, &t.Domain, &t.ContactEmail, &t.ContactPhone,
			&t.MaxSims, &t.MaxApns, &t.MaxUsers, &t.PurgeRetentionDays,
			&t.Settings, &t.State, &t.CreatedAt, &t.UpdatedAt, &t.CreatedBy, &t.UpdatedBy)
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, ErrDomainExists
		}
		return nil, fmt.Errorf("store: create tenant: %w", err)
	}
	return &t, nil
}

func (s *TenantStore) GetByID(ctx context.Context, id uuid.UUID) (*Tenant, error) {
	var t Tenant
	err := s.db.QueryRow(ctx, `
		SELECT id, name, domain, contact_email, contact_phone, max_sims, max_apns, max_users,
			purge_retention_days, settings, state, created_at, updated_at, created_by, updated_by
		FROM tenants
		WHERE id = $1
	`, id).Scan(&t.ID, &t.Name, &t.Domain, &t.ContactEmail, &t.ContactPhone,
		&t.MaxSims, &t.MaxApns, &t.MaxUsers, &t.PurgeRetentionDays,
		&t.Settings, &t.State, &t.CreatedAt, &t.UpdatedAt, &t.CreatedBy, &t.UpdatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTenantNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get tenant: %w", err)
	}
	return &t, nil
}

func (s *TenantStore) List(ctx context.Context, cursor string, limit int, stateFilter string) ([]Tenant, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{}
	conditions := []string{}
	argIdx := 1

	if stateFilter != "" {
		conditions = append(conditions, fmt.Sprintf("state = $%d", argIdx))
		args = append(args, stateFilter)
		argIdx++
	}

	if cursor != "" {
		cursorID, parseErr := uuid.Parse(cursor)
		if parseErr == nil {
			conditions = append(conditions, fmt.Sprintf("id < $%d", argIdx))
			args = append(args, cursorID)
			argIdx++
		}
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	args = append(args, limit+1)
	limitPlaceholder := fmt.Sprintf("$%d", argIdx)

	query := fmt.Sprintf(`
		SELECT id, name, domain, contact_email, contact_phone, max_sims, max_apns, max_users,
			purge_retention_days, settings, state, created_at, updated_at, created_by, updated_by
		FROM tenants
		%s
		ORDER BY created_at DESC, id DESC
		LIMIT %s
	`, where, limitPlaceholder)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list tenants: %w", err)
	}
	defer rows.Close()

	var results []Tenant
	for rows.Next() {
		var t Tenant
		if err := rows.Scan(&t.ID, &t.Name, &t.Domain, &t.ContactEmail, &t.ContactPhone,
			&t.MaxSims, &t.MaxApns, &t.MaxUsers, &t.PurgeRetentionDays,
			&t.Settings, &t.State, &t.CreatedAt, &t.UpdatedAt, &t.CreatedBy, &t.UpdatedBy); err != nil {
			return nil, "", fmt.Errorf("store: scan tenant: %w", err)
		}
		results = append(results, t)
	}

	nextCursor := ""
	if len(results) > limit {
		nextCursor = results[limit-1].ID.String()
		results = results[:limit]
	}

	return results, nextCursor, nil
}

func (s *TenantStore) Update(ctx context.Context, id uuid.UUID, p UpdateTenantParams) (*Tenant, error) {
	sets := []string{}
	args := []interface{}{id}
	argIdx := 2

	if p.Name != nil {
		sets = append(sets, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *p.Name)
		argIdx++
	}
	if p.ContactEmail != nil {
		sets = append(sets, fmt.Sprintf("contact_email = $%d", argIdx))
		args = append(args, *p.ContactEmail)
		argIdx++
	}
	if p.ContactPhone != nil {
		sets = append(sets, fmt.Sprintf("contact_phone = $%d", argIdx))
		args = append(args, *p.ContactPhone)
		argIdx++
	}
	if p.MaxSims != nil {
		sets = append(sets, fmt.Sprintf("max_sims = $%d", argIdx))
		args = append(args, *p.MaxSims)
		argIdx++
	}
	if p.MaxApns != nil {
		sets = append(sets, fmt.Sprintf("max_apns = $%d", argIdx))
		args = append(args, *p.MaxApns)
		argIdx++
	}
	if p.MaxUsers != nil {
		sets = append(sets, fmt.Sprintf("max_users = $%d", argIdx))
		args = append(args, *p.MaxUsers)
		argIdx++
	}
	if p.State != nil {
		sets = append(sets, fmt.Sprintf("state = $%d", argIdx))
		args = append(args, *p.State)
		argIdx++
	}
	if p.Settings != nil {
		sets = append(sets, fmt.Sprintf("settings = $%d", argIdx))
		args = append(args, *p.Settings)
		argIdx++
	}
	if p.UpdatedBy != nil {
		sets = append(sets, fmt.Sprintf("updated_by = $%d", argIdx))
		args = append(args, *p.UpdatedBy)
		argIdx++
	}

	if len(sets) == 0 {
		return s.GetByID(ctx, id)
	}

	query := fmt.Sprintf(`
		UPDATE tenants SET %s
		WHERE id = $1
		RETURNING id, name, domain, contact_email, contact_phone, max_sims, max_apns, max_users,
			purge_retention_days, settings, state, created_at, updated_at, created_by, updated_by
	`, strings.Join(sets, ", "))

	var t Tenant
	err := s.db.QueryRow(ctx, query, args...).
		Scan(&t.ID, &t.Name, &t.Domain, &t.ContactEmail, &t.ContactPhone,
			&t.MaxSims, &t.MaxApns, &t.MaxUsers, &t.PurgeRetentionDays,
			&t.Settings, &t.State, &t.CreatedAt, &t.UpdatedAt, &t.CreatedBy, &t.UpdatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTenantNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: update tenant: %w", err)
	}
	return &t, nil
}

func (s *TenantStore) GetStats(ctx context.Context, tenantID uuid.UUID) (*TenantStats, error) {
	var stats TenantStats

	err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE tenant_id = $1 AND state != 'terminated'`, tenantID).Scan(&stats.UserCount)
	if err != nil {
		return nil, fmt.Errorf("store: count users: %w", err)
	}

	err = s.db.QueryRow(ctx, `SELECT COUNT(*) FROM sims WHERE tenant_id = $1 AND state != 'purged'`, tenantID).Scan(&stats.SimCount)
	if err != nil {
		return nil, fmt.Errorf("store: count sims: %w", err)
	}

	err = s.db.QueryRow(ctx, `SELECT COUNT(*) FROM apns WHERE tenant_id = $1 AND state != 'archived'`, tenantID).Scan(&stats.APNCount)
	if err != nil {
		return nil, fmt.Errorf("store: count apns: %w", err)
	}

	return &stats, nil
}

func (s *TenantStore) CountActive(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM tenants WHERE state = 'active'`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: count active tenants: %w", err)
	}
	return count, nil
}

func (s *TenantStore) CountUsersByTenant(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var count int
	err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE tenant_id = $1 AND state != 'terminated'`, tenantID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: count users by tenant: %w", err)
	}
	return count, nil
}

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
	ErrAPNNotFound      = errors.New("store: apn not found")
	ErrAPNNameExists    = errors.New("store: apn name already exists for this tenant+operator")
	ErrAPNHasActiveSIMs = errors.New("store: apn has active sims")
)

type APN struct {
	ID                uuid.UUID       `json:"id"`
	TenantID          uuid.UUID       `json:"tenant_id"`
	OperatorID        uuid.UUID       `json:"operator_id"`
	Name              string          `json:"name"`
	DisplayName       *string         `json:"display_name"`
	APNType           string          `json:"apn_type"`
	SupportedRATTypes []string        `json:"supported_rat_types"`
	DefaultPolicyID   *uuid.UUID      `json:"default_policy_id"`
	State             string          `json:"state"`
	Settings          json.RawMessage `json:"settings"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
	CreatedBy         *uuid.UUID      `json:"created_by"`
	UpdatedBy         *uuid.UUID      `json:"updated_by"`
}

type CreateAPNParams struct {
	Name              string
	OperatorID        uuid.UUID
	APNType           string
	SupportedRATTypes []string
	DisplayName       *string
	DefaultPolicyID   *uuid.UUID
	Settings          json.RawMessage
	CreatedBy         *uuid.UUID
}

type UpdateAPNParams struct {
	DisplayName       *string
	SupportedRATTypes []string
	DefaultPolicyID   *uuid.UUID
	Settings          json.RawMessage
	UpdatedBy         *uuid.UUID
}

type APNStore struct {
	db *pgxpool.Pool
}

func NewAPNStore(db *pgxpool.Pool) *APNStore {
	return &APNStore{db: db}
}

var apnColumns = `id, tenant_id, operator_id, name, display_name, apn_type,
	supported_rat_types, default_policy_id, state, settings,
	created_at, updated_at, created_by, updated_by`

func scanAPN(row pgx.Row) (*APN, error) {
	var a APN
	err := row.Scan(
		&a.ID, &a.TenantID, &a.OperatorID, &a.Name, &a.DisplayName,
		&a.APNType, &a.SupportedRATTypes, &a.DefaultPolicyID,
		&a.State, &a.Settings, &a.CreatedAt, &a.UpdatedAt,
		&a.CreatedBy, &a.UpdatedBy,
	)
	return &a, err
}

func (s *APNStore) Create(ctx context.Context, tenantID uuid.UUID, p CreateAPNParams) (*APN, error) {
	settings := json.RawMessage(`{}`)
	if p.Settings != nil && len(p.Settings) > 0 {
		settings = p.Settings
	}
	ratTypes := p.SupportedRATTypes
	if ratTypes == nil {
		ratTypes = []string{}
	}

	row := s.db.QueryRow(ctx, `
		INSERT INTO apns (tenant_id, operator_id, name, display_name, apn_type,
			supported_rat_types, default_policy_id, settings, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
		RETURNING `+apnColumns,
		tenantID, p.OperatorID, p.Name, p.DisplayName, p.APNType,
		ratTypes, p.DefaultPolicyID, settings, p.CreatedBy,
	)

	a, err := scanAPN(row)
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, ErrAPNNameExists
		}
		return nil, fmt.Errorf("store: create apn: %w", err)
	}
	return a, nil
}

func (s *APNStore) GetByID(ctx context.Context, tenantID, id uuid.UUID) (*APN, error) {
	row := s.db.QueryRow(ctx,
		`SELECT `+apnColumns+` FROM apns WHERE id = $1 AND tenant_id = $2`,
		id, tenantID,
	)
	a, err := scanAPN(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAPNNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get apn: %w", err)
	}
	return a, nil
}

func (s *APNStore) GetByName(ctx context.Context, tenantID, operatorID uuid.UUID, name string) (*APN, error) {
	row := s.db.QueryRow(ctx,
		`SELECT `+apnColumns+` FROM apns WHERE tenant_id = $1 AND operator_id = $2 AND name = $3`,
		tenantID, operatorID, name,
	)
	a, err := scanAPN(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAPNNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get apn by name: %w", err)
	}
	return a, nil
}

func (s *APNStore) List(ctx context.Context, tenantID uuid.UUID, cursor string, limit int, stateFilter string, operatorIDFilter *uuid.UUID) ([]APN, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{tenantID}
	conditions := []string{"tenant_id = $1"}
	argIdx := 2

	if stateFilter != "" {
		conditions = append(conditions, fmt.Sprintf("state = $%d", argIdx))
		args = append(args, stateFilter)
		argIdx++
	}

	if operatorIDFilter != nil {
		conditions = append(conditions, fmt.Sprintf("operator_id = $%d", argIdx))
		args = append(args, *operatorIDFilter)
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

	where := "WHERE " + strings.Join(conditions, " AND ")

	args = append(args, limit+1)
	limitPlaceholder := fmt.Sprintf("$%d", argIdx)

	query := fmt.Sprintf(`SELECT %s FROM apns %s ORDER BY created_at DESC, id DESC LIMIT %s`,
		apnColumns, where, limitPlaceholder)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list apns: %w", err)
	}
	defer rows.Close()

	var results []APN
	for rows.Next() {
		var a APN
		if err := rows.Scan(
			&a.ID, &a.TenantID, &a.OperatorID, &a.Name, &a.DisplayName,
			&a.APNType, &a.SupportedRATTypes, &a.DefaultPolicyID,
			&a.State, &a.Settings, &a.CreatedAt, &a.UpdatedAt,
			&a.CreatedBy, &a.UpdatedBy,
		); err != nil {
			return nil, "", fmt.Errorf("store: scan apn: %w", err)
		}
		results = append(results, a)
	}

	nextCursor := ""
	if len(results) > limit {
		nextCursor = results[limit-1].ID.String()
		results = results[:limit]
	}

	return results, nextCursor, nil
}

func (s *APNStore) Update(ctx context.Context, tenantID, id uuid.UUID, p UpdateAPNParams) (*APN, error) {
	sets := []string{}
	args := []interface{}{id, tenantID}
	argIdx := 3

	if p.DisplayName != nil {
		sets = append(sets, fmt.Sprintf("display_name = $%d", argIdx))
		args = append(args, *p.DisplayName)
		argIdx++
	}
	if p.SupportedRATTypes != nil {
		sets = append(sets, fmt.Sprintf("supported_rat_types = $%d", argIdx))
		args = append(args, p.SupportedRATTypes)
		argIdx++
	}
	if p.DefaultPolicyID != nil {
		sets = append(sets, fmt.Sprintf("default_policy_id = $%d", argIdx))
		args = append(args, *p.DefaultPolicyID)
		argIdx++
	}
	if p.Settings != nil && len(p.Settings) > 0 {
		sets = append(sets, fmt.Sprintf("settings = $%d", argIdx))
		args = append(args, p.Settings)
		argIdx++
	}
	if p.UpdatedBy != nil {
		sets = append(sets, fmt.Sprintf("updated_by = $%d", argIdx))
		args = append(args, *p.UpdatedBy)
		argIdx++
	}

	if len(sets) == 0 {
		return s.GetByID(ctx, tenantID, id)
	}

	query := fmt.Sprintf(`UPDATE apns SET %s WHERE id = $1 AND tenant_id = $2 RETURNING %s`,
		strings.Join(sets, ", "), apnColumns)

	row := s.db.QueryRow(ctx, query, args...)
	a, err := scanAPN(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAPNNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: update apn: %w", err)
	}
	return a, nil
}

func (s *APNStore) Archive(ctx context.Context, tenantID, id uuid.UUID) error {
	var activeSIMs int
	err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM sims WHERE apn_id = $1 AND state NOT IN ('terminated', 'purged')`,
		id,
	).Scan(&activeSIMs)
	if err != nil {
		return fmt.Errorf("store: count active sims for apn: %w", err)
	}

	if activeSIMs > 0 {
		return ErrAPNHasActiveSIMs
	}

	tag, err := s.db.Exec(ctx,
		`UPDATE apns SET state = 'archived' WHERE id = $1 AND tenant_id = $2 AND state = 'active'`,
		id, tenantID,
	)
	if err != nil {
		return fmt.Errorf("store: archive apn: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAPNNotFound
	}
	return nil
}

func (s *APNStore) CountByTenant(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var count int
	err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM apns WHERE tenant_id = $1 AND state = 'active'`,
		tenantID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: count apns by tenant: %w", err)
	}
	return count, nil
}

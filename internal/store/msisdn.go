package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type MSISDN struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	OperatorID    uuid.UUID
	MSISDN        string
	State         string
	SimID         *uuid.UUID
	ReservedUntil *time.Time
	CreatedAt     time.Time
}

type MSISDNImportRow struct {
	MSISDN       string
	OperatorCode string
}

type MSISDNImportResult struct {
	Total    int
	Imported int
	Skipped  int
	Errors   []MSISDNImportError
}

type MSISDNImportError struct {
	Row     int    `json:"row"`
	MSISDN  string `json:"msisdn"`
	Message string `json:"message"`
}

var (
	ErrMSISDNExists       = errors.New("msisdn already exists")
	ErrMSISDNNotAvailable = errors.New("msisdn not available")
)

type MSISDNStore struct {
	db *pgxpool.Pool
}

func NewMSISDNStore(db *pgxpool.Pool) *MSISDNStore {
	return &MSISDNStore{db: db}
}

func (s *MSISDNStore) List(ctx context.Context, cursor string, limit int, state string, operatorID *uuid.UUID) ([]MSISDN, string, error) {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return nil, "", err
	}

	if limit <= 0 || limit > 100 {
		limit = 20
	}

	args := []interface{}{tenantID}
	conditions := []string{"tenant_id = $1"}
	argIdx := 2

	if state != "" {
		conditions = append(conditions, fmt.Sprintf("state = $%d", argIdx))
		args = append(args, state)
		argIdx++
	}

	if operatorID != nil {
		conditions = append(conditions, fmt.Sprintf("operator_id = $%d", argIdx))
		args = append(args, *operatorID)
		argIdx++
	}

	if cursor != "" {
		cursorID, parseErr := uuid.Parse(cursor)
		if parseErr == nil {
			conditions = append(conditions, fmt.Sprintf("id > $%d", argIdx))
			args = append(args, cursorID)
			argIdx++
		}
	}

	where := "WHERE " + strings.Join(conditions, " AND ")

	args = append(args, limit+1)
	limitPlaceholder := fmt.Sprintf("$%d", argIdx)

	query := fmt.Sprintf(`
		SELECT id, tenant_id, operator_id, msisdn, state, sim_id, reserved_until, created_at
		FROM msisdn_pool
		%s
		ORDER BY id
		LIMIT %s
	`, where, limitPlaceholder)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list msisdn pool: %w", err)
	}
	defer rows.Close()

	var results []MSISDN
	for rows.Next() {
		var m MSISDN
		if err := rows.Scan(
			&m.ID, &m.TenantID, &m.OperatorID, &m.MSISDN,
			&m.State, &m.SimID, &m.ReservedUntil, &m.CreatedAt,
		); err != nil {
			return nil, "", fmt.Errorf("scan msisdn: %w", err)
		}
		results = append(results, m)
	}

	nextCursor := ""
	if len(results) > limit {
		nextCursor = results[limit-1].ID.String()
		results = results[:limit]
	}

	return results, nextCursor, nil
}

func (s *MSISDNStore) GetByMSISDN(ctx context.Context, msisdn string) (*MSISDN, error) {
	var m MSISDN
	err := s.db.QueryRow(ctx, `
		SELECT id, tenant_id, operator_id, msisdn, state, sim_id, reserved_until, created_at
		FROM msisdn_pool
		WHERE msisdn = $1
	`, msisdn).Scan(
		&m.ID, &m.TenantID, &m.OperatorID, &m.MSISDN,
		&m.State, &m.SimID, &m.ReservedUntil, &m.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get msisdn: %w", err)
	}
	return &m, nil
}

func (s *MSISDNStore) GetByID(ctx context.Context, id uuid.UUID) (*MSISDN, error) {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	var m MSISDN
	err = s.db.QueryRow(ctx, `
		SELECT id, tenant_id, operator_id, msisdn, state, sim_id, reserved_until, created_at
		FROM msisdn_pool
		WHERE id = $1 AND tenant_id = $2
	`, id, tenantID).Scan(
		&m.ID, &m.TenantID, &m.OperatorID, &m.MSISDN,
		&m.State, &m.SimID, &m.ReservedUntil, &m.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get msisdn by id: %w", err)
	}
	return &m, nil
}

func (s *MSISDNStore) BulkImport(ctx context.Context, operatorID uuid.UUID, rows []MSISDNImportRow) (*MSISDNImportResult, error) {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	result := &MSISDNImportResult{
		Total: len(rows),
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	for i, row := range rows {
		_, execErr := tx.Exec(ctx, `
			INSERT INTO msisdn_pool (tenant_id, operator_id, msisdn, state)
			VALUES ($1, $2, $3, 'available')
		`, tenantID, operatorID, row.MSISDN)
		if execErr != nil {
			if isDuplicateKeyError(execErr) {
				result.Skipped++
				result.Errors = append(result.Errors, MSISDNImportError{
					Row:     i + 1,
					MSISDN:  row.MSISDN,
					Message: "MSISDN already exists",
				})
				continue
			}
			result.Errors = append(result.Errors, MSISDNImportError{
				Row:     i + 1,
				MSISDN:  row.MSISDN,
				Message: execErr.Error(),
			})
			continue
		}
		result.Imported++
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return result, nil
}

func (s *MSISDNStore) Assign(ctx context.Context, id uuid.UUID, simID uuid.UUID) (*MSISDN, error) {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var m MSISDN
	err = tx.QueryRow(ctx, `
		SELECT id, tenant_id, operator_id, msisdn, state, sim_id, reserved_until, created_at
		FROM msisdn_pool
		WHERE id = $1 AND tenant_id = $2
		FOR UPDATE
	`, id, tenantID).Scan(
		&m.ID, &m.TenantID, &m.OperatorID, &m.MSISDN,
		&m.State, &m.SimID, &m.ReservedUntil, &m.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("lock msisdn: %w", err)
	}

	if m.State != "available" {
		return nil, ErrMSISDNNotAvailable
	}

	err = tx.QueryRow(ctx, `
		UPDATE msisdn_pool SET state = 'assigned', sim_id = $2
		WHERE id = $1
		RETURNING id, tenant_id, operator_id, msisdn, state, sim_id, reserved_until, created_at
	`, id, simID).Scan(
		&m.ID, &m.TenantID, &m.OperatorID, &m.MSISDN,
		&m.State, &m.SimID, &m.ReservedUntil, &m.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("assign msisdn: %w", err)
	}

	_, err = tx.Exec(ctx, `
		UPDATE sims SET msisdn = $2, updated_at = NOW()
		WHERE id = $1 AND tenant_id = $3
	`, simID, m.MSISDN, tenantID)
	if err != nil {
		return nil, fmt.Errorf("update sim msisdn: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &m, nil
}

func (s *MSISDNStore) Release(ctx context.Context, simID uuid.UUID) error {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return err
	}

	tag, err := s.db.Exec(ctx, `
		UPDATE msisdn_pool SET state = 'available', sim_id = NULL
		WHERE sim_id = $1 AND tenant_id = $2 AND state = 'assigned'
	`, simID, tenantID)
	if err != nil {
		return fmt.Errorf("release msisdn: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

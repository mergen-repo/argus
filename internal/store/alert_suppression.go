package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrAlertSuppressionNotFound = errors.New("store: alert suppression not found")
	ErrDuplicateRuleName        = errors.New("store: duplicate suppression rule name")
)

type AlertSuppression struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	ScopeType  string
	ScopeValue string
	ExpiresAt  time.Time
	Reason     *string
	RuleName   *string
	CreatedBy  *uuid.UUID
	CreatedAt  time.Time
}

type AlertSuppressionStore struct {
	db *pgxpool.Pool
}

func NewAlertSuppressionStore(db *pgxpool.Pool) *AlertSuppressionStore {
	return &AlertSuppressionStore{db: db}
}

type CreateAlertSuppressionParams struct {
	TenantID   uuid.UUID
	ScopeType  string
	ScopeValue string
	ExpiresAt  time.Time
	Reason     *string
	RuleName   *string
	CreatedBy  *uuid.UUID
}

type ListAlertSuppressionsParams struct {
	ActiveOnly bool
	Cursor     *uuid.UUID
	Limit      int
}

type AlertMatchProbe struct {
	AlertID    uuid.UUID
	Type       string
	OperatorID *uuid.UUID
	DedupKey   *string
}

const alertSuppressionColumns = `id, tenant_id, scope_type, scope_value, expires_at, reason, rule_name, created_by, created_at`

func scanAlertSuppression(row pgx.Row) (*AlertSuppression, error) {
	var s AlertSuppression
	err := row.Scan(
		&s.ID, &s.TenantID, &s.ScopeType, &s.ScopeValue,
		&s.ExpiresAt, &s.Reason, &s.RuleName, &s.CreatedBy, &s.CreatedAt,
	)
	return &s, err
}

func (s *AlertSuppressionStore) Create(ctx context.Context, p CreateAlertSuppressionParams) (*AlertSuppression, error) {
	row := s.db.QueryRow(ctx, `
		INSERT INTO alert_suppressions (tenant_id, scope_type, scope_value, expires_at, reason, rule_name, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+alertSuppressionColumns,
		p.TenantID, p.ScopeType, p.ScopeValue, p.ExpiresAt, p.Reason, p.RuleName, p.CreatedBy,
	)
	as_, err := scanAlertSuppression(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "uq_alert_suppressions_tenant_rule_name" {
			return nil, ErrDuplicateRuleName
		}
		return nil, fmt.Errorf("store: create alert suppression: %w", err)
	}
	return as_, nil
}

func (s *AlertSuppressionStore) List(ctx context.Context, tenantID uuid.UUID, p ListAlertSuppressionsParams) ([]AlertSuppression, *uuid.UUID, error) {
	limit := p.Limit
	if limit <= 0 {
		limit = 50
	} else if limit > 100 {
		limit = 100
	}

	query := `SELECT ` + alertSuppressionColumns + ` FROM alert_suppressions WHERE tenant_id = $1`
	args := []interface{}{tenantID}
	argIdx := 2

	if p.ActiveOnly {
		query += fmt.Sprintf(" AND expires_at > NOW()")
	}

	if p.Cursor != nil {
		query += fmt.Sprintf(
			" AND (created_at, id) < ((SELECT created_at FROM alert_suppressions WHERE id = $%d), $%d)",
			argIdx, argIdx,
		)
		args = append(args, *p.Cursor)
		argIdx++
	}

	query += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", argIdx)
	args = append(args, limit+1)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("store: list alert suppressions: %w", err)
	}
	defer rows.Close()

	var results []AlertSuppression
	for rows.Next() {
		var as_ AlertSuppression
		if err := rows.Scan(
			&as_.ID, &as_.TenantID, &as_.ScopeType, &as_.ScopeValue,
			&as_.ExpiresAt, &as_.Reason, &as_.RuleName, &as_.CreatedBy, &as_.CreatedAt,
		); err != nil {
			return nil, nil, fmt.Errorf("store: scan alert suppression: %w", err)
		}
		results = append(results, as_)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("store: list alert suppressions: iterate: %w", err)
	}

	var nextCursor *uuid.UUID
	if len(results) > limit {
		id := results[limit-1].ID
		nextCursor = &id
		results = results[:limit]
	}

	return results, nextCursor, nil
}

func (s *AlertSuppressionStore) Delete(ctx context.Context, tenantID, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx,
		`DELETE FROM alert_suppressions WHERE tenant_id = $1 AND id = $2`,
		tenantID, id,
	)
	if err != nil {
		return fmt.Errorf("store: delete alert suppression: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAlertSuppressionNotFound
	}
	return nil
}

func (s *AlertSuppressionStore) MatchActive(ctx context.Context, tenantID uuid.UUID, probe AlertMatchProbe) (*AlertSuppression, error) {
	operatorIDStr := ""
	if probe.OperatorID != nil {
		operatorIDStr = probe.OperatorID.String()
	}
	dedupKeyStr := ""
	if probe.DedupKey != nil {
		dedupKeyStr = *probe.DedupKey
	}

	row := s.db.QueryRow(ctx, `
		SELECT `+alertSuppressionColumns+`
		FROM alert_suppressions
		WHERE tenant_id = $1
		  AND expires_at > NOW()
		  AND (
		    (scope_type = 'this'      AND scope_value = $2)
		    OR (scope_type = 'type'   AND scope_value = $3)
		    OR (scope_type = 'operator' AND $4 <> '' AND scope_value = $4)
		    OR (scope_type = 'dedup_key' AND $5 <> '' AND scope_value = $5)
		  )
		ORDER BY created_at DESC
		LIMIT 1`,
		tenantID, probe.AlertID.String(), probe.Type, operatorIDStr, dedupKeyStr,
	)

	as_, err := scanAlertSuppression(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: match active alert suppression: %w", err)
	}
	return as_, nil
}

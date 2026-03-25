package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

type PolicyViolation struct {
	ID            uuid.UUID       `json:"id"`
	TenantID      uuid.UUID       `json:"tenant_id"`
	SimID         uuid.UUID       `json:"sim_id"`
	PolicyID      uuid.UUID       `json:"policy_id"`
	VersionID     uuid.UUID       `json:"version_id"`
	RuleIndex     int             `json:"rule_index"`
	ViolationType string          `json:"violation_type"`
	ActionTaken   string          `json:"action_taken"`
	Details       json.RawMessage `json:"details"`
	SessionID     *uuid.UUID      `json:"session_id,omitempty"`
	OperatorID    *uuid.UUID      `json:"operator_id,omitempty"`
	APNID         *uuid.UUID      `json:"apn_id,omitempty"`
	Severity      string          `json:"severity"`
	CreatedAt     time.Time       `json:"created_at"`
}

type CreateViolationParams struct {
	TenantID      uuid.UUID
	SimID         uuid.UUID
	PolicyID      uuid.UUID
	VersionID     uuid.UUID
	RuleIndex     int
	ViolationType string
	ActionTaken   string
	Details       map[string]interface{}
	SessionID     *uuid.UUID
	OperatorID    *uuid.UUID
	APNID         *uuid.UUID
	Severity      string
}

type ListViolationsParams struct {
	Cursor        string
	Limit         int
	ViolationType string
	Severity      string
	SimID         *uuid.UUID
	PolicyID      *uuid.UUID
}

type PolicyViolationStore struct {
	db     *pgxpool.Pool
	logger zerolog.Logger
}

func NewPolicyViolationStore(db *pgxpool.Pool, logger zerolog.Logger) *PolicyViolationStore {
	return &PolicyViolationStore{
		db:     db,
		logger: logger.With().Str("store", "policy_violation").Logger(),
	}
}

func (s *PolicyViolationStore) Create(ctx context.Context, p CreateViolationParams) (*PolicyViolation, error) {
	details, _ := json.Marshal(p.Details)
	if details == nil {
		details = []byte("{}")
	}
	if p.Severity == "" {
		p.Severity = "info"
	}

	var v PolicyViolation
	err := s.db.QueryRow(ctx,
		`INSERT INTO policy_violations (tenant_id, sim_id, policy_id, version_id, rule_index, violation_type, action_taken, details, session_id, operator_id, apn_id, severity)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		 RETURNING id, tenant_id, sim_id, policy_id, version_id, rule_index, violation_type, action_taken, details, session_id, operator_id, apn_id, severity, created_at`,
		p.TenantID, p.SimID, p.PolicyID, p.VersionID, p.RuleIndex,
		p.ViolationType, p.ActionTaken, details,
		p.SessionID, p.OperatorID, p.APNID, p.Severity,
	).Scan(
		&v.ID, &v.TenantID, &v.SimID, &v.PolicyID, &v.VersionID,
		&v.RuleIndex, &v.ViolationType, &v.ActionTaken, &v.Details,
		&v.SessionID, &v.OperatorID, &v.APNID, &v.Severity, &v.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("store: create policy violation: %w", err)
	}
	return &v, nil
}

func (s *PolicyViolationStore) List(ctx context.Context, tenantID uuid.UUID, params ListViolationsParams) ([]PolicyViolation, string, error) {
	if params.Limit <= 0 || params.Limit > 100 {
		params.Limit = 50
	}

	args := []interface{}{tenantID}
	conditions := []string{"tenant_id = $1"}
	argIdx := 2

	if params.Cursor != "" {
		if ts, err := time.Parse(time.RFC3339Nano, params.Cursor); err == nil {
			conditions = append(conditions, fmt.Sprintf("created_at < $%d", argIdx))
			args = append(args, ts)
			argIdx++
		}
	}
	if params.ViolationType != "" {
		conditions = append(conditions, fmt.Sprintf("violation_type = $%d", argIdx))
		args = append(args, params.ViolationType)
		argIdx++
	}
	if params.Severity != "" {
		conditions = append(conditions, fmt.Sprintf("severity = $%d", argIdx))
		args = append(args, params.Severity)
		argIdx++
	}
	if params.SimID != nil {
		conditions = append(conditions, fmt.Sprintf("sim_id = $%d", argIdx))
		args = append(args, *params.SimID)
		argIdx++
	}
	if params.PolicyID != nil {
		conditions = append(conditions, fmt.Sprintf("policy_id = $%d", argIdx))
		args = append(args, *params.PolicyID)
		argIdx++
	}

	limitPlaceholder := fmt.Sprintf("$%d", argIdx)
	args = append(args, params.Limit+1)

	query := fmt.Sprintf(
		`SELECT id, tenant_id, sim_id, policy_id, version_id, rule_index, violation_type, action_taken, details, session_id, operator_id, apn_id, severity, created_at
		 FROM policy_violations WHERE %s ORDER BY created_at DESC LIMIT %s`,
		strings.Join(conditions, " AND "), limitPlaceholder,
	)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list policy violations: %w", err)
	}
	defer rows.Close()

	var violations []PolicyViolation
	for rows.Next() {
		var v PolicyViolation
		if err := rows.Scan(
			&v.ID, &v.TenantID, &v.SimID, &v.PolicyID, &v.VersionID,
			&v.RuleIndex, &v.ViolationType, &v.ActionTaken, &v.Details,
			&v.SessionID, &v.OperatorID, &v.APNID, &v.Severity, &v.CreatedAt,
		); err != nil {
			return nil, "", fmt.Errorf("store: scan policy violation: %w", err)
		}
		violations = append(violations, v)
	}

	nextCursor := ""
	if len(violations) > params.Limit {
		nextCursor = violations[params.Limit-1].CreatedAt.Format(time.RFC3339Nano)
		violations = violations[:params.Limit]
	}

	return violations, nextCursor, nil
}

func (s *PolicyViolationStore) CountByType(ctx context.Context, tenantID uuid.UUID) (map[string]int64, error) {
	rows, err := s.db.Query(ctx,
		`SELECT violation_type, COUNT(*) FROM policy_violations
		 WHERE tenant_id = $1 AND created_at > NOW() - INTERVAL '24 hours'
		 GROUP BY violation_type`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("store: count violations by type: %w", err)
	}
	defer rows.Close()

	result := make(map[string]int64)
	for rows.Next() {
		var vType string
		var count int64
		if err := rows.Scan(&vType, &count); err != nil {
			continue
		}
		result[vType] = count
	}
	return result, nil
}

func (s *PolicyViolationStore) GetByID(ctx context.Context, id uuid.UUID) (*PolicyViolation, error) {
	var v PolicyViolation
	err := s.db.QueryRow(ctx,
		`SELECT id, tenant_id, sim_id, policy_id, version_id, rule_index, violation_type, action_taken, details, session_id, operator_id, apn_id, severity, created_at
		 FROM policy_violations WHERE id = $1`,
		id,
	).Scan(
		&v.ID, &v.TenantID, &v.SimID, &v.PolicyID, &v.VersionID,
		&v.RuleIndex, &v.ViolationType, &v.ActionTaken, &v.Details,
		&v.SessionID, &v.OperatorID, &v.APNID, &v.Severity, &v.CreatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("violation not found")
		}
		return nil, fmt.Errorf("store: get policy violation: %w", err)
	}
	return &v, nil
}

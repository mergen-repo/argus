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
	"github.com/rs/zerolog"
)

var (
	ErrAlreadyAcknowledged = errors.New("store: violation already acknowledged")
	ErrViolationNotFound   = errors.New("store: violation not found")
)

type PolicyViolation struct {
	ID                 uuid.UUID       `json:"id"`
	TenantID           uuid.UUID       `json:"tenant_id"`
	SimID              uuid.UUID       `json:"sim_id"`
	PolicyID           uuid.UUID       `json:"policy_id"`
	VersionID          uuid.UUID       `json:"version_id"`
	RuleIndex          int             `json:"rule_index"`
	ViolationType      string          `json:"violation_type"`
	ActionTaken        string          `json:"action_taken"`
	Details            json.RawMessage `json:"details"`
	SessionID          *uuid.UUID      `json:"session_id,omitempty"`
	OperatorID         *uuid.UUID      `json:"operator_id,omitempty"`
	APNID              *uuid.UUID      `json:"apn_id,omitempty"`
	Severity           string          `json:"severity"`
	CreatedAt          time.Time       `json:"created_at"`
	AcknowledgedAt     *time.Time      `json:"acknowledged_at,omitempty"`
	AcknowledgedBy     *uuid.UUID      `json:"acknowledged_by,omitempty"`
	AcknowledgmentNote *string         `json:"acknowledgment_note,omitempty"`
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
	Acknowledged  *bool
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
	if params.Acknowledged != nil {
		if *params.Acknowledged {
			conditions = append(conditions, "acknowledged_at IS NOT NULL")
		} else {
			conditions = append(conditions, "acknowledged_at IS NULL")
		}
	}

	limitPlaceholder := fmt.Sprintf("$%d", argIdx)
	args = append(args, params.Limit+1)

	query := fmt.Sprintf(
		`SELECT id, tenant_id, sim_id, policy_id, version_id, rule_index, violation_type, action_taken, details, session_id, operator_id, apn_id, severity, created_at, acknowledged_at, acknowledged_by, acknowledgment_note
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
			&v.AcknowledgedAt, &v.AcknowledgedBy, &v.AcknowledgmentNote,
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
		`SELECT id, tenant_id, sim_id, policy_id, version_id, rule_index, violation_type, action_taken, details, session_id, operator_id, apn_id, severity, created_at, acknowledged_at, acknowledged_by, acknowledgment_note
		 FROM policy_violations WHERE id = $1`,
		id,
	).Scan(
		&v.ID, &v.TenantID, &v.SimID, &v.PolicyID, &v.VersionID,
		&v.RuleIndex, &v.ViolationType, &v.ActionTaken, &v.Details,
		&v.SessionID, &v.OperatorID, &v.APNID, &v.Severity, &v.CreatedAt,
		&v.AcknowledgedAt, &v.AcknowledgedBy, &v.AcknowledgmentNote,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrViolationNotFound
		}
		return nil, fmt.Errorf("store: get policy violation: %w", err)
	}
	return &v, nil
}

// PolicyViolationWithNames is an enriched PolicyViolation with joined parent-entity display fields.
// Used by list/detail endpoints that need UI-ready labels without per-row lookups.
type PolicyViolationWithNames struct {
	PolicyViolation
	ICCID               *string
	IMSI                *string
	MSISDN              *string
	OperatorName        *string
	OperatorCode        *string
	APNName             *string
	PolicyName          *string
	PolicyVersionNumber *int
}

// violationEnrichedJoin is the JOIN clause shared by all enriched violation queries.
// LEFT JOINs required for orphan safety. operators is tenant-agnostic.
// Violation rows are immutable events; no tenant-scoping on JOIN needed.
const violationEnrichedJoin = `
LEFT JOIN sims s ON v.sim_id = s.id
LEFT JOIN operators o ON v.operator_id = o.id
LEFT JOIN apns a ON v.apn_id = a.id
LEFT JOIN policies pol ON v.policy_id = pol.id
LEFT JOIN policy_versions pv ON v.version_id = pv.id`

// violationEnrichedSelect is the SELECT list for enriched violation queries.
const violationEnrichedSelect = `v.id, v.tenant_id, v.sim_id, v.policy_id, v.version_id,
	v.rule_index, v.violation_type, v.action_taken, v.details,
	v.session_id, v.operator_id, v.apn_id, v.severity, v.created_at,
	v.acknowledged_at, v.acknowledged_by, v.acknowledgment_note,
	s.iccid, s.imsi, s.msisdn,
	o.name AS operator_name, o.code AS operator_code,
	COALESCE(NULLIF(a.display_name, ''), a.name) AS apn_name,
	pol.name AS policy_name,
	pv.version AS policy_version_number`

// scanPolicyViolationWithNames is the ONLY scan helper for PolicyViolationWithNames.
// If you add fields to PolicyViolationWithNames, update this function AND all tests.
// Never inline-scan PolicyViolationWithNames rows elsewhere.
func scanPolicyViolationWithNames(row pgx.Row) (*PolicyViolationWithNames, error) {
	var v PolicyViolationWithNames
	err := row.Scan(
		&v.ID, &v.TenantID, &v.SimID, &v.PolicyID, &v.VersionID,
		&v.RuleIndex, &v.ViolationType, &v.ActionTaken, &v.Details,
		&v.SessionID, &v.OperatorID, &v.APNID, &v.Severity, &v.CreatedAt,
		&v.AcknowledgedAt, &v.AcknowledgedBy, &v.AcknowledgmentNote,
		&v.ICCID, &v.IMSI, &v.MSISDN,
		&v.OperatorName, &v.OperatorCode,
		&v.APNName, &v.PolicyName, &v.PolicyVersionNumber,
	)
	return &v, err
}

// ListEnriched returns PolicyViolations with joined parent-entity display names.
// Signature mirrors List; enriched fields are nullable (LEFT JOIN).
func (s *PolicyViolationStore) ListEnriched(ctx context.Context, tenantID uuid.UUID, params ListViolationsParams) ([]PolicyViolationWithNames, string, error) {
	if params.Limit <= 0 || params.Limit > 100 {
		params.Limit = 50
	}

	args := []interface{}{tenantID}
	conditions := []string{"v.tenant_id = $1"}
	argIdx := 2

	if params.Cursor != "" {
		if ts, err := time.Parse(time.RFC3339Nano, params.Cursor); err == nil {
			conditions = append(conditions, fmt.Sprintf("v.created_at < $%d", argIdx))
			args = append(args, ts)
			argIdx++
		}
	}
	if params.ViolationType != "" {
		conditions = append(conditions, fmt.Sprintf("v.violation_type = $%d", argIdx))
		args = append(args, params.ViolationType)
		argIdx++
	}
	if params.Severity != "" {
		conditions = append(conditions, fmt.Sprintf("v.severity = $%d", argIdx))
		args = append(args, params.Severity)
		argIdx++
	}
	if params.SimID != nil {
		conditions = append(conditions, fmt.Sprintf("v.sim_id = $%d", argIdx))
		args = append(args, *params.SimID)
		argIdx++
	}
	if params.PolicyID != nil {
		conditions = append(conditions, fmt.Sprintf("v.policy_id = $%d", argIdx))
		args = append(args, *params.PolicyID)
		argIdx++
	}
	if params.Acknowledged != nil {
		if *params.Acknowledged {
			conditions = append(conditions, "v.acknowledged_at IS NOT NULL")
		} else {
			conditions = append(conditions, "v.acknowledged_at IS NULL")
		}
	}

	limitPlaceholder := fmt.Sprintf("$%d", argIdx)
	args = append(args, params.Limit+1)

	query := fmt.Sprintf(
		`SELECT %s FROM policy_violations v %s WHERE %s ORDER BY v.created_at DESC LIMIT %s`,
		violationEnrichedSelect, violationEnrichedJoin,
		strings.Join(conditions, " AND "), limitPlaceholder,
	)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list enriched policy violations: %w", err)
	}
	defer rows.Close()

	var violations []PolicyViolationWithNames
	for rows.Next() {
		v, err := scanPolicyViolationWithNames(rows)
		if err != nil {
			return nil, "", fmt.Errorf("store: scan enriched policy violation: %w", err)
		}
		violations = append(violations, *v)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("store: iter enriched policy violations: %w", err)
	}

	nextCursor := ""
	if len(violations) > params.Limit {
		nextCursor = violations[params.Limit-1].CreatedAt.Format(time.RFC3339Nano)
		violations = violations[:params.Limit]
	}

	return violations, nextCursor, nil
}

// GetByIDEnriched returns one enriched PolicyViolation by ID, scoped to tenantID.
// Returns ErrViolationNotFound when no row matches (including cross-tenant mismatches).
func (s *PolicyViolationStore) GetByIDEnriched(ctx context.Context, id, tenantID uuid.UUID) (*PolicyViolationWithNames, error) {
	query := fmt.Sprintf(
		`SELECT %s FROM policy_violations v %s WHERE v.id = $1 AND v.tenant_id = $2`,
		violationEnrichedSelect, violationEnrichedJoin,
	)

	row := s.db.QueryRow(ctx, query, id, tenantID)
	v, err := scanPolicyViolationWithNames(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrViolationNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get enriched policy violation: %w", err)
	}
	return v, nil
}

func (s *PolicyViolationStore) Acknowledge(ctx context.Context, id, tenantID, userID uuid.UUID, note string) (*PolicyViolation, error) {
	var notePtr *string
	if note != "" {
		notePtr = &note
	}

	var v PolicyViolation
	err := s.db.QueryRow(ctx,
		`UPDATE policy_violations
		 SET acknowledged_at = NOW(), acknowledged_by = $3, acknowledgment_note = $4
		 WHERE id = $1 AND tenant_id = $2 AND acknowledged_at IS NULL
		 RETURNING id, tenant_id, sim_id, policy_id, version_id, rule_index, violation_type, action_taken, details, session_id, operator_id, apn_id, severity, created_at, acknowledged_at, acknowledged_by, acknowledgment_note`,
		id, tenantID, userID, notePtr,
	).Scan(
		&v.ID, &v.TenantID, &v.SimID, &v.PolicyID, &v.VersionID,
		&v.RuleIndex, &v.ViolationType, &v.ActionTaken, &v.Details,
		&v.SessionID, &v.OperatorID, &v.APNID, &v.Severity, &v.CreatedAt,
		&v.AcknowledgedAt, &v.AcknowledgedBy, &v.AcknowledgmentNote,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		checkErr := s.db.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM policy_violations WHERE id = $1 AND tenant_id = $2)`,
			id, tenantID,
		).Scan(&exists)
		if checkErr != nil || !exists {
			return nil, ErrViolationNotFound
		}
		return nil, ErrAlreadyAcknowledged
	}
	if err != nil {
		return nil, fmt.Errorf("store: acknowledge violation: %w", err)
	}
	return &v, nil
}

// CountInWindowAllTenants returns the global count of policy_violations rows
// created within the [from, to) timestamp window across every tenant. Added
// for FIX-237 fleet digest worker (violation_surge current count and rolling
// baseline). Read-only aggregate; no tenant scoping by design.
func (s *PolicyViolationStore) CountInWindowAllTenants(ctx context.Context, from, to time.Time) (int64, error) {
	var count int64
	err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM policy_violations WHERE created_at >= $1 AND created_at < $2`,
		from, to,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: count policy violations in window all tenants: %w", err)
	}
	return count, nil
}

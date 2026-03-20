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
	ErrSIMNotFound            = errors.New("store: sim not found")
	ErrICCIDExists            = errors.New("store: iccid already exists")
	ErrIMSIExists             = errors.New("store: imsi already exists")
	ErrInvalidStateTransition = errors.New("store: invalid state transition")
)

type SIM struct {
	ID                     uuid.UUID       `json:"id"`
	TenantID               uuid.UUID       `json:"tenant_id"`
	OperatorID             uuid.UUID       `json:"operator_id"`
	APNID                  *uuid.UUID      `json:"apn_id"`
	ICCID                  string          `json:"iccid"`
	IMSI                   string          `json:"imsi"`
	MSISDN                 *string         `json:"msisdn"`
	IPAddressID            *uuid.UUID      `json:"ip_address_id"`
	PolicyVersionID        *uuid.UUID      `json:"policy_version_id"`
	ESimProfileID          *uuid.UUID      `json:"esim_profile_id"`
	SimType                string          `json:"sim_type"`
	State                  string          `json:"state"`
	RATType                *string         `json:"rat_type"`
	MaxConcurrentSessions  int             `json:"max_concurrent_sessions"`
	SessionIdleTimeoutSec  int             `json:"session_idle_timeout_sec"`
	SessionHardTimeoutSec  int             `json:"session_hard_timeout_sec"`
	Metadata               json.RawMessage `json:"metadata"`
	ActivatedAt            *time.Time      `json:"activated_at"`
	SuspendedAt            *time.Time      `json:"suspended_at"`
	TerminatedAt           *time.Time      `json:"terminated_at"`
	PurgeAt                *time.Time      `json:"purge_at"`
	CreatedAt              time.Time       `json:"created_at"`
	UpdatedAt              time.Time       `json:"updated_at"`
}

type SimStateHistory struct {
	ID          int64      `json:"id"`
	SimID       uuid.UUID  `json:"sim_id"`
	FromState   *string    `json:"from_state"`
	ToState     string     `json:"to_state"`
	Reason      *string    `json:"reason"`
	TriggeredBy string     `json:"triggered_by"`
	UserID      *uuid.UUID `json:"user_id"`
	JobID       *uuid.UUID `json:"job_id"`
	CreatedAt   time.Time  `json:"created_at"`
}

type CreateSIMParams struct {
	ICCID      string
	IMSI       string
	MSISDN     *string
	OperatorID uuid.UUID
	APNID      uuid.UUID
	SimType    string
	RATType    *string
	Metadata   json.RawMessage
}

type ListSIMsParams struct {
	Cursor     string
	Limit      int
	ICCID      string
	IMSI       string
	MSISDN     string
	OperatorID *uuid.UUID
	APNID      *uuid.UUID
	State      string
	RATType    string
	Q          string
}

var validTransitions = map[string][]string{
	"ordered":     {"active"},
	"active":      {"suspended", "stolen_lost", "terminated"},
	"suspended":   {"active", "terminated"},
	"stolen_lost": {},
	"terminated":  {"purged"},
	"purged":      {},
}

func validateTransition(currentState, targetState string) error {
	allowed, ok := validTransitions[currentState]
	if !ok {
		return ErrInvalidStateTransition
	}
	for _, s := range allowed {
		if s == targetState {
			return nil
		}
	}
	return ErrInvalidStateTransition
}

type SIMStore struct {
	db *pgxpool.Pool
}

func NewSIMStore(db *pgxpool.Pool) *SIMStore {
	return &SIMStore{db: db}
}

var simColumns = `id, tenant_id, operator_id, apn_id, iccid, imsi, msisdn,
	ip_address_id, policy_version_id, esim_profile_id, sim_type, state, rat_type,
	max_concurrent_sessions, session_idle_timeout_sec, session_hard_timeout_sec,
	metadata, activated_at, suspended_at, terminated_at, purge_at,
	created_at, updated_at`

func scanSIM(row pgx.Row) (*SIM, error) {
	var s SIM
	err := row.Scan(
		&s.ID, &s.TenantID, &s.OperatorID, &s.APNID, &s.ICCID, &s.IMSI, &s.MSISDN,
		&s.IPAddressID, &s.PolicyVersionID, &s.ESimProfileID, &s.SimType, &s.State, &s.RATType,
		&s.MaxConcurrentSessions, &s.SessionIdleTimeoutSec, &s.SessionHardTimeoutSec,
		&s.Metadata, &s.ActivatedAt, &s.SuspendedAt, &s.TerminatedAt, &s.PurgeAt,
		&s.CreatedAt, &s.UpdatedAt,
	)
	return &s, err
}

func (s *SIMStore) Create(ctx context.Context, tenantID uuid.UUID, p CreateSIMParams) (*SIM, error) {
	metadata := json.RawMessage(`{}`)
	if p.Metadata != nil && len(p.Metadata) > 0 {
		metadata = p.Metadata
	}

	row := s.db.QueryRow(ctx, `
		INSERT INTO sims (tenant_id, operator_id, apn_id, iccid, imsi, msisdn,
			sim_type, rat_type, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING `+simColumns,
		tenantID, p.OperatorID, p.APNID, p.ICCID, p.IMSI, p.MSISDN,
		p.SimType, p.RATType, metadata,
	)

	sim, err := scanSIM(row)
	if err != nil {
		if isDuplicateKeyError(err) {
			if strings.Contains(err.Error(), "idx_sims_iccid") {
				return nil, ErrICCIDExists
			}
			if strings.Contains(err.Error(), "idx_sims_imsi") {
				return nil, ErrIMSIExists
			}
			return nil, ErrICCIDExists
		}
		return nil, fmt.Errorf("store: create sim: %w", err)
	}
	return sim, nil
}

func (s *SIMStore) GetByID(ctx context.Context, tenantID, id uuid.UUID) (*SIM, error) {
	row := s.db.QueryRow(ctx,
		`SELECT `+simColumns+` FROM sims WHERE id = $1 AND tenant_id = $2`,
		id, tenantID,
	)
	sim, err := scanSIM(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSIMNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get sim: %w", err)
	}
	return sim, nil
}

func (s *SIMStore) List(ctx context.Context, tenantID uuid.UUID, p ListSIMsParams) ([]SIM, string, error) {
	limit := p.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{tenantID}
	conditions := []string{"tenant_id = $1"}
	argIdx := 2

	if p.ICCID != "" {
		conditions = append(conditions, fmt.Sprintf("iccid = $%d", argIdx))
		args = append(args, p.ICCID)
		argIdx++
	}

	if p.IMSI != "" {
		conditions = append(conditions, fmt.Sprintf("imsi = $%d", argIdx))
		args = append(args, p.IMSI)
		argIdx++
	}

	if p.MSISDN != "" {
		conditions = append(conditions, fmt.Sprintf("msisdn = $%d", argIdx))
		args = append(args, p.MSISDN)
		argIdx++
	}

	if p.OperatorID != nil {
		conditions = append(conditions, fmt.Sprintf("operator_id = $%d", argIdx))
		args = append(args, *p.OperatorID)
		argIdx++
	}

	if p.APNID != nil {
		conditions = append(conditions, fmt.Sprintf("apn_id = $%d", argIdx))
		args = append(args, *p.APNID)
		argIdx++
	}

	if p.State != "" {
		conditions = append(conditions, fmt.Sprintf("state = $%d", argIdx))
		args = append(args, p.State)
		argIdx++
	}

	if p.RATType != "" {
		conditions = append(conditions, fmt.Sprintf("rat_type = $%d", argIdx))
		args = append(args, p.RATType)
		argIdx++
	}

	if p.Q != "" {
		searchTerm := "%" + p.Q + "%"
		conditions = append(conditions, fmt.Sprintf(
			"(iccid ILIKE $%d OR imsi ILIKE $%d OR msisdn ILIKE $%d)",
			argIdx, argIdx, argIdx,
		))
		args = append(args, searchTerm)
		argIdx++
	}

	if p.Cursor != "" {
		cursorID, parseErr := uuid.Parse(p.Cursor)
		if parseErr == nil {
			conditions = append(conditions, fmt.Sprintf("id < $%d", argIdx))
			args = append(args, cursorID)
			argIdx++
		}
	}

	where := "WHERE " + strings.Join(conditions, " AND ")

	args = append(args, limit+1)
	limitPlaceholder := fmt.Sprintf("$%d", argIdx)

	query := fmt.Sprintf(`SELECT %s FROM sims %s ORDER BY created_at DESC, id DESC LIMIT %s`,
		simColumns, where, limitPlaceholder)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list sims: %w", err)
	}
	defer rows.Close()

	var results []SIM
	for rows.Next() {
		var sim SIM
		if err := rows.Scan(
			&sim.ID, &sim.TenantID, &sim.OperatorID, &sim.APNID, &sim.ICCID, &sim.IMSI, &sim.MSISDN,
			&sim.IPAddressID, &sim.PolicyVersionID, &sim.ESimProfileID, &sim.SimType, &sim.State, &sim.RATType,
			&sim.MaxConcurrentSessions, &sim.SessionIdleTimeoutSec, &sim.SessionHardTimeoutSec,
			&sim.Metadata, &sim.ActivatedAt, &sim.SuspendedAt, &sim.TerminatedAt, &sim.PurgeAt,
			&sim.CreatedAt, &sim.UpdatedAt,
		); err != nil {
			return nil, "", fmt.Errorf("store: scan sim: %w", err)
		}
		results = append(results, sim)
	}

	nextCursor := ""
	if len(results) > limit {
		nextCursor = results[limit-1].ID.String()
		results = results[:limit]
	}

	return results, nextCursor, nil
}

func (s *SIMStore) ListStateHistory(ctx context.Context, simID uuid.UUID, cursor string, limit int) ([]SimStateHistory, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{simID}
	conditions := []string{"sim_id = $1"}
	argIdx := 2

	if cursor != "" {
		cursorID, parseErr := parseInt64(cursor)
		if parseErr == nil {
			conditions = append(conditions, fmt.Sprintf("id < $%d", argIdx))
			args = append(args, cursorID)
			argIdx++
		}
	}

	where := "WHERE " + strings.Join(conditions, " AND ")

	args = append(args, limit+1)
	limitPlaceholder := fmt.Sprintf("$%d", argIdx)

	query := fmt.Sprintf(`SELECT id, sim_id, from_state, to_state, reason, triggered_by, user_id, job_id, created_at
		FROM sim_state_history %s ORDER BY created_at DESC, id DESC LIMIT %s`,
		where, limitPlaceholder)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list sim state history: %w", err)
	}
	defer rows.Close()

	var results []SimStateHistory
	for rows.Next() {
		var h SimStateHistory
		if err := rows.Scan(
			&h.ID, &h.SimID, &h.FromState, &h.ToState, &h.Reason,
			&h.TriggeredBy, &h.UserID, &h.JobID, &h.CreatedAt,
		); err != nil {
			return nil, "", fmt.Errorf("store: scan sim state history: %w", err)
		}
		results = append(results, h)
	}

	nextCursor := ""
	if len(results) > limit {
		nextCursor = fmt.Sprintf("%d", results[limit-1].ID)
		results = results[:limit]
	}

	return results, nextCursor, nil
}

func (s *SIMStore) Activate(ctx context.Context, tenantID, simID uuid.UUID, ipAddressID uuid.UUID, userID *uuid.UUID) (*SIM, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: begin tx for activate: %w", err)
	}
	defer tx.Rollback(ctx)

	var currentState string
	var operatorID uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT state, operator_id FROM sims WHERE id = $1 AND tenant_id = $2 FOR UPDATE`,
		simID, tenantID,
	).Scan(&currentState, &operatorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSIMNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: lock sim for activate: %w", err)
	}

	if err := validateTransition(currentState, "active"); err != nil {
		return nil, err
	}

	row := tx.QueryRow(ctx, `
		UPDATE sims SET state = 'active', ip_address_id = $3, activated_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2
		RETURNING `+simColumns,
		simID, tenantID, ipAddressID,
	)
	sim, err := scanSIM(row)
	if err != nil {
		return nil, fmt.Errorf("store: update sim activate: %w", err)
	}

	if err := insertStateHistory(ctx, tx, simID, &currentState, "active", "user", userID, nil); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("store: commit activate: %w", err)
	}

	return sim, nil
}

func (s *SIMStore) Suspend(ctx context.Context, tenantID, simID uuid.UUID, userID *uuid.UUID, reason *string) (*SIM, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: begin tx for suspend: %w", err)
	}
	defer tx.Rollback(ctx)

	var currentState string
	err = tx.QueryRow(ctx,
		`SELECT state FROM sims WHERE id = $1 AND tenant_id = $2 FOR UPDATE`,
		simID, tenantID,
	).Scan(&currentState)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSIMNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: lock sim for suspend: %w", err)
	}

	if err := validateTransition(currentState, "suspended"); err != nil {
		return nil, err
	}

	row := tx.QueryRow(ctx, `
		UPDATE sims SET state = 'suspended', suspended_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2
		RETURNING `+simColumns,
		simID, tenantID,
	)
	sim, err := scanSIM(row)
	if err != nil {
		return nil, fmt.Errorf("store: update sim suspend: %w", err)
	}

	if err := insertStateHistory(ctx, tx, simID, &currentState, "suspended", "user", userID, reason); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("store: commit suspend: %w", err)
	}

	return sim, nil
}

func (s *SIMStore) Resume(ctx context.Context, tenantID, simID uuid.UUID, userID *uuid.UUID) (*SIM, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: begin tx for resume: %w", err)
	}
	defer tx.Rollback(ctx)

	var currentState string
	err = tx.QueryRow(ctx,
		`SELECT state FROM sims WHERE id = $1 AND tenant_id = $2 FOR UPDATE`,
		simID, tenantID,
	).Scan(&currentState)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSIMNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: lock sim for resume: %w", err)
	}

	if err := validateTransition(currentState, "active"); err != nil {
		return nil, err
	}

	row := tx.QueryRow(ctx, `
		UPDATE sims SET state = 'active', suspended_at = NULL, updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2
		RETURNING `+simColumns,
		simID, tenantID,
	)
	sim, err := scanSIM(row)
	if err != nil {
		return nil, fmt.Errorf("store: update sim resume: %w", err)
	}

	if err := insertStateHistory(ctx, tx, simID, &currentState, "active", "user", userID, nil); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("store: commit resume: %w", err)
	}

	return sim, nil
}

func (s *SIMStore) Terminate(ctx context.Context, tenantID, simID uuid.UUID, userID *uuid.UUID, reason *string, purgeRetentionDays int) (*SIM, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: begin tx for terminate: %w", err)
	}
	defer tx.Rollback(ctx)

	var currentState string
	var ipAddressID *uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT state, ip_address_id FROM sims WHERE id = $1 AND tenant_id = $2 FOR UPDATE`,
		simID, tenantID,
	).Scan(&currentState, &ipAddressID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSIMNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: lock sim for terminate: %w", err)
	}

	if err := validateTransition(currentState, "terminated"); err != nil {
		return nil, err
	}

	purgeInterval := fmt.Sprintf("%d days", purgeRetentionDays)
	row := tx.QueryRow(ctx, `
		UPDATE sims SET state = 'terminated', terminated_at = NOW(),
			purge_at = NOW() + $3::interval, updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2
		RETURNING `+simColumns,
		simID, tenantID, purgeInterval,
	)
	sim, err := scanSIM(row)
	if err != nil {
		return nil, fmt.Errorf("store: update sim terminate: %w", err)
	}

	if ipAddressID != nil {
		_, err = tx.Exec(ctx, `
			UPDATE ip_addresses SET state = 'reclaiming', reclaim_at = NOW() + $2::interval
			WHERE sim_id = $1 AND state IN ('allocated', 'reserved')`,
			simID, purgeInterval,
		)
		if err != nil {
			return nil, fmt.Errorf("store: schedule ip reclaim: %w", err)
		}
	}

	if err := insertStateHistory(ctx, tx, simID, &currentState, "terminated", "user", userID, reason); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("store: commit terminate: %w", err)
	}

	return sim, nil
}

func (s *SIMStore) ReportLost(ctx context.Context, tenantID, simID uuid.UUID, userID *uuid.UUID, reason *string) (*SIM, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: begin tx for report lost: %w", err)
	}
	defer tx.Rollback(ctx)

	var currentState string
	err = tx.QueryRow(ctx,
		`SELECT state FROM sims WHERE id = $1 AND tenant_id = $2 FOR UPDATE`,
		simID, tenantID,
	).Scan(&currentState)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSIMNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: lock sim for report lost: %w", err)
	}

	if err := validateTransition(currentState, "stolen_lost"); err != nil {
		return nil, err
	}

	row := tx.QueryRow(ctx, `
		UPDATE sims SET state = 'stolen_lost', updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2
		RETURNING `+simColumns,
		simID, tenantID,
	)
	sim, err := scanSIM(row)
	if err != nil {
		return nil, fmt.Errorf("store: update sim report lost: %w", err)
	}

	if err := insertStateHistory(ctx, tx, simID, &currentState, "stolen_lost", "user", userID, reason); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("store: commit report lost: %w", err)
	}

	return sim, nil
}

func insertStateHistory(ctx context.Context, tx pgx.Tx, simID uuid.UUID, fromState *string, toState, triggeredBy string, userID *uuid.UUID, reason *string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO sim_state_history (sim_id, from_state, to_state, reason, triggered_by, user_id)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		simID, fromState, toState, reason, triggeredBy, userID,
	)
	if err != nil {
		return fmt.Errorf("store: insert sim state history: %w", err)
	}
	return nil
}

func (s *SIMStore) InsertHistory(ctx context.Context, simID uuid.UUID, fromState *string, toState string, triggeredBy string, userID interface{}, reason interface{}) error {
	var uid *uuid.UUID
	if v, ok := userID.(*uuid.UUID); ok {
		uid = v
	}
	var r *string
	if v, ok := reason.(*string); ok {
		r = v
	}
	_, err := s.db.Exec(ctx, `
		INSERT INTO sim_state_history (sim_id, from_state, to_state, reason, triggered_by, user_id)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		simID, fromState, toState, r, triggeredBy, uid,
	)
	if err != nil {
		return fmt.Errorf("store: insert sim state history: %w", err)
	}
	return nil
}

func (s *SIMStore) TransitionState(ctx context.Context, simID uuid.UUID, targetState string, userID *uuid.UUID, triggeredBy string, reason interface{}, purgeRetentionDays int) (*SIM, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: begin tx for transition: %w", err)
	}
	defer tx.Rollback(ctx)

	var currentState string
	var tenantID uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT state, tenant_id FROM sims WHERE id = $1 FOR UPDATE`,
		simID,
	).Scan(&currentState, &tenantID)
	// Note: tenant_id is fetched but not used as a filter here because TransitionState
	// is called internally (e.g., from bulk import) where the SIM was just created
	// by the same process. All public-facing handlers use dedicated methods with tenant scoping.
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSIMNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: lock sim for transition: %w", err)
	}

	if err := validateTransition(currentState, targetState); err != nil {
		return nil, err
	}

	var setClause string
	var args []interface{}
	switch targetState {
	case "active":
		setClause = `state = 'active', activated_at = COALESCE(activated_at, NOW()), suspended_at = NULL, updated_at = NOW()`
		args = []interface{}{simID, tenantID}
	case "suspended":
		setClause = `state = 'suspended', suspended_at = NOW(), updated_at = NOW()`
		args = []interface{}{simID, tenantID}
	case "terminated":
		purgeInterval := fmt.Sprintf("%d days", purgeRetentionDays)
		setClause = `state = 'terminated', terminated_at = NOW(), purge_at = NOW() + $3::interval, updated_at = NOW()`
		args = []interface{}{simID, tenantID, purgeInterval}
	case "stolen_lost":
		setClause = `state = 'stolen_lost', updated_at = NOW()`
		args = []interface{}{simID, tenantID}
	default:
		setClause = `state = $3, updated_at = NOW()`
		args = []interface{}{simID, tenantID, targetState}
	}

	row := tx.QueryRow(ctx,
		`UPDATE sims SET `+setClause+` WHERE id = $1 AND tenant_id = $2 RETURNING `+simColumns,
		args...,
	)
	sim, err := scanSIM(row)
	if err != nil {
		return nil, fmt.Errorf("store: update sim transition: %w", err)
	}

	var reasonStr *string
	if v, ok := reason.(*string); ok {
		reasonStr = v
	}

	if err := insertStateHistory(ctx, tx, simID, &currentState, targetState, triggeredBy, userID, reasonStr); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("store: commit transition: %w", err)
	}

	return sim, nil
}

func (s *SIMStore) SetIPAndPolicy(ctx context.Context, simID uuid.UUID, ipAddressID *uuid.UUID, policyVersionID *uuid.UUID) error {
	sets := []string{}
	args := []interface{}{simID}
	argIdx := 2

	if ipAddressID != nil {
		sets = append(sets, fmt.Sprintf("ip_address_id = $%d", argIdx))
		args = append(args, *ipAddressID)
		argIdx++
	}
	if policyVersionID != nil {
		sets = append(sets, fmt.Sprintf("policy_version_id = $%d", argIdx))
		args = append(args, *policyVersionID)
		argIdx++
	}

	if len(sets) == 0 {
		return nil
	}

	query := fmt.Sprintf(`UPDATE sims SET %s WHERE id = $1`, strings.Join(sets, ", "))
	_, err := s.db.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("store: set ip and policy: %w", err)
	}
	return nil
}

func parseInt64(s string) (int64, error) {
	var n int64
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}

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
	ID                    uuid.UUID       `json:"id"`
	TenantID              uuid.UUID       `json:"tenant_id"`
	OperatorID            uuid.UUID       `json:"operator_id"`
	APNID                 *uuid.UUID      `json:"apn_id"`
	ICCID                 string          `json:"iccid"`
	IMSI                  string          `json:"imsi"`
	MSISDN                *string         `json:"msisdn"`
	IPAddressID           *uuid.UUID      `json:"ip_address_id"`
	PolicyVersionID       *uuid.UUID      `json:"policy_version_id"`
	ESimProfileID         *uuid.UUID      `json:"esim_profile_id"`
	SimType               string          `json:"sim_type"`
	State                 string          `json:"state"`
	RATType               *string         `json:"rat_type"`
	MaxConcurrentSessions int             `json:"max_concurrent_sessions"`
	SessionIdleTimeoutSec int             `json:"session_idle_timeout_sec"`
	SessionHardTimeoutSec int             `json:"session_hard_timeout_sec"`
	Metadata              json.RawMessage `json:"metadata"`
	ActivatedAt           *time.Time      `json:"activated_at"`
	SuspendedAt           *time.Time      `json:"suspended_at"`
	TerminatedAt          *time.Time      `json:"terminated_at"`
	PurgeAt               *time.Time      `json:"purge_at"`
	CreatedAt             time.Time       `json:"created_at"`
	UpdatedAt             time.Time       `json:"updated_at"`
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
	IPAddress  string
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
	"stolen_lost": {"terminated"},
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

// RecentVelocityPerHour returns the average number of SIMs created per
// hour over the last 24 hours. Used for the dashboard "SIM Velocity"
// KPI card. Returns 0 when no SIMs were added in the window.
func (s *SIMStore) RecentVelocityPerHour(ctx context.Context, tenantID uuid.UUID) (float64, error) {
	var count int64
	err := s.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM sims
		WHERE tenant_id = $1 AND created_at >= NOW() - INTERVAL '24 hours'
	`, tenantID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: sim velocity: %w", err)
	}
	return float64(count) / 24.0, nil
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
		if refErr, ok := asInvalidReference(err, simsFKConstraintColumn); ok {
			return nil, refErr
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

	if p.IPAddress != "" {
		conditions = append(conditions, fmt.Sprintf(
			"ip_address_id IN (SELECT id FROM ip_addresses WHERE address_v4::text LIKE $%d)",
			argIdx,
		))
		args = append(args, "%"+p.IPAddress+"%")
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

	activateReason := "activate"
	if err := insertStateHistory(ctx, tx, simID, &currentState, "active", "user", userID, &activateReason); err != nil {
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

	if currentState != "suspended" {
		return nil, ErrInvalidStateTransition
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

	resumeReason := "resume"
	if err := insertStateHistory(ctx, tx, simID, &currentState, "active", "user", userID, &resumeReason); err != nil {
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

	_, err = tx.Exec(ctx, `
		UPDATE msisdn_pool SET state = 'reserved', reserved_until = NOW() + $2::interval
		WHERE sim_id = $1 AND tenant_id = $3 AND state = 'assigned'`,
		simID, purgeInterval, tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: schedule msisdn release: %w", err)
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

func (s *SIMStore) RestoreState(ctx context.Context, tenantID, simID uuid.UUID, state string) (*SIM, error) {
	if state != "active" && state != "suspended" {
		return nil, ErrInvalidStateTransition
	}

	row := s.db.QueryRow(ctx, `
		UPDATE sims
		SET state = $3,
			activated_at = CASE
				WHEN $3 = 'active' THEN COALESCE(activated_at, NOW())
				ELSE activated_at
			END,
			suspended_at = CASE
				WHEN $3 = 'suspended' THEN COALESCE(suspended_at, NOW())
				ELSE NULL
			END,
			terminated_at = NULL,
			purge_at = NULL,
			updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2
		RETURNING `+simColumns,
		simID, tenantID, state,
	)

	sim, err := scanSIM(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSIMNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: restore sim state: %w", err)
	}
	return sim, nil
}

var ErrSIMStateBlocked = errors.New("store: sim state blocked for update")

func (s *SIMStore) PatchMetadata(ctx context.Context, tenantID, simID uuid.UUID, patch map[string]interface{}) (*SIM, error) {
	patchJSON, err := json.Marshal(patch)
	if err != nil {
		return nil, fmt.Errorf("store: marshal patch metadata: %w", err)
	}

	row := s.db.QueryRow(ctx, `
		UPDATE sims SET metadata = metadata || $3, updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2 AND state NOT IN ('terminated', 'purged')
		RETURNING `+simColumns,
		simID, tenantID, patchJSON,
	)

	sim, err := scanSIM(row)
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		checkErr := s.db.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM sims WHERE id = $1 AND tenant_id = $2)`,
			simID, tenantID,
		).Scan(&exists)
		if checkErr != nil {
			return nil, fmt.Errorf("store: check sim exists: %w", checkErr)
		}
		if !exists {
			return nil, ErrSIMNotFound
		}
		return nil, ErrSIMStateBlocked
	}
	if err != nil {
		return nil, fmt.Errorf("store: patch sim metadata: %w", err)
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

// ClearIPAddress nils-out the sims.ip_address_id column for the given SIM.
// Used by the RADIUS / Diameter Accounting/CCR-T release paths (STORY-092
// Wave 2) when a dynamic IP allocation is returned to the pool. SetIPAndPolicy
// cannot clear (it only sets non-nil pointers), so this is its inverse.
func (s *SIMStore) ClearIPAddress(ctx context.Context, simID uuid.UUID) error {
	_, err := s.db.Exec(ctx, `UPDATE sims SET ip_address_id = NULL WHERE id = $1`, simID)
	if err != nil {
		return fmt.Errorf("store: clear ip address: %w", err)
	}
	return nil
}

// GetByIMSI is INTENTIONALLY UNSCOPED for the RADIUS/Diameter hot path — see
// DEV-041/DEV-166. The AAA stack authenticates by IMSI before any tenant
// context exists, so the lookup cannot be tenant-scoped. API callers MUST
// use GetByIMSIScoped instead; the unscoped variant is reserved for
// internal/aaa/radius and internal/aaa/diameter.
func (s *SIMStore) GetByIMSI(ctx context.Context, imsi string) (*SIM, error) {
	row := s.db.QueryRow(ctx,
		`SELECT `+simColumns+` FROM sims WHERE imsi = $1 LIMIT 1`,
		imsi,
	)
	sim, err := scanSIM(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSIMNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get sim by imsi: %w", err)
	}
	return sim, nil
}

// GetByIMSIScoped is the tenant-scoped variant of GetByIMSI and MUST be used
// by every API caller. The RADIUS/Diameter hot path continues to use the
// unscoped GetByIMSI per DEV-041/DEV-166.
func (s *SIMStore) GetByIMSIScoped(ctx context.Context, imsi string, tenantID uuid.UUID) (*SIM, error) {
	row := s.db.QueryRow(ctx,
		`SELECT `+simColumns+` FROM sims WHERE imsi = $1 AND tenant_id = $2 LIMIT 1`,
		imsi, tenantID,
	)
	sim, err := scanSIM(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSIMNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get sim by imsi scoped: %w", err)
	}
	return sim, nil
}

// SIMSummary is a lightweight projection of sims used for bulk processing.
// OperatorID is non-nil (NOT NULL in schema). PolicyVersionID may be nil.
type SIMSummary struct {
	ID              uuid.UUID  `json:"id"`
	ICCID           string     `json:"iccid"`
	IMSI            string     `json:"imsi"`
	State           string     `json:"state"`
	PolicyVersionID *uuid.UUID `json:"policy_version_id"`
	OperatorID      uuid.UUID  `json:"operator_id"`
	SimType         string     `json:"sim_type"`
}

// FilterSIMIDsByTenant returns (owned, violations, error).
// owned contains IDs whose sims.tenant_id == tenantID.
// violations contains IDs with no matching row OR a different tenant_id —
// callers treat both cases as 403 to avoid revealing existence.
// Duplicate input IDs are deduplicated: a dup appears at most once in owned
// and never in violations.
// Input is chunked into batches of bulkImportBatchSize (500) to stay within
// Postgres parameter limits.
func (s *SIMStore) FilterSIMIDsByTenant(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) ([]uuid.UUID, []uuid.UUID, error) {
	if len(ids) == 0 {
		return []uuid.UUID{}, []uuid.UUID{}, nil
	}

	seen := make(map[uuid.UUID]struct{}, len(ids))
	deduped := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			deduped = append(deduped, id)
		}
	}

	ownedSet := make(map[uuid.UUID]struct{}, len(deduped))
	for batchStart := 0; batchStart < len(deduped); batchStart += bulkImportBatchSize {
		batchEnd := batchStart + bulkImportBatchSize
		if batchEnd > len(deduped) {
			batchEnd = len(deduped)
		}
		batch := deduped[batchStart:batchEnd]

		rows, err := s.db.Query(ctx,
			`SELECT id FROM sims WHERE tenant_id = $1 AND id = ANY($2)`,
			tenantID, batch,
		)
		if err != nil {
			return nil, nil, fmt.Errorf("store: filter sim ids by tenant: %w", err)
		}
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, nil, fmt.Errorf("store: scan sim id: %w", err)
			}
			ownedSet[id] = struct{}{}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, nil, fmt.Errorf("store: iter sim ids: %w", err)
		}
	}

	owned := make([]uuid.UUID, 0, len(ownedSet))
	violations := make([]uuid.UUID, 0)
	for _, id := range deduped {
		if _, ok := ownedSet[id]; ok {
			owned = append(owned, id)
		} else {
			violations = append(violations, id)
		}
	}
	return owned, violations, nil
}

// GetSIMsByIDs returns SIMSummary rows for all IDs owned by tenantID.
// IDs that do not belong to tenantID are silently excluded (call
// FilterSIMIDsByTenant first if you need the violations list).
// Input is chunked into batches of bulkImportBatchSize (500).
func (s *SIMStore) GetSIMsByIDs(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) ([]SIMSummary, error) {
	if len(ids) == 0 {
		return []SIMSummary{}, nil
	}

	results := make([]SIMSummary, 0, len(ids))
	for batchStart := 0; batchStart < len(ids); batchStart += bulkImportBatchSize {
		batchEnd := batchStart + bulkImportBatchSize
		if batchEnd > len(ids) {
			batchEnd = len(ids)
		}
		batch := ids[batchStart:batchEnd]

		rows, err := s.db.Query(ctx,
			`SELECT id, iccid, imsi, state, policy_version_id, operator_id, sim_type
			 FROM sims WHERE tenant_id = $1 AND id = ANY($2)`,
			tenantID, batch,
		)
		if err != nil {
			return nil, fmt.Errorf("store: get sims by ids: %w", err)
		}
		for rows.Next() {
			var sim SIMSummary
			if err := rows.Scan(&sim.ID, &sim.ICCID, &sim.IMSI, &sim.State, &sim.PolicyVersionID, &sim.OperatorID, &sim.SimType); err != nil {
				rows.Close()
				return nil, fmt.Errorf("store: scan sim summary: %w", err)
			}
			results = append(results, sim)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("store: iter sim summaries: %w", err)
		}
	}
	return results, nil
}

func parseInt64(s string) (int64, error) {
	var n int64
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}

type SIMFleetFilters struct {
	OperatorIDs []uuid.UUID
	APNIDs      []uuid.UUID
	RATTypes    []string
	SegmentID   *uuid.UUID
}

type OperatorCount struct {
	Name  string
	Count int
}

type APNCount struct {
	Name  string
	Count int
}

type RATCount struct {
	Name  string
	Count int
}

func (s *SIMStore) buildFleetFilterClauses(tenantID uuid.UUID, filters SIMFleetFilters, argIdx int, args []interface{}) ([]string, []interface{}, int) {
	conditions := []string{fmt.Sprintf("s.tenant_id = $%d", argIdx)}
	args = append(args, tenantID)
	argIdx++

	conditions = append(conditions, "s.state = 'active'")

	if len(filters.OperatorIDs) > 0 {
		placeholders := make([]string, len(filters.OperatorIDs))
		for i, id := range filters.OperatorIDs {
			placeholders[i] = fmt.Sprintf("$%d", argIdx)
			args = append(args, id)
			argIdx++
		}
		conditions = append(conditions, fmt.Sprintf("s.operator_id IN (%s)", strings.Join(placeholders, ", ")))
	}

	if len(filters.APNIDs) > 0 {
		placeholders := make([]string, len(filters.APNIDs))
		for i, id := range filters.APNIDs {
			placeholders[i] = fmt.Sprintf("$%d", argIdx)
			args = append(args, id)
			argIdx++
		}
		conditions = append(conditions, fmt.Sprintf("s.apn_id IN (%s)", strings.Join(placeholders, ", ")))
	}

	if len(filters.RATTypes) > 0 {
		placeholders := make([]string, len(filters.RATTypes))
		for i, rt := range filters.RATTypes {
			placeholders[i] = fmt.Sprintf("$%d", argIdx)
			args = append(args, rt)
			argIdx++
		}
		conditions = append(conditions, fmt.Sprintf("s.rat_type IN (%s)", strings.Join(placeholders, ", ")))
	}

	return conditions, args, argIdx
}

func (s *SIMStore) CountByFilters(ctx context.Context, tenantID uuid.UUID, filters SIMFleetFilters) (int, error) {
	args := []interface{}{}
	conditions, args, _ := s.buildFleetFilterClauses(tenantID, filters, 1, args)

	query := fmt.Sprintf(`SELECT COUNT(*) FROM sims s WHERE %s`, strings.Join(conditions, " AND "))

	var count int
	err := s.db.QueryRow(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: count sims by filters: %w", err)
	}
	return count, nil
}

func (s *SIMStore) AggregateByOperator(ctx context.Context, tenantID uuid.UUID, filters SIMFleetFilters) ([]OperatorCount, error) {
	args := []interface{}{}
	conditions, args, _ := s.buildFleetFilterClauses(tenantID, filters, 1, args)

	query := fmt.Sprintf(
		`SELECT o.name, COUNT(*) FROM sims s JOIN operators o ON s.operator_id = o.id WHERE %s GROUP BY o.id, o.name ORDER BY COUNT(*) DESC`,
		strings.Join(conditions, " AND "),
	)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: aggregate by operator: %w", err)
	}
	defer rows.Close()

	var results []OperatorCount
	for rows.Next() {
		var oc OperatorCount
		if err := rows.Scan(&oc.Name, &oc.Count); err != nil {
			return nil, fmt.Errorf("store: scan operator count: %w", err)
		}
		results = append(results, oc)
	}
	return results, nil
}

func (s *SIMStore) AggregateByAPN(ctx context.Context, tenantID uuid.UUID, filters SIMFleetFilters) ([]APNCount, error) {
	args := []interface{}{}
	conditions, args, _ := s.buildFleetFilterClauses(tenantID, filters, 1, args)

	query := fmt.Sprintf(
		`SELECT COALESCE(a.name, 'unassigned'), COUNT(*) FROM sims s LEFT JOIN apns a ON s.apn_id = a.id WHERE %s GROUP BY a.id, a.name ORDER BY COUNT(*) DESC`,
		strings.Join(conditions, " AND "),
	)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: aggregate by apn: %w", err)
	}
	defer rows.Close()

	var results []APNCount
	for rows.Next() {
		var ac APNCount
		if err := rows.Scan(&ac.Name, &ac.Count); err != nil {
			return nil, fmt.Errorf("store: scan apn count: %w", err)
		}
		results = append(results, ac)
	}
	return results, nil
}

func (s *SIMStore) AggregateByRATType(ctx context.Context, tenantID uuid.UUID, filters SIMFleetFilters) ([]RATCount, error) {
	args := []interface{}{}
	conditions, args, _ := s.buildFleetFilterClauses(tenantID, filters, 1, args)

	query := fmt.Sprintf(
		`SELECT COALESCE(s.rat_type, 'unknown'), COUNT(*) FROM sims s WHERE %s GROUP BY s.rat_type ORDER BY COUNT(*) DESC`,
		strings.Join(conditions, " AND "),
	)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: aggregate by rat type: %w", err)
	}
	defer rows.Close()

	var results []RATCount
	for rows.Next() {
		var rc RATCount
		if err := rows.Scan(&rc.Name, &rc.Count); err != nil {
			return nil, fmt.Errorf("store: scan rat count: %w", err)
		}
		results = append(results, rc)
	}
	return results, nil
}

func (s *SIMStore) FetchSample(ctx context.Context, tenantID uuid.UUID, filters SIMFleetFilters, limit int) ([]SIM, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}

	args := []interface{}{}
	conditions, args, argIdx := s.buildFleetFilterClauses(tenantID, filters, 1, args)

	args = append(args, limit)
	limitPlaceholder := fmt.Sprintf("$%d", argIdx)

	query := fmt.Sprintf(
		`SELECT %s FROM sims s WHERE %s ORDER BY s.created_at DESC LIMIT %s`,
		simColumns, strings.Join(conditions, " AND "), limitPlaceholder,
	)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: fetch sample sims: %w", err)
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
			return nil, fmt.Errorf("store: scan sample sim: %w", err)
		}
		results = append(results, sim)
	}
	return results, nil
}

func (s *SIMStore) UpdateLastRATType(ctx context.Context, simID uuid.UUID, operatorID uuid.UUID, ratType string) error {
	tag, err := s.db.Exec(ctx,
		`UPDATE sims SET rat_type = $3, updated_at = NOW() WHERE id = $1 AND operator_id = $2`,
		simID, operatorID, ratType,
	)
	if err != nil {
		return fmt.Errorf("store: update last rat_type: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSIMNotFound
	}
	return nil
}

type SIMStateCount struct {
	State string `json:"state"`
	Count int    `json:"count"`
}

func (s *SIMStore) CountByOperator(ctx context.Context, tenantID uuid.UUID) (map[uuid.UUID]int, error) {
	rows, err := s.db.Query(ctx, `
		SELECT operator_id, COUNT(*) FROM sims
		WHERE tenant_id = $1
		GROUP BY operator_id
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("store: count sims by operator: %w", err)
	}
	defer rows.Close()

	result := make(map[uuid.UUID]int)
	for rows.Next() {
		var opID uuid.UUID
		var count int
		if err := rows.Scan(&opID, &count); err != nil {
			return nil, fmt.Errorf("store: scan operator sim count: %w", err)
		}
		result[opID] = count
	}
	return result, nil
}

func (s *SIMStore) CountByTenant(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var count int
	err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM sims WHERE tenant_id = $1 AND state != 'purged'`, tenantID).
		Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: count sims by tenant: %w", err)
	}
	return count, nil
}

func (s *SIMStore) CountByAPN(ctx context.Context, tenantID uuid.UUID) (map[uuid.UUID]int64, error) {
	rows, err := s.db.Query(ctx, `
		SELECT apn_id, COUNT(*)
		FROM sims
		WHERE tenant_id = $1 AND apn_id IS NOT NULL AND state != 'purged'
		GROUP BY apn_id
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("store: count sims by apn: %w", err)
	}
	defer rows.Close()

	result := make(map[uuid.UUID]int64)
	for rows.Next() {
		var apnID uuid.UUID
		var count int64
		if err := rows.Scan(&apnID, &count); err != nil {
			return nil, fmt.Errorf("store: scan sim apn count: %w", err)
		}
		result[apnID] = count
	}
	return result, nil
}

func (s *SIMStore) CountByState(ctx context.Context, tenantID uuid.UUID) (int, []SIMStateCount, error) {
	rows, err := s.db.Query(ctx, `
		SELECT state, COUNT(*) FROM sims
		WHERE tenant_id = $1
		GROUP BY state
		ORDER BY COUNT(*) DESC
	`, tenantID)
	if err != nil {
		return 0, nil, fmt.Errorf("store: count sims by state: %w", err)
	}
	defer rows.Close()

	var results []SIMStateCount
	total := 0
	for rows.Next() {
		var sc SIMStateCount
		if err := rows.Scan(&sc.State, &sc.Count); err != nil {
			return 0, nil, fmt.Errorf("store: scan state count: %w", err)
		}
		total += sc.Count
		results = append(results, sc)
	}
	return total, results, nil
}

// CountByPolicyID returns the number of non-purged SIMs currently on any version of the
// given policy within the tenant. Canonical source for "SIMs on policy X" (FIX-208 F-125
// fix). Reads from sims.policy_version_id joined via policy_versions.policy_id, NOT from
// policy_assignments (which is kept for CoA/audit only and may include rows for removed
// SIMs — see FIX-208 duplication-audit).
//
// The policyID parameter is the policies.id (stable across version bumps), not a
// policy_version_id. The subquery resolves all versions belonging to that policy so the
// count reflects the policy regardless of which version is currently applied to each SIM.
func (s *SIMStore) CountByPolicyID(ctx context.Context, tenantID, policyID uuid.UUID) (int, error) {
	var count int
	err := s.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM sims
		WHERE tenant_id = $1
		  AND state != 'purged'
		  AND policy_version_id IN (SELECT id FROM policy_versions WHERE policy_id = $2)
	`, tenantID, policyID).Scan(&count)
	if err == nil {
		return count, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return 0, fmt.Errorf("store: count sims by policy: %w", err)
}

// ---------------------------------------------------------------------------
// SIMWithNames — enriched SIM with joined parent-entity display fields.
// ---------------------------------------------------------------------------

// SIMWithNames is an enriched SIM with joined parent-entity display fields.
// Used by list/detail endpoints that need UI-ready labels without per-row lookups.
type SIMWithNames struct {
	SIM
	OperatorName        *string
	OperatorCode        *string
	APNName             *string
	PolicyName          *string
	PolicyVersionNumber *int
}

// PAT-006: scanSIMWithNames is the ONLY scan helper for SIMWithNames.
// If you add fields to SIMWithNames, update this function AND all tests.
// Never inline-scan SIMWithNames rows elsewhere.
func scanSIMWithNames(row pgx.Row) (*SIMWithNames, error) {
	var s SIMWithNames
	err := row.Scan(
		&s.ID, &s.TenantID, &s.OperatorID, &s.APNID, &s.ICCID, &s.IMSI, &s.MSISDN,
		&s.IPAddressID, &s.PolicyVersionID, &s.ESimProfileID, &s.SimType, &s.State, &s.RATType,
		&s.MaxConcurrentSessions, &s.SessionIdleTimeoutSec, &s.SessionHardTimeoutSec,
		&s.Metadata, &s.ActivatedAt, &s.SuspendedAt, &s.TerminatedAt, &s.PurgeAt,
		&s.CreatedAt, &s.UpdatedAt,
		&s.OperatorName, &s.OperatorCode, &s.APNName, &s.PolicyName, &s.PolicyVersionNumber,
	)
	return &s, err
}

// simEnrichedJoin is the JOIN clause shared by all enriched SIM queries.
// LEFT JOINs are required for AC-8 orphan safety — INNER would hide orphan rows.
// operators is tenant-agnostic; apns and policies are tenant-scoped on the JOIN.
const simEnrichedJoin = `
LEFT JOIN operators o ON s.operator_id = o.id
LEFT JOIN apns a ON s.apn_id = a.id AND a.tenant_id = $1
LEFT JOIN policy_versions pv ON s.policy_version_id = pv.id
LEFT JOIN policies pol ON pv.policy_id = pol.id AND pol.tenant_id = $1`

// simEnrichedColumns is the SELECT list for enriched queries (after simColumns with s. prefix).
const simEnrichedSelect = `s.id, s.tenant_id, s.operator_id, s.apn_id, s.iccid, s.imsi, s.msisdn,
	s.ip_address_id, s.policy_version_id, s.esim_profile_id, s.sim_type, s.state, s.rat_type,
	s.max_concurrent_sessions, s.session_idle_timeout_sec, s.session_hard_timeout_sec,
	s.metadata, s.activated_at, s.suspended_at, s.terminated_at, s.purge_at,
	s.created_at, s.updated_at,
	o.name AS operator_name, o.code AS operator_code,
	COALESCE(NULLIF(a.display_name, ''), a.name) AS apn_name,
	pol.name AS policy_name,
	pv.version AS policy_version_number`

// buildSIMWhereClause builds WHERE predicates and args for SIM list queries.
// tableAlias is the table alias prefix (e.g. "s." for enriched queries, "" for plain queries).
// The caller MUST provide tenantID as $1 before calling this function.
// argIdx must start at 2 (since $1 is always tenantID).
// Returns (conditions, args, nextArgIdx).
func buildSIMWhereClause(p ListSIMsParams, tableAlias string, args []interface{}, argIdx int) ([]string, []interface{}, int) {
	ta := tableAlias

	conditions := []string{}

	if p.ICCID != "" {
		conditions = append(conditions, fmt.Sprintf("%siccid = $%d", ta, argIdx))
		args = append(args, p.ICCID)
		argIdx++
	}

	if p.IMSI != "" {
		conditions = append(conditions, fmt.Sprintf("%simsi = $%d", ta, argIdx))
		args = append(args, p.IMSI)
		argIdx++
	}

	if p.MSISDN != "" {
		conditions = append(conditions, fmt.Sprintf("%smsisdn = $%d", ta, argIdx))
		args = append(args, p.MSISDN)
		argIdx++
	}

	if p.OperatorID != nil {
		conditions = append(conditions, fmt.Sprintf("%soperator_id = $%d", ta, argIdx))
		args = append(args, *p.OperatorID)
		argIdx++
	}

	if p.APNID != nil {
		conditions = append(conditions, fmt.Sprintf("%sapn_id = $%d", ta, argIdx))
		args = append(args, *p.APNID)
		argIdx++
	}

	if p.State != "" {
		conditions = append(conditions, fmt.Sprintf("%sstate = $%d", ta, argIdx))
		args = append(args, p.State)
		argIdx++
	}

	if p.RATType != "" {
		conditions = append(conditions, fmt.Sprintf("%srat_type = $%d", ta, argIdx))
		args = append(args, p.RATType)
		argIdx++
	}

	if p.IPAddress != "" {
		conditions = append(conditions, fmt.Sprintf(
			"%sip_address_id IN (SELECT id FROM ip_addresses WHERE address_v4::text LIKE $%d)",
			ta, argIdx,
		))
		args = append(args, "%"+p.IPAddress+"%")
		argIdx++
	}

	if p.Q != "" {
		searchTerm := "%" + p.Q + "%"
		conditions = append(conditions, fmt.Sprintf(
			"(%siccid ILIKE $%d OR %simsi ILIKE $%d OR %smsisdn ILIKE $%d)",
			ta, argIdx, ta, argIdx, ta, argIdx,
		))
		args = append(args, searchTerm)
		argIdx++
	}

	if p.Cursor != "" {
		cursorID, parseErr := uuid.Parse(p.Cursor)
		if parseErr == nil {
			conditions = append(conditions, fmt.Sprintf("%sid < $%d", ta, argIdx))
			args = append(args, cursorID)
			argIdx++
		}
	}

	return conditions, args, argIdx
}

// ListEnriched returns SIMs with joined parent-entity display names.
// Signature mirrors List; enriched fields are nullable (LEFT JOIN).
func (s *SIMStore) ListEnriched(ctx context.Context, tenantID uuid.UUID, p ListSIMsParams) ([]SIMWithNames, string, error) {
	limit := p.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{tenantID}
	conditions := []string{"s.tenant_id = $1"}
	argIdx := 2

	extraConds, args, argIdx := buildSIMWhereClause(p, "s.", args, argIdx)
	conditions = append(conditions, extraConds...)

	where := "WHERE " + strings.Join(conditions, " AND ")

	args = append(args, limit+1)
	limitPlaceholder := fmt.Sprintf("$%d", argIdx)

	query := fmt.Sprintf(`SELECT %s FROM sims s %s %s ORDER BY s.created_at DESC, s.id DESC LIMIT %s`,
		simEnrichedSelect, simEnrichedJoin, where, limitPlaceholder)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list enriched sims: %w", err)
	}
	defer rows.Close()

	var results []SIMWithNames
	for rows.Next() {
		sim, err := scanSIMWithNames(rows)
		if err != nil {
			return nil, "", fmt.Errorf("store: scan enriched sim: %w", err)
		}
		results = append(results, *sim)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("store: iter enriched sims: %w", err)
	}

	nextCursor := ""
	if len(results) > limit {
		nextCursor = results[limit-1].ID.String()
		results = results[:limit]
	}

	return results, nextCursor, nil
}

// GetByIDEnriched returns one enriched SIM by ID, scoped to tenantID.
// Returns ErrSIMNotFound when no row matches (including cross-tenant mismatches).
func (s *SIMStore) GetByIDEnriched(ctx context.Context, tenantID, id uuid.UUID) (*SIMWithNames, error) {
	query := fmt.Sprintf(`SELECT %s FROM sims s %s WHERE s.tenant_id = $1 AND s.id = $2`,
		simEnrichedSelect, simEnrichedJoin)

	row := s.db.QueryRow(ctx, query, tenantID, id)
	sim, err := scanSIMWithNames(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSIMNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get enriched sim: %w", err)
	}
	return sim, nil
}

// GetManyByIDsEnriched returns a map of enriched SIMs keyed by sim.ID.
// Only SIMs belonging to tenantID are returned; foreign IDs are silently excluded.
// Empty input returns an empty map without a DB call.
// Input is chunked into batches of bulkImportBatchSize (500).
func (s *SIMStore) GetManyByIDsEnriched(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]*SIMWithNames, error) {
	result := make(map[uuid.UUID]*SIMWithNames, len(ids))
	if len(ids) == 0 {
		return result, nil
	}

	for batchStart := 0; batchStart < len(ids); batchStart += bulkImportBatchSize {
		batchEnd := batchStart + bulkImportBatchSize
		if batchEnd > len(ids) {
			batchEnd = len(ids)
		}
		batch := ids[batchStart:batchEnd]

		query := fmt.Sprintf(`SELECT %s FROM sims s %s WHERE s.tenant_id = $1 AND s.id = ANY($2)`,
			simEnrichedSelect, simEnrichedJoin)

		rows, err := s.db.Query(ctx, query, tenantID, batch)
		if err != nil {
			return nil, fmt.Errorf("store: get many enriched sims: %w", err)
		}
		for rows.Next() {
			sim, err := scanSIMWithNames(rows)
			if err != nil {
				rows.Close()
				return nil, fmt.Errorf("store: scan many enriched sim: %w", err)
			}
			result[sim.ID] = sim
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("store: iter many enriched sims: %w", err)
		}
	}

	return result, nil
}

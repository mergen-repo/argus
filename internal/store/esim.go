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

var (
	ErrESimProfileNotFound  = errors.New("store: esim profile not found")
	ErrProfileAlreadyEnabled = errors.New("store: another profile is already enabled for this SIM")
	ErrInvalidProfileState  = errors.New("store: invalid profile state transition")
	ErrSameProfile          = errors.New("store: source and target profiles are the same")
	ErrDifferentSIM         = errors.New("store: profiles belong to different SIMs")
)

type ESimProfile struct {
	ID                uuid.UUID  `json:"id"`
	SimID             uuid.UUID  `json:"sim_id"`
	EID               string     `json:"eid"`
	SMDPPlusID        *string    `json:"sm_dp_plus_id"`
	OperatorID        uuid.UUID  `json:"operator_id"`
	ProfileState      string     `json:"profile_state"`
	ICCIDOnProfile    *string    `json:"iccid_on_profile"`
	LastProvisionedAt *time.Time `json:"last_provisioned_at"`
	LastError         *string    `json:"last_error"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type SwitchResult struct {
	SimID         uuid.UUID
	OldProfile    *ESimProfile
	NewProfile    *ESimProfile
	NewOperatorID uuid.UUID
}

type ListESimProfilesParams struct {
	Cursor     string
	Limit      int
	SimID      *uuid.UUID
	OperatorID *uuid.UUID
	State      string
}

type ESimProfileStore struct {
	db *pgxpool.Pool
}

func NewESimProfileStore(db *pgxpool.Pool) *ESimProfileStore {
	return &ESimProfileStore{db: db}
}

var esimProfileColumns = `ep.id, ep.sim_id, ep.eid, ep.sm_dp_plus_id, ep.operator_id,
	ep.profile_state, ep.iccid_on_profile, ep.last_provisioned_at, ep.last_error,
	ep.created_at, ep.updated_at`

func scanESimProfile(row pgx.Row) (*ESimProfile, error) {
	var p ESimProfile
	err := row.Scan(
		&p.ID, &p.SimID, &p.EID, &p.SMDPPlusID, &p.OperatorID,
		&p.ProfileState, &p.ICCIDOnProfile, &p.LastProvisionedAt, &p.LastError,
		&p.CreatedAt, &p.UpdatedAt,
	)
	return &p, err
}

func (s *ESimProfileStore) GetByID(ctx context.Context, tenantID, id uuid.UUID) (*ESimProfile, error) {
	row := s.db.QueryRow(ctx,
		`SELECT `+esimProfileColumns+` FROM esim_profiles ep
		JOIN sims si ON ep.sim_id = si.id
		WHERE ep.id = $1 AND si.tenant_id = $2`,
		id, tenantID,
	)
	p, err := scanESimProfile(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrESimProfileNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get esim profile: %w", err)
	}
	return p, nil
}

func (s *ESimProfileStore) List(ctx context.Context, tenantID uuid.UUID, p ListESimProfilesParams) ([]ESimProfile, string, error) {
	limit := p.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{tenantID}
	conditions := []string{"si.tenant_id = $1"}
	argIdx := 2

	if p.SimID != nil {
		conditions = append(conditions, fmt.Sprintf("ep.sim_id = $%d", argIdx))
		args = append(args, *p.SimID)
		argIdx++
	}

	if p.OperatorID != nil {
		conditions = append(conditions, fmt.Sprintf("ep.operator_id = $%d", argIdx))
		args = append(args, *p.OperatorID)
		argIdx++
	}

	if p.State != "" {
		conditions = append(conditions, fmt.Sprintf("ep.profile_state = $%d", argIdx))
		args = append(args, p.State)
		argIdx++
	}

	if p.Cursor != "" {
		cursorID, parseErr := uuid.Parse(p.Cursor)
		if parseErr == nil {
			conditions = append(conditions, fmt.Sprintf("ep.id < $%d", argIdx))
			args = append(args, cursorID)
			argIdx++
		}
	}

	where := "WHERE " + strings.Join(conditions, " AND ")

	args = append(args, limit+1)
	limitPlaceholder := fmt.Sprintf("$%d", argIdx)

	query := fmt.Sprintf(`SELECT %s FROM esim_profiles ep
		JOIN sims si ON ep.sim_id = si.id
		%s ORDER BY ep.created_at DESC, ep.id DESC LIMIT %s`,
		esimProfileColumns, where, limitPlaceholder)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list esim profiles: %w", err)
	}
	defer rows.Close()

	var results []ESimProfile
	for rows.Next() {
		var profile ESimProfile
		if err := rows.Scan(
			&profile.ID, &profile.SimID, &profile.EID, &profile.SMDPPlusID, &profile.OperatorID,
			&profile.ProfileState, &profile.ICCIDOnProfile, &profile.LastProvisionedAt, &profile.LastError,
			&profile.CreatedAt, &profile.UpdatedAt,
		); err != nil {
			return nil, "", fmt.Errorf("store: scan esim profile: %w", err)
		}
		results = append(results, profile)
	}

	nextCursor := ""
	if len(results) > limit {
		nextCursor = results[limit-1].ID.String()
		results = results[:limit]
	}

	return results, nextCursor, nil
}

func (s *ESimProfileStore) GetEnabledProfileForSIM(ctx context.Context, simID uuid.UUID) (*ESimProfile, error) {
	row := s.db.QueryRow(ctx,
		`SELECT `+esimProfileColumns+` FROM esim_profiles ep
		WHERE ep.sim_id = $1 AND ep.profile_state = 'enabled'`,
		simID,
	)
	p, err := scanESimProfile(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: get enabled profile for sim: %w", err)
	}
	return p, nil
}

func (s *ESimProfileStore) Enable(ctx context.Context, tenantID, profileID uuid.UUID, userID *uuid.UUID) (*ESimProfile, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: begin tx for enable profile: %w", err)
	}
	defer tx.Rollback(ctx)

	var currentState string
	var simID uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT ep.profile_state, ep.sim_id FROM esim_profiles ep
		JOIN sims si ON ep.sim_id = si.id
		WHERE ep.id = $1 AND si.tenant_id = $2
		FOR UPDATE OF ep`,
		profileID, tenantID,
	).Scan(&currentState, &simID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrESimProfileNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: lock esim profile for enable: %w", err)
	}

	if currentState != "disabled" {
		return nil, ErrInvalidProfileState
	}

	var enabledCount int
	err = tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM esim_profiles WHERE sim_id = $1 AND profile_state = 'enabled' AND id != $2`,
		simID, profileID,
	).Scan(&enabledCount)
	if err != nil {
		return nil, fmt.Errorf("store: check enabled profiles: %w", err)
	}
	if enabledCount > 0 {
		return nil, ErrProfileAlreadyEnabled
	}

	row := tx.QueryRow(ctx,
		`UPDATE esim_profiles SET profile_state = 'enabled', last_provisioned_at = NOW(), last_error = NULL, updated_at = NOW()
		WHERE id = $1
		RETURNING `+strings.ReplaceAll(esimProfileColumns, "ep.", ""),
		profileID,
	)
	profile, err := scanESimProfile(row)
	if err != nil {
		return nil, fmt.Errorf("store: update esim profile enable: %w", err)
	}

	_, err = tx.Exec(ctx,
		`UPDATE sims SET esim_profile_id = $1, updated_at = NOW() WHERE id = $2`,
		profileID, simID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: update sim esim_profile_id: %w", err)
	}

	fromState := "disabled"
	if err := insertStateHistory(ctx, tx, simID, &fromState, "active", "user", userID, nil); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("store: commit enable profile: %w", err)
	}

	return profile, nil
}

func (s *ESimProfileStore) Disable(ctx context.Context, tenantID, profileID uuid.UUID, userID *uuid.UUID) (*ESimProfile, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: begin tx for disable profile: %w", err)
	}
	defer tx.Rollback(ctx)

	var currentState string
	var simID uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT ep.profile_state, ep.sim_id FROM esim_profiles ep
		JOIN sims si ON ep.sim_id = si.id
		WHERE ep.id = $1 AND si.tenant_id = $2
		FOR UPDATE OF ep`,
		profileID, tenantID,
	).Scan(&currentState, &simID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrESimProfileNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: lock esim profile for disable: %w", err)
	}

	if currentState != "enabled" {
		return nil, ErrInvalidProfileState
	}

	row := tx.QueryRow(ctx,
		`UPDATE esim_profiles SET profile_state = 'disabled', last_provisioned_at = NOW(), last_error = NULL, updated_at = NOW()
		WHERE id = $1
		RETURNING `+strings.ReplaceAll(esimProfileColumns, "ep.", ""),
		profileID,
	)
	profile, err := scanESimProfile(row)
	if err != nil {
		return nil, fmt.Errorf("store: update esim profile disable: %w", err)
	}

	_, err = tx.Exec(ctx,
		`UPDATE sims SET esim_profile_id = NULL, updated_at = NOW() WHERE id = $1`,
		simID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: clear sim esim_profile_id: %w", err)
	}

	fromState := "enabled"
	reason := "esim profile disabled"
	if err := insertStateHistory(ctx, tx, simID, &fromState, "disabled", "user", userID, &reason); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("store: commit disable profile: %w", err)
	}

	return profile, nil
}

func (s *ESimProfileStore) Switch(ctx context.Context, tenantID, sourceProfileID, targetProfileID uuid.UUID, userID *uuid.UUID) (*SwitchResult, error) {
	if sourceProfileID == targetProfileID {
		return nil, ErrSameProfile
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: begin tx for switch profile: %w", err)
	}
	defer tx.Rollback(ctx)

	var srcState string
	var srcSimID uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT ep.profile_state, ep.sim_id FROM esim_profiles ep
		JOIN sims si ON ep.sim_id = si.id
		WHERE ep.id = $1 AND si.tenant_id = $2
		FOR UPDATE OF ep`,
		sourceProfileID, tenantID,
	).Scan(&srcState, &srcSimID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrESimProfileNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: lock source profile: %w", err)
	}

	if srcState != "enabled" {
		return nil, ErrInvalidProfileState
	}

	var tgtState string
	var tgtSimID uuid.UUID
	var tgtOperatorID uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT ep.profile_state, ep.sim_id, ep.operator_id FROM esim_profiles ep
		WHERE ep.id = $1
		FOR UPDATE`,
		targetProfileID,
	).Scan(&tgtState, &tgtSimID, &tgtOperatorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrESimProfileNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: lock target profile: %w", err)
	}

	if srcSimID != tgtSimID {
		return nil, ErrDifferentSIM
	}

	if tgtState != "disabled" {
		return nil, ErrInvalidProfileState
	}

	srcCols := strings.ReplaceAll(esimProfileColumns, "ep.", "")
	row := tx.QueryRow(ctx,
		`UPDATE esim_profiles SET profile_state = 'disabled', last_provisioned_at = NOW(), last_error = NULL, updated_at = NOW()
		WHERE id = $1
		RETURNING `+srcCols,
		sourceProfileID,
	)
	oldProfile, err := scanESimProfile(row)
	if err != nil {
		return nil, fmt.Errorf("store: disable source profile: %w", err)
	}

	row = tx.QueryRow(ctx,
		`UPDATE esim_profiles SET profile_state = 'enabled', last_provisioned_at = NOW(), last_error = NULL, updated_at = NOW()
		WHERE id = $1
		RETURNING `+srcCols,
		targetProfileID,
	)
	newProfile, err := scanESimProfile(row)
	if err != nil {
		return nil, fmt.Errorf("store: enable target profile: %w", err)
	}

	_, err = tx.Exec(ctx,
		`UPDATE sims SET operator_id = $1, esim_profile_id = $2, apn_id = NULL, updated_at = NOW()
		WHERE id = $3`,
		tgtOperatorID, targetProfileID, srcSimID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: update sim for switch: %w", err)
	}

	reason := fmt.Sprintf("esim profile switch: %s -> %s", sourceProfileID, targetProfileID)
	fromState := "enabled"
	if err := insertStateHistory(ctx, tx, srcSimID, &fromState, "active", "user", userID, &reason); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("store: commit switch profile: %w", err)
	}

	return &SwitchResult{
		SimID:         srcSimID,
		OldProfile:    oldProfile,
		NewProfile:    newProfile,
		NewOperatorID: tgtOperatorID,
	}, nil
}

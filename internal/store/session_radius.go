package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrRadiusSessionNotFound = errors.New("store: radius session not found")
)

type RadiusSession struct {
	ID               uuid.UUID  `json:"id"`
	SimID            uuid.UUID  `json:"sim_id"`
	TenantID         uuid.UUID  `json:"tenant_id"`
	OperatorID       uuid.UUID  `json:"operator_id"`
	APNID            *uuid.UUID `json:"apn_id"`
	NASIP            *string    `json:"nas_ip"`
	FramedIP         *string    `json:"framed_ip"`
	CallingStationID *string    `json:"calling_station_id"`
	CalledStationID  *string    `json:"called_station_id"`
	RATType          *string    `json:"rat_type"`
	SessionState     string     `json:"session_state"`
	AuthMethod       *string    `json:"auth_method"`
	PolicyVersionID  *uuid.UUID `json:"policy_version_id"`
	AcctSessionID    *string    `json:"acct_session_id"`
	StartedAt        time.Time  `json:"started_at"`
	EndedAt          *time.Time `json:"ended_at"`
	TerminateCause   *string    `json:"terminate_cause"`
	BytesIn          int64      `json:"bytes_in"`
	BytesOut         int64      `json:"bytes_out"`
	PacketsIn        int64      `json:"packets_in"`
	PacketsOut       int64      `json:"packets_out"`
	LastInterimAt    *time.Time `json:"last_interim_at"`
}

type CreateRadiusSessionParams struct {
	SimID            uuid.UUID
	TenantID         uuid.UUID
	OperatorID       uuid.UUID
	APNID            *uuid.UUID
	NASIP            *string
	FramedIP         *string
	CallingStationID *string
	CalledStationID  *string
	RATType          *string
	AuthMethod       *string
	PolicyVersionID  *uuid.UUID
	AcctSessionID    *string
}

var radiusSessionColumns = `id, sim_id, tenant_id, operator_id, apn_id, nas_ip, framed_ip,
	calling_station_id, called_station_id, rat_type, session_state, auth_method,
	policy_version_id, acct_session_id, started_at, ended_at, terminate_cause,
	bytes_in, bytes_out, packets_in, packets_out, last_interim_at`

func scanRadiusSession(row pgx.Row) (*RadiusSession, error) {
	var s RadiusSession
	err := row.Scan(
		&s.ID, &s.SimID, &s.TenantID, &s.OperatorID, &s.APNID,
		&s.NASIP, &s.FramedIP, &s.CallingStationID, &s.CalledStationID,
		&s.RATType, &s.SessionState, &s.AuthMethod,
		&s.PolicyVersionID, &s.AcctSessionID,
		&s.StartedAt, &s.EndedAt, &s.TerminateCause,
		&s.BytesIn, &s.BytesOut, &s.PacketsIn, &s.PacketsOut,
		&s.LastInterimAt,
	)
	return &s, err
}

type RadiusSessionStore struct {
	db *pgxpool.Pool
}

func NewRadiusSessionStore(db *pgxpool.Pool) *RadiusSessionStore {
	return &RadiusSessionStore{db: db}
}

func (s *RadiusSessionStore) Create(ctx context.Context, p CreateRadiusSessionParams) (*RadiusSession, error) {
	row := s.db.QueryRow(ctx, `
		INSERT INTO sessions (sim_id, tenant_id, operator_id, apn_id, nas_ip, framed_ip,
			calling_station_id, called_station_id, rat_type, auth_method,
			policy_version_id, acct_session_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING `+radiusSessionColumns,
		p.SimID, p.TenantID, p.OperatorID, p.APNID, p.NASIP, p.FramedIP,
		p.CallingStationID, p.CalledStationID, p.RATType, p.AuthMethod,
		p.PolicyVersionID, p.AcctSessionID,
	)

	sess, err := scanRadiusSession(row)
	if err != nil {
		return nil, fmt.Errorf("store: create radius session: %w", err)
	}
	return sess, nil
}

func (s *RadiusSessionStore) GetByID(ctx context.Context, id uuid.UUID) (*RadiusSession, error) {
	row := s.db.QueryRow(ctx,
		`SELECT `+radiusSessionColumns+` FROM sessions WHERE id = $1`,
		id,
	)
	sess, err := scanRadiusSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRadiusSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get radius session: %w", err)
	}
	return sess, nil
}

func (s *RadiusSessionStore) GetByAcctSessionID(ctx context.Context, acctSessionID string) (*RadiusSession, error) {
	row := s.db.QueryRow(ctx,
		`SELECT `+radiusSessionColumns+` FROM sessions WHERE acct_session_id = $1 AND session_state = 'active' ORDER BY started_at DESC LIMIT 1`,
		acctSessionID,
	)
	sess, err := scanRadiusSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRadiusSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get radius session by acct_session_id: %w", err)
	}
	return sess, nil
}

func (s *RadiusSessionStore) UpdateCounters(ctx context.Context, id uuid.UUID, bytesIn, bytesOut, packetsIn, packetsOut int64) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE sessions SET
			bytes_in = $2, bytes_out = $3, packets_in = $4, packets_out = $5,
			last_interim_at = NOW()
		WHERE id = $1 AND session_state = 'active'`,
		id, bytesIn, bytesOut, packetsIn, packetsOut,
	)
	if err != nil {
		return fmt.Errorf("store: update radius session counters: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrRadiusSessionNotFound
	}
	return nil
}

func (s *RadiusSessionStore) Finalize(ctx context.Context, id uuid.UUID, terminateCause string, bytesIn, bytesOut, packetsIn, packetsOut int64) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE sessions SET
			session_state = 'closed',
			ended_at = NOW(),
			terminate_cause = $2,
			bytes_in = $3, bytes_out = $4,
			packets_in = $5, packets_out = $6,
			last_interim_at = NOW()
		WHERE id = $1 AND session_state = 'active'`,
		id, terminateCause, bytesIn, bytesOut, packetsIn, packetsOut,
	)
	if err != nil {
		return fmt.Errorf("store: finalize radius session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrRadiusSessionNotFound
	}
	return nil
}

func (s *RadiusSessionStore) CountActive(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM sessions WHERE session_state = 'active'`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: count active radius sessions: %w", err)
	}
	return count, nil
}

func (s *RadiusSessionStore) ListActiveBySIM(ctx context.Context, simID uuid.UUID) ([]RadiusSession, error) {
	rows, err := s.db.Query(ctx,
		`SELECT `+radiusSessionColumns+` FROM sessions WHERE sim_id = $1 AND session_state = 'active' ORDER BY started_at DESC`,
		simID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list active radius sessions by sim: %w", err)
	}
	defer rows.Close()

	var results []RadiusSession
	for rows.Next() {
		var sess RadiusSession
		if err := rows.Scan(
			&sess.ID, &sess.SimID, &sess.TenantID, &sess.OperatorID, &sess.APNID,
			&sess.NASIP, &sess.FramedIP, &sess.CallingStationID, &sess.CalledStationID,
			&sess.RATType, &sess.SessionState, &sess.AuthMethod,
			&sess.PolicyVersionID, &sess.AcctSessionID,
			&sess.StartedAt, &sess.EndedAt, &sess.TerminateCause,
			&sess.BytesIn, &sess.BytesOut, &sess.PacketsIn, &sess.PacketsOut,
			&sess.LastInterimAt,
		); err != nil {
			return nil, fmt.Errorf("store: scan radius session: %w", err)
		}
		results = append(results, sess)
	}
	return results, nil
}

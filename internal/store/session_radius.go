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
	ErrRadiusSessionNotFound = errors.New("store: radius session not found")
)

type RadiusSession struct {
	ID               uuid.UUID       `json:"id"`
	SimID            uuid.UUID       `json:"sim_id"`
	TenantID         uuid.UUID       `json:"tenant_id"`
	OperatorID       uuid.UUID       `json:"operator_id"`
	APNID            *uuid.UUID      `json:"apn_id"`
	NASIP            *string         `json:"nas_ip"`
	FramedIP         *string         `json:"framed_ip"`
	CallingStationID *string         `json:"calling_station_id"`
	CalledStationID  *string         `json:"called_station_id"`
	RATType          *string         `json:"rat_type"`
	SessionState     string          `json:"session_state"`
	AuthMethod       *string         `json:"auth_method"`
	PolicyVersionID  *uuid.UUID      `json:"policy_version_id"`
	AcctSessionID    *string         `json:"acct_session_id"`
	StartedAt        time.Time       `json:"started_at"`
	EndedAt          *time.Time      `json:"ended_at"`
	TerminateCause   *string         `json:"terminate_cause"`
	BytesIn          int64           `json:"bytes_in"`
	BytesOut         int64           `json:"bytes_out"`
	PacketsIn        int64           `json:"packets_in"`
	PacketsOut       int64           `json:"packets_out"`
	LastInterimAt    *time.Time      `json:"last_interim_at"`
	ProtocolType     string          `json:"protocol_type"`
	SliceInfo        json.RawMessage `json:"slice_info,omitempty"`
	SoRDecision      json.RawMessage `json:"sor_decision,omitempty"`
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
	ProtocolType     string
	SliceInfo        json.RawMessage
	SoRDecision      json.RawMessage
}

var radiusSessionColumns = `id, sim_id, tenant_id, operator_id, apn_id, nas_ip::text, framed_ip::text,
	calling_station_id, called_station_id, rat_type, session_state, auth_method,
	policy_version_id, acct_session_id, started_at, ended_at, terminate_cause,
	bytes_in, bytes_out, packets_in, packets_out, last_interim_at,
	protocol_type, slice_info, sor_decision`

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
		&s.ProtocolType, &s.SliceInfo, &s.SoRDecision,
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
	protocolType := p.ProtocolType
	if protocolType == "" {
		protocolType = "radius"
	}

	row := s.db.QueryRow(ctx, `
		INSERT INTO sessions (sim_id, tenant_id, operator_id, apn_id, nas_ip, framed_ip,
			calling_station_id, called_station_id, rat_type, auth_method,
			policy_version_id, acct_session_id, protocol_type, slice_info, sor_decision)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING `+radiusSessionColumns,
		p.SimID, p.TenantID, p.OperatorID, p.APNID, p.NASIP, p.FramedIP,
		p.CallingStationID, p.CalledStationID, p.RATType, p.AuthMethod,
		p.PolicyVersionID, p.AcctSessionID, protocolType, p.SliceInfo, p.SoRDecision,
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

func (s *RadiusSessionStore) CountActiveByTenant(ctx context.Context, tenantID uuid.UUID) (int64, error) {
	var count int64
	err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM sessions WHERE session_state = 'active' AND tenant_id = $1`,
		tenantID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: count active radius sessions by tenant: %w", err)
	}
	return count, nil
}

func (s *RadiusSessionStore) ListActiveTenantCounts(ctx context.Context) (map[string]int64, error) {
	rows, err := s.db.Query(ctx,
		`SELECT tenant_id::text, COUNT(*) FROM sessions WHERE session_state = 'active' GROUP BY tenant_id`,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list active session counts by tenant: %w", err)
	}
	defer rows.Close()

	result := make(map[string]int64)
	for rows.Next() {
		var tenantID string
		var count int64
		if err := rows.Scan(&tenantID, &count); err != nil {
			return nil, fmt.Errorf("store: scan active session tenant count: %w", err)
		}
		result[tenantID] = count
	}
	return result, nil
}

func (s *RadiusSessionStore) CountInWindow(ctx context.Context, tenantID uuid.UUID, from, to time.Time) (int64, error) {
	var count int64
	err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM sessions WHERE tenant_id = $1 AND started_at >= $2 AND started_at < $3`,
		tenantID, from, to,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: count radius sessions in window: %w", err)
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
			&sess.ProtocolType, &sess.SliceInfo, &sess.SoRDecision,
		); err != nil {
			return nil, fmt.Errorf("store: scan radius session: %w", err)
		}
		results = append(results, sess)
	}
	return results, nil
}

type ListActiveSessionsParams struct {
	TenantID    *uuid.UUID
	Cursor      string
	Limit       int
	SimID       *uuid.UUID
	OperatorID  *uuid.UUID
	APNID       *uuid.UUID
	MinDuration *int
	MinUsage    *int64
}

type SessionStatsResult struct {
	TotalActive    int64
	ByOperator     map[string]int64
	ByAPN          map[string]int64
	AvgDurationSec float64
	AvgBytes       float64
}

func (s *RadiusSessionStore) ListActiveFiltered(ctx context.Context, p ListActiveSessionsParams) ([]RadiusSession, string, error) {
	limit := p.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{}
	conditions := []string{"session_state = 'active'"}
	argIdx := 1

	if p.TenantID != nil {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIdx))
		args = append(args, *p.TenantID)
		argIdx++
	}
	if p.SimID != nil {
		conditions = append(conditions, fmt.Sprintf("sim_id = $%d", argIdx))
		args = append(args, *p.SimID)
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
	if p.MinDuration != nil {
		conditions = append(conditions, fmt.Sprintf("EXTRACT(EPOCH FROM (NOW() - started_at)) >= $%d", argIdx))
		args = append(args, *p.MinDuration)
		argIdx++
	}
	if p.MinUsage != nil {
		conditions = append(conditions, fmt.Sprintf("(bytes_in + bytes_out) >= $%d", argIdx))
		args = append(args, *p.MinUsage)
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

	query := fmt.Sprintf(`SELECT %s FROM sessions %s ORDER BY started_at DESC, id DESC LIMIT %s`,
		radiusSessionColumns, where, limitPlaceholder)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list active sessions filtered: %w", err)
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
			&sess.ProtocolType, &sess.SliceInfo, &sess.SoRDecision,
		); err != nil {
			return nil, "", fmt.Errorf("store: scan active session: %w", err)
		}
		results = append(results, sess)
	}

	nextCursor := ""
	if len(results) > limit {
		nextCursor = results[limit-1].ID.String()
		results = results[:limit]
	}

	return results, nextCursor, nil
}

func (s *RadiusSessionStore) GetActiveStats(ctx context.Context, tenantID *uuid.UUID) (*SessionStatsResult, error) {
	stats := &SessionStatsResult{
		ByOperator: make(map[string]int64),
		ByAPN:      make(map[string]int64),
	}

	tenantFilter := ""
	var args []interface{}
	if tenantID != nil {
		tenantFilter = " AND tenant_id = $1"
		args = append(args, *tenantID)
	}

	err := s.db.QueryRow(ctx, `
		SELECT COUNT(*),
			COALESCE(AVG(EXTRACT(EPOCH FROM (NOW() - started_at))), 0),
			COALESCE(AVG(bytes_in + bytes_out), 0)
		FROM sessions WHERE session_state = 'active'`+tenantFilter,
		args...,
	).Scan(&stats.TotalActive, &stats.AvgDurationSec, &stats.AvgBytes)
	if err != nil {
		return nil, fmt.Errorf("store: get active session stats: %w", err)
	}

	opRows, err := s.db.Query(ctx, `
		SELECT operator_id::text, COUNT(*) FROM sessions
		WHERE session_state = 'active'`+tenantFilter+` GROUP BY operator_id`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("store: get session stats by operator: %w", err)
	}
	defer opRows.Close()

	for opRows.Next() {
		var opID string
		var count int64
		if err := opRows.Scan(&opID, &count); err != nil {
			return nil, fmt.Errorf("store: scan operator stats: %w", err)
		}
		stats.ByOperator[opID] = count
	}

	apnRows, err := s.db.Query(ctx, `
		SELECT COALESCE(apn_id::text, 'none'), COUNT(*) FROM sessions
		WHERE session_state = 'active'`+tenantFilter+` GROUP BY apn_id`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("store: get session stats by apn: %w", err)
	}
	defer apnRows.Close()

	for apnRows.Next() {
		var apnID string
		var count int64
		if err := apnRows.Scan(&apnID, &count); err != nil {
			return nil, fmt.Errorf("store: scan apn stats: %w", err)
		}
		stats.ByAPN[apnID] = count
	}

	return stats, nil
}

func (s *RadiusSessionStore) TrafficByOperator(ctx context.Context, tenantID *uuid.UUID) (map[uuid.UUID]int64, error) {
	tenantFilter := ""
	var args []interface{}
	if tenantID != nil {
		tenantFilter = " AND tenant_id = $1"
		args = append(args, *tenantID)
	}

	rows, err := s.db.Query(ctx, `
		SELECT operator_id, COALESCE(SUM(bytes_in + bytes_out), 0)
		FROM sessions
		WHERE session_state = 'active'`+tenantFilter+`
		GROUP BY operator_id`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("store: traffic by operator: %w", err)
	}
	defer rows.Close()

	result := make(map[uuid.UUID]int64)
	for rows.Next() {
		var opID uuid.UUID
		var traffic int64
		if err := rows.Scan(&opID, &traffic); err != nil {
			return nil, fmt.Errorf("store: scan operator traffic: %w", err)
		}
		result[opID] = traffic
	}
	return result, nil
}

func (s *RadiusSessionStore) CountActiveForSIM(ctx context.Context, simID uuid.UUID) (int64, error) {
	var count int64
	err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM sessions WHERE sim_id = $1 AND session_state = 'active'`,
		simID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: count active sessions for sim: %w", err)
	}
	return count, nil
}

func (s *RadiusSessionStore) GetOldestActiveForSIM(ctx context.Context, simID uuid.UUID) (*RadiusSession, error) {
	row := s.db.QueryRow(ctx,
		`SELECT `+radiusSessionColumns+` FROM sessions WHERE sim_id = $1 AND session_state = 'active' ORDER BY started_at ASC LIMIT 1`,
		simID,
	)
	sess, err := scanRadiusSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRadiusSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get oldest active session for sim: %w", err)
	}
	return sess, nil
}

type ListBySIMSessionParams struct {
	Cursor string
	Limit  int
	State  string
}

func (s *RadiusSessionStore) ListBySIM(ctx context.Context, tenantID, simID uuid.UUID, params ListBySIMSessionParams) ([]RadiusSession, string, error) {
	limit := params.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{}
	conditions := []string{}
	argIdx := 1

	conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIdx))
	args = append(args, tenantID)
	argIdx++

	conditions = append(conditions, fmt.Sprintf("sim_id = $%d", argIdx))
	args = append(args, simID)
	argIdx++

	if params.State == "active" || params.State == "closed" {
		conditions = append(conditions, fmt.Sprintf("session_state = $%d", argIdx))
		args = append(args, params.State)
		argIdx++
	}

	if params.Cursor != "" {
		cursorID, parseErr := uuid.Parse(params.Cursor)
		if parseErr == nil {
			conditions = append(conditions, fmt.Sprintf("id < $%d", argIdx))
			args = append(args, cursorID)
			argIdx++
		}
	}

	where := "WHERE " + strings.Join(conditions, " AND ")
	args = append(args, limit+1)
	limitPlaceholder := fmt.Sprintf("$%d", argIdx)

	query := fmt.Sprintf(`SELECT %s FROM sessions %s ORDER BY started_at DESC, id DESC LIMIT %s`,
		radiusSessionColumns, where, limitPlaceholder)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list sessions by sim: %w", err)
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
			&sess.ProtocolType, &sess.SliceInfo, &sess.SoRDecision,
		); err != nil {
			return nil, "", fmt.Errorf("store: scan session by sim: %w", err)
		}
		results = append(results, sess)
	}

	nextCursor := ""
	if len(results) > limit {
		nextCursor = results[limit-1].ID.String()
		results = results[:limit]
	}

	return results, nextCursor, nil
}

func (s *RadiusSessionStore) GetLastSessionBySIM(ctx context.Context, tenantID, simID uuid.UUID) (*RadiusSession, error) {
	row := s.db.QueryRow(ctx,
		`SELECT `+radiusSessionColumns+` FROM sessions WHERE sim_id = $1 AND tenant_id = $2 ORDER BY started_at DESC LIMIT 1`,
		simID, tenantID,
	)
	sess, err := scanRadiusSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: get last session by sim: %w", err)
	}
	return sess, nil
}

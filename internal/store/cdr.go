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
	ErrCDRNotFound = errors.New("store: cdr not found")
)

type CDR struct {
	ID            int64      `json:"id"`
	SessionID     uuid.UUID  `json:"session_id"`
	SimID         uuid.UUID  `json:"sim_id"`
	TenantID      uuid.UUID  `json:"tenant_id"`
	OperatorID    uuid.UUID  `json:"operator_id"`
	APNID         *uuid.UUID `json:"apn_id"`
	RATType       *string    `json:"rat_type"`
	RecordType    string     `json:"record_type"`
	BytesIn       int64      `json:"bytes_in"`
	BytesOut      int64      `json:"bytes_out"`
	DurationSec   int        `json:"duration_sec"`
	UsageCost     *float64   `json:"usage_cost"`
	CarrierCost   *float64   `json:"carrier_cost"`
	RatePerMB     *float64   `json:"rate_per_mb"`
	RATMultiplier *float64   `json:"rat_multiplier"`
	Timestamp     time.Time  `json:"timestamp"`
}

type CreateCDRParams struct {
	SessionID     uuid.UUID
	SimID         uuid.UUID
	TenantID      uuid.UUID
	OperatorID    uuid.UUID
	APNID         *uuid.UUID
	RATType       *string
	RecordType    string
	BytesIn       int64
	BytesOut      int64
	DurationSec   int
	UsageCost     *float64
	CarrierCost   *float64
	RatePerMB     *float64
	RATMultiplier *float64
	Timestamp     time.Time
}

type ListCDRParams struct {
	Cursor     string
	Limit      int
	SimID      *uuid.UUID
	OperatorID *uuid.UUID
	From       *time.Time
	To         *time.Time
	MinCost    *float64
}

type CostAggRow struct {
	OperatorID       uuid.UUID `json:"operator_id"`
	Bucket           time.Time `json:"bucket"`
	TotalUsageCost   float64   `json:"total_usage_cost"`
	TotalCarrierCost float64   `json:"total_carrier_cost"`
	TotalBytes       int64     `json:"total_bytes"`
	ActiveSims       int64     `json:"active_sims"`
}

var cdrColumns = `id, session_id, sim_id, tenant_id, operator_id, apn_id, rat_type,
	record_type, bytes_in, bytes_out, duration_sec, usage_cost, carrier_cost,
	rate_per_mb, rat_multiplier, timestamp`

func scanCDR(row pgx.Row) (*CDR, error) {
	var c CDR
	err := row.Scan(
		&c.ID, &c.SessionID, &c.SimID, &c.TenantID, &c.OperatorID,
		&c.APNID, &c.RATType, &c.RecordType,
		&c.BytesIn, &c.BytesOut, &c.DurationSec,
		&c.UsageCost, &c.CarrierCost, &c.RatePerMB, &c.RATMultiplier,
		&c.Timestamp,
	)
	return &c, err
}

type CDRStore struct {
	db *pgxpool.Pool
}

func NewCDRStore(db *pgxpool.Pool) *CDRStore {
	return &CDRStore{db: db}
}

func (s *CDRStore) Create(ctx context.Context, p CreateCDRParams) (*CDR, error) {
	ts := p.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	row := s.db.QueryRow(ctx, `
		INSERT INTO cdrs (session_id, sim_id, tenant_id, operator_id, apn_id, rat_type,
			record_type, bytes_in, bytes_out, duration_sec,
			usage_cost, carrier_cost, rate_per_mb, rat_multiplier, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING `+cdrColumns,
		p.SessionID, p.SimID, p.TenantID, p.OperatorID, p.APNID, p.RATType,
		p.RecordType, p.BytesIn, p.BytesOut, p.DurationSec,
		p.UsageCost, p.CarrierCost, p.RatePerMB, p.RATMultiplier, ts,
	)

	c, err := scanCDR(row)
	if err != nil {
		return nil, fmt.Errorf("store: create cdr: %w", err)
	}
	return c, nil
}

func (s *CDRStore) CreateIdempotent(ctx context.Context, p CreateCDRParams) (*CDR, error) {
	ts := p.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	row := s.db.QueryRow(ctx, `
		INSERT INTO cdrs (session_id, sim_id, tenant_id, operator_id, apn_id, rat_type,
			record_type, bytes_in, bytes_out, duration_sec,
			usage_cost, carrier_cost, rate_per_mb, rat_multiplier, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (session_id, timestamp, record_type) DO NOTHING
		RETURNING `+cdrColumns,
		p.SessionID, p.SimID, p.TenantID, p.OperatorID, p.APNID, p.RATType,
		p.RecordType, p.BytesIn, p.BytesOut, p.DurationSec,
		p.UsageCost, p.CarrierCost, p.RatePerMB, p.RATMultiplier, ts,
	)

	c, err := scanCDR(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: create cdr idempotent: %w", err)
	}
	return c, nil
}

func (s *CDRStore) ListByTenant(ctx context.Context, tenantID uuid.UUID, p ListCDRParams) ([]CDR, string, error) {
	limit := p.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{tenantID}
	conditions := []string{"tenant_id = $1"}
	argIdx := 2

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
	if p.From != nil {
		conditions = append(conditions, fmt.Sprintf("timestamp >= $%d", argIdx))
		args = append(args, *p.From)
		argIdx++
	}
	if p.To != nil {
		conditions = append(conditions, fmt.Sprintf("timestamp <= $%d", argIdx))
		args = append(args, *p.To)
		argIdx++
	}
	if p.MinCost != nil {
		conditions = append(conditions, fmt.Sprintf("usage_cost >= $%d", argIdx))
		args = append(args, *p.MinCost)
		argIdx++
	}
	if p.Cursor != "" {
		cursorID := 0
		if _, err := fmt.Sscanf(p.Cursor, "%d", &cursorID); err == nil && cursorID > 0 {
			conditions = append(conditions, fmt.Sprintf("id < $%d", argIdx))
			args = append(args, cursorID)
			argIdx++
		}
	}

	where := "WHERE " + strings.Join(conditions, " AND ")
	args = append(args, limit+1)
	limitPlaceholder := fmt.Sprintf("$%d", argIdx)

	query := fmt.Sprintf(`SELECT %s FROM cdrs %s ORDER BY timestamp DESC, id DESC LIMIT %s`,
		cdrColumns, where, limitPlaceholder)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list cdrs: %w", err)
	}
	defer rows.Close()

	var results []CDR
	for rows.Next() {
		var c CDR
		if err := rows.Scan(
			&c.ID, &c.SessionID, &c.SimID, &c.TenantID, &c.OperatorID,
			&c.APNID, &c.RATType, &c.RecordType,
			&c.BytesIn, &c.BytesOut, &c.DurationSec,
			&c.UsageCost, &c.CarrierCost, &c.RatePerMB, &c.RATMultiplier,
			&c.Timestamp,
		); err != nil {
			return nil, "", fmt.Errorf("store: scan cdr: %w", err)
		}
		results = append(results, c)
	}

	nextCursor := ""
	if len(results) > limit {
		nextCursor = fmt.Sprintf("%d", results[limit-1].ID)
		results = results[:limit]
	}

	return results, nextCursor, nil
}

func (s *CDRStore) GetCostAggregation(ctx context.Context, tenantID uuid.UUID, from, to time.Time, operatorID *uuid.UUID) ([]CostAggRow, error) {
	args := []interface{}{tenantID, from, to}
	opFilter := ""
	if operatorID != nil {
		opFilter = " AND operator_id = $4"
		args = append(args, *operatorID)
	}

	query := fmt.Sprintf(`
		SELECT operator_id, bucket, total_cost, total_carrier_cost, total_bytes, active_sims
		FROM cdrs_daily
		WHERE tenant_id = $1 AND bucket >= $2 AND bucket <= $3%s
		ORDER BY bucket DESC, operator_id
	`, opFilter)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: get cost aggregation: %w", err)
	}
	defer rows.Close()

	var results []CostAggRow
	for rows.Next() {
		var r CostAggRow
		if err := rows.Scan(&r.OperatorID, &r.Bucket, &r.TotalUsageCost, &r.TotalCarrierCost, &r.TotalBytes, &r.ActiveSims); err != nil {
			return nil, fmt.Errorf("store: scan cost agg: %w", err)
		}
		results = append(results, r)
	}
	return results, nil
}

func (s *CDRStore) CountForExport(ctx context.Context, tenantID uuid.UUID, from, to time.Time, operatorID *uuid.UUID) (int64, error) {
	args := []interface{}{tenantID, from, to}
	opFilter := ""
	if operatorID != nil {
		opFilter = " AND operator_id = $4"
		args = append(args, *operatorID)
	}

	var count int64
	err := s.db.QueryRow(ctx, fmt.Sprintf(`
		SELECT COUNT(*) FROM cdrs
		WHERE tenant_id = $1 AND timestamp >= $2 AND timestamp <= $3%s
	`, opFilter), args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: count cdrs for export: %w", err)
	}
	return count, nil
}

type CDRExportCallback func(c CDR) error

func (s *CDRStore) StreamForExport(ctx context.Context, tenantID uuid.UUID, from, to time.Time, operatorID *uuid.UUID, callback CDRExportCallback) error {
	args := []interface{}{tenantID, from, to}
	opFilter := ""
	if operatorID != nil {
		opFilter = " AND operator_id = $4"
		args = append(args, *operatorID)
	}

	query := fmt.Sprintf(`
		SELECT %s FROM cdrs
		WHERE tenant_id = $1 AND timestamp >= $2 AND timestamp <= $3%s
		ORDER BY timestamp ASC, id ASC
	`, cdrColumns, opFilter)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("store: stream cdrs for export: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var c CDR
		if err := rows.Scan(
			&c.ID, &c.SessionID, &c.SimID, &c.TenantID, &c.OperatorID,
			&c.APNID, &c.RATType, &c.RecordType,
			&c.BytesIn, &c.BytesOut, &c.DurationSec,
			&c.UsageCost, &c.CarrierCost, &c.RatePerMB, &c.RATMultiplier,
			&c.Timestamp,
		); err != nil {
			return fmt.Errorf("store: scan cdr export row: %w", err)
		}
		if err := callback(c); err != nil {
			return fmt.Errorf("store: cdr export callback: %w", err)
		}
	}
	return nil
}

func (s *CDRStore) GetCumulativeSessionBytes(ctx context.Context, sessionID uuid.UUID) (int64, error) {
	var total int64
	err := s.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(bytes_in + bytes_out), 0)
		FROM cdrs
		WHERE session_id = $1
	`, sessionID).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("store: get cumulative session bytes: %w", err)
	}
	return total, nil
}

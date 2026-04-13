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

type UsageBucket struct {
	Bucket  time.Time `json:"bucket"`
	BytesIn int64     `json:"bytes_in"`
	BytesOut int64    `json:"bytes_out"`
	Cost    float64   `json:"cost"`
}

type TopSession struct {
	SessionID  uuid.UUID `json:"session_id"`
	StartedAt  time.Time `json:"started_at"`
	BytesTotal int64     `json:"bytes_total"`
	DurationSec int     `json:"duration_sec"`
}

type SIMUsageResult struct {
	SimID         uuid.UUID      `json:"sim_id"`
	Period        string         `json:"period"`
	TotalBytesIn  int64          `json:"total_bytes_in"`
	TotalBytesOut int64          `json:"total_bytes_out"`
	TotalCost     float64        `json:"total_cost"`
	Series        []UsageBucket  `json:"series"`
	TopSessions   []TopSession   `json:"top_sessions"`
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

func (s *CDRStore) GetSIMUsage(ctx context.Context, tenantID, simID uuid.UUID, period string) (*SIMUsageResult, error) {
	var truncFunc string
	var since time.Time
	now := time.Now().UTC()

	switch period {
	case "24h":
		truncFunc = "hour"
		since = now.Add(-24 * time.Hour)
	case "7d":
		truncFunc = "day"
		since = now.AddDate(0, 0, -7)
	default:
		period = "30d"
		truncFunc = "day"
		since = now.AddDate(0, 0, -30)
	}

	seriesQuery := fmt.Sprintf(`
		SELECT date_trunc('%s', timestamp) AS bucket,
			COALESCE(SUM(bytes_in), 0),
			COALESCE(SUM(bytes_out), 0),
			COALESCE(SUM(usage_cost), 0)
		FROM cdrs
		WHERE sim_id = $1 AND tenant_id = $2 AND timestamp >= $3
		GROUP BY bucket
		ORDER BY bucket ASC
	`, truncFunc)

	rows, err := s.db.Query(ctx, seriesQuery, simID, tenantID, since)
	if err != nil {
		return nil, fmt.Errorf("store: get sim usage series: %w", err)
	}
	defer rows.Close()

	var totalIn, totalOut int64
	var totalCost float64
	var series []UsageBucket

	for rows.Next() {
		var b UsageBucket
		if err := rows.Scan(&b.Bucket, &b.BytesIn, &b.BytesOut, &b.Cost); err != nil {
			return nil, fmt.Errorf("store: scan sim usage bucket: %w", err)
		}
		totalIn += b.BytesIn
		totalOut += b.BytesOut
		totalCost += b.Cost
		series = append(series, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: sim usage series rows: %w", err)
	}

	topQuery := `
		SELECT session_id,
			MIN(timestamp) AS started_at,
			COALESCE(SUM(bytes_in + bytes_out), 0) AS bytes_total,
			COALESCE(MAX(duration_sec), 0) AS duration_sec
		FROM cdrs
		WHERE sim_id = $1 AND tenant_id = $2 AND timestamp >= $3
		GROUP BY session_id
		ORDER BY bytes_total DESC
		LIMIT 5
	`

	topRows, err := s.db.Query(ctx, topQuery, simID, tenantID, since)
	if err != nil {
		return nil, fmt.Errorf("store: get sim top sessions: %w", err)
	}
	defer topRows.Close()

	var topSessions []TopSession
	for topRows.Next() {
		var t TopSession
		if err := topRows.Scan(&t.SessionID, &t.StartedAt, &t.BytesTotal, &t.DurationSec); err != nil {
			return nil, fmt.Errorf("store: scan top session: %w", err)
		}
		topSessions = append(topSessions, t)
	}
	if err := topRows.Err(); err != nil {
		return nil, fmt.Errorf("store: top sessions rows: %w", err)
	}

	if series == nil {
		series = []UsageBucket{}
	}
	if topSessions == nil {
		topSessions = []TopSession{}
	}

	return &SIMUsageResult{
		SimID:         simID,
		Period:        period,
		TotalBytesIn:  totalIn,
		TotalBytesOut: totalOut,
		TotalCost:     totalCost,
		Series:        series,
		TopSessions:   topSessions,
	}, nil
}

func (s *CDRStore) GetMonthlyCostForTenant(ctx context.Context, tenantID uuid.UUID) (float64, error) {
	var total float64
	err := s.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(total_usage_cost), 0)
		FROM cdrs_monthly
		WHERE tenant_id = $1
		  AND bucket >= date_trunc('month', NOW())
	`, tenantID).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("store: get monthly cost: %w", err)
	}
	return total, nil
}

type OperatorMetricBucket struct {
	Ts             time.Time `json:"ts"`
	AuthRatePerSec float64   `json:"auth_rate_per_sec"`
	ErrorRatePerSec float64  `json:"error_rate_per_sec"`
}

type APNTrafficBucket struct {
	Ts        time.Time `json:"ts"`
	BytesIn   int64     `json:"bytes_in"`
	BytesOut  int64     `json:"bytes_out"`
	AuthCount int64     `json:"auth_count"`
}

func (s *CDRStore) GetOperatorMetrics(ctx context.Context, tenantID, operatorID uuid.UUID, window string) ([]OperatorMetricBucket, error) {
	var truncInterval string
	var bucketSec float64
	var since time.Time
	now := time.Now().UTC()

	switch window {
	case "15m":
		truncInterval = "1 minute"
		bucketSec = 60
		since = now.Add(-15 * time.Minute)
	case "6h":
		truncInterval = "30 minutes"
		bucketSec = 1800
		since = now.Add(-6 * time.Hour)
	case "24h":
		truncInterval = "1 hour"
		bucketSec = 3600
		since = now.Add(-24 * time.Hour)
	default:
		truncInterval = "5 minutes"
		bucketSec = 300
		since = now.Add(-1 * time.Hour)
	}

	rows, err := s.db.Query(ctx, fmt.Sprintf(`
		SELECT
			date_trunc('%s', timestamp) AS bucket,
			COUNT(*)::float8 / $4 AS auth_rate,
			COUNT(*) FILTER (WHERE record_type IN ('auth_fail', 'reject'))::float8 / $4 AS error_rate
		FROM cdrs
		WHERE tenant_id = $1
		  AND operator_id = $2
		  AND timestamp >= $3
		GROUP BY bucket
		ORDER BY bucket ASC
	`, truncInterval), tenantID, operatorID, since, bucketSec)
	if err != nil {
		return nil, fmt.Errorf("store: get operator metrics: %w", err)
	}
	defer rows.Close()

	var results []OperatorMetricBucket
	for rows.Next() {
		var b OperatorMetricBucket
		if err := rows.Scan(&b.Ts, &b.AuthRatePerSec, &b.ErrorRatePerSec); err != nil {
			return nil, fmt.Errorf("store: scan operator metric bucket: %w", err)
		}
		results = append(results, b)
	}
	return results, nil
}

func (s *CDRStore) GetAPNTraffic(ctx context.Context, tenantID, apnID uuid.UUID, period string) ([]APNTrafficBucket, error) {
	var since time.Time
	now := time.Now().UTC()

	switch period {
	case "15m":
		since = now.Add(-15 * time.Minute)
	case "1h":
		since = now.Add(-1 * time.Hour)
	case "6h":
		since = now.Add(-6 * time.Hour)
	case "7d":
		since = now.AddDate(0, 0, -7)
	case "30d":
		since = now.AddDate(0, 0, -30)
	default:
		since = now.Add(-24 * time.Hour)
	}

	useHourly := period == "15m" || period == "1h" || period == "6h" || period == "24h" || period == ""

	if useHourly {
		r, err := s.db.Query(ctx, `
			SELECT bucket, COALESCE(SUM(total_bytes_in),0), COALESCE(SUM(total_bytes_out),0), COALESCE(SUM(record_count),0)
			FROM cdrs_hourly
			WHERE tenant_id = $1 AND apn_id = $2 AND bucket >= $3
			GROUP BY bucket
			ORDER BY bucket ASC
		`, tenantID, apnID, since)
		if err != nil {
			return nil, fmt.Errorf("store: get apn traffic (hourly): %w", err)
		}
		defer r.Close()

		var results []APNTrafficBucket
		for r.Next() {
			var b APNTrafficBucket
			if err := r.Scan(&b.Ts, &b.BytesIn, &b.BytesOut, &b.AuthCount); err != nil {
				return nil, fmt.Errorf("store: scan apn traffic bucket: %w", err)
			}
			results = append(results, b)
		}
		return results, nil
	}

	r, err := s.db.Query(ctx, `
		SELECT date_trunc('day', timestamp) AS bucket,
			COALESCE(SUM(bytes_in),0), COALESCE(SUM(bytes_out),0), COUNT(*)
		FROM cdrs
		WHERE tenant_id = $1 AND apn_id = $2 AND timestamp >= $3
		GROUP BY bucket
		ORDER BY bucket ASC
	`, tenantID, apnID, since)
	if err != nil {
		return nil, fmt.Errorf("store: get apn traffic (daily): %w", err)
	}
	defer r.Close()

	var results []APNTrafficBucket
	for r.Next() {
		var b APNTrafficBucket
		if err := r.Scan(&b.Ts, &b.BytesIn, &b.BytesOut, &b.AuthCount); err != nil {
			return nil, fmt.Errorf("store: scan apn traffic daily bucket: %w", err)
		}
		results = append(results, b)
	}
	return results, nil
}

func (s *CDRStore) GetTrafficHeatmap7x24(ctx context.Context, tenantID uuid.UUID) ([][]float64, error) {
	rows, err := s.db.Query(ctx, `
		SELECT
			EXTRACT(DOW FROM bucket)::int AS dow,
			EXTRACT(HOUR FROM bucket)::int AS hour,
			COALESCE(SUM(total_bytes_in + total_bytes_out), 0) AS total_bytes
		FROM cdrs_hourly
		WHERE tenant_id = $1
		  AND bucket >= NOW() - INTERVAL '7 days'
		GROUP BY dow, hour
		ORDER BY dow, hour
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("store: get traffic heatmap: %w", err)
	}
	defer rows.Close()

	matrix := make([][]float64, 7)
	for i := range matrix {
		matrix[i] = make([]float64, 24)
	}
	var maxVal float64
	type cell struct{ dow, hour int; val float64 }
	var cells []cell

	for rows.Next() {
		var dow, hour int
		var total float64
		if err := rows.Scan(&dow, &hour, &total); err != nil {
			return nil, fmt.Errorf("store: scan heatmap row: %w", err)
		}
		cells = append(cells, cell{dow, hour, total})
		if total > maxVal {
			maxVal = total
		}
	}

	for _, c := range cells {
		if maxVal > 0 {
			matrix[c.dow][c.hour] = c.val / maxVal
		}
	}
	return matrix, nil
}

func (s *CDRStore) SumBytesByAPN24h(ctx context.Context, tenantID uuid.UUID) (map[uuid.UUID]int64, error) {
	rows, err := s.db.Query(ctx, `
		SELECT apn_id, COALESCE(SUM(total_bytes_in + total_bytes_out), 0)
		FROM cdrs_hourly
		WHERE tenant_id = $1 AND apn_id IS NOT NULL AND bucket >= NOW() - INTERVAL '24 hours'
		GROUP BY apn_id
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("store: sum bytes by apn 24h: %w", err)
	}
	defer rows.Close()

	result := make(map[uuid.UUID]int64)
	for rows.Next() {
		var apnID uuid.UUID
		var total int64
		if err := rows.Scan(&apnID, &total); err != nil {
			return nil, fmt.Errorf("store: scan apn bytes row: %w", err)
		}
		result[apnID] = total
	}
	return result, nil
}

func (s *CDRStore) GetDailyKPISparklines(ctx context.Context, tenantID uuid.UUID, days int) (map[string][]float64, error) {
	if days <= 0 {
		days = 7
	}

	costRows, err := s.db.Query(ctx, `
		SELECT bucket, COALESCE(SUM(total_cost), 0), COALESCE(SUM(active_sims), 0)
		FROM cdrs_daily
		WHERE tenant_id = $1
		  AND bucket >= NOW() - ($2::int * INTERVAL '1 day')
		GROUP BY bucket
		ORDER BY bucket ASC
	`, tenantID, days)
	if err != nil {
		return nil, fmt.Errorf("store: get daily kpi sparklines (cdrs_daily): %w", err)
	}
	defer costRows.Close()

	type dailyRow struct {
		bucket     time.Time
		cost       float64
		activeSims float64
	}
	var daily []dailyRow
	for costRows.Next() {
		var r dailyRow
		if err := costRows.Scan(&r.bucket, &r.cost, &r.activeSims); err != nil {
			return nil, fmt.Errorf("store: scan daily sparkline row: %w", err)
		}
		daily = append(daily, r)
	}

	costSeries := make([]float64, len(daily))
	simSeries := make([]float64, len(daily))
	for i, r := range daily {
		costSeries[i] = r.cost
		simSeries[i] = r.activeSims
	}

	result := map[string][]float64{
		"monthly_cost": costSeries,
		"total_sims":   simSeries,
	}
	return result, nil
}

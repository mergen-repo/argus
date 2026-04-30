package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UsageAnalyticsStore struct {
	db *pgxpool.Pool
}

func NewUsageAnalyticsStore(db *pgxpool.Pool) *UsageAnalyticsStore {
	return &UsageAnalyticsStore{db: db}
}

type UsageTimePoint struct {
	Timestamp  time.Time `json:"ts"`
	TotalBytes int64     `json:"total_bytes"`
	BytesIn    int64     `json:"bytes_in"`
	BytesOut   int64     `json:"bytes_out"`
	Sessions   int64     `json:"sessions"`
	Auths      int64     `json:"auths"`
	UniqueSims int64     `json:"unique_sims"`
	GroupKey   string    `json:"group_key,omitempty"`
}

type UsageTotals struct {
	TotalBytes    int64 `json:"total_bytes"`
	TotalSessions int64 `json:"total_sessions"`
	TotalAuths    int64 `json:"total_auths"`
	UniqueSims    int64 `json:"unique_sims"`
}

type UsageBreakdownItem struct {
	Key        string `json:"key"`
	TotalBytes int64  `json:"total_bytes"`
	Sessions   int64  `json:"sessions"`
	Auths      int64  `json:"auths"`
	Percentage float64 `json:"percentage"`
}

type TopConsumer struct {
	SimID          uuid.UUID  `json:"sim_id"`
	TotalBytes     int64      `json:"total_bytes"`
	Sessions       int64      `json:"sessions"`
	BytesIn        int64      `json:"bytes_in"`
	BytesOut       int64      `json:"bytes_out"`
	AvgDurationSec *float64   `json:"avg_duration_sec,omitempty"`
	ICCID          string     `json:"iccid"`
	IMSI           string     `json:"imsi"`
	MSISDN         *string    `json:"msisdn,omitempty"`
	OperatorID     *uuid.UUID `json:"operator_id,omitempty"`
	APNID          *uuid.UUID `json:"apn_id,omitempty"`
}

type UsageQueryParams struct {
	TenantID   uuid.UUID
	Period     string
	From       time.Time
	To         time.Time
	GroupBy    string
	OperatorID *uuid.UUID
	APNID      *uuid.UUID
	RATType    *string
}

type PeriodSpec struct {
	BucketInterval string
	AggregateView  string
}

func ResolvePeriod(period string, from, to time.Time) PeriodSpec {
	if period == "custom" {
		duration := to.Sub(from)
		switch {
		case duration <= 2*time.Hour:
			return PeriodSpec{BucketInterval: "1 minute", AggregateView: "cdrs"}
		case duration <= 25*time.Hour:
			return PeriodSpec{BucketInterval: "15 minutes", AggregateView: "cdrs_hourly"}
		case duration <= 8*24*time.Hour:
			return PeriodSpec{BucketInterval: "1 hour", AggregateView: "cdrs_hourly"}
		case duration <= 31*24*time.Hour:
			return PeriodSpec{BucketInterval: "6 hours", AggregateView: "cdrs_daily"}
		default:
			return PeriodSpec{BucketInterval: "1 day", AggregateView: "cdrs_daily"}
		}
	}

	switch period {
	case "1h":
		return PeriodSpec{BucketInterval: "1 minute", AggregateView: "cdrs"}
	case "24h":
		return PeriodSpec{BucketInterval: "15 minutes", AggregateView: "cdrs_hourly"}
	case "7d":
		return PeriodSpec{BucketInterval: "1 hour", AggregateView: "cdrs_hourly"}
	case "30d":
		return PeriodSpec{BucketInterval: "6 hours", AggregateView: "cdrs_daily"}
	default:
		return PeriodSpec{BucketInterval: "1 hour", AggregateView: "cdrs_hourly"}
	}
}

func ResolveTimeRange(period string) (time.Time, time.Time) {
	now := time.Now().UTC()
	switch period {
	case "1h":
		return now.Add(-1 * time.Hour), now
	case "24h":
		return now.Add(-24 * time.Hour), now
	case "7d":
		return now.Add(-7 * 24 * time.Hour), now
	case "30d":
		return now.Add(-30 * 24 * time.Hour), now
	default:
		return now.Add(-24 * time.Hour), now
	}
}

// buildTimeSeriesQuery assembles the SQL + args for GetTimeSeries. Extracted so the dynamic
// query string (including COALESCE sentinel for nullable group_by columns) can be asserted in
// pure Go tests without a live database. See FIX-204.
func buildTimeSeriesQuery(p UsageQueryParams) (string, []interface{}) {
	spec := ResolvePeriod(p.Period, p.From, p.To)

	var selectCols, groupCols, orderCols string
	var fromClause string

	if spec.AggregateView == "cdrs" {
		fromClause = "cdrs"
		selectCols = fmt.Sprintf(`time_bucket('%s', timestamp) AS bucket,
			SUM(bytes_in + bytes_out) AS total_bytes,
			SUM(bytes_in) AS bytes_in,
			SUM(bytes_out) AS bytes_out,
			COUNT(*) AS sessions,
			COUNT(*) FILTER (WHERE record_type = 'start') AS auths,
			COUNT(DISTINCT sim_id) AS unique_sims`, spec.BucketInterval)
		groupCols = "bucket"
		orderCols = "bucket"
	} else if spec.AggregateView == "cdrs_hourly" {
		// cdrs_hourly has total_bytes_in/total_bytes_out but no sim_id dimension,
		// so unique_sims cannot be computed per bucket — returned as 0 (UI hides the row when 0).
		fromClause = "cdrs_hourly"
		selectCols = fmt.Sprintf(`time_bucket('%s', bucket) AS ts,
			SUM(total_bytes_in + total_bytes_out) AS total_bytes,
			SUM(total_bytes_in) AS bytes_in,
			SUM(total_bytes_out) AS bytes_out,
			SUM(record_count) AS sessions,
			SUM(record_count) AS auths,
			0::bigint AS unique_sims`, spec.BucketInterval)
		groupCols = "ts"
		orderCols = "ts"
	} else {
		// cdrs_daily has active_sims but only total_bytes (combined); bytes_in/bytes_out
		// are not available at daily granularity — returned as 0.
		fromClause = "cdrs_daily"
		selectCols = fmt.Sprintf(`time_bucket('%s', bucket) AS ts,
			SUM(total_bytes) AS total_bytes,
			0::bigint AS bytes_in,
			0::bigint AS bytes_out,
			0::bigint AS sessions,
			0::bigint AS auths,
			SUM(active_sims) AS unique_sims`, spec.BucketInterval)
		groupCols = "ts"
		orderCols = "ts"
	}

	if p.GroupBy != "" {
		col := groupByColumn(p.GroupBy)
		selectCols += fmt.Sprintf(`, COALESCE(%s::text, '__unassigned__') AS group_key`, col)
		groupCols += fmt.Sprintf(`, %s`, col)
	}

	args := []interface{}{p.TenantID, p.From, p.To}
	conditions := []string{"tenant_id = $1", bucketOrTimestamp(spec.AggregateView) + " >= $2", bucketOrTimestamp(spec.AggregateView) + " < $3"}
	argIdx := 4

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
	if p.RATType != nil {
		conditions = append(conditions, fmt.Sprintf("rat_type = $%d", argIdx))
		args = append(args, *p.RATType)
		argIdx++
	}

	query := fmt.Sprintf(`SELECT %s FROM %s WHERE %s GROUP BY %s ORDER BY %s`,
		selectCols, fromClause,
		strings.Join(conditions, " AND "),
		groupCols, orderCols)

	return query, args
}

func (s *UsageAnalyticsStore) GetTimeSeries(ctx context.Context, p UsageQueryParams) ([]UsageTimePoint, error) {
	query, args := buildTimeSeriesQuery(p)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: usage time series: %w", err)
	}
	defer rows.Close()

	var results []UsageTimePoint
	for rows.Next() {
		var tp UsageTimePoint
		if p.GroupBy != "" {
			if err := rows.Scan(&tp.Timestamp, &tp.TotalBytes, &tp.BytesIn, &tp.BytesOut, &tp.Sessions, &tp.Auths, &tp.UniqueSims, &tp.GroupKey); err != nil {
				return nil, fmt.Errorf("store: scan usage time point: %w", err)
			}
		} else {
			if err := rows.Scan(&tp.Timestamp, &tp.TotalBytes, &tp.BytesIn, &tp.BytesOut, &tp.Sessions, &tp.Auths, &tp.UniqueSims); err != nil {
				return nil, fmt.Errorf("store: scan usage time point: %w", err)
			}
		}
		results = append(results, tp)
	}

	return results, nil
}

func (s *UsageAnalyticsStore) GetTotals(ctx context.Context, p UsageQueryParams) (*UsageTotals, error) {
	args := []interface{}{p.TenantID, p.From, p.To}
	conditions := []string{"tenant_id = $1", "timestamp >= $2", "timestamp < $3"}
	argIdx := 4

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
	if p.RATType != nil {
		conditions = append(conditions, fmt.Sprintf("rat_type = $%d", argIdx))
		args = append(args, *p.RATType)
		argIdx++
	}

	query := fmt.Sprintf(`SELECT
		COALESCE(SUM(bytes_in + bytes_out), 0),
		COUNT(*),
		COUNT(*) FILTER (WHERE record_type = 'start'),
		COUNT(DISTINCT sim_id)
		FROM cdrs WHERE %s`,
		strings.Join(conditions, " AND "))

	var totals UsageTotals
	err := s.db.QueryRow(ctx, query, args...).Scan(
		&totals.TotalBytes, &totals.TotalSessions, &totals.TotalAuths, &totals.UniqueSims)
	if err != nil {
		return nil, fmt.Errorf("store: usage totals: %w", err)
	}
	return &totals, nil
}

func (s *UsageAnalyticsStore) GetBreakdowns(ctx context.Context, p UsageQueryParams, dimension string) ([]UsageBreakdownItem, error) {
	col := sanitizeDimension(dimension)
	if col == "" {
		return nil, nil
	}

	args := []interface{}{p.TenantID, p.From, p.To}
	conditions := []string{"tenant_id = $1", "timestamp >= $2", "timestamp < $3"}
	argIdx := 4

	if p.OperatorID != nil {
		conditions = append(conditions, fmt.Sprintf("operator_id = $%d", argIdx))
		args = append(args, *p.OperatorID)
		argIdx++
	}

	query := fmt.Sprintf(`SELECT
		COALESCE(%s::text, '__unassigned__') AS key,
		COALESCE(SUM(bytes_in + bytes_out), 0) AS total_bytes,
		COUNT(*) AS sessions,
		COUNT(*) FILTER (WHERE record_type = 'start') AS auths
		FROM cdrs WHERE %s
		GROUP BY %s
		ORDER BY total_bytes DESC
		LIMIT 50`,
		col, strings.Join(conditions, " AND "), col)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: usage breakdowns: %w", err)
	}
	defer rows.Close()

	var items []UsageBreakdownItem
	var totalAllBytes int64
	for rows.Next() {
		var item UsageBreakdownItem
		if err := rows.Scan(&item.Key, &item.TotalBytes, &item.Sessions, &item.Auths); err != nil {
			return nil, fmt.Errorf("store: scan breakdown: %w", err)
		}
		totalAllBytes += item.TotalBytes
		items = append(items, item)
	}

	for i := range items {
		if totalAllBytes > 0 {
			items[i].Percentage = float64(items[i].TotalBytes) / float64(totalAllBytes) * 100.0
		}
	}

	return items, nil
}

func (s *UsageAnalyticsStore) GetTopConsumers(ctx context.Context, p UsageQueryParams, limit int) ([]TopConsumer, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	args := []interface{}{p.TenantID, p.From, p.To}
	conditions := []string{"c.tenant_id = $1", "c.timestamp >= $2", "c.timestamp < $3"}
	argIdx := 4

	if p.OperatorID != nil {
		conditions = append(conditions, fmt.Sprintf("c.operator_id = $%d", argIdx))
		args = append(args, *p.OperatorID)
		argIdx++
	}
	if p.APNID != nil {
		conditions = append(conditions, fmt.Sprintf("c.apn_id = $%d", argIdx))
		args = append(args, *p.APNID)
		argIdx++
	}
	if p.RATType != nil {
		conditions = append(conditions, fmt.Sprintf("c.rat_type = $%d", argIdx))
		args = append(args, *p.RATType)
		argIdx++
	}

	args = append(args, limit)
	limitPlaceholder := fmt.Sprintf("$%d", argIdx)

	query := fmt.Sprintf(`SELECT
		c.sim_id,
		SUM(c.bytes_in + c.bytes_out) AS total_bytes,
		COUNT(DISTINCT c.session_id) AS sessions,
		SUM(c.bytes_in) AS bytes_in,
		SUM(c.bytes_out) AS bytes_out,
		AVG(c.duration_sec) AS avg_duration_sec,
		s.iccid,
		s.imsi,
		s.msisdn,
		s.operator_id,
		s.apn_id
		FROM cdrs c
		JOIN sims s ON s.id = c.sim_id
		WHERE %s
		GROUP BY c.sim_id, s.iccid, s.imsi, s.msisdn, s.operator_id, s.apn_id
		ORDER BY total_bytes DESC
		LIMIT %s`,
		strings.Join(conditions, " AND "), limitPlaceholder)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: top consumers: %w", err)
	}
	defer rows.Close()

	var consumers []TopConsumer
	for rows.Next() {
		var c TopConsumer
		if err := rows.Scan(&c.SimID, &c.TotalBytes, &c.Sessions,
			&c.BytesIn, &c.BytesOut, &c.AvgDurationSec,
			&c.ICCID, &c.IMSI, &c.MSISDN, &c.OperatorID, &c.APNID); err != nil {
			return nil, fmt.Errorf("store: scan top consumer: %w", err)
		}
		consumers = append(consumers, c)
	}

	return consumers, nil
}

func bucketOrTimestamp(view string) string {
	if view == "cdrs" {
		return "timestamp"
	}
	return "bucket"
}

func groupByColumn(gb string) string {
	switch gb {
	case "operator":
		return "operator_id"
	case "apn":
		return "apn_id"
	case "rat_type":
		return "rat_type"
	default:
		return gb
	}
}

func sanitizeDimension(dim string) string {
	switch dim {
	case "operator", "operator_id":
		return "operator_id"
	case "apn", "apn_id":
		return "apn_id"
	case "rat_type":
		return "rat_type"
	default:
		return ""
	}
}

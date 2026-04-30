package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrSLAReportNotFound = errors.New("store: sla report not found")

type SLAReportRow struct {
	ID            uuid.UUID       `json:"id"`
	TenantID      uuid.UUID       `json:"tenant_id"`
	OperatorID    *uuid.UUID      `json:"operator_id,omitempty"`
	WindowStart   time.Time       `json:"window_start"`
	WindowEnd     time.Time       `json:"window_end"`
	UptimePct     float64         `json:"uptime_pct"`
	LatencyP95Ms  int             `json:"latency_p95_ms"`
	IncidentCount int             `json:"incident_count"`
	MTTRSec       int             `json:"mttr_sec"`
	ErrorCount    int             `json:"error_count"`
	SessionsTotal int64           `json:"sessions_total"`
	Details       json.RawMessage `json:"details"`
	GeneratedAt   time.Time       `json:"generated_at"`
}

type SLAReportStore struct {
	db *pgxpool.Pool
}

func NewSLAReportStore(db *pgxpool.Pool) *SLAReportStore {
	return &SLAReportStore{db: db}
}

func (s *SLAReportStore) Create(ctx context.Context, row *SLAReportRow) (*SLAReportRow, error) {
	details := row.Details
	if len(details) == 0 {
		details = json.RawMessage(`{}`)
	}

	var r SLAReportRow
	err := s.db.QueryRow(ctx, `
		INSERT INTO sla_reports (tenant_id, operator_id, window_start, window_end,
			uptime_pct, latency_p95_ms, incident_count, mttr_sec, sessions_total, error_count, details)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, tenant_id, operator_id, window_start, window_end,
			uptime_pct, latency_p95_ms, incident_count, mttr_sec, sessions_total, error_count, details, generated_at
	`,
		row.TenantID, row.OperatorID, row.WindowStart, row.WindowEnd,
		row.UptimePct, row.LatencyP95Ms, row.IncidentCount, row.MTTRSec,
		row.SessionsTotal, row.ErrorCount, details,
	).Scan(
		&r.ID, &r.TenantID, &r.OperatorID, &r.WindowStart, &r.WindowEnd,
		&r.UptimePct, &r.LatencyP95Ms, &r.IncidentCount, &r.MTTRSec,
		&r.SessionsTotal, &r.ErrorCount, &r.Details, &r.GeneratedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("store: create sla report: %w", err)
	}
	return &r, nil
}

func (s *SLAReportStore) GetByID(ctx context.Context, tenantID, id uuid.UUID) (*SLAReportRow, error) {
	var r SLAReportRow
	err := s.db.QueryRow(ctx, `
		SELECT id, tenant_id, operator_id, window_start, window_end,
			uptime_pct, latency_p95_ms, incident_count, mttr_sec, sessions_total, error_count, details, generated_at
		FROM sla_reports
		WHERE id = $1 AND tenant_id = $2
	`, id, tenantID).Scan(
		&r.ID, &r.TenantID, &r.OperatorID, &r.WindowStart, &r.WindowEnd,
		&r.UptimePct, &r.LatencyP95Ms, &r.IncidentCount, &r.MTTRSec,
		&r.SessionsTotal, &r.ErrorCount, &r.Details, &r.GeneratedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSLAReportNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get sla report: %w", err)
	}
	return &r, nil
}

func (s *SLAReportStore) ListByTenant(ctx context.Context, tenantID uuid.UUID, from, to time.Time, operatorID *uuid.UUID, cursor string, limit int) ([]SLAReportRow, string, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	args := []interface{}{tenantID, from, to}
	conditions := []string{"tenant_id = $1", "window_end >= $2", "window_end < $3"}
	argIdx := 4

	if operatorID != nil {
		conditions = append(conditions, fmt.Sprintf("operator_id = $%d", argIdx))
		args = append(args, *operatorID)
		argIdx++
	}

	if cursor != "" {
		parts := strings.SplitN(cursor, "_", 2)
		if len(parts) == 2 {
			unixSec, err1 := strconv.ParseInt(parts[0], 10, 64)
			cursorID, err2 := uuid.Parse(parts[1])
			if err1 == nil && err2 == nil {
				cursorTime := time.Unix(unixSec, 0).UTC()
				conditions = append(conditions, fmt.Sprintf(
					"(window_end < $%d OR (window_end = $%d AND id < $%d))",
					argIdx, argIdx, argIdx+1,
				))
				args = append(args, cursorTime, cursorID)
				argIdx += 2
			}
		}
	}

	args = append(args, limit+1)
	limitPlaceholder := fmt.Sprintf("$%d", argIdx)

	query := fmt.Sprintf(`
		SELECT id, tenant_id, operator_id, window_start, window_end,
			uptime_pct, latency_p95_ms, incident_count, mttr_sec, sessions_total, error_count, details, generated_at
		FROM sla_reports
		WHERE %s
		ORDER BY window_end DESC, id DESC
		LIMIT %s
	`, strings.Join(conditions, " AND "), limitPlaceholder)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list sla reports: %w", err)
	}
	defer rows.Close()

	var results []SLAReportRow
	for rows.Next() {
		var r SLAReportRow
		if err := rows.Scan(
			&r.ID, &r.TenantID, &r.OperatorID, &r.WindowStart, &r.WindowEnd,
			&r.UptimePct, &r.LatencyP95Ms, &r.IncidentCount, &r.MTTRSec,
			&r.SessionsTotal, &r.ErrorCount, &r.Details, &r.GeneratedAt,
		); err != nil {
			return nil, "", fmt.Errorf("store: scan sla report: %w", err)
		}
		results = append(results, r)
	}

	nextCursor := ""
	if len(results) > limit {
		last := results[limit-1]
		nextCursor = fmt.Sprintf("%d_%s", last.WindowEnd.Unix(), last.ID.String())
		results = results[:limit]
	}

	return results, nextCursor, nil
}

type OperatorMonthAgg struct {
	OperatorID      uuid.UUID  `json:"operator_id"`
	OperatorName    string     `json:"operator_name"`
	OperatorCode    string     `json:"operator_code"`
	UptimePct       float64    `json:"uptime_pct"`
	IncidentCount   int        `json:"incident_count"`
	BreachMinutes   int        `json:"breach_minutes"`
	LatencyP95Ms    int        `json:"latency_p95_ms"`
	MTTRSec         int        `json:"mttr_sec"`
	SessionsTotal   int64      `json:"sessions_total"`
	SLAUptimeTarget float64    `json:"sla_uptime_target"`
	ReportID        *uuid.UUID `json:"report_id,omitempty"`
}

type MonthSummary struct {
	Year      int                `json:"year"`
	Month     int                `json:"month"`
	Overall   OperatorMonthAgg   `json:"overall"`
	Operators []OperatorMonthAgg `json:"operators"`
}

func (s *SLAReportStore) HistoryByMonth(ctx context.Context, tenantID uuid.UUID, year int, months int, operatorID *uuid.UUID) ([]MonthSummary, error) {
	if months <= 0 || months > 24 {
		months = 6
	}

	args := []interface{}{tenantID}
	argIdx := 2

	conditions := []string{
		"r.tenant_id = $1",
		"r.operator_id IS NOT NULL",
		"EXISTS (SELECT 1 FROM operator_grants og WHERE og.operator_id = r.operator_id AND og.tenant_id = r.tenant_id AND og.enabled = true)",
	}

	if operatorID != nil {
		conditions = append(conditions, fmt.Sprintf("r.operator_id = $%d", argIdx))
		args = append(args, *operatorID)
		argIdx++
	}

	var timeFilter string
	if year != 0 {
		timeFilter = fmt.Sprintf(
			"AND DATE_PART('year', r.window_start AT TIME ZONE 'UTC') = $%d", argIdx,
		)
		args = append(args, year)
		argIdx++
	} else {
		timeFilter = fmt.Sprintf(
			"AND r.window_start >= DATE_TRUNC('month', NOW() AT TIME ZONE 'UTC') - ($%d - 1) * interval '1 month'",
			argIdx,
		)
		args = append(args, months)
		argIdx++
	}

	query := fmt.Sprintf(`
		SELECT
			r.operator_id,
			o.name                                          AS operator_name,
			o.code                                          AS operator_code,
			r.uptime_pct,
			r.incident_count,
			COALESCE(NULLIF((r.details->>'breach_minutes'),'')::int, (r.mttr_sec * r.incident_count / 60), 0) AS breach_minutes,
			r.latency_p95_ms,
			r.mttr_sec,
			r.sessions_total,
			COALESCE(o.sla_uptime_target, 99.9)             AS sla_uptime_target,
			r.id                                            AS report_id,
			DATE_PART('year',  r.window_start AT TIME ZONE 'UTC')::int AS yr,
			DATE_PART('month', r.window_start AT TIME ZONE 'UTC')::int AS mo
		FROM sla_reports r
		JOIN operators o ON o.id = r.operator_id
		WHERE %s %s
		ORDER BY yr DESC, mo DESC, o.name ASC
	`, strings.Join(conditions, " AND "), timeFilter)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: history by month query: %w", err)
	}
	defer rows.Close()

	type rowData struct {
		agg   OperatorMonthAgg
		year  int
		month int
	}

	monthMap := make(map[[2]int]*MonthSummary)
	var monthOrder [][2]int

	for rows.Next() {
		var rd rowData
		var reportID uuid.UUID
		if err := rows.Scan(
			&rd.agg.OperatorID, &rd.agg.OperatorName, &rd.agg.OperatorCode,
			&rd.agg.UptimePct, &rd.agg.IncidentCount, &rd.agg.BreachMinutes,
			&rd.agg.LatencyP95Ms, &rd.agg.MTTRSec, &rd.agg.SessionsTotal,
			&rd.agg.SLAUptimeTarget, &reportID,
			&rd.year, &rd.month,
		); err != nil {
			return nil, fmt.Errorf("store: scan history row: %w", err)
		}
		rd.agg.ReportID = &reportID

		key := [2]int{rd.year, rd.month}
		ms, ok := monthMap[key]
		if !ok {
			ms = &MonthSummary{Year: rd.year, Month: rd.month}
			monthMap[key] = ms
			monthOrder = append(monthOrder, key)
		}
		ms.Operators = append(ms.Operators, rd.agg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: history by month rows: %w", err)
	}

	result := make([]MonthSummary, 0, len(monthOrder))
	for _, key := range monthOrder {
		ms := monthMap[key]
		ms.Overall = aggregateOverall(ms.Operators)
		ms.Overall.OperatorName = "overall"
		result = append(result, *ms)
	}
	return result, nil
}

func (s *SLAReportStore) MonthDetail(ctx context.Context, tenantID uuid.UUID, year, month int) (*MonthSummary, error) {
	from := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)

	rows, err := s.db.Query(ctx, `
		SELECT
			r.operator_id,
			o.name                                            AS operator_name,
			o.code                                            AS operator_code,
			r.uptime_pct,
			r.incident_count,
			COALESCE(NULLIF((r.details->>'breach_minutes'),'')::int, (r.mttr_sec * r.incident_count / 60), 0) AS breach_minutes,
			r.latency_p95_ms,
			r.mttr_sec,
			r.sessions_total,
			COALESCE(o.sla_uptime_target, 99.9)               AS sla_uptime_target,
			r.id                                              AS report_id
		FROM sla_reports r
		JOIN operators o ON o.id = r.operator_id
		WHERE r.tenant_id = $1
		  AND r.operator_id IS NOT NULL
		  AND EXISTS (
		      SELECT 1 FROM operator_grants og
		      WHERE og.operator_id = r.operator_id
		        AND og.tenant_id = r.tenant_id
		        AND og.enabled = true
		  )
		  AND r.window_start >= $2
		  AND r.window_start <  $3
		ORDER BY o.name ASC
	`, tenantID, from, to)
	if err != nil {
		return nil, fmt.Errorf("store: month detail query: %w", err)
	}
	defer rows.Close()

	ms := &MonthSummary{Year: year, Month: month}
	for rows.Next() {
		var agg OperatorMonthAgg
		var reportID uuid.UUID
		if err := rows.Scan(
			&agg.OperatorID, &agg.OperatorName, &agg.OperatorCode,
			&agg.UptimePct, &agg.IncidentCount, &agg.BreachMinutes,
			&agg.LatencyP95Ms, &agg.MTTRSec, &agg.SessionsTotal,
			&agg.SLAUptimeTarget, &reportID,
		); err != nil {
			return nil, fmt.Errorf("store: scan month detail row: %w", err)
		}
		agg.ReportID = &reportID
		ms.Operators = append(ms.Operators, agg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: month detail rows: %w", err)
	}

	ms.Overall = aggregateOverall(ms.Operators)
	ms.Overall.OperatorName = "overall"
	return ms, nil
}

func (s *SLAReportStore) GetByTenantOperatorMonth(ctx context.Context, tenantID, operatorID uuid.UUID, year, month int) (*SLAReportRow, error) {
	from := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)
	var r SLAReportRow
	err := s.db.QueryRow(ctx, `
		SELECT id, tenant_id, operator_id, window_start, window_end,
			uptime_pct, latency_p95_ms, incident_count, mttr_sec, sessions_total, error_count, details, generated_at
		FROM sla_reports
		WHERE tenant_id = $1 AND operator_id = $2 AND window_start = $3 AND window_end = $4
		LIMIT 1
	`, tenantID, operatorID, from, to).Scan(
		&r.ID, &r.TenantID, &r.OperatorID, &r.WindowStart, &r.WindowEnd,
		&r.UptimePct, &r.LatencyP95Ms, &r.IncidentCount, &r.MTTRSec,
		&r.SessionsTotal, &r.ErrorCount, &r.Details, &r.GeneratedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSLAReportNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get sla report by tenant+operator+month: %w", err)
	}
	return &r, nil
}

func (s *SLAReportStore) UpsertMonthlyRollup(ctx context.Context, row SLAReportRow) error {
	details := row.Details
	if len(details) == 0 {
		details = json.RawMessage(`{}`)
	}
	_, err := s.db.Exec(ctx, `
		INSERT INTO sla_reports
			(tenant_id, operator_id, window_start, window_end,
			 uptime_pct, latency_p95_ms, incident_count, mttr_sec,
			 sessions_total, error_count, details, generated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
		ON CONFLICT (tenant_id, COALESCE(operator_id, '00000000-0000-0000-0000-000000000000'::uuid), window_start, window_end)
		DO UPDATE SET
			uptime_pct     = EXCLUDED.uptime_pct,
			latency_p95_ms = EXCLUDED.latency_p95_ms,
			incident_count = EXCLUDED.incident_count,
			mttr_sec       = EXCLUDED.mttr_sec,
			sessions_total = EXCLUDED.sessions_total,
			error_count    = EXCLUDED.error_count,
			details        = EXCLUDED.details,
			generated_at   = NOW()
	`,
		row.TenantID, row.OperatorID, row.WindowStart, row.WindowEnd,
		row.UptimePct, row.LatencyP95Ms, row.IncidentCount, row.MTTRSec,
		row.SessionsTotal, row.ErrorCount, details,
	)
	if err != nil {
		return fmt.Errorf("store: upsert monthly rollup: %w", err)
	}
	return nil
}

// aggregateOverall computes a tenant-level monthly aggregate from per-operator rows.
// Uptime is session-weighted (avg of uptime_pct weighted by sessions_total, falling
// back to a plain mean when session counts are all zero) so high-traffic operators
// dominate the headline metric. Latency p95 and MTTR are also session-weighted
// averages rather than max, preventing a single low-traffic outlier from skewing
// the overall number. Incidents and breach minutes are summed.
func aggregateOverall(ops []OperatorMonthAgg) OperatorMonthAgg {
	if len(ops) == 0 {
		return OperatorMonthAgg{}
	}
	var overall OperatorMonthAgg
	var weightedUptime, weightedLatency, weightedMTTR, weightedTarget float64
	var totalSessions int64
	for _, op := range ops {
		overall.IncidentCount += op.IncidentCount
		overall.BreachMinutes += op.BreachMinutes
		overall.SessionsTotal += op.SessionsTotal
		w := float64(op.SessionsTotal)
		weightedUptime += op.UptimePct * w
		weightedLatency += float64(op.LatencyP95Ms) * w
		weightedMTTR += float64(op.MTTRSec) * w
		weightedTarget += op.SLAUptimeTarget * w
		totalSessions += op.SessionsTotal
	}
	if totalSessions > 0 {
		w := float64(totalSessions)
		overall.UptimePct = weightedUptime / w
		overall.LatencyP95Ms = int(weightedLatency / w)
		overall.MTTRSec = int(weightedMTTR / w)
		overall.SLAUptimeTarget = weightedTarget / w
	} else {
		n := float64(len(ops))
		var sumUptime, sumTarget float64
		var maxLatency, maxMTTR int
		for _, op := range ops {
			sumUptime += op.UptimePct
			sumTarget += op.SLAUptimeTarget
			if op.LatencyP95Ms > maxLatency {
				maxLatency = op.LatencyP95Ms
			}
			if op.MTTRSec > maxMTTR {
				maxMTTR = op.MTTRSec
			}
		}
		overall.UptimePct = sumUptime / n
		overall.SLAUptimeTarget = sumTarget / n
		overall.LatencyP95Ms = maxLatency
		overall.MTTRSec = maxMTTR
	}
	return overall
}

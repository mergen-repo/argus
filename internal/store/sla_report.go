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

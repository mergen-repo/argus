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

var ErrScheduledReportNotFound = errors.New("store: scheduled report not found")

type ScheduledReport struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	ReportType   string
	ScheduleCron string
	Format       string
	Recipients   []string
	Filters      json.RawMessage
	LastRunAt    *time.Time
	NextRunAt    *time.Time
	LastJobID    *uuid.UUID
	State        string
	CreatedAt    time.Time
	CreatedBy    *uuid.UUID
	UpdatedAt    time.Time
}

type ScheduledReportPatch struct {
	ScheduleCron *string
	Recipients   *[]string
	Filters      json.RawMessage
	State        *string
	Format       *string
	NextRunAt    *time.Time
}

type ScheduledReportStore struct {
	db *pgxpool.Pool
}

func NewScheduledReportStore(db *pgxpool.Pool) *ScheduledReportStore {
	return &ScheduledReportStore{db: db}
}

const scheduledReportColumns = `id, tenant_id, report_type, schedule_cron, format, recipients, filters,
	last_run_at, next_run_at, last_job_id, state, created_at, created_by, updated_at`

func scanScheduledReport(row pgx.Row) (*ScheduledReport, error) {
	var r ScheduledReport
	err := row.Scan(
		&r.ID, &r.TenantID, &r.ReportType, &r.ScheduleCron, &r.Format, &r.Recipients, &r.Filters,
		&r.LastRunAt, &r.NextRunAt, &r.LastJobID, &r.State, &r.CreatedAt, &r.CreatedBy, &r.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func scanScheduledReportRows(rows pgx.Rows) (*ScheduledReport, error) {
	var r ScheduledReport
	err := rows.Scan(
		&r.ID, &r.TenantID, &r.ReportType, &r.ScheduleCron, &r.Format, &r.Recipients, &r.Filters,
		&r.LastRunAt, &r.NextRunAt, &r.LastJobID, &r.State, &r.CreatedAt, &r.CreatedBy, &r.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *ScheduledReportStore) Create(ctx context.Context, tenantID uuid.UUID, createdBy *uuid.UUID, reportType, scheduleCron, format string, recipients []string, filters json.RawMessage, nextRunAt time.Time) (*ScheduledReport, error) {
	if filters == nil {
		filters = json.RawMessage(`{}`)
	}
	if recipients == nil {
		recipients = []string{}
	}

	row := s.db.QueryRow(ctx, `
		INSERT INTO scheduled_reports
			(tenant_id, created_by, report_type, schedule_cron, format, recipients, filters, next_run_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING `+scheduledReportColumns,
		tenantID, createdBy, reportType, scheduleCron, format, recipients, filters, nextRunAt,
	)

	r, err := scanScheduledReport(row)
	if err != nil {
		return nil, fmt.Errorf("store: create scheduled report: %w", err)
	}
	return r, nil
}

func (s *ScheduledReportStore) GetByID(ctx context.Context, id uuid.UUID) (*ScheduledReport, error) {
	row := s.db.QueryRow(ctx, `
		SELECT `+scheduledReportColumns+`
		FROM scheduled_reports
		WHERE id = $1`, id)

	r, err := scanScheduledReport(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrScheduledReportNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get scheduled report: %w", err)
	}
	return r, nil
}

func (s *ScheduledReportStore) List(ctx context.Context, tenantID uuid.UUID, cursor string, limit int) ([]*ScheduledReport, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	args := []interface{}{tenantID}
	conditions := []string{"tenant_id = $1"}
	argIdx := 2

	if cursor != "" {
		cursorID, err := uuid.Parse(cursor)
		if err == nil {
			conditions = append(conditions, fmt.Sprintf("id < $%d", argIdx))
			args = append(args, cursorID)
			argIdx++
		}
	}

	args = append(args, limit+1)
	limitPlaceholder := fmt.Sprintf("$%d", argIdx)

	query := fmt.Sprintf(`
		SELECT `+scheduledReportColumns+`
		FROM scheduled_reports
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT %s`,
		strings.Join(conditions, " AND "), limitPlaceholder)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list scheduled reports: %w", err)
	}
	defer rows.Close()

	var results []*ScheduledReport
	for rows.Next() {
		r, err := scanScheduledReportRows(rows)
		if err != nil {
			return nil, "", fmt.Errorf("store: scan scheduled report: %w", err)
		}
		results = append(results, r)
	}

	nextCursor := ""
	if len(results) > limit {
		nextCursor = results[limit-1].ID.String()
		results = results[:limit]
	}

	return results, nextCursor, nil
}

func (s *ScheduledReportStore) Update(ctx context.Context, id uuid.UUID, patch ScheduledReportPatch) error {
	sets := []string{}
	args := []interface{}{}
	argIdx := 1

	if patch.ScheduleCron != nil {
		sets = append(sets, fmt.Sprintf("schedule_cron = $%d", argIdx))
		args = append(args, *patch.ScheduleCron)
		argIdx++
	}
	if patch.Recipients != nil {
		sets = append(sets, fmt.Sprintf("recipients = $%d", argIdx))
		args = append(args, *patch.Recipients)
		argIdx++
	}
	if patch.Filters != nil {
		sets = append(sets, fmt.Sprintf("filters = $%d", argIdx))
		args = append(args, patch.Filters)
		argIdx++
	}
	if patch.State != nil {
		sets = append(sets, fmt.Sprintf("state = $%d", argIdx))
		args = append(args, *patch.State)
		argIdx++
	}
	if patch.Format != nil {
		sets = append(sets, fmt.Sprintf("format = $%d", argIdx))
		args = append(args, *patch.Format)
		argIdx++
	}
	if patch.NextRunAt != nil {
		sets = append(sets, fmt.Sprintf("next_run_at = $%d", argIdx))
		args = append(args, *patch.NextRunAt)
		argIdx++
	}

	if len(sets) == 0 {
		return nil
	}

	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)

	query := fmt.Sprintf(`
		UPDATE scheduled_reports
		SET %s
		WHERE id = $%d`,
		strings.Join(sets, ", "), argIdx)

	tag, err := s.db.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("store: update scheduled report: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrScheduledReportNotFound
	}
	return nil
}

func (s *ScheduledReportStore) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM scheduled_reports WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("store: delete scheduled report: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrScheduledReportNotFound
	}
	return nil
}

func (s *ScheduledReportStore) ListDue(ctx context.Context, now time.Time, limit int) ([]*ScheduledReport, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	rows, err := s.db.Query(ctx, `
		SELECT `+scheduledReportColumns+`
		FROM scheduled_reports
		WHERE state = 'active'
		  AND next_run_at IS NOT NULL
		  AND next_run_at <= $1
		ORDER BY next_run_at ASC
		LIMIT $2`,
		now, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list due scheduled reports: %w", err)
	}
	defer rows.Close()

	var results []*ScheduledReport
	for rows.Next() {
		r, err := scanScheduledReportRows(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan due scheduled report: %w", err)
		}
		results = append(results, r)
	}
	return results, nil
}

func (s *ScheduledReportStore) UpdateLastRun(ctx context.Context, id uuid.UUID, lastRunAt, nextRunAt time.Time, lastJobID uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE scheduled_reports
		SET last_run_at = $2, next_run_at = $3, last_job_id = $4, updated_at = NOW()
		WHERE id = $1`,
		id, lastRunAt, nextRunAt, lastJobID)
	if err != nil {
		return fmt.Errorf("store: update last run: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrScheduledReportNotFound
	}
	return nil
}

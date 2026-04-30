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
	ErrJobNotFound       = errors.New("job not found")
	ErrJobAlreadyRunning = errors.New("job already running")
	ErrJobCancelled      = errors.New("job cancelled")
)

type Job struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	Type            string
	State           string
	Priority        int
	Payload         json.RawMessage
	TotalItems      int
	ProcessedItems  int
	FailedItems     int
	ProgressPct     float64
	ErrorReport     json.RawMessage
	Result          json.RawMessage
	MaxRetries      int
	RetryCount      int
	RetryBackoffSec int
	ScheduledAt     *time.Time
	StartedAt       *time.Time
	CompletedAt     *time.Time
	CreatedAt       time.Time
	CreatedBy       *uuid.UUID
	CreatedByName   *string
	CreatedByEmail  *string
	LockedBy        *string
	LockedAt        *time.Time
}

type CreateJobParams struct {
	Type       string
	Priority   int
	Payload    json.RawMessage
	TotalItems int
	CreatedBy  *uuid.UUID
}

type JobListFilter struct {
	Type  string
	State string
}

type JobStore struct {
	db *pgxpool.Pool
}

func NewJobStore(db *pgxpool.Pool) *JobStore {
	return &JobStore{db: db}
}

func (s *JobStore) Create(ctx context.Context, p CreateJobParams) (*Job, error) {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	priority := p.Priority
	if priority == 0 {
		priority = 5
	}

	payload := p.Payload
	if payload == nil {
		payload = json.RawMessage(`{}`)
	}

	var job Job
	err = s.db.QueryRow(ctx, `
		INSERT INTO jobs (tenant_id, type, state, priority, payload, total_items, created_by)
		VALUES ($1, $2, 'queued', $3, $4, $5, $6)
		RETURNING id, tenant_id, type, state, priority, payload,
			total_items, processed_items, failed_items, progress_pct,
			error_report, result, max_retries, retry_count, retry_backoff_sec,
			scheduled_at, started_at, completed_at, created_at, created_by,
			locked_by, locked_at
	`, tenantID, p.Type, priority, payload, p.TotalItems, p.CreatedBy).Scan(
		&job.ID, &job.TenantID, &job.Type, &job.State, &job.Priority, &job.Payload,
		&job.TotalItems, &job.ProcessedItems, &job.FailedItems, &job.ProgressPct,
		&job.ErrorReport, &job.Result, &job.MaxRetries, &job.RetryCount, &job.RetryBackoffSec,
		&job.ScheduledAt, &job.StartedAt, &job.CompletedAt, &job.CreatedAt, &job.CreatedBy,
		&job.LockedBy, &job.LockedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create job: %w", err)
	}
	return &job, nil
}

func (s *JobStore) CreateWithTenantID(ctx context.Context, tenantID uuid.UUID, p CreateJobParams) (*Job, error) {
	priority := p.Priority
	if priority == 0 {
		priority = 5
	}

	payload := p.Payload
	if payload == nil {
		payload = json.RawMessage(`{}`)
	}

	var job Job
	err := s.db.QueryRow(ctx, `
		INSERT INTO jobs (tenant_id, type, state, priority, payload, total_items, created_by)
		VALUES ($1, $2, 'queued', $3, $4, $5, $6)
		RETURNING id, tenant_id, type, state, priority, payload,
			total_items, processed_items, failed_items, progress_pct,
			error_report, result, max_retries, retry_count, retry_backoff_sec,
			scheduled_at, started_at, completed_at, created_at, created_by,
			locked_by, locked_at
	`, tenantID, p.Type, priority, payload, p.TotalItems, p.CreatedBy).Scan(
		&job.ID, &job.TenantID, &job.Type, &job.State, &job.Priority, &job.Payload,
		&job.TotalItems, &job.ProcessedItems, &job.FailedItems, &job.ProgressPct,
		&job.ErrorReport, &job.Result, &job.MaxRetries, &job.RetryCount, &job.RetryBackoffSec,
		&job.ScheduledAt, &job.StartedAt, &job.CompletedAt, &job.CreatedAt, &job.CreatedBy,
		&job.LockedBy, &job.LockedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create job with tenant: %w", err)
	}
	return &job, nil
}

func (s *JobStore) GetByID(ctx context.Context, id uuid.UUID) (*Job, error) {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	var job Job
	err = s.db.QueryRow(ctx, `
		SELECT id, tenant_id, type, state, priority, payload,
			total_items, processed_items, failed_items, progress_pct,
			error_report, result, max_retries, retry_count, retry_backoff_sec,
			scheduled_at, started_at, completed_at, created_at, created_by,
			locked_by, locked_at
		FROM jobs
		WHERE id = $1 AND tenant_id = $2
	`, id, tenantID).Scan(
		&job.ID, &job.TenantID, &job.Type, &job.State, &job.Priority, &job.Payload,
		&job.TotalItems, &job.ProcessedItems, &job.FailedItems, &job.ProgressPct,
		&job.ErrorReport, &job.Result, &job.MaxRetries, &job.RetryCount, &job.RetryBackoffSec,
		&job.ScheduledAt, &job.StartedAt, &job.CompletedAt, &job.CreatedAt, &job.CreatedBy,
		&job.LockedBy, &job.LockedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrJobNotFound
		}
		return nil, fmt.Errorf("get job: %w", err)
	}
	return &job, nil
}

func (s *JobStore) GetByIDInternal(ctx context.Context, id uuid.UUID) (*Job, error) {
	var job Job
	err := s.db.QueryRow(ctx, `
		SELECT id, tenant_id, type, state, priority, payload,
			total_items, processed_items, failed_items, progress_pct,
			error_report, result, max_retries, retry_count, retry_backoff_sec,
			scheduled_at, started_at, completed_at, created_at, created_by,
			locked_by, locked_at
		FROM jobs
		WHERE id = $1
	`, id).Scan(
		&job.ID, &job.TenantID, &job.Type, &job.State, &job.Priority, &job.Payload,
		&job.TotalItems, &job.ProcessedItems, &job.FailedItems, &job.ProgressPct,
		&job.ErrorReport, &job.Result, &job.MaxRetries, &job.RetryCount, &job.RetryBackoffSec,
		&job.ScheduledAt, &job.StartedAt, &job.CompletedAt, &job.CreatedAt, &job.CreatedBy,
		&job.LockedBy, &job.LockedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrJobNotFound
		}
		return nil, fmt.Errorf("get job internal: %w", err)
	}
	return &job, nil
}

func (s *JobStore) List(ctx context.Context, cursor string, limit int, filter JobListFilter) ([]Job, string, error) {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return nil, "", err
	}

	if limit <= 0 || limit > 100 {
		limit = 20
	}

	args := []interface{}{tenantID}
	conditions := []string{"j.tenant_id = $1"}
	argIdx := 2

	if filter.Type != "" {
		conditions = append(conditions, fmt.Sprintf("j.type = $%d", argIdx))
		args = append(args, filter.Type)
		argIdx++
	}
	if filter.State != "" {
		conditions = append(conditions, fmt.Sprintf("j.state = $%d", argIdx))
		args = append(args, filter.State)
		argIdx++
	}

	if cursor != "" {
		cursorID, parseErr := uuid.Parse(cursor)
		if parseErr == nil {
			conditions = append(conditions, fmt.Sprintf("j.id < $%d", argIdx))
			args = append(args, cursorID)
			argIdx++
		}
	}

	where := "WHERE " + strings.Join(conditions, " AND ")

	args = append(args, limit+1)
	limitPlaceholder := fmt.Sprintf("$%d", argIdx)

	query := fmt.Sprintf(`
		SELECT j.id, j.tenant_id, j.type, j.state, j.priority, j.payload,
			j.total_items, j.processed_items, j.failed_items, j.progress_pct,
			j.error_report, j.result, j.max_retries, j.retry_count, j.retry_backoff_sec,
			j.scheduled_at, j.started_at, j.completed_at, j.created_at, j.created_by,
			j.locked_by, j.locked_at,
			u.name, u.email
		FROM jobs j
		LEFT JOIN users u ON j.created_by = u.id
		%s
		ORDER BY j.created_at DESC, j.id DESC
		LIMIT %s
	`, where, limitPlaceholder)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()

	var results []Job
	for rows.Next() {
		var job Job
		if err := rows.Scan(
			&job.ID, &job.TenantID, &job.Type, &job.State, &job.Priority, &job.Payload,
			&job.TotalItems, &job.ProcessedItems, &job.FailedItems, &job.ProgressPct,
			&job.ErrorReport, &job.Result, &job.MaxRetries, &job.RetryCount, &job.RetryBackoffSec,
			&job.ScheduledAt, &job.StartedAt, &job.CompletedAt, &job.CreatedAt, &job.CreatedBy,
			&job.LockedBy, &job.LockedAt,
			&job.CreatedByName, &job.CreatedByEmail,
		); err != nil {
			return nil, "", fmt.Errorf("scan job: %w", err)
		}
		results = append(results, job)
	}

	nextCursor := ""
	if len(results) > limit {
		nextCursor = results[limit-1].ID.String()
		results = results[:limit]
	}

	return results, nextCursor, nil
}

func (s *JobStore) Lock(ctx context.Context, jobID uuid.UUID, lockedBy string) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE jobs SET state = 'running', locked_by = $2, locked_at = NOW(), started_at = COALESCE(started_at, NOW())
		WHERE id = $1 AND state IN ('queued', 'retry_pending')
	`, jobID, lockedBy)
	if err != nil {
		return fmt.Errorf("lock job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrJobAlreadyRunning
	}
	return nil
}

func (s *JobStore) UpdateProgress(ctx context.Context, jobID uuid.UUID, processed, failed, total int) error {
	pct := float64(0)
	if total > 0 {
		pct = float64(processed+failed) / float64(total) * 100.0
	}
	if pct > 100 {
		pct = 100
	}

	_, err := s.db.Exec(ctx, `
		UPDATE jobs SET processed_items = $2, failed_items = $3, progress_pct = $4
		WHERE id = $1
	`, jobID, processed, failed, pct)
	if err != nil {
		return fmt.Errorf("update progress: %w", err)
	}
	return nil
}

func (s *JobStore) Complete(ctx context.Context, jobID uuid.UUID, errorReport json.RawMessage, result json.RawMessage) error {
	_, err := s.db.Exec(ctx, `
		UPDATE jobs SET state = 'completed', completed_at = NOW(), progress_pct = 100,
			error_report = $2, result = $3, locked_by = NULL, locked_at = NULL
		WHERE id = $1
	`, jobID, errorReport, result)
	if err != nil {
		return fmt.Errorf("complete job: %w", err)
	}
	return nil
}

func (s *JobStore) Fail(ctx context.Context, jobID uuid.UUID, errorReport json.RawMessage) error {
	_, err := s.db.Exec(ctx, `
		UPDATE jobs SET state = 'failed', completed_at = NOW(),
			error_report = $2, locked_by = NULL, locked_at = NULL
		WHERE id = $1
	`, jobID, errorReport)
	if err != nil {
		return fmt.Errorf("fail job: %w", err)
	}
	return nil
}

func (s *JobStore) Cancel(ctx context.Context, id uuid.UUID) error {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return err
	}

	tag, err := s.db.Exec(ctx, `
		UPDATE jobs SET state = 'cancelled', completed_at = COALESCE(completed_at, NOW())
		WHERE id = $1 AND tenant_id = $2 AND state IN ('queued', 'retry_pending', 'running')
	`, id, tenantID)
	if err != nil {
		return fmt.Errorf("cancel job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		var state string
		checkErr := s.db.QueryRow(ctx, `SELECT state FROM jobs WHERE id = $1 AND tenant_id = $2`, id, tenantID).Scan(&state)
		if checkErr != nil {
			if errors.Is(checkErr, pgx.ErrNoRows) {
				return ErrJobNotFound
			}
			return fmt.Errorf("check job state: %w", checkErr)
		}
		return fmt.Errorf("job cannot be cancelled in state: %s", state)
	}
	return nil
}

func (s *JobStore) SetRetryPending(ctx context.Context, jobID uuid.UUID) error {
	_, err := s.db.Exec(ctx, `
		UPDATE jobs SET state = 'retry_pending', retry_count = retry_count + 1,
			locked_by = NULL, locked_at = NULL
		WHERE id = $1
	`, jobID)
	if err != nil {
		return fmt.Errorf("set retry pending: %w", err)
	}
	return nil
}

func (s *JobStore) CheckCancelled(ctx context.Context, jobID uuid.UUID) (bool, error) {
	var state string
	err := s.db.QueryRow(ctx, `SELECT state FROM jobs WHERE id = $1`, jobID).Scan(&state)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrJobNotFound
		}
		return false, fmt.Errorf("check cancelled: %w", err)
	}
	return state == "cancelled", nil
}

func (s *JobStore) GetErrorReport(ctx context.Context, id uuid.UUID) (json.RawMessage, error) {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	var report json.RawMessage
	err = s.db.QueryRow(ctx, `
		SELECT error_report FROM jobs WHERE id = $1 AND tenant_id = $2
	`, id, tenantID).Scan(&report)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrJobNotFound
		}
		return nil, fmt.Errorf("get error report: %w", err)
	}
	return report, nil
}

func (s *JobStore) FindTimedOutJobs(ctx context.Context, timeout time.Duration) ([]Job, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, tenant_id, type, state, priority, payload,
			total_items, processed_items, failed_items, progress_pct,
			error_report, result, max_retries, retry_count, retry_backoff_sec,
			scheduled_at, started_at, completed_at, created_at, created_by,
			locked_by, locked_at
		FROM jobs
		WHERE state = 'running' AND locked_at < NOW() - $1::interval
	`, fmt.Sprintf("%d minutes", int(timeout.Minutes())))
	if err != nil {
		return nil, fmt.Errorf("find timed out jobs: %w", err)
	}
	defer rows.Close()

	var results []Job
	for rows.Next() {
		var job Job
		if err := rows.Scan(
			&job.ID, &job.TenantID, &job.Type, &job.State, &job.Priority, &job.Payload,
			&job.TotalItems, &job.ProcessedItems, &job.FailedItems, &job.ProgressPct,
			&job.ErrorReport, &job.Result, &job.MaxRetries, &job.RetryCount, &job.RetryBackoffSec,
			&job.ScheduledAt, &job.StartedAt, &job.CompletedAt, &job.CreatedAt, &job.CreatedBy,
			&job.LockedBy, &job.LockedAt,
		); err != nil {
			return nil, fmt.Errorf("scan timed out job: %w", err)
		}
		results = append(results, job)
	}

	return results, nil
}

func (s *JobStore) CreateRetryJob(ctx context.Context, original *Job, failedPayload json.RawMessage) (*Job, error) {
	payload := failedPayload
	if payload == nil {
		payload = original.Payload
	}

	var job Job
	err := s.db.QueryRow(ctx, `
		INSERT INTO jobs (tenant_id, type, state, priority, payload, total_items, created_by, retry_count, result)
		VALUES ($1, $2, 'queued', $3, $4, $5, $6, $7, $8)
		RETURNING id, tenant_id, type, state, priority, payload,
			total_items, processed_items, failed_items, progress_pct,
			error_report, result, max_retries, retry_count, retry_backoff_sec,
			scheduled_at, started_at, completed_at, created_at, created_by,
			locked_by, locked_at
	`, original.TenantID, original.Type, original.Priority, payload,
		original.FailedItems, original.CreatedBy, original.RetryCount+1,
		json.RawMessage(fmt.Sprintf(`{"retry_of":"%s"}`, original.ID.String())),
	).Scan(
		&job.ID, &job.TenantID, &job.Type, &job.State, &job.Priority, &job.Payload,
		&job.TotalItems, &job.ProcessedItems, &job.FailedItems, &job.ProgressPct,
		&job.ErrorReport, &job.Result, &job.MaxRetries, &job.RetryCount, &job.RetryBackoffSec,
		&job.ScheduledAt, &job.StartedAt, &job.CompletedAt, &job.CreatedAt, &job.CreatedBy,
		&job.LockedBy, &job.LockedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create retry job: %w", err)
	}
	return &job, nil
}

func (s *JobStore) CountActiveByTenant(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var count int
	err := s.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM jobs WHERE tenant_id = $1 AND state = 'running'
	`, tenantID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count active jobs: %w", err)
	}
	return count, nil
}

func (s *JobStore) TouchLock(ctx context.Context, jobID uuid.UUID, lockedBy string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE jobs SET locked_at = NOW()
		WHERE id = $1 AND locked_by = $2 AND state = 'running'
	`, jobID, lockedBy)
	if err != nil {
		return fmt.Errorf("touch lock: %w", err)
	}
	return nil
}

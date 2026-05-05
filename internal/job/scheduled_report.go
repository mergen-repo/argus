package job

// ScheduledReportProcessor handles JobTypeScheduledReportRun jobs.
// It supports two payload shapes:
//
//   1. on-demand (POST /api/v1/reports/generate) —
//      {"report_type":"...","format":"...","filters":{...},"tenant_id":"...","requested_by":"..."}
//
//   2. scheduled (created by ScheduledReportSweeper) —
//      {"scheduled_report_id":"<uuid>"}
//
// The processor builds the report, uploads it to S3, generates a signed URL
// (7-day TTL), and — for the scheduled path — sends a notification to each
// recipient listed on the scheduled_reports row, then advances next_run_at via
// NextRunAfter(schedule_cron, now).

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/observability/metrics"
	"github.com/btopcu/argus/internal/report"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const scheduledReportSignedURLTTL = 7 * 24 * time.Hour

type scheduledReportRunPayload struct {
	ScheduledReportID string         `json:"scheduled_report_id,omitempty"`
	ReportType        string         `json:"report_type,omitempty"`
	Format            string         `json:"format,omitempty"`
	Filters           map[string]any `json:"filters,omitempty"`
	TenantID          string         `json:"tenant_id,omitempty"`
	RequestedBy       string         `json:"requested_by,omitempty"`
}

type scheduledReportResult struct {
	S3Key      string `json:"s3_key"`
	SignedURL  string `json:"signed_url"`
	BytesSize  int    `json:"bytes_size"`
	ExpiresAt  string `json:"expires_at"`
	ReportType string `json:"report_type"`
	Format     string `json:"format"`
	Filename   string `json:"filename"`
}

type scheduledReportEngine interface {
	Build(ctx context.Context, req report.Request) (*report.Artifact, error)
}

type scheduledReportRowStore interface {
	GetByID(ctx context.Context, id uuid.UUID) (*store.ScheduledReport, error)
	UpdateLastRun(ctx context.Context, id uuid.UUID, lastRunAt, nextRunAt time.Time, lastJobID uuid.UUID) error
}

type scheduledReportStorage interface {
	Upload(ctx context.Context, bucket, key string, data []byte) error
	PresignGet(ctx context.Context, bucket, key string, ttl time.Duration) (string, error)
}

type scheduledReportMetrics interface {
	IncScheduledReportRun(reportType, result string)
}

// ScheduledReportProcessor builds a single scheduled or on-demand report.
type ScheduledReportProcessor struct {
	jobs     jobProgressTracker
	rows     scheduledReportRowStore
	engine   scheduledReportEngine
	storage  scheduledReportStorage
	eventBus busPublisher
	metrics  scheduledReportMetrics
	now      func() time.Time
	logger   zerolog.Logger
}

func NewScheduledReportProcessor(
	jobs *store.JobStore,
	rows *store.ScheduledReportStore,
	engine *report.Engine,
	storage scheduledReportStorage,
	eventBus *bus.EventBus,
	reg *metrics.Registry,
	logger zerolog.Logger,
) *ScheduledReportProcessor {
	return &ScheduledReportProcessor{
		jobs:     jobs,
		rows:     rows,
		engine:   engine,
		storage:  storage,
		eventBus: eventBus,
		metrics:  reg,
		now:      func() time.Time { return time.Now().UTC() },
		logger:   logger.With().Str("processor", JobTypeScheduledReportRun).Logger(),
	}
}

func (p *ScheduledReportProcessor) Type() string {
	return JobTypeScheduledReportRun
}

func (p *ScheduledReportProcessor) Process(ctx context.Context, j *store.Job) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	var payload scheduledReportRunPayload
	if err := json.Unmarshal(j.Payload, &payload); err != nil {
		return fmt.Errorf("scheduled_report: unmarshal payload: %w", err)
	}

	var (
		row    *store.ScheduledReport
		req    report.Request
		recips []string
	)

	tenantID := j.TenantID

	if payload.ScheduledReportID != "" {
		schedID, err := uuid.Parse(payload.ScheduledReportID)
		if err != nil {
			return fmt.Errorf("scheduled_report: parse scheduled_report_id: %w", err)
		}
		row, err = p.rows.GetByID(ctx, schedID)
		if err != nil {
			return fmt.Errorf("scheduled_report: load scheduled row: %w", err)
		}
		tenantID = row.TenantID
		filters := map[string]any{}
		if len(row.Filters) > 0 {
			_ = json.Unmarshal(row.Filters, &filters)
		}
		req = report.Request{
			Type:     report.ReportType(row.ReportType),
			Format:   report.Format(row.Format),
			TenantID: row.TenantID,
			Filters:  filters,
		}
		recips = row.Recipients
	} else {
		if payload.ReportType == "" || payload.Format == "" {
			return fmt.Errorf("scheduled_report: missing report_type or format")
		}
		req = report.Request{
			Type:     report.ReportType(payload.ReportType),
			Format:   report.Format(payload.Format),
			TenantID: tenantID,
			Filters:  payload.Filters,
		}
	}

	artifact, err := p.engine.Build(ctx, req)
	if err != nil {
		p.metrics.IncScheduledReportRun(string(req.Type), "failed")
		return fmt.Errorf("scheduled_report: build artifact: %w", err)
	}

	filename := artifact.Filename
	if filename == "" {
		filename = fmt.Sprintf("%s_%s%s",
			string(req.Type),
			p.now().Format("20060102_150405"),
			artifact.Extension())
	}

	s3Key := fmt.Sprintf("tenants/%s/reports/%s/%s",
		tenantID.String(), j.ID.String(), filename)

	if err := p.storage.Upload(ctx, "", s3Key, artifact.Bytes); err != nil {
		p.metrics.IncScheduledReportRun(string(req.Type), "failed")
		return fmt.Errorf("scheduled_report: s3 upload: %w", err)
	}

	signedURL, err := p.storage.PresignGet(ctx, "", s3Key, scheduledReportSignedURLTTL)
	if err != nil {
		p.logger.Warn().Err(err).Str("s3_key", s3Key).Msg("presign failed")
		signedURL = ""
	}

	expiresAt := p.now().Add(scheduledReportSignedURLTTL).Format(time.RFC3339)

	if row != nil && len(recips) > 0 && p.eventBus != nil {
		_ = p.eventBus.Publish(ctx, bus.SubjectNotification, map[string]any{
			"event_type":   "report_ready",
			"tenant_id":    tenantID.String(),
			"recipients":   recips,
			"report_type":  string(req.Type),
			"format":       string(req.Format),
			"download_url": signedURL,
			"expires_at":   expiresAt,
			"filename":     filename,
		})
	}

	if row != nil {
		next, nextErr := NextRunAfter(row.ScheduleCron, p.now())
		if nextErr != nil {
			p.logger.Warn().Err(nextErr).Str("schedule_cron", row.ScheduleCron).Msg("next run after failed; leaving next_run_at unchanged")
		} else {
			if updErr := p.rows.UpdateLastRun(ctx, row.ID, p.now(), next, j.ID); updErr != nil {
				p.logger.Warn().Err(updErr).Str("scheduled_report_id", row.ID.String()).Msg("update last run failed")
			}
		}
	}

	p.metrics.IncScheduledReportRun(string(req.Type), "succeeded")

	result, _ := json.Marshal(scheduledReportResult{
		S3Key:      s3Key,
		SignedURL:  signedURL,
		BytesSize:  len(artifact.Bytes),
		ExpiresAt:  expiresAt,
		ReportType: string(req.Type),
		Format:     string(req.Format),
		Filename:   filename,
	})

	if err := p.jobs.Complete(ctx, j.ID, nil, result); err != nil {
		return fmt.Errorf("scheduled_report: complete job: %w", err)
	}

	if p.eventBus != nil {
		_ = p.eventBus.Publish(ctx, bus.SubjectJobCompleted, map[string]any{
			"job_id":      j.ID.String(),
			"tenant_id":   tenantID.String(),
			"type":        JobTypeScheduledReportRun,
			"state":       "completed",
			"s3_key":      s3Key,
			"report_type": string(req.Type),
			"format":      string(req.Format),
		})
	}

	return nil
}

// ─── Sweeper ──────────────────────────────────────────────────────────────────

type scheduledReportLister interface {
	ListDue(ctx context.Context, now time.Time, limit int) ([]*store.ScheduledReport, error)
}

type scheduledReportEnqueuer interface {
	CreateWithTenantID(ctx context.Context, tenantID uuid.UUID, p store.CreateJobParams) (*store.Job, error)
}

// ScheduledReportSweeper enqueues a JobTypeScheduledReportRun job for each due row.
type ScheduledReportSweeper struct {
	jobs     jobProgressTracker
	rows     scheduledReportLister
	enqueue  scheduledReportEnqueuer
	eventBus busPublisher
	now      func() time.Time
	logger   zerolog.Logger
}

func NewScheduledReportSweeper(
	jobs *store.JobStore,
	rows *store.ScheduledReportStore,
	enqueue *store.JobStore,
	eventBus *bus.EventBus,
	logger zerolog.Logger,
) *ScheduledReportSweeper {
	return &ScheduledReportSweeper{
		jobs:     jobs,
		rows:     rows,
		enqueue:  enqueue,
		eventBus: eventBus,
		now:      func() time.Time { return time.Now().UTC() },
		logger:   logger.With().Str("processor", JobTypeScheduledReportSweeper).Logger(),
	}
}

func (p *ScheduledReportSweeper) Type() string {
	return JobTypeScheduledReportSweeper
}

func (p *ScheduledReportSweeper) Process(ctx context.Context, j *store.Job) error {
	due, err := p.rows.ListDue(ctx, p.now(), 100)
	if err != nil {
		return fmt.Errorf("scheduled_report_sweeper: list due: %w", err)
	}

	enqueued := 0
	var errs []string

	for _, row := range due {
		payload, _ := json.Marshal(map[string]string{
			"scheduled_report_id": row.ID.String(),
		})
		newJob, jobErr := p.enqueue.CreateWithTenantID(ctx, row.TenantID, store.CreateJobParams{
			Type:     JobTypeScheduledReportRun,
			Priority: 5,
			Payload:  payload,
		})
		if jobErr != nil {
			errs = append(errs, fmt.Sprintf("schedule=%s: %s", row.ID, jobErr.Error()))
			continue
		}
		if p.eventBus != nil {
			_ = p.eventBus.Publish(ctx, bus.SubjectJobQueue, JobMessage{
				JobID:    newJob.ID,
				TenantID: newJob.TenantID,
				Type:     JobTypeScheduledReportRun,
			})
		}
		enqueued++
	}

	result, _ := json.Marshal(map[string]any{
		"due":      len(due),
		"enqueued": enqueued,
		"errors":   strings.Join(errs, "; "),
	})

	if err := p.jobs.Complete(ctx, j.ID, nil, result); err != nil {
		return fmt.Errorf("scheduled_report_sweeper: complete job: %w", err)
	}
	p.logger.Info().Int("due", len(due)).Int("enqueued", enqueued).Msg("scheduled report sweep done")
	return nil
}

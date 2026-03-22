package job

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type S3Uploader interface {
	Upload(ctx context.Context, bucket, key string, data []byte) error
}

type S3ArchivalProcessor struct {
	jobs           *store.JobStore
	lifecycleStore *store.DataLifecycleStore
	storageStore   *store.StorageMonitorStore
	cdrStore       *store.CDRStore
	s3Uploader     S3Uploader
	eventBus       *bus.EventBus
	defaultBucket  string
	logger         zerolog.Logger
}

func NewS3ArchivalProcessor(
	jobs *store.JobStore,
	lifecycleStore *store.DataLifecycleStore,
	storageStore *store.StorageMonitorStore,
	cdrStore *store.CDRStore,
	s3Uploader S3Uploader,
	eventBus *bus.EventBus,
	defaultBucket string,
	logger zerolog.Logger,
) *S3ArchivalProcessor {
	return &S3ArchivalProcessor{
		jobs:           jobs,
		lifecycleStore: lifecycleStore,
		storageStore:   storageStore,
		cdrStore:       cdrStore,
		s3Uploader:     s3Uploader,
		eventBus:       eventBus,
		defaultBucket:  defaultBucket,
		logger:         logger.With().Str("processor", JobTypeS3Archival).Logger(),
	}
}

func (p *S3ArchivalProcessor) Type() string {
	return JobTypeS3Archival
}

type s3ArchivalPayload struct {
	HypertableName string `json:"hypertable_name"`
	DaysOlderThan  int    `json:"days_older_than"`
}

func (p *S3ArchivalProcessor) Process(ctx context.Context, job *store.Job) error {
	var payload s3ArchivalPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal s3 archival payload: %w", err)
	}

	hypertable := payload.HypertableName
	if hypertable == "" {
		hypertable = "cdrs"
	}
	daysOld := payload.DaysOlderThan
	if daysOld <= 0 {
		daysOld = 365
	}

	olderThan := time.Now().UTC().AddDate(0, 0, -daysOld)

	p.logger.Info().
		Str("job_id", job.ID.String()).
		Str("hypertable", hypertable).
		Time("older_than", olderThan).
		Msg("starting S3 archival")

	configs, err := p.lifecycleStore.ListRetentionConfigs(ctx)
	if err != nil {
		return fmt.Errorf("list retention configs: %w", err)
	}

	enabledTenants := make(map[uuid.UUID]*store.TenantRetentionConfig)
	for i, cfg := range configs {
		if cfg.S3ArchivalEnabled {
			enabledTenants[cfg.TenantID] = &configs[i]
		}
	}

	if len(enabledTenants) == 0 {
		p.logger.Info().Msg("no tenants with S3 archival enabled, skipping")
		result, _ := json.Marshal(map[string]interface{}{
			"status":  "skipped",
			"message": "no tenants with S3 archival enabled",
		})
		return p.jobs.Complete(ctx, job.ID, nil, result)
	}

	totalArchived := 0
	totalFailed := 0

	for tenantID, cfg := range enabledTenants {
		cancelled, checkErr := p.jobs.CheckCancelled(ctx, job.ID)
		if checkErr == nil && cancelled {
			p.logger.Info().Msg("S3 archival cancelled")
			break
		}

		retentionDays := cfg.CDRRetentionDays
		if retentionDays <= 0 {
			retentionDays = daysOld
		}
		tenantOlderThan := time.Now().UTC().AddDate(0, 0, -retentionDays)
		if tenantOlderThan.After(olderThan) {
			tenantOlderThan = olderThan
		}

		bucket := p.defaultBucket
		if cfg.S3ArchivalBucket != nil && *cfg.S3ArchivalBucket != "" {
			bucket = *cfg.S3ArchivalBucket
		}

		archived, failed := p.archiveTenantData(ctx, job, tenantID, hypertable, tenantOlderThan, bucket)
		totalArchived += archived
		totalFailed += failed

		_ = p.jobs.UpdateProgress(ctx, job.ID, totalArchived, totalFailed, totalArchived+totalFailed)
	}

	result, _ := json.Marshal(map[string]interface{}{
		"total_archived": totalArchived,
		"total_failed":   totalFailed,
		"tenants":        len(enabledTenants),
	})

	if err := p.jobs.Complete(ctx, job.ID, nil, result); err != nil {
		return fmt.Errorf("complete s3 archival job: %w", err)
	}

	if p.eventBus != nil {
		_ = p.eventBus.Publish(ctx, bus.SubjectJobCompleted, map[string]interface{}{
			"job_id":         job.ID.String(),
			"tenant_id":     job.TenantID.String(),
			"type":          JobTypeS3Archival,
			"state":         "completed",
			"total_archived": totalArchived,
			"total_failed":   totalFailed,
		})
	}

	return nil
}

func (p *S3ArchivalProcessor) archiveTenantData(ctx context.Context, job *store.Job, tenantID uuid.UUID, hypertable string, olderThan time.Time, bucket string) (int, int) {
	archived := 0
	failed := 0

	chunkStart := olderThan.AddDate(0, -1, 0)
	chunkEnd := olderThan

	s3Key := fmt.Sprintf("archival/%s/%s/%s/%s.csv",
		tenantID.String(),
		hypertable,
		chunkStart.Format("2006-01"),
		chunkEnd.Format("2006-01-02"),
	)

	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	header := []string{
		"id", "session_id", "sim_id", "tenant_id", "operator_id", "apn_id", "rat_type",
		"record_type", "bytes_in", "bytes_out", "duration_sec",
		"usage_cost", "carrier_cost", "rate_per_mb", "rat_multiplier", "timestamp",
	}
	if err := writer.Write(header); err != nil {
		p.logger.Error().Err(err).Msg("write csv header for archival")
		return 0, 1
	}

	rowCount := int64(0)
	err := p.lifecycleStore.StreamCDRsForArchival(ctx, tenantID, chunkStart, chunkEnd, func(c store.CDR) error {
		row := []string{
			fmt.Sprintf("%d", c.ID),
			c.SessionID.String(),
			c.SimID.String(),
			c.TenantID.String(),
			c.OperatorID.String(),
			uuidPtrString(c.APNID),
			stringPtrValue(c.RATType),
			c.RecordType,
			fmt.Sprintf("%d", c.BytesIn),
			fmt.Sprintf("%d", c.BytesOut),
			fmt.Sprintf("%d", c.DurationSec),
			floatPtrString(c.UsageCost),
			floatPtrString(c.CarrierCost),
			floatPtrString(c.RatePerMB),
			floatPtrString(c.RATMultiplier),
			c.Timestamp.Format(time.RFC3339),
		}
		rowCount++
		return writer.Write(row)
	})
	if err != nil {
		p.logger.Error().Err(err).
			Str("tenant_id", tenantID.String()).
			Msg("stream cdrs for archival failed")
		return 0, 1
	}

	writer.Flush()
	if writer.Error() != nil {
		p.logger.Error().Err(writer.Error()).Msg("flush csv for archival")
		return 0, 1
	}

	if rowCount == 0 {
		p.logger.Debug().
			Str("tenant_id", tenantID.String()).
			Msg("no data to archive for tenant")
		return 0, 0
	}

	rec, err := p.lifecycleStore.CreateArchivalRecord(ctx, store.S3ArchivalRecord{
		TenantID:        tenantID,
		TableName:       hypertable,
		ChunkName:       fmt.Sprintf("%s_%s", hypertable, chunkStart.Format("20060102")),
		ChunkRangeStart: chunkStart,
		ChunkRangeEnd:   chunkEnd,
		S3Bucket:        bucket,
		S3Key:           s3Key,
		SizeBytes:       int64(buf.Len()),
		RowCount:        rowCount,
		Status:          "uploading",
	})
	if err != nil {
		p.logger.Error().Err(err).Msg("create archival record")
		return 0, 1
	}

	if p.s3Uploader != nil {
		if uploadErr := p.s3Uploader.Upload(ctx, bucket, s3Key, buf.Bytes()); uploadErr != nil {
			errMsg := uploadErr.Error()
			_ = p.lifecycleStore.UpdateArchivalStatus(ctx, rec.ID, "failed", &errMsg)
			p.logger.Error().Err(uploadErr).
				Str("s3_key", s3Key).
				Msg("S3 upload failed")
			return 0, 1
		}
	}

	_ = p.lifecycleStore.UpdateArchivalStatus(ctx, rec.ID, "completed", nil)

	p.logger.Info().
		Str("tenant_id", tenantID.String()).
		Str("s3_key", s3Key).
		Int64("rows", rowCount).
		Msg("archival completed for tenant")

	archived++
	return archived, failed
}

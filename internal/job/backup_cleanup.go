package job

import (
	"context"
	"fmt"

	"github.com/btopcu/argus/internal/store"
	"github.com/rs/zerolog"
)

// BackupCleanupProcessorOpts holds dependencies for BackupCleanupProcessor.
type BackupCleanupProcessorOpts struct {
	Store            backupCleanupRecorder
	Uploader         BackupS3Client
	Bucket           string
	RetentionDaily   int
	RetentionWeekly  int
	RetentionMonthly int
	Logger           zerolog.Logger
}

// BackupCleanupProcessor deletes old backup files from S3 and marks runs expired.
type BackupCleanupProcessor struct {
	store            backupCleanupRecorder
	uploader         BackupS3Client
	bucket           string
	retentionDaily   int
	retentionWeekly  int
	retentionMonthly int
	logger           zerolog.Logger
}

func NewBackupCleanupProcessor(opts BackupCleanupProcessorOpts) *BackupCleanupProcessor {
	retDaily := opts.RetentionDaily
	if retDaily <= 0 {
		retDaily = 14
	}
	retWeekly := opts.RetentionWeekly
	if retWeekly <= 0 {
		retWeekly = 8
	}
	retMonthly := opts.RetentionMonthly
	if retMonthly <= 0 {
		retMonthly = 12
	}
	return &BackupCleanupProcessor{
		store:            opts.Store,
		uploader:         opts.Uploader,
		bucket:           opts.Bucket,
		retentionDaily:   retDaily,
		retentionWeekly:  retWeekly,
		retentionMonthly: retMonthly,
		logger:           opts.Logger.With().Str("processor", JobTypeBackupCleanup).Logger(),
	}
}

func (p *BackupCleanupProcessor) Type() string {
	return JobTypeBackupCleanup
}

func (p *BackupCleanupProcessor) Process(ctx context.Context, job *store.Job) error {
	type kindRetention struct {
		kind  string
		keepN int
	}
	schedule := []kindRetention{
		{"daily", p.retentionDaily},
		{"weekly", p.retentionWeekly},
		{"monthly", p.retentionMonthly},
	}

	totalDeleted := 0
	var errs []error

	for _, kr := range schedule {
		expiredKeys, err := p.store.ExpireOlderThan(ctx, kr.kind, kr.keepN)
		if err != nil {
			p.logger.Error().Err(err).Str("kind", kr.kind).Msg("expire old backup runs failed")
			errs = append(errs, fmt.Errorf("expire %s: %w", kr.kind, err))
			continue
		}

		for _, key := range expiredKeys {
			if p.uploader != nil {
				if delErr := p.uploader.Delete(ctx, p.bucket, key); delErr != nil {
					p.logger.Error().Err(delErr).Str("key", key).Msg("s3 delete backup failed")
					errs = append(errs, fmt.Errorf("delete %s: %w", key, delErr))
					continue
				}
			}
			totalDeleted++
		}

		p.logger.Info().
			Str("kind", kr.kind).
			Int("keep_n", kr.keepN).
			Int("expired", len(expiredKeys)).
			Msg("backup cleanup completed for kind")
	}

	if len(errs) > 0 {
		return fmt.Errorf("backup_cleanup: %d error(s), first: %w", len(errs), errs[0])
	}

	p.logger.Info().Int("total_deleted", totalDeleted).Msg("backup cleanup finished")
	return nil
}

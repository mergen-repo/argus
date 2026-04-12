package job

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/observability/metrics"
	"github.com/btopcu/argus/internal/store"
	"github.com/rs/zerolog"
)

// BackupS3Client is the S3 interface required by backup processors.
type BackupS3Client interface {
	Upload(ctx context.Context, bucket, key string, data []byte) error
	Download(ctx context.Context, bucket, key string) ([]byte, error)
	Delete(ctx context.Context, bucket, key string) error
}

// backupRecorder is the minimal store interface needed by BackupProcessor.
type backupRecorder interface {
	Record(ctx context.Context, r store.BackupRun) (int64, error)
	MarkSucceeded(ctx context.Context, id int64, finishedAt time.Time, size int64, sha string) error
	MarkFailed(ctx context.Context, id int64, errMsg string) error
}

// backupVerifyRecorder is the minimal store interface needed by BackupVerifyProcessor.
type backupVerifyRecorder interface {
	Latest(ctx context.Context, kind string) (*store.BackupRun, error)
	RecordVerification(ctx context.Context, runID int64, state string, tenants, sims int64, errMsg string) error
}

// backupCleanupRecorder is the minimal store interface needed by BackupCleanupProcessor.
type backupCleanupRecorder interface {
	ExpireOlderThan(ctx context.Context, kind string, keepN int) ([]string, error)
}

// BackupProcessorOpts holds all dependencies for a BackupProcessor.
type BackupProcessorOpts struct {
	Store       *store.BackupStore
	Uploader    BackupS3Client
	Bucket      string
	TempDir     string
	Timeout     time.Duration
	Kind        string
	DatabaseURL string
	Reg         *metrics.Registry
	Logger      zerolog.Logger
	EventBus    *bus.EventBus
}

// BackupProcessor runs pg_dump, uploads to S3, and records the result.
type BackupProcessor struct {
	store       backupRecorder
	uploader    BackupS3Client
	bucket      string
	tempDir     string
	timeout     time.Duration
	kind        string
	databaseURL string
	reg         *metrics.Registry
	logger      zerolog.Logger
	eventBus    *bus.EventBus
}

func NewBackupProcessor(opts BackupProcessorOpts) *BackupProcessor {
	jobType := opts.Kind
	if jobType == "" {
		jobType = "daily"
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	tempDir := opts.TempDir
	if tempDir == "" {
		tempDir = "/tmp"
	}
	return &BackupProcessor{
		store:       opts.Store,
		uploader:    opts.Uploader,
		bucket:      opts.Bucket,
		tempDir:     tempDir,
		timeout:     timeout,
		kind:        jobType,
		databaseURL: opts.DatabaseURL,
		reg:         opts.Reg,
		logger:      opts.Logger.With().Str("processor", "backup").Str("kind", jobType).Logger(),
		eventBus:    opts.EventBus,
	}
}

func (p *BackupProcessor) Type() string {
	switch p.kind {
	case "weekly":
		return JobTypeBackupWeekly
	case "monthly":
		return JobTypeBackupMonthly
	default:
		return JobTypeBackupDaily
	}
}

func (p *BackupProcessor) Process(ctx context.Context, job *store.Job) error {
	now := time.Now().UTC()
	ts := now.Format("20060102T150405Z")
	s3Key := p.kind + "/" + ts + ".dump"

	runID, err := p.store.Record(ctx, store.BackupRun{
		Kind:      p.kind,
		S3Bucket:  p.bucket,
		S3Key:     s3Key,
		StartedAt: now,
	})
	if err != nil {
		return fmt.Errorf("backup: record run: %w", err)
	}

	p.reg.IncBackupRun(p.kind, "started")

	if err := p.runBackup(ctx, runID, s3Key, now); err != nil {
		_ = p.store.MarkFailed(ctx, runID, err.Error())
		p.reg.IncBackupRun(p.kind, "failed")
		return err
	}

	p.reg.IncBackupRun(p.kind, "succeeded")
	return nil
}

func (p *BackupProcessor) runBackup(ctx context.Context, runID int64, s3Key string, started time.Time) error {
	connArgs, env, err := pgDumpArgs(p.databaseURL)
	if err != nil {
		return fmt.Errorf("backup: parse database url: %w", err)
	}

	tmpPath := fmt.Sprintf("%s/pg_backup_%s_%s.dump", p.tempDir, p.kind, started.Format("20060102T150405Z"))
	defer os.Remove(tmpPath)

	dumpCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	args := append([]string{"--format=custom", "--file=" + tmpPath}, connArgs...)
	cmd := exec.CommandContext(dumpCtx, "pg_dump", args...)
	cmd.Env = append(os.Environ(), env...)
	if out, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
		return fmt.Errorf("backup: pg_dump failed: %w: %s", cmdErr, string(out))
	}

	f, err := os.Open(tmpPath)
	if err != nil {
		return fmt.Errorf("backup: open dump file: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("backup: sha256 dump file: %w", err)
	}
	sha := hex.EncodeToString(h.Sum(nil))

	stat, err := f.Stat()
	if err != nil {
		return fmt.Errorf("backup: stat dump file: %w", err)
	}
	size := stat.Size()

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return fmt.Errorf("backup: read dump file: %w", err)
	}

	if p.uploader != nil {
		if err := p.uploader.Upload(ctx, p.bucket, s3Key, data); err != nil {
			return fmt.Errorf("backup: s3 upload: %w", err)
		}
	}

	finishedAt := time.Now().UTC()
	if err := p.store.MarkSucceeded(ctx, runID, finishedAt, size, sha); err != nil {
		return fmt.Errorf("backup: mark succeeded: %w", err)
	}

	p.reg.SetBackupLastSuccess(p.kind, finishedAt)

	if p.eventBus != nil {
		_ = p.eventBus.Publish(ctx, bus.SubjectBackupCompleted, map[string]interface{}{
			"kind":       p.kind,
			"run_id":     runID,
			"s3_bucket":  p.bucket,
			"s3_key":     s3Key,
			"size_bytes": size,
			"sha256":     sha,
		})
	}

	p.logger.Info().
		Int64("run_id", runID).
		Str("s3_key", s3Key).
		Int64("size_bytes", size).
		Msg("backup completed")

	return nil
}

// pgDumpArgs parses DATABASE_URL and returns pg_dump connection flags + env.
func pgDumpArgs(rawURL string) ([]string, []string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, fmt.Errorf("parse database url: %w", err)
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "5432"
	}
	user := u.User.Username()
	password, _ := u.User.Password()
	dbname := u.Path
	if len(dbname) > 0 && dbname[0] == '/' {
		dbname = dbname[1:]
	}

	args := []string{
		"--host=" + host,
		"--port=" + port,
		"--username=" + user,
		"--dbname=" + dbname,
		"--no-password",
	}
	var env []string
	if password != "" {
		env = append(env, "PGPASSWORD="+password)
	}
	return args, env, nil
}

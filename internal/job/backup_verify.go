package job

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog"
)

// BackupVerifyProcessorOpts holds dependencies for BackupVerifyProcessor.
type BackupVerifyProcessorOpts struct {
	Store       backupVerifyRecorder
	Uploader    BackupS3Client
	Bucket      string
	TempDir     string
	Timeout     time.Duration
	DatabaseURL string
	EventBus    *bus.EventBus
	Logger      zerolog.Logger
}

// BackupVerifyProcessor downloads the latest daily backup and smoke-tests it.
type BackupVerifyProcessor struct {
	store       backupVerifyRecorder
	uploader    BackupS3Client
	bucket      string
	tempDir     string
	timeout     time.Duration
	databaseURL string
	eventBus    *bus.EventBus
	logger      zerolog.Logger
}

func NewBackupVerifyProcessor(opts BackupVerifyProcessorOpts) *BackupVerifyProcessor {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	tempDir := opts.TempDir
	if tempDir == "" {
		tempDir = "/tmp"
	}
	return &BackupVerifyProcessor{
		store:       opts.Store,
		uploader:    opts.Uploader,
		bucket:      opts.Bucket,
		tempDir:     tempDir,
		timeout:     timeout,
		databaseURL: opts.DatabaseURL,
		eventBus:    opts.EventBus,
		logger:      opts.Logger.With().Str("processor", JobTypeBackupVerify).Logger(),
	}
}

func (p *BackupVerifyProcessor) Type() string {
	return JobTypeBackupVerify
}

func (p *BackupVerifyProcessor) Process(ctx context.Context, job *store.Job) error {
	run, err := p.store.Latest(ctx, "daily")
	if err != nil {
		return fmt.Errorf("backup_verify: get latest daily run: %w", err)
	}

	if run.State != "succeeded" {
		p.logger.Warn().Str("state", run.State).Msg("latest daily backup not succeeded, skipping verify")
		return nil
	}

	verifyErr := p.verify(ctx, run)
	if verifyErr != nil {
		_ = p.store.RecordVerification(ctx, run.ID, "failed", -1, -1, verifyErr.Error())
		return verifyErr
	}
	return nil
}

func (p *BackupVerifyProcessor) verify(ctx context.Context, run *store.BackupRun) error {
	ts := time.Now().UTC().Format("20060102T150405Z")
	scratchDB := fmt.Sprintf("argus_verify_%s", ts)
	tmpPath := fmt.Sprintf("%s/pg_verify_%s.dump", p.tempDir, ts)
	defer os.Remove(tmpPath)

	data, err := p.uploader.Download(ctx, p.bucket, run.S3Key)
	if err != nil {
		return fmt.Errorf("backup_verify: download backup: %w", err)
	}
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("backup_verify: write tmp file: %w", err)
	}

	adminURL, err := buildAdminURL(p.databaseURL)
	if err != nil {
		return fmt.Errorf("backup_verify: build admin url: %w", err)
	}

	adminConn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return fmt.Errorf("backup_verify: connect admin: %w", err)
	}
	defer adminConn.Close(ctx)

	if _, err := adminConn.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %q`, scratchDB)); err != nil {
		return fmt.Errorf("backup_verify: create scratch db: %w", err)
	}

	scratchURL, err := buildScratchURL(p.databaseURL, scratchDB)
	if err != nil {
		_ = dropScratchDB(ctx, adminConn, scratchDB)
		return fmt.Errorf("backup_verify: build scratch url: %w", err)
	}

	restoreCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	restoreArgs, restoreEnv, err := pgRestoreArgs(p.databaseURL, scratchURL)
	if err != nil {
		_ = dropScratchDB(ctx, adminConn, scratchDB)
		return fmt.Errorf("backup_verify: pg_restore args: %w", err)
	}

	args := append(restoreArgs, tmpPath)
	cmd := exec.CommandContext(restoreCtx, "pg_restore", args...)
	cmd.Env = append(os.Environ(), restoreEnv...)
	if out, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
		_ = dropScratchDB(ctx, adminConn, scratchDB)
		return fmt.Errorf("backup_verify: pg_restore failed: %w: %s", cmdErr, string(out))
	}

	scratchConn, err := pgx.Connect(ctx, scratchURL)
	if err != nil {
		_ = dropScratchDB(ctx, adminConn, scratchDB)
		return fmt.Errorf("backup_verify: connect scratch db: %w", err)
	}
	defer scratchConn.Close(ctx)

	var tenants int64
	if err := scratchConn.QueryRow(ctx, `SELECT COUNT(*) FROM tenants`).Scan(&tenants); err != nil {
		scratchConn.Close(ctx)
		_ = dropScratchDB(ctx, adminConn, scratchDB)
		return fmt.Errorf("backup_verify: count tenants: %w", err)
	}

	var sims int64
	if err := scratchConn.QueryRow(ctx, `SELECT COUNT(*) FROM sims`).Scan(&sims); err != nil {
		scratchConn.Close(ctx)
		_ = dropScratchDB(ctx, adminConn, scratchDB)
		return fmt.Errorf("backup_verify: count sims: %w", err)
	}
	scratchConn.Close(ctx)

	_ = dropScratchDB(ctx, adminConn, scratchDB)

	if err := p.store.RecordVerification(ctx, run.ID, "succeeded", tenants, sims, ""); err != nil {
		return fmt.Errorf("backup_verify: record verification: %w", err)
	}

	if p.eventBus != nil {
		_ = p.eventBus.Publish(ctx, bus.SubjectBackupVerified, map[string]interface{}{
			"run_id":  run.ID,
			"tenants": tenants,
			"sims":    sims,
		})
	}

	p.logger.Info().
		Int64("run_id", run.ID).
		Int64("tenants", tenants).
		Int64("sims", sims).
		Msg("backup verification succeeded")

	return nil
}

func dropScratchDB(ctx context.Context, conn *pgx.Conn, dbName string) error {
	_, err := conn.Exec(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %q WITH (FORCE)`, dbName))
	return err
}

func buildAdminURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	u.Path = "/postgres"
	return u.String(), nil
}

func buildScratchURL(rawURL, dbName string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	u.Path = "/" + dbName
	return u.String(), nil
}

func pgRestoreArgs(rawURL, scratchConnStr string) ([]string, []string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, err
	}
	password, _ := u.User.Password()

	args := []string{
		"--dbname=" + scratchConnStr,
		"--exit-on-error",
		"--no-password",
	}
	var env []string
	if password != "" {
		env = append(env, "PGPASSWORD="+password)
	}
	return args, env, nil
}

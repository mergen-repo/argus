package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrBackupRunNotFound = errors.New("store: backup run not found")

type BackupRun struct {
	ID           int64
	Kind         string
	State        string
	S3Bucket     string
	S3Key        string
	SizeBytes    int64
	SHA256       string
	StartedAt    time.Time
	FinishedAt   *time.Time
	DurationSec  *int
	ErrorMessage string
}

type BackupStore struct {
	pool *pgxpool.Pool
}

func NewBackupStore(pool *pgxpool.Pool) *BackupStore {
	return &BackupStore{pool: pool}
}

func (s *BackupStore) Record(ctx context.Context, r BackupRun) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO backup_runs (kind, state, s3_bucket, s3_key, size_bytes, started_at)
		VALUES ($1, 'running', $2, $3, 0, $4)
		RETURNING id
	`, r.Kind, r.S3Bucket, r.S3Key, r.StartedAt).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("store: record backup run: %w", err)
	}
	return id, nil
}

func (s *BackupStore) MarkSucceeded(ctx context.Context, id int64, finishedAt time.Time, size int64, sha string) error {
	started, durSec, err := s.fetchStartedAt(ctx, id)
	if err != nil {
		return err
	}
	dur := int(finishedAt.Sub(started).Seconds())
	if dur < 0 {
		dur = 0
	}
	durSec = &dur

	_, err = s.pool.Exec(ctx, `
		UPDATE backup_runs
		SET state = 'succeeded', finished_at = $2, size_bytes = $3, sha256 = $4, duration_seconds = $5
		WHERE id = $1
	`, id, finishedAt, size, sha, durSec)
	if err != nil {
		return fmt.Errorf("store: mark backup succeeded: %w", err)
	}
	return nil
}

func (s *BackupStore) MarkFailed(ctx context.Context, id int64, errMsg string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE backup_runs
		SET state = 'failed', finished_at = NOW(), error_message = $2
		WHERE id = $1
	`, id, errMsg)
	if err != nil {
		return fmt.Errorf("store: mark backup failed: %w", err)
	}
	return nil
}

func (s *BackupStore) ListRecent(ctx context.Context, kind string, limit int) ([]BackupRun, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, kind, state, s3_bucket, s3_key, size_bytes, COALESCE(sha256,''),
		       started_at, finished_at, duration_seconds, COALESCE(error_message,'')
		FROM backup_runs
		WHERE kind = $1
		ORDER BY started_at DESC
		LIMIT $2
	`, kind, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list recent backup runs: %w", err)
	}
	defer rows.Close()

	var results []BackupRun
	for rows.Next() {
		var r BackupRun
		if err := rows.Scan(&r.ID, &r.Kind, &r.State, &r.S3Bucket, &r.S3Key,
			&r.SizeBytes, &r.SHA256, &r.StartedAt, &r.FinishedAt, &r.DurationSec, &r.ErrorMessage); err != nil {
			return nil, fmt.Errorf("store: scan backup run: %w", err)
		}
		results = append(results, r)
	}
	return results, nil
}

func (s *BackupStore) Latest(ctx context.Context, kind string) (*BackupRun, error) {
	var r BackupRun
	err := s.pool.QueryRow(ctx, `
		SELECT id, kind, state, s3_bucket, s3_key, size_bytes, COALESCE(sha256,''),
		       started_at, finished_at, duration_seconds, COALESCE(error_message,'')
		FROM backup_runs
		WHERE kind = $1
		ORDER BY started_at DESC
		LIMIT 1
	`, kind).Scan(&r.ID, &r.Kind, &r.State, &r.S3Bucket, &r.S3Key,
		&r.SizeBytes, &r.SHA256, &r.StartedAt, &r.FinishedAt, &r.DurationSec, &r.ErrorMessage)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrBackupRunNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get latest backup run: %w", err)
	}
	return &r, nil
}

// ExpireOlderThan marks old runs as 'expired' and returns their S3 keys.
// It keeps the most recent keepN succeeded runs for the given kind.
func (s *BackupStore) ExpireOlderThan(ctx context.Context, kind string, keepN int) ([]string, error) {
	if keepN < 0 {
		keepN = 0
	}
	rows, err := s.pool.Query(ctx, `
		WITH ranked AS (
			SELECT id, s3_key,
			       ROW_NUMBER() OVER (ORDER BY started_at DESC) AS rn
			FROM backup_runs
			WHERE kind = $1 AND state IN ('succeeded','failed')
		),
		to_expire AS (
			UPDATE backup_runs br
			SET state = 'expired'
			FROM ranked r
			WHERE br.id = r.id AND r.rn > $2
			RETURNING br.s3_key
		)
		SELECT s3_key FROM to_expire
	`, kind, keepN)
	if err != nil {
		return nil, fmt.Errorf("store: expire old backup runs: %w", err)
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("store: scan expired key: %w", err)
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func (s *BackupStore) RecordVerification(ctx context.Context, runID int64, state string, tenants, sims int64, errMsg string) error {
	var tenantsPtr, simsPtr *int64
	if tenants >= 0 {
		tenantsPtr = &tenants
	}
	if sims >= 0 {
		simsPtr = &sims
	}
	var errMsgPtr *string
	if errMsg != "" {
		errMsgPtr = &errMsg
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO backup_verifications (backup_run_id, state, tenants_count, sims_count, error_message)
		VALUES ($1, $2, $3, $4, $5)
	`, runID, state, tenantsPtr, simsPtr, errMsgPtr)
	if err != nil {
		return fmt.Errorf("store: record backup verification: %w", err)
	}
	return nil
}

type BackupVerification struct {
	ID           int64
	BackupRunID  int64
	State        string
	TenantsCount *int64
	SimsCount    *int64
	ErrorMessage *string
	VerifiedAt   time.Time
}

func (s *BackupStore) LatestVerification(ctx context.Context) (*BackupVerification, error) {
	var v BackupVerification
	err := s.pool.QueryRow(ctx, `
		SELECT id, backup_run_id, state, tenants_count, sims_count, error_message, verified_at
		FROM backup_verifications
		ORDER BY verified_at DESC
		LIMIT 1
	`).Scan(&v.ID, &v.BackupRunID, &v.State, &v.TenantsCount, &v.SimsCount, &v.ErrorMessage, &v.VerifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: get latest verification: %w", err)
	}
	return &v, nil
}

func (s *BackupStore) fetchStartedAt(ctx context.Context, id int64) (time.Time, *int, error) {
	var startedAt time.Time
	var durSec *int
	err := s.pool.QueryRow(ctx, `SELECT started_at, duration_seconds FROM backup_runs WHERE id = $1`, id).
		Scan(&startedAt, &durSec)
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("store: fetch started_at for backup run: %w", err)
	}
	return startedAt, durSec, nil
}

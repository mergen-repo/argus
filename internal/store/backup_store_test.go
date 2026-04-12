package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testBackupPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Logf("skip: cannot connect to postgres: %v", err)
		return nil
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Logf("skip: postgres ping failed: %v", err)
		return nil
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func TestBackupStore_NewBackupStore(t *testing.T) {
	s := NewBackupStore(nil)
	if s == nil {
		t.Fatal("expected non-nil BackupStore")
	}
}

func TestBackupRun_Fields(t *testing.T) {
	now := time.Now().UTC()
	dur := 42
	r := BackupRun{
		ID:           1,
		Kind:         "daily",
		State:        "succeeded",
		S3Bucket:     "bucket",
		S3Key:        "daily/20260101T020000Z.dump",
		SizeBytes:    1024,
		SHA256:       "abc123",
		StartedAt:    now,
		FinishedAt:   &now,
		DurationSec:  &dur,
		ErrorMessage: "",
	}
	if r.Kind != "daily" {
		t.Errorf("expected kind=daily, got %s", r.Kind)
	}
	if r.SizeBytes != 1024 {
		t.Errorf("expected size=1024, got %d", r.SizeBytes)
	}
	if *r.DurationSec != 42 {
		t.Errorf("expected duration=42, got %d", *r.DurationSec)
	}
}

func TestBackupStore_ErrNotFound(t *testing.T) {
	if ErrBackupRunNotFound.Error() == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestBackupStore_Integration(t *testing.T) {
	pool := testBackupPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}

	ctx := context.Background()
	s := NewBackupStore(pool)

	now := time.Now().UTC().Truncate(time.Second)
	run := BackupRun{
		Kind:      "daily",
		S3Bucket:  "test-bucket",
		S3Key:     "daily/test.dump",
		StartedAt: now,
	}

	id, err := s.Record(ctx, run)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}

	finishedAt := now.Add(5 * time.Second)
	if err := s.MarkSucceeded(ctx, id, finishedAt, 4096, "deadbeef"); err != nil {
		t.Fatalf("MarkSucceeded: %v", err)
	}

	latest, err := s.Latest(ctx, "daily")
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if latest.ID != id {
		t.Errorf("expected id=%d, got %d", id, latest.ID)
	}
	if latest.State != "succeeded" {
		t.Errorf("expected state=succeeded, got %s", latest.State)
	}
	if latest.SHA256 != "deadbeef" {
		t.Errorf("expected sha256=deadbeef, got %s", latest.SHA256)
	}
	if latest.SizeBytes != 4096 {
		t.Errorf("expected size=4096, got %d", latest.SizeBytes)
	}

	runs, err := s.ListRecent(ctx, "daily", 10)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if len(runs) == 0 {
		t.Fatal("expected at least one run")
	}

	expiredKeys, err := s.ExpireOlderThan(ctx, "daily", 10)
	if err != nil {
		t.Fatalf("ExpireOlderThan: %v", err)
	}
	if len(expiredKeys) != 0 {
		t.Errorf("expected 0 expired (only 1 run, keepN=10), got %d", len(expiredKeys))
	}

	if err := s.RecordVerification(ctx, id, "succeeded", 5, 1000, ""); err != nil {
		t.Fatalf("RecordVerification: %v", err)
	}

	id2, err := s.Record(ctx, BackupRun{
		Kind:      "daily",
		S3Bucket:  "test-bucket",
		S3Key:     "daily/test2.dump",
		StartedAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("Record 2: %v", err)
	}
	if err := s.MarkFailed(ctx, id2, "pg_dump exited 1"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	latestFailed, err := s.Latest(ctx, "daily")
	if err != nil {
		t.Fatalf("Latest after failed: %v", err)
	}
	if latestFailed.State != "failed" {
		t.Errorf("expected state=failed, got %s", latestFailed.State)
	}
}

func TestBackupStore_ExpireRetention(t *testing.T) {
	pool := testBackupPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}

	ctx := context.Background()
	s := NewBackupStore(pool)

	base := time.Now().UTC().Truncate(time.Second)
	kind := "monthly"

	for i := 0; i < 5; i++ {
		id, err := s.Record(ctx, BackupRun{
			Kind:      kind,
			S3Bucket:  "test-bucket",
			S3Key:     "monthly/run" + string(rune('0'+i)) + ".dump",
			StartedAt: base.Add(time.Duration(i) * time.Hour),
		})
		if err != nil {
			t.Fatalf("Record %d: %v", i, err)
		}
		if err := s.MarkSucceeded(ctx, id, base.Add(time.Duration(i)*time.Hour+time.Minute), 100, "sha"+string(rune('0'+i))); err != nil {
			t.Fatalf("MarkSucceeded %d: %v", i, err)
		}
	}

	expiredKeys, err := s.ExpireOlderThan(ctx, kind, 3)
	if err != nil {
		t.Fatalf("ExpireOlderThan: %v", err)
	}
	if len(expiredKeys) != 2 {
		t.Errorf("expected 2 expired, got %d: %v", len(expiredKeys), expiredKeys)
	}
}

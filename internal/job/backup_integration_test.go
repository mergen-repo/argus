// Package job — Backup processor integration tests.
//
// # Requirements
//
// These tests gate on testing.Short() so `make test` (unit-only, -short) skips
// them and stays fast. Run them explicitly with:
//
//	go test ./internal/job/... -run Integration -v -race
//
// The real-database variant additionally requires DATABASE_URL in the environment
// and a reachable PostgreSQL instance. Without it the sub-test is skipped
// automatically.
//
// The fake-S3 variant (no external deps) runs whenever -short is NOT set.
package job

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/store"
)

// captureS3 is a thread-safe fake BackupS3Client that captures uploaded bytes
// so the integration test can verify SHA-256 consistency.
type captureS3 struct {
	mu      sync.Mutex
	uploads map[string][]byte
}

func newCaptureS3() *captureS3 {
	return &captureS3{uploads: make(map[string][]byte)}
}

func (c *captureS3) Upload(_ context.Context, _, key string, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	buf := make([]byte, len(data))
	copy(buf, data)
	c.uploads[key] = buf
	return nil
}

func (c *captureS3) Download(_ context.Context, _, key string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.uploads[key], nil
}

func (c *captureS3) Delete(_ context.Context, _, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.uploads, key)
	return nil
}

func (c *captureS3) Keys() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	keys := make([]string, 0, len(c.uploads))
	for k := range c.uploads {
		keys = append(keys, k)
	}
	return keys
}

// fakeMarkRecorder extends fakeBackupRecorder to capture the SHA-256 passed to
// MarkSucceeded, so the integration test can validate it matches the upload.
type fakeMarkRecorder struct {
	fakeBackupRecorder
	capturedSHA  string
	capturedSize int64
}

func (f *fakeMarkRecorder) MarkSucceeded(_ context.Context, id int64, _ time.Time, size int64, sha string) error {
	f.fakeBackupRecorder.MarkSucceeded(context.Background(), id, time.Now(), size, sha)
	f.capturedSHA = sha
	f.capturedSize = size
	return nil
}

// TestBackupProcessor_Integration_SHA256ConsistencyAndStateSucceeded verifies:
//  1. BackupProcessor runs pg_dump (stubbed via fake binary), uploads to S3.
//  2. The SHA-256 recorded in backup_runs matches the SHA-256 of the uploaded bytes.
//  3. MarkSucceeded is called (state = succeeded).
//  4. The S3 bucket contains exactly one key after Process().
//
// Gate: skip in -short mode (make test uses -short for unit-only runs).
func TestBackupProcessor_Integration_SHA256ConsistencyAndStateSucceeded(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: skipping in short mode")
	}

	installFakePgDump(t, 0)

	s3 := newCaptureS3()
	tmpDir := t.TempDir()
	rec := &fakeMarkRecorder{}

	p := &BackupProcessor{
		store:       rec,
		uploader:    s3,
		bucket:      "argus-backups",
		tempDir:     tmpDir,
		timeout:     30 * time.Second,
		kind:        "daily",
		databaseURL: "postgres://u:p@localhost:5432/db",
		reg:         nil,
		logger:      testLogger(),
	}

	if err := p.Process(context.Background(), &store.Job{}); err != nil {
		t.Fatalf("Process: %v", err)
	}

	// Verify state=succeeded: MarkSucceeded called exactly once, no MarkFailed.
	if len(rec.fakeBackupRecorder.succeededIDs) != 1 {
		t.Errorf("expected 1 MarkSucceeded, got %d", len(rec.fakeBackupRecorder.succeededIDs))
	}
	if len(rec.fakeBackupRecorder.failedIDs) != 0 {
		t.Errorf("expected 0 MarkFailed, got %d: %v",
			len(rec.fakeBackupRecorder.failedIDs), rec.fakeBackupRecorder.failedIDs)
	}

	// Verify exactly one S3 key was written.
	keys := s3.Keys()
	if len(keys) != 1 {
		t.Fatalf("expected 1 S3 key, got %d: %v", len(keys), keys)
	}

	// Verify SHA-256 consistency: hash what landed in S3 and compare against
	// what was passed to MarkSucceeded.
	uploaded := s3.uploads[keys[0]]
	h := sha256.New()
	if _, err := io.Copy(h, bytes.NewReader(uploaded)); err != nil {
		t.Fatalf("sha256 of uploaded bytes: %v", err)
	}
	expectedSHA := hex.EncodeToString(h.Sum(nil))

	if rec.capturedSHA == "" {
		t.Fatal("SHA-256 passed to MarkSucceeded is empty")
	}
	if rec.capturedSHA != expectedSHA {
		t.Errorf("SHA-256 mismatch: MarkSucceeded got %q, computed from S3 upload %q",
			rec.capturedSHA, expectedSHA)
	}
	if rec.capturedSize <= 0 {
		t.Errorf("size passed to MarkSucceeded must be > 0, got %d", rec.capturedSize)
	}
}

// TestBackupProcessor_Integration_NilUploaderSkipsS3 verifies that when no S3
// client is configured (uploader=nil), Process still calls MarkSucceeded — the
// local dump succeeded even though no remote upload happened.
func TestBackupProcessor_Integration_NilUploaderSkipsS3(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: skipping in short mode")
	}

	installFakePgDump(t, 0)

	rec := &fakeMarkRecorder{}
	tmpDir := t.TempDir()

	p := &BackupProcessor{
		store:       rec,
		uploader:    nil,
		bucket:      "argus-backups",
		tempDir:     tmpDir,
		timeout:     30 * time.Second,
		kind:        "weekly",
		databaseURL: "postgres://u:p@localhost:5432/db",
		reg:         nil,
		logger:      testLogger(),
	}

	if err := p.Process(context.Background(), &store.Job{}); err != nil {
		t.Fatalf("Process with nil uploader: %v", err)
	}

	if len(rec.fakeBackupRecorder.succeededIDs) != 1 {
		t.Errorf("expected 1 MarkSucceeded when uploader=nil, got %d",
			len(rec.fakeBackupRecorder.succeededIDs))
	}
}

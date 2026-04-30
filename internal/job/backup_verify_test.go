package job

import (
	"context"
	"testing"

	"github.com/btopcu/argus/internal/store"
)

// fakeVerifyRecorder satisfies backupVerifyRecorder.
type fakeVerifyRecorder struct {
	latestRun          *store.BackupRun
	latestErr          error
	verificationStates []string
	tenantsCounts      []int64
	simsCounts         []int64
}

func (f *fakeVerifyRecorder) Latest(_ context.Context, _ string) (*store.BackupRun, error) {
	if f.latestErr != nil {
		return nil, f.latestErr
	}
	return f.latestRun, nil
}

func (f *fakeVerifyRecorder) RecordVerification(_ context.Context, _ int64, state string, tenants, sims int64, _ string) error {
	f.verificationStates = append(f.verificationStates, state)
	f.tenantsCounts = append(f.tenantsCounts, tenants)
	f.simsCounts = append(f.simsCounts, sims)
	return nil
}

func TestBackupVerifyProcessor_Type(t *testing.T) {
	p := NewBackupVerifyProcessor(BackupVerifyProcessorOpts{})
	if p.Type() != JobTypeBackupVerify {
		t.Errorf("expected %s, got %s", JobTypeBackupVerify, p.Type())
	}
}

func TestBackupVerifyProcessor_Defaults(t *testing.T) {
	p := NewBackupVerifyProcessor(BackupVerifyProcessorOpts{})
	if p.timeout <= 0 {
		t.Errorf("expected positive timeout, got %v", p.timeout)
	}
	if p.tempDir == "" {
		t.Errorf("expected non-empty tempDir")
	}
}

func TestBackupVerifyProcessor_SkipsWhenNotSucceeded(t *testing.T) {
	rec := &fakeVerifyRecorder{
		latestRun: &store.BackupRun{
			ID:    1,
			State: "failed",
			S3Key: "daily/test.dump",
		},
	}
	s3 := &fakeS3{}

	p := &BackupVerifyProcessor{
		store:    rec,
		uploader: s3,
		bucket:   "mybucket",
		tempDir:  t.TempDir(),
		logger:   testLogger(),
	}

	if err := p.Process(context.Background(), &store.Job{}); err != nil {
		t.Fatalf("expected no error on skip, got: %v", err)
	}

	if len(rec.verificationStates) != 0 {
		t.Errorf("expected no verification recorded on skip, got %d", len(rec.verificationStates))
	}
	if len(s3.downloads) != 0 || len(s3.uploads) != 0 {
		t.Errorf("expected no S3 calls on skip")
	}
}

func TestBackupVerifyProcessor_FailsWhenDownloadFails(t *testing.T) {
	rec := &fakeVerifyRecorder{
		latestRun: &store.BackupRun{
			ID:       1,
			State:    "succeeded",
			S3Bucket: "mybucket",
			S3Key:    "daily/test.dump",
		},
	}
	s3 := &fakeS3{}

	p := &BackupVerifyProcessor{
		store:       rec,
		uploader:    s3,
		bucket:      "mybucket",
		tempDir:     t.TempDir(),
		databaseURL: "postgres://u:p@localhost:5432/db",
		logger:      testLogger(),
	}

	err := p.Process(context.Background(), &store.Job{})
	if err == nil {
		t.Fatal("expected error when S3 download fails")
	}

	if len(rec.verificationStates) != 1 {
		t.Fatalf("expected 1 verification recorded, got %d", len(rec.verificationStates))
	}
	if rec.verificationStates[0] != "failed" {
		t.Errorf("expected state=failed, got %s", rec.verificationStates[0])
	}
}

func TestBackupVerifyProcessor_FailsWhenNoDatabaseURL(t *testing.T) {
	rec := &fakeVerifyRecorder{
		latestRun: &store.BackupRun{
			ID:       2,
			State:    "succeeded",
			S3Bucket: "mybucket",
			S3Key:    "daily/test.dump",
		},
	}
	s3 := &fakeS3{
		downloads: map[string][]byte{
			"daily/test.dump": []byte("PGDUMPDATA"),
		},
	}

	p := &BackupVerifyProcessor{
		store:       rec,
		uploader:    s3,
		bucket:      "mybucket",
		tempDir:     t.TempDir(),
		databaseURL: "not-a-url",
		logger:      testLogger(),
	}

	err := p.Process(context.Background(), &store.Job{})
	if err == nil {
		t.Fatal("expected error with invalid databaseURL and real pg_restore attempt")
	}

	if len(rec.verificationStates) != 1 {
		t.Fatalf("expected 1 verification recorded, got %d", len(rec.verificationStates))
	}
	if rec.verificationStates[0] != "failed" {
		t.Errorf("expected state=failed, got %s", rec.verificationStates[0])
	}
}

func TestBuildAdminURL(t *testing.T) {
	cases := []struct {
		rawURL string
		want   string
	}{
		{"postgres://u:p@host:5432/argus", "postgres://u:p@host:5432/postgres"},
		{"postgres://u@host/db?sslmode=disable", "postgres://u@host/postgres?sslmode=disable"},
	}
	for _, tc := range cases {
		got, err := buildAdminURL(tc.rawURL)
		if err != nil {
			t.Fatalf("buildAdminURL(%q): %v", tc.rawURL, err)
		}
		if got != tc.want {
			t.Errorf("buildAdminURL(%q) = %q, want %q", tc.rawURL, got, tc.want)
		}
	}
}

func TestBuildScratchURL(t *testing.T) {
	got, err := buildScratchURL("postgres://u:p@host:5432/argus", "argus_verify_20260101T020000Z")
	if err != nil {
		t.Fatalf("buildScratchURL: %v", err)
	}
	want := "postgres://u:p@host:5432/argus_verify_20260101T020000Z"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

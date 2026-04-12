package job

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/store"
	"github.com/rs/zerolog"
)

// fakeBackupRecorder satisfies the backupRecorder interface without a real DB.
type fakeBackupRecorder struct {
	recorded     []store.BackupRun
	succeededIDs []int64
	failedIDs    []string
	nextID       int64
}

func (f *fakeBackupRecorder) Record(_ context.Context, r store.BackupRun) (int64, error) {
	f.nextID++
	r.ID = f.nextID
	f.recorded = append(f.recorded, r)
	return f.nextID, nil
}

func (f *fakeBackupRecorder) MarkSucceeded(_ context.Context, id int64, _ time.Time, _ int64, _ string) error {
	f.succeededIDs = append(f.succeededIDs, id)
	return nil
}

func (f *fakeBackupRecorder) MarkFailed(_ context.Context, _ int64, msg string) error {
	f.failedIDs = append(f.failedIDs, msg)
	return nil
}

// fakeS3 satisfies BackupS3Client without real S3.
type fakeS3 struct {
	uploads   []string
	downloads map[string][]byte
	deleted   []string
}

func (f *fakeS3) Upload(_ context.Context, _, key string, _ []byte) error {
	f.uploads = append(f.uploads, key)
	return nil
}

func (f *fakeS3) Download(_ context.Context, _, key string) ([]byte, error) {
	if f.downloads == nil {
		return nil, fmt.Errorf("fake s3: no downloads configured")
	}
	data, ok := f.downloads[key]
	if !ok {
		return nil, fmt.Errorf("fake s3: key not found: %s", key)
	}
	return data, nil
}

func (f *fakeS3) Delete(_ context.Context, _, key string) error {
	f.deleted = append(f.deleted, key)
	return nil
}

// testLogger returns a no-op zerolog logger for tests.
func testLogger() zerolog.Logger {
	return zerolog.Nop()
}

// installFakePgDump creates a temporary stub pg_dump script and prepends its
// directory to PATH. When exitCode==0, the stub writes "PGDUMPDATA" to the
// file specified via --file=<path>.
func installFakePgDump(t *testing.T, exitCode int) {
	t.Helper()
	dir := t.TempDir()
	var content string
	if exitCode != 0 {
		content = fmt.Sprintf("#!/bin/sh\nexit %d\n", exitCode)
	} else {
		content = `#!/bin/sh
for arg in "$@"; do
  case "$arg" in
    --file=*) outfile="${arg#--file=}" ;;
  esac
done
printf 'PGDUMPDATA' > "$outfile"
`
	}
	script := filepath.Join(dir, "pg_dump")
	if err := os.WriteFile(script, []byte(content), 0755); err != nil {
		t.Fatalf("write fake pg_dump: %v", err)
	}
	old := os.Getenv("PATH")
	t.Setenv("PATH", dir+":"+old)
}

func TestBackupProcessor_Type(t *testing.T) {
	cases := []struct {
		kind string
		want string
	}{
		{"daily", JobTypeBackupDaily},
		{"weekly", JobTypeBackupWeekly},
		{"monthly", JobTypeBackupMonthly},
		{"", JobTypeBackupDaily},
	}
	for _, tc := range cases {
		p := NewBackupProcessor(BackupProcessorOpts{Kind: tc.kind})
		if p.Type() != tc.want {
			t.Errorf("kind=%q: expected %s, got %s", tc.kind, tc.want, p.Type())
		}
	}
}

func TestBackupProcessor_Defaults(t *testing.T) {
	p := NewBackupProcessor(BackupProcessorOpts{})
	if p.kind != "daily" {
		t.Errorf("expected default kind=daily, got %s", p.kind)
	}
	if p.timeout <= 0 {
		t.Errorf("expected positive timeout, got %v", p.timeout)
	}
	if p.tempDir == "" {
		t.Errorf("expected non-empty tempDir")
	}
}

func TestPgDumpArgs_ParseURL(t *testing.T) {
	cases := []struct {
		url      string
		wantHost string
		wantPort string
		wantUser string
		wantDB   string
		wantEnv  bool
	}{
		{
			url:      "postgres://pguser:pgpass@localhost:5432/argus?sslmode=disable",
			wantHost: "localhost",
			wantPort: "5432",
			wantUser: "pguser",
			wantDB:   "argus",
			wantEnv:  true,
		},
		{
			url:      "postgres://admin@db:5433/mydb",
			wantHost: "db",
			wantPort: "5433",
			wantUser: "admin",
			wantDB:   "mydb",
			wantEnv:  false,
		},
	}
	for _, tc := range cases {
		args, env, err := pgDumpArgs(tc.url)
		if err != nil {
			t.Fatalf("pgDumpArgs(%s): %v", tc.url, err)
		}
		assertArg := func(label, val string) {
			t.Helper()
			for _, a := range args {
				if a == val {
					return
				}
			}
			t.Errorf("pgDumpArgs %s: expected arg %q, got %v", label, val, args)
		}
		assertArg("host", "--host="+tc.wantHost)
		assertArg("port", "--port="+tc.wantPort)
		assertArg("user", "--username="+tc.wantUser)
		assertArg("dbname", "--dbname="+tc.wantDB)
		if tc.wantEnv && len(env) == 0 {
			t.Errorf("expected PGPASSWORD env, got none")
		}
		if !tc.wantEnv && len(env) != 0 {
			t.Errorf("expected no env, got %v", env)
		}
	}
}

func TestBackupProcessor_ProcessSucceeds(t *testing.T) {
	installFakePgDump(t, 0)

	s3 := &fakeS3{}
	tmpDir := t.TempDir()
	fakeStore := &fakeBackupRecorder{}

	p := &BackupProcessor{
		store:       fakeStore,
		uploader:    s3,
		bucket:      "mybucket",
		tempDir:     tmpDir,
		timeout:     30 * time.Second,
		kind:        "daily",
		databaseURL: "postgres://u:p@localhost:5432/db",
		reg:         nil,
		logger:      testLogger(),
	}

	if err := p.Process(context.Background(), &store.Job{}); err != nil {
		t.Fatalf("Process returned unexpected error: %v", err)
	}

	if len(fakeStore.recorded) != 1 {
		t.Errorf("expected 1 Record call, got %d", len(fakeStore.recorded))
	}
	if len(fakeStore.succeededIDs) != 1 {
		t.Errorf("expected 1 MarkSucceeded call, got %d", len(fakeStore.succeededIDs))
	}
	if len(fakeStore.failedIDs) != 0 {
		t.Errorf("expected 0 MarkFailed calls, got %d: %v", len(fakeStore.failedIDs), fakeStore.failedIDs)
	}
	if len(s3.uploads) != 1 {
		t.Errorf("expected 1 S3 upload, got %d", len(s3.uploads))
	}
}

func TestBackupProcessor_ProcessFailsOnPgDump(t *testing.T) {
	installFakePgDump(t, 1)

	s3 := &fakeS3{}
	tmpDir := t.TempDir()
	fakeStore := &fakeBackupRecorder{}

	p := &BackupProcessor{
		store:       fakeStore,
		uploader:    s3,
		bucket:      "mybucket",
		tempDir:     tmpDir,
		timeout:     30 * time.Second,
		kind:        "daily",
		databaseURL: "postgres://u:p@localhost:5432/db",
		reg:         nil,
		logger:      testLogger(),
	}

	err := p.Process(context.Background(), &store.Job{})
	if err == nil {
		t.Fatal("expected error when pg_dump exits non-zero")
	}

	if len(fakeStore.failedIDs) != 1 {
		t.Errorf("expected 1 MarkFailed call, got %d: %v", len(fakeStore.failedIDs), fakeStore.failedIDs)
	}
	if len(s3.uploads) != 0 {
		t.Errorf("expected 0 S3 uploads on pg_dump failure, got %d", len(s3.uploads))
	}
}

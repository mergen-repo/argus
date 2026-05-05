package job

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rs/zerolog"
)

// fakeOrphanRows is a minimal pgx.Rows implementation for testing OrphanSessionDetector.
type fakeOrphanRows struct {
	rows []struct {
		tenantID string
		count    int
	}
	pos int
	err error
}

func (r *fakeOrphanRows) Close()                                       {}
func (r *fakeOrphanRows) Err() error                                   { return r.err }
func (r *fakeOrphanRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeOrphanRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeOrphanRows) Next() bool {
	if r.pos < len(r.rows) {
		r.pos++
		return true
	}
	return false
}
func (r *fakeOrphanRows) Scan(dest ...any) error {
	row := r.rows[r.pos-1]
	if len(dest) >= 2 {
		if s, ok := dest[0].(*string); ok {
			*s = row.tenantID
		}
		if n, ok := dest[1].(*int); ok {
			*n = row.count
		}
	}
	return nil
}
func (r *fakeOrphanRows) Values() ([]any, error) { return nil, nil }
func (r *fakeOrphanRows) RawValues() [][]byte    { return nil }
func (r *fakeOrphanRows) Conn() *pgx.Conn        { return nil }

// fakeSessionQuerier implements sessionQuerier using fakeOrphanRows.
type fakeSessionQuerier struct {
	rows     *fakeOrphanRows
	queryErr error
}

func (f *fakeSessionQuerier) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return f.rows, nil
}

// newTestOrphanDetector constructs a detector wired to a fake querier with a log buffer.
func newTestOrphanDetector(q sessionQuerier, buf *bytes.Buffer) *OrphanSessionDetector {
	logger := zerolog.New(buf)
	return &OrphanSessionDetector{
		db:     q,
		logger: logger,
	}
}

func TestOrphanSessionDetector_LogsWarning(t *testing.T) {
	rows := &fakeOrphanRows{
		rows: []struct {
			tenantID string
			count    int
		}{
			{tenantID: "tenant-abc", count: 5},
		},
	}
	q := &fakeSessionQuerier{rows: rows}

	var buf bytes.Buffer
	d := newTestOrphanDetector(q, &buf)

	if err := d.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	out := buf.String()
	if !bytes.Contains([]byte(out), []byte("orphan sessions detected")) {
		t.Errorf("expected 'orphan sessions detected' in log, got: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("tenant-abc")) {
		t.Errorf("expected tenant_id in log, got: %s", out)
	}
}

func TestOrphanSessionDetector_NoOrphans(t *testing.T) {
	rows := &fakeOrphanRows{}
	q := &fakeSessionQuerier{rows: rows}

	var buf bytes.Buffer
	d := newTestOrphanDetector(q, &buf)

	if err := d.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	out := buf.String()
	if bytes.Contains([]byte(out), []byte("orphan sessions detected")) {
		t.Errorf("expected no warning log when no orphans, got: %s", out)
	}
}

func TestOrphanSessionDetector_QueryError(t *testing.T) {
	q := &fakeSessionQuerier{queryErr: errors.New("db down")}

	var buf bytes.Buffer
	d := newTestOrphanDetector(q, &buf)

	err := d.Run(context.Background())
	if err == nil {
		t.Fatal("expected error from Run when query fails, got nil")
	}
}

func TestResolveOrphanSessionInterval(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want time.Duration
	}{
		{"default_when_unset", "", 30 * time.Minute},
		{"override_valid_15m", "15m", 15 * time.Minute},
		{"override_valid_1h", "1h", 1 * time.Hour},
		{"reject_invalid_falls_back", "not-a-duration", 30 * time.Minute},
		{"reject_zero_falls_back", "0s", 30 * time.Minute},
		{"reject_negative_falls_back", "-5m", 30 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ORPHAN_SESSION_CHECK_INTERVAL", tt.env)
			if got := resolveOrphanSessionInterval(); got != tt.want {
				t.Errorf("resolveOrphanSessionInterval() with env=%q = %v, want %v", tt.env, got, tt.want)
			}
		})
	}
}

func TestOrphanSessionDetector_MultiTenant(t *testing.T) {
	rows := &fakeOrphanRows{
		rows: []struct {
			tenantID string
			count    int
		}{
			{tenantID: "tenant-1", count: 3},
			{tenantID: "tenant-2", count: 7},
		},
	}
	q := &fakeSessionQuerier{rows: rows}

	var buf bytes.Buffer
	d := newTestOrphanDetector(q, &buf)

	if err := d.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	out := buf.String()
	if !bytes.Contains([]byte(out), []byte("tenant-1")) {
		t.Errorf("expected tenant-1 in log, got: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("tenant-2")) {
		t.Errorf("expected tenant-2 in log, got: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("10")) {
		t.Errorf("expected total_count=10 in log, got: %s", out)
	}
}

package job

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rs/zerolog"
)

// fakeExec captures every SQL statement passed to Exec so tests can assert on
// the actual DDL the partition creator issues. It also lets tests inject a
// synthetic error to exercise the wrapped-error path.
type fakeExec struct {
	stmts []string
	err   error
	// errAfter — if > 0, only the Nth call returns err; preceding calls succeed.
	errAfter int
	calls    int
}

func (f *fakeExec) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	f.calls++
	f.stmts = append(f.stmts, sql)
	if f.err != nil {
		if f.errAfter == 0 || f.calls == f.errAfter {
			return pgconn.CommandTag{}, f.err
		}
	}
	return pgconn.CommandTag{}, nil
}

func newTestCreator(db dbExec) *PartitionCreator {
	return newPartitionCreatorFromExec(db, zerolog.Nop())
}

func TestPartitionCreator_Type(t *testing.T) {
	p := &PartitionCreatorProcessor{}
	if p.Type() != JobTypePartitionCreate {
		t.Errorf("expected type %q, got %q", JobTypePartitionCreate, p.Type())
	}
	if JobTypePartitionCreate != "partition_create" {
		t.Errorf("JobTypePartitionCreate changed unexpectedly: %q", JobTypePartitionCreate)
	}
}

func TestPartitionCreator_Run_IssuesOneCreateTablePerMonthPerParent(t *testing.T) {
	fake := &fakeExec{}
	pc := newTestCreator(fake)

	if err := pc.Run(context.Background(), 3); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	// monthsAhead=3 → month offsets {0,1,2,3} = 4 per parent × 2 parents = 8.
	expected := 8
	if len(fake.stmts) != expected {
		t.Fatalf("expected %d CREATE TABLE statements, got %d:\n%s",
			expected, len(fake.stmts), strings.Join(fake.stmts, "\n"))
	}

	// Every statement must be idempotent (IF NOT EXISTS), typed as PARTITION OF,
	// and name a parent from the configured list.
	for i, s := range fake.stmts {
		if !strings.Contains(s, "CREATE TABLE IF NOT EXISTS") {
			t.Errorf("stmt[%d] missing IF NOT EXISTS: %s", i, s)
		}
		if !strings.Contains(s, "PARTITION OF") {
			t.Errorf("stmt[%d] missing PARTITION OF: %s", i, s)
		}
		if !strings.Contains(s, "FOR VALUES FROM (") || !strings.Contains(s, ") TO (") {
			t.Errorf("stmt[%d] missing FOR VALUES clause: %s", i, s)
		}
	}

	// First 4 statements belong to the first parent (audit_logs), next 4 to
	// the second (sim_state_history) — verifies parent iteration order.
	for i := 0; i < 4; i++ {
		if !strings.Contains(fake.stmts[i], `"audit_logs_`) {
			t.Errorf("stmt[%d] expected audit_logs partition, got: %s", i, fake.stmts[i])
		}
	}
	for i := 4; i < 8; i++ {
		if !strings.Contains(fake.stmts[i], `"sim_state_history_`) {
			t.Errorf("stmt[%d] expected sim_state_history partition, got: %s", i, fake.stmts[i])
		}
	}

	// The first audit_logs statement should cover the CURRENT month. Verify by
	// recomputing the expected name from time.Now.
	now := time.Now().UTC()
	wantFirst := fmt.Sprintf(`"audit_logs_%04d_%02d"`, now.Year(), int(now.Month()))
	if !strings.Contains(fake.stmts[0], wantFirst) {
		t.Errorf("first stmt did not target current month %s: %s", wantFirst, fake.stmts[0])
	}
}

func TestPartitionCreator_Run_ZeroMonthsAheadStillCreatesCurrent(t *testing.T) {
	fake := &fakeExec{}
	pc := newTestCreator(fake)

	if err := pc.Run(context.Background(), 0); err != nil {
		t.Fatalf("Run(0) unexpected error: %v", err)
	}
	// monthsAhead=0 → 1 offset × 2 parents = 2 statements.
	if len(fake.stmts) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(fake.stmts))
	}
}

func TestPartitionCreator_Run_NegativeMonthsAheadRejected(t *testing.T) {
	fake := &fakeExec{}
	pc := newTestCreator(fake)

	err := pc.Run(context.Background(), -1)
	if err == nil {
		t.Fatal("expected error for negative monthsAhead, got nil")
	}
	if !strings.Contains(err.Error(), "monthsAhead must be >= 0") {
		t.Errorf("error should mention the validation rule: %v", err)
	}
	if len(fake.stmts) != 0 {
		t.Errorf("no statements should be issued on validation failure, got %d", len(fake.stmts))
	}
}

func TestPartitionCreator_Run_WrapsDBErrorWithContext(t *testing.T) {
	sentinel := errors.New("simulated pg error")
	fake := &fakeExec{err: sentinel}
	pc := newTestCreator(fake)

	err := pc.Run(context.Background(), 3)
	if err == nil {
		t.Fatal("expected error from Run, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected wrapped sentinel error, got: %v", err)
	}
	// Error should include the partition name and date range for triage.
	msg := err.Error()
	if !strings.Contains(msg, "partition_creator: ensure") {
		t.Errorf("error missing wrapping prefix: %v", err)
	}
	if !strings.Contains(msg, "audit_logs_") {
		t.Errorf("error missing partition name: %v", err)
	}
	// On first failure Run stops early — so only ONE statement was attempted.
	if len(fake.stmts) != 1 {
		t.Errorf("expected fail-fast after 1 stmt, got %d", len(fake.stmts))
	}
}

func TestPartitionCreator_Run_FailFastOnFifthCallReportsCorrectPartition(t *testing.T) {
	sentinel := errors.New("boom")
	fake := &fakeExec{err: sentinel, errAfter: 5}
	pc := newTestCreator(fake)

	err := pc.Run(context.Background(), 3)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected wrapped sentinel, got: %v", err)
	}
	// 5th call is the FIRST sim_state_history partition (current month).
	if len(fake.stmts) != 5 {
		t.Fatalf("expected exactly 5 stmts attempted, got %d", len(fake.stmts))
	}
	if !strings.Contains(err.Error(), "sim_state_history_") {
		t.Errorf("expected error to name sim_state_history partition: %v", err)
	}
}

func TestPartitionName_FormatMatchesBootstrapConvention(t *testing.T) {
	cases := []struct {
		parent string
		in     time.Time
		want   string
	}{
		{"audit_logs", time.Date(2026, time.July, 15, 0, 0, 0, 0, time.UTC), "audit_logs_2026_07"},
		{"audit_logs", time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC), "audit_logs_2027_01"},
		{"sim_state_history", time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC), "sim_state_history_2026_12"},
	}
	for _, tc := range cases {
		got := partitionName(tc.parent, tc.in)
		if got != tc.want {
			t.Errorf("partitionName(%q, %s) = %q, want %q", tc.parent, tc.in.Format("2006-01-02"), got, tc.want)
		}
		if !partitionNameRE.MatchString(got) {
			t.Errorf("generated name %q fails regex validation", got)
		}
	}
}

func TestPartitionNameRegex_RejectsInjectionAttempts(t *testing.T) {
	bad := []string{
		"",
		"audit_logs",                     // missing date suffix
		"audit_logs_2026",                // missing month
		"audit_logs_26_07",               // 2-digit year
		"audit_logs_2026_7",              // 1-digit month
		"audit_logs_2026_07; DROP TABLE", // injection
		"AUDIT_LOGS_2026_07",             // uppercase
		"audit-logs_2026_07",             // hyphen
		"1audit_logs_2026_07",            // leading digit
	}
	for _, name := range bad {
		if partitionNameRE.MatchString(name) {
			t.Errorf("regex should have rejected %q", name)
		}
	}
}

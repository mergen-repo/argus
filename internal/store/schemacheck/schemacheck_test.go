package schemacheck

import (
	"context"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestVerify_EmptyManifestSucceeds(t *testing.T) {
	if err := Verify(context.Background(), nil, nil); err != nil {
		t.Fatalf("Verify with nil tables: unexpected error: %v", err)
	}
	if err := Verify(context.Background(), nil, []string{}); err != nil {
		t.Fatalf("Verify with empty tables: unexpected error: %v", err)
	}
}

func TestCriticalTables_CountAndOrder(t *testing.T) {
	if got := len(CriticalTables); got != 12 {
		t.Fatalf("CriticalTables length: got %d, want 12 (7 STORY-069 + 5 STORY-077)", got)
	}
	if !sort.StringsAreSorted(CriticalTables) {
		t.Fatalf("CriticalTables is not sorted: %v", CriticalTables)
	}
	required := []string{"sms_outbound", "user_views", "announcements", "webhook_configs"}
	for _, name := range required {
		if !contains(CriticalTables, name) {
			t.Errorf("CriticalTables missing required entry %q", name)
		}
	}
}

func contains(ss []string, target string) bool {
	for _, s := range ss {
		if s == target {
			return true
		}
	}
	return false
}

// TestVerify_MissingTableReportsError exercises the !exists branch of Verify
// using a synthetic, definitely-absent table name so no DDL side-effects are
// required. Skips when DATABASE_URL is unset, following the project's
// integration-test convention (see internal/store/sms_outbound_test.go).
// STORY-086 Gate F-A6.
func TestVerify_MissingTableReportsError(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("cannot connect to postgres: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("postgres ping failed: %v", err)
	}

	const bogus = "_schemacheck_missing_test_table_"
	err = Verify(ctx, pool, []string{bogus})
	if err == nil {
		t.Fatalf("Verify(bogus table): expected error, got nil")
	}
	if !strings.Contains(err.Error(), bogus) {
		t.Fatalf("Verify error must name the missing table %q, got: %v", bogus, err)
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("Verify error must mention 'missing', got: %v", err)
	}
}

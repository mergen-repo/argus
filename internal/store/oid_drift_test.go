package store

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestPgxPoolSurvivesDDLAfterFirstQuery is the FIX-301 reproduction test.
//
// Setup: open production-config pool (Layer 2 fix applied — DefaultQueryExecMode
// = QueryExecModeExec), pin connection A, run a SELECT (would cache stmt+OID
// under default exec mode). On separate connection B, DROP+CREATE the table —
// new OID. Re-run SELECT on connection A.
//
// BEFORE Layer 2 fix: connection A fails with "could not open relation with
// OID <stale>" (SQLSTATE XX000).
// AFTER Layer 2 fix: connection A succeeds (no OID pinning).
//
// Skips when DATABASE_URL is unset (matches existing store test pattern).
func TestPgxPoolSurvivesDDLAfterFirstQuery(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping OID drift test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pg, err := NewPostgres(ctx, dsn, 4, 1, 5*time.Minute)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pg.Close()

	const tableName = "oid_drift_test_t"
	_, _ = pg.Pool.Exec(ctx, "DROP TABLE IF EXISTS "+tableName)

	if _, err := pg.Pool.Exec(ctx, "CREATE TABLE "+tableName+" (id INT, val TEXT)"); err != nil {
		t.Fatalf("create initial table: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pg.Pool.Exec(context.Background(), "DROP TABLE IF EXISTS "+tableName)
	})

	if _, err := pg.Pool.Exec(ctx, "INSERT INTO "+tableName+" (id, val) VALUES (1, 'before')"); err != nil {
		t.Fatalf("seed initial row: %v", err)
	}

	// Acquire connection A and run the SELECT — this would cache the
	// prepared statement + relation OID under the default exec mode.
	connA, err := pg.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connA: %v", err)
	}

	var id int
	var val string
	row := connA.QueryRow(ctx, "SELECT id, val FROM "+tableName+" WHERE id = $1", 1)
	if err := row.Scan(&id, &val); err != nil {
		connA.Release()
		t.Fatalf("first select on connA: %v", err)
	}
	if id != 1 || val != "before" {
		connA.Release()
		t.Fatalf("unexpected first-select result: id=%d val=%q", id, val)
	}

	// On separate connection B (acquired distinctly from the pool), DROP
	// the table and re-CREATE it — new relation OID. Connection A's
	// caches (if exec mode pins them) now reference a stale OID.
	connB, err := pg.Pool.Acquire(ctx)
	if err != nil {
		connA.Release()
		t.Fatalf("acquire connB: %v", err)
	}
	if _, err := connB.Exec(ctx, "DROP TABLE "+tableName); err != nil {
		connA.Release()
		connB.Release()
		t.Fatalf("drop table on connB: %v", err)
	}
	if _, err := connB.Exec(ctx, "CREATE TABLE "+tableName+" (id INT, val TEXT)"); err != nil {
		connA.Release()
		connB.Release()
		t.Fatalf("recreate table on connB: %v", err)
	}
	if _, err := connB.Exec(ctx, "INSERT INTO "+tableName+" (id, val) VALUES (1, 'after')"); err != nil {
		connA.Release()
		connB.Release()
		t.Fatalf("seed re-created table on connB: %v", err)
	}
	connB.Release()

	// Re-run the SELECT on connection A. The crux of the bug.
	row = connA.QueryRow(ctx, "SELECT id, val FROM "+tableName+" WHERE id = $1", 1)
	err = row.Scan(&id, &val)
	connA.Release()

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if strings.Contains(pgErr.Message, "could not open relation with OID") {
				t.Fatalf("FIX-301 regression: stale OID error returned (SQLSTATE %s): %s", pgErr.Code, pgErr.Message)
			}
		}
		t.Fatalf("re-select on connA: %v", err)
	}
	if id != 1 || val != "after" {
		t.Fatalf("unexpected re-select result: id=%d val=%q (want id=1 val=\"after\")", id, val)
	}
}

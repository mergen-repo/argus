package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5"
)

// testDBName is the name of the disposable PostgreSQL database created by
// each STORY-087 test. Declared as a package-level constant so the CREATE
// and the per-test DSN resolver cannot drift out of sync.
const testDBName = "argus_story087_freshvol_test"

// migrationsDir returns the absolute path to the migrations/ directory in the
// project root. Uses runtime.Caller so the path is correct regardless of how
// `go test` is invoked (go test ./..., IDE, etc.).
func migrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	// this file is at internal/store/migration_freshvol_test.go
	// go up two directories to reach the project root
	dir := filepath.Join(filepath.Dir(file), "..", "..", "migrations")
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	return abs
}

// headVersion scans migrations/*.up.sql, parses the numeric timestamp prefix
// from each filename, and returns the maximum — the expected head version after
// m.Up() completes. Does NOT hard-code a version so future story additions do
// not break these tests.
func headVersion(t *testing.T, migrDir string) uint {
	t.Helper()
	entries, err := os.ReadDir(migrDir)
	if err != nil {
		t.Fatalf("ReadDir migrations: %v", err)
	}
	var versions []uint
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		parts := strings.SplitN(name, "_", 2)
		if len(parts) < 2 {
			continue
		}
		n, err := strconv.ParseUint(parts[0], 10, 64)
		if err != nil {
			continue
		}
		versions = append(versions, uint(n))
	}
	if len(versions) == 0 {
		t.Fatal("no .up.sql files found in migrations/")
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
	return versions[len(versions)-1]
}

// setupFreshDB creates a disposable PostgreSQL database derived from
// DATABASE_URL, runs no migrations (blank slate), and returns a configured
// *migrate.Migrate plus a cleanup function that drops the disposable DB.
//
// Callers are responsible for running m.Up() / m.Down() as needed.
func setupFreshDB(t *testing.T) (*migrate.Migrate, func()) {
	t.Helper()

	adminDSN := os.Getenv("DATABASE_URL")
	if adminDSN == "" {
		t.Skip("no test database available (set DATABASE_URL)")
	}

	// Build an admin DSN pointing at the default "postgres" maintenance DB so
	// we can CREATE / DROP the test database (cannot drop the DB we are
	// connected to, and DATABASE_URL may point at "argus").
	adminConnDSN, err := swapDBName(adminDSN, "postgres")
	if err != nil {
		t.Fatalf("build admin DSN: %v", err)
	}

	ctx := context.Background()

	// Create disposable database. `DROP DATABASE ... WITH (FORCE)` terminates
	// competing sessions before dropping and requires PostgreSQL ≥ 13. Argus
	// runs on PG 16 (see deploy/docker-compose.yml), so this is safe.
	adminConn, err := pgx.Connect(ctx, adminConnDSN)
	if err != nil {
		t.Fatalf("connect admin: %v", err)
	}
	_, _ = adminConn.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", testDBName))
	if _, err := adminConn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", testDBName)); err != nil {
		adminConn.Close(ctx)
		t.Fatalf("CREATE DATABASE %s: %v", testDBName, err)
	}
	adminConn.Close(ctx)

	// Build per-test DSN pointing at the disposable DB.
	testDSN, err := swapDBName(adminDSN, testDBName)
	if err != nil {
		t.Fatalf("build test DSN: %v", err)
	}

	migrDir := migrationsDir(t)
	srcURL := "file://" + migrDir

	m, err := migrate.New(srcURL, testDSN)
	if err != nil {
		// Cleanup before failing.
		dropDisposableDB(t, adminConnDSN, testDBName)
		t.Fatalf("migrate.New: %v", err)
	}

	cleanup := func() {
		m.Close()
		dropDisposableDB(t, adminConnDSN, testDBName)
	}
	return m, cleanup
}

// dropDisposableDB drops the test database via the admin connection.
func dropDisposableDB(t *testing.T, adminDSN, dbName string) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		t.Logf("warn: could not connect to drop %s: %v", dbName, err)
		return
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", dbName)); err != nil {
		t.Logf("warn: DROP DATABASE %s: %v", dbName, err)
	}
}

// swapDBName replaces the database name portion of a postgres DSN.
// Handles both URL-style (postgres://user:pass@host:port/dbname?...) and
// keyword-value style (host=... dbname=... ...).
func swapDBName(dsn, newDB string) (string, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		// Find the path segment: after the third slash (host part) up to '?' or end.
		// postgres://user:pass@host:port/DBNAME?params
		schemeEnd := strings.Index(dsn, "://")
		if schemeEnd < 0 {
			return "", fmt.Errorf("invalid DSN: %s", dsn)
		}
		rest := dsn[schemeEnd+3:] // user:pass@host:port/DBNAME?params
		slashIdx := strings.Index(rest, "/")
		if slashIdx < 0 {
			return dsn + "/" + newDB, nil
		}
		authority := rest[:slashIdx+1] // user:pass@host:port/
		tail := rest[slashIdx+1:]      // DBNAME?params
		qIdx := strings.Index(tail, "?")
		var params string
		if qIdx >= 0 {
			params = tail[qIdx:] // ?params
		}
		return dsn[:schemeEnd+3] + authority + newDB + params, nil
	}
	// keyword-value style: replace dbname=... or append
	parts := strings.Fields(dsn)
	found := false
	for i, p := range parts {
		if strings.HasPrefix(p, "dbname=") {
			parts[i] = "dbname=" + newDB
			found = true
			break
		}
	}
	if !found {
		parts = append(parts, "dbname="+newDB)
	}
	return strings.Join(parts, " "), nil
}

// TestFreshVolumeBootstrap_STORY087 verifies that a fresh empty database can
// be migrated to head without error, that sms_outbound exists with the correct
// column set (no FK on sim_id), and that the check_sim_exists trigger is
// installed — confirming that the pre-069 shim (20260412999999) neutralises
// the broken FK in 20260413000001:144.
func TestFreshVolumeBootstrap_STORY087(t *testing.T) {
	m, cleanup := setupFreshDB(t)
	t.Cleanup(cleanup)

	migrDir := migrationsDir(t)
	want := headVersion(t, migrDir)

	// Run migrations to head.
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("m.Up() failed: %v", err)
	}

	// Assert version matches the head computed from the filesystem.
	got, dirty, err := m.Version()
	if err != nil {
		t.Fatalf("m.Version() error: %v", err)
	}
	if dirty {
		t.Fatalf("schema_migrations dirty=true after Up()")
	}
	if got != want {
		t.Fatalf("version: got %d, want %d", got, want)
	}

	// Open a direct connection to query the disposable DB.
	testDSN := disposableDSN(t)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, testDSN)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}
	defer conn.Close(ctx)

	// AC-3: sms_outbound table exists.
	var regclass *string
	if err := conn.QueryRow(ctx, "SELECT to_regclass('public.sms_outbound')::text").Scan(&regclass); err != nil {
		t.Fatalf("to_regclass query: %v", err)
	}
	if regclass == nil {
		t.Fatal("sms_outbound does not exist after migration")
	}

	// AC-3: exact 12-column schema — assert names and nullability in ordinal
	// order. Guards against column drift between the STORY-087 shim
	// (20260412999999) and the STORY-086 authoritative spec
	// (20260417000004_sms_outbound_recover.up.sql:23-36).
	expectedCols := []struct {
		name     string
		nullable string
	}{
		{"id", "NO"},
		{"tenant_id", "NO"},
		{"sim_id", "NO"},
		{"msisdn", "NO"},
		{"text_hash", "NO"},
		{"text_preview", "YES"},
		{"status", "NO"},
		{"provider_message_id", "YES"},
		{"error_code", "YES"},
		{"queued_at", "NO"},
		{"sent_at", "YES"},
		{"delivered_at", "YES"},
	}
	rows, err := conn.Query(ctx,
		`SELECT column_name, is_nullable FROM information_schema.columns
		 WHERE table_schema='public' AND table_name='sms_outbound'
		 ORDER BY ordinal_position`,
	)
	if err != nil {
		t.Fatalf("information_schema.columns query: %v", err)
	}
	var gotCols []struct {
		name     string
		nullable string
	}
	for rows.Next() {
		var name, nullable string
		if err := rows.Scan(&name, &nullable); err != nil {
			rows.Close()
			t.Fatalf("scan column: %v", err)
		}
		gotCols = append(gotCols, struct {
			name     string
			nullable string
		}{name, nullable})
	}
	rows.Close()
	if len(gotCols) != len(expectedCols) {
		t.Fatalf("sms_outbound column count: got %d, want %d (got=%v)", len(gotCols), len(expectedCols), gotCols)
	}
	for i, want := range expectedCols {
		if gotCols[i].name != want.name || gotCols[i].nullable != want.nullable {
			t.Fatalf("sms_outbound column[%d]: got (%s, nullable=%s), want (%s, nullable=%s)",
				i, gotCols[i].name, gotCols[i].nullable, want.name, want.nullable)
		}
	}

	// AC-4: only one FK on sms_outbound (tenant_id → tenants), no FK on sim_id.
	var fkCount int
	if err := conn.QueryRow(ctx,
		"SELECT COUNT(*) FROM pg_constraint WHERE contype='f' AND conrelid='sms_outbound'::regclass",
	).Scan(&fkCount); err != nil {
		t.Fatalf("pg_constraint query: %v", err)
	}
	if fkCount != 1 {
		t.Fatalf("FK count on sms_outbound: got %d, want 1 (only tenant_id FK)", fkCount)
	}

	// AC-5: check_sim_exists trigger is installed and enabled (tgenabled='O'
	// = "origin — always fires").
	var tgname, tgenabled string
	if err := conn.QueryRow(ctx,
		`SELECT tgname, tgenabled FROM pg_trigger
		 WHERE tgrelid='sms_outbound'::regclass
		   AND tgname='trg_sms_outbound_check_sim'`,
	).Scan(&tgname, &tgenabled); err != nil {
		t.Fatalf("pg_trigger query: %v", err)
	}
	if tgenabled != "O" {
		t.Fatalf("trg_sms_outbound_check_sim tgenabled: got %q, want %q", tgenabled, "O")
	}

	// AC-5: smoke insert — the trigger raises SQLSTATE 23503 when sim_id
	// does not exist in sims. We need a valid tenant_id so the tenant FK
	// does not short-circuit the check; seed one inside a savepoint so
	// state does not bleed into subsequent assertions.
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin savepoint tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var tenantID string
	if err := tx.QueryRow(ctx,
		`INSERT INTO tenants (name, contact_email)
		 VALUES ('story087-trigger-smoke', 'smoke@story087.test')
		 RETURNING id`,
	).Scan(&tenantID); err != nil {
		t.Fatalf("seed tenant for trigger smoke: %v", err)
	}
	_, insertErr := tx.Exec(ctx,
		`INSERT INTO sms_outbound (tenant_id, sim_id, msisdn, text_hash)
		 VALUES ($1, '00000000-0000-0000-0000-000000000000', '+905550000000', 'abc')`,
		tenantID,
	)
	if insertErr == nil {
		t.Fatal("expected SQLSTATE 23503 (FK violation via trigger) on sms_outbound insert with bogus sim_id, got nil")
	}
	// The trigger raises via RAISE ... USING ERRCODE = 'foreign_key_violation'.
	// pgx surfaces the SQLSTATE on *pgconn.PgError — but since we already
	// captured an error, match by SQLSTATE string substring to avoid
	// importing pgconn and to tolerate driver wrapping.
	if !strings.Contains(insertErr.Error(), "23503") &&
		!strings.Contains(strings.ToLower(insertErr.Error()), "does not exist") {
		t.Fatalf("expected FK-violation trigger error (SQLSTATE 23503), got: %v", insertErr)
	}
	_ = tx.Rollback(ctx)

	// AC-6: three named indexes installed by 20260413000001:155-157 are present.
	//       (The PK index on `id` is not asserted by name — focus on the
	//       three non-PK indexes from the STORY-069 migration.)
	expectedIdx := []string{
		"idx_sms_outbound_provider_id",
		"idx_sms_outbound_status",
		"idx_sms_outbound_tenant_sim_time",
	}
	idxRows, err := conn.Query(ctx,
		`SELECT indexname FROM pg_indexes
		 WHERE schemaname='public' AND tablename='sms_outbound'
		   AND indexname = ANY($1)
		 ORDER BY indexname`,
		expectedIdx,
	)
	if err != nil {
		t.Fatalf("pg_indexes query: %v", err)
	}
	var gotIdx []string
	for idxRows.Next() {
		var n string
		if err := idxRows.Scan(&n); err != nil {
			idxRows.Close()
			t.Fatalf("scan index name: %v", err)
		}
		gotIdx = append(gotIdx, n)
	}
	idxRows.Close()
	if len(gotIdx) != len(expectedIdx) {
		t.Fatalf("sms_outbound indexes: got %v, want %v", gotIdx, expectedIdx)
	}
	for i, want := range expectedIdx {
		if gotIdx[i] != want {
			t.Fatalf("sms_outbound indexes[%d]: got %q, want %q", i, gotIdx[i], want)
		}
	}

	// AC-7: RLS policy sms_outbound_tenant_isolation exists and the table
	//       has both ENABLE and FORCE ROW LEVEL SECURITY set.
	var policyCount int
	if err := conn.QueryRow(ctx,
		`SELECT COUNT(*) FROM pg_policies
		 WHERE schemaname='public' AND tablename='sms_outbound'
		   AND policyname='sms_outbound_tenant_isolation'`,
	).Scan(&policyCount); err != nil {
		t.Fatalf("pg_policies query: %v", err)
	}
	if policyCount != 1 {
		t.Fatalf("sms_outbound RLS policy count: got %d, want 1", policyCount)
	}
	var rlsEnabled, rlsForced bool
	if err := conn.QueryRow(ctx,
		"SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE relname='sms_outbound'",
	).Scan(&rlsEnabled, &rlsForced); err != nil {
		t.Fatalf("pg_class RLS query: %v", err)
	}
	if !rlsEnabled || !rlsForced {
		t.Fatalf("sms_outbound RLS flags: relrowsecurity=%v relforcerowsecurity=%v, want both true",
			rlsEnabled, rlsForced)
	}
}

// TestLiveDBIdempotent_STORY087 verifies that running m.Up() a second time on
// an already-migrated database returns ErrNoChange and leaves the schema
// unchanged — confirming the shim is invisible on "live" (already-migrated) DBs.
func TestLiveDBIdempotent_STORY087(t *testing.T) {
	m, cleanup := setupFreshDB(t)
	t.Cleanup(cleanup)

	// First Up() — reach head.
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("first m.Up() failed: %v", err)
	}
	v1, _, err := m.Version()
	if err != nil {
		t.Fatalf("m.Version() after first Up(): %v", err)
	}

	// Snapshot constraint count before second Up().
	testDSN := disposableDSN(t)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, testDSN)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	var constraintsBefore int
	_ = conn.QueryRow(ctx, "SELECT COUNT(*) FROM pg_constraint").Scan(&constraintsBefore)

	// Second Up() — must be ErrNoChange.
	err = m.Up()
	if !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("second m.Up() error: got %v, want migrate.ErrNoChange", err)
	}

	// Version must be unchanged.
	v2, dirty2, err2 := m.Version()
	if err2 != nil {
		t.Fatalf("m.Version() after second Up(): %v", err2)
	}
	if dirty2 {
		t.Fatalf("schema dirty=true after second Up()")
	}
	if v2 != v1 {
		t.Fatalf("version changed on second Up(): %d → %d", v1, v2)
	}

	// Constraint count must be unchanged (no extra objects created by the shim).
	var constraintsAfter int
	_ = conn.QueryRow(ctx, "SELECT COUNT(*) FROM pg_constraint").Scan(&constraintsAfter)
	if constraintsAfter != constraintsBefore {
		t.Fatalf("pg_constraint count changed on second Up(): before=%d after=%d", constraintsBefore, constraintsAfter)
	}
}

// TestDownChain_STORY087 verifies that m.Down() (full roll-back) succeeds and
// leaves the schema in the initial empty state, confirming the down-chain is
// intact after adding the shim migration.
func TestDownChain_STORY087(t *testing.T) {
	m, cleanup := setupFreshDB(t)
	t.Cleanup(cleanup)

	// Migrate to head.
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("m.Up() failed: %v", err)
	}

	// Tear down all migrations.
	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("m.Down() failed: %v", err)
	}

	// Version must be nil (fully stepped down).
	_, _, vErr := m.Version()
	if !errors.Is(vErr, migrate.ErrNilVersion) {
		t.Fatalf("after Down(), Version() error: got %v, want migrate.ErrNilVersion", vErr)
	}

	// sms_outbound must not exist.
	testDSN := disposableDSN(t)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, testDSN)
	if err != nil {
		t.Fatalf("connect after Down(): %v", err)
	}
	defer conn.Close(ctx)

	var regclass *string
	if err := conn.QueryRow(ctx, "SELECT to_regclass('public.sms_outbound')::text").Scan(&regclass); err != nil {
		t.Fatalf("to_regclass query: %v", err)
	}
	if regclass != nil {
		t.Fatalf("sms_outbound still exists after Down() (to_regclass=%q)", *regclass)
	}
}

// disposableDSN returns a DSN pointing at the STORY-087 disposable database,
// derived from DATABASE_URL via swapDBName. The DSN is not stored on
// *migrate.Migrate directly, so callers that want a raw pgx connection must
// reconstruct it the same way setupFreshDB does.
func disposableDSN(t *testing.T) string {
	t.Helper()
	dsn, err := swapDBName(os.Getenv("DATABASE_URL"), testDBName)
	if err != nil {
		t.Fatalf("disposableDSN: %v", err)
	}
	return dsn
}

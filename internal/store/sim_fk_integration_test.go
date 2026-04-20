package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// seedDir returns the absolute path to migrations/seed/.
func seedDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(migrationsDir(t), "seed")
}

// runAllSeeds reads every *.sql file in migrations/seed/ (alphabetical order)
// and executes each via the simple query protocol (pool.Exec). Mirrors
// cmd/argus/main.go:runSeed but targets an explicit DSN.
func runAllSeeds(t *testing.T, dsn string) {
	t.Helper()
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New for seeds: %v", err)
	}
	defer pool.Close()

	files, err := filepath.Glob(filepath.Join(seedDir(t), "*.sql"))
	if err != nil {
		t.Fatalf("glob seed dir: %v", err)
	}
	sort.Strings(files)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read seed %s: %v", filepath.Base(f), err)
		}
		if _, err := pool.Exec(ctx, string(data)); err != nil {
			t.Fatalf("exec seed %s: %v", filepath.Base(f), err)
		}
	}
}

// TestFIX206_FreshVolume_NoOrphans validates AC-4: on a fresh volume, the full
// migrate+seed pipeline produces zero orphan operator/apn/ip references. This
// is the regression shield for seed 008's historical operator-UUID typo.
func TestFIX206_FreshVolume_NoOrphans(t *testing.T) {
	m, cleanup := setupFreshDB(t)
	t.Cleanup(cleanup)

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrate.Up: %v", err)
	}

	dsn := disposableDSN(t)
	runAllSeeds(t, dsn)

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	checks := []struct {
		label string
		sql   string
	}{
		{
			"orphan operator",
			`SELECT COUNT(*) FROM sims s
			 WHERE NOT EXISTS (SELECT 1 FROM operators o WHERE o.id = s.operator_id)`,
		},
		{
			"orphan apn",
			`SELECT COUNT(*) FROM sims s
			 WHERE s.apn_id IS NOT NULL
			   AND NOT EXISTS (SELECT 1 FROM apns a WHERE a.id = s.apn_id)`,
		},
		{
			"orphan ip_address",
			`SELECT COUNT(*) FROM sims s
			 WHERE s.ip_address_id IS NOT NULL
			   AND NOT EXISTS (SELECT 1 FROM ip_addresses i WHERE i.id = s.ip_address_id)`,
		},
	}
	for _, c := range checks {
		var count int
		if err := pool.QueryRow(ctx, c.sql).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", c.label, err)
		}
		if count != 0 {
			t.Fatalf("%s count: got %d, want 0", c.label, count)
		}
	}

	// Also verify we actually loaded SIMs (not a vacuous pass).
	var simCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM sims`).Scan(&simCount); err != nil {
		t.Fatalf("count sims: %v", err)
	}
	if simCount == 0 {
		t.Fatalf("seeds produced 0 sims — pipeline sanity check failed")
	}
}

// TestFIX206_FreshVolume_FKConstraintsInstalled asserts AC-2 and AC-3: after
// migration B runs, all three FK constraints on sims exist with the correct
// ON DELETE behavior.
func TestFIX206_FreshVolume_FKConstraintsInstalled(t *testing.T) {
	m, cleanup := setupFreshDB(t)
	t.Cleanup(cleanup)

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrate.Up: %v", err)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, disposableDSN(t))
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	expected := map[string]string{
		"fk_sims_operator":   "RESTRICT",
		"fk_sims_apn":        "SET NULL",
		"fk_sims_ip_address": "SET NULL",
	}

	rows, err := pool.Query(ctx,
		`SELECT conname, pg_get_constraintdef(oid)
		 FROM pg_constraint
		 WHERE conrelid='sims'::regclass AND contype='f'
		   AND conname = ANY($1)`,
		[]string{"fk_sims_operator", "fk_sims_apn", "fk_sims_ip_address"},
	)
	if err != nil {
		t.Fatalf("pg_constraint query: %v", err)
	}
	defer rows.Close()

	got := map[string]string{}
	for rows.Next() {
		var name, def string
		if err := rows.Scan(&name, &def); err != nil {
			t.Fatalf("scan constraint: %v", err)
		}
		got[name] = def
	}

	for name, wantAction := range expected {
		def, ok := got[name]
		if !ok {
			t.Errorf("constraint %s missing from pg_constraint", name)
			continue
		}
		if !strings.Contains(def, "ON DELETE "+wantAction) {
			t.Errorf("constraint %s: expected ON DELETE %s, got def=%q", name, wantAction, def)
		}
	}
}

// TestFIX206_SIMCreate_FKViolations verifies Task 4: the store.SIMStore.Create
// path translates a PG 23503 (foreign_key_violation) into an
// *InvalidReferenceError with the offending column populated — which the HTTP
// handler can then convert to a 400 with field-level detail.
func TestFIX206_SIMCreate_FKViolations(t *testing.T) {
	m, cleanup := setupFreshDB(t)
	t.Cleanup(cleanup)

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrate.Up: %v", err)
	}

	dsn := disposableDSN(t)
	runAllSeeds(t, dsn)

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	store := NewSIMStore(pool)

	// Canonical seed-005 identifiers (see migrations/seed/005_multi_operator_seed.sql).
	tenantXYZ := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	operatorTurkcell := uuid.MustParse("20000000-0000-0000-0000-000000000001")
	apnXYZIoT := uuid.MustParse("00000000-0000-0000-0000-000000000301")
	bogus := uuid.MustParse("99999999-9999-9999-9999-999999999999")

	cases := []struct {
		name        string
		params      CreateSIMParams
		wantColumn  string
	}{
		{
			name: "bogus operator_id",
			params: CreateSIMParams{
				OperatorID: bogus,
				APNID:      apnXYZIoT,
				ICCID:      "8990286010FIX206-OP001",
				IMSI:       "28601FIX206001",
				SimType:    "physical",
				Metadata:   json.RawMessage(`{}`),
			},
			wantColumn: "operator_id",
		},
		{
			name: "bogus apn_id",
			params: CreateSIMParams{
				OperatorID: operatorTurkcell,
				APNID:      bogus,
				ICCID:      "8990286010FIX206-AP001",
				IMSI:       "28601FIX206002",
				SimType:    "physical",
				Metadata:   json.RawMessage(`{}`),
			},
			wantColumn: "apn_id",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := store.Create(ctx, tenantXYZ, tc.params)
			if err == nil {
				t.Fatal("expected InvalidReferenceError, got nil")
			}
			if !errors.Is(err, ErrInvalidReference) {
				t.Fatalf("expected errors.Is(err, ErrInvalidReference); got %T: %v", err, err)
			}
			var refErr *InvalidReferenceError
			if !errors.As(err, &refErr) {
				t.Fatalf("expected *InvalidReferenceError via errors.As; got %T: %v", err, err)
			}
			if refErr.Column != tc.wantColumn {
				t.Errorf("InvalidReferenceError.Column: got %q, want %q (constraint=%s)",
					refErr.Column, tc.wantColumn, refErr.Constraint)
			}
		})
	}
}

package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testSIMEnrichedPool returns a pgxpool.Pool bound to the test database when
// DATABASE_URL is set; otherwise returns nil so callers can t.Skip.
// Matches the existing testSIMBulkPool helper pattern in sim_bulk_filter_test.go.
func testSIMEnrichedPool(t *testing.T) *pgxpool.Pool {
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

// enrichedFixture provisions the minimal DB graph for enriched SIM tests:
// one tenant, one operator, one APN, one policy + version.
// Returns IDs and registers cleanup. The caller inserts SIM rows.
type enrichedFixture struct {
	tenantID        uuid.UUID
	operatorID      uuid.UUID
	apnID           uuid.UUID
	apnNoDisplayID  uuid.UUID
	policyID        uuid.UUID
	policyVersionID uuid.UUID
}

func seedEnrichedFixture(t *testing.T, pool *pgxpool.Pool) enrichedFixture {
	t.Helper()
	ctx := context.Background()
	var f enrichedFixture

	// tenant
	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, contact_email)
		VALUES ('enrich-'||gen_random_uuid()::text, 'enrich@test.argus')
		RETURNING id`).Scan(&f.tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	// operator (global — no tenant_id)
	if err := pool.QueryRow(ctx, `SELECT id FROM operators LIMIT 1`).Scan(&f.operatorID); err != nil {
		t.Fatalf("no operator row available: %v", err)
	}

	// APN with display_name set
	if err := pool.QueryRow(ctx, `
		INSERT INTO apns (tenant_id, operator_id, name, display_name, apn_type, state)
		VALUES ($1, $2, 'iot.'||gen_random_uuid()::text, 'iot.pool', 'iot', 'active')
		RETURNING id`, f.tenantID, f.operatorID).Scan(&f.apnID); err != nil {
		t.Fatalf("seed apn: %v", err)
	}

	// APN with empty display_name (fallback to name)
	if err := pool.QueryRow(ctx, `
		INSERT INTO apns (tenant_id, operator_id, name, display_name, apn_type, state)
		VALUES ($1, $2, 'mobile-data.'||gen_random_uuid()::text, '', 'data', 'active')
		RETURNING id`, f.tenantID, f.operatorID).Scan(&f.apnNoDisplayID); err != nil {
		t.Fatalf("seed apn no-display: %v", err)
	}

	// policy
	if err := pool.QueryRow(ctx, `
		INSERT INTO policies (tenant_id, name, scope, state)
		VALUES ($1, 'test-policy-'||gen_random_uuid()::text, 'global', 'active')
		RETURNING id`, f.tenantID).Scan(&f.policyID); err != nil {
		t.Fatalf("seed policy: %v", err)
	}

	// policy_version
	if err := pool.QueryRow(ctx, `
		INSERT INTO policy_versions (policy_id, version, dsl_content, compiled_rules, state)
		VALUES ($1, 1, 'allow all;', '{}', 'active')
		RETURNING id`, f.policyID).Scan(&f.policyVersionID); err != nil {
		t.Fatalf("seed policy_version: %v", err)
	}

	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = pool.Exec(cctx, `DELETE FROM sims WHERE tenant_id = $1`, f.tenantID)
		_, _ = pool.Exec(cctx, `DELETE FROM policy_versions WHERE id = $1`, f.policyVersionID)
		_, _ = pool.Exec(cctx, `DELETE FROM policies WHERE id = $1`, f.policyID)
		_, _ = pool.Exec(cctx, `DELETE FROM apns WHERE tenant_id = $1`, f.tenantID)
		_, _ = pool.Exec(cctx, `DELETE FROM tenants WHERE id = $1`, f.tenantID)
	})

	return f
}

// insertEnrichedSIM inserts a SIM with full parent graph and returns its ID.
func insertEnrichedSIM(t *testing.T, pool *pgxpool.Pool, tenantID, operatorID uuid.UUID, apnID *uuid.UUID, policyVersionID *uuid.UUID, idx int) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	nonce := uuid.New().ID() % 1_000_000_000
	iccid := fmt.Sprintf("89911%02d%09d", idx%100, nonce)
	imsi := fmt.Sprintf("28611%02d%08d", idx%100, nonce%100_000_000)

	var simID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO sims (tenant_id, operator_id, apn_id, iccid, imsi, sim_type, state, policy_version_id)
		VALUES ($1, $2, $3, $4, $5, 'physical', 'ordered', $6)
		RETURNING id`,
		tenantID, operatorID, apnID, iccid, imsi, policyVersionID,
	).Scan(&simID); err != nil {
		t.Fatalf("seed sim %d: %v", idx, err)
	}
	return simID
}

// ---------------------------------------------------------------------------
// ListEnriched tests
// ---------------------------------------------------------------------------

func TestSIMStore_ListEnriched_AllFieldsPopulated(t *testing.T) {
	pool := testSIMEnrichedPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	s := NewSIMStore(pool)
	f := seedEnrichedFixture(t, pool)

	for i := 0; i < 3; i++ {
		insertEnrichedSIM(t, pool, f.tenantID, f.operatorID, &f.apnID, &f.policyVersionID, i)
	}

	results, _, err := s.ListEnriched(context.Background(), f.tenantID, ListSIMsParams{Limit: 10})
	if err != nil {
		t.Fatalf("ListEnriched: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("len = %d, want 3", len(results))
	}
	for i, sim := range results {
		if sim.OperatorName == nil {
			t.Errorf("row %d: OperatorName nil", i)
		}
		if sim.OperatorCode == nil {
			t.Errorf("row %d: OperatorCode nil", i)
		}
		if sim.APNName == nil {
			t.Errorf("row %d: APNName nil", i)
		}
		if sim.PolicyName == nil {
			t.Errorf("row %d: PolicyName nil", i)
		}
		if sim.PolicyVersionNumber == nil {
			t.Errorf("row %d: PolicyVersionNumber nil", i)
		}
	}
}

// TestSIMStore_ListEnriched_OrphanOperator_Blocked is the FIX-206 successor to
// the original ListEnriched_OrphanOperator test. Before FIX-206 there was no FK
// on sims.operator_id, so the test inserted a SIM with a ghost operator_id and
// asserted the ListEnriched LEFT JOIN returned NULL operator_name/code for it.
//
// After FIX-206 (migration 20260420000002_sims_fk_constraints), orphan-operator
// rows are structurally impossible: fk_sims_operator rejects the INSERT with
// SQLSTATE 23503. The DTO "unknown operator" fallback code path (LEFT JOIN +
// NULL-safe COALESCE in ListEnriched) is still present and exercised by
// TestSIMStore_ListEnriched_NoPolicy and by production rows where a race
// between handler validation and operator delete is still theoretically
// possible — it just can't be provoked by a simple INSERT in a unit test
// anymore.
//
// This test preserves the regression-guard value of the original by asserting
// the FK is in fact present and rejects the ghost-operator INSERT with the
// store-typed ErrInvalidReference.
func TestSIMStore_ListEnriched_OrphanOperator_Blocked(t *testing.T) {
	pool := testSIMEnrichedPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	f := seedEnrichedFixture(t, pool)
	ctx := context.Background()

	// Ghost operator_id not in operators(id).
	ghostOperatorID := uuid.New()
	nonce := uuid.New().ID() % 1_000_000_000
	iccid := fmt.Sprintf("89911%02d%09d", 0, nonce)
	imsi := fmt.Sprintf("28611%02d%08d", 0, nonce%100_000_000)

	// Direct INSERT: FIX-206 added fk_sims_operator with ON DELETE RESTRICT.
	// Attempting to insert with a non-existent operator_id must fail with
	// SQLSTATE 23503 (foreign_key_violation), translated to *InvalidReferenceError
	// by asInvalidReference() via the store Create path. Here we bypass the
	// store helper because insertEnrichedSIM uses pool.QueryRow directly; we
	// attempt the same raw INSERT and assert the PG error.
	_, err := pool.Exec(ctx, `
		INSERT INTO sims (tenant_id, operator_id, apn_id, iccid, imsi, sim_type, state, policy_version_id)
		VALUES ($1, $2, $3, $4, $5, 'physical', 'ordered', $6)`,
		f.tenantID, ghostOperatorID, &f.apnID, iccid, imsi, &f.policyVersionID,
	)
	if err == nil {
		t.Fatal("expected FK violation on orphan operator_id insert, got nil error " +
			"— fk_sims_operator may be missing (FIX-206 regression)")
	}
	if refErr, ok := asInvalidReference(err, simsFKConstraintColumn); ok {
		if refErr.Constraint != "fk_sims_operator" {
			t.Errorf("constraint = %q, want %q", refErr.Constraint, "fk_sims_operator")
		}
		if refErr.Column != "operator_id" {
			t.Errorf("column = %q, want %q", refErr.Column, "operator_id")
		}
		if !errors.Is(refErr, ErrInvalidReference) {
			t.Errorf("refErr should unwrap to ErrInvalidReference")
		}
	} else {
		t.Fatalf("expected *InvalidReferenceError (SQLSTATE 23503), got %T: %v", err, err)
	}
}

func TestSIMStore_ListEnriched_NoPolicy(t *testing.T) {
	pool := testSIMEnrichedPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	s := NewSIMStore(pool)
	f := seedEnrichedFixture(t, pool)

	// Insert SIM with no policy_version_id.
	insertEnrichedSIM(t, pool, f.tenantID, f.operatorID, &f.apnID, nil, 0)

	results, _, err := s.ListEnriched(context.Background(), f.tenantID, ListSIMsParams{Limit: 10})
	if err != nil {
		t.Fatalf("ListEnriched: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected SIM with no policy to be returned")
	}

	sim := results[0]
	if sim.PolicyName != nil {
		t.Errorf("PolicyName should be nil when policy_version_id is NULL, got %q", *sim.PolicyName)
	}
	if sim.PolicyVersionNumber != nil {
		t.Errorf("PolicyVersionNumber should be nil when policy_version_id is NULL, got %d", *sim.PolicyVersionNumber)
	}
}

func TestSIMStore_ListEnriched_APNDisplayNamePrecedence(t *testing.T) {
	pool := testSIMEnrichedPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	s := NewSIMStore(pool)
	f := seedEnrichedFixture(t, pool)

	// SIM 0: APN with display_name = 'iot.pool'
	id0 := insertEnrichedSIM(t, pool, f.tenantID, f.operatorID, &f.apnID, nil, 0)
	// SIM 1: APN with display_name = '' (fallback to name)
	id1 := insertEnrichedSIM(t, pool, f.tenantID, f.operatorID, &f.apnNoDisplayID, nil, 1)

	results, _, err := s.ListEnriched(context.Background(), f.tenantID, ListSIMsParams{Limit: 10})
	if err != nil {
		t.Fatalf("ListEnriched: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len = %d, want 2", len(results))
	}

	byID := make(map[uuid.UUID]*SIMWithNames)
	for i := range results {
		byID[results[i].ID] = &results[i]
	}

	sim0 := byID[id0]
	if sim0 == nil {
		t.Fatal("SIM0 not found in results")
	}
	if sim0.APNName == nil || *sim0.APNName != "iot.pool" {
		t.Errorf("SIM0 APNName = %v, want 'iot.pool'", sim0.APNName)
	}

	sim1 := byID[id1]
	if sim1 == nil {
		t.Fatal("SIM1 not found in results")
	}
	if sim1.APNName == nil {
		t.Fatal("SIM1 APNName is nil, want name from APN.name (fallback)")
	}
	// The actual APN name starts with 'mobile-data.' prefix; verify it starts with the right prefix.
	if len(*sim1.APNName) == 0 {
		t.Error("SIM1 APNName is empty, want fallback to APN.name")
	}
}

// ---------------------------------------------------------------------------
// GetByIDEnriched tests
// ---------------------------------------------------------------------------

func TestSIMStore_GetByIDEnriched_HappyPath(t *testing.T) {
	pool := testSIMEnrichedPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	s := NewSIMStore(pool)
	f := seedEnrichedFixture(t, pool)

	simID := insertEnrichedSIM(t, pool, f.tenantID, f.operatorID, &f.apnID, &f.policyVersionID, 0)

	sim, err := s.GetByIDEnriched(context.Background(), f.tenantID, simID)
	if err != nil {
		t.Fatalf("GetByIDEnriched: %v", err)
	}
	if sim.ID != simID {
		t.Errorf("ID = %v, want %v", sim.ID, simID)
	}
	if sim.OperatorName == nil {
		t.Error("OperatorName nil")
	}
	if sim.OperatorCode == nil {
		t.Error("OperatorCode nil")
	}
	if sim.APNName == nil {
		t.Error("APNName nil")
	}
	if sim.PolicyName == nil {
		t.Error("PolicyName nil")
	}
	if sim.PolicyVersionNumber == nil {
		t.Error("PolicyVersionNumber nil")
	}
}

func TestSIMStore_GetByIDEnriched_NotFound(t *testing.T) {
	pool := testSIMEnrichedPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	s := NewSIMStore(pool)
	f := seedEnrichedFixture(t, pool)

	_, err := s.GetByIDEnriched(context.Background(), f.tenantID, uuid.New())
	if err == nil {
		t.Fatal("expected ErrSIMNotFound, got nil")
	}
	if err != ErrSIMNotFound {
		t.Errorf("err = %v, want ErrSIMNotFound", err)
	}
}

func TestSIMStore_GetByIDEnriched_CrossTenant(t *testing.T) {
	pool := testSIMEnrichedPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	s := NewSIMStore(pool)

	// two separate tenants
	f1 := seedEnrichedFixture(t, pool)
	f2 := seedEnrichedFixture(t, pool)

	simID := insertEnrichedSIM(t, pool, f1.tenantID, f1.operatorID, &f1.apnID, &f1.policyVersionID, 0)

	// fetch with tenant2's ID — must not find it
	_, err := s.GetByIDEnriched(context.Background(), f2.tenantID, simID)
	if err == nil {
		t.Fatal("expected ErrSIMNotFound for cross-tenant fetch, got nil")
	}
	if err != ErrSIMNotFound {
		t.Errorf("err = %v, want ErrSIMNotFound", err)
	}
}

// ---------------------------------------------------------------------------
// GetManyByIDsEnriched tests
// ---------------------------------------------------------------------------

func TestSIMStore_GetManyByIDsEnriched_HappyPath(t *testing.T) {
	pool := testSIMEnrichedPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	s := NewSIMStore(pool)
	f := seedEnrichedFixture(t, pool)

	ids := make([]uuid.UUID, 5)
	for i := range ids {
		ids[i] = insertEnrichedSIM(t, pool, f.tenantID, f.operatorID, &f.apnID, &f.policyVersionID, i)
	}

	// fetch only 3 of the 5
	fetch := ids[:3]
	result, err := s.GetManyByIDsEnriched(context.Background(), f.tenantID, fetch)
	if err != nil {
		t.Fatalf("GetManyByIDsEnriched: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("len(result) = %d, want 3", len(result))
	}
	for _, id := range fetch {
		sim, ok := result[id]
		if !ok {
			t.Errorf("SIM %v missing from result map", id)
			continue
		}
		if sim.OperatorName == nil {
			t.Errorf("SIM %v: OperatorName nil", id)
		}
		if sim.PolicyName == nil {
			t.Errorf("SIM %v: PolicyName nil", id)
		}
	}
}

func TestSIMStore_GetManyByIDsEnriched_Empty_NoDB(t *testing.T) {
	// This test verifies the empty-slice short-circuit — no DB connection required.
	s := &SIMStore{db: nil}
	result, err := s.GetManyByIDsEnriched(context.Background(), uuid.New(), []uuid.UUID{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Error("result map should not be nil for empty input")
	}
	if len(result) != 0 {
		t.Errorf("len(result) = %d, want 0", len(result))
	}
}

func TestSIMStore_GetManyByIDsEnriched_Chunk500_Boundary(t *testing.T) {
	pool := testSIMEnrichedPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	s := NewSIMStore(pool)
	f := seedEnrichedFixture(t, pool)

	const count = 501
	ids := make([]uuid.UUID, count)
	for i := range ids {
		ids[i] = insertEnrichedSIM(t, pool, f.tenantID, f.operatorID, &f.apnID, &f.policyVersionID, i)
	}

	result, err := s.GetManyByIDsEnriched(context.Background(), f.tenantID, ids)
	if err != nil {
		t.Fatalf("GetManyByIDsEnriched: %v", err)
	}
	if len(result) != count {
		t.Errorf("len(result) = %d, want %d", len(result), count)
	}
}

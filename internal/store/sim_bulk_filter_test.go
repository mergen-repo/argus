package store

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testSIMBulkPool returns a pgxpool.Pool connected to the test database when
// DATABASE_URL is set; otherwise returns nil so DB-dependent tests can skip.
// Matches the existing testSMSPool / testIPPoolPool helper pattern.
func testSIMBulkPool(t *testing.T) *pgxpool.Pool {
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

// simBulkFixture provisions the minimal DB rows needed for bulk-filter tests:
// one tenant, one (existing) operator, one APN, and returns their IDs.
// The caller is responsible for inserting SIM rows via insertTestSIM.
// All rows created through this fixture are cleaned up in t.Cleanup.
func simBulkFixture(t *testing.T, pool *pgxpool.Pool) (tenantID, operatorID, apnID uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, contact_email)
		VALUES ('sim-bulk-'||gen_random_uuid()::text, 'bulk@test.argus')
		RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	if err := pool.QueryRow(ctx, `SELECT id FROM operators LIMIT 1`).Scan(&operatorID); err != nil {
		t.Fatalf("no operator row: %v", err)
	}

	if err := pool.QueryRow(ctx, `
		INSERT INTO apns (tenant_id, operator_id, name, display_name, apn_type, state)
		VALUES ($1, $2, 'bulk-'||gen_random_uuid()::text, 'Bulk Test APN', 'iot', 'active')
		RETURNING id`, tenantID, operatorID).Scan(&apnID); err != nil {
		t.Fatalf("seed apn: %v", err)
	}

	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = pool.Exec(cctx, `DELETE FROM sims WHERE tenant_id = $1`, tenantID)
		_, _ = pool.Exec(cctx, `DELETE FROM apns WHERE id = $1`, apnID)
		_, _ = pool.Exec(cctx, `DELETE FROM tenants WHERE id = $1`, tenantID)
	})
	return tenantID, operatorID, apnID
}

// insertTestSIM inserts one SIM and returns its ID. ICCID and IMSI are built
// from a per-call nonce to avoid unique-index collisions across parallel runs.
func insertTestSIM(t *testing.T, pool *pgxpool.Pool, tenantID, operatorID, apnID uuid.UUID, idx int) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	nonce := uuid.New().ID() % 1_000_000_000
	iccid := fmt.Sprintf("89905%02d%09d", idx%100, nonce)
	imsi := fmt.Sprintf("28695%02d%08d", idx%100, nonce%100_000_000)
	var simID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO sims (tenant_id, operator_id, apn_id, iccid, imsi, sim_type, state)
		VALUES ($1, $2, $3, $4, $5, 'physical', 'ordered')
		RETURNING id`, tenantID, operatorID, apnID, iccid, imsi).Scan(&simID); err != nil {
		t.Fatalf("seed sim %d: %v", idx, err)
	}
	return simID
}

// ---------------------------------------------------------------------------
// FilterSIMIDsByTenant tests
// ---------------------------------------------------------------------------

func TestFilterSIMIDsByTenant_Empty_ReturnsEmpty(t *testing.T) {
	pool := testSIMBulkPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	s := NewSIMStore(pool)
	tenantID := uuid.New()

	owned, violations, err := s.FilterSIMIDsByTenant(context.Background(), tenantID, []uuid.UUID{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(owned) != 0 {
		t.Errorf("owned len = %d, want 0", len(owned))
	}
	if len(violations) != 0 {
		t.Errorf("violations len = %d, want 0", len(violations))
	}
}

func TestFilterSIMIDsByTenant_AllOwned_ReturnsOwnedNoViolations(t *testing.T) {
	pool := testSIMBulkPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	s := NewSIMStore(pool)
	tenantID, operatorID, apnID := simBulkFixture(t, pool)

	ids := make([]uuid.UUID, 3)
	for i := range ids {
		ids[i] = insertTestSIM(t, pool, tenantID, operatorID, apnID, i)
	}

	owned, violations, err := s.FilterSIMIDsByTenant(context.Background(), tenantID, ids)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(owned) != len(ids) {
		t.Errorf("owned len = %d, want %d", len(owned), len(ids))
	}
	if len(violations) != 0 {
		t.Errorf("violations len = %d, want 0; got %v", len(violations), violations)
	}
}

func TestFilterSIMIDsByTenant_MixedOwnedAndForeign_SeparatesViolations(t *testing.T) {
	pool := testSIMBulkPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	s := NewSIMStore(pool)

	tenant1, op1, apn1 := simBulkFixture(t, pool)
	tenant2, op2, apn2 := simBulkFixture(t, pool)

	ownedSIM := insertTestSIM(t, pool, tenant1, op1, apn1, 0)
	foreignSIM := insertTestSIM(t, pool, tenant2, op2, apn2, 0)

	input := []uuid.UUID{ownedSIM, foreignSIM}
	owned, violations, err := s.FilterSIMIDsByTenant(context.Background(), tenant1, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(owned) != 1 || owned[0] != ownedSIM {
		t.Errorf("owned = %v, want [%v]", owned, ownedSIM)
	}
	if len(violations) != 1 || violations[0] != foreignSIM {
		t.Errorf("violations = %v, want [%v]", violations, foreignSIM)
	}
}

func TestFilterSIMIDsByTenant_MissingID_TreatedAsViolation(t *testing.T) {
	pool := testSIMBulkPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	s := NewSIMStore(pool)
	tenantID, operatorID, apnID := simBulkFixture(t, pool)

	realSIM := insertTestSIM(t, pool, tenantID, operatorID, apnID, 0)
	missingID := uuid.New()

	owned, violations, err := s.FilterSIMIDsByTenant(context.Background(), tenantID, []uuid.UUID{realSIM, missingID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(owned) != 1 || owned[0] != realSIM {
		t.Errorf("owned = %v, want [%v]", owned, realSIM)
	}
	if len(violations) != 1 || violations[0] != missingID {
		t.Errorf("violations = %v, want [%v]", violations, missingID)
	}
}

func TestFilterSIMIDsByTenant_DuplicateIDs_NotReportedAsViolations(t *testing.T) {
	pool := testSIMBulkPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	s := NewSIMStore(pool)
	tenantID, operatorID, apnID := simBulkFixture(t, pool)

	simID := insertTestSIM(t, pool, tenantID, operatorID, apnID, 0)
	input := []uuid.UUID{simID, simID, simID}

	owned, violations, err := s.FilterSIMIDsByTenant(context.Background(), tenantID, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(owned) != 1 {
		t.Errorf("owned len = %d, want 1 (deduped)", len(owned))
	}
	if len(violations) != 0 {
		t.Errorf("violations len = %d, want 0; duplicates must not become violations", len(violations))
	}
}

func TestFilterSIMIDsByTenant_ChunkBoundary_AtBatchSize(t *testing.T) {
	pool := testSIMBulkPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	s := NewSIMStore(pool)
	tenantID, operatorID, apnID := simBulkFixture(t, pool)

	const count = 501
	ids := make([]uuid.UUID, count)
	for i := range ids {
		ids[i] = insertTestSIM(t, pool, tenantID, operatorID, apnID, i)
	}

	owned, violations, err := s.FilterSIMIDsByTenant(context.Background(), tenantID, ids)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(owned) != count {
		t.Errorf("owned len = %d, want %d", len(owned), count)
	}
	if len(violations) != 0 {
		t.Errorf("violations len = %d, want 0", len(violations))
	}
}

// ---------------------------------------------------------------------------
// GetSIMsByIDs tests
// ---------------------------------------------------------------------------

func TestGetSIMsByIDs_Empty_ReturnsEmpty(t *testing.T) {
	pool := testSIMBulkPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	s := NewSIMStore(pool)
	tenantID := uuid.New()

	results, err := s.GetSIMsByIDs(context.Background(), tenantID, []uuid.UUID{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("results len = %d, want 0", len(results))
	}
}

func TestGetSIMsByIDs_TenantScoped_ExcludesForeign(t *testing.T) {
	pool := testSIMBulkPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	s := NewSIMStore(pool)

	tenant1, op1, apn1 := simBulkFixture(t, pool)
	tenant2, op2, apn2 := simBulkFixture(t, pool)

	ownedSIM := insertTestSIM(t, pool, tenant1, op1, apn1, 0)
	foreignSIM := insertTestSIM(t, pool, tenant2, op2, apn2, 0)

	results, err := s.GetSIMsByIDs(context.Background(), tenant1, []uuid.UUID{ownedSIM, foreignSIM})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].ID != ownedSIM {
		t.Errorf("returned SIM ID = %v, want %v", results[0].ID, ownedSIM)
	}
	if results[0].SimType == "" {
		t.Error("SimType should not be empty")
	}
}

func TestGetSIMsByIDs_ChunkBoundary(t *testing.T) {
	pool := testSIMBulkPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	s := NewSIMStore(pool)
	tenantID, operatorID, apnID := simBulkFixture(t, pool)

	const count = 501
	ids := make([]uuid.UUID, count)
	for i := range ids {
		ids[i] = insertTestSIM(t, pool, tenantID, operatorID, apnID, i)
	}

	results, err := s.GetSIMsByIDs(context.Background(), tenantID, ids)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != count {
		t.Errorf("results len = %d, want %d", len(results), count)
	}
	for _, sim := range results {
		if sim.ICCID == "" {
			t.Error("at least one SIM has empty ICCID")
			break
		}
	}
}

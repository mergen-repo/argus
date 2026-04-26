package store

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// insertActiveTestSIM inserts one SIM in 'active' state for CountWithPredicate tests.
// Uses a per-call nonce derived from idx + uuid to avoid ICCID/IMSI unique-index collisions.
func insertActiveTestSIM(t *testing.T, pool *pgxpool.Pool, tenantID, operatorID, apnID uuid.UUID, idx int) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	nonce := uuid.New().ID() % 1_000_000_000
	iccid := fmt.Sprintf("89907%02d%09d", idx%100, nonce)
	imsi := fmt.Sprintf("28697%02d%08d", idx%100, nonce%100_000_000)
	var simID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO sims (tenant_id, operator_id, apn_id, iccid, imsi, sim_type, state)
		VALUES ($1, $2, $3, $4, $5, 'physical', 'active')
		RETURNING id`, tenantID, operatorID, apnID, iccid, imsi).Scan(&simID); err != nil {
		t.Fatalf("insertActiveTestSIM idx=%d: %v", idx, err)
	}
	return simID
}

// TestCountWithPredicate_EmptyPredicate — empty predicate ("") counts all active SIMs
// in the tenant (maps to "TRUE" internally). DB-gated; skips without DATABASE_URL.
func TestCountWithPredicate_EmptyPredicate(t *testing.T) {
	pool := testSIMBulkPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}

	tenantID, operatorID, apnID := simBulkFixture(t, pool)
	s := NewSIMStore(pool)

	insertActiveTestSIM(t, pool, tenantID, operatorID, apnID, 0)
	insertActiveTestSIM(t, pool, tenantID, operatorID, apnID, 1)
	insertActiveTestSIM(t, pool, tenantID, operatorID, apnID, 2)

	count, err := s.CountWithPredicate(context.Background(), tenantID, "", nil)
	if err != nil {
		t.Fatalf("CountWithPredicate empty predicate: %v", err)
	}
	if count != 3 {
		t.Errorf("got count=%d, want 3", count)
	}
}

// TestCountWithPredicate_ApnFilter — predicate scoped to a specific APN name returns
// only the matching SIMs. Manually constructs the parameterized predicate fragment
// (mirroring what dsl.ToSQLPredicate produces for an apn equality rule). DB-gated.
func TestCountWithPredicate_ApnFilter(t *testing.T) {
	pool := testSIMBulkPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}

	tenantID, operatorID, apnID := simBulkFixture(t, pool)
	s := NewSIMStore(pool)

	insertActiveTestSIM(t, pool, tenantID, operatorID, apnID, 0)
	insertActiveTestSIM(t, pool, tenantID, operatorID, apnID, 1)

	var apnName string
	if err := pool.QueryRow(context.Background(), `SELECT name FROM apns WHERE id = $1`, apnID).Scan(&apnName); err != nil {
		t.Fatalf("fetch apn name: %v", err)
	}

	// $1 is already bound to tenantID by CountWithPredicate; $2 is the first dslArg.
	predicate := `EXISTS (SELECT 1 FROM apns a WHERE a.id = s.apn_id AND a.tenant_id = $1 AND a.name = $2)`
	args := []interface{}{apnName}

	count, err := s.CountWithPredicate(context.Background(), tenantID, predicate, args)
	if err != nil {
		t.Fatalf("CountWithPredicate apn filter: %v", err)
	}
	if count != 2 {
		t.Errorf("got count=%d, want 2", count)
	}
}

// TestCountWithPredicate_NoMatchReturnsZero — predicate that can never match any row
// returns 0 without error. DB-gated.
func TestCountWithPredicate_NoMatchReturnsZero(t *testing.T) {
	pool := testSIMBulkPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}

	tenantID, operatorID, apnID := simBulkFixture(t, pool)
	s := NewSIMStore(pool)

	insertActiveTestSIM(t, pool, tenantID, operatorID, apnID, 0)

	// $1 is tenantID (bound by CountWithPredicate); $2 is the first dslArg.
	predicate := `s.iccid = $2`
	args := []interface{}{"8990000000000000000_no_match"}

	count, err := s.CountWithPredicate(context.Background(), tenantID, predicate, args)
	if err != nil {
		t.Fatalf("CountWithPredicate no-match: %v", err)
	}
	if count != 0 {
		t.Errorf("got count=%d, want 0", count)
	}
}

//go:build integration

package aggregates

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// testAggregatesPool returns a pgxpool connected to DATABASE_URL, or nil so
// the caller can t.Skip. Matches the helper pattern used in sim_list_enriched_test.go.
func testAggregatesPool(t *testing.T) *pgxpool.Pool {
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

// f125Fixture holds the IDs provisioned for F-125 regression tests.
type f125Fixture struct {
	tenantID        uuid.UUID
	operatorID      uuid.UUID
	policyID        uuid.UUID
	policyVersionID uuid.UUID
}

// seedF125Fixture inserts a minimal graph: tenant → operator → policy → policy_version.
// Cleanup is registered on t; callers insert and clean up SIM rows separately.
func seedF125Fixture(t *testing.T, pool *pgxpool.Pool) f125Fixture {
	t.Helper()
	ctx := context.Background()
	var f f125Fixture

	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, contact_email)
		VALUES ('f125-'||gen_random_uuid()::text, 'f125@test.argus')
		RETURNING id`).Scan(&f.tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	if err := pool.QueryRow(ctx, `SELECT id FROM operators LIMIT 1`).Scan(&f.operatorID); err != nil {
		t.Fatalf("no operator row available (seed required): %v", err)
	}

	if err := pool.QueryRow(ctx, `
		INSERT INTO policies (tenant_id, name, scope, state)
		VALUES ($1, 'f125-policy-'||gen_random_uuid()::text, 'global', 'active')
		RETURNING id`, f.tenantID).Scan(&f.policyID); err != nil {
		t.Fatalf("seed policy: %v", err)
	}

	if err := pool.QueryRow(ctx, `
		INSERT INTO policy_versions (policy_id, version, dsl_content, compiled_rules, state)
		VALUES ($1, 1, 'allow all;', '{}', 'active')
		RETURNING id`, f.policyID).Scan(&f.policyVersionID); err != nil {
		t.Fatalf("seed policy_version: %v", err)
	}

	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = pool.Exec(cctx, `DELETE FROM policy_assignments WHERE policy_version_id = $1`, f.policyVersionID)
		_, _ = pool.Exec(cctx, `DELETE FROM sims WHERE tenant_id = $1`, f.tenantID)
		_, _ = pool.Exec(cctx, `DELETE FROM policy_versions WHERE id = $1`, f.policyVersionID)
		_, _ = pool.Exec(cctx, `DELETE FROM policies WHERE id = $1`, f.policyID)
		_, _ = pool.Exec(cctx, `DELETE FROM tenants WHERE id = $1`, f.tenantID)
	})

	return f
}

// insertF125SIM inserts a SIM owned by the fixture tenant with the given policy_version_id.
func insertF125SIM(t *testing.T, pool *pgxpool.Pool, f f125Fixture, idx int) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	nonce := uuid.New().ID() % 1_000_000_000
	iccid := fmt.Sprintf("89911F125%02d%09d", idx%100, nonce)
	imsi := fmt.Sprintf("28611F125%02d%08d", idx%100, nonce%100_000_000)

	var simID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO sims (tenant_id, operator_id, iccid, imsi, sim_type, state, policy_version_id)
		VALUES ($1, $2, $3, $4, 'physical', 'active', $5)
		RETURNING id`,
		f.tenantID, f.operatorID, iccid, imsi, f.policyVersionID,
	).Scan(&simID); err != nil {
		t.Fatalf("insert SIM[%d]: %v", idx, err)
	}
	return simID
}

// TestAggregates_F125_SameOperatorSIMCountEverywhere proves the canonical-source
// decision holds: SIMCountByPolicy reads from sims.policy_version_id (live FK),
// NOT from policy_assignments (CoA/audit history).
//
// Scenario:
//  1. Seed 10 SIMs with policy_version_id = PV  →  canonical count = 10
//  2. Insert a stale policy_assignments row for a SIM that does NOT exist in
//     sims (simulating an orphaned audit record).
//  3. PolicyStore.CountAssignedSIMs returns 11 (stale path).
//  4. Aggregates.SIMCountByPolicy still returns 10 (canonical path).
func TestAggregates_F125_SameOperatorSIMCountEverywhere(t *testing.T) {
	pool := testAggregatesPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set — skipping DB-gated F-125 integration test")
	}

	ctx := context.Background()
	f := seedF125Fixture(t, pool)

	for i := 0; i < 10; i++ {
		insertF125SIM(t, pool, f, i)
	}

	simStore := store.NewSIMStore(pool)
	sessionStore := store.NewRadiusSessionStore(pool)
	agg := NewDB(simStore, sessionStore, zerolog.Nop())

	opCount, err := agg.SIMCountByOperator(ctx, f.tenantID)
	if err != nil {
		t.Fatalf("SIMCountByOperator: %v", err)
	}
	if opCount[f.operatorID] != 10 {
		t.Errorf("SIMCountByOperator[opID]: got %d, want 10", opCount[f.operatorID])
	}

	canonical, err := agg.SIMCountByPolicy(ctx, f.tenantID, f.policyID)
	if err != nil {
		t.Fatalf("SIMCountByPolicy (canonical): %v", err)
	}
	if canonical != 10 {
		t.Errorf("SIMCountByPolicy (canonical, before stale insert): got %d, want 10", canonical)
	}

	ghostSimID := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO policy_assignments (sim_id, policy_version_id, coa_status)
		VALUES ($1, $2, 'pending')`,
		ghostSimID, f.policyVersionID,
	); err != nil {
		t.Fatalf("insert stale policy_assignment: %v", err)
	}

	policyStore := store.NewPolicyStore(pool)
	staleCount, err := policyStore.CountAssignedSIMs(ctx, f.policyID)
	if err != nil {
		t.Fatalf("PolicyStore.CountAssignedSIMs: %v", err)
	}
	if staleCount != 11 {
		t.Errorf("PolicyStore.CountAssignedSIMs (stale): got %d, want 11 — test setup may be wrong", staleCount)
	}

	canonicalAfter, err := agg.SIMCountByPolicy(ctx, f.tenantID, f.policyID)
	if err != nil {
		t.Fatalf("SIMCountByPolicy (canonical after stale insert): %v", err)
	}
	if canonicalAfter != 10 {
		t.Errorf("F-125 regression: SIMCountByPolicy returned %d after stale policy_assignments insert, want 10 (canonical source is sims.policy_version_id)", canonicalAfter)
	}
}

// TestAggregates_CacheConsistency verifies that three rapid successive calls to
// SIMCountByOperator result in exactly 1 cache miss and 2 cache hits, confirming
// the Redis decorator works correctly without a live database.
func TestAggregates_CacheConsistency(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	opID := uuid.New()
	tenantID := uuid.New()

	inner := newFakeAggregates()
	inner.simCountByOperator = map[uuid.UUID]int{opID: 5}

	metrics := newFakeMetrics()
	svc := &cachedAggregates{inner: inner, rdb: rdb, ttl: defaultTTL, reg: metrics}

	ctx := context.Background()

	for i := 0; i < 3; i++ {
		v, err := svc.SIMCountByOperator(ctx, tenantID)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i+1, err)
		}
		if v[opID] != 5 {
			t.Fatalf("call %d: want count=5 for opID, got %d", i+1, v[opID])
		}
	}

	if inner.calls["SIMCountByOperator"] != 1 {
		t.Errorf("inner store calls: got %d, want 1 (should be cached after first call)", inner.calls["SIMCountByOperator"])
	}

	const method = "sim_count_by_operator"

	if metrics.misses[method] != 1 {
		t.Errorf("cache misses for %q: got %d, want 1", method, metrics.misses[method])
	}
	if metrics.hits[method] != 2 {
		t.Errorf("cache hits for %q: got %d, want 2", method, metrics.hits[method])
	}
}

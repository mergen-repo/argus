package store

// FIX-253 Task 5 — store-level regression tests for Suspend IP release, Activate nullable IP,
// and Resume nullable IP. All tests are DB-gated: they skip when DATABASE_URL is unset.
//
// Tests 1–8 follow the testBreachPool / ippool_allocation_cycle_test patterns.

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testSuspendPool connects to Postgres using DATABASE_URL. Returns nil → t.Skip().
func testSuspendPool(t *testing.T) *pgxpool.Pool {
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

// suspendTestFixture holds all seeded IDs so cleanup can be registered once.
type suspendTestFixture struct {
	tenantID   uuid.UUID
	operatorID uuid.UUID
	apnID      uuid.UUID
	poolID     uuid.UUID
	simID      uuid.UUID
	ipID       uuid.UUID
}

// seedSuspendFixture seeds tenant → operator (reuse existing) → apn → pool → ip_address → sim.
// allocation_type controls whether the IP is 'dynamic' or 'static'.
// simState is the initial SIM state (usually 'active').
// If ipID is uuid.Nil the sim is seeded without an ip_address_id (NULL).
func seedSuspendFixture(t *testing.T, pool *pgxpool.Pool, allocType string, simState string, withIP bool) suspendTestFixture {
	t.Helper()
	ctx := context.Background()
	nonce := uuid.New().ID() % 1_000_000_000

	var fix suspendTestFixture

	// Tenant
	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, contact_email)
		VALUES ('fix253-'||gen_random_uuid()::text, 'fix253@test.invalid')
		RETURNING id`).Scan(&fix.tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	// Operator (reuse first available)
	if err := pool.QueryRow(ctx, `SELECT id FROM operators LIMIT 1`).Scan(&fix.operatorID); err != nil {
		t.Fatalf("get operator: %v", err)
	}

	// APN
	if err := pool.QueryRow(ctx, `
		INSERT INTO apns (tenant_id, operator_id, name, display_name, apn_type, state)
		VALUES ($1, $2, $3, 'FIX-253 Test', 'iot', 'active')
		RETURNING id`,
		fix.tenantID, fix.operatorID,
		fmt.Sprintf("fix253-%d", nonce),
	).Scan(&fix.apnID); err != nil {
		t.Fatalf("seed apn: %v", err)
	}

	// IP Pool
	if err := pool.QueryRow(ctx, `
		INSERT INTO ip_pools (tenant_id, apn_id, name, cidr_v4, total_addresses, used_addresses, state)
		VALUES ($1, $2, 'fix253-pool', '10.253.0.0/24'::cidr, 10, 1, 'active')
		RETURNING id`,
		fix.tenantID, fix.apnID,
	).Scan(&fix.poolID); err != nil {
		t.Fatalf("seed pool: %v", err)
	}

	// IP Address
	ipState := "allocated"
	if allocType == "static" {
		ipState = "reserved"
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
		VALUES ($1, '10.253.0.1'::inet, $2, $3)
		RETURNING id`,
		fix.poolID, allocType, ipState,
	).Scan(&fix.ipID); err != nil {
		t.Fatalf("seed ip_address: %v", err)
	}

	// SIM — with or without ip_address_id
	iccid := fmt.Sprintf("8990530%09d", nonce%1_000_000_000)
	imsi := fmt.Sprintf("286990%09d", nonce%1_000_000_000)
	if len(iccid) > 22 {
		iccid = iccid[:22]
	}
	if len(imsi) > 15 {
		imsi = imsi[:15]
	}

	if withIP {
		// Link sim_id on the ip_address row first.
		if err := pool.QueryRow(ctx, `
			INSERT INTO sims (tenant_id, operator_id, apn_id, iccid, imsi, sim_type, state, ip_address_id)
			VALUES ($1, $2, $3, $4, $5, 'physical', $6, $7)
			RETURNING id`,
			fix.tenantID, fix.operatorID, fix.apnID, iccid, imsi, simState, fix.ipID,
		).Scan(&fix.simID); err != nil {
			t.Fatalf("seed sim with IP: %v", err)
		}
		// Backfill sim_id on ip_addresses
		if _, err := pool.Exec(ctx, `UPDATE ip_addresses SET sim_id = $1 WHERE id = $2`, fix.simID, fix.ipID); err != nil {
			t.Fatalf("backfill ip sim_id: %v", err)
		}
	} else {
		if err := pool.QueryRow(ctx, `
			INSERT INTO sims (tenant_id, operator_id, apn_id, iccid, imsi, sim_type, state)
			VALUES ($1, $2, $3, $4, $5, 'physical', $6)
			RETURNING id`,
			fix.tenantID, fix.operatorID, fix.apnID, iccid, imsi, simState,
		).Scan(&fix.simID); err != nil {
			t.Fatalf("seed sim without IP: %v", err)
		}
	}

	// Cleanup (LIFO: sim first, then ip, pool, apn, tenant)
	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = pool.Exec(cctx, `DELETE FROM sim_state_history WHERE sim_id = $1`, fix.simID)
		_, _ = pool.Exec(cctx, `UPDATE sims SET ip_address_id = NULL WHERE id = $1`, fix.simID)
		_, _ = pool.Exec(cctx, `DELETE FROM sims WHERE id = $1`, fix.simID)
		_, _ = pool.Exec(cctx, `DELETE FROM ip_addresses WHERE id = $1`, fix.ipID)
		_, _ = pool.Exec(cctx, `DELETE FROM ip_pools WHERE id = $1`, fix.poolID)
		_, _ = pool.Exec(cctx, `DELETE FROM apns WHERE id = $1`, fix.apnID)
		_, _ = pool.Exec(cctx, `DELETE FROM tenants WHERE id = $1`, fix.tenantID)
	})

	return fix
}

// Test 1: Suspend releases a dynamic IP atomically.
// AC-1: used_addresses decremented, ip_address back to available, sims.ip_address_id = NULL.
func TestSIMStore_Suspend_ReleasesDynamicIP(t *testing.T) {
	dbPool := testSuspendPool(t)
	if dbPool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated suspend test")
	}
	fix := seedSuspendFixture(t, dbPool, "dynamic", "active", true)
	ctx := context.Background()
	s := NewSIMStore(dbPool)

	_, err := s.Suspend(ctx, fix.tenantID, fix.simID, nil, nil)
	if err != nil {
		t.Fatalf("Suspend: %v", err)
	}

	// Assert: sims.ip_address_id = NULL
	var simIPID *uuid.UUID
	if err := dbPool.QueryRow(ctx, `SELECT ip_address_id FROM sims WHERE id = $1`, fix.simID).Scan(&simIPID); err != nil {
		t.Fatalf("read sim ip_address_id: %v", err)
	}
	if simIPID != nil {
		t.Errorf("sims.ip_address_id = %v, want NULL", *simIPID)
	}

	// Assert: ip_addresses.state = 'available', sim_id = NULL, allocated_at = NULL
	var ipState string
	var ipSimID *uuid.UUID
	var ipAllocAt *string
	if err := dbPool.QueryRow(ctx,
		`SELECT state, sim_id, allocated_at::text FROM ip_addresses WHERE id = $1`, fix.ipID,
	).Scan(&ipState, &ipSimID, &ipAllocAt); err != nil {
		t.Fatalf("read ip_address: %v", err)
	}
	if ipState != "available" {
		t.Errorf("ip_addresses.state = %q, want 'available'", ipState)
	}
	if ipSimID != nil {
		t.Errorf("ip_addresses.sim_id = %v, want NULL", *ipSimID)
	}
	if ipAllocAt != nil {
		t.Errorf("ip_addresses.allocated_at = %v, want NULL", *ipAllocAt)
	}

	// Assert: ip_pools.used_addresses decremented (was 1, expect 0)
	var usedAddresses int
	if err := dbPool.QueryRow(ctx, `SELECT used_addresses FROM ip_pools WHERE id = $1`, fix.poolID).Scan(&usedAddresses); err != nil {
		t.Fatalf("read pool: %v", err)
	}
	if usedAddresses != 0 {
		t.Errorf("ip_pools.used_addresses = %d, want 0", usedAddresses)
	}

	// Assert: state_history row for suspend
	var histCount int
	if err := dbPool.QueryRow(ctx, `SELECT COUNT(*) FROM sim_state_history WHERE sim_id = $1 AND to_state = 'suspended'`, fix.simID).Scan(&histCount); err != nil {
		t.Fatalf("read state_history: %v", err)
	}
	if histCount < 1 {
		t.Errorf("no suspend state_history row found")
	}
}

// Test 2: Suspend preserves a static IP — ip_address row untouched.
// AC-1: allocation_type='static' → skip release block entirely.
func TestSIMStore_Suspend_PreservesStaticIP(t *testing.T) {
	dbPool := testSuspendPool(t)
	if dbPool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated suspend test")
	}
	fix := seedSuspendFixture(t, dbPool, "static", "active", true)
	ctx := context.Background()
	s := NewSIMStore(dbPool)

	_, err := s.Suspend(ctx, fix.tenantID, fix.simID, nil, nil)
	if err != nil {
		t.Fatalf("Suspend: %v", err)
	}

	// Assert: ip_addresses row UNTOUCHED (state still 'reserved', sim_id still set)
	var ipState string
	var ipSimID *uuid.UUID
	if err := dbPool.QueryRow(ctx,
		`SELECT state, sim_id FROM ip_addresses WHERE id = $1`, fix.ipID,
	).Scan(&ipState, &ipSimID); err != nil {
		t.Fatalf("read ip_address: %v", err)
	}
	if ipState != "reserved" {
		t.Errorf("static IP: ip_addresses.state = %q, want 'reserved'", ipState)
	}
	if ipSimID == nil || *ipSimID != fix.simID {
		t.Errorf("static IP: ip_addresses.sim_id = %v, want %v", ipSimID, fix.simID)
	}

	// Assert: pool used_addresses unchanged (was 1, should still be 1 for static)
	var usedAddresses int
	if err := dbPool.QueryRow(ctx, `SELECT used_addresses FROM ip_pools WHERE id = $1`, fix.poolID).Scan(&usedAddresses); err != nil {
		t.Fatalf("read pool: %v", err)
	}
	if usedAddresses != 1 {
		t.Errorf("static IP: ip_pools.used_addresses = %d, want 1 (unchanged)", usedAddresses)
	}
}

// Test 3: Suspend with ip_address_id = NULL is a no-op (no error, state transitions).
func TestSIMStore_Suspend_NullIPNoOp(t *testing.T) {
	dbPool := testSuspendPool(t)
	if dbPool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated suspend test")
	}
	fix := seedSuspendFixture(t, dbPool, "dynamic", "active", false)
	ctx := context.Background()
	s := NewSIMStore(dbPool)

	sim, err := s.Suspend(ctx, fix.tenantID, fix.simID, nil, nil)
	if err != nil {
		t.Fatalf("Suspend(nil IP): %v", err)
	}
	if sim.State != "suspended" {
		t.Errorf("sim.State = %q, want 'suspended'", sim.State)
	}

	// No ip_addresses rows should exist for this pool's SIM
	var count int
	if err := dbPool.QueryRow(ctx, `SELECT COUNT(*) FROM ip_addresses WHERE sim_id = $1`, fix.simID).Scan(&count); err != nil {
		t.Fatalf("count ip rows: %v", err)
	}
	if count != 0 {
		t.Errorf("ip_addresses with sim_id=%v = %d rows, want 0", fix.simID, count)
	}
}

// Test 4: Suspend when the referenced ip_address is in 'available' state (not 'allocated'/'reserved').
// This simulates an already-released IP — the SELECT ... WHERE state IN ('allocated','reserved')
// returns ErrNoRows, triggering the defensive NULL path without failing the suspend.
func TestSIMStore_Suspend_OrphanedIPRefDefensive(t *testing.T) {
	dbPool := testSuspendPool(t)
	if dbPool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated suspend test")
	}
	fix := seedSuspendFixture(t, dbPool, "dynamic", "active", true)
	ctx := context.Background()

	// Force the ip_addresses row to 'available' state (simulates a double-release / stale ref).
	// The FK (ip_address_id → ip_addresses.id) still holds, so PG won't reject the sims row.
	// But the Suspend's SELECT ... WHERE state IN ('allocated','reserved') will return ErrNoRows.
	if _, err := dbPool.Exec(ctx, `UPDATE ip_addresses SET state = 'available', sim_id = NULL, allocated_at = NULL WHERE id = $1`, fix.ipID); err != nil {
		t.Fatalf("force ip to available: %v", err)
	}

	s := NewSIMStore(dbPool)
	sim, err := s.Suspend(ctx, fix.tenantID, fix.simID, nil, nil)
	if err != nil {
		t.Fatalf("Suspend(stale ip ref): %v", err)
	}
	if sim.State != "suspended" {
		t.Errorf("sim.State = %q, want 'suspended'", sim.State)
	}
	// ip_address_id should be NULL'd defensively
	var simIPID *uuid.UUID
	if err := dbPool.QueryRow(ctx, `SELECT ip_address_id FROM sims WHERE id = $1`, fix.simID).Scan(&simIPID); err != nil {
		t.Fatalf("read sim ip_address_id: %v", err)
	}
	if simIPID != nil {
		t.Errorf("stale ip_address_id should be NULLed, got %v", *simIPID)
	}
}

// Test 5: Suspend un-exhausts a pool that had state='exhausted'.
func TestSIMStore_Suspend_ExhaustedPoolUnExhausts(t *testing.T) {
	dbPool := testSuspendPool(t)
	if dbPool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated suspend test")
	}
	fix := seedSuspendFixture(t, dbPool, "dynamic", "active", true)
	ctx := context.Background()

	// Mark pool as exhausted
	if _, err := dbPool.Exec(ctx, `UPDATE ip_pools SET state = 'exhausted' WHERE id = $1`, fix.poolID); err != nil {
		t.Fatalf("set pool exhausted: %v", err)
	}

	s := NewSIMStore(dbPool)
	_, err := s.Suspend(ctx, fix.tenantID, fix.simID, nil, nil)
	if err != nil {
		t.Fatalf("Suspend: %v", err)
	}

	var poolState string
	if err := dbPool.QueryRow(ctx, `SELECT state FROM ip_pools WHERE id = $1`, fix.poolID).Scan(&poolState); err != nil {
		t.Fatalf("read pool state: %v", err)
	}
	if poolState != "active" {
		t.Errorf("ip_pools.state = %q after release from exhausted pool, want 'active'", poolState)
	}
}

// Test 6: Activate with nil ipAddressID — UPDATE must not touch ip_address_id (no FK violation).
// DEV-393 defensive: nil → OmitsColumn.
func TestSIMStore_Activate_NilIPAddressID_OmitsColumn(t *testing.T) {
	dbPool := testSuspendPool(t)
	if dbPool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated activate test")
	}
	// Seed SIM in 'suspended' state without IP
	fix := seedSuspendFixture(t, dbPool, "dynamic", "suspended", false)
	ctx := context.Background()
	s := NewSIMStore(dbPool)

	sim, err := s.Activate(ctx, fix.tenantID, fix.simID, nil, nil)
	if err != nil {
		t.Fatalf("Activate(nil IP): %v", err)
	}
	if sim.State != "active" {
		t.Errorf("sim.State = %q, want 'active'", sim.State)
	}
	if sim.IPAddressID != nil {
		t.Errorf("sim.IPAddressID = %v, want nil (column must not be written)", *sim.IPAddressID)
	}

	// Verify in DB directly
	var dbIPID *uuid.UUID
	if err := dbPool.QueryRow(ctx, `SELECT ip_address_id FROM sims WHERE id = $1`, fix.simID).Scan(&dbIPID); err != nil {
		t.Fatalf("read sim: %v", err)
	}
	if dbIPID != nil {
		t.Errorf("db ip_address_id = %v, want NULL", *dbIPID)
	}
}

// Test 7: Activate with non-nil ipAddressID sets the column.
func TestSIMStore_Activate_NonNilIPAddressID_SetsColumn(t *testing.T) {
	dbPool := testSuspendPool(t)
	if dbPool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated activate test")
	}
	fix := seedSuspendFixture(t, dbPool, "dynamic", "suspended", false)
	ctx := context.Background()

	// Create a separate IP address row to assign
	var newIPID uuid.UUID
	if err := dbPool.QueryRow(ctx, `
		INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
		VALUES ($1, '10.253.0.99'::inet, 'dynamic', 'allocated')
		RETURNING id`, fix.poolID).Scan(&newIPID); err != nil {
		t.Fatalf("seed ip for activate test: %v", err)
	}
	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = dbPool.Exec(cctx, `UPDATE sims SET ip_address_id = NULL WHERE id = $1`, fix.simID)
		_, _ = dbPool.Exec(cctx, `DELETE FROM ip_addresses WHERE id = $1`, newIPID)
	})

	s := NewSIMStore(dbPool)
	sim, err := s.Activate(ctx, fix.tenantID, fix.simID, &newIPID, nil)
	if err != nil {
		t.Fatalf("Activate(&ipID): %v", err)
	}
	if sim.State != "active" {
		t.Errorf("sim.State = %q, want 'active'", sim.State)
	}
	if sim.IPAddressID == nil || *sim.IPAddressID != newIPID {
		t.Errorf("sim.IPAddressID = %v, want %v", sim.IPAddressID, newIPID)
	}
}

// Test 8: Resume with nil ipAddressID — UPDATE must not overwrite ip_address_id.
// Mirrors test 6 but for the Resume path.
func TestSIMStore_Resume_NilIPAddressID_OmitsColumn(t *testing.T) {
	dbPool := testSuspendPool(t)
	if dbPool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated resume test")
	}
	fix := seedSuspendFixture(t, dbPool, "dynamic", "suspended", false)
	ctx := context.Background()
	s := NewSIMStore(dbPool)

	sim, err := s.Resume(ctx, fix.tenantID, fix.simID, nil, nil)
	if err != nil {
		t.Fatalf("Resume(nil IP): %v", err)
	}
	if sim.State != "active" {
		t.Errorf("sim.State = %q, want 'active'", sim.State)
	}
	if sim.IPAddressID != nil {
		t.Errorf("sim.IPAddressID = %v, want nil (column must not be written)", *sim.IPAddressID)
	}

	// Verify in DB
	var dbIPID *uuid.UUID
	if err := dbPool.QueryRow(ctx, `SELECT ip_address_id FROM sims WHERE id = $1`, fix.simID).Scan(&dbIPID); err != nil {
		t.Fatalf("read sim: %v", err)
	}
	if dbIPID != nil {
		t.Errorf("db ip_address_id = %v, want NULL", *dbIPID)
	}
}

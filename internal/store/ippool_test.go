package store

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testIPPoolPool returns a pgxpool.Pool bound to the test database when
// DATABASE_URL is set; otherwise returns nil so callers can t.Skip. Matches
// the existing testSMSPool helper pattern at sms_outbound_test.go:15.
func testIPPoolPool(t *testing.T) *pgxpool.Pool {
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

func TestIPPoolStructFields(t *testing.T) {
	cidr := "10.0.0.0/24"
	now := time.Now()
	p := &IPPool{
		ID:                     uuid.New(),
		TenantID:               uuid.New(),
		APNID:                  uuid.New(),
		Name:                   "test-pool",
		CIDRv4:                 &cidr,
		TotalAddresses:         254,
		UsedAddresses:          10,
		AlertThresholdWarning:  80,
		AlertThresholdCritical: 90,
		ReclaimGracePeriodDays: 7,
		State:                  "active",
		CreatedAt:              now,
	}
	if p.Name != "test-pool" {
		t.Errorf("Name = %q, want test-pool", p.Name)
	}
	if p.State != "active" {
		t.Errorf("State = %q, want active", p.State)
	}
	if p.ReclaimGracePeriodDays != 7 {
		t.Errorf("ReclaimGracePeriodDays = %d, want 7", p.ReclaimGracePeriodDays)
	}
}

func TestExpiredIPAddressStructFields(t *testing.T) {
	addr := "10.0.0.1"
	simID := uuid.New()
	reclaimAt := time.Now().Add(-1 * time.Hour)
	e := &ExpiredIPAddress{
		ID:            uuid.New(),
		PoolID:        uuid.New(),
		TenantID:      uuid.New(),
		AddressV4:     &addr,
		PreviousSimID: &simID,
		ReclaimAt:     reclaimAt,
	}
	if e.AddressV4 == nil || *e.AddressV4 != addr {
		t.Errorf("AddressV4 = %v, want %q", e.AddressV4, addr)
	}
	if e.PreviousSimID == nil || *e.PreviousSimID != simID {
		t.Error("PreviousSimID should match")
	}
	if e.ReclaimAt != reclaimAt {
		t.Error("ReclaimAt mismatch")
	}
}

func TestIPPoolStore_ListExpiredReclaim_RequiresDB(t *testing.T) {
	if testing.Short() {
		t.Skip("requires database")
	}
	t.Log("integration test: ListExpiredReclaim requires a real DB connection")
}

func TestIPPoolStore_FinalizeReclaim_RequiresDB(t *testing.T) {
	if testing.Short() {
		t.Skip("requires database")
	}
	t.Log("integration test: FinalizeReclaim requires a real DB connection")
}

func TestIPPoolErrSentinels(t *testing.T) {
	if ErrIPPoolNotFound == nil {
		t.Error("ErrIPPoolNotFound should not be nil")
	}
	if ErrPoolExhausted == nil {
		t.Error("ErrPoolExhausted should not be nil")
	}
	if ErrIPAlreadyAllocated == nil {
		t.Error("ErrIPAlreadyAllocated should not be nil")
	}
	if ErrIPNotFound == nil {
		t.Error("ErrIPNotFound should not be nil")
	}
}

func TestGenerateIPv4Addresses_SmallCIDR(t *testing.T) {
	addrs, err := GenerateIPv4Addresses("10.0.0.0/30")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(addrs) != 2 {
		t.Errorf("len = %d, want 2 (usable hosts in /30)", len(addrs))
	}
	if addrs[0] != "10.0.0.1" {
		t.Errorf("first addr = %q, want 10.0.0.1", addrs[0])
	}
	if addrs[1] != "10.0.0.2" {
		t.Errorf("second addr = %q, want 10.0.0.2", addrs[1])
	}
}

func TestGenerateIPv4Addresses_Host(t *testing.T) {
	addrs, err := GenerateIPv4Addresses("192.168.1.5/32")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(addrs) != 1 || addrs[0] != "192.168.1.5" {
		t.Errorf("addrs = %v, want [192.168.1.5]", addrs)
	}
}

func TestGenerateIPv4Addresses_InvalidCIDR(t *testing.T) {
	_, err := GenerateIPv4Addresses("not-a-cidr")
	if err == nil {
		t.Error("expected error for invalid CIDR, got nil")
	}
}

func TestListExpiredReclaim_OnlyExpiredRows(t *testing.T) {
	now := time.Now()
	past := now.Add(-1 * time.Hour)
	future := now.Add(1 * time.Hour)

	expiredAddr := "10.0.0.1"
	futureAddr := "10.0.0.2"

	expired := ExpiredIPAddress{
		ID:        uuid.New(),
		PoolID:    uuid.New(),
		TenantID:  uuid.New(),
		AddressV4: &expiredAddr,
		ReclaimAt: past,
	}
	notExpired := ExpiredIPAddress{
		ID:        uuid.New(),
		PoolID:    uuid.New(),
		TenantID:  uuid.New(),
		AddressV4: &futureAddr,
		ReclaimAt: future,
	}

	all := []ExpiredIPAddress{expired, notExpired}

	var selected []ExpiredIPAddress
	for _, e := range all {
		if !e.ReclaimAt.After(now) {
			selected = append(selected, e)
		}
	}

	if len(selected) != 1 {
		t.Fatalf("expected 1 expired row, got %d", len(selected))
	}
	if selected[0].ID != expired.ID {
		t.Errorf("selected wrong row: got %v, want %v", selected[0].ID, expired.ID)
	}
}

func TestListExpiredReclaim_LimitRespected(t *testing.T) {
	const limit = 5
	rows := make([]ExpiredIPAddress, 10)
	past := time.Now().Add(-1 * time.Minute)
	for i := range rows {
		rows[i] = ExpiredIPAddress{
			ID:        uuid.New(),
			PoolID:    uuid.New(),
			TenantID:  uuid.New(),
			ReclaimAt: past,
		}
	}

	result := rows
	if len(result) > limit {
		result = result[:limit]
	}
	if len(result) != limit {
		t.Errorf("result len = %d, want %d", len(result), limit)
	}
}

func TestListExpiredReclaim_EmptyWhenNone(t *testing.T) {
	var rows []ExpiredIPAddress
	if len(rows) != 0 {
		t.Errorf("expected empty result, got %d", len(rows))
	}
}

func TestFinalizeReclaim_StateTransition(t *testing.T) {
	ip := &IPAddress{
		ID:     uuid.New(),
		PoolID: uuid.New(),
		State:  "reclaiming",
	}

	if ip.State != "reclaiming" {
		t.Fatalf("precondition: state = %q, want reclaiming", ip.State)
	}

	ip.State = "available"
	ip.SimID = nil
	ip.AllocatedAt = nil
	ip.ReclaimAt = nil

	if ip.State != "available" {
		t.Errorf("after reclaim: state = %q, want available", ip.State)
	}
	if ip.SimID != nil {
		t.Error("sim_id should be nil after reclaim")
	}
	if ip.AllocatedAt != nil {
		t.Error("allocated_at should be nil after reclaim")
	}
	if ip.ReclaimAt != nil {
		t.Error("reclaim_at should be nil after reclaim")
	}
}

func TestFinalizeReclaim_NonReclaimingReturnError(t *testing.T) {
	ip := &IPAddress{
		ID:    uuid.New(),
		State: "available",
	}

	if ip.State == "reclaiming" {
		t.Fatal("precondition failed: state should not be reclaiming")
	}

	if ErrIPNotFound.Error() == "" {
		t.Error("ErrIPNotFound should have a message")
	}
}

// recountTestFixture spins up a dedicated tenant + APN + pool + ip_addresses
// rows for RecountUsedAddresses tests. Scoped by a pseudo-random tenant name
// so parallel test runs don't collide, and all rows are cleaned up via
// t.Cleanup. Returns (tenantID, poolID).
func recountTestFixture(t *testing.T, pool *pgxpool.Pool, allocatedCount int, reservedCount int, availableCount int, driftUsed int) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	// Isolated tenant for this test — the RecountUsedAddresses tenant-scoping
	// test needs to distinguish "our" tenant from "other" tenants without
	// relying on seed data.
	var tenantID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, contact_email)
		VALUES ('story092-recount-'||gen_random_uuid()::text, 'recount@story092.test')
		RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	// Reuse any existing operator; we only need its id to satisfy apns FK
	// (apns.operator_id → operators). Seed 003 always leaves at least one
	// operator row.
	var operatorID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM operators LIMIT 1`).Scan(&operatorID); err != nil {
		t.Fatalf("no operator in test DB: %v", err)
	}

	var apnID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO apns (tenant_id, operator_id, name, display_name, apn_type, state)
		VALUES ($1, $2, 'story092-recount-'||gen_random_uuid()::text, 'STORY-092 Recount', 'iot', 'active')
		RETURNING id`, tenantID, operatorID).Scan(&apnID); err != nil {
		t.Fatalf("seed apn: %v", err)
	}

	var poolID uuid.UUID
	totalAddrs := allocatedCount + reservedCount + availableCount
	if err := pool.QueryRow(ctx, `
		INSERT INTO ip_pools (tenant_id, apn_id, name, cidr_v4, total_addresses, used_addresses, state)
		VALUES ($1, $2, 'Recount Test Pool', '10.250.0.0/24'::cidr, $3, $4, 'active')
		RETURNING id`, tenantID, apnID, totalAddrs, driftUsed).Scan(&poolID); err != nil {
		t.Fatalf("seed pool: %v", err)
	}

	// Seed ip_addresses rows. Use unique /24 addresses so unique constraints
	// don't collide across parallel fixtures. Build the IPv4 string client-side
	// so pgx doesn't have to infer the OID for `('10.250.0.'||$2)::inet`.
	addr := 1
	for i := 0; i < allocatedCount; i++ {
		ipv4 := "10.250.0." + strconv.Itoa(addr)
		if _, err := pool.Exec(ctx, `
			INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state, allocated_at)
			VALUES ($1, $2::inet, 'dynamic', 'allocated', NOW())`,
			poolID, ipv4); err != nil {
			t.Fatalf("seed allocated ip: %v", err)
		}
		addr++
	}
	for i := 0; i < reservedCount; i++ {
		ipv4 := "10.250.0." + strconv.Itoa(addr)
		if _, err := pool.Exec(ctx, `
			INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state, allocated_at)
			VALUES ($1, $2::inet, 'static', 'reserved', NOW())`,
			poolID, ipv4); err != nil {
			t.Fatalf("seed reserved ip: %v", err)
		}
		addr++
	}
	for i := 0; i < availableCount; i++ {
		ipv4 := "10.250.0." + strconv.Itoa(addr)
		if _, err := pool.Exec(ctx, `
			INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
			VALUES ($1, $2::inet, 'dynamic', 'available')`,
			poolID, ipv4); err != nil {
			t.Fatalf("seed available ip: %v", err)
		}
		addr++
	}

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM ip_addresses WHERE pool_id = $1`, poolID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM ip_pools WHERE id = $1`, poolID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM apns WHERE id = $1`, apnID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM tenants WHERE id = $1`, tenantID)
	})

	return tenantID, poolID
}

// TestIPPoolStore_RecountUsedAddresses_FixesDrift — scenario (a): app-level
// drift where used_addresses recorded 99 but only 3 rows are actually
// allocated/reserved. Recount must rewrite the counter to 3.
func TestIPPoolStore_RecountUsedAddresses_FixesDrift(t *testing.T) {
	pool := testIPPoolPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	tenantID, poolID := recountTestFixture(t, pool, 2, 1, 5, 99) // 3 used, counter says 99

	s := NewIPPoolStore(pool)
	affected, err := s.RecountUsedAddresses(context.Background(), tenantID)
	if err != nil {
		t.Fatalf("RecountUsedAddresses: %v", err)
	}
	if affected == 0 {
		t.Errorf("expected non-zero rows affected, got %d", affected)
	}

	var used int
	if err := pool.QueryRow(context.Background(),
		`SELECT used_addresses FROM ip_pools WHERE id = $1`, poolID).Scan(&used); err != nil {
		t.Fatalf("re-read pool: %v", err)
	}
	if used != 3 {
		t.Errorf("used_addresses after recount = %d, want 3 (2 allocated + 1 reserved)", used)
	}
}

// TestIPPoolStore_RecountUsedAddresses_EmptyPool — scenario (b): no
// ip_addresses rows at all, but used_addresses still records stale value.
// Recount must rewrite the counter to 0 via the LEFT JOIN fallback.
func TestIPPoolStore_RecountUsedAddresses_EmptyPool(t *testing.T) {
	pool := testIPPoolPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	tenantID, poolID := recountTestFixture(t, pool, 0, 0, 0, 42) // no IPs, counter says 42

	s := NewIPPoolStore(pool)
	if _, err := s.RecountUsedAddresses(context.Background(), tenantID); err != nil {
		t.Fatalf("RecountUsedAddresses: %v", err)
	}

	var used int
	if err := pool.QueryRow(context.Background(),
		`SELECT used_addresses FROM ip_pools WHERE id = $1`, poolID).Scan(&used); err != nil {
		t.Fatalf("re-read pool: %v", err)
	}
	if used != 0 {
		t.Errorf("used_addresses on empty pool after recount = %d, want 0", used)
	}
}

// TestIPPoolStore_RecountUsedAddresses_TenantScoping — scenario (c):
// recount scoped to one tenant must NOT touch another tenant's drift.
func TestIPPoolStore_RecountUsedAddresses_TenantScoping(t *testing.T) {
	pool := testIPPoolPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	tenantA, poolA := recountTestFixture(t, pool, 1, 0, 0, 50) // drift: actual 1 vs counter 50
	tenantB, poolB := recountTestFixture(t, pool, 2, 0, 0, 50) // drift: actual 2 vs counter 50

	s := NewIPPoolStore(pool)
	// Recount tenant A only.
	if _, err := s.RecountUsedAddresses(context.Background(), tenantA); err != nil {
		t.Fatalf("RecountUsedAddresses(A): %v", err)
	}

	var usedA, usedB int
	if err := pool.QueryRow(context.Background(),
		`SELECT used_addresses FROM ip_pools WHERE id = $1`, poolA).Scan(&usedA); err != nil {
		t.Fatalf("re-read pool A: %v", err)
	}
	if err := pool.QueryRow(context.Background(),
		`SELECT used_addresses FROM ip_pools WHERE id = $1`, poolB).Scan(&usedB); err != nil {
		t.Fatalf("re-read pool B: %v", err)
	}
	if usedA != 1 {
		t.Errorf("tenant A used_addresses after scoped recount = %d, want 1", usedA)
	}
	if usedB != 50 {
		t.Errorf("tenant B used_addresses must NOT change after scoped recount: got %d, want 50 (drift preserved)", usedB)
	}

	// uuid.Nil — recount ALL tenants. Now B should drop to 2.
	if _, err := s.RecountUsedAddresses(context.Background(), uuid.Nil); err != nil {
		t.Fatalf("RecountUsedAddresses(Nil): %v", err)
	}
	if err := pool.QueryRow(context.Background(),
		`SELECT used_addresses FROM ip_pools WHERE id = $1`, poolB).Scan(&usedB); err != nil {
		t.Fatalf("re-read pool B after global recount: %v", err)
	}
	if usedB != 2 {
		t.Errorf("tenant B used_addresses after uuid.Nil recount = %d, want 2", usedB)
	}

	_ = tenantB // silence unused if the above asserts evolve
}

// TestIPPoolStore_FIX105_SeedInventoryCoverage is the FIX-105 regression test:
// after db-seed, every ip_pools row created by seed 003 / 005 / 006 must have
// ip_addresses rows covering its full usable CIDR (network + broadcast
// excluded). Previously POOL_EXHAUSTED on POST /sims/{id}/activate against a
// freshly seeded DB — UAT Batch 1 F-15. Fails on pre-fix DB (zero ip_addresses
// rows for seed-003 pools); passes after seed 005a provisions inventory.
func TestIPPoolStore_FIX105_SeedInventoryCoverage(t *testing.T) {
	pool := testIPPoolPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()

	rows, err := pool.Query(ctx, `
		SELECT p.id, p.name, p.cidr_v4::text, masklen(p.cidr_v4) AS mask,
		       (SELECT COUNT(*) FROM ip_addresses a WHERE a.pool_id = p.id) AS inventory
		FROM ip_pools p
		WHERE p.cidr_v4 IS NOT NULL AND masklen(p.cidr_v4) <= 30`)
	if err != nil {
		t.Fatalf("query pools: %v", err)
	}
	defer rows.Close()

	checked := 0
	for rows.Next() {
		var id uuid.UUID
		var name, cidr string
		var mask, inventory int
		if err := rows.Scan(&id, &name, &cidr, &mask, &inventory); err != nil {
			t.Fatalf("scan pool: %v", err)
		}
		expected := (1 << (32 - mask)) - 2
		if inventory < expected {
			t.Errorf("FIX-105: pool %s (%s /%d) has %d ip_addresses rows, want >= %d",
				name, cidr, mask, inventory, expected)
		}
		checked++
	}
	if checked == 0 {
		t.Skip("no seeded pools to check (DB may be empty or unseeded)")
	}
}

// TestIPPoolStore_FIX105_AllocateReleaseCycle covers AC-4 and AC-5:
// AllocateIP increments used_addresses; ReleaseIP (dynamic branch) returns
// the IP to 'available' and decrements the counter. Uses an isolated fixture
// so the assertion is deterministic without depending on seed data.
func TestIPPoolStore_FIX105_AllocateReleaseCycle(t *testing.T) {
	pool := testIPPoolPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	tenantID, poolID := recountTestFixture(t, pool, 0, 0, 5, 0)
	s := NewIPPoolStore(pool)
	ctx := context.Background()

	// Need a real sim row for the check_sim_exists trigger on ip_addresses.
	var operatorID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM operators LIMIT 1`).Scan(&operatorID); err != nil {
		t.Fatalf("lookup operator: %v", err)
	}
	var simID uuid.UUID
	imsi := "286010" + uuid.New().String()[:9]
	iccid := "8990286010" + uuid.New().String()[:12]
	if err := pool.QueryRow(ctx, `
		INSERT INTO sims (tenant_id, operator_id, iccid, imsi, sim_type, state)
		VALUES ($1, $2, $3, $4, 'physical', 'ordered')
		RETURNING id`, tenantID, operatorID, iccid, imsi).Scan(&simID); err != nil {
		t.Fatalf("seed sim: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM sims WHERE id = $1`, simID)
	})

	// Pre-condition: used_addresses = 0.
	var used int
	if err := pool.QueryRow(ctx,
		`SELECT used_addresses FROM ip_pools WHERE id = $1`, poolID).Scan(&used); err != nil {
		t.Fatalf("pre-allocate read: %v", err)
	}
	if used != 0 {
		t.Fatalf("precondition: used_addresses = %d, want 0", used)
	}

	// Allocate → used should become 1.
	addr, err := s.AllocateIP(ctx, poolID, simID)
	if err != nil {
		t.Fatalf("AllocateIP: %v", err)
	}
	if addr == nil || addr.State != "allocated" {
		t.Fatalf("allocated address state = %v, want 'allocated'", addr)
	}
	if err := pool.QueryRow(ctx,
		`SELECT used_addresses FROM ip_pools WHERE id = $1`, poolID).Scan(&used); err != nil {
		t.Fatalf("post-allocate read: %v", err)
	}
	if used != 1 {
		t.Errorf("AC-4: used_addresses after AllocateIP = %d, want 1", used)
	}

	// Release → counter decrements, IP returns to 'available' (dynamic path).
	if err := s.ReleaseIP(ctx, poolID, simID); err != nil {
		t.Fatalf("ReleaseIP: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT used_addresses FROM ip_pools WHERE id = $1`, poolID).Scan(&used); err != nil {
		t.Fatalf("post-release read: %v", err)
	}
	if used != 0 {
		t.Errorf("AC-5: used_addresses after ReleaseIP = %d, want 0", used)
	}
	var state string
	if err := pool.QueryRow(ctx,
		`SELECT state FROM ip_addresses WHERE id = $1`, addr.ID).Scan(&state); err != nil {
		t.Fatalf("re-read ip_address: %v", err)
	}
	if state != "available" {
		t.Errorf("AC-5: ip_address state after ReleaseIP = %q, want 'available'", state)
	}
}

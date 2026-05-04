package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// TestIPPoolStore_AllocateReleaseCycle — STORY-092 AC-8.
//
// Property test: N=100 AllocateIP→ReleaseIP cycles distributed across 10
// concurrent goroutines (each doing 10 cycles) must leave ip_pools.used_addresses
// equal to the initial counter. The test provisions one tenant + APN +
// pool + 5 available IP addresses (enough headroom to avoid transient
// ErrPoolExhausted under concurrency) and a SIM per goroutine so each
// Allocate/Release pair is FK-unique.
//
// This test exercises the FOR UPDATE SKIP LOCKED contention path inside
// AllocateIP (ippool.go:635) and the used_addresses arithmetic inside
// ReleaseIP (ippool.go:703). -race catches any data-race in the Go-side
// wrapper; the SQL-side race is guarded by FOR UPDATE on the pool row.
func TestIPPoolStore_AllocateReleaseCycle(t *testing.T) {
	pool := testIPPoolPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()

	// Seed: tenant → operator → apn → pool with 5 available dynamic IPs.
	var tenantID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, contact_email)
		VALUES ('story092-cycle-'||gen_random_uuid()::text, 'cycle@story092.test')
		RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	var operatorID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM operators LIMIT 1`).Scan(&operatorID); err != nil {
		t.Fatalf("no operator: %v", err)
	}

	var apnID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO apns (tenant_id, operator_id, name, display_name, apn_type, state)
		VALUES ($1, $2, 'story092-cycle-'||gen_random_uuid()::text, 'STORY-092 Cycle', 'iot', 'active')
		RETURNING id`, tenantID, operatorID).Scan(&apnID); err != nil {
		t.Fatalf("seed apn: %v", err)
	}

	var poolID uuid.UUID
	const poolSize = 5
	if err := pool.QueryRow(ctx, `
		INSERT INTO ip_pools (tenant_id, apn_id, name, cidr_v4, total_addresses, used_addresses, state)
		VALUES ($1, $2, 'STORY-092 Cycle Pool', '10.249.0.0/24'::cidr, $3, 0, 'active')
		RETURNING id`, tenantID, apnID, poolSize).Scan(&poolID); err != nil {
		t.Fatalf("seed pool: %v", err)
	}

	for i := 1; i <= poolSize; i++ {
		ipv4 := "10.249.0." + strconv.Itoa(i)
		if _, err := pool.Exec(ctx, `
			INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
			VALUES ($1, $2::inet, 'dynamic', 'available')`, poolID, ipv4); err != nil {
			t.Fatalf("seed ip %s: %v", ipv4, err)
		}
	}

	// Seed 10 SIMs (one per goroutine) so each cycle binds a distinct sim_id
	// — avoids the ip_addresses.sim_id uniqueness surprise if present.
	const goroutines = 10
	simIDs := make([]uuid.UUID, goroutines)
	for i := 0; i < goroutines; i++ {
		// ICCID: 20 digits. IMSI: 15 digits. MSISDN: 12 digits. Build with a
		// per-goroutine index + per-test-run nanosecond suffix so parallel
		// runs and N>1 test cycles stay unique.
		nonce := uuid.New().ID() % 1_000_000_000 // 9-digit cap
		iccid := fmt.Sprintf("899099%d%09d", i%10, nonce)
		imsi := fmt.Sprintf("286995%d%08d", i%10, nonce%100_000_000)
		msisdn := fmt.Sprintf("9057%d%07d", i%10, nonce%10_000_000)
		var simID uuid.UUID
		if err := pool.QueryRow(ctx, `
			INSERT INTO sims (tenant_id, operator_id, apn_id, iccid, imsi, msisdn, sim_type, state, rat_type)
			VALUES ($1, $2, $3, $4, $5, $6, 'physical', 'active', 'lte')
			RETURNING id`, tenantID, operatorID, apnID, iccid, imsi, msisdn).Scan(&simID); err != nil {
			t.Fatalf("seed sim %d: %v", i, err)
		}
		simIDs[i] = simID
	}

	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = pool.Exec(cctx, `UPDATE sims SET ip_address_id = NULL WHERE tenant_id = $1`, tenantID)
		_, _ = pool.Exec(cctx, `DELETE FROM ip_addresses WHERE pool_id = $1`, poolID)
		_, _ = pool.Exec(cctx, `DELETE FROM ip_pools WHERE id = $1`, poolID)
		for _, simID := range simIDs {
			_, _ = pool.Exec(cctx, `DELETE FROM sims WHERE id = $1`, simID)
		}
		_, _ = pool.Exec(cctx, `DELETE FROM apns WHERE id = $1`, apnID)
		_, _ = pool.Exec(cctx, `DELETE FROM tenants WHERE id = $1`, tenantID)
	})

	// Record initial counter — should be 0 by our seed.
	var initialUsed int
	if err := pool.QueryRow(ctx, `SELECT used_addresses FROM ip_pools WHERE id = $1`, poolID).Scan(&initialUsed); err != nil {
		t.Fatalf("read initial used_addresses: %v", err)
	}
	if initialUsed != 0 {
		t.Fatalf("precondition: initial used_addresses = %d, want 0", initialUsed)
	}

	s := NewIPPoolStore(pool)

	// 10 goroutines × 10 cycles = 100 Allocate+Release pairs.
	const cyclesPerGoroutine = 10
	const totalCycles = goroutines * cyclesPerGoroutine
	var wg sync.WaitGroup
	wg.Add(goroutines)

	errCh := make(chan error, totalCycles)
	for g := 0; g < goroutines; g++ {
		simID := simIDs[g]
		go func(simID uuid.UUID) {
			defer wg.Done()
			for c := 0; c < cyclesPerGoroutine; c++ {
				// AllocateIP: may transiently hit ErrPoolExhausted if all 5 IPs
				// are held by other goroutines at this instant — retry until it
				// succeeds. Cap attempts so a true exhaustion bug doesn't spin.
				var allocated *IPAddress
				var allocErr error
				for attempt := 0; attempt < 50; attempt++ {
					allocated, allocErr = s.AllocateIP(context.Background(), poolID, simID)
					if allocErr == nil {
						break
					}
					if !errors.Is(allocErr, ErrPoolExhausted) {
						break
					}
				}
				if allocErr != nil {
					errCh <- fmt.Errorf("goroutine sim=%s cycle=%d allocate: %w", simID, c, allocErr)
					return
				}
				if allocated == nil {
					errCh <- fmt.Errorf("goroutine sim=%s cycle=%d: Allocate returned nil", simID, c)
					return
				}

				if err := s.ReleaseIP(context.Background(), poolID, simID); err != nil {
					errCh <- fmt.Errorf("goroutine sim=%s cycle=%d release: %w", simID, c, err)
					return
				}
			}
		}(simID)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("cycle error: %v", err)
	}
	if t.Failed() {
		return
	}

	// After all cycles, used_addresses must return to the initial value (0)
	// — every Allocate was matched by a Release for a 'dynamic' IP.
	var finalUsed int
	if err := pool.QueryRow(ctx, `SELECT used_addresses FROM ip_pools WHERE id = $1`, poolID).Scan(&finalUsed); err != nil {
		t.Fatalf("read final used_addresses: %v", err)
	}
	if finalUsed != initialUsed {
		t.Errorf("used_addresses after %d Alloc/Release cycles = %d, want %d (initial)",
			totalCycles, finalUsed, initialUsed)
	}

	// All ip_addresses in the pool must be 'available' post-cycle.
	var allocatedCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM ip_addresses WHERE pool_id = $1 AND state = 'allocated'`,
		poolID).Scan(&allocatedCount); err != nil {
		t.Fatalf("count allocated post-cycle: %v", err)
	}
	if allocatedCount != 0 {
		t.Errorf("ip_addresses still 'allocated' after full cycle = %d, want 0", allocatedCount)
	}
}

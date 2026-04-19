package store

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/audit"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testAuditPool(t *testing.T) *pgxpool.Pool {
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

func truncateAuditLogs(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `TRUNCATE audit_logs CASCADE`)
	if err != nil {
		t.Fatalf("truncate audit_logs: %v", err)
	}
}

func TestAuditChain_ConcurrentWrites_Integration(t *testing.T) {
	pool := testAuditPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	truncateAuditLogs(t, pool)
	t.Cleanup(func() { truncateAuditLogs(t, pool) })

	store := NewAuditStore(pool)
	ctx := context.Background()

	tenants := []uuid.UUID{
		uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		uuid.MustParse("00000000-0000-0000-0000-000000000003"),
	}
	actions := []string{"sim.create", "operator.update", "tenant.delete", "policy.activate", "user.login"}

	var wg sync.WaitGroup
	errs := make(chan error, 100)

	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				userID := uuid.New()
				entry := &audit.Entry{
					TenantID:   tenants[goroutineID%len(tenants)],
					UserID:     &userID,
					Action:     actions[(goroutineID+i)%len(actions)],
					EntityType: "test",
					EntityID:   fmt.Sprintf("g%d-e%d", goroutineID, i),
					CreatedAt:  time.Now().UTC(),
				}
				if _, err := store.CreateWithChain(ctx, entry); err != nil {
					errs <- fmt.Errorf("goroutine %d entry %d: %w", goroutineID, i, err)
					return
				}
			}
		}(g)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent write error: %v", err)
	}

	entries, err := store.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(entries) != 100 {
		t.Fatalf("entries = %d, want 100", len(entries))
	}

	result := audit.VerifyChain(entries)
	if !result.Verified {
		t.Fatalf("chain should be verified after concurrent writes via advisory lock, first_invalid=%v", result.FirstInvalid)
	}
	if result.TotalRows != 100 {
		t.Fatalf("total_rows = %d, want 100", result.TotalRows)
	}
}

func TestAuditChain_TamperDetection_HashedColumn_Integration(t *testing.T) {
	pool := testAuditPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	truncateAuditLogs(t, pool)
	t.Cleanup(func() { truncateAuditLogs(t, pool) })

	store := NewAuditStore(pool)
	ctx := context.Background()
	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	for i := 0; i < 5; i++ {
		userID := uuid.New()
		entry := &audit.Entry{
			TenantID:   tenantID,
			UserID:     &userID,
			Action:     fmt.Sprintf("action.%d", i),
			EntityType: "sim",
			EntityID:   fmt.Sprintf("sim-%d", i),
			CreatedAt:  time.Now().UTC(),
		}
		if _, err := store.CreateWithChain(ctx, entry); err != nil {
			t.Fatalf("CreateWithChain %d: %v", i, err)
		}
	}

	entries, err := store.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	result := audit.VerifyChain(entries)
	if !result.Verified {
		t.Fatal("chain should be valid before tampering")
	}

	tamperedID := entries[2].ID
	_, err = pool.Exec(ctx, `UPDATE audit_logs SET action = 'tampered.action' WHERE id = $1`, tamperedID)
	if err != nil {
		t.Fatalf("tamper UPDATE: %v", err)
	}

	entries, err = store.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll after tamper: %v", err)
	}
	result = audit.VerifyChain(entries)
	if result.Verified {
		t.Fatal("chain should NOT verify after tampering hashed column (action)")
	}
	if result.FirstInvalid == nil || *result.FirstInvalid != tamperedID {
		t.Fatalf("first_invalid = %v, want %d (tampered row)", result.FirstInvalid, tamperedID)
	}
}

func TestAuditChain_TamperDetection_UnhashedColumn_Integration(t *testing.T) {
	pool := testAuditPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	truncateAuditLogs(t, pool)
	t.Cleanup(func() { truncateAuditLogs(t, pool) })

	store := NewAuditStore(pool)
	ctx := context.Background()
	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	for i := 0; i < 3; i++ {
		userID := uuid.New()
		entry := &audit.Entry{
			TenantID:   tenantID,
			UserID:     &userID,
			Action:     fmt.Sprintf("action.%d", i),
			EntityType: "sim",
			EntityID:   fmt.Sprintf("sim-%d", i),
			CreatedAt:  time.Now().UTC(),
		}
		if _, err := store.CreateWithChain(ctx, entry); err != nil {
			t.Fatalf("CreateWithChain %d: %v", i, err)
		}
	}

	entries, err := store.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}

	tamperedID := entries[1].ID
	_, err = pool.Exec(ctx, `UPDATE audit_logs SET after_data = '{"tampered":true}' WHERE id = $1`, tamperedID)
	if err != nil {
		t.Fatalf("tamper UPDATE after_data: %v", err)
	}

	entries, err = store.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll after tamper: %v", err)
	}
	result := audit.VerifyChain(entries)
	if !result.Verified {
		t.Fatal("chain should still verify after tampering unhashed column (after_data) — after_data is not covered by the hash")
	}
}

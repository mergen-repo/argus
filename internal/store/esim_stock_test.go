package store

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testStockPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("testStockPool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func setupStockTenant(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	tenantID := uuid.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO tenants (id, name, slug, plan) VALUES ($1,$2,$3,'free')`,
		tenantID, "stock-test-"+tenantID.String()[:8], "stock-slug-"+tenantID.String()[:8],
	)
	if err != nil {
		t.Fatalf("setupStockTenant: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM esim_profile_stock WHERE tenant_id=$1`, tenantID)
		pool.Exec(context.Background(), `DELETE FROM tenants WHERE id=$1`, tenantID)
	})
	return tenantID
}

func setupStockOperator(t *testing.T, pool *pgxpool.Pool, tenantID uuid.UUID) uuid.UUID {
	t.Helper()
	operatorID := uuid.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO operators (id, tenant_id, name, code, country, protocol, active)
		VALUES ($1, $2, $3, $4, 'TR', 'radius', true)`,
		operatorID, tenantID, "stock-op-"+operatorID.String()[:8], "op"+operatorID.String()[:4],
	)
	if err != nil {
		t.Fatalf("setupStockOperator: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM operators WHERE id=$1`, operatorID)
	})
	return operatorID
}

func TestEsimProfileStockStore_SetTotalAndGet(t *testing.T) {
	pool := testStockPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated stock store test")
	}
	store := NewEsimProfileStockStore(pool)
	tenantID := setupStockTenant(t, pool)
	operatorID := setupStockOperator(t, pool, tenantID)

	stock, err := store.SetTotal(context.Background(), tenantID, operatorID, 100)
	if err != nil {
		t.Fatalf("SetTotal: %v", err)
	}
	if stock.Total != 100 {
		t.Errorf("expected total=100, got %d", stock.Total)
	}
	if stock.Allocated != 0 {
		t.Errorf("expected allocated=0, got %d", stock.Allocated)
	}
	if stock.Available != 100 {
		t.Errorf("expected available=100, got %d", stock.Available)
	}

	got, err := store.Get(context.Background(), tenantID, operatorID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Total != 100 {
		t.Errorf("expected Get total=100, got %d", got.Total)
	}
}

func TestEsimProfileStockStore_Allocate(t *testing.T) {
	pool := testStockPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated stock store test")
	}
	store := NewEsimProfileStockStore(pool)
	tenantID := setupStockTenant(t, pool)
	operatorID := setupStockOperator(t, pool, tenantID)

	store.SetTotal(context.Background(), tenantID, operatorID, 5)

	stock, err := store.Allocate(context.Background(), tenantID, operatorID)
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if stock.Allocated != 1 {
		t.Errorf("expected allocated=1, got %d", stock.Allocated)
	}
	if stock.Available != 4 {
		t.Errorf("expected available=4, got %d", stock.Available)
	}
}

func TestEsimProfileStockStore_Allocate_Exhausted(t *testing.T) {
	pool := testStockPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated stock store test")
	}
	store := NewEsimProfileStockStore(pool)
	tenantID := setupStockTenant(t, pool)
	operatorID := setupStockOperator(t, pool, tenantID)

	store.SetTotal(context.Background(), tenantID, operatorID, 1)
	store.Allocate(context.Background(), tenantID, operatorID)

	_, err := store.Allocate(context.Background(), tenantID, operatorID)
	if err != ErrStockExhausted {
		t.Errorf("expected ErrStockExhausted, got: %v", err)
	}
}

func TestEsimProfileStockStore_Deallocate(t *testing.T) {
	pool := testStockPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated stock store test")
	}
	store := NewEsimProfileStockStore(pool)
	tenantID := setupStockTenant(t, pool)
	operatorID := setupStockOperator(t, pool, tenantID)

	store.SetTotal(context.Background(), tenantID, operatorID, 10)
	store.Allocate(context.Background(), tenantID, operatorID)
	store.Allocate(context.Background(), tenantID, operatorID)

	stock, err := store.Deallocate(context.Background(), tenantID, operatorID)
	if err != nil {
		t.Fatalf("Deallocate: %v", err)
	}
	if stock.Allocated != 1 {
		t.Errorf("expected allocated=1 after deallocate, got %d", stock.Allocated)
	}
}

func TestEsimProfileStockStore_ListSummary(t *testing.T) {
	pool := testStockPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated stock store test")
	}
	store := NewEsimProfileStockStore(pool)
	tenantID := setupStockTenant(t, pool)
	op1 := setupStockOperator(t, pool, tenantID)
	op2 := setupStockOperator(t, pool, tenantID)

	store.SetTotal(context.Background(), tenantID, op1, 100)
	store.SetTotal(context.Background(), tenantID, op2, 200)

	summary, err := store.ListSummary(context.Background(), tenantID)
	if err != nil {
		t.Fatalf("ListSummary: %v", err)
	}
	if len(summary) < 2 {
		t.Errorf("expected at least 2 stock rows, got %d", len(summary))
	}
}

func TestEsimStockStore_ConcurrentAllocate_50of100(t *testing.T) {
	pool := testStockPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated concurrent stock test")
	}
	store := NewEsimProfileStockStore(pool)
	tenantID := setupStockTenant(t, pool)
	operatorID := setupStockOperator(t, pool, tenantID)

	if _, err := store.SetTotal(context.Background(), tenantID, operatorID, 50); err != nil {
		t.Fatalf("SetTotal: %v", err)
	}

	const goroutines = 100
	var (
		wg      sync.WaitGroup
		success atomic.Int64
		exhaust atomic.Int64
		other   atomic.Int64
	)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_, err := store.Allocate(context.Background(), tenantID, operatorID)
			switch {
			case err == nil:
				success.Add(1)
			case errors.Is(err, ErrStockExhausted):
				exhaust.Add(1)
			default:
				other.Add(1)
			}
		}()
	}
	wg.Wait()

	if success.Load() != 50 {
		t.Errorf("expected exactly 50 successes, got %d", success.Load())
	}
	if exhaust.Load() != 50 {
		t.Errorf("expected exactly 50 ErrStockExhausted, got %d", exhaust.Load())
	}
	if other.Load() != 0 {
		t.Errorf("expected 0 other errors, got %d", other.Load())
	}
}

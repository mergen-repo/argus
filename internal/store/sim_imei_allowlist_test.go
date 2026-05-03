package store

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testAllowlistPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://argus:argus_dev@localhost:5432/argus_dev?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Skipf("postgres not available: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("postgres not available: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

// setupAllowlistTenant creates a disposable tenant and returns its ID.
func setupAllowlistTenant(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO tenants (id, name, slug, plan) VALUES ($1, $2, $3, 'free')`,
		id, "allowlist-test-"+id.String()[:8], "allowlist-slug-"+id.String()[:8],
	)
	if err != nil {
		t.Fatalf("setupAllowlistTenant: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM tenants WHERE id = $1`, id)
	})
	return id
}

// setupAllowlistSIM inserts a minimal SIM row under tenantID and returns its id.
// operator_id uses a well-known seed UUID so the row lands in sims_default partition.
func setupAllowlistSIM(t *testing.T, pool *pgxpool.Pool, tenantID uuid.UUID) uuid.UUID {
	t.Helper()
	simID := uuid.New()
	operatorID := uuid.MustParse("20000000-0000-0000-0000-000000000001")
	iccid := "8990TEST" + simID.String()[:13]
	imsi := simID.String()[:15]
	_, err := pool.Exec(context.Background(),
		`INSERT INTO sims (id, tenant_id, operator_id, iccid, imsi, sim_type, state)
		 VALUES ($1, $2, $3, $4, $5, 'physical', 'ordered')`,
		simID, tenantID, operatorID, iccid, imsi,
	)
	if err != nil {
		t.Fatalf("setupAllowlistSIM: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM sim_imei_allowlist WHERE sim_id = $1`, simID)
		pool.Exec(context.Background(), `DELETE FROM sims WHERE id = $1 AND operator_id = $2`, simID, operatorID)
	})
	return simID
}

// TestSIMIMEIAllowlist_Add_List_Remove_IsAllowed covers the happy-path lifecycle.
func TestSIMIMEIAllowlist_Add_List_Remove_IsAllowed(t *testing.T) {
	pool := testAllowlistPool(t)
	ctx := context.Background()
	simStore := NewSIMStore(pool)
	store := NewSIMIMEIAllowlistStore(pool, simStore)

	tenantID := setupAllowlistTenant(t, pool)
	simID := setupAllowlistSIM(t, pool, tenantID)

	const imei1 = "123456789012345"
	const imei2 = "987654321098765"

	if err := store.Add(ctx, tenantID, simID, imei1, nil); err != nil {
		t.Fatalf("Add imei1: %v", err)
	}
	if err := store.Add(ctx, tenantID, simID, imei2, nil); err != nil {
		t.Fatalf("Add imei2: %v", err)
	}

	// idempotent second add must not error
	if err := store.Add(ctx, tenantID, simID, imei1, nil); err != nil {
		t.Fatalf("duplicate Add must be no-op: %v", err)
	}

	imeis, err := store.List(ctx, tenantID, simID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(imeis) != 2 {
		t.Errorf("List: got %d entries, want 2", len(imeis))
	}

	allowed, err := store.IsAllowed(ctx, tenantID, simID, imei1)
	if err != nil {
		t.Fatalf("IsAllowed: %v", err)
	}
	if !allowed {
		t.Error("IsAllowed: expected true for known imei1")
	}

	allowed, err = store.IsAllowed(ctx, tenantID, simID, "000000000000000")
	if err != nil {
		t.Fatalf("IsAllowed unknown: %v", err)
	}
	if allowed {
		t.Error("IsAllowed: expected false for unknown imei")
	}

	if err := store.Remove(ctx, tenantID, simID, imei1); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	imeis, err = store.List(ctx, tenantID, simID)
	if err != nil {
		t.Fatalf("List after remove: %v", err)
	}
	if len(imeis) != 1 || imeis[0] != imei2 {
		t.Errorf("List after remove: got %v, want [%s]", imeis, imei2)
	}
}

// TestSIMIMEIAllowlist_CrossTenantReturns_ErrSIMNotFound verifies AC-6: operations
// with a mismatched tenantID must return ErrSIMNotFound.
func TestSIMIMEIAllowlist_CrossTenantReturns_ErrSIMNotFound(t *testing.T) {
	pool := testAllowlistPool(t)
	ctx := context.Background()
	simStore := NewSIMStore(pool)
	store := NewSIMIMEIAllowlistStore(pool, simStore)

	tenantA := setupAllowlistTenant(t, pool)
	tenantB := setupAllowlistTenant(t, pool)
	simID := setupAllowlistSIM(t, pool, tenantA)

	const imei = "111111111111111"

	// tenantB trying to Add to tenantA's SIM
	err := store.Add(ctx, tenantB, simID, imei, nil)
	if !errors.Is(err, ErrSIMNotFound) {
		t.Errorf("Add cross-tenant: expected ErrSIMNotFound, got %v", err)
	}

	// tenantB trying to Remove from tenantA's SIM
	err = store.Remove(ctx, tenantB, simID, imei)
	if !errors.Is(err, ErrSIMNotFound) {
		t.Errorf("Remove cross-tenant: expected ErrSIMNotFound, got %v", err)
	}

	// tenantB trying to List tenantA's SIM
	_, err = store.List(ctx, tenantB, simID)
	if !errors.Is(err, ErrSIMNotFound) {
		t.Errorf("List cross-tenant: expected ErrSIMNotFound, got %v", err)
	}

	// IsAllowed cross-tenant must return (false, nil) — not an error
	allowed, err := store.IsAllowed(ctx, tenantB, simID, imei)
	if err != nil {
		t.Errorf("IsAllowed cross-tenant: expected nil error, got %v", err)
	}
	if allowed {
		t.Error("IsAllowed cross-tenant: expected false")
	}
}

// TestSIMIMEIAllowlist_EmptyIMEI_ReturnsError verifies that Add rejects blank IMEIs.
func TestSIMIMEIAllowlist_EmptyIMEI_ReturnsError(t *testing.T) {
	pool := testAllowlistPool(t)
	ctx := context.Background()
	simStore := NewSIMStore(pool)
	store := NewSIMIMEIAllowlistStore(pool, simStore)

	tenantID := setupAllowlistTenant(t, pool)
	simID := setupAllowlistSIM(t, pool, tenantID)

	err := store.Add(ctx, tenantID, simID, "", nil)
	if err == nil {
		t.Error("Add with empty imei: expected error, got nil")
	}

	err = store.Add(ctx, tenantID, simID, "   ", nil)
	if err == nil {
		t.Error("Add with whitespace imei: expected error, got nil")
	}
}

// TestSIMIMEIAllowlist_Remove_NonExistent_IsNoOp verifies that removing an IMEI
// that is not in the allowlist does not return an error.
func TestSIMIMEIAllowlist_Remove_NonExistent_IsNoOp(t *testing.T) {
	pool := testAllowlistPool(t)
	ctx := context.Background()
	simStore := NewSIMStore(pool)
	store := NewSIMIMEIAllowlistStore(pool, simStore)

	tenantID := setupAllowlistTenant(t, pool)
	simID := setupAllowlistSIM(t, pool, tenantID)

	if err := store.Remove(ctx, tenantID, simID, "999999999999999"); err != nil {
		t.Errorf("Remove non-existent: expected nil, got %v", err)
	}
}

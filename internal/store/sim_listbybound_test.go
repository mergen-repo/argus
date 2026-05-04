package store

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testBoundIMEIPool(t *testing.T) *pgxpool.Pool {
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

func setupBoundTenant(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO tenants (id, name, slug, plan) VALUES ($1, $2, $3, 'free')`,
		id, "bound-test-"+id.String()[:8], "bound-slug-"+id.String()[:8],
	)
	if err != nil {
		t.Fatalf("setupBoundTenant: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM sims WHERE tenant_id = $1`, id)
		pool.Exec(context.Background(), `DELETE FROM tenants WHERE id = $1`, id)
	})
	return id
}

func insertSIMWithBoundIMEI(t *testing.T, pool *pgxpool.Pool, tenantID uuid.UUID, iccidSuffix, boundIMEI string) uuid.UUID {
	t.Helper()
	simID := uuid.New()
	operatorID := uuid.MustParse("20000000-0000-0000-0000-000000000001")
	iccid := "8990BOUND" + iccidSuffix
	imsi := simID.String()[:15]
	mode := "strict"
	status := "pending"
	var err error
	if boundIMEI != "" {
		_, err = pool.Exec(context.Background(),
			`INSERT INTO sims (id, tenant_id, operator_id, iccid, imsi, sim_type, state, bound_imei, binding_mode, binding_status)
			 VALUES ($1, $2, $3, $4, $5, 'physical', 'active', $6, $7, $8)`,
			simID, tenantID, operatorID, iccid, imsi, boundIMEI, mode, status,
		)
	} else {
		_, err = pool.Exec(context.Background(),
			`INSERT INTO sims (id, tenant_id, operator_id, iccid, imsi, sim_type, state)
			 VALUES ($1, $2, $3, $4, $5, 'physical', 'active')`,
			simID, tenantID, operatorID, iccid, imsi,
		)
	}
	if err != nil {
		t.Fatalf("insertSIMWithBoundIMEI(%s): %v", iccidSuffix, err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM sims WHERE id = $1`, simID)
	})
	return simID
}

func TestSIMStore_ListByBoundIMEI_ExactMatch(t *testing.T) {
	pool := testBoundIMEIPool(t)
	ctx := context.Background()
	ss := NewSIMStore(pool)
	tenantID := setupBoundTenant(t, pool)

	imeiA := "111111111111111"
	imeiB := "222222222222222"
	simA := insertSIMWithBoundIMEI(t, pool, tenantID, "A001", imeiA)
	insertSIMWithBoundIMEI(t, pool, tenantID, "B001", imeiB)

	rows, err := ss.ListByBoundIMEI(ctx, tenantID, imeiA)
	if err != nil {
		t.Fatalf("ListByBoundIMEI: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].ID != simA {
		t.Errorf("row ID = %s, want %s", rows[0].ID, simA)
	}
}

func TestSIMStore_ListByBoundIMEI_NoMatches_ReturnsNil(t *testing.T) {
	pool := testBoundIMEIPool(t)
	ctx := context.Background()
	ss := NewSIMStore(pool)
	tenantID := setupBoundTenant(t, pool)

	insertSIMWithBoundIMEI(t, pool, tenantID, "C001", "333333333333333")

	rows, err := ss.ListByBoundIMEI(ctx, tenantID, "999999999999999")
	if err != nil {
		t.Fatalf("ListByBoundIMEI: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("got %d rows, want 0", len(rows))
	}
}

func TestSIMStore_ListByBoundIMEI_CrossTenant_Excluded(t *testing.T) {
	pool := testBoundIMEIPool(t)
	ctx := context.Background()
	ss := NewSIMStore(pool)
	tenantA := setupBoundTenant(t, pool)
	tenantB := setupBoundTenant(t, pool)

	sharedIMEI := "444444444444444"
	insertSIMWithBoundIMEI(t, pool, tenantA, "D001", sharedIMEI)
	insertSIMWithBoundIMEI(t, pool, tenantB, "E001", sharedIMEI)

	rowsA, err := ss.ListByBoundIMEI(ctx, tenantA, sharedIMEI)
	if err != nil {
		t.Fatalf("tenantA ListByBoundIMEI: %v", err)
	}
	if len(rowsA) != 1 {
		t.Errorf("tenantA: got %d rows, want 1", len(rowsA))
	}

	rowsB, err := ss.ListByBoundIMEI(ctx, tenantB, sharedIMEI)
	if err != nil {
		t.Fatalf("tenantB ListByBoundIMEI: %v", err)
	}
	if len(rowsB) != 1 {
		t.Errorf("tenantB: got %d rows, want 1", len(rowsB))
	}

	if len(rowsA) > 0 && len(rowsB) > 0 && rowsA[0].ID == rowsB[0].ID {
		t.Error("cross-tenant: both tenants returned the same SIM row")
	}
}

func TestSIMStore_ListByBoundIMEI_EmptyInput_ReturnsNil(t *testing.T) {
	pool := testBoundIMEIPool(t)
	ctx := context.Background()
	ss := NewSIMStore(pool)
	tenantID := setupBoundTenant(t, pool)

	rows, err := ss.ListByBoundIMEI(ctx, tenantID, "")
	if err != nil {
		t.Fatalf("empty IMEI should not error: %v", err)
	}
	if rows != nil {
		t.Errorf("empty IMEI: got non-nil rows")
	}

	rows, err = ss.ListByBoundIMEI(ctx, tenantID, "123")
	if err != nil {
		t.Fatalf("short IMEI should not error: %v", err)
	}
	if rows != nil {
		t.Errorf("short IMEI: got non-nil rows")
	}
}

package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testHistoryPool(t *testing.T) *pgxpool.Pool {
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

// setupHistoryTenant creates a disposable tenant and returns its ID.
func setupHistoryTenant(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO tenants (id, name, slug, plan) VALUES ($1, $2, $3, 'free')`,
		id, "history-test-"+id.String()[:8], "history-slug-"+id.String()[:8],
	)
	if err != nil {
		t.Fatalf("setupHistoryTenant: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM imei_history WHERE tenant_id = $1`, id)
		pool.Exec(context.Background(), `DELETE FROM tenants WHERE id = $1`, id)
	})
	return id
}

// setupHistorySIM inserts a minimal SIM row under tenantID and returns its id.
func setupHistorySIM(t *testing.T, pool *pgxpool.Pool, tenantID uuid.UUID) uuid.UUID {
	t.Helper()
	simID := uuid.New()
	operatorID := uuid.MustParse("20000000-0000-0000-0000-000000000001")
	iccid := "8990HIST" + simID.String()[:13]
	imsi := simID.String()[:15]
	_, err := pool.Exec(context.Background(),
		`INSERT INTO sims (id, tenant_id, operator_id, iccid, imsi, sim_type, state)
		 VALUES ($1, $2, $3, $4, $5, 'physical', 'ordered')`,
		simID, tenantID, operatorID, iccid, imsi,
	)
	if err != nil {
		t.Fatalf("setupHistorySIM: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM sims WHERE id = $1 AND operator_id = $2`, simID, operatorID)
	})
	return simID
}

// appendHistory is a test helper that inserts a history row with the given protocol and returns the row ID.
func appendHistory(t *testing.T, hs *IMEIHistoryStore, tenantID, simID uuid.UUID, imei, protocol string) uuid.UUID {
	t.Helper()
	id, err := hs.Append(context.Background(), tenantID, AppendIMEIHistoryParams{
		SIMID:           simID,
		ObservedIMEI:    imei,
		CaptureProtocol: protocol,
		WasMismatch:     false,
		AlarmRaised:     false,
	})
	if err != nil {
		t.Fatalf("Append (%s/%s): %v", imei, protocol, err)
	}
	return id
}

// TestIMEIHistory_Append_List_Basic covers Append and a simple List call.
func TestIMEIHistory_Append_List_Basic(t *testing.T) {
	pool := testHistoryPool(t)
	ctx := context.Background()
	simStore := NewSIMStore(pool)
	hs := NewIMEIHistoryStore(pool, simStore)

	tenantID := setupHistoryTenant(t, pool)
	simID := setupHistorySIM(t, pool, tenantID)

	id1 := appendHistory(t, hs, tenantID, simID, "123456789012345", "radius")
	id2 := appendHistory(t, hs, tenantID, simID, "987654321098765", "diameter_s6a")

	rows, nextCursor, err := hs.List(ctx, tenantID, simID, ListIMEIHistoryParams{Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("List: got %d rows, want 2", len(rows))
	}
	if nextCursor != "" {
		t.Errorf("nextCursor should be empty for 2 rows with limit 10, got %q", nextCursor)
	}
	ids := map[uuid.UUID]bool{id1: true, id2: true}
	for _, r := range rows {
		if !ids[r.ID] {
			t.Errorf("unexpected row ID %s", r.ID)
		}
		if r.TenantID != tenantID {
			t.Errorf("row TenantID = %s, want %s", r.TenantID, tenantID)
		}
		if r.SIMID != simID {
			t.Errorf("row SIMID = %s, want %s", r.SIMID, simID)
		}
	}
}

// TestIMEIHistory_List_CursorPagination inserts 5 rows and pages through them
// with limit=2, verifying 3 pages (2+2+1) and an empty cursor on the last page.
func TestIMEIHistory_List_CursorPagination(t *testing.T) {
	pool := testHistoryPool(t)
	ctx := context.Background()
	simStore := NewSIMStore(pool)
	hs := NewIMEIHistoryStore(pool, simStore)

	tenantID := setupHistoryTenant(t, pool)
	simID := setupHistorySIM(t, pool, tenantID)

	imeis := []string{
		"100000000000001",
		"100000000000002",
		"100000000000003",
		"100000000000004",
		"100000000000005",
	}
	for _, imei := range imeis {
		appendHistory(t, hs, tenantID, simID, imei, "radius")
	}

	// Page 1
	rows1, cursor1, err := hs.List(ctx, tenantID, simID, ListIMEIHistoryParams{Limit: 2})
	if err != nil {
		t.Fatalf("page1 List: %v", err)
	}
	if len(rows1) != 2 {
		t.Errorf("page1: got %d rows, want 2", len(rows1))
	}
	if cursor1 == "" {
		t.Fatal("page1: expected non-empty nextCursor")
	}

	// Page 2
	rows2, cursor2, err := hs.List(ctx, tenantID, simID, ListIMEIHistoryParams{Limit: 2, Cursor: cursor1})
	if err != nil {
		t.Fatalf("page2 List: %v", err)
	}
	if len(rows2) != 2 {
		t.Errorf("page2: got %d rows, want 2", len(rows2))
	}
	if cursor2 == "" {
		t.Fatal("page2: expected non-empty nextCursor")
	}

	// Page 3
	rows3, cursor3, err := hs.List(ctx, tenantID, simID, ListIMEIHistoryParams{Limit: 2, Cursor: cursor2})
	if err != nil {
		t.Fatalf("page3 List: %v", err)
	}
	if len(rows3) != 1 {
		t.Errorf("page3: got %d rows, want 1", len(rows3))
	}
	if cursor3 != "" {
		t.Errorf("page3: expected empty nextCursor, got %q", cursor3)
	}

	// No overlap between pages
	seen := map[uuid.UUID]bool{}
	for _, r := range append(append(rows1, rows2...), rows3...) {
		if seen[r.ID] {
			t.Errorf("duplicate row ID %s across pages", r.ID)
		}
		seen[r.ID] = true
	}
	if len(seen) != 5 {
		t.Errorf("total unique rows across pages = %d, want 5", len(seen))
	}
}

// TestIMEIHistory_List_ProtocolFilter inserts radius and diameter_s6a rows, then
// filters by protocol and verifies only the correct rows are returned.
func TestIMEIHistory_List_ProtocolFilter(t *testing.T) {
	pool := testHistoryPool(t)
	ctx := context.Background()
	simStore := NewSIMStore(pool)
	hs := NewIMEIHistoryStore(pool, simStore)

	tenantID := setupHistoryTenant(t, pool)
	simID := setupHistorySIM(t, pool, tenantID)

	appendHistory(t, hs, tenantID, simID, "100000000000001", "radius")
	appendHistory(t, hs, tenantID, simID, "100000000000002", "radius")
	appendHistory(t, hs, tenantID, simID, "100000000000003", "diameter_s6a")
	appendHistory(t, hs, tenantID, simID, "100000000000004", "5g_sba")

	proto := "radius"
	rows, _, err := hs.List(ctx, tenantID, simID, ListIMEIHistoryParams{Limit: 50, Protocol: &proto})
	if err != nil {
		t.Fatalf("List with protocol filter: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("protocol=radius: got %d rows, want 2", len(rows))
	}
	for _, r := range rows {
		if r.CaptureProtocol != "radius" {
			t.Errorf("unexpected protocol %q in filtered result", r.CaptureProtocol)
		}
	}

	proto = "diameter_s6a"
	rows, _, err = hs.List(ctx, tenantID, simID, ListIMEIHistoryParams{Limit: 50, Protocol: &proto})
	if err != nil {
		t.Fatalf("List with diameter filter: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("protocol=diameter_s6a: got %d rows, want 1", len(rows))
	}

	// Since filter — only rows after a timestamp
	since := time.Now().Add(time.Hour)
	rows, _, err = hs.List(ctx, tenantID, simID, ListIMEIHistoryParams{Limit: 50, Since: &since})
	if err != nil {
		t.Fatalf("List with future since: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("future since filter: got %d rows, want 0", len(rows))
	}
}

// TestIMEIHistory_List_CrossTenantReturns_ErrSIMNotFound verifies that listing
// imei_history for a SIM belonging to another tenant returns ErrSIMNotFound.
func TestIMEIHistory_List_CrossTenantReturns_ErrSIMNotFound(t *testing.T) {
	pool := testHistoryPool(t)
	ctx := context.Background()
	simStore := NewSIMStore(pool)
	hs := NewIMEIHistoryStore(pool, simStore)

	tenantA := setupHistoryTenant(t, pool)
	tenantB := setupHistoryTenant(t, pool)
	simID := setupHistorySIM(t, pool, tenantA)

	// Insert a row under tenantA so the SIM has history
	appendHistory(t, hs, tenantA, simID, "111111111111111", "radius")

	// TenantB trying to list tenantA's SIM history
	_, _, err := hs.List(ctx, tenantB, simID, ListIMEIHistoryParams{Limit: 10})
	if !errors.Is(err, ErrSIMNotFound) {
		t.Errorf("List cross-tenant: expected ErrSIMNotFound, got %v", err)
	}
}

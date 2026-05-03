package store

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testPoolPool(t *testing.T) *pgxpool.Pool {
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

func setupPoolTenant(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO tenants (id, name, slug, plan) VALUES ($1, $2, $3, 'free')`,
		id, "pool-test-"+id.String()[:8], "pool-slug-"+id.String()[:8],
	)
	if err != nil {
		t.Fatalf("setupPoolTenant: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM imei_whitelist WHERE tenant_id = $1`, id)
		pool.Exec(context.Background(), `DELETE FROM imei_greylist WHERE tenant_id = $1`, id)
		pool.Exec(context.Background(), `DELETE FROM imei_blacklist WHERE tenant_id = $1`, id)
		pool.Exec(context.Background(), `DELETE FROM tenants WHERE id = $1`, id)
	})
	return id
}

func ptrString(s string) *string { return &s }

// AC-2/AC-3/AC-4 round-trip on whitelist.
func TestIMEIPoolStore_Add_List_Delete_Whitelist(t *testing.T) {
	pool := testPoolPool(t)
	ctx := context.Background()
	ps := NewIMEIPoolStore(pool)
	tenantID := setupPoolTenant(t, pool)

	entry, err := ps.Add(ctx, tenantID, PoolWhitelist, AddEntryParams{
		Kind:        EntryKindFullIMEI,
		IMEIOrTAC:   "359211089765432",
		DeviceModel: ptrString("Test Phone"),
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if entry.IMEIOrTAC != "359211089765432" || entry.Kind != EntryKindFullIMEI {
		t.Fatalf("unexpected entry: %+v", entry)
	}

	rows, _, err := ps.List(ctx, tenantID, PoolWhitelist, ListParams{Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != entry.ID {
		t.Fatalf("List returned wrong rows: %+v", rows)
	}

	if err := ps.Delete(ctx, tenantID, PoolWhitelist, entry.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	rows, _, err = ps.List(ctx, tenantID, PoolWhitelist, ListParams{Limit: 10})
	if err != nil {
		t.Fatalf("List after Delete: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows after delete, got %d", len(rows))
	}
}

// AC-3: UNIQUE (tenant_id, imei_or_tac) violation.
func TestIMEIPoolStore_Add_DuplicateReturnsErrDuplicate(t *testing.T) {
	pool := testPoolPool(t)
	ctx := context.Background()
	ps := NewIMEIPoolStore(pool)
	tenantID := setupPoolTenant(t, pool)

	_, err := ps.Add(ctx, tenantID, PoolWhitelist, AddEntryParams{
		Kind: EntryKindFullIMEI, IMEIOrTAC: "111111111111111",
	})
	if err != nil {
		t.Fatalf("first Add: %v", err)
	}
	_, err = ps.Add(ctx, tenantID, PoolWhitelist, AddEntryParams{
		Kind: EntryKindFullIMEI, IMEIOrTAC: "111111111111111",
	})
	if !errors.Is(err, ErrPoolEntryDuplicate) {
		t.Fatalf("expected ErrPoolEntryDuplicate, got: %v", err)
	}
}

// Greylist requires quarantine_reason at SQL NOT NULL — store-or-handler boundary check.
// Store currently passes through; if quarantine_reason is nil, SQL NOT NULL surfaces as error.
func TestIMEIPoolStore_Add_Greylist_RequiresQuarantineReason(t *testing.T) {
	pool := testPoolPool(t)
	ctx := context.Background()
	ps := NewIMEIPoolStore(pool)
	tenantID := setupPoolTenant(t, pool)

	_, err := ps.Add(ctx, tenantID, PoolGreylist, AddEntryParams{
		Kind: EntryKindFullIMEI, IMEIOrTAC: "222222222222222",
		QuarantineReason: nil,
	})
	if err == nil {
		t.Fatalf("expected error for greylist without quarantine_reason")
	}
}

// Blacklist requires block_reason + imported_from at SQL NOT NULL.
func TestIMEIPoolStore_Add_Blacklist_RequiresBlockReasonAndImportedFrom(t *testing.T) {
	pool := testPoolPool(t)
	ctx := context.Background()
	ps := NewIMEIPoolStore(pool)
	tenantID := setupPoolTenant(t, pool)

	_, err := ps.Add(ctx, tenantID, PoolBlacklist, AddEntryParams{
		Kind: EntryKindFullIMEI, IMEIOrTAC: "333333333333333",
		BlockReason: nil, ImportedFrom: nil,
	})
	if err == nil {
		t.Fatalf("expected error for blacklist without block_reason/imported_from")
	}
}

// AC-6: Lookup with full_imei → matched_via=exact.
func TestIMEIPoolStore_Lookup_ExactFullIMEI(t *testing.T) {
	pool := testPoolPool(t)
	ctx := context.Background()
	ps := NewIMEIPoolStore(pool)
	tenantID := setupPoolTenant(t, pool)

	_, err := ps.Add(ctx, tenantID, PoolWhitelist, AddEntryParams{
		Kind: EntryKindFullIMEI, IMEIOrTAC: "444444444444444",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	res, err := ps.Lookup(ctx, tenantID, "444444444444444")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if len(res.Whitelist) != 1 || res.Whitelist[0].MatchedVia != "exact" {
		t.Fatalf("expected 1 whitelist exact match, got: %+v", res)
	}
}

// AC-6/AC-8: Lookup with TAC range → matched_via=tac_range.
func TestIMEIPoolStore_Lookup_TACRange(t *testing.T) {
	pool := testPoolPool(t)
	ctx := context.Background()
	ps := NewIMEIPoolStore(pool)
	tenantID := setupPoolTenant(t, pool)

	_, err := ps.Add(ctx, tenantID, PoolWhitelist, AddEntryParams{
		Kind: EntryKindTACRange, IMEIOrTAC: "35921108",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	res, err := ps.Lookup(ctx, tenantID, "359211089765432")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if len(res.Whitelist) != 1 || res.Whitelist[0].MatchedVia != "tac_range" {
		t.Fatalf("expected 1 whitelist tac_range match, got: %+v", res)
	}
}

// AC-6: Lookup matches across multiple pools.
func TestIMEIPoolStore_Lookup_MultipleMatches(t *testing.T) {
	pool := testPoolPool(t)
	ctx := context.Background()
	ps := NewIMEIPoolStore(pool)
	tenantID := setupPoolTenant(t, pool)

	_, err := ps.Add(ctx, tenantID, PoolWhitelist, AddEntryParams{
		Kind: EntryKindFullIMEI, IMEIOrTAC: "555555555555555",
	})
	if err != nil {
		t.Fatalf("Add whitelist: %v", err)
	}
	_, err = ps.Add(ctx, tenantID, PoolBlacklist, AddEntryParams{
		Kind: EntryKindTACRange, IMEIOrTAC: "55555555",
		BlockReason: ptrString("test"), ImportedFrom: ptrString("manual"),
	})
	if err != nil {
		t.Fatalf("Add blacklist: %v", err)
	}

	res, err := ps.Lookup(ctx, tenantID, "555555555555555")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if len(res.Whitelist) != 1 || len(res.Blacklist) != 1 {
		t.Fatalf("expected 1 whitelist + 1 blacklist match, got: %+v", res)
	}
}

// AC-6: empty result for IMEI in no pool.
func TestIMEIPoolStore_Lookup_NoMatches(t *testing.T) {
	pool := testPoolPool(t)
	ctx := context.Background()
	ps := NewIMEIPoolStore(pool)
	tenantID := setupPoolTenant(t, pool)

	res, err := ps.Lookup(ctx, tenantID, "000000000000000")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if len(res.Whitelist) != 0 || len(res.Greylist) != 0 || len(res.Blacklist) != 0 {
		t.Fatalf("expected empty result, got: %+v", res)
	}
}

// AC-9 prep: LookupKind single-pool variant returns true on TAC match.
func TestIMEIPoolStore_LookupKind_TACMatch(t *testing.T) {
	pool := testPoolPool(t)
	ctx := context.Background()
	ps := NewIMEIPoolStore(pool)
	tenantID := setupPoolTenant(t, pool)

	_, err := ps.Add(ctx, tenantID, PoolBlacklist, AddEntryParams{
		Kind: EntryKindTACRange, IMEIOrTAC: "12345678",
		BlockReason: ptrString("stolen"), ImportedFrom: ptrString("gsma_ceir"),
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	hit, err := ps.LookupKind(ctx, tenantID, PoolBlacklist, "123456789999999")
	if err != nil {
		t.Fatalf("LookupKind: %v", err)
	}
	if !hit {
		t.Fatal("expected TAC match → true")
	}

	miss, err := ps.LookupKind(ctx, tenantID, PoolBlacklist, "999999999999999")
	if err != nil {
		t.Fatalf("LookupKind miss: %v", err)
	}
	if miss {
		t.Fatal("expected non-match → false")
	}
}

// Cross-tenant Delete returns ErrPoolEntryNotFound.
func TestIMEIPoolStore_CrossTenantReturnsNotFound(t *testing.T) {
	pool := testPoolPool(t)
	ctx := context.Background()
	ps := NewIMEIPoolStore(pool)

	tenantA := setupPoolTenant(t, pool)
	tenantB := setupPoolTenant(t, pool)

	entry, err := ps.Add(ctx, tenantA, PoolWhitelist, AddEntryParams{
		Kind: EntryKindFullIMEI, IMEIOrTAC: "777777777777777",
	})
	if err != nil {
		t.Fatalf("Add (tenantA): %v", err)
	}

	err = ps.Delete(ctx, tenantB, PoolWhitelist, entry.ID)
	if !errors.Is(err, ErrPoolEntryNotFound) {
		t.Fatalf("expected ErrPoolEntryNotFound for cross-tenant delete, got: %v", err)
	}

	if err := ps.Delete(ctx, tenantA, PoolWhitelist, entry.ID); err != nil {
		t.Fatalf("Delete (rightful tenant): %v", err)
	}
}

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

// setupPoolTenantSchemaCompat is a schema-tolerant tenant helper for the
// STORY-096 D-189 test. The repo's older setupPoolTenant inserts into a
// hypothetical "slug" column that does not exist in the production schema;
// this helper uses the actual columns (name + contact_email + plan).
func setupPoolTenantSchemaCompat(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO tenants (id, name, contact_email, plan) VALUES ($1, $2, $3, 'standard')`,
		id, "pool-d189-"+id.String()[:8], "d189-"+id.String()[:8]+"@argus.local",
	)
	if err != nil {
		t.Fatalf("setupPoolTenantSchemaCompat: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM imei_whitelist WHERE tenant_id = $1`, id)
		pool.Exec(context.Background(), `DELETE FROM sims WHERE tenant_id = $1`, id)
		pool.Exec(context.Background(), `DELETE FROM tenants WHERE id = $1`, id)
	})
	return id
}

// TestIMEIPoolStore_List_IncludeBoundCount covers STORY-096 D-189: when
// ListParams.IncludeBoundCount is true, the store performs a single
// LEFT JOIN sims that populates PoolEntry.BoundSIMsCount per row. Two
// SIMs are seeded — one whose bound_imei matches a full_imei whitelist
// entry exactly, one whose bound_imei TAC-prefix-matches a tac_range entry —
// to exercise both predicate branches.
func TestIMEIPoolStore_List_IncludeBoundCount(t *testing.T) {
	pool := testPoolPool(t)
	ctx := context.Background()
	ps := NewIMEIPoolStore(pool)
	tenantID := setupPoolTenantSchemaCompat(t, pool)

	// Seed two whitelist entries: one full_imei (15-digit) and one tac_range
	// (8-digit prefix).
	fullIMEI := "359211089765432"
	tac := "35921108"

	full, err := ps.Add(ctx, tenantID, PoolWhitelist, AddEntryParams{
		Kind: EntryKindFullIMEI, IMEIOrTAC: fullIMEI,
	})
	if err != nil {
		t.Fatalf("Add full_imei: %v", err)
	}
	tacEntry, err := ps.Add(ctx, tenantID, PoolWhitelist, AddEntryParams{
		Kind: EntryKindTACRange, IMEIOrTAC: tac,
	})
	if err != nil {
		t.Fatalf("Add tac_range: %v", err)
	}

	// Seed two SIMs:
	//   simA — bound_imei = fullIMEI (matches both full_imei AND tac_range)
	//   simB — bound_imei starts with the same TAC but isn't the exact 15-digit
	operatorID := uuid.MustParse("20000000-0000-0000-0000-000000000001")
	simA := uuid.New()
	simB := uuid.New()
	otherTACBound := tac + "9999999" // 15-digit, same TAC, different full IMEI

	_, err = pool.Exec(ctx,
		`INSERT INTO sims (id, tenant_id, operator_id, iccid, imsi, sim_type, state, bound_imei)
		 VALUES ($1, $2, $3, $4, $5, 'physical', 'active', $6)`,
		simA, tenantID, operatorID, "8990D189A"+simA.String()[:13], simA.String()[:15], fullIMEI,
	)
	if err != nil {
		t.Fatalf("seed simA: %v", err)
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO sims (id, tenant_id, operator_id, iccid, imsi, sim_type, state, bound_imei)
		 VALUES ($1, $2, $3, $4, $5, 'physical', 'active', $6)`,
		simB, tenantID, operatorID, "8990D189B"+simB.String()[:13], simB.String()[:15], otherTACBound,
	)
	if err != nil {
		t.Fatalf("seed simB: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM sims WHERE id = ANY($1)`, []uuid.UUID{simA, simB})
	})

	// Without IncludeBoundCount, BoundSIMsCount is zero on every row.
	rows, _, err := ps.List(ctx, tenantID, PoolWhitelist, ListParams{Limit: 50})
	if err != nil {
		t.Fatalf("List (no bound count): %v", err)
	}
	for _, r := range rows {
		if r.BoundSIMsCount != 0 {
			t.Errorf("BoundSIMsCount = %d on %s/%s without flag, want 0",
				r.BoundSIMsCount, r.Kind, r.IMEIOrTAC)
		}
	}

	// With IncludeBoundCount, the COUNT is populated per entry.
	got, _, err := ps.List(ctx, tenantID, PoolWhitelist, ListParams{
		Limit:             50,
		IncludeBoundCount: true,
	})
	if err != nil {
		t.Fatalf("List (with bound count): %v", err)
	}

	var fullCount, tacCount int
	for _, r := range got {
		switch r.ID {
		case full.ID:
			fullCount = r.BoundSIMsCount
		case tacEntry.ID:
			tacCount = r.BoundSIMsCount
		}
	}

	// full_imei entry should match exactly simA (bound_imei == fullIMEI).
	if fullCount != 1 {
		t.Errorf("full_imei BoundSIMsCount = %d, want 1", fullCount)
	}
	// tac_range entry should match BOTH simA (full IMEI shares TAC prefix)
	// and simB (different full IMEI but same TAC prefix). Both qualify.
	if tacCount != 2 {
		t.Errorf("tac_range BoundSIMsCount = %d, want 2", tacCount)
	}
}

package store

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newTestRadiusSessionStore(t *testing.T) *RadiusSessionStore {
	t.Helper()
	dbURL := "postgres://argus:argus_dev@localhost:5432/argus_dev?sslmode=disable"
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skipf("postgres not available: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		t.Skipf("postgres not available: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return NewRadiusSessionStore(pool)
}

func TestRadiusSessionStore_CreateAndGet(t *testing.T) {
	store := newTestRadiusSessionStore(t)
	ctx := context.Background()

	tenantID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	operatorID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	simID := uuid.MustParse("00000000-0000-0000-0000-000000000003")
	acctSessID := "test-acct-" + uuid.New().String()[:8]

	created, err := store.Create(ctx, CreateRadiusSessionParams{
		SimID:         simID,
		TenantID:      tenantID,
		OperatorID:    operatorID,
		AcctSessionID: &acctSessID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if created.ID == uuid.Nil {
		t.Error("created session ID should not be nil")
	}
	if created.SessionState != "active" {
		t.Errorf("SessionState = %q, want active", created.SessionState)
	}

	got, err := store.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("GetByID ID = %s, want %s", got.ID, created.ID)
	}

	gotByAcct, err := store.GetByAcctSessionID(ctx, acctSessID)
	if err != nil {
		t.Fatalf("GetByAcctSessionID: %v", err)
	}
	if gotByAcct.ID != created.ID {
		t.Errorf("GetByAcctSessionID ID = %s, want %s", gotByAcct.ID, created.ID)
	}
}

func TestRadiusSessionStore_UpdateCounters(t *testing.T) {
	store := newTestRadiusSessionStore(t)
	ctx := context.Background()

	tenantID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	operatorID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	simID := uuid.MustParse("00000000-0000-0000-0000-000000000004")

	created, err := store.Create(ctx, CreateRadiusSessionParams{
		SimID:      simID,
		TenantID:   tenantID,
		OperatorID: operatorID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.UpdateCounters(ctx, created.ID, 1024, 2048, 100, 200); err != nil {
		t.Fatalf("UpdateCounters: %v", err)
	}

	got, err := store.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.BytesIn != 1024 {
		t.Errorf("BytesIn = %d, want 1024", got.BytesIn)
	}
	if got.BytesOut != 2048 {
		t.Errorf("BytesOut = %d, want 2048", got.BytesOut)
	}
	if got.LastInterimAt == nil {
		t.Error("LastInterimAt should not be nil after update")
	}
}

func TestRadiusSessionStore_Finalize(t *testing.T) {
	store := newTestRadiusSessionStore(t)
	ctx := context.Background()

	tenantID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	operatorID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	simID := uuid.MustParse("00000000-0000-0000-0000-000000000005")

	created, err := store.Create(ctx, CreateRadiusSessionParams{
		SimID:      simID,
		TenantID:   tenantID,
		OperatorID: operatorID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.Finalize(ctx, created.ID, "user_request", 5000, 10000, 500, 1000); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	got, err := store.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.SessionState != "closed" {
		t.Errorf("SessionState = %q, want closed", got.SessionState)
	}
	if got.EndedAt == nil {
		t.Error("EndedAt should not be nil after finalize")
	}
	if got.TerminateCause == nil || *got.TerminateCause != "user_request" {
		t.Errorf("TerminateCause = %v, want user_request", got.TerminateCause)
	}
}

func TestRadiusSessionStore_CountActive(t *testing.T) {
	store := newTestRadiusSessionStore(t)
	ctx := context.Background()

	count, err := store.CountActive(ctx)
	if err != nil {
		t.Fatalf("CountActive: %v", err)
	}
	if count < 0 {
		t.Errorf("CountActive = %d, should be >= 0", count)
	}
}

func TestRadiusSessionStore_ListActiveBySIM(t *testing.T) {
	store := newTestRadiusSessionStore(t)
	ctx := context.Background()

	tenantID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	operatorID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	simID := uuid.MustParse("00000000-0000-0000-0000-000000000006")

	_, err := store.Create(ctx, CreateRadiusSessionParams{
		SimID:      simID,
		TenantID:   tenantID,
		OperatorID: operatorID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	sessions, err := store.ListActiveBySIM(ctx, simID)
	if err != nil {
		t.Fatalf("ListActiveBySIM: %v", err)
	}
	if len(sessions) == 0 {
		t.Error("expected at least 1 active session")
	}
}

func TestGetLastSessionBySIM_TenantMismatch(t *testing.T) {
	store := newTestRadiusSessionStore(t)
	ctx := context.Background()

	tenantID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	operatorID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	simID := uuid.New()

	_, err := store.Create(ctx, CreateRadiusSessionParams{
		SimID:      simID,
		TenantID:   tenantID,
		OperatorID: operatorID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	differentTenantID := uuid.New()
	sess, err := store.GetLastSessionBySIM(ctx, differentTenantID, simID)
	if err != nil {
		t.Fatalf("GetLastSessionBySIM: %v", err)
	}
	if sess != nil {
		t.Error("expected nil session when tenant does not match")
	}
}

func TestGetLastSessionBySIM_MatchingTenant(t *testing.T) {
	store := newTestRadiusSessionStore(t)
	ctx := context.Background()

	tenantID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	operatorID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	simID := uuid.New()

	created, err := store.Create(ctx, CreateRadiusSessionParams{
		SimID:      simID,
		TenantID:   tenantID,
		OperatorID: operatorID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	sess, err := store.GetLastSessionBySIM(ctx, tenantID, simID)
	if err != nil {
		t.Fatalf("GetLastSessionBySIM: %v", err)
	}
	if sess == nil {
		t.Fatal("expected session, got nil")
	}
	if sess.ID != created.ID {
		t.Errorf("session ID = %s, want %s", sess.ID, created.ID)
	}
}

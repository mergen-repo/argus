package store

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTenantStruct(t *testing.T) {
	tenant := Tenant{
		Name:         "Test Tenant",
		ContactEmail: "admin@test.com",
		MaxSims:      100000,
		MaxApns:      100,
		MaxUsers:     50,
		MaxAPIKeys:   20,
		State:        "active",
	}

	if tenant.Name != "Test Tenant" {
		t.Errorf("Name = %q, want %q", tenant.Name, "Test Tenant")
	}
	if tenant.MaxUsers != 50 {
		t.Errorf("MaxUsers = %d, want %d", tenant.MaxUsers, 50)
	}
	if tenant.MaxAPIKeys != 20 {
		t.Errorf("MaxAPIKeys = %d, want %d", tenant.MaxAPIKeys, 20)
	}
}

func TestTenantMaxAPIKeysDefault(t *testing.T) {
	tenant := Tenant{}
	if tenant.MaxAPIKeys != 0 {
		t.Errorf("MaxAPIKeys zero-value = %d, want 0", tenant.MaxAPIKeys)
	}
}

func TestCreateTenantParamsDefaults(t *testing.T) {
	p := CreateTenantParams{
		Name:         "Test",
		ContactEmail: "admin@test.com",
	}

	if p.MaxSims != nil {
		t.Error("MaxSims should be nil (default applied in Create)")
	}
	if p.MaxApns != nil {
		t.Error("MaxApns should be nil (default applied in Create)")
	}
	if p.MaxUsers != nil {
		t.Error("MaxUsers should be nil (default applied in Create)")
	}
}

func TestUpdateTenantParamsOptional(t *testing.T) {
	name := "Updated Name"
	p := UpdateTenantParams{
		Name: &name,
	}

	if p.Name == nil || *p.Name != "Updated Name" {
		t.Error("Name should be set")
	}
	if p.ContactEmail != nil {
		t.Error("ContactEmail should be nil")
	}
	if p.State != nil {
		t.Error("State should be nil")
	}
}

func TestTenantStatsDefaults(t *testing.T) {
	stats := TenantStats{}

	if stats.SimCount != 0 {
		t.Errorf("SimCount = %d, want 0", stats.SimCount)
	}
	if stats.UserCount != 0 {
		t.Errorf("UserCount = %d, want 0", stats.UserCount)
	}
	if stats.APNCount != 0 {
		t.Errorf("APNCount = %d, want 0", stats.APNCount)
	}
	if stats.ActiveSessions != 0 {
		t.Errorf("ActiveSessions = %d, want 0", stats.ActiveSessions)
	}
}

func TestErrDomainExists(t *testing.T) {
	if ErrDomainExists.Error() != "store: domain already exists" {
		t.Errorf("ErrDomainExists = %q", ErrDomainExists.Error())
	}
}

func TestErrTenantNotFound(t *testing.T) {
	if ErrTenantNotFound.Error() != "store: tenant not found" {
		t.Errorf("ErrTenantNotFound = %q", ErrTenantNotFound.Error())
	}
}

func TestTenantWithCountsStruct(t *testing.T) {
	twc := TenantWithCounts{
		Tenant:    Tenant{Name: "Acme", State: "active"},
		SimCount:  42,
		UserCount: 7,
	}
	if twc.Name != "Acme" {
		t.Errorf("Name = %q, want Acme", twc.Name)
	}
	if twc.SimCount != 42 {
		t.Errorf("SimCount = %d, want 42", twc.SimCount)
	}
	if twc.UserCount != 7 {
		t.Errorf("UserCount = %d, want 7", twc.UserCount)
	}
}

func testTenantPool(t *testing.T) *pgxpool.Pool {
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

func TestTenantStore_ListWithCounts_ReturnsCorrectCounts(t *testing.T) {
	pool := testTenantPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()

	var tenantID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, contact_email)
		VALUES ('fix103-list-counts-'||gen_random_uuid()::text, 'fix103@test.local')
		RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	var operatorID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM operators LIMIT 1`).Scan(&operatorID); err != nil {
		t.Fatalf("no operator in test DB: %v", err)
	}

	imsi := "286010" + uuid.New().String()[:9]
	iccid := "8990286010" + uuid.New().String()[:12]
	var simID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO sims (tenant_id, operator_id, iccid, imsi, sim_type, state)
		VALUES ($1, $2, $3, $4, 'physical', 'active')
		RETURNING id`, tenantID, operatorID, iccid, imsi).Scan(&simID); err != nil {
		t.Fatalf("seed sim: %v", err)
	}

	var userID uuid.UUID
	userEmail := "fix103-" + uuid.New().String()[:8] + "@test.local"
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (tenant_id, email, password_hash, role, state)
		VALUES ($1, $2, 'x', 'tenant_admin', 'active')
		RETURNING id`, tenantID, userEmail).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	var purgedSimID uuid.UUID
	purgedImsi := "286010" + uuid.New().String()[:9]
	purgedIccid := "8990286010" + uuid.New().String()[:12]
	if err := pool.QueryRow(ctx, `
		INSERT INTO sims (tenant_id, operator_id, iccid, imsi, sim_type, state)
		VALUES ($1, $2, $3, $4, 'physical', 'purged')
		RETURNING id`, tenantID, operatorID, purgedIccid, purgedImsi).Scan(&purgedSimID); err != nil {
		t.Fatalf("seed purged sim: %v", err)
	}

	t.Cleanup(func() {
		cleanCtx := context.Background()
		_, _ = pool.Exec(cleanCtx, `DELETE FROM sims WHERE id IN ($1, $2)`, simID, purgedSimID)
		_, _ = pool.Exec(cleanCtx, `DELETE FROM users WHERE id = $1`, userID)
		_, _ = pool.Exec(cleanCtx, `DELETE FROM tenants WHERE id = $1`, tenantID)
	})

	s := NewTenantStore(pool)
	results, _, err := s.ListWithCounts(ctx, "", 100, "")
	if err != nil {
		t.Fatalf("ListWithCounts: %v", err)
	}

	var found *TenantWithCounts
	for i := range results {
		if results[i].ID == tenantID {
			found = &results[i]
			break
		}
	}
	if found == nil {
		t.Fatal("seeded tenant not found in ListWithCounts results")
	}
	if found.SimCount != 1 {
		t.Errorf("SimCount = %d, want 1 (purged sim excluded)", found.SimCount)
	}
	if found.UserCount != 1 {
		t.Errorf("UserCount = %d, want 1", found.UserCount)
	}
}

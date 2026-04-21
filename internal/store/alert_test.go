package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testAlertPool(t *testing.T) *pgxpool.Pool {
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

func seedAlertTenant(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	_, err := pool.Exec(ctx,
		`INSERT INTO tenants (id, name, contact_email) VALUES ($1, $2, $3)`,
		id, "alert-test-tenant-"+id.String()[:8], "alert-test-"+id.String()[:8]+"@test.invalid",
	)
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM alerts WHERE tenant_id = $1`, id)
		_, _ = pool.Exec(context.Background(), `DELETE FROM tenants WHERE id = $1`, id)
	})
	return id
}

func seedAlertUser(t *testing.T, pool *pgxpool.Pool, tenantID uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	_, err := pool.Exec(ctx,
		`INSERT INTO users (id, tenant_id, email, password_hash, name, role) VALUES ($1, $2, $3, $4, $5, $6)`,
		id, tenantID, "alertuser-"+id.String()[:8]+"@test.invalid", "$2a$10$placeholder", "Alert Tester", "tenant_admin",
	)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	// NULL out acknowledged_by before deleting the user so the FK to users(id) does not block cleanup.
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `UPDATE alerts SET acknowledged_by = NULL WHERE acknowledged_by = $1`, id)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id)
	})
	return id
}

func makeAlertParams(tenantID uuid.UUID) CreateAlertParams {
	return CreateAlertParams{
		TenantID:    tenantID,
		Type:        "sim.data_spike",
		Severity:    "high",
		Source:      "sim",
		Title:       "Test Alert",
		Description: "Test description",
	}
}

func TestAlertStore_Create_InsertsWithDefaults(t *testing.T) {
	pool := testAlertPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert store test")
	}
	tenantID := seedAlertTenant(t, pool)
	s := NewAlertStore(pool)
	ctx := context.Background()

	a, err := s.Create(ctx, makeAlertParams(tenantID))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if a.State != "open" {
		t.Errorf("State: got %q, want %q", a.State, "open")
	}
	if time.Since(a.FiredAt) > 5*time.Second {
		t.Errorf("FiredAt too old: %v", a.FiredAt)
	}
	if a.ID == uuid.Nil {
		t.Error("ID should not be nil")
	}
}

func TestAlertStore_Create_PersistsAllFields(t *testing.T) {
	pool := testAlertPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert store test")
	}
	tenantID := seedAlertTenant(t, pool)
	s := NewAlertStore(pool)
	ctx := context.Background()

	simID := uuid.New()
	opID := uuid.New()
	apnID := uuid.New()
	dk := "dedup-" + uuid.New().String()
	firedAt := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Microsecond)
	meta := json.RawMessage(`{"key":"value","count":42}`)

	p := CreateAlertParams{
		TenantID:    tenantID,
		Type:        "operator.down",
		Severity:    "critical",
		Source:      "operator",
		Title:       "Operator Down",
		Description: "Operator is unreachable",
		Meta:        meta,
		SimID:       &simID,
		OperatorID:  &opID,
		APNID:       &apnID,
		DedupKey:    &dk,
		FiredAt:     firedAt,
	}
	created, err := s.Create(ctx, p)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.GetByID(ctx, tenantID, created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}

	if got.Type != "operator.down" {
		t.Errorf("Type: got %q, want %q", got.Type, "operator.down")
	}
	if got.Severity != "critical" {
		t.Errorf("Severity: got %q, want %q", got.Severity, "critical")
	}
	if got.Source != "operator" {
		t.Errorf("Source: got %q, want %q", got.Source, "operator")
	}
	if got.Title != "Operator Down" {
		t.Errorf("Title: got %q, want %q", got.Title, "Operator Down")
	}
	if got.Description != "Operator is unreachable" {
		t.Errorf("Description: got %q", got.Description)
	}
	var gotMeta, wantMeta map[string]interface{}
	if err := json.Unmarshal(got.Meta, &gotMeta); err != nil {
		t.Fatalf("unmarshal got.Meta: %v", err)
	}
	if err := json.Unmarshal(meta, &wantMeta); err != nil {
		t.Fatalf("unmarshal want meta: %v", err)
	}
	if fmt.Sprintf("%v", gotMeta) != fmt.Sprintf("%v", wantMeta) {
		t.Errorf("Meta: got %v, want %v", gotMeta, wantMeta)
	}
	if got.SimID == nil || *got.SimID != simID {
		t.Errorf("SimID mismatch")
	}
	if got.OperatorID == nil || *got.OperatorID != opID {
		t.Errorf("OperatorID mismatch")
	}
	if got.APNID == nil || *got.APNID != apnID {
		t.Errorf("APNID mismatch")
	}
	if got.DedupKey == nil || *got.DedupKey != dk {
		t.Errorf("DedupKey mismatch")
	}
	if !got.FiredAt.Equal(firedAt) {
		t.Errorf("FiredAt: got %v, want %v", got.FiredAt, firedAt)
	}
}

func TestAlertStore_ListByTenant_FiltersCombined(t *testing.T) {
	pool := testAlertPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert store test")
	}
	tenantID := seedAlertTenant(t, pool)
	s := NewAlertStore(pool)
	ctx := context.Background()

	seeds := []CreateAlertParams{
		{TenantID: tenantID, Type: "t1", Severity: "critical", Source: "operator", Title: "A1", Description: ""},
		{TenantID: tenantID, Type: "t1", Severity: "high", Source: "operator", Title: "A2", Description: ""},
		{TenantID: tenantID, Type: "t1", Severity: "critical", Source: "infra", Title: "A3", Description: ""},
		{TenantID: tenantID, Type: "t1", Severity: "high", Source: "sim", Title: "A4", Description: ""},
	}
	for i := range seeds {
		if _, err := s.Create(ctx, seeds[i]); err != nil {
			t.Fatalf("seed[%d]: %v", i, err)
		}
	}

	rows, _, err := s.ListByTenant(ctx, tenantID, ListAlertsParams{
		Source:   "operator",
		Severity: "critical",
	})
	if err != nil {
		t.Fatalf("ListByTenant: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("len(rows): got %d, want 1", len(rows))
	}
	if len(rows) > 0 && rows[0].Source != "operator" {
		t.Errorf("Source: got %q, want operator", rows[0].Source)
	}
}

func TestAlertStore_ListByTenant_CursorPagination(t *testing.T) {
	pool := testAlertPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert store test")
	}
	tenantID := seedAlertTenant(t, pool)
	s := NewAlertStore(pool)
	ctx := context.Background()

	base := time.Now().UTC().Add(-1 * time.Hour)
	for i := 0; i < 5; i++ {
		p := CreateAlertParams{
			TenantID:    tenantID,
			Type:        "t.page",
			Severity:    "info",
			Source:      "system",
			Title:       "Page Alert",
			Description: "",
			FiredAt:     base.Add(time.Duration(i) * time.Minute),
		}
		if _, err := s.Create(ctx, p); err != nil {
			t.Fatalf("seed row %d: %v", i, err)
		}
	}

	page1, cursor1, err := s.ListByTenant(ctx, tenantID, ListAlertsParams{Type: "t.page", Limit: 2})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("page1 len: got %d, want 2", len(page1))
	}
	if cursor1 == nil {
		t.Fatal("cursor1 should not be nil")
	}

	page2, cursor2, err := s.ListByTenant(ctx, tenantID, ListAlertsParams{Type: "t.page", Limit: 2, Cursor: cursor1})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2) != 2 {
		t.Fatalf("page2 len: got %d, want 2", len(page2))
	}
	if cursor2 == nil {
		t.Fatal("cursor2 should not be nil")
	}

	page3, cursor3, err := s.ListByTenant(ctx, tenantID, ListAlertsParams{Type: "t.page", Limit: 2, Cursor: cursor2})
	if err != nil {
		t.Fatalf("page3: %v", err)
	}
	if len(page3) != 1 {
		t.Fatalf("page3 len: got %d, want 1", len(page3))
	}
	if cursor3 != nil {
		t.Error("cursor3 should be nil on last page")
	}

	seen := map[uuid.UUID]bool{}
	for _, p := range append(append(page1, page2...), page3...) {
		if seen[p.ID] {
			t.Errorf("duplicate ID across pages: %s", p.ID)
		}
		seen[p.ID] = true
	}
}

func TestAlertStore_ListByTenant_TenantScope(t *testing.T) {
	pool := testAlertPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert store test")
	}
	tenantA := seedAlertTenant(t, pool)
	tenantB := seedAlertTenant(t, pool)
	s := NewAlertStore(pool)
	ctx := context.Background()

	p := makeAlertParams(tenantA)
	if _, err := s.Create(ctx, p); err != nil {
		t.Fatalf("seed tenantA alert: %v", err)
	}

	rows, _, err := s.ListByTenant(ctx, tenantB, ListAlertsParams{})
	if err != nil {
		t.Fatalf("ListByTenant tenantB: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("tenantB should see 0 rows, got %d", len(rows))
	}
}

func TestAlertStore_UpdateState_ValidTransition(t *testing.T) {
	pool := testAlertPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert store test")
	}
	tenantID := seedAlertTenant(t, pool)
	userID := seedAlertUser(t, pool, tenantID)
	s := NewAlertStore(pool)
	ctx := context.Background()

	a, err := s.Create(ctx, makeAlertParams(tenantID))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	acked, err := s.UpdateState(ctx, tenantID, a.ID, "acknowledged", &userID)
	if err != nil {
		t.Fatalf("UpdateState open→acknowledged: %v", err)
	}
	if acked.State != "acknowledged" {
		t.Errorf("State: got %q, want acknowledged", acked.State)
	}
	if acked.AcknowledgedAt == nil {
		t.Error("AcknowledgedAt should not be nil")
	}
	if acked.AcknowledgedBy == nil || *acked.AcknowledgedBy != userID {
		t.Error("AcknowledgedBy should match userID")
	}

	resolved, err := s.UpdateState(ctx, tenantID, a.ID, "resolved", nil)
	if err != nil {
		t.Fatalf("UpdateState acknowledged→resolved: %v", err)
	}
	if resolved.State != "resolved" {
		t.Errorf("State: got %q, want resolved", resolved.State)
	}
	if resolved.ResolvedAt == nil {
		t.Error("ResolvedAt should not be nil")
	}
	if resolved.AcknowledgedAt == nil {
		t.Error("AcknowledgedAt should be preserved after resolved transition")
	}
}

func TestAlertStore_UpdateState_InvalidTransition(t *testing.T) {
	pool := testAlertPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert store test")
	}
	tenantID := seedAlertTenant(t, pool)
	s := NewAlertStore(pool)
	ctx := context.Background()

	id := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO alerts (id, tenant_id, type, severity, source, state, title)
		VALUES ($1, $2, 'test', 'info', 'system', 'resolved', 'Direct Insert')
	`, id, tenantID)
	if err != nil {
		t.Fatalf("direct insert resolved alert: %v", err)
	}

	_, err = s.UpdateState(ctx, tenantID, id, "open", nil)
	if !errors.Is(err, ErrInvalidAlertTransition) {
		t.Errorf("expected ErrInvalidAlertTransition, got: %v", err)
	}
}

func TestAlertStore_UpdateState_SuppressedNotExposed(t *testing.T) {
	pool := testAlertPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert store test")
	}
	tenantID := seedAlertTenant(t, pool)
	s := NewAlertStore(pool)
	ctx := context.Background()

	a, err := s.Create(ctx, makeAlertParams(tenantID))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = s.UpdateState(ctx, tenantID, a.ID, "suppressed", nil)
	if !errors.Is(err, ErrInvalidAlertTransition) {
		t.Errorf("expected ErrInvalidAlertTransition for suppressed, got: %v", err)
	}
}

func TestAlertStore_UpdateState_NotFound(t *testing.T) {
	pool := testAlertPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert store test")
	}
	tenantID := seedAlertTenant(t, pool)
	s := NewAlertStore(pool)
	ctx := context.Background()

	_, err := s.UpdateState(ctx, tenantID, uuid.New(), "acknowledged", nil)
	if !errors.Is(err, ErrAlertNotFound) {
		t.Errorf("expected ErrAlertNotFound, got: %v", err)
	}
}

func TestAlertStore_UpdateState_TenantIsolation(t *testing.T) {
	pool := testAlertPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert store test")
	}
	tenantA := seedAlertTenant(t, pool)
	tenantB := seedAlertTenant(t, pool)
	s := NewAlertStore(pool)
	ctx := context.Background()

	a, err := s.Create(ctx, makeAlertParams(tenantA))
	if err != nil {
		t.Fatalf("Create under tenantA: %v", err)
	}

	_, err = s.UpdateState(ctx, tenantB, a.ID, "acknowledged", nil)
	if !errors.Is(err, ErrAlertNotFound) {
		t.Errorf("expected ErrAlertNotFound from tenantB scope, got: %v", err)
	}
}

func TestAlertStore_CountByTenantAndState(t *testing.T) {
	pool := testAlertPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert store test")
	}
	tenantID := seedAlertTenant(t, pool)
	s := NewAlertStore(pool)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := s.Create(ctx, makeAlertParams(tenantID)); err != nil {
			t.Fatalf("seed open[%d]: %v", i, err)
		}
	}
	for i := 0; i < 2; i++ {
		id := uuid.New()
		_, err := pool.Exec(ctx, `
			INSERT INTO alerts (id, tenant_id, type, severity, source, state, title)
			VALUES ($1, $2, 'test', 'info', 'system', 'resolved', 'Resolved Alert')
		`, id, tenantID)
		if err != nil {
			t.Fatalf("seed resolved[%d]: %v", i, err)
		}
	}

	openCount, err := s.CountByTenantAndState(ctx, tenantID, "open")
	if err != nil {
		t.Fatalf("count open: %v", err)
	}
	if openCount != 3 {
		t.Errorf("open count: got %d, want 3", openCount)
	}

	resolvedCount, err := s.CountByTenantAndState(ctx, tenantID, "resolved")
	if err != nil {
		t.Fatalf("count resolved: %v", err)
	}
	if resolvedCount != 2 {
		t.Errorf("resolved count: got %d, want 2", resolvedCount)
	}
}

func TestAlertStore_DeleteOlderThan(t *testing.T) {
	pool := testAlertPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert store test")
	}
	tenantID := seedAlertTenant(t, pool)
	s := NewAlertStore(pool)
	ctx := context.Background()

	oldFiredAt := time.Now().UTC().Add(-200 * 24 * time.Hour)
	for i := 0; i < 3; i++ {
		id := uuid.New()
		_, err := pool.Exec(ctx, `
			INSERT INTO alerts (id, tenant_id, type, severity, source, title, fired_at)
			VALUES ($1, $2, 'old', 'info', 'system', 'Old Alert', $3)
		`, id, tenantID, oldFiredAt)
		if err != nil {
			t.Fatalf("seed old[%d]: %v", i, err)
		}
	}
	newFiredAt := time.Now().UTC().Add(-10 * 24 * time.Hour)
	for i := 0; i < 2; i++ {
		id := uuid.New()
		_, err := pool.Exec(ctx, `
			INSERT INTO alerts (id, tenant_id, type, severity, source, title, fired_at)
			VALUES ($1, $2, 'new', 'info', 'system', 'New Alert', $3)
		`, id, tenantID, newFiredAt)
		if err != nil {
			t.Fatalf("seed new[%d]: %v", i, err)
		}
	}

	cutoff := time.Now().UTC().Add(-180 * 24 * time.Hour)
	deleted, err := s.DeleteOlderThan(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeleteOlderThan: %v", err)
	}
	if deleted < 3 {
		t.Errorf("deleted: got %d, want >= 3", deleted)
	}

	remaining, _, err := s.ListByTenant(ctx, tenantID, ListAlertsParams{Type: "new"})
	if err != nil {
		t.Fatalf("list remaining: %v", err)
	}
	if len(remaining) != 2 {
		t.Errorf("remaining new alerts: got %d, want 2", len(remaining))
	}
}

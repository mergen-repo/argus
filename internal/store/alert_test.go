package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
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

	acked, err := s.UpdateState(ctx, tenantID, a.ID, "acknowledged", &userID, 0)
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

	resolved, err := s.UpdateState(ctx, tenantID, a.ID, "resolved", nil, 0)
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

	_, err = s.UpdateState(ctx, tenantID, id, "open", nil, 0)
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

	_, err = s.UpdateState(ctx, tenantID, a.ID, "suppressed", nil, 0)
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

	_, err := s.UpdateState(ctx, tenantID, uuid.New(), "acknowledged", nil, 0)
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

	_, err = s.UpdateState(ctx, tenantB, a.ID, "acknowledged", nil, 0)
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

func makeDedupParams(tenantID uuid.UUID, dedupKey, sev string) CreateAlertParams {
	dk := dedupKey
	return CreateAlertParams{
		TenantID:    tenantID,
		Type:        "sim.data_spike",
		Severity:    sev,
		Source:      "sim",
		Title:       "Dedup Alert",
		Description: "dedup test",
		DedupKey:    &dk,
	}
}

func severityOrdinalOf(sev string) int {
	switch sev {
	case "critical":
		return 5
	case "high":
		return 4
	case "medium":
		return 3
	case "low":
		return 2
	case "info":
		return 1
	}
	return 0
}

func TestAlertStore_UpsertWithDedup_FirstEventInserts(t *testing.T) {
	pool := testAlertPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert store test")
	}
	tenantID := seedAlertTenant(t, pool)
	s := NewAlertStore(pool)
	ctx := context.Background()

	firedAt := time.Now().UTC().Add(-30 * time.Second).Truncate(time.Microsecond)
	p := makeDedupParams(tenantID, "dk-"+uuid.New().String(), "high")
	p.FiredAt = firedAt

	a, res, err := s.UpsertWithDedup(ctx, p, severityOrdinalOf("high"))
	if err != nil {
		t.Fatalf("UpsertWithDedup: %v", err)
	}
	if res != UpsertInserted {
		t.Errorf("result: got %v, want UpsertInserted", res)
	}
	if a.OccurrenceCount != 1 {
		t.Errorf("OccurrenceCount: got %d, want 1", a.OccurrenceCount)
	}
	if !a.FirstSeenAt.Equal(firedAt) {
		t.Errorf("FirstSeenAt: got %v, want %v", a.FirstSeenAt, firedAt)
	}
	if !a.LastSeenAt.Equal(firedAt) {
		t.Errorf("LastSeenAt: got %v, want %v", a.LastSeenAt, firedAt)
	}
}

func TestAlertStore_UpsertWithDedup_SecondEventIncrements(t *testing.T) {
	pool := testAlertPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert store test")
	}
	tenantID := seedAlertTenant(t, pool)
	s := NewAlertStore(pool)
	ctx := context.Background()

	dk := "dk-" + uuid.New().String()
	p := makeDedupParams(tenantID, dk, "high")

	first, _, err := s.UpsertWithDedup(ctx, p, severityOrdinalOf("high"))
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	firstFiredAt := first.FiredAt
	firstLastSeen := first.LastSeenAt

	time.Sleep(15 * time.Millisecond)

	second, res, err := s.UpsertWithDedup(ctx, p, severityOrdinalOf("high"))
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if res != UpsertDeduplicated {
		t.Errorf("result: got %v, want UpsertDeduplicated", res)
	}
	if second.ID != first.ID {
		t.Errorf("id drift: got %s, want %s", second.ID, first.ID)
	}
	if second.OccurrenceCount != 2 {
		t.Errorf("OccurrenceCount: got %d, want 2", second.OccurrenceCount)
	}
	if !second.LastSeenAt.After(firstLastSeen) {
		t.Errorf("LastSeenAt should advance: first=%v second=%v", firstLastSeen, second.LastSeenAt)
	}
	if !second.FiredAt.Equal(firstFiredAt) {
		t.Errorf("FiredAt drift: first=%v second=%v", firstFiredAt, second.FiredAt)
	}
}

func TestAlertStore_UpsertWithDedup_SeverityEscalationUpdatesInPlace(t *testing.T) {
	pool := testAlertPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert store test")
	}
	tenantID := seedAlertTenant(t, pool)
	s := NewAlertStore(pool)
	ctx := context.Background()

	dk := "dk-" + uuid.New().String()
	pMed := makeDedupParams(tenantID, dk, "medium")
	if _, _, err := s.UpsertWithDedup(ctx, pMed, severityOrdinalOf("medium")); err != nil {
		t.Fatalf("first (medium): %v", err)
	}

	pHigh := makeDedupParams(tenantID, dk, "high")
	second, _, err := s.UpsertWithDedup(ctx, pHigh, severityOrdinalOf("high"))
	if err != nil {
		t.Fatalf("second (high): %v", err)
	}
	if second.Severity != "high" {
		t.Errorf("Severity: got %q, want high (escalation)", second.Severity)
	}
}

func TestAlertStore_UpsertWithDedup_SeverityDowngradeKeepsHigher(t *testing.T) {
	pool := testAlertPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert store test")
	}
	tenantID := seedAlertTenant(t, pool)
	s := NewAlertStore(pool)
	ctx := context.Background()

	dk := "dk-" + uuid.New().String()
	pCrit := makeDedupParams(tenantID, dk, "critical")
	if _, _, err := s.UpsertWithDedup(ctx, pCrit, severityOrdinalOf("critical")); err != nil {
		t.Fatalf("first (critical): %v", err)
	}

	pLow := makeDedupParams(tenantID, dk, "low")
	second, _, err := s.UpsertWithDedup(ctx, pLow, severityOrdinalOf("low"))
	if err != nil {
		t.Fatalf("second (low): %v", err)
	}
	if second.Severity != "critical" {
		t.Errorf("Severity: got %q, want critical (no downgrade)", second.Severity)
	}
}

func TestAlertStore_UpsertWithDedup_ConcurrentHit(t *testing.T) {
	pool := testAlertPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert store test")
	}
	tenantID := seedAlertTenant(t, pool)
	s := NewAlertStore(pool)
	ctx := context.Background()

	dk := "dk-" + uuid.New().String()
	const n = 10

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		inserted int
		deduped  int
		errs     []error
	)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			p := makeDedupParams(tenantID, dk, "high")
			_, res, err := s.UpsertWithDedup(ctx, p, severityOrdinalOf("high"))
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			switch res {
			case UpsertInserted:
				inserted++
			case UpsertDeduplicated:
				deduped++
			}
		}()
	}
	wg.Wait()

	if len(errs) != 0 {
		t.Fatalf("goroutine errors: %v", errs)
	}
	if inserted != 1 {
		t.Errorf("inserted: got %d, want 1", inserted)
	}
	if deduped != n-1 {
		t.Errorf("deduplicated: got %d, want %d", deduped, n-1)
	}

	var rowCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM alerts WHERE tenant_id=$1 AND dedup_key=$2`, tenantID, dk).Scan(&rowCount); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if rowCount != 1 {
		t.Errorf("row count: got %d, want 1", rowCount)
	}

	final, err := s.FindActiveByDedupKey(ctx, tenantID, dk)
	if err != nil {
		t.Fatalf("FindActiveByDedupKey: %v", err)
	}
	if final.OccurrenceCount != n {
		t.Errorf("OccurrenceCount: got %d, want %d", final.OccurrenceCount, n)
	}
}

func TestAlertStore_UpsertWithDedup_CooldownActive_ReturnsCoolingDown(t *testing.T) {
	pool := testAlertPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert store test")
	}
	tenantID := seedAlertTenant(t, pool)
	s := NewAlertStore(pool)
	ctx := context.Background()

	dk := "dk-" + uuid.New().String()
	id := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO alerts (id, tenant_id, type, severity, source, state, title, dedup_key, cooldown_until)
		VALUES ($1, $2, 'sim.data_spike', 'high', 'sim', 'resolved', 'seed', $3, NOW() + INTERVAL '5 minutes')
	`, id, tenantID, dk); err != nil {
		t.Fatalf("seed resolved with cooldown: %v", err)
	}

	p := makeDedupParams(tenantID, dk, "high")
	a, res, err := s.UpsertWithDedup(ctx, p, severityOrdinalOf("high"))
	if err != nil {
		t.Fatalf("UpsertWithDedup: %v", err)
	}
	if res != UpsertCoolingDown {
		t.Errorf("result: got %v, want UpsertCoolingDown", res)
	}
	if a != nil {
		t.Errorf("alert should be nil on cooling down, got %+v", a)
	}

	var rowCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM alerts WHERE tenant_id=$1 AND dedup_key=$2`, tenantID, dk).Scan(&rowCount); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rowCount != 1 {
		t.Errorf("row count: got %d, want 1 (no new row during cooldown)", rowCount)
	}
}

func TestAlertStore_UpsertWithDedup_CooldownExpired_InsertsFresh(t *testing.T) {
	pool := testAlertPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert store test")
	}
	tenantID := seedAlertTenant(t, pool)
	s := NewAlertStore(pool)
	ctx := context.Background()

	dk := "dk-" + uuid.New().String()
	id := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO alerts (id, tenant_id, type, severity, source, state, title, dedup_key, cooldown_until)
		VALUES ($1, $2, 'sim.data_spike', 'high', 'sim', 'resolved', 'seed', $3, NOW() - INTERVAL '1 second')
	`, id, tenantID, dk); err != nil {
		t.Fatalf("seed resolved with expired cooldown: %v", err)
	}

	p := makeDedupParams(tenantID, dk, "high")
	a, res, err := s.UpsertWithDedup(ctx, p, severityOrdinalOf("high"))
	if err != nil {
		t.Fatalf("UpsertWithDedup: %v", err)
	}
	if res != UpsertInserted {
		t.Errorf("result: got %v, want UpsertInserted", res)
	}
	if a == nil || a.State != "open" {
		t.Errorf("alert: got %+v, want open", a)
	}

	var rowCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM alerts WHERE tenant_id=$1 AND dedup_key=$2`, tenantID, dk).Scan(&rowCount); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rowCount != 2 {
		t.Errorf("row count: got %d, want 2 (resolved + new open)", rowCount)
	}
}

func TestAlertStore_UpdateState_ResolveStampsCooldownUntil(t *testing.T) {
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

	before := time.Now().UTC()
	resolved, err := s.UpdateState(ctx, tenantID, a.ID, "resolved", nil, 5)
	if err != nil {
		t.Fatalf("UpdateState resolve with cooldown: %v", err)
	}
	if resolved.CooldownUntil == nil {
		t.Fatal("CooldownUntil should not be nil")
	}
	minExpected := before.Add(4 * time.Minute)
	maxExpected := before.Add(6 * time.Minute)
	if resolved.CooldownUntil.Before(minExpected) || resolved.CooldownUntil.After(maxExpected) {
		t.Errorf("CooldownUntil: got %v, want in [%v, %v]", *resolved.CooldownUntil, minExpected, maxExpected)
	}
}

func TestAlertStore_UpdateState_ResolveWithZeroCooldown_NoStamp(t *testing.T) {
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

	resolved, err := s.UpdateState(ctx, tenantID, a.ID, "resolved", nil, 0)
	if err != nil {
		t.Fatalf("UpdateState resolve without cooldown: %v", err)
	}
	if resolved.CooldownUntil != nil {
		t.Errorf("CooldownUntil should remain nil, got %v", *resolved.CooldownUntil)
	}
}

func TestAlertStore_SuppressAlert_FromOpen_Succeeds(t *testing.T) {
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

	supp, err := s.SuppressAlert(ctx, tenantID, a.ID, "maintenance window")
	if err != nil {
		t.Fatalf("SuppressAlert: %v", err)
	}
	if supp.State != "suppressed" {
		t.Errorf("State: got %q, want suppressed", supp.State)
	}
	var meta map[string]interface{}
	if err := json.Unmarshal(supp.Meta, &meta); err != nil {
		t.Fatalf("unmarshal meta: %v", err)
	}
	if meta["suppress_reason"] != "maintenance window" {
		t.Errorf("suppress_reason: got %v, want 'maintenance window'", meta["suppress_reason"])
	}
}

func TestAlertStore_SuppressAlert_FromResolved_Fails(t *testing.T) {
	pool := testAlertPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert store test")
	}
	tenantID := seedAlertTenant(t, pool)
	s := NewAlertStore(pool)
	ctx := context.Background()

	id := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO alerts (id, tenant_id, type, severity, source, state, title)
		VALUES ($1, $2, 'test', 'info', 'system', 'resolved', 'resolved seed')
	`, id, tenantID); err != nil {
		t.Fatalf("seed resolved: %v", err)
	}

	_, err := s.SuppressAlert(ctx, tenantID, id, "no reason")
	if !errors.Is(err, ErrInvalidAlertTransition) {
		t.Errorf("expected ErrInvalidAlertTransition, got: %v", err)
	}
}

func TestAlertStore_UnsuppressAlert_ReopensToOpen(t *testing.T) {
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
	acked, err := s.UpdateState(ctx, tenantID, a.ID, "acknowledged", &userID, 0)
	if err != nil {
		t.Fatalf("ack: %v", err)
	}
	if acked.AcknowledgedAt == nil {
		t.Fatal("AcknowledgedAt should be set after ack")
	}
	originalAckAt := *acked.AcknowledgedAt

	supp, err := s.SuppressAlert(ctx, tenantID, a.ID, "temp mute")
	if err != nil {
		t.Fatalf("SuppressAlert: %v", err)
	}
	if supp.State != "suppressed" {
		t.Fatalf("state: got %q, want suppressed", supp.State)
	}

	reopened, err := s.UnsuppressAlert(ctx, tenantID, a.ID)
	if err != nil {
		t.Fatalf("UnsuppressAlert: %v", err)
	}
	if reopened.State != "open" {
		t.Errorf("State: got %q, want open", reopened.State)
	}
	if reopened.AcknowledgedAt == nil || !reopened.AcknowledgedAt.Equal(originalAckAt) {
		t.Errorf("AcknowledgedAt not preserved: got %v, want %v", reopened.AcknowledgedAt, originalAckAt)
	}
	if reopened.AcknowledgedBy == nil || *reopened.AcknowledgedBy != userID {
		t.Errorf("AcknowledgedBy not preserved: got %v, want %v", reopened.AcknowledgedBy, userID)
	}
}

func TestAlertStore_UnsuppressAlert_FromOpen_Fails(t *testing.T) {
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

	_, err = s.UnsuppressAlert(ctx, tenantID, a.ID)
	if !errors.Is(err, ErrInvalidAlertTransition) {
		t.Errorf("expected ErrInvalidAlertTransition, got: %v", err)
	}
}

func TestAlertStore_FindActiveByDedupKey_ResolvedNotReturned(t *testing.T) {
	pool := testAlertPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert store test")
	}
	tenantID := seedAlertTenant(t, pool)
	s := NewAlertStore(pool)
	ctx := context.Background()

	dk := "dk-" + uuid.New().String()
	id := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO alerts (id, tenant_id, type, severity, source, state, title, dedup_key)
		VALUES ($1, $2, 'test', 'info', 'system', 'resolved', 'resolved seed', $3)
	`, id, tenantID, dk); err != nil {
		t.Fatalf("seed resolved with dedup_key: %v", err)
	}

	_, err := s.FindActiveByDedupKey(ctx, tenantID, dk)
	if !errors.Is(err, ErrAlertNotFound) {
		t.Errorf("expected ErrAlertNotFound for resolved row, got: %v", err)
	}
}

func TestAlertStore_UpsertWithDedup_NilDedupKey_FallsThroughToCreate(t *testing.T) {
	pool := testAlertPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert store test")
	}
	tenantID := seedAlertTenant(t, pool)
	s := NewAlertStore(pool)
	ctx := context.Background()

	p := makeAlertParams(tenantID)
	if p.DedupKey != nil {
		t.Fatalf("makeAlertParams should have nil DedupKey")
	}

	a, res, err := s.UpsertWithDedup(ctx, p, severityOrdinalOf("high"))
	if err != nil {
		t.Fatalf("UpsertWithDedup nil dedup: %v", err)
	}
	if res != UpsertInserted {
		t.Errorf("result: got %v, want UpsertInserted", res)
	}
	if a == nil || a.ID == uuid.Nil {
		t.Fatalf("alert ID should be set, got %+v", a)
	}
	if a.State != "open" {
		t.Errorf("State: got %q, want open", a.State)
	}
}

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

func testSuppressionPool(t *testing.T) *pgxpool.Pool {
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

func seedSuppressionTenant(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	_, err := pool.Exec(ctx,
		`INSERT INTO tenants (id, name, contact_email) VALUES ($1, $2, $3)`,
		id, "supp-test-tenant-"+id.String()[:8], "supp-test-"+id.String()[:8]+"@test.invalid",
	)
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM alert_suppressions WHERE tenant_id = $1`, id)
		_, _ = pool.Exec(context.Background(), `DELETE FROM alerts WHERE tenant_id = $1`, id)
		_, _ = pool.Exec(context.Background(), `DELETE FROM tenants WHERE id = $1`, id)
	})
	return id
}

func makeSuppressionParams(tenantID uuid.UUID, scopeType, scopeValue string, expiresIn time.Duration) CreateAlertSuppressionParams {
	return CreateAlertSuppressionParams{
		TenantID:   tenantID,
		ScopeType:  scopeType,
		ScopeValue: scopeValue,
		ExpiresAt:  time.Now().UTC().Add(expiresIn),
	}
}

func TestAlertSuppressionStore_CreateAndMatchActive_TypeScope(t *testing.T) {
	pool := testSuppressionPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert suppression store test")
	}
	tenantID := seedSuppressionTenant(t, pool)
	ss := NewAlertSuppressionStore(pool)
	ctx := context.Background()

	alertType := "sim.data_spike"
	p := makeSuppressionParams(tenantID, "type", alertType, 1*time.Hour)
	reason := "maintenance"
	p.Reason = &reason

	created, err := ss.Create(ctx, p)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == uuid.Nil {
		t.Error("ID should not be nil")
	}
	if created.ScopeType != "type" {
		t.Errorf("ScopeType: got %q, want type", created.ScopeType)
	}
	if created.ScopeValue != alertType {
		t.Errorf("ScopeValue: got %q, want %q", created.ScopeValue, alertType)
	}

	probe := AlertMatchProbe{
		AlertID: uuid.New(),
		Type:    alertType,
	}
	match, err := ss.MatchActive(ctx, tenantID, probe)
	if err != nil {
		t.Fatalf("MatchActive: %v", err)
	}
	if match == nil {
		t.Fatal("MatchActive: expected a match, got nil")
	}
	if match.ID != created.ID {
		t.Errorf("MatchActive ID: got %s, want %s", match.ID, created.ID)
	}

	otherTenant := seedSuppressionTenant(t, pool)
	noMatch, err := ss.MatchActive(ctx, otherTenant, probe)
	if err != nil {
		t.Fatalf("MatchActive other tenant: %v", err)
	}
	if noMatch != nil {
		t.Error("MatchActive should return nil for different tenant")
	}
}

func TestAlertSuppressionStore_DuplicateRuleName_ReturnsErrDuplicateRuleName(t *testing.T) {
	pool := testSuppressionPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert suppression store test")
	}
	tenantID := seedSuppressionTenant(t, pool)
	ss := NewAlertSuppressionStore(pool)
	ctx := context.Background()

	ruleName := "rule-" + uuid.New().String()[:8]
	p := makeSuppressionParams(tenantID, "type", "sim.data_spike", 1*time.Hour)
	p.RuleName = &ruleName

	if _, err := ss.Create(ctx, p); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	p2 := makeSuppressionParams(tenantID, "type", "operator.down", 2*time.Hour)
	p2.RuleName = &ruleName
	_, err := ss.Create(ctx, p2)
	if !errors.Is(err, ErrDuplicateRuleName) {
		t.Errorf("expected ErrDuplicateRuleName, got: %v", err)
	}
}

func TestAlertStore_DeleteOlderThanForTenant_ScopesCorrectly(t *testing.T) {
	pool := testSuppressionPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert suppression store test")
	}
	tenantA := seedSuppressionTenant(t, pool)
	tenantB := seedSuppressionTenant(t, pool)
	as_ := NewAlertStore(pool)
	ctx := context.Background()

	oldFiredAt := time.Now().UTC().Add(-200 * 24 * time.Hour)

	for _, tid := range []uuid.UUID{tenantA, tenantB} {
		id := uuid.New()
		_, err := pool.Exec(ctx, `
			INSERT INTO alerts (id, tenant_id, type, severity, source, title, fired_at)
			VALUES ($1, $2, 'old.event', 'info', 'system', 'Old Alert', $3)
		`, id, tid, oldFiredAt)
		if err != nil {
			t.Fatalf("seed old alert for tenant %s: %v", tid, err)
		}
	}

	cutoff := time.Now().UTC().Add(-180 * 24 * time.Hour)
	deleted, err := as_.DeleteOlderThanForTenant(ctx, tenantA, cutoff)
	if err != nil {
		t.Fatalf("DeleteOlderThanForTenant: %v", err)
	}
	if deleted < 1 {
		t.Errorf("deleted: got %d, want >= 1", deleted)
	}

	var countB int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM alerts WHERE tenant_id=$1 AND type='old.event'`, tenantB).Scan(&countB); err != nil {
		t.Fatalf("count tenantB: %v", err)
	}
	if countB != 1 {
		t.Errorf("tenantB row should still exist, got count=%d", countB)
	}
}

func TestAlertStore_ListSimilar_DedupKeyMatchAndTypeSourceFallback(t *testing.T) {
	pool := testSuppressionPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert suppression store test")
	}
	tenantID := seedSuppressionTenant(t, pool)
	as_ := NewAlertStore(pool)
	ctx := context.Background()

	dk := "dk-similar-" + uuid.New().String()
	anchor, err := as_.Create(ctx, CreateAlertParams{
		TenantID:    tenantID,
		Type:        "sim.data_spike",
		Severity:    "high",
		Source:      "sim",
		Title:       "Anchor Alert",
		Description: "anchor",
		DedupKey:    &dk,
	})
	if err != nil {
		t.Fatalf("create anchor: %v", err)
	}

	sibling, err := as_.Create(ctx, CreateAlertParams{
		TenantID:    tenantID,
		Type:        "sim.data_spike",
		Severity:    "medium",
		Source:      "sim",
		Title:       "Sibling Alert",
		Description: "sibling",
		DedupKey:    &dk,
	})
	if err != nil {
		t.Fatalf("create sibling: %v", err)
	}

	results, strategy, err := as_.ListSimilar(ctx, tenantID, anchor, 10)
	if err != nil {
		t.Fatalf("ListSimilar dedup_key: %v", err)
	}
	if strategy != "dedup_key" {
		t.Errorf("strategy: got %q, want dedup_key", strategy)
	}
	found := false
	for _, r := range results {
		if r.ID == anchor.ID {
			t.Error("anchor should not appear in results")
		}
		if r.ID == sibling.ID {
			found = true
		}
	}
	if !found {
		t.Error("sibling not found in dedup_key results")
	}

	anchNoDedup, err := as_.Create(ctx, CreateAlertParams{
		TenantID:    tenantID,
		Type:        "operator.down",
		Severity:    "critical",
		Source:      "operator",
		Title:       "Anchor No Dedup",
		Description: "no dedup anchor",
	})
	if err != nil {
		t.Fatalf("create anchor no dedup: %v", err)
	}

	sameTypeSrc, err := as_.Create(ctx, CreateAlertParams{
		TenantID:    tenantID,
		Type:        "operator.down",
		Severity:    "high",
		Source:      "operator",
		Title:       "Same Type Source",
		Description: "same type and source",
	})
	if err != nil {
		t.Fatalf("create same type source: %v", err)
	}

	results2, strategy2, err := as_.ListSimilar(ctx, tenantID, anchNoDedup, 10)
	if err != nil {
		t.Fatalf("ListSimilar type_source: %v", err)
	}
	if strategy2 != "type_source" {
		t.Errorf("strategy: got %q, want type_source", strategy2)
	}
	found2 := false
	for _, r := range results2 {
		if r.ID == anchNoDedup.ID {
			t.Error("anchor should not appear in type_source results")
		}
		if r.ID == sameTypeSrc.ID {
			found2 = true
		}
	}
	if !found2 {
		t.Error("same-type-source alert not found in type_source results")
	}
}

// FIX-229 Gate F-B1: round out the MatchActive scope branch matrix (this /
// operator / dedup_key). The pre-existing TypeScope test covered only one of
// the four scope branches; these three siblings extend coverage to the rest.
func TestAlertSuppressionStore_MatchActive_ThisScope(t *testing.T) {
	pool := testSuppressionPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert suppression store test")
	}
	tenantID := seedSuppressionTenant(t, pool)
	ss := NewAlertSuppressionStore(pool)
	ctx := context.Background()

	alertID := uuid.New()
	p := makeSuppressionParams(tenantID, "this", alertID.String(), 1*time.Hour)
	created, err := ss.Create(ctx, p)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	probe := AlertMatchProbe{AlertID: alertID, Type: "any.type"}
	match, err := ss.MatchActive(ctx, tenantID, probe)
	if err != nil {
		t.Fatalf("MatchActive: %v", err)
	}
	if match == nil {
		t.Fatal("MatchActive: expected match for matching alert id, got nil")
	}
	if match.ID != created.ID {
		t.Errorf("MatchActive ID: got %s, want %s", match.ID, created.ID)
	}

	otherProbe := AlertMatchProbe{AlertID: uuid.New(), Type: "any.type"}
	noMatch, err := ss.MatchActive(ctx, tenantID, otherProbe)
	if err != nil {
		t.Fatalf("MatchActive other id: %v", err)
	}
	if noMatch != nil {
		t.Error("MatchActive should return nil for different alert id under 'this' scope")
	}
}

func TestAlertSuppressionStore_MatchActive_OperatorScope(t *testing.T) {
	pool := testSuppressionPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert suppression store test")
	}
	tenantID := seedSuppressionTenant(t, pool)
	ss := NewAlertSuppressionStore(pool)
	ctx := context.Background()

	operatorID := uuid.New()
	p := makeSuppressionParams(tenantID, "operator", operatorID.String(), 1*time.Hour)
	created, err := ss.Create(ctx, p)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	probe := AlertMatchProbe{
		AlertID:    uuid.New(),
		Type:       "operator.down",
		OperatorID: &operatorID,
	}
	match, err := ss.MatchActive(ctx, tenantID, probe)
	if err != nil {
		t.Fatalf("MatchActive: %v", err)
	}
	if match == nil {
		t.Fatal("MatchActive: expected match for matching operator id, got nil")
	}
	if match.ID != created.ID {
		t.Errorf("MatchActive ID: got %s, want %s", match.ID, created.ID)
	}

	otherOp := uuid.New()
	otherProbe := AlertMatchProbe{
		AlertID:    uuid.New(),
		Type:       "operator.down",
		OperatorID: &otherOp,
	}
	noMatch, err := ss.MatchActive(ctx, tenantID, otherProbe)
	if err != nil {
		t.Fatalf("MatchActive other operator: %v", err)
	}
	if noMatch != nil {
		t.Error("MatchActive should return nil for different operator id under 'operator' scope")
	}
}

func TestAlertSuppressionStore_MatchActive_DedupKeyScope(t *testing.T) {
	pool := testSuppressionPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert suppression store test")
	}
	tenantID := seedSuppressionTenant(t, pool)
	ss := NewAlertSuppressionStore(pool)
	ctx := context.Background()

	dedupKey := "dedup-test-" + uuid.New().String()[:8]
	p := makeSuppressionParams(tenantID, "dedup_key", dedupKey, 1*time.Hour)
	created, err := ss.Create(ctx, p)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	probe := AlertMatchProbe{
		AlertID:  uuid.New(),
		Type:     "any.type",
		DedupKey: &dedupKey,
	}
	match, err := ss.MatchActive(ctx, tenantID, probe)
	if err != nil {
		t.Fatalf("MatchActive: %v", err)
	}
	if match == nil {
		t.Fatal("MatchActive: expected match for matching dedup_key, got nil")
	}
	if match.ID != created.ID {
		t.Errorf("MatchActive ID: got %s, want %s", match.ID, created.ID)
	}

	otherDK := "dedup-other-" + uuid.New().String()[:8]
	otherProbe := AlertMatchProbe{
		AlertID:  uuid.New(),
		Type:     "any.type",
		DedupKey: &otherDK,
	}
	noMatch, err := ss.MatchActive(ctx, tenantID, otherProbe)
	if err != nil {
		t.Fatalf("MatchActive other dedup_key: %v", err)
	}
	if noMatch != nil {
		t.Error("MatchActive should return nil for different dedup_key under 'dedup_key' scope")
	}
}

func TestAlertSuppressionStore_MatchActive_ExpiredNotReturned(t *testing.T) {
	pool := testSuppressionPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert suppression store test")
	}
	tenantID := seedSuppressionTenant(t, pool)
	ss := NewAlertSuppressionStore(pool)
	ctx := context.Background()

	alertType := "sim.data_spike"
	p := makeSuppressionParams(tenantID, "type", alertType, -1*time.Hour)

	if _, err := ss.Create(ctx, p); err != nil {
		t.Fatalf("Create expired suppression: %v", err)
	}

	probe := AlertMatchProbe{
		AlertID: uuid.New(),
		Type:    alertType,
	}
	match, err := ss.MatchActive(ctx, tenantID, probe)
	if err != nil {
		t.Fatalf("MatchActive: %v", err)
	}
	if match != nil {
		t.Error("MatchActive should return nil for expired suppression")
	}
}

func TestAlertSuppressionStore_Delete_NotFound(t *testing.T) {
	pool := testSuppressionPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated alert suppression store test")
	}
	tenantID := seedSuppressionTenant(t, pool)
	ss := NewAlertSuppressionStore(pool)
	ctx := context.Background()

	err := ss.Delete(ctx, tenantID, uuid.New())
	if !errors.Is(err, ErrAlertSuppressionNotFound) {
		t.Errorf("expected ErrAlertSuppressionNotFound, got: %v", err)
	}
}

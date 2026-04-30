//go:build integration

package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/btopcu/argus/internal/apierr"
)

func testOnboardingPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("cannot connect to postgres: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("postgres ping failed: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func onboardingTenantCtx(tenantID uuid.UUID) context.Context {
	return context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)
}

func TestOnboardingSessionStore_Create(t *testing.T) {
	pool := testOnboardingPool(t)
	s := NewOnboardingSessionStore(pool)

	tenantID := uuid.New()
	userID := uuid.New()

	sess, err := s.Create(context.Background(), tenantID, userID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sess.ID == uuid.Nil {
		t.Fatal("expected non-nil session ID")
	}
	if sess.CurrentStep != 1 {
		t.Errorf("CurrentStep = %d, want 1", sess.CurrentStep)
	}
	if sess.State != "in_progress" {
		t.Errorf("State = %q, want in_progress", sess.State)
	}
	if sess.TenantID != tenantID {
		t.Errorf("TenantID mismatch")
	}
}

func TestOnboardingSessionStore_GetByID(t *testing.T) {
	pool := testOnboardingPool(t)
	s := NewOnboardingSessionStore(pool)

	tenantID := uuid.New()
	userID := uuid.New()

	created, err := s.Create(context.Background(), tenantID, userID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	ctx := onboardingTenantCtx(tenantID)
	got, err := s.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID mismatch: got %s, want %s", got.ID, created.ID)
	}
}

func TestOnboardingSessionStore_GetByID_NotFound(t *testing.T) {
	pool := testOnboardingPool(t)
	s := NewOnboardingSessionStore(pool)

	tenantID := uuid.New()
	ctx := onboardingTenantCtx(tenantID)

	_, err := s.GetByID(ctx, uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestOnboardingSessionStore_UpdateStep(t *testing.T) {
	pool := testOnboardingPool(t)
	s := NewOnboardingSessionStore(pool)

	tenantID := uuid.New()
	userID := uuid.New()

	created, err := s.Create(context.Background(), tenantID, userID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	payload := json.RawMessage(`{"apn":"internet","network_type":"LTE"}`)
	if err := s.UpdateStep(context.Background(), created.ID, 2, payload, 3); err != nil {
		t.Fatalf("UpdateStep: %v", err)
	}

	ctx := onboardingTenantCtx(tenantID)
	got, err := s.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID after UpdateStep: %v", err)
	}
	if got.CurrentStep != 3 {
		t.Errorf("CurrentStep = %d, want 3", got.CurrentStep)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(got.StepData[1], &m); err != nil {
		t.Fatalf("unmarshal step_2_data: %v", err)
	}
	if m["apn"] != "internet" {
		t.Errorf("step_2_data.apn = %v, want internet", m["apn"])
	}
}

func TestOnboardingSessionStore_UpdateStep_InvalidStepN_Integration(t *testing.T) {
	pool := testOnboardingPool(t)
	s := NewOnboardingSessionStore(pool)

	id := uuid.New()
	for _, bad := range []int{0, 6} {
		err := s.UpdateStep(context.Background(), id, bad, []byte(`{}`), 1)
		if err == nil {
			t.Errorf("stepN=%d: expected error, got nil", bad)
		}
	}
}

func TestOnboardingSessionStore_MarkCompleted(t *testing.T) {
	pool := testOnboardingPool(t)
	s := NewOnboardingSessionStore(pool)

	tenantID := uuid.New()
	userID := uuid.New()

	created, err := s.Create(context.Background(), tenantID, userID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := s.MarkCompleted(context.Background(), created.ID); err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}

	ctx := onboardingTenantCtx(tenantID)
	got, err := s.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID after MarkCompleted: %v", err)
	}
	if got.State != "completed" {
		t.Errorf("State = %q, want completed", got.State)
	}
	if got.CompletedAt == nil {
		t.Error("CompletedAt should not be nil after MarkCompleted")
	}
	if got.CurrentStep != 6 {
		t.Errorf("CurrentStep = %d, want 6", got.CurrentStep)
	}
}

func TestOnboardingSessionStore_TenantIsolation(t *testing.T) {
	pool := testOnboardingPool(t)
	s := NewOnboardingSessionStore(pool)

	tenantA := uuid.New()
	tenantB := uuid.New()
	userID := uuid.New()

	created, err := s.Create(context.Background(), tenantA, userID)
	if err != nil {
		t.Fatalf("Create with tenantA: %v", err)
	}

	ctxB := onboardingTenantCtx(tenantB)
	_, err = s.GetByID(ctxB, created.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("tenant B should not see tenant A's session; got err=%v", err)
	}
}

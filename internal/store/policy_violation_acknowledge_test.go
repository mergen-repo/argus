package store

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

func newTestPolicyViolationStore(t *testing.T) *PolicyViolationStore {
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
	return NewPolicyViolationStore(pool, zerolog.Nop())
}

func TestPolicyViolationStore_Acknowledge_Happy(t *testing.T) {
	s := newTestPolicyViolationStore(t)
	ctx := context.Background()

	tenantID := uuid.New()
	userID := uuid.New()

	v, err := s.Create(ctx, CreateViolationParams{
		TenantID:      tenantID,
		SimID:         uuid.New(),
		PolicyID:      uuid.New(),
		VersionID:     uuid.New(),
		RuleIndex:     0,
		ViolationType: "rate_limit",
		ActionTaken:   "block",
		Severity:      "warning",
	})
	if err != nil {
		t.Fatalf("Create violation: %v", err)
	}

	acked, err := s.Acknowledge(ctx, v.ID, tenantID, userID, "test note")
	if err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}

	if acked.AcknowledgedAt == nil {
		t.Error("AcknowledgedAt should be set")
	}
	if acked.AcknowledgedBy == nil || *acked.AcknowledgedBy != userID {
		t.Errorf("AcknowledgedBy = %v, want %v", acked.AcknowledgedBy, userID)
	}
	if acked.AcknowledgmentNote == nil || *acked.AcknowledgmentNote != "test note" {
		t.Errorf("AcknowledgmentNote = %v, want 'test note'", acked.AcknowledgmentNote)
	}
}

func TestPolicyViolationStore_Acknowledge_AlreadyAcknowledged(t *testing.T) {
	s := newTestPolicyViolationStore(t)
	ctx := context.Background()

	tenantID := uuid.New()
	userID := uuid.New()

	v, err := s.Create(ctx, CreateViolationParams{
		TenantID:      tenantID,
		SimID:         uuid.New(),
		PolicyID:      uuid.New(),
		VersionID:     uuid.New(),
		RuleIndex:     0,
		ViolationType: "rate_limit",
		ActionTaken:   "block",
		Severity:      "warning",
	})
	if err != nil {
		t.Fatalf("Create violation: %v", err)
	}

	if _, err := s.Acknowledge(ctx, v.ID, tenantID, userID, "first"); err != nil {
		t.Fatalf("First Acknowledge: %v", err)
	}

	_, err = s.Acknowledge(ctx, v.ID, tenantID, userID, "second")
	if !errors.Is(err, ErrAlreadyAcknowledged) {
		t.Errorf("expected ErrAlreadyAcknowledged, got %v", err)
	}
}

func TestPolicyViolationStore_Acknowledge_WrongTenant(t *testing.T) {
	s := newTestPolicyViolationStore(t)
	ctx := context.Background()

	ownerTenant := uuid.New()
	wrongTenant := uuid.New()
	userID := uuid.New()

	v, err := s.Create(ctx, CreateViolationParams{
		TenantID:      ownerTenant,
		SimID:         uuid.New(),
		PolicyID:      uuid.New(),
		VersionID:     uuid.New(),
		ViolationType: "rate_limit",
		ActionTaken:   "block",
		Severity:      "info",
	})
	if err != nil {
		t.Fatalf("Create violation: %v", err)
	}

	_, err = s.Acknowledge(ctx, v.ID, wrongTenant, userID, "")
	if err == nil {
		t.Error("expected error for wrong tenant, got nil")
	}
	if errors.Is(err, ErrAlreadyAcknowledged) {
		t.Error("expected 'not found' error, got ErrAlreadyAcknowledged")
	}
}

func TestPolicyViolationStore_List_AcknowledgedFilter(t *testing.T) {
	s := newTestPolicyViolationStore(t)
	ctx := context.Background()

	tenantID := uuid.New()
	userID := uuid.New()

	for i := 0; i < 4; i++ {
		v, err := s.Create(ctx, CreateViolationParams{
			TenantID:      tenantID,
			SimID:         uuid.New(),
			PolicyID:      uuid.New(),
			VersionID:     uuid.New(),
			ViolationType: "rate_limit",
			ActionTaken:   "block",
			Severity:      "info",
		})
		if err != nil {
			t.Fatalf("Create violation %d: %v", i, err)
		}
		if i < 2 {
			if _, err := s.Acknowledge(ctx, v.ID, tenantID, userID, ""); err != nil {
				t.Fatalf("Acknowledge %d: %v", i, err)
			}
		}
	}

	ackTrue := true
	ackedList, _, err := s.List(ctx, tenantID, ListViolationsParams{Limit: 50, Acknowledged: &ackTrue})
	if err != nil {
		t.Fatalf("List acknowledged: %v", err)
	}
	if len(ackedList) < 2 {
		t.Errorf("expected >= 2 acknowledged, got %d", len(ackedList))
	}

	ackFalse := false
	unackedList, _, err := s.List(ctx, tenantID, ListViolationsParams{Limit: 50, Acknowledged: &ackFalse})
	if err != nil {
		t.Fatalf("List unacknowledged: %v", err)
	}
	if len(unackedList) < 2 {
		t.Errorf("expected >= 2 unacknowledged, got %d", len(unackedList))
	}

	for _, v := range ackedList {
		if v.AcknowledgedAt == nil {
			t.Error("acknowledged list contains unacknowledged violation")
		}
	}
	for _, v := range unackedList {
		if v.AcknowledgedAt != nil {
			t.Error("unacknowledged list contains acknowledged violation")
		}
	}
}

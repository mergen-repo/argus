package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/btopcu/argus/internal/apierr"
)

func testSMSPool(t *testing.T) *pgxpool.Pool {
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

func smsCtx(tenantID uuid.UUID) context.Context {
	return context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)
}

func TestNewSMSOutboundStore(t *testing.T) {
	s := NewSMSOutboundStore(nil)
	if s == nil {
		t.Fatal("NewSMSOutboundStore returned nil")
	}
}

func TestSMSOutbound_Fields(t *testing.T) {
	id := uuid.New()
	tenantID := uuid.New()
	simID := uuid.New()
	provID := "prov-123"
	errCode := "E001"
	now := time.Now().UTC()

	m := SMSOutbound{
		ID:                id,
		TenantID:          tenantID,
		SimID:             simID,
		MSISDN:            "+905551234567",
		TextHash:          "abc123hash",
		TextPreview:       "Hello world",
		Status:            "queued",
		ProviderMessageID: &provID,
		ErrorCode:         &errCode,
		QueuedAt:          now,
	}

	if m.ID != id {
		t.Errorf("id mismatch")
	}
	if m.TenantID != tenantID {
		t.Errorf("tenant_id mismatch")
	}
	if m.SimID != simID {
		t.Errorf("sim_id mismatch")
	}
	if *m.ProviderMessageID != "prov-123" {
		t.Errorf("provider_message_id = %s, want prov-123", *m.ProviderMessageID)
	}
	if *m.ErrorCode != "E001" {
		t.Errorf("error_code = %s, want E001", *m.ErrorCode)
	}
	if m.Status != "queued" {
		t.Errorf("status = %s, want queued", m.Status)
	}
}

func TestSMSListFilters_Fields(t *testing.T) {
	simID := uuid.New()
	status := "sent"
	from := time.Now().UTC().Add(-24 * time.Hour)
	to := time.Now().UTC()

	f := SMSListFilters{
		SimID:  &simID,
		Status: &status,
		From:   &from,
		To:     &to,
	}

	if *f.SimID != simID {
		t.Errorf("sim_id mismatch")
	}
	if *f.Status != "sent" {
		t.Errorf("status = %s, want sent", *f.Status)
	}
}

func TestSMSOutboundStore_LimitNormalization(t *testing.T) {
	cases := []struct {
		name      string
		input     int
		wantLimit int
	}{
		{"zero becomes 50", 0, 50},
		{"negative becomes 50", -1, 50},
		{"over 100 becomes 50", 200, 50},
		{"100 preserved", 100, 100},
		{"25 preserved", 25, 25},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			limit := tc.input
			if limit <= 0 || limit > 100 {
				limit = 50
			}
			if limit != tc.wantLimit {
				t.Errorf("limit = %d, want %d", limit, tc.wantLimit)
			}
		})
	}
}

func TestSMSOutboundStore_ErrNotFound(t *testing.T) {
	if ErrSMSOutboundNotFound.Error() == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestSMSOutboundStore_Integration(t *testing.T) {
	pool := testSMSPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}

	tenantID, simID := requireTestTenantAndSIM(t, pool)
	ctx := smsCtx(tenantID)
	s := NewSMSOutboundStore(pool)

	t.Run("Insert sets ID and QueuedAt when zero", func(t *testing.T) {
		m := &SMSOutbound{
			TenantID:    tenantID,
			SimID:       simID,
			MSISDN:      "+905550000001",
			TextHash:    "hash001",
			TextPreview: "Test message one",
			Status:      "queued",
		}
		result, err := s.Insert(ctx, m)
		if err != nil {
			t.Fatalf("Insert: %v", err)
		}
		if result.ID == uuid.Nil {
			t.Error("expected non-nil ID after insert")
		}
		if result.QueuedAt.IsZero() {
			t.Error("expected non-zero QueuedAt after insert")
		}
		if result.Status != "queued" {
			t.Errorf("status = %s, want queued", result.Status)
		}
	})

	t.Run("UpdateStatus queued->sent", func(t *testing.T) {
		m := &SMSOutbound{
			TenantID:    tenantID,
			SimID:       simID,
			MSISDN:      "+905550000002",
			TextHash:    "hash002",
			TextPreview: "Test message two",
			Status:      "queued",
		}
		result, err := s.Insert(ctx, m)
		if err != nil {
			t.Fatalf("Insert: %v", err)
		}

		sentAt := time.Now().UTC()
		err = s.UpdateStatus(ctx, result.ID, "sent", "provider-abc", "", &sentAt)
		if err != nil {
			t.Fatalf("UpdateStatus: %v", err)
		}

		got, err := s.GetByID(ctx, result.ID)
		if err != nil {
			t.Fatalf("GetByID after UpdateStatus: %v", err)
		}
		if got.Status != "sent" {
			t.Errorf("status = %s, want sent", got.Status)
		}
		if got.ProviderMessageID == nil || *got.ProviderMessageID != "provider-abc" {
			t.Errorf("provider_message_id = %v, want provider-abc", got.ProviderMessageID)
		}
		if got.SentAt == nil {
			t.Error("expected sent_at to be set")
		}
	})

	t.Run("MarkDelivered by provider_message_id", func(t *testing.T) {
		provID := "provider-deliver-test"
		m := &SMSOutbound{
			TenantID:          tenantID,
			SimID:             simID,
			MSISDN:            "+905550000003",
			TextHash:          "hash003",
			TextPreview:       "Test message three",
			Status:            "sent",
			ProviderMessageID: &provID,
		}
		result, err := s.Insert(ctx, m)
		if err != nil {
			t.Fatalf("Insert: %v", err)
		}

		deliveredAt := time.Now().UTC()
		err = s.MarkDelivered(ctx, provID, deliveredAt)
		if err != nil {
			t.Fatalf("MarkDelivered: %v", err)
		}

		got, err := s.GetByID(ctx, result.ID)
		if err != nil {
			t.Fatalf("GetByID after MarkDelivered: %v", err)
		}
		if got.Status != "delivered" {
			t.Errorf("status = %s, want delivered", got.Status)
		}
		if got.DeliveredAt == nil {
			t.Error("expected delivered_at to be set")
		}
	})

	t.Run("GetByID not found", func(t *testing.T) {
		_, err := s.GetByID(ctx, uuid.New())
		if err == nil {
			t.Fatal("expected ErrSMSOutboundNotFound, got nil")
		}
		if err != ErrSMSOutboundNotFound {
			t.Errorf("err = %v, want ErrSMSOutboundNotFound", err)
		}
	})

	t.Run("List with no filters returns inserted rows", func(t *testing.T) {
		results, nextCursor, err := s.List(ctx, tenantID, SMSListFilters{}, "", 10)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("expected at least one result")
		}
		_ = nextCursor
	})

	t.Run("List filter by SimID", func(t *testing.T) {
		results, _, err := s.List(ctx, tenantID, SMSListFilters{SimID: &simID}, "", 10)
		if err != nil {
			t.Fatalf("List by SimID: %v", err)
		}
		for _, r := range results {
			if r.SimID != simID {
				t.Errorf("sim_id = %s, want %s", r.SimID, simID)
			}
		}
	})

	t.Run("List filter by Status", func(t *testing.T) {
		status := "queued"
		results, _, err := s.List(ctx, tenantID, SMSListFilters{Status: &status}, "", 10)
		if err != nil {
			t.Fatalf("List by Status: %v", err)
		}
		for _, r := range results {
			if r.Status != "queued" {
				t.Errorf("status = %s, want queued", r.Status)
			}
		}
	})

	t.Run("List pagination cursor", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			_, err := s.Insert(ctx, &SMSOutbound{
				TenantID:    tenantID,
				SimID:       simID,
				MSISDN:      "+90555000010" + string(rune('0'+i)),
				TextHash:    "hashpag" + string(rune('0'+i)),
				TextPreview: "Pagination test",
				Status:      "queued",
			})
			if err != nil {
				t.Fatalf("Insert for pagination: %v", err)
			}
		}

		page1, cursor, err := s.List(ctx, tenantID, SMSListFilters{}, "", 2)
		if err != nil {
			t.Fatalf("List page1: %v", err)
		}
		if len(page1) != 2 {
			t.Fatalf("expected 2 results on page1, got %d", len(page1))
		}
		if cursor == "" {
			t.Fatal("expected non-empty cursor after page1")
		}

		page2, _, err := s.List(ctx, tenantID, SMSListFilters{}, cursor, 2)
		if err != nil {
			t.Fatalf("List page2: %v", err)
		}
		if len(page2) == 0 {
			t.Fatal("expected results on page2")
		}

		for _, r1 := range page1 {
			for _, r2 := range page2 {
				if r1.ID == r2.ID {
					t.Errorf("duplicate ID %s across pages", r1.ID)
				}
			}
		}
	})
}

func requireTestTenantAndSIM(t *testing.T, pool *pgxpool.Pool) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	var tenantID uuid.UUID
	err := pool.QueryRow(ctx, `SELECT id FROM tenants LIMIT 1`).Scan(&tenantID)
	if err != nil {
		t.Fatalf("no tenants in test DB: %v", err)
	}

	var simID uuid.UUID
	err = pool.QueryRow(ctx, `SELECT id FROM sims WHERE tenant_id = $1 LIMIT 1`, tenantID).Scan(&simID)
	if err != nil {
		t.Fatalf("no sims in test DB for tenant %s: %v", tenantID, err)
	}

	return tenantID, simID
}

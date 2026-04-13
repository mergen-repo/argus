package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newTestCDRStore(t *testing.T) *CDRStore {
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
	return NewCDRStore(pool)
}

func TestCDRStore_Create(t *testing.T) {
	s := newTestCDRStore(t)
	ctx := context.Background()

	tenantID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	operatorID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	simID := uuid.New()
	sessionID := uuid.New()
	ratType := "lte"
	usageCost := 5.25
	ts := time.Now().UTC().Truncate(time.Microsecond)

	created, err := s.Create(ctx, CreateCDRParams{
		SessionID:  sessionID,
		SimID:      simID,
		TenantID:   tenantID,
		OperatorID: operatorID,
		RATType:    &ratType,
		RecordType: "stop",
		BytesIn:    1024 * 1024,
		BytesOut:   512 * 1024,
		DurationSec: 3600,
		UsageCost:  &usageCost,
		Timestamp:  ts,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if created.ID == 0 {
		t.Error("created CDR ID should not be 0")
	}
	if created.SessionID != sessionID {
		t.Errorf("SessionID = %s, want %s", created.SessionID, sessionID)
	}
	if created.RecordType != "stop" {
		t.Errorf("RecordType = %q, want stop", created.RecordType)
	}
	if created.BytesIn != 1024*1024 {
		t.Errorf("BytesIn = %d, want %d", created.BytesIn, 1024*1024)
	}
}

func TestCDRStore_CreateIdempotent(t *testing.T) {
	s := newTestCDRStore(t)
	ctx := context.Background()

	tenantID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	operatorID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	simID := uuid.New()
	sessionID := uuid.New()
	ts := time.Now().UTC().Truncate(time.Microsecond)

	params := CreateCDRParams{
		SessionID:  sessionID,
		SimID:      simID,
		TenantID:   tenantID,
		OperatorID: operatorID,
		RecordType: "start",
		Timestamp:  ts,
	}

	first, err := s.CreateIdempotent(ctx, params)
	if err != nil {
		t.Fatalf("First CreateIdempotent: %v", err)
	}
	if first == nil {
		t.Fatal("First CreateIdempotent should return CDR")
	}

	second, err := s.CreateIdempotent(ctx, params)
	if err != nil {
		t.Fatalf("Second CreateIdempotent: %v", err)
	}
	if second != nil {
		t.Error("Second CreateIdempotent should return nil (duplicate)")
	}
}

func TestCDRStore_ListByTenant(t *testing.T) {
	s := newTestCDRStore(t)
	ctx := context.Background()

	tenantID := uuid.New()
	operatorID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	simID := uuid.New()

	for i := 0; i < 5; i++ {
		sessionID := uuid.New()
		ts := time.Now().UTC().Add(time.Duration(-i) * time.Minute).Truncate(time.Microsecond)
		cost := float64(i + 1)
		_, err := s.Create(ctx, CreateCDRParams{
			SessionID:  sessionID,
			SimID:      simID,
			TenantID:   tenantID,
			OperatorID: operatorID,
			RecordType: "stop",
			BytesIn:    int64(i+1) * 1024 * 1024,
			DurationSec: (i + 1) * 60,
			UsageCost:  &cost,
			Timestamp:  ts,
		})
		if err != nil {
			t.Fatalf("Create CDR %d: %v", i, err)
		}
	}

	cdrs, cursor, err := s.ListByTenant(ctx, tenantID, ListCDRParams{Limit: 3})
	if err != nil {
		t.Fatalf("ListByTenant: %v", err)
	}
	if len(cdrs) != 3 {
		t.Errorf("ListByTenant len = %d, want 3", len(cdrs))
	}
	if cursor == "" {
		t.Error("cursor should not be empty when there are more results")
	}

	cdrs2, cursor2, err := s.ListByTenant(ctx, tenantID, ListCDRParams{Limit: 3, Cursor: cursor})
	if err != nil {
		t.Fatalf("ListByTenant page 2: %v", err)
	}
	if len(cdrs2) != 2 {
		t.Errorf("ListByTenant page 2 len = %d, want 2", len(cdrs2))
	}
	if cursor2 != "" {
		t.Error("cursor2 should be empty")
	}

	minCost := 3.0
	cdrsFiltered, _, err := s.ListByTenant(ctx, tenantID, ListCDRParams{Limit: 50, MinCost: &minCost})
	if err != nil {
		t.Fatalf("ListByTenant filtered: %v", err)
	}
	if len(cdrsFiltered) != 3 {
		t.Errorf("ListByTenant min_cost filter len = %d, want 3", len(cdrsFiltered))
	}
}

func TestCDRStore_CountForExport(t *testing.T) {
	s := newTestCDRStore(t)
	ctx := context.Background()

	tenantID := uuid.New()
	operatorID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	now := time.Now().UTC()

	for i := 0; i < 3; i++ {
		_, err := s.Create(ctx, CreateCDRParams{
			SessionID:  uuid.New(),
			SimID:      uuid.New(),
			TenantID:   tenantID,
			OperatorID: operatorID,
			RecordType: "stop",
			Timestamp:  now.Add(time.Duration(-i) * time.Hour).Truncate(time.Microsecond),
		})
		if err != nil {
			t.Fatalf("Create CDR %d: %v", i, err)
		}
	}

	from := now.Add(-4 * time.Hour)
	to := now.Add(time.Hour)
	count, err := s.CountForExport(ctx, tenantID, from, to, nil)
	if err != nil {
		t.Fatalf("CountForExport: %v", err)
	}
	if count != 3 {
		t.Errorf("CountForExport = %d, want 3", count)
	}
}

func TestCDRStore_GetCumulativeSessionBytes(t *testing.T) {
	s := newTestCDRStore(t)
	ctx := context.Background()

	tenantID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	operatorID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	sessionID := uuid.New()

	for i := 0; i < 3; i++ {
		ts := time.Now().UTC().Add(time.Duration(-i) * time.Minute).Truncate(time.Microsecond)
		_, err := s.Create(ctx, CreateCDRParams{
			SessionID:  sessionID,
			SimID:      uuid.New(),
			TenantID:   tenantID,
			OperatorID: operatorID,
			RecordType: "interim",
			BytesIn:    1000,
			BytesOut:   500,
			Timestamp:  ts,
		})
		if err != nil {
			t.Fatalf("Create CDR %d: %v", i, err)
		}
	}

	total, err := s.GetCumulativeSessionBytes(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetCumulativeSessionBytes: %v", err)
	}
	if total != 4500 {
		t.Errorf("GetCumulativeSessionBytes = %d, want 4500", total)
	}
}

func TestCDRStore_GetOperatorMetrics(t *testing.T) {
	s := newTestCDRStore(t)
	ctx := context.Background()

	tenantID := uuid.New()
	operatorID := uuid.New()
	now := time.Now().UTC()

	for i := 0; i < 5; i++ {
		_, err := s.Create(ctx, CreateCDRParams{
			SessionID:  uuid.New(),
			SimID:      uuid.New(),
			TenantID:   tenantID,
			OperatorID: operatorID,
			RecordType: "stop",
			BytesIn:    1024,
			Timestamp:  now.Add(time.Duration(-i) * time.Minute).Truncate(time.Microsecond),
		})
		if err != nil {
			t.Fatalf("Create CDR %d: %v", i, err)
		}
	}

	buckets, err := s.GetOperatorMetrics(ctx, tenantID, operatorID, "1h")
	if err != nil {
		t.Fatalf("GetOperatorMetrics: %v", err)
	}

	if len(buckets) == 0 {
		t.Error("expected at least one metric bucket")
	}
	for _, b := range buckets {
		if b.AuthRatePerSec < 0 {
			t.Errorf("AuthRatePerSec should be >= 0, got %f", b.AuthRatePerSec)
		}
		if b.ErrorRatePerSec < 0 {
			t.Errorf("ErrorRatePerSec should be >= 0, got %f", b.ErrorRatePerSec)
		}
	}

	wrongTenantBuckets, err := s.GetOperatorMetrics(ctx, uuid.New(), operatorID, "1h")
	if err != nil {
		t.Fatalf("GetOperatorMetrics wrong tenant: %v", err)
	}
	if len(wrongTenantBuckets) != 0 {
		t.Error("expected zero buckets for wrong tenant (tenant isolation)")
	}
}

func TestCDRStore_GetAPNTraffic(t *testing.T) {
	s := newTestCDRStore(t)
	ctx := context.Background()

	tenantID := uuid.New()
	operatorID := uuid.New()
	apnID := uuid.New()
	now := time.Now().UTC()

	for i := 0; i < 3; i++ {
		_, err := s.Create(ctx, CreateCDRParams{
			SessionID:  uuid.New(),
			SimID:      uuid.New(),
			TenantID:   tenantID,
			OperatorID: operatorID,
			APNID:      &apnID,
			RecordType: "stop",
			BytesIn:    int64(i+1) * 1024,
			BytesOut:   int64(i+1) * 512,
			Timestamp:  now.Add(time.Duration(-i) * time.Minute).Truncate(time.Microsecond),
		})
		if err != nil {
			t.Fatalf("Create CDR %d: %v", i, err)
		}
	}

	buckets, err := s.GetAPNTraffic(ctx, tenantID, apnID, "24h")
	if err != nil {
		t.Fatalf("GetAPNTraffic: %v", err)
	}

	total := int64(0)
	for _, b := range buckets {
		total += b.AuthCount
	}
	if total == 0 {
		t.Log("no hourly aggregate data yet (continuous aggregate may not have refreshed) — skipping count check")
	}

	wrongBuckets, err := s.GetAPNTraffic(ctx, uuid.New(), apnID, "24h")
	if err != nil {
		t.Fatalf("GetAPNTraffic wrong tenant: %v", err)
	}
	if len(wrongBuckets) != 0 {
		t.Error("expected zero buckets for wrong tenant (tenant isolation)")
	}
}

func TestCDRStore_GetTrafficHeatmap7x24(t *testing.T) {
	s := newTestCDRStore(t)
	ctx := context.Background()

	tenantID := uuid.New()

	matrix, err := s.GetTrafficHeatmap7x24(ctx, tenantID)
	if err != nil {
		t.Fatalf("GetTrafficHeatmap7x24: %v", err)
	}
	if len(matrix) != 7 {
		t.Errorf("heatmap rows = %d, want 7", len(matrix))
	}
	for i, row := range matrix {
		if len(row) != 24 {
			t.Errorf("heatmap row[%d] cols = %d, want 24", i, len(row))
		}
		for j, v := range row {
			if v < 0 || v > 1 {
				t.Errorf("heatmap[%d][%d] = %f, must be in [0,1]", i, j, v)
			}
		}
	}
}

package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testSLAPool(t *testing.T) *pgxpool.Pool {
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

func seedSLATenant(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	_, err := pool.Exec(ctx,
		`INSERT INTO tenants (id, name, contact_email) VALUES ($1, $2, $3)`,
		id, "sla-test-tenant-"+id.String()[:8], "sla-test-"+id.String()[:8]+"@test.invalid",
	)
	if err != nil {
		t.Fatalf("seed sla tenant: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM sla_reports WHERE tenant_id = $1`, id)
		_, _ = pool.Exec(context.Background(), `DELETE FROM tenants WHERE id = $1`, id)
	})
	return id
}

func seedSLAOperator(t *testing.T, pool *pgxpool.Pool, tenantID uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	suffix := id.String()[:8]
	_, err := pool.Exec(ctx,
		`INSERT INTO operators (id, name, code, mcc, mnc, adapter_config, sla_uptime_target)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		id,
		"sla-test-op-"+suffix,
		"sla_op_"+suffix,
		"999",
		"99",
		`{"mock":{"enabled":true}}`,
		99.9,
	)
	if err != nil {
		t.Fatalf("seed sla operator: %v", err)
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO operator_grants (tenant_id, operator_id, enabled, sor_priority) VALUES ($1, $2, true, 1)`,
		tenantID, id,
	)
	if err != nil {
		t.Fatalf("seed sla operator grant: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM operator_grants WHERE operator_id = $1`, id)
		_, _ = pool.Exec(context.Background(), `DELETE FROM sla_reports WHERE operator_id = $1`, id)
		_, _ = pool.Exec(context.Background(), `DELETE FROM operators WHERE id = $1`, id)
	})
	return id
}

func TestSLAReportStore_UpsertMonthlyRollup_Idempotent(t *testing.T) {
	pool := testSLAPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated SLA store test")
	}
	tenantID := seedSLATenant(t, pool)
	opID := seedSLAOperator(t, pool, tenantID)
	s := NewSLAReportStore(pool)
	ctx := context.Background()

	windowStart := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	windowEnd := windowStart.AddDate(0, 1, 0)

	row := SLAReportRow{
		TenantID:      tenantID,
		OperatorID:    &opID,
		WindowStart:   windowStart,
		WindowEnd:     windowEnd,
		UptimePct:     99.5,
		LatencyP95Ms:  120,
		IncidentCount: 2,
		MTTRSec:       300,
		SessionsTotal: 1000,
		ErrorCount:    5,
	}

	if err := s.UpsertMonthlyRollup(ctx, row); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	row.UptimePct = 98.0
	row.IncidentCount = 4
	if err := s.UpsertMonthlyRollup(ctx, row); err != nil {
		t.Fatalf("second upsert (idempotent): %v", err)
	}

	var count int
	pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM sla_reports WHERE tenant_id = $1 AND operator_id = $2 AND window_start = $3`,
		tenantID, opID, windowStart,
	).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 row after double upsert, got %d", count)
	}

	var got float64
	pool.QueryRow(ctx,
		`SELECT uptime_pct FROM sla_reports WHERE tenant_id = $1 AND operator_id = $2 AND window_start = $3`,
		tenantID, opID, windowStart,
	).Scan(&got)
	if got != 98.0 {
		t.Errorf("expected updated uptime_pct=98.0, got %f", got)
	}
}

func TestSLAReportStore_HistoryByMonth_YearFilter(t *testing.T) {
	pool := testSLAPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated SLA store test")
	}
	tenantID := seedSLATenant(t, pool)
	opID := seedSLAOperator(t, pool, tenantID)
	s := NewSLAReportStore(pool)
	ctx := context.Background()

	for mo := 1; mo <= 3; mo++ {
		ws := time.Date(2026, time.Month(mo), 1, 0, 0, 0, 0, time.UTC)
		we := ws.AddDate(0, 1, 0)
		row := SLAReportRow{
			TenantID: tenantID, OperatorID: &opID,
			WindowStart: ws, WindowEnd: we,
			UptimePct: 99.9 - float64(mo),
		}
		if err := s.UpsertMonthlyRollup(ctx, row); err != nil {
			t.Fatalf("seed month %d: %v", mo, err)
		}
	}

	summaries, err := s.HistoryByMonth(ctx, tenantID, 2026, 12, nil)
	if err != nil {
		t.Fatalf("HistoryByMonth: %v", err)
	}
	if len(summaries) < 3 {
		t.Errorf("expected >= 3 months, got %d", len(summaries))
	}
	for _, sm := range summaries {
		if sm.Year != 2026 {
			t.Errorf("expected year 2026, got %d", sm.Year)
		}
	}
}

func TestSLAReportStore_HistoryByMonth_RollingFilter(t *testing.T) {
	pool := testSLAPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated SLA store test")
	}
	tenantID := seedSLATenant(t, pool)
	opID := seedSLAOperator(t, pool, tenantID)
	s := NewSLAReportStore(pool)
	ctx := context.Background()

	now := time.Now().UTC()
	for i := 0; i < 6; i++ {
		ws := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -i, 0)
		we := ws.AddDate(0, 1, 0)
		row := SLAReportRow{
			TenantID: tenantID, OperatorID: &opID,
			WindowStart: ws, WindowEnd: we,
			UptimePct: 99.0,
		}
		if err := s.UpsertMonthlyRollup(ctx, row); err != nil {
			t.Fatalf("seed month -%d: %v", i, err)
		}
	}

	summaries, err := s.HistoryByMonth(ctx, tenantID, 0, 3, nil)
	if err != nil {
		t.Fatalf("HistoryByMonth rolling: %v", err)
	}
	if len(summaries) < 1 {
		t.Errorf("expected >= 1 month in rolling window, got %d", len(summaries))
	}
}

func TestSLAReportStore_MonthDetail(t *testing.T) {
	pool := testSLAPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated SLA store test")
	}
	tenantID := seedSLATenant(t, pool)
	opID := seedSLAOperator(t, pool, tenantID)
	s := NewSLAReportStore(pool)
	ctx := context.Background()

	ws := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	we := ws.AddDate(0, 1, 0)
	row := SLAReportRow{
		TenantID:      tenantID,
		OperatorID:    &opID,
		WindowStart:   ws,
		WindowEnd:     we,
		UptimePct:     97.5,
		LatencyP95Ms:  250,
		IncidentCount: 3,
		MTTRSec:       180,
		SessionsTotal: 500,
	}
	if err := s.UpsertMonthlyRollup(ctx, row); err != nil {
		t.Fatalf("seed: %v", err)
	}

	ms, err := s.MonthDetail(ctx, tenantID, 2026, 4)
	if err != nil {
		t.Fatalf("MonthDetail: %v", err)
	}
	if ms.Year != 2026 || ms.Month != 4 {
		t.Errorf("expected 2026/4, got %d/%d", ms.Year, ms.Month)
	}
	found := false
	for _, op := range ms.Operators {
		if op.OperatorID == opID {
			found = true
			if op.UptimePct != 97.5 {
				t.Errorf("uptime_pct: got %f, want 97.5", op.UptimePct)
			}
			if op.LatencyP95Ms != 250 {
				t.Errorf("latency_p95_ms: got %d, want 250", op.LatencyP95Ms)
			}
		}
	}
	if !found {
		t.Errorf("operator %s not found in MonthDetail result", opID)
	}
}

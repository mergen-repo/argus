package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testBreachPool(t *testing.T) *pgxpool.Pool {
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

func seedBreachOperator(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	suffix := id.String()[:8]
	_, err := pool.Exec(ctx,
		`INSERT INTO operators (id, name, code, mcc, mnc, adapter_config)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		id,
		"breach-test-op-"+suffix,
		"breach_op_"+suffix,
		"998",
		"98",
		`{"mock":{"enabled":true}}`,
	)
	if err != nil {
		t.Fatalf("seed breach operator: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM operator_health_logs WHERE operator_id = $1`, id)
		_, _ = pool.Exec(context.Background(), `DELETE FROM operators WHERE id = $1`, id)
	})
	return id
}

func insertHealthLogs(t *testing.T, pool *pgxpool.Pool, opID uuid.UUID, logs []struct {
	at        time.Time
	status    string
	latencyMs *int
}) {
	t.Helper()
	ctx := context.Background()
	for _, l := range logs {
		_, err := pool.Exec(ctx,
			`INSERT INTO operator_health_logs (operator_id, checked_at, status, latency_ms, circuit_state)
			 VALUES ($1, $2, $3, $4, 'closed')`,
			opID, l.at, l.status, l.latencyMs,
		)
		if err != nil {
			t.Fatalf("insert health log at %v: %v", l.at, err)
		}
	}
}

func intPtr(v int) *int { return &v }

func TestOperatorBreachDetection_ShortDown_NoBreach(t *testing.T) {
	pool := testBreachPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated breach test")
	}
	opID := seedBreachOperator(t, pool)
	s := NewOperatorStore(pool)
	ctx := context.Background()

	base := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	logs := []struct {
		at        time.Time
		status    string
		latencyMs *int
	}{
		{base, "down", intPtr(0)},
		{base.Add(60 * time.Second), "down", intPtr(0)},
		{base.Add(120 * time.Second), "down", intPtr(0)},
	}
	insertHealthLogs(t, pool, opID, logs)

	breaches, err := s.BreachesForOperatorMonth(ctx, opID, 2026, 3, 500)
	if err != nil {
		t.Fatalf("BreachesForOperatorMonth: %v", err)
	}
	if len(breaches) != 0 {
		t.Errorf("expected 0 breaches for 2-min outage, got %d", len(breaches))
	}
}

func TestOperatorBreachDetection_FiveMinDown_OneBreach(t *testing.T) {
	pool := testBreachPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated breach test")
	}
	opID := seedBreachOperator(t, pool)
	s := NewOperatorStore(pool)
	ctx := context.Background()

	base := time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC)
	logs := []struct {
		at        time.Time
		status    string
		latencyMs *int
	}{
		{base, "down", intPtr(0)},
		{base.Add(60 * time.Second), "down", intPtr(0)},
		{base.Add(120 * time.Second), "down", intPtr(0)},
		{base.Add(180 * time.Second), "down", intPtr(0)},
		{base.Add(240 * time.Second), "down", intPtr(0)},
		{base.Add(300 * time.Second), "down", intPtr(0)},
		{base.Add(360 * time.Second), "down", intPtr(0)},
	}
	insertHealthLogs(t, pool, opID, logs)

	breaches, err := s.BreachesForOperatorMonth(ctx, opID, 2026, 3, 500)
	if err != nil {
		t.Fatalf("BreachesForOperatorMonth: %v", err)
	}
	if len(breaches) != 1 {
		t.Fatalf("expected 1 breach, got %d", len(breaches))
	}
	if breaches[0].Cause != "down" {
		t.Errorf("cause: got %q, want %q", breaches[0].Cause, "down")
	}
	if breaches[0].DurationSec < 300 {
		t.Errorf("duration_sec: got %d, want >= 300", breaches[0].DurationSec)
	}
}

func TestOperatorBreachDetection_LatencyOnly_Breach(t *testing.T) {
	pool := testBreachPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated breach test")
	}
	opID := seedBreachOperator(t, pool)
	s := NewOperatorStore(pool)
	ctx := context.Background()

	base := time.Date(2026, 3, 17, 10, 0, 0, 0, time.UTC)
	logs := []struct {
		at        time.Time
		status    string
		latencyMs *int
	}{
		{base, "up", intPtr(600)},
		{base.Add(60 * time.Second), "up", intPtr(700)},
		{base.Add(120 * time.Second), "up", intPtr(650)},
		{base.Add(180 * time.Second), "up", intPtr(800)},
		{base.Add(240 * time.Second), "up", intPtr(550)},
		{base.Add(300 * time.Second), "up", intPtr(600)},
		{base.Add(360 * time.Second), "up", intPtr(620)},
	}
	insertHealthLogs(t, pool, opID, logs)

	breaches, err := s.BreachesForOperatorMonth(ctx, opID, 2026, 3, 500)
	if err != nil {
		t.Fatalf("BreachesForOperatorMonth: %v", err)
	}
	if len(breaches) != 1 {
		t.Fatalf("expected 1 latency breach, got %d", len(breaches))
	}
	if breaches[0].Cause != "latency" {
		t.Errorf("cause: got %q, want %q", breaches[0].Cause, "latency")
	}
}

func TestOperatorBreachDetection_Mixed_Breach(t *testing.T) {
	pool := testBreachPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated breach test")
	}
	opID := seedBreachOperator(t, pool)
	s := NewOperatorStore(pool)
	ctx := context.Background()

	base := time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC)
	logs := []struct {
		at        time.Time
		status    string
		latencyMs *int
	}{
		{base, "down", intPtr(0)},
		{base.Add(60 * time.Second), "down", intPtr(0)},
		{base.Add(120 * time.Second), "up", intPtr(700)},
		{base.Add(180 * time.Second), "up", intPtr(600)},
		{base.Add(240 * time.Second), "up", intPtr(550)},
		{base.Add(300 * time.Second), "up", intPtr(600)},
		{base.Add(360 * time.Second), "up", intPtr(580)},
	}
	insertHealthLogs(t, pool, opID, logs)

	breaches, err := s.BreachesForOperatorMonth(ctx, opID, 2026, 3, 500)
	if err != nil {
		t.Fatalf("BreachesForOperatorMonth: %v", err)
	}
	if len(breaches) != 1 {
		t.Fatalf("expected 1 mixed breach, got %d", len(breaches))
	}
	if breaches[0].Cause != "mixed" {
		t.Errorf("cause: got %q, want %q", breaches[0].Cause, "mixed")
	}
}

func TestOperatorBreachDetection_GapSplitsTwoBreaches(t *testing.T) {
	pool := testBreachPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated breach test")
	}
	opID := seedBreachOperator(t, pool)
	s := NewOperatorStore(pool)
	ctx := context.Background()

	base := time.Date(2026, 3, 19, 10, 0, 0, 0, time.UTC)
	logs := []struct {
		at        time.Time
		status    string
		latencyMs *int
	}{
		{base, "down", intPtr(0)},
		{base.Add(60 * time.Second), "down", intPtr(0)},
		{base.Add(120 * time.Second), "down", intPtr(0)},
		{base.Add(180 * time.Second), "down", intPtr(0)},
		{base.Add(240 * time.Second), "down", intPtr(0)},
		{base.Add(300 * time.Second), "down", intPtr(0)},
		{base.Add(900 * time.Second), "down", intPtr(0)},
		{base.Add(960 * time.Second), "down", intPtr(0)},
		{base.Add(1020 * time.Second), "down", intPtr(0)},
		{base.Add(1080 * time.Second), "down", intPtr(0)},
		{base.Add(1140 * time.Second), "down", intPtr(0)},
		{base.Add(1200 * time.Second), "down", intPtr(0)},
	}
	insertHealthLogs(t, pool, opID, logs)

	breaches, err := s.BreachesForOperatorMonth(ctx, opID, 2026, 3, 500)
	if err != nil {
		t.Fatalf("BreachesForOperatorMonth: %v", err)
	}
	if len(breaches) != 2 {
		t.Errorf("expected 2 breaches (gap > 120s splits them), got %d", len(breaches))
	}
}

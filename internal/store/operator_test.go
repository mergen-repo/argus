package store

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOperatorStruct(t *testing.T) {
	o := Operator{
		Name:                      "Test Operator",
		Code:                      "test_op",
		MCC:                       "286",
		MNC:                       "01",
		AdapterConfig:             json.RawMessage(`{"mock":{"enabled":true}}`),
		HealthStatus:              "unknown",
		HealthCheckIntervalSec:    30,
		FailoverPolicy:            "reject",
		FailoverTimeoutMs:         5000,
		CircuitBreakerThreshold:   5,
		CircuitBreakerRecoverySec: 60,
		State:                     "active",
	}

	if o.Name != "Test Operator" {
		t.Errorf("Name = %q, want %q", o.Name, "Test Operator")
	}
	if o.Code != "test_op" {
		t.Errorf("Code = %q, want %q", o.Code, "test_op")
	}
	if o.MCC != "286" {
		t.Errorf("MCC = %q, want %q", o.MCC, "286")
	}
	if o.MNC != "01" {
		t.Errorf("MNC = %q, want %q", o.MNC, "01")
	}
	// STORY-090 Wave 2 D2-B: AdapterType field removed. The nested
	// adapter_config carries the per-protocol enablement flags.
	if string(o.AdapterConfig) != `{"mock":{"enabled":true}}` {
		t.Errorf("AdapterConfig = %q, want nested mock config", string(o.AdapterConfig))
	}
	if o.HealthStatus != "unknown" {
		t.Errorf("HealthStatus = %q, want %q", o.HealthStatus, "unknown")
	}
	if o.HealthCheckIntervalSec != 30 {
		t.Errorf("HealthCheckIntervalSec = %d, want %d", o.HealthCheckIntervalSec, 30)
	}
	if o.FailoverPolicy != "reject" {
		t.Errorf("FailoverPolicy = %q, want %q", o.FailoverPolicy, "reject")
	}
	if o.CircuitBreakerThreshold != 5 {
		t.Errorf("CircuitBreakerThreshold = %d, want %d", o.CircuitBreakerThreshold, 5)
	}
	if o.CircuitBreakerRecoverySec != 60 {
		t.Errorf("CircuitBreakerRecoverySec = %d, want %d", o.CircuitBreakerRecoverySec, 60)
	}
}

func TestCreateOperatorParamsDefaults(t *testing.T) {
	p := CreateOperatorParams{
		Name:          "Test",
		Code:          "test",
		MCC:           "286",
		MNC:           "01",
		AdapterConfig: json.RawMessage(`{"mock":{"enabled":true}}`),
	}

	if p.FailoverPolicy != nil {
		t.Error("FailoverPolicy should be nil (default applied in Create)")
	}
	if p.FailoverTimeoutMs != nil {
		t.Error("FailoverTimeoutMs should be nil (default applied in Create)")
	}
	if p.CircuitBreakerThreshold != nil {
		t.Error("CircuitBreakerThreshold should be nil (default applied in Create)")
	}
	if p.CircuitBreakerRecoverySec != nil {
		t.Error("CircuitBreakerRecoverySec should be nil (default applied in Create)")
	}
	if p.HealthCheckIntervalSec != nil {
		t.Error("HealthCheckIntervalSec should be nil (default applied in Create)")
	}
}

func TestUpdateOperatorParamsOptional(t *testing.T) {
	name := "Updated"
	p := UpdateOperatorParams{
		Name: &name,
	}

	if p.Name == nil || *p.Name != "Updated" {
		t.Error("Name should be set")
	}
	if p.FailoverPolicy != nil {
		t.Error("FailoverPolicy should be nil")
	}
	if p.State != nil {
		t.Error("State should be nil")
	}
}

func TestOperatorGrantStruct(t *testing.T) {
	g := OperatorGrant{
		Enabled: true,
	}

	if !g.Enabled {
		t.Error("Enabled should be true")
	}
}

func TestOperatorHealthLogStruct(t *testing.T) {
	latency := 42
	h := OperatorHealthLog{
		Status:       "healthy",
		LatencyMs:    &latency,
		CircuitState: "closed",
	}

	if h.Status != "healthy" {
		t.Errorf("Status = %q, want %q", h.Status, "healthy")
	}
	if *h.LatencyMs != 42 {
		t.Errorf("LatencyMs = %d, want %d", *h.LatencyMs, 42)
	}
	if h.CircuitState != "closed" {
		t.Errorf("CircuitState = %q, want %q", h.CircuitState, "closed")
	}
}

func TestErrOperatorNotFound(t *testing.T) {
	if ErrOperatorNotFound.Error() != "store: operator not found" {
		t.Errorf("ErrOperatorNotFound = %q", ErrOperatorNotFound.Error())
	}
}

func TestErrOperatorCodeExists(t *testing.T) {
	if ErrOperatorCodeExists.Error() != "store: operator code already exists" {
		t.Errorf("ErrOperatorCodeExists = %q", ErrOperatorCodeExists.Error())
	}
}

func TestErrGrantNotFound(t *testing.T) {
	if ErrGrantNotFound.Error() != "store: operator grant not found" {
		t.Errorf("ErrGrantNotFound = %q", ErrGrantNotFound.Error())
	}
}

func TestErrGrantExists(t *testing.T) {
	if ErrGrantExists.Error() != "store: operator grant already exists" {
		t.Errorf("ErrGrantExists = %q", ErrGrantExists.Error())
	}
}

func TestOperatorSupportedRATTypes(t *testing.T) {
	o := Operator{
		SupportedRATTypes: []string{"nb_iot", "lte_m", "lte"},
	}

	if len(o.SupportedRATTypes) != 3 {
		t.Errorf("SupportedRATTypes len = %d, want 3", len(o.SupportedRATTypes))
	}
	if o.SupportedRATTypes[0] != "nb_iot" {
		t.Errorf("SupportedRATTypes[0] = %q, want %q", o.SupportedRATTypes[0], "nb_iot")
	}
}

func TestOperatorGrantSupportedRATTypes(t *testing.T) {
	g := OperatorGrant{
		Enabled:           true,
		SupportedRATTypes: []string{"5G_SA", "LTE"},
	}

	if len(g.SupportedRATTypes) != 2 {
		t.Errorf("SupportedRATTypes len = %d, want 2", len(g.SupportedRATTypes))
	}
	if g.SupportedRATTypes[0] != "5G_SA" {
		t.Errorf("SupportedRATTypes[0] = %q, want %q", g.SupportedRATTypes[0], "5G_SA")
	}
	if g.SupportedRATTypes[1] != "LTE" {
		t.Errorf("SupportedRATTypes[1] = %q, want %q", g.SupportedRATTypes[1], "LTE")
	}

	gEmpty := OperatorGrant{}
	if len(gEmpty.SupportedRATTypes) != 0 {
		t.Errorf("SupportedRATTypes should be empty by default, got %d elements", len(gEmpty.SupportedRATTypes))
	}
}

func TestGrantWithOperatorRATFields(t *testing.T) {
	gw := GrantWithOperator{
		OperatorGrant: OperatorGrant{
			SupportedRATTypes: []string{"5G_SA", "LTE"},
		},
		OperatorSupportedRATTypes: []string{"LTE", "3G"},
	}

	if len(gw.SupportedRATTypes) != 2 {
		t.Errorf("grant-level SupportedRATTypes len = %d, want 2", len(gw.SupportedRATTypes))
	}
	if gw.SupportedRATTypes[0] != "5G_SA" {
		t.Errorf("grant-level SupportedRATTypes[0] = %q, want %q", gw.SupportedRATTypes[0], "5G_SA")
	}
	if len(gw.OperatorSupportedRATTypes) != 2 {
		t.Errorf("OperatorSupportedRATTypes len = %d, want 2", len(gw.OperatorSupportedRATTypes))
	}
	if gw.OperatorSupportedRATTypes[0] != "LTE" {
		t.Errorf("OperatorSupportedRATTypes[0] = %q, want %q", gw.OperatorSupportedRATTypes[0], "LTE")
	}
}

func TestOperatorSLAUptimeTarget(t *testing.T) {
	target := 99.90
	o := Operator{
		SLAUptimeTarget: &target,
	}

	if o.SLAUptimeTarget == nil || *o.SLAUptimeTarget != 99.90 {
		t.Error("SLAUptimeTarget should be 99.90")
	}

	o2 := Operator{}
	if o2.SLAUptimeTarget != nil {
		t.Error("SLAUptimeTarget should be nil by default")
	}
}

func testOperatorHealthPool(t *testing.T) *pgxpool.Pool {
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

func truncateOperatorHealthLogs(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `DELETE FROM operator_health_logs WHERE operator_id IN (
		'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa',
		'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb',
		'cccccccc-cccc-cccc-cccc-cccccccccccc',
		'dddddddd-dddd-dddd-dddd-dddddddddddd'
	)`)
	if err != nil {
		t.Fatalf("cleanup operator_health_logs: %v", err)
	}
}

func TestLatestHealthWithLatencyByOperator(t *testing.T) {
	pool := testOperatorHealthPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	truncateOperatorHealthLogs(t, pool)
	t.Cleanup(func() { truncateOperatorHealthLogs(t, pool) })

	ctx := context.Background()
	now := time.Now().UTC()

	ops := []uuid.UUID{
		uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
		uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc"),
	}

	type seedRow struct {
		opID      uuid.UUID
		ago       time.Duration
		latencyMs *int
		status    string
	}

	lat := func(v int) *int { return &v }

	seeds := []seedRow{
		{ops[0], 15 * time.Minute, lat(100), "healthy"},
		{ops[0], 10 * time.Minute, lat(90), "healthy"},
		{ops[0], 5 * time.Minute, lat(80), "healthy"},
		{ops[1], 15 * time.Minute, lat(200), "degraded"},
		{ops[1], 10 * time.Minute, lat(210), "degraded"},
		{ops[1], 5 * time.Minute, lat(195), "degraded"},
		{ops[2], 15 * time.Minute, nil, "unhealthy"},
		{ops[2], 10 * time.Minute, nil, "unhealthy"},
		{ops[2], 5 * time.Minute, nil, "unhealthy"},
	}

	for _, s := range seeds {
		_, err := pool.Exec(ctx,
			`INSERT INTO operator_health_logs (operator_id, checked_at, status, latency_ms, circuit_state)
			 VALUES ($1, $2, $3, $4, 'closed')`,
			s.opID, now.Add(-s.ago), s.status, s.latencyMs,
		)
		if err != nil {
			t.Fatalf("seed operator_health_logs: %v", err)
		}
	}

	store := NewOperatorStore(pool)
	result, err := store.LatestHealthWithLatencyByOperator(ctx)
	if err != nil {
		t.Fatalf("LatestHealthWithLatencyByOperator: %v", err)
	}

	if len(result) < 3 {
		t.Fatalf("expected at least 3 entries in result map, got %d", len(result))
	}

	snap0 := result[ops[0]]
	if snap0.LatencyMs == nil {
		t.Fatal("ops[0]: expected non-nil LatencyMs (latest row has 80)")
	}
	if *snap0.LatencyMs != 80 {
		t.Errorf("ops[0]: LatencyMs = %d, want 80 (latest row)", *snap0.LatencyMs)
	}

	snap1 := result[ops[1]]
	if snap1.LatencyMs == nil {
		t.Fatal("ops[1]: expected non-nil LatencyMs (latest row has 195)")
	}
	if *snap1.LatencyMs != 195 {
		t.Errorf("ops[1]: LatencyMs = %d, want 195 (latest row)", *snap1.LatencyMs)
	}

	snap2 := result[ops[2]]
	if snap2.LatencyMs != nil {
		t.Errorf("ops[2]: expected nil LatencyMs (latest row has NULL latency), got %d", *snap2.LatencyMs)
	}
}

func TestGetLatencyTrend_12Buckets_ZeroFill(t *testing.T) {
	pool := testOperatorHealthPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}

	opID := uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")
	truncateOperatorHealthLogs(t, pool)
	t.Cleanup(func() { truncateOperatorHealthLogs(t, pool) })

	ctx := context.Background()
	bucketDur := 5 * time.Minute
	sinceDur := 1 * time.Hour

	now := time.Now().UTC()
	base := now.Add(-sinceDur).Truncate(bucketDur)

	type seedPoint struct {
		bucketOffset int
		latencyMs    int
	}

	points := []seedPoint{
		{1, 50},
		{6, 120},
		{11, 75},
	}

	for _, p := range points {
		ts := base.Add(time.Duration(p.bucketOffset) * bucketDur).Add(10 * time.Second)
		_, err := pool.Exec(ctx,
			`INSERT INTO operator_health_logs (operator_id, checked_at, status, latency_ms, circuit_state)
			 VALUES ($1, $2, 'healthy', $3, 'closed')`,
			opID, ts, p.latencyMs,
		)
		if err != nil {
			t.Fatalf("seed latency trend row: %v", err)
		}
	}

	store := NewOperatorStore(pool)
	trend, err := store.GetLatencyTrend(ctx, opID, sinceDur, bucketDur)
	if err != nil {
		t.Fatalf("GetLatencyTrend: %v", err)
	}

	if len(trend) != 12 {
		t.Fatalf("expected exactly 12 buckets, got %d", len(trend))
	}

	nonZero := 0
	for _, v := range trend {
		if v > 0 {
			nonZero++
		}
	}
	if nonZero != 3 {
		t.Errorf("expected 3 non-zero buckets, got %d (trend: %v)", nonZero, trend)
	}

	// FIX-203 Gate F-A1 — bucket-index alignment guard. TimescaleDB
	// `time_bucket($1::interval, checked_at)` (no explicit origin) and
	// Go's `origin := now.Add(-since).Truncate(bucket)` must produce
	// the SAME bucket boundaries for all 5-minute (or any bucket that
	// divides evenly into an hour) buckets anchored to UTC. If a seed
	// row lands at bucketOffset K relative to the Go-computed origin,
	// `trend[K]` MUST be non-zero; all other slots must be zero.
	// Regression guard against silent index drift.
	expectedIdx := map[int]float64{
		1:  50,
		6:  120,
		11: 75,
	}
	for _, p := range points {
		got := trend[p.bucketOffset]
		want := float64(p.latencyMs)
		// Allow ±1ms tolerance to absorb AVG float rounding noise.
		if got < want-1 || got > want+1 {
			t.Errorf("trend[%d] = %.1f, want ≈%d (TimescaleDB/Go bucket-index drift?)", p.bucketOffset, got, p.latencyMs)
		}
	}
	for i, v := range trend {
		if _, seeded := expectedIdx[i]; !seeded && v != 0 {
			t.Errorf("trend[%d] = %.1f, want 0 (bucket has no seed rows)", i, v)
		}
	}
}

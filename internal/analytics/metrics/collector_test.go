package metrics

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379", DB: 13})
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("redis not available: %v", err)
	}
	client.FlushDB(ctx)
	t.Cleanup(func() {
		client.FlushDB(ctx)
		client.Close()
	})
	return client
}

type mockSessionCounter struct {
	count int64
}

func (m *mockSessionCounter) CountActive(_ context.Context) (int64, error) {
	return m.count, nil
}

func TestRecordAuth_IncrementsCounters(t *testing.T) {
	rdb := newTestRedis(t)
	ctx := context.Background()
	c := NewCollector(rdb, noopLogger())

	opID := uuid.New()
	c.SetOperatorIDs([]uuid.UUID{opID})

	for i := 0; i < 100; i++ {
		c.RecordAuth(ctx, opID, true, 5)
	}

	epoch := time.Now().Unix()
	epochStr := strconv.FormatInt(epoch, 10)
	total, err := rdb.Get(ctx, fmt.Sprintf("%s:%s", keyAuthTotal, epochStr)).Int64()
	if err != nil {
		t.Fatalf("get total: %v", err)
	}
	if total != 100 {
		t.Errorf("total = %d, want 100", total)
	}

	success, err := rdb.Get(ctx, fmt.Sprintf("%s:%s", keyAuthSuccess, epochStr)).Int64()
	if err != nil {
		t.Fatalf("get success: %v", err)
	}
	if success != 100 {
		t.Errorf("success = %d, want 100", success)
	}
}

func TestRecordAuth_ErrorRate(t *testing.T) {
	rdb := newTestRedis(t)
	ctx := context.Background()
	c := NewCollector(rdb, noopLogger())

	opID := uuid.New()
	c.SetOperatorIDs([]uuid.UUID{opID})

	seedTTL := 30 * time.Second

	targetEpoch := time.Now().Unix() + 1
	targetEpochStr := strconv.FormatInt(targetEpoch, 10)

	pipe := rdb.Pipeline()
	pipe.Set(ctx, fmt.Sprintf("%s:%s", keyAuthTotal, targetEpochStr), 100, seedTTL)
	pipe.Set(ctx, fmt.Sprintf("%s:%s", keyAuthSuccess, targetEpochStr), 90, seedTTL)
	pipe.Set(ctx, fmt.Sprintf("%s:%s", keyAuthFailure, targetEpochStr), 10, seedTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		t.Fatalf("seed redis: %v", err)
	}

	for time.Now().Unix() < targetEpoch+1 {
		time.Sleep(50 * time.Millisecond)
	}

	m, err := c.GetMetrics(ctx)
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}

	if m.AuthPerSec != 100 {
		t.Errorf("AuthPerSec = %d, want 100", m.AuthPerSec)
	}
	if m.AuthErrorRate < 0.09 || m.AuthErrorRate > 0.11 {
		t.Errorf("AuthErrorRate = %.4f, want ~0.10", m.AuthErrorRate)
	}
}


func TestLatencyPercentiles(t *testing.T) {
	rdb := newTestRedis(t)
	ctx := context.Background()
	c := NewCollector(rdb, noopLogger())

	opID := uuid.New()
	c.SetOperatorIDs([]uuid.UUID{opID})

	latencies := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 15, 20, 25, 30, 35, 40, 45, 50, 55, 100}
	for _, l := range latencies {
		c.RecordAuth(ctx, opID, true, l)
	}

	m, err := c.GetMetrics(ctx)
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}

	if m.Latency.P50 <= 0 {
		t.Errorf("P50 = %d, want > 0", m.Latency.P50)
	}
	if m.Latency.P95 <= m.Latency.P50 {
		t.Errorf("P95 = %d should be > P50 = %d", m.Latency.P95, m.Latency.P50)
	}
	if m.Latency.P99 < m.Latency.P95 {
		t.Errorf("P99 = %d should be >= P95 = %d", m.Latency.P99, m.Latency.P95)
	}
}

func TestPerOperatorMetrics(t *testing.T) {
	rdb := newTestRedis(t)
	ctx := context.Background()
	c := NewCollector(rdb, noopLogger())

	opA := uuid.New()
	opB := uuid.New()
	c.SetOperatorIDs([]uuid.UUID{opA, opB})

	seedTTL := 30 * time.Second

	targetEpoch := time.Now().Unix() + 1
	targetEpochStr := strconv.FormatInt(targetEpoch, 10)

	pipe := rdb.Pipeline()
	pipe.Set(ctx, fmt.Sprintf("%s:%s:%s", keyAuthTotal, opA.String(), targetEpochStr), 50, seedTTL)
	pipe.Set(ctx, fmt.Sprintf("%s:%s:%s", keyAuthSuccess, opA.String(), targetEpochStr), 50, seedTTL)
	pipe.Set(ctx, fmt.Sprintf("%s:%s:%s", keyAuthTotal, opB.String(), targetEpochStr), 50, seedTTL)
	pipe.Set(ctx, fmt.Sprintf("%s:%s:%s", keyAuthSuccess, opB.String(), targetEpochStr), 30, seedTTL)
	pipe.Set(ctx, fmt.Sprintf("%s:%s:%s", keyAuthFailure, opB.String(), targetEpochStr), 20, seedTTL)
	pipe.Set(ctx, fmt.Sprintf("%s:%s", keyAuthTotal, targetEpochStr), 100, seedTTL)
	pipe.Set(ctx, fmt.Sprintf("%s:%s", keyAuthSuccess, targetEpochStr), 80, seedTTL)
	pipe.Set(ctx, fmt.Sprintf("%s:%s", keyAuthFailure, targetEpochStr), 20, seedTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		t.Fatalf("seed redis: %v", err)
	}

	for time.Now().Unix() < targetEpoch+1 {
		time.Sleep(50 * time.Millisecond)
	}

	m, err := c.GetMetrics(ctx)
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}

	opAMetrics, ok := m.ByOperator[opA.String()]
	if !ok {
		t.Fatal("operator A metrics not found")
	}
	if opAMetrics.AuthPerSec != 50 {
		t.Errorf("opA AuthPerSec = %d, want 50", opAMetrics.AuthPerSec)
	}

	opBMetrics, ok := m.ByOperator[opB.String()]
	if !ok {
		t.Fatal("operator B metrics not found")
	}
	if opBMetrics.AuthPerSec != 50 {
		t.Errorf("opB AuthPerSec = %d, want 50", opBMetrics.AuthPerSec)
	}
	if opBMetrics.AuthErrorRate < 0.35 || opBMetrics.AuthErrorRate > 0.45 {
		t.Errorf("opB AuthErrorRate = %.4f, want ~0.40", opBMetrics.AuthErrorRate)
	}
}

func TestSystemStatus_Healthy(t *testing.T) {
	s := DeriveStatus(0.02)
	if s != StatusHealthy {
		t.Errorf("status = %s, want healthy", s)
	}
}

func TestSystemStatus_Degraded(t *testing.T) {
	s := DeriveStatus(0.10)
	if s != StatusDegraded {
		t.Errorf("status = %s, want degraded", s)
	}
}

func TestSystemStatus_Critical(t *testing.T) {
	s := DeriveStatus(0.25)
	if s != StatusCritical {
		t.Errorf("status = %s, want critical", s)
	}
}

func TestActiveSessionCount(t *testing.T) {
	rdb := newTestRedis(t)
	ctx := context.Background()
	c := NewCollector(rdb, noopLogger())

	mock := &mockSessionCounter{count: 42}
	c.SetSessionCounter(mock)

	m, err := c.GetMetrics(ctx)
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}
	if m.ActiveSessions != 42 {
		t.Errorf("ActiveSessions = %d, want 42", m.ActiveSessions)
	}
}

func TestGetMetrics_NoData(t *testing.T) {
	rdb := newTestRedis(t)
	ctx := context.Background()
	c := NewCollector(rdb, noopLogger())

	m, err := c.GetMetrics(ctx)
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}
	if m.AuthPerSec != 0 {
		t.Errorf("AuthPerSec = %d, want 0", m.AuthPerSec)
	}
	if m.AuthErrorRate != 0 {
		t.Errorf("AuthErrorRate = %f, want 0", m.AuthErrorRate)
	}
	if m.SystemStatus != StatusHealthy {
		t.Errorf("SystemStatus = %s, want healthy", m.SystemStatus)
	}
}

func TestDeriveStatus_BoundaryValues(t *testing.T) {
	tests := []struct {
		name      string
		errorRate float64
		want      SystemStatus
	}{
		{"zero", 0.0, StatusHealthy},
		{"just_below_degraded", 0.0499, StatusHealthy},
		{"exactly_degraded", 0.05, StatusDegraded},
		{"mid_degraded", 0.10, StatusDegraded},
		{"just_below_critical", 0.1999, StatusDegraded},
		{"exactly_critical", 0.20, StatusCritical},
		{"well_above_critical", 0.50, StatusCritical},
		{"full_failure", 1.0, StatusCritical},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeriveStatus(tt.errorRate)
			if got != tt.want {
				t.Errorf("DeriveStatus(%.4f) = %s, want %s", tt.errorRate, got, tt.want)
			}
		})
	}
}

func TestToRealtimePayload(t *testing.T) {
	m := SystemMetrics{
		AuthPerSec:     42,
		AuthErrorRate:  0.05,
		Latency:        LatencyPercentiles{P50: 5, P95: 20, P99: 50},
		ActiveSessions: 100,
		SystemStatus:   StatusDegraded,
	}

	p := ToRealtimePayload(m)

	if p.AuthPerSec != 42 {
		t.Errorf("AuthPerSec = %d, want 42", p.AuthPerSec)
	}
	if p.ErrorRate != 0.05 {
		t.Errorf("ErrorRate = %f, want 0.05", p.ErrorRate)
	}
	if p.LatencyP50 != 5 {
		t.Errorf("LatencyP50 = %d, want 5", p.LatencyP50)
	}
	if p.LatencyP95 != 20 {
		t.Errorf("LatencyP95 = %d, want 20", p.LatencyP95)
	}
	if p.ActiveSessions != 100 {
		t.Errorf("ActiveSessions = %d, want 100", p.ActiveSessions)
	}
	if p.SystemStatus != StatusDegraded {
		t.Errorf("SystemStatus = %s, want degraded", p.SystemStatus)
	}
	if p.Timestamp == "" {
		t.Error("Timestamp should not be empty")
	}
}

func TestRecordAuth_NilRedis(t *testing.T) {
	c := NewCollector(nil, noopLogger())
	c.RecordAuth(context.Background(), uuid.New(), true, 5)
}

func noopLogger() zerolog.Logger {
	return zerolog.Nop()
}

package anomaly

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379", DB: 15})
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

func TestCheckSIMCloning_NoCloning(t *testing.T) {
	rdb := newTestRedis(t)

	d := NewRealtimeDetector(rdb, DefaultThresholds(), zerolog.Nop())
	ctx := context.Background()

	simID := uuid.New()
	tenantID := uuid.New()

	results, err := d.CheckAuth(ctx, AuthEvent{
		IMSI:      "001010000000001",
		SimID:     simID,
		TenantID:  tenantID,
		NASIP:     "10.0.0.1",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, r := range results {
		if r.Type == TypeSIMCloning {
			t.Error("expected no SIM cloning detection for single NAS IP")
		}
	}
}

func TestCheckSIMCloning_Detected(t *testing.T) {
	rdb := newTestRedis(t)

	d := NewRealtimeDetector(rdb, DefaultThresholds(), zerolog.Nop())
	ctx := context.Background()

	simID := uuid.New()
	tenantID := uuid.New()
	imsi := "001010000000002"

	_, err := d.CheckAuth(ctx, AuthEvent{
		IMSI:      imsi,
		SimID:     simID,
		TenantID:  tenantID,
		NASIP:     "10.0.0.1",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	results, err := d.CheckAuth(ctx, AuthEvent{
		IMSI:      imsi,
		SimID:     simID,
		TenantID:  tenantID,
		NASIP:     "10.0.0.2",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, r := range results {
		if r.Type == TypeSIMCloning {
			found = true
			if r.Severity != SeverityCritical {
				t.Errorf("expected critical severity, got %s", r.Severity)
			}
			if r.SimID == nil || *r.SimID != simID {
				t.Error("expected sim_id in result")
			}
		}
	}
	if !found {
		t.Error("expected SIM cloning detection")
	}
}

func TestCheckAuthFlood_NoFlood(t *testing.T) {
	rdb := newTestRedis(t)

	thresholds := DefaultThresholds()
	thresholds.AuthFloodMax = 5
	d := NewRealtimeDetector(rdb, thresholds, zerolog.Nop())
	ctx := context.Background()

	simID := uuid.New()
	tenantID := uuid.New()

	for i := 0; i < 3; i++ {
		results, err := d.CheckAuth(ctx, AuthEvent{
			IMSI:      "001010000000003",
			SimID:     simID,
			TenantID:  tenantID,
			NASIP:     "10.0.0.1",
			Timestamp: time.Now(),
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range results {
			if r.Type == TypeAuthFlood {
				t.Errorf("unexpected auth flood at iteration %d", i)
			}
		}
	}
}

func TestCheckAuthFlood_Detected(t *testing.T) {
	rdb := newTestRedis(t)

	thresholds := DefaultThresholds()
	thresholds.AuthFloodMax = 5
	d := NewRealtimeDetector(rdb, thresholds, zerolog.Nop())
	ctx := context.Background()

	simID := uuid.New()
	tenantID := uuid.New()

	var floodDetected bool
	for i := 0; i < 10; i++ {
		results, err := d.CheckAuth(ctx, AuthEvent{
			IMSI:      "001010000000004",
			SimID:     simID,
			TenantID:  tenantID,
			NASIP:     "10.0.0.1",
			Timestamp: time.Now(),
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range results {
			if r.Type == TypeAuthFlood {
				floodDetected = true
				if r.Severity != SeverityHigh {
					t.Errorf("expected high severity, got %s", r.Severity)
				}
			}
		}
	}
	if !floodDetected {
		t.Error("expected auth flood detection")
	}
}

func TestCheckNASFlood_Detected(t *testing.T) {
	rdb := newTestRedis(t)

	thresholds := DefaultThresholds()
	thresholds.NASFloodMax = 3
	d := NewRealtimeDetector(rdb, thresholds, zerolog.Nop())
	ctx := context.Background()

	tenantID := uuid.New()

	var floodDetected bool
	for i := 0; i < 6; i++ {
		results, err := d.CheckAuth(ctx, AuthEvent{
			IMSI:      "0010100000000" + string(rune('A'+i)),
			SimID:     uuid.New(),
			TenantID:  tenantID,
			NASIP:     "192.168.1.1",
			Timestamp: time.Now(),
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range results {
			if r.Type == TypeNASFlood {
				floodDetected = true
				if r.Severity != SeverityHigh {
					t.Errorf("expected high severity, got %s", r.Severity)
				}
			}
		}
	}
	if !floodDetected {
		t.Error("expected NAS flood detection")
	}
}

func TestBulkJobFilter(t *testing.T) {
	rdb := newTestRedis(t)

	thresholds := DefaultThresholds()
	thresholds.AuthFloodMax = 1
	thresholds.FilterBulkJobs = true
	d := NewRealtimeDetector(rdb, thresholds, zerolog.Nop())
	ctx := context.Background()

	results, err := d.CheckAuth(ctx, AuthEvent{
		IMSI:      "001010000000010",
		SimID:     uuid.New(),
		TenantID:  uuid.New(),
		NASIP:     "10.0.0.1",
		Source:    "bulk_job",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) > 0 {
		t.Error("expected no detections for bulk_job source events")
	}
}

func TestCheckSIMCloning_SameNAS_NoDetection(t *testing.T) {
	rdb := newTestRedis(t)

	d := NewRealtimeDetector(rdb, DefaultThresholds(), zerolog.Nop())
	ctx := context.Background()

	simID := uuid.New()
	tenantID := uuid.New()
	imsi := "001010000000020"

	for i := 0; i < 5; i++ {
		results, err := d.CheckAuth(ctx, AuthEvent{
			IMSI:      imsi,
			SimID:     simID,
			TenantID:  tenantID,
			NASIP:     "10.0.0.1",
			Timestamp: time.Now(),
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range results {
			if r.Type == TypeSIMCloning {
				t.Error("expected no SIM cloning detection for same NAS IP")
			}
		}
	}
}

func TestNilRedis_NoError(t *testing.T) {
	d := NewRealtimeDetector(nil, DefaultThresholds(), zerolog.Nop())
	ctx := context.Background()

	results, err := d.CheckAuth(ctx, AuthEvent{
		IMSI:      "001010000000030",
		SimID:     uuid.New(),
		TenantID:  uuid.New(),
		NASIP:     "10.0.0.1",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if results != nil {
		t.Error("expected nil results when redis is nil")
	}
}

func TestCheckAuthFlood_ExactlyAtThreshold(t *testing.T) {
	rdb := newTestRedis(t)

	thresholds := DefaultThresholds()
	thresholds.AuthFloodMax = 5
	d := NewRealtimeDetector(rdb, thresholds, zerolog.Nop())
	ctx := context.Background()

	simID := uuid.New()
	tenantID := uuid.New()

	var floodDetected bool
	for i := 0; i < 5; i++ {
		results, err := d.CheckAuth(ctx, AuthEvent{
			IMSI:      "001010000000040",
			SimID:     simID,
			TenantID:  tenantID,
			NASIP:     "10.0.0.1",
			Timestamp: time.Now(),
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range results {
			if r.Type == TypeAuthFlood {
				floodDetected = true
			}
		}
	}
	if floodDetected {
		t.Error("at exactly threshold (5), should NOT trigger flood detection")
	}
}

func TestCheckSIMCloning_ThreeDistinctNAS(t *testing.T) {
	rdb := newTestRedis(t)

	d := NewRealtimeDetector(rdb, DefaultThresholds(), zerolog.Nop())
	ctx := context.Background()

	simID := uuid.New()
	tenantID := uuid.New()
	imsi := "001010000000050"

	nasIPs := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}
	cloningCount := 0

	for _, nasIP := range nasIPs {
		results, err := d.CheckAuth(ctx, AuthEvent{
			IMSI:      imsi,
			SimID:     simID,
			TenantID:  tenantID,
			NASIP:     nasIP,
			Timestamp: time.Now(),
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range results {
			if r.Type == TypeSIMCloning {
				cloningCount++
			}
		}
	}
	if cloningCount == 0 {
		t.Error("expected SIM cloning detection with 3 distinct NAS IPs")
	}
}

func TestExtractIP(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"10.0.0.1:123456789", "10.0.0.1"},
		{"192.168.1.1:999", "192.168.1.1"},
		{"nocolon", "nocolon"},
		{"[2001:db8::1]:4500", "2001:db8::1"},
		{"[::1]:1812", "::1"},
		{"2001:db8::1", "2001:db8::1"},
	}
	for _, tt := range tests {
		got := extractIP(tt.input)
		if got != tt.expected {
			t.Errorf("extractIP(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

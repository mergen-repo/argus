package operator

import (
	"testing"

	"github.com/rs/zerolog"
)

func TestCalculateUptime(t *testing.T) {
	tests := []struct {
		total    int64
		failed   int64
		expected float64
	}{
		{0, 0, 100.0},
		{100, 0, 100.0},
		{100, 1, 99.0},
		{100, 5, 95.0},
		{1000, 10, 99.0},
		{100, 100, 0.0},
	}

	for _, tt := range tests {
		got := CalculateUptime(tt.total, tt.failed)
		if got != tt.expected {
			t.Errorf("CalculateUptime(%d, %d) = %.2f, want %.2f", tt.total, tt.failed, got, tt.expected)
		}
	}
}

func TestCheckSLAViolation(t *testing.T) {
	target999 := 99.9
	target99 := 99.0

	tests := []struct {
		uptime    float64
		target    *float64
		violation bool
	}{
		{100.0, &target999, false},
		{99.9, &target999, false},
		{99.8, &target999, true},
		{99.0, &target99, false},
		{98.5, &target99, true},
		{95.0, nil, false},
	}

	for _, tt := range tests {
		got := CheckSLAViolation(tt.uptime, tt.target)
		if got != tt.violation {
			t.Errorf("CheckSLAViolation(%.2f, %v) = %v, want %v", tt.uptime, tt.target, got, tt.violation)
		}
	}
}

func TestPercentile(t *testing.T) {
	sorted := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	tests := []struct {
		pct  int
		want int
	}{
		{50, 5},
		{95, 10},
		{99, 10},
		{10, 1},
	}

	for _, tt := range tests {
		got := percentile(sorted, tt.pct)
		if got != tt.want {
			t.Errorf("percentile(sorted, %d) = %d, want %d", tt.pct, got, tt.want)
		}
	}
}

func TestPercentile_Empty(t *testing.T) {
	got := percentile(nil, 50)
	if got != 0 {
		t.Errorf("percentile(nil, 50) = %d, want 0", got)
	}
}

func TestPercentile_Single(t *testing.T) {
	got := percentile([]int{42}, 95)
	if got != 42 {
		t.Errorf("percentile([42], 95) = %d, want 42", got)
	}
}

func TestNewSLATracker_NilRedis(t *testing.T) {
	tracker := NewSLATracker(nil, zerolog.Nop())
	if tracker == nil {
		t.Fatal("NewSLATracker should not return nil")
	}

	tracker.RecordLatency(nil, [16]byte{}, 100)

	p50, p95, p99 := tracker.GetLatencyPercentiles(nil, [16]byte{})
	if p50 != 0 || p95 != 0 || p99 != 0 {
		t.Errorf("nil redis should return 0s, got %d %d %d", p50, p95, p99)
	}
}

func TestSLAMetrics_ComputeWithNilRedis(t *testing.T) {
	tracker := NewSLATracker(nil, zerolog.Nop())

	target := 99.9
	metrics := tracker.ComputeMetrics(nil, [16]byte{}, 1000, 5, &target)

	if metrics.Uptime24h != 99.5 {
		t.Errorf("Uptime24h = %.2f, want 99.50", metrics.Uptime24h)
	}
	if !metrics.SLAViolation {
		t.Error("expected SLA violation (99.5 < 99.9)")
	}
	if metrics.SLATarget != 99.9 {
		t.Errorf("SLATarget = %.1f, want 99.9", metrics.SLATarget)
	}
	if metrics.TotalChecks != 1000 {
		t.Errorf("TotalChecks = %d, want 1000", metrics.TotalChecks)
	}
	if metrics.FailedChecks != 5 {
		t.Errorf("FailedChecks = %d, want 5", metrics.FailedChecks)
	}
}

func TestSLAMetrics_NoViolationWithoutTarget(t *testing.T) {
	tracker := NewSLATracker(nil, zerolog.Nop())

	metrics := tracker.ComputeMetrics(nil, [16]byte{}, 100, 50, nil)

	if metrics.SLAViolation {
		t.Error("should not be a violation when no target is set")
	}
	if metrics.Uptime24h != 50.0 {
		t.Errorf("Uptime24h = %.2f, want 50.00", metrics.Uptime24h)
	}
}

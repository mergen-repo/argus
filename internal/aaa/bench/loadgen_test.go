package bench

import (
	"testing"
	"time"
)

func TestSortDurations(t *testing.T) {
	ds := []time.Duration{
		5 * time.Millisecond,
		1 * time.Millisecond,
		10 * time.Millisecond,
		3 * time.Millisecond,
		7 * time.Millisecond,
	}
	sortDurations(ds)

	for i := 1; i < len(ds); i++ {
		if ds[i] < ds[i-1] {
			t.Errorf("not sorted at index %d: %v > %v", i, ds[i-1], ds[i])
		}
	}
}

func TestLoadGenResultString(t *testing.T) {
	r := &LoadGenResult{
		TotalSent:    10000,
		TotalSuccess: 9990,
		TotalFailed:  5,
		TotalTimeout: 5,
		Duration:     60 * time.Second,
		RateActual:   166.67,
		LatencyP50:   3 * time.Millisecond,
		LatencyP95:   15 * time.Millisecond,
		LatencyP99:   40 * time.Millisecond,
		LatencyMin:   500 * time.Microsecond,
		LatencyMax:   100 * time.Millisecond,
		LatencyAvg:   5 * time.Millisecond,
	}

	s := r.String()
	if s == "" {
		t.Error("String() returned empty")
	}
}

func TestDefaultLoadGenConfig(t *testing.T) {
	cfg := DefaultLoadGenConfig()
	if cfg.Concurrency != 100 {
		t.Errorf("Concurrency = %d, want 100", cfg.Concurrency)
	}
	if cfg.RatePerSec != 10000 {
		t.Errorf("RatePerSec = %d, want 10000", cfg.RatePerSec)
	}
	if cfg.Duration != 60*time.Second {
		t.Errorf("Duration = %v, want 60s", cfg.Duration)
	}
}

func TestNewLoadGenerator(t *testing.T) {
	cfg := DefaultLoadGenConfig()
	cfg.IMSICount = 50
	lg := NewLoadGenerator(cfg)

	if len(lg.imsis) != 50 {
		t.Errorf("imsis len = %d, want 50", len(lg.imsis))
	}
}

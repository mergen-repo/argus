package scenario

import (
	"math"
	"testing"

	"github.com/btopcu/argus/internal/simulator/config"
)

func TestPick_HonorsWeights(t *testing.T) {
	scenarios := []config.ScenarioConfig{
		{Name: "a", Weight: 0.7, SessionDurationSeconds: [2]int{60, 120}, InterimIntervalSeconds: 30, BytesPerInterimInRange: [2]int{1, 2}, BytesPerInterimOutRange: [2]int{1, 2}},
		{Name: "b", Weight: 0.3, SessionDurationSeconds: [2]int{60, 120}, InterimIntervalSeconds: 30, BytesPerInterimInRange: [2]int{1, 2}, BytesPerInterimOutRange: [2]int{1, 2}},
	}
	p := New(scenarios, 42) // deterministic seed
	counts := map[string]int{}
	const N = 10000
	for i := 0; i < N; i++ {
		counts[p.Pick().Name]++
	}
	// Expect ~70% a, ~30% b. Allow ±3% margin for the 10k trial.
	ra := float64(counts["a"]) / N
	rb := float64(counts["b"]) / N
	if math.Abs(ra-0.70) > 0.03 {
		t.Errorf("a ratio=%.3f, expected ~0.70", ra)
	}
	if math.Abs(rb-0.30) > 0.03 {
		t.Errorf("b ratio=%.3f, expected ~0.30", rb)
	}
}

func TestPick_SampleFieldsInRange(t *testing.T) {
	scenarios := []config.ScenarioConfig{
		{Name: "x", Weight: 1.0, SessionDurationSeconds: [2]int{100, 200}, InterimIntervalSeconds: 15, BytesPerInterimInRange: [2]int{1000, 2000}, BytesPerInterimOutRange: [2]int{500, 1500}},
	}
	p := New(scenarios, 1)
	for i := 0; i < 100; i++ {
		s := p.Pick()
		if s.Name != "x" {
			t.Fatalf("unexpected scenario: %s", s.Name)
		}
		if s.SessionDuration.Seconds() < 100 || s.SessionDuration.Seconds() > 200 {
			t.Errorf("duration out of range: %v", s.SessionDuration)
		}
		if s.InterimInterval.Seconds() != 15 {
			t.Errorf("interim interval wrong: %v", s.InterimInterval)
		}
		if s.BytesPerInterimIn < 1000 || s.BytesPerInterimIn > 2000 {
			t.Errorf("bytes-in out of range: %d", s.BytesPerInterimIn)
		}
		if s.BytesPerInterimOut < 500 || s.BytesPerInterimOut > 1500 {
			t.Errorf("bytes-out out of range: %d", s.BytesPerInterimOut)
		}
	}
}

func TestJitterDuration_Range(t *testing.T) {
	p := New([]config.ScenarioConfig{{Name: "n", Weight: 1.0, SessionDurationSeconds: [2]int{1, 2}, InterimIntervalSeconds: 1, BytesPerInterimInRange: [2]int{1, 2}, BytesPerInterimOutRange: [2]int{1, 2}}}, 7)
	for i := 0; i < 50; i++ {
		d := p.JitterDuration(5, 15)
		if d.Seconds() < 5 || d.Seconds() > 15 {
			t.Errorf("jitter out of range: %v", d)
		}
	}
}

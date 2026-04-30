package reactive

import (
	"testing"
	"time"

	"github.com/btopcu/argus/internal/simulator/config"
)

func newTracker(base, max time.Duration, maxPerHour int) *RejectTracker {
	cfg := config.ReactiveDefaults{
		RejectBackoffBase:       base,
		RejectBackoffMax:        max,
		RejectMaxRetriesPerHour: maxPerHour,
	}
	return NewRejectTracker(cfg)
}

func TestBackoff_Curve(t *testing.T) {
	fakeNow := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	tr := newTracker(30*time.Second, 600*time.Second, 5)
	tr.now = func() time.Time { return fakeNow }

	type want struct {
		wait      time.Duration
		suspended bool
	}
	cases := []want{
		{30 * time.Second, false},
		{60 * time.Second, false},
		{120 * time.Second, false},
		{240 * time.Second, false},
		{480 * time.Second, false},
	}

	for i, c := range cases {
		wait, suspended := tr.NextBackoff("op1", "imsi1")
		if wait != c.wait {
			t.Errorf("attempt %d: want wait=%v got %v", i+1, c.wait, wait)
		}
		if suspended != c.suspended {
			t.Errorf("attempt %d: want suspended=%v got %v", i+1, c.suspended, suspended)
		}
	}

	// 6th attempt crosses maxPerHour=5 → suspended=true, wait = 1h (all at same fakeNow)
	wait, suspended := tr.NextBackoff("op1", "imsi1")
	if !suspended {
		t.Errorf("attempt 6: expected suspended=true")
	}
	if wait != time.Hour {
		t.Errorf("attempt 6: want wait=1h got %v", wait)
	}
}

func TestBackoff_CappedAtMax(t *testing.T) {
	fakeNow := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	tr := newTracker(200*time.Second, 400*time.Second, 10)
	tr.now = func() time.Time { return fakeNow }

	type want struct {
		wait time.Duration
	}
	cases := []want{
		{200 * time.Second},
		{400 * time.Second},
		{400 * time.Second},
		{400 * time.Second},
	}

	for i, c := range cases {
		wait, _ := tr.NextBackoff("op1", "imsi1")
		if wait != c.wait {
			t.Errorf("attempt %d: want=%v got=%v", i+1, c.wait, wait)
		}
	}
}

func TestBackoff_WindowSlide(t *testing.T) {
	fakeNow := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	tr := newTracker(30*time.Second, 600*time.Second, 5)
	tr.now = func() time.Time { return fakeNow }

	// First reject in the window.
	wait1, _ := tr.NextBackoff("op1", "imsi1")
	if wait1 != 30*time.Second {
		t.Fatalf("first reject: want 30s got %v", wait1)
	}

	// Advance past 1h window.
	fakeNow = fakeNow.Add(61 * time.Minute)
	tr.now = func() time.Time { return fakeNow }

	// Next reject should reset to count=1, wait=base.
	wait2, suspended := tr.NextBackoff("op1", "imsi1")
	if suspended {
		t.Error("after window slide: expected suspended=false")
	}
	if wait2 != 30*time.Second {
		t.Errorf("after window slide: want 30s got %v", wait2)
	}
}

func TestBackoff_SuspendedClearsWithReset(t *testing.T) {
	fakeNow := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	tr := newTracker(30*time.Second, 600*time.Second, 5)
	tr.now = func() time.Time { return fakeNow }

	// Trigger suspension (6 rapid attempts).
	for i := 0; i < 6; i++ {
		tr.NextBackoff("op1", "imsi1")
	}

	// Verify suspended.
	_, suspended := tr.NextBackoff("op1", "imsi1")
	if !suspended {
		t.Fatal("expected suspended=true before reset")
	}

	// Reset clears the state.
	tr.Reset("op1", "imsi1")

	// Next attempt should behave as if fresh.
	wait, suspended := tr.NextBackoff("op1", "imsi1")
	if suspended {
		t.Error("after Reset: expected suspended=false")
	}
	if wait != 30*time.Second {
		t.Errorf("after Reset: want 30s got %v", wait)
	}
}

func TestBackoff_Allowed(t *testing.T) {
	fakeNow := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	tr := newTracker(30*time.Second, 600*time.Second, 5)
	tr.now = func() time.Time { return fakeNow }

	// Fresh key → Allowed returns true.
	if !tr.Allowed("op1", "imsi1") {
		t.Error("fresh key: expected Allowed=true")
	}

	// After a reject, Allowed returns false until time advances past nextAllowed.
	tr.NextBackoff("op1", "imsi1") // wait=30s

	if tr.Allowed("op1", "imsi1") {
		t.Error("immediately after reject: expected Allowed=false")
	}

	// Advance to exactly nextAllowed boundary (30s).
	fakeNow = fakeNow.Add(30 * time.Second)
	tr.now = func() time.Time { return fakeNow }

	if !tr.Allowed("op1", "imsi1") {
		t.Error("at nextAllowed boundary: expected Allowed=true")
	}
}

func TestBackoff_MultiKey(t *testing.T) {
	fakeNow := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	tr := newTracker(30*time.Second, 600*time.Second, 5)
	tr.now = func() time.Time { return fakeNow }

	// Drive key1 to attempt 3.
	tr.NextBackoff("op1", "imsi1")
	tr.NextBackoff("op1", "imsi1")
	w3, _ := tr.NextBackoff("op1", "imsi1")

	// key2 should start fresh at attempt 1.
	w1, _ := tr.NextBackoff("op2", "imsi2")

	if w3 != 120*time.Second {
		t.Errorf("key1 attempt 3: want 120s got %v", w3)
	}
	if w1 != 30*time.Second {
		t.Errorf("key2 attempt 1: want 30s got %v", w1)
	}
}

func TestBackoff_ResetNonExistent(t *testing.T) {
	tr := newTracker(30*time.Second, 600*time.Second, 5)
	// Must not panic.
	tr.Reset("nonexistent", "key")
}

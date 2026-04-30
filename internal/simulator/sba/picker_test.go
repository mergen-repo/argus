package sba

import (
	"fmt"
	"testing"
)

// TestShouldUseSBA_EdgeCases verifies the deterministic edge-case behaviour.
func TestShouldUseSBA_EdgeCases(t *testing.T) {
	sc := SessionContext{AcctSessionID: "sess-abc-001"}

	if ShouldUseSBA(sc, 0.0) {
		t.Error("rate=0: expected false, got true")
	}
	if ShouldUseSBA(sc, -1.0) {
		t.Error("rate=-1: expected false, got true")
	}
	if !ShouldUseSBA(sc, 1.0) {
		t.Error("rate=1: expected true, got false")
	}
	if !ShouldUseSBA(sc, 2.0) {
		t.Error("rate=2: expected true, got false")
	}
}

// TestShouldUseSBA_Deterministic verifies that the same AcctSessionID always
// produces the same result regardless of how many times ShouldUseSBA is called.
func TestShouldUseSBA_Deterministic(t *testing.T) {
	sc := SessionContext{AcctSessionID: "sess-determinism-test"}
	rate := 0.5
	first := ShouldUseSBA(sc, rate)
	for i := 0; i < 100; i++ {
		if got := ShouldUseSBA(sc, rate); got != first {
			t.Errorf("iter %d: got %v, want %v (not deterministic)", i, got, first)
		}
	}
}

// TestShouldUseSBA_Distribution verifies that over 1 000 distinct session IDs
// the observed SBA fraction at rate=0.3 lands in the tolerance band [0.25, 0.35].
// SHA-256 is a high-quality PRF over short ASCII strings; deviations beyond ±5%
// at n=1 000 would be extraordinarily unlikely and indicate a hashing bug.
func TestShouldUseSBA_Distribution(t *testing.T) {
	const n = 1_000
	rate := 0.3
	count := 0
	for i := 0; i < n; i++ {
		sc := SessionContext{AcctSessionID: fmt.Sprintf("sess-%06d", i)}
		if ShouldUseSBA(sc, rate) {
			count++
		}
	}
	fraction := float64(count) / n
	if fraction < 0.25 || fraction > 0.35 {
		t.Errorf("rate=0.3 over %d sessions: fraction=%.4f, want [0.25, 0.35]", n, fraction)
	}
}

// TestShouldUseSBA_DistinctSessions verifies that different AcctSessionIDs
// that produce different hash buckets are not all forced into the same outcome.
func TestShouldUseSBA_DistinctSessions(t *testing.T) {
	rate := 0.5
	trueCount, falseCount := 0, 0
	for i := 0; i < 50; i++ {
		sc := SessionContext{AcctSessionID: fmt.Sprintf("unique-sess-%d", i)}
		if ShouldUseSBA(sc, rate) {
			trueCount++
		} else {
			falseCount++
		}
	}
	if trueCount == 0 {
		t.Error("rate=0.5 over 50 sessions: never selected sba; expected some true values")
	}
	if falseCount == 0 {
		t.Error("rate=0.5 over 50 sessions: always selected sba; expected some false values")
	}
}

// TestShouldUseSBA_RateZeroNeverTrue verifies over many sessions that rate=0.0
// never produces true.
func TestShouldUseSBA_RateZeroNeverTrue(t *testing.T) {
	for i := 0; i < 200; i++ {
		sc := SessionContext{AcctSessionID: fmt.Sprintf("sess-zero-%d", i)}
		if ShouldUseSBA(sc, 0.0) {
			t.Errorf("rate=0.0: session %d returned true (expected always false)", i)
		}
	}
}

// TestShouldUseSBA_RateOneAlwaysTrue verifies over many sessions that rate=1.0
// always produces true.
func TestShouldUseSBA_RateOneAlwaysTrue(t *testing.T) {
	for i := 0; i < 200; i++ {
		sc := SessionContext{AcctSessionID: fmt.Sprintf("sess-one-%d", i)}
		if !ShouldUseSBA(sc, 1.0) {
			t.Errorf("rate=1.0: session %d returned false (expected always true)", i)
		}
	}
}

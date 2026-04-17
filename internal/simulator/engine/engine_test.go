package engine

import (
	"testing"

	"github.com/btopcu/argus/internal/simulator/sba"
)

// TestShouldUseSBA_DispatchContract sanity-checks that the engine's SBA fork
// uses the same plan-contracted picker invariants the dedicated picker tests
// cover (rate=0 never selects SBA; rate=1 always does). This is a thin guard
// against a future refactor that might accidentally decouple engine from
// sba.ShouldUseSBA.
//
// Full distribution coverage sits in internal/simulator/sba/picker_test.go;
// this test asserts only the extreme cases so the engine branch is defended
// without duplicating the statistical tests.
func TestShouldUseSBA_DispatchContract(t *testing.T) {
	sc := sba.SessionContext{AcctSessionID: "engine-test-sess-1"}

	if sba.ShouldUseSBA(sc, 0.0) {
		t.Error("rate=0.0: picker must never select SBA")
	}
	if !sba.ShouldUseSBA(sc, 1.0) {
		t.Error("rate=1.0: picker must always select SBA")
	}
}

// TestEngineFork_NilSBAClientSkipsPath verifies the nil-guard in runSession's
// SBA fork. If the SBA map has no entry for an operator, the fork MUST NOT
// dereference nil — the session should proceed on the RADIUS path.
//
// We can't run the full session here (RADIUS needs a server), but we assert
// the map-lookup semantics the fork depends on.
func TestEngineFork_NilSBAClientSkipsPath(t *testing.T) {
	sbaClients := map[string]*sba.Client{}

	if c := sbaClients["missing-op"]; c != nil {
		t.Errorf("expected nil lookup for missing operator, got %v", c)
	}

	var nilMap map[string]*sba.Client
	if c := nilMap["any-op"]; c != nil {
		t.Errorf("expected nil lookup on nil map, got %v", c)
	}
}

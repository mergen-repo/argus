package alertstate_test

import (
	"testing"

	"github.com/btopcu/argus/internal/alertstate"
	"github.com/google/uuid"
)

func TestDedupKey_Deterministic(t *testing.T) {
	tid := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	sim := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	k1 := alertstate.DedupKey(tid, "high_usage", "aaa", &sim, nil, nil)
	k2 := alertstate.DedupKey(tid, "high_usage", "aaa", &sim, nil, nil)
	if k1 != k2 {
		t.Fatalf("expected deterministic key, got %q vs %q", k1, k2)
	}
	if len(k1) != 64 {
		t.Fatalf("expected 64-char hex, got len=%d", len(k1))
	}
}

func TestDedupKey_DiffersByEntity(t *testing.T) {
	tid := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	id := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	kSim := alertstate.DedupKey(tid, "t", "s", &id, nil, nil)
	kOp := alertstate.DedupKey(tid, "t", "s", nil, &id, nil)
	kApn := alertstate.DedupKey(tid, "t", "s", nil, nil, &id)
	if kSim == kOp || kOp == kApn || kSim == kApn {
		t.Fatal("entity prefix must differentiate keys with the same UUID")
	}
}

func TestDedupKey_DoesNotIncludeSeverity(t *testing.T) {
	tid := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	k1 := alertstate.DedupKey(tid, "high_usage", "aaa", nil, nil, nil)
	k2 := alertstate.DedupKey(tid, "high_usage", "aaa", nil, nil, nil)
	if k1 != k2 {
		t.Fatal("severity is not a parameter; identical inputs must produce identical key")
	}
}

func TestDedupKey_DoesNotIncludeNil(t *testing.T) {
	tid := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	k1 := alertstate.DedupKey(tid, "t", "s", nil, nil, nil)
	k2 := alertstate.DedupKey(tid, "t", "s", nil, nil, nil)
	if k1 != k2 {
		t.Fatal("all-nil entity triple must be stable across calls")
	}
}

func TestTransitions_RejectsResolvedToAnything(t *testing.T) {
	targets := []string{alertstate.StateOpen, alertstate.StateAcknowledged, alertstate.StateResolved, alertstate.StateSuppressed}
	for _, to := range targets {
		if alertstate.CanTransition(alertstate.StateResolved, to) {
			t.Errorf("resolved is terminal; CanTransition(resolved, %q) must be false", to)
		}
	}
}

func TestTransitions_AllowsOpenToSuppressed(t *testing.T) {
	if !alertstate.CanTransition(alertstate.StateOpen, alertstate.StateSuppressed) {
		t.Fatal("open → suppressed must be allowed")
	}
}

func TestIsUpdateAllowed_RejectsSuppressed(t *testing.T) {
	if alertstate.IsUpdateAllowed(alertstate.StateSuppressed) {
		t.Fatal("suppressed must not be reachable via PATCH /alerts/{id}")
	}
	if !alertstate.IsUpdateAllowed(alertstate.StateAcknowledged) {
		t.Fatal("acknowledged must be allowed via PATCH")
	}
	if !alertstate.IsUpdateAllowed(alertstate.StateResolved) {
		t.Fatal("resolved must be allowed via PATCH")
	}
}

func TestValidate_AcceptsAllFour(t *testing.T) {
	for _, s := range alertstate.AllStates {
		if err := alertstate.Validate(s); err != nil {
			t.Errorf("Validate(%q) unexpected error: %v", s, err)
		}
	}
}

func TestValidate_RejectsUnknown(t *testing.T) {
	cases := []string{"", "pending", "OPEN", "Open"}
	for _, s := range cases {
		if err := alertstate.Validate(s); err == nil {
			t.Errorf("Validate(%q) expected error, got nil", s)
		}
	}
}

func TestIsActive(t *testing.T) {
	active := []string{alertstate.StateOpen, alertstate.StateAcknowledged, alertstate.StateSuppressed}
	for _, s := range active {
		if !alertstate.IsActive(s) {
			t.Errorf("IsActive(%q) expected true", s)
		}
	}
	if alertstate.IsActive(alertstate.StateResolved) {
		t.Fatal("IsActive(resolved) must be false")
	}
}

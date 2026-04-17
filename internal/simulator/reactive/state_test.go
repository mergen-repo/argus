package reactive

import (
	"sync"
	"testing"
	"time"
)

func TestState_String(t *testing.T) {
	tests := []struct {
		state SessionState
		want  string
	}{
		{StateIdle, "Idle"},
		{StateAuthenticating, "Authenticating"},
		{StateAuthenticated, "Authenticated"},
		{StateActive, "Active"},
		{StateBackingOff, "BackingOff"},
		{StateTerminating, "Terminating"},
		{StateSuspended, "Suspended"},
		{SessionState(99), "Unknown"},
	}
	for _, tc := range tests {
		if got := tc.state.String(); got != tc.want {
			t.Errorf("SessionState(%d).String() = %q, want %q", tc.state, got, tc.want)
		}
	}
}

func TestState_Transition_Valid(t *testing.T) {
	transitions := []struct {
		from SessionState
		to   SessionState
	}{
		{StateIdle, StateAuthenticating},
		{StateAuthenticating, StateAuthenticated},
		{StateAuthenticated, StateActive},
		{StateActive, StateTerminating},
		{StateActive, StateBackingOff},
		{StateBackingOff, StateSuspended},
	}

	for _, tc := range transitions {
		s := &Session{}
		s.State.Store(uint32(tc.from))
		if !s.Transition(tc.from, tc.to) {
			t.Errorf("Transition(%s → %s) returned false, want true", tc.from, tc.to)
		}
		if got := s.CurrentState(); got != tc.to {
			t.Errorf("after Transition(%s → %s): CurrentState() = %s, want %s", tc.from, tc.to, got, tc.to)
		}
	}
}

func TestState_Transition_Race(t *testing.T) {
	s := &Session{}

	const goroutines = 100
	results := make([]bool, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			results[i] = s.Transition(StateIdle, StateAuthenticating)
		}()
	}
	wg.Wait()

	trueCount := 0
	for _, r := range results {
		if r {
			trueCount++
		}
	}
	if trueCount != 1 {
		t.Errorf("exactly 1 goroutine should win the CAS, got %d", trueCount)
	}
	if got := s.CurrentState(); got != StateAuthenticating {
		t.Errorf("final state = %s, want Authenticating", got)
	}
}

func TestState_DeadlineRoundTrip(t *testing.T) {
	s := &Session{}
	now := time.Now().Truncate(time.Nanosecond)
	s.UpdateDeadline(now)
	got := s.CurrentDeadline()
	diff := got.Sub(now)
	if diff < 0 {
		diff = -diff
	}
	if diff > time.Nanosecond {
		t.Errorf("deadline round-trip diff = %v, want <= 1ns", diff)
	}
}

func TestState_DeadlineZero(t *testing.T) {
	s := &Session{}
	got := s.CurrentDeadline()
	if !got.IsZero() {
		t.Errorf("new Session deadline = %v, want zero Time", got)
	}
}

func TestState_DisconnectCause(t *testing.T) {
	s := &Session{}
	s.SetDisconnectCause(CauseDM)
	if got := s.CurrentDisconnectCause(); got != CauseDM {
		t.Errorf("DisconnectCause = %d, want CauseDM (%d)", got, CauseDM)
	}
}

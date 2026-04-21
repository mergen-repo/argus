package alertstate

import "errors"

const (
	StateOpen         = "open"
	StateAcknowledged = "acknowledged"
	StateResolved     = "resolved"
	StateSuppressed   = "suppressed"
)

var (
	AllStates    = []string{StateOpen, StateAcknowledged, StateResolved, StateSuppressed}
	ActiveStates = []string{StateOpen, StateAcknowledged, StateSuppressed}

	// UpdateAllowedStates: suppressed is NOT reachable via PATCH /alerts/{id} — preserve FIX-209 API contract.
	UpdateAllowedStates = map[string]bool{StateAcknowledged: true, StateResolved: true}

	Transitions = map[string]map[string]bool{
		StateOpen:         {StateAcknowledged: true, StateResolved: true, StateSuppressed: true},
		StateAcknowledged: {StateResolved: true, StateSuppressed: true},
		StateSuppressed:   {StateOpen: true, StateResolved: true},
		StateResolved:     {},
	}

	ErrInvalidAlertState      = errors.New("invalid alert state")
	ErrInvalidAlertTransition = errors.New("invalid alert state transition")
)

var allStatesSet = map[string]bool{
	StateOpen: true, StateAcknowledged: true, StateResolved: true, StateSuppressed: true,
}

var activeStatesSet = map[string]bool{
	StateOpen: true, StateAcknowledged: true, StateSuppressed: true,
}

func Validate(s string) error {
	if !allStatesSet[s] {
		return ErrInvalidAlertState
	}
	return nil
}

func IsActive(s string) bool {
	return activeStatesSet[s]
}

func CanTransition(from, to string) bool {
	targets, ok := Transitions[from]
	if !ok {
		return false
	}
	return targets[to]
}

func IsUpdateAllowed(s string) bool {
	return UpdateAllowedStates[s]
}

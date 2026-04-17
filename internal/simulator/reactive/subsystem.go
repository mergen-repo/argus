package reactive

import "github.com/btopcu/argus/internal/simulator/config"

// Subsystem bundles the reactive components together for ergonomic engine
// wiring. A nil pointer means "reactive disabled" — engine paths check for nil
// and fall through to non-reactive behaviour, preserving byte-identical
// pre-STORY-085 semantics.
type Subsystem struct {
	Cfg      config.ReactiveDefaults
	Rejects  *RejectTracker
	Registry *Registry
	Listener *Listener // may be nil when CoA listener is disabled
}

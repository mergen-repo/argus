// Package reactive implements STORY-085 approach-B behaviour for the
// operator simulator: a per-session state machine, reject back-off,
// CoA/DM listener, and Session-Timeout respect.
//
// State machine:
//
//	Idle ──Auth─► Authenticating ──Accept─► Authenticated ──Start─► Active ─┐
//	 ▲                  │                                                    │
//	 │                  Reject                                               │
//	 │                  ▼                                                    │
//	 └──cooldown── BackingOff ──max-retries─► Suspended                      │
//	                                                                         │
//	 ┌────────── Terminating ◄── DM / deadline / scenario-end ───────────────┘
//
// Single-writer invariant (PAT-001): the engine is the sole writer of
// SimulatorReactiveTerminationsTotal. Listeners and trackers return
// sentinel signals; the engine classifies and emits metrics.
package reactive

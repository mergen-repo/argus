# FIX-107 Plan: Resume State Machine Consistency

> S effort | 3 tasks | Pre-release bugfix

## Root Cause

`store.Resume` calls `validateTransition(currentState, "active")` which passes for both
`suspended` and `ordered` states (the global `validTransitions` map allows both).
The Resume handler lacks the IP allocation + policy auto-assign logic that Activate has,
so `ordered → active` via Resume produces an inconsistent SIM (no policy, no IP).

## AC-1 Decision: Option (a) — Restrict Resume to `suspended → active`

**Rationale:**
- G-015 treats `ordered → active` as a distinct Activate path ("bulk import auto-activate")
- Resume semantically means "un-suspend" — an `ordered` SIM has never been active
- Option (b) would duplicate Activate's IP + policy logic into Resume; FIX-102 policy-resolver
  factoring is not yet landed, making duplication premature
- Option (a) is a single guard in one function — truly S effort

**Not changed:** the global `validTransitions` map stays as-is because `Activate`, `TransitionState`
(bulk import/bulk state change), and `RestoreState` all legitimately use `ordered → active`.

## Tasks

### Task 1: Store — Guard Resume to suspended-only
**Files:** `internal/store/sim.go`
- Add explicit pre-check in `Resume`: `if currentState != "suspended" { return ErrInvalidStateTransition }`
  before calling `validateTransition`
- This makes Resume reject `ordered`, `active`, `stolen_lost`, `terminated`, `purged` — all non-suspended
- `validateTransition` call stays as secondary guard (belt + suspenders)
- Unit tests (AC-3 + AC-4):
  - `suspended → active` via Resume: allowed
  - `ordered → active` via Resume: rejected with `ErrInvalidStateTransition`
  - All other states via Resume: rejected

### Task 2: Handler — Error message + test coverage
**Files:** `internal/api/sim/handler.go`
- No handler code change needed — the existing error mapping already returns
  `422 INVALID_STATE_TRANSITION` with `"Cannot resume SIM in '%s' state"` when
  `ErrInvalidStateTransition` is returned
- Verify via handler-level test that POST /sims/{id}/resume on an `ordered` SIM
  returns 422 (not 200)

### Task 3: Docs — State machine spec + UAT update
**Files:** `docs/architecture/PROTOCOLS.md` (or state machine section), `docs/UAT.md`
- Document the Resume transition rule: `suspended → active` only
- Document the Activate transition rule: `ordered → active` (with IP + policy auto-assign)
- UAT-003 Step 6: flip to negative test — Resume on `ordered` SIM returns 422

## Regression Risk

**Low** — single guard addition in one store function. No shared transition matrix changed.
`TransitionState` (bulk import) and `Activate` paths are unaffected.

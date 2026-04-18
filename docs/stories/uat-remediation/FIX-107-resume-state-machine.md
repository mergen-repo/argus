# FIX-107: Resume State Machine Consistency

> Tier 4 (low impact) — `POST /sims/{id}/resume` accepts `ordered→active`
> transition but the Resume code path does not call IP allocation or
> policy auto-assign. Under STORY-092 dynamic IP, this may no longer be
> user-visible for IP, but policy still matters.

## User Story

As a SIM Manager, whether I transition a SIM to active via Activate or
Resume, the resulting SIM has consistent downstream state — policy
assigned (if APN has default), audit events written, and state machine
semantics uniform.

## Source Finding

- UAT Batch 1 report: `docs/reports/uat-acceptance-batch1-2026-04-18.md` — **F-17 HIGH**
- Evidence: `POST /sims/{id}/resume` on an `ordered` SIM returns 200 with `state=active`, but `policy_version_id` stays NULL
- Observation: the `validateTransition` function allows `ordered → active` via Resume (not just from `suspended`), giving Resume a second meaning
- Related inline fix already applied: Activate/Resume pass distinct `reason` labels to `sim_state_history` (commit 7175e53)

## Acceptance Criteria

- [ ] AC-1: Plan triages two options and picks one with advisor consult:
  - **(a)** Resume is restricted to `suspended → active` only. `ordered → active` requires the explicit Activate endpoint. Returns `422 INVALID_STATE_TRANSITION` otherwise.
  - **(b)** Resume remains permissive for `ordered → active` but the handler calls the same policy auto-assign (and IP allocation if still relevant per STORY-092) that Activate calls.
- [ ] AC-2: Whichever option is chosen, state-machine semantics are documented in `docs/architecture/PROTOCOLS.md` or wherever SIM state transitions are specified, with the single source of truth referenced from both handlers
- [ ] AC-3: Unit test covers every allowed transition including the explicit choice made in AC-1
- [ ] AC-4: Unit test covers every rejected transition returning `422 INVALID_STATE_TRANSITION` with the actionable code
- [ ] AC-5: If option (b) is chosen, integration test asserts `policy_version_id` is set after Resume from ordered
- [ ] AC-6: UAT-003 Step 6 expectations updated in `docs/UAT.md` to match the chosen semantics — if (a), step 6 becomes a negative test; if (b), step 6 asserts policy assigned
- [ ] AC-7: Regression: UAT-003 passes end-to-end with the refined step 6 expectation

## Out of Scope

- IP allocation logic (STORY-092 owns this)
- Other state transitions (suspend, terminate, report-lost)

## Dependencies

- Blocked by: FIX-105 (if option (b) chosen and IP allocation is in scope), FIX-102 policy-resolver factoring (shared helper)
- Blocks: UAT-003 rerun

## Architecture Reference

- Store: `internal/store/sim.go` — `Resume` (~line 458), `Activate` (~line 367), `validateTransition`
- Handler: `internal/api/sim/handler.go` — `Resume` (line 985), `Activate`
- State history insert: `insertStateHistory` (line 686)
- Related: STORY-011 (SIM CRUD)

## Test Scenarios

- [ ] Unit: transition matrix exhaustive
- [ ] Integration: Resume from suspended → policy retained, IP retained
- [ ] Integration (option b only): Resume from ordered → policy assigned
- [ ] Regression: UAT-003 step 6

## Effort

S — state-machine cleanup, one handler, one store function, docs update. One Dev iteration.

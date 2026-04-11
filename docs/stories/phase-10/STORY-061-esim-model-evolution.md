# STORY-061: eSIM Model Evolution

## User Story
As an eSIM operations engineer, I want the eSIM data model to properly represent multi-profile SIMs and a real `available` state (distinct from `disabled`), so that Argus can manage modern eSIM devices that hold multiple profiles simultaneously.

## Description
STORY-028 merged `available` into `disabled` and enforced `UNIQUE(sim_id)` limiting each SIM to a single profile — both deliberate pragmatic deviations documented in DEV-088. Real eSIM SIMs hold multiple profiles with exactly one `enabled` at a time. This story evolves the data model to match that reality.

## Architecture Reference
- Services: SVC-03 (Core API), SVC-06 (Operator)
- Packages: internal/esim, internal/store/esim, internal/api/esim, migrations
- Source: docs/stories/phase-5/STORY-028-review.md (DEV-088 deviation), docs/architecture/db/_index.md (TBL-12)

## Screen Reference
- SCR-072 (eSIM Profiles List — show multiple profiles per SIM)
- SCR-073 (eSIM Profile Detail — state + switch flow)

## Acceptance Criteria
- [ ] AC-1: Profile state machine adds `available` state as distinct from `disabled`.
  - `available`: loaded on SIM, inactive, eligible to be enabled
  - `disabled`: explicitly deactivated by operator, requires manual re-enable
  - `enabled`: currently active (only one per SIM)
  - `deleted`: soft-deleted
  - State transitions: `available → enabled → disabled → available` (and `enabled → available` via switch). Migration converts existing `disabled` rows: if SIM has never had an active profile, remain `disabled`; if exists as part of a switch history, set to `available`.
- [ ] AC-2: Multi-profile schema change. `esim_profiles` constraint `UNIQUE(sim_id)` relaxed to partial unique: `UNIQUE(sim_id) WHERE state = 'enabled'`. Multiple `available`/`disabled` rows per SIM allowed. Non-partial unique on `(sim_id, profile_id)` added.
- [ ] AC-3: Profile Switch post-handler completes the flow:
  - Disable old profile (set `state=disabled`)
  - Enable new profile (set `state=enabled`)
  - Trigger CoA/DM if active session exists (via STORY-060 AC-6 integration)
  - Reallocate IP for new APN (via `internal/ipam` allocator)
  - Reassign policy (via `internal/policy` lookup) based on new operator + SIM segment
  - All 4 steps atomic via transaction + post-commit side effects. On failure: rollback profile state, leave session intact.
- [ ] AC-4: eSIM UI updates to show multi-profile view. Profile list per SIM on SIM Detail eSIM tab. "Load profile" action adds a new `available` profile. "Enable" switches active profile. "Disable" sets to `disabled`. "Delete" soft-deletes (only when not `enabled`). 5 new error codes and API contract updates in ERROR_CODES.md.

## Dependencies
- Blocked by: STORY-057 (needs working SIM detail), STORY-060 AC-6 (CoA/DM trigger for Profile Switch)
- Blocks: Phase 10 Gate

## Test Scenarios
- [ ] Migration: Existing `disabled` rows migrated correctly based on history. Forward + backward migration tested.
- [ ] Integration: Load 3 profiles for a SIM → all in `available` state, 3 rows exist. Enable profile B → state becomes `enabled`, A and C remain `available`.
- [ ] Integration: Switch from B to C → B becomes `disabled` (or `available` per spec — decide via AC-1 state rules), C becomes `enabled`. Session CoA dispatched. IP reallocated. Policy re-resolved.
- [ ] Integration: Unique constraint prevents two `enabled` profiles on same SIM (insert fails with constraint violation).
- [ ] Integration: Delete `available` profile → soft delete succeeds. Delete `enabled` profile → 409 Conflict error.
- [ ] E2E: SIM detail eSIM tab shows profile list with state badges. "Load profile" modal adds new profile, "Enable" switches active.

## Effort Estimate
- Size: M
- Complexity: Medium (schema migration + handler refactor, CoA/IP/policy reallocation composition)

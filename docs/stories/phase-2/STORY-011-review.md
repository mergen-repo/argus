# Post-Story Review: STORY-011 — SIM CRUD & State Machine

> Date: 2026-03-20

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| STORY-012 | SIM Segments depend on SIMStore.List for filtered queries. `ListSIMsParams` struct and cursor pagination pattern are directly reusable. No blockers. | NO_CHANGE |
| STORY-013 | Bulk SIM Import already partially implemented in `internal/job/import.go` using SIMStore.Create, TransitionState, InsertHistory, SetIPAndPolicy. The import.go file was updated during STORY-011 for the new Create signature (tenantID param). Ready to proceed. | NO_CHANGE |
| STORY-014 | MSISDN Pool Management depends on SIM store for assignment. SIM.MSISDN field is already a nullable column. No blockers. | NO_CHANGE |
| STORY-015 | RADIUS auth will need SIM lookup by IMSI (hot path). SIMStore currently has no `GetByIMSI` method. Will need to be added in STORY-015 or a prep task. Not a blocker since STORY-015 is in Phase 3. | NO_CHANGE |
| STORY-039 | Compliance Reporting & Auto-Purge depends on the purge_at field set by Terminate. The `terminated -> purged` transition is defined in validTransitions but no Purge store method exists yet. Will need implementation in STORY-039. | NO_CHANGE |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| decisions.md | DEV-027 to DEV-030, PERF-005, PERF-006 already recorded (6 entries) | NO_CHANGE (already current) |
| GLOSSARY.md | Added "SIM State Machine" and "Auto-Purge" terms | UPDATED |
| ARCHITECTURE.md | No changes needed | NO_CHANGE |
| SCREENS.md | No changes (backend-only story) | NO_CHANGE |
| FRONTEND.md | No changes (backend-only story) | NO_CHANGE |
| FUTURE.md | No changes | NO_CHANGE |
| Makefile | No changes needed (no new services/targets) | NO_CHANGE |
| CLAUDE.md | No changes needed (no port/URL changes) | NO_CHANGE |
| ROUTEMAP.md | STORY-011 marked DONE, progress 20%, next story STORY-012, changelog entry added | UPDATED |
| ERROR_CODES.md | Fixed file reference: `internal/gateway/errors.go` -> `internal/apierr/apierr.go` (pre-existing error) | UPDATED |

## Cross-Doc Consistency

- Contradictions found: 2 (1 pre-existing, 1 known/accepted)

### Issue 1: PRODUCT.md BR-1 vs Code — stolen_lost -> terminated transition
- **Type:** Known deviation
- **Location:** `internal/store/sim.go:89` (`validTransitions["stolen_lost"] = {}`) vs `docs/PRODUCT.md` BR-1 table
- **Detail:** PRODUCT.md BR-1 defines STOLEN/LOST -> TERMINATED as a valid transition. Code does not implement it.
- **Status:** ACCEPTED (DEV-029) — Story AC only specifies ACTIVE/SUSPENDED -> TERMINATED. Can be added later.

### Issue 2: API Index references 3 unimplemented endpoints to STORY-011
- **Type:** Minor inconsistency
- **Location:** `docs/architecture/api/_index.md` — API-035, API-043, API-053
- **Detail:** API-035 (GET /api/v1/apns/:id/sims), API-043 (PATCH /api/v1/sims/:id), API-053 (POST /api/v1/sims/compare) are listed in the API index with "See STORY-011" but were not in the story's API Contract or Acceptance Criteria, and were not implemented.
- **Status:** NEEDS_ATTENTION — These should be reassigned to future stories or marked as deferred in the API index. API-043 could go to a future SIM metadata update story. API-053 is a comparison feature likely for frontend stories. API-035 can be added when the APN detail page needs it.

### Issue 3: ERROR_CODES.md file reference (pre-existing)
- **Type:** Incorrect reference
- **Location:** `docs/architecture/ERROR_CODES.md` line 5
- **Detail:** Referenced `internal/gateway/errors.go` which does not exist. Actual file is `internal/apierr/apierr.go`.
- **Status:** FIXED

### Issue 4: apierr.go has MSISDN error codes not in ERROR_CODES.md
- **Type:** Minor inconsistency (pre-existing)
- **Location:** `internal/apierr/apierr.go` lines 27-28 (`CodeMSISDNNotFound`, `CodeMSISDNNotAvailable`)
- **Detail:** These codes exist in apierr.go but are not documented in ERROR_CODES.md. They were likely added as forward declarations for STORY-014 (MSISDN Pool).
- **Status:** NEEDS_ATTENTION — Should be documented when STORY-014 is implemented.

## Implementation Quality Notes

### Strengths
- Clean separation: Store (data access) -> Handler (HTTP + orchestration) -> Router (wiring) -> Main (DI)
- All state transitions use SELECT FOR UPDATE to prevent concurrent mutation races
- IP allocation rollback on activation failure (handler orchestrates allocate-then-activate with compensating release)
- Comprehensive validation: ICCID max 22, IMSI max 15, enum checks for sim_type and rat_type
- Gate caught and fixed SQL injection risk in TransitionState + missing updated_at timestamps
- Test coverage: 8 store tests + 9 handler tests covering struct fields, state machine validation, response conversion, input validation

### Observations
- `TransitionState` and `InsertHistory` are public convenience methods used by `job/import.go` for bulk import. They bypass tenant scoping intentionally (documented in DEV-028).
- `SetIPAndPolicy` method does not set `updated_at = NOW()` — minor gap, but only used by import flow.
- The `stubs.go` file was properly deleted, no dangling references.

## Project Health

- Stories completed: 11/55 (20%)
- Current phase: Phase 2 — Core SIM & APN
- Next story: STORY-012 — SIM Segments & Group-First UX
- Blockers: None
- Phase 2 progress: 3/6 stories done (STORY-009, STORY-010, STORY-011)

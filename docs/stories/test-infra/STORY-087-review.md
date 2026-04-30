# Post-Story Review: STORY-087 — [TECH-DEBT] D-032 Pre-069 sms_outbound Shim

> Date: 2026-04-17

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| STORY-088 | D-033 `go vet` non-pointer Unmarshal fix — completely orthogonal to migrations. No dependency on STORY-087 migration files or test harness. | REPORT ONLY — NO_CHANGE |
| Mini Phase Gate | Fresh-volume bootstrap now clean; gate can verify `make down && make up && make db-migrate` succeeds end-to-end. Gate note: empirical run will hit D-037 (TimescaleDB columnstore blocker) until that is resolved. | REPORT ONLY |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| `docs/ROUTEMAP.md` | STORY-087 row: `[~] IN PROGRESS → [x] DONE`. D-032: `[ ] PENDING → ✓ RESOLVED (2026-04-17)`. D-037 row added (TimescaleDB columnstore). Change Log: 2 rows added (DONE + REVIEW). | UPDATED |
| `docs/USERTEST.md` | Added `## STORY-087:` section (5 scenarios: fresh-volume bootstrap, FK check, trigger/index/RLS, live-DB no-op sentinel, down-chain). | UPDATED |
| `docs/architecture/db/_index.md` | D-032 remediation chain bullet at line 166 already added by developer Task 7 — verified correct. | NO_CHANGE |
| `docs/brainstorming/decisions.md` | DEV-243 entry at line 470 already added by gate fix F-A1 — verified content matches gate claim. | NO_CHANGE |
| `docs/ARCHITECTURE.md` | No new packages, APIs, tables, or screens — scale line unchanged. | NO_CHANGE |
| `docs/GLOSSARY.md` | No new domain terms introduced. Migration/FK/RLS/trigger terminology already covered. | NO_CHANGE |
| `docs/FUTURE.md` | No new opportunities or invalidations. | NO_CHANGE |
| `Makefile` | No new targets or services. | NO_CHANGE |
| `CLAUDE.md` | No Docker URL/port changes. | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 0
- `db/_index.md` TBL-42 description already mentions the D-032 remediation chain, consistent with the new shim at `20260412999999` and STORY-086's repair at `20260417000004`.
- `decisions.md` DEV-243 correctly references golang-migrate v4.19.1 `readUp()` forward-only iteration and `CREATE TABLE IF NOT EXISTS` neutralisation semantics — consistent with plan and gate.
- STORY-087 row in Tech Debt Cleanup track now DONE; D-032 in Tech Debt table now RESOLVED — both consistent with gate PASS.

## Decision Tracing

- Decisions checked: 1 (DEV-243 — new this story)
- Orphaned (approved but not applied): 0
- DEV-243 reflected in: `migrations/20260412999999_story_087_sms_outbound_pre_069_shim.up.sql` (shim up-side comment block citing D-032/DEV-239/STORY-086); `internal/store/migration_freshvol_test.go` (`TestFreshVolumeBootstrap_STORY087` test name + skip comment); `docs/architecture/db/_index.md` line 166 (remediation chain bullet).

## USERTEST Completeness

- Entry exists: YES (added by this review)
- Type: Backend note with 5 scenarios covering fresh-volume bootstrap, FK validation, trigger/index/RLS verification, live-DB no-op sentinel check, and down-chain verification.

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting this story: 1 (D-032)
- Already ✓ RESOLVED by Gate: 0 (gate did not flip D-032 status — that is Reviewer scope)
- Resolved by Reviewer (Gate missed marking): 1 — D-032 flipped to `✓ RESOLVED (2026-04-17)` in ROUTEMAP Tech Debt table
- NOT addressed (CRITICAL): 0

## Mock Status

- N/A — not a Frontend-First project. No `src/mocks/` directory involved. This story is pure DDL + Go integration tests.

## Issues

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | D-032 Tech Debt row not yet marked RESOLVED in ROUTEMAP (gate leaves that to Reviewer) | NON-BLOCKING | FIXED | Flipped `[ ] PENDING` → `✓ RESOLVED (2026-04-17)` in ROUTEMAP Tech Debt table. |
| 2 | STORY-087 row still `[~] IN PROGRESS \| Review` — not flipped to DONE | NON-BLOCKING | FIXED | Row updated to `[x] DONE \| — \| 2026-04-17`. |
| 3 | TimescaleDB 2.26.2 columnstore/RLS DDL incompatibility (pre-existing, surfaces in gate empirical run) has no ROUTEMAP tracking entry | NON-BLOCKING | FIXED | Added D-037 to ROUTEMAP Tech Debt table. Target: POST-GA test-infra. Operator note: the pinned `timescale/timescaledb:latest-pg16@sha256:...` image in `deploy/docker-compose.yml` is fixed-digest, so all operators encounter the same version. |
| 4 | USERTEST.md missing STORY-087 section | NON-BLOCKING | FIXED | Added `## STORY-087:` section with 5 backend scenarios (see Documents Updated). |

## Project Health

- Stories completed: Test Infra track 5/5 DONE; Tech Debt track 1/2 (STORY-087 DONE, STORY-088 PENDING)
- Current phase: Test Infrastructure + Tech Debt Cleanup
- Next story: STORY-088 — [TECH-DEBT] D-033 `go vet` non-pointer Unmarshal fix (XS)
- Blockers: None

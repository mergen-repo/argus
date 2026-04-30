# Post-Story Review: STORY-086 — [AUDIT-GAP] Restore missing `sms_outbound` table + boot-time schema-integrity check

> Date: 2026-04-17

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| Documentation Phase (D1-D4) | STORY-086 is the final Phase-10 story. All 24/24 Phase-10 stories are now DONE (pending Ana Amil commit step). Documentation Phase is fully unblocked. | NO_CHANGE to story files — REPORT ONLY |
| STORY-083/084/085 (Test-Infra track) | Independent track; no impact from STORY-086. Still sequenced STORY-080→082 done; 083/084 PENDING. | NO_CHANGE |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| `docs/stories/phase-10/STORY-086-review.md` | This file — created | CREATED |
| `docs/USERTEST.md` | Appended `## STORY-086:` section (4 test blocks: DB probe before/after, API smoke, trigger rejection, boot-check FATAL demo) | UPDATED |
| `docs/brainstorming/decisions.md` | Appended PAT-004 to Bug Patterns section (FK-to-partitioned-parent pattern) | UPDATED |
| `docs/ARCHITECTURE.md` | Added `schemacheck/` subpackage line to `internal/store/` tree | UPDATED |
| `docs/GLOSSARY.md` | No changes — see Check #4 for rationale | NO_CHANGE |
| `docs/ROUTEMAP.md` | No changes required by Reviewer — tech debt table already correct per Gate | NO_CHANGE |
| `docs/architecture/db/_index.md` | Audit caveat removed by Developer (Task 5) — verified present | NO_CHANGE |
| `docs/architecture/CONFIG.md` | No new env vars — no change needed | NO_CHANGE |
| `docs/architecture/PROTOCOLS.md` | N/A | NO_CHANGE |
| `docs/architecture/TESTING.md` | N/A | NO_CHANGE |
| `docs/FUTURE.md` | N/A | NO_CHANGE |
| `docs/SCREENS.md` | N/A (no UI) | NO_CHANGE |
| `docs/PRODUCT.md` | F-055 was already marked COVERED; repair migration restores the functionality | NO_CHANGE |
| `Makefile` | No new Make targets needed (plan noted `make smoke-test` absent; ad-hoc curl documented in USERTEST) | NO_CHANGE |
| `CLAUDE.md` | No Docker ports/URLs changed | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 0
- TBL-42 in `docs/architecture/db/_index.md` line 52 now reads the root-cause summary (repair migration note + trigger enforcement) — matches DEV-239 and gate report. Caveat removed.
- `internal/store/schemacheck/` appears in ARCHITECTURE.md project tree — consistent with actual directory at `internal/store/schemacheck/schemacheck.go`.
- ROUTEMAP D-025: `✓ RESOLVED (2026-04-17)` with repair migration filename — correct.
- ROUTEMAP D-032: `[ ] PENDING` (fresh-volume latent bug in 20260413000001) — correct, not in scope for this story.
- ROUTEMAP D-033: `[ ] PENDING` (pre-existing `go vet` hit in `internal/policy/dryrun/service_test.go:333`) — correct deferral, out of scope.
- ROUTEMAP STORY-086 status still `[~] IN PROGRESS` — intentional, Ana Amil flips to DONE at commit step.
- Phase counter `23/24` — intentional, same reasoning.

## Decision Tracing

- Decisions checked: DEV-239 (the story's primary decision)
- DEV-239 row content verified: includes FK-to-partitioned-sims root cause, trigger addition (gate F-A1/F-A3 fix), handler-layer belt-and-suspenders, RLS clarification (pre-existed in 20260413000002), manifest count correction (13→12), fresh-volume follow-up (D-032), smoke note. Wording updated in-gate (F-A2+F-A7) to correct "application code enforces" → "DB trigger enforces + handler validates". Final ACCEPTED status intact.
- Orphaned approved decisions: 0

## USERTEST Completeness

- Entry exists: YES (appended by Reviewer this cycle)
- Type: backend/altyapi — 4 blocks: (a) before/after DB probe, (b) API smoke curl, (c) trigger rejection demonstration with bogus sim_id → FK violation SQLSTATE, (d) boot-check FATAL demo (drop table → restart → FATAL log → remigrate → clean boot)

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting STORY-086: 1 (D-025)
- Already `✓ RESOLVED` by Gate: 1 — D-025 (`sms_outbound` absent from live PG). Gate verified repair migration applied, `to_regclass` non-null, `GET /api/v1/sms/history` returns HTTP 200.
- Resolved by Reviewer (Gate missed marking): 0
- NOT addressed (CRITICAL): 0

New items added by Developer/Gate (not targeting this story):
- D-032: STORY-086 DEV-239 fresh-volume latent bug — `[ ] PENDING` targeting follow-up story. Correct.
- D-033: pre-existing `go vet` in `internal/policy/dryrun/service_test.go:333` — `[ ] PENDING` targeting follow-up story. Correct (out of STORY-086 scope per Phase 10 policy exception for pre-existing items).

## Mock Status

N/A — no `src/mocks/` directory. No mock retirement applicable.

## 14-Check Detail Table

| # | Check | Status | Evidence / Fix |
|---|-------|--------|----------------|
| 1 | Next-story impact | PASS | STORY-086 is last Phase-10 story. Documentation Phase (D1-D4) fully unblocked. Test-Infra track (083/084/085) independent. No assumptions changed for upcoming stories. REPORT ONLY — no story edits made. |
| 2 | Architecture evolution (`internal/store/schemacheck` placement) | PASS | `internal/store/schemacheck` is a defensible location: the package's sole dependency is `pgxpool.Pool` (already the store layer's pool type); probing DB schema is semantically consistent with `internal/store`. Moving to `internal/bootcheck/` would be a personal-preference refactor, not a correctness fix. ARCHITECTURE.md project tree updated to list the new subpackage (`│   │   └── schemacheck/`). |
| 3 | Docs: ARCHITECTURE.md, API index, DB index, SCREENS, PRODUCT | UPDATED | ARCHITECTURE.md: `schemacheck/` subpackage added to `internal/store/` tree. Scale header `241 APIs, 51 tables` unchanged — sms_outbound was already counted as TBL-42; no new APIs introduced. TBL-42 in `db/_index.md` line 52: audit caveat removed by Developer, replaced with root-cause summary (repair migration + trigger enforcement) — verified correct. CONFIG.md: no new env vars. PROTOCOLS/TESTING/FUTURE/PRODUCT: no changes needed. |
| 4 | GLOSSARY: new terms? | PASS | Two candidates evaluated: (a) "Boot-time schema integrity check" — implementation detail of the schemacheck package, not a domain/business term; excluded per project convention (same rationale as "dashboard cache invalidator" and "sessions counter reconciler" in STORY-062 review). (b) "check_sim_exists trigger" — a DB enforcement mechanism first introduced by STORY-064/DEV-169 with no glossary entry; STORY-086 leans on it, but it remains an infrastructure-level implementation detail, not a domain vocabulary term operators or developers need to look up in a glossary. Excluded. No GLOSSARY changes needed. |
| 5 | decisions.md: DEV-239 accuracy | PASS | DEV-239 verified present at line 459. Content is accurate and complete: FK-to-partitioned-`sims(id)` root cause, reproduction via `psql`, STORY-064 trigger precedent, repair via 20260417000004 (no FK + trigger + RLS), handler-layer belt-and-suspenders, fresh-volume follow-up (D-032), manifest count correction (dispatch said 13; actual 12), smoke note. In-gate fix (F-A2+F-A7) correctly updated wording from "application code enforces" to "DB trigger enforces". Single `| ACCEPTED |` pipe terminator confirmed. |
| 6 | USERTEST completeness | UPDATED | No STORY-086 section existed. Appended `## STORY-086:` section to USERTEST.md with: (a) before/after DB probe (`to_regclass`, `schema_migrations` state), (b) API smoke curl (`GET /api/v1/sms/history` → HTTP 200 + `status: "success"`), (c) trigger rejection (`INSERT` with bogus sim_id → FK violation SQLSTATE, `pg_trigger` confirmation), (d) boot-check FATAL demo (drop table → `docker compose restart argus` → grep for FATAL log → remigrate → clean boot). Follows project Turkish-language USERTEST convention. |
| 7 | ROUTEMAP Tech Debt | PASS | D-025: `✓ RESOLVED (2026-04-17)` with repair migration 20260417000004 and boot-time schemacheck guard — verified correct (Gate applied). D-032: `[ ] PENDING` (fresh-volume latent bug) — correct. D-033: `[ ] PENDING` (pre-existing `go vet`) — correct deferral. D-027 duplicate collision fixed in-gate (renumbered 086 follow-up from duplicate D-027 to D-032). No new D-NNN collisions. STORY-086 row status still `[~] IN PROGRESS` — correct until Ana Amil commit step. |
| 8 | Next-story unblock | PASS | STORY-086 is the 24th and final Phase-10 story. Phase 10 completion unblocks the Documentation Phase (D1 Specification, D2 Presentations, D3 Rollout Guide, D4 User Guide). No story-file edits needed — REPORT ONLY. |
| 9 | Tests | PASS | Gate-verified: full Go suite 2879/2879 PASS (0 regressions). Schemacheck unit tests 3/3 PASS (TestVerify_EmptyManifestSucceeds, TestCriticalTables_CountAndOrder, TestVerify_MissingTableReportsError added in-gate for F-A6). SMS store integration suite 19/19 PASS (includes new TestSmsOutbound_RelationPresentAfterMigrations). `go vet ./internal/store/schemacheck/...` PASS. |
| 10 | Bug patterns — new PAT-NNN | UPDATED | PAT-004 added to `docs/brainstorming/decisions.md` Bug Patterns section: "Never declare a FK `REFERENCES parent(id)` against a LIST/RANGE-partitioned table unless the FK columns exactly match a UNIQUE/PK constraint on the parent." Root cause = PG rejects partial-column FK refs to partitioned tables. Prevention = use `check_X_exists` BEFORE trigger (STORY-064/DEV-169 precedent). Affected = any future migration referencing `sims`, `sessions`, `cdrs`, `sim_state_history`, `operator_health_logs`, or any future partitioned table. REPORT ONLY for further analysis; fix already applied in STORY-064 and now propagated correctly to STORY-086. |
| 11 | ERROR_CODES.md | PASS | No new error codes introduced. The `check_sim_exists` trigger raises with `ERRCODE = 'foreign_key_violation'` (SQLSTATE 23503). This SQLSTATE is already handled in the existing error mapping. ERROR_CODES.md requires no changes. |
| 12 | CLAUDE.md | PASS | No Docker ports/URLs changed. No new services added. CLAUDE.md remains accurate. |
| 13 | Compliance: RLS + tenant_id | PASS | Repair migration `20260417000004_sms_outbound_recover.up.sql` mirrors `20260413000002_story_069_rls.up.sql:34-38` exactly: `ENABLE ROW LEVEL SECURITY`, `FORCE ROW LEVEL SECURITY`, `DROP POLICY IF EXISTS sms_outbound_tenant_isolation`, `CREATE POLICY sms_outbound_tenant_isolation USING (tenant_id = current_setting('app.current_tenant', true)::uuid)`. RLS-on-sms_outbound pre-existed in 20260413000002 (dispatch errata noted and corrected in plan + DEV-239). `tenant_id` column is `NOT NULL REFERENCES tenants(id)` in DDL. |
| 14 | Frontend (mock sweep) | PASS (N/A) | SKIPPED_NO_UI — Gate Scout-UI found zero `web/*.tsx` diff hits. SCR-132 (`/sms`) renders correctly on `GET /api/v1/sms/history` HTTP 200 via existing FE code. No mocks to retire. |

## Issues

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | USERTEST.md missing STORY-086 section | NON-BLOCKING | FIXED | Appended `## STORY-086:` section with 4 test blocks (DB probe, API smoke, trigger rejection, boot-check FATAL demo). |
| 2 | ARCHITECTURE.md project tree missing `schemacheck/` subpackage | NON-BLOCKING | FIXED | Added `│   │   └── schemacheck/` line under `internal/store/` in project structure listing. |
| 3 | PAT-004 (FK-to-partitioned-parent pattern) not captured | NON-BLOCKING | FIXED | Appended PAT-004 to Bug Patterns section in `docs/brainstorming/decisions.md`. |

## Project Health

- Stories completed: 55/55 original + 24/24 Phase-10 = all dev stories done (STORY-086 pending commit step)
- Current phase: Phase 10 — 23/24 (STORY-086 Gate PASS; Reviewer PASS; Ana Amil commit step pending)
- Next story: Documentation Phase D1 (Specification) — unblocked by STORY-086 completion
- Blockers: None — STORY-086 is the final Phase-10 blocker. All gate findings resolved or correctly deferred (D-032 fresh-volume latent bug, D-033 pre-existing vet).

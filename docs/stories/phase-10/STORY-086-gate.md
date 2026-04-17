# Gate Report: STORY-086 — [AUDIT-GAP] Restore missing `sms_outbound` table + boot-time schema-integrity check

## Summary

- **Requirements Tracing**: 5/5 ACs implemented (AC-1 root cause, AC-2 repair migration, AC-3 boot check, AC-4 regression test, AC-5 doc sync)
- **Gap Analysis**: 5/5 acceptance criteria passed
- **Compliance**: COMPLIANT (matches STORY-064/DEV-169 `check_sim_exists` trigger precedent after gate fix)
- **Tests**: full suite 2879/2879 PASS (0 regressions); schemacheck 3/3 PASS (including new F-A6 failure-path test); sms store integration 19/19 PASS
- **Test Coverage**: failure-path test for `Verify` added in-gate; trigger-level sim FK enforcement functionally verified against live DB
- **Performance**: N/A (migration + boot check; 12 trivial `to_regclass` calls ≈ 12-24 ms boot cost — accepted)
- **Build**: PASS (`go build ./...`)
- **Screen Mockup Compliance**: N/A (no UI story — SCR-132 `/sms` renders correctly on `GET /api/v1/sms/history` HTTP 200 via existing FE code)
- **UI Quality**: N/A (no FE changes)
- **Token Enforcement**: N/A
- **Turkish Text**: N/A
- **Overall**: PASS

## Team Composition

- Analysis Scout: 7 findings (F-A1..F-A7)
- Test/Build Scout: 1 finding (F-B1 — pre-existing, out-of-scope)
- UI Scout: 0 findings (SKIPPED_NO_UI verdict — no web/*.tsx diff hits)
- De-duplicated: 8 → 7 actionable findings (F-A2 and F-A7 overlap — merged into single fix)

## Fixes Applied

| # | Finding | Category | File | Change | Verified |
|---|---------|----------|------|--------|----------|
| 1 | F-A1+F-A3 | Compliance (DB integrity) | `migrations/20260417000004_sms_outbound_recover.up.sql` | Appended `CREATE OR REPLACE FUNCTION check_sim_exists()` (verbatim from `20260412000007_fk_integrity_triggers.up.sql:4-16`) + `DROP TRIGGER IF EXISTS trg_sms_outbound_check_sim ON sms_outbound; CREATE TRIGGER trg_sms_outbound_check_sim BEFORE INSERT OR UPDATE OF sim_id ...`. Mirrors STORY-064/DEV-169 precedent (`esim_profiles`/`ip_addresses`/`ota_commands`). Idempotent via DROP IF EXISTS. | Applied to live DB via `docker cp` + `psql -f`; bogus sim_id INSERT rejected with `FK violation: sim_id 00000000-0000-0000-0000-000000000001 does not exist in sims`; happy-path INSERT succeeded |
| 2 | F-A2+F-A7 | Compliance (doc drift) | `migrations/20260417000004_sms_outbound_recover.up.sql` header comment; `docs/brainstorming/decisions.md` DEV-239 | Replaced "Application code (internal/store/sms_outbound.go) enforces..." wording with "DB trigger (`check_sim_exists` BEFORE INSERT/UPDATE) enforces... plus handler layer (`internal/api/sms/handler.go` Send via `simStore.GetByID` before Insert) validates as belt-and-suspenders." Kept trailing `ACCEPTED` pipe intact. | grep confirms new wording present; old misleading wording removed |
| 3 | F-A4 | Compliance (tracking) | `docs/ROUTEMAP.md` | Renumbered STORY-086 follow-up row from duplicated `D-027` to `D-032`; kept line-375 STORY-079 F-U1 row as `D-027` (chronologically older — it is the original claimant). Updated DEV-239 cell reference from "tracked as tech debt for a future story" to "tracked as **D-032** in ROUTEMAP Tech Debt". | `grep -c '^\| D-027 \|' docs/ROUTEMAP.md` → 1 (single STORY-079 F-U1); `grep -c '^\| D-032 \|' docs/ROUTEMAP.md` → 1 (STORY-086 DEV-239 follow-up) |
| 4 | F-A5 | Compliance (manifest clarity) | `internal/store/schemacheck/schemacheck.go:19` | Added trailing comment `// global seed table — no RLS (per STORY-069)` on `"notification_templates"`. Count still 12, sort order preserved. | `go build ./...` PASS; `go test ./internal/store/schemacheck/... -count=1` 3/3 PASS |
| 5 | F-A6 | Test (failure-path coverage) | `internal/store/schemacheck/schemacheck_test.go` | Added `TestVerify_MissingTableReportsError` — probes `Verify` with synthetic `_schemacheck_missing_test_table_`, asserts err != nil + err names the missing table. Guarded by `DATABASE_URL` skip (project convention). **No DDL side-effects** (advisor-recommended variant of scout's rename approach — safer since a failed rename mid-test would leave live stack broken). | Ran with `DATABASE_URL=postgres://argus:argus_secret@localhost:5450/argus?sslmode=disable` → 3/3 PASS including the new test; without `DATABASE_URL` → skipped cleanly |

## Escalated Issues

None. No architectural questions, no missing-feature escalations.

## Deferred Items (tracked in ROUTEMAP → Tech Debt)

| # | Finding | Target Story | Written to ROUTEMAP |
|---|---------|-------------|---------------------|
| D-033 | F-B1 — `internal/policy/dryrun/service_test.go:333` non-pointer `json.Unmarshal` flagged by `go vet`. Pre-existing from STORY-024 commit d9acf3d, out of STORY-086 scope. Fix is a one-line `violations` → `&violations` change but requires reviewing the surrounding decode pattern. | Follow-up story | YES |

Note: D-032 (STORY-086 DEV-239 fresh-volume sim_id FK follow-up) was already added by the developer during implementation and is preserved — it is the follow-up for the original `20260413000001` defect, not a Gate-discovered item.

## Performance Summary

- **Boot-time schemacheck cost**: 12 `to_regclass` round-trips × ~1-2 ms = 12-24 ms total (one-time, synchronous, on every `serve` start). Accepted per plan Risks section; batching into a single `to_regclass(unnest(...))` is a future optimisation.
- **Trigger overhead per INSERT**: single `EXISTS (SELECT 1 FROM sims WHERE id = NEW.sim_id)` lookup — cheap (PK index hit). The handler already calls `simStore.GetByID` upstream, so hot-path cost is bounded by Redis rate-limit + Redis text cache + job enqueue — trigger adds one more indexed DB probe per insert. Acceptable for SMS volumes.

## Token & Component Enforcement

N/A — no UI changes.

## Verification

- **Go build after fixes**: `go build ./...` → PASS
- **Schemacheck unit tests**: 2/2 PASS (no `DATABASE_URL`)
- **Schemacheck integration tests**: 3/3 PASS (with `DATABASE_URL` — includes new failure-path test)
- **SMS store tests**: 19/19 PASS (baseline match)
- **Full Go test suite**: 2879/2879 PASS (baseline match — 0 regressions)
- **`go vet ./internal/store/schemacheck/...`**: PASS (0 issues)
- **Live DB trigger verification**:
  - `SELECT tgname FROM pg_trigger WHERE tgrelid = 'sms_outbound'::regclass AND NOT tgisinternal;` → `trg_sms_outbound_check_sim` (enabled `O`)
  - Bogus sim_id INSERT → `ERROR: FK violation: sim_id 00000000-0000-0000-0000-000000000001 does not exist in sims` (via `foreign_key_violation` SQLSTATE)
  - Happy-path INSERT with a real tenant/sim pair → succeeded (`id=36dcd986-8565-4e8a-917d-0051bf686285`); cleaned up.
- **Live API smoke**: `GET /api/v1/sms/history` with admin bearer token → HTTP 200, well-formed envelope with populated rows.
- **Fix iterations**: 1 (no second-pass needed)

## Passed Items

- Repair migration idempotent on re-apply (`NOTICE: relation "sms_outbound" already exists, skipping` for every object, `DROP TRIGGER IF EXISTS / CREATE TRIGGER` reinstalls cleanly)
- Trigger function definition is **byte-exact** with `20260412000007_fk_integrity_triggers.up.sql:4-16` (advisor-flagged constraint — future greps for identical definitions will succeed)
- Schemacheck manifest count still 12 (STORY-069 × 7 + STORY-077 × 5); sort order preserved
- DEV-239 row integrity maintained (single `| ACCEPTED |` terminator, no broken pipes)
- ROUTEMAP D-032 row preserves STORY-086 DEV-239 context; new D-033 row tracks F-B1 deferral; D-027 duplicate resolved
- Boot-path contract unchanged (still FATAL on any missing table, runs before pool gauge / read replica / listeners)
- Zero FE code touched; SCR-132 render unaffected

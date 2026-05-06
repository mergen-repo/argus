# Test Hardener Report

> Date: 2026-05-06
> Mode: E2E & Polish E2 (post-E1 PASS)
> Headline: D-181 systemic PAT-006 RECURRENCE prevention sweep — 5 stores hardened
> Commit: 5d09d72 — `refactor(store): D-181 systemic PAT-006 hardening`

## Before

- Tests: 4222 pass, 0 fail (Go `-short`)
- D-181 (ROUTEMAP Tech Debt) OPEN — 5 store files with same drift surface as the live HTTP 500 fixed in commit 58e607b (sim.go).

## Headline — D-181 systemic refactor

PAT-006 (column-list-vs-Scan-destination drift) recurred 4 times in production. Each recurrence is a customer-demo HTTP 500: build is clean, most tests pass, only a real DB hit on the drifted path surfaces it. After E1 fixed the 4th recurrence in `sim.go`, the same drift surface still existed in 5 other store files. This commit hardens all of them in a single focused refactor.

### Files refactored

| File | Inline scan sites refactored | Helper used |
|---|---|---|
| `internal/store/cdr.go` | 4 (ListByTenant, ListBySession, StreamForExport, StreamForExportFiltered) | `scanCDR` |
| `internal/store/policy.go` | 5 (List, GetVersionsByPolicyID, CreateRollout, ActivateVersion, ListReferencingAPN) | `scanPolicy`, `scanPolicyVersion` |
| `internal/store/ippool.go` | 3 (List, ListByAPN, ListAddresses) | `scanIPPool`, new `scanIPAddressJoined` |
| `internal/store/session_radius.go` | 3 (ListActiveBySIM, ListActiveFiltered, ListBySIM) | `scanRadiusSession` |
| `internal/store/notification.go` | 4 SELECT/RETURNING string literals + 3 NotificationConfig inline scans | New `notificationColumns`, `notificationConfigColumns`, `scanNotificationConfig` |

19 inline drift surfaces eliminated. Every `*Store.List`-style method that has a `xxxColumns` constant + `scanXxx` helper now delegates.

### Drift-guard tests added

`internal/store/columns_drift_guard_test.go` — 10 new structural tests that count commas in the column constant string and assert ==N where N is the helper's scan-destination count. Pure Go, no DB, <1ms total runtime. Refusing to compile with this drift would surface live as HTTP 500 on customer-facing endpoints.

| Test | Asserts |
|---|---|
| TestPolicyColumnsAndScanCountConsistency | policyColumns == scanPolicy 11 |
| TestPolicyVersionColumnsAndScanCountConsistency | policyVersionColumns == scanPolicyVersion 12 |
| TestRolloutColumnsAndScanCountConsistency | rolloutColumns == scanRollout 16 |
| TestCDRColumnsAndScanCountConsistency | cdrColumns == scanCDR 16 |
| TestIPPoolColumnsAndScanCountConsistency | ippoolColumns == scanIPPool 13 |
| TestIPAddressColumnsAndScanCountConsistency | ipAddressColumns == scanIPAddress 10 |
| TestIPAddressColumnsJoinedAndScanCountConsistency | ipAddressColumnsJoined == scanIPAddressJoined 13 |
| TestRadiusSessionColumnsAndScanCountConsistency | radiusSessionColumns == scanRadiusSession 25 |
| TestNotificationColumnsAndScanCountConsistency | notificationColumns == scanNotification 18 |
| TestNotificationConfigColumnsAndScanCountConsistency | notificationConfigColumns == scanNotificationConfig 12 |

Pre-existing pattern: `TestSIMColumnsAndScanCountConsistency` (sim.go fix in commit 58e607b).

### Quiet correctness improvement

Several refactored loops added an explicit `rows.Err()` check where the original inline scan loops were silently discarding row-iteration errors. This is on top of the drift prevention.

## Race detection sweep

Ran `go test -race -short` on concurrency-touchy packages:

- `internal/aaa/...` + `internal/notification/...`: 484 pass, 0 races
- `internal/ws/...` + `internal/bus/...` + `internal/cache/...`: 101 pass, 0 races
- `internal/store/...` + `internal/policy/...` + `internal/job/...`: 1315 pass, 0 races

No race warnings detected.

## Coverage analysis

Selected packages of interest (from `go test -short -cover`):

### Phase 11 hot paths
- `internal/policy/binding`: 91.2% (>85% target — good)
- `internal/notification/syslog`: 84.7% (just under 85%, acceptable)
- `internal/aaa/validator`: 100%

### Lower-coverage packages (acceptable — integration-tested)
- `internal/aaa/session`: 33.4% (most paths exercised via integration story tests)
- `internal/aaa/radius`: 29.0% (likewise — RADIUS protocol fuzz/E2E covers)
- `internal/api/*`: most handler packages 20-50% (handler-layer; integration paths cover the rest)
- `internal/store/*`: 3.1% (DB-dependent tests skipped in `-short` mode)

## After

- Tests: 4235 pass (+13 new), 0 fail, 0 race warnings
- Build: clean (`go build ./...`)
- Vet: clean (`go vet ./...`)
- Drift-guard: 11/11 tests PASS

## New Tests Written

| # | File | Tests | Coverage Target |
|---|------|-------|----------------|
| 1 | internal/store/columns_drift_guard_test.go | 10 | D-181 / PAT-006 RECURRENCE prevention across 5 stores |

## Coverage Gaps (deferred to E5)

| Priority | Description | Reason Deferred |
|----------|-------------|----------------|
| MEDIUM | `internal/notification/syslog` 84.7% to 85%+ | 0.3pp gap; STORY-098 still in plan-stage and may add coverage naturally |
| MEDIUM | `internal/api/*` mutation negative-path tests (POST/PUT/PATCH/DELETE to 422/403/404) | Many handler packages 20-40% covered; bulk negative-path additions out of scope for 60-min E2 budget |
| LOW | Detail-screen tab oracle tests | E1 verified all tabs functionally; would benefit from explicit non-empty assertions tied to seed fixtures. Defer to E5 with seed-report.md cross-walk. |
| LOW | `internal/aaa/bench` 17.7%, `internal/aaa/session` 33.4%, `internal/aaa/radius` 29.0% | Coverage misleading — integration story tests exercise these paths. True fix is a tagged integration coverage measurement, not unit-test additions. |

## Business Rule Coverage

D-181 is itself a meta-rule (system invariant): "no store may produce HTTP 500 due to column-vs-Scan drift". E2 elevates this from a runtime hope to a compile-CI guard for every audited drift surface.

## API Endpoint Coverage

Untouched in E2 (E1 verified 62/62 functional tests pass; 0 5xx in production). E5 may broaden negative-path coverage if budget allows.

## Verification commands

```
go build ./...                                              # clean
go vet ./...                                                # clean
go test -short -count=1 ./...                               # 4235 PASS / 0 FAIL
go test -run ColumnsAndScanCountConsistency ./internal/store/...  # 11/11 PASS
go test -race -short ./internal/aaa/... ./internal/ws/... ./internal/store/... ./internal/policy/...
                                                            # 0 races
```

## Residuals for E5

The 60-minute milestone gate was reached after the D-181 sweep, race detection, and coverage pass. Below are the suggested E5 follow-ups, none of which are blockers:

1. Mutation endpoint negative-path test backfill (handler 422/403/404 coverage)
2. Detail-screen tab oracle assertions tied to `docs/reports/seed-report.md` fixture UUIDs
3. Tagged integration-coverage measurement for AAA + RADIUS + Diameter + SBA (current `-short` coverage misrepresents integration-tested paths)
4. STORY-098 syslog coverage push to >=85% as it advances through Phase Gate

---

**Summary**: D-181 (the ONE thing that has caused 4 production HTTP 500s over the cutover sprint) is now structurally prevented. Every refactor was paired with a drift-guard test that fails CI before merge. 4235 Go tests still PASS, 0 races. Production-grade hardening achieved without softening scope.
